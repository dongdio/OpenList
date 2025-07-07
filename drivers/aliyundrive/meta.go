package aliyundrive

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 存储阿里云盘所需的额外配置信息
type Addition struct {
	driver.RootID
	RefreshToken string `json:"refresh_token" required:"true"`
	// DeviceID       string `json:"device_id" required:"true"`
	OrderBy        string `json:"order_by" type:"select" options:"name,size,updated_at,created_at"`
	OrderDirection string `json:"order_direction" type:"select" options:"ASC,DESC"`
	RapidUpload    bool   `json:"rapid_upload"`
	InternalUpload bool   `json:"internal_upload"`
}

var config = driver.Config{
	Name:        "Aliyundrive",
	DefaultRoot: "root",
	Alert: `warning|此驱动可能存在无限循环的问题。
此驱动已被弃用，不再维护，将在未来版本中移除。
建议使用官方驱动 AliyundriveOpen。`,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(AliDrive)
	})
}
