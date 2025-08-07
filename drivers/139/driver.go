package _139

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	streamPkg "github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

// Yun139 实现 139 云盘驱动的主结构体，包含所有云盘操作方法
// 继承 model.Storage，包含 Addition 配置、定时器、账号等信息
// 该结构体是驱动的核心，负责与 139 云盘 API 交互和管理文件操作
type Yun139 struct {
	model.Storage
	Addition
	cronEntryId       cron.EntryID // 定时器，用于定时刷新 token
	Account           string       // 账号，存储用户账号信息
	ref               *Yun139      // 引用存储，用于多实例共享 token
	PersonalCloudHost string       // 个人云主机地址，从路由策略中获取
}

// Config 返回驱动的元配置信息
// 该方法返回 139 云盘驱动的基本配置，如名称、是否支持本地排序等
// 返回值: driver.Config 结构体，包含驱动的元信息
func (d *Yun139) Config() driver.Config {
	return config
}

// GetAddition 返回驱动的附加配置项
// 该方法返回 139 云盘驱动的附加配置，如授权信息、云盘类型等
// 返回值: driver.Additional 接口，指向 Addition 结构体
func (d *Yun139) GetAddition() driver.Additional {
	return &d.Addition
}

// Init 初始化驱动，包括 token 刷新、路由策略查询、定时任务等
// 该方法在驱动启动时调用，用于设置驱动的初始状态
// 参数 ctx: 上下文，用于控制初始化过程
// 返回值: 初始化成功返回 nil，否则返回错误
func (d *Yun139) Init(ctx context.Context) error {
	if d.ref == nil {
		if len(d.Authorization) == 0 {
			return errors.Errorf("authorization is empty")
		}
		err := d.refreshToken()
		if err != nil {
			return err
		}

		// Query Route Policy
		var resp QueryRoutePolicyResp
		_, err = d.requestRoute(base.Json{
			"userInfo": base.Json{
				"userType":    1,
				"accountType": 1,
				"accountName": d.Account},
			"modAddrType": 1,
		}, &resp)
		if err != nil {
			return err
		}
		for _, policyItem := range resp.Data.RoutePolicyList {
			if policyItem.ModName == "personal" {
				d.PersonalCloudHost = policyItem.HTTPSURL
				break
			}
		}
		if len(d.PersonalCloudHost) == 0 {
			return errors.Errorf("PersonalCloudHost is empty")
		}

		d.cronEntryId, err = global.CronConfig.AddFunc("0 */12 * * *", func() {
			err := d.refreshToken()
			if err != nil {
				log.Errorf("%+v", err)
			}
		})
	}
	switch d.Addition.Type {
	case MetaPersonalNew:
		if len(d.Addition.RootFolderID) == 0 {
			d.RootFolderID = "/"
		}
	case MetaPersonal:
		if len(d.Addition.RootFolderID) == 0 {
			d.RootFolderID = "root"
		}
	case MetaGroup:
		if len(d.Addition.RootFolderID) == 0 {
			d.RootFolderID = d.CloudID
		}
	case MetaFamily:
	default:
		return errs.NotImplement
	}
	return nil
}

// InitReference 设置引用存储，用于多实例共享 token
// 该方法用于设置一个引用驱动实例，以便多个驱动实例共享同一个 token
// 参数 storage: 驱动实例，需要是 Yun139 类型
// 返回值: 设置成功返回 nil，否则返回错误
func (d *Yun139) InitReference(storage driver.Driver) error {
	refStorage, ok := storage.(*Yun139)
	if ok {
		d.ref = refStorage
		return nil
	}
	return errs.NotSupport
}

// Drop 停止定时任务，清理引用
// 该方法在驱动卸载时调用，用于清理资源和停止定时任务
// 参数 ctx: 上下文，用于控制清理过程
// 返回值: 清理成功返回 nil，否则返回错误
func (d *Yun139) Drop(ctx context.Context) error {
	if d.cronEntryId > 0 {
		global.CronConfig.Remove(d.cronEntryId)
		d.cronEntryId = 0
	}
	d.ref = nil
	return nil
}

// List 列出指定目录下的所有文件和文件夹，自动根据类型分流
// 该方法用于获取指定目录的内容列表，根据云盘类型调用不同的实现
// 参数 ctx: 上下文，dir: 目录对象，args: 列表参数
// 返回值: 文件和文件夹的对象列表，错误信息
func (d *Yun139) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	switch d.Addition.Type {
	case MetaPersonalNew:
		return d.personalGetFiles(dir.GetID())
	case MetaPersonal:
		return d.getFiles(dir.GetID())
	case MetaFamily:
		return d.familyGetFiles(dir.GetID())
	case MetaGroup:
		return d.groupGetFiles(dir.GetID())
	default:
		return nil, errs.NotImplement
	}
}

// Link 获取文件的下载直链，自动根据类型分流
// 该方法用于获取指定文件的下载链接，根据云盘类型调用不同的实现
// 参数 ctx: 上下文，file: 文件对象，args: 链接参数
// 返回值: 下载链接对象，错误信息
func (d *Yun139) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	var url string
	var err error
	switch d.Addition.Type {
	case MetaPersonalNew:
		url, err = d.personalGetLink(file.GetID())
	case MetaPersonal:
		url, err = d.getLink(file.GetID())
	case MetaFamily:
		url, err = d.familyGetLink(file.GetID(), file.GetPath())
	case MetaGroup:
		url, err = d.groupGetLink(file.GetID(), file.GetPath())
	default:
		return nil, errs.NotImplement
	}
	if err != nil {
		return nil, err
	}
	return &model.Link{URL: url}, nil
}

// MakeDir 在指定目录下创建新文件夹，自动根据类型分流
// 该方法用于在指定目录下创建新文件夹，根据云盘类型调用不同的实现
// 参数 ctx: 上下文，parentDir: 父目录对象，dirName: 新文件夹名称
// 返回值: 创建成功返回 nil，否则返回错误
func (d *Yun139) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	var err error
	switch d.Addition.Type {
	case MetaPersonalNew:
		data := base.Json{
			"parentFileId":   parentDir.GetID(),
			"name":           dirName,
			"description":    "",
			"type":           "folder",
			"fileRenameMode": "force_rename",
		}
		pathname := "/file/create"
		_, err = d.personalPost(pathname, data, nil)
	case MetaPersonal:
		data := base.Json{
			"createCatalogExtReq": base.Json{
				"parentCatalogID": parentDir.GetID(),
				"newCatalogName":  dirName,
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			},
		}
		pathname := "/orchestration/personalCloud/catalog/v1.0/createCatalogExt"
		_, err = d.post(pathname, data, nil)
	case MetaFamily:
		data := base.Json{
			"cloudID": d.CloudID,
			"commonAccountInfo": base.Json{
				"account":     d.getAccount(),
				"accountType": 1,
			},
			"docLibName": dirName,
			"path":       path.Join(parentDir.GetPath(), parentDir.GetID()),
		}
		pathname := "/orchestration/familyCloud-rebuild/cloudCatalog/v1.0/createCloudDoc"
		_, err = d.post(pathname, data, nil)
	case MetaGroup:
		data := base.Json{
			"catalogName": dirName,
			"commonAccountInfo": base.Json{
				"account":     d.getAccount(),
				"accountType": 1,
			},
			"groupID":      d.CloudID,
			"parentFileId": parentDir.GetID(),
			"path":         path.Join(parentDir.GetPath(), parentDir.GetID()),
		}
		pathname := "/orchestration/group-rebuild/catalog/v1.0/createGroupCatalog"
		_, err = d.post(pathname, data, nil)
	default:
		err = errs.NotImplement
	}
	return err
}

// Move 移动文件或文件夹到目标目录，自动根据类型分流
// 该方法用于将源对象移动到目标目录，根据云盘类型调用不同的实现
// 参数 ctx: 上下文，srcObj: 源对象，dstDir: 目标目录
// 返回值: 移动后的对象，错误信息
func (d *Yun139) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	switch d.Addition.Type {
	case MetaPersonalNew:
		data := base.Json{
			"fileIds":        []string{srcObj.GetID()},
			"toParentFileId": dstDir.GetID(),
		}
		pathname := "/file/batchMove"
		_, err := d.personalPost(pathname, data, nil)
		if err != nil {
			return nil, err
		}
		return srcObj, nil
	case MetaGroup:
		var contentList []string
		var catalogList []string
		if srcObj.IsDir() {
			catalogList = append(catalogList, srcObj.GetID())
		} else {
			contentList = append(contentList, srcObj.GetID())
		}
		data := base.Json{
			"taskType":    3,
			"srcType":     2,
			"srcGroupID":  d.CloudID,
			"destType":    2,
			"destGroupID": d.CloudID,
			"destPath":    dstDir.GetPath(),
			"contentList": contentList,
			"catalogList": catalogList,
			"commonAccountInfo": base.Json{
				"account":     d.getAccount(),
				"accountType": 1,
			},
		}
		pathname := "/orchestration/group-rebuild/task/v1.0/createBatchOprTask"
		_, err := d.post(pathname, data, nil)
		if err != nil {
			return nil, err
		}
		return srcObj, nil
	case MetaPersonal:
		var contentInfoList []string
		var catalogInfoList []string
		if srcObj.IsDir() {
			catalogInfoList = append(catalogInfoList, srcObj.GetID())
		} else {
			contentInfoList = append(contentInfoList, srcObj.GetID())
		}
		data := base.Json{
			"createBatchOprTaskReq": base.Json{
				"taskType":   3,
				"actionType": "304",
				"taskInfo": base.Json{
					"contentInfoList": contentInfoList,
					"catalogInfoList": catalogInfoList,
					"newCatalogID":    dstDir.GetID(),
				},
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			},
		}
		pathname := "/orchestration/personalCloud/batchOprTask/v1.0/createBatchOprTask"
		_, err := d.post(pathname, data, nil)
		if err != nil {
			return nil, err
		}
		return srcObj, nil
	default:
		return nil, errs.NotImplement
	}
}

// Rename 重命名文件或文件夹，自动根据类型分流
// 该方法用于重命名文件或文件夹，根据云盘类型调用不同的实现
// 参数 ctx: 上下文，srcObj: 源对象，newName: 新名称
// 返回值: 重命名成功返回 nil，否则返回错误
func (d *Yun139) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	var err error
	switch d.Addition.Type {
	case MetaPersonalNew:
		data := base.Json{
			"fileId":      srcObj.GetID(),
			"name":        newName,
			"description": "",
		}
		pathname := "/file/update"
		_, err = d.personalPost(pathname, data, nil)
	case MetaPersonal:
		var data base.Json
		var pathname string
		if srcObj.IsDir() {
			data = base.Json{
				"catalogID":   srcObj.GetID(),
				"catalogName": newName,
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			}
			pathname = "/orchestration/personalCloud/catalog/v1.0/updateCatalogInfo"
		} else {
			data = base.Json{
				"contentID":   srcObj.GetID(),
				"contentName": newName,
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			}
			pathname = "/orchestration/personalCloud/content/v1.0/updateContentInfo"
		}
		_, err = d.post(pathname, data, nil)
	case MetaGroup:
		var data base.Json
		var pathname string
		if srcObj.IsDir() {
			data = base.Json{
				"groupID":           d.CloudID,
				"modifyCatalogID":   srcObj.GetID(),
				"modifyCatalogName": newName,
				"path":              srcObj.GetPath(),
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			}
			pathname = "/orchestration/group-rebuild/catalog/v1.0/modifyGroupCatalog"
		} else {
			data = base.Json{
				"groupID":     d.CloudID,
				"contentID":   srcObj.GetID(),
				"contentName": newName,
				"path":        srcObj.GetPath(),
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			}
			pathname = "/orchestration/group-rebuild/content/v1.0/modifyGroupContent"
		}
		_, err = d.post(pathname, data, nil)
	case MetaFamily:
		var data base.Json
		var pathname string
		if srcObj.IsDir() {
			// 网页接口不支持重命名家庭云文件夹
			// data = base.Json{
			// 	"catalogType": 3,
			// 	"catalogID":   srcObj.GetID(),
			// 	"catalogName": newName,
			// 	"commonAccountInfo": base.Json{
			// 		"account":     d.getAccount(),
			// 		"accountType": 1,
			// 	},
			// 	"path": srcObj.GetPath(),
			// }
			// pathname = "/orchestration/familyCloud-rebuild/photoContent/v1.0/modifyCatalogInfo"
			return errs.NotImplement
		} else {
			data = base.Json{
				"contentID":   srcObj.GetID(),
				"contentName": newName,
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
				"path": srcObj.GetPath(),
			}
			pathname = "/orchestration/familyCloud-rebuild/photoContent/v1.0/modifyContentInfo"
		}
		_, err = d.post(pathname, data, nil)
	default:
		err = errs.NotImplement
	}
	return err
}

// Copy 复制文件或文件夹到目标目录，自动根据类型分流
// 该方法用于将源对象复制到目标目录，根据云盘类型调用不同的实现
// 参数 ctx: 上下文，srcObj: 源对象，dstDir: 目标目录
// 返回值: 复制成功返回 nil，否则返回错误
func (d *Yun139) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	var err error
	switch d.Addition.Type {
	case MetaPersonalNew:
		data := base.Json{
			"fileIds":        []string{srcObj.GetID()},
			"toParentFileId": dstDir.GetID(),
		}
		pathname := "/file/batchCopy"
		_, err := d.personalPost(pathname, data, nil)
		return err
	case MetaPersonal:
		var contentInfoList []string
		var catalogInfoList []string
		if srcObj.IsDir() {
			catalogInfoList = append(catalogInfoList, srcObj.GetID())
		} else {
			contentInfoList = append(contentInfoList, srcObj.GetID())
		}
		data := base.Json{
			"createBatchOprTaskReq": base.Json{
				"taskType":   3,
				"actionType": 309,
				"taskInfo": base.Json{
					"contentInfoList": contentInfoList,
					"catalogInfoList": catalogInfoList,
					"newCatalogID":    dstDir.GetID(),
				},
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			},
		}
		pathname := "/orchestration/personalCloud/batchOprTask/v1.0/createBatchOprTask"
		_, err = d.post(pathname, data, nil)
	default:
		err = errs.NotImplement
	}
	return err
}

// Remove 删除文件或文件夹，自动根据类型分流
// 该方法用于删除文件或文件夹，根据云盘类型调用不同的实现
// 参数 ctx: 上下文，obj: 要删除的对象
// 返回值: 删除成功返回 nil，否则返回错误
func (d *Yun139) Remove(ctx context.Context, obj model.Obj) error {
	switch d.Addition.Type {
	case MetaPersonalNew:
		data := base.Json{
			"fileIds": []string{obj.GetID()},
		}
		pathname := "/recyclebin/batchTrash"
		_, err := d.personalPost(pathname, data, nil)
		return err
	case MetaGroup:
		var contentList []string
		var catalogList []string
		// 必须使用完整路径删除
		if obj.IsDir() {
			catalogList = append(catalogList, obj.GetPath())
		} else {
			contentList = append(contentList, path.Join(obj.GetPath(), obj.GetID()))
		}
		data := base.Json{
			"taskType":    2,
			"srcGroupID":  d.CloudID,
			"contentList": contentList,
			"catalogList": catalogList,
			"commonAccountInfo": base.Json{
				"account":     d.getAccount(),
				"accountType": 1,
			},
		}
		pathname := "/orchestration/group-rebuild/task/v1.0/createBatchOprTask"
		_, err := d.post(pathname, data, nil)
		return err
	case MetaPersonal:
		fallthrough
	case MetaFamily:
		var contentInfoList []string
		var catalogInfoList []string
		if obj.IsDir() {
			catalogInfoList = append(catalogInfoList, obj.GetID())
		} else {
			contentInfoList = append(contentInfoList, obj.GetID())
		}
		data := base.Json{
			"createBatchOprTaskReq": base.Json{
				"taskType":   2,
				"actionType": 201,
				"taskInfo": base.Json{
					"newCatalogID":    "",
					"contentInfoList": contentInfoList,
					"catalogInfoList": catalogInfoList,
				},
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
			},
		}
		pathname := "/orchestration/personalCloud/batchOprTask/v1.0/createBatchOprTask"
		if d.isFamily() {
			data = base.Json{
				"catalogList": catalogInfoList,
				"contentList": contentInfoList,
				"commonAccountInfo": base.Json{
					"account":     d.getAccount(),
					"accountType": 1,
				},
				"sourceCloudID":     d.CloudID,
				"sourceCatalogType": 1002,
				"taskType":          2,
				"path":              obj.GetPath(),
			}
			pathname = "/orchestration/familyCloud-rebuild/batchOprTask/v1.0/createBatchOprTask"
		}
		_, err := d.post(pathname, data, nil)
		return err
	default:
		return errs.NotImplement
	}
}

// getPartSize 计算分片上传的分片大小，支持自定义
// 该方法根据文件大小和配置计算合适的分片大小
// 参数 size: 文件大小，单位为字节
// 返回值: 计算得到的分片大小，单位为字节
func (d *Yun139) getPartSize(size int64) int64 {
	if d.CustomUploadPartSize != 0 {
		return d.CustomUploadPartSize
	}
	// 网盘对于分片数量存在上限
	if size/utils.GB > 30 {
		return 512 * utils.MB
	}
	return 100 * utils.MB
}

// Put 上传文件到目标目录，自动处理分片、冲突、进度等
// 该方法用于将文件上传到指定目录，支持分片上传、断点续传、冲突处理等功能
// 根据云盘类型调用不同的实现，支持自定义分片大小和进度回调
// 参数:
//   - ctx: 上下文，用于控制上传过程和取消操作
//   - dstDir: 目标目录对象，指定上传到哪个目录
//   - stream: 文件流，包含文件内容、名称、大小等信息
//   - up: 进度回调函数，用于报告上传进度
//
// 返回值: 上传成功返回 nil，否则返回错误
func (d *Yun139) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	switch d.Addition.Type {
	case MetaPersonalNew:
		var err error
		fullHash := stream.GetHash().GetHash(utils.SHA256)
		if len(fullHash) != utils.SHA256.Width {
			cacheFileProgress := model.UpdateProgressWithRange(up, 0, 50)
			up = model.UpdateProgressWithRange(up, 50, 100)
			_, fullHash, err = streamPkg.CacheFullInTempFileAndHash(stream, cacheFileProgress, utils.SHA256)
			if err != nil {
				return err
			}
		}

		size := stream.GetSize()
		partSize := d.getPartSize(size)
		part := int64(1)
		if size > partSize {
			part = (size + partSize - 1) / partSize
		}
		partInfos := make([]PartInfo, 0, part)
		for i := int64(0); i < part; i++ {
			if utils.IsCanceled(ctx) {
				return ctx.Err()
			}
			start := i * partSize
			byteSize := size - start
			if byteSize > partSize {
				byteSize = partSize
			}
			partNumber := i + 1
			partInfo := PartInfo{
				PartNumber: partNumber,
				PartSize:   byteSize,
				ParallelHashCtx: ParallelHashCtx{
					PartOffset: start,
				},
			}
			partInfos = append(partInfos, partInfo)
		}

		// 筛选出前 100 个 partInfos
		firstPartInfos := partInfos
		if len(firstPartInfos) > 100 {
			firstPartInfos = firstPartInfos[:100]
		}

		// 创建任务，获取上传信息和前100个分片的上传地址
		data := base.Json{
			"contentHash":          fullHash,
			"contentHashAlgorithm": "SHA256",
			"contentType":          "application/octet-stream",
			"parallelUpload":       false,
			"partInfos":            firstPartInfos,
			"size":                 size,
			"parentFileId":         dstDir.GetID(),
			"name":                 stream.GetName(),
			"type":                 "file",
			"fileRenameMode":       "auto_rename",
		}
		pathname := "/file/create"
		var resp PersonalUploadResp
		_, err = d.personalPost(pathname, data, &resp)
		if err != nil {
			return err
		}

		// 判断文件是否已存在
		// resp.Data.Exist: true 已存在同名文件且校验相同，云端不会重复增加文件，无需手动处理冲突
		if resp.Data.Exist {
			return nil
		}

		// 判断文件是否支持快传
		// resp.Data.RapidUpload: true 支持快传，但此处直接检测是否返回分片的上传地址
		// 快传的情况下同样需要手动处理冲突
		if resp.Data.PartInfos != nil {
			// 读取前100个分片的上传地址
			uploadPartInfos := resp.Data.PartInfos

			// 获取后续分片的上传地址
			for i := 101; i < len(partInfos); i += 100 {
				end := i + 100
				if end > len(partInfos) {
					end = len(partInfos)
				}
				batchPartInfos := partInfos[i:end]

				moreData := base.Json{
					"fileId":    resp.Data.FileID,
					"uploadId":  resp.Data.UploadID,
					"partInfos": batchPartInfos,
					"commonAccountInfo": base.Json{
						"account":     d.getAccount(),
						"accountType": 1,
					},
				}
				var moreResp PersonalUploadURLResp
				_, err = d.personalPost("/file/getUploadUrl", moreData, &moreResp)
				if err != nil {
					return err
				}
				uploadPartInfos = append(uploadPartInfos, moreResp.Data.PartInfos...)
			}

			// 创建进度跟踪器
			p := driver.NewProgress(size, up)

			// 创建限速上传流
			rateLimited := driver.NewLimitedUploadStream(ctx, stream)

			var response *resty.Response
			// 上传所有分片
			for _, uploadPartInfo := range uploadPartInfos {
				index := uploadPartInfo.PartNumber - 1
				partSizeTmp := partInfos[index].PartSize
				log.Debugf("[139] uploading part %+v/%+v", index, len(uploadPartInfos))
				limitReader := io.LimitReader(rateLimited, partSizeTmp)

				// 更新进度
				r := io.TeeReader(limitReader, p)
				var result InterLayerUploadResult
				response, err = base.RestyClient.R().
					WithContext(ctx).
					SetBody(r).
					SetHeader("Content-Type", "application/octet-stream").
					SetContentLength(true).
					SetHeader("contentSize", strconv.FormatInt(size, 10)).
					SetHeader("Origin", "https://yun.139.com").
					SetHeader("Referer", "https://yun.139.com/").
					SetResult(&result).
					Post(uploadPartInfo.UploadURL)
				if err != nil {
					return err
				}
				log.Debugf("[139] uploaded: %s", response.String())
				if response.StatusCode() != http.StatusOK {
					return errors.Errorf("unexpected status code: %d", response.StatusCode())
				}
			}

			// 完成上传
			data = base.Json{
				"contentHash":          fullHash,
				"contentHashAlgorithm": "SHA256",
				"fileId":               resp.Data.FileID,
				"uploadId":             resp.Data.UploadID,
			}
			_, err = d.personalPost("/file/complete", data, nil)
			if err != nil {
				return err
			}
		}

		// 处理冲突
		if resp.Data.FileName != stream.GetName() {
			log.Debugf("[139] conflict detected: %s != %s", resp.Data.FileName, stream.GetName())
			// 给服务器一定时间处理数据，避免无法刷新文件列表
			time.Sleep(time.Millisecond * 500)
			// 刷新并获取文件列表
			files, err := d.List(ctx, dstDir, model.ListArgs{Refresh: true})
			if err != nil {
				return err
			}
			// 删除旧文件
			for _, file := range files {
				if file.GetName() != stream.GetName() {
					continue
				}
				log.Debugf("[139] conflict: removing old: %s", file.GetName())
				// 删除前重命名旧文件，避免仍旧冲突
				err = d.Rename(ctx, file, stream.GetName()+random.String(4))
				if err != nil {
					return err
				}
				err = d.Remove(ctx, file)
				if err != nil {
					return err
				}
				break
			}
			// 重命名新文件
			for _, file := range files {
				if file.GetName() == resp.Data.FileName {
					log.Debugf("[139] conflict: renaming new: %s => %s", file.GetName(), stream.GetName())
					err = d.Rename(ctx, file, stream.GetName())
					if err != nil {
						return err
					}
					break
				}
			}
		}
		return nil
	case MetaPersonal:
		fallthrough
	case MetaFamily:
		// 处理冲突
		// 获取文件列表
		files, err := d.List(ctx, dstDir, model.ListArgs{})
		if err != nil {
			return err
		}
		// 删除旧文件
		for _, file := range files {
			if file.GetName() == stream.GetName() {
				log.Debugf("[139] conflict: removing old: %s", file.GetName())
				// 删除前重命名旧文件，避免仍旧冲突
				err = d.Rename(ctx, file, stream.GetName()+random.String(4))
				if err != nil {
					return err
				}
				err = d.Remove(ctx, file)
				if err != nil {
					return err
				}
				break
			}
		}
		var reportSize int64
		if d.ReportRealSize {
			reportSize = stream.GetSize()
		} else {
			reportSize = 0
		}
		data := base.Json{
			"manualRename": 2,
			"operation":    0,
			"fileCount":    1,
			"totalSize":    reportSize,
			"uploadContentList": []base.Json{{
				"contentName": stream.GetName(),
				"contentSize": reportSize,
				// "digest": "5a3231986ce7a6b46e408612d385bafa"
			}},
			"parentCatalogID": dstDir.GetID(),
			"newCatalogName":  "",
			"commonAccountInfo": base.Json{
				"account":     d.getAccount(),
				"accountType": 1,
			},
		}
		pathname := "/orchestration/personalCloud/uploadAndDownload/v1.0/pcUploadFileRequest"
		if d.isFamily() {
			data = d.newJSON(base.Json{
				"fileCount":    1,
				"manualRename": 2,
				"operation":    0,
				"path":         path.Join(dstDir.GetPath(), dstDir.GetID()),
				"seqNo":        random.String(32), // 序列号不能为空
				"totalSize":    reportSize,
				"uploadContentList": []base.Json{{
					"contentName": stream.GetName(),
					"contentSize": reportSize,
					// "digest": "5a3231986ce7a6b46e408612d385bafa"
				}},
			})
			pathname = "/orchestration/familyCloud-rebuild/content/v1.0/getFileUploadURL"
		}
		var resp UploadResp
		_, err = d.post(pathname, data, &resp)
		if err != nil {
			return err
		}
		if resp.Data.Result.ResultCode != "0" {
			return errors.Errorf("get file upload url failed with result code: %s, message: %s", resp.Data.Result.ResultCode, resp.Data.Result.ResultDesc)
		}

		size := stream.GetSize()
		// Progress
		p := driver.NewProgress(size, up)
		partSize := d.getPartSize(size)
		part := int64(1)
		if size > partSize {
			part = (size + partSize - 1) / partSize
		}
		rateLimited := driver.NewLimitedUploadStream(ctx, stream)
		var response *resty.Response
		for i := int64(0); i < part; i++ {
			if utils.IsCanceled(ctx) {
				return ctx.Err()
			}

			start := i * partSize
			byteSize := min(size-start, partSize)

			limitReader := io.LimitReader(rateLimited, byteSize)
			// Update Progress
			r := io.TeeReader(limitReader, p)

			var result InterLayerUploadResult
			response, err = base.RestyClient.R().
				WithContext(ctx).
				SetBody(r).
				SetHeader("Content-Type", "text/plain;name="+unicode(stream.GetName())).
				SetHeader("contentSize", strconv.FormatInt(size, 10)).
				SetHeader("range", fmt.Sprintf("bytes=%d-%d", start, start+byteSize-1)).
				SetHeader("uploadtaskID", resp.Data.UploadResult.UploadTaskID).
				SetHeader("rangeType", "0").
				SetContentLength(true).
				SetResult(&result).
				Post(resp.Data.UploadResult.RedirectionURL)
			if err != nil {
				return err
			}
			if response.StatusCode() != http.StatusOK {
				return errors.Errorf("unexpected status code: %d", response.StatusCode())
			}
			if result.ResultCode != 0 {
				return errors.Errorf("upload failed with result code: %d, message: %s", result.ResultCode, result.Msg)
			} else {
				log.Debugf("[139] uploaded: %+v", result)
			}
		}
		return nil
	default:
		return errs.NotImplement
	}
}

// Other 扩展接口，支持视频预览等特殊操作
func (d *Yun139) Other(ctx context.Context, args model.OtherArgs) (any, error) {
	switch d.Addition.Type {
	case MetaPersonalNew:
		var resp base.Json
		var uri string
		data := base.Json{
			"category": "video",
			"fileId":   args.Obj.GetID(),
		}
		switch args.Method {
		case "video_preview":
			uri = "/videoPreview/getPreviewInfo"
		default:
			return nil, errs.NotSupport
		}
		_, err := d.personalPost(uri, data, &resp)
		if err != nil {
			return nil, err
		}
		return resp["data"], nil
	default:
		return nil, errs.NotImplement
	}
}

var _ driver.Driver = (*Yun139)(nil)