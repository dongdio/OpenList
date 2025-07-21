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
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/task"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 自定义错误类型
var (
	// ErrCopyFailed 复制失败错误
	ErrCopyFailed = errors.New("failed to copy file or directory")

	// ErrSourceNotFound 源对象未找到错误
	ErrSourceNotFound = errors.New("source object not found")

	// ErrStreamCreationFailed 流创建失败错误
	ErrStreamCreationFailed = errors.New("failed to create file stream")

	// ErrOperationCanceled 操作已取消错误
	ErrOperationCanceled = errors.New("operation was canceled")
)

// CopyTask 表示异步文件/目录复制操作
type CopyTask struct {
	task.TaskExtension
	Status       string        // 当前状态
	SrcObjPath   string        // 源对象路径
	DstDirPath   string        // 目标目录路径
	SrcStorageMp string        // 源存储挂载路径
	DstStorageMp string        // 目标存储挂载路径
	srcStorage   driver.Driver // 源存储驱动
	dstStorage   driver.Driver // 目标存储驱动
}

// GetName 返回复制任务的可读名称
func (t *CopyTask) GetName() string {
	return fmt.Sprintf("copy [%s](%s) to [%s](%s)",
		t.SrcStorageMp, t.SrcObjPath,
		t.DstStorageMp, t.DstDirPath)
}

// GetStatus 返回复制任务的当前状态
func (t *CopyTask) GetStatus() string {
	return t.Status
}

// Run 执行复制任务
// 初始化存储驱动（如果需要）并委托给copyBetween2Storages
func (t *CopyTask) Run() error {
	// 初始化任务计时信息
	if err := t.ReinitCtx(); err != nil {
		return err
	}
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	var err error

	// 初始化源存储（如果需要）
	if t.srcStorage == nil {
		t.srcStorage, err = op.GetStorageByMountPath(t.SrcStorageMp)
		if err != nil {
			return errors.Wrapf(ErrStorageNotFound, "source: %s", err.Error())
		}
	}

	// 初始化目标存储（如果需要）
	if t.dstStorage == nil {
		t.dstStorage, err = op.GetStorageByMountPath(t.DstStorageMp)
		if err != nil {
			return errors.Wrapf(ErrStorageNotFound, "destination: %s", err.Error())
		}
	}

	// 执行复制操作
	return copyBetween2Storages(t, t.srcStorage, t.dstStorage, t.SrcObjPath, t.DstDirPath)
}

// CopyTaskManager 管理异步复制任务
var CopyTaskManager *tache.Manager[*CopyTask]

// _copy 创建用于在存储之间复制文件/目录的复制任务
// 尝试使用存储的原生复制功能（如果源和目标在同一存储上）
func _copy(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	// 获取源存储和路径
	srcStorage, srcObjActualPath, err := op.GetStorageAndActualPath(srcObjPath)
	if err != nil {
		return nil, errors.Wrapf(ErrStorageNotFound, "source: %s", err.Error())
	}

	// 获取目标存储和路径
	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return nil, errors.Wrapf(ErrStorageNotFound, "destination: %s", err.Error())
	}

	// 如果源和目标在同一存储上，尝试使用存储的原生复制功能
	if srcStorage.GetStorage() == dstStorage.GetStorage() {
		err = op.Copy(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)

		// 如果存储支持原生复制，返回结果
		if !errors.Is(err, errs.NotImplement) && !errors.Is(err, errs.NotSupport) {
			return nil, err
		}
		// 否则，回退到基于任务的方法
	}

	// 处理无任务上下文标志进行同步复制
	if ctx.Value(consts.NoTaskKey) != nil {
		return handleSynchronousCopy(ctx, srcStorage, dstStorage, srcObjPath, srcObjActualPath, dstDirActualPath)
	}

	// 从上下文获取任务创建者
	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User)

	// 创建并配置复制任务
	t := &CopyTask{
		TaskExtension: task.TaskExtension{
			Creator: taskCreator,
			ApiUrl:  common.GetApiURL(ctx),
		},
		srcStorage:   srcStorage,
		dstStorage:   dstStorage,
		SrcObjPath:   srcObjActualPath,
		DstDirPath:   dstDirActualPath,
		SrcStorageMp: srcStorage.GetStorage().MountPath,
		DstStorageMp: dstStorage.GetStorage().MountPath,
		Status:       "initialized",
	}

	// 添加任务到管理器
	CopyTaskManager.Add(t)

	return t, nil
}

// handleSynchronousCopy 在禁用任务时执行同步复制操作
// 用于不应该排队的直接文件复制
func handleSynchronousCopy(ctx context.Context, srcStorage, dstStorage driver.Driver,
	srcObjPath, srcObjActualPath, dstDirActualPath string) (task.TaskExtensionInfo, error) {

	// 获取源对象
	srcObj, err := op.Get(ctx, srcStorage, srcObjActualPath)
	if err != nil {
		return nil, errors.Wrapf(ErrSourceNotFound, "path: %s, error: %s", srcObjPath, err.Error())
	}

	// 对于非目录对象，执行直接复制
	if !srcObj.IsDir() {
		// 获取源文件链接
		linkRes, _, err := op.Link(ctx, srcStorage, srcObjActualPath, model.LinkArgs{})
		if err != nil {
			return nil, errors.Wrapf(ErrLinkFailed, "path: %s, error: %s", srcObjPath, err.Error())
		}

		// 为源对象创建文件流
		fileStream, err := stream.NewSeekableStream(&stream.FileStream{
			Obj: srcObj,
			Ctx: ctx,
		}, linkRes)
		if err != nil {
			_ = linkRes.Close()
			return nil, errors.Wrapf(ErrStreamCreationFailed, "path: %s, error: %s", srcObjPath, err.Error())
		}

		// 执行直接上传到目标
		return nil, op.Put(ctx, dstStorage, dstDirActualPath, fileStream, nil, false)
	}

	// 目录处理将通过基于任务的方法进行
	return nil, errors.New("synchronous copy only supports files, not directories")
}

// copyBetween2Storages 在两个存储之间复制文件或目录
// 处理文件和目录的逻辑
func copyBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver,
	srcObjPath, dstDirPath string) error {

	// 更新任务状态并获取源对象
	t.Status = "getting source object"
	srcObj, err := op.Get(t.Ctx(), srcStorage, srcObjPath)
	if err != nil {
		return errors.Wrapf(ErrSourceNotFound, "path: %s, error: %s", srcObjPath, err.Error())
	}

	// 处理目录复制
	if srcObj.IsDir() {
		return copyDirectoryBetween2Storages(t, srcStorage, dstStorage, srcObj, srcObjPath, dstDirPath)
	}

	// 处理文件复制
	return copyFileBetween2Storages(t, srcStorage, dstStorage, srcObjPath, dstDirPath)
}

// copyDirectoryBetween2Storages 处理两个存储之间的目录复制
// 创建目标目录并为每个子项安排任务
func copyDirectoryBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver,
	srcDirObj model.Obj, srcObjPath, dstDirPath string) error {

	// 列出源目录中的对象
	t.Status = "listing source directory contents"
	dirContents, err := op.List(t.Ctx(), srcStorage, srcObjPath, model.ListArgs{})
	if err != nil {
		return errors.Wrapf(ErrListFailed, "path: %s, error: %s", srcObjPath, err.Error())
	}

	// 创建目标目录
	dstDirFullPath := stdpath.Join(dstDirPath, srcDirObj.GetName())
	t.Status = "creating destination directory: " + dstDirFullPath
	err = op.MakeDir(t.Ctx(), dstStorage, dstDirFullPath)
	if err != nil {
		return errors.Wrapf(ErrMakeDirFailed, "path: %s, error: %s", dstDirFullPath, err.Error())
	}

	// 为目录中的每个项目安排复制任务
	t.Status = "scheduling copy tasks for directory contents"
	for _, childObj := range dirContents {
		// 检查操作是否已取消
		if utils.IsCanceled(t.Ctx()) {
			t.Status = "operation canceled"
			return ErrOperationCanceled
		}

		// 创建并安排子项的复制任务
		childTask := &CopyTask{
			TaskExtension: task.TaskExtension{
				Creator: t.GetCreator(),
				ApiUrl:  t.ApiUrl,
			},
			srcStorage:   srcStorage,
			dstStorage:   dstStorage,
			SrcObjPath:   stdpath.Join(srcObjPath, childObj.GetName()),
			DstDirPath:   dstDirFullPath,
			SrcStorageMp: srcStorage.GetStorage().MountPath,
			DstStorageMp: dstStorage.GetStorage().MountPath,
			Status:       "scheduled",
		}
		CopyTaskManager.Add(childTask)
	}

	t.Status = "all child copy tasks scheduled"
	return nil
}

// copyFileBetween2Storages 处理两个存储之间的文件复制
// 获取源文件链接并上传到目标
func copyFileBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver,
	srcFilePath, dstDirPath string) error {

	// 获取源文件
	t.Status = "getting source file: " + srcFilePath
	srcFile, err := op.Get(t.Ctx(), srcStorage, srcFilePath)
	if err != nil {
		return errors.Wrapf(ErrSourceNotFound, "path: %s, error: %s", srcFilePath, err.Error())
	}

	// 获取源文件链接
	t.Status = "getting link to source file"
	linkRes, _, err := op.Link(t.Ctx(), srcStorage, srcFilePath, model.LinkArgs{})
	if err != nil {
		return errors.Wrapf(ErrLinkFailed, "path: %s, error: %s", srcFilePath, err.Error())
	}

	// 为源文件创建文件流
	t.Status = "creating file stream"
	fileStream, err := stream.NewSeekableStream(&stream.FileStream{
		Obj: srcFile,
		Ctx: t.Ctx(),
	}, linkRes)

	if err != nil {
		_ = linkRes.Close()
		return errors.Wrapf(ErrStreamCreationFailed, "path: %s, error: %s", srcFilePath, err.Error())
	}

	// 设置任务总字节数
	t.SetTotalBytes(fileStream.GetSize())
	t.Status = "uploading file to destination"

	// 上传文件到目标
	// 传递进度回调函数以更新任务进度
	err = op.Put(t.Ctx(), dstStorage, dstDirPath, fileStream, t.SetProgress, true)
	if err != nil {
		t.Status = "upload failed: " + err.Error()
		return errors.Wrapf(ErrUploadFailed, "path: %s, error: %s", srcFilePath, err.Error())
	}

	t.Status = "completed"
	return nil
}
