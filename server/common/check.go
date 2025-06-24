package common

import (
	"path"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/driver"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/pkg/utils"
)

// IsStorageSignEnabled 检查指定路径的存储是否启用了签名
//
// 参数:
//   - rawPath: 原始路径
//
// 返回:
//   - bool: 如果存储启用了签名返回true，否则返回false
func IsStorageSignEnabled(rawPath string) bool {
	storage := op.GetBalancedStorage(rawPath)
	return storage != nil && storage.GetStorage().EnableSign
}

// CanWrite 检查指定元数据和路径是否具有写入权限
//
// 参数:
//   - meta: 元数据对象
//   - path: 请求路径
//
// 返回:
//   - bool: 如果有写入权限返回true，否则返回false
func CanWrite(meta *model.Meta, path string) bool {
	if meta == nil || !meta.Write {
		return false
	}
	return meta.WSub || meta.Path == path
}

// IsApply 检查元数据是否应用于请求路径
//
// 参数:
//   - metaPath: 元数据路径
//   - reqPath: 请求路径
//   - applySub: 是否应用于子路径
//
// 返回:
//   - bool: 如果元数据应用于请求路径返回true，否则返回false
func IsApply(metaPath, reqPath string, applySub bool) bool {
	// 如果路径相等，则元数据直接应用
	if utils.PathEqual(metaPath, reqPath) {
		return true
	}
	// 如果请求路径是元数据路径的子路径，且元数据应用于子路径，则元数据应用
	return utils.IsSubPath(metaPath, reqPath) && applySub
}

// CanAccess 检查用户是否可以访问指定路径
//
// 参数:
//   - user: 用户对象
//   - meta: 元数据对象
//   - reqPath: 请求路径
//   - password: 访问密码
//
// 返回:
//   - bool: 如果用户可以访问返回true，否则返回false
func CanAccess(user *model.User, meta *model.Meta, reqPath string, password string) bool {
	// 检查参数
	if user == nil {
		return false
	}

	// 检查隐藏规则
	// 如果元数据存在且用户不能查看隐藏内容，且元数据有隐藏规则，且规则应用于请求路径的父目录
	if meta != nil && !user.CanSeeHides() && meta.Hide != "" &&
		IsApply(meta.Path, path.Dir(reqPath), meta.HSub) {
		// 检查文件名是否匹配隐藏规则
		for _, hide := range strings.Split(meta.Hide, "\n") {
			if hide == "" {
				continue
			}
			// 使用正则表达式匹配
			re, err := regexp2.Compile(hide, regexp2.None)
			if err != nil {
				continue
			}
			if isMatch, _ := re.MatchString(path.Base(reqPath)); isMatch {
				return false
			}
		}
	}

	// 如果用户可以在没有密码的情况下访问
	if user.CanAccessWithoutPassword() {
		return true
	}

	// 如果元数据不存在或没有设置密码
	if meta == nil || meta.Password == "" {
		return true
	}

	// 如果元数据不应用于子路径，且请求路径不是元数据路径
	if !utils.PathEqual(meta.Path, reqPath) && !meta.PSub {
		return true
	}

	// 验证密码
	return meta.Password == password
}

// ShouldProxy 判断是否应该代理指定的文件
// 代理条件：
// 1. 存储配置必须代理
// 2. 存储启用了Web代理
// 3. 文件扩展名在代理类型列表中
//
// 参数:
//   - storage: 存储驱动
//   - filename: 文件名
//
// 返回:
//   - bool: 如果应该代理返回true，否则返回false
func ShouldProxy(storage driver.Driver, filename string) bool {
	// 参数检查
	if storage == nil || filename == "" {
		return false
	}

	// 检查存储配置是否必须代理或启用了Web代理
	if storage.Config().MustProxy() || storage.GetStorage().WebProxy {
		return true
	}

	// 检查文件扩展名是否在代理类型列表中
	if utils.SliceContains(conf.SlicesMap[conf.ProxyTypes], utils.Ext(filename)) {
		return true
	}

	return false
}
