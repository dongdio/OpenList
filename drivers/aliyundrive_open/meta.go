package aliyundrive_open

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 存储阿里云盘开放平台所需的额外配置信息
type Addition struct {
	// DriveType 驱动类型：
	// - default: 默认个人云盘
	// - resource: 资源库（推荐）
	// - backup: 备份库
	DriveType string `json:"drive_type" type:"select" options:"default,resource,backup" default:"resource"`
	driver.RootID
	// RefreshToken 刷新令牌，用于获取访问令牌
	// 必须提供有效的刷新令牌才能访问阿里云盘
	RefreshToken string `json:"refresh_token" required:"true"`
	// OrderBy 文件排序字段
	// - name: 按名称排序
	// - size: 按大小排序
	// - updated_at: 按更新时间排序
	// - created_at: 按创建时间排序
	OrderBy string `json:"order_by" type:"select" options:"name,size,updated_at,created_at"`
	// OrderDirection 排序方向
	// - ASC: 升序排列
	// - DESC: 降序排列
	OrderDirection string `json:"order_direction" type:"select" options:"ASC,DESC"`
	// UseOnlineAPI 是否使用在线API刷新令牌
	// 启用后将使用外部API服务刷新令牌，无需提供ClientID和ClientSecret
	UseOnlineAPI bool `json:"use_online_api" default:"true"`
	// AlipanType 阿里云盘类型
	// - default: 默认
	AlipanType string `json:"alipan_type" required:"true" type:"select" default:"default" options:"default,alipanTV"`
	// APIAddress API地址，用于刷新令牌
	// 仅当UseOnlineAPI为true时有效
	APIAddress string `json:"api_url_address" default:"https://api.oplist.org/alicloud/renewapi"`
	// ClientID 客户端ID
	// 如果不使用在线API则必须提供
	ClientID string `json:"client_id" help:"如果不使用在线API刷新令牌，则必须填写"`
	// ClientSecret 客户端密钥
	// 如果不使用在线API则必须提供
	ClientSecret string `json:"client_secret" help:"如果不使用在线API刷新令牌，则必须填写"`
	// RemoveWay 删除方式
	// - trash: 移入回收站（可恢复）
	// - delete: 直接删除（不可恢复）
	RemoveWay string `json:"remove_way" required:"true" type:"select" options:"trash,delete"`
	// RapidUpload 是否启用秒传
	// 启用后可加速上传，但进度显示可能不准确
	RapidUpload bool `json:"rapid_upload" help:"启用此选项后，文件将先上传到服务器，因此进度显示可能不准确"`
	// InternalUpload 是否使用内部上传
	// 仅适用于阿里云ECS北京区域的服务器，可提升上传速度
	InternalUpload bool `json:"internal_upload" help:"如果您使用的是位于北京的阿里云ECS，可以开启此选项以提升上传速度"`
	// LIVPDownloadFormat LIVP文件下载格式
	// - jpeg: 下载为JPEG图片
	// - mov: 下载为MOV视频
	LIVPDownloadFormat string `json:"livp_download_format" type:"select" options:"jpeg,mov" default:"jpeg"`
	// AccessToken 访问令牌
	// 运行时使用，不需要用户填写，由系统自动管理
	AccessToken string
}

// 驱动配置
var config = driver.Config{
	Name:              "AliyundriveOpen", // 驱动名称
	DefaultRoot:       "root",            // 默认根目录ID
	NoOverwriteUpload: true,              // 是否禁止覆盖上传（是：禁止覆盖）
}

// 阿里云盘开放平台API基础URL
var apiURL = "https://openapi.alipan.com"

// init 初始化函数，注册驱动
func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(AliyundriveOpen)
	})
}