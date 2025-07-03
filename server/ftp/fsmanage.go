package ftp

import (
	"context"
	stdpath "path"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

// Mkdir 在指定路径创建目录
// ctx: 上下文，包含用户信息
// path: 目录路径
func Mkdir(ctx context.Context, path string) error {
	// 获取用户信息
	user, ok := ctx.Value("user").(*model.User)
	if !ok {
		return errs.PermissionDenied
	}

	// 转换相对路径为绝对路径
	reqPath, err := user.JoinPath(path)
	if err != nil {
		return err
	}

	// 检查用户权限
	if !user.CanWrite() || !user.CanFTPManage() {
		// 如果用户没有全局写入权限，检查元数据中的权限
		meta, err := op.GetNearestMeta(stdpath.Dir(reqPath))
		if err != nil {
			if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
				return err
			}
			// 元数据不存在时继续，默认为无权限
		}

		// 检查目录的写入权限
		if !common.CanWrite(meta, reqPath) {
			return errs.PermissionDenied
		}
	}

	// 创建目录
	return fs.MakeDir(ctx, reqPath)
}

// Remove 删除指定路径的文件或目录
// ctx: 上下文，包含用户信息
// path: 文件或目录路径
func Remove(ctx context.Context, path string) error {
	// 获取用户信息
	user, ok := ctx.Value("user").(*model.User)
	if !ok {
		return errs.PermissionDenied
	}

	// 检查用户权限
	if !user.CanRemove() || !user.CanFTPManage() {
		return errs.PermissionDenied
	}

	// 转换相对路径为绝对路径
	reqPath, err := user.JoinPath(path)
	if err != nil {
		return err
	}

	// 删除文件或目录
	return fs.Remove(ctx, reqPath)
}

// Rename 重命名或移动文件/目录
// ctx: 上下文，包含用户信息
// oldPath: 原路径
// newPath: 新路径
func Rename(ctx context.Context, oldPath, newPath string) error {
	// 获取用户信息
	user, ok := ctx.Value("user").(*model.User)
	if !ok {
		return errs.PermissionDenied
	}

	// 转换相对路径为绝对路径
	srcPath, err := user.JoinPath(oldPath)
	if err != nil {
		return err
	}

	dstPath, err := user.JoinPath(newPath)
	if err != nil {
		return err
	}

	// 解析源路径和目标路径的目录和文件名
	srcDir, srcBase := stdpath.Split(srcPath)
	dstDir, dstBase := stdpath.Split(dstPath)

	// 处理不同情况：重命名（相同目录）或移动（不同目录）
	if srcDir == dstDir {
		// 相同目录下的重命名操作
		if !user.CanRename() || !user.CanFTPManage() {
			return errs.PermissionDenied
		}
		return fs.Rename(ctx, srcPath, dstBase)
	} else {
		// 不同目录下的移动操作（可能同时包含重命名）
		if !user.CanFTPManage() || !user.CanMove() || (srcBase != dstBase && !user.CanRename()) {
			return errs.PermissionDenied
		}

		// 尝试移动文件/目录
		if err = fs.Move(ctx, srcPath, dstDir); err != nil {
			// 如果文件名不同，移动失败就直接返回错误
			if srcBase != dstBase {
				return err
			}

			// 文件名相同但移动失败，尝试复制
			if _, err1 := fs.Copy(ctx, srcPath, dstDir); err1 != nil {
				return errors.Errorf("failed move for %v, and failed try copying for %v", err, err1)
			}
			return nil
		}

		// 如果移动成功但文件名不同，还需要进行重命名
		if srcBase != dstBase {
			return fs.Rename(ctx, stdpath.Join(dstDir, srcBase), dstBase)
		}
		return nil
	}
}