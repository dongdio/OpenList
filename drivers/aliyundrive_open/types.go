package aliyundrive_open

import (
	"time"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// ErrResp 表示API响应的错误信息
type ErrResp struct {
	Code    string `json:"code"`    // 错误码，用于识别错误类型
	Message string `json:"message"` // 错误信息，用于展示给用户
}

// Files 表示文件列表响应
type Files struct {
	Items      []File `json:"items"`       // 文件列表，包含目录下的所有文件和文件夹
	NextMarker string `json:"next_marker"` // 下一页标记，用于分页获取更多文件
}

// File 表示阿里云盘文件信息
type File struct {
	DriveID       string    `json:"drive_id"`       // 驱动ID，标识文件所属的云盘
	FileID        string    `json:"file_id"`        // 文件ID，文件的唯一标识符
	ParentFileID  string    `json:"parent_file_id"` // 父文件ID，文件所在目录的ID
	Name          string    `json:"name"`           // 文件名，显示的文件名称
	Size          int64     `json:"size"`           // 文件大小，单位为字节
	FileExtension string    `json:"file_extension"` // 文件扩展名，不包含点号
	ContentHash   string    `json:"content_hash"`   // 内容哈希，SHA1算法计算的文件内容哈希值
	Category      string    `json:"category"`       // 文件分类，如图片、视频、文档等
	Type          string    `json:"type"`           // 文件类型，folder表示目录，file表示文件
	Thumbnail     string    `json:"thumbnail"`      // 缩略图URL，用于预览图片或视频
	URL           string    `json:"url"`            // 文件URL，用于下载或预览文件
	CreatedAt     time.Time `json:"created_at"`     // 创建时间，文件创建的UTC时间
	UpdatedAt     time.Time `json:"updated_at"`     // 更新时间，文件最后修改的UTC时间

	// 仅创建时使用的字段
	FileName string `json:"file_name"` // 文件名（创建时使用），与Name字段功能相同
}

// fileToObj 将阿里云盘文件转换为通用对象
// 将API返回的File对象转换为系统内部使用的Object对象
func fileToObj(f File) *model.ObjThumb {
	// 如果Name为空，使用FileName字段
	if f.Name == "" {
		f.Name = f.FileName
	}

	// 创建并返回通用对象
	return &model.ObjThumb{
		Object: model.Object{
			ID:       f.FileID,                                     // 文件ID
			Name:     f.Name,                                       // 文件名
			Size:     f.Size,                                       // 文件大小
			Modified: f.UpdatedAt,                                  // 修改时间
			IsFolder: f.Type == "folder",                           // 是否为文件夹
			Ctime:    f.CreatedAt,                                  // 创建时间
			HashInfo: utils.NewHashInfo(utils.SHA1, f.ContentHash), // 哈希信息
		},
		Thumbnail: model.Thumbnail{Thumbnail: f.Thumbnail}, // 缩略图
	}
}

// PartInfo 表示分片上传信息
type PartInfo struct {
	Etag        any    `json:"etag"`         // 分片的ETag，用于验证上传完整性
	PartNumber  int    `json:"part_number"`  // 分片编号，从1开始的连续整数
	PartSize    any    `json:"part_size"`    // 分片大小，单位为字节
	UploadURL   string `json:"upload_url"`   // 上传URL，用于上传分片数据
	ContentType string `json:"content_type"` // 内容类型，指定上传内容的MIME类型
}

// CreateResp 表示创建文件的响应
type CreateResp struct {
	FileID       string     `json:"file_id"`        // 文件ID，创建的文件唯一标识符
	UploadID     string     `json:"upload_id"`      // 上传ID，分片上传的会话标识
	RapidUpload  bool       `json:"rapid_upload"`   // 是否秒传，true表示文件已存在无需上传
	PartInfoList []PartInfo `json:"part_info_list"` // 分片信息列表，包含各分片的上传信息
}

// MoveOrCopyResp 表示移动或复制文件的响应
type MoveOrCopyResp struct {
	Exist   bool   `json:"exist"`    // 文件是否已存在，true表示目标位置已有同名文件
	DriveID string `json:"drive_id"` // 驱动ID，文件所属的云盘ID
	FileID  string `json:"file_id"`  // 文件ID，移动或复制后的文件ID
}
