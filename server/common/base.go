package common

import (
	"fmt"
	"net/http"
	stdpath "path"
	"strings"

	"github.com/dongdio/OpenList/internal/conf"
)

// GetApiUrl 根据请求信息生成API的完整URL
// 如果配置中的SiteURL已经是完整URL，则直接使用
// 否则根据请求信息构建完整URL
// 参数:
//   - r: HTTP请求，可以为nil
//
// 返回:
//   - 完整的API URL，不带尾部斜杠
func GetApiUrl(r *http.Request) string {
	api := conf.Conf.SiteURL
	// 如果已经是完整URL（以http开头）
	if strings.HasPrefix(api, "http") {
		return strings.TrimSuffix(api, "/")
	}

	// 根据请求信息构建URL
	if r != nil {
		// 确定协议（HTTP或HTTPS）
		protocol := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			protocol = "https"
		}

		// 获取主机名
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}

		// 构建完整URL
		api = fmt.Sprintf("%s://%s", protocol, stdpath.Join(host, api))
	}

	// 确保没有尾部斜杠
	return strings.TrimSuffix(api, "/")
}
