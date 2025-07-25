package fs

import (
	"context"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/task"
)

// 自定义错误类型
var (
	ErrMakeDirFailed = errors.New("failed to make directory")
	ErrRenameFailed  = errors.New("failed to rename object")
	ErrRemoveFailed  = errors.New("failed to remove object")
	ErrOtherFailed   = errors.New("operation failed")
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
	// 参数验证
	if path == "" {
		return errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return errors.WithMessage(err, "failed get storage")
	}

	// 创建目录
	err = op.MakeDir(ctx, storage, actualPath, lazyCache...)
	if err != nil {
		return errors.Wrap(ErrMakeDirFailed, err.Error())
	}

	return nil
}

// rename 重命名对象
// 参数:
//   - ctx: 上下文
//   - srcPath: 源路径
//   - dstName: 目标名称
//   - lazyCache: 是否延迟缓存
//
// 返回:
//   - error: 错误信息
func rename(ctx context.Context, srcPath, dstName string, lazyCache ...bool) error {
	// 参数验证
	if srcPath == "" || dstName == "" {
		return errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, srcActualPath, err := getStorageWithCache(srcPath)
	if err != nil {
		return errors.WithMessage(err, "failed get storage")
	}

	// 重命名对象
	err = op.Rename(ctx, storage, srcActualPath, dstName, lazyCache...)
	if err != nil {
		return errors.Wrap(ErrRenameFailed, err.Error())
	}

	return nil
}

// remove 删除对象
// 参数:
//   - ctx: 上下文
//   - path: 对象路径
//
// 返回:
//   - error: 错误信息
func remove(ctx context.Context, path string) error {
	// 参数验证
	if path == "" {
		return errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return errors.WithMessage(err, "failed get storage")
	}

	// 删除对象
	err = op.Remove(ctx, storage, actualPath)
	if err != nil {
		return errors.Wrap(ErrRemoveFailed, err.Error())
	}

	return nil
}

// other 执行其他操作
// 参数:
//   - ctx: 上下文
//   - args: 操作参数
//
// 返回:
//   - any: 操作结果
//   - error: 错误信息
func other(ctx context.Context, args model.FsOtherArgs) (any, error) {
	// 参数验证
	if args.Path == "" {
		return nil, errors.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(args.Path)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get storage")
	}

	// 更新路径
	args.Path = actualPath

	// 执行操作
	result, err := op.Other(ctx, storage, args)
	if err != nil {
		return nil, errors.Wrap(ErrOtherFailed, err.Error())
	}

	return result, nil
}

// TaskData 任务数据结构
type TaskData struct {
	task.TaskExtension
	Status        string        `json:"-"` // don't save status to save space
	SrcActualPath string        `json:"src_path"`
	DstActualPath string        `json:"dst_path"`
	SrcStorage    driver.Driver `json:"-"`
	DstStorage    driver.Driver `json:"-"`
	SrcStorageMp  string        `json:"src_storage_mp"`
	DstStorageMp  string        `json:"dst_storage_mp"`
}

// GetStatus 获取任务状态
// 返回:
//   - string: 任务状态
func (t *TaskData) GetStatus() string {
	return t.Status
}
