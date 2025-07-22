package tool

import (
	"context"
	"fmt"
	"os"
	stdpath "path"
	"path/filepath"
	"sync"
	"time"

	"github.com/OpenListTeam/tache"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/task"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 错误类型定义
var (
	ErrSourceNotFound      = errors.New("源文件或目录不存在")
	ErrDestinationNotFound = errors.New("目标目录不存在")
	ErrInvalidSource       = errors.New("无效的源文件或目录")
	ErrInvalidDestination  = errors.New("无效的目标目录")
	ErrTransferCanceled    = errors.New("传输任务被取消")
)

// 常量定义
const (
	bufferSize = 32 * 1024 // 32KB 缓冲区大小
)

// 缓冲区池，用于减少内存分配
var bufferPool = sync.Pool{
	New: func() any {
		buffer := make([]byte, bufferSize)
		return &buffer
	},
}

// TransferTask 表示一个文件传输任务
// 支持从标准文件系统、对象存储和URL传输到目标存储
type TransferTask struct {
	task.TaskExtension
	Status       string        `json:"-"`              // 不保存状态以节省空间
	SrcObjPath   string        `json:"src_obj_path"`   // 源对象路径
	DstDirPath   string        `json:"dst_dir_path"`   // 目标目录路径
	SrcStorage   driver.Driver `json:"-"`              // 源存储驱动（可能为nil表示标准文件系统或URL）
	DstStorage   driver.Driver `json:"-"`              // 目标存储驱动
	SrcStorageMp string        `json:"src_storage_mp"` // 源存储挂载点
	DstStorageMp string        `json:"dst_storage_mp"` // 目标存储挂载点
	DeletePolicy DeletePolicy  `json:"delete_policy"`  // 删除策略
	URL          string        `json:"-"`              // 源URL（用于直接从URL上传）
}

// Run 执行传输任务
// 根据源类型选择不同的传输方式：
// 1. 从URL直接上传
// 2. 从标准文件系统传输
// 3. 从对象存储传输
func (t *TransferTask) Run() error {
	// 重新初始化上下文并设置任务开始时间
	if err := t.ReinitCtx(); err != nil {
		return errors.Wrap(err, "无法初始化任务上下文")
	}
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	// 检查目标存储是否有效
	if t.DstStorage == nil {
		return errors.Wrap(ErrInvalidDestination, "目标存储不能为空")
	}

	// 根据源类型选择不同的传输方式
	if t.SrcStorage == nil {
		if t.DeletePolicy == UploadDownloadStream {
			return t.transferFromURL()
		}
		return t.transferFromStdPath()
	}

	return t.transferFromObjPath()
}

// transferFromURL 从URL直接上传到目标存储
func (t *TransferTask) transferFromURL() error {
	t.Status = "从URL获取数据流"

	// 检查URL是否有效
	if t.URL == "" {
		return errors.New("URL不能为空")
	}

	// 获取范围读取器
	rr, err := stream.GetRangeReaderFromLink(t.GetTotalBytes(), &model.Link{URL: t.URL})
	if err != nil {
		return errors.Wrap(err, "无法从URL创建范围读取器")
	}

	// 读取整个内容
	r, err := rr.RangeRead(t.Ctx(), http_range.Range{Length: t.GetTotalBytes()})
	if err != nil {
		return errors.Wrap(err, "无法从URL读取数据")
	}

	// 准备文件流
	name := t.SrcObjPath
	mimetype := utils.GetMimeType(name)
	s := &stream.FileStream{
		Ctx: t.Ctx(),
		Obj: &model.Object{
			Name:     name,
			Size:     t.GetTotalBytes(),
			Modified: time.Now(),
			IsFolder: false,
		},
		Reader:   r,
		Mimetype: mimetype,
		Closers:  utils.NewClosers(r),
	}

	// 上传到目标存储
	t.Status = "上传数据到目标存储"
	return op.Put(t.Ctx(), t.DstStorage, t.DstDirPath, s, t.SetProgress)
}

// GetName 返回任务的描述性名称
func (t *TransferTask) GetName() string {
	if t.DeletePolicy == UploadDownloadStream {
		return fmt.Sprintf("上传 [%s](%s) 到 [%s](%s)", t.SrcObjPath, t.URL, t.DstStorageMp, t.DstDirPath)
	}
	return fmt.Sprintf("传输 [%s](%s) 到 [%s](%s)", t.SrcStorageMp, t.SrcObjPath, t.DstStorageMp, t.DstDirPath)
}

// GetStatus 返回任务的当前状态
func (t *TransferTask) GetStatus() string {
	return t.Status
}

// OnSucceeded 在任务成功完成后执行清理操作
func (t *TransferTask) OnSucceeded() {
	if t.DeletePolicy == DeleteOnUploadSucceed || t.DeletePolicy == DeleteAlways {
		t.cleanupSource()
	}
}

// OnFailed 在任务失败后执行清理操作
func (t *TransferTask) OnFailed() {
	if t.DeletePolicy == DeleteOnUploadFailed || t.DeletePolicy == DeleteAlways {
		t.cleanupSource()
	}
}

// cleanupSource 根据源类型清理源文件
func (t *TransferTask) cleanupSource() {
	if t.SrcStorage == nil {
		t.removeStdTemp()
	} else {
		t.removeObjTemp()
	}
}

// TransferTaskManager 管理所有传输任务
var TransferTaskManager *tache.Manager[*TransferTask]

// TransferFromStd 从标准文件系统传输文件到目标存储
// 为目录中的每个文件创建单独的传输任务
func TransferFromStd(ctx context.Context, tempDir, dstDirPath string, deletePolicy DeletePolicy) error {
	// 获取目标存储和实际路径
	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return errors.Wrap(err, "无法获取目标存储")
	}

	// 检查源目录是否存在
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return errors.Wrap(err, "无法读取源目录")
	}

	// 获取任务创建者
	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User)

	// 为每个文件创建传输任务
	for _, entry := range entries {
		t := &TransferTask{
			TaskExtension: task.TaskExtension{
				Creator: taskCreator,
			},
			SrcObjPath:   stdpath.Join(tempDir, entry.Name()),
			DstDirPath:   dstDirActualPath,
			DstStorage:   dstStorage,
			DstStorageMp: dstStorage.GetStorage().MountPath,
			DeletePolicy: deletePolicy,
		}
		TransferTaskManager.Add(t)
	}

	return nil
}

// transferFromStdPath 从标准文件系统传输文件或目录
func (t *TransferTask) transferFromStdPath() error {
	t.Status = "获取源对象信息"

	// 检查源文件或目录是否存在
	info, err := os.Stat(t.SrcObjPath)
	if err != nil {
		return errors.Wrap(err, "无法获取源文件信息")
	}

	// 如果是目录，为每个子项创建传输任务
	if info.IsDir() {
		t.Status = "源对象是目录，列出文件"
		entries, err := os.ReadDir(t.SrcObjPath)
		if err != nil {
			return errors.Wrap(err, "无法读取源目录")
		}

		// 为每个子项创建传输任务
		for _, entry := range entries {
			// 检查是否取消
			if utils.IsCanceled(t.Ctx()) {
				return ErrTransferCanceled
			}

			srcRawPath := stdpath.Join(t.SrcObjPath, entry.Name())
			dstObjPath := stdpath.Join(t.DstDirPath, info.Name())

			taskTmp := &TransferTask{
				TaskExtension: task.TaskExtension{
					Creator: t.Creator,
				},
				SrcObjPath:   srcRawPath,
				DstDirPath:   dstObjPath,
				DstStorage:   t.DstStorage,
				SrcStorageMp: t.SrcStorageMp,
				DstStorageMp: t.DstStorageMp,
				DeletePolicy: t.DeletePolicy,
			}
			TransferTaskManager.Add(taskTmp)
		}

		t.Status = "源对象是目录，已添加所有文件的传输任务"
		return nil
	}

	// 如果是文件，直接传输
	return t.transferStdFile()
}

// transferStdFile 传输标准文件系统中的单个文件
func (t *TransferTask) transferStdFile() error {
	t.Status = "打开源文件"

	// 打开源文件
	rc, err := os.Open(t.SrcObjPath)
	if err != nil {
		return errors.Wrapf(err, "无法打开文件 %s", t.SrcObjPath)
	}
	defer rc.Close()

	// 获取文件信息
	info, err := rc.Stat()
	if err != nil {
		return errors.Wrapf(err, "无法获取文件信息 %s", t.SrcObjPath)
	}

	// 准备文件流
	mimetype := utils.GetMimeType(t.SrcObjPath)
	s := &stream.FileStream{
		Ctx: t.Ctx(),
		Obj: &model.Object{
			Name:     filepath.Base(t.SrcObjPath),
			Size:     info.Size(),
			Modified: info.ModTime(),
			IsFolder: false,
		},
		Reader:   rc,
		Mimetype: mimetype,
		Closers:  utils.NewClosers(rc),
	}

	// 设置总字节数并上传
	t.SetTotalBytes(info.Size())
	t.Status = "上传文件到目标存储"
	return op.Put(t.Ctx(), t.DstStorage, t.DstDirPath, s, t.SetProgress)
}

// removeStdTemp 删除标准文件系统中的临时文件
func (t *TransferTask) removeStdTemp() {
	// 检查文件是否存在且不是目录
	info, err := os.Stat(t.SrcObjPath)
	if err != nil || info.IsDir() {
		return
	}

	// 删除文件
	if err := os.Remove(t.SrcObjPath); err != nil {
		log.WithFields(log.Fields{
			"path":  t.SrcObjPath,
			"error": err,
		}).Error("无法删除临时文件")
	} else {
		log.WithField("path", t.SrcObjPath).Debug("已删除临时文件")
	}
}

// TransferFromObj 从对象存储传输文件到目标存储
func TransferFromObj(ctx context.Context, tempDir, dstDirPath string, deletePolicy DeletePolicy) error {
	// 获取源存储和实际路径
	srcStorage, srcObjActualPath, err := op.GetStorageAndActualPath(tempDir)
	if err != nil {
		return errors.Wrap(err, "无法获取源存储")
	}

	// 获取目标存储和实际路径
	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return errors.Wrap(err, "无法获取目标存储")
	}

	// 列出源目录中的对象
	objs, err := op.List(ctx, srcStorage, srcObjActualPath, model.ListArgs{})
	if err != nil {
		return errors.Wrapf(err, "无法列出源目录 [%s] 中的对象", tempDir)
	}

	// 获取任务创建者
	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User)

	// 为每个对象创建传输任务
	for _, obj := range objs {
		// 检查是否取消
		if utils.IsCanceled(ctx) {
			return ErrTransferCanceled
		}

		t := &TransferTask{
			TaskExtension: task.TaskExtension{
				Creator: taskCreator,
			},
			SrcObjPath:   stdpath.Join(srcObjActualPath, obj.GetName()),
			DstDirPath:   dstDirActualPath,
			SrcStorage:   srcStorage,
			DstStorage:   dstStorage,
			SrcStorageMp: srcStorage.GetStorage().MountPath,
			DstStorageMp: dstStorage.GetStorage().MountPath,
			DeletePolicy: deletePolicy,
		}
		TransferTaskManager.Add(t)
	}

	return nil
}

// transferFromObjPath 从对象存储传输文件或目录
func (t *TransferTask) transferFromObjPath() error {
	t.Status = "获取源对象信息"

	// 获取源对象
	srcObj, err := op.Get(t.Ctx(), t.SrcStorage, t.SrcObjPath)
	if err != nil {
		return errors.Wrapf(err, "无法获取源对象 [%s]", t.SrcObjPath)
	}

	// 如果是目录，为每个子对象创建传输任务
	if srcObj.IsDir() {
		t.Status = "源对象是目录，列出对象"
		objs, err := op.List(t.Ctx(), t.SrcStorage, t.SrcObjPath, model.ListArgs{})
		if err != nil {
			return errors.Wrapf(err, "无法列出源目录 [%s] 中的对象", t.SrcObjPath)
		}

		// 为每个子对象创建传输任务
		for _, obj := range objs {
			// 检查是否取消
			if utils.IsCanceled(t.Ctx()) {
				return ErrTransferCanceled
			}

			srcObjPath := stdpath.Join(t.SrcObjPath, obj.GetName())
			dstObjPath := stdpath.Join(t.DstDirPath, srcObj.GetName())

			TransferTaskManager.Add(&TransferTask{
				TaskExtension: task.TaskExtension{
					Creator: t.Creator,
				},
				SrcObjPath:   srcObjPath,
				DstDirPath:   dstObjPath,
				SrcStorage:   t.SrcStorage,
				DstStorage:   t.DstStorage,
				SrcStorageMp: t.SrcStorageMp,
				DstStorageMp: t.DstStorageMp,
				DeletePolicy: t.DeletePolicy,
			})
		}

		t.Status = "源对象是目录，已添加所有对象的传输任务"
		return nil
	}

	// 如果是文件，直接传输
	return t.transferObjFile()
}

// transferObjFile 传输对象存储中的单个文件
func (t *TransferTask) transferObjFile() error {
	t.Status = "获取源文件信息"

	// 获取源文件
	srcFile, err := op.Get(t.Ctx(), t.SrcStorage, t.SrcObjPath)
	if err != nil {
		return errors.Wrapf(err, "无法获取源文件 [%s]", t.SrcObjPath)
	}

	// 获取文件链接
	link, _, err := op.Link(t.Ctx(), t.SrcStorage, t.SrcObjPath, model.LinkArgs{})
	if err != nil {
		return errors.Wrapf(err, "无法获取源文件 [%s] 的链接", t.SrcObjPath)
	}
	defer link.Close()

	// 创建可定位流
	t.Status = "创建文件流"
	ss, err := stream.NewSeekableStream(&stream.FileStream{
		Obj: srcFile,
		Ctx: t.Ctx(),
	}, link)
	if err != nil {
		return errors.Wrapf(err, "无法为源文件 [%s] 创建流", t.SrcObjPath)
	}

	// 设置总字节数并上传
	t.SetTotalBytes(ss.GetSize())
	t.Status = "上传文件到目标存储"
	return op.Put(t.Ctx(), t.DstStorage, t.DstDirPath, ss, t.SetProgress)
}

// removeObjTemp 删除对象存储中的临时文件
func (t *TransferTask) removeObjTemp() {
	// 检查对象是否存在且不是目录
	srcObj, err := op.Get(t.Ctx(), t.SrcStorage, t.SrcObjPath)
	if err != nil || srcObj.IsDir() {
		return
	}

	// 删除对象
	if err = op.Remove(t.Ctx(), t.SrcStorage, t.SrcObjPath); err != nil {
		log.WithFields(log.Fields{
			"path":  t.SrcObjPath,
			"error": err,
		}).Error("无法删除临时对象")
	} else {
		log.WithField("path", t.SrcObjPath).Debug("已删除临时对象")
	}
}

// 为了保持向后兼容性，保留原有的函数名称，但实现重定向到新的函数
func transferStd(ctx context.Context, tempDir, dstDirPath string, deletePolicy DeletePolicy) error {
	return TransferFromStd(ctx, tempDir, dstDirPath, deletePolicy)
}

func transferObj(ctx context.Context, tempDir, dstDirPath string, deletePolicy DeletePolicy) error {
	return TransferFromObj(ctx, tempDir, dstDirPath, deletePolicy)
}
