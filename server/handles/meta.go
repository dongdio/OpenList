package handles

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/server/common"
)

// ListMetas returns a paginated list of metadata configurations
func ListMetas(c *gin.Context) {
	var req model.PageReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Ensure pagination parameters are valid
	req.Validate()
	log.Debugf("Meta list request: %+v", req)

	// Fetch metadata entries with pagination
	metas, total, err := op.GetMetas(req.Page, req.PerPage)
	if err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}

	common.SuccessResp(c, common.PageResp{
		Content: metas,
		Total:   total,
	})
}

// CreateMeta creates a new metadata configuration
func CreateMeta(c *gin.Context) {
	var req model.Meta
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Validate hide patterns
	invalidPattern, err := validateHidePatterns(req.Hide)
	if err != nil {
		common.ErrorStrResp(c, fmt.Sprintf("Invalid hide pattern [%s]: %s", invalidPattern, err.Error()), 400)
		return
	}

	if err = op.CreateMeta(&req); err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}

	common.SuccessResp(c)
}

// UpdateMeta updates an existing metadata configuration
func UpdateMeta(c *gin.Context) {
	var req model.Meta
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Validate hide patterns
	invalidPattern, err := validateHidePatterns(req.Hide)
	if err != nil {
		common.ErrorStrResp(c, fmt.Sprintf("Invalid hide pattern [%s]: %s", invalidPattern, err.Error()), 400)
		return
	}

	if err = op.UpdateMeta(&req); err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}

	common.SuccessResp(c)
}

// validateHidePatterns checks if all hide patterns are valid regular expressions
// Returns the first invalid pattern and an error if validation fails
func validateHidePatterns(hide string) (string, error) {
	if strings.TrimSpace(hide) == "" {
		return "", nil
	}

	patterns := strings.Split(hide, "\n")
	var err error
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		_, err = regexp2.Compile(pattern, regexp2.None)
		if err != nil {
			return pattern, err
		}
	}

	return "", nil
}

// DeleteMeta deletes a metadata configuration by ID
func DeleteMeta(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		common.ErrorStrResp(c, "Missing required parameter: id", 400)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ErrorStrResp(c, "Invalid ID format, must be a number", 400)
		return
	}

	if err = op.DeleteMetaByID(uint(id)); err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}

	common.SuccessResp(c)
}

// GetMeta retrieves a specific metadata configuration by ID
func GetMeta(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		common.ErrorStrResp(c, "Missing required parameter: id", 400)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ErrorStrResp(c, "Invalid ID format, must be a number", 400)
		return
	}

	meta, err := op.GetMetaByID(uint(id))
	if err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}

	common.SuccessResp(c, meta)
}
