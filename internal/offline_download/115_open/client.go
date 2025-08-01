package _115_open

import (
	"context"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	open "github.com/dongdio/OpenList/v4/drivers/115_open"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

type Open115 struct {
}

func (o *Open115) Name() string {
	return "115 Open"
}

func (o *Open115) Items() []model.SettingItem {
	return nil
}

func (o *Open115) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

func (o *Open115) Init() (string, error) {
	return "ok", nil
}

func (o *Open115) IsReady() bool {
	tempDir := setting.GetStr(consts.Pan115OpenTempDir)
	if tempDir == "" {
		return false
	}
	storage, _, err := op.GetStorageAndActualPath(tempDir)
	if err != nil {
		return false
	}
	if _, ok := storage.(*open.Open115); !ok {
		return false
	}
	return true
}

func (o *Open115) AddURL(args *tool.AddURLLinkArgs) (string, error) {
	storage, actualPath, err := op.GetStorageAndActualPath(args.TempDir)
	if err != nil {
		return "", err
	}
	driver115Open, ok := storage.(*open.Open115)
	if !ok {
		return "", errors.New("unsupported storage driver for offline download, only 115 Cloud is supported")
	}

	ctx := context.Background()

	if err = op.MakeDir(ctx, storage, actualPath); err != nil {
		return "", err
	}

	parentDir, err := op.GetUnwrap(ctx, storage, actualPath)
	if err != nil {
		return "", err
	}

	hashs, err := driver115Open.OfflineDownload(ctx, []string{args.URL}, parentDir)
	if err != nil || len(hashs) < 1 {
		return "", errors.Wrapf(err, "failed to add offline download task")
	}

	return hashs[0], nil
}

func (o *Open115) Remove(task *tool.DownloadTask) error {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return err
	}
	driver115Open, ok := storage.(*open.Open115)
	if !ok {
		return errors.New("unsupported storage driver for offline download, only 115 Open is supported")
	}

	ctx := context.Background()
	if err = driver115Open.DeleteOfflineTask(ctx, task.GID, false); err != nil {
		return err
	}
	return nil
}

func (o *Open115) Status(task *tool.DownloadTask) (*tool.Status, error) {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return nil, err
	}
	driver115Open, ok := storage.(*open.Open115)
	if !ok {
		return nil, errors.New("unsupported storage driver for offline download, only 115 Open is supported")
	}

	tasks, err := driver115Open.OfflineList(context.Background())
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

	for _, t := range tasks.Tasks {
		if t.InfoHash != task.GID {
			continue
		}
		s.Progress = float64(t.PercentDone)
		s.Status = t.GetStatus()
		s.Completed = t.IsDone()
		s.TotalBytes = t.Size
		if t.IsFailed() {
			s.Err = errors.New(t.GetStatus())
		}
		return s, nil
	}
	s.Err = errors.New("the task has been deleted")
	return s, nil
}

var _ tool.Tool = (*Open115)(nil)

func init() {
	tool.Tools.Add(&Open115{})
}