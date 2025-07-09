package initialize

import (
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

func initOfflineDownloadTools() {
	for k, v := range tool.Tools {
		res, err := v.Init()
		if err != nil {
			utils.Log.Warnf("init offline download %s failed: %s", k, err)
		} else {
			utils.Log.Infof("init offline download %s success: %s", k, res)
		}
	}
}