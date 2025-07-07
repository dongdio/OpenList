package aliyundrive_open

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/OpenListTeam/rateg"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// AliyundriveOpen 阿里云盘开放平台驱动实现
// 实现了阿里云盘开放平台的文件存储驱动
type AliyundriveOpen struct {
	model.Storage // 存储基础信息
	Addition      // 额外配置信息

	DriveID string // 云盘ID

	// 限流函数，用于控制API请求频率
	limitList func(ctx context.Context, data base.Json) (*Files, error)
	limitLink func(ctx context.Context, file model.Obj) (*model.Link, error)
	ref       *AliyundriveOpen // 引用另一个驱动实例，用于共享配置
}

// Config 返回驱动配置
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) Config() driver.Config {
	return config
}

// GetAddition 返回额外配置
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) GetAddition() driver.Additional {
	return &d.Addition
}

// Init 初始化驱动
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) Init(ctx context.Context) error {
	// 设置默认值
	if d.LIVPDownloadFormat == "" {
		d.LIVPDownloadFormat = "jpeg"
	}
	if d.DriveType == "" {
		d.DriveType = "default"
	}

	// 获取驱动ID
	res, err := d.request("/adrive/v1.0/user/getDriveInfo", http.MethodPost, nil)
	if err != nil {
		return errors.Wrap(err, "获取驱动信息失败")
	}
	d.DriveID = utils.GetBytes(res, d.DriveType+"_drive_id").String()
	if d.DriveID == "" {
		return errors.Errorf("获取驱动ID失败，请确认驱动类型[%s]是否正确", d.DriveType)
	}

	// 设置限流
	d.limitList = rateg.LimitFnCtx(d.list, rateg.LimitFnOption{
		Limit:  4, // 每秒最多4个请求
		Bucket: 1,
	})
	d.limitLink = rateg.LimitFnCtx(d.link, rateg.LimitFnOption{
		Limit:  1, // 每秒最多1个请求
		Bucket: 1,
	})
	return nil
}

// InitReference 初始化引用
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) InitReference(storage driver.Driver) error {
	refStorage, ok := storage.(*AliyundriveOpen)
	if ok {
		d.ref = refStorage
		return nil
	}
	return errs.NotSupport
}

// Drop 释放资源
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) Drop(ctx context.Context) error {
	d.ref = nil
	return nil
}

// GetRoot 获取根目录对象
// 实现 driver.GetRooter 接口以正确设置根对象
func (d *AliyundriveOpen) GetRoot(ctx context.Context) (model.Obj, error) {
	return &model.Object{
		ID:       d.RootFolderID,
		Path:     "/",
		Name:     "root",
		Size:     0,
		Modified: d.Modified,
		IsFolder: true,
	}, nil
}

// List 列出目录内容
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if d.limitList == nil {
		return nil, errors.New("驱动未初始化")
	}

	// 获取文件列表
	files, err := d.getFiles(ctx, dir.GetID())
	if err != nil {
		return nil, errors.Wrap(err, "获取文件列表失败")
	}

	// 转换为通用对象
	objs, err := utils.SliceConvert(files, func(src File) (model.Obj, error) {
		obj := fileToObj(src)
		// 设置正确的路径
		if dir.GetPath() != "" {
			obj.Path = filepath.Join(dir.GetPath(), obj.GetName())
		}
		return obj, nil
	})

	return objs, err
}

// link 获取文件下载链接（内部方法，带限流）
func (d *AliyundriveOpen) link(ctx context.Context, file model.Obj) (*model.Link, error) {
	res, err := d.request("/adrive/v1.0/openFile/getDownloadUrl", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":   d.DriveID,
			"file_id":    file.GetID(),
			"expire_sec": 14400, // 链接有效期4小时
		})
	})
	if err != nil {
		return nil, errors.Wrap(err, "获取下载链接失败")
	}

	// 获取下载URL
	url := utils.GetBytes(res, "url").String()
	if url == "" {
		// 处理LIVP文件特殊情况
		if utils.Ext(file.GetName()) == "livp" {
			url = utils.GetBytes(res, "streamsUrl", d.LIVPDownloadFormat).String()
			if url == "" {
				return nil, errors.Errorf("获取LIVP文件下载URL失败: %s", string(res))
			}
		} else {
			return nil, errors.Errorf("获取下载URL失败: %s", string(res))
		}
	}
	exp := time.Minute * 60 // 设置缓存时间为60分钟
	return &model.Link{
		URL:        url,
		Expiration: &exp,
	}, nil
}

// Link 获取文件下载链接
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if d.limitLink == nil {
		return nil, errors.New("驱动未初始化")
	}
	return d.limitLink(ctx, file)
}

// MakeDir 创建目录
// 实现 driver.MkdirResult 接口
func (d *AliyundriveOpen) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	nowTime, _ := getNowTime()
	newDir := File{CreatedAt: nowTime, UpdatedAt: nowTime}

	// 创建目录
	_, err := d.request("/adrive/v1.0/openFile/create", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":        d.DriveID,
			"parent_file_id":  parentDir.GetID(),
			"name":            dirName,
			"type":            "folder",
			"check_name_mode": "refuse", // 如果存在同名文件，拒绝创建
		}).SetResult(&newDir)
	})
	if err != nil {
		return nil, errors.Wrap(err, "创建目录失败")
	}

	obj := fileToObj(newDir)

	// 设置正确的路径
	if parentDir.GetPath() != "" {
		obj.Path = filepath.Join(parentDir.GetPath(), dirName)
	} else {
		obj.Path = "/" + dirName
	}

	return obj, nil
}

// Move 移动文件/目录
// 实现 driver.MoveResult 接口
func (d *AliyundriveOpen) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	var resp MoveOrCopyResp

	// 移动文件/目录
	_, err := d.request("/adrive/v1.0/openFile/move", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":          d.DriveID,
			"file_id":           srcObj.GetID(),
			"to_parent_file_id": dstDir.GetID(),
			"check_name_mode":   "ignore", // 可选值: ignore,auto_rename,refuse
			// "new_name":          "newName", // 当存在同名文件时使用的新名称
		}).SetResult(&resp)
	})
	if err != nil {
		return nil, errors.Wrap(err, "移动文件失败")
	}

	// 更新对象信息
	if srcObj, ok := srcObj.(*model.ObjThumb); ok {
		srcObj.ID = resp.FileID
		srcObj.Modified = time.Now()
		srcObj.Path = filepath.Join(dstDir.GetPath(), srcObj.GetName())

		// 检查目标目录中是否有重复文件
		if err := d.removeDuplicateFiles(ctx, dstDir.GetPath(), srcObj.GetName(), srcObj.GetID()); err != nil {
			// 只记录警告而不返回错误，因为移动操作已经成功完成
			log.Warnf("[阿里云盘开放版] 移动后删除重复文件失败: %v", err)
		}
		return srcObj, nil
	}
	return nil, errors.New("无法转换源对象类型")
}

// Rename 重命名文件/目录
// 实现 driver.RenameResult 接口
func (d *AliyundriveOpen) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	var newFile File

	// 重命名文件/目录
	_, err := d.request("/adrive/v1.0/openFile/update", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id": d.DriveID,
			"file_id":  srcObj.GetID(),
			"name":     newName,
		}).SetResult(&newFile)
	})
	if err != nil {
		return nil, errors.Wrap(err, "重命名文件失败")
	}

	// 检查父目录中是否有重复文件
	parentPath := filepath.Dir(srcObj.GetPath())
	if err := d.removeDuplicateFiles(ctx, parentPath, newName, newFile.FileID); err != nil {
		// 只记录警告而不返回错误，因为重命名操作已经成功完成
		log.Warnf("[阿里云盘开放版] 重命名后删除重复文件失败: %v", err)
	}

	obj := fileToObj(newFile)

	// 设置正确的路径
	if parentPath != "" && parentPath != "." {
		obj.Path = filepath.Join(parentPath, newName)
	} else {
		obj.Path = "/" + newName
	}

	return obj, nil
}

// Copy 复制文件/目录
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	var resp MoveOrCopyResp

	// 复制文件/目录
	_, err := d.request("/adrive/v1.0/openFile/copy", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":          d.DriveID,
			"file_id":           srcObj.GetID(),
			"to_parent_file_id": dstDir.GetID(),
			"auto_rename":       false, // 不自动重命名
		}).SetResult(&resp)
	})
	if err != nil {
		return errors.Wrap(err, "复制文件失败")
	}

	// 检查目标目录中是否有重复文件
	if err := d.removeDuplicateFiles(ctx, dstDir.GetPath(), srcObj.GetName(), resp.FileID); err != nil {
		// 只记录警告而不返回错误，因为复制操作已经成功完成
		log.Warnf("[阿里云盘开放版] 复制后删除重复文件失败: %v", err)
	}

	return nil
}

// Remove 删除文件/目录
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) Remove(ctx context.Context, obj model.Obj) error {
	// 根据配置选择删除方式
	uri := "/adrive/v1.0/openFile/recyclebin/trash"
	if d.RemoveWay == "delete" {
		uri = "/adrive/v1.0/openFile/delete"
	}

	// 删除文件/目录
	_, err := d.request(uri, http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id": d.DriveID,
			"file_id":  obj.GetID(),
		})
	})
	if err != nil {
		return errors.Wrap(err, "删除文件失败")
	}
	return nil
}

// Put 上传文件
// 实现 driver.PutResult 接口
func (d *AliyundriveOpen) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	// 上传文件
	obj, err := d.upload(ctx, dstDir, stream, up)
	if err != nil {
		return nil, errors.Wrap(err, "上传文件失败")
	}

	// 设置正确的路径
	if obj != nil && obj.GetPath() == "" {
		if dstDir.GetPath() != "" {
			if objWithPath, ok := obj.(model.SetPath); ok {
				objWithPath.SetPath(filepath.Join(dstDir.GetPath(), obj.GetName()))
			}
		}
	}

	return obj, nil
}

// Other 处理其他操作
// 实现 driver.Driver 接口
func (d *AliyundriveOpen) Other(ctx context.Context, args model.OtherArgs) (any, error) {
	var resp base.Json
	var uri string
	data := base.Json{
		"drive_id": d.DriveID,
		"file_id":  args.Obj.GetID(),
	}

	// 根据方法选择不同的API
	switch args.Method {
	case "video_preview":
		uri = "/adrive/v1.0/openFile/getVideoPreviewPlayInfo"
		data["category"] = "live_transcoding"
		data["url_expire_sec"] = 14400 // 链接有效期4小时
	default:
		return nil, errs.NotSupport
	}

	// 发送请求
	_, err := d.request(uri, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetResult(&resp)
	})
	if err != nil {
		return nil, errors.Wrap(err, "处理其他操作失败")
	}
	return resp, nil
}

// 确保AliyundriveOpen实现了所有必要的接口
var _ driver.Driver = (*AliyundriveOpen)(nil)
var _ driver.MkdirResult = (*AliyundriveOpen)(nil)
var _ driver.MoveResult = (*AliyundriveOpen)(nil)
var _ driver.RenameResult = (*AliyundriveOpen)(nil)
var _ driver.PutResult = (*AliyundriveOpen)(nil)
var _ driver.GetRooter = (*AliyundriveOpen)(nil)
