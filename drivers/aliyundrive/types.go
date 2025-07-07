package aliyundrive

import (
	"time"

	"github.com/dongdio/OpenList/v4/internal/model"
)

// RespErr 表示API响应的错误信息
type RespErr struct {
	Code    string `json:"code"`    // 错误码
	Message string `json:"message"` // 错误信息
}

// Files 表示文件列表响应
type Files struct {
	Items      []File `json:"items"`       // 文件列表
	NextMarker string `json:"next_marker"` // 下一页标记
}

// File 表示阿里云盘文件信息
type File struct {
	DriveID       string    `json:"drive_id"`       // 驱动ID
	CreatedAt     time.Time `json:"created_at"`     // 创建时间
	FileExtension string    `json:"file_extension"` // 文件扩展名
	FileID        string    `json:"file_id"`        // 文件ID
	Type          string    `json:"type"`           // 类型
	Name          string    `json:"name"`           // 名称
	Category      string    `json:"category"`       // 分类
	ParentFileID  string    `json:"parent_file_id"` // 父文件ID
	UpdatedAt     time.Time `json:"updated_at"`     // 更新时间
	Size          int64     `json:"size"`           // 大小
	Thumbnail     string    `json:"thumbnail"`      // 缩略图
	URL           string    `json:"url"`            // URL
}

// fileToObj 将阿里云盘文件转换为通用对象
func fileToObj(f File) *model.ObjThumb {
	return &model.ObjThumb{
		Object: model.Object{
			ID:       f.FileID,
			Name:     f.Name,
			Size:     f.Size,
			Modified: f.UpdatedAt,
			IsFolder: f.Type == "folder",
		},
		Thumbnail: model.Thumbnail{Thumbnail: f.Thumbnail},
	}
}

// UploadResp 表示上传响应
type UploadResp struct {
	FileID       string `json:"file_id"`   // 文件ID
	UploadID     string `json:"upload_id"` // 上传ID
	PartInfoList []struct {
		UploadURL         string `json:"upload_url"`          // 上传URL
		InternalUploadURL string `json:"internal_upload_url"` // 内部上传URL
	} `json:"part_info_list"`

	RapidUpload bool `json:"rapid_upload"` // 是否秒传
}
