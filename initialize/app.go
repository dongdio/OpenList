package initialize

import (
	"github.com/dongdio/OpenList/v4/global"
)

func InitApp(server ...bool) {
	InitConfig()
	initLog()
	initializeDB()

	initUser()
	initSettings()
	initTasks()
	if global.Dev {
		initDevData()
		initDevDo()
	}
	initStreamLimit()
	initIndex()
	initUpgradePatch()

	if len(server) > 0 && server[0] {
		// 只有server启动时加载
		initOfflineDownloadTools()
		initLoadStorages()
		initTaskManager()
	}
}