package data

import "github.com/dongdio/OpenList/cmd/flags"

func InitData() {
	initUser()
	initSettings()
	initTasks()
	if flags.Dev {
		initDevData()
		initDevDo()
	}
}