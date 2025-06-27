package middlewares

import (
	"bytes"
	"fmt"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/utility/utils"
)

// ErrorLogging 中间件，记录HTTP响应中的错误信息
// 捕获响应体中的错误信息或状态码错误，并记录到日志中
//
// 返回:
//   - gin.HandlerFunc: Gin中间件函数
func ErrorLogging() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 使用自定义的响应写入器包装原始ResponseWriter
		w := &responseBodyWriter{
			body:           &bytes.Buffer{},
			ResponseWriter: c.Writer,
		}
		c.Writer = w

		// 执行后续处理
		c.Next()

		// 提取错误信息
		var errorMsg string

		// 尝试从响应体中解析错误信息
		if w.body.Len() > 0 {
			var jsonBody struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}

			// 尝试将响应体解析为JSON
			if err := utils.Json.Unmarshal(w.body.Bytes(), &jsonBody); err == nil {
				// 如果code不是200，说明有错误
				if jsonBody.Code != 200 {
					errorMsg = fmt.Sprintf(" 错误: code=%d, message=%s", jsonBody.Code, jsonBody.Message)
				}
			}
		}

		// 如果状态码大于等于400，提取错误信息
		if c.Writer.Status() >= 400 {
			if len(c.Errors) > 0 {
				// 使用Gin的错误信息
				errorMsg = c.Errors.String()
			} else if errorMsg == "" && w.body.Len() > 0 {
				// 使用响应体作为错误信息
				body := w.body.String()
				// 限制日志长度，避免过长
				if len(body) > 500 {
					errorMsg = body[:500] + "..."
				} else {
					errorMsg = body
				}
			}
		}

		// 记录错误信息
		if errorMsg != "" {
			log.Error(errorMsg)
		}
	}
}

// responseBodyWriter 自定义的响应写入器
// 同时将响应内容写入到原始ResponseWriter和缓冲区中
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write 实现io.Writer接口
// 同时将内容写入到原始ResponseWriter和缓冲区
//
// 参数:
//   - b: 要写入的字节切片
//
// 返回:
//   - int: 写入的字节数
//   - error: 错误信息
func (r *responseBodyWriter) Write(b []byte) (int, error) {
	// 写入到缓冲区
	r.body.Write(b)
	// 写入到原始ResponseWriter
	return r.ResponseWriter.Write(b)
}