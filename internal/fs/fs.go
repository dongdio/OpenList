package fs

import (
	"context"
	"io"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/internal/driver"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/utility/errs"
	"github.com/dongdio/OpenList/utility/task"
)

// 文件系统操作包
// 本包的目的是将挂载路径转换为实际路径，然后将实际路径传递给op包

type ListArgs struct {
	Refresh bool
	NoLog   bool
}

// List 列出指定路径下的对象
func List(ctx context.Context, path string, args *ListArgs) ([]model.Obj, error) {
	res, err := list(ctx, path, args)
	if err != nil && !args.NoLog {
		log.Errorf("failed to list %s: %+v", path, err)
	}
	return res, err
}

type GetArgs struct {
	NoLog bool
}

// Get 获取指定路径的对象
func Get(ctx context.Context, path string, args *GetArgs) (model.Obj, error) {
	res, err := get(ctx, path)
	if err != nil && !args.NoLog {
		log.Warnf("failed to get %s: %s", path, err)
	}
	return res, err
}

// Link 获取指定路径对象的链接
func Link(ctx context.Context, path string, args model.LinkArgs) (*model.Link, model.Obj, error) {
	res, file, err := link(ctx, path, args)
	if err != nil {
		log.Errorf("failed to link %s: %+v", path, err)
	}
	return res, file, err
}

// MakeDir 创建目录
func MakeDir(ctx context.Context, path string, lazyCache ...bool) error {
	err := makeDir(ctx, path, lazyCache...)
	if err != nil {
		log.Errorf("failed to make directory %s: %+v", path, err)
	}
	return err
}

// Move 移动文件或目录
func Move(ctx context.Context, srcPath, dstDirPath string, lazyCache ...bool) error {
	err := move(ctx, srcPath, dstDirPath, lazyCache...)
	if err != nil {
		log.Errorf("failed to move %s to %s: %+v", srcPath, dstDirPath, err)
	}
	return err
}

// MoveWithTask 创建移动任务
func MoveWithTask(ctx context.Context, srcPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	res, err := _move(ctx, srcPath, dstDirPath, lazyCache...)
	if err != nil {
		log.Errorf("failed to move %s to %s: %+v", srcPath, dstDirPath, err)
	}
	return res, err
}

// MoveWithTaskAndValidation 创建带验证的移动任务
func MoveWithTaskAndValidation(ctx context.Context, srcPath, dstDirPath string, validateExistence bool, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	res, err := _moveWithValidation(ctx, srcPath, dstDirPath, validateExistence, lazyCache...)
	if err != nil {
		log.Errorf("failed to move %s to %s: %+v", srcPath, dstDirPath, err)
	}
	return res, err
}

// Copy 复制文件或目录
func Copy(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	res, err := _copy(ctx, srcObjPath, dstDirPath, lazyCache...)
	if err != nil {
		log.Errorf("failed to copy %s to %s: %+v", srcObjPath, dstDirPath, err)
	}
	return res, err
}

// Rename 重命名文件或目录
func Rename(ctx context.Context, srcPath, dstName string, lazyCache ...bool) error {
	err := rename(ctx, srcPath, dstName, lazyCache...)
	if err != nil {
		log.Errorf("failed to rename %s to %s: %+v", srcPath, dstName, err)
	}
	return err
}

// Remove 删除文件或目录
func Remove(ctx context.Context, path string) error {
	err := remove(ctx, path)
	if err != nil {
		log.Errorf("failed to remove %s: %+v", path, err)
	}
	return err
}

// PutDirectly 直接上传文件
func PutDirectly(ctx context.Context, dstDirPath string, file model.FileStreamer, lazyCache ...bool) error {
	err := putDirectly(ctx, dstDirPath, file, lazyCache...)
	if err != nil {
		log.Errorf("failed to put file to %s: %+v", dstDirPath, err)
	}
	return err
}

// PutAsTask 创建上传任务
func PutAsTask(ctx context.Context, dstDirPath string, file model.FileStreamer) (task.TaskExtensionInfo, error) {
	t, err := putAsTask(ctx, dstDirPath, file)
	if err != nil {
		log.Errorf("failed to put file to %s: %+v", dstDirPath, err)
	}
	return t, err
}

// ArchiveMeta 获取归档元数据
func ArchiveMeta(ctx context.Context, path string, args model.ArchiveMetaArgs) (*model.ArchiveMetaProvider, error) {
	meta, err := archiveMeta(ctx, path, args)
	if err != nil {
		log.Errorf("failed to get archive metadata for %s: %+v", path, err)
	}
	return meta, err
}

// ArchiveList 列出归档内容
func ArchiveList(ctx context.Context, path string, args model.ArchiveListArgs) ([]model.Obj, error) {
	objs, err := archiveList(ctx, path, args)
	if err != nil {
		log.Errorf("failed to list archive [%s]%s: %+v", path, args.InnerPath, err)
	}
	return objs, err
}

// ArchiveDecompress 解压归档
func ArchiveDecompress(ctx context.Context, srcObjPath, dstDirPath string, args model.ArchiveDecompressArgs, lazyCache ...bool) (task.TaskExtensionInfo, error) {
	t, err := archiveDecompress(ctx, srcObjPath, dstDirPath, args, lazyCache...)
	if err != nil {
		log.Errorf("failed to decompress [%s]%s: %+v", srcObjPath, args.InnerPath, err)
	}
	return t, err
}

// ArchiveDriverExtract 提取归档内容
func ArchiveDriverExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (*model.Link, model.Obj, error) {
	l, obj, err := archiveDriverExtract(ctx, path, args)
	if err != nil {
		log.Errorf("failed to extract [%s]%s: %+v", path, args.InnerPath, err)
	}
	return l, obj, err
}

// ArchiveInternalExtract 内部提取归档内容
func ArchiveInternalExtract(ctx context.Context, path string, args model.ArchiveInnerArgs) (io.ReadCloser, int64, error) {
	reader, size, err := archiveInternalExtract(ctx, path, args)
	if err != nil {
		log.Errorf("failed to extract [%s]%s: %+v", path, args.InnerPath, err)
	}
	return reader, size, err
}

type GetStoragesArgs struct {
}

// GetStorage 获取存储驱动
func GetStorage(path string, args *GetStoragesArgs) (driver.Driver, error) {
	storageDriver, _, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, err
	}
	return storageDriver, nil
}

// Other 执行其他文件系统操作
func Other(ctx context.Context, args model.FsOtherArgs) (any, error) {
	res, err := other(ctx, args)
	if err != nil {
		log.Errorf("failed to execute other operation on %s: %+v", args.Path, err)
	}
	return res, err
}

// PutURL 从URL上传文件
func PutURL(ctx context.Context, path, dstName, urlStr string) error {
	storage, dstDirActualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return errors.WithMessage(err, "failed to get storage")
	}

	if storage.Config().NoUpload {
		return errors.WithStack(errs.UploadNotSupported)
	}

	_, isPutURL := storage.(driver.PutURL)
	_, isPutURLResult := storage.(driver.PutURLResult)
	if !isPutURL && !isPutURLResult {
		return errs.NotImplement
	}

	return op.PutURL(ctx, storage, dstDirActualPath, dstName, urlStr)
}