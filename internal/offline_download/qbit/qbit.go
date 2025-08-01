package qbit

import (
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/qbittorrent"
)

type QBittorrent struct {
	client qbittorrent.Client
}

func (a *QBittorrent) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

func (a *QBittorrent) Name() string {
	return "qBittorrent"
}

func (a *QBittorrent) Items() []model.SettingItem {
	// qBittorrent settings
	return []model.SettingItem{
		{Key: consts.QbittorrentUrl, Value: "http://admin:adminadmin@localhost:8080/", Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
		{Key: consts.QbittorrentSeedtime, Value: "0", Type: consts.TypeNumber, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
}

func (a *QBittorrent) Init() (string, error) {
	a.client = nil
	url := setting.GetStr(consts.QbittorrentUrl)
	qbClient, err := qbittorrent.New(url)
	if err != nil {
		return "", err
	}
	a.client = qbClient
	return "ok", nil
}

func (a *QBittorrent) IsReady() bool {
	return a.client != nil
}

func (a *QBittorrent) AddURL(args *tool.AddURLLinkArgs) (string, error) {
	err := a.client.AddFromLink(args.URL, args.TempDir, args.UID)
	if err != nil {
		return "", err
	}
	return args.UID, nil
}

func (a *QBittorrent) Remove(task *tool.DownloadTask) error {
	err := a.client.Delete(task.GID, false)
	return err
}

func (a *QBittorrent) Status(task *tool.DownloadTask) (*tool.Status, error) {
	info, err := a.client.GetInfo(task.GID)
	if err != nil {
		return nil, err
	}
	s := &tool.Status{}
	s.TotalBytes = info.Size
	s.Progress = float64(info.Completed) / float64(info.Size) * 100
	switch info.State {
	case qbittorrent.UPLOADING, qbittorrent.PAUSEDUP, qbittorrent.QUEUEDUP, qbittorrent.STALLEDUP, qbittorrent.FORCEDUP, qbittorrent.CHECKINGUP:
		s.Completed = true
	case qbittorrent.ALLOCATING, qbittorrent.DOWNLOADING, qbittorrent.METADL, qbittorrent.PAUSEDDL, qbittorrent.QUEUEDDL, qbittorrent.STALLEDDL, qbittorrent.CHECKINGDL, qbittorrent.FORCEDDL, qbittorrent.CHECKINGRESUMEDATA, qbittorrent.MOVING:
		s.Status = "[qBittorrent] downloading"
	case qbittorrent.ERROR, qbittorrent.MISSINGFILES, qbittorrent.UNKNOWN:
		s.Err = errors.Errorf("[qBittorrent] failed to download %s, error: %s", task.GID, info.State)
	default:
		s.Err = errors.Errorf("[qBittorrent] unknown error occurred downloading %s", task.GID)
	}
	return s, nil
}

var _ tool.Tool = (*QBittorrent)(nil)

func init() {
	tool.Tools.Add(&QBittorrent{})
}
