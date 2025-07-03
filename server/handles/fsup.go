package handles

import (
	"io"
	"net/url"
	stdpath "path"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/task"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 获取上传文件的最后修改时间
// 从请求头中的Last-Modified字段获取，如果不存在则返回当前时间
func getLastModified(c *gin.Context) time.Time {
	now := time.Now()
	lastModifiedStr := c.GetHeader("Last-Modified")
	if lastModifiedStr == "" {
		return now
	}

	lastModifiedMillisecond, err := strconv.ParseInt(lastModifiedStr, 10, 64)
	if err != nil {
		return now
	}

	return time.UnixMilli(lastModifiedMillisecond)
}

// 从请求头中获取文件哈希信息
func getFileHashes(c *gin.Context) map[*utils.HashType]string {
	hashes := make(map[*utils.HashType]string)

	if md5 := c.GetHeader("X-File-Md5"); md5 != "" {
		hashes[utils.MD5] = md5
	}

	if sha1 := c.GetHeader("X-File-Sha1"); sha1 != "" {
		hashes[utils.SHA1] = sha1
	}

	if sha256 := c.GetHeader("X-File-Sha256"); sha256 != "" {
		hashes[utils.SHA256] = sha256
	}

	return hashes
}

// FsStream 处理流式上传文件请求
func FsStream(c *gin.Context) {
	// 获取上传路径
	path := c.GetHeader("File-Path")
	if path == "" {
		common.ErrorStrResp(c, "missing File-Path header", 400)
		return
	}

	// URL解码路径
	path, err := url.PathUnescape(path)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取上传选项
	asTask := c.GetHeader("As-Task") == "true"
	overwrite := c.GetHeader("Overwrite") != "false"

	// 获取用户并验证路径
	user := c.MustGet("user").(*model.User)
	path, err = user.JoinPath(path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 检查文件是否存在（如果不允许覆盖）
	if !overwrite {
		if res, _ := fs.Get(c, path, &fs.GetArgs{NoLog: true}); res != nil {
			// 丢弃请求体数据
			_, _ = utils.CopyWithBuffer(io.Discard, c.Request.Body)
			common.ErrorStrResp(c, "file exists", 403)
			return
		}
	}

	// 分离目录和文件名
	dir, name := stdpath.Split(path)

	// 获取文件大小
	sizeStr := c.GetHeader("Content-Length")
	if sizeStr == "" {
		sizeStr = "0"
	}

	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取文件哈希信息
	hashes := getFileHashes(c)

	// 获取MIME类型
	mimetype := c.GetHeader("Content-Type")
	if len(mimetype) == 0 {
		mimetype = utils.GetMimeType(name)
	}

	// 创建文件流对象
	s := &stream.FileStream{
		Obj: &model.Object{
			Name:     name,
			Size:     size,
			Modified: getLastModified(c),
			HashInfo: utils.NewHashInfoByMap(hashes),
		},
		Reader:       c.Request.Body,
		Mimetype:     mimetype,
		WebPutAsTask: asTask,
	}

	// 根据是否作为任务执行上传
	var t task.TaskExtensionInfo
	if asTask {
		t, err = fs.PutAsTask(c, dir, s)
	} else {
		err = fs.PutDirectly(c, dir, s, true)
	}

	// 确保请求体被关闭
	defer c.Request.Body.Close()

	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 处理上传结果
	if t == nil {
		// 确保读取完所有请求体数据
		if n, _ := io.ReadFull(c.Request.Body, []byte{0}); n == 1 {
			_, _ = utils.CopyWithBuffer(io.Discard, c.Request.Body)
		}
		common.SuccessResp(c)
		return
	}

	// 返回任务信息
	common.SuccessResp(c, gin.H{
		"task": getTaskInfo(t),
	})
}

// FsForm 处理表单上传文件请求
func FsForm(c *gin.Context) {
	// 获取上传路径
	path := c.GetHeader("File-Path")
	if path == "" {
		common.ErrorStrResp(c, "missing File-Path header", 400)
		return
	}

	// URL解码路径
	path, err := url.PathUnescape(path)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取上传选项
	asTask := c.GetHeader("As-Task") == "true"
	overwrite := c.GetHeader("Overwrite") != "false"

	// 获取用户并验证路径
	user := c.MustGet("user").(*model.User)
	path, err = user.JoinPath(path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 检查文件是否存在（如果不允许覆盖）
	if !overwrite {
		if res, _ := fs.Get(c, path, &fs.GetArgs{NoLog: true}); res != nil {
			_, _ = utils.CopyWithBuffer(io.Discard, c.Request.Body)
			common.ErrorStrResp(c, "file exists", 403)
			return
		}
	}

	// 检查存储是否支持上传
	storage, err := fs.GetStorage(path, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	if storage.Config().NoUpload {
		common.ErrorStrResp(c, "Current storage doesn't support upload", 405)
		return
	}

	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 打开文件
	f, err := file.Open()
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	defer f.Close()

	// 分离目录和文件名
	dir, name := stdpath.Split(path)

	// 获取文件哈希信息
	hashes := getFileHashes(c)

	// 获取MIME类型
	mimetype := file.Header.Get("Content-Type")
	if len(mimetype) == 0 {
		mimetype = utils.GetMimeType(name)
	}

	// 创建文件流对象
	s := stream.FileStream{
		Obj: &model.Object{
			Name:     name,
			Size:     file.Size,
			Modified: getLastModified(c),
			HashInfo: utils.NewHashInfoByMap(hashes),
		},
		Reader:       f,
		Mimetype:     mimetype,
		WebPutAsTask: asTask,
	}

	// 根据是否作为任务执行上传
	var t task.TaskExtensionInfo
	if asTask {
		// 包装Reader以避免关闭
		s.Reader = struct {
			io.Reader
		}{f}
		t, err = fs.PutAsTask(c, dir, &s)
	} else {
		err = fs.PutDirectly(c, dir, &s, true)
	}

	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 处理上传结果
	if t == nil {
		common.SuccessResp(c)
		return
	}

	// 返回任务信息
	common.SuccessResp(c, gin.H{
		"task": getTaskInfo(t),
	})
}