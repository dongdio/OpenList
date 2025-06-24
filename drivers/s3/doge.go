package s3

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"

	"resty.dev/v3"

	"github.com/dongdio/OpenList/drivers/base"
	"github.com/dongdio/OpenList/pkg/utils"
)

type TmpTokenResponse struct {
	Code int                  `json:"code"`
	Msg  string               `json:"msg"`
	Data TmpTokenResponseData `json:"data,omitempty"`
}

type TmpTokenResponseData struct {
	Credentials Credentials `json:"Credentials"`
	ExpiredAt   int         `json:"ExpiredAt"`
}

type Credentials struct {
	AccessKeyId     string `json:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty"`
}

func getCredentials(AccessKey, SecretKey string) (rst Credentials, err error) {
	apiPath := "/auth/tmp_token.json"
	var reqBody []byte
	reqBody, err = utils.Json.Marshal(map[string]any{"channel": "OSS_FULL", "scopes": []string{"*"}})
	if err != nil {
		return
	}

	signStr := apiPath + "\n" + string(reqBody)
	hmacObj := hmac.New(sha1.New, []byte(SecretKey))
	hmacObj.Write([]byte(signStr))
	sign := hex.EncodeToString(hmacObj.Sum(nil))
	Authorization := "TOKEN " + AccessKey + ":" + sign

	var resp *resty.Response
	resp, err = base.NoRedirectClient.R().
		SetHeader("Authorization", Authorization).
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		Post("https://api.dogecloud.com" + apiPath)
	if err != nil {
		return
	}

	var tmpTokenResp TmpTokenResponse
	err = utils.Json.Unmarshal(resp.Bytes(), &tmpTokenResp)
	if err != nil {
		return rst, err
	}

	return tmpTokenResp.Data.Credentials, nil
}