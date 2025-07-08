package _189pc

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 存储189云盘的配置信息，包含用户凭据、排序规则、上传方式等设置。
type Addition struct {
	driver.RootID
	Username       string `json:"username" required:"true"`                                                         // 用户名，用于登录189云盘
	Password       string `json:"password" required:"true"`                                                         // 密码，用于登录189云盘
	ValidateCode   string `json:"validate_code"`                                                                    // 验证码，用于额外的身份验证
	OrderBy        string `json:"order_by" type:"select" options:"filename,filesize,lastOpTime" default:"filename"` // 排序依据，可选文件名、大小或最后操作时间
	OrderDirection string `json:"order_direction" type:"select" options:"asc,desc" default:"asc"`                   // 排序方向，升序或降序
	Type           string `json:"type" type:"select" options:"personal,family" default:"personal"`                  // 账户类型，个人或家庭
	FamilyID       string `json:"family_id"`                                                                        // 家庭ID，当类型为家庭时使用
	UploadMethod   string `json:"upload_method" type:"select" options:"stream,rapid,old" default:"stream"`          // 上传方法，流式、快速或旧版
	UploadThread   string `json:"upload_thread" default:"3" help:"1<=thread<=32"`                                   // 上传线程数，范围1到32
	FamilyTransfer bool   `json:"family_transfer"`                                                                  // 是否启用家庭账户文件转移
	RapidUpload    bool   `json:"rapid_upload"`                                                                     // 是否启用快速上传模式
	NoUseOcr       bool   `json:"no_use_ocr"`                                                                       // 是否禁用OCR功能
}

// config 驱动配置，定义了驱动的基本信息和默认设置。
var config = driver.Config{
	Name:        "189CloudPC", // 驱动名称
	DefaultRoot: "-11",        // 默认根目录ID
	CheckStatus: true,         // 是否需要检查状态
}

// init 初始化驱动，将189云盘驱动注册到系统中。
func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(Cloud189PC)
	})
}

// 常量定义，包含189云盘的账户类型、应用ID、客户端类型、版本号及相关URL
const (
	_accountType = "02"         // 账户类型代码
	_appID       = "8025431004" // 应用ID
	_clientType  = "10020"      // 客户端类型代码
	_version     = "6.2"        // 版本号

	_webURL    = "https://cloud.189.cn"        // 主网页URL
	_authURL   = "https://open.e.189.cn"       // 认证服务URL
	_apiURL    = "https://api.cloud.189.cn"    // API服务URL
	_uploadURL = "https://upload.cloud.189.cn" // 上传服务URL

	_returnURL = "https://m.cloud.189.cn/zhuanti/2020/loginErrorPc/index.html" // 登录失败返回URL

	PC  = "TELEPC"  // PC客户端标识
	MAC = "TELEMAC" // MAC客户端标识

	_channelID = "web_cloud.189.cn" // 渠道ID
)
