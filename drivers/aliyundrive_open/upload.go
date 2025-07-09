package aliyundrive_open

import (
	"context"
	"encoding/base64"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	streamPkg "github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// makePartInfos 创建分片信息列表
// 根据分片数量生成分片信息数组
func makePartInfos(size int) []base.Json {
	partInfoList := make([]base.Json, size)
	for i := 0; i < size; i++ {
		partInfoList[i] = base.Json{"part_number": 1 + i}
	}
	return partInfoList
}

// calPartSize 计算上传分片大小
// 根据文件大小动态调整分片大小，确保分片数量不超过10,000
func calPartSize(fileSize int64) int64 {
	var partSize int64 = 20 * utils.MB // 默认分片大小20MB
	if fileSize > partSize {
		if fileSize > 1*utils.TB { // 文件大小超过1TB
			partSize = 5 * utils.GB // 分片大小5GB
		} else if fileSize > 768*utils.GB { // 超过768GB
			partSize = 109951163 // ≈ 104.8576MB，将1TB分成10,000片
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

// getUploadURL 获取上传URL
// 获取指定文件分片的上传URL列表
func (d *AliyundriveOpen) getUploadURL(count int, fileID, uploadID string) ([]PartInfo, error) {
	// 创建分片信息列表
	partInfoList := makePartInfos(count)

	// 发送请求获取上传URL
	var resp CreateResp
	_, err := d.request("/adrive/v1.0/openFile/getUploadUrl", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":       d.DriveID,
			"file_id":        fileID,
			"part_info_list": partInfoList,
			"upload_id":      uploadID,
		}).SetResult(&resp)
	})
	if err != nil {
		return nil, errors.Wrap(err, "获取上传URL失败")
	}

	// 检查返回的分片信息是否完整
	if len(resp.PartInfoList) != count {
		return nil, errors.Errorf("获取上传URL不完整，期望%d个，实际获取%d个", count, len(resp.PartInfoList))
	}

	return resp.PartInfoList, nil
}

// uploadPart 上传单个分片
// 将指定数据上传到指定的分片URL
func (d *AliyundriveOpen) uploadPart(ctx context.Context, r io.Reader, partInfo PartInfo) error {
	uploadURL := partInfo.UploadURL
	// 如果启用内部上传，替换URL为内部地址
	if d.InternalUpload {
		uploadURL = strings.ReplaceAll(uploadURL, "https://cn-beijing-data.aliyundrive.net/", "http://ccp-bj29-bj-1592982087.oss-cn-beijing-internal.aliyuncs.com/")
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "PUT", uploadURL, r)
	if err != nil {
		return errors.Wrap(err, "创建上传请求失败")
	}

	// 发送请求
	res, err := base.HttpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "发送上传请求失败")
	}
	defer res.Body.Close()

	// 检查响应状态
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusConflict {
		return errors.Errorf("上传状态错误: %d", res.StatusCode)
	}
	return nil
}

// completeUpload 完成上传
// 通知服务器所有分片已上传完成
func (d *AliyundriveOpen) completeUpload(fileID, uploadID string) (model.Obj, error) {
	var newFile File
	_, err := d.request("/adrive/v1.0/openFile/complete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":  d.DriveID,
			"file_id":   fileID,
			"upload_id": uploadID,
		}).SetResult(&newFile)
	})
	if err != nil {
		return nil, errors.Wrap(err, "完成上传失败")
	}

	// 检查返回的文件信息是否有效
	if newFile.FileID == "" {
		return nil, errors.New("完成上传后返回的文件ID为空")
	}

	return fileToObj(newFile), nil
}

// ProofRange 表示证明码范围
type ProofRange struct {
	Start int64 // 起始位置，文件中证明码开始的字节位置
	End   int64 // 结束位置，文件中证明码结束的字节位置
}

// getProofRange 获取证明码范围
// 根据输入字符串和文件大小计算证明码的字节范围
func getProofRange(input string, size int64) (*ProofRange, error) {
	if size == 0 {
		return &ProofRange{}, nil
	}

	// 从输入字符串中提取16位十六进制字符串
	tmpStr := utils.GetMD5EncodeStr(input)[0:16]
	tmpInt, err := strconv.ParseUint(tmpStr, 16, 64)
	if err != nil {
		return nil, errors.Wrap(err, "解析十六进制字符串失败")
	}

	// 计算范围
	index := tmpInt % uint64(size)
	pr := &ProofRange{
		Start: int64(index),
		End:   int64(index) + 8, // 读取8字节作为证明码
	}

	// 确保结束位置不超过文件大小
	if pr.End >= size {
		pr.End = size
	}
	return pr, nil
}

// calProofCode 计算证明码
// 计算文件指定范围的数据的Base64编码，用于秒传验证
func (d *AliyundriveOpen) calProofCode(stream model.FileStreamer) (string, error) {
	// 获取证明码范围
	proofRange, err := getProofRange(d.getAccessToken(), stream.GetSize())
	if err != nil {
		return "", errors.Wrap(err, "获取证明码范围失败")
	}

	// 读取指定范围的数据
	length := proofRange.End - proofRange.Start
	reader, err := stream.RangeRead(http_range.Range{Start: proofRange.Start, Length: length})
	if err != nil {
		return "", errors.Wrap(err, "读取文件范围失败")
	}

	// 读取数据到缓冲区
	buf := make([]byte, length)
	n, err := io.ReadFull(reader, buf)
	if err == io.ErrUnexpectedEOF {
		return "", errors.Errorf("读取数据不完整，期望=%d，实际=%d", len(buf), n)
	}
	if err != nil {
		return "", errors.Wrap(err, "读取数据失败")
	}

	// 返回Base64编码的证明码
	return base64.StdEncoding.EncodeToString(buf), nil
}

// upload 上传文件
// 实现文件上传，包括秒传和分片上传
func (d *AliyundriveOpen) upload(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	// 1. 计算分片大小和准备上传数据
	var partSize = calPartSize(stream.GetSize())
	const dateFormat = "2006-01-02T15:04:05.000Z"
	mtimeStr := stream.ModTime().UTC().Format(dateFormat)
	ctimeStr := stream.CreateTime().UTC().Format(dateFormat)

	// 准备创建数据
	createData := base.Json{
		"drive_id":          d.DriveID,
		"parent_file_id":    dstDir.GetID(),
		"name":              stream.GetName(),
		"type":              "file",
		"check_name_mode":   "ignore", // 如果存在同名文件，保留两者
		"local_modified_at": mtimeStr, // 保留本地修改时间
		"local_created_at":  ctimeStr, // 保留本地创建时间
	}

	// 计算分片数量和分片信息
	count := int(math.Ceil(float64(stream.GetSize()) / float64(partSize)))
	createData["part_info_list"] = makePartInfos(count)

	// 2. 尝试秒传
	rapidUpload := !stream.IsForceStreamUpload() && stream.GetSize() > 100*utils.KB && d.RapidUpload
	if rapidUpload {
		log.Debugf("[阿里云盘开放版] 开始计算预哈希")
		// 读取前1024字节计算预哈希
		reader, err := stream.RangeRead(http_range.Range{Start: 0, Length: 1024})
		if err != nil {
			return nil, errors.Wrap(err, "读取文件头部失败")
		}
		hash, err := utils.HashReader(utils.SHA1, reader)
		if err != nil {
			return nil, errors.Wrap(err, "计算预哈希失败")
		}
		createData["size"] = stream.GetSize()
		createData["pre_hash"] = hash
	}

	// 3. 创建文件
	var createResp CreateResp
	_, err, e := d.requestReturnErrResp("/adrive/v1.0/openFile/create", http.MethodPost, func(req *resty.Request) {
		req.SetBody(createData).SetResult(&createResp)
	})
	if err != nil {
		// 处理预哈希匹配的情况（可能是秒传）
		if e.Code != "PreHashMatched" || !rapidUpload {
			return nil, errors.Wrap(err, "创建文件失败")
		}
		log.Debugf("[阿里云盘开放版] 预哈希匹配，开始秒传")

		// 获取完整哈希
		hash := stream.GetHash().GetHash(utils.SHA1)
		if len(hash) != utils.SHA1.Width {
			cacheFileProgress := model.UpdateProgressWithRange(up, 0, 50)
			up = model.UpdateProgressWithRange(up, 50, 100)
			_, hash, err = streamPkg.CacheFullInTempFileAndHash(stream, cacheFileProgress, utils.SHA1)
			if err != nil {
				return nil, errors.Wrap(err, "计算完整哈希失败")
			}
		}

		// 准备秒传数据
		delete(createData, "pre_hash")
		createData["proof_version"] = "v1"
		createData["content_hash_name"] = "sha1"
		createData["content_hash"] = hash
		createData["proof_code"], err = d.calProofCode(stream)
		if err != nil {
			return nil, errors.Errorf("计算证明码失败: %s", err.Error())
		}

		// 发送秒传请求
		_, err = d.request("/adrive/v1.0/openFile/create", http.MethodPost, func(req *resty.Request) {
			req.SetBody(createData).SetResult(&createResp)
		})
		if err != nil {
			return nil, errors.Wrap(err, "秒传失败")
		}
	}

	// 4. 如果不是秒传，执行正常上传
	if !createResp.RapidUpload {
		log.Debugf("[阿里云盘开放版] 开始正常上传，文件大小: %d, 分片大小: %d, 分片数量: %d",
			stream.GetSize(), partSize, len(createResp.PartInfoList))

		preTime := time.Now()
		var offset, length int64 = 0, partSize

		// 上传每个分片
		for i := 0; i < len(createResp.PartInfoList); i++ {
			// 检查是否取消
			if utils.IsCanceled(ctx) {
				return nil, ctx.Err()
			}

			// 如果过去50分钟，刷新上传URL（阿里云URL有效期通常为1小时）
			if time.Since(preTime) > 50*time.Minute {
				log.Debugf("[阿里云盘开放版] 上传URL即将过期，刷新URL")
				createResp.PartInfoList, err = d.getUploadURL(count, createResp.FileID, createResp.UploadID)
				if err != nil {
					return nil, errors.Wrap(err, "刷新上传URL失败")
				}
				preTime = time.Now()
			}

			// 计算当前分片大小
			if remain := stream.GetSize() - offset; length > remain {
				length = remain
			}

			// 准备读取器
			rd := utils.NewMultiReadable(io.LimitReader(stream, partSize))
			if rapidUpload {
				srd, err := stream.RangeRead(http_range.Range{Start: offset, Length: length})
				if err != nil {
					return nil, errors.Wrap(err, "读取文件分片失败")
				}
				rd = utils.NewMultiReadable(srd)
			}

			// 上传分片，失败时重试
			partNumber := i + 1
			log.Debugf("[阿里云盘开放版] 上传第%d/%d个分片，大小: %d", partNumber, count, length)
			err = retry.Do(func() error {
				_ = rd.Reset()
				rateLimitedRd := driver.NewLimitedUploadStream(ctx, rd)
				return d.uploadPart(ctx, rateLimitedRd, createResp.PartInfoList[i])
			},
				retry.Attempts(3),                   // 最多重试3次
				retry.DelayType(retry.BackOffDelay), // 使用退避算法延迟
				retry.Delay(time.Second))            // 初始延迟1秒
			if err != nil {
				return nil, errors.Wrapf(err, "上传第%d个分片失败", partNumber)
			}

			// 更新偏移量和进度
			offset += partSize
			up(float64(i+1) * 100 / float64(count)) // 更新进度百分比
		}
	} else {
		log.Debugf("[阿里云盘开放版] 秒传成功，文件ID: %s", createResp.FileID)
		up(100) // 秒传完成，进度设为100%
	}

	log.Debugf("[阿里云盘开放版] 上传完成，准备提交")

	// 5. 完成上传
	return d.completeUpload(createResp.FileID, createResp.UploadID)
}