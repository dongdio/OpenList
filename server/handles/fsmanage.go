package handles

import (
	"fmt"
	stdpath "path"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/sign"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/generic"
	"github.com/dongdio/OpenList/v4/utility/task"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// MkdirOrLinkReq 创建目录或获取链接请求
type MkdirOrLinkReq struct {
	Path string `json:"path" form:"path" binding:"required"`
}

// FsMkdir 创建目录处理函数
func FsMkdir(c *gin.Context) {
	var req MkdirOrLinkReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	user := c.Value(consts.UserKey).(*model.User)
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 检查用户权限
	if !user.CanWrite() {
		meta, err := op.GetNearestMeta(stdpath.Dir(reqPath))
		if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			common.ErrorResp(c, err, 500, true)
			return
		}

		if !common.CanWrite(meta, reqPath) {
			common.ErrorResp(c, errs.PermissionDenied, 403)
			return
		}
	}

	if err = fs.MakeDir(c.Request.Context(), reqPath); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}

// MoveCopyReq 移动或复制请求
type MoveCopyReq struct {
	SrcDir    string   `json:"src_dir" binding:"required"`
	DstDir    string   `json:"dst_dir" binding:"required"`
	Names     []string `json:"names" binding:"required"`
	Overwrite bool     `json:"overwrite"`
}

// FsMove 文件移动处理函数
func FsMove(c *gin.Context) {
	var req MoveCopyReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	if len(req.Names) == 0 {
		common.ErrorStrResp(c, "Empty file names", 400)
		return
	}

	user := c.Value(consts.UserKey).(*model.User)
	if !user.CanMove() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	srcDir, err := user.JoinPath(req.SrcDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	dstDir, err := user.JoinPath(req.DstDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	ctx := c.Request.Context()
	if !req.Overwrite {
		for _, name := range req.Names {
			if res, _ := fs.Get(ctx, stdpath.Join(dstDir, name), &fs.GetArgs{NoLog: true}); res != nil {
				common.ErrorStrResp(c, fmt.Sprintf("file [%s] exists", name), 403)
				return
			}
		}
	}

	// 创建所有任务，所有验证将在后台异步进行
	addedTasks := make([]task.TaskExtensionInfo, 0, len(req.Names))
	for i, name := range req.Names {
		// 最后一个参数表示是否懒加载缓存（当有多个文件时）
		isLazyCache := len(req.Names) > i+1
		t, err := fs.MoveWithTaskAndValidation(ctx, stdpath.Join(srcDir, name), dstDir, !req.Overwrite, isLazyCache)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}

		if t != nil {
			addedTasks = append(addedTasks, t)
		}
	}

	if len(addedTasks) > 0 {
		common.SuccessResp(c, gin.H{
			"message": fmt.Sprintf("Successfully created %d move task(s)", len(addedTasks)),
			"tasks":   getTaskInfos(addedTasks),
		})
		return
	}

	common.SuccessResp(c, gin.H{
		"message": "Move operations completed immediately",
	})
}

// FsCopy 文件复制处理函数
func FsCopy(c *gin.Context) {
	var req MoveCopyReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	if len(req.Names) == 0 {
		common.ErrorStrResp(c, "Empty file names", 400)
		return
	}

	user := c.Value(consts.UserKey).(*model.User)
	if !user.CanCopy() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	srcDir, err := user.JoinPath(req.SrcDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	dstDir, err := user.JoinPath(req.DstDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	ctx := c.Request.Context()
	if !req.Overwrite {
		for _, name := range req.Names {
			if res, _ := fs.Get(ctx, stdpath.Join(dstDir, name), &fs.GetArgs{NoLog: true}); res != nil {
				common.ErrorStrResp(c, fmt.Sprintf("file [%s] exists", name), 403)
				return
			}
		}
	}

	// 创建所有任务，所有验证将在后台异步进行
	addedTasks := make([]task.TaskExtensionInfo, 0, len(req.Names))
	for i, name := range req.Names {
		// 最后一个参数表示是否懒加载缓存（当有多个文件时）
		isLazyCache := len(req.Names) > i+1
		t, err := fs.Copy(ctx, stdpath.Join(srcDir, name), dstDir, isLazyCache)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}

		if t != nil {
			addedTasks = append(addedTasks, t)
		}
	}

	// 立即返回任务信息
	if len(addedTasks) > 0 {
		common.SuccessResp(c, gin.H{
			"message": fmt.Sprintf("Successfully created %d copy task(s)", len(addedTasks)),
			"tasks":   getTaskInfos(addedTasks),
		})
	} else {
		common.SuccessResp(c, gin.H{
			"message": "Copy operations completed immediately",
		})
	}
}

// RenameReq 重命名请求
type RenameReq struct {
	Path      string `json:"path" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Overwrite bool   `json:"overwrite"`
}

// FsRename 文件重命名处理函数
func FsRename(c *gin.Context) {
	var req RenameReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	user := c.Value(consts.UserKey).(*model.User)
	if !user.CanRename() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	ctx := c.Request.Context()
	// 检查是否存在同名文件（如果不允许覆盖）
	if !req.Overwrite {
		dstPath := stdpath.Join(stdpath.Dir(reqPath), req.Name)
		if dstPath != reqPath {
			if res, _ := fs.Get(ctx, dstPath, &fs.GetArgs{NoLog: true}); res != nil {
				common.ErrorStrResp(c, fmt.Sprintf("file [%s] exists", req.Name), 403)
				return
			}
		}
	}

	if err = fs.Rename(ctx, reqPath, req.Name); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}

// RemoveReq 删除文件请求
type RemoveReq struct {
	Dir   string   `json:"dir" binding:"required"`
	Names []string `json:"names" binding:"required"`
}

// FsRemove 文件删除处理函数
func FsRemove(c *gin.Context) {
	var req RemoveReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	if len(req.Names) == 0 {
		common.ErrorStrResp(c, "Empty file names", 400)
		return
	}

	user := c.Value(consts.UserKey).(*model.User)
	if !user.CanRemove() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	reqDir, err := user.JoinPath(req.Dir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	ctx := c.Request.Context()
	for _, name := range req.Names {
		filePath := stdpath.Join(reqDir, name)
		err = fs.Remove(ctx, filePath)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
	}

	common.SuccessResp(c)
}

// RemoveEmptyDirectoryReq 删除空目录请求
type RemoveEmptyDirectoryReq struct {
	SrcDir string `json:"src_dir" binding:"required"`
}

// FsRemoveEmptyDirectory 删除空目录处理函数
func FsRemoveEmptyDirectory(c *gin.Context) {
	var req RemoveEmptyDirectoryReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	user := c.Value(consts.UserKey).(*model.User)
	if !user.CanRemove() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	srcDir, err := user.JoinPath(req.SrcDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	meta, err := op.GetNearestMeta(srcDir)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500, true)
		return
	}
	common.GinWithValue(c, consts.MetaKey, meta)

	ctx := c.Request.Context()
	rootFiles, err := fs.List(ctx, srcDir, &fs.ListArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 记录文件路径
	filePathMap := make(map[model.Obj]string)
	// 记录父文件
	fileParentMap := make(map[model.Obj]model.Obj)
	// 待删除文件队列
	removingFiles := generic.NewQueue[model.Obj]()
	// 已删除文件记录
	removedFiles := make(map[string]bool)

	// 初始化队列，添加所有顶层目录
	for _, file := range rootFiles {
		if !file.IsDir() {
			continue
		}
		removingFiles.Push(file)
		filePathMap[file] = srcDir
	}

	// 递归处理空目录
	for !removingFiles.IsEmpty() {
		removingFile := removingFiles.Pop()
		removingFilePath := stdpath.Join(filePathMap[removingFile], removingFile.GetName())

		if removedFiles[removingFilePath] {
			continue
		}

		subFiles, err := fs.List(ctx, removingFilePath, &fs.ListArgs{Refresh: true})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}

		if len(subFiles) == 0 {
			// 删除空目录
			err = fs.Remove(ctx, removingFilePath)
			if err != nil {
				common.ErrorResp(c, err, 500)
				return
			}
			removedFiles[removingFilePath] = true

			// 重新检查父文件夹
			parentFile, exist := fileParentMap[removingFile]
			if exist {
				removingFiles.Push(parentFile)
			}
		} else {
			// 递归处理子目录
			for _, subFile := range subFiles {
				if !subFile.IsDir() {
					continue
				}
				removingFiles.Push(subFile)
				filePathMap[subFile] = removingFilePath
				fileParentMap[subFile] = removingFile
			}
		}
	}

	common.SuccessResp(c)
}

// Link 返回真实链接，仅供代理程序使用，可能包含cookie，因此仅允许管理员使用
func Link(c *gin.Context) {
	var req MkdirOrLinkReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 不需要连接base_path，因为它始终是完整路径
	rawPath := req.Path
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 如果是仅本地存储，生成签名URL
	if storage.Config().NoLinkURL || storage.Config().OnlyLinkMFile {
		common.SuccessResp(c, model.Link{
			URL: fmt.Sprintf("%s/p%s?d&sign=%s",
				common.GetApiUrl(c),
				utils.EncodePath(rawPath, true),
				sign.Sign(rawPath)),
		})
		return
	}

	// 获取存储链接
	link, _, err := fs.Link(c.Request.Context(), rawPath, model.LinkArgs{IP: c.ClientIP(), Header: c.Request.Header, Redirect: true})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	defer link.Close()
	common.SuccessResp(c, link)
}