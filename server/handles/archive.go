package handles

import (
	"fmt"
	"net/url"
	stdpath "path"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/fs"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/internal/sign"
	"github.com/dongdio/OpenList/server/common"
	"github.com/dongdio/OpenList/utility/archive/tool"
	"github.com/dongdio/OpenList/utility/errs"
	"github.com/dongdio/OpenList/utility/task"
	"github.com/dongdio/OpenList/utility/utils"
)

// ArchiveMetaReq 归档文件元数据请求参数
type ArchiveMetaReq struct {
	Path        string `json:"path" form:"path" binding:"required"`
	Password    string `json:"password" form:"password"`
	Refresh     bool   `json:"refresh" form:"refresh"`
	ArchivePass string `json:"archive_pass" form:"archive_pass"`
}

// ArchiveMetaResp 归档文件元数据响应
type ArchiveMetaResp struct {
	Comment     string               `json:"comment"`        // 归档文件注释
	IsEncrypted bool                 `json:"encrypted"`      // 是否加密
	Content     []ArchiveContentResp `json:"content"`        // 归档内容
	Sort        *model.Sort          `json:"sort,omitempty"` // 排序信息
	RawURL      string               `json:"raw_url"`        // 原始URL
	Sign        string               `json:"sign"`           // 签名
}

// ArchiveContentResp 归档内容响应
type ArchiveContentResp struct {
	ObjResp
	Children []ArchiveContentResp `json:"children"` // 子项
}

// toObjsRespWithoutSignAndThumb 转换对象为响应格式（不包含签名和缩略图）
func toObjsRespWithoutSignAndThumb(obj model.Obj) ObjResp {
	return ObjResp{
		Name:        obj.GetName(),
		Size:        obj.GetSize(),
		IsDir:       obj.IsDir(),
		Modified:    obj.ModTime(),
		Created:     obj.CreateTime(),
		HashInfoStr: obj.GetHash().String(),
		HashInfo:    obj.GetHash().Export(),
		Sign:        "",
		Thumb:       "",
		Type:        utils.GetObjType(obj.GetName(), obj.IsDir()),
	}
}

// toContentResp 递归转换对象树为内容响应
func toContentResp(objs []model.ObjTree) []ArchiveContentResp {
	if objs == nil {
		return nil
	}

	result, _ := utils.SliceConvert(objs, func(src model.ObjTree) (ArchiveContentResp, error) {
		return ArchiveContentResp{
			ObjResp:  toObjsRespWithoutSignAndThumb(src),
			Children: toContentResp(src.GetChildren()),
		}, nil
	})

	return result
}

// FsArchiveMeta 获取归档文件元数据
func FsArchiveMeta(c *gin.Context) {
	var req ArchiveMetaReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取用户并验证权限
	user := c.MustGet("user").(*model.User)
	if !user.CanReadArchives() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	// 获取完整路径
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 获取元数据
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500, true)
		return
	}
	c.Set("meta", meta)

	// 验证访问权限
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}

	// 准备归档参数
	archiveArgs := model.ArchiveArgs{
		LinkArgs: model.LinkArgs{
			Header:  c.Request.Header,
			Type:    c.Query("type"),
			HttpReq: c.Request,
		},
		Password: req.ArchivePass,
	}

	// 获取归档元数据
	ret, err := fs.ArchiveMeta(c, reqPath, model.ArchiveMetaArgs{
		ArchiveArgs: archiveArgs,
		Refresh:     req.Refresh,
	})
	if err != nil {
		if errors.Is(err, errs.WrongArchivePassword) {
			common.ErrorResp(c, err, 202)
		} else {
			common.ErrorResp(c, err, 500)
		}
		return
	}

	// 生成签名
	signature := ""
	if isEncrypt(meta, reqPath) || setting.GetBool(consts.SignAll) {
		signature = sign.SignArchive(reqPath)
	}

	// 确定API路径
	api := "/ae"
	if ret.DriverProviding {
		api = "/ad"
	}

	// 返回响应
	common.SuccessResp(c, ArchiveMetaResp{
		Comment:     ret.GetComment(),
		IsEncrypted: ret.IsEncrypted(),
		Content:     toContentResp(ret.GetTree()),
		Sort:        ret.Sort,
		RawURL:      fmt.Sprintf("%s%s%s", common.GetApiUrl(c.Request), api, utils.EncodePath(reqPath, true)),
		Sign:        signature,
	})
}

// ArchiveListReq 归档文件列表请求参数
type ArchiveListReq struct {
	ArchiveMetaReq
	model.PageReq
	InnerPath string `json:"inner_path" form:"inner_path"`
}

// ArchiveListResp 归档文件列表响应
type ArchiveListResp struct {
	Content []ObjResp `json:"content"` // 内容列表
	Total   int64     `json:"total"`   // 总数
}

// FsArchiveList 获取归档文件内容列表
func FsArchiveList(c *gin.Context) {
	var req ArchiveListReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证分页参数
	req.Validate()

	// 获取用户并验证权限
	user := c.MustGet("user").(*model.User)
	if !user.CanReadArchives() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	// 获取完整路径
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 获取元数据
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500, true)
		return
	}
	c.Set("meta", meta)

	// 验证访问权限
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}

	// 获取归档文件列表
	objs, err := fs.ArchiveList(c, reqPath, model.ArchiveListArgs{
		ArchiveInnerArgs: model.ArchiveInnerArgs{
			ArchiveArgs: model.ArchiveArgs{
				LinkArgs: model.LinkArgs{
					Header:  c.Request.Header,
					Type:    c.Query("type"),
					HttpReq: c.Request,
				},
				Password: req.ArchivePass,
			},
			InnerPath: utils.FixAndCleanPath(req.InnerPath),
		},
		Refresh: req.Refresh,
	})
	if err != nil {
		if errors.Is(err, errs.WrongArchivePassword) {
			common.ErrorResp(c, err, 202)
		} else {
			common.ErrorResp(c, err, 500)
		}
		return
	}

	// 分页处理
	total, objs := pagination(objs, &req.PageReq)

	// 转换为响应格式
	result, _ := utils.SliceConvert(objs, func(src model.Obj) (ObjResp, error) {
		return toObjsRespWithoutSignAndThumb(src), nil
	})

	// 返回响应
	common.SuccessResp(c, ArchiveListResp{
		Content: result,
		Total:   int64(total),
	})
}

// StringOrArray 可以是字符串或字符串数组的类型
type StringOrArray []string

// UnmarshalJSON 实现JSON反序列化接口
func (s *StringOrArray) UnmarshalJSON(data []byte) error {
	// 尝试解析为单个字符串
	var value string
	if err := utils.Json.Unmarshal(data, &value); err == nil {
		*s = []string{value}
		return nil
	}

	// 尝试解析为字符串数组
	var sliceValue []string
	if err := utils.Json.Unmarshal(data, &sliceValue); err != nil {
		return err
	}
	*s = sliceValue
	return nil
}

// ArchiveDecompressReq 归档文件解压请求参数
type ArchiveDecompressReq struct {
	SrcDir        string        `json:"src_dir" form:"src_dir" binding:"required"`
	DstDir        string        `json:"dst_dir" form:"dst_dir" binding:"required"`
	Name          StringOrArray `json:"name" form:"name" binding:"required"`
	ArchivePass   string        `json:"archive_pass" form:"archive_pass"`
	InnerPath     string        `json:"inner_path" form:"inner_path"`
	CacheFull     bool          `json:"cache_full" form:"cache_full"`
	PutIntoNewDir bool          `json:"put_into_new_dir" form:"put_into_new_dir"`
}

// FsArchiveDecompress 解压缩归档文件
func FsArchiveDecompress(c *gin.Context) {
	var req ArchiveDecompressReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证请求参数
	if len(req.Name) == 0 {
		common.ErrorStrResp(c, "name is required", 400)
		return
	}

	// 获取用户并验证权限
	user := c.MustGet("user").(*model.User)
	if !user.CanDecompress() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	// 构建源路径列表
	srcPaths := make([]string, 0, len(req.Name))
	for _, name := range req.Name {
		srcPath, err := user.JoinPath(stdpath.Join(req.SrcDir, name))
		if err != nil {
			common.ErrorResp(c, err, 403)
			return
		}
		srcPaths = append(srcPaths, srcPath)
	}

	// 获取目标目录
	dstDir, err := user.JoinPath(req.DstDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 处理每个源文件
	tasks := make([]task.TaskExtensionInfo, 0, len(srcPaths))
	for _, srcPath := range srcPaths {
		tk, err := fs.ArchiveDecompress(c, srcPath, dstDir, model.ArchiveDecompressArgs{
			ArchiveInnerArgs: model.ArchiveInnerArgs{
				ArchiveArgs: model.ArchiveArgs{
					LinkArgs: model.LinkArgs{
						Header:  c.Request.Header,
						Type:    c.Query("type"),
						HttpReq: c.Request,
					},
					Password: req.ArchivePass,
				},
				InnerPath: utils.FixAndCleanPath(req.InnerPath),
			},
			CacheFull:     req.CacheFull,
			PutIntoNewDir: req.PutIntoNewDir,
		})
		if err != nil {
			if errors.Is(err, errs.WrongArchivePassword) {
				common.ErrorResp(c, err, 202)
			} else {
				common.ErrorResp(c, err, 500)
			}
			return
		}
		if tk != nil {
			tasks = append(tasks, tk)
		}
	}

	// 返回任务信息
	common.SuccessResp(c, gin.H{
		"task": getTaskInfos(tasks),
	})
}

// ArchiveDown 下载归档文件中的内容
func ArchiveDown(c *gin.Context) {
	// 获取路径参数
	archiveRawPath := c.MustGet("path").(string)
	innerPath := utils.FixAndCleanPath(c.Query("inner"))
	password := c.Query("pass")
	filename := stdpath.Base(innerPath)

	// 获取存储
	storage, err := fs.GetStorage(archiveRawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 判断是否需要代理
	if common.ShouldProxy(storage, filename) {
		ArchiveProxy(c)
		return
	}

	// 提取归档文件内容
	link, _, err := fs.ArchiveDriverExtract(c, archiveRawPath, model.ArchiveInnerArgs{
		ArchiveArgs: model.ArchiveArgs{
			LinkArgs: model.LinkArgs{
				IP:       c.ClientIP(),
				Header:   c.Request.Header,
				Type:     c.Query("type"),
				HttpReq:  c.Request,
				Redirect: true,
			},
			Password: password,
		},
		InnerPath: innerPath,
	})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 下载文件
	down(c, link)
}

// ArchiveProxy 代理归档文件中的内容
func ArchiveProxy(c *gin.Context) {
	// 获取路径参数
	archiveRawPath := c.MustGet("path").(string)
	innerPath := utils.FixAndCleanPath(c.Query("inner"))
	password := c.Query("pass")
	filename := stdpath.Base(innerPath)

	// 获取存储
	storage, err := fs.GetStorage(archiveRawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 检查是否可以代理
	if canProxy(storage, filename) {
		// 提取归档文件内容
		link, file, err := fs.ArchiveDriverExtract(c, archiveRawPath, model.ArchiveInnerArgs{
			ArchiveArgs: model.ArchiveArgs{
				LinkArgs: model.LinkArgs{
					Header:  c.Request.Header,
					Type:    c.Query("type"),
					HttpReq: c.Request,
				},
				Password: password,
			},
			InnerPath: innerPath,
		})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}

		// 本地代理
		localProxy(c, link, file, storage.GetStorage().ProxyRange)
	} else {
		common.ErrorStrResp(c, "proxy not allowed", 403)
	}
}

// ArchiveInternalExtract 内部提取归档文件内容
func ArchiveInternalExtract(c *gin.Context) {
	// 获取路径参数
	archiveRawPath := c.MustGet("path").(string)
	innerPath := utils.FixAndCleanPath(c.Query("inner"))
	password := c.Query("pass")

	// 提取归档文件内容
	rc, size, err := fs.ArchiveInternalExtract(c, archiveRawPath, model.ArchiveInnerArgs{
		ArchiveArgs: model.ArchiveArgs{
			LinkArgs: model.LinkArgs{
				Header:  c.Request.Header,
				Type:    c.Query("type"),
				HttpReq: c.Request,
			},
			Password: password,
		},
		InnerPath: innerPath,
	})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 确保关闭文件
	defer func() {
		if err = rc.Close(); err != nil {
			log.Errorf("failed to close file streamer, %v", err)
		}
	}()

	// 设置响应头
	headers := map[string]string{
		"Referrer-Policy": "no-referrer",
		"Cache-Control":   "max-age=0, no-cache, no-store, must-revalidate",
	}

	// 设置文件名
	filename := stdpath.Base(innerPath)
	headers["Content-Disposition"] = fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filename, url.PathEscape(filename))

	// 设置内容类型
	contentType := c.Request.Header.Get("Content-Type")
	if contentType == "" {
		contentType = utils.GetMimeType(filename)
	}

	// 发送文件内容
	c.DataFromReader(200, size, contentType, rc, headers)
}

// ArchiveExtensions 获取支持的归档扩展名
func ArchiveExtensions(c *gin.Context) {
	// 获取所有支持的扩展名
	extensions := make([]string, 0, len(tool.Tools))
	for key := range tool.Tools {
		extensions = append(extensions, key)
	}

	// 返回扩展名列表
	common.SuccessResp(c, extensions)
}