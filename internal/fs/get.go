package fs

import (
	"context"
	stdpath "path"
	"time"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 自定义错误类型
var (
	// ErrObjectNotFound 对象未找到错误
	ErrObjectNotFound = errors.New("object not found")
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
	// 修复并清理路径
	path = utils.FixAndCleanPath(path)

	// 处理根路径特殊情况
	if path == "/" {
		return &model.Object{
			Name:     "root",
			Size:     0,
			Modified: time.Time{},
			IsFolder: true,
		}, nil
	}

	// 检查是否为虚拟文件
	dirPath := stdpath.Dir(path)
	fileName := stdpath.Base(path)
	virtualFiles := op.GetStorageVirtualFilesByPath(dirPath)

	// 使用索引优化查找虚拟文件
	for i := range virtualFiles {
		if virtualFiles[i].GetName() == fileName {
			return virtualFiles[i], nil
		}
	}

	// 获取存储和实际路径
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, errors.Wrap(ErrStorageNotFound, err.Error())
	}

	// 获取对象
	obj, err := op.Get(ctx, storage, actualPath)
	if err != nil {
		return nil, errors.Wrap(ErrObjectNotFound, err.Error())
	}

	return obj, nil
}
