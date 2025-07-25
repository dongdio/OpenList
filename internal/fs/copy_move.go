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
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/task"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 自定义错误类型
var (
	ErrTransferFailed = errors.New("transfer failed")
	ErrCopyFailed     = errors.New("copy failed")
	ErrMoveFailed     = errors.New("move failed")
)

// taskType 任务类型
type taskType uint8

// String 获取任务类型字符串表示
// 返回:
//   - string: 任务类型字符串
func (t taskType) String() string {
	if t == copy {
		return "copy"
	} else {
		return "move"
	}
}

// 任务类型常量
const (
	copy taskType = iota
	move
)

// FileTransferTask 文件传输任务结构
type FileTransferTask struct {
	TaskData
	TaskType taskType
	groupID  string
}

// GetName 获取任务名称
// 返回:
//   - string: 任务名称
func (t *FileTransferTask) GetName() string {
	return fmt.Sprintf("%s [%s](%s) to [%s](%s)", t.TaskType, t.SrcStorageMp, t.SrcActualPath, t.DstStorageMp, t.DstActualPath)
}

// Run 执行任务
// 返回:
//   - error: 错误信息
func (t *FileTransferTask) Run() error {
	// 重新初始化上下文
	if err := t.ReinitCtx(); err != nil {
		return err
	}

	// 初始化任务时间
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	var err error

	// 获取源存储驱动
	if t.SrcStorage == nil {
		t.SrcStorage, err = op.GetStorageByMountPath(t.SrcStorageMp)
		if err != nil {
			return errors.WithMessage(err, "failed get source storage")
		}
	}

	// 获取目标存储驱动
	if t.DstStorage == nil {
		t.DstStorage, err = op.GetStorageByMountPath(t.DstStorageMp)
		if err != nil {
			return errors.WithMessage(err, "failed get destination storage")
		}
	}

	// 在两个存储之间传输文件
	return putBetween2Storages(t, t.SrcStorage, t.DstStorage, t.SrcActualPath, t.DstActualPath)
}

// OnSucceeded 任务成功回调
func (t *FileTransferTask) OnSucceeded() {
	task_group.TransferCoordinator.Done(t.groupID, true)
}

// OnFailed 任务失败回调
func (t *FileTransferTask) OnFailed() {
	task_group.TransferCoordinator.Done(t.groupID, false)
}

// SetRetry 设置重试
// 参数:
//   - retry: 当前重试次数
//   - maxRetry: 最大重试次数
func (t *FileTransferTask) SetRetry(retry int, maxRetry int) {
	t.TaskExtension.SetRetry(retry, maxRetry)
	if retry == 0 &&
		(len(t.groupID) == 0 || // 重启恢复
			(t.GetErr() == nil && t.GetState() != tache.StatePending)) { // 手动重试
		t.groupID = stdpath.Join(t.DstStorageMp, t.DstActualPath)
		var payload any
		if t.TaskType == move {
			payload = task_group.SrcPathToRemove(stdpath.Join(t.SrcStorageMp, t.SrcActualPath))
		}
		task_group.TransferCoordinator.AddTask(t.groupID, payload)
	}
}

// transfer 在两个路径之间传输文件
// 参数:
//   - ctx: 上下文
//   - taskType: 任务类型（复制或移动）
//   - srcObjPath: 源对象路径
//   - dstDirPath: 目标目录路径
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - task.TaskExtensionInfo: 任务信息
//   - error: 错误信息
func transfer(ctx context.Context, taskType taskType, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
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

	// 如果源和目标在同一存储中，尝试使用存储的原生复制/移动功能
	if srcStorage.GetStorage() == dstStorage.GetStorage() {
		if taskType == copy {
			err = op.Copy(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)
			if !errors.Is(err, errs.NotImplement) && !errors.Is(err, errs.NotSupport) {
				if err != nil {
					return nil, errors.Wrap(ErrCopyFailed, err.Error())
				}
				return nil, nil
			}
		} else {
			err = op.Move(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)
			if !errors.Is(err, errs.NotImplement) && !errors.Is(err, errs.NotSupport) {
				if err != nil {
					return nil, errors.Wrap(ErrMoveFailed, err.Error())
				}
				return nil, nil
			}
		}
	} else if ctx.Value(consts.NoTaskKey) != nil {
		return nil, fmt.Errorf("can't %s files between two storages, please use the front-end ", taskType)
	}

	// 获取任务创建者
	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User)

	// 创建文件传输任务
	t := &FileTransferTask{
		TaskData: TaskData{
			TaskExtension: task.TaskExtension{
				Creator: taskCreator,
				ApiUrl:  common.GetApiURL(ctx),
			},
			SrcStorage:    srcStorage,
			DstStorage:    dstStorage,
			SrcActualPath: srcObjActualPath,
			DstActualPath: dstDirActualPath,
			SrcStorageMp:  srcStorage.GetStorage().MountPath,
			DstStorageMp:  dstStorage.GetStorage().MountPath,
		},
		TaskType: taskType,
		groupID:  dstDirPath,
	}

	// 添加任务
	if taskType == copy {
		task_group.TransferCoordinator.AddTask(dstDirPath, nil)
		CopyTaskManager.Add(t)
	} else {
		task_group.TransferCoordinator.AddTask(dstDirPath, task_group.SrcPathToRemove(srcObjPath))
		MoveTaskManager.Add(t)
	}

	return t, nil
}

// putBetween2Storages 在两个存储之间传输文件
// 参数:
//   - t: 文件传输任务
//   - srcStorage: 源存储驱动
//   - dstStorage: 目标存储驱动
//   - srcActualPath: 源实际路径
//   - dstDirActualPath: 目标目录实际路径
//
// 返回:
//   - error: 错误信息
func putBetween2Storages(t *FileTransferTask, srcStorage, dstStorage driver.Driver, srcActualPath, dstDirActualPath string) error {
	// 更新任务状态
	t.Status = "getting src object"

	// 获取源对象
	srcObj, err := op.Get(t.Ctx(), srcStorage, srcActualPath)
	if err != nil {
		return errors.WithMessagef(err, "failed get src [%s] file", srcActualPath)
	}

	// 处理目录传输
	if srcObj.IsDir() {
		t.Status = "src object is dir, listing objs"

		// 获取目录内容
		objs, err := op.List(t.Ctx(), srcStorage, srcActualPath, model.ListArgs{})
		if err != nil {
			return errors.WithMessagef(err, "failed list src [%s] objs", srcActualPath)
		}

		// 构建目标路径
		dstActualPath := stdpath.Join(dstDirActualPath, srcObj.GetName())

		// 如果是复制操作，添加刷新路径
		if t.TaskType == copy {
			task_group.TransferCoordinator.AppendPayload(t.groupID, task_group.DstPathToRefresh(dstActualPath))
		}

		// 为目录中的每个对象创建传输任务
		for _, obj := range objs {
			// 检查是否取消
			if utils.IsCanceled(t.Ctx()) {
				return nil
			}

			// 创建子任务
			task := &FileTransferTask{
				TaskType: t.TaskType,
				TaskData: TaskData{
					TaskExtension: task.TaskExtension{
						Creator: t.GetCreator(),
						ApiUrl:  t.ApiUrl,
					},
					SrcStorage:    srcStorage,
					DstStorage:    dstStorage,
					SrcActualPath: stdpath.Join(srcActualPath, obj.GetName()),
					DstActualPath: dstActualPath,
					SrcStorageMp:  srcStorage.GetStorage().MountPath,
					DstStorageMp:  dstStorage.GetStorage().MountPath,
				},
				groupID: t.groupID,
			}

			// 添加任务
			task_group.TransferCoordinator.AddTask(t.groupID, nil)
			if t.TaskType == copy {
				CopyTaskManager.Add(task)
			} else {
				MoveTaskManager.Add(task)
			}
		}

		// 更新任务状态
		t.Status = fmt.Sprintf("src object is dir, added all %s tasks of objs", t.TaskType)
		return nil
	}

	// 处理文件传输
	return putFileBetween2Storages(t, srcStorage, dstStorage, srcActualPath, dstDirActualPath)
}

// putFileBetween2Storages 在两个存储之间传输单个文件
// 参数:
//   - tsk: 文件传输任务
//   - srcStorage: 源存储驱动
//   - dstStorage: 目标存储驱动
//   - srcActualPath: 源实际路径
//   - dstDirActualPath: 目标目录实际路径
//
// 返回:
//   - error: 错误信息
func putFileBetween2Storages(tsk *FileTransferTask, srcStorage, dstStorage driver.Driver, srcActualPath, dstDirActualPath string) error {
	// 获取源文件
	srcFile, err := op.Get(tsk.Ctx(), srcStorage, srcActualPath)
	if err != nil {
		return errors.WithMessagef(err, "failed get src [%s] file", srcActualPath)
	}

	// 设置总字节数
	tsk.SetTotalBytes(srcFile.GetSize())

	// 获取文件链接
	link, _, err := op.Link(tsk.Ctx(), srcStorage, srcActualPath, model.LinkArgs{})
	if err != nil {
		return errors.WithMessagef(err, "failed get [%s] link", srcActualPath)
	}

	// 创建可查找流
	ss, err := stream.NewSeekableStream(&stream.FileStream{
		Obj: srcFile,
		Ctx: tsk.Ctx(),
	}, link)

	// 处理错误
	if err != nil {
		_ = link.Close()
		return errors.WithMessagef(err, "failed get [%s] stream", srcActualPath)
	}

	// 更新总字节数
	tsk.SetTotalBytes(ss.GetSize())

	// 执行上传操作
	return op.Put(tsk.Ctx(), dstStorage, dstDirActualPath, ss, tsk.SetProgress, true)
}

// 任务管理器
var (
	CopyTaskManager *tache.Manager[*FileTransferTask]
	MoveTaskManager *tache.Manager[*FileTransferTask]
)
