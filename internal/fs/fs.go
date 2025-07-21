// 添加驱动信息缓存策略和配置项优化

package fs

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/task"
)

// 文件系统操作包
// 本包的目的是将挂载路径转换为实际路径，然后将实际路径传递给op包

// 驱动信息缓存
var (
	// 存储驱动缓存
	storageDriverCache     = make(map[string]driver.Driver)
	storageDriverCacheMu   sync.RWMutex
	storageDriverCacheTTL  = 5 * time.Minute
	storageDriverCacheTime = make(map[string]time.Time)

	// 对象缓存
	objectCache     = make(map[string]model.Obj)
	objectCacheMu   sync.RWMutex
	objectCacheTTL  = 30 * time.Second
	objectCacheTime = make(map[string]time.Time)
)

// Config 配置选项
type Config struct {
	// 驱动缓存TTL
	DriverCacheTTL time.Duration

	// 对象缓存TTL
	ObjectCacheTTL time.Duration

	// 是否启用驱动缓存
	EnableDriverCache bool

	// 是否启用对象缓存
	EnableObjectCache bool
}

// DefaultConfig 默认配置
var DefaultConfig = Config{
	DriverCacheTTL:    5 * time.Minute,
	ObjectCacheTTL:    30 * time.Second,
	EnableDriverCache: true,
	EnableObjectCache: true,
}

// currentConfig 当前配置
var currentConfig = DefaultConfig

// SetConfig 设置文件系统配置
func SetConfig(config Config) {
	storageDriverCacheMu.Lock()
	objectCacheMu.Lock()
	defer storageDriverCacheMu.Unlock()
	defer objectCacheMu.Unlock()

	currentConfig = config

	// 更新缓存TTL
	storageDriverCacheTTL = config.DriverCacheTTL
	objectCacheTTL = config.ObjectCacheTTL

	// 如果禁用缓存，则清空缓存
	if !config.EnableDriverCache {
		storageDriverCache = make(map[string]driver.Driver)
		storageDriverCacheTime = make(map[string]time.Time)
	}

	if !config.EnableObjectCache {
		objectCache = make(map[string]model.Obj)
		objectCacheTime = make(map[string]time.Time)
	}
}

// GetConfig 获取当前配置
func GetConfig() Config {
	return currentConfig
}

// ClearCache 清除所有缓存
func ClearCache() {
	storageDriverCacheMu.Lock()
	objectCacheMu.Lock()
	defer storageDriverCacheMu.Unlock()
	defer objectCacheMu.Unlock()

	storageDriverCache = make(map[string]driver.Driver)
	storageDriverCacheTime = make(map[string]time.Time)
	objectCache = make(map[string]model.Obj)
	objectCacheTime = make(map[string]time.Time)

	log.Info("文件系统缓存已清除")
}

// 定期清理过期缓存
func init() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			cleanExpiredCache()
		}
	}()
}

// cleanExpiredCache 清理过期缓存
func cleanExpiredCache() {
	now := time.Now()

	// 清理驱动缓存
	storageDriverCacheMu.Lock()
	for path, cacheTime := range storageDriverCacheTime {
		if now.Sub(cacheTime) > storageDriverCacheTTL {
			delete(storageDriverCache, path)
			delete(storageDriverCacheTime, path)
		}
	}
	storageDriverCacheMu.Unlock()

	// 清理对象缓存
	objectCacheMu.Lock()
	for path, cacheTime := range objectCacheTime {
		if now.Sub(cacheTime) > objectCacheTTL {
			delete(objectCache, path)
			delete(objectCacheTime, path)
		}
	}
	objectCacheMu.Unlock()
}

// getCachedStorage 从缓存获取存储驱动
func getCachedStorage(path string) (driver.Driver, bool) {
	if !currentConfig.EnableDriverCache {
		return nil, false
	}

	storageDriverCacheMu.RLock()
	defer storageDriverCacheMu.RUnlock()

	storage, ok := storageDriverCache[path]
	if !ok {
		return nil, false
	}

	// 检查缓存是否过期
	cacheTime, ok := storageDriverCacheTime[path]
	if !ok || time.Since(cacheTime) > storageDriverCacheTTL {
		return nil, false
	}

	return storage, true
}

// setCachedStorage 设置存储驱动缓存
func setCachedStorage(path string, storage driver.Driver) {
	if !currentConfig.EnableDriverCache {
		return
	}

	storageDriverCacheMu.Lock()
	defer storageDriverCacheMu.Unlock()

	storageDriverCache[path] = storage
	storageDriverCacheTime[path] = time.Now()
}

// getCachedObject 从缓存获取对象
func getCachedObject(path string) (model.Obj, bool) {
	if !currentConfig.EnableObjectCache {
		return nil, false
	}

	objectCacheMu.RLock()
	defer objectCacheMu.RUnlock()

	obj, ok := objectCache[path]
	if !ok {
		return nil, false
	}

	// 检查缓存是否过期
	cacheTime, ok := objectCacheTime[path]
	if !ok || time.Since(cacheTime) > objectCacheTTL {
		return nil, false
	}

	return obj, true
}

// setCachedObject 设置对象缓存
func setCachedObject(path string, obj model.Obj) {
	if !currentConfig.EnableObjectCache {
		return
	}

	objectCacheMu.Lock()
	defer objectCacheMu.Unlock()

	objectCache[path] = obj
	objectCacheTime[path] = time.Now()
}

// ListArgs 列表参数结构体
type ListArgs struct {
	Refresh bool // 是否刷新缓存
	NoLog   bool // 是否不记录日志
}

// List 列出指定路径下的对象
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 列表参数
//
// 返回:
//   - []model.Obj: 对象列表
//   - error: 错误信息
func List(ctx context.Context, path string, args *ListArgs) ([]model.Obj, error) {
	if args == nil {
		args = &ListArgs{}
	}

	res, err := list(ctx, path, args)
	if err != nil && !args.NoLog {
		log.Errorf("failed to list %s: %+v", path, err)
	}
	return res, err
}

// GetArgs 获取参数结构体
type GetArgs struct {
	NoLog   bool // 是否不记录日志
	Refresh bool // 是否刷新缓存
}

// Get 获取指定路径的对象
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 获取参数
//
// 返回:
//   - model.Obj: 对象信息
//   - error: 错误信息
func Get(ctx context.Context, path string, args *GetArgs) (model.Obj, error) {
	if args == nil {
		args = &GetArgs{}
	}

	// 如果不刷新缓存，尝试从缓存获取
	if !args.Refresh {
		if obj, ok := getCachedObject(path); ok {
			return obj, nil
		}
	}

	res, err := get(ctx, path)
	if err != nil {
		if !args.NoLog {
			log.Warnf("failed to get %s: %s", path, err)
		}
		return nil, err
	}

	// 缓存对象
	setCachedObject(path, res)

	return res, nil
}

// Link 获取指定路径对象的链接
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 链接参数
//
// 返回:
//   - *model.Link: 链接信息
//   - model.Obj: 对象信息
//   - error: 错误信息
func Link(ctx context.Context, path string, args model.LinkArgs) (*model.Link, model.Obj, error) {
	res, file, err := link(ctx, path, args)
	if err != nil {
		log.Errorf("failed to link %s: %+v", path, err)
	}
	return res, file, err
}

// MakeDir 创建目录
// 参数:
//   - ctx: 上下文
//   - path: 目录路径
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func MakeDir(ctx context.Context, path string, lazyCache ...bool) error {
	err := makeDir(ctx, path, lazyCache...)
	if err != nil {
		log.Errorf("failed to make directory %s: %+v", path, err)
	}
	return err
}

// Move 移动文件或目录
// 参数:
//   - ctx: 上下文
//   - srcPath: 源路径
//   - dstDirPath: 目标目录路径
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func Move(ctx context.Context, srcPath, dstDirPath string, lazyCache ...bool) error {
	err := move(ctx, srcPath, dstDirPath, lazyCache...)
	if err != nil {
		log.Errorf("failed to move %s to %s: %+v", srcPath, dstDirPath, err)
	}

	// 移动成功后，清除相关缓存
	if err == nil {
		objectCacheMu.Lock()
		delete(objectCache, srcPath)
		delete(objectCacheTime, srcPath)
		objectCacheMu.Unlock()
	}

	return err
}

// MoveWithTask 创建移动任务
// 参数:
//   - ctx: 上下文
//   - srcPath: 源路径
//   - dstDirPath: 目标目录路径
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func MoveWithTask(ctx context.Context, srcPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	res, err := _move(ctx, srcPath, dstDirPath, lazyCache...)
	if err != nil {
		log.Errorf("failed to move %s to %s: %+v", srcPath, dstDirPath, err)
	}
	return res, err
}

// MoveWithTaskAndValidation 创建带验证的移动任务
// 参数:
//   - ctx: 上下文
//   - srcPath: 源路径
//   - dstDirPath: 目标目录路径
//   - validateExistence: 是否验证目标存在性
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func MoveWithTaskAndValidation(ctx context.Context, srcPath, dstDirPath string, validateExistence bool, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	res, err := _moveWithValidation(ctx, srcPath, dstDirPath, validateExistence, lazyCache...)
	if err != nil {
		log.Errorf("failed to move %s to %s: %+v", srcPath, dstDirPath, err)
	}
	return res, err
}

// Copy 复制文件或目录
// 参数:
//   - ctx: 上下文
//   - srcObjPath: 源对象路径
//   - dstDirPath: 目标目录路径
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func Copy(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	res, err := _copy(ctx, srcObjPath, dstDirPath, lazyCache...)
	if err != nil {
		log.Errorf("failed to copy %s to %s: %+v", srcObjPath, dstDirPath, err)
	}
	return res, err
}

// Rename 重命名文件或目录
// 参数:
//   - ctx: 上下文
//   - srcPath: 源路径
//   - dstName: 新名称
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func Rename(ctx context.Context, srcPath, dstName string, lazyCache ...bool) error {
	err := rename(ctx, srcPath, dstName, lazyCache...)
	if err != nil {
		log.Errorf("failed to rename %s to %s: %+v", srcPath, dstName, err)
	}

	// 重命名成功后，清除相关缓存
	if err == nil {
		objectCacheMu.Lock()
		delete(objectCache, srcPath)
		delete(objectCacheTime, srcPath)
		objectCacheMu.Unlock()
	}

	return err
}

// Remove 删除文件或目录
// 参数:
//   - ctx: 上下文
//   - path: 路径
//
// 返回:
//   - error: 错误信息
func Remove(ctx context.Context, path string) error {
	err := remove(ctx, path)
	if err != nil {
		log.Errorf("failed to remove %s: %+v", path, err)
	}

	// 删除成功后，清除相关缓存
	if err == nil {
		objectCacheMu.Lock()
		delete(objectCache, path)
		delete(objectCacheTime, path)
		objectCacheMu.Unlock()
	}

	return err
}

// PutDirectly 直接上传文件
// 参数:
//   - ctx: 上下文
//   - dstDirPath: 目标目录路径
//   - file: 文件流
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func PutDirectly(ctx context.Context, dstDirPath string, file model.FileStreamer, lazyCache ...bool) error {
	err := putDirectly(ctx, dstDirPath, file, lazyCache...)
	if err != nil {
		log.Errorf("failed to put file to %s: %+v", dstDirPath, err)
	}
	return err
}

// PutAsTask 创建上传任务
// 参数:
//   - ctx: 上下文
//   - dstDirPath: 目标目录路径
//   - file: 文件流
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func PutAsTask(ctx context.Context, dstDirPath string, file model.FileStreamer) (task.TaskExtensionInfo, error) {
	t, err := putAsTask(ctx, dstDirPath, file)
	if err != nil {
		log.Errorf("failed to put file to %s: %+v", dstDirPath, err)
	}
	return t, err
}

// ArchiveMeta 获取归档元数据
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档元数据参数
//
// 返回:
//   - *model.ArchiveMetaProvider: 归档元数据提供者
//   - error: 错误信息
func ArchiveMeta(ctx context.Context, path string, args model.ArchiveMetaArgs) (*model.ArchiveMetaProvider, error) {
	meta, err := archiveMeta(ctx, path, args)
	if err != nil {
		log.Errorf("failed to get archive metadata for %s: %+v", path, err)
	}
	return meta, err
}

// ArchiveList 列出归档内容
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档列表参数
//
// 返回:
//   - []model.Obj: 对象列表
//   - error: 错误信息
func ArchiveList(ctx context.Context, path string, args model.ArchiveListArgs) ([]model.Obj, error) {
	objs, err := archiveList(ctx, path, args)
	if err != nil {
		log.Errorf("failed to list archive [%s]%s: %+v", path, args.InnerPath, err)
	}
	return objs, err
}

// ArchiveDecompress 解压归档
// 参数:
//   - ctx: 上下文
//   - srcObjPath: 源对象路径
//   - dstDirPath: 目标目录路径
//   - args: 归档解压参数
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func ArchiveDecompress(ctx context.Context, srcObjPath, dstDirPath string, args model.ArchiveDecompressArgs, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	t, err := archiveDecompress(ctx, srcObjPath, dstDirPath, args, lazyCache...)
	if err != nil {
		log.Errorf("failed to decompress [%s]%s: %+v", srcObjPath, args.InnerPath, err)
	}
	return t, err
}

// ArchiveDriverExtract 提取归档内容
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档内部参数
//
// 返回:
//   - *model.Link: 链接信息
//   - model.Obj: 对象信息
//   - error: 错误信息
func ArchiveDriverExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (*model.Link, model.Obj, error) {
	l, obj, err := archiveDriverExtract(ctx, path, args)
	if err != nil {
		log.Errorf("failed to extract [%s]%s: %+v", path, args.InnerPath, err)
	}
	return l, obj, err
}

// ArchiveInternalExtract 内部提取归档内容
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档内部参数
//
// 返回:
//   - io.ReadCloser: 读取器
//   - int64: 大小
//   - error: 错误信息
func ArchiveInternalExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (io.ReadCloser, int64, error) {
	reader, size, err := archiveInternalExtract(ctx, path, args)
	if err != nil {
		log.Errorf("failed to extract [%s]%s: %+v", path, args.InnerPath, err)
	}
	return reader, size, err
}

// GetStoragesArgs 获取存储参数结构体
type GetStoragesArgs struct {
	Refresh bool // 是否刷新缓存
}

// GetStorage 获取存储驱动
// 参数:
//   - path: 路径
//   - args: 获取存储参数
//
// 返回:
//   - driver.Driver: 存储驱动
//   - error: 错误信息
func GetStorage(path string, args *GetStoragesArgs) (driver.Driver, error) {
	if args == nil {
		args = &GetStoragesArgs{}
	}

	// 如果不刷新缓存，尝试从缓存获取
	if !args.Refresh {
		if storage, ok := getCachedStorage(path); ok {
			return storage, nil
		}
	}

	// 获取存储驱动
	storageDriver, _, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, errors.Wrap(ErrStorageNotFound, err.Error())
	}

	// 缓存存储驱动
	setCachedStorage(path, storageDriver)

	return storageDriver, nil
}

// Other 执行其他文件系统操作
// 参数:
//   - ctx: 上下文
//   - args: 其他操作参数
//
// 返回:
//   - any: 操作结果
//   - error: 错误信息
func Other(ctx context.Context, args model.FsOtherArgs) (any, error) {
	res, err := other(ctx, args)
	if err != nil {
		log.Errorf("failed to execute other operation on %s: %+v", args.Path, err)
	}
	return res, err
}

// PutURL 从URL上传文件
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - dstName: 目标名称
//   - urlStr: URL字符串
//
// 返回:
//   - error: 错误信息
func PutURL(ctx context.Context, path, dstName, urlStr string) error {
	// 获取存储和实际路径
	storage, dstDirActualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return errors.Wrap(ErrStorageNotFound, err.Error())
	}

	// 检查存储是否支持上传
	if storage.Config().NoUpload {
		return errors.Wrap(errs.UploadNotSupported, "storage does not support uploads")
	}

	// 检查存储是否实现了PutURL或PutURLResult接口
	_, isPutURL := storage.(driver.PutURL)
	_, isPutURLResult := storage.(driver.PutURLResult)
	if !isPutURL && !isPutURLResult {
		return errors.Wrap(errs.NotImplement, "storage does not implement PutURL")
	}

	// 执行URL上传
	return op.PutURL(ctx, storage, dstDirActualPath, dstName, urlStr)
}
