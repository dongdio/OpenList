package _123

import (
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// File 表示123网盘中的文件或目录
type File struct {
	// 文件名
	FileName string `json:"FileName"`
	// 文件大小（字节）
	Size int64 `json:"Size"`
	// 更新时间
	UpdateAt time.Time `json:"UpdateAt"`
	// 文件ID
	FileId int64 `json:"FileId"`
	// 类型：0-文件，1-目录
	Type int `json:"Type"`
	// 文件的ETag
	Etag string `json:"Etag"`
	// S3存储的键标志
	S3KeyFlag string `json:"S3KeyFlag"`
	// 下载URL
	DownloadUrl string `json:"DownloadUrl"`
}

// CreateTime 返回文件的创建时间
// 注：123网盘API不提供创建时间，使用更新时间代替
func (f File) CreateTime() time.Time {
	return f.UpdateAt
}

// GetHash 返回文件的哈希信息
// 注：目前未实现，返回空哈希信息
func (f File) GetHash() utils.HashInfo {
	return utils.HashInfo{}
}

// GetPath 返回文件的路径
// 注：目前未实现，返回空字符串
func (f File) GetPath() string {
	return ""
}

// GetSize 返回文件的大小
func (f File) GetSize() int64 {
	return f.Size
}

// GetName 返回文件的名称
func (f File) GetName() string {
	return f.FileName
}

// ModTime 返回文件的修改时间
func (f File) ModTime() time.Time {
	return f.UpdateAt
}

// IsDir 判断是否为目录
func (f File) IsDir() bool {
	return f.Type == 1
}

// GetID 返回文件的ID（字符串形式）
func (f File) GetID() string {
	return strconv.FormatInt(f.FileId, 10)
}

// Thumb 返回文件的缩略图URL
func (f File) Thumb() string {
	if f.DownloadUrl == "" {
		return ""
	}
	du, err := url.Parse(f.DownloadUrl)
	if err != nil {
		return ""
	}
	// 设置缩略图路径和尺寸
	du.Path = strings.TrimSuffix(du.Path, "_24_24") + "_70_70"
	query := du.Query()
	query.Set("w", "70")
	query.Set("h", "70")
	// 设置文件类型（如果未设置）
	if !query.Has("type") {
		query.Set("type", strings.TrimPrefix(path.Base(f.FileName), "."))
	}
	// 设置交易密钥（如果未设置）
	if !query.Has("trade_key") {
		query.Set("trade_key", "123pan-thumbnail")
	}
	du.RawQuery = query.Encode()
	return du.String()
}

// 确保File类型实现了model.Obj和model.Thumb接口
var _ model.Obj = (*File)(nil)
var _ model.Thumb = (*File)(nil)

// Files 表示文件列表响应
type Files struct {
	Data struct {
		// 下一页标识符
		Next string `json:"Next"`
		// 总文件数
		Total int `json:"Total"`
		// 文件列表
		InfoList []File `json:"InfoList"`
	} `json:"data"`
}

// UploadResp 表示上传请求的响应
type UploadResp struct {
	Data struct {
		// S3访问密钥ID
		AccessKeyId string `json:"AccessKeyId"`
		// S3存储桶名称
		Bucket string `json:"Bucket"`
		// S3对象键
		Key string `json:"Key"`
		// S3访问密钥
		SecretAccessKey string `json:"SecretAccessKey"`
		// S3会话令牌
		SessionToken string `json:"SessionToken"`
		// 文件ID
		FileId int64 `json:"FileId"`
		// 是否重用现有文件
		Reuse bool `json:"Reuse"`
		// S3端点URL
		EndPoint string `json:"EndPoint"`
		// 存储节点
		StorageNode string `json:"StorageNode"`
		// 上传ID
		UploadId string `json:"UploadId"`
	} `json:"data"`
}

// S3PreSignedURLs 表示S3预签名URL响应
type S3PreSignedURLs struct {
	Data struct {
		// 预签名URL映射，键为分片编号，值为URL
		PreSignedUrls map[string]string `json:"presignedUrls"`
	} `json:"data"`
}
