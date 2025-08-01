package kodbox

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

func (d *KodBox) getToken() error {
	var authResp CommonResp
	res, err := base.RestyClient.R().
		SetResult(&authResp).
		SetQueryParams(map[string]string{
			"name":     d.UserName,
			"password": d.Password,
		}).
		Post(d.Address + "/?user/index/loginSubmit")
	if err != nil {
		return err
	}
	if res.StatusCode() >= 400 {
		return errors.Errorf("get token failed: %s", res.String())
	}

	if res.StatusCode() == 200 && authResp.Code.(bool) == false {
		return errors.Errorf("get token failed: %s", res.String())
	}

	d.authorization = fmt.Sprintf("%s", authResp.Info)
	return nil
}

func (d *KodBox) request(method string, pathname string, callback base.ReqCallback, noRedirect ...bool) ([]byte, error) {
	full := pathname
	if !strings.HasPrefix(pathname, "http") {
		full = d.Address + pathname
	}
	req := base.RestyClient.R()
	if len(noRedirect) > 0 && noRedirect[0] {
		req = base.NoRedirectClient.R()
	}
	req.SetFormData(map[string]string{
		"accessToken": d.authorization,
	})
	callback(req)

	var (
		res        *resty.Response
		commonResp *CommonResp
		err        error
		skip       bool
	)
	for i := 0; i < 2; i++ {
		if skip {
			break
		}
		res, err = req.Execute(method, full)
		if err != nil {
			return nil, err
		}

		err := utils.JSONTool.Unmarshal(res.Bytes(), &commonResp)
		if err != nil {
			return nil, err
		}

		switch commonResp.Code.(type) {
		case bool:
			skip = true
		case string:
			if commonResp.Code.(string) == "10001" {
				err = d.getToken()
				if err != nil {
					return nil, err
				}
				req.SetFormData(map[string]string{"accessToken": d.authorization})
			}
		}
	}
	if commonResp.Code.(bool) == false {
		return nil, errors.Errorf("request failed: %s", commonResp.Data)
	}
	return res.Bytes(), nil
}
