package handler

import (
	"github.com/gin-gonic/gin"

	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// ListBlogPosts 查询文章列表(管理员,含草稿)。
func (h *BlogHandler) ListBlogPosts(c *gin.Context) {
	var page dto.PageReq
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BindError(c, err)
		return
	}

	result, err := h.service.List(c.Request.Context(), appblog.ListFilter{
		Page:     page.Page,
		PageSize: page.PageSize,
		Keyword:  page.Keyword,
		Status:   c.Query("status"),
		Lang:     c.Query("lang"),
	})
	if err != nil {
		httpCode, message := h.handleError("查询博客列表失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}

	list := make([]dto.BlogPostResp, 0, len(result.List))
	for _, item := range result.List {
		list = append(list, toBlogPostResp(item))
	}
	response.Success(c, response.PagedData(list, result.Total, result.Page, result.PageSize))
}

// GetBlogPost 获取文章详情(管理员编辑回填)。
func (h *BlogHandler) GetBlogPost(c *gin.Context) {
	id, err := parseBlogID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的文章 ID")
		return
	}

	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		httpCode, message := h.handleError("查询博客失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toBlogPostResp(item))
}

// CreateBlogPost 创建文章。作者取当前登录管理员。
func (h *BlogHandler) CreateBlogPost(c *gin.Context) {
	var req dto.CreateBlogPostReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	authorID, _ := currentUserID(c)

	item, err := h.service.Create(c.Request.Context(), appblog.CreateInput{
		Title:          req.Title,
		Slug:           req.Slug,
		Summary:        req.Summary,
		CoverImage:     req.CoverImage,
		ContentHTML:    req.ContentHTML,
		Status:         req.Status,
		InviteCode:     req.InviteCode,
		GateEnabled:    req.GateEnabled,
		GatePosition:   req.GatePosition,
		Lang:           req.Lang,
		Tags:           req.Tags,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		OGImage:        req.OGImage,
		AuthorID:       authorID,
	})
	if err != nil {
		httpCode, message := h.handleError("创建博客失败", "创建失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toBlogPostResp(item))
}

// UpdateBlogPost 更新文章。
func (h *BlogHandler) UpdateBlogPost(c *gin.Context) {
	id, err := parseBlogID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的文章 ID")
		return
	}

	var req dto.UpdateBlogPostReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	item, err := h.service.Update(c.Request.Context(), id, appblog.UpdateInput{
		Title:          req.Title,
		Slug:           req.Slug,
		Summary:        req.Summary,
		CoverImage:     req.CoverImage,
		ContentHTML:    req.ContentHTML,
		Status:         req.Status,
		InviteCode:     req.InviteCode,
		GateEnabled:    req.GateEnabled,
		GatePosition:   req.GatePosition,
		Lang:           req.Lang,
		Tags:           req.Tags,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		OGImage:        req.OGImage,
	})
	if err != nil {
		httpCode, message := h.handleError("更新博客失败", "更新失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toBlogPostResp(item))
}

// DeleteBlogPost 删除文章。
func (h *BlogHandler) DeleteBlogPost(c *gin.Context) {
	id, err := parseBlogID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的文章 ID")
		return
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		httpCode, message := h.handleError("删除博客失败", "删除失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, nil)
}
