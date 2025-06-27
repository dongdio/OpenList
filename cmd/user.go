package cmd

import (
	"crypto/tls"
	"fmt"
	"time"

	"resty.dev/v3"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/utility/utils"
)

func DelAdminCacheOnline() {
	admin, err := op.GetAdmin()
	if err != nil {
		utils.Log.Errorf("[del_admin_cache] get admin error: %+v", err)
		return
	}
	DelUserCacheOnline(admin.Username)
}

func DelUserCacheOnline(username string) {
	client := resty.New().SetTimeout(1 * time.Second).SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify})
	token := setting.GetStr(consts.Token)
	port := conf.Conf.Scheme.HttpPort
	u := fmt.Sprintf("http://localhost:%d/api/admin/user/del_cache", port)
	if port == -1 {
		if conf.Conf.Scheme.HttpsPort == -1 {
			utils.Log.Warnf("[del_user_cache] no open port")
			return
		}
		u = fmt.Sprintf("https://localhost:%d/api/admin/user/del_cache", conf.Conf.Scheme.HttpsPort)
	}
	res, err := client.R().SetHeader("Authorization", token).SetQueryParam("username", username).Post(u)
	if err != nil {
		utils.Log.Warnf("[del_user_cache_online] failed: %+v", err)
		return
	}
	if res.StatusCode() != 200 {
		utils.Log.Warnf("[del_user_cache_online] failed: %+v", res.String())
		return
	}
	code := utils.GetBytes(res.Bytes(), "code").Int()
	msg := utils.GetBytes(res.Bytes(), "message").String()
	if code != 200 {
		utils.Log.Errorf("[del_user_cache_online] error: %s", msg)
		return
	}
	utils.Log.Debugf("[del_user_cache_online] del user [%s] cache success", username)
}