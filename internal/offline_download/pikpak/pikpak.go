package pikpak

import (
	"context"
	"strconv"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/drivers/pikpak"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

type PikPak struct {
	refreshTaskCache bool
}

func (p *PikPak) Name() string {
	return "PikPak"
}

func (p *PikPak) Items() []model.SettingItem {
	return nil
}

func (p *PikPak) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

func (p *PikPak) Init() (string, error) {
	p.refreshTaskCache = false
	return "ok", nil
}

func (p *PikPak) IsReady() bool {
	tempDir := setting.GetStr(consts.PikPakTempDir)
	if tempDir == "" {
		return false
	}
	storage, _, err := op.GetStorageAndActualPath(tempDir)
	if err != nil {
		return false
	}
	if _, ok := storage.(*pikpak.PikPak); !ok {
		return false
	}
	return true
}

func (p *PikPak) AddURL(args *tool.AddURLLinkArgs) (string, error) {
	// 添加新任务刷新缓存
	p.refreshTaskCache = true
	storage, actualPath, err := op.GetStorageAndActualPath(args.TempDir)
	if err != nil {
		return "", err
	}
	pikpakDriver, ok := storage.(*pikpak.PikPak)
	if !ok {
		return "", errors.New("unsupported storage driver for offline download, only Pikpak is supported")
	}

	ctx := context.Background()

	if err = op.MakeDir(ctx, storage, actualPath); err != nil {
		return "", err
	}

	parentDir, err := op.GetUnwrap(ctx, storage, actualPath)
	if err != nil {
		return "", err
	}

	t, err := pikpakDriver.OfflineDownload(ctx, args.URL, parentDir, "")
	if err != nil {
		return "", errors.Wrapf(err, "failed to add offline download task")
	}

	return t.ID, nil
}

func (p *PikPak) Remove(task *tool.DownloadTask) error {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return err
	}
	pikpakDriver, ok := storage.(*pikpak.PikPak)
	if !ok {
		return errors.New("unsupported storage driver for offline download, only Pikpak is supported")
	}
	ctx := context.Background()
	err = pikpakDriver.DeleteOfflineTasks(ctx, []string{task.GID}, false)
	if err != nil {
		return err
	}
	return nil
}

func (p *PikPak) Status(task *tool.DownloadTask) (*tool.Status, error) {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return nil, err
	}
	pikpakDriver, ok := storage.(*pikpak.PikPak)
	if !ok {
		return nil, errors.New("unsupported storage driver for offline download, only Pikpak is supported")
	}
	tasks, err := p.GetTasks(pikpakDriver)
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
	for _, t := range tasks {
		if t.ID != task.GID {
			continue
		}
		s.Progress = float64(t.Progress)
		s.Status = t.Message
		s.Completed = t.Phase == "PHASE_TYPE_COMPLETE"
		s.TotalBytes, err = strconv.ParseInt(t.FileSize, 10, 64)
		if err != nil {
			s.TotalBytes = 0
		}
		if t.Phase == "PHASE_TYPE_ERROR" {
			s.Err = errors.New(t.Message)
		}
		return s, nil
	}
	s.Err = errors.New("the task has been deleted")
	return s, nil
}

func init() {
	tool.Tools.Add(&PikPak{})
}