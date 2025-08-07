package middlewares

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Down 中间件生成器，用于处理下载请求的验证
// 验证文件路径和签名
//
// 参数:
//   - verifyFunc: 签名验证函数，接受路径和签名参数
//
// 返回:
//   - gin.HandlerFunc: Gin中间件函数
func Down(verifyFunc func(string, string) error) func(c *gin.Context) {
	return func(c *gin.Context) {
		// 解析并清理路径
		rawPath := parsePath(c.Param("path"))
		if rawPath == "" {
			common.ErrorStrResp(c, "无效的路径", 400)
			c.Abort()
			return
		}

		// 将路径保存到上下文中
		common.GinWithValue(c, consts.PathKey, rawPath)

		// 获取最近的元数据
		meta, err := op.GetNearestMeta(rawPath)
		if err != nil {
			if !errs.Is(errs.Cause(err), errs.MetaNotFound) {
				common.ErrorResp(c, err, 500, true)
				c.Abort()
				return
			}
		}

		// 将元数据保存到上下文中
		common.GinWithValue(c, consts.MetaKey, meta)

		// 验证签名
		if needSign(meta, rawPath) {
			sign := c.Query("sign")
			err = verifyFunc(rawPath, strings.TrimSuffix(sign, "/"))
			if err != nil {
				common.ErrorResp(c, err, 401)
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// parsePath 解析并清理URL路径
// 处理URL编码和特殊字符
//
// 参数:
//   - path: 原始路径字符串
//
// 返回:
//   - string: 清理后的路径
func parsePath(path string) string {
	// 先对路径进行URL解码
	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		// 如果解码失败，使用原始路径
		decodedPath = path
	}

	// 修复并清理路径
	return utils.FixAndCleanPath(decodedPath)
}

// needSign 判断是否需要签名验证
//
// 参数:
//   - meta: 元数据对象，可能为nil
//   - path: 文件路径
//
// 返回:
//   - bool: 如果需要签名验证返回true，否则返回false
func needSign(meta *model.Meta, path string) bool {
	// 如果全局设置需要签名
	if setting.GetBool(consts.SignAll) {
		return true
	}

	// 如果存储设置需要签名
	if common.IsStorageSignEnabled(path) {
		return true
	}

	// 如果元数据不存在或没有密码
	if meta == nil || meta.Password == "" {
		return false
	}

	// 如果元数据不应用于子路径且路径不是元数据路径
	if !meta.PSub && path != meta.Path {
		return false
	}

	return true
}