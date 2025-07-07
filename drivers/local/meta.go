package local

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// Addition 本地存储驱动的额外配置选项
type Addition struct {
	driver.RootPath
	// Thumbnail 是否启用缩略图功能
	Thumbnail bool `json:"thumbnail" required:"true" help:"启用缩略图功能"`
	// ThumbCacheFolder 缩略图缓存文件夹路径
	// 如果为空，则不会缓存缩略图到磁盘
	ThumbCacheFolder string `json:"thumb_cache_folder" help:"缩略图缓存文件夹路径，留空则不缓存缩略图"`
	// ThumbConcurrency 生成缩略图的并发数
	// 控制可以同时生成多少个缩略图
	ThumbConcurrency string `json:"thumb_concurrency" default:"16" required:"false" help:"缩略图生成并发数，控制同时可以生成多少个缩略图"`
	// VideoThumbPos 视频缩略图位置
	// 如果是数字（整数或浮点数），表示视频的秒数
	// 如果以'%'结尾，表示视频时长的百分比
	VideoThumbPos string `json:"video_thumb_pos" default:"20%" required:"false" help:"视频缩略图位置，数字表示秒数，百分比表示视频时长的比例"`
	// ShowHidden 是否显示隐藏文件和文件夹
	ShowHidden bool `json:"show_hidden" default:"true" required:"false" help:"是否显示隐藏的文件和文件夹"`
	// MkdirPerm 创建文件夹的权限，八进制表示
	MkdirPerm string `json:"mkdir_perm" default:"777" help:"创建文件夹的权限（八进制）"`
	// RecycleBinPath 回收站路径
	// 如果为空或保持'delete permanently'，则永久删除文件
	RecycleBinPath string `json:"recycle_bin_path" default:"delete permanently" help:"回收站路径，留空或保持'delete permanently'则永久删除文件"`
}

// 驱动配置
var config = driver.Config{
	Name:        "Local", // 驱动名称
	OnlyLocal:   true,    // 仅本地操作
	LocalSort:   true,    // 本地排序
	NoCache:     true,    // 不使用缓存
	DefaultRoot: "/",     // 默认根路径
}

// init 初始化函数，注册本地存储驱动
func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(Local)
	})
}
