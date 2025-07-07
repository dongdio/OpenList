package _115

import (
	"time"

	"github.com/SheltonZhu/115driver/pkg/driver"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 确保FileObj实现了model.Obj接口
var _ model.Obj = (*FileObj)(nil)

// FileObj 封装115云盘文件对象
type FileObj struct {
	driver.File // 嵌入115驱动的File结构体
}

// CreateTime 获取文件创建时间
// 实现model.Obj接口
func (f *FileObj) CreateTime() time.Time {
	return f.File.CreateTime
}

// GetHash 获取文件哈希信息
// 实现model.Obj接口
func (f *FileObj) GetHash() utils.HashInfo {
	return utils.NewHashInfo(utils.SHA1, f.Sha1)
}

// UploadResult 上传结果结构体
type UploadResult struct {
	driver.BasicResp // 嵌入基本响应结构体
	Data             struct {
		PickCode string `json:"pick_code"` // 文件提取码
		FileSize int    `json:"file_size"` // 文件大小
		FileID   string `json:"file_id"`   // 文件ID
		ThumbURL string `json:"thumb_url"` // 缩略图URL
		Sha1     string `json:"sha1"`      // 文件SHA1哈希值
		Aid      int    `json:"aid"`       // 附件ID
		FileName string `json:"file_name"` // 文件名
		Cid      string `json:"cid"`       // 目录ID
		IsVideo  int    `json:"is_video"`  // 是否为视频文件，1表示是
	} `json:"data"` // 响应数据
}
