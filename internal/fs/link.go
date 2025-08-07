package fs

import (
	"context"
	"strings"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
)

// ErrLinkFailed 链接生成失败错误
var ErrLinkFailed = errs.New("failed to generate link")

// link 生成文件链接
// 参数:
//   - ctx: 上下文
//   - path: 文件路径
//   - args: 链接参数
//
// 返回:
//   - *model.Link: 链接信息
//   - model.Obj: 文件对象
//   - error: 错误信息
func link(ctx context.Context, path string, args model.LinkArgs) (*model.Link, model.Obj, error) {
	// 参数验证
	if path == "" {
		return nil, nil, errs.WithStack(ErrInvalidPath)
	}

	// 获取存储驱动和实际路径
	storage, actualPath, err := getStorageWithCache(path)
	if err != nil {
		return nil, nil, errs.Wrap(err, "failed get storage")
	}

	// 生成链接
	l, obj, err := op.Link(ctx, storage, actualPath, args)
	if err != nil {
		return nil, nil, errs.Wrap(ErrLinkFailed, err.Error())
	}

	// 处理相对URL，确保完整的URL路径
	if l.URL != "" && !strings.HasPrefix(l.URL, "http://") && !strings.HasPrefix(l.URL, "https://") {
		l.URL = common.GetApiURL(ctx) + l.URL
	}

	return l, obj, nil
}