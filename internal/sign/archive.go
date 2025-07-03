package sign

import (
	"sync"
	"time"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/sign"
)

var onceArchive sync.Once
var instanceArchive sign.Sign

func SignArchive(data string) string {
	expire := setting.GetInt(consts.LinkExpiration, 0)
	if expire == 0 {
		return NotExpiredArchive(data)
	} else {
		return WithDurationArchive(data, time.Duration(expire)*time.Hour)
	}
}

func WithDurationArchive(data string, d time.Duration) string {
	onceArchive.Do(InstanceArchive)
	return instanceArchive.Sign(data, time.Now().Add(d).Unix())
}

func NotExpiredArchive(data string) string {
	onceArchive.Do(InstanceArchive)
	return instanceArchive.Sign(data, 0)
}

func VerifyArchive(data string, sign string) error {
	onceArchive.Do(InstanceArchive)
	return instanceArchive.Verify(data, sign)
}

func InstanceArchive() {
	instanceArchive = sign.NewHMACSign([]byte(setting.GetStr(consts.Token) + "-archive"))
}