package _123

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Pan123 实现123网盘的驱动
type Pan123 struct {
	model.Storage
	Addition
	// API请求速率限制器映射，键为API路径
	apiRateLimit sync.Map
}

// Config 返回驱动配置
func (d *Pan123) Config() driver.Config {
	return config
}

// GetAddition 返回驱动附加配置
func (d *Pan123) GetAddition() driver.Additional {
	return &d.Addition
}

// Init 初始化驱动，验证凭据有效性
func (d *Pan123) Init(ctx context.Context) error {
	// 通过请求用户信息接口验证凭据
	_, err := d.Request(UserInfo, http.MethodGet, nil, nil)
	return err
}

// Drop 注销登录
func (d *Pan123) Drop(ctx context.Context) error {
	// 尝试注销，但忽略可能的错误
	_, _ = d.Request(Logout, http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{})
	}, nil)
	return nil
}

// List 列出指定目录下的文件和子目录
func (d *Pan123) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	// 获取目录下的文件列表
	files, err := d.getFiles(ctx, dir.GetID(), dir.GetName())
	if err != nil {
		return nil, err
	}
	// 将File类型转换为model.Obj类型
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return src, nil
	})
}

// Link 获取文件的下载链接
func (d *Pan123) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// 尝试将file转换为File类型
	if f, ok := file.(File); ok {
		// 准备请求数据
		data := base.Json{
			"driveId":   0,
			"etag":      f.Etag,
			"fileId":    f.FileId,
			"fileName":  f.FileName,
			"s3keyFlag": f.S3KeyFlag,
			"size":      f.Size,
			"type":      f.Type,
		}

		// 请求下载信息
		resp, err := d.Request(DownloadInfo, http.MethodPost, func(req *resty.Request) {
			req.SetContext(ctx).SetBody(data)
		}, nil)
		if err != nil {
			return nil, err
		}

		// 解析下载URL
		downloadUrl := utils.GetBytes(resp, "data", "DownloadUrl").String()
		ou, err := url.Parse(downloadUrl)
		if err != nil {
			return nil, err
		}
		urlString := ou.String()
		// 处理可能的base64编码参数
		nu := ou.Query().Get("params")

		if nu != "" {
			du, err := base64.StdEncoding.DecodeString(nu)
			if err != nil {
				return nil, err
			}
			u, err := url.Parse(string(du))
			if err != nil {
				return nil, err
			}
			urlString = u.String()
		}

		log.Debug("download url: ", urlString)

		// 发送请求获取最终下载链接
		res, err := base.NoRedirectClient.R().SetHeader("Referer", "https://www.123pan.com/").Get(urlString)
		if err != nil {
			return nil, err
		}
		log.Debug(res.String())

		// 创建链接对象
		link := model.Link{
			URL: urlString,
		}

		log.Debugln("res code: ", res.StatusCode())
		// 处理重定向或直接返回的情况
		if res.StatusCode() == 302 {
			link.URL = res.Header().Get("location")
		} else if res.StatusCode() < 300 {
			link.URL = utils.GetBytes(res.Bytes(), "data", "redirect_url").String()
		}

		// 设置请求头
		link.Header = http.Header{
			"Referer": []string{fmt.Sprintf("%s://%s/", ou.Scheme, ou.Host)},
		}
		return &link, nil
	} else {
		return nil, errs.New("无法将对象转换为123网盘文件类型")
	}
}

// MakeDir 创建目录
func (d *Pan123) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	// 准备创建目录的请求数据
	data := base.Json{
		"driveId":      0,
		"etag":         "",
		"fileName":     dirName,
		"parentFileId": parentDir.GetID(),
		"size":         0,
		"type":         1, // 1表示目录
	}

	// 发送创建目录请求
	_, err := d.Request(Mkdir, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, nil)
	return err
}

// Move 移动文件或目录
func (d *Pan123) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	// 准备移动请求数据
	data := base.Json{
		"fileIdList":   []base.Json{{"FileId": srcObj.GetID()}},
		"parentFileId": dstDir.GetID(),
	}

	// 发送移动请求
	_, err := d.Request(Move, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, nil)
	return err
}

// Rename 重命名文件或目录
func (d *Pan123) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	// 准备重命名请求数据
	data := base.Json{
		"driveId":  0,
		"fileId":   srcObj.GetID(),
		"fileName": newName,
	}

	// 发送重命名请求
	_, err := d.Request(Rename, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, nil)
	return err
}

// Copy 复制文件或目录（不支持）
func (d *Pan123) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

// Remove 删除文件或目录（移至回收站）
func (d *Pan123) Remove(ctx context.Context, obj model.Obj) error {
	// 尝试将obj转换为File类型
	if f, ok := obj.(File); ok {
		// 准备删除请求数据
		data := base.Json{
			"driveId":           0,
			"operation":         true,
			"fileTrashInfoList": []File{f},
		}

		// 发送删除请求
		_, err := d.Request(Trash, http.MethodPost, func(req *resty.Request) {
			req.SetBody(data)
		}, nil)
		return err
	} else {
		return errs.New("无法将对象转换为123网盘文件类型")
	}
}

// Put 上传文件
func (d *Pan123) Put(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) error {
	// 获取文件的MD5哈希值
	etag := file.GetHash().GetHash(utils.MD5)
	var err error

	// 如果没有MD5哈希值，则计算一个
	if len(etag) < utils.MD5.Width {
		cacheFileProgress := model.UpdateProgressWithRange(up, 0, 50)
		up = model.UpdateProgressWithRange(up, 50, 100)
		_, etag, err = stream.CacheFullInTempFileAndHash(file, cacheFileProgress, utils.MD5)
		if err != nil {
			return err
		}
	}

	// 准备上传请求数据
	data := base.Json{
		"driveId":      0,
		"duplicate":    2, // 2->覆盖 1->重命名 0->默认
		"etag":         strings.ToLower(etag),
		"fileName":     file.GetName(),
		"parentFileId": dstDir.GetID(),
		"size":         file.GetSize(),
		"type":         0, // 0表示文件
	}

	// 发送上传请求
	var resp UploadResp
	res, err := d.Request(UploadRequest, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetContext(ctx)
	}, &resp)
	if err != nil {
		return err
	}
	log.Debugln("upload request res: ", string(res))

	// 如果文件已存在或不需要上传，直接返回
	if resp.Data.Reuse || resp.Data.Key == "" {
		return nil
	}

	// 根据响应选择上传方式
	if resp.Data.AccessKeyId == "" || resp.Data.SecretAccessKey == "" || resp.Data.SessionToken == "" {
		// 使用自定义上传方法
		err = d.newUpload(ctx, &resp, file, up)
		return err
	} else {
		// 使用AWS S3 SDK上传
		cfg := &aws.Config{
			Credentials:      credentials.NewStaticCredentials(resp.Data.AccessKeyId, resp.Data.SecretAccessKey, resp.Data.SessionToken),
			Region:           aws.String("123pan"),
			Endpoint:         aws.String(resp.Data.EndPoint),
			S3ForcePathStyle: aws.Bool(true),
		}

		// 创建S3会话
		s, err := session.NewSession(cfg)
		if err != nil {
			return err
		}

		// 创建上传器
		uploader := s3manager.NewUploader(s)

		// 如果文件大小超过最大分片限制，调整分片大小
		if file.GetSize() > s3manager.MaxUploadParts*s3manager.DefaultUploadPartSize {
			uploader.PartSize = file.GetSize() / (s3manager.MaxUploadParts - 1)
		}

		// 准备上传输入
		input := &s3manager.UploadInput{
			Bucket: &resp.Data.Bucket,
			Key:    &resp.Data.Key,
			Body: driver.NewLimitedUploadStream(ctx, &driver.ReaderUpdatingProgress{
				Reader:         file,
				UpdateProgress: up,
			}),
		}

		// 执行上传
		_, err = uploader.UploadWithContext(ctx, input)
		if err != nil {
			return err
		}
	}

	// 完成上传
	_, err = d.Request(UploadComplete, http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"fileId": resp.Data.FileId,
		}).SetContext(ctx)
	}, nil)
	return err
}

// APIRateLimit 实现API请求速率限制
func (d *Pan123) APIRateLimit(ctx context.Context, api string) error {
	// 为每个API路径创建或获取一个速率限制器
	value, _ := d.apiRateLimit.LoadOrStore(api,
		rate.NewLimiter(rate.Every(700*time.Millisecond), 1))
	limiter := value.(*rate.Limiter)

	// 等待直到可以进行下一次请求
	return limiter.Wait(ctx)
}

// 确保Pan123类型实现了driver.Driver接口
var _ driver.Driver = (*Pan123)(nil)