package fs

import (
	"context"
	"fmt"
	stdpath "path"
	"time"

	"github.com/OpenListTeam/tache"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/task_group"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/task"
)

// 自定义错误类型
var (
	ErrUploadFailed = errors.New("upload failed")
)

// UploadTask 上传任务结构
type UploadTask struct {
	task.TaskExtension
	storage          driver.Driver
	dstDirActualPath string
	file             model.FileStreamer
}

// GetName 获取任务名称
// 返回:
//   - string: 任务名称
func (t *UploadTask) GetName() string {
	return fmt.Sprintf("upload %s to [%s](%s)", t.file.GetName(), t.storage.GetStorage().MountPath, t.dstDirActualPath)
}

// GetStatus 获取任务状态
// 返回:
//   - string: 任务状态
func (t *UploadTask) GetStatus() string {
	return "uploading"
}

// Run 执行任务
// 返回:
//   - error: 错误信息
func (t *UploadTask) Run() error {
	// 初始化任务时间
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	// 执行上传操作
	return op.Put(t.Ctx(), t.storage, t.dstDirActualPath, t.file, t.SetProgress, true)
}

// OnSucceeded 任务成功回调
func (t *UploadTask) OnSucceeded() {
	task_group.TransferCoordinator.Done(stdpath.Join(t.storage.GetStorage().MountPath, t.dstDirActualPath), true)
}

// OnFailed 任务失败回调
func (t *UploadTask) OnFailed() {
	task_group.TransferCoordinator.Done(stdpath.Join(t.storage.GetStorage().MountPath, t.dstDirActualPath), false)
}

// SetRetry 设置重试
// 参数:
//   - retry: 当前重试次数
//   - maxRetry: 最大重试次数
func (t *UploadTask) SetRetry(retry int, maxRetry int) {
	t.TaskExtension.SetRetry(retry, maxRetry)
	if retry == 0 &&
		(t.GetErr() == nil && t.GetState() != tache.StatePending) { // 手动重试
		task_group.TransferCoordinator.AddTask(stdpath.Join(t.storage.GetStorage().MountPath, t.dstDirActualPath), nil)
	}
}

// UploadTaskManager 上传任务管理器
var UploadTaskManager *tache.Manager[*UploadTask]

// putAsTask 添加上传任务并立即返回
// 参数:
//   - ctx: 上下文
//   - dstDirPath: 目标目录路径
//   - file: 文件流
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func putAsTask(ctx context.Context, dstDirPath string, file model.FileStreamer) (task.TaskExtensionInfo, error) {
	// 参数验证
	if dstDirPath == "" {
		return nil, errors.WithStack(ErrInvalidPath)
	}

	if file == nil {
		return nil, errors.New("file cannot be nil")
	}

	// 获取存储驱动和实际路径
	storage, dstDirActualPath, err := getStorageWithCache(dstDirPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get storage")
	}

	// 检查存储是否支持上传
	if storage.Config().NoUpload {
		return nil, errors.WithStack(errs.UploadNotSupported)
	}

	// 如果文件需要缓存，先创建临时文件
	if file.NeedStore() {
		_, err := file.CacheFullInTempFile()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create temp file")
		}
	}

	// 获取任务创建者
	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User) // taskCreator is nil when convert failed

	// 创建上传任务
	t := &UploadTask{
		TaskExtension: task.TaskExtension{
			Creator: taskCreator,
			ApiUrl:  common.GetApiURL(ctx),
		},
		storage:          storage,
		dstDirActualPath: dstDirActualPath,
		file:             file,
	}

	// 设置总字节数
	t.SetTotalBytes(file.GetSize())

	// 添加任务
	task_group.TransferCoordinator.AddTask(dstDirPath, nil)
	UploadTaskManager.Add(t)

	return t, nil
}

// putDirectly 直接上传文件并在完成后返回
// 参数:
//   - ctx: 上下文
//   - dstDirPath: 目标目录路径
//   - file: 文件流
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func putDirectly(ctx context.Context, dstDirPath string, file model.FileStreamer, lazyCache ...bool) error {
	// 参数验证
	if dstDirPath == "" {
		_ = file.Close()
		return errors.WithStack(ErrInvalidPath)
	}

	if file == nil {
		return errors.New("file cannot be nil")
	}

	// 获取存储驱动和实际路径
	storage, dstDirActualPath, err := getStorageWithCache(dstDirPath)
	if err != nil {
		_ = file.Close()
		return errors.WithMessage(err, "failed get storage")
	}

	// 检查存储是否支持上传
	if storage.Config().NoUpload {
		_ = file.Close()
		return errors.WithStack(errs.UploadNotSupported)
	}

	// 执行上传操作
	err = op.Put(ctx, storage, dstDirActualPath, file, nil, lazyCache...)
	if err != nil {
		return errors.Wrap(ErrUploadFailed, err.Error())
	}

	return nil
}
