package op

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/generic"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 错误类型定义
var (
	ErrStorageNotFound    = errors.New("存储不存在")
	ErrStorageInitFailed  = errors.New("存储初始化失败")
	ErrStorageDisabled    = errors.New("存储已禁用")
	ErrDriverNotSupported = errors.New("不支持的驱动类型")
	ErrInvalidMountPath   = errors.New("无效的挂载路径")
	ErrDriverTypeLocked   = errors.New("驱动类型不能更改")
)

// 存储状态常量
const (
	STATUS_WORK     = "工作中"
	STATUS_DISABLED = "已禁用"
)

// 缓存相关常量
const (
	pathCacheSize = 300 // 路径缓存大小
)

// storagesMap 维护挂载路径到存储驱动的映射
// 虽然标记为驱动，但实际上代表由驱动包装的存储
var storagesMap generic.MapOf[string, driver.Driver]

// 路径匹配缓存，用于加速频繁访问的路径查找
var (
	pathMatchCache   = make(map[string][]driver.Driver)
	pathMatchMutex   = sync.RWMutex{}
	virtualFileCache = make(map[string][]model.Obj)
	virtualFileMutex = sync.RWMutex{}
)

// GetAllStorages 返回所有活动的存储驱动
// 返回的切片是按挂载路径排序的
func GetAllStorages() []driver.Driver {
	storages := storagesMap.Values()
	slices.SortFunc(storages, func(a, b driver.Driver) int {
		return strings.Compare(a.GetStorage().MountPath, b.GetStorage().MountPath)
	})
	return storages
}

// HasStorage 检查给定挂载路径是否存在存储
func HasStorage(mountPath string) bool {
	return storagesMap.Has(utils.FixAndCleanPath(mountPath))
}

// GetStorageByMountPath 通过挂载路径获取存储驱动
func GetStorageByMountPath(mountPath string) (driver.Driver, error) {
	mountPath = utils.FixAndCleanPath(mountPath)
	storageDriver, ok := storagesMap.Load(mountPath)
	if !ok {
		return nil, errors.Wrapf(ErrStorageNotFound, "挂载路径: %s", mountPath)
	}
	return storageDriver, nil
}

// CreateStorage 将存储配置保存到数据库并实例化驱动
// 返回存储ID和可能发生的错误
func CreateStorage(ctx context.Context, storage model.Storage) (uint, error) {
	// 设置修改时间和清理挂载路径
	storage.Modified = time.Now()
	storage.MountPath = utils.FixAndCleanPath(storage.MountPath)

	// 检查挂载路径是否有效
	if storage.MountPath == "" {
		return 0, errors.Wrap(ErrInvalidMountPath, "挂载路径不能为空")
	}

	// 检查驱动是否存在
	driverName := storage.Driver
	driverConstructor, err := GetDriver(driverName)
	if err != nil {
		return 0, errors.Wrapf(ErrDriverNotSupported, "获取驱动构造器失败: %v", err)
	}

	// 创建存储驱动实例
	storageDriver := driverConstructor()

	// 将存储插入数据库
	if err = db.CreateStorage(&storage); err != nil {
		return storage.ID, errors.Wrapf(err, "在数据库中创建存储失败")
	}

	// 使用其驱动初始化存储
	if err = initStorage(ctx, storage, storageDriver); err != nil {
		log.WithFields(log.Fields{
			"mount_path": storage.MountPath,
			"driver":     storage.Driver,
			"error":      err,
		}).Warn("存储已在数据库中创建但初始化失败")
	}

	// 异步调用钩子以避免阻塞
	go CallStorageHooks("add", storageDriver)

	log.WithFields(log.Fields{
		"id":         storage.ID,
		"mount_path": storage.MountPath,
		"driver":     storage.Driver,
	}).Debug("存储创建成功")

	// 清除路径缓存
	clearPathCache()

	return storage.ID, err
}

// LoadStorage 从数据库加载现有存储到内存
func LoadStorage(ctx context.Context, storage model.Storage) error {
	// 清理挂载路径
	storage.MountPath = utils.FixAndCleanPath(storage.MountPath)

	// 检查驱动是否存在
	driverName := storage.Driver
	driverConstructor, err := GetDriver(driverName)
	if err != nil {
		return errors.Wrapf(ErrDriverNotSupported, "获取驱动构造器失败: %v", err)
	}

	// 创建存储驱动实例
	storageDriver := driverConstructor()

	// 初始化存储
	if err = initStorage(ctx, storage, storageDriver); err != nil {
		return errors.Wrapf(err, "初始化存储失败: %s", storage.MountPath)
	}

	// 异步调用钩子
	go CallStorageHooks("add", storageDriver)

	log.WithFields(log.Fields{
		"id":         storage.ID,
		"mount_path": storage.MountPath,
		"driver":     storage.Driver,
	}).Debug("存储加载成功")

	// 清除路径缓存
	clearPathCache()

	return nil
}

// getCurrentGoroutineStack 捕获当前goroutine的堆栈跟踪
// 在发生panic时用于调试
func getCurrentGoroutineStack() string {
	const stackSize = 1 << 16 // 64KB
	buf := make([]byte, stackSize)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// initStorage 使用其配置初始化存储驱动
// 此函数对panic具有弹性，如果发生panic将保存错误状态
func initStorage(ctx context.Context, storage model.Storage, storageDriver driver.Driver) (err error) {
	// 设置存储信息
	storageDriver.SetStorage(storage)
	driverStorage := storageDriver.GetStorage()

	// 捕获初始化期间的panic
	defer func() {
		if r := recover(); r != nil {
			errInfo := fmt.Sprintf("[panic] err: %v\nstack: %s\n", r, getCurrentGoroutineStack())
			log.WithFields(log.Fields{
				"mount_path": driverStorage.MountPath,
				"driver":     driverStorage.Driver,
				"error":      errInfo,
			}).Error("存储初始化过程中发生panic")

			driverStorage.SetStatus(errInfo)
			MustSaveDriverStorage(storageDriver)
			storagesMap.Store(driverStorage.MountPath, storageDriver)
			err = errors.Wrap(ErrStorageInitFailed, errInfo)
		}
	}()

	// 解析额外配置
	err = utils.JSONTool.UnmarshalFromString(driverStorage.Addition, storageDriver.GetAddition())
	if err == nil {
		// 处理引用存储
		if ref, ok := storageDriver.(driver.Reference); ok {
			if strings.HasPrefix(driverStorage.Remark, "ref:/") {
				refMountPath := driverStorage.Remark
				i := strings.Index(refMountPath, "\n")
				if i > 0 {
					refMountPath = refMountPath[4:i]
				} else {
					refMountPath = refMountPath[4:]
				}

				refStorage, refErr := GetStorageByMountPath(refMountPath)
				if refErr != nil {
					err = errors.Wrapf(refErr, "引用错误")
				} else {
					if initErr := ref.InitReference(refStorage); initErr != nil {
						if errs.IsNotSupportError(initErr) {
							err = errors.Errorf("引用错误: 存储与 %s 不兼容", storageDriver.Config().Name)
						} else {
							err = errors.Wrap(initErr, "初始化引用失败")
						}
					}
				}
			}
		}
	} else {
		err = errors.Wrap(err, "解析额外配置失败")
	}

	// 如果没有错误，初始化驱动
	if err == nil {
		err = storageDriver.Init(ctx)
		if err != nil {
			err = errors.Wrap(err, "驱动初始化失败")
		}
	}

	// 无论初始化状态如何，都将驱动存储在内存中
	storagesMap.Store(driverStorage.MountPath, storageDriver)

	// 根据初始化结果更新状态
	if err != nil {
		if IsUseOnlineAPI(storageDriver) {
			driverStorage.SetStatus(utils.SanitizeHTML(err.Error()))
		} else {
			driverStorage.SetStatus(err.Error())
		}
		err = errors.Wrap(err, "failed init storage")
	} else {
		driverStorage.SetStatus(STATUS_WORK)
	}

	// 将更新的状态保存到数据库
	MustSaveDriverStorage(storageDriver)
	return err
}

func IsUseOnlineAPI(storageDriver driver.Driver) bool {
	v := reflect.ValueOf(storageDriver.GetAddition())
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false
	}
	field := v.FieldByName("UseOnlineAPI")
	if !field.IsValid() {
		return false
	}
	if field.Kind() != reflect.Bool {
		return false
	}
	return field.Bool()
}

// EnableStorage 启用之前禁用的存储
func EnableStorage(ctx context.Context, id uint) error {
	// 获取存储信息
	storage, err := db.GetStorageByID(id)
	if err != nil {
		return errors.Wrapf(err, "获取存储失败: ID=%d", id)
	}

	// 检查存储是否已启用
	if !storage.Disabled {
		return errors.New("存储已经启用")
	}

	// 更新数据库中的状态
	storage.Disabled = false
	if err = db.UpdateStorage(storage); err != nil {
		return errors.Wrap(err, "在数据库中更新存储失败")
	}

	// 加载存储到内存
	if err = LoadStorage(ctx, *storage); err != nil {
		return errors.Wrap(err, "加载存储失败")
	}

	// 清除路径缓存
	clearPathCache()

	return nil
}

// DisableStorage 禁用活动存储
func DisableStorage(ctx context.Context, id uint) error {
	// 获取存储信息
	storage, err := db.GetStorageByID(id)
	if err != nil {
		return errors.Wrapf(err, "获取存储失败: ID=%d", id)
	}

	// 检查存储是否已禁用
	if storage.Disabled {
		return errors.New("存储已经禁用")
	}

	// 获取存储驱动
	storageDriver, err := GetStorageByMountPath(storage.MountPath)
	if err != nil {
		return errors.Wrap(err, "获取存储驱动失败")
	}

	// 从驱动中删除存储
	if err = storageDriver.Drop(ctx); err != nil {
		log.WithFields(log.Fields{
			"id":         id,
			"mount_path": storage.MountPath,
			"error":      err,
		}).Warn("删除存储时出现错误")
		// 继续处理，不返回错误
	}

	// 更新状态并保存到数据库
	storage.Disabled = true
	storage.SetStatus(STATUS_DISABLED)
	if err = db.UpdateStorage(storage); err != nil {
		return errors.Wrap(err, "在数据库中更新存储失败")
	}

	// 从内存中移除
	storagesMap.Delete(storage.MountPath)

	// 异步调用钩子
	go CallStorageHooks("del", storageDriver)

	// 清除路径缓存
	clearPathCache()

	return nil
}

// UpdateStorage 更新现有存储配置
// 包括使用新配置重新初始化驱动
func UpdateStorage(ctx context.Context, storage model.Storage) error {
	// 获取旧存储信息
	oldStorage, err := db.GetStorageByID(storage.ID)
	if err != nil {
		return errors.Wrapf(err, "获取旧存储失败: ID=%d", storage.ID)
	}

	// 驱动类型不能更改
	if oldStorage.Driver != storage.Driver {
		return ErrDriverTypeLocked
	}

	// 更新修改时间和清理挂载路径
	storage.Modified = time.Now()
	storage.MountPath = utils.FixAndCleanPath(storage.MountPath)

	// 更新数据库
	if err = db.UpdateStorage(&storage); err != nil {
		return errors.Wrap(err, "在数据库中更新存储失败")
	}

	// 如果禁用，无需更新内存
	if storage.Disabled {
		return nil
	}

	// 获取当前驱动
	storageDriver, err := GetStorageByMountPath(oldStorage.MountPath)
	if err != nil {
		return errors.Wrap(err, "获取存储驱动失败")
	}

	// 如果挂载路径已更改，删除旧路径
	if oldStorage.MountPath != storage.MountPath {
		storagesMap.Delete(oldStorage.MountPath)
	}

	// 删除当前实例
	if err = storageDriver.Drop(ctx); err != nil {
		log.WithFields(log.Fields{
			"id":         storage.ID,
			"mount_path": oldStorage.MountPath,
			"error":      err,
		}).Warn("删除存储时出现错误")
		// 继续处理，不返回错误
	}

	// 使用新配置重新初始化
	if err = initStorage(ctx, storage, storageDriver); err != nil {
		return errors.Wrap(err, "重新初始化存储失败")
	}

	// 异步调用钩子
	go CallStorageHooks("update", storageDriver)

	log.WithFields(log.Fields{
		"id":         storage.ID,
		"mount_path": storage.MountPath,
		"driver":     storage.Driver,
	}).Debug("存储更新成功")

	// 清除路径缓存
	clearPathCache()

	return nil
}

// DeleteStorageByID 完全删除存储
func DeleteStorageByID(ctx context.Context, id uint) error {
	// 获取存储信息
	storage, err := db.GetStorageByID(id)
	if err != nil {
		return errors.Wrapf(err, "获取存储失败: ID=%d", id)
	}

	// 如果存储处于活动状态，先删除它
	if !storage.Disabled {
		var storageDriver driver.Driver
		storageDriver, err = GetStorageByMountPath(storage.MountPath)
		if err != nil {
			return errors.Wrap(err, "获取存储驱动失败")
		}

		// 从驱动中删除存储
		if err = storageDriver.Drop(ctx); err != nil {
			log.WithFields(log.Fields{
				"id":         id,
				"mount_path": storage.MountPath,
				"error":      err,
			}).Warn("删除存储时出现错误")
			// 继续处理，不返回错误
		}

		// 从内存中移除
		storagesMap.Delete(storage.MountPath)

		// 异步调用钩子
		go CallStorageHooks("del", storageDriver)
	}

	// 从数据库删除
	if err = db.DeleteStorageByID(id); err != nil {
		return errors.Wrapf(err, "从数据库中删除存储失败: ID=%d", id)
	}

	// 清除路径缓存
	clearPathCache()

	return nil
}

// MustSaveDriverStorage 保存驱动存储配置
// 并记录任何错误而不返回它们
func MustSaveDriverStorage(driver driver.Driver) {
	if err := saveDriverStorage(driver); err != nil {
		log.WithFields(log.Fields{
			"mount_path": driver.GetStorage().MountPath,
			"driver":     driver.GetStorage().Driver,
			"error":      err,
		}).Error("保存驱动存储失败")
	}
}

// saveDriverStorage 将驱动的存储配置保存到数据库
func saveDriverStorage(driver driver.Driver) error {
	storage := driver.GetStorage()
	addition := driver.GetAddition()

	// 序列化附加数据
	additionJSON, err := utils.JSONTool.MarshalToString(addition)
	if err != nil {
		return errors.Wrap(err, "序列化附加数据失败")
	}

	storage.Addition = additionJSON

	// 更新数据库
	if err = db.UpdateStorage(storage); err != nil {
		return errors.Wrap(err, "在数据库中更新存储失败")
	}

	return nil
}

// clearPathCache 清除路径匹配和虚拟文件缓存
func clearPathCache() {
	pathMatchMutex.Lock()
	pathMatchCache = make(map[string][]driver.Driver)
	pathMatchMutex.Unlock()

	virtualFileMutex.Lock()
	virtualFileCache = make(map[string][]model.Obj)
	virtualFileMutex.Unlock()
}

// getStoragesByPath 通过最长匹配路径获取存储，包括平衡存储
// 例如，给定路径 /a/b, /a/c, /a/d/e, 和 /a/d/e.balance:
// getStoragesByPath(/a/d/e/f) => [/a/d/e, /a/d/e.balance]
func getStoragesByPath(path string) []driver.Driver {
	path = utils.FixAndCleanPath(path)

	// 检查缓存
	pathMatchMutex.RLock()
	if storages, ok := pathMatchCache[path]; ok {
		pathMatchMutex.RUnlock()
		return slices.Clone(storages) // 返回副本以防止修改
	}
	pathMatchMutex.RUnlock()

	storages := make([]driver.Driver, 0)
	curSlashCount := 0

	storagesMap.Range(func(mountPath string, value driver.Driver) bool {
		actualMountPath := utils.GetActualMountPath(mountPath)

		// 检查此路径是否是请求路径的父级
		if utils.IsSubPath(actualMountPath, path) {
			slashCount := strings.Count(utils.PathAddSeparatorSuffix(actualMountPath), "/")

			// 如果这是更长的匹配，替换当前匹配
			if slashCount > curSlashCount {
				storages = storages[:0]
				curSlashCount = slashCount
			}

			// 如果这与当前匹配处于同一级别，添加它
			if slashCount == curSlashCount {
				storages = append(storages, value)
			}
		}
		return true
	})

	// 对存储进行排序以获得一致的结果
	sort.Slice(storages, func(i, j int) bool {
		return storages[i].GetStorage().MountPath < storages[j].GetStorage().MountPath
	})

	// 更新缓存（如果缓存未满）
	pathMatchMutex.Lock()
	if len(pathMatchCache) < pathCacheSize {
		pathMatchCache[path] = slices.Clone(storages)
	}
	pathMatchMutex.Unlock()

	return storages
}

// GetStorageVirtualFilesByPath 返回路径下存储生成的虚拟文件/文件夹
// 例如，给定存储位置：/a/b, /a/c, /a/d/e, /a/b.balance1, /av
// GetStorageVirtualFilesByPath(/a) => 虚拟文件夹 b, c, d
func GetStorageVirtualFilesByPath(prefix string) []model.Obj {
	prefix = utils.FixAndCleanPath(prefix)

	// 检查缓存
	virtualFileMutex.RLock()
	if files, ok := virtualFileCache[prefix]; ok {
		virtualFileMutex.RUnlock()
		return slices.Clone(files) // 返回副本以防止修改
	}
	virtualFileMutex.RUnlock()

	files := make([]model.Obj, 0)
	storages := storagesMap.Values()

	// 按顺序然后按挂载路径对存储进行排序
	sort.Slice(storages, func(i, j int) bool {
		if storages[i].GetStorage().Order == storages[j].GetStorage().Order {
			return storages[i].GetStorage().MountPath < storages[j].GetStorage().MountPath
		}
		return storages[i].GetStorage().Order < storages[j].GetStorage().Order
	})

	uniqueNames := mapset.NewSet[string]()

	// 查找前缀下所有唯一的一级文件夹
	for _, storage := range storages {
		mountPath := utils.GetActualMountPath(storage.GetStorage().MountPath)

		// 跳过不在前缀下的路径
		if len(prefix) >= len(mountPath) || !utils.IsSubPath(prefix, mountPath) {
			continue
		}

		// 提取前缀下的第一个文件夹名称
		relativePath := mountPath[len(prefix):]
		if relativePath[0] == '/' {
			relativePath = relativePath[1:]
		}

		firstDir := strings.Split(relativePath, "/")[0]

		// 添加唯一的文件夹名称
		if uniqueNames.Add(firstDir) {
			files = append(files, &model.Object{
				Name:     firstDir,
				Size:     0,
				Modified: storage.GetStorage().Modified,
				IsFolder: true,
			})
		}
	}

	// 更新缓存（如果缓存未满）
	virtualFileMutex.Lock()
	if len(virtualFileCache) < pathCacheSize {
		virtualFileCache[prefix] = slices.Clone(files)
	}
	virtualFileMutex.Unlock()

	return files
}

// balanceMap 跟踪轮询状态，用于多个存储的负载均衡
var (
	balanceMap   generic.MapOf[string, int]
	balanceMutex sync.Mutex
)

// GetBalancedStorage 返回路径的存储，如果存在多个则使用轮询
func GetBalancedStorage(path string) driver.Driver {
	path = utils.FixAndCleanPath(path)
	storages := getStoragesByPath(path)

	switch len(storages) {
	case 0:
		return nil
	case 1:
		return storages[0]
	default:
		// 获取这些存储的虚拟路径（公共前缀）
		virtualPath := utils.GetActualMountPath(storages[0].GetStorage().MountPath)

		// 线程安全地获取和更新计数器
		balanceMutex.Lock()
		defer balanceMutex.Unlock()

		// 加载或初始化轮询计数器
		counter, _ := balanceMap.LoadOrStore(virtualPath, 0)

		// 增加计数器并环绕
		nextCounter := (counter + 1) % len(storages)
		balanceMap.Store(virtualPath, nextCounter)

		return storages[counter]
	}
}

// ResetStorageCaches 重置所有存储相关的缓存
// 这在配置更改或需要强制刷新缓存时很有用
func ResetStorageCaches() {
	clearPathCache()

	balanceMutex.Lock()
	balanceMap = generic.MapOf[string, int]{}
	balanceMutex.Unlock()

	log.Debug("存储缓存已重置")
}

// GetStorageStats 返回存储统计信息
// 包括活动存储数量、禁用存储数量和总存储数量
func GetStorageStats() map[string]int {
	storages := storagesMap.Values()
	stats := map[string]int{
		"total":    len(storages),
		"active":   0,
		"disabled": 0,
	}

	for _, s := range storages {
		if s.GetStorage().Disabled {
			stats["disabled"]++
		} else {
			stats["active"]++
		}
	}

	return stats
}