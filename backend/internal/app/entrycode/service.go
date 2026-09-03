// Package entrycode 管理「客户入口码」:香港直连入口 direct.hop-base.com/c/<码>/ 用它区分客户。
//
// 存储:复用 settings 表单键 entry_codes_json(group=entry,非公开),值为 JSON 数组。
// 码基数很小(每客户一个),不值得单开 ent 表与迁移;与仓库既有「settings JSON 驱动配置」
// 惯例一致。绑定客户账号仅作展示/归属,鉴权仍由 API Key 决定——入口码不参与计费与权限。
package entrycode

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
)

const (
	settingKey   = "entry_codes_json"
	settingGroup = "entry"
	codeAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	codeLength   = 12
)

// 领域错误。
var (
	ErrNotFound     = errors.New("入口码不存在")
	ErrUserNotFound = errors.New("绑定的用户不存在")
	ErrConflict     = errors.New("入口码已存在")
)

// EntryCode 一个客户入口码及其归属与用量。
type EntryCode struct {
	Code         string    `json:"code"`
	UserID       int       `json:"user_id"`    // 0 = 未绑定客户
	UserEmail    string    `json:"user_email"` // 绑定时快照,便于列表直接展示
	Note         string    `json:"note"`       // 备注:客户名 / 用途
	Enabled      bool      `json:"enabled"`    // 展示用启停标记(当前不参与鉴权)
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastUsedAt   time.Time `json:"last_used_at,omitempty"`
	RequestCount int64     `json:"request_count"`
}

// Service 入口码用例编排。
type Service struct {
	settings *appsettings.Service
	users    *appuser.Service
}

// NewService 创建服务。users 用于绑定时校验并快照客户邮箱。
func NewService(settings *appsettings.Service, users *appuser.Service) *Service {
	return &Service{settings: settings, users: users}
}

// CreateInput 生成新入口码的输入。
type CreateInput struct {
	Note   string
	UserID int
}

// UpdateInput 更新一个入口码;指针字段为 nil 表示不改。
type UpdateInput struct {
	Note    *string
	Enabled *bool
	UserID  *int
}

// List 返回全部入口码(按创建时间倒序)。
func (s *Service) List(ctx context.Context) ([]EntryCode, error) {
	codes, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(codes, func(i, j int) bool { return codes[i].CreatedAt.After(codes[j].CreatedAt) })
	return codes, nil
}

// Create 生成一个新入口码。
func (s *Service) Create(ctx context.Context, in CreateInput) (EntryCode, error) {
	email, err := s.resolveUser(ctx, in.UserID)
	if err != nil {
		return EntryCode{}, err
	}
	codes, err := s.load(ctx)
	if err != nil {
		return EntryCode{}, err
	}
	now := time.Now()
	ec := EntryCode{
		Code:      generateCode(codes),
		UserID:    in.UserID,
		UserEmail: email,
		Note:      strings.TrimSpace(in.Note),
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	codes = append(codes, ec)
	if err := s.save(ctx, codes); err != nil {
		return EntryCode{}, err
	}
	return ec, nil
}

// Update 修改一个入口码。
func (s *Service) Update(ctx context.Context, code string, in UpdateInput) (EntryCode, error) {
	codes, err := s.load(ctx)
	if err != nil {
		return EntryCode{}, err
	}
	idx := indexOf(codes, code)
	if idx < 0 {
		return EntryCode{}, ErrNotFound
	}
	if in.Note != nil {
		codes[idx].Note = strings.TrimSpace(*in.Note)
	}
	if in.Enabled != nil {
		codes[idx].Enabled = *in.Enabled
	}
	if in.UserID != nil {
		email, err := s.resolveUser(ctx, *in.UserID)
		if err != nil {
			return EntryCode{}, err
		}
		codes[idx].UserID = *in.UserID
		codes[idx].UserEmail = email
	}
	codes[idx].UpdatedAt = time.Now()
	if err := s.save(ctx, codes); err != nil {
		return EntryCode{}, err
	}
	return codes[idx], nil
}

// Delete 删除一个入口码。
func (s *Service) Delete(ctx context.Context, code string) error {
	codes, err := s.load(ctx)
	if err != nil {
		return err
	}
	idx := indexOf(codes, code)
	if idx < 0 {
		return ErrNotFound
	}
	codes = append(codes[:idx], codes[idx+1:]...)
	return s.save(ctx, codes)
}

// resolveUser 校验绑定用户并返回其邮箱;userID<=0 视为未绑定。
func (s *Service) resolveUser(ctx context.Context, userID int) (string, error) {
	if userID <= 0 {
		return "", nil
	}
	u, err := s.users.Get(ctx, userID)
	if err != nil {
		return "", ErrUserNotFound
	}
	return u.Email, nil
}

func (s *Service) load(ctx context.Context) ([]EntryCode, error) {
	items, err := s.settings.List(ctx, settingGroup)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Key != settingKey {
			continue
		}
		if strings.TrimSpace(item.Value) == "" {
			return []EntryCode{}, nil
		}
		var codes []EntryCode
		if err := json.Unmarshal([]byte(item.Value), &codes); err != nil {
			return nil, err
		}
		return codes, nil
	}
	return []EntryCode{}, nil
}

func (s *Service) save(ctx context.Context, codes []EntryCode) error {
	raw, err := json.Marshal(codes)
	if err != nil {
		return err
	}
	return s.settings.Update(ctx, []appsettings.ItemInput{{
		Key:   settingKey,
		Value: string(raw),
		Group: settingGroup,
	}})
}

func indexOf(codes []EntryCode, code string) int {
	for i := range codes {
		if codes[i].Code == code {
			return i
		}
	}
	return -1
}

// generateCode 生成一个不与现有码冲突的随机码。
func generateCode(existing []EntryCode) string {
	for {
		var b strings.Builder
		buf := make([]byte, codeLength)
		_, _ = rand.Read(buf)
		for _, x := range buf {
			b.WriteByte(codeAlphabet[int(x)%len(codeAlphabet)])
		}
		code := b.String()
		if indexOf(existing, code) < 0 {
			return code
		}
	}
}
