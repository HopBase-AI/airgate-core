package plugin

import (
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
}

func (w *ttftWriter) Write(b []byte) (int, error) {
	if len(b) > 0 {
		w.firstWriteOnce.Do(func() { w.firstWriteAt = time.Now() })
	}
	return w.ResponseWriter.Write(b)
}

func (w *ttftWriter) WriteString(s string) (int, error) {
	if len(s) > 0 {
		w.firstWriteOnce.Do(func() { w.firstWriteAt = time.Now() })
	}
	return w.ResponseWriter.WriteString(s)
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
