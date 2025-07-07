package _115_open

import (
	"time"

	sdk "github.com/OpenListTeam/115-sdk-go"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Obj 115开放平台文件对象，继承自SDK中的文件结构
type Obj sdk.GetFilesResp_File

// Thumb 获取文件缩略图URL
// 实现model.Thumb接口
func (o *Obj) Thumb() string {
	return o.Thumbnail
}

// CreateTime 获取文件创建时间
// 实现model.Obj接口
func (o *Obj) CreateTime() time.Time {
	return time.Unix(o.UpPt, 0)
}

// GetHash 获取文件哈希信息
// 实现model.Obj接口
func (o *Obj) GetHash() utils.HashInfo {
	return utils.NewHashInfo(utils.SHA1, o.Sha1)
}

// GetID 获取文件ID
// 实现model.Obj接口
func (o *Obj) GetID() string {
	return o.Fid
}

// GetName 获取文件名称
// 实现model.Obj接口
func (o *Obj) GetName() string {
	return o.Fn
}

// GetPath 获取文件路径
// 实现model.Obj接口
// 注：115开放平台API不提供完整路径，返回空字符串
func (o *Obj) GetPath() string {
	return ""
}

// GetSize 获取文件大小（字节）
// 实现model.Obj接口
func (o *Obj) GetSize() int64 {
	return o.FS
}

// IsDir 判断是否为目录
// 实现model.Obj接口
// 注：115开放平台中，Fc="0"表示目录，其他值表示文件
func (o *Obj) IsDir() bool {
	return o.Fc == "0"
}

// ModTime 获取文件修改时间
// 实现model.Obj接口
func (o *Obj) ModTime() time.Time {
	return time.Unix(o.Upt, 0)
}

// 确保Obj实现了model.Obj和model.Thumb接口
var _ model.Obj = (*Obj)(nil)
var _ model.Thumb = (*Obj)(nil)
