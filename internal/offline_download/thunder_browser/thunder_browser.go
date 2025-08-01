package thunder_browser

import (
	"context"
	"strconv"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/drivers/thunder_browser"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

type ThunderBrowser struct {
	refreshTaskCache bool
}

func (t *ThunderBrowser) Name() string {
	return "ThunderBrowser"
}

func (t *ThunderBrowser) Items() []model.SettingItem {
	return nil
}

func (t *ThunderBrowser) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

func (t *ThunderBrowser) Init() (string, error) {
	t.refreshTaskCache = false
	return "ok", nil
}

func (t *ThunderBrowser) IsReady() bool {
	tempDir := setting.GetStr(consts.ThunderBrowserTempDir)
	if tempDir == "" {
		return false
	}
	storage, _, err := op.GetStorageAndActualPath(tempDir)
	if err != nil {
		return false
	}

	switch storage.(type) {
	case *thunder_browser.ThunderBrowser, *thunder_browser.ThunderBrowserExpert:
		return true
	default:
		return false
	}
}

func (t *ThunderBrowser) AddURL(args *tool.AddURLLinkArgs) (string, error) {
	// 添加新任务刷新缓存
	t.refreshTaskCache = true
	storage, actualPath, err := op.GetStorageAndActualPath(args.TempDir)
	if err != nil {
		return "", err
	}

	ctx := context.Background()

	if err = op.MakeDir(ctx, storage, actualPath); err != nil {
		return "", err
	}

	parentDir, err := op.GetUnwrap(ctx, storage, actualPath)
	if err != nil {
		return "", err
	}

	var task *thunder_browser.OfflineTask
	switch v := storage.(type) {
	case *thunder_browser.ThunderBrowser:
		task, err = v.OfflineDownload(ctx, args.URL, parentDir, "")
	case *thunder_browser.ThunderBrowserExpert:
		task, err = v.OfflineDownload(ctx, args.URL, parentDir, "")
	default:
		return "", errors.New("unsupported storage driver for offline download, only ThunderBrowser is supported")
	}

	if err != nil {
		return "", errors.Wrapf(err, "failed to add offline download task")
	}

	if task == nil {
		return "", errors.New("failed to add offline download task: task is nil")
	}

	return task.ID, nil
}

func (t *ThunderBrowser) Remove(task *tool.DownloadTask) error {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return err
	}

	ctx := context.Background()

	switch v := storage.(type) {
	case *thunder_browser.ThunderBrowser:
		err = v.DeleteOfflineTasks(ctx, []string{task.GID})
	case *thunder_browser.ThunderBrowserExpert:
		err = v.DeleteOfflineTasks(ctx, []string{task.GID})
	default:
		return errors.New("unsupported storage driver for offline download, only ThunderBrowser is supported")
	}

	if err != nil {
		return err
	}
	return nil
}

func (t *ThunderBrowser) Status(task *tool.DownloadTask) (*tool.Status, error) {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return nil, err
	}

	var tasks []thunder_browser.OfflineTask

	switch v := storage.(type) {
	case *thunder_browser.ThunderBrowser:
		tasks, err = t.GetTasks(v)
	case *thunder_browser.ThunderBrowserExpert:
		tasks, err = t.GetTasksExpert(v)
	default:
		return nil, errors.New("unsupported storage driver for offline download, only ThunderBrowser is supported")
	}

	if err != nil {
		return nil, err
	}

	s := &tool.Status{
		Progress:  0,
		NewGID:    "",
		Completed: false,
		Status:    "the task has been deleted",
		Err:       nil,
	}

	for _, tsk := range tasks {
		if tsk.ID != task.GID {
			continue
		}
		s.Progress = float64(tsk.Progress)
		s.Status = tsk.Message
		s.Completed = tsk.Phase == "PHASE_TYPE_COMPLETE"
		s.TotalBytes, err = strconv.ParseInt(tsk.FileSize, 10, 64)
		if err != nil {
			s.TotalBytes = 0
		}
		if tsk.Phase == "PHASE_TYPE_ERROR" {
			s.Err = errors.New(tsk.Message)
		}
		return s, nil
	}

	s.Err = errors.Errorf("the task has been deleted")
	return s, nil
}

func init() {
	tool.Tools.Add(&ThunderBrowser{})
}