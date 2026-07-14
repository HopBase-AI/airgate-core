package server

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/internal/plugin"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// blogImageAllowedExt 博客图片允许的扩展名(不含 svg:避免内嵌脚本的 XSS 面)。
var blogImageAllowedExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// handleBlogImageUpload 博客正文/封面图片上传(管理员路由)。
//
// 放在 server 包而非 handler 包:需要 *ent.Client 走 plugin.NewAssetStorage,
// 而 handler 层禁止 import ent。走 AssetStorage(S3/R2 或本地 data/assets 兜底),
// purpose=upload,返回可公开访问 URL 供 TipTap 嵌入。
func (s *Server) handleBlogImageUpload(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择要上传的图片")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > 10<<20 {
		response.BadRequest(c, "图片大小不能超过 10MB")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !blogImageAllowedExt[ext] {
		response.BadRequest(c, "只支持 PNG/JPG/GIF/WebP 格式")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		response.InternalError(c, "读取文件失败")
		return
	}

	// 内容类型一律由已校验的扩展名推导,不信任客户端上传的 Content-Type——
	// 否则 S3/R2 后端会把客户端声明的 text/html 原样存回并在资源 URL 上以 HTML 提供(存储型 XSS)。
	contentType := contentTypeFromExt(ext)

	var userID int64
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(int); ok {
			userID = int64(id)
		}
	}

	storage, err := plugin.NewAssetStorage(c.Request.Context(), s.db)
	if err != nil {
		response.InternalError(c, "存储初始化失败")
		return
	}
	stored, err := storage.Store(
		c.Request.Context(),
		userID,
		plugin.AssetPurposeUpload,
		contentType,
		strings.TrimPrefix(ext, "."),
		data,
	)
	if err != nil {
		response.InternalError(c, "上传失败")
		return
	}

	response.Success(c, map[string]string{"url": stored.PublicURL})
}
