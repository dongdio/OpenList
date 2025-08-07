package _115_open

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	sdk "github.com/OpenListTeam/115-sdk-go"
	"golang.org/x/time/rate"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Open115 115开放平台存储驱动实现
type Open115 struct {
	model.Storage
	Addition
	client  *sdk.Client   // 115开放平台SDK客户端
	limiter *rate.Limiter // 请求速率限制器
}

// Config 返回驱动配置
// 实现driver.Driver接口
func (d *Open115) Config() driver.Config {
	return config
}

// GetAddition 返回额外配置
// 实现driver.Driver接口
func (d *Open115) GetAddition() driver.Additional {
	return &d.Addition
}

// Init 初始化驱动
// 实现driver.Driver接口
func (d *Open115) Init(ctx context.Context) error {
	// 创建115开放平台客户端
	d.client = sdk.New(
		sdk.WithRefreshToken(d.Addition.RefreshToken),
		sdk.WithAccessToken(d.Addition.AccessToken),
		// 当令牌刷新时保存新的令牌
		sdk.WithOnRefreshToken(func(accessToken, refreshToken string) {
			d.Addition.AccessToken = accessToken
			d.Addition.RefreshToken = refreshToken
			op.MustSaveDriverStorage(d)
		}),
	)

	// 在调试或开发模式下启用调试输出
	if global.Debug || global.Dev {
		d.client.SetDebug(true)
	}

	// 测试API连接，获取用户信息
	_, err := d.client.UserInfo(ctx)
	if err != nil {
		return errs.Wrap(err, "初始化115开放平台客户端失败")
	}

	// 如果设置了速率限制，初始化限制器
	if d.Addition.LimitRate > 0 {
		d.limiter = rate.NewLimiter(rate.Limit(d.Addition.LimitRate), 1)
	}

	return nil
}

// WaitLimit 等待请求限制
// 如果设置了速率限制，会等待直到可以执行请求
func (d *Open115) WaitLimit(ctx context.Context) error {
	if d.limiter != nil {
		return d.limiter.Wait(ctx)
	}
	return nil
}

// Drop 释放资源
// 实现driver.Driver接口
func (d *Open115) Drop(ctx context.Context) error {
	// 无需特殊资源释放
	return nil
}

// List 列出目录内容
// 实现driver.Driver接口
func (d *Open115) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	var result []model.Obj
	pageSize := int64(200) // 每页最大200条记录
	offset := int64(0)

	// 分页获取所有文件
	for {
		// 等待请求限制
		if err := d.WaitLimit(ctx); err != nil {
			return nil, errs.Wrap(err, "等待请求限制时被中断")
		}

		// 构建获取文件列表请求
		resp, err := d.client.GetFiles(ctx, &sdk.GetFilesReq{
			CID:     dir.GetID(),                        // 目录ID
			Limit:   pageSize,                           // 每页大小
			Offset:  offset,                             // 偏移量
			ASC:     d.Addition.OrderDirection == "asc", // 是否升序排序
			O:       d.Addition.OrderBy,                 // 排序字段
			ShowDir: true,                               // 显示目录
		})

		if err != nil {
			return nil, errs.Wrap(err, "获取文件列表失败")
		}

		// 转换文件列表为模型对象
		result = append(result, utils.MustSliceConvert(resp.Data, func(src sdk.GetFilesResp_File) model.Obj {
			v := Obj(src)
			return &v
		})...)

		// 检查是否已获取所有文件
		if len(result) >= int(resp.Count) {
			break
		}

		// 更新偏移量，获取下一页
		offset += pageSize
	}

	return result, nil
}

// Link 获取文件下载链接
// 实现driver.Driver接口
func (d *Open115) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// 等待请求限制
	if err := d.WaitLimit(ctx); err != nil {
		return nil, errs.Wrap(err, "等待请求限制时被中断")
	}

	// 获取用户代理
	var userAgent string
	if args.Header != nil {
		userAgent = args.Header.Get("User-Agent")
	}
	if userAgent == "" {
		userAgent = consts.ChromeUserAgent
	}

	// 类型断言为115对象
	obj, ok := file.(*Obj)
	if !ok {
		return nil, errs.New("无法将对象转换为115开放平台文件对象")
	}

	// 获取提取码
	pickCode := obj.Pc

	// 获取下载URL
	resp, err := d.client.DownURL(ctx, pickCode, userAgent)
	if err != nil {
		return nil, errs.Wrap(err, "获取下载URL失败")
	}

	// 从响应中获取URL
	downloadInfo, ok := resp[obj.GetID()]
	if !ok {
		return nil, errs.Errorf("无法获取文件[%s]的下载链接", obj.GetID())
	}

	// 构建链接
	return &model.Link{
		URL: downloadInfo.URL.URL,
		Header: http.Header{
			"User-Agent": []string{userAgent},
		},
	}, nil
}

func (d *Open115) GetObjInfo(ctx context.Context, path string) (model.Obj, error) {
	if err := d.WaitLimit(ctx); err != nil {
		return nil, err
	}
	resp, err := d.client.GetFolderInfoByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	return &Obj{
		Fid:  resp.FileID,
		Fn:   resp.FileName,
		Fc:   resp.FileCategory,
		Sha1: resp.Sha1,
		Pc:   resp.PickCode,
	}, nil
}

// MakeDir 创建目录
// 实现driver.Driver接口
func (d *Open115) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	// 等待请求限制
	if err := d.WaitLimit(ctx); err != nil {
		return nil, errs.Wrap(err, "等待请求限制时被中断")
	}

	// 创建目录
	resp, err := d.client.Mkdir(ctx, parentDir.GetID(), dirName)
	if err != nil {
		return nil, errs.Wrap(err, "创建目录失败")
	}

	// 构建目录对象
	now := time.Now().Unix()
	return &Obj{
		Fid:  resp.FileID,       // 文件ID
		Pid:  parentDir.GetID(), // 父目录ID
		Fn:   dirName,           // 目录名
		Fc:   "0",               // 类型为目录
		Upt:  now,               // 修改时间
		Uet:  now,               // 编辑时间
		UpPt: now,               // 创建时间
	}, nil
}

// Move 移动文件/目录
// 实现driver.Driver接口
func (d *Open115) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// 等待请求限制
	if err := d.WaitLimit(ctx); err != nil {
		return nil, errs.Wrap(err, "等待请求限制时被中断")
	}

	// 执行移动操作
	_, err := d.client.Move(ctx, &sdk.MoveReq{
		FileIDs: srcObj.GetID(), // 要移动的文件ID
		ToCid:   dstDir.GetID(), // 目标目录ID
	})

	if err != nil {
		return nil, errs.Wrap(err, "移动文件失败")
	}

	return srcObj, nil
}

// Rename 重命名文件/目录
// 实现driver.Driver接口
func (d *Open115) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	// 等待请求限制
	if err := d.WaitLimit(ctx); err != nil {
		return nil, errs.Wrap(err, "等待请求限制时被中断")
	}

	// 执行重命名操作
	_, err := d.client.UpdateFile(ctx, &sdk.UpdateFileReq{
		FileID:  srcObj.GetID(), // 文件ID
		FileNma: newName,        // 新文件名
	})

	if err != nil {
		return nil, errs.Wrap(err, "重命名文件失败")
	}

	// 更新对象的文件名
	obj, ok := srcObj.(*Obj)
	if ok {
		obj.Fn = newName
	}

	return srcObj, nil
}

// Copy 复制文件/目录
// 实现driver.Driver接口
func (d *Open115) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// 等待请求限制
	if err := d.WaitLimit(ctx); err != nil {
		return nil, errs.Wrap(err, "等待请求限制时被中断")
	}

	// 执行复制操作
	_, err := d.client.Copy(ctx, &sdk.CopyReq{
		PID:     dstDir.GetID(), // 目标目录ID
		FileID:  srcObj.GetID(), // 要复制的文件ID
		NoDupli: "1",            // 不允许重复
	})

	if err != nil {
		return nil, errs.Wrap(err, "复制文件失败")
	}

	return srcObj, nil
}

// Remove 删除文件/目录
// 实现driver.Driver接口
func (d *Open115) Remove(ctx context.Context, obj model.Obj) error {
	// 等待请求限制
	if err := d.WaitLimit(ctx); err != nil {
		return errs.Wrap(err, "等待请求限制时被中断")
	}

	// 类型断言为115对象
	fileObj, ok := obj.(*Obj)
	if !ok {
		return errs.New("无法将对象转换为115开放平台文件对象")
	}

	// 执行删除操作
	_, err := d.client.DelFile(ctx, &sdk.DelFileReq{
		FileIDs:  fileObj.GetID(), // 要删除的文件ID
		ParentID: fileObj.Pid,     // 父目录ID
	})

	if err != nil {
		return errs.Wrap(err, "删除文件失败")
	}

	return nil
}

// Put 上传文件
// 实现driver.Driver接口
func (d *Open115) Put(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) error {
	// 等待请求限制
	err := d.WaitLimit(ctx)
	if err != nil {
		return errs.Wrap(err, "等待请求限制时被中断")
	}

	// 获取文件SHA1哈希
	sha1 := file.GetHash().GetHash(utils.SHA1)
	if len(sha1) != utils.SHA1.Width {
		// 如果没有SHA1哈希，计算一个
		cacheFileProgress := model.UpdateProgressWithRange(up, 0, 50)
		up = model.UpdateProgressWithRange(up, 50, 100)
		_, sha1, err = stream.CacheFullInTempFileAndHash(file, cacheFileProgress, utils.SHA1)

		if err != nil {
			return errs.Wrap(err, "计算文件SHA1哈希失败")
		}
	}

	// 计算文件前128KB的SHA1哈希（预哈希）
	const PreHashSize int64 = 128 * utils.KB
	hashSize := min(file.GetSize(), PreHashSize)

	reader, err := file.RangeRead(http_range.Range{Start: 0, Length: hashSize})
	if err != nil {
		return errs.Wrap(err, "读取文件前部分失败")
	}

	sha1128k, err := utils.HashReader(utils.SHA1, reader)
	if err != nil {
		return errs.Wrap(err, "计算文件预哈希失败")
	}

	// 1. 初始化上传
	resp, err := d.client.UploadInit(ctx, &sdk.UploadInitReq{
		FileName: file.GetName(),            // 文件名
		FileSize: file.GetSize(),            // 文件大小
		Target:   dstDir.GetID(),            // 目标目录ID
		FileID:   strings.ToUpper(sha1),     // 文件SHA1（完整哈希）
		PreID:    strings.ToUpper(sha1128k), // 文件预哈希（前128KB）
	})

	if err != nil {
		return errs.Wrap(err, "初始化上传失败")
	}

	// 如果状态为2，表示秒传成功
	if resp.Status == 2 {
		up(100)
		return nil
	}

	// 2. 两步验证（如果需要）
	if utils.SliceContains([]int{6, 7, 8}, resp.Status) {
		// 解析签名检查范围
		signCheck := strings.Split(resp.SignCheck, "-") // 格式："2392148-2392298"，表示取该范围内容的SHA1
		start, err := strconv.ParseInt(signCheck[0], 10, 64)
		if err != nil {
			return errs.Wrap(err, "解析签名检查范围起始位置失败")
		}

		end, err := strconv.ParseInt(signCheck[1], 10, 64)
		if err != nil {
			return errs.Wrap(err, "解析签名检查范围结束位置失败")
		}

		// 读取指定范围的数据
		reader, err = file.RangeRead(http_range.Range{Start: start, Length: end - start + 1})
		if err != nil {
			return errs.Wrap(err, "读取文件指定范围失败")
		}

		// 计算该范围的SHA1哈希
		signVal, err := utils.HashReader(utils.SHA1, reader)
		if err != nil {
			return errs.Wrap(err, "计算签名值失败")
		}

		// 再次初始化上传，带上签名信息
		resp, err = d.client.UploadInit(ctx, &sdk.UploadInitReq{
			FileName: file.GetName(),            // 文件名
			FileSize: file.GetSize(),            // 文件大小
			Target:   dstDir.GetID(),            // 目标目录ID
			FileID:   strings.ToUpper(sha1),     // 文件SHA1（完整哈希）
			PreID:    strings.ToUpper(sha1128k), // 文件预哈希（前128KB）
			SignKey:  resp.SignKey,              // 签名密钥
			SignVal:  strings.ToUpper(signVal),  // 签名值
		})

		if err != nil {
			return errs.Wrap(err, "带签名初始化上传失败")
		}

		// 如果状态为2，表示秒传成功
		if resp.Status == 2 {
			up(100)
			return nil
		}
	}

	// 3. 获取上传令牌
	tokenResp, err := d.client.UploadGetToken(ctx)
	if err != nil {
		return errs.Wrap(err, "获取上传令牌失败")
	}

	// 4. 上传文件
	err = d.multpartUpload(ctx, file, up, tokenResp, resp)
	if err != nil {
		return errs.Wrap(err, "分片上传文件失败")
	}

	return nil
}

func (d *Open115) OfflineDownload(ctx context.Context, uris []string, dstDir model.Obj) ([]string, error) {
	return d.client.AddOfflineTaskURIs(ctx, uris, dstDir.GetID())
}

func (d *Open115) DeleteOfflineTask(ctx context.Context, infoHash string, deleteFiles bool) error {
	return d.client.DeleteOfflineTask(ctx, infoHash, deleteFiles)
}

func (d *Open115) OfflineList(ctx context.Context) (*sdk.OfflineTaskListResp, error) {
	resp, err := d.client.OfflineTaskList(ctx, 1)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// func (d *Open115) GetArchiveMeta(ctx context.Context, obj model.Obj, args model.ArchiveArgs) (model.ArchiveMeta, error) {
// 	// TODO get archive file meta-info, return errs.NotImplement to use an internal archive tool, optional
// 	return nil, errs.NotImplement
// }

// func (d *Open115) ListArchive(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) ([]model.Obj, error) {
// 	// TODO list args.InnerPath in the archive obj, return errs.NotImplement to use an internal archive tool, optional
// 	return nil, errs.NotImplement
// }

// func (d *Open115) Extract(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) (*model.Link, error) {
// 	// TODO return link of file args.InnerPath in the archive obj, return errs.NotImplement to use an internal archive tool, optional
// 	return nil, errs.NotImplement
// }

// func (d *Open115) ArchiveDecompress(ctx context.Context, srcObj, dstDir model.Obj, args model.ArchiveDecompressArgs) ([]model.Obj, error) {
// 	// TODO extract args.InnerPath path in the archive srcObj to the dstDir location, optional
// 	// a folder with the same name as the archive file needs to be created to store the extracted results if args.PutIntoNewDir
// 	// return errs.NotImplement to use an internal archive tool
// 	return nil, errs.NotImplement
// }

// func (d *Template) Other(ctx context.Context, args model.OtherArgs) (any, error) {
//	return nil, errs.NotSupport
// }

// 确保Open115实现了driver.Driver接口
var _ driver.Driver = (*Open115)(nil)