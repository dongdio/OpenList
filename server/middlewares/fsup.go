package middlewares

import (
	"net/url"
	stdpath "path"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

// FsUp 中间件，处理文件上传请求的权限验证
// 检查用户是否有权限上传文件到指定路径
func FsUp(c *gin.Context) {
	// 获取文件路径和密码
	path := c.GetHeader("File-Path")
	password := c.GetHeader("Password")

	// 检查路径是否为空
	if path == "" {
		common.ErrorStrResp(c, "文件路径不能为空", 400)
		c.Abort()
		return
	}

	// 解码URL路径
	var err error
	path, err = url.PathUnescape(path)
	if err != nil {
		common.ErrorResp(c, errors.Wrap(err, "路径解码失败"), 400)
		c.Abort()
		return
	}

	// 获取用户信息
	userObj, exists := c.Value(consts.UserKey).(*model.User)
	if !exists {
		common.ErrorStrResp(c, "用户未认证", 401)
		c.Abort()
		return
	}

	// 构建完整路径
	path, err = userObj.JoinPath(path)
	if err != nil {
		common.ErrorResp(c, errors.Wrap(err, "路径构建失败"), 403)
		c.Abort()
		return
	}

	// 获取最近的元数据
	parentDir := stdpath.Dir(path)
	meta, err := op.GetNearestMeta(parentDir)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			common.ErrorResp(c, err, 500, true)
			c.Abort()
			return
		}
		// 如果没有找到元数据，meta将为nil
	}

	// 检查访问权限和写入权限
	if !(common.CanAccess(userObj, meta, path, password) && (userObj.CanWrite() || common.CanWrite(meta, parentDir))) {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		c.Abort()
		return
	}

	c.Next()
}