package _189pc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errgroup"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"

	"github.com/avast/retry-go"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"resty.dev/v3"
)

// SignatureHeader 生成请求签名头，用于189云盘API的身份验证。
// url: 请求的目标URL
// method: HTTP请求方法（如GET、POST）
// params: 请求参数的加密字符串
// isFamily: 是否为家庭账户请求
func (y *Cloud189PC) SignatureHeader(url, method, params string, isFamily bool) map[string]string {
	dateOfGmt := getHttpDateStr()
	sessionKey := y.getTokenInfo().SessionKey
	sessionSecret := y.getTokenInfo().SessionSecret
	if isFamily {
		sessionKey = y.getTokenInfo().FamilySessionKey
		sessionSecret = y.getTokenInfo().FamilySessionSecret
	}

	header := map[string]string{
		"Date":         dateOfGmt,
		"SessionKey":   sessionKey,
		"X-Request-ID": uuid.NewString(),
		"Signature":    signatureOfHmac(sessionSecret, sessionKey, method, url, dateOfGmt, params),
	}
	return header
}

// EncryptParams 加密请求参数，使用AES-ECB算法。
// params: 待加密的参数
// isFamily: 是否为家庭账户请求，决定使用哪个密钥
func (y *Cloud189PC) EncryptParams(params Params, isFamily bool) string {
	sessionSecret := y.getTokenInfo().SessionSecret
	if isFamily {
		sessionSecret = y.getTokenInfo().FamilySessionSecret
	}
	if params != nil {
		return AesECBEncrypt(params.Encode(), sessionSecret[:16])
	}
	return ""
}

// request 发送HTTP请求到189云盘API，支持自动处理会话失效重试。
// url: 请求的目标URL
// method: HTTP请求方法
// callback: 请求回调，用于设置额外参数或头
// params: 请求参数，将被加密后附加到URL
// resp: 响应结构体，用于解析返回数据
// isFamily: 是否为家庭账户请求
func (y *Cloud189PC) request(url, method string, callback base.ReqCallback, params Params, resp any, isFamily ...bool) ([]byte, error) {
	req := y.getClient().R().SetQueryParams(clientSuffix())

	// 设置加密后的参数
	encryptedParams := y.EncryptParams(params, isBool(isFamily...))
	if encryptedParams != "" {
		req.SetQueryParam("params", encryptedParams)
	}

	// 设置签名头
	req.SetHeaders(y.SignatureHeader(url, method, encryptedParams, isBool(isFamily...)))

	// 设置错误响应结构体
	var errResp RespErr
	req.SetError(&errResp)

	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	res, err := req.Execute(method, url)
	if err != nil {
		return nil, errors.Wrap(err, "执行HTTP请求失败")
	}

	// 检查会话失效并重试
	if strings.Contains(res.String(), "userSessionBO is null") {
		if err = y.refreshSession(); err != nil {
			return nil, errors.Wrap(err, "刷新会话失败")
		}
		return y.request(url, method, callback, params, resp, isFamily...)
	}

	// 检查无效会话密钥并重试
	if strings.Contains(res.String(), "InvalidSessionKey") {
		if err = y.refreshSession(); err != nil {
			return nil, errors.Wrap(err, "刷新会话失败")
		}
		return y.request(url, method, callback, params, resp, isFamily...)
	}

	// 处理API返回的错误
	if errResp.HasError() {
		return nil, &errResp
	}
	return res.Bytes(), nil
}

// get 发送GET请求到189云盘API。
// url: 请求的目标URL
// callback: 请求回调，用于设置额外参数或头
// resp: 响应结构体，用于解析返回数据
// isFamily: 是否为家庭账户请求
func (y *Cloud189PC) get(url string, callback base.ReqCallback, resp any, isFamily ...bool) ([]byte, error) {
	return y.request(url, http.MethodGet, callback, nil, resp, isFamily...)
}

// post 发送POST请求到189云盘API。
// url: 请求的目标URL
// callback: 请求回调，用于设置额外参数或头
// resp: 响应结构体，用于解析返回数据
// isFamily: 是否为家庭账户请求
func (y *Cloud189PC) post(url string, callback base.ReqCallback, resp any, isFamily ...bool) ([]byte, error) {
	return y.request(url, http.MethodPost, callback, nil, resp, isFamily...)
}

// put 发送PUT请求到189云盘API，通常用于文件上传。
// ctx: 请求上下文
// url: 请求的目标URL
// headers: 额外的请求头
// sign: 是否需要签名
// file: 文件数据读取器
// isFamily: 是否为家庭账户请求
func (y *Cloud189PC) put(ctx context.Context, url string, headers map[string]string, sign bool, file io.Reader, isFamily bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, file)
	if err != nil {
		return nil, errors.Wrap(err, "创建PUT请求失败")
	}

	query := req.URL.Query()
	for key, value := range clientSuffix() {
		query.Add(key, value)
	}
	req.URL.RawQuery = query.Encode()

	for key, value := range headers {
		req.Header.Add(key, value)
	}

	if sign {
		for key, value := range y.SignatureHeader(url, http.MethodPut, "", isFamily) {
			req.Header.Add(key, value)
		}
	}

	resp, err := base.HttpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "执行PUT请求失败")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "读取PUT响应失败")
	}

	var errResp RespErr
	_ = utils.JSONTool.Unmarshal(body, &errResp)
	_ = xml.Unmarshal(body, &errResp)
	if errResp.HasError() {
		return nil, &errResp
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("PUT请求失败，错误信息：%s", string(body))
	}
	return body, nil
}

// getFiles 获取指定文件夹下的所有文件和子文件夹列表。
// ctx: 请求上下文
// fileId: 文件夹ID
// isFamily: 是否为家庭账户请求
func (y *Cloud189PC) getFiles(ctx context.Context, fileID string, isFamily bool) ([]model.Obj, error) {
	res := make([]model.Obj, 0, 100)
	for pageNum := 1; ; pageNum++ {
		resp, err := y.getFilesWithPage(ctx, fileID, isFamily, pageNum, 1000, y.OrderBy, y.OrderDirection)
		if err != nil {
			return nil, errors.Wrap(err, "获取文件列表失败")
		}
		// 获取完毕跳出
		if resp.FileListAO.Count == 0 {
			break
		}

		for i := 0; i < len(resp.FileListAO.FolderList); i++ {
			res = append(res, &resp.FileListAO.FolderList[i])
		}
		for i := 0; i < len(resp.FileListAO.FileList); i++ {
			res = append(res, &resp.FileListAO.FileList[i])
		}
	}
	return res, nil
}

// getFilesWithPage 获取指定文件夹下某一页的文件和子文件夹列表。
// ctx: 请求上下文
// fileId: 文件夹ID
// isFamily: 是否为家庭账户请求
// pageNum: 页码
// pageSize: 每页记录数
// orderBy: 排序字段
// orderDirection: 排序方向
func (y *Cloud189PC) getFilesWithPage(ctx context.Context, fileID string, isFamily bool, pageNum int, pageSize int, orderBy string, orderDirection string) (*Cloud189FilesResp, error) {
	fullURL := _apiURL
	if isFamily {
		fullURL += "/family/file"
	}
	fullURL += "/listFiles.action"

	var resp Cloud189FilesResp
	_, err := y.get(fullURL, func(r *resty.Request) {
		r.SetContext(ctx)
		r.SetQueryParams(map[string]string{
			"folderId":   fileID,
			"fileType":   "0",
			"mediaAttr":  "0",
			"iconOption": "5",
			"pageNum":    fmt.Sprint(pageNum),
			"pageSize":   fmt.Sprint(pageSize),
		})
		if isFamily {
			r.SetQueryParams(map[string]string{
				"familyId":   y.FamilyID,
				"orderBy":    toFamilyOrderBy(orderBy),
				"descending": toDesc(orderDirection),
			})
		} else {
			r.SetQueryParams(map[string]string{
				"recursive":  "0",
				"orderBy":    orderBy,
				"descending": toDesc(orderDirection),
			})
		}
	}, &resp, isFamily)
	if err != nil {
		return nil, errors.Wrap(err, "获取分页文件列表失败")
	}
	return &resp, nil
}

// findFileByName 在指定文件夹中按名称查找文件。
// ctx: 请求上下文
// searchName: 搜索的文件名
// folderId: 文件夹ID
// isFamily: 是否为家庭账户请求
func (y *Cloud189PC) findFileByName(ctx context.Context, searchName string, folderID string, isFamily bool) (*Cloud189File, error) {
	for pageNum := 1; ; pageNum++ {
		resp, err := y.getFilesWithPage(ctx, folderID, isFamily, pageNum, 10, "filename", "asc")
		if err != nil {
			return nil, err
		}
		// 获取完毕跳出
		if resp.FileListAO.Count == 0 {
			return nil, errs.ObjectNotFound
		}
		for i := 0; i < len(resp.FileListAO.FileList); i++ {
			file := resp.FileListAO.FileList[i]
			if file.Name == searchName {
				return &file, nil
			}
		}
	}
}

func (y *Cloud189PC) login() (err error) {
	// 初始化登陆所需参数
	if y.loginParam == nil {
		if err = y.initLoginParam(); err != nil {
			// 验证码也通过错误返回
			return err
		}
	}
	defer func() {
		// 销毁验证码
		y.ValidateCode = ""
		// 销毁登陆参数
		y.loginParam = nil
		// 遇到错误，重新加载登陆参数(刷新验证码)
		if err != nil && y.NoUseOcr {
			if err1 := y.initLoginParam(); err1 != nil {
				err = errors.Errorf("err1: %s \nerr2: %s", err, err1)
			}
		}
	}()

	param := y.loginParam
	var loginresp LoginResp
	_, err = y.client.R().
		SetForceResponseContentType("application/json;charset=UTF-8").
		SetResult(&loginresp).
		SetHeaders(map[string]string{
			"REQID": param.ReqID,
			"lt":    param.Lt,
		}).
		SetFormData(map[string]string{
			"appKey":       _appID,
			"accountType":  _accountType,
			"userName":     param.RsaUsername,
			"password":     param.RsaPassword,
			"validateCode": y.ValidateCode,
			"captchaToken": param.CaptchaToken,
			"returnUrl":    _returnURL,
			// "mailSuffix":   "@189.cn",
			"dynamicCheck": "FALSE",
			"clientType":   _clientType,
			"cb_SaveName":  "1",
			"isOauth2":     "false",
			"state":        "",
			"paramId":      param.ParamID,
		}).
		Post(_authURL + "/api/logbox/oauth2/loginSubmit.do")
	if err != nil {
		return err
	}
	if loginresp.ToUrl == "" {
		return errors.Errorf("login failed,No toUrl obtained, msg: %s", loginresp.Msg)
	}

	// 获取Session
	var erron RespErr
	var tokenInfo AppSessionResp
	_, err = y.client.R().
		SetResult(&tokenInfo).SetError(&erron).
		SetQueryParams(clientSuffix()).
		SetQueryParam("redirectURL", loginresp.ToUrl).
		Post(_apiURL + "/getSessionForPC.action")
	if err != nil {
		return
	}

	if erron.HasError() {
		return &erron
	}
	if tokenInfo.ResCode != 0 {
		err = errors.New(tokenInfo.ResMessage)
		return
	}
	y.tokenInfo = &tokenInfo
	return
}

/* 初始化登陆需要的参数
*  如果遇到验证码返回错误
 */
func (y *Cloud189PC) initLoginParam() error {
	// 清除cookie
	jar, _ := cookiejar.New(nil)
	y.client.SetCookieJar(jar)

	res, err := y.client.R().
		SetQueryParams(map[string]string{
			"appId":      _appID,
			"clientType": _clientType,
			"returnURL":  _returnURL,
			"timeStamp":  fmt.Sprint(timestamp()),
		}).
		Get(_webURL + "/api/portal/unifyLoginForPC.action")
	if err != nil {
		return err
	}

	param := LoginParam{
		CaptchaToken: regexp.MustCompile(`'captchaToken' value='(.+?)'`).FindStringSubmatch(res.String())[1],
		Lt:           regexp.MustCompile(`lt = "(.+?)"`).FindStringSubmatch(res.String())[1],
		ParamID:      regexp.MustCompile(`paramId = "(.+?)"`).FindStringSubmatch(res.String())[1],
		ReqID:        regexp.MustCompile(`reqId = "(.+?)"`).FindStringSubmatch(res.String())[1],
		// jRsaKey:      regexp.MustCompile(`"j_rsaKey" value="(.+?)"`).FindStringSubmatch(res.String())[1],
	}

	// 获取rsa公钥
	var encryptConf EncryptConfResp
	_, err = y.client.R().
		SetForceResponseContentType("application/json;charset=UTF-8").
		SetResult(&encryptConf).
		SetFormData(map[string]string{"appId": _appID}).
		Post(_authURL + "/api/logbox/config/encryptConf.do")
	if err != nil {
		return err
	}

	param.JRsaKey = fmt.Sprintf("-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----", encryptConf.Data.PubKey)
	param.RsaUsername = encryptConf.Data.Pre + RsaEncrypt(param.JRsaKey, y.Username)
	param.RsaPassword = encryptConf.Data.Pre + RsaEncrypt(param.JRsaKey, y.Password)
	y.loginParam = &param

	// 判断是否需要验证码
	resp, err := y.client.R().
		SetHeader("REQID", param.ReqID).
		SetFormData(map[string]string{
			"appKey":      _appID,
			"accountType": _accountType,
			"userName":    param.RsaUsername,
		}).Post(_authURL + "/api/logbox/oauth2/needcaptcha.do")
	if err != nil {
		return err
	}
	if resp.String() == "0" {
		return nil
	}

	// 拉取验证码
	imgRes, err := y.client.R().
		SetQueryParams(map[string]string{
			"token": param.CaptchaToken,
			"REQID": param.ReqID,
			"rnd":   fmt.Sprint(timestamp()),
		}).
		Get(_authURL + "/api/logbox/oauth2/picCaptcha.do")
	if err != nil {
		return errors.Errorf("failed to obtain verification code")
	}
	if imgRes.Size() > 20 {
		if setting.GetStr(consts.OcrApi) != "" && !y.NoUseOcr {
			vRes, err := base.RestyClient.R().
				SetMultipartField("image", "validateCode.png", "image/png", bytes.NewReader(imgRes.Bytes())).
				Post(setting.GetStr(consts.OcrApi))
			if err != nil {
				return err
			}
			if utils.GetBytes(vRes.Bytes(), "status").Int() == 200 {
				y.ValidateCode = utils.GetBytes(vRes.Bytes(), "result").String()
				return nil
			}
		}

		// 返回验证码图片给前端
		return errors.Errorf(`need img validate code: <img src="data:image/png;base64,%s"/>`, base64.StdEncoding.EncodeToString(imgRes.Bytes()))
	}
	return nil
}

// 刷新会话
func (y *Cloud189PC) refreshSession() (err error) {
	if y.ref != nil {
		return y.ref.refreshSession()
	}
	var erron RespErr
	var userSessionResp UserSessionResp
	_, err = y.client.R().
		SetResult(&userSessionResp).SetError(&erron).
		SetQueryParams(clientSuffix()).
		SetQueryParams(map[string]string{
			"appId":       _appID,
			"accessToken": y.tokenInfo.AccessToken,
		}).
		SetHeader("X-Request-ID", uuid.NewString()).
		Get(_apiURL + "/getSessionForPC.action")
	if err != nil {
		return err
	}

	// 错误影响正常访问，下线该储存
	defer func() {
		if err != nil {
			y.GetStorage().SetStatus(fmt.Sprintf("%+v", err.Error()))
			op.MustSaveDriverStorage(y)
		}
	}()

	if erron.HasError() {
		if erron.ResponseCode == "UserInvalidOpenToken" {
			if err = y.login(); err != nil {
				return err
			}
		}
		return &erron
	}
	y.tokenInfo.UserSessionResp = userSessionResp
	return
}

// StreamUpload 普通上传
// 无法上传大小为0的文件
func (y *Cloud189PC) StreamUpload(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress, isFamily bool, overwrite bool) (model.Obj, error) {
	size := file.GetSize()
	sliceSize := partSize(size)

	params := Params{
		"parentFolderId": dstDir.GetID(),
		"fileName":       url.QueryEscape(file.GetName()),
		"fileSize":       fmt.Sprint(file.GetSize()),
		"sliceSize":      fmt.Sprint(sliceSize),
		"lazyCheck":      "1",
	}

	fullURL := _uploadURL
	if isFamily {
		params.Set("familyId", y.FamilyID)
		fullURL += "/family"
	} else {
		// params.Set("extend", `{"opScene":"1","relativepath":"","rootfolderid":""}`)
		fullURL += "/person"
	}

	// 初始化上传
	var initMultiUpload InitMultiUploadResp
	_, err := y.request(fullURL+"/initMultiUpload", http.MethodGet, func(req *resty.Request) {
		req.SetContext(ctx)
	}, params, &initMultiUpload, isFamily)
	if err != nil {
		return nil, err
	}

	threadG, upCtx := errgroup.NewGroupWithContext(ctx, y.uploadThread,
		retry.Attempts(3),
		retry.Delay(time.Second),
		retry.DelayType(retry.BackOffDelay))

	count := int(size / sliceSize)
	lastPartSize := size % sliceSize
	if lastPartSize > 0 {
		count++
	} else {
		lastPartSize = sliceSize
	}
	fileMd5 := utils.MD5.NewFunc()
	silceMd5 := utils.MD5.NewFunc()
	silceMd5Hexs := make([]string, 0, count)
	teeReader := io.TeeReader(file, io.MultiWriter(fileMd5, silceMd5))
	byteSize := sliceSize
	for i := 1; i <= count; i++ {
		if utils.IsCanceled(upCtx) {
			break
		}
		if i == count {
			byteSize = lastPartSize
		}
		byteData := make([]byte, byteSize)
		// 读取块
		silceMd5.Reset()
		if _, err = io.ReadFull(teeReader, byteData); err != io.EOF && err != nil {
			return nil, err
		}

		// 计算块md5并进行hex和base64编码
		md5Bytes := silceMd5.Sum(nil)
		silceMd5Hexs = append(silceMd5Hexs, strings.ToUpper(hex.EncodeToString(md5Bytes)))
		partInfo := fmt.Sprintf("%d-%s", i, base64.StdEncoding.EncodeToString(md5Bytes))

		threadG.Go(func(ctx context.Context) error {
			uploadUrls, err := y.GetMultiUploadUrls(ctx, isFamily, initMultiUpload.Data.UploadFileID, partInfo)
			if err != nil {
				return err
			}

			// step.4 上传切片
			uploadURL := uploadUrls[0]
			_, err = y.put(ctx, uploadURL.RequestURL, uploadURL.Headers, false,
				driver.NewLimitedUploadStream(ctx, bytes.NewReader(byteData)), isFamily)
			if err != nil {
				return err
			}
			up(float64(threadG.Success()) * 100 / float64(count))
			return nil
		})
	}
	if err = threadG.Wait(); err != nil {
		return nil, err
	}

	fileMd5Hex := strings.ToUpper(hex.EncodeToString(fileMd5.Sum(nil)))
	sliceMd5Hex := fileMd5Hex
	if file.GetSize() > sliceSize {
		sliceMd5Hex = strings.ToUpper(utils.GetMD5EncodeStr(strings.Join(silceMd5Hexs, "\n")))
	}

	// 提交上传
	var resp CommitMultiUploadFileResp
	_, err = y.request(fullURL+"/commitMultiUploadFile", http.MethodGet,
		func(req *resty.Request) {
			req.SetContext(ctx)
		}, Params{
			"uploadFileId": initMultiUpload.Data.UploadFileID,
			"fileMd5":      fileMd5Hex,
			"sliceMd5":     sliceMd5Hex,
			"lazyCheck":    "1",
			"isLog":        "0",
			"opertype":     IF(overwrite, "3", "1"),
		}, &resp, isFamily)
	if err != nil {
		return nil, err
	}
	return resp.toFile(), nil
}

func (y *Cloud189PC) RapidUpload(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, isFamily bool, overwrite bool) (model.Obj, error) {
	fileMd5 := stream.GetHash().GetHash(utils.MD5)
	if len(fileMd5) < utils.MD5.Width {
		return nil, errors.New("invalid hash")
	}

	uploadInfo, err := y.OldUploadCreate(ctx, dstDir.GetID(), fileMd5, stream.GetName(), fmt.Sprint(stream.GetSize()), isFamily)
	if err != nil {
		return nil, err
	}

	if uploadInfo.FileDataExists != 1 {
		return nil, errors.New("rapid upload fail")
	}

	return y.OldUploadCommit(ctx, uploadInfo.FileCommitURL, uploadInfo.UploadFileID, isFamily, overwrite)
}

// FastUpload 快传
func (y *Cloud189PC) FastUpload(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress, isFamily bool, overwrite bool) (model.Obj, error) {
	var (
		cache = file.GetFile()
		tmpF  *os.File
		err   error
	)
	size := file.GetSize()
	if _, ok := cache.(io.ReaderAt); !ok && size > 0 {
		tmpF, err = os.CreateTemp(conf.Conf.TempDir, "file-*")
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = tmpF.Close()
			_ = os.Remove(tmpF.Name())
		}()
		cache = tmpF
	}
	sliceSize := partSize(size)
	count := int(size / sliceSize)
	lastSliceSize := size % sliceSize
	if lastSliceSize > 0 {
		count++
	} else {
		lastSliceSize = sliceSize
	}

	// step.1 优先计算所需信息
	byteSize := sliceSize
	fileMd5 := utils.MD5.NewFunc()
	sliceMd5 := utils.MD5.NewFunc()
	sliceMd5Hexs := make([]string, 0, count)
	partInfos := make([]string, 0, count)
	writers := []io.Writer{fileMd5, sliceMd5}
	if tmpF != nil {
		writers = append(writers, tmpF)
	}
	written := int64(0)
	for i := 1; i <= count; i++ {
		if utils.IsCanceled(ctx) {
			return nil, ctx.Err()
		}

		if i == count {
			byteSize = lastSliceSize
		}

		n, err := utils.CopyWithBufferN(io.MultiWriter(writers...), file, byteSize)
		written += n
		if err != nil && err != io.EOF {
			return nil, err
		}
		md5Byte := sliceMd5.Sum(nil)
		sliceMd5Hexs = append(sliceMd5Hexs, strings.ToUpper(hex.EncodeToString(md5Byte)))
		partInfos = append(partInfos, fmt.Sprint(i, "-", base64.StdEncoding.EncodeToString(md5Byte)))
		sliceMd5.Reset()
	}

	if tmpF != nil {
		if size > 0 && written != size {
			return nil, errs.NewErr(err, "CreateTempFile failed, incoming stream actual size= %d, expect = %d ", written, size)
		}
		_, err = tmpF.Seek(0, io.SeekStart)
		if err != nil {
			return nil, errs.NewErr(err, "CreateTempFile failed, can't seek to 0 ")
		}
	}

	fileMd5Hex := strings.ToUpper(hex.EncodeToString(fileMd5.Sum(nil)))
	sliceMd5Hex := fileMd5Hex
	if size > sliceSize {
		sliceMd5Hex = strings.ToUpper(utils.GetMD5EncodeStr(strings.Join(sliceMd5Hexs, "\n")))
	}

	fullURL := _uploadURL
	if isFamily {
		fullURL += "/family"
	} else {
		// params.Set("extend", `{"opScene":"1","relativepath":"","rootfolderid":""}`)
		fullURL += "/person"
	}

	// 尝试恢复进度
	uploadProgress, ok := base.GetUploadProgress[*UploadProgress](y, y.getTokenInfo().SessionKey, fileMd5Hex)
	if !ok {
		// step.2 预上传
		params := Params{
			"parentFolderId": dstDir.GetID(),
			"fileName":       url.QueryEscape(file.GetName()),
			"fileSize":       fmt.Sprint(file.GetSize()),
			"fileMd5":        fileMd5Hex,
			"sliceSize":      fmt.Sprint(sliceSize),
			"sliceMd5":       sliceMd5Hex,
		}
		if isFamily {
			params.Set("familyId", y.FamilyID)
		}
		var uploadInfo InitMultiUploadResp
		_, err = y.request(fullURL+"/initMultiUpload", http.MethodGet, func(req *resty.Request) {
			req.SetContext(ctx)
		}, params, &uploadInfo, isFamily)
		if err != nil {
			return nil, err
		}
		uploadProgress = &UploadProgress{
			UploadInfo:  uploadInfo,
			UploadParts: partInfos,
		}
	}

	uploadInfo := uploadProgress.UploadInfo.Data
	// 网盘中不存在该文件，开始上传
	if uploadInfo.FileDataExists != 1 {
		threadG, upCtx := errgroup.NewGroupWithContext(ctx, y.uploadThread,
			retry.Attempts(3),
			retry.Delay(time.Second),
			retry.DelayType(retry.BackOffDelay))
		for i, uploadPart := range uploadProgress.UploadParts {
			if utils.IsCanceled(upCtx) {
				break
			}

			i, uploadPart := i, uploadPart
			threadG.Go(func(ctx context.Context) error {
				// step.3 获取上传链接
				uploadURLs, err := y.GetMultiUploadUrls(ctx, isFamily, uploadInfo.UploadFileID, uploadPart)
				if err != nil {
					return err
				}
				uploadURL := uploadURLs[0]

				byteSize, offset := sliceSize, int64(uploadURL.PartNumber-1)*sliceSize
				if uploadURL.PartNumber == count {
					byteSize = lastSliceSize
				}

				// step.4 上传切片
				_, err = y.put(ctx, uploadURL.RequestURL, uploadURL.Headers, false, io.NewSectionReader(cache, offset, byteSize), isFamily)
				if err != nil {
					return err
				}

				up(float64(threadG.Success()) * 100 / float64(len(uploadURLs)))
				uploadProgress.UploadParts[i] = ""
				return nil
			})
		}
		if err = threadG.Wait(); err != nil {
			if errors.Is(err, context.Canceled) {
				uploadProgress.UploadParts = utils.SliceFilter(uploadProgress.UploadParts, func(s string) bool { return s != "" })
				base.SaveUploadProgress(y, uploadProgress, y.getTokenInfo().SessionKey, fileMd5Hex)
			}
			return nil, err
		}
	}

	// step.5 提交
	var resp CommitMultiUploadFileResp
	_, err = y.request(fullURL+"/commitMultiUploadFile", http.MethodGet,
		func(req *resty.Request) {
			req.SetContext(ctx)
		}, Params{
			"uploadFileId": uploadInfo.UploadFileID,
			"isLog":        "0",
			"opertype":     IF(overwrite, "3", "1"),
		}, &resp, isFamily)
	if err != nil {
		return nil, err
	}
	return resp.toFile(), nil
}

// GetMultiUploadUrls 获取上传切片信息
// 对http body有大小限制，分片信息太多会出错
func (y *Cloud189PC) GetMultiUploadUrls(ctx context.Context, isFamily bool, uploadFileId string, partInfo ...string) ([]UploadUrlInfo, error) {
	fullURL := _uploadURL
	if isFamily {
		fullURL += "/family"
	} else {
		fullURL += "/person"
	}

	var uploadUrlsResp UploadUrlsResp
	_, err := y.request(fullURL+"/getMultiUploadUrls", http.MethodGet,
		func(req *resty.Request) {
			req.SetContext(ctx)
		}, Params{
			"uploadFileId": uploadFileId,
			"partInfo":     strings.Join(partInfo, ","),
		}, &uploadUrlsResp, isFamily)
	if err != nil {
		return nil, err
	}
	uploadUrls := uploadUrlsResp.Data

	if len(uploadUrls) != len(partInfo) {
		return nil, errors.Errorf("uploadUrls get error, due to get length %d, real length %d", len(partInfo), len(uploadUrls))
	}

	uploadURLInfos := make([]UploadUrlInfo, 0, len(uploadUrls))
	for k, v := range uploadUrls {
		partNumber, err := strconv.Atoi(strings.TrimPrefix(k, "partNumber_"))
		if err != nil {
			return nil, err
		}
		uploadURLInfos = append(uploadURLInfos, UploadUrlInfo{
			PartNumber:     partNumber,
			Headers:        ParseHttpHeader(v.RequestHeader),
			UploadUrlsData: v,
		})
	}
	sort.Slice(uploadURLInfos, func(i, j int) bool {
		return uploadURLInfos[i].PartNumber < uploadURLInfos[j].PartNumber
	})
	return uploadURLInfos, nil
}

// OldUpload 旧版本上传，家庭云不支持覆盖
func (y *Cloud189PC) OldUpload(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress, isFamily bool, overwrite bool) (model.Obj, error) {
	cacheFileProgress := model.UpdateProgressWithRange(up, 0, 50)
	up = model.UpdateProgressWithRange(up, 50, 100)
	tempFile, fileMd5, err := stream.CacheFullInTempFileAndHash(file, cacheFileProgress, utils.MD5)

	if err != nil {
		return nil, err
	}
	rateLimited := driver.NewLimitedUploadStream(ctx, io.NopCloser(tempFile))

	// 创建上传会话
	uploadInfo, err := y.OldUploadCreate(ctx, dstDir.GetID(), fileMd5, file.GetName(), fmt.Sprint(file.GetSize()), isFamily)
	if err != nil {
		return nil, err
	}

	// 网盘中不存在该文件，开始上传
	status := GetUploadFileStatusResp{CreateUploadFileResp: *uploadInfo}
	for status.GetSize() < file.GetSize() && status.FileDataExists != 1 {
		if utils.IsCanceled(ctx) {
			return nil, ctx.Err()
		}

		header := map[string]string{
			"ResumePolicy": "1",
			"Expect":       "100-continue",
		}

		if isFamily {
			header["FamilyId"] = fmt.Sprint(y.FamilyID)
			header["UploadFileId"] = fmt.Sprint(status.UploadFileID)
		} else {
			header["Edrive-UploadFileId"] = fmt.Sprint(status.UploadFileID)
		}

		_, err := y.put(ctx, status.FileUploadURL, header, true, rateLimited, isFamily)
		if err, ok := err.(*RespErr); ok && err.Code != "InputStreamReadError" {
			return nil, err
		}

		// 获取断点状态
		fullURL := _apiURL + "/getUploadFileStatus.action"
		if y.isFamily() {
			fullURL = _apiURL + "/family/file/getFamilyFileStatus.action"
		}
		_, err = y.get(fullURL, func(req *resty.Request) {
			req.SetContext(ctx).SetQueryParams(map[string]string{
				"uploadFileId": fmt.Sprint(status.UploadFileID),
				"resumePolicy": "1",
			})
			if isFamily {
				req.SetQueryParam("familyId", fmt.Sprint(y.FamilyID))
			}
		}, &status, isFamily)
		if err != nil {
			return nil, err
		}
		if _, err := tempFile.Seek(status.GetSize(), io.SeekStart); err != nil {
			return nil, err
		}
		up(float64(status.GetSize()) / float64(file.GetSize()) * 100)
	}

	return y.OldUploadCommit(ctx, status.FileCommitURL, status.UploadFileID, isFamily, overwrite)
}

// OldUploadCreate 创建上传会话
func (y *Cloud189PC) OldUploadCreate(ctx context.Context, parentID string, fileMd5, fileName, fileSize string, isFamily bool) (*CreateUploadFileResp, error) {
	var uploadInfo CreateUploadFileResp

	fullURL := _apiURL + "/createUploadFile.action"
	if isFamily {
		fullURL = _apiURL + "/family/file/createFamilyFile.action"
	}
	_, err := y.post(fullURL, func(req *resty.Request) {
		req.SetContext(ctx)
		if isFamily {
			req.SetQueryParams(map[string]string{
				"familyId":     y.FamilyID,
				"parentId":     parentID,
				"fileMd5":      fileMd5,
				"fileName":     fileName,
				"fileSize":     fileSize,
				"resumePolicy": "1",
			})
		} else {
			req.SetFormData(map[string]string{
				"parentFolderId": parentID,
				"fileName":       fileName,
				"size":           fileSize,
				"md5":            fileMd5,
				"opertype":       "3",
				"flag":           "1",
				"resumePolicy":   "1",
				"isLog":          "0",
			})
		}
	}, &uploadInfo, isFamily)

	if err != nil {
		return nil, err
	}
	return &uploadInfo, nil
}

// OldUploadCommit 提交上传文件
func (y *Cloud189PC) OldUploadCommit(ctx context.Context, fileCommitURL string, uploadFileID int64, isFamily bool, overwrite bool) (model.Obj, error) {
	var resp OldCommitUploadFileResp
	_, err := y.post(fileCommitURL, func(req *resty.Request) {
		req.SetContext(ctx)
		if isFamily {
			req.SetHeaders(map[string]string{
				"ResumePolicy": "1",
				"UploadFileId": fmt.Sprint(uploadFileID),
				"FamilyId":     fmt.Sprint(y.FamilyID),
			})
		} else {
			req.SetFormData(map[string]string{
				"opertype":     IF(overwrite, "3", "1"),
				"resumePolicy": "1",
				"uploadFileId": fmt.Sprint(uploadFileID),
				"isLog":        "0",
			})
		}
	}, &resp, isFamily)
	if err != nil {
		return nil, err
	}
	return resp.toFile(), nil
}

func (y *Cloud189PC) isFamily() bool {
	return y.Type == "family"
}

func (y *Cloud189PC) isLogin() bool {
	if y.tokenInfo == nil {
		return false
	}
	_, err := y.get(_apiURL+"/getUserInfo.action", nil, nil)
	return err == nil
}

// 创建家庭云中转文件夹
func (y *Cloud189PC) createFamilyTransferFolder() error {
	var rootFolder Cloud189Folder
	_, err := y.post(_apiURL+"/family/file/createFolder.action", func(req *resty.Request) {
		req.SetQueryParams(map[string]string{
			"folderName": "FamilyTransferFolder",
			"familyId":   y.FamilyID,
		})
	}, &rootFolder, true)
	if err != nil {
		return err
	}
	y.familyTransferFolder = &rootFolder
	return nil
}

// 清理中转文件夹
func (y *Cloud189PC) cleanFamilyTransfer(ctx context.Context) error {
	transferFolderID := y.familyTransferFolder.GetID()
	for pageNum := 1; ; pageNum++ {
		resp, err := y.getFilesWithPage(ctx, transferFolderID, true, pageNum, 100, "lastOpTime", "asc")
		if err != nil {
			return err
		}
		// 获取完毕跳出
		if resp.FileListAO.Count == 0 {
			break
		}

		var tasks []BatchTaskInfo
		for i := 0; i < len(resp.FileListAO.FolderList); i++ {
			folder := resp.FileListAO.FolderList[i]
			tasks = append(tasks, BatchTaskInfo{
				FileID:   folder.GetID(),
				FileName: folder.GetName(),
				IsFolder: BoolToNumber(folder.IsDir()),
			})
		}
		for i := 0; i < len(resp.FileListAO.FileList); i++ {
			file := resp.FileListAO.FileList[i]
			tasks = append(tasks, BatchTaskInfo{
				FileID:   file.GetID(),
				FileName: file.GetName(),
				IsFolder: BoolToNumber(file.IsDir()),
			})
		}

		if len(tasks) > 0 {
			// 删除
			result, err := y.CreateBatchTask("DELETE", y.FamilyID, "", nil, tasks...)
			if err != nil {
				return err
			}
			err = y.WaitBatchTask("DELETE", result.TaskID, time.Second)
			if err != nil {
				return err
			}
			// 永久删除
			result, err = y.CreateBatchTask("CLEAR_RECYCLE", y.FamilyID, "", nil, tasks...)
			if err != nil {
				return err
			}
			err = y.WaitBatchTask("CLEAR_RECYCLE", result.TaskID, time.Second)
			return err
		}
	}
	return nil
}

// 获取家庭云所有用户信息
func (y *Cloud189PC) getFamilyInfoList() ([]FamilyInfoResp, error) {
	var resp FamilyInfoListResp
	_, err := y.get(_apiURL+"/family/manage/getFamilyList.action", nil, &resp, true)
	if err != nil {
		return nil, err
	}
	return resp.FamilyInfoResp, nil
}

// 抽取家庭云ID
func (y *Cloud189PC) getFamilyID() (string, error) {
	infos, err := y.getFamilyInfoList()
	if err != nil {
		return "", err
	}
	if len(infos) == 0 {
		return "", errors.Errorf("cannot get automatically,please input family_id")
	}
	for _, info := range infos {
		if strings.Contains(y.getTokenInfo().LoginName, info.RemarkName) {
			return fmt.Sprint(info.FamilyID), nil
		}
	}
	return fmt.Sprint(infos[0].FamilyID), nil
}

// SaveFamilyFileToPersonCloud 保存家庭云中的文件到个人云
func (y *Cloud189PC) SaveFamilyFileToPersonCloud(ctx context.Context, familyID string, srcObj, dstDir model.Obj, overwrite bool) error {
	// _, err := y.post(_apiURL+"/family/file/saveFileToMember.action", func(req *resty.Request) {
	// 	req.SetQueryParams(map[string]string{
	// 		"channelId":    "home",
	// 		"familyId":     familyID,
	// 		"destParentId": destParentId,
	// 		"fileIdList":   familyFileId,
	// 	})
	// }, nil)
	// return err

	task := BatchTaskInfo{
		FileID:   srcObj.GetID(),
		FileName: srcObj.GetName(),
		IsFolder: BoolToNumber(srcObj.IsDir()),
	}
	resp, err := y.CreateBatchTask("COPY", familyID, dstDir.GetID(), map[string]string{
		"groupId":  "null",
		"copyType": "2",
		"shareId":  "null",
	}, task)
	if err != nil {
		return err
	}

	for {
		state, err := y.CheckBatchTask("COPY", resp.TaskID)
		if err != nil {
			return err
		}
		switch state.TaskStatus {
		case 2:
			task.DealWay = IF(overwrite, 3, 2)
			// 冲突时覆盖文件
			if err := y.ManageBatchTask("COPY", resp.TaskID, dstDir.GetID(), task); err != nil {
				return err
			}
		case 4:
			return nil
		}
		time.Sleep(time.Millisecond * 400)
	}
}

// Delete 永久删除文件
func (y *Cloud189PC) Delete(ctx context.Context, familyID string, srcObj model.Obj) error {
	task := BatchTaskInfo{
		FileID:   srcObj.GetID(),
		FileName: srcObj.GetName(),
		IsFolder: BoolToNumber(srcObj.IsDir()),
	}
	// 删除源文件
	resp, err := y.CreateBatchTask("DELETE", familyID, "", nil, task)
	if err != nil {
		return err
	}
	err = y.WaitBatchTask("DELETE", resp.TaskID, time.Second)
	if err != nil {
		return err
	}
	// 清除回收站
	resp, err = y.CreateBatchTask("CLEAR_RECYCLE", familyID, "", nil, task)
	if err != nil {
		return err
	}
	err = y.WaitBatchTask("CLEAR_RECYCLE", resp.TaskID, time.Second)
	if err != nil {
		return err
	}
	return nil
}

func (y *Cloud189PC) CreateBatchTask(aType string, familyID string, targetFolderID string, other map[string]string, taskInfos ...BatchTaskInfo) (*CreateBatchTaskResp, error) {
	var resp CreateBatchTaskResp
	_, err := y.post(_apiURL+"/batch/createBatchTask.action", func(req *resty.Request) {
		req.SetFormData(map[string]string{
			"type":      aType,
			"taskInfos": MustString(utils.JSONTool.MarshalToString(taskInfos)),
		})
		if targetFolderID != "" {
			req.SetFormData(map[string]string{"targetFolderId": targetFolderID})
		}
		if familyID != "" {
			req.SetFormData(map[string]string{"familyId": familyID})
		}
		req.SetFormData(other)
	}, &resp, familyID != "")
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// CheckBatchTask 检测任务状态
func (y *Cloud189PC) CheckBatchTask(aType string, taskID string) (*BatchTaskStateResp, error) {
	var resp BatchTaskStateResp
	_, err := y.post(_apiURL+"/batch/checkBatchTask.action", func(req *resty.Request) {
		req.SetFormData(map[string]string{
			"type":   aType,
			"taskId": taskID,
		})
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetConflictTaskInfo 获取冲突的任务信息
func (y *Cloud189PC) GetConflictTaskInfo(aType string, taskID string) (*BatchTaskConflictTaskInfoResp, error) {
	var resp BatchTaskConflictTaskInfoResp
	_, err := y.post(_apiURL+"/batch/getConflictTaskInfo.action", func(req *resty.Request) {
		req.SetFormData(map[string]string{
			"type":   aType,
			"taskId": taskID,
		})
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ManageBatchTask 处理冲突
func (y *Cloud189PC) ManageBatchTask(aType string, taskID string, targetFolderID string, taskInfos ...BatchTaskInfo) error {
	_, err := y.post(_apiURL+"/batch/manageBatchTask.action", func(req *resty.Request) {
		req.SetFormData(map[string]string{
			"targetFolderId": targetFolderID,
			"type":           aType,
			"taskId":         taskID,
			"taskInfos":      MustString(utils.JSONTool.MarshalToString(taskInfos)),
		})
	}, nil)
	return err
}

var ErrIsConflict = errors.New("there is a conflict with the target object")

// WaitBatchTask 等待任务完成
func (y *Cloud189PC) WaitBatchTask(aType string, taskID string, t time.Duration) error {
	for {
		state, err := y.CheckBatchTask(aType, taskID)
		if err != nil {
			return err
		}
		switch state.TaskStatus {
		case 2:
			return ErrIsConflict
		case 4:
			return nil
		}
		time.Sleep(t)
	}
}

func (y *Cloud189PC) getTokenInfo() *AppSessionResp {
	if y.ref != nil {
		return y.ref.getTokenInfo()
	}
	return y.tokenInfo
}

func (y *Cloud189PC) getClient() *resty.Client {
	if y.ref != nil {
		return y.ref.getClient()
	}
	return y.client
}
