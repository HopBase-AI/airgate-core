package member

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// 带密码即建账号：邮箱小写、密码 bcrypt、白名单去重并只能选企业主可见分组（stub 可见 [1 2 3]）。
func TestCreateWithPasswordCreatesAccount(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo)
	item, err := svc.Create(context.Background(), 7, CreateInput{
		Name: "张三", Email: " Zhang.San@Example.com ", Password: "secret6", QuotaUSD: 10,
		AllowedGroupIDs: []int64{2, 2, 3},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repo.accountCreated == nil {
		t.Fatalf("应走 CreateWithAccount")
	}
	if repo.accountCreated.Email != "zhang.san@example.com" || repo.accountCreated.Username != "张三" {
		t.Fatalf("account = %+v", *repo.accountCreated)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repo.accountCreated.PasswordHash), []byte("secret6")); err != nil {
		t.Fatalf("密码应 bcrypt 存储: %v", err)
	}
	if got := repo.created.AllowedGroupIDs; len(got) != 2 || got[0] != 2 || got[1] != 3 || !repo.created.HasAllowedGroupIDs {
		t.Fatalf("allowed groups = %v, want 去重后 [2 3]", got)
	}
	if item.AccountUserID == 0 || item.AccountEmail != "zhang.san@example.com" {
		t.Fatalf("item = %+v", item)
	}
}

func TestCreateWithPasswordValidation(t *testing.T) {
	cases := []struct {
		name  string
		input CreateInput
		taken bool
		want  error
	}{
		{"缺邮箱", CreateInput{Name: "a", Password: "secret6"}, false, ErrEmailRequired},
		{"邮箱格式", CreateInput{Name: "a", Email: "not-an-email", Password: "secret6"}, false, ErrInvalidEmail},
		{"密码太短", CreateInput{Name: "a", Email: "a@b.co", Password: "12345"}, false, ErrPasswordTooShort},
		{"邮箱被占", CreateInput{Name: "a", Email: "a@b.co", Password: "secret6"}, true, ErrEmailAlreadyExists},
		{"分组越界", CreateInput{Name: "a", Email: "a@b.co", Password: "secret6", AllowedGroupIDs: []int64{9}}, false, ErrGroupNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepo{emailTaken: tc.taken}
			_, err := NewService(repo).Create(context.Background(), 7, tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if repo.accountCreated != nil {
				t.Fatalf("校验失败不应建账号")
			}
		})
	}
}

// 不带密码沿用老模型：不建账号，白名单为空表示继承全部。
func TestCreateWithoutPasswordKeepsLegacyModel(t *testing.T) {
	repo := &stubRepo{}
	item, err := NewService(repo).Create(context.Background(), 7, CreateInput{Name: "老成员"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repo.accountCreated != nil || item.AccountUserID != 0 {
		t.Fatalf("不带密码不应建账号: %+v", item)
	}
	if !repo.created.HasAllowedGroupIDs || len(repo.created.AllowedGroupIDs) != 0 {
		t.Fatalf("白名单应写成空集: %+v", repo.created)
	}
}

// 编辑：有账号的成员改邮箱/重置密码走账号补丁；老成员改密码报无账号；白名单整体替换。
func TestUpdateAccountFieldsAndGroups(t *testing.T) {
	repo := &stubRepo{find: Member{ID: 3, OwnerID: 7, AccountUserID: 42, AccountEmail: "old@example.com", QuotaPeriod: QuotaPeriodMonthly}}
	svc := NewService(repo)
	newEmail := "New@Example.com"
	pw := "newpass6"
	groups := []int64{1}
	if _, err := svc.Update(context.Background(), 7, 3, UpdateInput{Email: &newEmail, Password: &pw, AllowedGroupIDs: &groups}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if repo.accountPatch == nil || repo.accountPatch.Email == nil || *repo.accountPatch.Email != "new@example.com" || repo.accountPatch.PasswordHash == nil {
		t.Fatalf("account patch = %+v", repo.accountPatch)
	}
	if repo.updated.Email == nil || *repo.updated.Email != "new@example.com" {
		t.Fatalf("成员邮箱应同步小写: %+v", repo.updated.Email)
	}
	if !repo.updated.HasAllowedGroupIDs || len(repo.updated.AllowedGroupIDs) != 1 {
		t.Fatalf("白名单未整体替换: %+v", repo.updated)
	}

	// 密码留空 = 不改；邮箱不变 = 不打账号补丁
	repo2 := &stubRepo{find: Member{ID: 3, OwnerID: 7, AccountUserID: 42, AccountEmail: "old@example.com"}}
	empty := ""
	same := "old@example.com"
	if _, err := NewService(repo2).Update(context.Background(), 7, 3, UpdateInput{Email: &same, Password: &empty}); err != nil {
		t.Fatalf("Update(noop): %v", err)
	}
	if repo2.accountPatch != nil {
		t.Fatalf("无实质改动不应打账号补丁: %+v", repo2.accountPatch)
	}

	// 老模型成员改密码 → ErrMemberNoAccount
	repo3 := &stubRepo{find: Member{ID: 4, OwnerID: 7}}
	if _, err := NewService(repo3).Update(context.Background(), 7, 4, UpdateInput{Password: &pw}); !errors.Is(err, ErrMemberNoAccount) {
		t.Fatalf("err = %v, want ErrMemberNoAccount", err)
	}
	// 清空白名单 = 继承全部
	repo4 := &stubRepo{find: Member{ID: 5, OwnerID: 7}}
	none := []int64{}
	if _, err := NewService(repo4).Update(context.Background(), 7, 5, UpdateInput{AllowedGroupIDs: &none}); err != nil {
		t.Fatalf("Update(clear groups): %v", err)
	}
	if !repo4.updated.HasAllowedGroupIDs || len(repo4.updated.AllowedGroupIDs) != 0 {
		t.Fatalf("清空白名单应写空集: %+v", repo4.updated)
	}
}
