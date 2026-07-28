package plugin

import (
	"bytes"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ttftWriter 包装 gin.ResponseWriter，记录首个响应体字节写出客户端的时刻。
// 与 forwardState.startedAt 之差即「客户端感知 TTFT」，是 TTFT 分段埋点的终点。
// 仅在首写时多一次分支判断，不改变任何写出行为（Flush/Hijack 等经嵌入接口透传）。
//
// 注意：这里测的是「客户端感知」的首字节，SSE keep-alive ping（如 openai 插件图片生成
// 走的 startSSEPingKeepAlive，可能来自独立 goroutine 写同一个 writer）算作首字节是符合
// 定义的——ping 确实是客户端收到的第一个字节。firstWriteOnce 只保证并发写入下这个时间戳
// 只被设置一次、且对后续读者可见，不改变「ping 也算首字节」这个语义本身。
type ttftWriter struct {
	gin.ResponseWriter
	firstWriteOnce sync.Once
	firstWriteAt   time.Time

	stateMu              sync.RWMutex
	statusCode           int
	heartbeatWritten     bool
	applicationDataWrote bool
}

func (w *ttftWriter) Write(b []byte) (int, error) {
	w.observeBody(b)
	return w.ResponseWriter.Write(b)
}

func (w *ttftWriter) WriteString(s string) (int, error) {
	w.observeBody([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

func (w *ttftWriter) WriteHeader(statusCode int) {
	w.stateMu.Lock()
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
	w.stateMu.Unlock()
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *ttftWriter) observeBody(data []byte) {
	if len(data) == 0 {
		return
	}
	w.firstWriteOnce.Do(func() { w.firstWriteAt = time.Now() })
	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	if isSSECommentOnly(data) {
		w.heartbeatWritten = true
		return
	}
	w.applicationDataWrote = true
}

// isSSECommentOnly recognizes protocol-neutral SSE comments. Blank lines are
// allowed around comments, but a payload containing any data/event field is
// application data and commits the selected upstream account.
func isSSECommentOnly(data []byte) bool {
	sawComment := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if len(line) == 0 || line[0] != ':' {
			return false
		}
		sawComment = true
	}
	return sawComment
}

func (w *ttftWriter) heartbeatOnlyWritten() bool {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	statusOK := w.statusCode == 0 || (w.statusCode >= http.StatusOK && w.statusCode < http.StatusMultipleChoices)
	return statusOK && w.heartbeatWritten && !w.applicationDataWrote
}

func (w *ttftWriter) applicationResponseCommitted() bool {
	w.stateMu.RLock()
	applicationDataWrote := w.applicationDataWrote
	heartbeatWritten := w.heartbeatWritten
	statusCode := w.statusCode
	w.stateMu.RUnlock()
	if applicationDataWrote {
		return true
	}
	statusOK := statusCode == 0 || (statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices)
	if statusOK && heartbeatWritten {
		return false
	}
	return w.Written()
}

func streamHeartbeatOnlyWritten(c *gin.Context) bool {
	if c == nil {
		return false
	}
	w, ok := c.Writer.(*ttftWriter)
	return ok && w.heartbeatOnlyWritten()
}

func streamApplicationResponseCommitted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if w, ok := c.Writer.(*ttftWriter); ok {
		return w.applicationResponseCommitted()
	}
	return c.Writer.Written()
}

// installTTFTWriter 把计时包装器装到 gin ctx 上；返回包装器用于事后读取。
// 重复安装时复用已有包装器（防御 failover 重入，实际 Forward 只装一次）。
func installTTFTWriter(c *gin.Context) *ttftWriter {
	if existing, ok := c.Writer.(*ttftWriter); ok {
		return existing
	}
	w := &ttftWriter{ResponseWriter: c.Writer}
	c.Writer = w
	return w
}
