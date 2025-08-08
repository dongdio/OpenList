package fs

import (
	"context"
	stdpath "path"
	"strings"
	"time"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

func link(ctx context.Context, path string, args model.LinkArgs) (*model.Link, model.Obj, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		return nil, nil, errs.WithMessage(err, "failed get storage")
	}
	l, obj, err := op.Link(ctx, storage, actualPath, args)
	if err != nil {
		return nil, nil, errs.WithMessage(err, "failed link")
	}
	if l.URL != "" && !strings.HasPrefix(l.URL, "http://") && !strings.HasPrefix(l.URL, "https://") {
		l.URL = common.GetApiURL(ctx) + l.URL
	}
	return l, obj, nil
}

func get(ctx context.Context, path string) (model.Obj, error) {
	path = utils.FixAndCleanPath(path)
	// maybe a virtual file
	if path != "/" {
		virtualFiles := op.GetStorageVirtualFilesByPath(stdpath.Dir(path))
		for _, f := range virtualFiles {
			if f.GetName() == stdpath.Base(path) {
				return f, nil
			}
		}
	}
	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		// if there are no storage prefix with path, maybe root folder
		if path == "/" {
			return &model.Object{
				Name:     "root",
				Size:     0,
				Modified: time.Time{},
				IsFolder: true,
			}, nil
		}
		return nil, errs.WithMessage(err, "failed get storage")
	}
	return op.Get(ctx, storage, actualPath)
}