package webdav

import (
	"net/http"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

// getModTime 从HTTP请求头中获取文件修改时间
// 使用X-OC-Mtime头部字段，该字段在OwnCloud/NextCloud中使用
//
// 参数:
//   - r: HTTP请求
//
// 返回:
//   - time.Time: 文件修改时间，如果头部不存在则返回当前时间
func (h *Handler) getModTime(r *http.Request) time.Time {
	return h.getHeaderTime(r, "X-OC-Mtime", "")
}

// getCreateTime 从HTTP请求头中获取文件创建时间
// 尝试使用X-OC-Ctime头部字段，如果不存在则回退到X-OC-Mtime
// 注：OwnCloud/NextCloud尚未实现此功能，但我们添加支持以便与rclone兼容
//
// 参数:
//   - r: HTTP请求
//
// 返回:
//   - time.Time: 文件创建时间，如果头部不存在则返回当前时间
func (h *Handler) getCreateTime(r *http.Request) time.Time {
	return h.getHeaderTime(r, "X-OC-Ctime", "X-OC-Mtime")
}

// getHeaderTime 从HTTP请求头中获取时间信息
// 支持主要头部字段和备选头部字段
//
// 参数:
//   - r: HTTP请求
//   - header: 主要头部字段名称
//   - alternative: 备选头部字段名称，如果为空则不使用备选字段
//
// 返回:
//   - time.Time: 解析后的时间，如果解析失败则返回当前时间
func (h *Handler) getHeaderTime(r *http.Request, header, alternative string) time.Time {
	if r == nil {
		log.Warn("getHeaderTime被调用时传入了nil请求")
		return time.Now()
	}

	// 获取主要头部字段值
	headerValue := r.Header.Get(header)

	// 如果主要字段不存在且提供了备选字段，则尝试备选字段
	if headerValue == "" && alternative != "" {
		headerValue = r.Header.Get(alternative)
	}

	// 解析时间戳
	if headerValue != "" {
		unixTimestamp, err := strconv.ParseInt(headerValue, 10, 64)
		if err == nil {
			return time.Unix(unixTimestamp, 0)
		}
		log.Warnf("WebDAV getHeaderTime 解析时间失败，头部：%s，错误：%s", header, err)
	}

	// 默认返回当前时间
	return time.Now()
}
