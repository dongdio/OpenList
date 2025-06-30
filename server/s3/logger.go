package s3

// Credits: https://pkg.go.dev/github.com/rclone/rclone@v1.65.2/cmd/serve/s3
// Package s3 implements a fake s3 server for OpenList

import (
	"fmt"

	"github.com/itsHenry35/gofakes3"

	"github.com/dongdio/OpenList/utility/utils"
)

// logger implements gofakes3.Logger interface for OpenList's logging system
type logger struct{}

// Print logs messages with appropriate log level
func (l logger) Print(level gofakes3.LogLevel, v ...any) {
	message := fmt.Sprintln(v...)
	prefix := "serve s3: "

	switch level {
	case gofakes3.LogInfo:
		utils.Log.Debugf("%s%s", prefix, message)
	case gofakes3.LogWarn:
		utils.Log.Infof("%s%s", prefix, message)
	default: // Including gofakes3.LogErr
		utils.Log.Errorf("%s%s", prefix, message)
	}
}
