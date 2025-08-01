package weiyun

import (
	"context"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/avast/retry-go"
	weiyunsdkgo "github.com/foxxorcat/weiyun-sdk-go"
	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/errgroup"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type WeiYun struct {
	model.Storage
	Addition

	client      *weiyunsdkgo.WeiYunClient
	cronEntryId cron.EntryID
	rootFolder  *Folder

	uploadThread int
}

func (d *WeiYun) Config() driver.Config {
	return config
}

func (d *WeiYun) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *WeiYun) Init(ctx context.Context) error {
	// 限制上传线程数
	d.uploadThread, _ = strconv.Atoi(d.UploadThread)
	if d.uploadThread < 4 || d.uploadThread > 32 {
		d.uploadThread, d.UploadThread = 4, "4"
	}

	d.client = weiyunsdkgo.NewWeiYunClient().
		SetClient(base.NewRestyClient().Client())
	err := d.client.SetCookiesStr(d.Cookies).RefreshCtoken()
	if err != nil {
		return err
	}

	// Cookie过期回调
	d.client.SetOnCookieExpired(func(err error) {
		d.Status = err.Error()
		op.MustSaveDriverStorage(d)
	})

	// cookie更新回调
	d.client.SetOnCookieUpload(func(c []*http.Cookie) {
		d.Cookies = weiyunsdkgo.CookieToString(weiyunsdkgo.ClearCookie(c))
		op.MustSaveDriverStorage(d)
	})

	// qqCookie保活
	if d.client.LoginType() == 1 {
		d.cronEntryId, err = global.CronConfig.AddFunc("*/5 * * * *", func() {
			_ = d.client.KeepAlive()
		})

	}

	// 获取默认根目录dirKey
	if d.RootFolderID == "" {
		userInfo, err := d.client.DiskUserInfoGet()
		if err != nil {
			return err
		}
		d.RootFolderID = userInfo.MainDirKey
	}

	// 处理目录ID，找到PdirKey
	folders, err := d.client.LibDirPathGet(d.RootFolderID)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return errors.Errorf("invalid directory ID")
	}

	folder := folders[len(folders)-1]
	d.rootFolder = &Folder{
		PFolder: &Folder{
			Folder: weiyunsdkgo.Folder{
				DirKey: folder.PdirKey,
			},
		},
		Folder: folder.Folder,
	}
	return nil
}

func (d *WeiYun) Drop(ctx context.Context) error {
	d.client = nil
	if d.cronEntryId > 0 {
		global.CronConfig.Remove(d.cronEntryId)
		d.cronEntryId = 0
	}
	return nil
}

func (d *WeiYun) GetRoot(ctx context.Context) (model.Obj, error) {
	return d.rootFolder, nil
}

func (d *WeiYun) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if folder, ok := dir.(*Folder); ok {
		var files []model.Obj
		for {
			data, err := d.client.DiskDirFileList(folder.GetID(), weiyunsdkgo.WarpParamOption(
				weiyunsdkgo.QueryFileOptionOffest(int64(len(files))),
				weiyunsdkgo.QueryFileOptionGetType(weiyunsdkgo.FileAndDir),
				weiyunsdkgo.QueryFileOptionSort(func() weiyunsdkgo.OrderBy {
					switch d.OrderBy {
					case "name":
						return weiyunsdkgo.FileName
					case "size":
						return weiyunsdkgo.FileSize
					case "updated_at":
						return weiyunsdkgo.FileMtime
					default:
						return weiyunsdkgo.FileName
					}
				}(), d.OrderDirection == "desc"),
			))
			if err != nil {
				return nil, err
			}

			if files == nil {
				files = make([]model.Obj, 0, data.TotalDirCount+data.TotalFileCount)
			}

			for _, dir := range data.DirList {
				files = append(files, &Folder{
					PFolder: folder,
					Folder:  dir,
				})
			}

			for _, file := range data.FileList {
				files = append(files, &File{
					PFolder: folder,
					File:    file,
				})
			}

			if data.FinishFlag || len(data.DirList)+len(data.FileList) == 0 {
				return files, nil
			}
		}
	}
	return nil, errs.NotSupport
}

func (d *WeiYun) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if file, ok := file.(*File); ok {
		data, err := d.client.DiskFileDownload(weiyunsdkgo.FileParam{PdirKey: file.GetPKey(), FileID: file.GetID()})
		if err != nil {
			return nil, err
		}
		return &model.Link{
			URL: data.DownloadUrl,
			Header: http.Header{
				"Cookie": []string{data.CookieName + "=" + data.CookieValue},
			},
		}, nil
	}
	return nil, errs.NotSupport
}

func (d *WeiYun) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	if folder, ok := parentDir.(*Folder); ok {
		newFolder, err := d.client.DiskDirCreate(weiyunsdkgo.FolderParam{
			PPdirKey: folder.GetPKey(),
			PdirKey:  folder.DirKey,
			DirName:  dirName,
		})
		if err != nil {
			return nil, err
		}
		return &Folder{
			PFolder: folder,
			Folder:  *newFolder,
		}, nil
	}
	return nil, errs.NotSupport
}

func (d *WeiYun) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// TODO: 默认策略为重命名，使用缓存可能出现冲突。微云app也有这个冲突，不知道腾讯怎么搞的
	if dstDirFolder, ok := dstDir.(*Folder); ok {
		dstParam := weiyunsdkgo.FolderParam{
			PdirKey: dstDirFolder.GetPKey(),
			DirKey:  dstDirFolder.GetID(),
			DirName: dstDirFolder.GetName(),
		}
		switch srcObjValue := srcObj.(type) {
		case *File:
			err := d.client.DiskFileMove(weiyunsdkgo.FileParam{
				PPdirKey: srcObjValue.PFolder.GetPKey(),
				PdirKey:  srcObjValue.GetPKey(),
				FileID:   srcObjValue.GetID(),
				FileName: srcObjValue.GetName(),
			}, dstParam)
			if err != nil {
				return nil, err
			}
			return &File{
				PFolder: dstDirFolder,
				File:    srcObjValue.File,
			}, nil
		case *Folder:
			err := d.client.DiskDirMove(weiyunsdkgo.FolderParam{
				PPdirKey: srcObjValue.PFolder.GetPKey(),
				PdirKey:  srcObjValue.GetPKey(),
				DirKey:   srcObjValue.GetID(),
				DirName:  srcObjValue.GetName(),
			}, dstParam)
			if err != nil {
				return nil, err
			}
			return &Folder{
				PFolder: dstDirFolder,
				Folder:  srcObjValue.Folder,
			}, nil
		}
	}
	return nil, errs.NotSupport
}

func (d *WeiYun) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	switch srcObjValue := srcObj.(type) {
	case *File:
		err := d.client.DiskFileRename(weiyunsdkgo.FileParam{
			PPdirKey: srcObjValue.PFolder.GetPKey(),
			PdirKey:  srcObjValue.GetPKey(),
			FileID:   srcObjValue.GetID(),
			FileName: srcObjValue.GetName(),
		}, newName)
		if err != nil {
			return nil, err
		}
		newFile := srcObjValue.File
		newFile.FileName = newName
		newFile.FileCtime = weiyunsdkgo.TimeStamp(time.Now())
		return &File{
			PFolder: srcObjValue.PFolder,
			File:    newFile,
		}, nil
	case *Folder:
		err := d.client.DiskDirAttrModify(weiyunsdkgo.FolderParam{
			PPdirKey: srcObjValue.PFolder.GetPKey(),
			PdirKey:  srcObjValue.GetPKey(),
			DirKey:   srcObjValue.GetID(),
			DirName:  srcObjValue.GetName(),
		}, newName)
		if err != nil {
			return nil, err
		}

		newFolder := srcObjValue.Folder
		newFolder.DirName = newName
		newFolder.DirCtime = weiyunsdkgo.TimeStamp(time.Now())
		return &Folder{
			PFolder: srcObjValue.PFolder,
			Folder:  newFolder,
		}, nil
	}
	return nil, errs.NotSupport
}

func (d *WeiYun) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotImplement
}

func (d *WeiYun) Remove(ctx context.Context, obj model.Obj) error {
	switch objValue := obj.(type) {
	case *File:
		return d.client.DiskFileDelete(weiyunsdkgo.FileParam{
			PPdirKey: objValue.PFolder.GetPKey(),
			PdirKey:  objValue.GetPKey(),
			FileID:   objValue.GetID(),
			FileName: objValue.GetName(),
		})
	case *Folder:
		return d.client.DiskDirDelete(weiyunsdkgo.FolderParam{
			PPdirKey: objValue.PFolder.GetPKey(),
			PdirKey:  objValue.GetPKey(),
			DirKey:   objValue.GetID(),
			DirName:  objValue.GetName(),
		})
	}
	return errs.NotSupport
}

func (d *WeiYun) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	// NOTE:
	// 秒传需要sha1最后一个状态,但sha1无法逆运算需要读完整个文件(或许可以??)
	// 服务器支持上传进度恢复,不需要额外实现
	var folder *Folder
	var ok bool
	if folder, ok = dstDir.(*Folder); !ok {
		return nil, errs.NotSupport
	}
	file, err := stream.CacheFullInTempFile()
	if err != nil {
		return nil, err
	}

	// step 1.
	preData, err := d.client.PreUpload(ctx, weiyunsdkgo.UpdloadFileParam{
		PdirKey: folder.GetPKey(),
		DirKey:  folder.DirKey,

		FileName: stream.GetName(),
		FileSize: stream.GetSize(),
		File:     file,

		ChannelCount:    4,
		FileExistOption: 1,
	})
	if err != nil {
		return nil, err
	}

	// not fast upload
	if !preData.FileExist {
		// step.2 增加上传通道
		if len(preData.ChannelList) < d.uploadThread {
			newCh, err := d.client.AddUploadChannel(len(preData.ChannelList), d.uploadThread, preData.UploadAuthData)
			if err != nil {
				return nil, err
			}
			preData.ChannelList = append(preData.ChannelList, newCh.AddChannels...)
		}
		// step.3 上传
		threadG, upCtx := errgroup.NewGroupWithContext(ctx, len(preData.ChannelList),
			retry.Attempts(3),
			retry.Delay(time.Second),
			retry.DelayType(retry.BackOffDelay))

		total := atomic.Int64{}
		for _, channel := range preData.ChannelList {
			if utils.IsCanceled(upCtx) {
				break
			}

			var channel = channel
			threadG.Go(func(ctx context.Context) error {
				for {
					channel.Len = int(math.Min(float64(stream.GetSize()-channel.Offset), float64(channel.Len)))
					len64 := int64(channel.Len)
					upData, err := d.client.UploadFile(upCtx, channel, preData.UploadAuthData,
						driver.NewLimitedUploadStream(ctx, io.NewSectionReader(file, channel.Offset, len64)))
					if err != nil {
						return err
					}
					cur := total.Add(len64)
					up(float64(cur) * 100.0 / float64(stream.GetSize()))
					// 上传完成
					if upData.UploadState != 1 {
						return nil
					}
					channel = upData.Channel
				}
			})
		}
		if err = threadG.Wait(); err != nil {
			return nil, err
		}
	}

	return &File{
		PFolder: folder,
		File:    preData.File,
	}, nil
}

// func (d *WeiYun) Other(ctx context.Context, args model.OtherArgs) (any, error) {
// 	return nil, errs.NotSupport
// }

var _ driver.Driver = (*WeiYun)(nil)
var _ driver.GetRooter = (*WeiYun)(nil)
var _ driver.MkdirResult = (*WeiYun)(nil)

// var _ driver.CopyResult = (*WeiYun)(nil)
var _ driver.MoveResult = (*WeiYun)(nil)
var _ driver.Remove = (*WeiYun)(nil)

var _ driver.PutResult = (*WeiYun)(nil)
var _ driver.RenameResult = (*WeiYun)(nil)