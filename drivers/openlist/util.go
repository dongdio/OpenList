package openlist

import (
	"net/http"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/drivers/base"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/server/common"
	"github.com/dongdio/OpenList/utility/utils"
)

func (d *OpenList) login() error {
	if d.Username == "" {
		return nil
	}
	var resp common.Resp[LoginResp]
	_, _, err := d.request("/auth/login", http.MethodPost, func(req *resty.Request) {
		req.SetResult(&resp).SetBody(base.Json{
			"username": d.Username,
			"password": d.Password,
		})
	})
	if err != nil {
		return err
	}
	d.Token = resp.Data.Token
	op.MustSaveDriverStorage(d)
	return nil
}

func (d *OpenList) request(api, method string, callback base.ReqCallback, retry ...bool) ([]byte, int, error) {
	url := d.Address + "/api" + api
	req := base.RestyClient.R()
	req.SetHeader("Authorization", d.Token)
	if callback != nil {
		callback(req)
	}
	res, err := req.Execute(method, url)
	if err != nil {
		code := 0
		if res != nil {
			code = res.StatusCode()
		}
		return nil, code, err
	}
	log.Debugf("[openlist] response body: %s", res.String())
	if res.StatusCode() >= 400 {
		return nil, res.StatusCode(), errors.Errorf("request failed, status: %s", res.Status())
	}
	code := int(utils.GetBytes(res.Bytes(), "code").Int())
	if code != 200 {
		if (code == 401 || code == 403) && !utils.IsBool(retry...) {
			err = d.login()
			if err != nil {
				return nil, code, err
			}
			return d.request(api, method, callback, true)
		}
		return nil, code, errors.Errorf("request failed,code: %d, message: %s", code, utils.GetBytes(res.Bytes(), "message").String())
	}
	return res.Bytes(), 200, nil
}