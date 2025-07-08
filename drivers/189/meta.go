package _189

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
	"resty.dev/v3"
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

const (
	_refer           = "https://cloud.189.cn/"
	_fileInfoURL     = "https://cloud.189.cn/api/portal/getFileInfo.action"
	_createFolder    = "https://cloud.189.cn/api/open/file/createFolder.action"
	_createBatchTask = "https://cloud.189.cn/api/open/batch/createBatchTask.action"
	_renameFile      = "https://cloud.189.cn/api/open/file/renameFile.action"
	_renameFolder    = "https://cloud.189.cn/api/open/file/renameFolder.action"

	_listFiles        = "https://cloud.189.cn/api/open/file/listFiles.action"
	_getUserBriefInfo = "https://cloud.189.cn/v2/getUserBriefInfo.action"
	_generateRsaKey   = "https://cloud.189.cn/api/security/generateRsaKey.action"
)

func _callBack(data map[string]string) func(req *resty.Request) {
	return func(req *resty.Request) {
		req.SetFormData(data)
	}
}
