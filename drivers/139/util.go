package _139

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

// isFamily 判断当前驱动类型是否为家庭云
// 返回 true 如果驱动类型是家庭云，否则返回 false
// 该方法用于区分不同的云盘类型，以便在请求中设置正确的服务类型
func (d *Yun139) isFamily() bool {
	return d.Type == "family"
}

// encodeURIComponent 实现 JavaScript 的 encodeURIComponent 行为
// 对输入字符串进行 URL 编码，并处理特定字符以匹配 JavaScript 的编码规则
// 参数 str: 需要编码的字符串
// 返回值: 编码后的字符串
func encodeURIComponent(str string) string {
	encoded := url.QueryEscape(str)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "%21", "!")
	encoded = strings.ReplaceAll(encoded, "%27", "'")
	encoded = strings.ReplaceAll(encoded, "%28", "(")
	encoded = strings.ReplaceAll(encoded, "%29", ")")
	encoded = strings.ReplaceAll(encoded, "%2A", "*")
	return encoded
}

// calSign 计算 API 请求签名
// 根据请求体、时间戳和随机字符串生成签名，用于 API 请求的身份验证
// 参数 body: 请求体字符串，ts: 时间戳字符串，randStr: 随机字符串
// 返回值: 计算得到的签名字符串（大写MD5值）
func calSign(body, ts, randStr string) string {
	bodyEncoded := encodeURIComponent(body)
	chars := strings.Split(bodyEncoded, "")
	sort.Strings(chars)
	sortedBody := strings.Join(chars, "")
	base64Body := base64.StdEncoding.EncodeToString([]byte(sortedBody))
	md5Body := utils.GetMD5EncodeStr(base64Body)
	combinedStr := md5Body + utils.GetMD5EncodeStr(ts+":"+randStr)
	finalSign := strings.ToUpper(utils.GetMD5EncodeStr(combinedStr))
	return finalSign
}

// getTime 解析时间字符串为 time.Time
// 将特定格式的时间字符串转换为 Go 的 time.Time 类型
// 参数 t: 时间字符串，格式为 "20060102150405"
// 返回值: 解析后的 time.Time 对象
func getTime(t string) time.Time {
	parsedTime, _ := time.ParseInLocation("20060102150405", t, utils.CNLoc)
	return parsedTime
}

// refreshToken 刷新授权 token，自动处理过期和存储
// 检查当前 token 是否即将过期或已过期，并请求刷新 token
// 如果 token 有效期大于15天，则无需刷新；如果已过期，则返回错误
// 返回值: 刷新成功返回 nil，否则返回错误
func (d *Yun139) refreshToken() error {
	if d.ref != nil {
		return d.ref.refreshToken()
	}
	decodedAuth, err := base64.StdEncoding.DecodeString(d.Authorization)
	if err != nil {
		return errors.Errorf("authorization decode failed: %s", err)
	}
	authStr := string(decodedAuth)
	authParts := strings.Split(authStr, ":")
	if len(authParts) < 3 {
		return errors.Errorf("authorization is invalid, splits < 3")
	}
	d.Account = authParts[1]
	tokenParts := strings.Split(authParts[2], "|")
	if len(tokenParts) < 4 {
		return errors.Errorf("authorization is invalid, strs < 4")
	}
	expirationTime, err := strconv.ParseInt(tokenParts[3], 10, 64)
	if err != nil {
		return errors.Errorf("authorization is invalid")
	}
	timeRemaining := expirationTime - time.Now().UnixMilli()
	if timeRemaining > 1000*60*60*24*15 {
		// Authorization有效期大于15天无需刷新
		return nil
	}
	if timeRemaining < 0 {
		return errors.Errorf("authorization has expired")
	}

	link := "https://aas.caiyun.feixin.10086.cn:443/tellin/authTokenRefresh.do"
	var resp RefreshTokenResp
	requestBody := "<root><token>" + authParts[2] + "</token><account>" + authParts[1] + "</account><clienttype>656</clienttype></root>"
	_, err = base.RestyClient.R().
		SetForceResponseContentType("application/xml").
		SetBody(requestBody).
		SetResult(&resp).
		Post(link)
	if err != nil {
		return err
	}
	if resp.Return != "0" {
		return errors.Errorf("failed to refresh token: %s", resp.Desc)
	}
	d.Authorization = base64.StdEncoding.EncodeToString([]byte(authParts[0] + ":" + authParts[1] + ":" + resp.Token))
	op.MustSaveDriverStorage(d)
	return nil
}

// request 统一的 API 请求封装，自动加签名和头部
// 对 139 云盘 API 进行请求，自动处理签名、头部设置和错误处理
// 参数 pathname: API 路径，method: HTTP 方法，callback: 请求回调函数，resp: 响应结构体指针
// 返回值: 响应字节数据和错误信息
func (d *Yun139) request(pathname string, method string, callback base.ReqCallback, resp any) ([]byte, error) {
	link := "https://yun.139.com" + pathname
	request := base.RestyClient.R()
	randomStr := random.String(16)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if callback != nil {
		callback(request)
	}
	bodyData, err := utils.Json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	signature := calSign(string(bodyData), timestamp, randomStr)
	serviceType := "1"
	if d.isFamily() {
		serviceType = "2"
	}
	request.SetHeaders(map[string]string{
		"Accept":                 "application/json, text/plain, */*",
		"CMS-DEVICE":             "default",
		"Authorization":          "Basic " + d.getAuthorization(),
		"mcloud-channel":         "1000101",
		"mcloud-client":          "10701",
		"mcloud-sign":            fmt.Sprintf("%s,%s,%s", timestamp, randomStr, signature),
		"mcloud-version":         "7.14.0",
		"Origin":                 "https://yun.139.com",
		"Referer":                "https://yun.139.com/w/",
		"x-DeviceInfo":           "||9|7.14.0|chrome|120.0.0.0|||windows 10||zh-CN|||",
		"x-huawei-channelSrc":    "10000034",
		"x-inner-ntwk":           "2",
		"x-m4c-caller":           "PC",
		"x-m4c-src":              "10002",
		"x-SvcType":              serviceType,
		"Inner-Hcy-Router-Https": "1",
	})

	var errorResp BaseResp
	request.SetResult(&errorResp)
	response, err := request.Execute(method, link)
	if err != nil {
		return nil, err
	}
	log.Debugln(response.String())
	if !errorResp.Success {
		return nil, errors.New(errorResp.Message)
	}
	if resp != nil {
		err = utils.Json.Unmarshal(response.Bytes(), resp)
		if err != nil {
			return nil, err
		}
	}
	return response.Bytes(), nil
}

func (d *Yun139) requestRoute(data any, resp any) ([]byte, error) {
	link := "https://user-njs.yun.139.com/user/route/qryRoutePolicy"
	req := base.RestyClient.R()
	randStr := random.String(16)
	ts := time.Now().Format("2006-01-02 15:04:05")
	callback := func(req *resty.Request) {
		req.SetBody(data)
	}
	if callback != nil {
		callback(req)
	}
	body, err := utils.Json.Marshal(req.Body)
	if err != nil {
		return nil, err
	}
	sign := calSign(string(body), ts, randStr)
	svcType := "1"
	if d.isFamily() {
		svcType = "2"
	}
	req.SetHeaders(map[string]string{
		"Accept":                 "application/json, text/plain, */*",
		"CMS-DEVICE":             "default",
		"Authorization":          "Basic " + d.getAuthorization(),
		"mcloud-channel":         "1000101",
		"mcloud-client":          "10701",
		"mcloud-sign":            fmt.Sprintf("%s,%s,%s", ts, randStr, sign),
		"mcloud-version":         "7.14.0",
		"Origin":                 "https://yun.139.com",
		"Referer":                "https://yun.139.com/w/",
		"x-DeviceInfo":           "||9|7.14.0|chrome|120.0.0.0|||windows 10||zh-CN|||",
		"x-huawei-channelSrc":    "10000034",
		"x-inner-ntwk":           "2",
		"x-m4c-caller":           "PC",
		"x-m4c-src":              "10002",
		"x-SvcType":              svcType,
		"Inner-Hcy-Router-Https": "1",
	})

	var e BaseResp
	req.SetResult(&e)
	res, err := req.Execute(http.MethodPost, link)
	if err != nil {
		return nil, err
	}
	log.Debugln(res.String())
	if !e.Success {
		return nil, errors.New(e.Message)
	}
	if resp != nil {
		err = utils.Json.Unmarshal(res.Bytes(), resp)
		if err != nil {
			return nil, err
		}
	}
	return res.Bytes(), nil
}

func (d *Yun139) post(pathname string, data any, resp any) ([]byte, error) {
	return d.request(pathname, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, resp)
}

func (d *Yun139) getFiles(catalogID string) ([]model.Obj, error) {
	start := 0
	limit := 100
	files := make([]model.Obj, 0)
	for {
		data := base.Json{
			"catalogID":       catalogID,
			"sortDirection":   1,
			"startNumber":     start + 1,
			"endNumber":       start + limit,
			"filterType":      0,
			"catalogSortType": 0,
			"contentSortType": 0,
			"commonAccountInfo": base.Json{
				"account":     d.getAccount(),
				"accountType": 1,
			},
		}
		var resp GetDiskResp
		_, err := d.post("/orchestration/personalCloud/catalog/v1.0/getDisk", data, &resp)
		if err != nil {
			return nil, err
		}
		for _, catalog := range resp.Data.GetDiskResult.CatalogList {
			f := model.Object{
				ID:       catalog.CatalogID,
				Name:     catalog.CatalogName,
				Size:     0,
				Modified: getTime(catalog.UpdateTime),
				Ctime:    getTime(catalog.CreateTime),
				IsFolder: true,
			}
			files = append(files, &f)
		}
		for _, content := range resp.Data.GetDiskResult.ContentList {
			f := model.ObjThumb{
				Object: model.Object{
					ID:       content.ContentID,
					Name:     content.ContentName,
					Size:     content.ContentSize,
					Modified: getTime(content.UpdateTime),
					HashInfo: utils.NewHashInfo(utils.MD5, content.Digest),
				},
				Thumbnail: model.Thumbnail{Thumbnail: content.ThumbnailURL},
				// Thumbnail: content.BigthumbnailURL,
			}
			files = append(files, &f)
		}
		if start+limit >= resp.Data.GetDiskResult.NodeCount {
			break
		}
		start += limit
	}
	return files, nil
}

func (d *Yun139) newJSON(data map[string]any) base.Json {
	common := map[string]any{
		"catalogType": 3,
		"cloudID":     d.CloudID,
		"cloudType":   1,
		"commonAccountInfo": base.Json{
			"account":     d.getAccount(),
			"accountType": 1,
		},
	}
	return utils.MergeMap(data, common)
}

func (d *Yun139) familyGetFiles(catalogID string) ([]model.Obj, error) {
	pageNum := 1
	files := make([]model.Obj, 0)
	for {
		data := d.newJSON(base.Json{
			"catalogID":       catalogID,
			"contentSortType": 0,
			"pageInfo": base.Json{
				"pageNum":  pageNum,
				"pageSize": 100,
			},
			"sortDirection": 1,
		})
		var resp QueryContentListResp
		_, err := d.post("/orchestration/familyCloud-rebuild/content/v1.2/queryContentList", data, &resp)
		if err != nil {
			return nil, err
		}
		tmpPath := resp.Data.Path
		for _, catalog := range resp.Data.CloudCatalogList {
			f := model.Object{
				ID:       catalog.CatalogID,
				Name:     catalog.CatalogName,
				Size:     0,
				IsFolder: true,
				Modified: getTime(catalog.LastUpdateTime),
				Ctime:    getTime(catalog.CreateTime),
				Path:     tmpPath, // 文件夹上一级的Path
			}
			files = append(files, &f)
		}
		for _, content := range resp.Data.CloudContentList {
			f := model.ObjThumb{
				Object: model.Object{
					ID:       content.ContentID,
					Name:     content.ContentName,
					Size:     content.ContentSize,
					Modified: getTime(content.LastUpdateTime),
					Ctime:    getTime(content.CreateTime),
					Path:     tmpPath, // 文件所在目录的Path
				},
				Thumbnail: model.Thumbnail{Thumbnail: content.ThumbnailURL},
				// Thumbnail: content.BigthumbnailURL,
			}
			files = append(files, &f)
		}
		if resp.Data.TotalCount == 0 {
			break
		}
		pageNum++
	}
	return files, nil
}

func (d *Yun139) groupGetFiles(catalogID string) ([]model.Obj, error) {
	pageNum := 1
	files := make([]model.Obj, 0)
	for {
		data := d.newJSON(base.Json{
			"groupID":         d.CloudID,
			"catalogID":       path.Base(catalogID),
			"contentSortType": 0,
			"sortDirection":   1,
			"startNumber":     pageNum,
			"endNumber":       pageNum + 99,
			"path":            path.Join(d.RootFolderID, catalogID),
		})

		var resp QueryGroupContentListResp
		_, err := d.post("/orchestration/group-rebuild/content/v1.0/queryGroupContentList", data, &resp)
		if err != nil {
			return nil, err
		}
		tmpPath := resp.Data.GetGroupContentResult.ParentCatalogID
		for _, catalog := range resp.Data.GetGroupContentResult.CatalogList {
			f := model.Object{
				ID:       catalog.CatalogID,
				Name:     catalog.CatalogName,
				Size:     0,
				IsFolder: true,
				Modified: getTime(catalog.UpdateTime),
				Ctime:    getTime(catalog.CreateTime),
				Path:     catalog.Path, // 文件夹的真实Path， root:/开头
			}
			files = append(files, &f)
		}
		for _, content := range resp.Data.GetGroupContentResult.ContentList {
			f := model.ObjThumb{
				Object: model.Object{
					ID:       content.ContentID,
					Name:     content.ContentName,
					Size:     content.ContentSize,
					Modified: getTime(content.UpdateTime),
					Ctime:    getTime(content.CreateTime),
					Path:     tmpPath, // 文件所在目录的Path
				},
				Thumbnail: model.Thumbnail{Thumbnail: content.ThumbnailURL},
				// Thumbnail: content.BigthumbnailURL,
			}
			files = append(files, &f)
		}
		if (pageNum + 99) > resp.Data.GetGroupContentResult.NodeCount {
			break
		}
		pageNum = pageNum + 100
	}
	return files, nil
}

func (d *Yun139) getLink(contentID string) (string, error) {
	data := base.Json{
		"appName":   "",
		"contentID": contentID,
		"commonAccountInfo": base.Json{
			"account":     d.getAccount(),
			"accountType": 1,
		},
	}
	res, err := d.post("/orchestration/personalCloud/uploadAndDownload/v1.0/downloadRequest",
		data, nil)
	if err != nil {
		return "", err
	}
	return utils.GetBytes(res, "data", "downloadURL").String(), nil
}
func (d *Yun139) familyGetLink(contentID string, path string) (string, error) {
	data := d.newJSON(base.Json{
		"contentID": contentID,
		"path":      path,
	})
	res, err := d.post("/orchestration/familyCloud-rebuild/content/v1.0/getFileDownLoadURL",
		data, nil)
	if err != nil {
		return "", err
	}
	return utils.GetBytes(res, "data", "downloadURL").String(), nil
}

func (d *Yun139) groupGetLink(contentID string, path string) (string, error) {
	data := d.newJSON(base.Json{
		"contentID": contentID,
		"groupID":   d.CloudID,
		"path":      path,
	})
	res, err := d.post("/orchestration/group-rebuild/groupManage/v1.0/getGroupFileDownLoadURL",
		data, nil)
	if err != nil {
		return "", err
	}
	return utils.GetBytes(res, "data", "downloadURL").String(), nil
}

func unicode(str string) string {
	textQuoted := strconv.QuoteToASCII(str)
	textUnquoted := textQuoted[1 : len(textQuoted)-1]
	return textUnquoted
}

func (d *Yun139) personalRequest(pathname string, method string, callback base.ReqCallback, resp any) ([]byte, error) {
	link := d.getPersonalCloudHost() + pathname
	req := base.RestyClient.R()
	randStr := random.String(16)
	ts := time.Now().Format(time.DateTime)
	if callback != nil {
		callback(req)
	}
	body, err := utils.Json.Marshal(req.Body)
	if err != nil {
		return nil, err
	}
	sign := calSign(string(body), ts, randStr)
	svcType := "1"
	if d.isFamily() {
		svcType = "2"
	}
	req.SetHeaders(map[string]string{
		"Accept":               "application/json, text/plain, */*",
		"Authorization":        "Basic " + d.getAuthorization(),
		"Caller":               "web",
		"Cms-Device":           "default",
		"Mcloud-Channel":       "1000101",
		"Mcloud-Client":        "10701",
		"Mcloud-Route":         "001",
		"Mcloud-Sign":          fmt.Sprintf("%s,%s,%s", ts, randStr, sign),
		"Mcloud-Version":       "7.14.0",
		"x-DeviceInfo":         "||9|7.14.0|chrome|120.0.0.0|||windows 10||zh-CN|||",
		"x-huawei-channelSrc":  "10000034",
		"x-inner-ntwk":         "2",
		"x-m4c-caller":         "PC",
		"x-m4c-src":            "10002",
		"x-SvcType":            svcType,
		"X-Yun-Api-Version":    "v1",
		"X-Yun-App-Channel":    "10000034",
		"X-Yun-Channel-Source": "10000034",
		"X-Yun-Client-Info":    "||9|7.14.0|chrome|120.0.0.0|||windows 10||zh-CN|||dW5kZWZpbmVk||",
		"X-Yun-Module-Type":    "100",
		"X-Yun-Svc-Type":       "1",
	})

	var e BaseResp
	req.SetResult(&e)
	res, err := req.Execute(method, link)
	if err != nil {
		return nil, err
	}
	log.Debugln(res.String())
	if !e.Success {
		return nil, errors.New(e.Message)
	}
	if resp != nil {
		err = utils.Json.Unmarshal(res.Bytes(), resp)
		if err != nil {
			return nil, err
		}
	}
	return res.Bytes(), nil
}
func (d *Yun139) personalPost(pathname string, data any, resp any) ([]byte, error) {
	return d.personalRequest(pathname, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, resp)
}

func getPersonalTime(t string) time.Time {
	stamp, err := time.ParseInLocation("2006-01-02T15:04:05.999-07:00", t, utils.CNLoc)
	if err != nil {
		panic(err)
	}
	return stamp
}

func (d *Yun139) personalGetFiles(fileId string) ([]model.Obj, error) {
	files := make([]model.Obj, 0)
	nextPageCursor := ""
	for {
		data := base.Json{
			"imageThumbnailStyleList": []string{"Small", "Large"},
			"orderBy":                 "updated_at",
			"orderDirection":          "DESC",
			"pageInfo": base.Json{
				"pageCursor": nextPageCursor,
				"pageSize":   100,
			},
			"parentFileId": fileId,
		}
		var resp PersonalListResp
		_, err := d.personalPost("/file/list", data, &resp)
		if err != nil {
			return nil, err
		}
		nextPageCursor = resp.Data.NextPageCursor
		for _, item := range resp.Data.Items {
			var isFolder = item.Type == "folder"
			var f model.Obj
			if isFolder {
				f = &model.Object{
					ID:       item.FileID,
					Name:     item.Name,
					Size:     0,
					Modified: getPersonalTime(item.UpdatedAt),
					Ctime:    getPersonalTime(item.CreatedAt),
					IsFolder: isFolder,
				}
			} else {
				var Thumbnails = item.Thumbnails
				var ThumbnailUrl string
				if d.UseLargeThumbnail {
					for _, thumb := range Thumbnails {
						if strings.Contains(thumb.Style, "Large") {
							ThumbnailUrl = thumb.URL
							break
						}
					}
				}
				if ThumbnailUrl == "" && len(Thumbnails) > 0 {
					ThumbnailUrl = Thumbnails[len(Thumbnails)-1].URL
				}
				f = &model.ObjThumb{
					Object: model.Object{
						ID:       item.FileID,
						Name:     item.Name,
						Size:     item.Size,
						Modified: getPersonalTime(item.UpdatedAt),
						Ctime:    getPersonalTime(item.CreatedAt),
						IsFolder: isFolder,
					},
					Thumbnail: model.Thumbnail{Thumbnail: ThumbnailUrl},
				}
			}
			files = append(files, f)
		}
		if len(nextPageCursor) == 0 {
			break
		}
	}
	return files, nil
}

func (d *Yun139) personalGetLink(fileID string) (string, error) {
	data := base.Json{
		"fileId": fileID,
	}
	res, err := d.personalPost("/file/getDownloadUrl",
		data, nil)
	if err != nil {
		return "", err
	}
	var cdnURL = utils.GetBytes(res, "data", "cdnUrl").String()
	if cdnURL != "" {
		return cdnURL, nil
	}
	return utils.GetBytes(res, "data", "url").String(), nil
}

func (d *Yun139) getAuthorization() string {
	if d.ref != nil {
		return d.ref.getAuthorization()
	}
	return d.Authorization
}

func (d *Yun139) getAccount() string {
	if d.ref != nil {
		return d.ref.getAccount()
	}
	return d.Account
}

func (d *Yun139) getPersonalCloudHost() string {
	if d.ref != nil {
		return d.ref.getPersonalCloudHost()
	}
	return d.PersonalCloudHost
}
