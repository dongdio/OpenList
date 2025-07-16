package tool

import (
	"context"
	"net/url"
	stdpath "path"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	_115 "github.com/dongdio/OpenList/v4/drivers/115"
	open "github.com/dongdio/OpenList/v4/drivers/115_open"
	"github.com/dongdio/OpenList/v4/drivers/pikpak"
	"github.com/dongdio/OpenList/v4/drivers/thunder"
	"github.com/dongdio/OpenList/v4/drivers/thunder_browser"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/task"
)

type DeletePolicy string

const (
	DeleteOnUploadSucceed DeletePolicy = "delete_on_upload_succeed"
	DeleteOnUploadFailed  DeletePolicy = "delete_on_upload_failed"
	DeleteNever           DeletePolicy = "delete_never"
	DeleteAlways          DeletePolicy = "delete_always"
	UploadDownloadStream  DeletePolicy = "upload_download_stream"
)

type AddURLArgs struct {
	URL          string
	DstDirPath   string
	Tool         string
	DeletePolicy DeletePolicy
}

func AddURL(ctx context.Context, args *AddURLArgs) (task.TaskExtensionInfo, error) {
	// check storage
	storage, dstDirActualPath, err := op.GetStorageAndActualPath(args.DstDirPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get storage")
	}
	// check is it could upload
	if storage.Config().NoUpload {
		return nil, errors.WithStack(errs.UploadNotSupported)
	}
	// check path is valid
	obj, err := op.Get(ctx, storage, dstDirActualPath)
	if err != nil {
		if !errs.IsObjectNotFound(err) {
			return nil, errors.WithMessage(err, "failed get object")
		}
	} else {
		if !obj.IsDir() {
			// can't add to a file
			return nil, errors.WithStack(errs.NotFolder)
		}
	}
	// try putting url
	if args.Tool == "SimpleHttp" {
		err = tryPutUrl(ctx, args.DstDirPath, args.URL)
		if err == nil || !errors.Is(err, errs.NotImplement) {
			return nil, err
		}
	}

	// get tool
	tool, err := Tools.Get(args.Tool)
	if err != nil {
		return nil, errors.Wrapf(err, "failed get offline download tool")
	}
	// check tool is ready
	if !tool.IsReady() {
		// try to init tool
		if _, err = tool.Init(); err != nil {
			return nil, errors.Wrapf(err, "failed init offline download tool %s", args.Tool)
		}
	}

	uid := uuid.NewString()
	tempDir := filepath.Join(conf.Conf.TempDir, args.Tool, uid)
	deletePolicy := args.DeletePolicy

	// 如果当前 storage 是对应网盘，则直接下载到目标路径，无需转存
	switch args.Tool {
	case "115 Cloud":
		if _, ok := storage.(*_115.Pan115); ok {
			tempDir = args.DstDirPath
		} else {
			tempDir = filepath.Join(setting.GetStr(consts.Pan115TempDir), uid)
		}
	case "115 Open":
		if _, ok := storage.(*open.Open115); ok {
			tempDir = args.DstDirPath
		} else {
			tempDir = filepath.Join(setting.GetStr(consts.Pan115OpenTempDir), uid)
		}
	case "PikPak":
		if _, ok := storage.(*pikpak.PikPak); ok {
			tempDir = args.DstDirPath
		} else {
			tempDir = filepath.Join(setting.GetStr(consts.PikPakTempDir), uid)
		}
	case "Thunder":
		if _, ok := storage.(*thunder.Thunder); ok {
			tempDir = args.DstDirPath
		} else {
			tempDir = filepath.Join(setting.GetStr(consts.ThunderTempDir), uid)
		}
	case "ThunderBrowser":
		switch storage.(type) {
		case *thunder_browser.ThunderBrowser, *thunder_browser.ThunderBrowserExpert:
			tempDir = args.DstDirPath
		default:
			tempDir = filepath.Join(setting.GetStr(consts.ThunderBrowserTempDir), uid)
		}
	}

	taskCreator, _ := ctx.Value(consts.UserKey).(*model.User) // taskCreator is nil when convert failed
	t := &DownloadTask{
		TaskExtension: task.TaskExtension{
			Creator: taskCreator,
		},
		Url:          args.URL,
		DstDirPath:   args.DstDirPath,
		TempDir:      tempDir,
		DeletePolicy: deletePolicy,
		Toolname:     args.Tool,
		tool:         tool,
	}
	DownloadTaskManager.Add(t)
	return t, nil
}

func tryPutUrl(ctx context.Context, path, urlStr string) error {
	var dstName string
	u, err := url.Parse(urlStr)
	if err == nil {
		dstName = stdpath.Base(u.Path)
	} else {
		dstName = "UnnamedURL"
	}
	return fs.PutURL(ctx, path, dstName, urlStr)
}