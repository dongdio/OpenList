package handles

import (
	"fmt"
	stdpath "path"
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/internal/fs"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/pkg/errs"
	"github.com/dongdio/OpenList/pkg/generic"
	"github.com/dongdio/OpenList/server/common"
)

// RecursiveMoveReq 递归移动请求参数
type RecursiveMoveReq struct {
	SrcDir         string `json:"src_dir" binding:"required"`         // 源目录
	DstDir         string `json:"dst_dir" binding:"required"`         // 目标目录
	ConflictPolicy string `json:"conflict_policy" binding:"required"` // 冲突处理策略
}

// FsRecursiveMove 处理递归移动文件请求
func FsRecursiveMove(c *gin.Context) {
	var req RecursiveMoveReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证冲突处理策略
	if req.ConflictPolicy != OVERWRITE && req.ConflictPolicy != SKIP && req.ConflictPolicy != CANCEL {
		common.ErrorStrResp(c, "invalid conflict policy, must be one of: overwrite, skip, cancel", 400)
		return
	}

	// 获取用户并验证权限
	user := c.MustGet("user").(*model.User)
	if !user.CanMove() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	// 获取完整路径
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

	// 检查源目录和目标目录是否相同
	if srcDir == dstDir {
		common.ErrorStrResp(c, "source and destination directories are the same", 400)
		return
	}

	// 检查目标目录是否是源目录的子目录
	if strings.HasPrefix(dstDir, srcDir+"/") {
		common.ErrorStrResp(c, "cannot move a directory to its subdirectory", 400)
		return
	}

	// 获取元数据
	meta, err := op.GetNearestMeta(srcDir)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500, true)
		return
	}
	c.Set("meta", meta)

	// 获取源目录下的文件列表
	rootFiles, err := fs.List(c, srcDir, &fs.ListArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 如果不是覆盖策略，则获取目标目录下的文件列表用于冲突检查
	var existingFileNames []string
	if req.ConflictPolicy != OVERWRITE {
		dstFiles, err := fs.List(c, dstDir, &fs.ListArgs{})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		existingFileNames = make([]string, 0, len(dstFiles))
		for _, dstFile := range dstFiles {
			existingFileNames = append(existingFileNames, dstFile.GetName())
		}
	}

	// 记录文件路径并准备移动队列
	filePathMap := make(map[model.Obj]string)
	movingFiles := generic.NewQueue[model.Obj]()
	movingFileNames := make([]string, 0, len(rootFiles))

	// 初始化队列
	for _, file := range rootFiles {
		movingFiles.Push(file)
		filePathMap[file] = srcDir
	}

	// 广度优先遍历处理所有文件
	for !movingFiles.IsEmpty() {
		movingFile := movingFiles.Pop()
		movingFilePath := filePathMap[movingFile]
		movingFileName := stdpath.Join(movingFilePath, movingFile.GetName())

		if movingFile.IsDir() {
			// 如果是目录，递归处理子文件
			subFilePath := movingFileName
			subFiles, err := fs.List(c, movingFileName, &fs.ListArgs{Refresh: true})
			if err != nil {
				common.ErrorResp(c, err, 500)
				return
			}

			for _, subFile := range subFiles {
				movingFiles.Push(subFile)
				filePathMap[subFile] = subFilePath
			}
			continue
		}
		// 如果是文件，处理移动逻辑

		// 如果源路径和目标路径相同，跳过
		if movingFilePath == dstDir {
			continue
		}

		// 处理文件冲突
		if slices.Contains(existingFileNames, movingFile.GetName()) {
			switch req.ConflictPolicy {
			case CANCEL:
				common.ErrorStrResp(c, fmt.Sprintf("file [%s] exists", movingFile.GetName()), 403)
				return
			case SKIP:
				continue
			}
		} else if req.ConflictPolicy != OVERWRITE {
			// 记录文件名以检测后续冲突
			existingFileNames = append(existingFileNames, movingFile.GetName())
		}

		// 添加到待移动文件列表
		movingFileNames = append(movingFileNames, movingFileName)
	}

	// 执行文件移动
	movedCount := 0
	totalFiles := len(movingFileNames)

	for i, fileName := range movingFileNames {
		// 是否是最后一个文件
		isLast := i >= totalFiles-1

		// 移动文件
		err := fs.Move(c, fileName, dstDir, !isLast)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		movedCount++
	}

	// 返回成功响应
	common.SuccessWithMsgResp(c, fmt.Sprintf("Successfully moved %d %s",
		movedCount, common.Pluralize(movedCount, "file", "files")))
}

// BatchRenameReq 批量重命名请求参数
type BatchRenameReq struct {
	SrcDir        string `json:"src_dir" binding:"required"` // 源目录
	RenameObjects []struct {
		SrcName string `json:"src_name" binding:"required"` // 原文件名
		NewName string `json:"new_name" binding:"required"` // 新文件名
	} `json:"rename_objects" binding:"required"` // 重命名对象列表
}

// FsBatchRename 处理批量重命名请求
func FsBatchRename(c *gin.Context) {
	var req BatchRenameReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证请求参数
	if len(req.RenameObjects) == 0 {
		common.ErrorStrResp(c, "rename_objects cannot be empty", 400)
		return
	}

	// 获取用户并验证权限
	user := c.MustGet("user").(*model.User)
	if !user.CanRename() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	// 获取完整路径
	reqPath, err := user.JoinPath(req.SrcDir)
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

	// 执行重命名操作
	renamedCount := 0
	for _, renameObject := range req.RenameObjects {
		if renameObject.SrcName == "" || renameObject.NewName == "" {
			continue
		}

		// 构建文件路径
		filePath := stdpath.Join(reqPath, renameObject.SrcName)

		// 执行重命名
		if err := fs.Rename(c, filePath, renameObject.NewName); err != nil {
			common.ErrorResp(c, err, 500)
			return
		}

		renamedCount++
	}

	// 返回成功响应
	common.SuccessWithMsgResp(c, fmt.Sprintf("Successfully renamed %d %s",
		renamedCount, common.Pluralize(renamedCount, "file", "files")))
}

// RegexRenameReq 正则表达式重命名请求参数
type RegexRenameReq struct {
	SrcDir       string `json:"src_dir" binding:"required"`        // 源目录
	SrcNameRegex string `json:"src_name_regex" binding:"required"` // 源文件名正则表达式
	NewNameRegex string `json:"new_name_regex" binding:"required"` // 新文件名正则表达式
}

// FsRegexRename 处理正则表达式重命名请求
func FsRegexRename(c *gin.Context) {
	var req RegexRenameReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取用户并验证权限
	user := c.MustGet("user").(*model.User)
	if !user.CanRename() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	// 获取完整路径
	reqPath, err := user.JoinPath(req.SrcDir)
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

	// 编译源文件名正则表达式
	srcRegexp, err := regexp.Compile(req.SrcNameRegex)
	if err != nil {
		common.ErrorResp(c, fmt.Errorf("invalid source name regex: %w", err), 400)
		return
	}

	// 获取目录下的文件列表
	files, err := fs.List(c, reqPath, &fs.ListArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 执行重命名操作
	renamedCount := 0
	for _, file := range files {
		// 检查文件名是否匹配正则表达式
		if !srcRegexp.MatchString(file.GetName()) {
			continue
		}
		// 构建文件路径
		filePath := stdpath.Join(reqPath, file.GetName())
		// 生成新文件名
		newFileName := srcRegexp.ReplaceAllString(file.GetName(), req.NewNameRegex)

		// 如果新旧文件名相同，跳过
		if newFileName == file.GetName() {
			continue
		}
		// 执行重命名
		if err := fs.Rename(c, filePath, newFileName); err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		renamedCount++
	}

	// 返回成功响应
	common.SuccessWithMsgResp(c, fmt.Sprintf("Successfully renamed %d %s using regex",
		renamedCount, common.Pluralize(renamedCount, "file", "files")))
}
