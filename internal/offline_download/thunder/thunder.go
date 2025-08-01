package thunder

import (
	"context"
	"strconv"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/drivers/thunder"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

type Thunder struct {
	refreshTaskCache bool
}

func (t *Thunder) Name() string {
	return "Thunder"
}

func (t *Thunder) Items() []model.SettingItem {
	return nil
}

func (t *Thunder) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

func (t *Thunder) Init() (string, error) {
	t.refreshTaskCache = false
	return "ok", nil
}

func (t *Thunder) IsReady() bool {
	tempDir := setting.GetStr(consts.ThunderTempDir)
	if tempDir == "" {
		return false
	}
	storage, _, err := op.GetStorageAndActualPath(tempDir)
	if err != nil {
		return false
	}
	if _, ok := storage.(*thunder.Thunder); !ok {
		return false
	}
	return true
}

func (t *Thunder) AddURL(args *tool.AddURLLinkArgs) (string, error) {
	// 添加新任务刷新缓存
	t.refreshTaskCache = true
	storage, actualPath, err := op.GetStorageAndActualPath(args.TempDir)
	if err != nil {
		return "", err
	}
	thunderDriver, ok := storage.(*thunder.Thunder)
	if !ok {
		return "", errors.New("unsupported storage driver for offline download, only Thunder is supported")
	}

	ctx := context.Background()

	if err = op.MakeDir(ctx, storage, actualPath); err != nil {
		return "", err
	}

	parentDir, err := op.GetUnwrap(ctx, storage, actualPath)
	if err != nil {
		return "", err
	}

	task, err := thunderDriver.OfflineDownload(ctx, args.URL, parentDir, "")
	if err != nil {
		return "", errors.Wrapf(err, "failed to add offline download task")
	}

	return task.ID, nil
}

func (t *Thunder) Remove(task *tool.DownloadTask) error {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return err
	}
	thunderDriver, ok := storage.(*thunder.Thunder)
	if !ok {
		return errors.New("unsupported storage driver for offline download, only Thunder is supported")
	}
	ctx := context.Background()
	err = thunderDriver.DeleteOfflineTasks(ctx, []string{task.GID}, false)
	if err != nil {
		return err
	}
	return nil
}

func (t *Thunder) Status(task *tool.DownloadTask) (*tool.Status, error) {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return nil, err
	}
	thunderDriver, ok := storage.(*thunder.Thunder)
	if !ok {
		return nil, errors.New("unsupported storage driver for offline download, only Thunder is supported")
	}
	tasks, err := t.GetTasks(thunderDriver)
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
	s.Err = errors.New("the task has been deleted")
	return s, nil
}

func init() {
	tool.Tools.Add(&Thunder{})
}