package fs

import (
	"context"
	"errors"
	"path"
	"path/filepath"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// 自定义错误类型
var (
	// ErrWalkFailed 遍历失败错误
	ErrWalkFailed = errors.New("failed to walk filesystem")

	// ErrMaxDepthReached 已达到最大深度错误
	ErrMaxDepthReached = errors.New("maximum walk depth reached")
)

// WalkFS traverses filesystem fs starting at name up to depth levels.
//
// WalkFS will stop when current depth > `depth`. For each visited node,
// WalkFS calls walkFn. If a visited file system node is a directory and
// walkFn returns path.SkipDir, walkFS will skip traversal of this node.
//
// 参数:
//   - ctx: 上下文
//   - depth: 最大遍历深度
//   - name: 起始路径
//   - info: 起始对象信息
//   - walkFn: 遍历回调函数
//
// 返回:
//   - error: 错误信息
func WalkFS(ctx context.Context, depth int, name string, info model.Obj, walkFn func(reqPath string, info model.Obj) error) error {
	// 检查上下文是否已取消
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 调用回调函数处理当前节点
	walkFnErr := walkFn(name, info)
	if walkFnErr != nil {
		if info.IsDir() && errors.Is(walkFnErr, filepath.SkipDir) {
			return nil
		}
		return walkFnErr
	}

	// 如果不是目录或已达到最大深度，停止遍历
	if !info.IsDir() || depth <= 0 {
		return nil
	}

	// 获取最近的元数据
	meta, _ := op.GetNearestMeta(name)

	// 读取目录内容
	objs, err := List(context.WithValue(ctx, consts.MetaKey, meta), name, &ListArgs{})
	if err != nil {
		return err
	}

	// 预分配足够的容量
	newDepth := depth - 1

	// 遍历子节点
	for i := range objs {
		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return ctx.Err()
		}

		filename := path.Join(name, objs[i].GetName())
		if err = WalkFS(ctx, newDepth, filename, objs[i], walkFn); err != nil {
			if errors.Is(err, filepath.SkipDir) {
				break
			}
			return err
		}
	}

	return nil
}
