package fs

import (
	"context"
	"fmt"
	"time"

	"github.com/OpenListTeam/tache"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/task"
)

// 自定义错误类型
var (
	// ErrUploadFailed 上传失败错误
	ErrUploadFailed = errors.New("failed to upload file")

	// ErrTempFileFailed 临时文件创建失败错误
	ErrTempFileFailed = errors.New("failed to create temporary file")
)

// UploadTask 上传任务结构体
type UploadTask struct {
	task.TaskExtension
	storage          driver.Driver      // 存储驱动
	dstDirActualPath string             // 目标目录实际路径
	file             model.FileStreamer // 文件流
	status           string             // 当前状态
}

// GetName 获取任务名称
func (t *UploadTask) GetName() string {
	return fmt.Sprintf("upload %s to [%s](%s)", t.file.GetName(), t.storage.GetStorage().MountPath, t.dstDirActualPath)
}

// GetStatus 获取任务状态
func (t *UploadTask) GetStatus() string {
	if t.status == "" {
		return "uploading"
	}
	return t.status
}

// Run 执行上传任务
func (t *UploadTask) Run() error {
	// 初始化任务时间
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	// 设置任务状态
	t.status = "uploading"

	// 执行上传操作
	err := op.Put(t.Ctx(), t.storage, t.dstDirActualPath, t.file, t.SetProgress, true)
	if err != nil {
		t.status = "failed: " + err.Error()
		return errors.Wrap(ErrUploadFailed, err.Error())
	}

	t.status = "completed"
	return nil
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
	// 获取存储和实际路径
	storage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return nil, errors.Wrap(ErrStorageNotFound, err.Error())
	}

	// 检查存储是否支持上传
	if storage.Config().NoUpload {
		return nil, errors.Wrap(errs.UploadNotSupported, "storage does not support uploads")
	}

	// 如果文件需要存储，创建临时文件
	if file.NeedStore() {
		_, err = file.CacheFullInTempFile()
		if err != nil {
			return nil, errors.Wrap(ErrTempFileFailed, err.Error())
		}
	}

	// 从上下文获取任务创建者
	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User)

	// 创建上传任务
	t := &UploadTask{
		TaskExtension: task.TaskExtension{
			Creator: taskCreator,
		},
		storage:          storage,
		dstDirActualPath: dstDirActualPath,
		file:             file,
		status:           "initialized",
	}

	// 设置任务总字节数
	t.SetTotalBytes(file.GetSize())

	// 添加任务到管理器
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
	// 获取存储和实际路径
	storage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		_ = file.Close()
		return errors.Wrap(ErrStorageNotFound, err.Error())
	}

	// 检查存储是否支持上传
	if storage.Config().NoUpload {
		_ = file.Close()
		return errors.Wrap(errs.UploadNotSupported, "storage does not support uploads")
	}

	// 执行上传操作
	return op.Put(ctx, storage, dstDirActualPath, file, nil, lazyCache...)
}
