package fs

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	stdpath "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/OpenListTeam/tache"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/task_group"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/task"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 自定义错误类型
var (
	ErrArchiveMetaFailed       = errors.New("failed to get archive metadata")
	ErrArchiveListFailed       = errors.New("failed to list archive contents")
	ErrArchiveDecompressFailed = errors.New("failed to decompress archive")
	ErrArchiveExtractFailed    = errors.New("failed to extract from archive")
	ErrTempFileCreationFailed  = errors.New("failed to create temporary file")
)

// ArchiveDownloadTask 归档下载任务结构
type ArchiveDownloadTask struct {
	TaskData
	model.ArchiveDecompressArgs
}

// GetName 获取任务名称
// 返回:
//   - string: 任务名称
func (t *ArchiveDownloadTask) GetName() string {
	return fmt.Sprintf("decompress [%s](%s)[%s] to [%s](%s) with password <%s>", t.SrcStorageMp, t.SrcActualPath,
		t.InnerPath, t.DstStorageMp, t.DstActualPath, t.Password)
}

// Run 执行任务
// 返回:
//   - error: 错误信息
func (t *ArchiveDownloadTask) Run() error {
	// 重新初始化上下文
	if err := t.ReinitCtx(); err != nil {
		return err
	}

	// 初始化任务时间
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	// 执行任务并获取上传任务
	uploadTask, err := t.RunWithoutPushUploadTask()
	if err != nil {
		return err
	}

	// 设置上传任务组ID并添加任务
	uploadTask.groupID = stdpath.Join(uploadTask.DstStorageMp, uploadTask.DstActualPath)
	task_group.TransferCoordinator.AddTask(uploadTask.groupID, nil)
	ArchiveContentUploadTaskManager.Add(uploadTask)

	return nil
}

// RunWithoutPushUploadTask 执行任务但不添加上传任务
// 返回:
//   - *ArchiveContentUploadTask: 上传任务
//   - error: 错误信息
func (t *ArchiveDownloadTask) RunWithoutPushUploadTask() (*ArchiveContentUploadTask, error) {

	// 获取归档工具和流
	srcObj, tool, ss, err := op.GetArchiveToolAndStream(t.Ctx(), t.SrcStorage, t.SrcActualPath, model.LinkArgs{})
	if err != nil {
		return nil, errors.Wrap(ErrArchiveDecompressFailed, err.Error())
	}

	// 确保流被关闭
	defer func() {
		var e error
		for _, s := range ss {
			e = stderrors.Join(e, s.Close())
		}
		if e != nil {
			log.Errorf("failed to close file streamer, %v", e)
		}
	}()

	// 处理进度更新函数
	var decompressUp model.UpdateProgress
	if t.CacheFull {
		// 计算总大小
		var total, cur int64 = 0, 0
		for _, s := range ss {
			total += s.GetSize()
		}
		t.SetTotalBytes(total)
		t.Status = "getting src object"

		// 缓存每个流
		for _, s := range ss {
			if s.GetFile() == nil {
				_, err = stream.CacheFullInTempFileAndWriter(s, func(p float64) {
					t.SetProgress((float64(cur) + float64(s.GetSize())*p/100.0) / float64(total))
				}, nil)
			}
			cur += s.GetSize()
			if err != nil {
				return nil, errors.Wrap(ErrTempFileCreationFailed, err.Error())
			}
		}
		t.SetProgress(100.0)
		decompressUp = func(_ float64) {}
	} else {
		decompressUp = t.SetProgress
	}

	// 更新任务状态
	t.Status = "walking and decompressing"

	// 创建临时目录
	dir, err := os.MkdirTemp(conf.Conf.TempDir, "dir-*")
	if err != nil {
		return nil, errors.Wrap(ErrTempFileCreationFailed, err.Error())
	}

	// 解压归档
	err = tool.Decompress(ss, dir, t.ArchiveInnerArgs, decompressUp)
	if err != nil {
		return nil, errors.Wrap(ErrArchiveDecompressFailed, err.Error())
	}

	// 获取基本名称
	baseName := strings.TrimSuffix(srcObj.GetName(), stdpath.Ext(srcObj.GetName()))

	// 创建上传任务
	uploadTask := &ArchiveContentUploadTask{
		TaskExtension: task.TaskExtension{
			Creator: t.GetCreator(),
			ApiUrl:  t.ApiUrl,
		},
		ObjName:       baseName,
		InPlace:       !t.PutIntoNewDir,
		FilePath:      dir,
		DstActualPath: t.DstActualPath,
		dstStorage:    t.DstStorage,
		DstStorageMp:  t.DstStorageMp,
	}

	return uploadTask, nil
}

// ArchiveDownloadTaskManager 归档下载任务管理器
var ArchiveDownloadTaskManager *tache.Manager[*ArchiveDownloadTask]

// ArchiveContentUploadTask 归档内容上传任务结构
type ArchiveContentUploadTask struct {
	task.TaskExtension
	status        string
	ObjName       string
	InPlace       bool
	FilePath      string
	DstActualPath string
	dstStorage    driver.Driver
	DstStorageMp  string
	finalized     bool
	groupID       string
}

// GetName 获取任务名称
// 返回:
//   - string: 任务名称
func (t *ArchiveContentUploadTask) GetName() string {
	return fmt.Sprintf("upload %s to [%s](%s)", t.ObjName, t.DstStorageMp, t.DstActualPath)
}

// GetStatus 获取任务状态
// 返回:
//   - string: 任务状态
func (t *ArchiveContentUploadTask) GetStatus() string {
	return t.status
}

// Run 执行任务
// 返回:
//   - error: 错误信息
func (t *ArchiveContentUploadTask) Run() error {
	if err := t.ReinitCtx(); err != nil {
		return err
	}
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()
	return t.RunWithNextTaskCallback(func(nextTsk *ArchiveContentUploadTask) error {
		ArchiveContentUploadTaskManager.Add(nextTsk)
		return nil
	})
}

// OnSucceeded 任务成功回调
func (t *ArchiveContentUploadTask) OnSucceeded() {
	task_group.TransferCoordinator.Done(t.groupID, true)
}

// OnFailed 任务失败回调
func (t *ArchiveContentUploadTask) OnFailed() {
	task_group.TransferCoordinator.Done(t.groupID, false)
}

// SetRetry 设置重试次数
// 参数:
//   - retry: 当前重试次数
//   - maxRetry: 最大重试次数
func (t *ArchiveContentUploadTask) SetRetry(retry int, maxRetry int) {
	t.TaskExtension.SetRetry(retry, maxRetry)
	if retry == 0 &&
		(len(t.groupID) == 0 || // 重启恢复
			(t.GetErr() == nil && t.GetState() != tache.StatePending)) { // 手动重试
		t.groupID = stdpath.Join(t.DstStorageMp, t.DstActualPath)
		task_group.TransferCoordinator.AddTask(t.groupID, nil)
	}
}

// RunWithNextTaskCallback 执行下一个任务回调
// 参数:
//   - f: 回调函数
//
// 返回:
//   - error: 错误信息
func (t *ArchiveContentUploadTask) RunWithNextTaskCallback(f func(nextTask *ArchiveContentUploadTask) error) error {
	info, err := os.Stat(t.FilePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		t.status = "src object is dir, listing objs"
		nextDstActualPath := t.DstActualPath
		if !t.InPlace {
			nextDstActualPath = stdpath.Join(nextDstActualPath, t.ObjName)
			err = op.MakeDir(t.Ctx(), t.dstStorage, nextDstActualPath)
			if err != nil {
				return err
			}
		}
		entries, err := os.ReadDir(t.FilePath)
		if err != nil {
			return err
		}
		if !t.InPlace && len(t.groupID) > 0 {
			task_group.TransferCoordinator.AppendPayload(t.groupID, task_group.DstPathToRefresh(nextDstActualPath))
		}
		var es error
		for _, entry := range entries {
			var nextFilePath string
			if entry.IsDir() {
				nextFilePath, err = moveToTempPath(stdpath.Join(t.FilePath, entry.Name()), "dir-")
			} else {
				nextFilePath, err = moveToTempPath(stdpath.Join(t.FilePath, entry.Name()), "file-")
			}
			if err != nil {
				es = stderrors.Join(es, err)
				continue
			}
			if len(t.groupID) > 0 {
				task_group.TransferCoordinator.AddTask(t.groupID, nil)
			}
			err = f(&ArchiveContentUploadTask{
				TaskExtension: task.TaskExtension{
					Creator: t.GetCreator(),
					ApiUrl:  t.ApiUrl,
				},
				ObjName:       entry.Name(),
				InPlace:       false,
				FilePath:      nextFilePath,
				DstActualPath: nextDstActualPath,
				dstStorage:    t.dstStorage,
				DstStorageMp:  t.DstStorageMp,
				groupID:       t.groupID,
			})
			if err != nil {
				es = stderrors.Join(es, err)
			}
		}
		if es != nil {
			return es
		}
	} else {
		file, err := os.Open(t.FilePath)
		if err != nil {
			return err
		}
		fs := &stream.FileStream{
			Obj: &model.Object{
				Name:     t.ObjName,
				Size:     info.Size(),
				Modified: time.Now(),
			},
			Mimetype:     utils.GetMimeType(stdpath.Ext(t.ObjName)),
			WebPutAsTask: true,
			Reader:       file,
		}
		fs.Closers.Add(file)
		t.status = "uploading"
		err = op.Put(t.Ctx(), t.dstStorage, t.DstActualPath, fs, t.SetProgress, true)
		if err != nil {
			return err
		}
	}
	t.deleteSrcFile()
	return nil
}

// Cancel 取消任务
func (t *ArchiveContentUploadTask) Cancel() {
	t.TaskExtension.Cancel()
	if !conf.Conf.Tasks.AllowRetryCanceled {
		t.deleteSrcFile()
	}
}

// deleteSrcFile 删除源文件
func (t *ArchiveContentUploadTask) deleteSrcFile() {
	if !t.finalized {
		_ = os.RemoveAll(t.FilePath)
		t.finalized = true
	}
}

// moveToTempPath 移动文件到临时路径
// 参数:
//   - path: 源文件路径
//   - prefix: 临时文件前缀
//
// 返回:
//   - string: 临时文件路径
//   - error: 错误信息
func moveToTempPath(path, prefix string) (string, error) {
	newPath, err := genTempFileName(prefix)
	if err != nil {
		return "", err
	}
	err = os.Rename(path, newPath)
	if err != nil {
		return "", err
	}
	return newPath, nil
}

// genTempFileName 生成临时文件名
// 参数:
//   - prefix: 临时文件前缀
//
// 返回:
//   - string: 临时文件路径
//   - error: 错误信息
func genTempFileName(prefix string) (string, error) {
	retry := 0
	t := time.Now().UnixMilli()
	for retry < 10000 {
		newPath := filepath.Join(conf.Conf.TempDir, prefix+fmt.Sprintf("%x-%x", t, rand.Uint32()))
		if _, err := os.Stat(newPath); err != nil {
			if os.IsNotExist(err) {
				return newPath, nil
			} else {
				return "", err
			}
		}
		retry++
	}
	return "", errors.New("failed to generate temp-file name: too many retries")
}

type archiveContentUploadTaskManagerType struct {
	*tache.Manager[*ArchiveContentUploadTask]
}

// Remove 移除任务
func (m *archiveContentUploadTaskManagerType) Remove(id string) {
	if t, ok := m.GetByID(id); ok {
		t.deleteSrcFile()
		m.Manager.Remove(id)
	}
}

// RemoveAll 移除所有任务
func (m *archiveContentUploadTaskManagerType) RemoveAll() {
	tasks := m.GetAll()
	for _, t := range tasks {
		m.Remove(t.GetID())
	}
}

// RemoveByState 根据状态移除任务
func (m *archiveContentUploadTaskManagerType) RemoveByState(state ...tache.State) {
	tasks := m.GetByState(state...)
	for _, t := range tasks {
		m.Remove(t.GetID())
	}
}

// RemoveByCondition 根据条件移除任务
func (m *archiveContentUploadTaskManagerType) RemoveByCondition(condition func(task *ArchiveContentUploadTask) bool) {
	tasks := m.GetByCondition(condition)
	for _, t := range tasks {
		m.Remove(t.GetID())
	}
}

var ArchiveContentUploadTaskManager = &archiveContentUploadTaskManagerType{
	Manager: nil,
}

// archiveMeta 获取归档元数据
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档元数据参数
//
// 返回:
//   - *model.ArchiveMetaProvider: 归档元数据提供者
//   - error: 错误信息
func archiveMeta(ctx context.Context, path string, args model.ArchiveMetaArgs) (*model.ArchiveMetaProvider, error) {
	// 参数验证
	if path == "" {
		return nil, errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get storage")
	}

	// 获取归档元数据
	meta, err := op.GetArchiveMeta(ctx, storage, actualPath, args)
	if err != nil {
		return nil, errors.Wrap(ErrArchiveMetaFailed, err.Error())
	}

	return meta, nil
}

// archiveList 列出归档内容
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档列表参数
//
// 返回:
//   - []model.Obj: 对象列表
//   - error: 错误信息
func archiveList(ctx context.Context, path string, args model.ArchiveListArgs) ([]model.Obj, error) {
	// 参数验证
	if path == "" {
		return nil, errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get storage")
	}

	// 列出归档内容
	objs, err := op.ListArchive(ctx, storage, actualPath, args)
	if err != nil {
		return nil, errors.Wrap(ErrArchiveListFailed, err.Error())
	}

	return objs, nil
}

// archiveDecompress 解压归档
// 参数:
//   - ctx: 上下文
//   - srcObjPath: 源归档对象路径
//   - dstDirPath: 目标解压目录路径
//   - args: 解压参数
//   - lazyCache: 是否懒加载缓存
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func archiveDecompress(ctx context.Context, srcObjPath, dstDirPath string, args model.ArchiveDecompressArgs, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	// 参数验证
	if srcObjPath == "" || dstDirPath == "" {
		return nil, errors.WithStack(ErrInvalidPath)
	}

	// 获取源存储驱动和实际路径
	srcStorage, srcObjActualPath, err := getStorageWithCache(srcObjPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get src storage")
	}

	// 获取目标存储驱动和实际路径
	dstStorage, dstDirActualPath, err := getStorageWithCache(dstDirPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get dst storage")
	}

	// 如果源和目标在同一存储中，尝试使用存储的原生解压功能
	if srcStorage.GetStorage() == dstStorage.GetStorage() {
		err = op.ArchiveDecompress(ctx, srcStorage, srcObjActualPath, dstDirActualPath, args, lazyCache...)
		if !errors.Is(err, errs.NotImplement) {
			if err != nil {
				return nil, errors.Wrap(ErrArchiveDecompressFailed, err.Error())
			}
			return nil, nil
		}
	}

	// 创建归档下载任务
	tsk := &ArchiveDownloadTask{
		TaskData: TaskData{
			SrcStorage:    srcStorage,
			DstStorage:    dstStorage,
			SrcActualPath: srcObjActualPath,
			DstActualPath: dstDirActualPath,
			SrcStorageMp:  srcStorage.GetStorage().MountPath,
			DstStorageMp:  dstStorage.GetStorage().MountPath,
		},
		ArchiveDecompressArgs: args,
	}

	// 如果不需要异步任务，直接执行
	if ctx.Value(consts.NoTaskKey) != nil {
		tsk.Base.SetCtx(ctx)
		uploadTask, err := tsk.RunWithoutPushUploadTask()
		if err != nil {
			return nil, errors.WithMessagef(err, "failed download [%s]", srcObjPath)
		}
		defer uploadTask.deleteSrcFile()

		// 定义递归回调函数
		var callback func(t *ArchiveContentUploadTask) error
		callback = func(t *ArchiveContentUploadTask) error {
			tsk.Base.SetCtx(ctx)
			e := t.RunWithNextTaskCallback(callback)
			t.deleteSrcFile()
			return e
		}
		uploadTask.Base.SetCtx(ctx)
		return nil, uploadTask.RunWithNextTaskCallback(callback)
	} else {
		tsk.Creator, _ = ctx.Value(consts.UserKey).(*model.User)
		tsk.ApiUrl = common.GetApiURL(ctx)
		// 添加到任务管理器
		ArchiveDownloadTaskManager.Add(tsk)
		return tsk, nil
	}
}

// archiveDriverExtract 从归档中提取文件（使用驱动）
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档内部参数
//
// 返回:
//   - *model.Link: 链接
//   - model.Obj: 对象
//   - error: 错误信息
func archiveDriverExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (*model.Link, model.Obj, error) {
	// 参数验证
	if path == "" {
		return nil, nil, errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "failed get storage")
	}

	// 从归档中提取
	l, obj, err := op.DriverExtract(ctx, storage, actualPath, args)
	if err != nil {
		return nil, nil, errors.Wrap(ErrArchiveExtractFailed, err.Error())
	}

	return l, obj, nil
}

// archiveInternalExtract 从归档中提取文件（内部）
// 参数:
//   - ctx: 上下文
//   - path: 路径
//   - args: 归档内部参数
//
// 返回:
//   - io.ReadCloser: 读取器
//   - int64: 大小
//   - error: 错误信息
func archiveInternalExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (io.ReadCloser, int64, error) {
	// 参数验证
	if path == "" {
		return nil, 0, errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return nil, 0, errors.WithMessage(err, "failed get storage")
	}

	// 从归档中提取
	l, size, err := op.InternalExtract(ctx, storage, actualPath, args)
	if err != nil {
		return nil, 0, errors.Wrap(ErrArchiveExtractFailed, err.Error())
	}

	return l, size, nil
}