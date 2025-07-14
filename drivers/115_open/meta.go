package _115_open

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 定义115开放平台驱动的额外配置选项
type Addition struct {
	// 根目录ID
	driver.RootID

	// OrderBy 文件排序字段
	// - file_name: 按文件名排序
	// - file_size: 按文件大小排序
	// - user_utime: 按更新时间排序
	// - file_type: 按文件类型排序
	OrderBy string `json:"order_by" type:"select" options:"file_name,file_size,user_utime,file_type" help:"选择文件排序的字段"`

	// OrderDirection 排序方向
	// - asc: 升序
	// - desc: 降序
	OrderDirection string `json:"order_direction" type:"select" options:"asc,desc" help:"选择排序方向（升序或降序）"`

	// LimitRate API请求速率限制
	LimitRate float64 `json:"limit_rate" type:"float" default:"1" help:"限制所有API请求速率（每秒[限制]个请求）"`

	// RefreshToken 115开放平台刷新令牌
	RefreshToken string `json:"refresh_token" required:"true" help:"115开放平台的刷新令牌，用于获取新的访问令牌"`

	// AccessToken 115开放平台访问令牌
	AccessToken string `json:"access_token" required:"true" help:"115开放平台的访问令牌，用于API认证"`
}

// 驱动配置
var config = driver.Config{
	Name:        "115 Open",
	DefaultRoot: "0",
}

// init 初始化函数，注册115开放平台驱动
func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(Open115)
	})
}