package ftp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	stdpath "path"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/pkg/stream"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/fs"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/pkg/errs"
	"github.com/dongdio/OpenList/server/common"
)

// FileUploadProxy 文件上传代理，实现了ftpserver.FileTransfer接口
type FileUploadProxy struct {
	ftpserver.FileTransfer
	buffer *os.File        // 临时文件缓冲区
	path   string          // 目标文件路径
	ctx    context.Context // 上下文
	trunc  bool            // 是否截断文件
}

// uploadAuth 验证上传权限
// ctx: 上下文
// path: 文件路径
func uploadAuth(ctx context.Context, path string) error {
	// 获取用户信息
	user, ok := ctx.Value("user").(*model.User)
	if !ok {
		return errs.PermissionDenied
	}

	// 获取目录的元数据
	meta, err := op.GetNearestMeta(stdpath.Dir(path))
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			return err
		}
		// 元数据不存在时继续，使用nil元数据
	}

	// 检查权限
	metaPass, _ := ctx.Value("meta_pass").(string)
	if !(common.CanAccess(user, meta, path, metaPass) &&
		((user.CanFTPManage() && user.CanWrite()) || common.CanWrite(meta, stdpath.Dir(path)))) {
		return errs.PermissionDenied
	}

	return nil
}

// OpenUpload 打开一个文件用于上传
// ctx: 上下文
// path: 目标文件路径
// trunc: 是否截断已存在的文件
func OpenUpload(ctx context.Context, path string, trunc bool) (*FileUploadProxy, error) {
	// 验证上传权限
	err := uploadAuth(ctx, path)
	if err != nil {
		return nil, err
	}

	// 创建临时文件
	tmpFile, err := os.CreateTemp(conf.Conf.TempDir, "file-*")
	if err != nil {
		return nil, err
	}

	return &FileUploadProxy{buffer: tmpFile, path: path, ctx: ctx, trunc: trunc}, nil
}

// Read 从文件读取数据（不支持）
func (f *FileUploadProxy) Read(p []byte) (n int, err error) {
	return 0, errs.NotSupport
}

// Write 向文件写入数据
func (f *FileUploadProxy) Write(p []byte) (n int, err error) {
	// 写入临时文件
	n, err = f.buffer.Write(p)
	if err != nil {
		return
	}

	// 应用上传限速
	err = stream.ClientUploadLimit.WaitN(f.ctx, n)
	return
}

// Seek 设置下一次读写的位置
func (f *FileUploadProxy) Seek(offset int64, whence int) (int64, error) {
	return f.buffer.Seek(offset, whence)
}

// Close 关闭上传代理并完成文件上传
func (f *FileUploadProxy) Close() error {
	// 解析路径
	dir, name := stdpath.Split(f.path)

	// 获取文件大小
	size, err := f.buffer.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	// 重置文件指针到开头
	if _, err := f.buffer.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// 读取前512字节以检测MIME类型
	arr := make([]byte, 512)
	if _, err = f.buffer.Read(arr); err != nil {
		return err
	}
	contentType := http.DetectContentType(arr)

	// 再次重置文件指针到开头
	if _, err = f.buffer.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// 如果需要截断，先删除原文件
	if f.trunc {
		_ = fs.Remove(f.ctx, f.path)
	}

	// 创建文件流
	s := &stream.FileStream{
		Obj: &model.Object{
			Name:     name,
			Size:     size,
			Modified: time.Now(),
		},
		Mimetype:     contentType,
		WebPutAsTask: true,
	}
	s.SetTmpFile(f.buffer)

	// 执行上传
	_, err = fs.PutAsTask(f.ctx, dir, s)
	return err
}

// FileUploadWithLengthProxy 具有预定义长度的文件上传代理
type FileUploadWithLengthProxy struct {
	ftpserver.FileTransfer
	ctx           context.Context // 上下文
	path          string          // 目标文件路径
	length        int64           // 文件大小
	first512Bytes [512]byte       // 前512字节缓冲区
	pFirst        int             // 缓冲区中已使用的字节数
	pipeWriter    io.WriteCloser  // 管道写入端
	errChan       chan error      // 错误通道
}

// OpenUploadWithLength 打开一个具有预定义长度的文件用于上传
// ctx: 上下文
// path: 目标文件路径
// trunc: 是否截断已存在的文件
// length: 文件大小
func OpenUploadWithLength(ctx context.Context, path string, trunc bool, length int64) (*FileUploadWithLengthProxy, error) {
	// 验证上传权限
	err := uploadAuth(ctx, path)
	if err != nil {
		return nil, err
	}

	// 如果需要截断，先删除原文件
	if trunc {
		_ = fs.Remove(ctx, path)
	}

	return &FileUploadWithLengthProxy{ctx: ctx, path: path, length: length}, nil
}

// Read 从文件读取数据（不支持）
func (f *FileUploadWithLengthProxy) Read(p []byte) (n int, err error) {
	return 0, errs.NotSupport
}

// write 内部写入方法
func (f *FileUploadWithLengthProxy) write(p []byte) (n int, err error) {
	// 如果管道已经创建，写入数据到管道
	if f.pipeWriter != nil {
		select {
		case e := <-f.errChan:
			return 0, e
		default:
			return f.pipeWriter.Write(p)
		}
	} else if len(p) < 512-f.pFirst {
		// 如果数据不足512字节且缓冲区未满，将数据复制到缓冲区
		copy(f.first512Bytes[f.pFirst:], p)
		f.pFirst += len(p)
		return len(p), nil
	} else {
		// 数据足够多，可以创建管道开始上传
		// 填充缓冲区剩余空间
		copy(f.first512Bytes[f.pFirst:], p[:512-f.pFirst])

		// 检测MIME类型
		contentType := http.DetectContentType(f.first512Bytes[:])
		dir, name := stdpath.Split(f.path)

		// 创建管道
		reader, writer := io.Pipe()
		f.errChan = make(chan error, 1)

		// 创建文件流
		s := &stream.FileStream{
			Obj: &model.Object{
				Name:     name,
				Size:     f.length,
				Modified: time.Now(),
			},
			Mimetype:     contentType,
			WebPutAsTask: false,
			Reader:       reader,
		}

		// 在后台执行上传
		go func() {
			e := fs.PutDirectly(f.ctx, dir, s, true)
			f.errChan <- e
			close(f.errChan)
		}()

		f.pipeWriter = writer

		// 先写入缓冲区数据
		n, err = writer.Write(f.first512Bytes[:])
		if err != nil {
			return n, err
		}

		// 再写入剩余数据
		n1, err := writer.Write(p[512-f.pFirst:])
		if err != nil {
			return n1 + 512 - f.pFirst, err
		}

		f.pFirst = 512
		return len(p), nil
	}
}

// Write 向文件写入数据
func (f *FileUploadWithLengthProxy) Write(p []byte) (n int, err error) {
	// 写入数据
	n, err = f.write(p)
	if err != nil {
		return
	}

	// 应用上传限速
	err = stream.ClientUploadLimit.WaitN(f.ctx, n)
	return
}

// Seek 设置下一次读写的位置（不支持）
func (f *FileUploadWithLengthProxy) Seek(offset int64, whence int) (int64, error) {
	return 0, errs.NotSupport
}

// Close 关闭上传代理并完成文件上传
func (f *FileUploadWithLengthProxy) Close() error {
	if f.pipeWriter != nil {
		// 已创建管道，关闭管道并等待上传完成
		err := f.pipeWriter.Close()
		if err != nil {
			return err
		}
		return <-f.errChan
	} else {
		// 未创建管道，直接上传缓冲区数据
		data := f.first512Bytes[:f.pFirst]
		contentType := http.DetectContentType(data)
		dir, name := stdpath.Split(f.path)

		// 创建文件流
		s := &stream.FileStream{
			Obj: &model.Object{
				Name:     name,
				Size:     int64(f.pFirst),
				Modified: time.Now(),
			},
			Mimetype:     contentType,
			WebPutAsTask: false,
			Reader:       bytes.NewReader(data),
		}

		return fs.PutDirectly(f.ctx, dir, s, true)
	}
}
