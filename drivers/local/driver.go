package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	pkgerrors "github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	_ "golang.org/x/image/webp"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/sign"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/stream"
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

	// 缓存清理状态
	lastCacheCleanTime time.Time // 上次缓存清理时间
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
			return pkgerrors.Wrapf(err, "无法解析目录权限值 '%s'，请使用有效的八进制数字", d.MkdirPerm)
		}
		d.mkdirPerm = int32(v)
	}

	// 检查根目录是否存在
	rootPath := d.GetRootPath()
	if !utils.Exists(rootPath) {
		return pkgerrors.Errorf("根目录 '%s' 不存在，请先创建该目录", rootPath)
	}

	// 如果根路径不是绝对路径，转换为绝对路径
	if !filepath.IsAbs(rootPath) {
		abs, err := filepath.Abs(rootPath)
		if err != nil {
			return pkgerrors.Wrapf(err, "无法将根路径 '%s' 转换为绝对路径", rootPath)
		}
		d.Addition.RootFolderPath = abs
	}

	// 如果配置了缩略图缓存目录，确保目录存在
	if d.ThumbCacheFolder != "" {
		if !utils.Exists(d.ThumbCacheFolder) {
			err := os.MkdirAll(d.ThumbCacheFolder, os.FileMode(d.mkdirPerm))
			if err != nil {
				return pkgerrors.Wrapf(err, "无法创建缩略图缓存目录 '%s'", d.ThumbCacheFolder)
			}
		}

		// 验证缓存目录是否可写
		testFile := filepath.Join(d.ThumbCacheFolder, ".write_test")
		err := os.WriteFile(testFile, []byte("test"), 0666)
		if err != nil {
			return pkgerrors.Wrapf(err, "缩略图缓存目录 '%s' 不可写", d.ThumbCacheFolder)
		}
		_ = os.Remove(testFile) // 删除测试文件

		// 检查是否需要清理过期缓存
		if d.ThumbCacheMaxAge > 0 {
			go func() {
				// 延迟5秒执行，避免影响启动速度
				time.Sleep(5 * time.Second)
				count, err := d.CleanThumbCache(d.ThumbCacheMaxAge)
				if err != nil {
					log.Warnf("[本地驱动] 清理过期缩略图缓存失败: %v", err)
				} else if count > 0 {
					log.Infof("[本地驱动] 已清理 %d 个过期缩略图缓存文件", count)
				}
			}()
		}
	}

	// 解析缩略图并发数
	if d.ThumbConcurrency != "" {
		v, err := strconv.Atoi(d.ThumbConcurrency)
		if err != nil {
			return pkgerrors.Wrapf(err, "无法解析缩略图并发数 '%s'，请使用有效的整数", d.ThumbConcurrency)
		}
		if v < 0 {
			return pkgerrors.Errorf("缩略图并发数不能为负数: %d", v)
		}
		d.thumbConcurrency = v
	}

	// 初始化令牌桶
	if d.thumbConcurrency == 0 {
		// 无限制
		d.thumbTokenBucket = NewNopTokenBucket()
	} else {
		// 有限制，创建令牌桶
		d.thumbTokenBucket = NewStaticTokenBucketWithMigration(d.thumbTokenBucket, d.thumbConcurrency)
	}

	// 验证缩略图质量设置
	if d.ThumbQuality < 1 || d.ThumbQuality > 100 {
		log.Warnf("[本地驱动] 缩略图质量设置无效 (%d)，使用默认值 85", d.ThumbQuality)
		d.ThumbQuality = 85
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
			return pkgerrors.Errorf("无效的视频缩略图位置值: '%s'，错误: %v", d.VideoThumbPos, err)
		}
		if val < 0 || val > 100 {
			return pkgerrors.Errorf("无效的视频缩略图位置百分比: %.2f%%, 必须在0到100之间", val)
		}
		d.videoThumbPosIsPercentage = true
		d.videoThumbPos = val / 100 // 转换为小数
	} else {
		// 秒数表示
		val, err := strconv.ParseFloat(d.VideoThumbPos, 64)
		if err != nil {
			return pkgerrors.Errorf("无效的视频缩略图位置值: '%s'，错误: %v", d.VideoThumbPos, err)
		}
		if val < 0 {
			return pkgerrors.Errorf("无效的视频缩略图位置时间: %.2f秒，必须为正数", val)
		}
		d.videoThumbPosIsPercentage = false
		d.videoThumbPos = val
	}

	// 验证回收站路径
	if d.RecycleBinPath != "" && d.RecycleBinPath != "delete permanently" {
		if !utils.Exists(d.RecycleBinPath) {
			err := os.MkdirAll(d.RecycleBinPath, os.FileMode(d.mkdirPerm))
			if err != nil {
				return pkgerrors.Wrapf(err, "无法创建回收站目录 '%s'", d.RecycleBinPath)
			}
		}

		// 检查回收站目录是否可写
		testFile := filepath.Join(d.RecycleBinPath, ".write_test")
		err := os.WriteFile(testFile, []byte("test"), 0666)
		if err != nil {
			return pkgerrors.Wrapf(err, "回收站目录 '%s' 不可写", d.RecycleBinPath)
		}
		_ = os.Remove(testFile) // 删除测试文件
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
		return nil, pkgerrors.Wrapf(err, "读取目录 '%s' 失败", dirPath)
	}

	// 预分配足够的容量以避免多次扩容
	files := make([]model.Obj, 0, len(fileInfos))

	// 转换为对象列表
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
	var thumbURL string
	if d.Thumbnail {
		fileName := fileInfo.Name()
		fileType := utils.GetFileType(fileName)
		if fileType == consts.IMAGE || fileType == consts.VIDEO {
			// 构建缩略图URL
			thumbURL = common.GetApiURL(ctx) + stdpath.Join("/d", reqPath, fileName)
			thumbURL = utils.EncodePath(thumbURL, true)
			thumbURL += "?type=thumb&sign=" + sign.Sign(stdpath.Join(reqPath, fileName))
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
	fileName := fileInfo.Name()
	fullPath := filepath.Join(dirPath, fileName)
	timeInfo, err := times.Stat(fullPath)
	if err == nil && timeInfo.HasBirthTime() {
		createTime = timeInfo.BirthTime()
	}

	// 创建文件对象
	fileObj := model.ObjThumb{
		Object: model.Object{
			Path:     fullPath,
			Name:     fileName,
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

// Get 获取指定路径的文件对象
// 实现driver.Driver接口
func (d *Local) Get(ctx context.Context, path string) (model.Obj, error) {
	// 构建完整路径
	fullPath := filepath.Join(d.GetRootPath(), path)

	// 获取文件信息
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errs.ObjectNotFound
		}
		return nil, pkgerrors.Wrapf(err, "获取文件 '%s' 信息失败", fullPath)
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
			return nil, pkgerrors.Wrapf(err, "获取文件 '%s' 的缩略图失败", file.GetName())
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
				return nil, pkgerrors.Wrapf(err, "打开缩略图文件 '%s' 失败", *thumbPath)
			}
			// 获取缩略图文件大小用于Content-Length
			stat, err := fileHandle.Stat()
			if err != nil {
				fileHandle.Close()
				return nil, pkgerrors.Wrapf(err, "获取缩略图文件 '%s' 信息失败", *thumbPath)
			}
			link.ContentLength = stat.Size()
			link.MFile = fileHandle
		} else if buffer != nil {
			// 使用内存中的缩略图
			link.MFile = bytes.NewReader(buffer.Bytes())
			link.ContentLength = int64(buffer.Len())
		} else {
			return nil, fmt.Errorf("生成缩略图失败：既无缓存路径也无缓冲区数据")
		}
	} else {
		// 普通文件，直接打开
		fileHandle, err := os.Open(filePath)
		if err != nil {
			return nil, pkgerrors.Wrapf(err, "打开文件 '%s' 失败", filePath)
		}
		link.MFile = fileHandle
	}

	if link.MFile != nil && !d.Config().OnlyLinkMFile {
		link.AddIfCloser(link.MFile)
		link.RangeReader = &model.FileRangeReader{
			RangeReaderIF: stream.GetRangeReaderFromMFile(file.GetSize(), link.MFile),
		}
		link.MFile = nil
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
		return pkgerrors.Wrapf(err, "创建目录 '%s' 失败", fullPath)
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
		return fmt.Errorf("%w: '%s' -> '%s'", ErrCyclicCopy, srcPath, dstPath)
	}

	// 检查目标是否已存在
	if utils.Exists(dstPath) {
		if srcObj.IsDir() {
			return fmt.Errorf("%w: '%s'", ErrDirectoryExists, dstPath)
		}
		return fmt.Errorf("%w: '%s'", ErrFileExists, dstPath)
	}

	// 尝试直接重命名（移动）
	err := os.Rename(srcPath, dstPath)
	if err == nil {
		return nil
	}

	// 处理跨设备移动错误
	if !strings.Contains(err.Error(), "invalid cross-device link") {
		// 其他错误
		return wrapPathError(err, "移动", fmt.Sprintf("%s -> %s", srcPath, dstPath))
	}

	log.Debugf("[本地驱动] 检测到跨设备移动，使用复制+删除方式: '%s' -> '%s'", srcPath, dstPath)

	// 先复制
	if err = d.Copy(ctx, srcObj, dstDir); err != nil {
		return fmt.Errorf("跨设备复制失败: %w", err)
	}

	// 复制成功后删除源文件
	var removeErr error
	if srcObj.IsDir() {
		removeErr = os.RemoveAll(srcObj.GetPath())
	} else {
		removeErr = os.Remove(srcObj.GetPath())
	}

	if removeErr != nil {
		return wrapPathError(removeErr, "删除源文件", srcPath)
	}

	return nil
}

// Rename 重命名文件/目录
// 实现driver.Driver接口
func (d *Local) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	srcPath := srcObj.GetPath()
	dstPath := filepath.Join(filepath.Dir(srcPath), newName)

	// 检查目标是否已存在
	if utils.Exists(dstPath) {
		return pkgerrors.Errorf("目标路径 '%s' 已存在，无法重命名", dstPath)
	}

	// 执行重命名
	err := os.Rename(srcPath, dstPath)
	if err != nil {
		return pkgerrors.Wrapf(err, "重命名 '%s' 为 '%s' 失败", srcPath, newName)
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
		return fmt.Errorf("%w: '%s' -> '%s'", ErrCyclicCopy, srcPath, dstPath)
	}

	// 检查目标是否已存在
	if utils.Exists(dstPath) {
		if srcObj.IsDir() {
			return fmt.Errorf("%w: '%s'", ErrDirectoryExists, dstPath)
		}
		return fmt.Errorf("%w: '%s'", ErrFileExists, dstPath)
	}

	// 使用otiai10/copy库执行高效安全的复制
	err := cp.Copy(srcPath, dstPath, cp.Options{
		Sync:          true, // 复制后同步到磁盘，可能影响性能
		PreserveTimes: true, // 保留时间戳
		PreserveOwner: true, // 保留所有者信息
	})

	if err != nil {
		return wrapPathError(err, "复制", fmt.Sprintf("%s -> %s", srcPath, dstPath))
	}

	return nil
}

// Remove 删除文件/目录
// 实现driver.Driver接口
func (d *Local) Remove(ctx context.Context, obj model.Obj) error {
	objPath := obj.GetPath()

	// 根据配置决定是永久删除还是移动到回收站
	if utils.SliceContains([]string{"", "delete permanently"}, d.RecycleBinPath) {
		// 永久删除
		var err error
		if obj.IsDir() {
			err = os.RemoveAll(objPath)
		} else {
			err = os.Remove(objPath)
		}
		if err != nil {
			return pkgerrors.Wrapf(err, "删除 '%s' 失败", objPath)
		}
	} else {
		// 确保回收站目录存在
		if !utils.Exists(d.RecycleBinPath) {
			if err := os.MkdirAll(d.RecycleBinPath, os.FileMode(d.mkdirPerm)); err != nil {
				return pkgerrors.Wrapf(err, "创建回收站目录 '%s' 失败", d.RecycleBinPath)
			}
		}

		// 移动到回收站
		fileName := obj.GetName()
		dstPath := filepath.Join(d.RecycleBinPath, fileName)

		// 处理同名文件
		if utils.Exists(dstPath) {
			// 添加时间戳避免冲突
			dstPath = filepath.Join(d.RecycleBinPath, fileName+"_"+time.Now().Format("20060102150405"))
		}

		// 执行移动
		err := os.Rename(objPath, dstPath)
		if err != nil {
			return pkgerrors.Wrapf(err, "移动 '%s' 到回收站 '%s' 失败", objPath, dstPath)
		}
	}

	return nil
}

// Put 上传文件
// 实现driver.Driver接口
func (d *Local) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	// 构建目标文件路径
	fileName := stream.GetName()
	fullPath := filepath.Join(dstDir.GetPath(), fileName)

	// 检查目标文件是否已存在
	if utils.Exists(fullPath) && !d.Config().NoOverwriteUpload {
		// 如果允许覆盖，先删除原文件
		if err := os.Remove(fullPath); err != nil {
			return pkgerrors.Wrapf(err, "删除已存在的文件 '%s' 失败", fullPath)
		}
	}

	// 创建目标文件
	outFile, err := os.Create(fullPath)
	if err != nil {
		return pkgerrors.Wrapf(err, "创建文件 '%s' 失败", fullPath)
	}

	// 确保文件关闭并处理错误
	defer func() {
		closeErr := outFile.Close()
		if closeErr != nil && err == nil {
			err = pkgerrors.Wrapf(closeErr, "关闭文件 '%s' 失败", fullPath)
		}

		// 如果上下文取消，删除未完成的文件
		if errors.Is(err, context.Canceled) {
			_ = os.Remove(fullPath)
			log.Infof("[本地驱动] 上传取消，已删除未完成的文件: '%s'", fullPath)
		}
	}()

	// 复制数据
	err = utils.CopyWithCtx(ctx, outFile, stream, stream.GetSize(), up)
	if err != nil {
		return pkgerrors.Wrapf(err, "复制数据到文件 '%s' 失败", fullPath)
	}

	// 设置文件修改时间
	modTime := stream.ModTime()
	if !modTime.IsZero() {
		if err = os.Chtimes(fullPath, modTime, modTime); err != nil {
			// 只记录日志，不中断操作
			log.Warnf("[本地驱动] 设置文件 '%s' 时间失败: %v", fullPath, err)
		}
	}

	return nil
}

// ValidateDriver 验证驱动配置的有效性
// 实现driver.Driver接口的扩展方法
func (d *Local) ValidateDriver() error {
	// 验证根路径
	rootPath := d.GetRootPath()
	if rootPath == "" {
		return errors.New("根路径不能为空")
	}

	if !utils.Exists(rootPath) {
		return pkgerrors.Errorf("根目录 '%s' 不存在", rootPath)
	}

	// 验证根目录权限
	testFile := filepath.Join(rootPath, ".write_test")
	err := os.WriteFile(testFile, []byte("test"), 0666)
	if err != nil {
		return pkgerrors.Wrapf(err, "根目录 '%s' 不可写", rootPath)
	}
	_ = os.Remove(testFile) // 删除测试文件

	// 验证缩略图缓存目录
	if d.ThumbCacheFolder != "" && !utils.Exists(d.ThumbCacheFolder) {
		return pkgerrors.Errorf("缩略图缓存目录 '%s' 不存在", d.ThumbCacheFolder)
	}

	// 验证回收站目录
	if d.RecycleBinPath != "" &&
		d.RecycleBinPath != "delete permanently" &&
		!utils.Exists(d.RecycleBinPath) {
		return pkgerrors.Errorf("回收站目录 '%s' 不存在", d.RecycleBinPath)
	}

	// 验证缩略图质量
	if d.ThumbQuality < 1 || d.ThumbQuality > 100 {
		return pkgerrors.Errorf("缩略图质量必须在1-100之间，当前值: %d", d.ThumbQuality)
	}

	// 验证缩略图并发数
	if d.ThumbConcurrency != "" {
		v, err := strconv.Atoi(d.ThumbConcurrency)
		if err != nil {
			return pkgerrors.Wrapf(err, "无法解析缩略图并发数 '%s'", d.ThumbConcurrency)
		}
		if v < 0 {
			return pkgerrors.Errorf("缩略图并发数不能为负数: %d", v)
		}
	}

	// 验证目录权限
	if d.MkdirPerm != "" {
		_, err := strconv.ParseUint(d.MkdirPerm, 8, 32)
		if err != nil {
			return pkgerrors.Wrapf(err, "无法解析目录权限值 '%s'", d.MkdirPerm)
		}
	}

	return nil
}

// 确保Local实现了driver.Driver接口
var _ driver.Driver = (*Local)(nil)

// 更新错误处理相关函数，使用Go 1.24.5的错误处理最佳实践

// 自定义错误类型
var (
	// ErrInvalidPath 路径无效错误
	ErrInvalidPath = errors.New("无效的文件路径")

	// ErrPermissionDenied 权限拒绝错误
	ErrPermissionDenied = errors.New("权限拒绝")

	// ErrDirectoryExists 目录已存在错误
	ErrDirectoryExists = errors.New("目录已存在")

	// ErrFileExists 文件已存在错误
	ErrFileExists = errors.New("文件已存在")

	// ErrCyclicCopy 循环复制错误
	ErrCyclicCopy = errors.New("目标是源的子目录，无法操作")
)

// wrapPathError 包装路径相关错误
func wrapPathError(err error, op, path string) error {
	if err == nil {
		return nil
	}

	// 使用errors.Join组合错误
	return fmt.Errorf("%s %s: %w", op, path, err)
}

// isErrorType 检查错误是否为特定类型
func isErrorType(err, target error) bool {
	return errors.Is(err, target)
}
