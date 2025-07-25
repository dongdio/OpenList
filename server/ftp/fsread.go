package ftp

import (
	"context"
	"io"
	fs2 "io/fs"
	"net/http"
	"os"
	"time"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/stream"
)

// FileDownloadProxy 文件下载代理，实现了ftpserver.FileTransfer接口
type FileDownloadProxy struct {
	model.File
	io.Closer
	ctx context.Context
}

// OpenDownload 打开一个文件用于下载
// ctx: 上下文，包含用户信息和其他元数据
// reqPath: 请求的文件路径
// offset: 开始读取的偏移量
// 返回文件下载代理和错误
func OpenDownload(ctx context.Context, reqPath string, offset int64) (*FileDownloadProxy, error) {
	// 获取用户信息
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok {
		return nil, errs.PermissionDenied
	}

	// 获取最近的元数据用于权限检查
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			return nil, err
		}
		// 元数据不存在时继续，使用nil元数据
	}

	// 将元数据添加到上下文
	ctx = context.WithValue(ctx, consts.MetaKey, meta)

	// 检查用户是否有权限访问
	metaPass, _ := ctx.Value(consts.MetaPassKey).(string)
	if !common.CanAccess(user, meta, reqPath, metaPass) {
		return nil, errs.PermissionDenied
	}

	// 获取下载链接

	header, ok := ctx.Value(consts.ProxyHeaderKey).(http.Header)
	if !ok || header == nil {
		return nil, errors.New("proxy header not found in context")
	}
	clientIP, _ := ctx.Value(consts.ClientIPKey).(string)
	link, obj, err := fs.Link(ctx, reqPath, model.LinkArgs{IP: clientIP, Header: header})
	if err != nil {
		return nil, err
	}

	// 创建文件流
	fileStream, err := stream.NewSeekableStream(&stream.FileStream{
		Obj: obj,
		Ctx: ctx,
	}, link)
	if err != nil {
		_ = link.Close()
		return nil, err
	}
	// 创建支持随机访问的读取器
	reader, err := stream.NewReadAtSeeker(fileStream, offset)
	if err != nil {
		_ = fileStream.Close()
		return nil, err
	}

	return &FileDownloadProxy{File: reader, Closer: fileStream, ctx: ctx}, nil
}

// Read 从文件读取数据
func (f *FileDownloadProxy) Read(p []byte) (n int, err error) {
	// 读取数据
	n, err = f.File.Read(p)
	if err != nil {
		return
	}

	// 应用下载限速
	err = stream.ClientDownloadLimit.WaitN(f.ctx, n)
	return
}

func (f *FileDownloadProxy) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = f.File.ReadAt(p, off)
	if err != nil {
		return
	}
	err = stream.ClientDownloadLimit.WaitN(f.ctx, n)
	return
}

func (f *FileDownloadProxy) Write(p []byte) (n int, err error) {
	return 0, errs.NotSupport
}

// OsFileInfoAdapter 将model.Obj适配为os.FileInfo接口
type OsFileInfoAdapter struct {
	obj model.Obj // 原始对象
}

// Name 返回文件名
func (o *OsFileInfoAdapter) Name() string {
	return o.obj.GetName()
}

// Size 返回文件大小
func (o *OsFileInfoAdapter) Size() int64 {
	return o.obj.GetSize()
}

// Mode 返回文件模式
func (o *OsFileInfoAdapter) Mode() fs2.FileMode {
	var mode fs2.FileMode = 0755
	if o.IsDir() {
		mode |= fs2.ModeDir
	}
	return mode
}

// ModTime 返回修改时间
func (o *OsFileInfoAdapter) ModTime() time.Time {
	return o.obj.ModTime()
}

// IsDir 判断是否为目录
func (o *OsFileInfoAdapter) IsDir() bool {
	return o.obj.IsDir()
}

// Sys 返回底层数据源
func (o *OsFileInfoAdapter) Sys() any {
	return o.obj
}

// Stat 获取文件或目录的信息
// ctx: 上下文，包含用户信息
// path: 文件路径
// 返回文件信息和错误
func Stat(ctx context.Context, path string) (os.FileInfo, error) {
	// 获取用户信息
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok {
		return nil, errs.PermissionDenied
	}

	// 转换路径
	reqPath, err := user.JoinPath(path)
	if err != nil {
		return nil, err
	}

	// 获取元数据用于权限检查
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			return nil, err
		}
		// 元数据不存在时继续，使用nil元数据
	}

	// 将元数据添加到上下文
	ctx = context.WithValue(ctx, consts.MetaKey, meta)

	// 检查访问权限
	metaPass, _ := ctx.Value(consts.MetaPassKey).(string)
	if !common.CanAccess(user, meta, reqPath, metaPass) {
		return nil, errs.PermissionDenied
	}

	// 获取文件对象
	obj, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		return nil, err
	}

	return &OsFileInfoAdapter{obj: obj}, nil
}

// List 列出目录内容
// ctx: 上下文，包含用户信息
// path: 目录路径
// 返回目录中文件信息的列表和错误
func List(ctx context.Context, path string) ([]os.FileInfo, error) {
	// 获取用户信息
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok {
		return nil, errs.PermissionDenied
	}

	// 转换路径
	reqPath, err := user.JoinPath(path)
	if err != nil {
		return nil, err
	}

	// 获取元数据用于权限检查
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			return nil, err
		}
		// 元数据不存在时继续，使用nil元数据
	}

	// 将元数据添加到上下文
	ctx = context.WithValue(ctx, consts.MetaKey, meta)

	// 检查访问权限
	metaPass, _ := ctx.Value(consts.MetaPassKey).(string)
	if !common.CanAccess(user, meta, reqPath, metaPass) {
		return nil, errs.PermissionDenied
	}

	// 列出目录内容
	objs, err := fs.List(ctx, reqPath, &fs.ListArgs{})
	if err != nil {
		return nil, err
	}

	// 转换为os.FileInfo切片
	ret := make([]os.FileInfo, len(objs))
	for i, obj := range objs {
		ret[i] = &OsFileInfoAdapter{obj: obj}
	}

	return ret, nil
}