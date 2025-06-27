// Package fs provides filesystem operations for OpenList
package fs

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"net/http"
	"os"
	stdpath "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/xhofe/tache"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/driver"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/utility/errs"
	streamPkg "github.com/dongdio/OpenList/utility/stream"
	"github.com/dongdio/OpenList/utility/task"
)

// ArchiveDownloadTask represents a task for downloading and decompressing archive files
type ArchiveDownloadTask struct {
	task.TaskExtension
	model.ArchiveDecompressArgs
	status       string        // Current status of the task
	SrcObjPath   string        // Source object path
	DstDirPath   string        // Destination directory path
	srcStorage   driver.Driver // Source storage driver
	dstStorage   driver.Driver // Destination storage driver
	SrcStorageMp string        // Source storage mount path
	DstStorageMp string        // Destination storage mount path
}

// GetName returns a human-readable name for the archive download task
func (t *ArchiveDownloadTask) GetName() string {
	return fmt.Sprintf("decompress [%s](%s)[%s] to [%s](%s) with password <%s>",
		t.SrcStorageMp, t.SrcObjPath,
		t.InnerPath, t.DstStorageMp, t.DstDirPath, t.Password)
}

// GetStatus returns the current status of the archive download task
func (t *ArchiveDownloadTask) GetStatus() string {
	return t.status
}

// Run executes the archive download task
func (t *ArchiveDownloadTask) Run() error {
	t.ReinitCtx()
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	uploadTask, err := t.RunWithoutPushUploadTask()
	if err != nil {
		return err
	}

	ArchiveContentUploadTaskManager.Add(uploadTask)
	return nil
}

// RunWithoutPushUploadTask performs the decompression without adding the resulting upload task to the manager
// Returns the upload task for the extracted content
func (t *ArchiveDownloadTask) RunWithoutPushUploadTask() (*ArchiveContentUploadTask, error) {
	var err error

	// Initialize source storage if needed
	if t.srcStorage == nil {
		t.srcStorage, err = op.GetStorageByMountPath(t.SrcStorageMp)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to get source storage")
		}
	}

	// Get the archive tool and stream
	srcObj, archiveTool, streams, err := op.GetArchiveToolAndStream(t.Ctx(), t.srcStorage, t.SrcObjPath, model.LinkArgs{
		Header: http.Header{},
	})
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get archive tool and stream")
	}

	// Ensure all streams are closed
	defer func() {
		for _, stream := range streams {
			// Safely close each stream, ignoring nil errors
			if closeErr := stream.Close(); closeErr != nil {
				log.Errorf("failed to close file stream: %v", closeErr)
			}
		}
	}()

	var decompressProgressUpdater model.UpdateProgress

	// Handle full caching if enabled
	if t.CacheFull {
		var totalSize, currentSize int64 = 0, 0

		// Calculate total size of all streams
		for _, stream := range streams {
			totalSize += stream.GetSize()
		}

		t.SetTotalBytes(totalSize)
		t.status = "caching source object"

		// Cache each stream that doesn't already have a file
		for _, stream := range streams {
			if stream.GetFile() == nil {
				_, err = streamPkg.CacheFullInTempFileAndUpdateProgress(stream, func(progress float64) {
					// Calculate overall progress including already processed streams
					currentProgress := (float64(currentSize) + float64(stream.GetSize())*progress/100.0) / float64(totalSize)
					t.SetProgress(currentProgress)
				})

				if err != nil {
					return nil, errors.WithMessage(err, "failed to cache stream in temp file")
				}
			}
			currentSize += stream.GetSize()
		}

		t.SetProgress(100.0)
		// No need to update progress during decompression since we've already downloaded everything
		decompressProgressUpdater = func(_ float64) {}
	} else {
		// Use the task's progress updater directly
		decompressProgressUpdater = t.SetProgress
	}

	// Create temporary directory for decompression
	t.status = "decompressing archive"
	tempDir, err := os.MkdirTemp(conf.Conf.TempDir, "dir-*")
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create temporary directory")
	}

	// Perform decompression
	err = archiveTool.Decompress(streams, tempDir, t.ArchiveInnerArgs, decompressProgressUpdater)
	if err != nil {
		// Clean up the temporary directory on error
		_ = os.RemoveAll(tempDir)
		return nil, errors.WithMessage(err, "failed to decompress archive")
	}

	// Extract base name without extension for the output
	baseName := strings.TrimSuffix(srcObj.GetName(), stdpath.Ext(srcObj.GetName()))

	// Create upload task for the decompressed content
	uploadTask := &ArchiveContentUploadTask{
		TaskExtension: task.TaskExtension{
			Creator: t.GetCreator(),
		},
		ObjName:      baseName,
		InPlace:      !t.PutIntoNewDir,
		FilePath:     tempDir,
		DstDirPath:   t.DstDirPath,
		dstStorage:   t.dstStorage,
		DstStorageMp: t.DstStorageMp,
	}

	return uploadTask, nil
}

// ArchiveDownloadTaskManager manages asynchronous archive download tasks
var ArchiveDownloadTaskManager *tache.Manager[*ArchiveDownloadTask]

// ArchiveContentUploadTask represents a task for uploading decompressed archive content
type ArchiveContentUploadTask struct {
	task.TaskExtension
	status       string        // Current status of the task
	ObjName      string        // Object name
	InPlace      bool          // Whether to upload in place or in a new directory
	FilePath     string        // Path to the file or directory to upload
	DstDirPath   string        // Destination directory path
	dstStorage   driver.Driver // Destination storage driver
	DstStorageMp string        // Destination storage mount path
	finalized    bool          // Whether the task has been finalized (resources cleaned up)
}

// GetName returns a human-readable name for the archive content upload task
func (t *ArchiveContentUploadTask) GetName() string {
	return fmt.Sprintf("upload %s to [%s](%s)", t.ObjName, t.DstStorageMp, t.DstDirPath)
}

// GetStatus returns the current status of the archive content upload task
func (t *ArchiveContentUploadTask) GetStatus() string {
	return t.status
}

// Run executes the archive content upload task
func (t *ArchiveContentUploadTask) Run() error {
	t.ReinitCtx()
	t.ClearEndTime()
	t.SetStartTime(time.Now())
	defer func() { t.SetEndTime(time.Now()) }()

	// Run with a callback that adds new tasks to the manager
	return t.RunWithNextTaskCallback(func(nextTask *ArchiveContentUploadTask) error {
		ArchiveContentUploadTaskManager.Add(nextTask)
		return nil
	})
}

// RunWithNextTaskCallback executes the upload task with a custom callback for handling subtasks
func (t *ArchiveContentUploadTask) RunWithNextTaskCallback(nextTaskCallback func(nextTask *ArchiveContentUploadTask) error) error {
	var err error

	// Initialize destination storage if needed
	if t.dstStorage == nil {
		t.dstStorage, err = op.GetStorageByMountPath(t.DstStorageMp)
		if err != nil {
			return errors.WithMessage(err, "failed to get destination storage")
		}
	}

	// Get file info
	fileInfo, err := os.Stat(t.FilePath)
	if err != nil {
		return errors.WithMessage(err, "failed to get file info")
	}

	// Handle directory upload
	if fileInfo.IsDir() {
		return t.processDirectory(nextTaskCallback)
	}

	// Handle file upload
	return t.processFile(fileInfo)
}

// processDirectory handles uploading a directory and its contents
func (t *ArchiveContentUploadTask) processDirectory(nextTaskCallback func(nextTask *ArchiveContentUploadTask) error) error {
	t.status = "processing directory contents"

	// Determine destination path
	destinationPath := t.DstDirPath
	if !t.InPlace {
		// Create a new directory with the object name
		destinationPath = stdpath.Join(destinationPath, t.ObjName)

		// Create the directory in the destination storage
		err := op.MakeDir(t.Ctx(), t.dstStorage, destinationPath)
		if err != nil {
			return errors.WithMessage(err, "failed to create destination directory")
		}
	}

	// Read directory entries
	entries, err := os.ReadDir(t.FilePath)
	if err != nil {
		return errors.WithMessage(err, "failed to read directory contents")
	}

	// Process each entry
	var es error
	for _, entry := range entries {
		// Check if operation has been canceled
		select {
		case <-t.Ctx().Done():
			t.status = "operation canceled"
			return nil
		default:
		}

		// Move the entry to a temporary path
		var nextFilePath string
		entryPath := stdpath.Join(t.FilePath, entry.Name())

		if entry.IsDir() {
			nextFilePath, err = moveToTempPath(entryPath, "dir-")
		} else {
			nextFilePath, err = moveToTempPath(entryPath, "file-")
		}

		if err != nil {
			es = stderrors.Join(es, errors.WithMessagef(err, "failed to move %s to temp path", entry.Name()))
			continue
		}

		// Create subtask for the entry
		subtask := &ArchiveContentUploadTask{
			TaskExtension: task.TaskExtension{
				Creator: t.GetCreator(),
			},
			ObjName:      entry.Name(),
			InPlace:      false,
			FilePath:     nextFilePath,
			DstDirPath:   destinationPath,
			dstStorage:   t.dstStorage,
			DstStorageMp: t.DstStorageMp,
		}

		// Schedule the subtask
		if err = nextTaskCallback(subtask); err != nil {
			es = stderrors.Join(es, errors.WithMessagef(err, "failed to schedule subtask for %s", entry.Name()))
		}
	}

	return es
}

// processFile handles uploading a single file
func (t *ArchiveContentUploadTask) processFile(fileInfo os.FileInfo) error {
	// Set total bytes for progress tracking
	t.SetTotalBytes(fileInfo.Size())

	// Open the file
	file, err := os.Open(t.FilePath)
	if err != nil {
		return errors.WithMessage(err, "failed to open file")
	}
	defer file.Close()

	// Create a file stream
	fileStream := &streamPkg.FileStream{
		Obj: &model.Object{
			Name:     t.ObjName,
			Size:     fileInfo.Size(),
			Modified: time.Now(),
		},
		Mimetype:     mime.TypeByExtension(filepath.Ext(t.ObjName)),
		WebPutAsTask: true,
		Reader:       file,
	}
	fileStream.Closers.Add(file)

	// Upload the file
	t.status = "uploading file"
	err = op.Put(t.Ctx(), t.dstStorage, t.DstDirPath, fileStream, t.SetProgress, true)
	if err != nil {
		return errors.WithMessage(err, "failed to upload file")
	}

	t.status = "completed"
	return nil
}

// Cancel cancels the task and cleans up resources if allowed
func (t *ArchiveContentUploadTask) Cancel() {
	t.TaskExtension.Cancel()
	if !conf.Conf.Tasks.AllowRetryCanceled {
		t.deleteSrcFile()
	}
}

// deleteSrcFile removes the source file or directory if it hasn't been finalized yet
func (t *ArchiveContentUploadTask) deleteSrcFile() {
	if !t.finalized {
		_ = os.RemoveAll(t.FilePath)
		t.finalized = true
	}
}

// moveToTempPath moves a file or directory to a temporary path
func moveToTempPath(path, prefix string) (string, error) {
	newPath, err := genTempFileName(prefix)
	if err != nil {
		return "", errors.WithMessage(err, "failed to generate temporary file name")
	}

	err = os.Rename(path, newPath)
	if err != nil {
		return "", errors.WithMessage(err, "failed to rename file")
	}

	return newPath, nil
}

// genTempFileName generates a unique temporary file name with the given prefix
// It uses a random number and ensures the path doesn't already exist
func genTempFileName(prefix string) (string, error) {
	// Create a new random source with current time seed
	// (replaces deprecated rand.Seed in Go 1.20+)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	const maxRetries = 1000
	for retry := 0; retry < maxRetries; retry++ {
		// Generate random filename
		randomNum := r.Uint32()
		newPath := stdpath.Join(conf.Conf.TempDir, prefix+strconv.FormatUint(uint64(randomNum), 10))

		// Check if the path already exists
		_, err := os.Stat(newPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Path doesn't exist, we can use it
				return newPath, nil
			}
			// Some other error occurred
			return "", errors.WithMessage(err, "failed to check if path exists")
		}
		// Path exists, try again
	}

	return "", errors.New("failed to generate unique temporary file name after multiple attempts")
}

// archiveContentUploadTaskManagerType extends the tache.Manager with additional functionality
type archiveContentUploadTaskManagerType struct {
	*tache.Manager[*ArchiveContentUploadTask]
}

// Remove removes a task by ID and ensures its resources are cleaned up
func (m *archiveContentUploadTaskManagerType) Remove(id string) {
	if uploadTask, exists := m.GetByID(id); exists {
		uploadTask.deleteSrcFile()
		m.Manager.Remove(id)
	}
}

// RemoveAll removes all tasks and ensures their resources are cleaned up
func (m *archiveContentUploadTaskManagerType) RemoveAll() {
	tasks := m.GetAll()
	for _, v := range tasks {
		m.Remove(v.GetID())
	}
}

// RemoveByState removes tasks with the specified states and ensures their resources are cleaned up
func (m *archiveContentUploadTaskManagerType) RemoveByState(states ...tache.State) {
	tasks := m.GetByState(states...)
	for _, v := range tasks {
		m.Remove(v.GetID())
	}
}

// RemoveByCondition removes tasks that match the specified condition and ensures their resources are cleaned up
func (m *archiveContentUploadTaskManagerType) RemoveByCondition(condition func(task *ArchiveContentUploadTask) bool) {
	tasks := m.GetByCondition(condition)
	for _, v := range tasks {
		m.Remove(v.GetID())
	}
}

// ArchiveContentUploadTaskManager manages archive content upload tasks
var ArchiveContentUploadTaskManager = &archiveContentUploadTaskManagerType{
	Manager: nil,
}

// archiveMeta retrieves metadata for an archive at the specified path
func archiveMeta(ctx context.Context, path string, args model.ArchiveMetaArgs) (*model.ArchiveMetaProvider, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get storage")
	}
	return op.GetArchiveMeta(ctx, storage, actualPath, args)
}

// archiveList lists the contents of an archive at the specified path
func archiveList(ctx context.Context, path string, args model.ArchiveListArgs) ([]model.Obj, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get storage")
	}
	return op.ListArchive(ctx, storage, actualPath, args)
}

// archiveDecompress decompresses an archive to a destination directory
// It tries to use the storage's native decompression if available, otherwise creates a task
func archiveDecompress(ctx context.Context, srcObjPath, dstDirPath string, args model.ArchiveDecompressArgs, lazyCache ...bool) (task.TaskExtensionInfo, error) {
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

	// If source and destination are on the same storage, try to use the storage's native decompression
	if srcStorage.GetStorage() == dstStorage.GetStorage() {
		err = op.ArchiveDecompress(ctx, srcStorage, srcObjActualPath, dstDirActualPath, args, lazyCache...)
		if !errors.Is(err, errs.NotImplement) {
			return nil, err
		}
		// If not implemented, fall back to task-based approach
	}

	// Get the task creator (user) from context
	taskCreator, _ := ctx.Value("user").(*model.User)

	// Create decompression task
	downloadTask := &ArchiveDownloadTask{
		TaskExtension: task.TaskExtension{
			Creator: taskCreator,
		},
		ArchiveDecompressArgs: args,
		srcStorage:            srcStorage,
		dstStorage:            dstStorage,
		SrcObjPath:            srcObjActualPath,
		DstDirPath:            dstDirActualPath,
		SrcStorageMp:          srcStorage.GetStorage().MountPath,
		DstStorageMp:          dstStorage.GetStorage().MountPath,
	}

	// Handle synchronous execution if requested
	if ctx.Value(consts.NoTaskKey) != nil {
		return handleSynchronousDecompression(ctx, downloadTask, srcObjPath)
	}

	// Add task to manager for asynchronous execution
	ArchiveDownloadTaskManager.Add(downloadTask)
	return downloadTask, nil
}

// handleSynchronousDecompression performs decompression synchronously
func handleSynchronousDecompression(ctx context.Context, task *ArchiveDownloadTask, srcObjPath string) (task.TaskExtensionInfo, error) {
	// Run the download task
	uploadTask, err := task.RunWithoutPushUploadTask()
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to download archive [%s]", srcObjPath)
	}
	defer uploadTask.deleteSrcFile()

	// Define recursive callback for handling nested content
	var processCallback func(t *ArchiveContentUploadTask) error
	processCallback = func(t *ArchiveContentUploadTask) error {
		err = t.RunWithNextTaskCallback(processCallback)
		t.deleteSrcFile()
		return err
	}

	// Process the upload task
	return nil, uploadTask.RunWithNextTaskCallback(processCallback)
}

// archiveDriverExtract extracts a file from an archive using the storage driver
func archiveDriverExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (*model.Link, model.Obj, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "failed to get storage")
	}
	return op.DriverExtract(ctx, storage, actualPath, args)
}

// archiveInternalExtract extracts a file from an archive using internal extraction
func archiveInternalExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (io.ReadCloser, int64, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, 0, errors.WithMessage(err, "failed to get storage")
	}
	return op.InternalExtract(ctx, storage, actualPath, args)
}