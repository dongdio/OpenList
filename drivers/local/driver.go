package local

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"os"
	stdpath "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/times"
	cp "github.com/otiai10/copy"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	_ "golang.org/x/image/webp"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/sign"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Local 本地存储驱动实现
type Local struct {
	model.Storage
	Addition
	mkdirPerm int32 // 创建目录的权限，八进制表示

	// 缩略图并发控制
	thumbConcurrency int         // 缩略图生成并发数，0表示无限制
	thumbTokenBucket TokenBucket // 令牌桶，用于限制并发

	// 视频缩略图位置
	videoThumbPos             float64 // 视频缩略图位置，秒数或百分比
	videoThumbPosIsPercentage bool    // 是否为百分比表示
}

// Config 返回驱动配置
// 实现driver.Driver接口
func (d *Local) Config() driver.Config {
	return config
}

// Init 初始化驱动
// 实现driver.Driver接口
func (d *Local) Init(ctx context.Context) error {
	// 解析目录创建权限
	if d.MkdirPerm == "" {
		d.mkdirPerm = 0777 // 默认权限
	} else {
		v, err := strconv.ParseUint(d.MkdirPerm, 8, 32)
		if err != nil {
			return errors.Wrap(err, "解析目录权限失败")
		}
		d.mkdirPerm = int32(v)
	}

	// 检查根目录是否存在
	if !utils.Exists(d.GetRootPath()) {
		return errors.Errorf("根目录 %s 不存在", d.GetRootPath())
	}

	// 如果根路径不是绝对路径，转换为绝对路径
	if !filepath.IsAbs(d.GetRootPath()) {
		abs, err := filepath.Abs(d.GetRootPath())
		if err != nil {
			return errors.Wrap(err, "转换根路径为绝对路径失败")
		}
		d.Addition.RootFolderPath = abs
	}

	// 如果配置了缩略图缓存目录，确保目录存在
	if d.ThumbCacheFolder != "" && !utils.Exists(d.ThumbCacheFolder) {
		err := os.MkdirAll(d.ThumbCacheFolder, os.FileMode(d.mkdirPerm))
		if err != nil {
			return errors.Wrap(err, "创建缩略图缓存目录失败")
		}
	}

	// 解析缩略图并发数
	if d.ThumbConcurrency != "" {
		v, err := strconv.ParseUint(d.ThumbConcurrency, 10, 32)
		if err != nil {
			return errors.Wrap(err, "解析缩略图并发数失败")
		}
		d.thumbConcurrency = int(v)
	}

	// 初始化令牌桶
	if d.thumbConcurrency == 0 {
		// 无限制
		d.thumbTokenBucket = NewNopTokenBucket()
	} else {
		// 有限制，创建令牌桶
		d.thumbTokenBucket = NewStaticTokenBucketWithMigration(d.thumbTokenBucket, d.thumbConcurrency)
	}

	// 解析视频缩略图位置
	if d.VideoThumbPos == "" {
		d.VideoThumbPos = "20%" // 默认为20%位置
	}

	if strings.HasSuffix(d.VideoThumbPos, "%") {
		// 百分比表示
		percentage := strings.TrimSuffix(d.VideoThumbPos, "%")
		val, err := strconv.ParseFloat(percentage, 64)
		if err != nil {
			return errors.Errorf("无效的视频缩略图位置值: %s, 错误: %s", d.VideoThumbPos, err)
		}
		if val < 0 || val > 100 {
			return errors.Errorf("无效的视频缩略图位置值: %s, 百分比必须在0到100之间", d.VideoThumbPos)
		}
		d.videoThumbPosIsPercentage = true
		d.videoThumbPos = val / 100 // 转换为小数
	} else {
		// 秒数表示
		val, err := strconv.ParseFloat(d.VideoThumbPos, 64)
		if err != nil {
			return errors.Errorf("无效的视频缩略图位置值: %s, 错误: %s", d.VideoThumbPos, err)
		}
		if val < 0 {
			return errors.Errorf("无效的视频缩略图位置值: %s, 时间必须为正数", d.VideoThumbPos)
		}
		d.videoThumbPosIsPercentage = false
		d.videoThumbPos = val
	}
	return nil
}

// Drop 释放资源
// 实现driver.Driver接口
func (d *Local) Drop(ctx context.Context) error {
	// 本地驱动无需特殊释放资源
	return nil
}

// GetAddition 返回额外配置
// 实现driver.Driver接口
func (d *Local) GetAddition() driver.Additional {
	return &d.Addition
}

// List 列出目录内容
// 实现driver.Driver接口
func (d *Local) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	dirPath := dir.GetPath()
	// 读取目录内容
	fileInfos, err := readDir(dirPath)
	if err != nil {
		return nil, errors.Wrap(err, "读取目录失败")
	}

	// 转换为对象列表
	var files []model.Obj
	for _, fileInfo := range fileInfos {
		// 根据配置决定是否显示隐藏文件
		if d.ShowHidden || !isHidden(fileInfo, dirPath) {
			files = append(files, d.FileInfoToObj(ctx, fileInfo, args.ReqPath, dirPath))
		}
	}
	return files, nil
}

// FileInfoToObj 将文件信息转换为对象
// 参数:
//   - ctx: 上下文
//   - fileInfo: 文件信息
//   - reqPath: 请求路径
//   - dirPath: 目录完整路径
//
// 返回:
//   - model.Obj: 文件对象
func (d *Local) FileInfoToObj(ctx context.Context, fileInfo fs.FileInfo, reqPath string, dirPath string) model.Obj {
	// 处理缩略图URL
	thumbURL := ""
	if d.Thumbnail {
		fileType := utils.GetFileType(fileInfo.Name())
		if fileType == consts.IMAGE || fileType == consts.VIDEO {
			// 构建缩略图URL
			thumbURL = common.GetApiUrl(ctx) + stdpath.Join("/d", reqPath, fileInfo.Name())
			thumbURL = utils.EncodePath(thumbURL, true)
			thumbURL += "?type=thumb&sign=" + sign.Sign(stdpath.Join(reqPath, fileInfo.Name()))
		}
	}

	// 判断是否为文件夹（包括符号链接指向的文件夹）
	isFolder := fileInfo.IsDir() || isSymlinkDir(fileInfo, dirPath)

	// 计算文件大小（文件夹大小为0）
	var size int64
	if !isFolder {
		size = fileInfo.Size()
	}

	// 获取创建时间
	var createTime time.Time
	timeInfo, err := times.Stat(stdpath.Join(dirPath, fileInfo.Name()))
	if err == nil && timeInfo.HasBirthTime() {
		createTime = timeInfo.BirthTime()
	}

	// 创建文件对象
	fileObj := model.ObjThumb{
		Object: model.Object{
			Path:     filepath.Join(dirPath, fileInfo.Name()),
			Name:     fileInfo.Name(),
			Modified: fileInfo.ModTime(),
			Size:     size,
			IsFolder: isFolder,
			Ctime:    createTime,
		},
		Thumbnail: model.Thumbnail{
			Thumbnail: thumbURL,
		},
	}
	return &fileObj
}

// GetMeta 获取文件元数据
// 参数:
//   - ctx: 上下文
//   - path: 文件路径
//
// 返回:
//   - model.Obj: 文件对象
//   - error: 错误信息
func (d *Local) GetMeta(ctx context.Context, path string) (model.Obj, error) {
	// 获取文件信息
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, errors.Wrap(err, "获取文件信息失败")
	}

	// 转换为对象
	fileObj := d.FileInfoToObj(ctx, fileInfo, path, filepath.Dir(path))

	// 注释掉的哈希设置代码，可能用于将来实现
	// h := "123123"
	// if s, ok := fileInfo.(model.SetHash); ok && fileObj.GetHash() == ("","")  {
	//	s.SetHash(h,"SHA1")
	// }

	return fileObj, nil
}

// Get 获取指定路径的文件对象
// 实现driver.Driver接口
func (d *Local) Get(ctx context.Context, path string) (model.Obj, error) {
	// 构建完整路径
	fullPath := filepath.Join(d.GetRootPath(), path)

	// 获取文件信息
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if strings.Contains(err.Error(), "cannot find the file") {
			return nil, errs.ObjectNotFound
		}
		return nil, errors.Wrap(err, "获取文件信息失败")
	}

	// 判断是否为文件夹
	isFolder := fileInfo.IsDir() || isSymlinkDir(fileInfo, filepath.Dir(fullPath))

	// 计算文件大小
	size := fileInfo.Size()
	if isFolder {
		size = 0
	}

	// 获取创建时间
	var createTime time.Time
	timeInfo, err := times.Stat(fullPath)
	if err == nil && timeInfo.HasBirthTime() {
		createTime = timeInfo.BirthTime()
	}

	// 创建文件对象
	fileObj := model.Object{
		Path:     fullPath,
		Name:     fileInfo.Name(),
		Modified: fileInfo.ModTime(),
		Ctime:    createTime,
		Size:     size,
		IsFolder: isFolder,
	}
	return &fileObj, nil
}

// Link 获取文件链接
// 实现driver.Driver接口
func (d *Local) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	filePath := file.GetPath()
	var link model.Link

	// 处理缩略图请求
	if args.Type == "thumb" && utils.Ext(file.GetName()) != "svg" {
		var buffer *bytes.Buffer
		var thumbPath *string

		// 使用令牌桶限制并发
		err := d.thumbTokenBucket.Do(ctx, func() error {
			var err error
			buffer, thumbPath, err = d.getThumb(file)
			return err
		})

		if err != nil {
			return nil, errors.Wrap(err, "获取缩略图失败")
		}

		// 设置响应头
		link.Header = http.Header{
			"Content-Type": []string{"image/png"},
		}

		// 设置响应体
		if thumbPath != nil {
			// 使用缓存的缩略图
			fileHandle, err := os.Open(*thumbPath)
			if err != nil {
				return nil, errors.Wrap(err, "打开缩略图文件失败")
			}
			link.MFile = fileHandle
		} else {
			// 使用内存中的缩略图
			link.MFile = bytes.NewReader(buffer.Bytes())
			// 不设置Content-Length，让http包自动计算
			// link.Header.Set("Content-Length", strconv.Itoa(buffer.Len()))
		}
	} else {
		// 普通文件，直接打开
		fileHandle, err := os.Open(filePath)
		if err != nil {
			return nil, errors.Wrap(err, "打开文件失败")
		}
		link.MFile = fileHandle
	}

	return &link, nil
}

// MakeDir 创建目录
// 实现driver.Driver接口
func (d *Local) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	// 构建完整路径
	fullPath := filepath.Join(parentDir.GetPath(), dirName)

	// 创建目录
	err := os.MkdirAll(fullPath, os.FileMode(d.mkdirPerm))
	if err != nil {
		return errors.Wrap(err, "创建目录失败")
	}

	return nil
}

// Move 移动文件/目录
// 实现driver.Driver接口
func (d *Local) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	srcPath := srcObj.GetPath()
	dstPath := filepath.Join(dstDir.GetPath(), srcObj.GetName())

	// 检查目标是否为源的子目录，防止循环引用
	if utils.IsSubPath(srcPath, dstPath) {
		return errors.New("目标文件夹是源文件夹的子文件夹，无法移动")
	}

	// 尝试直接重命名（移动）
	err := os.Rename(srcPath, dstPath)
	if err == nil {
		return nil
	}
	// 处理跨设备移动错误
	if !strings.Contains(err.Error(), "invalid cross-device link") {
		// 其他错误
		return errors.Wrap(err, "移动文件失败")
	}
	log.Debugf("[本地驱动] 检测到跨设备移动，使用复制+删除方式: %s -> %s", srcPath, dstPath)

	// 先复制
	if err = d.Copy(ctx, srcObj, dstDir); err != nil {
		return errors.Wrap(err, "跨设备复制失败")
	}

	// 复制成功后删除源文件
	var removeErr error
	if srcObj.IsDir() {
		removeErr = os.RemoveAll(srcObj.GetPath())
	} else {
		removeErr = os.Remove(srcObj.GetPath())
	}

	if removeErr != nil {
		return errors.Wrap(removeErr, "删除源文件失败")
	}

	return nil
}

// Rename 重命名文件/目录
// 实现driver.Driver接口
func (d *Local) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	srcPath := srcObj.GetPath()
	dstPath := filepath.Join(filepath.Dir(srcPath), newName)

	// 执行重命名
	err := os.Rename(srcPath, dstPath)
	if err != nil {
		return errors.Wrap(err, "重命名失败")
	}

	return nil
}

// Copy 复制文件/目录
// 实现driver.Driver接口
func (d *Local) Copy(_ context.Context, srcObj, dstDir model.Obj) error {
	srcPath := srcObj.GetPath()
	dstPath := filepath.Join(dstDir.GetPath(), srcObj.GetName())

	// 检查目标是否为源的子目录，防止循环引用
	if utils.IsSubPath(srcPath, dstPath) {
		return errors.New("目标文件夹是源文件夹的子文件夹，无法复制")
	}

	// 使用otiai10/copy库执行高效安全的复制
	return cp.Copy(srcPath, dstPath, cp.Options{
		Sync:          true, // 复制后同步到磁盘，可能影响性能
		PreserveTimes: true, // 保留时间戳
		PreserveOwner: true, // 保留所有者信息
	})
}

// Remove 删除文件/目录
// 实现driver.Driver接口
func (d *Local) Remove(ctx context.Context, obj model.Obj) error {
	// 根据配置决定是永久删除还是移动到回收站
	if utils.SliceContains([]string{"", "delete permanently"}, d.RecycleBinPath) {
		// 永久删除
		var err error
		if obj.IsDir() {
			err = os.RemoveAll(obj.GetPath())
		} else {
			err = os.Remove(obj.GetPath())
		}
		if err != nil {
			return errors.Wrap(err, "删除文件失败")
		}
	} else {
		// 移动到回收站
		dstPath := filepath.Join(d.RecycleBinPath, obj.GetName())

		// 处理同名文件
		if utils.Exists(dstPath) {
			// 添加时间戳避免冲突
			dstPath = filepath.Join(d.RecycleBinPath, obj.GetName()+"_"+time.Now().Format("20060102150405"))
		}

		// 执行移动
		err := os.Rename(obj.GetPath(), dstPath)
		if err != nil {
			return errors.Wrap(err, "移动到回收站失败")
		}
	}

	return nil
}

// Put 上传文件
// 实现driver.Driver接口
func (d *Local) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	// 构建目标文件路径
	fullPath := filepath.Join(dstDir.GetPath(), stream.GetName())

	// 创建目标文件
	outFile, err := os.Create(fullPath)
	if err != nil {
		return errors.Wrap(err, "创建文件失败")
	}

	// 确保文件关闭并处理错误
	defer func() {
		_ = outFile.Close()
		// 如果上下文取消，删除未完成的文件
		if errors.Is(err, context.Canceled) {
			_ = os.Remove(fullPath)
		}
	}()

	// 复制数据
	err = utils.CopyWithCtx(ctx, outFile, stream, stream.GetSize(), up)
	if err != nil {
		return errors.Wrap(err, "复制文件数据失败")
	}

	// 设置文件修改时间
	err = os.Chtimes(fullPath, stream.ModTime(), stream.ModTime())
	if err != nil {
		// 只记录日志，不中断操作
		log.Errorf("[本地驱动] 设置文件时间失败 %s: %s", fullPath, err)
	}

	return nil
}

// 确保Local实现了driver.Driver接口
var _ driver.Driver = (*Local)(nil)
