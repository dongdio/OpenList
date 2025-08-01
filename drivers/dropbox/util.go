package dropbox

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

func (d *Dropbox) refreshToken() error {
	if d.UserOnlineAPI && len(d.APIAddress) > 0 {
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
				"driver_txt": "dropboxs_go",
			}).
			Get(u)
		if err != nil {
			return err
		}
		if resp.RefreshToken == "" || resp.AccessToken == "" {
			if resp.ErrorMessage != "" {
				return errors.Errorf("failed to refresh token: %s", resp.ErrorMessage)
			}
			return errors.Errorf("empty token returned from official API, a wrong refresh token may have been used")
		}
		d.AccessToken = resp.AccessToken
		d.RefreshToken = resp.RefreshToken
		op.MustSaveDriverStorage(d)
		return nil
	}
	url := d.base + "/oauth2/token"
	// if utils.SliceContains([]string{"", DefaultClientID}, d.ClientID) {
	// 	url = d.APIAddress
	// }
	var tokenResp TokenResp
	resp, err := base.RestyClient.R().
		SetFormData(map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": d.RefreshToken,
			"client_id":     d.ClientID,
			"client_secret": d.ClientSecret,
		}).
		Post(url)
	if err != nil {
		return err
	}
	log.Debugf("[dropbox] refresh token response: %s", resp.String())
	if resp.StatusCode() != 200 {
		return errors.Errorf("failed to refresh token: %s", resp.String())
	}
	_ = utils.JSONTool.UnmarshalFromString(resp.String(), &tokenResp)
	d.AccessToken = tokenResp.AccessToken
	op.MustSaveDriverStorage(d)
	return nil

}

func (d *Dropbox) request(uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error) {
	req := base.RestyClient.R().
		SetAuthToken(d.AccessToken)
	if d.RootNamespaceId != "" {
		apiPathRootJson, err := utils.JSONTool.
			MarshalToString(map[string]any{
				".tag": "root",
				"root": d.RootNamespaceId,
			})
		if err != nil {
			return nil, err
		}
		req.SetHeader("Dropbox-API-Path-Root", apiPathRootJson)
	}
	if callback != nil {
		callback(req)
	}
	if method == http.MethodPost && req.Body != nil {
		req.SetHeader("Content-Type", "application/json")
	}
	var e ErrorResp
	req.SetError(&e)
	res, err := req.Execute(method, d.base+uri)
	if err != nil {
		return nil, err
	}
	log.Debugf("[dropbox] request (%s) response: %s", uri, res.String())
	isRetry := len(retry) > 0 && retry[0]
	if res.StatusCode() != 200 {
		body := res.String()
		if !isRetry && (utils.SliceMeet([]string{"expired_access_token", "invalid_access_token", "authorization"}, body,
			func(item string, v string) bool {
				return strings.Contains(v, item)
			}) || d.AccessToken == "") {

			err = d.refreshToken()
			if err != nil {
				return nil, err
			}
			return d.request(uri, method, callback, true)
		}
		return nil, errors.Errorf("%s:%s", e.Error, e.ErrorSummary)
	}
	return res.Bytes(), nil
}

func (d *Dropbox) list(ctx context.Context, data base.Json, isContinue bool) (*ListResp, error) {
	var resp ListResp
	uri := "/2/files/list_folder"
	if isContinue {
		uri += "/continue"
	}
	_, err := d.request(uri, http.MethodPost, func(req *resty.Request) {
		req.SetContext(ctx).SetBody(data).SetResult(&resp)
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (d *Dropbox) getFiles(ctx context.Context, path string) ([]File, error) {
	hasMore := true
	var marker string
	res := make([]File, 0)

	data := base.Json{
		"include_deleted":                     false,
		"include_has_explicit_shared_members": false,
		"include_mounted_folders":             false,
		"include_non_downloadable_files":      false,
		"limit":                               2000,
		"path":                                path,
		"recursive":                           false,
	}
	resp, err := d.list(ctx, data, false)
	if err != nil {
		return nil, err
	}
	marker = resp.Cursor
	hasMore = resp.HasMore
	res = append(res, resp.Entries...)

	for hasMore {
		data = base.Json{
			"cursor": marker,
		}
		resp, err = d.list(ctx, data, true)
		if err != nil {
			return nil, err
		}
		marker = resp.Cursor
		hasMore = resp.HasMore
		res = append(res, resp.Entries...)
	}
	return res, nil
}

func (d *Dropbox) finishUploadSession(ctx context.Context, toPath string, offset int64, sessionId string) error {
	url := d.contentBase + "/2/files/upload_session/finish"
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+d.AccessToken)

	uploadFinishArgs := UploadFinishArgs{
		Commit: struct {
			Autorename     bool   `json:"autorename"`
			Mode           string `json:"mode"`
			Mute           bool   `json:"mute"`
			Path           string `json:"path"`
			StrictConflict bool   `json:"strict_conflict"`
		}{
			Autorename:     true,
			Mode:           "add",
			Mute:           false,
			Path:           toPath,
			StrictConflict: false,
		},
		Cursor: UploadCursor{
			Offset:    offset,
			SessionID: sessionId,
		},
	}

	argsJson, err := utils.JSONTool.MarshalToString(uploadFinishArgs)
	if err != nil {
		return err
	}
	req.Header.Set("Dropbox-API-Arg", argsJson)

	res, err := base.HttpClient.Do(req)
	if err != nil {
		log.Errorf("failed to update file when finish session, err: %+v", err)
		return err
	}
	_ = res.Body.Close()
	return nil
}

func (d *Dropbox) startUploadSession(ctx context.Context) (string, error) {
	url := d.contentBase + "/2/files/upload_session/start"
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+d.AccessToken)
	req.Header.Set("Dropbox-API-Arg", "{\"close\":false}")

	res, err := base.HttpClient.Do(req)
	if err != nil {
		log.Errorf("failed to update file when start session, err: %+v", err)
		return "", err
	}

	body, err := io.ReadAll(res.Body)
	sessionId := utils.GetBytes(body, "session_id").String()

	_ = res.Body.Close()
	return sessionId, nil
}
