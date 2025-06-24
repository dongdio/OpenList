package webdav

import (
	"net/http"
)

// BufferedResponseWriter 是一个HTTP响应写入器的缓冲实现
// 它先将所有数据保存在内存中，然后一次性写入到实际的ResponseWriter
// 这对于需要在发送响应前知道完整内容的情况很有用
type BufferedResponseWriter struct {
	statusCode int         // HTTP状态码
	data       []byte      // 缓冲的响应数据
	header     http.Header // HTTP头部
}

// Header 返回HTTP头部映射
// 实现http.ResponseWriter接口
//
// 返回:
//   - http.Header: HTTP头部映射
func (w *BufferedResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

// Write 将数据写入缓冲区
// 实现http.ResponseWriter接口
//
// 参数:
//   - bytes: 要写入的数据
//
// 返回:
//   - int: 写入的字节数
//   - error: 写入错误，通常为nil
func (w *BufferedResponseWriter) Write(bytes []byte) (int, error) {
	w.data = append(w.data, bytes...)
	return len(bytes), nil
}

// WriteHeader 设置HTTP状态码
// 实现http.ResponseWriter接口
// 注意：只有第一次调用会生效
//
// 参数:
//   - statusCode: HTTP状态码
func (w *BufferedResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
}

// StatusCode 返回当前设置的HTTP状态码
//
// 返回:
//   - int: HTTP状态码，如果未设置则为0
func (w *BufferedResponseWriter) StatusCode() int {
	return w.statusCode
}

// Size 返回当前缓冲的数据大小
//
// 返回:
//   - int: 缓冲的字节数
func (w *BufferedResponseWriter) Size() int {
	return len(w.data)
}

// WriteToResponse 将缓冲的内容写入到实际的HTTP响应写入器
// 包括状态码、头部和正文数据
//
// 参数:
//   - rw: 目标HTTP响应写入器
//
// 返回:
//   - int: 写入的字节数
//   - error: 写入错误
func (w *BufferedResponseWriter) WriteToResponse(rw http.ResponseWriter) (int, error) {
	if rw == nil {
		return 0, nil
	}

	// 复制所有头部字段
	h := rw.Header()
	for k, vs := range w.header {
		for _, v := range vs {
			h.Add(k, v)
		}
	}

	// 写入状态码，如果未设置则默认为200 OK
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	rw.WriteHeader(w.statusCode)

	// 写入正文数据
	if len(w.data) > 0 {
		return rw.Write(w.data)
	}
	return 0, nil
}

// NewBufferedResponseWriter 创建一个新的BufferedResponseWriter实例
//
// 返回:
//   - *BufferedResponseWriter: 新的缓冲响应写入器
func NewBufferedResponseWriter() *BufferedResponseWriter {
	return &BufferedResponseWriter{
		statusCode: 0,
		data:       make([]byte, 0, 512), // 预分配适当的初始容量
		header:     make(http.Header),
	}
}
