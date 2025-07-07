package _115_open

import (
	"context"
	"encoding/base64"
	"io"
	"time"

	sdk "github.com/OpenListTeam/115-sdk-go"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/avast/retry-go"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// calPartSize 根据文件大小计算合适的分片大小
// 参数:
//   - fileSize: 文件大小（字节）
//
// 返回:
//   - int64: 计算得到的分片大小（字节）
func calPartSize(fileSize int64) int64 {
	var partSize int64 = 20 * utils.MB // 默认分片大小为20MB

	// 根据文件大小动态调整分片大小
	if fileSize > partSize {
		if fileSize > 1*utils.TB { // 文件大小超过1TB
			partSize = 5 * utils.GB // 分片大小设为5GB
		} else if fileSize > 768*utils.GB { // 超过768GB
			partSize = 109951163 // ≈ 104.8576MB，将1TB分成10,000个分片
		} else if fileSize > 512*utils.GB { // 超过512GB
			partSize = 82463373 // ≈ 78.6432MB
		} else if fileSize > 384*utils.GB { // 超过384GB
			partSize = 54975582 // ≈ 52.4288MB
		} else if fileSize > 256*utils.GB { // 超过256GB
			partSize = 41231687 // ≈ 39.3216MB
		} else if fileSize > 128*utils.GB { // 超过128GB
			partSize = 27487791 // ≈ 26.2144MB
		}
	}

	return partSize
}

// singleUpload 执行单文件上传（非分片）
// 参数:
//   - ctx: 上下文
//   - tempF: 临时文件
//   - tokenResp: 上传令牌响应
//   - initResp: 上传初始化响应
//
// 返回:
//   - error: 错误信息
func (d *Open115) singleUpload(ctx context.Context, tempF model.File, tokenResp *sdk.UploadGetTokenResp, initResp *sdk.UploadInitResp) error {
	// 创建OSS客户端
	ossClient, err := oss.New(
		tokenResp.Endpoint,
		tokenResp.AccessKeyId,
		tokenResp.AccessKeySecret,
		oss.SecurityToken(tokenResp.SecurityToken),
	)
	if err != nil {
		return errors.Wrap(err, "创建OSS客户端失败")
	}

	// 获取存储桶
	bucket, err := ossClient.Bucket(initResp.Bucket)
	if err != nil {
		return errors.Wrap(err, "获取OSS存储桶失败")
	}

	// 执行上传，设置回调参数
	err = bucket.PutObject(
		initResp.Object,
		tempF,
		oss.Callback(base64.StdEncoding.EncodeToString([]byte(initResp.Callback.Value.Callback))),
		oss.CallbackVar(base64.StdEncoding.EncodeToString([]byte(initResp.Callback.Value.CallbackVar))),
	)

	if err != nil {
		return errors.Wrap(err, "上传文件到OSS失败")
	}

	return nil
}

// 回调结果结构定义（当前未使用，保留作为参考）
// type CallbackResult struct {
// 	State   bool   `json:"state"`
// 	Code    int    `json:"code"`
// 	Message string `json:"message"`
// 	Data    struct {
// 		PickCode string `json:"pick_code"` // 文件提取码
// 		FileName string `json:"file_name"` // 文件名
// 		FileSize int64  `json:"file_size"` // 文件大小
// 		FileID   string `json:"file_id"`   // 文件ID
// 		ThumbURL string `json:"thumb_url"` // 缩略图URL
// 		Sha1     string `json:"sha1"`      // 文件SHA1哈希
// 		Aid      int    `json:"aid"`       // 附件ID
// 		Cid      string `json:"cid"`       // 目录ID
// 	} `json:"data"`
// }

// multpartUpload 执行分片上传
// 参数:
//   - ctx: 上下文
//   - stream: 文件流
//   - up: 上传进度更新回调
//   - tokenResp: 上传令牌响应
//   - initResp: 上传初始化响应
//
// 返回:
//   - error: 错误信息
func (d *Open115) multpartUpload(ctx context.Context, stream model.FileStreamer, up driver.UpdateProgress, tokenResp *sdk.UploadGetTokenResp, initResp *sdk.UploadInitResp) error {
	// 获取文件大小
	fileSize := stream.GetSize()

	// 计算分片大小
	chunkSize := calPartSize(fileSize)

	// 创建OSS客户端
	ossClient, err := oss.New(
		tokenResp.Endpoint,
		tokenResp.AccessKeyId,
		tokenResp.AccessKeySecret,
		oss.SecurityToken(tokenResp.SecurityToken),
	)
	if err != nil {
		return errors.Wrap(err, "创建OSS客户端失败")
	}

	// 获取存储桶
	bucket, err := ossClient.Bucket(initResp.Bucket)
	if err != nil {
		return errors.Wrap(err, "获取OSS存储桶失败")
	}

	// 初始化分片上传，启用顺序上传模式
	imur, err := bucket.InitiateMultipartUpload(initResp.Object, oss.Sequential())
	if err != nil {
		return errors.Wrap(err, "初始化分片上传失败")
	}

	// 计算分片数量
	partNum := (stream.GetSize() + chunkSize - 1) / chunkSize

	// 创建分片列表
	parts := make([]oss.UploadPart, partNum)

	// 上传偏移量
	offset := int64(0)

	// 逐个上传分片
	for i := int64(1); i <= partNum; i++ {
		// 检查上下文是否已取消
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}

		// 计算当前分片大小
		partSize := chunkSize
		if i == partNum {
			// 最后一个分片可能不足chunkSize
			partSize = fileSize - (i-1)*chunkSize
		}

		// 创建可重置的读取器
		rd := utils.NewMultiReadable(io.LimitReader(stream, partSize))

		// 使用重试机制上传分片
		err = retry.Do(
			func() error {
				// 重置读取器
				_ = rd.Reset()

				// 创建限速上传流
				rateLimitedRd := driver.NewLimitedUploadStream(ctx, rd)

				// 上传分片
				part, err := bucket.UploadPart(imur, rateLimitedRd, partSize, int(i))
				if err != nil {
					return errors.Wrap(err, "上传分片失败")
				}

				// 保存分片信息
				parts[i-1] = part
				return nil
			},
			retry.Attempts(3),                   // 最多重试3次
			retry.DelayType(retry.BackOffDelay), // 使用退避延迟
			retry.Delay(time.Second),            // 初始延迟1秒
		)

		if err != nil {
			return errors.Wrapf(err, "上传第%d个分片失败", i)
		}

		// 更新偏移量和进度
		if i == partNum {
			offset = fileSize
		} else {
			offset += partSize
		}

		// 更新上传进度
		up(float64(offset) / float64(fileSize) * 100)
	}

	// 完成分片上传
	_, err = bucket.CompleteMultipartUpload(
		imur,
		parts,
		oss.Callback(base64.StdEncoding.EncodeToString([]byte(initResp.Callback.Value.Callback))),
		oss.CallbackVar(base64.StdEncoding.EncodeToString([]byte(initResp.Callback.Value.CallbackVar))),
		// oss.CallbackResult(&callbackRespBytes), // 如需获取回调结果可取消注释
	)

	if err != nil {
		return errors.Wrap(err, "完成分片上传失败")
	}

	return nil
}
