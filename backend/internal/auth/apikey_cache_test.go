package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// apikey_cache_test.go — API Key 验证缓存失效（本地 + Redis 两级）测试。

func seedLocalCache(t *testing.T, hash string) {
	t.Helper()
	apiKeyCache.Store(hash, apiKeyCacheEntry{
		info:      &APIKeyInfo{KeyID: 1},
		expiresAt: time.Now().Add(time.Hour),
	})
}

func localCacheHas(hash string) bool {
	_, ok := apiKeyCache.Load(hash)
	return ok
}

func TestInvalidateAPIKeyCacheByHash(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	SetAPIKeyCacheRedis(rdb)
	t.Cleanup(func() { SetAPIKeyCacheRedis(nil) })

	hash := HashAPIKey("sk-test-by-hash")
	seedLocalCache(t, hash)
	if err := rdb.Set(context.Background(), apiKeyRedisCacheKey(hash), "cached", time.Hour).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	InvalidateAPIKeyCacheByHash(hash)

	if localCacheHas(hash) {
		t.Fatal("本地缓存应被清除")
	}
	if mr.Exists(apiKeyRedisCacheKey(hash)) {
		t.Fatal("Redis 缓存应被清除")
	}
}

func TestInvalidateAPIKeyCacheByHashWithoutRedis(t *testing.T) {
	SetAPIKeyCacheRedis(nil)
	hash := HashAPIKey("sk-test-no-redis")
	seedLocalCache(t, hash)

	InvalidateAPIKeyCacheByHash(hash) // Redis 未配置分支：只清本地，不 panic

	if localCacheHas(hash) {
		t.Fatal("本地缓存应被清除")
	}
}

func TestInvalidateAPIKeyCacheDelegates(t *testing.T) {
	SetAPIKeyCacheRedis(nil)
	key := "sk-test-delegate"
	hash := HashAPIKey(key)
	seedLocalCache(t, hash)

	InvalidateAPIKeyCache(key) // 明文入口应委托到按 hash 失效

	if localCacheHas(hash) {
		t.Fatal("按明文失效应命中同一 hash 键")
	}
}

func TestInvalidateAPIKeyCacheAll(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	SetAPIKeyCacheRedis(rdb)
	t.Cleanup(func() { SetAPIKeyCacheRedis(nil) })

	h1, h2 := HashAPIKey("sk-all-1"), HashAPIKey("sk-all-2")
	seedLocalCache(t, h1)
	seedLocalCache(t, h2)

	InvalidateAPIKeyCache("") // 空串 = 清全部

	if localCacheHas(h1) || localCacheHas(h2) {
		t.Fatal("清全部后本地缓存应为空")
	}
}
