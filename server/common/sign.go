package common

import (
	stdpath "path"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/internal/sign"
)

func Sign(obj model.Obj, parent string, encrypt bool) string {
	if obj.IsDir() || (!encrypt && !setting.GetBool(conf.SignAll)) {
		return ""
	}
	return sign.Sign(stdpath.Join(parent, obj.GetName()))
}