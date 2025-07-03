package middlewares

import (
	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

// SearchIndex 中间件，检查搜索索引是否可用
// 如果搜索索引设置为"none"，则拒绝搜索请求
//
// 参数:
//   - c: Gin上下文
func SearchIndex(c *gin.Context) {
	// 获取搜索索引模式设置
	mode := setting.GetStr(consts.SearchIndex)

	// 检查是否禁用了搜索
	if mode == "none" {
		common.ErrorResp(c, errs.SearchNotAvailable, 500)
		c.Abort()
		return
	}

	// 搜索可用，继续处理
	c.Next()
}