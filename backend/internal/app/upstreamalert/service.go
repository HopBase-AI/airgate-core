package upstreamalert

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
)

// reasonExcerptLimit 通知正文里保留的上游原文长度。够看清是哪种欠费即可，
// 中继常把一长串 request id 拼在后面，全带上只会把通知撑得没法读。
const reasonExcerptLimit = 160

// alertInterval 同一账号的重报间隔。欠费不会自愈，报一次就沉底容易被划走，
// 所以按小时重报；真去充了值，下一次请求成功就不再有事件，自然停。
const alertInterval = time.Hour

// Service 上游欠费预警引擎。
type Service struct {
	repo     Repository
	notifier Notifier
	now      func() time.Time
}

// NewService 创建预警引擎。
func NewService(repo Repository, notifier Notifier) *Service {
	return &Service{repo: repo, notifier: notifier, now: time.Now}
}

// OnAccountEvent 账号异常事件落库后的回调。调用方已在独立 goroutine 里且做了 recover，
// 这里只要保证不 panic、不长时间阻塞即可。非欠费事件直接返回，开销是一次字符串扫描。
func (s *Service) OnAccountEvent(ctx context.Context, accountID int, reason string, upstreamStatus int) {
	if s == nil || accountID <= 0 || !IsOutOfCredit(reason) {
		return
	}
	s.Notify(ctx, accountID, reason, upstreamStatus)
}

// Notify 给全部管理员投一条欠费通知（OnAccountEvent 的工作体，测试直接调用）。
func (s *Service) Notify(ctx context.Context, accountID int, reason string, upstreamStatus int) {
	acct, found, err := s.repo.Account(ctx, accountID)
	if err != nil {
		slog.Error("upstream_credit_alert_load_account_failed", "account_id", accountID, "error", err)
		return
	}
	if !found {
		return
	}

	admins, err := s.repo.AdminUserIDs(ctx)
	if err != nil {
		slog.Error("upstream_credit_alert_load_admins_failed", "account_id", accountID, "error", err)
		return
	}
	if len(admins) == 0 {
		slog.Warn("upstream_credit_alert_no_admin", "account_id", accountID)
		return
	}

	title := fmt.Sprintf("上游账号欠费：%s", displayName(acct))
	content := composeContent(acct, reason, upstreamStatus)
	bucket := s.now().Truncate(alertInterval).Unix()

	var sent int
	for _, admin := range admins {
		created, err := s.notifier.Create(ctx, appnotification.CreateInput{
			UserID:    admin,
			Kind:      appnotification.KindSystem,
			Level:     appnotification.LevelDanger,
			Title:     title,
			Content:   content,
			Link:      "/admin/accounts",
			DedupeKey: fmt.Sprintf("upstream_credit:%d:%d:%d", acct.ID, bucket, admin),
		})
		if err != nil {
			slog.Error("upstream_credit_alert_notify_failed",
				"account_id", accountID, "recipient", admin, "error", err)
			continue
		}
		if created {
			sent++
		}
	}
	if sent > 0 {
		slog.Warn("upstream_credit_alert_sent",
			"account_id", acct.ID, "account_name", acct.Name,
			"upstream_status", upstreamStatus, "recipients", sent)
	}
}

func displayName(acct Account) string {
	if strings.TrimSpace(acct.Name) != "" {
		return acct.Name
	}
	return fmt.Sprintf("#%d", acct.ID)
}

func composeContent(acct Account, reason string, upstreamStatus int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "账号 %s（#%d", displayName(acct), acct.ID)
	if acct.Platform != "" {
		fmt.Fprintf(&b, " · %s", acct.Platform)
	}
	b.WriteString("）被上游判为余额不足，该账号已无法服务请求。")
	if upstreamStatus > 0 {
		fmt.Fprintf(&b, "上游返回 HTTP %d。", upstreamStatus)
	}
	b.WriteString("欠费不会自行恢复，failover 也兜不住整池同时欠费，请尽快去上游充值。\n\n上游原文：")
	b.WriteString(excerpt(reason))
	return b.String()
}

func excerpt(reason string) string {
	trimmed := strings.TrimSpace(reason)
	runes := []rune(trimmed)
	if len(runes) <= reasonExcerptLimit {
		return trimmed
	}
	return string(runes[:reasonExcerptLimit]) + "…"
}
