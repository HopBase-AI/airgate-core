package blog

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/internal/pkg/pagination"
)

var (
	// inviteCodeRe 与 auth.sanitizeInviteCode 保持一致:4~16 位字母数字。
	inviteCodeRe = regexp.MustCompile(`^[A-Za-z0-9]{4,16}$`)
	// slugStripRe 将非 [a-z0-9] 连续片段折叠为单个连字符。
	slugStripRe = regexp.MustCompile(`[^a-z0-9]+`)
)

// Service 提供博客域用例编排。
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService 创建博客服务。
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// List 查询文章列表(管理员传空 Status 取全部,公开页传 published)。
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	page, pageSize := pagination.Normalize(filter.Page, filter.PageSize)
	filter.Page = page
	filter.PageSize = pageSize

	list, total, err := s.repo.List(ctx, filter)
	if err != nil {
		sdk.LoggerFromContext(ctx).Error("blog_lookup_failed", "op", "list", sdk.LogFieldError, err)
		return ListResult{}, err
	}
	return ListResult{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get 按 ID 获取文章(管理员编辑回填)。
func (s *Service) Get(ctx context.Context, id int) (Post, error) {
	return s.repo.FindByID(ctx, id)
}

// GetPublishedBySlug 按 slug 获取已发布文章(公开详情页);未发布视为不存在。
func (s *Service) GetPublishedBySlug(ctx context.Context, slug string) (Post, error) {
	post, err := s.repo.FindBySlug(ctx, strings.TrimSpace(slug))
	if err != nil {
		return Post{}, err
	}
	if post.Status != StatusPublished {
		return Post{}, ErrPostNotFound
	}
	return post, nil
}

// Create 创建文章。
func (s *Service) Create(ctx context.Context, input CreateInput) (Post, error) {
	logger := sdk.LoggerFromContext(ctx)

	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return Post{}, ErrTitleRequired
	}

	code, err := normalizeInviteCode(input.InviteCode)
	if err != nil {
		return Post{}, err
	}
	input.InviteCode = code

	base := slugify(input.Slug)
	if base == "" {
		base = slugify(input.Title)
	}
	slug, err := s.ensureUniqueSlug(ctx, base, 0)
	if err != nil {
		return Post{}, err
	}
	input.Slug = slug

	input.ContentHTML = SanitizeHTML(input.ContentHTML)
	input.Status = normalizeStatus(input.Status)
	input.GatePosition = clampGate(input.GatePosition)
	input.Lang = normalizeLang(input.Lang)
	input.Sites = normalizeSites(input.Sites)
	if input.Status == StatusPublished {
		now := s.now()
		input.PublishedAt = &now
	}

	post, err := s.repo.Create(ctx, input)
	if err != nil {
		logger.Error("blog_persist_failed", "op", "create", "title", input.Title, sdk.LogFieldError, err)
		return Post{}, err
	}
	logger.Info("blog_create_succeeded", "id", post.ID, "slug", post.Slug, "status", post.Status)
	return post, nil
}

// Update 更新文章(部分字段)。
func (s *Service) Update(ctx context.Context, id int, input UpdateInput) (Post, error) {
	logger := sdk.LoggerFromContext(ctx)

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return Post{}, err
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return Post{}, ErrTitleRequired
		}
		input.Title = &title
	}

	if input.InviteCode != nil {
		code, err := normalizeInviteCode(*input.InviteCode)
		if err != nil {
			return Post{}, err
		}
		input.InviteCode = &code
	}

	if input.Slug != nil {
		base := slugify(*input.Slug)
		if base == "" {
			title := existing.Title
			if input.Title != nil {
				title = *input.Title
			}
			base = slugify(title)
		}
		slug, err := s.ensureUniqueSlug(ctx, base, id)
		if err != nil {
			return Post{}, err
		}
		input.Slug = &slug
	}

	if input.ContentHTML != nil {
		clean := SanitizeHTML(*input.ContentHTML)
		input.ContentHTML = &clean
	}

	if input.Status != nil {
		st := normalizeStatus(*input.Status)
		input.Status = &st
		// 首次转为已发布时补 published_at;取消发布不清空(保留历史发布时间)。
		if st == StatusPublished && existing.PublishedAt == nil {
			now := s.now()
			input.PublishedAt = &now
		}
	}

	if input.GatePosition != nil {
		g := clampGate(*input.GatePosition)
		input.GatePosition = &g
	}

	if input.Lang != nil {
		l := normalizeLang(*input.Lang)
		input.Lang = &l
	}

	if input.Sites != nil {
		sites := normalizeSites(*input.Sites)
		input.Sites = &sites
	}

	post, err := s.repo.Update(ctx, id, input)
	if err != nil {
		logger.Error("blog_persist_failed", "op", "update", "id", id, sdk.LogFieldError, err)
		return Post{}, err
	}
	logger.Info("blog_update_succeeded", "id", id)
	return post, nil
}

// Delete 删除文章。
func (s *Service) Delete(ctx context.Context, id int) error {
	logger := sdk.LoggerFromContext(ctx)
	if err := s.repo.Delete(ctx, id); err != nil {
		logger.Error("blog_persist_failed", "op", "delete", "id", id, sdk.LogFieldError, err)
		return err
	}
	logger.Info("blog_delete_succeeded", "id", id)
	return nil
}

// IncrementView 阅读量 +1;best-effort,失败不影响页面渲染。
func (s *Service) IncrementView(ctx context.Context, id int) {
	if err := s.repo.IncrementViewCount(ctx, id); err != nil {
		sdk.LoggerFromContext(ctx).Warn("blog_view_increment_failed", "id", id, sdk.LogFieldError, err)
	}
}

// ensureUniqueSlug 返回未被占用的 slug;冲突时追加 -2/-3…,兜底追加时间戳。
func (s *Service) ensureUniqueSlug(ctx context.Context, base string, excludeID int) (string, error) {
	if base == "" {
		base = "post-" + strconv.FormatInt(s.now().UnixNano(), 36)
	}
	slug := base
	for i := 2; i <= 100; i++ {
		exists, err := s.repo.SlugExists(ctx, slug, excludeID)
		if err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
		slug = base + "-" + strconv.Itoa(i)
	}
	// 极端冲突兜底:追加纳秒时间戳基本保证唯一。
	return base + "-" + strconv.FormatInt(s.now().UnixNano(), 36), nil
}

// slugify 将标题/输入转为 URL 友好 slug:小写、非字母数字折叠为连字符、去首尾连字符、限长。
// 纯中文标题会被折叠为空,交由 ensureUniqueSlug 生成随机 slug。
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugStripRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-")
	}
	return s
}

// normalizeInviteCode 校验并归一化邀请码(为空=未设置;非法=报错;合法=转小写)。
func normalizeInviteCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", nil
	}
	if !inviteCodeRe.MatchString(code) {
		return "", ErrInvalidInviteCode
	}
	return strings.ToLower(code), nil
}

// normalizeStatus 仅接受 draft/published,其余回退 draft。
func normalizeStatus(status string) string {
	if status == StatusPublished {
		return StatusPublished
	}
	return StatusDraft
}

// clampGate 将注册墙位置夹到 [0,100]。
func clampGate(pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > 100 {
		return 100
	}
	return pos
}

// normalizeLang 只接受博客公开切换器支持的三种语言；新文章默认繁体。
func normalizeLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "zh-cn", "zh-hans", "zh-sg":
		return LangSimplified
	case "en", "en-us", "en-gb":
		return LangEnglish
	case "zh-hant", "zh-hk", "zh-tw", "zh-mo":
		return LangTraditional
	default:
		return LangTraditional
	}
}

// normalizeSites 去空白、去空项、去重,保序;空/全空 → nil(表示所有站点可见)。
func normalizeSites(sites []string) []string {
	if len(sites) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(sites))
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
