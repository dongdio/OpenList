package fs

import (
	"context"
	"fmt"
	stdpath "path"
	"time"

	"github.com/pkg/errors"
	"github.com/xhofe/tache"

	"github.com/dongdio/OpenList/internal/driver"
	"github.com/dongdio/OpenList/internal/errs"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/internal/task"
	"github.com/dongdio/OpenList/pkg/utils"
)

// MoveTask represents an asynchronous file/directory move operation
type MoveTask struct {
	task.TaskExtension
	Status       string        `json:"-"`              // Current status (not persisted)
	SrcObjPath   string        `json:"src_path"`       // Source object path
	DstDirPath   string        `json:"dst_path"`       // Destination directory path
	SrcStorageMp string        `json:"src_storage_mp"` // Source storage mount path
	DstStorageMp string        `json:"dst_storage_mp"` // Destination storage mount path
	srcStorage   driver.Driver // Source storage driver (not persisted)
	dstStorage   driver.Driver // Destination storage driver (not persisted)
}

// GetName returns a human-readable name for the move task
func (t *MoveTask) GetName() string {
	return fmt.Sprintf("move [%s](%s) to [%s](%s)",
		t.SrcStorageMp, t.SrcObjPath,
		t.DstStorageMp, t.DstDirPath)
}

// GetStatus returns the current status of the move task
func (t *MoveTask) GetStatus() string {
	return t.Status
}

// Run executes the move task
// It initializes the storage drivers if needed and delegates to moveBetween2Storages
func (t *MoveTask) Run() error {
	// Initialize task timing information
	t.ReinitCtx()
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

	// Perform the move operation
	return moveBetween2Storages(t, t.srcStorage, t.dstStorage, t.SrcObjPath, t.DstDirPath)
}

// MoveTaskManager manages asynchronous move tasks
var MoveTaskManager *tache.Manager[*MoveTask]

// moveBetween2Storages moves a file or directory between two storages
// This handles the logic for both files and directories
func moveBetween2Storages(t *MoveTask, srcStorage, dstStorage driver.Driver, srcObjPath, dstDirPath string) error {
	// Update task status and get the source object
	t.Status = "getting src object"
	srcObj, err := op.Get(t.Ctx(), srcStorage, srcObjPath)
	if err != nil {
		return errors.WithMessagef(err, "failed to get source object [%s]", srcObjPath)
	}

	// Handle directory move
	if srcObj.IsDir() {
		return moveDirectoryBetween2Storages(t, srcStorage, dstStorage, srcObj, srcObjPath, dstDirPath)
	} else {
		// Handle file move
		return moveFileBetween2Storages(t, srcStorage, dstStorage, srcObjPath, dstDirPath)
	}
}

// moveDirectoryBetween2Storages handles moving a directory between two storages
// It creates the destination directory and schedules tasks for each child item
func moveDirectoryBetween2Storages(t *MoveTask, srcStorage, dstStorage driver.Driver,
	srcDirObj model.Obj, srcObjPath, dstDirPath string) error {

	// List objects in the source directory
	t.Status = "listing source directory contents"
	dirContents, err := op.List(t.Ctx(), srcStorage, srcObjPath, model.ListArgs{})
	if err != nil {
		return errors.WithMessagef(err, "failed to list contents of [%s]", srcObjPath)
	}

	// Create the destination directory
	dstObjPath := stdpath.Join(dstDirPath, srcDirObj.GetName())
	t.Status = "creating destination directory"

	err = op.MakeDir(t.Ctx(), dstStorage, dstObjPath)
	if err != nil {
		// Provide specific error message for upload not supported
		if errors.Is(err, errs.UploadNotSupported) {
			return errors.WithMessagef(err,
				"destination storage [%s] does not support creating directories",
				dstStorage.GetStorage().MountPath)
		}
		return errors.WithMessagef(err,
			"failed to create destination directory [%s] in storage [%s]",
			dstObjPath, dstStorage.GetStorage().MountPath)
	}

	// Schedule move tasks for each item in the directory
	for _, childObj := range dirContents {
		// Check if operation has been canceled
		if utils.IsCanceled(t.Ctx()) {
			t.Status = "operation canceled"
			return nil
		}

		// Create and schedule a move task for the child
		MoveTaskManager.Add(&MoveTask{
			TaskExtension: task.TaskExtension{
				Creator: t.GetCreator(),
			},
			srcStorage:   srcStorage,
			dstStorage:   dstStorage,
			SrcObjPath:   stdpath.Join(srcObjPath, childObj.GetName()),
			DstDirPath:   dstObjPath,
			SrcStorageMp: srcStorage.GetStorage().MountPath,
			DstStorageMp: dstStorage.GetStorage().MountPath,
		})
	}

	// Remove the source directory after all items have been scheduled for move
	t.Status = "cleaning up source directory"
	err = op.Remove(t.Ctx(), srcStorage, srcObjPath)
	if err != nil {
		t.Status = "completed (source directory cleanup pending)"
		return nil // Don't fail the move if we can't delete the source
	}

	t.Status = "completed"
	return nil
}

// moveFileBetween2Storages handles moving a file between two storages
// It first copies the file to the destination, then removes it from the source
func moveFileBetween2Storages(tsk *MoveTask, srcStorage, dstStorage driver.Driver,
	srcFilePath, dstDirPath string) error {

	tsk.Status = "copying file to destination"

	// Create a copy task to handle the file copy operation
	copyTask := &CopyTask{
		TaskExtension: task.TaskExtension{
			Creator: tsk.GetCreator(),
		},
		srcStorage:   srcStorage,
		dstStorage:   dstStorage,
		SrcObjPath:   srcFilePath,
		DstDirPath:   dstDirPath,
		SrcStorageMp: srcStorage.GetStorage().MountPath,
		DstStorageMp: dstStorage.GetStorage().MountPath,
	}

	// Share the context with the copy task
	copyTask.SetCtx(tsk.Ctx())

	// Perform the copy operation
	err := copyBetween2Storages(copyTask, srcStorage, dstStorage, srcFilePath, dstDirPath)
	if err != nil {
		// Provide specific error message for upload not supported
		if errors.Is(err, errs.UploadNotSupported) {
			return errors.WithMessagef(err,
				"destination storage [%s] does not support file uploads",
				dstStorage.GetStorage().MountPath)
		}
		return errors.WithMessagef(err,
			"failed to copy [%s] to destination storage [%s]",
			srcFilePath, dstStorage.GetStorage().MountPath)
	}

	// Update progress to 50% after successful copy
	tsk.SetProgress(50)
	tsk.Status = "verifying file in destination"

	// check if the file exists in the destination
	dstFilePath := stdpath.Join(dstDirPath, stdpath.Base(srcFilePath))
	const (
		maxRetries    = 3
		retryInterval = time.Second
	)
	var checkErr error
	for range maxRetries {
		_, checkErr = op.Get(tsk.Ctx(), dstStorage, dstFilePath)
		if checkErr == nil {
			break
		}
		time.Sleep(retryInterval)
	}
	if checkErr != nil {
		return errors.WithMessagef(checkErr, "failed to verify file[%s] in destination after copy", dstFilePath)
	}

	// Delete the source file after successful copy
	tsk.Status = "deleting source file"
	err = op.Remove(tsk.Ctx(), srcStorage, srcFilePath)
	if err != nil {
		return errors.WithMessagef(err,
			"failed to delete source file [%s] from storage [%s] after successful copy",
			srcFilePath, srcStorage.GetStorage().MountPath)
	}

	// Set complete progress and status
	tsk.SetProgress(100)
	tsk.Status = "completed"
	return nil
}

// _move creates a move task for moving files/directories between storages
// It tries to use storage's native move capability if source and destination are on the same storage
func _move(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
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

	// If source and destination are on the same storage, try to use the storage's native move capability
	if srcStorage.GetStorage() == dstStorage.GetStorage() {
		err = op.Move(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)

		// If the storage supports native move, return the result
		if !errors.Is(err, errs.NotImplement) && !errors.Is(err, errs.NotSupport) {
			return nil, err
		}
		// Otherwise, fall back to the task-based approach
	}

	// Get the task creator (user) from context
	taskCreator, _ := ctx.Value("user").(*model.User)

	// Create and configure the move task
	t := &MoveTask{
		TaskExtension: task.TaskExtension{
			Creator: taskCreator,
		},
		srcStorage:   srcStorage,
		dstStorage:   dstStorage,
		SrcObjPath:   srcObjActualPath,
		DstDirPath:   dstDirActualPath,
		SrcStorageMp: srcStorage.GetStorage().MountPath,
		DstStorageMp: dstStorage.GetStorage().MountPath,
	}

	// Add the task to the manager
	MoveTaskManager.Add(t)

	return t, nil
}
