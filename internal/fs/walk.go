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

// ErrWalkFailed 遍历失败错误
var ErrWalkFailed = errors.New("walk failed")

// WalkFS 遍历文件系统，从指定名称开始，最多遍历到指定深度
//
// WalkFS 在当前深度 > `depth` 时停止。对于每个访问的节点，
// WalkFS 调用 walkFn。如果访问的文件系统节点是一个目录，并且
// walkFn 返回 path.SkipDir，walkFS 将跳过该节点的遍历。
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
	// 参数验证
	if info == nil {
		return errors.New("info cannot be nil")
	}

	if walkFn == nil {
		return errors.New("walkFn cannot be nil")
	}

	// 调用回调函数处理当前节点
	walkFnErr := walkFn(name, info)
	if walkFnErr != nil {
		// 如果是目录且回调函数返回 SkipDir，则跳过该目录
		if info.IsDir() && errors.Is(walkFnErr, filepath.SkipDir) {
			return nil
		}
		return walkFnErr
	}

	// 如果不是目录或已达到最大深度，则停止遍历
	if !info.IsDir() || depth <= 0 {
		return nil
	}

	// 获取最近的元数据
	meta, _ := op.GetNearestMeta(name)

	// 获取目录内容
	objs, err := List(context.WithValue(ctx, consts.MetaKey, meta), name, &ListArgs{})
	if err != nil {
		return errors.Join(ErrWalkFailed, err)
	}

	// 遍历子节点
	for _, fileInfo := range objs {
		// 构建子节点路径
		filename := path.Join(name, fileInfo.GetName())

		// 递归遍历子节点
		if err = WalkFS(ctx, depth-1, filename, fileInfo, walkFn); err != nil {
			// 如果子节点返回 SkipDir，则跳过当前目录的剩余内容
			if errors.Is(err, filepath.SkipDir) {
				break
			}
			return err
		}
	}

	return nil
}
