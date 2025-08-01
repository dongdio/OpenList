package cloudreve

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/cookie"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// do others that not defined in Driver interface

const loginPath = "/user/session"

func (d *Cloudreve) getUA() string {
	if d.CustomUA != "" {
		return d.CustomUA
	}
	return consts.ChromeUserAgent
}

func (d *Cloudreve) request(method string, path string, callback base.ReqCallback, out any) error {
	if d.ref != nil {
		return d.ref.request(method, path, callback, out)
	}
	u := d.Address + "/api/v3" + path
	var r Resp
	req := base.RestyClient.R().
		SetHeaders(map[string]string{
			"Cookie":     "cloudreve-session=" + d.Cookie,
			"Accept":     "application/json, text/plain, */*",
			"User-Agent": d.getUA(),
		}).
		SetResult(&r)

	if callback != nil {
		callback(req)
	}
	resp, err := req.Execute(method, u)
	if err != nil {
		return err
	}
	if !resp.IsSuccess() {
		return errors.New(resp.String())
	}

	if r.Code != 0 {
		// 刷新 cookie
		if r.Code == http.StatusUnauthorized && path != loginPath {
			if d.Username != "" && d.Password != "" {
				err = d.login()
				if err != nil {
					return err
				}
				return d.request(method, path, callback, out)
			}
		}

		return errors.New(r.Msg)
	}
	sess := cookie.GetCookie(resp.Cookies(), "cloudreve-session")
	if sess != nil {
		d.Cookie = sess.Value
	}
	if out != nil && r.Data != nil {
		var marshal []byte
		marshal, err = utils.JSONTool.Marshal(r.Data)
		if err != nil {
			return err
		}
		err = utils.JSONTool.Unmarshal(marshal, out)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Cloudreve) login() error {
	var siteConfig Config
	err := d.request(http.MethodGet, "/site/config", nil, &siteConfig)
	if err != nil {
		return err
	}
	for i := 0; i < 5; i++ {
		err = d.doLogin(siteConfig.LoginCaptcha)
		if err == nil {
			break
		}
		if err.Error() != "CAPTCHA not match." {
			break
		}
	}
	return err
}

func (d *Cloudreve) doLogin(needCaptcha bool) error {
	var captchaCode string
	var err error
	if needCaptcha {
		var captcha string
		err = d.request(http.MethodGet, "/site/captcha", nil, &captcha)
		if err != nil {
			return err
		}
		if len(captcha) == 0 {
			return errors.New("can not get captcha")
		}
		i := strings.Index(captcha, ",")
		dec := base64.NewDecoder(base64.StdEncoding, strings.NewReader(captcha[i+1:]))
		vRes, err := base.RestyClient.R().
			SetMultipartField(
				"image", "validateCode.png", "image/png", dec).
			Post(setting.GetStr(consts.OcrApi))
		if err != nil {
			return err
		}
		if utils.GetBytes(vRes.Bytes(), "status").Int() != 200 {
			return errors.New("ocr error:" + utils.GetBytes(vRes.Bytes(), "msg").String())
		}
		captchaCode = utils.GetBytes(vRes.Bytes(), "result").String()
	}
	var resp Resp
	err = d.request(http.MethodPost, loginPath, func(req *resty.Request) {
		req.SetBody(base.Json{
			"username":    d.Addition.Username,
			"Password":    d.Addition.Password,
			"captchaCode": captchaCode,
		})
	}, &resp)
	return err
}

func convertSrc(obj model.Obj) map[string]any {
	m := make(map[string]any)
	var dirs []string
	var items []string
	if obj.IsDir() {
		dirs = append(dirs, obj.GetID())
	} else {
		items = append(items, obj.GetID())
	}
	m["dirs"] = dirs
	m["items"] = items
	return m
}

func (d *Cloudreve) GetThumb(file Object) (model.Thumbnail, error) {
	if !d.Addition.EnableThumbAndFolderSize {
		return model.Thumbnail{}, nil
	}
	req := base.NoRedirectClient.R()
	req.SetHeaders(map[string]string{
		"Cookie":     "cloudreve-session=" + d.Cookie,
		"Accept":     "image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
		"User-Agent": d.getUA(),
	})
	resp, err := req.Execute(http.MethodGet, d.Address+"/api/v3/file/thumb/"+file.Id)
	if err != nil {
		return model.Thumbnail{}, err
	}
	return model.Thumbnail{
		Thumbnail: resp.Header().Get("Location"),
	}, nil
}

func (d *Cloudreve) upLocal(ctx context.Context, stream model.FileStreamer, u UploadInfo, up driver.UpdateProgress) error {
	var finish int64 = 0
	var chunk int = 0
	DEFAULT := int64(u.ChunkSize)
	for finish < stream.GetSize() {
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}
		left := stream.GetSize() - finish
		byteSize := min(left, DEFAULT)
		utils.Log.Debugf("[Cloudreve-Local] upload range: %d-%d/%d", finish, finish+byteSize-1, stream.GetSize())
		byteData := make([]byte, byteSize)
		n, err := io.ReadFull(stream, byteData)
		utils.Log.Debug(err, n)
		if err != nil {
			return err
		}
		err = d.request(http.MethodPost, "/file/upload/"+u.SessionID+"/"+strconv.Itoa(chunk), func(req *resty.Request) {
			req.SetHeader("Content-Type", "application/octet-stream")
			req.SetContentLength(true)
			req.SetHeader("Content-Length", strconv.FormatInt(byteSize, 10))
			req.SetHeader("User-Agent", d.getUA())
			req.SetBody(driver.NewLimitedUploadStream(ctx, bytes.NewReader(byteData)))
			req.AddRetryConditions(func(r *resty.Response, err error) bool {
				if err != nil {
					return true
				}
				if r.IsError() {
					return true
				}
				var retryResp Resp
				jErr := utils.JSONTool.Unmarshal(r.Bytes(), &retryResp)
				if jErr != nil {
					return true
				}
				if retryResp.Code != 0 {
					return true
				}
				return false
			})
		}, nil)
		if err != nil {
			return err
		}
		finish += byteSize
		up(float64(finish) * 100 / float64(stream.GetSize()))
		chunk++
	}
	return nil
}

func (d *Cloudreve) upRemote(ctx context.Context, stream model.FileStreamer, u UploadInfo, up driver.UpdateProgress) error {
	var (
		uploadUrl        = u.UploadURLs[0]
		credential       = u.Credential
		finish     int64 = 0
		chunk            = 0
		DEFAULT          = int64(u.ChunkSize)
		retryCount       = 0
		maxRetries       = 3
	)

	for finish < stream.GetSize() {
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}
		byteSize := min(stream.GetSize()-finish, DEFAULT)
		utils.Log.Debugf("[Cloudreve-Remote] upload range: %d-%d/%d", finish, finish+byteSize-1, stream.GetSize())

		byteData := make([]byte, byteSize)
		n, err := io.ReadFull(stream, byteData)
		utils.Log.Debug(err, n)
		if err != nil {
			return err
		}
		err = func() error {
			var result Resp
			resp, e := base.NoRedirectClient.R().
				WithContext(ctx).
				SetHeader("Authorization", fmt.Sprint(credential)).
				SetHeader("User-Agent", d.getUA()).
				SetHeader("Content-Length", strconv.Itoa(int(byteSize))).
				SetBody(driver.NewLimitedUploadStream(ctx, bytes.NewReader(byteData))).
				SetResult(&result).
				Post(uploadUrl + "?chunk=" + strconv.Itoa(chunk))
			if e != nil {
				return e
			}
			if resp.StatusCode() != 200 {
				return errors.New(resp.Status())
			}
			if result.Code != 0 {
				return errors.New(result.Msg)
			}
			return nil
		}()
		if err == nil {
			retryCount = 0
			finish += byteSize
			up(float64(finish) * 100 / float64(stream.GetSize()))
			chunk++
		} else {
			retryCount++
			if retryCount > maxRetries {
				return errors.Errorf("upload failed after %d retries due to server errors, error: %s", maxRetries, err)
			}
			backoff := time.Duration(1<<retryCount) * time.Second
			utils.Log.Warnf("[Cloudreve-Remote] server errors while uploading, retrying after %v...", backoff)
			time.Sleep(backoff)
		}
	}
	return nil
}

func (d *Cloudreve) upOneDrive(ctx context.Context, stream model.FileStreamer, u UploadInfo, up driver.UpdateProgress) error {
	var (
		uploadUrl        = u.UploadURLs[0]
		finish     int64 = 0
		DEFAULT          = int64(u.ChunkSize)
		retryCount       = 0
		maxRetries       = 3
	)

	for finish < stream.GetSize() {
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}
		byteSize := min(stream.GetSize()-finish, DEFAULT)
		utils.Log.Debugf("[Cloudreve-OneDrive] upload range: %d-%d/%d", finish, finish+byteSize-1, stream.GetSize())

		byteData := make([]byte, byteSize)
		n, err := io.ReadFull(stream, byteData)
		utils.Log.Debug(err, n)
		if err != nil {
			return err
		}
		resp, err := base.NoRedirectClient.R().
			SetContext(ctx).
			SetHeader("Content-Length", strconv.Itoa(int(byteSize))).
			SetHeader("User-Agent", d.getUA()).
			SetHeader("Content-Range", fmt.Sprintf("bytes %d-%d/%d", finish, finish+byteSize-1, stream.GetSize())).
			SetBody(driver.NewLimitedUploadStream(ctx, bytes.NewReader(byteData))).
			Put(uploadUrl)
		if err != nil {
			return err
		}
		switch {
		case resp.StatusCode() >= 500 && resp.StatusCode() <= 504:
			retryCount++
			if retryCount > maxRetries {
				return errors.Errorf("upload failed after %d retries due to server errors, error %d", maxRetries, resp.StatusCode())
			}
			backoff := time.Duration(1<<retryCount) * time.Second
			utils.Log.Warnf("[Cloudreve-OneDrive] server errors %d while uploading, retrying after %v...", resp.StatusCode(), backoff)
			time.Sleep(backoff)
		case resp.StatusCode() != 201 && resp.StatusCode() != 202 && resp.StatusCode() != 200:
			return errors.New(resp.String())
		default:
			retryCount = 0
			finish += byteSize
			up(float64(finish) * 100 / float64(stream.GetSize()))
		}
	}
	// 上传成功发送回调请求
	return d.request(http.MethodPost, "/callback/onedrive/finish/"+u.SessionID, func(req *resty.Request) { req.SetBody("{}") }, nil)
}

func (d *Cloudreve) upS3(ctx context.Context, stream model.FileStreamer, u UploadInfo, up driver.UpdateProgress) error {

	var (
		finish     int64 = 0
		chunk      int   = 0
		etags      []string
		DEFAULT    = int64(u.ChunkSize)
		retryCount = 0
		maxRetries = 3
	)
	for finish < stream.GetSize() {
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}
		byteSize := min(stream.GetSize()-finish, DEFAULT)
		utils.Log.Debugf("[Cloudreve-S3] upload range: %d-%d/%d", finish, finish+byteSize-1, stream.GetSize())

		byteData := make([]byte, byteSize)
		_, err := io.ReadFull(stream, byteData)
		if err != nil {
			return err
		}

		resp, err := base.NoRedirectClient.R().
			SetContext(ctx).
			SetHeader("Content-Length", strconv.Itoa(int(byteSize))).
			SetBody(driver.NewLimitedUploadStream(ctx, bytes.NewReader(byteData))).
			Put(u.UploadURLs[chunk])
		if err != nil {
			return err
		}
		etag := resp.Header().Get("ETag")
		switch {
		case resp.StatusCode() != 200:
			retryCount++
			if retryCount > maxRetries {
				return errors.Errorf("upload failed after %d retries due to server errors, error %d", maxRetries, resp.StatusCode())
			}
			backoff := time.Duration(1<<retryCount) * time.Second
			utils.Log.Warnf("[Cloudreve-S3] server errors %d while uploading, retrying after %v...", resp.StatusCode(), backoff)
			time.Sleep(backoff)
		case etag == "":
			return errors.New("failed to get ETag from header")
		default:
			retryCount = 0
			etags = append(etags, etag)
			finish += byteSize
			up(float64(finish) * 100 / float64(stream.GetSize()))
			chunk++
		}
	}

	// s3LikeFinishUpload
	// https://github.com/cloudreve/frontend/blob/b485bf297974cbe4834d2e8e744ae7b7e5b2ad39/src/component/Uploader/core/api/index.ts#L204-L252
	bodyBuilder := &strings.Builder{}
	bodyBuilder.WriteString("<CompleteMultipartUpload>")
	for i, etag := range etags {
		bodyBuilder.WriteString(fmt.Sprintf(
			`<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>`,
			i+1, // PartNumber 从 1 开始
			etag,
		))
	}
	bodyBuilder.WriteString("</CompleteMultipartUpload>")
	resp, err := base.NoRedirectClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/xml").
		SetHeader("User-Agent", d.getUA()).
		SetBody(bodyBuilder.String()).
		Post(u.CompleteURL)
	if err != nil {
		return err
	}
	if resp.StatusCode() != 200 {
		return errors.Errorf("up status: %d, error: %s", resp.StatusCode(), resp.String())
	}
	// 上传成功发送回调请求
	err = d.request(http.MethodGet, "/callback/s3/"+u.SessionID, nil, nil)
	if err != nil {
		return err
	}
	return nil
}