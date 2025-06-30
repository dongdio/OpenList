package aliyundrive_open

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/drivers/base"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/utility/utils"
)

// do others that not defined in Driver interface

func (d *AliyundriveOpen) _refreshToken() (string, string, error) {
	if d.UseOnlineAPI && len(d.APIAddress) > 0 {
		u := d.APIAddress
		var resp struct {
			RefreshToken string `json:"refresh_token"`
			AccessToken  string `json:"access_token"`
			ErrorMessage string `json:"text"`
		}
		_, err := base.RestyClient.R().
			SetResult(&resp).
			SetHeader("User-Agent", "Mozilla/5.0 (Macintosh; Apple macOS 15_5) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/138.0.0.0 Openlist/425.6.30").
			SetQueryParams(map[string]string{
				"refresh_ui": d.RefreshToken,
				"server_use": "true",
				"driver_txt": "alicloud_qr",
			}).
			Get(u)
		if err != nil {
			return "", "", err
		}
		if resp.RefreshToken == "" || resp.AccessToken == "" {
			if resp.ErrorMessage != "" {
				return "", "", errors.Errorf("failed to refresh token: %s", resp.ErrorMessage)
			}
			return "", "", errors.Errorf("empty token returned from official API")
		}
		return resp.AccessToken, resp.RefreshToken, nil
	}
	// 本地刷新逻辑必须要求client_id和client_secret
	if d.ClientID == "" || d.ClientSecret == "" {
		return "", "", errors.Errorf("empty ClientID or ClientSecret")
	}
	url := API_URL + "/oauth/access_token"
	var e ErrResp
	res, err := base.RestyClient.R().
		SetBody(base.Json{
			"client_id":     d.ClientID,
			"client_secret": d.ClientSecret,
			"grant_type":    "refresh_token",
			"refresh_token": d.RefreshToken,
		}).
		// SetResult(&resp).
		SetError(&e).
		Post(url)
	if err != nil {
		return "", "", err
	}
	log.Debugf("[ali_open] refresh token response: %s", res.String())
	if e.Code != "" {
		return "", "", errors.Errorf("failed to refresh token: %s", e.Message)
	}
	refresh, access := utils.GetBytes(res.Bytes(), "refresh_token").String(), utils.GetBytes(res.Bytes(), "access_token").String()
	if refresh == "" {
		return "", "", errors.Errorf("failed to refresh token: refresh token is empty, resp: %s", res.String())
	}
	curSub, err := getSub(d.RefreshToken)
	if err != nil {
		return "", "", err
	}
	newSub, err := getSub(refresh)
	if err != nil {
		return "", "", err
	}
	if curSub != newSub {
		return "", "", errors.New("failed to refresh token: sub not match")
	}
	return refresh, access, nil
}

func getSub(token string) (string, error) {
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return "", errors.New("not a jwt token because of invalid segments")
	}
	bs, err := base64.RawStdEncoding.DecodeString(segments[1])
	if err != nil {
		return "", errors.New("failed to decode jwt token")
	}
	return utils.GetBytes(bs, "sub").String(), nil
}

func (d *AliyundriveOpen) refreshToken() error {
	if d.ref != nil {
		return d.ref.refreshToken()
	}
	refresh, access, err := d._refreshToken()
	for range 3 {
		if err == nil {
			break
		} else {
			log.Errorf("[ali_open] failed to refresh token: %s", err)
		}
		refresh, access, err = d._refreshToken()
	}
	if err != nil {
		return err
	}
	log.Infof("[ali_open] token exchange: %s -> %s", d.RefreshToken, refresh)
	d.RefreshToken, d.AccessToken = refresh, access
	op.MustSaveDriverStorage(d)
	return nil
}

func (d *AliyundriveOpen) request(uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error) {
	b, err, _ := d.requestReturnErrResp(uri, method, callback, retry...)
	return b, err
}

func (d *AliyundriveOpen) requestReturnErrResp(uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error, *ErrResp) {
	req := base.RestyClient.R()
	// TODO check whether access_token is expired
	req.SetHeader("Authorization", "Bearer "+d.getAccessToken())
	if method == http.MethodPost {
		req.SetHeader("Content-Type", "application/json")
	}
	if callback != nil {
		callback(req)
	}
	var e ErrResp
	req.SetError(&e)
	res, err := req.Execute(method, API_URL+uri)
	if err != nil {
		if res != nil {
			log.Errorf("[aliyundrive_open] request error: %s", res.String())
		}
		return nil, err, nil
	}
	isRetry := len(retry) > 0 && retry[0]
	if e.Code != "" {
		if !isRetry && (utils.SliceContains([]string{"AccessTokenInvalid", "AccessTokenExpired", "I400JD"}, e.Code) || d.getAccessToken() == "") {
			err = d.refreshToken()
			if err != nil {
				return nil, err, nil
			}
			return d.requestReturnErrResp(uri, method, callback, true)
		}
		return nil, errors.Errorf("%s:%s", e.Code, e.Message), &e
	}
	return res.Bytes(), nil, nil
}

func (d *AliyundriveOpen) list(ctx context.Context, data base.Json) (*Files, error) {
	var resp Files
	_, err := d.request("/adrive/v1.0/openFile/list", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetResult(&resp)
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (d *AliyundriveOpen) getFiles(ctx context.Context, fileId string) ([]File, error) {
	marker := "first"
	res := make([]File, 0)
	for marker != "" {
		if marker == "first" {
			marker = ""
		}
		data := base.Json{
			"drive_id":        d.DriveId,
			"limit":           200,
			"marker":          marker,
			"order_by":        d.OrderBy,
			"order_direction": d.OrderDirection,
			"parent_file_id":  fileId,
			// "category":              "",
			// "type":                  "",
			// "video_thumbnail_time":  120000,
			// "video_thumbnail_width": 480,
			// "image_thumbnail_width": 480,
		}
		resp, err := d.limitList(ctx, data)
		if err != nil {
			return nil, err
		}
		marker = resp.NextMarker
		res = append(res, resp.Items...)
	}
	return res, nil
}

func getNowTime() (time.Time, string) {
	nowTime := time.Now()
	nowTimeStr := nowTime.Format("2006-01-02T15:04:05.000Z")
	return nowTime, nowTimeStr
}

func (d *AliyundriveOpen) getAccessToken() string {
	if d.ref != nil {
		return d.ref.getAccessToken()
	}
	return d.AccessToken
}

// Remove duplicate files with the same name in the given directory path,
// preserving the file with the given skipID if provided
func (d *AliyundriveOpen) removeDuplicateFiles(ctx context.Context, parentPath string, fileName string, skipID string) error {
	// Handle empty path (root directory) case
	if parentPath == "" {
		parentPath = "/"
	}

	// List all files in the parent directory
	files, err := op.List(ctx, d, parentPath, model.ListArgs{})
	if err != nil {
		return err
	}

	// Find all files with the same name
	var duplicates []model.Obj
	for _, file := range files {
		if file.GetName() == fileName && file.GetID() != skipID {
			duplicates = append(duplicates, file)
		}
	}

	// Remove all duplicates files, except the file with the given ID
	for _, file := range duplicates {
		err := d.Remove(ctx, file)
		if err != nil {
			return err
		}
	}

	return nil
}