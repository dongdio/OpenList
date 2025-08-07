package onedrive_sharelink

import (
	"context"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type OnedriveSharelink struct {
	model.Storage
	cronEntryId cron.EntryID
	Addition
}

func (d *OnedriveSharelink) Config() driver.Config {
	return config
}

func (d *OnedriveSharelink) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *OnedriveSharelink) Init(ctx context.Context) error {
	// Initialize error variable
	var err error

	// If there is "-my" in the URL, it is NOT a SharePoint link
	d.IsSharepoint = !strings.Contains(d.ShareLinkURL, "-my")

	// Initialize cron job to run every hour
	d.cronEntryId, err = global.CronConfig.AddFunc("0 */1 * * *", func() {
		var err error
		d.Headers, err = d.getHeaders(ctx)
		if err != nil {
			log.Errorf("%+v", err)
		}
	})
	if err != nil {
		log.Errorf("onedrive sharelink 设置定时任务失败: %+v\n", err)
	}

	// Get initial headers
	d.Headers, err = d.getHeaders(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (d *OnedriveSharelink) Drop(ctx context.Context) error {
	if d.cronEntryId > 0 {
		global.CronConfig.Remove(d.cronEntryId)
		d.cronEntryId = 0
	}
	return nil
}

func (d *OnedriveSharelink) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	path := dir.GetPath()
	files, err := d.getFiles(ctx, path)
	if err != nil {
		return nil, err
	}

	// Convert the slice of files to the required model.Obj format
	return utils.SliceConvert(files, func(src Item) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *OnedriveSharelink) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// Get the unique ID of the file
	uniqueId := file.GetID()
	// Cut the first char and the last char
	uniqueId = uniqueId[1 : len(uniqueId)-1]
	url := d.downloadLinkPrefix + uniqueId
	header := d.Headers

	// If the headers are older than 30 minutes, get new headers
	if d.HeaderTime < time.Now().Unix()-1800 {
		var err error
		log.Debug("headers are older than 30 minutes, get new headers")
		header, err = d.getHeaders(ctx)
		if err != nil {
			return nil, err
		}
	}

	return &model.Link{
		URL:    url,
		Header: header,
	}, nil
}

func (d *OnedriveSharelink) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	// TODO create folder, optional
	return errs.NotImplement
}

func (d *OnedriveSharelink) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	// TODO move obj, optional
	return errs.NotImplement
}

func (d *OnedriveSharelink) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	// TODO rename obj, optional
	return errs.NotImplement
}

func (d *OnedriveSharelink) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	// TODO copy obj, optional
	return errs.NotImplement
}

func (d *OnedriveSharelink) Remove(ctx context.Context, obj model.Obj) error {
	// TODO remove obj, optional
	return errs.NotImplement
}

func (d *OnedriveSharelink) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	// TODO upload file, optional
	return errs.NotImplement
}

// func (d *OnedriveSharelink) Other(ctx context.Context, args model.OtherArgs) (any, error) {
//	return nil, errs.NotSupport
// }

var _ driver.Driver = (*OnedriveSharelink)(nil)