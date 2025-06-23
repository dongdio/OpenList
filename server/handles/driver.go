package handles

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/server/common"
)

// ListDriverInfo returns information about all available storage drivers
// This includes their configuration options and capabilities
func ListDriverInfo(c *gin.Context) {
	common.SuccessResp(c, op.GetDriverInfoMap())
}

// ListDriverNames returns a list of all available storage driver names
// This is useful for UI components that need to display driver selection options
func ListDriverNames(c *gin.Context) {
	common.SuccessResp(c, op.GetDriverNames())
}

// GetDriverInfo returns detailed information about a specific storage driver
// The driver name must be provided as a query parameter
func GetDriverInfo(c *gin.Context) {
	driverName := c.Query("driver")
	if driverName == "" {
		common.ErrorStrResp(c, "driver name is required", 400)
		return
	}

	infoMap := op.GetDriverInfoMap()
	driverInfo, exists := infoMap[driverName]
	if !exists {
		common.ErrorStrResp(c, fmt.Sprintf("driver [%s] not found", driverName), 404)
		return
	}

	common.SuccessResp(c, driverInfo)
}
