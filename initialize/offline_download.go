package initialize

import (
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

func initOfflineDownloadTools() {
	for k, v := range tool.Tools {
		res, err := v.Init()
		if err != nil {
			utils.Log.Warnf("init tool %s failed: %s", k, err)
		} else {
			utils.Log.Infof("init tool %s success: %s", k, res)
		}
	}
}