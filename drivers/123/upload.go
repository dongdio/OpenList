package _123

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/pkg/errors"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// getS3PreSignedUrls 获取S3预签名URL，用于分片上传
// 参数：
//   - ctx: 上下文
//   - upReq: 上传请求响应
//   - start: 起始分片编号
//   - end: 结束分片编号
//
// 返回：
//   - 预签名URL响应和可能的错误
func (d *Pan123) getS3PreSignedUrls(ctx context.Context, upReq *UploadResp, start, end int) (*S3PreSignedURLs, error) {
	data := base.Json{
		"bucket":          upReq.Data.Bucket,
		"key":             upReq.Data.Key,
		"partNumberEnd":   end,
		"partNumberStart": start,
		"uploadId":        upReq.Data.UploadId,
		"StorageNode":     upReq.Data.StorageNode,
	}
	var s3PreSignedUrls S3PreSignedURLs
	_, err := d.Request(S3PreSignedUrls, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetContext(ctx)
	}, &s3PreSignedUrls)
	if err != nil {
		return nil, err
	}
	return &s3PreSignedUrls, nil
}

// getS3Auth 获取S3认证信息，用于单个文件上传
// 参数：
//   - ctx: 上下文
//   - upReq: 上传请求响应
//   - start: 起始分片编号
//   - end: 结束分片编号
//
// 返回：
//   - S3认证响应和可能的错误
func (d *Pan123) getS3Auth(ctx context.Context, upReq *UploadResp, start, end int) (*S3PreSignedURLs, error) {
	data := base.Json{
		"StorageNode":     upReq.Data.StorageNode,
		"bucket":          upReq.Data.Bucket,
		"key":             upReq.Data.Key,
		"partNumberEnd":   end,
		"partNumberStart": start,
		"uploadId":        upReq.Data.UploadId,
	}
	var s3PreSignedUrls S3PreSignedURLs
	_, err := d.Request(S3Auth, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetContext(ctx)
	}, &s3PreSignedUrls)
	if err != nil {
		return nil, err
	}
	return &s3PreSignedUrls, nil
}

// completeS3 完成S3上传
// 参数：
//   - ctx: 上下文
//   - upReq: 上传请求响应
//   - file: 文件流
//   - isMultipart: 是否为分片上传
//
// 返回：
//   - 可能的错误
func (d *Pan123) completeS3(ctx context.Context, upReq *UploadResp, file model.FileStreamer, isMultipart bool) error {
	data := base.Json{
		"StorageNode": upReq.Data.StorageNode,
		"bucket":      upReq.Data.Bucket,
		"fileId":      upReq.Data.FileId,
		"fileSize":    file.GetSize(),
		"isMultipart": isMultipart,
		"key":         upReq.Data.Key,
		"uploadId":    upReq.Data.UploadId,
	}
	_, err := d.Request(UploadCompleteV2, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetContext(ctx)
	}, nil)
	return err
}

// newUpload 实现自定义上传逻辑
// 参数：
//   - ctx: 上下文
//   - upReq: 上传请求响应
//   - file: 文件流
//   - up: 上传进度回调函数
//
// 返回：
//   - 可能的错误
func (d *Pan123) newUpload(ctx context.Context, upReq *UploadResp, file model.FileStreamer, up driver.UpdateProgress) error {
	// 将文件缓存到临时文件中
	tmpF, err := file.CacheFullInTempFile()
	if err != nil {
		return err
	}

	// 计算分片大小和数量
	size := file.GetSize()
	chunkSize := min(size, 16*utils.MB)
	chunkCount := int(size / chunkSize)
	lastChunkSize := size % chunkSize
	if lastChunkSize > 0 {
		chunkCount++
	} else {
		lastChunkSize = chunkSize
	}

	// 确定批量获取预签名URL的策略
	batchSize := 1
	getS3UploadUrl := d.getS3Auth
	if chunkCount > 1 {
		batchSize = 10
		getS3UploadUrl = d.getS3PreSignedUrls
	}

	// 分批上传文件分片
	for i := 1; i <= chunkCount; i += batchSize {
		// 检查上下文是否已取消
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}

		// 计算当前批次的起止分片编号
		start := i
		end := min(i+batchSize, chunkCount+1)

		// 获取预签名URL
		s3PreSignedUrls, err := getS3UploadUrl(ctx, upReq, start, end)
		if err != nil {
			return err
		}

		// 上传每个分片
		for j := start; j < end; j++ {
			// 检查上下文是否已取消
			if utils.IsCanceled(ctx) {
				return ctx.Err()
			}

			// 确定当前分片大小
			curSize := chunkSize
			if j == chunkCount {
				curSize = lastChunkSize
			}

			// 上传分片
			err = d.uploadS3Chunk(ctx, upReq, s3PreSignedUrls, j, end,
				io.NewSectionReader(tmpF, chunkSize*int64(j-1), curSize), curSize, false, getS3UploadUrl)
			if err != nil {
				return err
			}

			// 更新上传进度
			up(float64(j) * 100 / float64(chunkCount))
		}
	}

	// 完成上传
	return d.completeS3(ctx, upReq, file, chunkCount > 1)
}

// uploadS3Chunk 上传单个S3分片
// 参数：
//   - ctx: 上下文
//   - upReq: 上传请求响应
//   - s3PreSignedUrls: 预签名URL响应
//   - cur: 当前分片编号
//   - end: 结束分片编号
//   - reader: 分片数据读取器
//   - curSize: 当前分片大小
//   - retry: 是否为重试操作
//   - getS3UploadUrl: 获取上传URL的函数
//
// 返回：
//   - 可能的错误
func (d *Pan123) uploadS3Chunk(ctx context.Context, upReq *UploadResp, s3PreSignedUrls *S3PreSignedURLs,
	cur, end int, reader *io.SectionReader, curSize int64, retry bool,
	getS3UploadUrl func(ctx context.Context, upReq *UploadResp, start int, end int) (*S3PreSignedURLs, error)) error {

	// 获取当前分片的上传URL
	uploadUrl := s3PreSignedUrls.Data.PreSignedUrls[strconv.Itoa(cur)]
	if uploadUrl == "" {
		return errors.Errorf("上传URL为空，s3PreSignedUrls: %+v", s3PreSignedUrls)
	}

	// 创建HTTP请求
	req, err := http.NewRequest("PUT", uploadUrl, driver.NewLimitedUploadStream(ctx, reader))
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	req.ContentLength = curSize
	// req.Header.Set("Content-Length", strconv.FormatInt(curSize, 10))

	// 发送请求
	res, err := base.HttpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// 处理响应
	if res.StatusCode == http.StatusForbidden {
		if retry {
			return errors.Errorf("上传S3分片 %d 失败，状态码: %d", cur, res.StatusCode)
		}

		// 刷新预签名URL并重试
		newS3PreSignedUrls, err := getS3UploadUrl(ctx, upReq, cur, end)
		if err != nil {
			return err
		}
		s3PreSignedUrls.Data.PreSignedUrls = newS3PreSignedUrls.Data.PreSignedUrls

		// 重置读取位置并重试
		_, err = reader.Seek(0, io.SeekStart)
		if err != nil {
			return errors.Wrap(err, "重置文件读取位置失败")
		}
		return d.uploadS3Chunk(ctx, upReq, s3PreSignedUrls, cur, end, reader, curSize, true, getS3UploadUrl)
	}

	// 处理其他错误状态码
	if res.StatusCode != http.StatusOK {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return errors.Wrapf(err, "读取错误响应失败，状态码: %d", res.StatusCode)
		}
		return errors.Errorf("上传S3分片 %d 失败，状态码: %d，响应: %s", cur, res.StatusCode, body)
	}

	return nil
}
