package aliyundrive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/dustinxie/ecc"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// createSession 创建设备会话
func (d *AliDrive) createSession() error {
	state, ok := userStates.Load(d.UserID)
	if !ok {
		return errors.Errorf("无法加载用户状态，用户ID: %s", d.UserID)
	}

	// 生成签名
	d.sign()

	// 重试计数增加
	state.retry++
	if state.retry > 3 {
		state.retry = 0
		return errors.New("创建会话失败，已重试3次")
	}

	// 发送创建会话请求
	_, err, _ := d.request("https://api.alipan.com/users/v1/users/device/create_session", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"deviceName":   "samsung",
			"modelName":    "SM-G9810",
			"nonce":        0,
			"pubKey":       PublicKeyToHex(&state.privateKey.PublicKey),
			"refreshToken": d.RefreshToken,
		})
	}, nil)

	if err == nil {
		state.retry = 0
	}
	return err
}

// func (d *AliDrive) renewSession() error {
// 	_, err, _ := d.request("https://api.alipan.com/users/v1/users/device/renew_session", http.MethodPost, nil, nil)
// 	return err
// }

// sign 生成签名
func (d *AliDrive) sign() {
	state, _ := userStates.Load(d.UserID)
	secpAppID := "5dde4e1bdf9e4966b387ba58f4b3fdc3"
	signData := fmt.Sprintf("%s:%s:%s:%d", secpAppID, state.deviceID, d.UserID, 0)
	hash := sha256.Sum256([]byte(signData))
	data, _ := ecc.SignBytes(state.privateKey, hash[:], ecc.RecID|ecc.LowerS)
	state.signature = hex.EncodeToString(data)
}

// do others that not defined in Driver interface

// refreshToken 刷新访问令牌
func (d *AliDrive) refreshToken() error {
	url := "https://auth.alipan.com/v2/account/token"
	var resp base.TokenResp
	var e RespErr

	// 发送刷新令牌请求
	_, err := base.RestyClient.R().
		SetBody(base.Json{"refresh_token": d.RefreshToken, "grant_type": "refresh_token"}).
		SetResult(&resp).
		SetError(&e).
		Post(url)

	if err != nil {
		return errors.Wrap(err, "发送刷新令牌请求失败")
	}

	if e.Code != "" {
		return errors.Errorf("刷新令牌失败: %s", e.Message)
	}

	if resp.RefreshToken == "" {
		return errors.New("刷新令牌失败: 返回的刷新令牌为空")
	}

	// 更新令牌
	d.RefreshToken, d.AccessToken = resp.RefreshToken, resp.AccessToken
	op.MustSaveDriverStorage(d)
	return nil
}

// request 发送API请求
func (d *AliDrive) request(url, method string, callback base.ReqCallback, resp any) ([]byte, error, RespErr) {
	req := base.RestyClient.R()
	state, ok := userStates.Load(d.UserID)

	if !ok {
		if url == "https://api.alipan.com/v2/user/get" {
			state = &State{}
		} else {
			return nil, errors.Errorf("无法加载用户状态，用户ID: %s", d.UserID), RespErr{}
		}
	}

	// 设置请求头
	req.SetHeaders(map[string]string{
		"Authorization": "Bearer\t" + d.AccessToken,
		"content-type":  "application/json",
		"origin":        "https://www.alipan.com",
		"Referer":       "https://alipan.com/",
		"X-Signature":   state.signature,
		"x-request-id":  uuid.NewString(),
		"X-Canary":      "client=Android,app=adrive,version=v4.1.0",
		"X-Device-Id":   state.deviceID,
	})

	// 设置请求体
	if callback != nil {
		callback(req)
	} else {
		req.SetBody("{}")
	}

	// 设置响应处理
	if resp != nil {
		req.SetResult(resp)
	}

	var e RespErr
	req.SetError(&e)

	// 发送请求
	res, err := req.Execute(method, url)
	if err != nil {
		return nil, errors.Wrap(err, "发送请求失败"), e
	}

	// 处理错误响应
	if e.Code != "" {
		switch e.Code {
		case "AccessTokenInvalid":
			// 令牌无效，尝试刷新
			err = d.refreshToken()
			if err != nil {
				return nil, errors.Wrap(err, "刷新令牌失败"), e
			}
		case "DeviceSessionSignatureInvalid":
			// 会话签名无效，尝试创建新会话
			err = d.createSession()
			if err != nil {
				return nil, errors.Wrap(err, "创建会话失败"), e
			}
		default:
			return nil, errors.New(e.Message), e
		}
		// 重试请求
		return d.request(url, method, callback, resp)
	} else if res.IsError() {
		return nil, errors.Errorf("请求返回错误状态码: %s", res.Status()), e
	}

	return res.Bytes(), nil, e
}

// getFiles 获取目录下的文件列表
func (d *AliDrive) getFiles(fileID string) ([]File, error) {
	marker := "first"
	result := make([]File, 0)

	for marker != "" {
		if marker == "first" {
			marker = ""
		}

		var resp Files
		data := base.Json{
			"drive_id":                d.DriveID,
			"fields":                  "*",
			"image_thumbnail_process": "image/resize,w_400/format,jpeg",
			"image_url_process":       "image/resize,w_1920/format,jpeg",
			"limit":                   200,
			"marker":                  marker,
			"order_by":                d.OrderBy,
			"order_direction":         d.OrderDirection,
			"parent_file_id":          fileID,
			"video_thumbnail_process": "video/snapshot,t_0,f_jpg,ar_auto,w_300",
			"url_expire_sec":          14400,
		}

		_, err, _ := d.request("https://api.alipan.com/v2/file/list", http.MethodPost, func(req *resty.Request) {
			req.SetBody(data)
		}, &resp)

		if err != nil {
			return nil, errors.Wrap(err, "获取文件列表失败")
		}

		marker = resp.NextMarker
		result = append(result, resp.Items...)
	}

	return result, nil
}

// batch 批量处理文件操作（移动/复制）
func (d *AliDrive) batch(srcID, dstID string, url string) error {
	res, err, _ := d.request("https://api.alipan.com/v3/batch", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"requests": []base.Json{
				{
					"headers": base.Json{
						"Content-Type": "application/json",
					},
					"method": "POST",
					"id":     srcID,
					"body": base.Json{
						"drive_id":          d.DriveID,
						"file_id":           srcID,
						"to_drive_id":       d.DriveID,
						"to_parent_file_id": dstID,
					},
					"url": url,
				},
			},
			"resource": "file",
		})
	}, nil)

	if err != nil {
		return errors.Wrap(err, "批量操作请求失败")
	}

	status := utils.GetBytes(res, "responses.0.status").Int()
	if status >= 100 && status < 400 {
		return nil
	}

	return errors.Errorf("批量操作失败，状态码: %d，响应: %s", status, string(res))
}