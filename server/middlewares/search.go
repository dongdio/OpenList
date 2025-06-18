package middlewares

import (
	"github.com/gin-gonic/gin"

	"github.com/OpenListTeam/OpenList/internal/conf"
	"github.com/OpenListTeam/OpenList/internal/errs"
	"github.com/OpenListTeam/OpenList/internal/setting"
	"github.com/OpenListTeam/OpenList/server/common"
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