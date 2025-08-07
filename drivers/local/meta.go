package local

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 本地存储驱动的额外配置选项
type Addition struct {
	driver.RootPath
	// Thumbnail 是否启用缩略图功能
	// 启用后，图片和视频文件将生成缩略图
	Thumbnail bool `json:"thumbnail" required:"true" help:"启用缩略图功能，支持图片和视频文件"`

	// ThumbCacheFolder 缩略图缓存文件夹路径
	// 如果为空，则不会缓存缩略图到磁盘，每次请求都会重新生成
	// 建议配置缓存目录以提高性能
	ThumbCacheFolder string `json:"thumb_cache_folder" help:"缩略图缓存文件夹路径，留空则不缓存缩略图，建议配置以提高性能"`

	// ThumbConcurrency 生成缩略图的并发数
	// 控制可以同时生成多少个缩略图，避免系统资源过度消耗
	// 设置为0表示无限制，但可能导致系统负载过高
	ThumbConcurrency string `json:"thumb_concurrency" default:"16" required:"false" help:"缩略图生成并发数，控制同时可以生成多少个缩略图，0表示无限制"`

	// VideoThumbPos 视频缩略图位置
	// 如果是数字（整数或浮点数），表示视频的秒数
	// 如果以'%'结尾，表示视频时长的百分比
	// 例如：10表示第10秒，20%表示视频时长的20%位置
	VideoThumbPos string `json:"video_thumb_pos" default:"20%" required:"false" help:"视频缩略图位置，数字表示秒数(如10)，百分比表示视频时长的比例(如20%)"`

	// ShowHidden 是否显示隐藏文件和文件夹
	// 在Unix/Linux系统中，以点(.)开头的文件被视为隐藏文件
	// 在Windows系统中，通过文件属性的FILE_ATTRIBUTE_HIDDEN标志判断
	ShowHidden bool `json:"show_hidden" default:"false" required:"false" help:"是否显示隐藏的文件和文件夹，默认不显示"`

	// MkdirPerm 创建文件夹的权限，八进制表示
	// 仅在Unix/Linux系统中有效
	// 默认为777，表示所有用户都有读写执行权限
	MkdirPerm string `json:"mkdir_perm" default:"777" help:"创建文件夹的权限（八进制），如777表示所有用户都有读写执行权限"`

	// RecycleBinPath 回收站路径
	// 如果为空或保持'delete permanently'，则永久删除文件
	// 否则将删除的文件移动到指定的回收站目录
	RecycleBinPath string `json:"recycle_bin_path" default:"delete permanently" help:"回收站路径，留空或保持'delete permanently'则永久删除文件，否则移动到指定目录"`
}

// 驱动配置
var config = driver.Config{
	Name:              "Local", // 驱动名称
	LocalSort:         true,    // 本地排序
	NoCache:           true,    // 不使用缓存
	DefaultRoot:       "/",     // 默认根路径
	OnlyLinkMFile:     false,   // 是否只返回文件句柄
	NoLinkURL:         true,    // 不返回URL链接
	NoOverwriteUpload: false,   // 允许覆盖上传
}

// init 初始化函数，注册本地存储驱动
func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Local{}
	})
}