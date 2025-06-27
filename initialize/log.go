package initialize

import (
	"io"
	"log"
	"os"

	"github.com/natefinch/lumberjack"
	"github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/global"
	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/utility/utils"
)

func init() {
	formatter := logrus.TextFormatter{
		ForceColors:               true,
		EnvironmentOverrideColors: true,
		TimestampFormat:           "2006-01-02 15:04:05",
		FullTimestamp:             true,
	}
	logrus.SetFormatter(&formatter)
	utils.Log.SetFormatter(&formatter)
}

func setLog(l *logrus.Logger) {
	if global.Debug || global.Dev {
		l.SetLevel(logrus.DebugLevel)
		l.SetReportCaller(true)
	} else {
		l.SetLevel(logrus.InfoLevel)
		l.SetReportCaller(false)
	}
}

func initLog() {
	setLog(logrus.StandardLogger())
	setLog(utils.Log)
	logConfig := conf.Conf.Log
	if logConfig.Enable {
		var w io.Writer = &lumberjack.Logger{
			Filename:   logConfig.Name,
			MaxSize:    logConfig.MaxSize, // megabytes
			MaxBackups: logConfig.MaxBackups,
			MaxAge:     logConfig.MaxAge,   // days
			Compress:   logConfig.Compress, // disabled by default
		}
		if global.Debug || global.Dev || global.LogStd {
			w = io.MultiWriter(os.Stdout, w)
		}
		logrus.SetOutput(w)
	}
	log.SetOutput(logrus.StandardLogger().Out)
	utils.Log.Infof("init logrus...")
}