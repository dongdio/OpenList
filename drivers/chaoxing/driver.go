package chaoxing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type ChaoXing struct {
	model.Storage
	Addition
	cronEntryId cron.EntryID
	config      driver.Config
	conf        Conf
}

func (d *ChaoXing) Config() driver.Config {
	return d.config
}

func (d *ChaoXing) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *ChaoXing) refreshCookie() error {
	cookie, err := d.Login()
	if err != nil {
		d.Status = err.Error()
		op.MustSaveDriverStorage(d)
		return nil
	}
	d.Addition.Cookie = cookie
	op.MustSaveDriverStorage(d)
	return nil
}

func (d *ChaoXing) Init(ctx context.Context) error {
	err := d.refreshCookie()
	if err != nil {
		log.Errorf(err.Error())
	}

	d.cronEntryId, err = global.CronConfig.AddFunc("0 */12 * * *", func() {
		err := d.refreshCookie()
		if err != nil {
			log.Errorf("chaoxing refresh cookie error: %+v", err)
		}
	})
	if err != nil {
		log.Errorf("chaoxing 设置定时任务失败: %+v\n", err)
	}
	return nil
}

func (d *ChaoXing) Drop(ctx context.Context) error {
	if d.cronEntryId > 0 {
		global.CronConfig.Remove(d.cronEntryId)
		d.cronEntryId = 0
	}
	return nil
}

func (d *ChaoXing) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.GetFiles(dir.GetID())
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *ChaoXing) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	var resp DownResp
	ua := d.conf.ua
	fileId := strings.Split(file.GetID(), "$")[1]
	_, err := d.requestDownload("/screen/note_note/files/status/"+fileId, http.MethodPost, func(req *resty.Request) {
		req.SetHeader("User-Agent", ua)
	}, &resp)
	if err != nil {
		return nil, err
	}
	u := resp.Download
	return &model.Link{
		URL: u,
		Header: http.Header{
			"Cookie":     []string{d.Cookie},
			"Referer":    []string{d.conf.referer},
			"User-Agent": []string{ua},
		},
		Concurrency: 2,
		PartSize:    10 * utils.MB,
	}, nil
}

func (d *ChaoXing) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	query := map[string]string{
		"bbsid": d.Addition.Bbsid,
		"name":  dirName,
		"pid":   parentDir.GetID(),
	}
	var resp ListFileResp
	_, err := d.request("/pc/resource/addResourceFolder", http.MethodGet, func(req *resty.Request) {
		req.SetQueryParams(query)
	}, &resp)
	if err != nil {
		return err
	}
	if resp.Result != 1 {
		msg := fmt.Sprintf("error:%s", resp.Msg)
		return errors.New(msg)
	}
	return nil
}

func (d *ChaoXing) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	query := map[string]string{
		"bbsid":     d.Addition.Bbsid,
		"folderIds": srcObj.GetID(),
		"targetId":  dstDir.GetID(),
	}
	if !srcObj.IsDir() {
		query = map[string]string{
			"bbsid":    d.Addition.Bbsid,
			"recIds":   strings.Split(srcObj.GetID(), "$")[0],
			"targetId": dstDir.GetID(),
		}
	}
	var resp ListFileResp
	_, err := d.request("/pc/resource/moveResource", http.MethodGet, func(req *resty.Request) {
		req.SetQueryParams(query)
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Status {
		msg := fmt.Sprintf("error:%s", resp.Msg)
		return errors.New(msg)
	}
	return nil
}

func (d *ChaoXing) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	query := map[string]string{
		"bbsid":    d.Addition.Bbsid,
		"folderId": srcObj.GetID(),
		"name":     newName,
	}
	path := "/pc/resource/updateResourceFolderName"
	if !srcObj.IsDir() {
		// path = "/pc/resource/updateResourceFileName"
		// query = map[string]string{
		// 	"bbsid":    d.Addition.Bbsid,
		// 	"recIds":   strings.Split(srcObj.GetID(), "$")[0],
		// 	"name":     newName,
		// }
		return errors.New("此网盘不支持修改文件名")
	}
	var resp ListFileResp
	_, err := d.request(path, http.MethodGet, func(req *resty.Request) {
		req.SetQueryParams(query)
	}, &resp)
	if err != nil {
		return err
	}
	if resp.Result != 1 {
		msg := fmt.Sprintf("error:%s", resp.Msg)
		return errors.New(msg)
	}
	return nil
}

func (d *ChaoXing) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	// TODO copy obj, optional
	return errs.NotImplement
}

func (d *ChaoXing) Remove(ctx context.Context, obj model.Obj) error {
	query := map[string]string{
		"bbsid":     d.Addition.Bbsid,
		"folderIds": obj.GetID(),
	}
	path := "/pc/resource/deleteResourceFolder"
	var resp ListFileResp
	if !obj.IsDir() {
		path = "/pc/resource/deleteResourceFile"
		query = map[string]string{
			"bbsid":  d.Addition.Bbsid,
			"recIds": strings.Split(obj.GetID(), "$")[0],
		}
	}
	_, err := d.request(path, http.MethodGet, func(req *resty.Request) {
		req.SetQueryParams(query)
	}, &resp)
	if err != nil {
		return err
	}
	if resp.Result != 1 {
		msg := fmt.Sprintf("error:%s", resp.Msg)
		return errors.New(msg)
	}
	return nil
}

func (d *ChaoXing) Put(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) error {
	var resp UploadDataRsp
	_, err := d.request("https://noteyd.chaoxing.com/pc/files/getUploadConfig", http.MethodGet, func(req *resty.Request) {
	}, &resp)
	if err != nil {
		return err
	}
	if resp.Result != 1 {
		return errors.New("get upload data error")
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filePart, err := writer.CreateFormFile("file", file.GetName())
	if err != nil {
		return err
	}
	_, err = utils.CopyWithBuffer(filePart, file)
	if err != nil {
		return err
	}
	err = writer.WriteField("_token", resp.Msg.Token)
	if err != nil {
		return err
	}
	err = writer.WriteField("puid", fmt.Sprintf("%d", resp.Msg.Puid))
	if err != nil {
		fmt.Println("Error writing param2 to request body:", err)
		return err
	}
	err = writer.Close()
	if err != nil {
		return err
	}
	r := driver.NewLimitedUploadStream(ctx, &driver.ReaderUpdatingProgress{
		Reader: &driver.SimpleReaderWithSize{
			Reader: body,
			Size:   int64(body.Len()),
		},
		UpdateProgress: up,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://pan-yz.chaoxing.com/upload", r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Content-Length", fmt.Sprintf("%d", body.Len()))
	resps, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resps.Body.Close()
	bodys, err := io.ReadAll(resps.Body)
	if err != nil {
		return err
	}
	var fileRsp UploadFileDataRsp
	err = utils.JSONTool.Unmarshal(bodys, &fileRsp)
	if err != nil {
		return err
	}
	if fileRsp.Msg != "success" {
		return errors.New(fileRsp.Msg)
	}
	uploadDoneParam := UploadDoneParam{Key: fileRsp.ObjectID, Cataid: "100000019", Param: fileRsp.Data}
	params, err := utils.JSONTool.Marshal(uploadDoneParam)
	if err != nil {
		return err
	}
	query := map[string]string{
		"bbsid":  d.Addition.Bbsid,
		"pid":    dstDir.GetID(),
		"type":   "yunpan",
		"params": url.QueryEscape("[" + string(params) + "]"),
	}
	var respd ListFileResp
	_, err = d.request("/pc/resource/addResource", http.MethodGet, func(req *resty.Request) {
		req.SetQueryParams(query)
	}, &respd)
	if err != nil {
		return err
	}
	if respd.Result != 1 {
		msg := fmt.Sprintf("error:%v", resp.Msg)
		return errors.New(msg)
	}
	return nil
}

var _ driver.Driver = (*ChaoXing)(nil)