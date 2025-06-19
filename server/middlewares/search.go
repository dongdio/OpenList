package middlewares

import (
	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/errs"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/server/common"
)

func SearchIndex(c *gin.Context) {
	mode := setting.GetStr(conf.SearchIndex)
	if mode == "none" {
		common.ErrorResp(c, errs.SearchNotAvailable, 500)
		c.Abort()
	} else {
		c.Next()
	}
}