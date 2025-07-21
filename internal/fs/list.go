package fs

import (
	"context"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 自定义错误类型
var (
	// ErrStorageNotFound 存储未找到错误
	ErrStorageNotFound = errors.New("storage not found")

	// ErrListFailed 列表获取失败错误
	ErrListFailed = errors.New("failed to get object list")
)

// List files
func list(ctx context.Context, path string, args *ListArgs) ([]model.Obj, error) {
	// 预先验证参数
	if args == nil {
		args = &ListArgs{}
	}

	// 使用 context.WithTimeout 防止长时间阻塞
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 从上下文中获取元数据和用户信息
	meta, _ := ctx.Value(consts.MetaKey).(*model.Meta)
	user, _ := ctx.Value(consts.UserKey).(*model.User)

	// 并发获取虚拟文件和存储文件
	virtualFiles := op.GetStorageVirtualFilesByPath(path)

	// 初始化对象合并器
	om := model.NewObjMerge()
	if whetherHide(user, meta, path) {
		om.InitHideReg(meta.Hide)
	}

	// 获取存储和实际路径
	storage, actualPath, err := op.GetStorageAndActualPath(path)

	// 优化错误处理逻辑
	if err != nil {
		if len(virtualFiles) == 0 {
			return nil, errors.Wrap(ErrStorageNotFound, err.Error())
		}
		// 如果有虚拟文件但存储获取失败，记录警告但继续处理
		if !args.NoLog {
			log.Warnf("failed to get storage for %s, using virtual files only: %v", path, err)
		}
		// 只有虚拟文件，直接返回
		return virtualFiles, nil
	}

	// 获取存储中的文件列表
	var storageObjs []model.Obj
	if storage != nil {
		// 使用带超时的上下文
		storageObjs, err = op.List(ctx, storage, actualPath, model.ListArgs{
			ReqPath: path,
			Refresh: args.Refresh,
		})
		if err != nil {
			if !args.NoLog {
				log.Errorf("fs/list: %+v", err)
			}
			// 只有在没有虚拟文件时才返回错误
			if len(virtualFiles) == 0 {
				return nil, errors.Wrap(ErrListFailed, err.Error())
			}
			// 有虚拟文件时，将 storageObjs 设为空切片而不是 nil
			storageObjs = make([]model.Obj, 0)
		}
	} else {
		// 确保 storageObjs 不为 nil
		storageObjs = make([]model.Obj, 0)
	}

	// 合并对象并返回
	return om.Merge(storageObjs, virtualFiles...), nil
}

// whetherHide 判断是否需要隐藏文件
// 参数:
//   - user: 用户信息
//   - meta: 元数据
//   - path: 路径
//
// 返回:
//   - bool: 如果需要隐藏则返回true，否则返回false
func whetherHide(user *model.User, meta *model.Meta, path string) bool {
	// 提前返回，减少不必要的检查
	if user != nil && user.CanSeeHides() {
		return false
	}
	if meta == nil || meta.Hide == "" {
		return false
	}
	// 优化路径比较逻辑
	if !utils.PathEqual(meta.Path, path) && !meta.HSub {
		return false
	}
	return true
}
