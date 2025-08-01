package _189pc

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Cloud189PC 天翼云盘PC客户端驱动实现
type Cloud189PC struct {
	model.Storage
	Addition

	identity string

	client *resty.Client

	loginParam *LoginParam
	tokenInfo  *AppSessionResp

	uploadThread int

	familyTransferFolder    *Cloud189Folder
	cleanFamilyTransferFile func()

	storageConfig driver.Config
	ref           *Cloud189PC
}

// Config 获取驱动配置
// 返回值: 驱动配置信息
func (y *Cloud189PC) Config() driver.Config {
	if y.storageConfig.Name == "" {
		y.storageConfig = config
	}
	return y.storageConfig
}

// GetAddition 获取附加配置
// 返回值: 驱动附加配置
func (y *Cloud189PC) GetAddition() driver.Additional {
	return &y.Addition
}

// Init 初始化驱动
// 初始化客户端、登录状态、家庭云ID等
// 参数:
//   - ctx: 上下文
//
// 返回值: 错误信息
func (y *Cloud189PC) Init(ctx context.Context) (err error) {
	y.storageConfig = config
	if y.isFamily() {
		// 兼容旧上传接口
		if y.Addition.RapidUpload || y.Addition.UploadMethod == "old" {
			y.storageConfig.NoOverwriteUpload = true
		}
	} else {
		// 家庭云转存，不支持覆盖上传
		if y.Addition.FamilyTransfer {
			y.storageConfig.NoOverwriteUpload = true
		}
	}
	// 处理个人云和家庭云参数
	if y.isFamily() && y.RootFolderID == "-11" {
		y.RootFolderID = ""
	}
	if !y.isFamily() && y.RootFolderID == "" {
		y.RootFolderID = "-11"
	}

	// 限制上传线程数
	y.uploadThread, _ = strconv.Atoi(y.UploadThread)
	if y.uploadThread < 1 || y.uploadThread > 32 {
		y.uploadThread, y.UploadThread = 3, "3"
	}

	if y.ref == nil {
		// 初始化请求客户端
		if y.client == nil {
			y.client = base.NewRestyClient().SetHeaders(map[string]string{
				"Accept":  "application/json;charset=UTF-8",
				"Referer": _webURL,
			})
		}

		// 避免重复登录
		identity := utils.GetMD5EncodeStr(y.Username + y.Password)
		if !y.isLogin() || y.identity != identity {
			y.identity = identity
			if err = y.login(); err != nil {
				return
			}
		}
	}

	// 处理家庭云ID
	if y.FamilyID == "" {
		if y.FamilyID, err = y.getFamilyID(); err != nil {
			return err
		}
	}

	// 创建中转文件夹
	if y.FamilyTransfer {
		if err := y.createFamilyTransferFolder(); err != nil {
			return err
		}
	}

	// 清理转存文件节流
	y.cleanFamilyTransferFile = utils.NewThrottle2(time.Minute, func() {
		if err := y.cleanFamilyTransfer(context.TODO()); err != nil {
			utils.Log.Errorf("cleanFamilyTransferFolderError:%s", err)
		}
	})
	return
}

// InitReference 初始化引用
// 用于在多个存储实例之间共享登录状态
// 参数:
//   - storage: 要引用的驱动实例
//
// 返回值: 错误信息
func (y *Cloud189PC) InitReference(storage driver.Driver) error {
	refStorage, ok := storage.(*Cloud189PC)
	if ok {
		y.ref = refStorage
		return nil
	}
	return errs.NotSupport
}

// Drop 清除引用
// 参数:
//   - ctx: 上下文
//
// 返回值: 错误信息
func (y *Cloud189PC) Drop(ctx context.Context) error {
	y.ref = nil
	return nil
}

// List 列出目录内容
// 获取指定目录下的所有文件和文件夹
// 参数:
//   - ctx: 上下文
//   - dir: 目录对象
//   - args: 列表参数
//
// 返回值: 文件和文件夹对象列表，错误信息
func (y *Cloud189PC) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	return y.getFiles(ctx, dir.GetID(), y.isFamily())
}

// Link 获取文件下载链接
// 参数:
//   - ctx: 上下文
//   - file: 文件对象
//   - args: 链接参数
//
// 返回值: 下载链接，错误信息
func (y *Cloud189PC) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	var downloadURL struct {
		URL string `json:"fileDownloadUrl"`
	}

	isFamily := y.isFamily()
	fullURL := _apiURL
	if isFamily {
		fullURL += "/family/file"
	}
	fullURL += "/getFileDownloadUrl.action"

	_, err := y.get(fullURL, func(r *resty.Request) {
		r.SetContext(ctx)
		r.SetQueryParam("fileId", file.GetID())
		if isFamily {
			r.SetQueryParams(map[string]string{
				"familyId": y.FamilyID,
			})
		} else {
			r.SetQueryParams(map[string]string{
				"dt":   "3",
				"flag": "1",
			})
		}
	}, &downloadURL, isFamily)
	if err != nil {
		return nil, err
	}

	// 重定向获取真实链接
	downloadURL.URL = strings.Replace(strings.ReplaceAll(downloadURL.URL, "&amp;", "&"), "http://", "https://", 1)
	res, err := base.NoRedirectClient.R().SetContext(ctx).
		SetDoNotParseResponse(true).
		Get(downloadURL.URL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode() == 302 {
		downloadURL.URL = res.Header().Get("location")
	}

	link := &model.Link{
		URL: downloadURL.URL,
		Header: http.Header{
			"User-Agent": []string{consts.ChromeUserAgent},
		},
	}
	/*
		// 获取链接有效时常
		strs := regexp.MustCompile(`(?i)expire[^=]*=([0-9]*)`).FindStringSubmatch(downloadURL.URL)
		if len(strs) == 2 {
			timestamp, err := strconv.ParseInt(strs[1], 10, 64)
			if err == nil {
				expired := time.Duration(timestamp-time.Now().Unix()) * time.Second
				like.Expiration = &expired
			}
		}
	*/
	return link, nil
}

// MakeDir 创建目录
// 在指定目录下创建新的文件夹
// 参数:
//   - ctx: 上下文
//   - parentDir: 父目录对象
//   - dirName: 新目录名称
//
// 返回值: 新创建的目录对象，错误信息
func (y *Cloud189PC) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	isFamily := y.isFamily()
	fullURL := _apiURL
	if isFamily {
		fullURL += "/family/file"
	}
	fullURL += "/createFolder.action"

	var newFolder Cloud189Folder
	_, err := y.post(fullURL, func(req *resty.Request) {
		req.SetContext(ctx)
		req.SetQueryParams(map[string]string{
			"folderName":   dirName,
			"relativePath": "",
		})
		if isFamily {
			req.SetQueryParams(map[string]string{
				"familyId": y.FamilyID,
				"parentId": parentDir.GetID(),
			})
		} else {
			req.SetQueryParams(map[string]string{
				"parentFolderId": parentDir.GetID(),
			})
		}
	}, &newFolder, isFamily)
	if err != nil {
		return nil, err
	}
	return &newFolder, nil
}

// Move 移动文件或文件夹
// 将文件或文件夹移动到新的目录
// 参数:
//   - ctx: 上下文
//   - srcObj: 源文件或文件夹对象
//   - dstDir: 目标目录对象
//
// 返回值: 移动后的对象，错误信息
func (y *Cloud189PC) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	isFamily := y.isFamily()
	other := map[string]string{"targetFileName": dstDir.GetName()}

	resp, err := y.CreateBatchTask("MOVE", IF(isFamily, y.FamilyID, ""), dstDir.GetID(), other, BatchTaskInfo{
		FileID:   srcObj.GetID(),
		FileName: srcObj.GetName(),
		IsFolder: BoolToNumber(srcObj.IsDir()),
	})
	if err != nil {
		return nil, err
	}
	if err = y.WaitBatchTask("MOVE", resp.TaskID, time.Millisecond*400); err != nil {
		return nil, err
	}
	return srcObj, nil
}

// Rename 重命名文件或文件夹
// 参数:
//   - ctx: 上下文
//   - srcObj: 要重命名的文件或文件夹对象
//   - newName: 新名称
//
// 返回值: 重命名后的对象，错误信息
func (y *Cloud189PC) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	isFamily := y.isFamily()
	queryParam := make(map[string]string)
	fullURL := _apiURL
	method := http.MethodPost
	if isFamily {
		fullURL += "/family/file"
		method = http.MethodGet
		queryParam["familyId"] = y.FamilyID
	}

	var newObj model.Obj
	switch f := srcObj.(type) {
	case *Cloud189File:
		fullURL += "/renameFile.action"
		queryParam["fileId"] = srcObj.GetID()
		queryParam["destFileName"] = newName
		newObj = &Cloud189File{Icon: f.Icon} // 复用预览
	case *Cloud189Folder:
		fullURL += "/renameFolder.action"
		queryParam["folderId"] = srcObj.GetID()
		queryParam["destFolderName"] = newName
		newObj = &Cloud189Folder{}
	default:
		return nil, errs.NotSupport
	}

	_, err := y.request(fullURL, method, func(req *resty.Request) {
		req.SetContext(ctx).SetQueryParams(queryParam)
	}, nil, newObj, isFamily)
	if err != nil {
		return nil, err
	}
	return newObj, nil
}

// Copy 复制文件或文件夹
// 将文件或文件夹复制到新的目录
// 参数:
//   - ctx: 上下文
//   - srcObj: 源文件或文件夹对象
//   - dstDir: 目标目录对象
//
// 返回值: 错误信息
func (y *Cloud189PC) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	isFamily := y.isFamily()
	other := map[string]string{"targetFileName": dstDir.GetName()}

	resp, err := y.CreateBatchTask("COPY", IF(isFamily, y.FamilyID, ""), dstDir.GetID(), other, BatchTaskInfo{
		FileID:   srcObj.GetID(),
		FileName: srcObj.GetName(),
		IsFolder: BoolToNumber(srcObj.IsDir()),
	})

	if err != nil {
		return err
	}
	return y.WaitBatchTask("COPY", resp.TaskID, time.Second)
}

// Remove 删除文件或文件夹
// 参数:
//   - ctx: 上下文
//   - obj: 要删除的文件或文件夹对象
//
// 返回值: 错误信息
func (y *Cloud189PC) Remove(ctx context.Context, obj model.Obj) error {
	isFamily := y.isFamily()

	resp, err := y.CreateBatchTask("DELETE", IF(isFamily, y.FamilyID, ""), "", nil, BatchTaskInfo{
		FileID:   obj.GetID(),
		FileName: obj.GetName(),
		IsFolder: BoolToNumber(obj.IsDir()),
	})
	if err != nil {
		return err
	}
	// 批量任务数量限制，过快会导致无法删除
	return y.WaitBatchTask("DELETE", resp.TaskID, time.Millisecond*200)
}

// Put 上传文件
// 将文件上传到指定目录
// 参数:
//   - ctx: 上下文
//   - dstDir: 目标目录对象
//   - stream: 文件流
//   - up: 上传进度回调
//
// 返回值: 上传后的文件对象，错误信息
func (y *Cloud189PC) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (newObj model.Obj, err error) {
	overwrite := true
	isFamily := y.isFamily()

	// 响应时间长,按需启用
	if y.Addition.RapidUpload && !stream.IsForceStreamUpload() {
		if newObj, err := y.RapidUpload(ctx, dstDir, stream, isFamily, overwrite); err == nil {
			return newObj, nil
		}
	}

	uploadMethod := y.UploadMethod
	if stream.IsForceStreamUpload() {
		uploadMethod = "stream"
	}

	// 旧版上传家庭云也有限制
	if uploadMethod == "old" {
		return y.OldUpload(ctx, dstDir, stream, up, isFamily, overwrite)
	}

	// 开启家庭云转存
	if !isFamily && y.FamilyTransfer {
		// 修改上传目标为家庭云文件夹
		transferDstDir := dstDir
		dstDir = y.familyTransferFolder

		// 使用临时文件名
		srcName := stream.GetName()
		stream = &WrapFileStreamer{
			FileStreamer: stream,
			Name:         fmt.Sprintf("0%s.transfer", uuid.NewString()),
		}

		// 使用家庭云上传
		isFamily = true
		overwrite = false

		defer func() {
			if newObj != nil {
				// 转存家庭云文件到个人云
				err = y.SaveFamilyFileToPersonCloud(context.TODO(), y.FamilyID, newObj, transferDstDir, true)
				// 删除家庭云源文件
				go y.Delete(context.TODO(), y.FamilyID, newObj)
				// 批量任务有概率删不掉
				go y.cleanFamilyTransferFile()
				// 转存失败返回错误
				if err != nil {
					return
				}

				// 查找转存文件
				var file *Cloud189File
				file, err = y.findFileByName(context.TODO(), newObj.GetName(), transferDstDir.GetID(), false)
				if err != nil {
					if err == errs.ObjectNotFound {
						err = errors.Errorf("unknown error: No transfer file obtained %s", newObj.GetName())
					}
					return
				}

				// 重命名转存文件
				newObj, err = y.Rename(context.TODO(), file, srcName)
				if err != nil {
					// 重命名失败删除源文件
					_ = y.Delete(context.TODO(), "", file)
				}
				return
			}
		}()
	}

	switch uploadMethod {
	case "rapid":
		return y.FastUpload(ctx, dstDir, stream, up, isFamily, overwrite)
	case "stream":
		if stream.GetSize() == 0 {
			return y.FastUpload(ctx, dstDir, stream, up, isFamily, overwrite)
		}
		fallthrough
	default:
		return y.StreamUpload(ctx, dstDir, stream, up, isFamily, overwrite)
	}
}