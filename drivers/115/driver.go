package _115

import (
	"context"
	"strings"
	"sync"

	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/pkg/errors"
	"golang.org/x/time/rate"

	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	streamPkg "github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Pan115 115云盘存储驱动实现
type Pan115 struct {
	model.Storage
	Addition
	client     *driver115.Pan115Client // 115云盘客户端
	limiter    *rate.Limiter           // 请求速率限制器
	appVerOnce sync.Once               // 确保应用版本只初始化一次
}

// Config 返回驱动配置
// 实现driver.Driver接口
func (p *Pan115) Config() driver.Config {
	return config
}

// GetAddition 返回额外配置
// 实现driver.Driver接口
func (p *Pan115) GetAddition() driver.Additional {
	return &p.Addition
}

// Init 初始化驱动
// 实现driver.Driver接口
func (p *Pan115) Init(ctx context.Context) error {
	// 初始化应用版本号
	p.appVerOnce.Do(p.initAppVer)

	// 如果设置了速率限制，初始化限制器
	if p.LimitRate > 0 {
		p.limiter = rate.NewLimiter(rate.Limit(p.LimitRate), 1)
	}

	// 执行登录
	return p.login()
}

// WaitLimit 等待请求限制
// 如果设置了速率限制，会等待直到可以执行请求
// 参数:
//   - ctx: 上下文
//
// 返回:
//   - error: 错误信息，通常是上下文取消或超时
func (p *Pan115) WaitLimit(ctx context.Context) error {
	if p.limiter != nil {
		return p.limiter.Wait(ctx)
	}
	return nil
}

// Drop 释放资源
// 实现driver.Driver接口
func (p *Pan115) Drop(ctx context.Context) error {
	// 无需特殊资源释放
	return nil
}

// List 列出目录内容
// 实现driver.Driver接口
func (p *Pan115) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return nil, err
	}

	// 获取文件列表
	files, err := p.getFiles(dir.GetID())
	if err != nil && !errors.Is(err, driver115.ErrNotExist) {
		return nil, errors.Wrap(err, "获取文件列表失败")
	}

	// 转换为model.Obj接口类型
	return utils.SliceConvert(files, func(src FileObj) (model.Obj, error) {
		return &src, nil
	})
}

// Link 获取文件下载链接
// 实现driver.Driver接口
func (p *Pan115) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return nil, err
	}

	// 获取用户代理
	userAgent := args.Header.Get("User-Agent")

	// 获取下载信息
	downloadInfo, err := p.DownloadWithUA(file.(*FileObj).PickCode, userAgent)
	if err != nil {
		return nil, errors.Wrap(err, "获取下载信息失败")
	}

	// 构建链接
	link := &model.Link{
		URL:    downloadInfo.Url.Url,
		Header: downloadInfo.Header,
	}

	return link, nil
}

// MakeDir 创建目录
// 实现driver.Driver接口
func (p *Pan115) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return nil, err
	}

	// 准备请求数据
	result := driver115.MkdirResp{}
	form := map[string]string{
		"pid":   parentDir.GetID(), // 父目录ID
		"cname": dirName,           // 新目录名
	}

	// 创建请求
	req := p.client.NewRequest().
		SetFormData(form).
		SetResult(&result).
		ForceContentType("application/json;charset=UTF-8")

	// 发送请求
	resp, err := req.Post(driver115.ApiDirAdd)

	// 检查错误
	err = driver115.CheckErr(err, &result, resp)
	if err != nil {
		return nil, errors.Wrap(err, "创建目录失败")
	}

	// 获取新创建的目录信息
	newDir, err := p.getNewFile(result.FileID)
	if err != nil {
		return nil, errors.Wrap(err, "获取新目录信息失败")
	}

	return newDir, nil
}

// Move 移动文件/目录
// 实现driver.Driver接口
func (p *Pan115) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return nil, err
	}

	// 执行移动操作
	if err := p.client.Move(dstDir.GetID(), srcObj.GetID()); err != nil {
		return nil, errors.Wrap(err, "移动文件失败")
	}

	// 获取移动后的文件信息
	movedFile, err := p.getNewFile(srcObj.GetID())
	if err != nil {
		return nil, errors.Wrap(err, "获取移动后的文件信息失败")
	}

	return movedFile, nil
}

// Rename 重命名文件/目录
// 实现driver.Driver接口
func (p *Pan115) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return nil, err
	}

	// 执行重命名操作
	if err := p.client.Rename(srcObj.GetID(), newName); err != nil {
		return nil, errors.Wrap(err, "重命名失败")
	}

	// 获取重命名后的文件信息
	renamedFile, err := p.getNewFile(srcObj.GetID())
	if err != nil {
		return nil, errors.Wrap(err, "获取重命名后的文件信息失败")
	}

	return renamedFile, nil
}

// Copy 复制文件/目录
// 实现driver.Driver接口
func (p *Pan115) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return err
	}

	// 执行复制操作
	err := p.client.Copy(dstDir.GetID(), srcObj.GetID())
	if err != nil {
		return errors.Wrap(err, "复制文件失败")
	}

	return nil
}

// Remove 删除文件/目录
// 实现driver.Driver接口
func (p *Pan115) Remove(ctx context.Context, obj model.Obj) error {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return err
	}

	// 执行删除操作
	err := p.client.Delete(obj.GetID())
	if err != nil {
		return errors.Wrap(err, "删除文件失败")
	}

	return nil
}

// Put 上传文件
// 实现driver.Driver接口
func (p *Pan115) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	// 等待请求限制
	if err := p.WaitLimit(ctx); err != nil {
		return nil, err
	}

	var (
		fastInfo *driver115.UploadInitResp
		dirID    = dstDir.GetID()
	)

	// 检查上传是否可用
	ok, err := p.client.UploadAvailable()
	if err != nil || !ok {
		return nil, errors.Wrap(err, "上传服务不可用")
	}

	// 检查文件大小限制
	if stream.GetSize() > p.client.UploadMetaInfo.SizeLimit {
		return nil, errors.Wrap(driver115.ErrUploadTooLarge, "文件大小超过限制")
	}

	// 计算文件预哈希（前128KB）
	const PreHashSize int64 = 128 * utils.KB
	hashSize := PreHashSize
	if stream.GetSize() < PreHashSize {
		hashSize = stream.GetSize()
	}

	// 读取文件前部分用于计算预哈希
	reader, err := stream.RangeRead(http_range.Range{Start: 0, Length: hashSize})
	if err != nil {
		return nil, errors.Wrap(err, "读取文件失败")
	}

	// 计算预哈希
	preHash, err := utils.HashReader(utils.SHA1, reader)
	if err != nil {
		return nil, errors.Wrap(err, "计算预哈希失败")
	}
	preHash = strings.ToUpper(preHash)

	// 获取完整哈希
	fullHash := stream.GetHash().GetHash(utils.SHA1)
	if len(fullHash) <= 0 {
		// 如果没有完整哈希，计算一个
		_, fullHash, err = streamPkg.CacheFullInTempFileAndHash(stream, utils.SHA1)
		if err != nil {
			return nil, errors.Wrap(err, "计算完整哈希失败")
		}
	}
	fullHash = strings.ToUpper(fullHash)

	// 尝试秒传
	// 注意：115有秒传超时限制，超时后即使哈希正确也会返回"sig invalid"错误
	fastInfo, err = p.rapidUpload(stream.GetSize(), stream.GetName(), dirID, preHash, fullHash, stream)
	if err != nil {
		return nil, errors.Wrap(err, "秒传初始化失败")
	}

	// 检查秒传是否成功
	matched, err := fastInfo.Ok()
	if err != nil {
		return nil, errors.Wrap(err, "检查秒传结果失败")
	}

	if matched {
		// 秒传成功，获取文件信息
		uploadedFile, err := p.getNewFileByPickCode(fastInfo.PickCode)
		if err != nil {
			return nil, errors.Wrap(err, "获取秒传文件信息失败")
		}
		return uploadedFile, nil
	}

	var uploadResult *UploadResult

	// 秒传失败，根据文件大小选择上传方式
	if stream.GetSize() <= 10*utils.MB {
		// 小文件（<=10MB）使用普通上传
		uploadResult, err = p.UploadByOSS(ctx, &fastInfo.UploadOSSParams, stream, dirID, up)
		if err != nil {
			return nil, errors.Wrap(err, "普通上传失败")
		}
	} else {
		// 大文件使用分片上传
		uploadResult, err = p.UploadByMultipart(ctx, &fastInfo.UploadOSSParams, stream.GetSize(), stream, dirID, up)
		if err != nil {
			return nil, errors.Wrap(err, "分片上传失败")
		}
	}

	// 获取上传后的文件信息
	uploadedFile, err := p.getNewFile(uploadResult.Data.FileID)
	if err != nil {
		return nil, errors.Wrap(err, "获取上传文件信息失败")
	}

	return uploadedFile, nil
}

// OfflineList 获取离线下载任务列表
// 参数:
//   - ctx: 上下文
//
// 返回:
//   - []*driver115.OfflineTask: 离线下载任务列表
//   - error: 错误信息
func (p *Pan115) OfflineList(ctx context.Context) ([]*driver115.OfflineTask, error) {
	resp, err := p.client.ListOfflineTask(0)
	if err != nil {
		return nil, errors.Wrap(err, "获取离线下载任务列表失败")
	}
	return resp.Tasks, nil
}

// OfflineDownload 添加离线下载任务
// 参数:
//   - ctx: 上下文
//   - uris: 下载链接列表
//   - dstDir: 目标目录
//
// 返回:
//   - []string: 成功添加的任务ID列表
//   - error: 错误信息
func (p *Pan115) OfflineDownload(ctx context.Context, uris []string, dstDir model.Obj) ([]string, error) {
	taskIDs, err := p.client.AddOfflineTaskURIs(uris, dstDir.GetID(), driver115.WithAppVer(appVer))
	if err != nil {
		return nil, errors.Wrap(err, "添加离线下载任务失败")
	}
	return taskIDs, nil
}

// DeleteOfflineTasks 删除离线下载任务
// 参数:
//   - ctx: 上下文
//   - hashes: 任务哈希列表
//   - deleteFiles: 是否同时删除已下载的文件
//
// 返回:
//   - error: 错误信息
func (p *Pan115) DeleteOfflineTasks(ctx context.Context, hashes []string, deleteFiles bool) error {
	err := p.client.DeleteOfflineTasks(hashes, deleteFiles)
	if err != nil {
		return errors.Wrap(err, "删除离线下载任务失败")
	}
	return nil
}

// 确保Pan115实现了driver.Driver接口
var _ driver.Driver = (*Pan115)(nil)
