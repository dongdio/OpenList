package middlewares

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/internal/conf"
)

// ForceHTTPS 中间件，强制将HTTP请求重定向到HTTPS
// 如果请求不是通过HTTPS发起的，则将其重定向到HTTPS版本
//
// 参数:
//   - c: Gin上下文
func ForceHTTPS(c *gin.Context) {
	// 检查请求是否通过TLS（HTTPS）发起
	if c.Request.TLS == nil {
		// 获取当前主机名
		host := c.Request.Host

		// 将HTTP端口替换为HTTPS端口
		host = strings.Replace(
			host,
			fmt.Sprintf(":%d", conf.Conf.Scheme.HttpPort),
			fmt.Sprintf(":%d", conf.Conf.Scheme.HttpsPort),
			1,
		)

		// 重定向到HTTPS版本
		c.Redirect(302, "https://"+host+c.Request.RequestURI)
		c.Abort()
		return
	}

	// 如果已经是HTTPS请求，继续处理
	c.Next()
}

// ForceHTTPS 中间件，强制将HTTP请求重定向到HTTPS
// 如果请求不是通过HTTPS发起的，则将其重定向到HTTPS版本
//
// 参数:
//   - c: Gin上下文
// func ForceHTTPS1(r *ghttp.Request) {
// 	// 检查请求是否通过TLS（HTTPS）发起
// 	if r.Request.TLS == nil {
// 		// 获取当前主机名
// 		host := r.Request.Host
//
// 		// 将HTTP端口替换为HTTPS端口
// 		host = strings.Replace(
// 			host,
// 			fmt.Sprintf(":%d", conf.Conf.Scheme.HttpPort),
// 			fmt.Sprintf(":%d", conf.Conf.Scheme.HttpsPort),
// 			1,
// 		)
//
// 		// 重定向到HTTPS版本
// 		r.Response.RedirectTo("https://"+host+r.Request.RequestURI, 302)
// 		return
// 	}
//
// 	// 如果已经是HTTPS请求，继续处理
// 	r.Middleware.Next()
// }