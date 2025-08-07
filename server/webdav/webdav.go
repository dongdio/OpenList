// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package webdav 提供WebDAV服务器实现
// WebDAV(Web-based Distributed Authoring and Versioning)是HTTP协议的扩展，
// 允许客户端在Web服务器上执行远程内容管理操作
package webdav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/net"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 添加一个字节池来重用缓冲区
var bufferPool = sync.Pool{
	New: func() any {
		// 32KB 是一个比较合适的缓冲区大小，适用于大多数文件传输场景
		buffer := make([]byte, 32*1024)
		return &buffer
	},
}

// getBuffer 从池中获取缓冲区
func getBuffer() *[]byte {
	return bufferPool.Get().(*[]byte)
}

// putBuffer 将缓冲区放回池中
func putBuffer(buf *[]byte) {
	bufferPool.Put(buf)
}

// 添加全局并发限制和缓存控制
var (
	// 最大并发连接数
	maxConcurrentConnections = 100

	// 并发连接计数器
	concurrentConnections int32

	// 并发连接锁
	connectionsMutex sync.Mutex

	// 文件信息缓存
	fileInfoCache = sync.Map{}

	// 缓存过期时间（秒）
	cacheExpiration int64 = 30
)

// acquireConnection 获取连接资源，如果超过最大并发数则返回错误
func acquireConnection() error {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()

	if concurrentConnections >= int32(maxConcurrentConnections) {
		return errs.New("服务器繁忙，请稍后再试")
	}

	concurrentConnections++
	return nil
}

// releaseConnection 释放连接资源
func releaseConnection() {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()

	if concurrentConnections > 0 {
		concurrentConnections--
	}
}

// cacheKey 生成缓存键
func cacheKey(path string) string {
	return "webdav:" + path
}

// getCachedFileInfo 从缓存获取文件信息
func getCachedFileInfo(ctx context.Context, path string) (model.Obj, bool) {
	key := cacheKey(path)
	if val, ok := fileInfoCache.Load(key); ok {
		cacheItem := val.(struct {
			info      model.Obj
			timestamp int64
		})

		// 检查缓存是否过期
		if time.Now().Unix()-cacheItem.timestamp < cacheExpiration {
			return cacheItem.info, true
		}

		// 缓存过期，删除
		fileInfoCache.Delete(key)
	}

	return nil, false
}

// setCachedFileInfo 设置文件信息缓存
func setCachedFileInfo(path string, info model.Obj) {
	key := cacheKey(path)
	fileInfoCache.Store(key, struct {
		info      model.Obj
		timestamp int64
	}{
		info:      info,
		timestamp: time.Now().Unix(),
	})
}

// clearFileInfoCache 清除指定路径的文件信息缓存
func clearFileInfoCache(path string) {
	key := cacheKey(path)
	fileInfoCache.Delete(key)
}

// Handler 实现WebDAV协议的HTTP处理器
type Handler struct {
	// Prefix 是要从WebDAV资源路径中删除的URL路径前缀
	// 用于在子目录中挂载WebDAV服务
	Prefix string

	// LockSystem 是锁管理系统
	// 用于支持WebDAV的锁定功能
	LockSystem LockSystem

	// Logger 是可选的错误日志记录器
	// 如果非nil，将为所有HTTP请求调用它来记录错误
	Logger func(*http.Request, error)
}

// stripPrefix 从请求路径中删除配置的前缀
//
// 参数:
//   - p: 原始请求路径
//
// 返回:
//   - string: 处理后的路径
//   - int: HTTP状态码，成功时为http.StatusOK
//   - error: 错误信息，如果前缀不匹配则返回errPrefixMismatch
func (h *Handler) stripPrefix(p string) (string, int, error) {
	// 如果没有设置前缀，直接返回原始路径
	if h.Prefix == "" {
		return p, http.StatusOK, nil
	}

	// 检查路径是否以前缀开头
	if !strings.HasPrefix(p, h.Prefix) {
		return p, http.StatusNotFound, errPrefixMismatch
	}

	// 删除前缀并返回结果
	resultPath := strings.TrimPrefix(p, h.Prefix)
	return resultPath, http.StatusOK, nil
}

// ServeHTTP 处理所有WebDAV请求
// 实现http.Handler接口
//
// 参数:
//   - w: HTTP响应写入器
//   - r: HTTP请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 应用并发限制
	if err := acquireConnection(); err != nil {
		h.writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	defer releaseConnection()

	// 默认状态和错误
	status, err := http.StatusBadRequest, errUnsupportedMethod

	// 检查锁系统是否已配置
	if h.LockSystem == nil {
		status, err = http.StatusInternalServerError, errNoLockSystem
		h.writeError(w, status, err)
		return
	}

	// 创建缓冲响应写入器，用于延迟发送响应
	brw := NewBufferedResponseWriter()
	useBufferedWriter := true

	// 根据HTTP方法分发到对应的处理函数
	switch r.Method {
	case "OPTIONS":
		status, err = h.handleOptions(brw, r)

	case "GET", "HEAD", "POST":
		// 对于获取内容的请求，直接写入响应，不使用缓冲
		useBufferedWriter = false
		responseWriter := &common.WrittenResponseWriter{ResponseWriter: w}
		status, err = h.handleGetHeadPost(responseWriter, r)
		// 如果响应已经写入，不再设置状态码
		if status != 0 && responseWriter.IsWritten() {
			status = 0
		}

	case "DELETE":
		status, err = h.handleDelete(brw, r)

	case "PUT":
		status, err = h.handlePut(brw, r)

	case "MKCOL":
		status, err = h.handleMkcol(brw, r)

	case "COPY", "MOVE":
		status, err = h.handleCopyMove(brw, r)

	case "LOCK":
		status, err = h.handleLock(brw, r)

	case "UNLOCK":
		status, err = h.handleUnlock(brw, r)

	case "PROPFIND":
		status, err = h.handlePropfind(brw, r)
		// 如果PROPFIND出错，将其作为空文件夹呈现给客户端
		if err != nil {
			status = http.StatusNotFound
		}

	case "PROPPATCH":
		status, err = h.handleProppatch(brw, r)
	}

	// 写入响应
	if status != 0 {
		// 如果有状态码，直接写入
		w.WriteHeader(status)
		if status != http.StatusNoContent {
			w.Write([]byte(StatusText(status)))
		}
	} else if useBufferedWriter {
		// 否则将缓冲的响应写入
		brw.WriteToResponse(w)
	}

	// 记录错误
	if h.Logger != nil && err != nil {
		h.Logger(r, err)
	}
}

// writeError 向响应写入错误信息
func (h *Handler) writeError(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		w.Write([]byte(StatusText(status)))
	}
	if h.Logger != nil && err != nil {
		h.Logger(nil, err)
	}
}

// lock 创建一个资源的锁
// 用于在无显式锁头的情况下确保资源安全
//
// 参数:
//   - now: 当前时间
//   - root: 要锁定的资源路径
//
// 返回:
//   - string: 锁令牌
//   - int: HTTP状态码，成功时为0
//   - error: 错误信息
func (h *Handler) lock(now time.Time, root string) (token string, status int, err error) {
	// 使用无限超时创建一个零深度锁
	token, err = h.LockSystem.Create(now, LockDetails{
		Root:      root,
		Duration:  infiniteTimeout,
		ZeroDepth: true,
	})

	if err != nil {
		if errs.Is(err, ErrLocked) {
			// 资源已被锁定
			return "", StatusLocked, err
		}
		// 其他错误
		return "", http.StatusInternalServerError, err
	}

	return token, 0, nil
}

// confirmLocks 确认请求可以访问指定的资源
// 处理WebDAV的If头部，检查锁定条件
//
// 参数:
//   - r: HTTP请求
//   - src: 源资源路径
//   - dst: 目标资源路径（可选）
//
// 返回:
//   - func(): 释放函数，调用后释放临时锁
//   - int: HTTP状态码，成功时为0
//   - error: 错误信息
func (h *Handler) confirmLocks(r *http.Request, src, dst string) (release func(), status int, err error) {
	// 获取If头部
	header := r.Header.Get("If")

	if header == "" {
		// 如果If头部为空，表示客户端未创建锁
		// 但我们仍需检查资源是否被其他客户端锁定
		// 为此创建临时锁并在请求结束时释放
		now, srcToken, dstToken := time.Now(), "", ""

		// 如果提供了源路径，尝试锁定它
		if src != "" {
			srcToken, status, err = h.lock(now, src)
			if err != nil {
				return nil, status, err
			}
		}

		// 如果提供了目标路径，尝试锁定它
		if dst != "" {
			dstToken, status, err = h.lock(now, dst)
			if err != nil {
				// 如果目标锁定失败，释放源锁
				if srcToken != "" {
					h.LockSystem.Unlock(now, srcToken)
				}
				return nil, status, err
			}
		}

		// 返回释放函数，用于请求结束时释放临时锁
		return func() {
			if dstToken != "" {
				h.LockSystem.Unlock(now, dstToken)
			}
			if srcToken != "" {
				h.LockSystem.Unlock(now, srcToken)
			}
		}, 0, nil
	}

	// 解析If头部
	parsedIfHeader, ok := parseIfHeader(header)
	if !ok {
		return nil, http.StatusBadRequest, errInvalidIfHeader
	}

	// If头部是ifLists的逻辑或(OR)，任何一个ifList匹配即可
	for _, item := range parsedIfHeader.lists {
		// 确定源资源路径
		lockSrc := item.resourceTag
		if lockSrc == "" {
			// 如果没有资源标签，使用请求的源路径
			lockSrc = src
		} else {
			// 否则解析URL并确保主机匹配
			parsedURL, err := url.Parse(lockSrc)
			if err != nil {
				continue
			}
			if parsedURL.Host != r.Host {
				continue
			}
			// 移除前缀并检查路径
			lockSrc, status, err = h.stripPrefix(parsedURL.Path)
			if err != nil {
				return nil, status, err
			}
		}

		// 尝试确认锁
		release, err = h.LockSystem.Confirm(time.Now(), lockSrc, dst, item.conditions...)
		if errs.Is(err, ErrConfirmationFailed) {
			// 如果确认失败，尝试下一个列表
			continue
		}
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}

		// 确认成功
		return release, 0, nil
	}

	// WebDAV规范10.4.1节指出，如果评估此头部且所有状态列表都失败，
	// 则请求必须失败，状态为412(Precondition Failed)。
	// 我们遵循规范，即使litmus测试中的cond_put_corrupt_token测试用例
	// 在看到412而不是423(Locked)时会发出警告。
	return nil, http.StatusPreconditionFailed, ErrLocked
}

// getUserAndPath 从请求中获取用户信息并处理路径
// 这个辅助函数用于减少代码重复，统一处理用户信息获取和路径处理
//
// 参数:
//   - ctx: 请求上下文
//   - reqPath: 请求路径
//
// 返回:
//   - string: 处理后的完整路径
//   - *model.User: 用户信息
//   - int: HTTP状态码，成功时为0
//   - error: 错误信息
func getUserAndPath(ctx context.Context, reqPath string) (string, *model.User, int, error) {
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok || user == nil {
		return "", nil, http.StatusUnauthorized, errs.New("未找到用户信息")
	}

	fullPath, err := user.JoinPath(reqPath)
	if err != nil {
		return "", user, http.StatusForbidden, errs.Wrap(err, "无法访问请求路径")
	}

	return fullPath, user, 0, nil
}

// getFileInfo 获取文件信息，并统一处理错误
//
// 参数:
//   - ctx: 请求上下文
//   - path: 文件路径
//
// 返回:
//   - model.Obj: 文件信息
//   - int: HTTP状态码，成功时为0
//   - error: 错误信息
func getFileInfo(ctx context.Context, path string) (model.Obj, int, error) {
	// 尝试从缓存获取
	if info, found := getCachedFileInfo(ctx, path); found {
		return info, 0, nil
	}

	// 缓存未命中，从文件系统获取
	fi, err := fs.Get(ctx, path, &fs.GetArgs{})
	if err != nil {
		if errs.IsObjectNotFound(err) {
			return nil, http.StatusNotFound, errs.Wrap(err, "文件不存在")
		}
		return nil, http.StatusInternalServerError, errs.Wrap(err, "获取文件信息失败")
	}

	// 将结果存入缓存
	setCachedFileInfo(path, fi)

	return fi, 0, nil
}

// handleOptions 处理OPTIONS请求
// 用于发现服务器支持的功能
//
// 参数:
//   - w: HTTP响应写入器
//   - r: HTTP请求
//
// 返回:
//   - int: HTTP状态码
//   - error: 错误信息
func (h *Handler) handleOptions(w http.ResponseWriter, r *http.Request) (status int, err error) {
	// 移除路径前缀
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, errs.Wrap(err, "路径前缀处理失败")
	}

	// 获取用户信息并处理路径
	ctx := r.Context()
	var pathStatus int
	reqPath, _, pathStatus, err = getUserAndPath(ctx, reqPath)
	if err != nil {
		return pathStatus, err
	}

	// 根据资源类型设置允许的HTTP方法
	allow := "OPTIONS, LOCK, PUT, MKCOL"

	// 获取文件信息
	fi, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err == nil {
		if fi.IsDir() {
			allow = "OPTIONS, LOCK, DELETE, PROPPATCH, COPY, MOVE, UNLOCK, PROPFIND"
		} else {
			allow = "OPTIONS, LOCK, GET, HEAD, POST, DELETE, PROPPATCH, COPY, MOVE, UNLOCK, PROPFIND, PUT"
		}
	}

	// 设置响应头
	w.Header().Set("Allow", allow)
	// http://www.webdav.org/specs/rfc4918.html#dav.compliance.classes
	w.Header().Set("DAV", "1, 2")
	// http://msdn.microsoft.com/en-au/library/cc250217.aspx
	w.Header().Set("MS-Author-Via", "DAV")

	return 0, nil
}

func (h *Handler) handleGetHeadPost(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, err
	}

	ctx := r.Context()
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok || user == nil {
		return http.StatusUnauthorized, errs.New("未找到用户信息")
	}

	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return http.StatusForbidden, errs.Wrap(err, "无法访问请求路径")
	}

	fi, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		if errs.IsObjectNotFound(err) {
			return http.StatusNotFound, errs.Wrap(err, "文件不存在")
		}
		return http.StatusInternalServerError, errs.Wrap(err, "获取文件信息失败")
	}

	if fi.IsDir() {
		return http.StatusMethodNotAllowed, errs.New("不能对目录执行此操作")
	}

	storage, _ := fs.GetStorage(reqPath, &fs.GetStoragesArgs{})
	if storage == nil {
		return http.StatusInternalServerError, errs.New("无法获取存储信息")
	}

	// 处理WebDAV 302重定向
	if storage.GetStorage().Webdav302() {
		var link *model.Link
		link, _, err = fs.Link(ctx, reqPath, model.LinkArgs{
			IP:       utils.ClientIP(r),
			Header:   r.Header,
			Redirect: true,
		})
		if err != nil {
			return http.StatusInternalServerError, errs.Wrap(err, "生成链接失败")
		}
		defer link.Close()
		http.Redirect(w, r, link.URL, http.StatusFound)
		return 0, nil
	}

	// 处理WebDAV代理URL
	if storage.GetStorage().WebdavProxyURL() {
		if u := common.GenerateDownProxyURL(storage.GetStorage(), reqPath); u != "" {
			w.Header().Set("Cache-Control", "max-age=0, no-cache, no-store, must-revalidate")
			http.Redirect(w, r, u, http.StatusFound)
			return 0, nil
		}
	}

	// 获取文件链接
	link, _, err := fs.Link(ctx, reqPath, model.LinkArgs{Header: r.Header})
	if err != nil {
		return http.StatusInternalServerError, errs.Wrap(err, "生成文件链接失败")
	}
	defer link.Close()

	// 处理范围请求
	if storage.GetStorage().ProxyRange {
		link = common.ProxyRange(ctx, link, fi.GetSize())
	}

	// 代理文件内容
	err = common.Proxy(w, r, link, fi)
	if err != nil {
		var statusCode net.ErrorHTTPStatusCode
		if errs.As(errs.Unwrap(err), &statusCode) {
			return int(statusCode), errs.Wrapf(err, "代理请求失败，状态码: %d", int(statusCode))
		}
		return http.StatusInternalServerError, errs.Wrap(err, "代理文件内容失败")
	}

	return 0, nil
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, errs.Wrap(err, "路径前缀处理失败")
	}

	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, errs.Wrap(err, "锁确认失败")
	}
	defer release()

	ctx := r.Context()
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok || user == nil {
		return http.StatusUnauthorized, errs.New("未找到用户信息")
	}

	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return http.StatusForbidden, errs.Wrap(err, "无法访问请求路径")
	}

	// 检查文件是否存在
	_, err = fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		if errs.IsObjectNotFound(err) {
			return http.StatusNotFound, errs.Wrap(err, "要删除的文件不存在")
		}
		return http.StatusInternalServerError, errs.Wrap(err, "获取文件信息失败")
	}

	// 执行删除操作
	if err = fs.Remove(ctx, reqPath); err != nil {
		if os.IsPermission(err) {
			return http.StatusForbidden, errs.Wrap(err, "没有删除权限")
		} else if errs.IsObjectNotFound(err) {
			// 可能是并发操作导致的文件已被删除
			return http.StatusNotFound, errs.Wrap(err, "文件已不存在")
		} else if strings.Contains(err.Error(), "not empty") {
			return http.StatusConflict, errs.Wrap(err, "目录不为空")
		}
		return http.StatusInternalServerError, errs.Wrap(err, "删除操作失败")
	}

	// 清除文件缓存
	clearFileInfoCache(reqPath)

	// 清除父目录缓存
	clearFileInfoCache(path.Dir(reqPath))

	return http.StatusNoContent, nil
}

func (h *Handler) handlePut(w http.ResponseWriter, r *http.Request) (status int, err error) {
	defer func() {
		// 读取并丢弃剩余的请求体数据
		if r.Body != nil {
			buffer := getBuffer()
			_, _ = io.CopyBuffer(io.Discard, r.Body, *buffer)
			putBuffer(buffer)
			_ = r.Body.Close()
		}
	}()

	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, errs.Wrap(err, "路径前缀处理失败")
	}
	if reqPath == "" {
		return http.StatusMethodNotAllowed, errs.New("空路径不允许PUT操作")
	}

	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, errs.Wrap(err, "锁确认失败")
	}
	defer release()

	ctx := r.Context()
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok || user == nil {
		return http.StatusUnauthorized, errs.New("未找到用户信息")
	}

	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return http.StatusForbidden, errs.Wrap(err, "无法访问请求路径")
	}

	obj := model.Object{
		Name:     path.Base(reqPath),
		Size:     r.ContentLength,
		Modified: h.getModTime(r),
		Ctime:    h.getCreateTime(r),
	}

	mimetype := r.Header.Get("Content-Type")
	if mimetype == "" {
		mimetype = utils.GetMimeType(reqPath)
	}

	fsStream := &stream.FileStream{
		Obj:      &obj,
		Reader:   r.Body,
		Mimetype: mimetype,
	}

	err = fs.PutDirectly(ctx, path.Dir(reqPath), fsStream)
	if err != nil {
		if errs.IsNotFoundError(err) {
			return http.StatusNotFound, errs.Wrap(err, "目标目录不存在")
		}
		return http.StatusMethodNotAllowed, errs.Wrap(err, "文件上传失败")
	}

	// 清除文件缓存
	clearFileInfoCache(reqPath)

	// 清除父目录缓存
	clearFileInfoCache(path.Dir(reqPath))

	fi, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		fi = &obj
	} else {
		// 更新缓存
		setCachedFileInfo(reqPath, fi)
	}

	etag, err := findETag(ctx, h.LockSystem, reqPath, fi)
	if err != nil {
		return http.StatusInternalServerError, errs.Wrap(err, "无法生成ETag")
	}

	w.Header().Set("Etag", etag)
	return http.StatusCreated, nil
}

func (h *Handler) handleMkcol(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, errs.Wrap(err, "路径前缀处理失败")
	}

	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, errs.Wrap(err, "锁确认失败")
	}
	defer release()

	ctx := r.Context()
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok || user == nil {
		return http.StatusUnauthorized, errs.New("未找到用户信息")
	}

	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return http.StatusForbidden, errs.Wrap(err, "无法访问请求路径")
	}

	// RFC 4918 9.3.1: MKCOL 请求不应包含请求体
	if r.ContentLength > 0 {
		return http.StatusUnsupportedMediaType, errs.New("MKCOL 请求不应包含请求体")
	}

	// RFC 4918 9.3.1: 检查目标路径是否已存在
	if _, err = fs.Get(ctx, reqPath, &fs.GetArgs{}); err == nil {
		return http.StatusMethodNotAllowed, errs.New("目标路径已存在，无法创建")
	}

	// RFC 4918 9.3.1: 检查父目录是否存在
	reqDir := path.Dir(reqPath)
	if _, err = fs.Get(ctx, reqDir, &fs.GetArgs{}); err != nil {
		if errs.IsObjectNotFound(err) {
			return http.StatusConflict, errs.Wrap(err, "父目录不存在")
		}
		return http.StatusInternalServerError, errs.Wrap(err, "检查父目录失败")
	}

	// 创建目录
	if err = fs.MakeDir(ctx, reqPath); err != nil {
		if os.IsNotExist(err) {
			return http.StatusConflict, errs.Wrap(err, "父目录不存在")
		} else if os.IsPermission(err) {
			return http.StatusForbidden, errs.Wrap(err, "没有创建目录的权限")
		} else if strings.Contains(err.Error(), "already exists") {
			return http.StatusMethodNotAllowed, errs.Wrap(err, "目录已存在")
		}
		return http.StatusInternalServerError, errs.Wrap(err, "创建目录失败")
	}

	return http.StatusCreated, nil
}

func (h *Handler) handleCopyMove(w http.ResponseWriter, r *http.Request) (status int, err error) {
	// 检查目标路径
	hdr := r.Header.Get("Destination")
	if hdr == "" {
		return http.StatusBadRequest, errs.Wrap(errInvalidDestination, "缺少目标路径")
	}

	u, err := url.Parse(hdr)
	if err != nil {
		return http.StatusBadRequest, errs.Wrap(errInvalidDestination, "目标路径格式错误")
	}

	if u.Host != "" && u.Host != r.Host {
		return http.StatusBadGateway, errs.Wrap(errInvalidDestination, "不支持跨主机操作")
	}

	// 处理源路径
	src, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, errs.Wrap(err, "处理源路径前缀失败")
	}

	// 处理目标路径
	dst, status, err := h.stripPrefix(u.Path)
	if err != nil {
		return status, errs.Wrap(err, "处理目标路径前缀失败")
	}

	if dst == "" {
		return http.StatusBadGateway, errs.Wrap(errInvalidDestination, "目标路径为空")
	}

	if dst == src {
		return http.StatusForbidden, errs.Wrap(errDestinationEqualsSource, "源路径和目标路径相同")
	}

	// 获取用户信息并处理路径
	ctx := r.Context()
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok || user == nil {
		return http.StatusUnauthorized, errs.New("未找到用户信息")
	}

	src, err = user.JoinPath(src)
	if err != nil {
		return http.StatusForbidden, errs.Wrap(err, "无法访问源路径")
	}

	dst, err = user.JoinPath(dst)
	if err != nil {
		return http.StatusForbidden, errs.Wrap(err, "无法访问目标路径")
	}

	var release func()

	// 根据操作类型处理
	if r.Method == "COPY" {
		// 对于COPY操作，只需要锁定目标路径
		release, status, err = h.confirmLocks(r, "", dst)
		if err != nil {
			return status, errs.Wrap(err, "锁确认失败")
		}
		defer release()

		// 处理深度参数
		depth := infiniteDepth
		if hdr = r.Header.Get("Depth"); hdr != "" {
			depth = parseDepth(hdr)
			if depth != 0 && depth != infiniteDepth {
				return http.StatusBadRequest, errs.Wrap(errInvalidDepth, "COPY操作只支持深度为0或infinity")
			}
		}

		// 执行复制操作
		return copyFiles(ctx, src, dst, r.Header.Get("Overwrite") != "F")
	}

	// 对于MOVE操作，需要锁定源和目标路径
	release, status, err = h.confirmLocks(r, src, dst)
	if err != nil {
		return status, errs.Wrap(err, "锁确认失败")
	}
	defer release()

	// 处理深度参数
	if hdr = r.Header.Get("Depth"); hdr != "" {
		if parseDepth(hdr) != infiniteDepth {
			return http.StatusBadRequest, errs.Wrap(errInvalidDepth, "MOVE操作只支持深度为infinity")
		}
	}

	// 执行移动操作
	return moveFiles(ctx, src, dst, r.Header.Get("Overwrite") == "T")
}

func (h *Handler) handleLock(w http.ResponseWriter, r *http.Request) (retStatus int, retErr error) {
	duration, err := parseTimeout(r.Header.Get("Timeout"))
	if err != nil {
		return http.StatusBadRequest, err
	}
	li, status, err := readLockInfo(r.Body)
	if err != nil {
		return status, err
	}

	ctx := r.Context()
	user := ctx.Value(consts.UserKey).(*model.User)
	token, now, created := "", time.Now(), false
	var ld LockDetails
	if li == (lockInfo{}) {
		// An empty lockInfo means to refresh the lock.
		ih, ok := parseIfHeader(r.Header.Get("If"))
		if !ok {
			return http.StatusBadRequest, errInvalidIfHeader
		}
		if len(ih.lists) == 1 && len(ih.lists[0].conditions) == 1 {
			token = ih.lists[0].conditions[0].Token
		}
		if token == "" {
			return http.StatusBadRequest, errInvalidLockToken
		}
		ld, err = h.LockSystem.Refresh(now, token, duration)
		if err != nil {
			if errs.Is(err, ErrNoSuchLock) {
				return http.StatusPreconditionFailed, err
			}
			return http.StatusInternalServerError, err
		}

	} else {
		// Section 9.10.3 says that "If no Depth header is submitted on a LOCK request,
		// then the request MUST act as if a "Depth:infinity" had been submitted."
		depth := infiniteDepth
		if hdr := r.Header.Get("Depth"); hdr != "" {
			depth = parseDepth(hdr)
			if depth != 0 && depth != infiniteDepth {
				// Section 9.10.3 says that "Values other than 0 or infinity must not be
				// used with the Depth header on a LOCK method".
				return http.StatusBadRequest, errInvalidDepth
			}
		}
		var reqPath string
		reqPath, status, err = h.stripPrefix(r.URL.Path)
		if err != nil {
			return status, err
		}
		reqPath, err = user.JoinPath(reqPath)
		if err != nil {
			return 403, err
		}
		ld = LockDetails{
			Root:      reqPath,
			Duration:  duration,
			OwnerXML:  li.Owner.InnerXML,
			ZeroDepth: depth == 0,
		}
		token, err = h.LockSystem.Create(now, ld)
		if err != nil {
			if errs.Is(err, ErrLocked) {
				return StatusLocked, err
			}
			return http.StatusInternalServerError, err
		}
		defer func() {
			if retErr != nil {
				h.LockSystem.Unlock(now, token)
			}
		}()

		// ??? Why create resource here?
		// // Create the resource if it didn't previously exist.
		// if _, err := h.FileSystem.Stat(ctx, reqPath); err != nil {
		//	f, err := h.FileSystem.OpenFile(ctx, reqPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
		//	if err != nil {
		//		// TODO: detect missing intermediate dirs and return http.StatusConflict?
		//		return http.StatusInternalServerError, err
		//	}
		//	f.Close()
		//	created = true
		// }

		// http://www.webdav.org/specs/rfc4918.html#HEADER_Lock-Token says that the
		// Lock-Token value is a Coded-URL. We add angle brackets.
		w.Header().Set("Lock-Token", "<"+token+">")
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if created {
		// This is "w.WriteHeader(http.StatusCreated)" and not "return
		// http.StatusCreated, nil" because we write our own (XML) response to w
		// and Handler.ServeHTTP would otherwise write "Created".
		w.WriteHeader(http.StatusCreated)
	}
	writeLockInfo(w, token, ld)
	return 0, nil
}

func (h *Handler) handleUnlock(w http.ResponseWriter, r *http.Request) (status int, err error) {
	// http://www.webdav.org/specs/rfc4918.html#HEADER_Lock-Token says that the
	// Lock-Token value is a Coded-URL. We strip its angle brackets.
	t := r.Header.Get("Lock-Token")
	if len(t) < 2 || t[0] != '<' || t[len(t)-1] != '>' {
		return http.StatusBadRequest, errInvalidLockToken
	}
	t = t[1 : len(t)-1]

	switch err = h.LockSystem.Unlock(time.Now(), t); err {
	case nil:
		return http.StatusNoContent, err
	case ErrForbidden:
		return http.StatusForbidden, err
	case ErrLocked:
		return StatusLocked, err
	case ErrNoSuchLock:
		return http.StatusConflict, err
	default:
		return http.StatusInternalServerError, err
	}
}

func (h *Handler) handlePropfind(w http.ResponseWriter, r *http.Request) (status int, err error) {
	// 移除路径前缀
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, errs.Wrap(err, "路径前缀处理失败")
	}

	// 获取用户信息和处理路径
	var userAgent = r.Header.Get("User-Agent")
	ctx := context.WithValue(r.Context(), consts.UserAgentKey, userAgent)

	reqPath, user, status, err := getUserAndPath(ctx, reqPath)
	if err != nil {
		return status, err
	}

	// 获取文件信息
	fi, status, err := getFileInfo(ctx, reqPath)
	if err != nil {
		return status, err
	}

	// 处理深度参数
	depth := infiniteDepth
	if hdr := r.Header.Get("Depth"); hdr != "" {
		depth = parseDepth(hdr)
		if depth == invalidDepth {
			return http.StatusBadRequest, errs.Wrap(errInvalidDepth, "无效的深度参数")
		}
	}

	// 读取 PROPFIND 请求体
	pf, status, err := readPropfind(r.Body)
	if err != nil {
		return status, errs.Wrap(err, "解析PROPFIND请求体失败")
	}

	// 创建多状态响应写入器
	mw := multistatusWriter{w: w}

	// 定义文件系统遍历函数
	walkFn := func(reqPath string, info model.Obj, err error) error {
		if err != nil {
			return errs.Wrap(err, "遍历文件系统失败")
		}

		var pstats []Propstat
		if pf.Propname != nil {
			// 处理属性名请求
			pnames, err := propnames(ctx, h.LockSystem, info)
			if err != nil {
				return errs.Wrap(err, "获取属性名失败")
			}
			pstat := Propstat{Status: http.StatusOK}
			for _, xmlname := range pnames {
				pstat.Props = append(pstat.Props, Property{XMLName: xmlname})
			}
			pstats = append(pstats, pstat)
		} else if pf.Allprop != nil {
			// 处理所有属性请求
			pstats, err = allprop(ctx, h.LockSystem, info, pf.Prop)
			if err != nil {
				return errs.Wrap(err, "获取所有属性失败")
			}
		} else {
			// 处理指定属性请求
			pstats, err = props(ctx, h.LockSystem, info, pf.Prop)
			if err != nil {
				return errs.Wrap(err, "获取指定属性失败")
			}
		}

		// 构建响应路径
		href := path.Join(h.Prefix, strings.TrimPrefix(reqPath, user.BasePath))
		if href != "/" && info.IsDir() {
			href += "/"
		}

		// 写入响应
		return mw.write(makePropstatResponse(href, pstats))
	}

	// 遍历文件系统
	walkErr := walkFS(ctx, depth, reqPath, fi, walkFn)
	closeErr := mw.close()

	if walkErr != nil {
		return http.StatusInternalServerError, errs.Wrap(walkErr, "遍历文件系统失败")
	}
	if closeErr != nil {
		return http.StatusInternalServerError, errs.Wrap(closeErr, "关闭响应写入器失败")
	}

	return 0, nil
}

func (h *Handler) handleProppatch(w http.ResponseWriter, r *http.Request) (status int, err error) {
	// 移除路径前缀
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, errs.Wrap(err, "路径前缀处理失败")
	}

	// 确认锁定状态
	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, errs.Wrap(err, "锁确认失败")
	}
	defer release()

	// 获取用户信息和处理路径
	ctx := r.Context()
	var pathStatus int
	reqPath, _, pathStatus, err = getUserAndPath(ctx, reqPath)
	if err != nil {
		return pathStatus, err
	}

	// 检查文件是否存在
	var fileStatus int
	_, fileStatus, err = getFileInfo(ctx, reqPath)
	if err != nil {
		return fileStatus, err
	}

	// 读取 PROPPATCH 请求体
	patches, status, err := readProppatch(r.Body)
	if err != nil {
		return status, errs.Wrap(err, "解析PROPPATCH请求体失败")
	}

	// 应用属性修改
	pstats, err := patch(ctx, h.LockSystem, reqPath, patches)
	if err != nil {
		return http.StatusInternalServerError, errs.Wrap(err, "应用属性修改失败")
	}

	// 创建多状态响应写入器
	mw := multistatusWriter{w: w}

	// 写入响应
	writeErr := mw.write(makePropstatResponse(r.URL.Path, pstats))
	closeErr := mw.close()

	if writeErr != nil {
		return http.StatusInternalServerError, errs.Wrap(writeErr, "写入响应失败")
	}
	if closeErr != nil {
		return http.StatusInternalServerError, errs.Wrap(closeErr, "关闭响应写入器失败")
	}

	return 0, nil
}

func makePropstatResponse(href string, pstats []Propstat) *response {
	resp := response{
		Href:     []string{(&url.URL{Path: href}).EscapedPath()},
		Propstat: make([]propstat, 0, len(pstats)),
	}
	for _, p := range pstats {
		var xmlErr *xmlError
		if p.XMLError != "" {
			xmlErr = &xmlError{InnerXML: []byte(p.XMLError)}
		}
		resp.Propstat = append(resp.Propstat, propstat{
			Status:              fmt.Sprintf("HTTP/1.1 %d %s", p.Status, StatusText(p.Status)),
			Prop:                p.Props,
			ResponseDescription: p.ResponseDescription,
			Error:               xmlErr,
		})
	}
	return &resp
}

const (
	infiniteDepth = -1
	invalidDepth  = -2
)

// parseDepth maps the strings "0", "1" and "infinity" to 0, 1 and
// infiniteDepth. Parsing any other string returns invalidDepth.
//
// Different WebDAV methods have further constraints on valid depths:
//   - PROPFIND has no further restrictions, as per section 9.1.
//   - COPY accepts only "0" or "infinity", as per section 9.8.3.
//   - MOVE accepts only "infinity", as per section 9.9.2.
//   - LOCK accepts only "0" or "infinity", as per section 9.10.3.
//
// These constraints are enforced by the handleXxx methods.
func parseDepth(s string) int {
	switch s {
	case "0":
		return 0
	case "1":
		return 1
	case "infinity":
		return infiniteDepth
	}
	return invalidDepth
}

// http://www.webdav.org/specs/rfc4918.html#status.code.extensions.to.http11
const (
	StatusMulti               = 207
	StatusUnprocessableEntity = 422
	StatusLocked              = 423
	StatusFailedDependency    = 424
	StatusInsufficientStorage = 507
)

func StatusText(code int) string {
	switch code {
	case StatusMulti:
		return "Multi-Status"
	case StatusUnprocessableEntity:
		return "Unprocessable Entity"
	case StatusLocked:
		return "Locked"
	case StatusFailedDependency:
		return "Failed Dependency"
	case StatusInsufficientStorage:
		return "Insufficient Storage"
	}
	return http.StatusText(code)
}

var (
	errDestinationEqualsSource = errs.New("webdav: destination equals source")
	errDirectoryNotEmpty       = errs.New("webdav: directory not empty")
	errInvalidDepth            = errs.New("webdav: invalid depth")
	errInvalidDestination      = errs.New("webdav: invalid destination")
	errInvalidIfHeader         = errs.New("webdav: invalid If header")
	errInvalidLockInfo         = errs.New("webdav: invalid lock info")
	errInvalidLockToken        = errs.New("webdav: invalid lock token")
	errInvalidPropfind         = errs.New("webdav: invalid propfind")
	errInvalidProppatch        = errs.New("webdav: invalid proppatch")
	errInvalidResponse         = errs.New("webdav: invalid response")
	errInvalidTimeout          = errs.New("webdav: invalid timeout")
	errNoFileSystem            = errs.New("webdav: no file system")
	errNoLockSystem            = errs.New("webdav: no lock system")
	errNotADirectory           = errs.New("webdav: not a directory")
	errPrefixMismatch          = errs.New("webdav: prefix mismatch")
	errRecursionTooDeep        = errs.New("webdav: recursion too deep")
	errUnsupportedLockInfo     = errs.New("webdav: unsupported lock info")
	errUnsupportedMethod       = errs.New("webdav: unsupported method")
)