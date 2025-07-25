package pikpak

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/pkg/errors"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

var AndroidAlgorithms = []string{
	"SOP04dGzk0TNO7t7t9ekDbAmx+eq0OI1ovEx",
	"nVBjhYiND4hZ2NCGyV5beamIr7k6ifAsAbl",
	"Ddjpt5B/Cit6EDq2a6cXgxY9lkEIOw4yC1GDF28KrA",
	"VVCogcmSNIVvgV6U+AochorydiSymi68YVNGiz",
	"u5ujk5sM62gpJOsB/1Gu/zsfgfZO",
	"dXYIiBOAHZgzSruaQ2Nhrqc2im",
	"z5jUTBSIpBN9g4qSJGlidNAutX6",
	"KJE2oveZ34du/g1tiimm",
}

var WebAlgorithms = []string{
	"C9qPpZLN8ucRTaTiUMWYS9cQvWOE",
	"+r6CQVxjzJV6LCV",
	"F",
	"pFJRC",
	"9WXYIDGrwTCz2OiVlgZa90qpECPD6olt",
	"/750aCr4lm/Sly/c",
	"RB+DT/gZCrbV",
	"",
	"CyLsf7hdkIRxRm215hl",
	"7xHvLi2tOYP0Y92b",
	"ZGTXXxu8E/MIWaEDB+Sm/",
	"1UI3",
	"E7fP5Pfijd+7K+t6Tg/NhuLq0eEUVChpJSkrKxpO",
	"ihtqpG6FMt65+Xk+tWUH2",
	"NhXXU9rg4XXdzo7u5o",
}

var PCAlgorithms = []string{
	"KHBJ07an7ROXDoK7Db",
	"G6n399rSWkl7WcQmw5rpQInurc1DkLmLJqE",
	"JZD1A3M4x+jBFN62hkr7VDhkkZxb9g3rWqRZqFAAb",
	"fQnw/AmSlbbI91Ik15gpddGgyU7U",
	"/Dv9JdPYSj3sHiWjouR95NTQff",
	"yGx2zuTjbWENZqecNI+edrQgqmZKP",
	"ljrbSzdHLwbqcRn",
	"lSHAsqCkGDGxQqqwrVu",
	"TsWXI81fD1",
	"vk7hBjawK/rOSrSWajtbMk95nfgf3",
}

const (
	OSSUserAgent               = "aliyun-sdk-android/2.9.13(Linux/Android 14/M2004j7ac;UKQ1.231108.001)"
	OssSecurityTokenHeaderName = "X-OSS-Security-Token"
	ThreadsNum                 = 10
)

const (
	AndroidClientID      = "YNxT9w7GMdWvEOKa"
	AndroidClientSecret  = "dbw2OtmVEeuUvIptb1Coyg"
	AndroidClientVersion = "1.53.2"
	AndroidPackageName   = "com.pikcloud.pikpak"
	AndroidSdkVersion    = "2.0.6.206003"
	WebClientID          = "YUMx5nI8ZU8Ap8pm"
	WebClientSecret      = "dbw2OtmVEeuUvIptb1Coyg"
	WebClientVersion     = "2.0.0"
	WebPackageName       = "mypikpak.com"
	WebSdkVersion        = "8.0.3"
	PCClientID           = "YvtoWO6GNHiuCl7x"
	PCClientSecret       = "1NIH5R1IEe2pAxZE3hv3uA"
	PCClientVersion      = "undefined" // 2.6.11.4955
	PCPackageName        = "mypikpak.com"
	PCSdkVersion         = "8.0.3"
)

func (d *PikPak) login() error {
	// 检查用户名和密码是否为空
	if d.Addition.Username == "" || d.Addition.Password == "" {
		return errors.New("username or password is empty")
	}

	url := "https://user.mypikpak.net/v1/auth/signin"
	// 使用 用户填写的 CaptchaToken —————— (验证后的captcha_token)
	if d.GetCaptchaToken() == "" {
		if err := d.RefreshCaptchaTokenInLogin(GetAction(http.MethodPost, url), d.Username); err != nil {
			return err
		}
	}

	var e ErrResp
	res, err := base.RestyClient.SetRetryCount(1).R().SetError(&e).SetBody(base.Json{
		"captcha_token": d.GetCaptchaToken(),
		"client_id":     d.ClientID,
		"client_secret": d.ClientSecret,
		"username":      d.Username,
		"password":      d.Password,
	}).SetQueryParam("client_id", d.ClientID).Post(url)
	if err != nil {
		return err
	}
	if e.ErrorCode != 0 {
		return &e
	}
	data := res.Bytes()
	d.RefreshToken = utils.GetBytes(data, "refresh_token").String()
	d.AccessToken = utils.GetBytes(data, "access_token").String()
	d.Common.SetUserID(utils.GetBytes(data, "sub").String())
	return nil
}

func (d *PikPak) refreshToken(refreshToken string) error {
	url := "https://user.mypikpak.net/v1/auth/token"
	var e ErrResp
	res, err := base.RestyClient.SetRetryCount(1).R().SetError(&e).
		SetHeader("user-agent", "").SetBody(base.Json{
		"client_id":     d.ClientID,
		"client_secret": d.ClientSecret,
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	}).SetQueryParam("client_id", d.ClientID).Post(url)
	if err != nil {
		d.Status = err.Error()
		op.MustSaveDriverStorage(d)
		return err
	}
	if e.ErrorCode != 0 {
		if e.ErrorCode == 4126 {
			// 1. 未填写 username 或 password
			if d.Addition.Username == "" || d.Addition.Password == "" {
				return errors.New("refresh_token invalid, please re-provide refresh_token")
			} else {
				// refresh_token invalid, re-login
				return d.login()
			}
		}
		d.Status = e.Error()
		op.MustSaveDriverStorage(d)
		return errors.New(e.Error())
	}
	data := res.Bytes()
	d.Status = "work"
	d.RefreshToken = utils.GetBytes(data, "refresh_token").String()
	d.AccessToken = utils.GetBytes(data, "access_token").String()
	d.Common.SetUserID(utils.GetBytes(data, "sub").String())
	d.Addition.RefreshToken = d.RefreshToken
	op.MustSaveDriverStorage(d)
	return nil
}

func (d *PikPak) request(url string, method string, callback base.ReqCallback, resp any) ([]byte, error) {
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		// "Authorization":   "Bearer " + d.AccessToken,
		"User-Agent":      d.GetUserAgent(),
		"X-Device-ID":     d.GetDeviceID(),
		"X-Captcha-Token": d.GetCaptchaToken(),
	})
	if d.AccessToken != "" {
		req.SetHeader("Authorization", "Bearer "+d.AccessToken)
	}

	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	var e ErrResp
	req.SetError(&e)
	res, err := req.Execute(method, url)
	if err != nil {
		return nil, err
	}

	switch e.ErrorCode {
	case 0:
		return res.Bytes(), nil
	case 4122, 4121, 16:
		// access_token 过期
		if err1 := d.refreshToken(d.RefreshToken); err1 != nil {
			return nil, err1
		}
		return d.request(url, method, callback, resp)
	case 9: // 验证码token过期
		if err = d.RefreshCaptchaTokenAtLogin(GetAction(method, url), d.GetUserID()); err != nil {
			return nil, err
		}
		return d.request(url, method, callback, resp)
	case 10: // 操作频繁
		return nil, errors.New(e.ErrorDescription)
	default:
		return nil, errors.New(e.Error())
	}
}

func (d *PikPak) getFiles(id string) ([]File, error) {
	res := make([]File, 0)
	pageToken := "first"
	for pageToken != "" {
		if pageToken == "first" {
			pageToken = ""
		}
		query := map[string]string{
			"parent_id":      id,
			"thumbnail_size": "SIZE_LARGE",
			"with_audit":     "true",
			"limit":          "100",
			"filters":        `{"phase":{"eq":"PHASE_TYPE_COMPLETE"},"trashed":{"eq":false}}`,
			"page_token":     pageToken,
		}
		var resp Files
		_, err := d.request("https://api-drive.mypikpak.net/drive/v1/files", http.MethodGet, func(req *resty.Request) {
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

func GetAction(method string, url string) string {
	urlpath := regexp.MustCompile(`://[^/]+((/[^/\s?#]+)*)`).FindStringSubmatch(url)[1]
	return method + ":" + urlpath
}

type Common struct {
	client       *resty.Client
	CaptchaToken string
	UserID       string
	// 必要值,签名相关
	ClientID      string
	ClientSecret  string
	ClientVersion string
	PackageName   string
	Algorithms    []string
	DeviceID      string
	UserAgent     string
	// 验证码token刷新成功回调
	RefreshCTokenCk func(token string)
}

func generateDeviceSign(deviceID, packageName string) string {

	signatureBase := fmt.Sprintf("%s%s%s%s", deviceID, packageName, "1", "appkey")

	sha1Hash := sha1.New()
	sha1Hash.Write([]byte(signatureBase))
	sha1Result := sha1Hash.Sum(nil)

	sha1String := hex.EncodeToString(sha1Result)

	md5Hash := md5.New()
	md5Hash.Write([]byte(sha1String))
	md5Result := md5Hash.Sum(nil)

	md5String := hex.EncodeToString(md5Result)

	deviceSign := fmt.Sprintf("div101.%s%s", deviceID, md5String)

	return deviceSign
}

func BuildCustomUserAgent(deviceID, clientID, appName, sdkVersion, clientVersion, packageName, userID string) string {
	deviceSign := generateDeviceSign(deviceID, packageName)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("ANDROID-%s/%s ", appName, clientVersion))
	sb.WriteString("protocolVersion/200 ")
	sb.WriteString("accesstype/ ")
	sb.WriteString(fmt.Sprintf("clientid/%s ", clientID))
	sb.WriteString(fmt.Sprintf("clientversion/%s ", clientVersion))
	sb.WriteString("action_type/ ")
	sb.WriteString("networktype/WIFI ")
	sb.WriteString("sessionid/ ")
	sb.WriteString(fmt.Sprintf("deviceid/%s ", deviceID))
	sb.WriteString("providername/NONE ")
	sb.WriteString(fmt.Sprintf("devicesign/%s ", deviceSign))
	sb.WriteString("refresh_token/ ")
	sb.WriteString(fmt.Sprintf("sdkversion/%s ", sdkVersion))
	sb.WriteString(fmt.Sprintf("datetime/%d ", time.Now().UnixMilli()))
	sb.WriteString(fmt.Sprintf("usrno/%s ", userID))
	sb.WriteString(fmt.Sprintf("appname/android-%s ", appName))
	sb.WriteString("session_origin/ ")
	sb.WriteString("grant_type/ ")
	sb.WriteString("appid/ ")
	sb.WriteString("clientip/ ")
	sb.WriteString("devicename/Xiaomi_M2004j7ac ")
	sb.WriteString("osversion/13 ")
	sb.WriteString("platformversion/10 ")
	sb.WriteString("accessmode/ ")
	sb.WriteString("devicemodel/M2004J7AC ")

	return sb.String()
}

func (c *Common) SetDeviceID(deviceID string) {
	c.DeviceID = deviceID
}

func (c *Common) SetUserID(userID string) {
	c.UserID = userID
}

func (c *Common) SetUserAgent(userAgent string) {
	c.UserAgent = userAgent
}

func (c *Common) SetCaptchaToken(captchaToken string) {
	c.CaptchaToken = captchaToken
}
func (c *Common) GetCaptchaToken() string {
	return c.CaptchaToken
}

func (c *Common) GetUserAgent() string {
	return c.UserAgent
}

func (c *Common) GetDeviceID() string {
	return c.DeviceID
}

func (c *Common) GetUserID() string {
	return c.UserID
}

// RefreshCaptchaTokenAtLogin 刷新验证码token(登录后)
func (d *PikPak) RefreshCaptchaTokenAtLogin(action, userID string) error {
	metas := map[string]string{
		"client_version": d.ClientVersion,
		"package_name":   d.PackageName,
		"user_id":        userID,
	}
	metas["timestamp"], metas["captcha_sign"] = d.Common.GetCaptchaSign()
	return d.refreshCaptchaToken(action, metas)
}

// RefreshCaptchaTokenInLogin 刷新验证码token(登录时)
func (d *PikPak) RefreshCaptchaTokenInLogin(action, username string) error {
	metas := make(map[string]string)
	if ok, _ := regexp.MatchString(`\w+([-+.]\w+)*@\w+([-.]\w+)*\.\w+([-.]\w+)*`, username); ok {
		metas["email"] = username
	} else if len(username) >= 11 && len(username) <= 18 {
		metas["phone_number"] = username
	} else {
		metas["username"] = username
	}
	return d.refreshCaptchaToken(action, metas)
}

// GetCaptchaSign 获取验证码签名
func (c *Common) GetCaptchaSign() (timestamp, sign string) {
	timestamp = fmt.Sprint(time.Now().UnixMilli())
	str := fmt.Sprint(c.ClientID, c.ClientVersion, c.PackageName, c.DeviceID, timestamp)
	for _, algorithm := range c.Algorithms {
		str = utils.GetMD5EncodeStr(str + algorithm)
	}
	sign = "1." + str
	return
}

// refreshCaptchaToken 刷新CaptchaToken
func (d *PikPak) refreshCaptchaToken(action string, metas map[string]string) error {
	param := CaptchaTokenRequest{
		Action:       action,
		CaptchaToken: d.GetCaptchaToken(),
		ClientID:     d.ClientID,
		DeviceID:     d.GetDeviceID(),
		Meta:         metas,
		RedirectUri:  "xlaccsdk01://xbase.cloud/callback?state=harbor",
	}
	var e ErrResp
	var resp CaptchaTokenResponse
	_, err := d.request("https://user.mypikpak.net/v1/shield/captcha/init", http.MethodPost, func(req *resty.Request) {
		req.SetError(&e).SetBody(param).SetQueryParam("client_id", d.ClientID)
	}, &resp)

	if err != nil {
		return err
	}

	if e.IsError() {
		return errors.New(e.Error())
	}

	if resp.Url != "" {
		return errors.Errorf(`need verify: <a target="_blank" href="%s">Click Here</a>`, resp.Url)
	}

	if d.Common.RefreshCTokenCk != nil {
		d.Common.RefreshCTokenCk(resp.CaptchaToken)
	}
	d.Common.SetCaptchaToken(resp.CaptchaToken)
	return nil
}

func (d *PikPak) UploadByOSS(ctx context.Context, params *S3Params, s model.FileStreamer, up driver.UpdateProgress) error {
	ossClient, err := oss.New(params.Endpoint, params.AccessKeyID, params.AccessKeySecret)
	if err != nil {
		return err
	}
	bucket, err := ossClient.Bucket(params.Bucket)
	if err != nil {
		return err
	}

	err = bucket.PutObject(params.Key, driver.NewLimitedUploadStream(ctx, &driver.ReaderUpdatingProgress{
		Reader:         s,
		UpdateProgress: up,
	}), OssOption(params)...)
	if err != nil {
		return err
	}
	return nil
}

func (d *PikPak) UploadByMultipart(ctx context.Context, params *S3Params, fileSize int64, s model.FileStreamer, up driver.UpdateProgress) error {
	var (
		chunks    []oss.FileChunk
		parts     []oss.UploadPart
		imur      oss.InitiateMultipartUploadResult
		ossClient *oss.Client
		bucket    *oss.Bucket
		err       error
	)

	tmpF, err := s.CacheFullInTempFile()
	if err != nil {
		return err
	}

	if ossClient, err = oss.New(params.Endpoint, params.AccessKeyID, params.AccessKeySecret); err != nil {
		return err
	}

	if bucket, err = ossClient.Bucket(params.Bucket); err != nil {
		return err
	}

	ticker := time.NewTicker(time.Hour * 12)
	defer ticker.Stop()
	// 设置超时
	timeout := time.NewTimer(time.Hour * 24)

	if chunks, err = SplitFile(fileSize); err != nil {
		return err
	}

	if imur, err = bucket.InitiateMultipartUpload(params.Key,
		oss.SetHeader(OssSecurityTokenHeaderName, params.SecurityToken),
		oss.UserAgentHeader(OSSUserAgent),
	); err != nil {
		return err
	}

	wg := sync.WaitGroup{}
	wg.Add(len(chunks))

	chunksCh := make(chan oss.FileChunk)
	errCh := make(chan error)
	UploadedPartsCh := make(chan oss.UploadPart)
	quit := make(chan struct{})

	// producer
	go chunksProducer(chunksCh, chunks)
	go func() {
		wg.Wait()
		quit <- struct{}{}
	}()

	completedNum := atomic.Int32{}
	// consumers
	for i := 0; i < ThreadsNum; i++ {
		go func(threadId int) {
			defer func() {
				if r := recover(); r != nil {
					errCh <- errors.Errorf("recovered in %v", r)
				}
			}()
			for chunk := range chunksCh {
				var part oss.UploadPart // 出现错误就继续尝试，共尝试3次
				for range 3 {
					select {
					case <-ctx.Done():
						break
					case <-ticker.C:
						errCh <- errors.Wrap(err, "ossToken 过期")
					default:
					}

					buf := make([]byte, chunk.Size)
					if _, err = tmpF.ReadAt(buf, chunk.Offset); err != nil && !errors.Is(err, io.EOF) {
						continue
					}

					b := driver.NewLimitedUploadStream(ctx, bytes.NewReader(buf))
					if part, err = bucket.UploadPart(imur, b, chunk.Size, chunk.Number, OssOption(params)...); err == nil {
						break
					}
				}
				if err != nil {
					errCh <- errors.Wrap(err, fmt.Sprintf("上传 %s 的第%d个分片时出现错误：%v", s.GetName(), chunk.Number, err))
				} else {
					num := completedNum.Add(1)
					up(float64(num) * 100.0 / float64(len(chunks)))
				}
				UploadedPartsCh <- part
			}
		}(i)
	}

	go func() {
		for part := range UploadedPartsCh {
			parts = append(parts, part)
			wg.Done()
		}
	}()
LOOP:
	for {
		select {
		case <-ticker.C:
			// ossToken 过期
			return err
		case <-quit:
			break LOOP
		case <-errCh:
			return err
		case <-timeout.C:
			return errors.Errorf("time out")
		}
	}

	// EOF错误是xml的Unmarshal导致的，响应其实是json格式，所以实际上上传是成功的
	if _, err = bucket.CompleteMultipartUpload(imur, parts, OssOption(params)...); err != nil && !errors.Is(err, io.EOF) {
		// 当文件名含有 &< 这两个字符之一时响应的xml解析会出现错误，实际上上传是成功的
		if filename := filepath.Base(s.GetName()); !strings.ContainsAny(filename, "&<") {
			return err
		}
	}
	return nil
}

func chunksProducer(ch chan oss.FileChunk, chunks []oss.FileChunk) {
	for _, chunk := range chunks {
		ch <- chunk
	}
}

func SplitFile(fileSize int64) (chunks []oss.FileChunk, err error) {
	for i := int64(1); i < 10; i++ {
		if fileSize < i*utils.GB { // 文件大小小于iGB时分为i*100片
			if chunks, err = SplitFileByPartNum(fileSize, int(i*100)); err != nil {
				return
			}
			break
		}
	}
	if fileSize > 9*utils.GB { // 文件大小大于9GB时分为1000片
		if chunks, err = SplitFileByPartNum(fileSize, 1000); err != nil {
			return
		}
	}
	// 单个分片大小不能小于1MB
	if chunks[0].Size < 1*utils.MB {
		if chunks, err = SplitFileByPartSize(fileSize, 1*utils.MB); err != nil {
			return
		}
	}
	return
}

// SplitFileByPartNum splits big file into parts by the num of parts.
// Split the file with specified parts count, returns the split result when error is nil.
func SplitFileByPartNum(fileSize int64, chunkNum int) ([]oss.FileChunk, error) {
	if chunkNum <= 0 || chunkNum > 10000 {
		return nil, errors.New("chunkNum invalid")
	}

	if int64(chunkNum) > fileSize {
		return nil, errors.New("oss: chunkNum invalid")
	}

	var chunks []oss.FileChunk
	chunk := oss.FileChunk{}
	chunkN := (int64)(chunkNum)
	for i := range chunkN {
		chunk.Number = int(i + 1)
		chunk.Offset = i * (fileSize / chunkN)
		if i == chunkN-1 {
			chunk.Size = fileSize/chunkN + fileSize%chunkN
		} else {
			chunk.Size = fileSize / chunkN
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// SplitFileByPartSize splits big file into parts by the size of parts.
// Splits the file by the part size. Returns the FileChunk when error is nil.
func SplitFileByPartSize(fileSize int64, chunkSize int64) ([]oss.FileChunk, error) {
	if chunkSize <= 0 {
		return nil, errors.New("chunkSize invalid")
	}

	chunkN := fileSize / chunkSize
	if chunkN >= 10000 {
		return nil, errors.New("Too many parts, please increase part size")
	}

	var chunks []oss.FileChunk
	chunk := oss.FileChunk{}
	for i := int64(0); i < chunkN; i++ {
		chunk.Number = int(i + 1)
		chunk.Offset = i * chunkSize
		chunk.Size = chunkSize
		chunks = append(chunks, chunk)
	}

	if fileSize%chunkSize > 0 {
		chunk.Number = len(chunks) + 1
		chunk.Offset = int64(len(chunks)) * chunkSize
		chunk.Size = fileSize % chunkSize
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// OssOption get options
func OssOption(params *S3Params) []oss.Option {
	options := []oss.Option{
		oss.SetHeader(OssSecurityTokenHeaderName, params.SecurityToken),
		oss.UserAgentHeader(OSSUserAgent),
	}
	return options
}