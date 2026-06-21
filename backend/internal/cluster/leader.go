// Package cluster 提供跨实例的领导选举：保证同一时刻只有一个 core 实例运行那些
// "全局单例"的后台循环（插件后台任务、资产迁移/清理、配额刷新）。这样蓝绿 / 多实例
// 部署期间即便短暂同时跑两份 core，也不会重复轮询上游、重复计费或重复删资产。
//
// 机制：基于 Redis 的租约。每个实例周期性地用 Lua CAS 脚本「获取或续约」一把全局锁，
// 持有锁者即 leader；关停时主动释放，接班实例尽快接管。
package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const leaderKey = "airgate:core:leader"

// acquireScript 获取或续约：仅当锁空闲或已属于自己时才写入并刷新 TTL（原子 CAS）。
var acquireScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur == false or cur == ARGV[1] then
  redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
  return 1
end
return 0
`)

// releaseScript 释放：仅当锁属于自己时才删除，避免误删他人持有的锁。
var releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// Leader 基于 Redis 租约的领导选举器。请用 New 构造。
type Leader struct {
	rdb      *redis.Client
	id       string
	ttl      time.Duration
	isLeader atomic.Bool
}

// New 构造领导选举器。ttl 为租约时长，续约间隔为 ttl/3。
func New(rdb *redis.Client, ttl time.Duration) *Leader {
	return &Leader{rdb: rdb, id: instanceID(), ttl: ttl}
}

// IsLeader 返回本实例当前是否持有领导权。
// nil 接收者返回 true：让未接线领导选举的调用方退化为「单实例即领导」的旧行为。
func (l *Leader) IsLeader() bool {
	if l == nil {
		return true
	}
	return l.isLeader.Load()
}

// Start 先同步尝试获取一次（使随后「启动即执行一次」的循环能读到正确的领导态），
// 再起一个 goroutine 周期续约/重试，直到 ctx 取消。
func (l *Leader) Start(ctx context.Context) {
	if l == nil || l.rdb == nil {
		return
	}
	l.tryAcquire(ctx)
	go l.loop(ctx)
}

func (l *Leader) loop(ctx context.Context) {
	ticker := time.NewTicker(l.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.tryAcquire(ctx)
		}
	}
}

func (l *Leader) tryAcquire(ctx context.Context) {
	res, err := acquireScript.Run(ctx, l.rdb, []string{leaderKey}, l.id, l.ttl.Milliseconds()).Int()
	if err != nil {
		// Redis 异常时主动放弃领导权：宁可单例短暂暂停后台任务，
		// 也不要在网络分区时出现两个 leader 重复执行。
		if l.isLeader.Swap(false) {
			slog.Warn("leader_lease_lost", "reason", "redis_error", "error", err)
		}
		return
	}
	became := res == 1
	if became != l.isLeader.Swap(became) {
		slog.Info("leader_state_changed", "is_leader", became, "instance", l.id)
	}
}

// Resign 关停时主动释放租约，让接班实例尽快接管（否则需等 TTL 自然过期）。
func (l *Leader) Resign(ctx context.Context) {
	if l == nil || l.rdb == nil {
		return
	}
	l.isLeader.Store(false)
	_, _ = releaseScript.Run(ctx, l.rdb, []string{leaderKey}, l.id).Result()
}

// instanceID 生成本实例唯一标识：主机名 + PID + 随机后缀（同主机蓝绿双容器 PID 不同，
// 随机后缀再兜底极端碰撞）。
func instanceID() string {
	host, _ := os.Hostname()
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(buf))
}
