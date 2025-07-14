package _115

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 定义115云盘驱动的额外配置选项
type Addition struct {
	// Cookie 115网盘的Cookie信息
	Cookie string `json:"cookie" type:"text" help:"Cookie和二维码令牌至少需要提供一个"`
	// QRCodeToken 115网盘的二维码登录令牌
	QRCodeToken string `json:"qrcode_token" type:"text" help:"Cookie和二维码令牌至少需要提供一个"`
	// QRCodeSource 二维码来源设备类型
	QRCodeSource string `json:"qrcode_source" type:"select" options:"web,android,ios,tv,alipaymini,wechatmini,qandroid" default:"linux" help:"选择二维码设备类型，默认为linux"`
	// PageSize 每页列表大小
	PageSize int64 `json:"page_size" type:"number" default:"1000" help:"115驱动列表API的每页大小"`
	// LimitRate API请求速率限制
	LimitRate float64 `json:"limit_rate" type:"float" default:"2" help:"限制所有API请求速率（每秒[限制]个请求）"`
	driver.RootID
}

// 驱动配置
var config = driver.Config{
	Name:        "115 Cloud", // 驱动名称
	DefaultRoot: "0",         // 默认根目录ID
	// OnlyProxy:   true,      // 是否仅支持代理模式（已注释）
	// NoOverwriteUpload: true, // 是否禁止覆盖上传（已注释）
}

// init 初始化函数，注册115云盘驱动
func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(Pan115)
	})
}