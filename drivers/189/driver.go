package _189

import (
	"context"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Cloud189 189云盘存储驱动实现
type Cloud189 struct {
	model.Storage
	Addition
	header     map[string]string // HTTP请求头
	rsa        Rsa               // RSA加密参数
	sessionKey string            // 会话密钥
}

// Config 返回驱动配置
// 实现driver.Driver接口
func (d *Cloud189) Config() driver.Config {
	return config
}

// GetAddition 返回额外配置
// 实现driver.Driver接口
func (d *Cloud189) GetAddition() driver.Additional {
	return &d.Addition
}

// Init 初始化驱动
// 实现driver.Driver接口
func (d *Cloud189) Init(ctx context.Context) error {
	// 初始化HTTP请求头
	d.header = map[string]string{
		"Referer": "https://cloud.189.cn/",
	}

	// 执行登录
	return d.newLogin()
}

// Drop 释放资源
// 实现driver.Driver接口
func (d *Cloud189) Drop(ctx context.Context) error {
	// 无需特殊资源释放
	return nil
}

// List 列出目录内容
// 实现driver.Driver接口
func (d *Cloud189) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	return d.getFiles(dir.GetID())
}

// Link 获取文件下载链接
// 实现driver.Driver接口
func (d *Cloud189) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	var resp DownResp
	fileInfoURL := "https://cloud.189.cn/api/portal/getFileInfo.action"

	// 获取文件信息
	_, err := d.request(fileInfoURL, http.MethodGet, func(req *resty.Request) {
		req.SetQueryParam("fileId", file.GetID())
	}, &resp)

	if err != nil {
		return nil, errors.Wrap(err, "获取文件信息失败")
	}

	// 创建不自动重定向的客户端
	client := base.NoRedirectClient.R().
		SetHeaders(d.header).
		SetHeader("User-Agent", base.UserAgent)

	// 请求下载链接
	res, err := client.Get("https:" + resp.FileDownloadUrl)
	if err != nil {
		return nil, errors.Wrap(err, "请求下载链接失败")
	}

	log.Debugln("下载链接状态:", res.Status(), res.String())

	link := model.Link{}
	log.Debugln("初始下载URL:", resp.FileDownloadUrl)

	// 处理重定向
	if res.StatusCode() == 302 {
		link.URL = res.Header().Get("location")
		log.Debugln("重定向后URL:", link.URL)

		// 再次请求以获取最终URL
		res, err = client.Get(link.URL)
		if err != nil {
			log.Warnf("请求重定向链接失败: %s", err.Error())
		} else if res.StatusCode() == 302 {
			link.URL = res.Header().Get("location")
			log.Debugln("最终URL:", link.URL)
		}
	} else {
		link.URL = resp.FileDownloadUrl
	}

	// 确保使用HTTPS
	link.URL = strings.Replace(link.URL, "http://", "https://", 1)
	return &link, nil
}

// MakeDir 创建目录
// 实现driver.Driver接口
func (d *Cloud189) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	form := map[string]string{
		"parentFolderId": parentDir.GetID(),
		"folderName":     dirName,
	}

	_, err := d.request("https://cloud.189.cn/api/open/file/createFolder.action", http.MethodPost, func(req *resty.Request) {
		req.SetFormData(form)
	}, nil)

	if err != nil {
		return errors.Wrap(err, "创建目录失败")
	}

	return nil
}

// Move 移动文件/目录
// 实现driver.Driver接口
func (d *Cloud189) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	// 确定对象类型
	isFolder := 0
	if srcObj.IsDir() {
		isFolder = 1
	}

	// 构建任务信息
	taskInfos := []base.Json{
		{
			"fileId":   srcObj.GetID(),
			"fileName": srcObj.GetName(),
			"isFolder": isFolder,
		},
	}

	// 序列化任务信息
	taskInfosBytes, err := utils.Json.Marshal(taskInfos)
	if err != nil {
		return errors.Wrap(err, "序列化任务信息失败")
	}

	// 构建表单数据
	form := map[string]string{
		"type":           "MOVE",
		"targetFolderId": dstDir.GetID(),
		"taskInfos":      string(taskInfosBytes),
	}

	// 发送请求
	_, err = d.request("https://cloud.189.cn/api/open/batch/createBatchTask.action", http.MethodPost, func(req *resty.Request) {
		req.SetFormData(form)
	}, nil)

	if err != nil {
		return errors.Wrap(err, "移动文件失败")
	}

	return nil
}

// Rename 重命名文件/目录
// 实现driver.Driver接口
func (d *Cloud189) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	// 根据对象类型选择不同的API和参数
	url := "https://cloud.189.cn/api/open/file/renameFile.action"
	idKey := "fileId"
	nameKey := "destFileName"

	if srcObj.IsDir() {
		url = "https://cloud.189.cn/api/open/file/renameFolder.action"
		idKey = "folderId"
		nameKey = "destFolderName"
	}

	// 构建表单数据
	form := map[string]string{
		idKey:   srcObj.GetID(),
		nameKey: newName,
	}

	// 发送请求
	_, err := d.request(url, http.MethodPost, func(req *resty.Request) {
		req.SetFormData(form)
	}, nil)

	if err != nil {
		return errors.Wrap(err, "重命名失败")
	}

	return nil
}

// Copy 复制文件/目录
// 实现driver.Driver接口
func (d *Cloud189) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	// 确定对象类型
	isFolder := 0
	if srcObj.IsDir() {
		isFolder = 1
	}

	// 构建任务信息
	taskInfos := []base.Json{
		{
			"fileId":   srcObj.GetID(),
			"fileName": srcObj.GetName(),
			"isFolder": isFolder,
		},
	}

	// 序列化任务信息
	taskInfosBytes, err := utils.Json.Marshal(taskInfos)
	if err != nil {
		return errors.Wrap(err, "序列化任务信息失败")
	}

	// 构建表单数据
	form := map[string]string{
		"type":           "COPY",
		"targetFolderId": dstDir.GetID(),
		"taskInfos":      string(taskInfosBytes),
	}

	// 发送请求
	_, err = d.request("https://cloud.189.cn/api/open/batch/createBatchTask.action", http.MethodPost, func(req *resty.Request) {
		req.SetFormData(form)
	}, nil)

	if err != nil {
		return errors.Wrap(err, "复制文件失败")
	}

	return nil
}

// Remove 删除文件/目录
// 实现driver.Driver接口
func (d *Cloud189) Remove(ctx context.Context, obj model.Obj) error {
	// 确定对象类型
	isFolder := 0
	if obj.IsDir() {
		isFolder = 1
	}

	// 构建任务信息
	taskInfos := []base.Json{
		{
			"fileId":   obj.GetID(),
			"fileName": obj.GetName(),
			"isFolder": isFolder,
		},
	}

	// 序列化任务信息
	taskInfosBytes, err := utils.Json.Marshal(taskInfos)
	if err != nil {
		return errors.Wrap(err, "序列化任务信息失败")
	}

	// 构建表单数据
	form := map[string]string{
		"type":           "DELETE",
		"targetFolderId": "",
		"taskInfos":      string(taskInfosBytes),
	}

	// 发送请求
	_, err = d.request("https://cloud.189.cn/api/open/batch/createBatchTask.action", http.MethodPost, func(req *resty.Request) {
		req.SetFormData(form)
	}, nil)

	if err != nil {
		return errors.Wrap(err, "删除文件失败")
	}

	return nil
}

// Put 上传文件
// 实现driver.Driver接口
func (d *Cloud189) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	return d.newUpload(ctx, dstDir, stream, up)
}

// 确保Cloud189实现了driver.Driver接口
var _ driver.Driver = (*Cloud189)(nil)
