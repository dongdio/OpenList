package _123

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 定义123网盘驱动的额外配置参数
type Addition struct {
	// 用户名（必填）
	Username string `json:"username" required:"true"`
	// 密码（必填）
	Password string `json:"password" required:"true"`
	// 根目录ID
	driver.RootID
	// 排序字段（已注释掉，可能为未来功能预留）
	// OrderBy        string `json:"order_by" type:"select" options:"file_id,file_name,size,update_at" default:"file_name"`
	// 排序方向（已注释掉，可能为未来功能预留）
	// OrderDirection string `json:"order_direction" type:"select" options:"asc,desc" default:"asc"`
	// 访问令牌，由登录过程获取
	AccessToken  string
	UploadThread int `json:"UploadThread" type:"number" default:"3" help:"the threads of upload"`
}

// 驱动配置信息
var config = driver.Config{
	// 驱动名称
	Name: "123Pan",
	// 默认根目录ID
	DefaultRoot: "0",
	// 是否使用本地排序
	LocalSort: true,
}

// init 初始化函数，在包被导入时自动注册驱动
func init() {
	op.RegisterDriver(func() driver.Driver {
		// 新增默认选项 要在RegisterDriver初始化设置 才会对正在使用的用户生效
		return &Pan123{
			Addition: Addition{
				UploadThread: 3,
			},
		}
	})
}