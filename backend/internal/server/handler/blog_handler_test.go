package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
)

// blogFakeRepo 内存实现 appblog.Repository,供 handler 层 httptest。
type blogFakeRepo struct {
	posts  map[int]appblog.Post
	nextID int
}

func newBlogFakeRepo() *blogFakeRepo { return &blogFakeRepo{posts: map[int]appblog.Post{}, nextID: 1} }

func (r *blogFakeRepo) List(_ context.Context, f appblog.ListFilter) ([]appblog.Post, int64, error) {
	var out []appblog.Post
	for _, p := range r.posts {
		if f.Status != "" && p.Status != f.Status {
			continue
		}
		out = append(out, p)
	}
	return out, int64(len(out)), nil
}
func (r *blogFakeRepo) FindByID(_ context.Context, id int) (appblog.Post, error) {
	p, ok := r.posts[id]
	if !ok {
		return appblog.Post{}, appblog.ErrPostNotFound
	}
	return p, nil
}
func (r *blogFakeRepo) FindBySlug(_ context.Context, slug string) (appblog.Post, error) {
	for _, p := range r.posts {
		if p.Slug == slug {
			return p, nil
		}
	}
	return appblog.Post{}, appblog.ErrPostNotFound
}
func (r *blogFakeRepo) Create(_ context.Context, in appblog.CreateInput) (appblog.Post, error) {
	id := r.nextID
	r.nextID++
	p := appblog.Post{ID: id, Title: in.Title, Slug: in.Slug, Status: in.Status, ContentHTML: in.ContentHTML, InviteCode: in.InviteCode, AuthorID: in.AuthorID, PublishedAt: in.PublishedAt}
	r.posts[id] = p
	return p, nil
}
func (r *blogFakeRepo) Update(_ context.Context, id int, in appblog.UpdateInput) (appblog.Post, error) {
	p, ok := r.posts[id]
	if !ok {
		return appblog.Post{}, appblog.ErrPostNotFound
	}
	if in.Title != nil {
		p.Title = *in.Title
	}
	r.posts[id] = p
	return p, nil
}
func (r *blogFakeRepo) Delete(_ context.Context, id int) error {
	if _, ok := r.posts[id]; !ok {
		return appblog.ErrPostNotFound
	}
	delete(r.posts, id)
	return nil
}
func (r *blogFakeRepo) SlugExists(_ context.Context, slug string, excludeID int) (bool, error) {
	for id, p := range r.posts {
		if p.Slug == slug && id != excludeID {
			return true, nil
		}
	}
	return false, nil
}
func (r *blogFakeRepo) IncrementViewCount(context.Context, int) error { return nil }

func newBlogHandlerRouter(repo *blogFakeRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewBlogHandler(appblog.NewService(repo))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", 1); c.Next() })
	r.GET("/blog/posts", h.ListBlogPosts)
	r.GET("/blog/articles", h.ListPublishedArticles)
	r.POST("/blog/posts", h.CreateBlogPost)
	r.GET("/blog/posts/:id", h.GetBlogPost)
	r.PUT("/blog/posts/:id", h.UpdateBlogPost)
	r.DELETE("/blog/posts/:id", h.DeleteBlogPost)
	return r
}

func TestBlogHandler_ListPublishedArticlesIncludesLanguage(t *testing.T) {
	repo := newBlogFakeRepo()
	repo.posts[1] = appblog.Post{
		ID: 1, Title: "English Post", Slug: "english-post-en",
		Status: appblog.StatusPublished, Lang: appblog.LangEnglish,
	}
	r := newBlogHandlerRouter(repo)
	w := doJSON(t, r, http.MethodGet, "/blog/articles", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data []struct {
			Slug string `json:"slug"`
			Lang string `json:"lang"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || len(resp.Data) != 1 || resp.Data[0].Slug != "english-post-en" || resp.Data[0].Lang != appblog.LangEnglish {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

func doJSON(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBlogHandler_CreateHappyPath(t *testing.T) {
	r := newBlogHandlerRouter(newBlogFakeRepo())
	w := doJSON(t, r, http.MethodPost, "/blog/posts", `{"title":"Hello","status":"published"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID     int    `json:"id"`
			Slug   string `json:"slug"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || resp.Data.ID == 0 || resp.Data.Slug != "hello" || resp.Data.Status != "published" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

func TestBlogHandler_CreateMissingTitleIsBindError(t *testing.T) {
	r := newBlogHandlerRouter(newBlogFakeRepo())
	w := doJSON(t, r, http.MethodPost, "/blog/posts", `{"summary":"no title"}`)
	if w.Code == http.StatusOK {
		t.Fatalf("missing required title should not be 200, got body %s", w.Body.String())
	}
}

func TestBlogHandler_CreateInvalidStatusRejected(t *testing.T) {
	r := newBlogHandlerRouter(newBlogFakeRepo())
	w := doJSON(t, r, http.MethodPost, "/blog/posts", `{"title":"x","status":"garbage"}`)
	if w.Code == http.StatusOK {
		t.Fatalf("invalid status enum should be rejected by binding, got 200")
	}
}

func TestBlogHandler_CreateInvalidInviteIs422(t *testing.T) {
	r := newBlogHandlerRouter(newBlogFakeRepo())
	w := doJSON(t, r, http.MethodPost, "/blog/posts", `{"title":"x","invite_code":"!!"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid invite code should map to 422, got %d", w.Code)
	}
}

func TestBlogHandler_GetNotFoundIs404(t *testing.T) {
	r := newBlogHandlerRouter(newBlogFakeRepo())
	w := doJSON(t, r, http.MethodGet, "/blog/posts/999", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing post should be 404, got %d", w.Code)
	}
}

func TestBlogHandler_UpdateAndDelete(t *testing.T) {
	repo := newBlogFakeRepo()
	r := newBlogHandlerRouter(repo)
	// 先建一篇
	doJSON(t, r, http.MethodPost, "/blog/posts", `{"title":"Orig"}`)
	// 更新标题
	w := doJSON(t, r, http.MethodPut, "/blog/posts/1", `{"title":"Renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d", w.Code)
	}
	if repo.posts[1].Title != "Renamed" {
		t.Fatalf("title not updated: %q", repo.posts[1].Title)
	}
	// 删除
	w = doJSON(t, r, http.MethodDelete, "/blog/posts/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d", w.Code)
	}
	// 再删 → 404
	w = doJSON(t, r, http.MethodDelete, "/blog/posts/1", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("second delete should be 404, got %d", w.Code)
	}
}

func TestBlogHandler_BadIDParam(t *testing.T) {
	r := newBlogHandlerRouter(newBlogFakeRepo())
	w := doJSON(t, r, http.MethodGet, "/blog/posts/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric id should be 400, got %d", w.Code)
	}
}

func TestBlogHandler_AuthorPropagated(t *testing.T) {
	repo := newBlogFakeRepo()
	r := newBlogHandlerRouter(repo)
	doJSON(t, r, http.MethodPost, "/blog/posts", `{"title":"X"}`)
	if repo.posts[1].AuthorID != 1 {
		t.Fatalf("author_id = %d, want 1 (from injected user_id)", repo.posts[1].AuthorID)
	}
}

func TestBlogHandler_HandleErrorMapping(t *testing.T) {
	h := NewBlogHandler(appblog.NewService(newBlogFakeRepo()))
	cases := []struct {
		err  error
		want int
	}{
		{appblog.ErrPostNotFound, 404},
		{appblog.ErrSlugConflict, 422},
		{appblog.ErrInvalidInviteCode, 422},
		{appblog.ErrTitleRequired, 422},
		{context.DeadlineExceeded, 500},
	}
	for _, tc := range cases {
		code, _ := h.handleError("log", "public", tc.err)
		if code != tc.want {
			t.Errorf("handleError(%v) = %d, want %d", tc.err, code, tc.want)
		}
	}
}
