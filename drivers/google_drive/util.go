package google_drive

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// do others that not defined in Driver interface

type googleDriveServiceAccount struct {
	// Type                    string `json:"type"`
	// ProjectID               string `json:"project_id"`
	// PrivateKeyID            string `json:"private_key_id"`
	PrivateKey  string `json:"private_key"`
	ClientEMail string `json:"client_email"`
	// ClientID                string `json:"client_id"`
	// AuthURI                 string `json:"auth_uri"`
	TokenURI string `json:"token_uri"`
	// AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url"`
	// ClientX509CertURL       string `json:"client_x509_cert_url"`
}

func (d *GoogleDrive) refreshToken() error {
	// 使用在线API刷新Token，无需ClientID和ClientSecret
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
				"driver_txt": "googleui_go",
			}).
			Get(u)
		if err != nil {
			return err
		}
		if resp.RefreshToken == "" || resp.AccessToken == "" {
			if resp.ErrorMessage != "" {
				return errors.Errorf("empty token returned from official API: %s", resp.ErrorMessage)
			}
			return errors.Errorf("empty token returned from official API, a wrong refresh token may have been used")
		}
		d.AccessToken = resp.AccessToken
		d.RefreshToken = resp.RefreshToken
		op.MustSaveDriverStorage(d)
		return nil
	}
	// 使用本地客户端的情况下检查是否为空
	if d.ClientID == "" || d.ClientSecret == "" {
		return errors.Errorf("empty ClientID or ClientSecret")
	}
	// 走原有的刷新逻辑

	// googleDriveServiceAccountFile gdsaFile
	gdsaFile, gdsaFileErr := os.Stat(d.RefreshToken)
	if gdsaFileErr == nil {
		gdsaFileThis := d.RefreshToken
		if gdsaFile.IsDir() {
			if len(d.ServiceAccountFileList) <= 0 {
				gdsaReadDir, gdsaDirErr := os.ReadDir(d.RefreshToken)
				if gdsaDirErr != nil {
					log.Error("read dir fail")
					return gdsaDirErr
				}
				var gdsaFileList []string
				for _, fi := range gdsaReadDir {
					if !fi.IsDir() {
						match, _ := regexp.MatchString("^.*\\.json$", fi.Name())
						if !match {
							continue
						}
						gdsaDirText := d.RefreshToken
						if d.RefreshToken[len(d.RefreshToken)-1:] != "/" {
							gdsaDirText = d.RefreshToken + "/"
						}
						gdsaFileList = append(gdsaFileList, gdsaDirText+fi.Name())
					}
				}
				d.ServiceAccountFileList = gdsaFileList
				gdsaFileThis = d.ServiceAccountFileList[d.ServiceAccountFile]
				d.ServiceAccountFile++
			} else {
				if d.ServiceAccountFile < len(d.ServiceAccountFileList) {
					d.ServiceAccountFile++
				} else {
					d.ServiceAccountFile = 0
				}
				gdsaFileThis = d.ServiceAccountFileList[d.ServiceAccountFile]
			}
		}

		gdsaFileThisContent, err := os.ReadFile(gdsaFileThis)
		if err != nil {
			return err
		}

		// Now let's unmarshal the data into `payload`
		var jsonData googleDriveServiceAccount
		err = utils.JSONTool.Unmarshal(gdsaFileThisContent, &jsonData)
		if err != nil {
			return err
		}

		gdsaScope := "https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/drive.appdata https://www.googleapis.com/auth/drive.file https://www.googleapis.com/auth/drive.metadata https://www.googleapis.com/auth/drive.metadata.readonly https://www.googleapis.com/auth/drive.readonly https://www.googleapis.com/auth/drive.scripts"

		timeNow := time.Now()
		var timeStart int64 = timeNow.Unix()
		var timeEnd int64 = timeNow.Add(time.Minute * 60).Unix()

		// load private key from string
		privateKeyPem, _ := pem.Decode([]byte(jsonData.PrivateKey))
		privateKey, _ := x509.ParsePKCS8PrivateKey(privateKeyPem.Bytes)

		jwtToken := jwt.NewWithClaims(jwt.SigningMethodRS256,
			jwt.MapClaims{
				"iss":   jsonData.ClientEMail,
				"scope": gdsaScope,
				"aud":   jsonData.TokenURI,
				"exp":   timeEnd,
				"iat":   timeStart,
			})
		assertion, err := jwtToken.SignedString(privateKey)
		if err != nil {
			return err
		}

		var resp base.TokenResp
		var e TokenError
		res, err := base.RestyClient.R().SetResult(&resp).SetError(&e).
			SetFormData(map[string]string{
				"assertion":  assertion,
				"grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
			}).Post(jsonData.TokenURI)
		if err != nil {
			return err
		}
		log.Debug(res.String())
		if e.Error != "" {
			return errors.Errorf(e.Error)
		}
		d.AccessToken = resp.AccessToken
		return nil
	} else if os.IsExist(gdsaFileErr) {
		return gdsaFileErr
	}
	url := "https://www.googleapis.com/oauth2/v4/token"
	var resp base.TokenResp
	var e TokenError
	res, err := base.RestyClient.R().SetResult(&resp).SetError(&e).
		SetFormData(map[string]string{
			"client_id":     d.ClientID,
			"client_secret": d.ClientSecret,
			"refresh_token": d.RefreshToken,
			"grant_type":    "refresh_token",
		}).Post(url)
	if err != nil {
		return err
	}
	log.Debug(res.String())
	if e.Error != "" {
		return errors.Errorf(e.Error)
	}
	d.AccessToken = resp.AccessToken
	return nil
}

func (d *GoogleDrive) request(url string, method string, callback base.ReqCallback, resp any) ([]byte, error) {
	req := base.RestyClient.R()
	req.SetHeader("Authorization", "Bearer "+d.AccessToken)
	req.SetQueryParam("includeItemsFromAllDrives", "true")
	req.SetQueryParam("supportsAllDrives", "true")
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	var e Error
	req.SetError(&e)
	res, err := req.Execute(method, url)
	if err != nil {
		return nil, err
	}
	if e.Error.Code != 0 {
		if e.Error.Code == 401 {
			err = d.refreshToken()
			if err != nil {
				return nil, err
			}
			return d.request(url, method, callback, resp)
		}
		return nil, errors.Errorf("%s: %v", e.Error.Message, e.Error.Errors)
	}
	return res.Bytes(), nil
}

func (d *GoogleDrive) getFiles(id string) ([]File, error) {
	pageToken := "first"
	res := make([]File, 0)
	for pageToken != "" {
		if pageToken == "first" {
			pageToken = ""
		}
		var resp Files
		orderBy := "folder,name,modifiedTime desc"
		if d.OrderBy != "" {
			orderBy = d.OrderBy + " " + d.OrderDirection
		}
		query := map[string]string{
			"orderBy":  orderBy,
			"fields":   "files(id,name,mimeType,size,modifiedTime,createdTime,thumbnailLink,shortcutDetails,md5Checksum,sha1Checksum,sha256Checksum),nextPageToken",
			"pageSize": "1000",
			"q":        fmt.Sprintf("'%s' in parents and trashed = false", id),
			// "includeItemsFromAllDrives": "true",
			// "supportsAllDrives":         "true",
			"pageToken": pageToken,
		}
		_, err := d.request("https://www.googleapis.com/drive/v3/files", http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
		}, &resp)
		if err != nil {
			return nil, err
		}
		pageToken = resp.NextPageToken
		res = append(res, resp.Files...)
	}
	return res, nil
}

func (d *GoogleDrive) chunkUpload(ctx context.Context, stream model.FileStreamer, url string) error {
	var defaultChunkSize = d.ChunkSize * 1024 * 1024
	var offset int64 = 0
	for offset < stream.GetSize() {
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}
		chunkSize := stream.GetSize() - offset
		if chunkSize > defaultChunkSize {
			chunkSize = defaultChunkSize
		}
		reader, err := stream.RangeRead(http_range.Range{Start: offset, Length: chunkSize})
		if err != nil {
			return err
		}
		reader = driver.NewLimitedUploadStream(ctx, reader)
		_, err = d.request(url, http.MethodPut, func(req *resty.Request) {
			req.SetHeaders(map[string]string{
				"Content-Length": strconv.FormatInt(chunkSize, 10),
				"Content-Range":  fmt.Sprintf("bytes %d-%d/%d", offset, offset+chunkSize-1, stream.GetSize()),
			}).SetBody(reader).SetContext(ctx)
		}, nil)
		if err != nil {
			return err
		}
		offset += chunkSize
	}
	return nil
}
