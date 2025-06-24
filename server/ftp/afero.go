package ftp

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"

	"github.com/dongdio/OpenList/internal/fs"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/pkg/errs"
)

// AferoAdapter 实现了afero.Fs接口，用于将OpenList的文件系统操作适配到Afero接口
type AferoAdapter struct {
	ctx          context.Context // 上下文，包含用户信息等
	nextFileSize int64           // 下一个将要上传的文件大小
}

// NewAferoAdapter 创建一个新的AferoAdapter实例
func NewAferoAdapter(ctx context.Context) *AferoAdapter {
	return &AferoAdapter{ctx: ctx}
}

// Create 创建文件（未实现，使用GetHandle代替）
func (a *AferoAdapter) Create(_ string) (afero.File, error) {
	// 未实现，请使用GetHandle方法
	return nil, errs.NotImplement
}

// Mkdir 创建目录
func (a *AferoAdapter) Mkdir(name string, _ os.FileMode) error {
	return Mkdir(a.ctx, name)
}

// MkdirAll 创建目录及其所有不存在的父目录
func (a *AferoAdapter) MkdirAll(path string, perm os.FileMode) error {
	return a.Mkdir(path, perm)
}

// Open 打开文件（未实现，使用GetHandle代替）
func (a *AferoAdapter) Open(_ string) (afero.File, error) {
	// 未实现，请使用GetHandle方法或ReadDir方法
	return nil, errs.NotImplement
}

// OpenFile 打开文件（未实现，使用GetHandle代替）
func (a *AferoAdapter) OpenFile(_ string, _ int, _ os.FileMode) (afero.File, error) {
	// 未实现，请使用GetHandle方法
	return nil, errs.NotImplement
}

// Remove 删除文件或目录
func (a *AferoAdapter) Remove(name string) error {
	return Remove(a.ctx, name)
}

// RemoveAll 删除文件或目录及其内容
func (a *AferoAdapter) RemoveAll(path string) error {
	return a.Remove(path)
}

// Rename 重命名文件或目录
func (a *AferoAdapter) Rename(oldName, newName string) error {
	return Rename(a.ctx, oldName, newName)
}

// Stat 获取文件或目录的信息
func (a *AferoAdapter) Stat(name string) (os.FileInfo, error) {
	return Stat(a.ctx, name)
}

// Name 返回文件系统的名称
func (a *AferoAdapter) Name() string {
	return "OpenList FTP Endpoint"
}

// Chmod 更改文件或目录的权限（不支持）
func (a *AferoAdapter) Chmod(_ string, _ os.FileMode) error {
	return errs.NotSupport
}

// Chown 更改文件或目录的所有者（不支持）
func (a *AferoAdapter) Chown(_ string, _, _ int) error {
	return errs.NotSupport
}

// Chtimes 更改文件或目录的访问和修改时间（不支持）
func (a *AferoAdapter) Chtimes(_ string, _ time.Time, _ time.Time) error {
	return errs.NotSupport
}

// ReadDir 读取目录内容
func (a *AferoAdapter) ReadDir(name string) ([]os.FileInfo, error) {
	return List(a.ctx, name)
}

// GetHandle 获取文件传输句柄，用于文件上传和下载
func (a *AferoAdapter) GetHandle(name string, flags int, offset int64) (ftpserver.FileTransfer, error) {
	// 获取之前通过SIZE命令设置的文件大小
	fileSize := a.nextFileSize
	a.nextFileSize = 0

	// 检查不支持的标志
	if (flags & os.O_SYNC) != 0 {
		return nil, errs.NotSupport
	}
	if (flags & os.O_APPEND) != 0 {
		return nil, errs.NotSupport
	}

	// 获取用户并转换路径
	user, ok := a.ctx.Value("user").(*model.User)
	if !ok {
		return nil, errors.New("user not found in context")
	}

	path, err := user.JoinPath(name)
	if err != nil {
		return nil, err
	}

	// 检查文件是否存在
	_, err = fs.Get(a.ctx, path, &fs.GetArgs{})
	exists := err == nil

	// 检查标志与文件存在状态是否匹配
	if (flags&os.O_CREATE) == 0 && !exists {
		return nil, errs.ObjectNotFound
	}
	if (flags&os.O_EXCL) != 0 && exists {
		return nil, errors.New("file already exists")
	}

	// 根据操作类型创建适当的句柄
	if (flags & os.O_WRONLY) != 0 {
		if offset != 0 {
			return nil, errs.NotSupport
		}
		trunc := (flags & os.O_TRUNC) != 0
		if fileSize > 0 {
			return OpenUploadWithLength(a.ctx, path, trunc, fileSize)
		} else {
			return OpenUpload(a.ctx, path, trunc)
		}
	}

	return OpenDownload(a.ctx, path, offset)
}

// Site 处理FTP SITE命令
func (a *AferoAdapter) Site(param string) *ftpserver.AnswerCommand {
	spl := strings.SplitN(param, " ", 2)
	cmd := strings.ToUpper(spl[0])

	var params string
	if len(spl) > 1 {
		params = spl[1]
	} else {
		params = ""
	}

	switch cmd {
	case "SIZE":
		code, msg := HandleSIZE(params, a)
		return &ftpserver.AnswerCommand{
			Code:    code,
			Message: msg,
		}
	}

	return nil
}

// SetNextFileSize 设置下一个将要上传的文件大小
func (a *AferoAdapter) SetNextFileSize(size int64) {
	a.nextFileSize = size
}
