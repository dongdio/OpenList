package initialize

import (
	"time"

	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/global"
)

func initCron() {
	global.CronConfig = cron.New(
		cron.WithLocation(time.Local),
		cron.WithChain(cron.DelayIfStillRunning(cron.DefaultLogger)),
	)
	var err error
	for k, v := range global.GetJobs() {
		_, err = global.CronConfig.AddFunc(k, v)
		if err != nil {
			logrus.Errorf("failed to add cron job: %+v", err)
		}
	}
	go func() {
		defer global.CronConfig.Stop()
		global.CronConfig.Run()
	}()
}