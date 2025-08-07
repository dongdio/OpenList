package _115

import (
	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/drivers/base"
)

// 全局变量
var (
	// md5Salt 用于生成签名的MD5盐值
	md5Salt = "Qclm8MGWUv59TnrR0XPg"

	// appVer 默认应用版本号，当无法获取最新版本时使用
	appVer = "27.0.5.7"
)

// getAppVersion 获取115云盘应用的最新版本信息
// 返回:
//   - []driver115.AppVersion: 应用版本列表
//   - error: 错误信息
func (p *Pan115) getAppVersion() ([]driver115.AppVersion, error) {
	result := driver115.VersionResp{}
	resp, err := base.RestyClient.R().Get(driver115.ApiGetVersion)

	// 检查请求和响应错误
	err = checkErr(err, &result, resp)
	if err != nil {
		return nil, errs.Wrap(err, "获取应用版本信息失败")
	}

	return result.Data.GetAppVersions(), nil
}

// getAppVer 获取Windows平台的最新应用版本号
// 如果获取失败，则返回默认版本号
// 返回:
//   - string: 应用版本号
func (p *Pan115) getAppVer() string {
	// TODO: 添加缓存机制，避免频繁请求
	versions, err := p.getAppVersion()
	if err != nil {
		log.Warnf("[115] 获取应用版本失败: %v", err)
		return appVer // 返回默认版本号
	}

	// 查找Windows平台的版本号
	for _, version := range versions {
		if version.AppName == "win" {
			return version.Version
		}
	}

	// 未找到Windows版本时返回默认版本
	return appVer
}

// initAppVer 初始化应用版本号
// 在驱动初始化时调用，确保使用最新的应用版本
func (p *Pan115) initAppVer() {
	appVer = p.getAppVer()
}