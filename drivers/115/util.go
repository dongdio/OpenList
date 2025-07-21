package _115

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cipher "github.com/SheltonZhu/115driver/pkg/crypto/ec115"
	crypto "github.com/SheltonZhu/115driver/pkg/crypto/m115"
	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"github.com/pkg/errors"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// login 登录115云盘
// 返回:
//   - error: 登录失败时的错误信息
func (p *Pan115) login() error {
	var err error

	// 设置客户端选项
	opts := []driver115.Option{
		driver115.UA(p.getUA()), // 设置用户代理
		func(c *driver115.Pan115Client) {
			// 设置TLS配置，根据全局配置决定是否跳过证书验证
			c.Client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify})
		},
	}

	// 创建115云盘客户端
	p.client = driver115.New(opts...)

	// 准备凭证对象
	credential := &driver115.Credential{}

	// 根据提供的登录方式选择登录方法
	if p.QRCodeToken != "" {
		// 使用二维码令牌登录
		session := &driver115.QRCodeSession{
			UID: p.QRCodeToken,
		}

		// 执行二维码登录
		if credential, err = p.client.QRCodeLoginWithApp(session, driver115.LoginApp(p.QRCodeSource)); err != nil {
			return errors.Wrap(err, "二维码登录失败")
		}

		// 登录成功后，更新Cookie并清除二维码令牌
		p.Cookie = fmt.Sprintf("UID=%s;CID=%s;SEID=%s;KID=%s",
			credential.UID, credential.CID, credential.SEID, credential.KID)
		p.QRCodeToken = ""
	} else if p.Cookie != "" {
		// 使用Cookie登录
		if err = credential.FromCookie(p.Cookie); err != nil {
			return errors.Wrap(err, "从Cookie导入凭证失败")
		}

		// 导入凭证到客户端
		p.client.ImportCredential(credential)
	} else {
		// 未提供任何登录方式
		return errors.New("缺少Cookie或二维码令牌")
	}

	// 检查登录状态
	return p.client.LoginCheck()
}

// getFiles 获取指定目录下的文件列表
// 参数:
//   - fileId: 文件夹ID
//
// 返回:
//   - []FileObj: 文件对象列表
//   - error: 错误信息
func (p *Pan115) getFiles(fileId string) ([]FileObj, error) {
	result := make([]FileObj, 0)

	// 确保页面大小有效
	if p.PageSize <= 0 {
		p.PageSize = driver115.FileListLimit
	}

	// 获取文件列表
	files, err := p.client.ListWithLimit(fileId, p.PageSize, driver115.WithMultiUrls())
	if err != nil {
		return nil, errors.Wrap(err, "获取文件列表失败")
	}

	// 转换为自定义文件对象
	for _, file := range *files {
		result = append(result, FileObj{file})
	}

	return result, nil
}

// getNewFile 根据文件ID获取文件信息
// 参数:
//   - fileId: 文件ID
//
// 返回:
//   - *FileObj: 文件对象
//   - error: 错误信息
func (p *Pan115) getNewFile(fileId string) (*FileObj, error) {
	file, err := p.client.GetFile(fileId)
	if err != nil {
		return nil, errors.Wrap(err, "获取文件信息失败")
	}
	return &FileObj{*file}, nil
}

// getNewFileByPickCode 根据提取码获取文件信息
// 参数:
//   - pickCode: 文件提取码
//
// 返回:
//   - *FileObj: 文件对象
//   - error: 错误信息
func (p *Pan115) getNewFileByPickCode(pickCode string) (*FileObj, error) {
	// 准备响应结构
	result := driver115.GetFileInfoResponse{}

	// 创建请求
	req := p.client.NewRequest().
		SetQueryParam("pick_code", pickCode).
		ForceContentType("application/json;charset=UTF-8").
		SetResult(&result)

	// 发送请求
	resp, err := req.Get(driver115.ApiFileInfo)

	// 检查错误
	if err = driver115.CheckErr(err, &result, resp); err != nil {
		return nil, errors.Wrap(err, "获取文件信息失败")
	}

	// 检查响应中是否包含文件信息
	if len(result.Files) == 0 {
		return nil, errors.New("未获取到文件信息")
	}

	// 获取第一个文件信息
	fileInfo := result.Files[0]

	// 创建并填充文件对象
	fileObj := &FileObj{}
	fileObj.From(fileInfo)

	return fileObj, nil
}

// getUA 获取用户代理字符串
// 返回:
//   - string: 格式化的用户代理字符串
func (p *Pan115) getUA() string {
	return fmt.Sprintf("Mozilla/5.0 115Browser/%s", appVer)
}

// DownloadWithUA 使用指定用户代理获取下载信息
// 参数:
//   - pickCode: 文件提取码
//   - ua: 用户代理字符串
//
// 返回:
//   - *driver115.DownloadInfo: 下载信息
//   - error: 错误信息
func (p *Pan115) DownloadWithUA(pickCode, ua string) (*driver115.DownloadInfo, error) {
	// 生成加密密钥
	key := crypto.GenerateKey()

	// 准备响应结构
	result := driver115.DownloadResp{}

	// 准备请求参数
	params, err := utils.JSONTool.Marshal(map[string]string{"pick_code": pickCode})
	if err != nil {
		return nil, errors.Wrap(err, "序列化请求参数失败")
	}

	// 加密请求数据
	data := crypto.Encode(params, key)

	// 构建请求URL
	reqUrl := driver115.AndroidApiDownloadGetUrl + "?t=" + driver115.Now().String()

	// 发送请求
	resp, err := base.RestyClient.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Cookie", p.Cookie).
		SetHeader("User-Agent", ua).
		SetFormDataFromValues(url.Values{"data": []string{data}}).
		SetResult(&result).
		Post(reqUrl)

	if err != nil {
		return nil, errors.Wrap(err, "发送下载请求失败")
	}

	// 检查响应错误
	if err = result.Err(resp.String()); err != nil {
		return nil, errors.Wrap(err, "下载请求返回错误")
	}

	// 解密响应数据
	decodedData, err := crypto.Decode(string(result.EncodedData), key)
	if err != nil {
		return nil, errors.Wrap(err, "解密响应数据失败")
	}

	// 构建下载信息
	info := &driver115.DownloadInfo{}
	info.PickCode = pickCode
	info.Header = resp.Request.Header
	info.Url.Url = utils.GetBytes(decodedData, "url").String()

	return info, nil
}

// GenerateToken 生成上传令牌
// 参数:
//   - fileID: 文件ID
//   - preID: 预哈希值
//   - timeStamp: 时间戳
//   - fileSize: 文件大小
//   - signKey: 签名密钥
//   - signVal: 签名值
//
// 返回:
//   - string: 生成的令牌
func (p *Pan115) GenerateToken(fileID, preID, timeStamp, fileSize, signKey, signVal string) string {
	// 获取用户ID
	userID := strconv.FormatInt(p.client.UserID, 10)

	// 计算用户ID的MD5哈希
	userIDMd5 := md5.Sum([]byte(userID))

	// 组合所有参数，计算最终令牌
	tokenData := md5Salt + fileID + fileSize + signKey + signVal + userID + timeStamp +
		hex.EncodeToString(userIDMd5[:]) + appVer

	// 计算MD5哈希
	tokenMd5 := md5.Sum([]byte(tokenData))

	// 返回十六进制编码的令牌
	return hex.EncodeToString(tokenMd5[:])
}

// rapidUpload 尝试秒传文件
// 参数:
//   - fileSize: 文件大小
//   - fileName: 文件名
//   - dirID: 目标目录ID
//   - preID: 预哈希值
//   - fileID: 文件ID（完整哈希值）
//   - stream: 文件流
//
// 返回:
//   - *driver115.UploadInitResp: 上传初始化响应
//   - error: 错误信息
func (p *Pan115) rapidUpload(fileSize int64, fileName, dirID, preID, fileID string, stream model.FileStreamer) (*driver115.UploadInitResp, error) {
	var (
		ecdhCipher   *cipher.EcdhCipher // ECDH加密器
		encrypted    []byte             // 加密后的数据
		decrypted    []byte             // 解密后的数据
		encodedToken string             // 编码后的令牌
		err          error
		target       = "U_1_" + dirID                  // 上传目标
		result       = driver115.UploadInitResp{}      // 结果结构
		fileSizeStr  = strconv.FormatInt(fileSize, 10) // 文件大小字符串
	)

	// 创建ECDH加密器
	if ecdhCipher, err = cipher.NewEcdhCipher(); err != nil {
		return nil, errors.Wrap(err, "创建ECDH加密器失败")
	}

	// 获取用户ID
	userID := strconv.FormatInt(p.client.UserID, 10)

	// 准备表单数据
	form := url.Values{}
	form.Set("appid", "0")
	form.Set("appversion", appVer)
	form.Set("userid", userID)
	form.Set("filename", fileName)
	form.Set("filesize", fileSizeStr)
	form.Set("fileid", fileID)
	form.Set("target", target)
	form.Set("sig", p.client.GenerateSignature(fileID, target))

	// 初始化签名参数
	signKey, signVal := "", ""

	// 循环处理，处理签名验证
	for retry := true; retry; {
		// 获取当前时间戳
		timestamp := driver115.NowMilli()

		// 编码令牌
		if encodedToken, err = ecdhCipher.EncodeToken(timestamp.ToInt64()); err != nil {
			return nil, errors.Wrap(err, "编码令牌失败")
		}

		// 准备查询参数
		params := map[string]string{
			"k_ec": encodedToken,
		}

		// 更新表单数据
		form.Set("t", timestamp.String())
		form.Set("token", p.GenerateToken(fileID, preID, timestamp.String(), fileSizeStr, signKey, signVal))

		// 如果有签名参数，添加到表单
		if signKey != "" && signVal != "" {
			form.Set("sign_key", signKey)
			form.Set("sign_val", signVal)
		}

		// 加密表单数据
		if encrypted, err = ecdhCipher.Encrypt([]byte(form.Encode())); err != nil {
			return nil, errors.Wrap(err, "加密表单数据失败")
		}

		// 创建请求
		req := p.client.NewRequest().
			SetQueryParams(params).
			SetBody(encrypted).
			SetHeaderVerbatim("Content-Type", "application/x-www-form-urlencoded")

		// 发送请求
		resp, err := req.Post(driver115.ApiUploadInit)
		if err != nil {
			return nil, errors.Wrap(err, "发送秒传初始化请求失败")
		}

		// 解密响应
		if decrypted, err = ecdhCipher.Decrypt(resp.Body()); err != nil {
			return nil, errors.Wrap(err, "解密响应失败")
		}

		// 解析响应
		if err = driver115.CheckErr(utils.JSONTool.Unmarshal(decrypted, &result), &result, resp); err != nil {
			return nil, errors.Wrap(err, "解析响应失败")
		}

		// 处理签名验证
		if result.Status == 7 {
			// 需要更新签名参数
			signKey = result.SignKey
			signVal, err = UploadDigestRange(stream, result.SignCheck)
			if err != nil {
				return nil, errors.Wrap(err, "计算签名值失败")
			}
		} else {
			// 不需要继续重试
			retry = false
		}

		// 保存SHA1哈希值
		result.SHA1 = fileID
	}

	return &result, nil
}

// UploadDigestRange 计算指定范围的文件哈希值
// 参数:
//   - stream: 文件流
//   - rangeSpec: 范围规范字符串，格式为"开始-结束"
//
// 返回:
//   - string: 计算得到的哈希值
//   - error: 错误信息
func UploadDigestRange(stream model.FileStreamer, rangeSpec string) (result string, err error) {
	var start, end int64

	// 解析范围规范
	if _, err = fmt.Sscanf(rangeSpec, "%d-%d", &start, &end); err != nil {
		return "", errors.Wrap(err, "解析范围规范失败")
	}

	// 计算长度
	length := end - start + 1

	// 读取指定范围的数据
	reader, err := stream.RangeRead(http_range.Range{Start: start, Length: length})
	if err != nil {
		return "", errors.Wrap(err, "读取文件范围失败")
	}

	// 计算SHA1哈希
	hashStr, err := utils.HashReader(utils.SHA1, reader)
	if err != nil {
		return "", errors.Wrap(err, "计算哈希失败")
	}

	// 转换为大写
	result = strings.ToUpper(hashStr)
	return
}

// UploadByOSS 使用阿里云OSS SDK上传文件
// 参数:
//   - ctx: 上下文
//   - params: OSS上传参数
//   - s: 文件流
//   - dirID: 目标目录ID
//   - up: 上传进度更新回调
//
// 返回:
//   - *UploadResult: 上传结果
//   - error: 错误信息
func (p *Pan115) UploadByOSS(ctx context.Context, params *driver115.UploadOSSParams, s model.FileStreamer, dirID string, up driver.UpdateProgress) (*UploadResult, error) {
	// 获取OSS令牌
	ossToken, err := p.client.GetOSSToken()
	if err != nil {
		return nil, errors.Wrap(err, "获取OSS令牌失败")
	}

	// 创建OSS客户端
	ossClient, err := oss.New(driver115.OSSEndpoint, ossToken.AccessKeyID, ossToken.AccessKeySecret)
	if err != nil {
		return nil, errors.Wrap(err, "创建OSS客户端失败")
	}

	// 获取存储桶
	bucket, err := ossClient.Bucket(params.Bucket)
	if err != nil {
		return nil, errors.Wrap(err, "获取存储桶失败")
	}

	// 用于存储回调结果
	var bodyBytes []byte

	// 创建带进度更新的上传流
	reader := driver.NewLimitedUploadStream(ctx, &driver.ReaderUpdatingProgress{
		Reader:         s,
		UpdateProgress: up,
	})

	// 执行上传
	err = bucket.PutObject(params.Object, reader, append(
		driver115.OssOption(params, ossToken),
		oss.CallbackResult(&bodyBytes),
	)...)

	if err != nil {
		return nil, errors.Wrap(err, "上传文件失败")
	}

	// 解析上传结果
	var uploadResult UploadResult
	if err = utils.JSONTool.Unmarshal(bodyBytes, &uploadResult); err != nil {
		return nil, errors.Wrap(err, "解析上传结果失败")
	}

	// 检查上传结果中的错误
	if err = uploadResult.Err(string(bodyBytes)); err != nil {
		return nil, errors.Wrap(err, "上传结果包含错误")
	}

	return &uploadResult, nil
}

// UploadByMultipart 使用分片上传方式上传文件
// 参数:
//   - ctx: 上下文
//   - params: OSS上传参数
//   - fileSize: 文件大小
//   - s: 文件流
//   - dirID: 目标目录ID
//   - up: 上传进度更新回调
//   - opts: 分片上传选项
//
// 返回:
//   - *UploadResult: 上传结果
//   - error: 错误信息
func (p *Pan115) UploadByMultipart(ctx context.Context, params *driver115.UploadOSSParams, fileSize int64, s model.FileStreamer,
	dirID string, up driver.UpdateProgress, opts ...driver115.UploadMultipartOption) (*UploadResult, error) {
	var (
		chunks    []oss.FileChunk                   // 分片列表
		parts     []oss.UploadPart                  // 已上传分片列表
		imur      oss.InitiateMultipartUploadResult // 分片上传初始化结果
		ossClient *oss.Client                       // OSS客户端
		bucket    *oss.Bucket                       // 存储桶
		ossToken  *driver115.UploadOSSTokenResp     // OSS令牌
		bodyBytes []byte                            // 回调结果
		err       error
	)

	// 将文件缓存到临时文件中，以便分片读取
	tmpF, err := s.CacheFullInTempFile()
	if err != nil {
		return nil, errors.Wrap(err, "缓存文件到临时文件失败")
	}

	// 应用上传选项
	options := driver115.DefalutUploadMultipartOptions()
	if len(opts) > 0 {
		for _, f := range opts {
			f(options)
		}
	}
	// OSS启用Sequential必须按顺序上传，因此设置线程数为1
	options.ThreadsNum = 1

	// 获取OSS令牌
	if ossToken, err = p.client.GetOSSToken(); err != nil {
		return nil, errors.Wrap(err, "获取OSS令牌失败")
	}

	// 创建OSS客户端，启用MD5和CRC校验
	if ossClient, err = oss.New(driver115.OSSEndpoint, ossToken.AccessKeyID, ossToken.AccessKeySecret, oss.EnableMD5(true), oss.EnableCRC(true)); err != nil {
		return nil, errors.Wrap(err, "创建OSS客户端失败")
	}

	// 获取存储桶
	if bucket, err = ossClient.Bucket(params.Bucket); err != nil {
		return nil, errors.Wrap(err, "获取存储桶失败")
	}

	// OSS令牌一小时后会失效，每50分钟重新获取一次
	ticker := time.NewTicker(options.TokenRefreshTime)
	defer ticker.Stop()

	// 设置上传超时
	timeout := time.NewTimer(options.Timeout)
	defer timeout.Stop()

	// 计算文件分片
	if chunks, err = SplitFile(fileSize); err != nil {
		return nil, errors.Wrap(err, "计算文件分片失败")
	}

	// 初始化分片上传
	if imur, err = bucket.InitiateMultipartUpload(params.Object,
		oss.SetHeader(driver115.OssSecurityTokenHeaderName, ossToken.SecurityToken),
		oss.UserAgentHeader(driver115.OSSUserAgent),
		oss.EnableSha1(), oss.Sequential(),
	); err != nil {
		return nil, errors.Wrap(err, "初始化分片上传失败")
	}

	// 同步等待所有分片上传完成
	wg := sync.WaitGroup{}
	wg.Add(len(chunks))

	// 创建通道
	chunksCh := make(chan oss.FileChunk)         // 分片通道
	errCh := make(chan error)                    // 错误通道
	uploadedPartsCh := make(chan oss.UploadPart) // 已上传分片通道
	quit := make(chan struct{})                  // 退出通道

	// 生产者：发送分片到通道
	go chunksProducer(chunksCh, chunks)

	// 等待所有分片上传完成后发送退出信号
	go func() {
		wg.Wait()
		quit <- struct{}{}
	}()

	// 已完成分片数量，用于更新进度
	completedNum := atomic.Int32{}

	// 消费者：上传分片
	for i := 0; i < options.ThreadsNum; i++ {
		go func(threadId int) {
			// 捕获panic
			defer func() {
				if r := recover(); r != nil {
					errCh <- errors.Errorf("recovered in %v", r)
				}
			}()

			// 处理每个分片
			for chunk := range chunksCh {
				var part oss.UploadPart // 出现错误就继续尝试，共尝试3次
				for retry := 0; retry < 3; retry++ {
					select {
					case <-ctx.Done():
						// 上下文取消
						break
					case <-ticker.C:
						// 刷新OSS令牌
						if ossToken, err = p.client.GetOSSToken(); err != nil {
							errCh <- errors.Wrap(err, "刷新令牌时出现错误")
						}
					default:
					}

					// 读取分片数据
					buf := make([]byte, chunk.Size)
					if _, err = tmpF.ReadAt(buf, chunk.Offset); err != nil && !errors.Is(err, io.EOF) {
						continue
					}

					// 上传分片
					if part, err = bucket.UploadPart(imur, driver.NewLimitedUploadStream(ctx, bytes.NewReader(buf)),
						chunk.Size, chunk.Number, driver115.OssOption(params, ossToken)...); err == nil {
						break
					}
				}

				// 处理上传错误
				if err != nil {
					errCh <- errors.Wrap(err, fmt.Sprintf("上传 %s 的第%d个分片时出现错误：%v", s.GetName(), chunk.Number, err))
				} else {
					// 更新进度
					num := completedNum.Add(1)
					up(float64(num) * 100.0 / float64(len(chunks)))
				}

				// 发送已上传分片
				uploadedPartsCh <- part
			}
		}(i)
	}

	// 收集已上传分片
	go func() {
		for part := range uploadedPartsCh {
			parts = append(parts, part)
			wg.Done()
		}
	}()

	// 主循环：等待上传完成或错误发生
LOOP:
	for {
		select {
		case <-ticker.C:
			// 刷新OSS令牌
			if ossToken, err = p.client.GetOSSToken(); err != nil {
				return nil, errors.Wrap(err, "刷新令牌失败")
			}
		case <-quit:
			// 所有分片上传完成
			break LOOP
		case err := <-errCh:
			// 发生错误
			return nil, err
		case <-timeout.C:
			// 上传超时
			return nil, errors.New("上传超时")
		}
	}

	// 完成分片上传
	// 注意：OSS分片上传不计算SHA1，可能导致115服务器校验错误
	// params.Callback.Callback = strings.ReplaceAll(params.Callback.Callback, "${sha1}", params.SHA1)
	if _, err = bucket.CompleteMultipartUpload(imur, parts, append(
		driver115.OssOption(params, ossToken),
		oss.CallbackResult(&bodyBytes),
	)...); err != nil {
		return nil, errors.Wrap(err, "完成分片上传失败")
	}

	// 解析上传结果
	var uploadResult UploadResult
	if err = utils.JSONTool.Unmarshal(bodyBytes, &uploadResult); err != nil {
		return nil, errors.Wrap(err, "解析上传结果失败")
	}

	// 检查上传结果中的错误
	if err = uploadResult.Err(string(bodyBytes)); err != nil {
		return nil, errors.Wrap(err, "上传结果包含错误")
	}

	return &uploadResult, nil
}

// chunksProducer 分片生产者，将分片发送到通道
// 参数:
//   - ch: 分片通道
//   - chunks: 分片列表
func chunksProducer(ch chan oss.FileChunk, chunks []oss.FileChunk) {
	for _, chunk := range chunks {
		ch <- chunk
	}
}

// SplitFile 根据文件大小计算合适的分片方案
// 参数:
//   - fileSize: 文件大小
//
// 返回:
//   - []oss.FileChunk: 分片列表
//   - error: 错误信息
func SplitFile(fileSize int64) (chunks []oss.FileChunk, err error) {
	// 根据文件大小选择不同的分片策略
	for i := int64(1); i < 10; i++ {
		if fileSize < i*utils.GB { // 文件大小小于i GB时分为i*1000片
			if chunks, err = SplitFileByPartNum(fileSize, int(i*1000)); err != nil {
				return nil, errors.Wrap(err, "按分片数量拆分文件失败")
			}
			break
		}
	}

	// 文件大小大于9GB时分为10000片
	if fileSize > 9*utils.GB {
		if chunks, err = SplitFileByPartNum(fileSize, 10000); err != nil {
			return nil, errors.Wrap(err, "按分片数量拆分大文件失败")
		}
	}

	// 确保单个分片大小不小于100KB
	if len(chunks) > 0 && chunks[0].Size < 100*utils.KB {
		if chunks, err = SplitFileByPartSize(fileSize, 100*utils.KB); err != nil {
			return nil, errors.Wrap(err, "按分片大小重新拆分文件失败")
		}
	}

	return chunks, nil
}

// SplitFileByPartNum 按分片数量拆分文件
// 参数:
//   - fileSize: 文件大小
//   - chunkNum: 分片数量
//
// 返回:
//   - []oss.FileChunk: 分片列表
//   - error: 错误信息
func SplitFileByPartNum(fileSize int64, chunkNum int) ([]oss.FileChunk, error) {
	// 验证分片数量范围
	if chunkNum <= 0 || chunkNum > 10000 {
		return nil, errors.New("分片数量无效")
	}

	// 确保分片数量不超过文件大小
	if int64(chunkNum) > fileSize {
		return nil, errors.New("分片数量超过文件大小")
	}

	chunks := make([]oss.FileChunk, 0, chunkNum)
	chunk := oss.FileChunk{}
	chunkN := int64(chunkNum)

	// 计算每个分片的大小和偏移量
	for i := int64(0); i < chunkN; i++ {
		chunk.Number = int(i + 1)
		chunk.Offset = i * (fileSize / chunkN)

		// 最后一个分片需要包含剩余的字节
		if i == chunkN-1 {
			chunk.Size = fileSize/chunkN + fileSize%chunkN
		} else {
			chunk.Size = fileSize / chunkN
		}

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// SplitFileByPartSize 按分片大小拆分文件
// 参数:
//   - fileSize: 文件大小
//   - chunkSize: 分片大小
//
// 返回:
//   - []oss.FileChunk: 分片列表
//   - error: 错误信息
func SplitFileByPartSize(fileSize int64, chunkSize int64) ([]oss.FileChunk, error) {
	// 验证分片大小
	if chunkSize <= 0 {
		return nil, errors.New("分片大小无效")
	}

	// 计算分片数量
	chunkN := fileSize / chunkSize

	// 验证分片数量不超过限制
	if chunkN >= 10000 {
		return nil, errors.New("分片数量过多，请增加分片大小")
	}

	// 预分配分片列表
	chunks := make([]oss.FileChunk, 0, chunkN+1)
	chunk := oss.FileChunk{}

	// 计算每个完整分片
	for i := int64(0); i < chunkN; i++ {
		chunk.Number = int(i + 1)
		chunk.Offset = i * chunkSize
		chunk.Size = chunkSize
		chunks = append(chunks, chunk)
	}

	// 处理最后一个不完整分片
	if fileSize%chunkSize > 0 {
		chunk.Number = len(chunks) + 1
		chunk.Offset = int64(len(chunks)) * chunkSize
		chunk.Size = fileSize % chunkSize
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// checkErr 检查错误并处理响应结果
// 参数:
//   - err: 原始错误
//   - result: 包含错误信息的响应结果
//   - restyResp: Resty响应对象
//
// 返回:
//   - error: 处理后的错误
func checkErr(err error, result driver115.ResultWithErr, restyResp *resty.Response) error {
	// 如果没有原始错误，检查响应结果中的错误
	if err == nil {
		err = result.Err(restyResp.String())
	}

	// 返回错误或nil
	if err != nil {
		return errors.Wrap(err, "API请求失败")
	}

	return nil
}
