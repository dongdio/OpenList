package _189

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
	myrand "github.com/dongdio/OpenList/v4/utility/utils/random"
)

// 注：旧版登录方法已注释，保留作为参考
// func (d *Cloud189) login() error {
//	url := "https://cloud.189.cn/api/portal/loginUrl.action?redirectURL=https%3A%2F%2Fcloud.189.cn%2Fmain.action"
//	b := ""
//	lt := ""
//	ltText := regexp.MustCompile(`lt = "(.+?)"`)
//	var res *resty.Response
//	var err error
//	for i := 0; i < 3; i++ {
//		res, err = d.client.R().Get(url)
//		if err != nil {
//			return err
//		}
//		// 已经登陆
//		if res.RawResponse.Request.URL.String() == "https://cloud.189.cn/web/main" {
//			return nil
//		}
//		b = res.String()
//		ltTextArr := ltText.FindStringSubmatch(b)
//		if len(ltTextArr) > 0 {
//			lt = ltTextArr[1]
//			break
//		} else {
//			<-time.After(time.Second)
//		}
//	}
//	if lt == "" {
//		return errs.Errorf("get page: %s \nstatus: %d \nrequest url: %s\nredirect url: %s",
//			b, res.StatusCode(), res.RawResponse.Request.URL.String(), res.Header().Get("location"))
//	}
//	captchaToken := regexp.MustCompile(`captchaToken' value='(.+?)'`).FindStringSubmatch(b)[1]
//	returnUrl := regexp.MustCompile(`returnUrl = '(.+?)'`).FindStringSubmatch(b)[1]
//	paramId := regexp.MustCompile(`paramId = "(.+?)"`).FindStringSubmatch(b)[1]
//	//reqId := regexp.MustCompile(`reqId = "(.+?)"`).FindStringSubmatch(b)[1]
//	jRsakey := regexp.MustCompile(`j_rsaKey" value="(\S+)"`).FindStringSubmatch(b)[1]
//	vCodeID := regexp.MustCompile(`picCaptcha\.do\?token\=([A-Za-z0-9\&\=]+)`).FindStringSubmatch(b)[1]
//	vCodeRS := ""
//	if vCodeID != "" {
//		// need ValidateCode
//		log.Debugf("try to identify verification codes")
//		timeStamp := strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
//		u := "https://open.e.189.cn/api/logbox/oauth2/picCaptcha.do?token=" + vCodeID + timeStamp
//		imgRes, err := d.client.R().SetHeaders(map[string]string{
//			"User-Agent":     "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:74.0) Gecko/20100101 Firefox/76.0",
//			"Referer":        "https://open.e.189.cn/api/logbox/oauth2/unifyAccountLogin.do",
//			"Sec-Fetch-Dest": "image",
//			"Sec-Fetch-Mode": "no-cors",
//			"Sec-Fetch-Site": "same-origin",
//		}).Get(u)
//		if err != nil {
//			return err
//		}
//		// Enter the verification code manually
//		//err = message.GetMessenger().WaitSend(message.Message{
//		//	Type:    "image",
//		//	Content: "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgres.Bytes()),
//		//}, 10)
//		//if err != nil {
//		//	return err
//		//}
//		//vCodeRS, err = message.GetMessenger().WaitReceive(30)
//		// use ocr api
//		vRes, err := base.RestyClient.R().SetMultipartField(
//			"image", "validateCode.png", "image/png", bytes.NewReader(imgres.Bytes())).
//			Post(setting.GetStr(conf.OcrApi))
//		if err != nil {
//			return err
//		}
//		if utils.GetBytes(vres.Bytes(), "status").Int() != 200 {
//			return errs.New("ocr error:" + utils.GetBytes(vres.Bytes(), "msg").String())
//		}
//		vCodeRS = utils.GetBytes(vres.Bytes(), "result").String()
//		log.Debugln("code: ", vCodeRS)
//	}
//	userRsa := RsaEncode([]byte(d.Username), jRsakey, true)
//	passwordRsa := RsaEncode([]byte(d.Password), jRsakey, true)
//	url = "https://open.e.189.cn/api/logbox/oauth2/loginSubmit.do"
//	var loginResp LoginResp
//	res, err = d.client.R().
//		SetHeaders(map[string]string{
//			"lt":         lt,
//			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36",
//			"Referer":    "https://open.e.189.cn/",
//			"accept":     "application/json;charset=UTF-8",
//		}).SetFormData(map[string]string{
//		"appKey":       "cloud",
//		"accountType":  "01",
//		"userName":     "{RSA}" + userRsa,
//		"password":     "{RSA}" + passwordRsa,
//		"validateCode": vCodeRS,
//		"captchaToken": captchaToken,
//		"returnUrl":    returnUrl,
//		"mailSuffix":   "@pan.cn",
//		"paramId":      paramId,
//		"clientType":   "10010",
//		"dynamicCheck": "FALSE",
//		"cb_SaveName":  "1",
//		"isOauth2":     "false",
//	}).Post(url)
//	if err != nil {
//		return err
//	}
//	err = utils.Json.Unmarshal(res.Bytes(), &loginResp)
//	if err != nil {
//		log.Error(err.Error())
//		return err
//	}
//	if loginResp.Result != 0 {
//		return errs.Errorf(loginResp.Msg)
//	}
//	_, err = d.client.R().Get(loginResp.ToUrl)
//	return err
// }

// request 发送HTTP请求并处理响应
// 参数:
//   - url: 请求URL
//   - method: HTTP方法
//   - callback: 请求回调函数，用于设置请求参数
//   - resp: 响应结构体指针，用于解析响应JSON
//
// 返回:
//   - []byte: 响应体字节数组
//   - error: 错误信息
func (d *Cloud189) request(url string, method string, callback base.ReqCallback, resp any) ([]byte, error) {
	var errResp Error

	// 创建请求
	req := base.RestyClient.R().
		SetError(&errResp).
		SetHeaders(d.header).
		SetHeader("Accept", "application/json;charset=UTF-8").
		SetQueryParams(map[string]string{
			"noCache": random(), // 添加随机参数避免缓存
		})

	// 应用回调函数
	if callback != nil {
		callback(req)
	}

	// 设置响应结构体
	if resp != nil {
		req.SetResult(resp)
	}

	// 执行请求
	res, err := req.Execute(method, url)
	if err != nil {
		return nil, errs.Wrap(err, "执行HTTP请求失败")
	}

	// 检查API错误
	if errResp.ErrorCode != "" {
		if errResp.ErrorCode == "InvalidSessionKey" {
			// 会话过期，重新登录
			log.Debug("会话密钥无效，尝试重新登录")
			err = d.newLogin()
			if err != nil {
				return nil, errs.Wrap(err, "重新登录失败")
			}
			// 重新发送请求
			return d.request(url, method, callback, resp)
		}
		return nil, errs.Errorf("API错误: [%s] %s", errResp.ErrorCode, errResp.ErrorMsg)
	}

	// 检查响应代码
	resCode := utils.GetBytes(res.Bytes(), "res_code").Int()
	if resCode != 0 {
		resMessage := utils.GetBytes(res.Bytes(), "res_message").String()
		err = errs.Errorf("响应错误: [%d] %s", resCode, resMessage)
	}

	return res.Bytes(), err
}

// getFiles 获取指定文件夹下的文件列表
// 参数:
//   - fileId: 文件夹ID
//
// 返回:
//   - []model.Obj: 文件对象列表
//   - error: 错误信息
func (d *Cloud189) getFiles(fileID string) ([]model.Obj, error) {
	result := make([]model.Obj, 0)
	pageNum := 1

	// 分页获取文件列表
	for {
		var resp Files
		_, err := d.request(_listFiles, http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(map[string]string{
				"pageSize":   "60", // 每页60条记录
				"pageNum":    strconv.Itoa(pageNum),
				"mediaType":  "0",          // 所有类型
				"folderId":   fileID,       // 文件夹ID
				"iconOption": "5",          // 图标选项
				"orderBy":    "lastOpTime", // 按最后操作时间排序
				"descending": "true",       // 降序排列
			})
		}, &resp)

		if err != nil {
			return nil, errs.Wrap(err, "获取文件列表失败")
		}

		// 没有更多数据，退出循环
		if resp.FileListAO.Count == 0 {
			break
		}

		// 处理文件夹
		for _, folder := range resp.FileListAO.FolderList {
			lastOpTime := utils.MustParseCNTime(folder.LastOpTime)
			result = append(result, &model.Object{
				ID:       strconv.FormatInt(folder.ID, 10),
				Name:     folder.Name,
				Modified: lastOpTime,
				IsFolder: true,
			})
		}

		// 处理文件
		for _, file := range resp.FileListAO.FileList {
			lastOpTime := utils.MustParseCNTime(file.LastOpTime)
			result = append(result, &model.ObjThumb{
				Object: model.Object{
					ID:       strconv.FormatInt(file.ID, 10),
					Name:     file.Name,
					Modified: lastOpTime,
					Size:     file.Size,
				},
				Thumbnail: model.Thumbnail{Thumbnail: file.Icon.SmallURL},
			})
		}

		// 下一页
		pageNum++
	}

	return result, nil
}

// getSessionKey 获取会话密钥
// 返回:
//   - string: 会话密钥
//   - error: 错误信息
func (d *Cloud189) getSessionKey() (string, error) {
	resp, err := d.request(_getUserBriefInfo, http.MethodGet, nil, nil)
	if err != nil {
		return "", errs.Wrap(err, "获取用户信息失败")
	}

	sessionKey := utils.GetBytes(resp, "sessionKey").String()
	if sessionKey == "" {
		return "", errs.New("未找到会话密钥")
	}

	return sessionKey, nil
}

// getResKey 获取RSA加密密钥
// 返回:
//   - string: 公钥
//   - string: 公钥ID
//   - error: 错误信息
func (d *Cloud189) getResKey() (string, string, error) {
	now := time.Now().UnixMilli()

	// 如果已有有效的RSA密钥，直接返回
	if d.rsa.Expire > now {
		return d.rsa.PubKey, d.rsa.PkID, nil
	}

	// 获取新的RSA密钥
	resp, err := d.request(_generateRsaKey, http.MethodGet, nil, nil)
	if err != nil {
		return "", "", errs.Wrap(err, "获取RSA密钥失败")
	}

	// 解析响应
	pubKey := utils.GetBytes(resp, "pubKey").String()
	pkID := utils.GetBytes(resp, "pkId").String()
	expire := utils.GetBytes(resp, "expire").Int()

	// 更新缓存
	d.rsa.PubKey = pubKey
	d.rsa.PkID = pkID
	d.rsa.Expire = expire

	return pubKey, pkID, nil
}

// uploadRequest 发送上传相关的请求
// 参数:
//   - uri: 请求URI
//   - form: 表单数据
//   - resp: 响应结构体指针
//
// 返回:
//   - []byte: 响应体字节数组
//   - error: 错误信息
func (d *Cloud189) uploadRequest(uri string, form map[string]string, resp any) ([]byte, error) {
	// 生成请求参数
	currentTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
	requestID := Random("xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx")
	secretKey := Random("xxxxxxxxxxxx4xxxyxxxxxxxxxxxxxxx")
	secretKey = secretKey[0 : 16+int(16*myrand.Rand.Float32())]

	// 加密表单数据
	formQueryString := qs(form)
	encryptedData := AesEncrypt([]byte(formQueryString), []byte(secretKey[0:16]))
	hexData := hex.EncodeToString(encryptedData)

	// 计算签名
	signature := hmacSha1(fmt.Sprintf("SessionKey=%s&Operate=GET&RequestURI=%s&Date=%s&params=%s",
		d.sessionKey, uri, currentTime, hexData), secretKey)

	// 获取RSA密钥
	pubKey, pkID, err := d.getResKey()
	if err != nil {
		return nil, err
	}

	// 加密密钥
	encryptedKey := RsaEncode([]byte(secretKey), pubKey, false)

	// 创建请求
	req := base.RestyClient.R().
		SetQueryParam("params", hexData).
		SetHeaders(d.header).
		SetHeaders(map[string]string{
			"accept":         "application/json;charset=UTF-8",
			"SessionKey":     d.sessionKey,
			"Signature":      signature,
			"X-Request-Date": currentTime,
			"X-Request-ID":   requestID,
			"EncryptionText": encryptedKey,
			"PkId":           pkID,
		})

	// 设置响应结构体
	if resp != nil {
		req.SetResult(resp)
	}
	u := "https://upload.cloud.189.cn" + uri

	// 执行请求
	res, err := req.Get(u)
	if err != nil {
		return nil, errs.Wrap(err, "执行上传请求失败")
	}

	// 检查响应
	responseData := res.Bytes()
	if utils.GetBytes(responseData, "code").String() != "SUCCESS" {
		return nil, errs.Errorf("上传请求失败: %s - %s",
			uri, utils.GetBytes(responseData, "msg").String())
	}

	return responseData, nil
}

const _sliceFileSize int64 = 10 * 1024 * 1024 // 定义分片大小（10MB）

// newUpload 实现分片上传
// 参数:
//   - ctx: 上下文
//   - dstDir: 目标目录对象
//   - file: 文件流
//   - up: 进度更新回调
//
// 返回:
//   - error: 错误信息
func (d *Cloud189) newUpload(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) error {
	// 获取会话密钥
	sessionKey, err := d.getSessionKey()
	if err != nil {
		return err
	}
	d.sessionKey = sessionKey

	// 计算分片数量
	totalSlices := int64(math.Ceil(float64(file.GetSize()) / float64(_sliceFileSize)))

	// 初始化上传
	res, err := d.uploadRequest("/person/initMultiUpload", map[string]string{
		"parentFolderId": dstDir.GetID(),
		"fileName":       encode(file.GetName()),
		"fileSize":       strconv.FormatInt(file.GetSize(), 10),
		"sliceSize":      strconv.FormatInt(_sliceFileSize, 10),
		"lazyCheck":      "1", // 懒校验模式
	}, nil)

	if err != nil {
		return errs.Wrap(err, "初始化上传失败")
	}

	// 获取上传文件ID
	uploadFileID := utils.GetBytes(res, "data", "uploadFileId").String()
	if uploadFileID == "" {
		return errs.New("获取上传文件ID失败")
	}

	// 可以获取已上传的分片信息（目前未使用）
	// _, err = d.uploadRequest("/person/getUploadedPartsInfo", map[string]string{
	//	"uploadFileId": uploadFileId,
	// }, nil)

	// 上传分片
	var uploadedBytes int64 = 0
	var numBytes int
	md5List := make([]string, 0, totalSlices)
	md5Sum := md5.New() // 计算整个文件的MD5

	for i := int64(1); i <= totalSlices; i++ {
		// 检查是否取消
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}

		// 计算当前分片大小
		sliceSize := min(file.GetSize()-uploadedBytes, _sliceFileSize)

		// 读取分片数据
		sliceData := make([]byte, sliceSize)
		numBytes, err = io.ReadFull(file, sliceData)
		if err != nil {
			return errs.Wrap(err, "读取文件分片失败")
		}

		uploadedBytes += int64(numBytes)

		// 计算分片MD5
		sliceMD5Bytes := getMd5(sliceData)
		sliceMD5Hex := hex.EncodeToString(sliceMD5Bytes)
		sliceMD5Base64 := base64.StdEncoding.EncodeToString(sliceMD5Bytes)

		// 添加到MD5列表并更新整体MD5
		md5List = append(md5List, strings.ToUpper(sliceMD5Hex))
		md5Sum.Write(sliceData)

		// 获取分片上传URL
		var uploadUrlsResp UploadUrlsResp
		_, err = d.uploadRequest("/person/getMultiUploadUrls", map[string]string{
			"partInfo":     fmt.Sprintf("%s-%s", strconv.FormatInt(i, 10), sliceMD5Base64),
			"uploadFileId": uploadFileID,
		}, &uploadUrlsResp)

		if err != nil {
			return errs.Wrap(err, "获取分片上传URL失败")
		}

		// 获取上传数据
		uploadData := uploadUrlsResp.UploadUrls["partNumber_"+strconv.FormatInt(i, 10)]
		log.Debugf("分片上传数据: %+v", uploadData)

		requestURL := uploadData.RequestURL
		uploadHeaders := strings.Split(decodeURIComponent(uploadData.RequestHeader), "&")

		// 创建上传请求
		cli := base.RestyClient.R().
			SetContext(ctx).
			SetBody(driver.NewLimitedUploadStream(ctx, bytes.NewReader(sliceData)))
		for _, headerItem := range uploadHeaders {
			j := strings.Index(headerItem, "=")
			if j > 0 {
				cli.SetHeader(headerItem[0:j], headerItem[j+1:])
			}
		}
		resp, err := cli.Put(requestURL)
		if err != nil {
			return errs.Wrap(err, "执行分片上传请求失败")
		}
		// 检查响应状态
		if resp.StatusCode() != http.StatusOK {
			_ = resp.Body.Close()
			return errs.Errorf("分片上传失败，状态码: %d", resp.StatusCode())
		}

		log.Debugf("189 分片上传响应: %+v\n", resp.String())
		_ = resp.Body.Close()

		// 更新进度
		up(float64(i) * 100 / float64(totalSlices))
	}

	// 计算文件MD5和分片MD5
	fileMD5 := hex.EncodeToString(md5Sum.Sum(nil))
	sliceMD5 := fileMD5

	// 如果文件大于一个分片，计算分片MD5列表的MD5
	if file.GetSize() > _sliceFileSize {
		sliceMD5 = utils.GetMD5EncodeStr(strings.Join(md5List, "\n"))
	}

	// 提交上传
	_, err = d.uploadRequest("/person/commitMultiUploadFile", map[string]string{
		"uploadFileId": uploadFileID,
		"fileMd5":      fileMD5,
		"sliceMd5":     sliceMD5,
		"lazyCheck":    "1",
		"opertype":     "3", // 操作类型：上传完成
	}, nil)
	return errs.Wrap(err, "提交上传失败")
}