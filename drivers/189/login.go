package _189

import (
	"strconv"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// AppConf 应用配置响应结构体
type AppConf struct {
	Data struct {
		AccountType     string `json:"accountType"`     // 账号类型
		AgreementCheck  string `json:"agreementCheck"`  // 协议检查
		AppKey          string `json:"appKey"`          // 应用密钥
		ClientType      int    `json:"clientType"`      // 客户端类型
		IsOauth2        bool   `json:"isOauth2"`        // 是否OAuth2
		LoginSort       string `json:"loginSort"`       // 登录排序
		MailSuffix      string `json:"mailSuffix"`      // 邮箱后缀
		PageKey         string `json:"pageKey"`         // 页面密钥
		ParamId         string `json:"paramId"`         // 参数ID
		RegReturnUrl    string `json:"regReturnUrl"`    // 注册返回URL
		ReqId           string `json:"reqId"`           // 请求ID
		ReturnUrl       string `json:"returnUrl"`       // 返回URL
		ShowFeedback    string `json:"showFeedback"`    // 显示反馈
		ShowPwSaveName  string `json:"showPwSaveName"`  // 显示密码保存名称
		ShowQrSaveName  string `json:"showQrSaveName"`  // 显示二维码保存名称
		ShowSmsSaveName string `json:"showSmsSaveName"` // 显示短信保存名称
		Sso             string `json:"sso"`             // 单点登录
	} `json:"data"`
	Msg    string `json:"msg"`    // 响应消息
	Result string `json:"result"` // 响应结果
}

// EncryptConf 加密配置响应结构体
type EncryptConf struct {
	Result int `json:"result"` // 响应结果
	Data   struct {
		UpSmsOn   string `json:"upSmsOn"`   // 短信验证开关
		Pre       string `json:"pre"`       // 前缀
		PreDomain string `json:"preDomain"` // 前缀域名
		PubKey    string `json:"pubKey"`    // 公钥
	} `json:"data"`
}

// newLogin 实现新版登录逻辑
// 返回错误信息，nil表示登录成功
func (d *Cloud189) newLogin() error {
	// 访问登录URL，检查是否已登录
	loginURL := "https://cloud.189.cn/api/portal/loginUrl.action?redirectURL=https%3A%2F%2Fcloud.189.cn%2Fmain.action"
	res, err := base.RestyClient.R().SetHeaders(d.header).Get(loginURL)
	if err != nil {
		return errors.Wrap(err, "访问登录页面失败")
	}

	// 检查是否已登录
	redirectURL := res.RawResponse.Request.URL
	if redirectURL.String() == "https://cloud.189.cn/web/main" {
		return nil // 已登录，直接返回
	}

	// 获取登录参数
	lt := redirectURL.Query().Get("lt")
	reqId := redirectURL.Query().Get("reqId")
	appId := redirectURL.Query().Get("appId")

	// 设置请求头
	headers := map[string]string{
		"lt":      lt,
		"reqid":   reqId,
		"referer": redirectURL.String(),
		"origin":  "https://open.e.189.cn",
	}

	// 获取应用配置
	var appConf AppConf
	res, err = base.RestyClient.R().
		SetHeaders(headers).
		SetFormData(map[string]string{
			"version": "2.0",
			"appKey":  appId,
		}).
		SetResult(&appConf).
		Post("https://open.e.189.cn/api/logbox/oauth2/appConf.do")

	if err != nil {
		return errors.Wrap(err, "获取应用配置失败")
	}

	log.Debugf("189云盘应用配置响应: %s", res.String())
	if appConf.Result != "0" {
		return errors.Errorf("应用配置获取失败: %s", appConf.Msg)
	}

	// 获取加密配置
	var encryptConf EncryptConf
	res, err = base.RestyClient.R().
		SetHeaders(headers).
		SetFormData(map[string]string{
			"appId": appId,
		}).
		Post("https://open.e.189.cn/api/logbox/config/encryptConf.do")

	if err != nil {
		return errors.Wrap(err, "获取加密配置失败")
	}

	err = utils.Json.Unmarshal(res.Bytes(), &encryptConf)
	if err != nil {
		return errors.Wrap(err, "解析加密配置失败")
	}

	log.Debugf("189云盘加密配置响应: %s\n%+v", res.String(), encryptConf)
	if encryptConf.Result != 0 {
		return errors.Errorf("获取加密配置失败: %s", res.String())
	}

	// TODO: 实现验证码处理逻辑

	// 执行登录请求
	// 对用户名和密码进行RSA加密
	encryptedUsername := encryptConf.Data.Pre + RsaEncode([]byte(d.Username), encryptConf.Data.PubKey, true)
	encryptedPassword := encryptConf.Data.Pre + RsaEncode([]byte(d.Password), encryptConf.Data.PubKey, true)

	loginData := map[string]string{
		"version":         "v2.0",
		"apToken":         "",
		"appKey":          appId,
		"accountType":     appConf.Data.AccountType,
		"userName":        encryptedUsername,
		"epd":             encryptedPassword,
		"captchaType":     "",
		"validateCode":    "",
		"smsValidateCode": "",
		"captchaToken":    "",
		"returnUrl":       appConf.Data.ReturnUrl,
		"mailSuffix":      appConf.Data.MailSuffix,
		"dynamicCheck":    "FALSE",
		"clientType":      strconv.Itoa(appConf.Data.ClientType),
		"cb_SaveName":     "3",
		"isOauth2":        strconv.FormatBool(appConf.Data.IsOauth2),
		"state":           "",
		"paramId":         appConf.Data.ParamId,
	}

	res, err = base.RestyClient.R().
		SetHeaders(headers).
		SetFormData(loginData).
		Post("https://open.e.189.cn/api/logbox/oauth2/loginSubmit.do")

	if err != nil {
		return errors.Wrap(err, "提交登录请求失败")
	}

	log.Debugf("189云盘登录响应: %s", res.String())

	// 检查登录结果
	loginResult := utils.GetBytes(res.Bytes(), "result").Int()
	if loginResult != 0 {
		return errors.Errorf("登录失败: %s", utils.GetBytes(res.Bytes(), "msg").String())
	}

	return nil
}
