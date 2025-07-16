package middlewares

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// StoragesLoaded 中间件，检查存储是否已加载完成
// 如果存储尚未加载，除了某些特定路径外，其他请求将被拒绝
//
// 允许通过的路径包括:
// - 根路径和favicon.ico
// - 静态资源路径（assets、images、streamer、static）
func StoragesLoaded(c *gin.Context) {
	if !conf.StoragesLoaded {
		if utils.SliceContains([]string{"", "/", "/favicon.ico"}, c.Request.URL.Path) {
			c.Next()
			return
		}
		paths := []string{"/assets", "/images", "/streamer", "/static"}
		for _, path := range paths {
			if strings.HasPrefix(c.Request.URL.Path, path) {
				c.Next()
				return
			}
		}
		common.ErrorStrResp(c, "Loading storage, please wait", 500)
		c.Abort()
		return
	}
	common.GinWithValue(c,
		consts.ApiUrlKey, common.GetApiUrlFromRequest(c.Request),
	)
	c.Next()
}