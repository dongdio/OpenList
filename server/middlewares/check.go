package middlewares

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/server/common"
	"github.com/dongdio/OpenList/utility/utils"
)

// StoragesLoaded 中间件，检查存储是否已加载完成
// 如果存储尚未加载，除了某些特定路径外，其他请求将被拒绝
//
// 允许通过的路径包括:
// - 根路径和favicon.ico
// - 静态资源路径（assets、images、streamer、static）
func StoragesLoaded(c *gin.Context) {
	// 如果存储已加载，直接放行
	if conf.StoragesLoaded {
		c.Next()
		return
	}

	// 检查是否为根路径或favicon.ico
	requestPath := c.Request.URL.Path
	if utils.SliceContains([]string{"", "/", "/favicon.ico"}, requestPath) {
		c.Next()
		return
	}

	// 检查是否为静态资源路径
	staticPaths := []string{"/assets", "/images", "/streamer", "/static"}
	for _, path := range staticPaths {
		if strings.HasPrefix(requestPath, path) {
			c.Next()
			return
		}
	}

	// 其他路径返回存储加载中的提示
	common.ErrorStrResp(c, "存储加载中，请稍候", 500)
	c.Abort()
}