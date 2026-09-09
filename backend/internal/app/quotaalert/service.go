package quotaalert

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// 展示用北京时区：周期截止日对用户按北京时间呈现。
var beijingTZ = time.FixedZone("Asia/Shanghai", 8*3600)

// Service 额度预警引擎。
type Service struct {
	repo      Repository
	notifier  Notifier
	sendEmail EmailFunc
	now       func() time.Time
}

// NewService 创建额度预警引擎；邮件发送经 SetEmailSender 注入（未注入只投站内通知）。
func NewService(repo Repository, notifier Notifier) *Service {
	return &Service{repo: repo, notifier: notifier, now: time.Now}
}

// SetEmailSender 注入系统邮件发送口。
func (s *Service) SetEmailSender(fn EmailFunc) {
	s.sendEmail = fn
}

// OnCharged billing.Recorder 扣费提交后的回调：异步复核，绝不阻塞计费协程；
// 请求 context 可能已取消，改用不带取消的副本。
func (s *Service) OnCharged(ctx context.Context, ev billing.ChargeEvent) {
	if s == nil || (len(ev.MemberIDs) == 0 && len(ev.DepartmentIDs) == 0) {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("quota_alert_panic", "panic", rec)
			}
		}()
		s.Process(bg, ev)
	}()
}

// Process 同步复核一批成员 / 部门（OnCharged 的工作体，测试直接调用）。
func (s *Service) Process(ctx context.Context, ev billing.ChargeEvent) {
	now := s.now()
	if len(ev.MemberIDs) > 0 {
		members, err := s.repo.MemberSnapshots(ctx, ev.MemberIDs, now)
		if err != nil {
			slog.Error("quota_alert_load_members_failed", "member_ids", ev.MemberIDs, "error", err)
		}
		for _, m := range members {
			s.checkMember(ctx, m)
		}
	}
	if len(ev.DepartmentIDs) > 0 {
		departments, err := s.repo.DepartmentSnapshots(ctx, ev.DepartmentIDs, now)
		if err != nil {
			slog.Error("quota_alert_load_departments_failed", "department_ids", ev.DepartmentIDs, "error", err)
		}
		for _, d := range departments {
			s.checkDepartment(ctx, d)
		}
	}
}

// levelFor 按已用 / 额度判定级别：空串表示未达阈值；quota ≤ 0（不限）永不预警。
func levelFor(used, quota float64) string {
	if quota <= 0 {
		return ""
	}
	pct := used / quota * 100
	switch {
	case pct >= ExhaustPercent:
		return appnotification.LevelDanger
	case pct >= WarnPercent:
		return appnotification.LevelWarning
	default:
		return ""
	}
}

// 收件人规则：企业主 + 部门负责人的登录账号。成员本人不再收到自己的额度预警；
// 负责人是被预警成员本人时也不投（负责人只盯部门，本人的额度由企业主处理）。
const (
	ownerHint   = "请在团队管理中调整额度。"
	managerHint = "请关注本部门用量，如需调整额度请联系企业管理员。"
)

func (s *Service) checkMember(ctx context.Context, m MemberSnapshot) {
	level := levelFor(m.PeriodUsed, m.QuotaUSD)
	if level == "" {
		return
	}
	title := alertTitle("成员 "+m.Name, m.PeriodUsed, m.QuotaUSD, level)
	// 企业主：跳团队管理调额度。
	if m.OwnerID > 0 {
		s.deliver(ctx, alert{
			recipient:  m.OwnerID,
			email:      m.OwnerEmail,
			dedupeKey:  memberDedupeKey(m, level, m.OwnerID),
			level:      level,
			title:      title,
			content:    alertContent(m.PeriodUsed, m.QuotaUSD, m.PeriodEnd, ownerHint),
			link:       "/team",
			targetKind: "member",
			targetID:   m.ID,
		})
	}
	// 所属部门负责人：跳用量页按部门筛选；负责人就是本人或企业主时不重复投。
	if m.ManagerUserID > 0 && m.ManagerUserID != m.AccountUserID && m.ManagerUserID != m.OwnerID {
		s.deliver(ctx, alert{
			recipient:  m.ManagerUserID,
			email:      m.ManagerEmail,
			dedupeKey:  memberDedupeKey(m, level, m.ManagerUserID),
			level:      level,
			title:      title,
			content:    alertContent(m.PeriodUsed, m.QuotaUSD, m.PeriodEnd, managerHint),
			link:       departmentUsageLink(m.DepartmentID),
			targetKind: "member",
			targetID:   m.ID,
		})
	}
}

func (s *Service) checkDepartment(ctx context.Context, d DepartmentSnapshot) {
	level := levelFor(d.PeriodUsed, d.QuotaUSD)
	if level == "" {
		return
	}
	title := alertTitle("部门 "+d.Name, d.PeriodUsed, d.QuotaUSD, level)
	if d.OwnerID > 0 {
		s.deliver(ctx, alert{
			recipient:  d.OwnerID,
			email:      d.OwnerEmail,
			dedupeKey:  departmentDedupeKey(d, level, d.OwnerID),
			level:      level,
			title:      title,
			content:    alertContent(d.PeriodUsed, d.QuotaUSD, d.PeriodEnd, ownerHint),
			link:       "/team",
			targetKind: "department",
			targetID:   d.ID,
		})
	}
	if d.ManagerUserID > 0 && d.ManagerUserID != d.OwnerID {
		s.deliver(ctx, alert{
			recipient:  d.ManagerUserID,
			email:      d.ManagerEmail,
			dedupeKey:  departmentDedupeKey(d, level, d.ManagerUserID),
			level:      level,
			title:      title,
			content:    alertContent(d.PeriodUsed, d.QuotaUSD, d.PeriodEnd, managerHint),
			link:       departmentUsageLink(d.ID),
			targetKind: "department",
			targetID:   d.ID,
		})
	}
}

// 去重钥匙：对象:本期起点:级别:收件人——同一事件对每个收件人各去重一次，换期后自然重新预警。
func memberDedupeKey(m MemberSnapshot, level string, recipient int) string {
	return fmt.Sprintf("member:%d:%d:%s:%d", m.ID, m.PeriodStart.Unix(), level, recipient)
}

func departmentDedupeKey(d DepartmentSnapshot, level string, recipient int) string {
	return fmt.Sprintf("department:%d:%d:%s:%d", d.ID, d.PeriodStart.Unix(), level, recipient)
}

// departmentUsageLink 负责人跳用量页按部门筛选；成员未分配部门时退回用量页。
func departmentUsageLink(departmentID int) string {
	if departmentID <= 0 {
		return "/usage"
	}
	return fmt.Sprintf("/usage?department_id=%d", departmentID)
}

// alert 一条待投递的预警（站内 + 邮件）。
type alert struct {
	recipient  int
	email      string
	dedupeKey  string
	level      string
	title      string
	content    string
	link       string
	targetKind string
	targetID   int
}

// deliver 先投站内通知；只有真正新建（未被 dedupe 拦下）才发邮件，邮件与站内信共用一把去重钥匙。
func (s *Service) deliver(ctx context.Context, a alert) {
	created, err := s.notifier.Create(ctx, appnotification.CreateInput{
		UserID:    a.recipient,
		Kind:      appnotification.KindQuotaAlert,
		Level:     a.level,
		Title:     a.title,
		Content:   a.content,
		Link:      a.link,
		DedupeKey: a.dedupeKey,
	})
	if err != nil {
		slog.Error("quota_alert_notify_failed", "target", a.targetKind, "target_id", a.targetID,
			"recipient", a.recipient, "error", err)
		return
	}
	if !created {
		return
	}
	slog.Info("quota_alert_sent", "target", a.targetKind, "target_id", a.targetID,
		"recipient", a.recipient, "level", a.level)
	if s.sendEmail != nil && a.email != "" {
		s.sendEmail(a.email, a.title, renderEmail(a.title, a.content))
	}
}

// alertTitle 「成员 张三 本期额度已用 82%」/「部门 研发部 本期额度已用尽」。
func alertTitle(subject string, used, quota float64, level string) string {
	prefix := subject
	if !strings.HasSuffix(prefix, "的") {
		prefix += " "
	}
	if level == appnotification.LevelDanger {
		return prefix + "本期额度已用尽"
	}
	return fmt.Sprintf("%s本期额度已用 %d%%", prefix, int(used/quota*100))
}

// alertContent 「已用 $X / 额度 $Y，剩余 $Z，周期截止 2026-10-01。<hint>」
func alertContent(used, quota float64, periodEnd *time.Time, hint string) string {
	remaining := quota - used
	if remaining < 0 {
		remaining = 0
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已用 $%.2f / 额度 $%.2f，剩余 $%.2f", used, quota, remaining)
	if periodEnd != nil {
		fmt.Fprintf(&b, "，周期截止 %s（北京时间）", periodEnd.In(beijingTZ).Format("2006-01-02"))
	} else {
		b.WriteString("，该额度为一次性总额，不自动重置")
	}
	b.WriteString("。")
	b.WriteString(hint)
	return b.String()
}

// renderEmail 简单的系统邮件正文：标题 + 内容，文本已转义。
func renderEmail(title, content string) string {
	return `<div style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 480px; margin: 0 auto; background: #ffffff; border-radius: 8px; border: 1px solid #e5e7eb;">
<div style="padding: 32px 28px;">
<div style="font-size: 16px; font-weight: 600; color: #111; margin-bottom: 16px;">` + html.EscapeString(title) + `</div>
<p style="color: #555; font-size: 14px; line-height: 1.6; margin: 0;">` + html.EscapeString(content) + `</p>
</div>
<div style="border-top: 1px solid #f0f0f0; padding: 14px 28px;">
<p style="color: #c0c0c0; font-size: 11px; margin: 0; text-align: center;">此邮件由系统自动发送，请登录控制台查看详情</p>
</div>
</div>`
}
