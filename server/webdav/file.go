// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdav

import (
	"context"
	"errors"
	"net/http"
	"path"
	"path/filepath"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
)

// slashClean 清理路径名，确保以斜杠开头
// 与path.Clean("/" + name)等价，但效率略高
//
// 参数:
//   - name: 要清理的路径名
//
// 返回:
//   - string: 清理后的路径名，始终以斜杠开头
func slashClean(name string) string {
	if name == "" || name[0] != '/' {
		name = "/" + name
	}
	return path.Clean(name)
}

// moveFiles 移动文件和/或目录从源路径到目标路径
// 处理WebDAV的MOVE请求
//
// 根据WebDAV规范9.9.4节，返回不同的HTTP状态码
//
// 参数:
//   - ctx: 上下文，包含用户信息
//   - src: 源路径
//   - dst: 目标路径
//   - overwrite: 是否覆盖目标位置的现有文件
//
// 返回:
//   - int: HTTP状态码
//   - error: 错误信息
func moveFiles(ctx context.Context, src, dst string, overwrite bool) (status int, err error) {
	if ctx == nil {
		return http.StatusInternalServerError, errors.New("上下文为空")
	}

	// 提取源和目标的目录和文件名
	srcDir := path.Dir(src)
	dstDir := path.Dir(dst)
	srcName := path.Base(src)
	dstName := path.Base(dst)

	// 获取用户信息
	userVal := ctx.Value(consts.UserKey)
	if userVal == nil {
		return http.StatusUnauthorized, errors.New("未找到用户信息")
	}

	user, ok := userVal.(*model.User)
	if !ok {
		return http.StatusInternalServerError, errors.New("用户信息类型错误")
	}

	// 检查权限
	if srcDir != dstDir && !user.CanMove() {
		return http.StatusForbidden, nil
	}
	if srcName != dstName && !user.CanRename() {
		return http.StatusForbidden, nil
	}

	// 根据情况执行重命名或移动
	if srcDir == dstDir {
		// 同目录内移动，本质上是重命名
		err = fs.Rename(ctx, src, dstName)
		if err != nil {
			return http.StatusInternalServerError, err
		}
	} else {
		// 跨目录移动，先移动文件，如果文件名不同再重命名
		err = fs.Move(ctx, src, dstDir)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if srcName != dstName {
			err = fs.Rename(ctx, path.Join(dstDir, srcName), dstName)
			if err != nil {
				return http.StatusInternalServerError, err
			}
		}
	}

	// 根据WebDAV规范，移动成功返回201 Created
	// 如果目标已存在且被覆盖，应返回204 No Content
	if overwrite {
		return http.StatusNoContent, nil
	}
	return http.StatusCreated, nil
}

// copyFiles 复制文件和/或目录从源路径到目标路径
// 处理WebDAV的COPY请求
//
// 根据WebDAV规范9.8.5节，返回不同的HTTP状态码
//
// 参数:
//   - ctx: 上下文，包含用户信息
//   - src: 源路径
//   - dst: 目标路径
//   - overwrite: 是否覆盖目标位置的现有文件
//
// 返回:
//   - int: HTTP状态码
//   - error: 错误信息
func copyFiles(ctx context.Context, src, dst string, overwrite bool) (status int, err error) {
	if ctx == nil {
		return http.StatusInternalServerError, errors.New("上下文为空")
	}

	// 获取目标目录
	dstDir := path.Dir(dst)

	// 执行复制操作，设置NoTask标志避免创建任务
	noTaskCtx := context.WithValue(ctx, consts.NoTaskKey, struct{}{})
	_, err = fs.Copy(noTaskCtx, src, dstDir)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	// 根据WebDAV规范，如果目标已存在且被覆盖，应返回204 No Content
	// 如果复制成功且创建了新资源，应返回201 Created
	if overwrite {
		return http.StatusNoContent, nil
	}
	return http.StatusCreated, nil
}

// walkFS 遍历文件系统，从指定路径开始，最多遍历到指定深度
// 类似于filepath.Walk，但支持WebDAV的深度参数
//
// 参数:
//   - ctx: 上下文
//   - depth: 遍历深度，0表示不遍历子目录，1表示只遍历一级子目录，-1表示遍历所有子目录
//   - name: 起始路径
//   - info: 起始路径的文件信息
//   - walkFn: 遍历回调函数，对每个文件调用
//
// 返回:
//   - error: 遍历错误
func walkFS(ctx context.Context, depth int, name string, info model.Obj, walkFn func(reqPath string, info model.Obj, err error) error) error {
	if ctx == nil || walkFn == nil {
		return errors.New("上下文或回调函数为空")
	}

	// 对当前节点调用回调函数
	err := walkFn(name, info, nil)
	if err != nil {
		if info.IsDir() && errors.Is(err, filepath.SkipDir) {
			return nil // 如果是目录且要求跳过，直接返回nil
		}
		return err
	}

	// 如果不是目录或深度为0，不继续遍历
	if !info.IsDir() || depth == 0 {
		return nil
	}

	// 如果深度为1，下一级设置为0
	if depth == 1 {
		depth = 0
	}

	// 获取最近的元数据，用于权限检查
	meta, _ := op.GetNearestMeta(name)

	// 创建带元数据的上下文
	metaCtx := context.WithValue(ctx, consts.MetaKey, meta)

	// 列出目录内容
	objs, err := fs.List(metaCtx, name, &fs.ListArgs{})
	if err != nil {
		return walkFn(name, info, err)
	}

	// 遍历子节点
	for _, fileInfo := range objs {
		filename := path.Join(name, fileInfo.GetName())

		// 递归遍历
		err = walkFS(ctx, depth, filename, fileInfo, walkFn)
		if err != nil {
			if !fileInfo.IsDir() || !errors.Is(err, filepath.SkipDir) {
				return err
			}
			// 如果是目录且要求跳过，继续遍历下一个节点
		}
	}

	return nil
}