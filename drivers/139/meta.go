package _139

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 定义 139 云盘驱动的配置项
// 用于存储授权信息、云盘类型、分片大小等参数
// 该结构体会被序列化用于配置管理
// 字段说明见各字段注释
type Addition struct {
	// Authorization: 139 云盘的授权令牌，必填，用于身份验证和API调用
	Authorization string `json:"authorization" type:"text" required:"true"`
	// RootID: 根目录 ID，继承自 driver.RootID，用于指定驱动的根目录
	driver.RootID
	// Type: 云盘类型（新版个人云、家庭云、群组云、旧版个人云），用于区分不同的云盘服务类型
	Type string `json:"type" type:"select" options:"personal_new,family,group,personal" default:"personal_new"`
	// CloudID: 云盘 ID（家庭云/群组云专用），用于指定家庭云或群组云的唯一标识
	CloudID string `json:"cloud_id"`
	// CustomUploadPartSize: 自定义上传分片大小（字节），0 表示自动，用于控制文件上传时的分片大小
	CustomUploadPartSize int64 `json:"custom_upload_part_size" type:"number" default:"0" help:"0 for auto"`
	// ReportRealSize: 上传时是否报告真实文件大小，启用后上传时会报告文件的真实大小
	ReportRealSize bool `json:"report_real_size" type:"bool" default:"true" help:"Enable to report the real file size during upload"`
	// UseLargeThumbnail: 是否使用大尺寸缩略图，启用后会获取图像的大尺寸缩略图
	UseLargeThumbnail bool `json:"use_large_thumbnail" type:"bool" default:"false" help:"Enable to use large thumbnail for images"`
}

// config 定义驱动的基本元信息
// 包含驱动名称、是否支持本地排序、是否支持代理范围选项等基本配置
var config = driver.Config{
	Name:             "139Yun", // 驱动名称，用于在系统中唯一标识该驱动
	LocalSort:        true,     // 启用本地排序，文件列表将由本地逻辑进行排序
	ProxyRangeOption: true,     // 启用代理范围选项，支持通过代理进行范围请求
}

// init 在包初始化时注册 139 云盘驱动
// 该函数会在包加载时自动执行，将 139 云盘驱动注册到系统中
func init() {
	op.RegisterDriver(func() driver.Driver {
		d := &Yun139{}
		d.ProxyRange = true
		return d
	})
}
