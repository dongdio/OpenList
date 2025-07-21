package fs

import (
	"context"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

// 自定义错误类型
var (
	// ErrMakeDirFailed 创建目录失败错误
	ErrMakeDirFailed = errors.New("failed to create directory")

	// ErrMoveFailed 移动失败错误
	ErrMoveFailed = errors.New("failed to move object")

	// ErrRenameFailed 重命名失败错误
	ErrRenameFailed = errors.New("failed to rename object")

	// ErrRemoveFailed 删除失败错误
	ErrRemoveFailed = errors.New("failed to remove object")

	// ErrOtherOperationFailed 其他操作失败错误
	ErrOtherOperationFailed = errors.New("operation failed")
)

// makeDir 创建目录
// 参数:
//   - ctx: 上下文
//   - path: 目录路径
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func makeDir(ctx context.Context, path string, lazyCache ...bool) error {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return errors.Wrap(ErrStorageNotFound, err.Error())
	}

	err = op.MakeDir(ctx, storage, actualPath, lazyCache...)
	if err != nil {
		return errors.Wrap(ErrMakeDirFailed, err.Error())
	}

	return nil
}

// move 移动文件或目录
// 参数:
//   - ctx: 上下文
//   - srcPath: 源路径
//   - dstDirPath: 目标目录路径
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func move(ctx context.Context, srcPath, dstDirPath string, lazyCache ...bool) error {
	srcStorage, srcActualPath, err := op.GetStorageAndActualPath(srcPath)
	if err != nil {
		return errors.Wrapf(ErrStorageNotFound, "source: %s", err.Error())
	}

	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return errors.Wrapf(ErrStorageNotFound, "destination: %s", err.Error())
	}

	if srcStorage.GetStorage() != dstStorage.GetStorage() {
		return errors.Wrap(errs.MoveBetweenTwoStorages, "source and destination are on different storages")
	}

	err = op.Move(ctx, srcStorage, srcActualPath, dstDirActualPath, lazyCache...)
	if err != nil {
		return errors.Wrap(ErrMoveFailed, err.Error())
	}

	return nil
}

// rename 重命名文件或目录
// 参数:
//   - ctx: 上下文
//   - srcPath: 源路径
//   - dstName: 新名称
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func rename(ctx context.Context, srcPath, dstName string, lazyCache ...bool) error {
	storage, srcActualPath, err := op.GetStorageAndActualPath(srcPath)
	if err != nil {
		return errors.Wrap(ErrStorageNotFound, err.Error())
	}

	err = op.Rename(ctx, storage, srcActualPath, dstName, lazyCache...)
	if err != nil {
		return errors.Wrap(ErrRenameFailed, err.Error())
	}

	return nil
}

// remove 删除文件或目录
// 参数:
//   - ctx: 上下文
//   - path: 对象路径
//
// 返回:
//   - error: 错误信息
func remove(ctx context.Context, path string) error {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return errors.Wrap(ErrStorageNotFound, err.Error())
	}

	err = op.Remove(ctx, storage, actualPath)
	if err != nil {
		return errors.Wrap(ErrRemoveFailed, err.Error())
	}

	return nil
}

// other 执行其他文件系统操作
// 参数:
//   - ctx: 上下文
//   - args: 操作参数
//
// 返回:
//   - any: 操作结果
//   - error: 错误信息
func other(ctx context.Context, args model.FsOtherArgs) (any, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(args.Path)
	if err != nil {
		return nil, errors.Wrap(ErrStorageNotFound, err.Error())
	}

	args.Path = actualPath
	result, err := op.Other(ctx, storage, args)
	if err != nil {
		return nil, errors.Wrap(ErrOtherOperationFailed, err.Error())
	}

	return result, nil
}
