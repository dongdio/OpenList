package handles

import (
	"fmt"
	stdpath "path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/internal/sign"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// ListReq 文件列表请求参数
type ListReq struct {
	model.PageReq
	Path     string `json:"path" form:"path" binding:"required"`
	Password string `json:"password" form:"password"`
	Refresh  bool   `json:"refresh"`
}

// DirReq 目录请求参数
type DirReq struct {
	Path      string `json:"path" form:"path" binding:"required"`
	Password  string `json:"password" form:"password"`
	ForceRoot bool   `json:"force_root" form:"force_root"`
}

// ObjResp 对象响应结构
type ObjResp struct {
	Id          string                     `json:"id"`
	Path        string                     `json:"path"`
	Name        string                     `json:"name"`
	Size        int64                      `json:"size"`
	IsDir       bool                       `json:"is_dir"`
	Modified    time.Time                  `json:"modified"`
	Created     time.Time                  `json:"created"`
	Sign        string                     `json:"sign"`
	Thumb       string                     `json:"thumb"`
	Type        int                        `json:"type"`
	HashInfoStr string                     `json:"hashinfo"`
	HashInfo    map[*utils.HashType]string `json:"hash_info"`
}

// FsListResp 文件列表响应结构
type FsListResp struct {
	Content  []ObjResp `json:"content"`
	Total    int64     `json:"total"`
	Readme   string    `json:"readme"`
	Header   string    `json:"header"`
	Write    bool      `json:"write"`
	Provider string    `json:"provider"`
}

// FsList 获取文件列表
func FsList(c *gin.Context) {
	var req ListReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证分页参数
	req.Validate()

	// 获取用户和路径
	user := c.Value(consts.UserKey).(*model.User)
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
	common.GinWithValue(c, consts.MetaKey, meta)

	// 检查访问权限
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}

	// 检查刷新权限
	if !user.CanWrite() && !common.CanWrite(meta, reqPath) && req.Refresh {
		common.ErrorStrResp(c, "Refresh without permission", 403)
		return
	}

	// 获取文件列表
	objs, err := fs.List(c.Request.Context(), reqPath, &fs.ListArgs{Refresh: req.Refresh})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 分页处理
	total, objs := pagination(objs, &req.PageReq)

	// 获取存储提供者信息
	provider := "unknown"
	storage, err := fs.GetStorage(reqPath, &fs.GetStoragesArgs{})
	if err == nil {
		provider = storage.GetStorage().Driver
	}

	// 返回结果
	common.SuccessResp(c, FsListResp{
		Content:  toObjsResp(objs, reqPath, isEncrypt(meta, reqPath)),
		Total:    int64(total),
		Readme:   getReadme(meta, reqPath),
		Header:   getHeader(meta, reqPath),
		Write:    user.CanWrite() || common.CanWrite(meta, reqPath),
		Provider: provider,
	})
}

// FsDirs 获取目录列表
func FsDirs(c *gin.Context) {
	var req DirReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取用户
	user := c.Value(consts.UserKey).(*model.User)

	// 处理路径
	reqPath := req.Path
	if req.ForceRoot {
		if !user.IsAdmin() {
			common.ErrorStrResp(c, "Permission denied", 403)
			return
		}
	} else {
		tmp, err := user.JoinPath(req.Path)
		if err != nil {
			common.ErrorResp(c, err, 403)
			return
		}
		reqPath = tmp
	}

	// 获取元数据
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500, true)
		return
	}
	common.GinWithValue(c, consts.MetaKey, meta)

	// 检查访问权限
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}

	// 获取文件列表
	objs, err := fs.List(c.Request.Context(), reqPath, &fs.ListArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 过滤出目录
	dirs := filterDirs(objs)
	common.SuccessResp(c, dirs)
}

// DirResp 目录响应结构
type DirResp struct {
	Name     string    `json:"name"`
	Modified time.Time `json:"modified"`
}

// filterDirs 过滤目录
func filterDirs(objs []model.Obj) []DirResp {
	dirs := make([]DirResp, 0, len(objs))
	for _, obj := range objs {
		if obj.IsDir() {
			dirs = append(dirs, DirResp{
				Name:     obj.GetName(),
				Modified: obj.ModTime(),
			})
		}
	}
	return dirs
}

// getReadme 获取README内容
func getReadme(meta *model.Meta, path string) string {
	if meta != nil && (utils.PathEqual(meta.Path, path) || meta.RSub) {
		return meta.Readme
	}
	return ""
}

// getHeader 获取Header内容
func getHeader(meta *model.Meta, path string) string {
	if meta != nil && (utils.PathEqual(meta.Path, path) || meta.HeaderSub) {
		return meta.Header
	}
	return ""
}

// isEncrypt 检查路径是否加密
func isEncrypt(meta *model.Meta, path string) bool {
	if common.IsStorageSignEnabled(path) {
		return true
	}
	if meta == nil || meta.Password == "" {
		return false
	}
	if !utils.PathEqual(meta.Path, path) && !meta.PSub {
		return false
	}
	return true
}

// pagination 分页处理
func pagination(objs []model.Obj, req *model.PageReq) (int, []model.Obj) {
	pageIndex, pageSize := req.Page, req.PerPage
	total := len(objs)

	// 计算起始位置
	start := (pageIndex - 1) * pageSize
	if start >= total {
		return total, []model.Obj{}
	}

	// 计算结束位置
	end := start + pageSize
	if end > total {
		end = total
	}

	return total, objs[start:end]
}

// toObjsResp 转换对象为响应格式
func toObjsResp(objs []model.Obj, parent string, encrypt bool) []ObjResp {
	resp := make([]ObjResp, 0, len(objs))

	for _, obj := range objs {
		thumb, _ := model.GetThumb(obj)
		resp = append(resp, ObjResp{
			Id:          obj.GetID(),
			Path:        obj.GetPath(),
			Name:        obj.GetName(),
			Size:        obj.GetSize(),
			IsDir:       obj.IsDir(),
			Modified:    obj.ModTime(),
			Created:     obj.CreateTime(),
			HashInfoStr: obj.GetHash().String(),
			HashInfo:    obj.GetHash().Export(),
			Sign:        common.Sign(obj, parent, encrypt),
			Thumb:       thumb,
			Type:        utils.GetObjType(obj.GetName(), obj.IsDir()),
		})
	}

	return resp
}

// FsGetReq 获取文件请求参数
type FsGetReq struct {
	Path     string `json:"path" form:"path" binding:"required"`
	Password string `json:"password" form:"password"`
}

// FsGetResp 获取文件响应结构
type FsGetResp struct {
	ObjResp
	RawURL   string    `json:"raw_url"`
	Readme   string    `json:"readme"`
	Header   string    `json:"header"`
	Provider string    `json:"provider"`
	Related  []ObjResp `json:"related"`
}

// FsGet 获取文件信息
func FsGet(c *gin.Context) {
	var req FsGetReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取用户和路径
	user := c.Value(consts.UserKey).(*model.User)
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 获取元数据
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500)
		return
	}
	common.GinWithValue(c, consts.MetaKey, meta)

	// 检查访问权限
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}

	// 获取文件对象
	obj, err := fs.Get(c.Request.Context(), reqPath, &fs.GetArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 获取存储提供者信息
	var rawURL string
	storage, err := fs.GetStorage(reqPath, &fs.GetStoragesArgs{})
	provider := "unknown"
	if err == nil {
		provider = storage.Config().Name
	} else {
		common.ErrorResp(c, err, 500)
		return
	}

	// 处理非目录文件
	if !obj.IsDir() {
		if storage.Config().MustProxy() || storage.GetStorage().WebProxy {
			rawURL = common.GenerateDownProxyURL(storage.GetStorage(), reqPath)
			if rawURL == "" {
				query := ""
				if isEncrypt(meta, reqPath) || setting.GetBool(consts.SignAll) {
					query = "?sign=" + sign.Sign(reqPath)
				}
				rawURL = fmt.Sprintf("%s/p%s%s",
					common.GetApiURL(c),
					utils.EncodePath(reqPath, true),
					query)
			}
		} else {
			// 文件有原始URL
			if url, ok := model.GetUrl(obj); ok {
				rawURL = url
			} else {
				// 如果存储不是代理，使用fs.Link获取原始URL
				link, _, err := fs.Link(c.Request.Context(), reqPath, model.LinkArgs{
					IP:       c.ClientIP(),
					Header:   c.Request.Header,
					Redirect: true,
				})
				if err != nil {
					common.ErrorResp(c, err, 500)
					return
				}
				defer link.Close()
				rawURL = link.URL
			}
		}
	}
	// 获取相关文件
	var related []model.Obj
	parentPath := stdpath.Dir(reqPath)
	sameLevelFiles, err := fs.List(c.Request.Context(), parentPath, &fs.ListArgs{})
	if err == nil {
		related = filterRelated(sameLevelFiles, obj)
	}

	// 获取父目录元数据
	parentMeta, _ := op.GetNearestMeta(parentPath)
	thumb, _ := model.GetThumb(obj)

	// 返回结果
	common.SuccessResp(c, FsGetResp{
		ObjResp: ObjResp{
			Id:          obj.GetID(),
			Path:        obj.GetPath(),
			Name:        obj.GetName(),
			Size:        obj.GetSize(),
			IsDir:       obj.IsDir(),
			Modified:    obj.ModTime(),
			Created:     obj.CreateTime(),
			HashInfoStr: obj.GetHash().String(),
			HashInfo:    obj.GetHash().Export(),
			Sign:        common.Sign(obj, parentPath, isEncrypt(meta, reqPath)),
			Type:        utils.GetFileType(obj.GetName()),
			Thumb:       thumb,
		},
		RawURL:   rawURL,
		Readme:   getReadme(meta, reqPath),
		Header:   getHeader(meta, reqPath),
		Provider: provider,
		Related:  toObjsResp(related, parentPath, isEncrypt(parentMeta, parentPath)),
	})
}

// filterRelated 过滤相关文件
func filterRelated(objs []model.Obj, obj model.Obj) []model.Obj {
	var related []model.Obj
	nameWithoutExt := strings.TrimSuffix(obj.GetName(), stdpath.Ext(obj.GetName()))

	for _, o := range objs {
		if o.GetName() == obj.GetName() {
			continue
		}
		if strings.HasPrefix(o.GetName(), nameWithoutExt) {
			related = append(related, o)
		}
	}

	return related
}

// FsOtherReq 其他文件系统操作请求参数
type FsOtherReq struct {
	model.FsOtherArgs
	Password string `json:"password" form:"password"`
}

// FsOther 处理其他文件系统操作
func FsOther(c *gin.Context) {
	var req FsOtherReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取用户和路径
	user := c.Value(consts.UserKey).(*model.User)
	var err error
	req.Path, err = user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 获取元数据
	meta, err := op.GetNearestMeta(req.Path)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500)
		return
	}
	common.GinWithValue(c, consts.MetaKey, meta)

	// 检查访问权限
	if !common.CanAccess(user, meta, req.Path, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}

	// 执行其他操作
	res, err := fs.Other(c.Request.Context(), req.FsOtherArgs)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, res)
}