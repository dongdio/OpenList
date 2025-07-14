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

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// _refreshToken 刷新令牌（内部方法）
// 通过在线API或本地方式刷新访问令牌和刷新令牌
func (d *AliyundriveOpen) _refreshToken() (string, string, error) {
	// 使用在线API刷新令牌
	if d.UseOnlineAPI && len(d.APIAddress) > 0 {
		u := d.APIAddress
		var resp struct {
			RefreshToken string `json:"refresh_token"` // 新的刷新令牌
			AccessToken  string `json:"access_token"`  // 新的访问令牌
			ErrorMessage string `json:"text"`          // 错误信息
		}
		// 根据AlipanType选项设置driver_txt
		driverTxt := "alicloud_qr"
		if d.AlipanType == "alipanTV" {
			driverTxt = "alicloud_tv"
		}
		// 发送请求到在线API
		_, err := base.RestyClient.R().
			SetResult(&resp).
			SetHeader("User-Agent", "Mozilla/5.0 (Macintosh; Apple macOS 15_5) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/138.0.0.0 Openlist/425.6.30").
			SetQueryParams(map[string]string{
				"refresh_ui": d.RefreshToken, // 当前的刷新令牌
				"server_use": "true",         // 服务器端使用标记
				"driver_txt": driverTxt,      // 驱动类型标记
			}).
			Get(u)
		if err != nil {
			return "", "", errors.Wrap(err, "发送在线API请求失败")
		}

		// 检查响应是否有效
		if resp.RefreshToken == "" || resp.AccessToken == "" {
			if resp.ErrorMessage != "" {
				return "", "", errors.Errorf("刷新令牌失败: %s", resp.ErrorMessage)
			}
			return "", "", errors.New("官方API返回的令牌为空，可能使用了错误的刷新令牌")
		}

		// 返回新的令牌
		return resp.RefreshToken, resp.AccessToken, nil
	}

	// 本地刷新逻辑（不使用在线API时）
	if d.ClientID == "" || d.ClientSecret == "" {
		return "", "", errors.New("ClientID或ClientSecret为空，无法刷新令牌")
	}

	// 构建请求URL和参数
	url := apiURL + "/oauth/access_token"
	var e ErrResp
	res, err := base.RestyClient.R().
		SetBody(base.Json{
			"client_id":     d.ClientID,
			"client_secret": d.ClientSecret,
			"grant_type":    "refresh_token",
			"refresh_token": d.RefreshToken,
		}).
		SetError(&e).
		Post(url)
	if err != nil {
		return "", "", errors.Wrap(err, "发送刷新令牌请求失败")
	}

	// 记录响应内容用于调试
	log.Debugf("[阿里云盘开放版] 刷新令牌响应: %s", res.String())

	// 检查是否有错误
	if e.Code != "" {
		return "", "", errors.Errorf("刷新令牌失败: %s", e.Message)
	}

	// 解析响应获取新令牌
	refresh := utils.GetBytes(res.Bytes(), "refresh_token").String()
	access := utils.GetBytes(res.Bytes(), "access_token").String()
	if refresh == "" {
		return "", "", errors.Errorf("刷新令牌失败: 返回的刷新令牌为空，响应: %s", res.String())
	}

	// 验证新旧令牌的sub字段是否一致，确保是同一用户
	curSub, err := getSub(d.RefreshToken)
	if err != nil {
		return "", "", errors.Wrap(err, "获取当前令牌的sub失败")
	}
	newSub, err := getSub(refresh)
	if err != nil {
		return "", "", errors.Wrap(err, "获取新令牌的sub失败")
	}
	if curSub != newSub {
		return "", "", errors.New("刷新令牌失败: sub不匹配，可能是不同用户的令牌")
	}

	return refresh, access, nil
}

// getSub 从JWT令牌中提取sub字段
// JWT令牌格式: header.payload.signature
func getSub(token string) (string, error) {
	// 分割JWT令牌
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return "", errors.New("不是有效的JWT令牌，段数不正确")
	}

	// 解码payload部分（Base64URL编码）
	bs, err := base64.RawStdEncoding.DecodeString(segments[1])
	if err != nil {
		return "", errors.Wrap(err, "解码JWT令牌失败")
	}

	// 从payload中提取sub字段
	sub := utils.GetBytes(bs, "sub").String()
	if sub == "" {
		return "", errors.New("JWT令牌中未找到sub字段")
	}

	return sub, nil
}

// refreshToken 刷新令牌
// 公开方法，处理令牌刷新并保存新令牌
func (d *AliyundriveOpen) refreshToken() error {
	// 如果有引用其他实例，使用引用实例的刷新方法
	if d.ref != nil {
		return d.ref.refreshToken()
	}

	// 尝试刷新令牌，最多重试3次
	var refresh, access string
	var err error
	refresh, access, err = d._refreshToken()

	// 如果失败，重试最多3次
	retryCount := 0
	maxRetries := 3
	for retryCount < maxRetries && err != nil {
		log.Errorf("[阿里云盘开放版] 刷新令牌失败(%d/%d): %s", retryCount+1, maxRetries, err)
		time.Sleep(time.Second * time.Duration(retryCount+1)) // 增加重试间隔
		refresh, access, err = d._refreshToken()
		retryCount++
	}

	if err != nil {
		return errors.Wrap(err, "多次尝试刷新令牌失败")
	}

	// 记录令牌变化并保存
	log.Infof("[阿里云盘开放版] 令牌已更新")
	d.RefreshToken, d.AccessToken = refresh, access
	op.MustSaveDriverStorage(d)
	return nil
}

// request 发送API请求
// 包装requestReturnErrResp方法，忽略错误响应对象
func (d *AliyundriveOpen) request(uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error) {
	b, err, _ := d.requestReturnErrResp(uri, method, callback, retry...)
	return b, err
}

// requestReturnErrResp 发送API请求并返回错误响应
// 处理授权、请求发送和错误处理
func (d *AliyundriveOpen) requestReturnErrResp(uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error, *ErrResp) {
	// 创建请求
	req := base.RestyClient.R()

	// 设置授权头
	accessToken := d.getAccessToken()
	if accessToken != "" {
		req.SetHeader("Authorization", "Bearer "+accessToken)
	}

	// 设置内容类型
	if method == http.MethodPost {
		req.SetHeader("Content-Type", "application/json")
	}

	// 应用回调函数
	if callback != nil {
		callback(req)
	}

	// 设置错误响应对象
	var e ErrResp
	req.SetError(&e)

	// 发送请求
	res, err := req.Execute(method, apiURL+uri)
	if err != nil {
		if res != nil {
			log.Errorf("[阿里云盘开放版] 请求错误: %s", res.String())
		}
		return nil, errors.Wrap(err, "发送请求失败"), nil
	}

	// 检查是否已经是重试请求
	isRetry := len(retry) > 0 && retry[0]

	// 处理错误响应
	if e.Code != "" {
		// 如果是令牌无效或过期，且不是重试请求，则刷新令牌后重试
		if !isRetry && (utils.SliceContains([]string{"AccessTokenInvalid", "AccessTokenExpired", "I400JD"}, e.Code) || accessToken == "") {
			log.Debugf("[阿里云盘开放版] 令牌已过期，正在刷新: %s", e.Message)
			err = d.refreshToken()
			if err != nil {
				return nil, errors.Wrap(err, "刷新令牌失败"), nil
			}
			// 使用新令牌重试请求
			return d.requestReturnErrResp(uri, method, callback, true)
		}
		return nil, errors.Errorf("%s: %s", e.Code, e.Message), &e
	}

	return res.Bytes(), nil, nil
}

// list 列出文件（内部方法，带限流）
// 获取指定目录下的文件列表
func (d *AliyundriveOpen) list(ctx context.Context, data base.Json) (*Files, error) {
	var resp Files
	_, err := d.request("/adrive/v1.0/openFile/list", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetResult(&resp)
	})
	if err != nil {
		return nil, errors.Wrap(err, "列出文件失败")
	}
	return &resp, nil
}

// getFiles 获取目录下的所有文件
// 处理分页获取所有文件
func (d *AliyundriveOpen) getFiles(ctx context.Context, fileID string) ([]File, error) {
	marker := "first" // 分页标记，首次请求使用特殊值
	result := make([]File, 0)

	// 循环获取所有分页
	for marker != "" {
		if marker == "first" {
			marker = "" // 首次请求将marker设为空
		}

		// 构建请求参数
		data := base.Json{
			"drive_id":        d.DriveID,
			"limit":           200, // 每页200条记录
			"marker":          marker,
			"order_by":        d.OrderBy,
			"order_direction": d.OrderDirection,
			"parent_file_id":  fileID,
			// 以下是可选参数，根据需要可以取消注释
			// "category":              "",
			// "type":                  "",
			// "video_thumbnail_time":  120000,
			// "video_thumbnail_width": 480,
			// "image_thumbnail_width": 480,
		}

		// 发送请求
		resp, err := d.limitList(ctx, data)
		if err != nil {
			return nil, errors.Wrap(err, "获取文件列表失败")
		}

		// 更新分页标记和结果
		marker = resp.NextMarker
		result = append(result, resp.Items...)

		// 检查是否有上下文取消
		if utils.IsCanceled(ctx) {
			return nil, ctx.Err()
		}
	}

	return result, nil
}

// getNowTime 获取当前时间和格式化字符串
// 返回当前时间对象和ISO8601格式的时间字符串
func getNowTime() (time.Time, string) {
	nowTime := time.Now()
	nowTimeStr := nowTime.Format("2006-01-02T15:04:05.000Z")
	return nowTime, nowTimeStr
}

// getAccessToken 获取访问令牌
// 如果有引用其他实例，使用引用实例的访问令牌
func (d *AliyundriveOpen) getAccessToken() string {
	if d.ref != nil {
		return d.ref.getAccessToken()
	}
	return d.AccessToken
}

// removeDuplicateFiles 删除目录中的重复文件
// 删除指定目录中与给定文件名相同但ID不同的文件
func (d *AliyundriveOpen) removeDuplicateFiles(ctx context.Context, parentPath string, fileName string, skipID string) error {
	// 处理空路径（根目录）情况
	if parentPath == "" {
		parentPath = "/"
	}

	// 列出目录中的所有文件
	files, err := op.List(ctx, d, parentPath, model.ListArgs{})
	if err != nil {
		return errors.Wrap(err, "列出目录内容失败")
	}

	// 查找所有同名文件
	var duplicates []model.Obj
	for _, file := range files {
		if file.GetName() == fileName && file.GetID() != skipID {
			duplicates = append(duplicates, file)
		}
	}

	// 删除所有重复文件，保留指定ID的文件
	for _, file := range duplicates {
		err = d.Remove(ctx, file)
		if err != nil {
			return errors.Wrapf(err, "删除重复文件 [%s] 失败", file.GetID())
		}
		log.Debugf("[阿里云盘开放版] 已删除重复文件: %s (ID: %s)", file.GetName(), file.GetID())
	}

	return nil
}