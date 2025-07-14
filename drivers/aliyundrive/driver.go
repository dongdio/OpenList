package aliyundrive

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/cron"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// AliDrive 阿里云盘驱动实现
type AliDrive struct {
	model.Storage
	Addition
	AccessToken string
	cron        *cron.Cron
	DriveID     string
	UserID      string
}

// Config 返回驱动配置
func (d *AliDrive) Config() driver.Config {
	return config
}

// GetAddition 返回额外配置
func (d *AliDrive) GetAddition() driver.Additional {
	return &d.Addition
}

// Init 初始化驱动
func (d *AliDrive) Init(ctx context.Context) error {
	// 刷新令牌
	err := d.refreshToken()
	if err != nil {
		return errors.Wrap(err, "刷新令牌失败")
	}

	// 获取驱动ID和用户ID
	res, err, _ := d.request("https://api.alipan.com/v2/user/get", http.MethodPost, nil, nil)
	if err != nil {
		return errors.Wrap(err, "获取用户信息失败")
	}
	d.DriveID = utils.GetBytes(res, "default_drive_id").String()
	d.UserID = utils.GetBytes(res, "user_id").String()

	// 设置定时刷新令牌
	d.cron = cron.NewCron(time.Hour * 2)
	d.cron.Do(func() {
		err := d.refreshToken()
		if err != nil {
			log.Errorf("刷新令牌失败: %+v", err)
		}
	})

	if global.Has(d.UserID) {
		return nil
	}

	// 初始化设备ID
	deviceID := utils.HashData(utils.SHA256, []byte(d.UserID))
	// 初始化私钥
	privateKey, _ := PrivateKeyFromHex(deviceID)
	state := State{
		privateKey: privateKey,
		deviceID:   deviceID,
	}
	// 存储状态
	global.Store(d.UserID, &state)
	// 初始化签名
	d.sign()
	return nil
}

// Drop 释放驱动资源
func (d *AliDrive) Drop(ctx context.Context) error {
	if d.cron != nil {
		d.cron.Stop()
	}
	return nil
}

// List 列出目录内容
func (d *AliDrive) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.getFiles(dir.GetID())
	if err != nil {
		return nil, errors.Wrap(err, "获取文件列表失败")
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

// Link 获取文件下载链接
func (d *AliDrive) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	data := base.Json{
		"drive_id":   d.DriveID,
		"file_id":    file.GetID(),
		"expire_sec": 14400,
	}
	res, err, _ := d.request("https://api.alipan.com/v2/file/get_download_url", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, nil)
	if err != nil {
		return nil, errors.Wrap(err, "获取下载链接失败")
	}
	return &model.Link{
		Header: http.Header{
			"Referer": []string{"https://www.alipan.com/"},
		},
		URL: utils.GetBytes(res, "url").String(),
	}, nil
}

// MakeDir 创建目录
func (d *AliDrive) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	_, err, _ := d.request("https://api.alipan.com/adrive/v2/file/createWithFolders", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"check_name_mode": "refuse",
			"drive_id":        d.DriveID,
			"name":            dirName,
			"parent_file_id":  parentDir.GetID(),
			"type":            "folder",
		})
	}, nil)
	return err
}

// Move 移动文件/目录
func (d *AliDrive) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	err := d.batch(srcObj.GetID(), dstDir.GetID(), "/file/move")
	return err
}

// Rename 重命名文件/目录
func (d *AliDrive) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	_, err, _ := d.request("https://api.alipan.com/v3/file/update", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"check_name_mode": "refuse",
			"drive_id":        d.DriveID,
			"file_id":         srcObj.GetID(),
			"name":            newName,
		})
	}, nil)
	return err
}

// Copy 复制文件/目录
func (d *AliDrive) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	err := d.batch(srcObj.GetID(), dstDir.GetID(), "/file/copy")
	return err
}

// Remove 删除文件/目录
func (d *AliDrive) Remove(ctx context.Context, obj model.Obj) error {
	_, err, _ := d.request("https://api.alipan.com/v2/recyclebin/trash", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id": d.DriveID,
			"file_id":  obj.GetID(),
		})
	}, nil)
	return err
}

// Put 上传文件
func (d *AliDrive) Put(ctx context.Context, dstDir model.Obj, streamer model.FileStreamer, up driver.UpdateProgress) error {
	file := &stream.FileStream{
		Obj:      streamer,
		Reader:   streamer,
		Mimetype: streamer.GetMimetype(),
	}
	const DEFAULT int64 = 10485760 // 10MB
	var count = int(math.Ceil(float64(streamer.GetSize()) / float64(DEFAULT)))

	// 准备分片信息
	partInfoList := make([]base.Json, 0, count)
	for i := 1; i <= count; i++ {
		partInfoList = append(partInfoList, base.Json{"part_number": i})
	}

	// 准备请求体
	reqBody := base.Json{
		"check_name_mode": "overwrite",
		"drive_id":        d.DriveID,
		"name":            file.GetName(),
		"parent_file_id":  dstDir.GetID(),
		"part_info_list":  partInfoList,
		"size":            file.GetSize(),
		"type":            "file",
	}

	var localFile *os.File
	if fileStream, ok := file.Reader.(*stream.FileStream); ok {
		localFile, _ = fileStream.Reader.(*os.File)
	}

	// 处理秒传
	if d.RapidUpload {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		_, err := utils.CopyWithBufferN(buf, file, 1024)
		if err != nil {
			return errors.Wrap(err, "读取文件头部失败")
		}
		reqBody["pre_hash"] = utils.HashData(utils.SHA1, buf.Bytes())
		if localFile != nil {
			if _, err = localFile.Seek(0, io.SeekStart); err != nil {
				return errors.Wrap(err, "文件指针重置失败")
			}
		} else {
			// 把头部拼接回去
			file.Reader = struct {
				io.Reader
				io.Closer
			}{
				Reader: io.MultiReader(buf, file),
				Closer: file,
			}
		}
	} else {
		reqBody["content_hash_name"] = "none"
		reqBody["proof_version"] = "v1"
	}

	var resp UploadResp
	_, err, e := d.request("https://api.alipan.com/adrive/v2/file/createWithFolders", http.MethodPost, func(req *resty.Request) {
		req.SetBody(reqBody)
	}, &resp)

	if err != nil && e.Code != "PreHashMatched" {
		return errors.Wrap(err, "创建文件失败")
	}

	// 处理预哈希匹配的情况
	if d.RapidUpload && e.Code == "PreHashMatched" {
		delete(reqBody, "pre_hash")
		h := sha1.New()
		if localFile != nil {
			if err = utils.CopyWithCtx(ctx, h, localFile, 0, nil); err != nil {
				return errors.Wrap(err, "计算文件哈希失败")
			}
			if _, err = localFile.Seek(0, io.SeekStart); err != nil {
				return errors.Wrap(err, "文件指针重置失败")
			}
		} else {
			tempFile, err := os.CreateTemp(conf.Conf.TempDir, "file-*")
			if err != nil {
				return errors.Wrap(err, "创建临时文件失败")
			}
			defer func() {
				_ = tempFile.Close()
				_ = os.Remove(tempFile.Name())
			}()
			if err = utils.CopyWithCtx(ctx, io.MultiWriter(tempFile, h), file, 0, nil); err != nil {
				return errors.Wrap(err, "写入临时文件失败")
			}
			localFile = tempFile
		}
		reqBody["content_hash"] = hex.EncodeToString(h.Sum(nil))
		reqBody["content_hash_name"] = "sha1"
		reqBody["proof_version"] = "v1"

		/*
			js 隐性转换太坑不知道有没有bug
			var n = e.access_token，
			r = new BigNumber('0x'.concat(md5(n).slice(0, 16)))，
			i = new BigNumber(t.file.size)，
			o = i ? r.mod(i) : new gt.BigNumber(0);
			(t.file.slice(o.toNumber(), Math.min(o.plus(8).toNumber(), t.file.size)))
		*/
		buf := make([]byte, 8)
		r, _ := new(big.Int).SetString(utils.GetMD5EncodeStr(d.AccessToken)[:16], 16)
		i := new(big.Int).SetInt64(file.GetSize())
		o := new(big.Int).SetInt64(0)
		if file.GetSize() > 0 {
			o = r.Mod(r, i)
		}
		n, _ := io.NewSectionReader(localFile, o.Int64(), 8).Read(buf[:8])
		reqBody["proof_code"] = base64.StdEncoding.EncodeToString(buf[:n])

		_, err, e := d.request("https://api.alipan.com/adrive/v2/file/createWithFolders", http.MethodPost, func(req *resty.Request) {
			req.SetBody(reqBody)
		}, &resp)
		if err != nil && e.Code != "PreHashMatched" {
			return errors.Wrap(err, "秒传验证失败")
		}
		if resp.RapidUpload {
			return nil
		}
		// 秒传失败
		if _, err = localFile.Seek(0, io.SeekStart); err != nil {
			return errors.Wrap(err, "文件指针重置失败")
		}
		file.Reader = localFile
	}

	// 分片上传
	rateLimited := driver.NewLimitedUploadStream(ctx, file)
	for i, partInfo := range resp.PartInfoList {
		if utils.IsCanceled(ctx) {
			return ctx.Err()
		}
		url := partInfo.UploadURL
		if d.InternalUpload {
			url = partInfo.InternalUploadURL
		}
		req, err := http.NewRequest("PUT", url, io.LimitReader(rateLimited, DEFAULT))
		if err != nil {
			return errors.Wrap(err, "创建上传请求失败")
		}
		req = req.WithContext(ctx)
		res, err := base.HttpClient.Do(req)
		if err != nil {
			return errors.Wrap(err, "上传分片失败")
		}
		_ = res.Body.Close()
		if count > 0 {
			up(float64(i) * 100 / float64(count))
		}
	}

	// 完成上传
	var resp2 base.Json
	_, err, e = d.request("https://api.alipan.com/v2/file/complete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":  d.DriveID,
			"file_id":   resp.FileID,
			"upload_id": resp.UploadID,
		})
	}, &resp2)
	if err != nil && e.Code != "PreHashMatched" {
		return errors.Wrap(err, "完成上传失败")
	}
	if resp2["file_id"] == resp.FileID {
		return nil
	}
	return errors.Errorf("上传完成但返回的文件ID不匹配: %+v", resp2)
}

// Other 处理其他操作
func (d *AliDrive) Other(ctx context.Context, args model.OtherArgs) (any, error) {
	var resp base.Json
	var url string
	data := base.Json{
		"drive_id": d.DriveID,
		"file_id":  args.Obj.GetID(),
	}
	switch args.Method {
	case "doc_preview":
		url = "https://api.alipan.com/v2/file/get_office_preview_url"
		data["access_token"] = d.AccessToken
	case "video_preview":
		url = "https://api.alipan.com/v2/file/get_video_preview_play_info"
		data["category"] = "live_transcoding"
		data["url_expire_sec"] = 14400
	default:
		return nil, errs.NotSupport
	}
	_, err, _ := d.request(url, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, &resp)
	if err != nil {
		return nil, errors.Wrap(err, "处理其他操作失败")
	}
	return resp, nil
}

var _ driver.Driver = (*AliDrive)(nil)