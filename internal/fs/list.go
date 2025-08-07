package fs

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// ErrListFailed 列表获取失败错误
var ErrListFailed = errs.New("failed to list objects")

// List files
func list(ctx context.Context, path string, args *ListArgs) ([]model.Obj, error) {
	// 预分配合理大小的切片，减少内存重新分配
	const initialCapacity = 50

	meta, _ := ctx.Value(consts.MetaKey).(*model.Meta)
	user, _ := ctx.Value(consts.UserKey).(*model.User)

	// 获取虚拟文件
	virtualFiles := op.GetStorageVirtualFilesByPath(path)

	// 获取存储驱动
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil && len(virtualFiles) == 0 {
		return nil, errs.WithMessage(err, "failed get storage")
	}

	var objs []model.Obj
	if storage != nil {

		// 获取对象列表
		objs, err = op.List(ctx, storage, actualPath, model.ListArgs{
			ReqPath: path,
			Refresh: args != nil && args.Refresh,
		})

		if err != nil {
			if args != nil && !args.NoLog {
				log.Errorf("fs/list: %+v", err)
			}

			// 如果没有虚拟文件，则返回错误
			if len(virtualFiles) == 0 {
				return nil, errs.Wrap(ErrListFailed, err.Error())
			}
		}
	}

	// 创建对象合并器
	om := model.NewObjMerge()

	// 判断是否需要隐藏文件
	if whetherHide(user, meta, path) {
		om.InitHideReg(meta.Hide)
	}

	// 合并对象和虚拟文件
	return om.Merge(objs, virtualFiles...), nil
}

// whetherHide 判断是否需要隐藏文件
// 参数:
//   - user: 用户信息
//   - meta: 元数据信息
//   - path: 路径
//
// 返回:
//   - bool: 是否需要隐藏
func whetherHide(user *model.User, meta *model.Meta, path string) bool {
	// 如果是管理员，不隐藏
	if user == nil || user.CanSeeHides() {
		return false
	}

	// 如果元数据为空，不隐藏
	if meta == nil {
		return false
	}

	// 如果隐藏规则为空，不隐藏
	if meta.Hide == "" {
		return false
	}

	// 如果元数据不应用于子文件夹，且路径不等于元数据路径，不隐藏
	if !utils.PathEqual(meta.Path, path) && !meta.HSub {
		return false
	}

	// 如果是访客，隐藏
	return true
}