package handles

import (
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/search"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// SearchRequest extends the base search request with password for accessing protected folders
type SearchRequest struct {
	model.SearchReq
	Password string `json:"password"`
}

// SearchResponse extends SearchNode with additional type information for UI rendering
type SearchResponse struct {
	model.SearchNode
	Type int `json:"type"` // File type for UI rendering
}

// Search handles file/folder search requests with permission filtering
func Search(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Get user and validate permissions
	user := c.Value(consts.UserKey).(*model.User)

	// Convert relative path to absolute path
	var err error
	req.Parent, err = user.JoinPath(req.Parent)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Validate search parameters
	if err = req.Validate(); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Perform the search
	nodes, total, err := search.Search(c, req.SearchReq)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// Filter results based on user permissions
	filteredNodes := filterSearchResults(nodes, user, req.Password)

	// Return paginated results
	common.SuccessResp(c, common.PageResp{
		Content: utils.MustSliceConvert(filteredNodes, convertToSearchResponse),
		Total:   total,
	})
}

// filterSearchResults removes search results that the user doesn't have permission to access
func filterSearchResults(nodes []model.SearchNode, user *model.User, password string) []model.SearchNode {
	var filteredNodes []model.SearchNode

	for _, node := range nodes {
		// Skip nodes outside user's base path
		if !strings.HasPrefix(node.Parent, user.BasePath) {
			continue
		}

		// Get metadata for permission check
		meta, err := op.GetNearestMeta(node.Parent)
		if err != nil && !errs.Is(errs.Cause(err), errs.MetaNotFound) {
			continue
		}

		// Check if user can access this node
		nodePath := path.Join(node.Parent, node.Name)
		if !common.CanAccess(user, meta, nodePath, password) {
			continue
		}

		filteredNodes = append(filteredNodes, node)
	}

	return filteredNodes
}

// convertToSearchResponse converts a SearchNode to a SearchResponse by adding type information
func convertToSearchResponse(node model.SearchNode) SearchResponse {
	return SearchResponse{
		SearchNode: node,
		Type:       utils.GetObjType(node.Name, node.IsDir),
	}
}