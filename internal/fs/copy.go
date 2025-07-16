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

// CopyTask represents an asynchronous file/directory copy operation
type CopyTask struct {
	task.TaskExtension
	Status       string        `json:"-"`              // Current status (not persisted)
	SrcObjPath   string        `json:"src_path"`       // Source object path
	DstDirPath   string        `json:"dst_path"`       // Destination directory path
	SrcStorageMp string        `json:"src_storage_mp"` // Source storage mount path
	DstStorageMp string        `json:"dst_storage_mp"` // Destination storage mount path
	srcStorage   driver.Driver // Source storage driver (not persisted)
	dstStorage   driver.Driver // Destination storage driver (not persisted)
}

// GetName returns a human-readable name for the copy task
func (t *CopyTask) GetName() string {
	return fmt.Sprintf("copy [%s](%s) to [%s](%s)",
		t.SrcStorageMp, t.SrcObjPath,
		t.DstStorageMp, t.DstDirPath)
}

// GetStatus returns the current status of the copy task
func (t *CopyTask) GetStatus() string {
	return t.Status
}

// Run executes the copy task
// It initializes the storage drivers if needed and delegates to copyBetween2Storages
func (t *CopyTask) Run() error {
	// Initialize task timing information
	if err := t.ReinitCtx(); err != nil {
		return err
	}
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	var err error

	// Initialize source storage if needed
	if t.srcStorage == nil {
		t.srcStorage, err = op.GetStorageByMountPath(t.SrcStorageMp)
		if err != nil {
			return errors.WithMessage(err, "failed to get source storage")
		}
	}

	// Initialize destination storage if needed
	if t.dstStorage == nil {
		t.dstStorage, err = op.GetStorageByMountPath(t.DstStorageMp)
		if err != nil {
			return errors.WithMessage(err, "failed to get destination storage")
		}
	}

	// Perform the copy operation
	return copyBetween2Storages(t, t.srcStorage, t.dstStorage, t.SrcObjPath, t.DstDirPath)
}

// CopyTaskManager manages asynchronous copy tasks
var CopyTaskManager *tache.Manager[*CopyTask]

// _copy creates a copy task for copying files/directories between storages
// It tries to use storage's native copy capability if source and destination are on the same storage
func _copy(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	// Get source storage and path
	srcStorage, srcObjActualPath, err := op.GetStorageAndActualPath(srcObjPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get source storage")
	}

	// Get destination storage and path
	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get destination storage")
	}

	// If source and destination are on the same storage, try to use the storage's native copy capability
	if srcStorage.GetStorage() == dstStorage.GetStorage() {
		err = op.Copy(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)

		// If the storage supports native copy, return the result
		if !errors.Is(err, errs.NotImplement) && !errors.Is(err, errs.NotSupport) {
			return nil, err
		}
		// Otherwise, fall back to the task-based approach
	}

	// Handle no-task context flag for synchronous copy
	if ctx.Value(consts.NoTaskKey) != nil {
		return handleSynchronousCopy(ctx, srcStorage, dstStorage, srcObjPath, srcObjActualPath, dstDirActualPath)
	}

	// Get the task creator (user) from context
	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User)

	// Create and configure the copy task
	t := &CopyTask{
		TaskExtension: task.TaskExtension{
			Creator: taskCreator,
			ApiUrl:  common.GetApiUrl(ctx),
		},
		srcStorage:   srcStorage,
		dstStorage:   dstStorage,
		SrcObjPath:   srcObjActualPath,
		DstDirPath:   dstDirActualPath,
		SrcStorageMp: srcStorage.GetStorage().MountPath,
		DstStorageMp: dstStorage.GetStorage().MountPath,
	}

	// Add the task to the manager
	CopyTaskManager.Add(t)

	return t, nil
}

// handleSynchronousCopy performs a synchronous copy operation when tasks are disabled
// This is used for direct file copies that shouldn't be queued
func handleSynchronousCopy(ctx context.Context, srcStorage, dstStorage driver.Driver,
	srcObjPath, srcObjActualPath, dstDirActualPath string) (task.TaskExtensionInfo, error) {

	// Get the source object
	srcObj, err := op.Get(ctx, srcStorage, srcObjActualPath)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to get source object [%s]", srcObjPath)
	}

	// For non-directory objects, perform a direct copy
	if !srcObj.IsDir() {
		// Get a link to the source file
		linkRes, _, err := op.Link(ctx, srcStorage, srcObjActualPath, model.LinkArgs{})
		if err != nil {
			return nil, errors.WithMessagef(err, "failed to get link for [%s]", srcObjPath)
		}

		// Create a file stream for the source object
		fileStream, err := stream.NewSeekableStream(&stream.FileStream{
			Obj: srcObj,
			Ctx: ctx,
		}, linkRes)
		if err != nil {
			_ = linkRes.Close()
			return nil, errors.WithMessagef(err, "failed to create stream for [%s]", srcObjPath)
		}

		// Perform the direct upload to the destination
		return nil, op.Put(ctx, dstStorage, dstDirActualPath, fileStream, nil, false)
	}

	// Directory handling would go through the task-based approach anyway
	return nil, errors.New("synchronous copy only supports files, not directories")
}

// copyBetween2Storages copies a file or directory between two storages
// This handles the logic for both files and directories
func copyBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver,
	srcObjPath, dstDirPath string) error {

	// Update task status and get the source object
	t.Status = "getting source object"
	srcObj, err := op.Get(t.Ctx(), srcStorage, srcObjPath)
	if err != nil {
		return errors.WithMessagef(err, "failed to get source object [%s]", srcObjPath)
	}

	// Handle directory copy
	if srcObj.IsDir() {
		return copyDirectoryBetween2Storages(t, srcStorage, dstStorage, srcObj, srcObjPath, dstDirPath)
	}

	// Handle file copy
	return copyFileBetween2Storages(t, srcStorage, dstStorage, srcObjPath, dstDirPath)
}

// copyDirectoryBetween2Storages handles copying a directory between two storages
// It creates the destination directory and schedules tasks for each child item
func copyDirectoryBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver,
	srcDirObj model.Obj, srcObjPath, dstDirPath string) error {

	// List objects in the source directory
	t.Status = "listing source directory contents"
	dirContents, err := op.List(t.Ctx(), srcStorage, srcObjPath, model.ListArgs{})
	if err != nil {
		return errors.WithMessagef(err, "failed to list contents of [%s]", srcObjPath)
	}

	// Schedule copy tasks for each item in the directory
	for _, childObj := range dirContents {
		// Check if operation has been canceled
		if utils.IsCanceled(t.Ctx()) {
			t.Status = "operation canceled"
			return nil
		}

		// Create and schedule a copy task for the child
		CopyTaskManager.Add(&CopyTask{
			TaskExtension: task.TaskExtension{
				Creator: t.GetCreator(),
				ApiUrl:  t.ApiUrl,
			},
			srcStorage:   srcStorage,
			dstStorage:   dstStorage,
			SrcObjPath:   stdpath.Join(srcObjPath, childObj.GetName()),
			DstDirPath:   stdpath.Join(dstDirPath, srcDirObj.GetName()),
			SrcStorageMp: srcStorage.GetStorage().MountPath,
			DstStorageMp: dstStorage.GetStorage().MountPath,
		})
	}

	t.Status = "all child copy tasks scheduled"
	return nil
}

// copyFileBetween2Storages handles copying a file between two storages
// It gets a link to the source file and uploads it to the destination
func copyFileBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver,
	srcFilePath, dstDirPath string) error {

	// Get the source file
	srcFile, err := op.Get(t.Ctx(), srcStorage, srcFilePath)
	if err != nil {
		return errors.WithMessagef(err, "failed to get source file [%s]", srcFilePath)
	}

	// Set total bytes for progress tracking
	t.SetTotalBytes(srcFile.GetSize())

	var linkRes *model.Link
	// Get a link to the source file
	linkRes, _, err = op.Link(t.Ctx(), srcStorage, srcFilePath, model.LinkArgs{})
	if err != nil {
		return errors.WithMessagef(err, "failed to get link for [%s]", srcFilePath)
	}

	// Create a file stream for the source file
	fileStream, err := stream.NewSeekableStream(&stream.FileStream{
		Obj: srcFile,
		Ctx: t.Ctx(),
	}, linkRes)

	if err != nil {
		_ = linkRes.Close()
		return errors.WithMessagef(err, "failed to create stream for [%s]", srcFilePath)
	}

	// Upload the file to the destination
	// Pass the progress callback function to update task progress
	return op.Put(t.Ctx(), dstStorage, dstDirPath, fileStream, t.SetProgress, true)
}