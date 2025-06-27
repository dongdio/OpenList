package common

import (
	stdpath "path"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/internal/sign"
)

// Sign 为对象生成签名
// 目录对象不生成签名
// 非加密对象，在未开启全局签名的情况下不生成签名
//
// 参数:
//   - obj: 需要签名的对象
//   - parent: 父路径
//   - encrypt: 是否是加密对象
//
// 返回:
//   - 签名字符串，如果不需要签名则返回空字符串
func Sign(obj model.Obj, parent string, encrypt bool) string {
	// 目录不需要签名
	// 非加密对象且未开启全局签名时不需要签名
	if obj.IsDir() || (!encrypt && !setting.GetBool(consts.SignAll)) {
		return ""
	}

	// 生成签名（包含路径信息）
	return sign.Sign(stdpath.Join(parent, obj.GetName()))
}