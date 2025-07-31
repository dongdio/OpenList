package webdav

import (
	"context"
	"net/http"
	"os"
	"path"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/gowebdav"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type WebDav struct {
	model.Storage
	Addition
	client      *gowebdav.Client
	cronEntryId cron.EntryID
}

func (d *WebDav) Config() driver.Config {
	return config
}

func (d *WebDav) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *WebDav) Init(ctx context.Context) error {
	err := d.setClient()
	if err == nil {
		// 每12小时刷新一次
		d.cronEntryId, err = global.CronConfig.AddFunc("0 */12 * * *", func() {
			err := d.setClient()
			if err != nil {
				log.Errorf("%+v", err)
			}
		})
		if err != nil {
			log.Errorf("webdav 设置定时任务失败: %+v\n", err)
		}
	}
	return err
}

func (d *WebDav) Drop(ctx context.Context) error {
	if d.cronEntryId > 0 {
		global.CronConfig.Remove(d.cronEntryId)
		d.cronEntryId = 0
	}
	return nil
}

func (d *WebDav) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.client.ReadDir(dir.GetPath())
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src os.FileInfo) (model.Obj, error) {
		return &model.Object{
			Name:     src.Name(),
			Size:     src.Size(),
			Modified: src.ModTime(),
			IsFolder: src.IsDir(),
		}, nil
	})
}

func (d *WebDav) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	url, header, err := d.client.Link(file.GetPath())
	if err != nil {
		return nil, err
	}
	return &model.Link{
		URL:    url,
		Header: header,
	}, nil
}

func (d *WebDav) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	return d.client.MkdirAll(path.Join(parentDir.GetPath(), dirName), 0644)
}

func (d *WebDav) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	return d.client.Rename(getPath(srcObj), path.Join(dstDir.GetPath(), srcObj.GetName()), true)
}

func (d *WebDav) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	return d.client.Rename(getPath(srcObj), path.Join(path.Dir(srcObj.GetPath()), newName), true)
}

func (d *WebDav) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return d.client.Copy(getPath(srcObj), path.Join(dstDir.GetPath(), srcObj.GetName()), true)
}

func (d *WebDav) Remove(ctx context.Context, obj model.Obj) error {
	return d.client.RemoveAll(getPath(obj))
}

func (d *WebDav) Put(ctx context.Context, dstDir model.Obj, s model.FileStreamer, up driver.UpdateProgress) error {
	callback := func(r *http.Request) {
		r.Header.Set("Content-Type", s.GetMimetype())
		r.ContentLength = s.GetSize()
	}
	reader := driver.NewLimitedUploadStream(ctx, &driver.ReaderUpdatingProgress{
		Reader:         s,
		UpdateProgress: up,
	})
	err := d.client.WriteStream(path.Join(dstDir.GetPath(), s.GetName()), reader, 0644, callback)
	return err
}

var _ driver.Driver = (*WebDav)(nil)