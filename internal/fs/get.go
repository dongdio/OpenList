package fs

import (
	"context"
	stdpath "path"
	"time"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 自定义错误类型
var (
	// ErrObjectNotFound 对象未找到错误
	ErrObjectNotFound = errs.New("object not found")

	// ErrInvalidPath 无效路径错误
	ErrInvalidPath = errs.New("invalid path")

	// rootObject 根目录对象，避免重复创建
	rootObject = &model.Object{
		Name:     "root",
		Size:     0,
		Modified: time.Time{},
		IsFolder: true,
	}
)

// get 获取指定路径的对象
// 参数:
//   - ctx: 上下文
//   - path: 对象路径
//
// 返回:
//   - model.Obj: 对象信息
//   - error: 错误信息
func get(ctx context.Context, path string) (model.Obj, error) {
	// 参数验证
	if path == "" {
		return nil, errs.WithStack(ErrInvalidPath)
	}

	// 修复并清理路径
	path = utils.FixAndCleanPath(path)

	// 处理根路径特殊情况
	if path == "/" {
		return rootObject, nil
	}

	// 检查是否为虚拟文件
	dirPath := stdpath.Dir(path)
	baseName := stdpath.Base(path)

	// 获取虚拟文件列表
	virtualFiles := op.GetStorageVirtualFilesByPath(dirPath)
	for _, f := range virtualFiles {
		if f.GetName() == baseName {
			return f, nil
		}
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return nil, errs.WithMessage(err, "failed get storage")
	}

	// 获取对象
	obj, err := op.Get(ctx, storage, actualPath)
	if err != nil {
		return nil, errs.Wrap(ErrObjectNotFound, err.Error())
	}

	return obj, nil
}