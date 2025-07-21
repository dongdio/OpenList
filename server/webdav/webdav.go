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
	"time"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/sign"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/net"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

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
	if h.Prefix == "" {
		return p, http.StatusOK, nil
	}

	// 尝试删除前缀并检查是否成功
	if resultPath := strings.TrimPrefix(p, h.Prefix); len(resultPath) < len(p) {
		return resultPath, http.StatusOK, nil
	}

	return p, http.StatusNotFound, errPrefixMismatch
}

// ServeHTTP 处理所有WebDAV请求
// 实现http.Handler接口
//
// 参数:
//   - w: HTTP响应写入器
//   - r: HTTP请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 默认状态和错误
	status, err := http.StatusBadRequest, errUnsupportedMethod

	// 创建缓冲响应写入器，用于延迟发送响应
	brw := NewBufferedResponseWriter()
	useBufferedWriter := true

	// 检查锁系统是否已配置
	if h.LockSystem == nil {
		status, err = http.StatusInternalServerError, errNoLockSystem
	} else {
		// 根据HTTP方法分发到对应的处理函数
		switch r.Method {
		case "OPTIONS":
			// 处理OPTIONS请求，用于发现服务器支持的功能
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
			// 处理删除资源请求
			status, err = h.handleDelete(brw, r)

		case "PUT":
			// 处理上传文件请求
			status, err = h.handlePut(brw, r)

		case "MKCOL":
			// 处理创建集合(目录)请求
			status, err = h.handleMkcol(brw, r)

		case "COPY", "MOVE":
			// 处理复制和移动资源请求
			status, err = h.handleCopyMove(brw, r)

		case "LOCK":
			// 处理锁定资源请求
			status, err = h.handleLock(brw, r)

		case "UNLOCK":
			// 处理解锁资源请求
			status, err = h.handleUnlock(brw, r)

		case "PROPFIND":
			// 处理属性查询请求
			status, err = h.handlePropfind(brw, r)
			// 如果PROPFIND出错，将其作为空文件夹呈现给客户端
			if err != nil {
				status = http.StatusNotFound
			}

		case "PROPPATCH":
			// 处理属性修改请求
			status, err = h.handleProppatch(brw, r)
		}
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
		if err == ErrLocked {
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
	ifHeader := r.Header.Get("If")

	if ifHeader == "" {
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
	parsedIfHeader, ok := parseIfHeader(ifHeader)
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
		if errors.Is(err, ErrConfirmationFailed) {
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
		return status, err
	}

	// 获取用户信息
	ctx := r.Context()
	user, ok := ctx.Value(consts.UserKey).(*model.User)
	if !ok || user == nil {
		return http.StatusUnauthorized, errors.New("未找到用户信息")
	}

	// 合并用户路径
	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return http.StatusForbidden, err
	}

	allow := "OPTIONS, LOCK, PUT, MKCOL"
	if fi, err := fs.Get(ctx, reqPath, &fs.GetArgs{}); err == nil {
		if fi.IsDir() {
			allow = "OPTIONS, LOCK, DELETE, PROPPATCH, COPY, MOVE, UNLOCK, PROPFIND"
		} else {
			allow = "OPTIONS, LOCK, GET, HEAD, POST, DELETE, PROPPATCH, COPY, MOVE, UNLOCK, PROPFIND, PUT"
		}
	}
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
	// TODO: check locks for read-only access??
	ctx := r.Context()
	user := ctx.Value(consts.UserKey).(*model.User)
	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return http.StatusForbidden, err
	}
	fi, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		return http.StatusNotFound, err
	}
	// if r.Method == http.MethodHead {
	// 	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.GetSize()))
	// 	return http.StatusOK, nil
	// }
	if fi.IsDir() {
		return http.StatusMethodNotAllowed, nil
	}
	// Let ServeContent determine the Content-Type header.
	storage, _ := fs.GetStorage(reqPath, &fs.GetStoragesArgs{})
	downProxyURL := storage.GetStorage().DownProxyUrl
	if storage.GetStorage().WebdavNative() || (storage.GetStorage().WebdavProxy() && downProxyURL == "") {
		link, _, err := fs.Link(ctx, reqPath, model.LinkArgs{Header: r.Header})
		if err != nil {
			return http.StatusInternalServerError, err
		}
		defer link.Close()
		if storage.GetStorage().ProxyRange {
			link = common.ProxyRange(ctx, link, fi.GetSize())
		}
		err = common.Proxy(w, r, link, fi)
		if err != nil {
			var statusCode net.ErrorHTTPStatusCode
			if errors.As(errors.Unwrap(err), &statusCode) {
				return int(statusCode), err
			}
			return http.StatusInternalServerError, errors.Errorf("webdav proxy error: %+v", err)
		}
	} else if storage.GetStorage().WebdavProxy() && downProxyURL != "" {
		u := fmt.Sprintf("%s%s?sign=%s",
			strings.Split(downProxyURL, "\n")[0],
			utils.EncodePath(reqPath, true),
			sign.Sign(reqPath))
		w.Header().Set("Cache-Control", "max-age=0, no-cache, no-store, must-revalidate")
		http.Redirect(w, r, u, http.StatusFound)
	} else {
		link, _, err := fs.Link(ctx, reqPath, model.LinkArgs{IP: utils.ClientIP(r), Header: r.Header, Redirect: true})
		if err != nil {
			return http.StatusInternalServerError, err
		}
		defer link.Close()
		http.Redirect(w, r, link.URL, http.StatusFound)
	}
	return 0, nil
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, err
	}
	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, err
	}
	defer release()

	ctx := r.Context()
	user := ctx.Value(consts.UserKey).(*model.User)
	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return 403, err
	}
	// TODO: return MultiStatus where appropriate.

	// "godoc os RemoveAll" says that "If the path does not exist, RemoveAll
	// returns nil (no error)." WebDAV semantics are that it should return a
	// "404 Not Found". We therefore have to Stat before we RemoveAll.
	if _, err := fs.Get(ctx, reqPath, &fs.GetArgs{}); err != nil {
		if errs.IsObjectNotFound(err) {
			return http.StatusNotFound, err
		}
		return http.StatusMethodNotAllowed, err
	}
	if err := fs.Remove(ctx, reqPath); err != nil {
		return http.StatusMethodNotAllowed, err
	}
	// fs.ClearCache(path.Dir(reqPath))
	return http.StatusNoContent, nil
}

func (h *Handler) handlePut(w http.ResponseWriter, r *http.Request) (status int, err error) {
	defer func() {
		if n, _ := io.ReadFull(r.Body, []byte{0}); n == 1 {
			_, _ = utils.CopyWithBuffer(io.Discard, r.Body)
		}
		_ = r.Body.Close()
	}()
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, err
	}
	if reqPath == "" {
		return http.StatusMethodNotAllowed, nil
	}
	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, err
	}
	defer release()
	// TODO(rost): Support the If-Match, If-None-Match headers? See bradfitz'
	// comments in http.checkEtag.
	ctx := r.Context()
	user := ctx.Value(consts.UserKey).(*model.User)
	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return http.StatusForbidden, err
	}
	obj := model.Object{
		Name:     path.Base(reqPath),
		Size:     r.ContentLength,
		Modified: h.getModTime(r),
		Ctime:    h.getCreateTime(r),
	}
	fsStream := &stream.FileStream{
		Obj:      &obj,
		Reader:   r.Body,
		Mimetype: r.Header.Get("Content-Type"),
	}
	if fsStream.Mimetype == "" {
		fsStream.Mimetype = utils.GetMimeType(reqPath)
	}
	err = fs.PutDirectly(ctx, path.Dir(reqPath), fsStream)
	if errs.IsNotFoundError(err) {
		return http.StatusNotFound, err
	}

	// TODO(rost): Returning 405 Method Not Allowed might not be appropriate.
	if err != nil {
		return http.StatusMethodNotAllowed, err
	}
	fi, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		fi = &obj
	}
	etag, err := findETag(ctx, h.LockSystem, reqPath, fi)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	w.Header().Set("Etag", etag)
	return http.StatusCreated, nil
}

func (h *Handler) handleMkcol(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, err
	}
	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, err
	}
	defer release()

	ctx := r.Context()
	user := ctx.Value(consts.UserKey).(*model.User)
	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return 403, err
	}

	if r.ContentLength > 0 {
		return http.StatusUnsupportedMediaType, nil
	}

	// RFC 4918 9.3.1
	// 405 (Method Not Allowed) - MKCOL can only be executed on an unmapped URL
	if _, err := fs.Get(ctx, reqPath, &fs.GetArgs{}); err == nil {
		return http.StatusMethodNotAllowed, err
	}
	// RFC 4918 9.3.1
	// 409 (Conflict) The server MUST NOT create those intermediate collections automatically.
	reqDir := path.Dir(reqPath)
	if _, err := fs.Get(ctx, reqDir, &fs.GetArgs{}); err != nil {
		if errs.IsObjectNotFound(err) {
			return http.StatusConflict, err
		}
		return http.StatusMethodNotAllowed, err
	}
	if err := fs.MakeDir(ctx, reqPath); err != nil {
		if os.IsNotExist(err) {
			return http.StatusConflict, err
		}
		return http.StatusMethodNotAllowed, err
	}
	return http.StatusCreated, nil
}

func (h *Handler) handleCopyMove(w http.ResponseWriter, r *http.Request) (status int, err error) {
	hdr := r.Header.Get("Destination")
	if hdr == "" {
		return http.StatusBadRequest, errInvalidDestination
	}
	u, err := url.Parse(hdr)
	if err != nil {
		return http.StatusBadRequest, errInvalidDestination
	}
	if u.Host != "" && u.Host != r.Host {
		return http.StatusBadGateway, errInvalidDestination
	}

	src, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, err
	}

	dst, status, err := h.stripPrefix(u.Path)
	if err != nil {
		return status, err
	}

	if dst == "" {
		return http.StatusBadGateway, errInvalidDestination
	}
	if dst == src {
		return http.StatusForbidden, errDestinationEqualsSource
	}

	ctx := r.Context()
	user := ctx.Value(consts.UserKey).(*model.User)
	src, err = user.JoinPath(src)
	if err != nil {
		return 403, err
	}
	dst, err = user.JoinPath(dst)
	if err != nil {
		return 403, err
	}

	if r.Method == "COPY" {
		// Section 7.5.1 says that a COPY only needs to lock the destination,
		// not both destination and source. Strictly speaking, this is racy,
		// even though a COPY doesn't modify the source, if a concurrent
		// operation modifies the source. However, the litmus test explicitly
		// checks that COPYing a locked-by-another source is OK.
		release, status, err := h.confirmLocks(r, "", dst)
		if err != nil {
			return status, err
		}
		defer release()

		// Section 9.8.3 says that "The COPY method on a collection without a Depth
		// header must act as if a Depth header with value "infinity" was included".
		depth := infiniteDepth
		if hdr := r.Header.Get("Depth"); hdr != "" {
			depth = parseDepth(hdr)
			if depth != 0 && depth != infiniteDepth {
				// Section 9.8.3 says that "A client may submit a Depth header on a
				// COPY on a collection with a value of "0" or "infinity"."
				return http.StatusBadRequest, errInvalidDepth
			}
		}
		return copyFiles(ctx, src, dst, r.Header.Get("Overwrite") != "F")
	}

	release, status, err := h.confirmLocks(r, src, dst)
	if err != nil {
		return status, err
	}
	defer release()

	// Section 9.9.2 says that "The MOVE method on a collection must act as if
	// a "Depth: infinity" header was used on it. A client must not submit a
	// Depth header on a MOVE on a collection with any value but "infinity"."
	if hdr := r.Header.Get("Depth"); hdr != "" {
		if parseDepth(hdr) != infiniteDepth {
			return http.StatusBadRequest, errInvalidDepth
		}
	}
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
			if errors.Is(err, ErrNoSuchLock) {
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
		reqPath, status, err := h.stripPrefix(r.URL.Path)
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
			if errors.Is(err, ErrLocked) {
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
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, err
	}
	var (
		userAgent = r.Header.Get("User-Agent")
		ctx       = context.WithValue(r.Context(), consts.UserAgentKey, userAgent)
		user      = ctx.Value(consts.UserKey).(*model.User)
	)
	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return 403, err
	}
	fi, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		if errs.IsNotFoundError(err) {
			return http.StatusNotFound, err
		}
		return http.StatusMethodNotAllowed, err
	}
	depth := infiniteDepth
	if hdr := r.Header.Get("Depth"); hdr != "" {
		depth = parseDepth(hdr)
		if depth == invalidDepth {
			return http.StatusBadRequest, errInvalidDepth
		}
	}
	pf, status, err := readPropfind(r.Body)
	if err != nil {
		return status, err
	}

	mw := multistatusWriter{w: w}

	walkFn := func(reqPath string, info model.Obj, err error) error {
		if err != nil {
			return err
		}
		var pstats []Propstat
		if pf.Propname != nil {
			pnames, err := propnames(ctx, h.LockSystem, info)
			if err != nil {
				return err
			}
			pstat := Propstat{Status: http.StatusOK}
			for _, xmlname := range pnames {
				pstat.Props = append(pstat.Props, Property{XMLName: xmlname})
			}
			pstats = append(pstats, pstat)
		} else if pf.Allprop != nil {
			pstats, err = allprop(ctx, h.LockSystem, info, pf.Prop)
		} else {
			pstats, err = props(ctx, h.LockSystem, info, pf.Prop)
		}
		if err != nil {
			return err
		}
		href := path.Join(h.Prefix, strings.TrimPrefix(reqPath, user.BasePath))
		if href != "/" && info.IsDir() {
			href += "/"
		}
		return mw.write(makePropstatResponse(href, pstats))
	}

	walkErr := walkFS(ctx, depth, reqPath, fi, walkFn)
	closeErr := mw.close()
	if walkErr != nil {
		return http.StatusInternalServerError, walkErr
	}
	if closeErr != nil {
		return http.StatusInternalServerError, closeErr
	}
	return 0, nil
}

func (h *Handler) handleProppatch(w http.ResponseWriter, r *http.Request) (status int, err error) {
	reqPath, status, err := h.stripPrefix(r.URL.Path)
	if err != nil {
		return status, err
	}
	release, status, err := h.confirmLocks(r, reqPath, "")
	if err != nil {
		return status, err
	}
	defer release()

	ctx := r.Context()
	user := ctx.Value(consts.UserKey).(*model.User)
	reqPath, err = user.JoinPath(reqPath)
	if err != nil {
		return 403, err
	}
	if _, err = fs.Get(ctx, reqPath, &fs.GetArgs{}); err != nil {
		if errs.IsObjectNotFound(err) {
			return http.StatusNotFound, err
		}
		return http.StatusMethodNotAllowed, err
	}
	patches, status, err := readProppatch(r.Body)
	if err != nil {
		return status, err
	}
	pstats, err := patch(ctx, h.LockSystem, reqPath, patches)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	mw := multistatusWriter{w: w}
	writeErr := mw.write(makePropstatResponse(r.URL.Path, pstats))
	closeErr := mw.close()
	if writeErr != nil {
		return http.StatusInternalServerError, writeErr
	}
	if closeErr != nil {
		return http.StatusInternalServerError, closeErr
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
	errDestinationEqualsSource = errors.New("webdav: destination equals source")
	errDirectoryNotEmpty       = errors.New("webdav: directory not empty")
	errInvalidDepth            = errors.New("webdav: invalid depth")
	errInvalidDestination      = errors.New("webdav: invalid destination")
	errInvalidIfHeader         = errors.New("webdav: invalid If header")
	errInvalidLockInfo         = errors.New("webdav: invalid lock info")
	errInvalidLockToken        = errors.New("webdav: invalid lock token")
	errInvalidPropfind         = errors.New("webdav: invalid propfind")
	errInvalidProppatch        = errors.New("webdav: invalid proppatch")
	errInvalidResponse         = errors.New("webdav: invalid response")
	errInvalidTimeout          = errors.New("webdav: invalid timeout")
	errNoFileSystem            = errors.New("webdav: no file system")
	errNoLockSystem            = errors.New("webdav: no lock system")
	errNotADirectory           = errors.New("webdav: not a directory")
	errPrefixMismatch          = errors.New("webdav: prefix mismatch")
	errRecursionTooDeep        = errors.New("webdav: recursion too deep")
	errUnsupportedLockInfo     = errors.New("webdav: unsupported lock info")
	errUnsupportedMethod       = errors.New("webdav: unsupported method")
)
