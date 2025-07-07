package _189

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 定义189云盘驱动的额外配置选项
type Addition struct {
	// Username 189云盘账号用户名
	Username string `json:"username" required:"true"`
	// Password 189云盘账号密码
	Password string `json:"password" required:"true"`
	// Cookie 当需要验证码时填写Cookie
	Cookie string `json:"cookie" help:"当需要验证码时填写Cookie"`
	driver.RootID
}

// 驱动配置
var config = driver.Config{
	Name:        "189Cloud",                      // 驱动名称
	LocalSort:   true,                            // 本地排序
	DefaultRoot: "-11",                           // 默认根目录ID
	Alert:       `info|如果此驱动不可用，您可以尝试使用189PC驱动。`, // 提示信息
}

// init 初始化函数，注册189云盘驱动
func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(Cloud189)
	})
}
