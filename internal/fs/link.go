package fs

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
)

// 自定义错误类型
var (
	// ErrLinkFailed 链接获取失败错误
	ErrLinkFailed = errors.New("failed to get object link")
)

// link 获取指定路径对象的链接
// 参数:
//   - ctx: 上下文
//   - path: 对象路径
//   - args: 链接参数
//
// 返回:
//   - *model.Link: 链接信息
//   - model.Obj: 对象信息
//   - error: 错误信息
func link(ctx context.Context, path string, args model.LinkArgs) (*model.Link, model.Obj, error) {
	// 获取存储和实际路径
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, nil, errors.Wrap(ErrStorageNotFound, err.Error())
	}

	// 获取链接
	l, obj, err := op.Link(ctx, storage, actualPath, args)
	if err != nil {
		return nil, nil, errors.Wrap(ErrLinkFailed, err.Error())
	}

	// 处理相对URL
	if l != nil && l.URL != "" && !strings.HasPrefix(l.URL, "http://") && !strings.HasPrefix(l.URL, "https://") {
		l.URL = common.GetApiURL(ctx) + l.URL
	}

	return l, obj, nil
}
