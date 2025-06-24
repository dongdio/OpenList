package middlewares

import (
	"io"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/pkg/stream"
)

// MaxAllowed 中间件生成器，限制并发请求数量
// 使用信号量模式实现请求限流
//
// 参数:
//   - n: 最大并发请求数
//
// 返回:
//   - gin.HandlerFunc: Gin中间件函数
func MaxAllowed(n int) gin.HandlerFunc {
	// 创建大小为n的信号量
	sem := make(chan struct{}, n)

	// 获取信号量
	acquire := func() { sem <- struct{}{} }

	// 释放信号量
	release := func() { <-sem }

	return func(c *gin.Context) {
		// 获取信号量，如果已满则阻塞等待
		acquire()

		// 确保在请求处理完成后释放信号量
		defer release()

		// 继续处理请求
		c.Next()
	}
}

// UploadRateLimiter 中间件生成器，限制上传速率
// 包装请求体，应用速率限制
//
// 参数:
//   - limiter: 速率限制器
//
// 返回:
//   - gin.HandlerFunc: Gin中间件函数
func UploadRateLimiter(limiter stream.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果没有限制器，直接继续
		if limiter == nil {
			c.Next()
			return
		}

		// 包装请求体，应用速率限制
		c.Request.Body = &stream.RateLimitReader{
			Reader:  c.Request.Body,
			Limiter: limiter,
			Ctx:     c,
		}

		c.Next()
	}
}

// ResponseWriterWrapper 响应写入器包装器
// 将响应写入重定向到自定义的写入器
type ResponseWriterWrapper struct {
	gin.ResponseWriter           // 原始的响应写入器
	WrapWriter         io.Writer // 包装的写入器
}

// Write 方法重写，将数据写入到包装的写入器
//
// 参数:
//   - p: 要写入的字节切片
//
// 返回:
//   - n: 写入的字节数
//   - err: 写入错误
func (w *ResponseWriterWrapper) Write(p []byte) (n int, err error) {
	return w.WrapWriter.Write(p)
}

// DownloadRateLimiter 中间件生成器，限制下载速率
// 包装响应写入器，应用速率限制
//
// 参数:
//   - limiter: 速率限制器
//
// 返回:
//   - gin.HandlerFunc: Gin中间件函数
func DownloadRateLimiter(limiter stream.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果没有限制器，直接继续
		if limiter == nil {
			c.Next()
			return
		}

		// 包装响应写入器，应用速率限制
		c.Writer = &ResponseWriterWrapper{
			ResponseWriter: c.Writer,
			WrapWriter: &stream.RateLimitWriter{
				Writer:  c.Writer,
				Limiter: limiter,
				Ctx:     c,
			},
		}

		c.Next()
	}
}
