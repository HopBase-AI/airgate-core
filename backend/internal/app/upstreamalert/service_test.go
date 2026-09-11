package upstreamalert

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
)

type fakeRepo struct {
	acct     Account
	found    bool
	acctErr  error
	admins   []int
	adminErr error
}

func (f *fakeRepo) Account(context.Context, int) (Account, bool, error) {
	return f.acct, f.found, f.acctErr
}

func (f *fakeRepo) AdminUserIDs(context.Context) ([]int, error) {
	return f.admins, f.adminErr
}

type fakeNotifier struct {
	sent []appnotification.CreateInput
	seen map[string]bool
	err  error
}

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{seen: map[string]bool{}} }

// Create 复刻真实实现的去重语义：dedupe_key 命中过就返回 created=false 且不报错。
func (f *fakeNotifier) Create(_ context.Context, in appnotification.CreateInput) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.seen[in.DedupeKey] {
		return false, nil
	}
	f.seen[in.DedupeKey] = true
	f.sent = append(f.sent, in)
	return true, nil
}

func newTestService(repo *fakeRepo, notifier *fakeNotifier, now time.Time) *Service {
	s := NewService(repo, notifier)
	s.now = func() time.Time { return now }
	return s
}

func baseRepo() *fakeRepo {
	return &fakeRepo{
		acct:   Account{ID: 90, Name: "jinkundong-0.25", Platform: "openai"},
		found:  true,
		admins: []int{1, 7},
	}
}

const creditReason = "HTTP 403: 用户额度不足, 剩余额度: ¥-1.297390"

func TestOnAccountEventNotifiesEveryAdmin(t *testing.T) {
	repo, notifier := baseRepo(), newFakeNotifier()
	svc := newTestService(repo, notifier, time.Unix(1789000000, 0))

	svc.OnAccountEvent(context.Background(), 90, creditReason, 403)

	if len(notifier.sent) != 2 {
		t.Fatalf("投递条数 = %d, want 2（两个管理员各一条）", len(notifier.sent))
	}
	got := notifier.sent[0]
	if got.Level != appnotification.LevelDanger {
		t.Errorf("level = %q, want danger", got.Level)
	}
	if !strings.Contains(got.Title, "jinkundong-0.25") {
		t.Errorf("标题里没带账号名: %q", got.Title)
	}
	// 正文要足够管理员直接行动：账号、平台、上游状态码、原文都得在
	for _, want := range []string{"#90", "openai", "HTTP 403", "剩余额度"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("正文缺少 %q: %q", want, got.Content)
		}
	}
	if notifier.sent[0].UserID == notifier.sent[1].UserID {
		t.Error("两条通知发给了同一个人")
	}
}

func TestOnAccountEventIgnoresNonCreditFailures(t *testing.T) {
	// 上游挂了、限流、凭证失效都能靠 failover 绕过，报警只会变成噪声
	for _, reason := range []string{
		"Our servers are currently overloaded. Please try again later.",
		"upstream produced no output before the gateway idle limit",
		"Incorrect API key provided",
	} {
		repo, notifier := baseRepo(), newFakeNotifier()
		svc := newTestService(repo, notifier, time.Unix(1789000000, 0))
		svc.OnAccountEvent(context.Background(), 90, reason, 502)
		if len(notifier.sent) != 0 {
			t.Errorf("reason=%q 不该报警，却投了 %d 条", reason, len(notifier.sent))
		}
	}
}

func TestNotifyDedupesWithinTheHourAndRepeatsAfter(t *testing.T) {
	repo, notifier := baseRepo(), newFakeNotifier()
	start := time.Unix(1789000000, 0)
	svc := newTestService(repo, notifier, start)

	// 一次欠费会连着打爆很多请求，同一小时内只该提醒一次
	for range 5 {
		svc.OnAccountEvent(context.Background(), 90, creditReason, 403)
	}
	if len(notifier.sent) != 2 {
		t.Fatalf("同小时内投递 = %d, want 2（两个管理员各一条）", len(notifier.sent))
	}

	// 欠费不会自愈：跨到下一个小时要再提醒一次，免得第一条被划走就没人管了
	svc.now = func() time.Time { return start.Add(time.Hour) }
	svc.OnAccountEvent(context.Background(), 90, creditReason, 403)
	if len(notifier.sent) != 4 {
		t.Fatalf("跨小时后投递 = %d, want 4", len(notifier.sent))
	}
}

func TestNotifySeparatesAccounts(t *testing.T) {
	repo, notifier := baseRepo(), newFakeNotifier()
	svc := newTestService(repo, notifier, time.Unix(1789000000, 0))
	svc.OnAccountEvent(context.Background(), 90, creditReason, 403)

	repo.acct = Account{ID: 89, Name: "pptoken-0.2", Platform: "openai"}
	svc.OnAccountEvent(context.Background(), 89, creditReason, 403)

	if len(notifier.sent) != 4 {
		t.Fatalf("两个账号各两条 = %d, want 4", len(notifier.sent))
	}
}

func TestNotifyToleratesRepoFailures(t *testing.T) {
	cases := map[string]*fakeRepo{
		"账号查不到":  {found: false, admins: []int{1}},
		"读账号出错":  {acctErr: errors.New("boom"), admins: []int{1}},
		"读管理员出错": {acct: Account{ID: 90}, found: true, adminErr: errors.New("boom")},
		"没有管理员":  {acct: Account{ID: 90}, found: true, admins: nil},
	}
	for name, repo := range cases {
		t.Run(name, func(t *testing.T) {
			notifier := newFakeNotifier()
			svc := newTestService(repo, notifier, time.Unix(1789000000, 0))
			svc.OnAccountEvent(context.Background(), 90, creditReason, 403)
			if len(notifier.sent) != 0 {
				t.Errorf("异常路径不该投递，却投了 %d 条", len(notifier.sent))
			}
		})
	}
}

func TestNotifyTruncatesLongUpstreamText(t *testing.T) {
	repo, notifier := baseRepo(), newFakeNotifier()
	svc := newTestService(repo, notifier, time.Unix(1789000000, 0))
	svc.OnAccountEvent(context.Background(), 90, "额度不足"+strings.Repeat("请", 400), 403)

	if len(notifier.sent) == 0 {
		t.Fatal("没有投递")
	}
	if !strings.HasSuffix(notifier.sent[0].Content, "…") {
		t.Error("超长原文没有被截断")
	}
}
