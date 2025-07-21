package net

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/utility/http_range"
)

// scanETag 判断 s 中是否存在语法有效的 ETag
// 如果存在，返回 ETag 和消费 ETag 后的剩余文本
// 否则返回 "", ""
func scanETag(s string) (etag string, remain string) {
	s = textproto.TrimString(s)
	start := 0
	if strings.HasPrefix(s, "W/") {
		start = 2
	}
	if len(s[start:]) < 2 || s[start] != '"' {
		return "", ""
	}
	// ETag 格式为 W/"text" 或 "text"
	// 参见 RFC 7232 2.3
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch {
		// ETag 中允许的字符值
		case c == 0x21 || c >= 0x23 && c <= 0x7E || c >= 0x80:
		case c == '"':
			return s[:i+1], s[i+1:]
		default:
			return "", ""
		}
	}
	return "", ""
}

// etagStrongMatch 报告 a 和 b 是否使用强 ETag 比较匹配
// 假设 a 和 b 是有效的 ETag
func etagStrongMatch(a, b string) bool {
	return a == b && a != "" && a[0] == '"'
}

// etagWeakMatch 报告 a 和 b 是否使用弱 ETag 比较匹配
// 假设 a 和 b 是有效的 ETag
func etagWeakMatch(a, b string) bool {
	return strings.TrimPrefix(a, "W/") == strings.TrimPrefix(b, "W/")
}

// condResult 是 HTTP 请求前提条件检查的结果
// 参见 https://tools.ietf.org/html/rfc7232 第 3 节
type condResult int

const (
	condNone condResult = iota
	condTrue
	condFalse
)

// checkIfMatch 检查 If-Match 头
func checkIfMatch(w http.ResponseWriter, r *http.Request) condResult {
	im := r.Header.Get("If-Match")
	if im == "" {
		return condNone
	}
	r.Header.Del("If-Match")

	for {
		im = textproto.TrimString(im)
		if len(im) == 0 {
			break
		}
		if im[0] == ',' {
			im = im[1:]
			continue
		}
		if im[0] == '*' {
			return condTrue
		}
		etag, remain := scanETag(im)
		if etag == "" {
			break
		}
		if etagStrongMatch(etag, w.Header().Get("Etag")) {
			return condTrue
		}
		im = remain
	}

	return condFalse
}

// checkIfUnmodifiedSince 检查 If-Unmodified-Since 头
func checkIfUnmodifiedSince(r *http.Request, modtime time.Time) condResult {
	ius := r.Header.Get("If-Unmodified-Since")
	if ius == "" {
		return condNone
	}
	r.Header.Del("If-Unmodified-Since")

	if isZeroTime(modtime) {
		return condNone
	}

	t, err := http.ParseTime(ius)
	if err != nil {
		return condNone
	}

	// Last-Modified 头截断亚秒精度，所以 modtime 也需要截断
	modtime = modtime.Truncate(time.Second)
	if ret := modtime.Compare(t); ret <= 0 {
		return condTrue
	}

	return condFalse
}

// checkIfNoneMatch 检查 If-None-Match 头
func checkIfNoneMatch(w http.ResponseWriter, r *http.Request) condResult {
	inm := r.Header.Get("If-None-Match")
	if inm == "" {
		return condNone
	}
	r.Header.Del("If-None-Match")

	buf := inm
	for {
		buf = textproto.TrimString(buf)
		if len(buf) == 0 {
			break
		}
		if buf[0] == ',' {
			buf = buf[1:]
			continue
		}
		if buf[0] == '*' {
			return condFalse
		}
		etag, remain := scanETag(buf)
		if etag == "" {
			break
		}
		if etagWeakMatch(etag, w.Header().Get("Etag")) {
			return condFalse
		}
		buf = remain
	}

	return condTrue
}

// checkIfModifiedSince 检查 If-Modified-Since 头
func checkIfModifiedSince(r *http.Request, modtime time.Time) condResult {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return condNone
	}

	ims := r.Header.Get("If-Modified-Since")
	if ims == "" {
		return condNone
	}
	r.Header.Del("If-Modified-Since")

	if isZeroTime(modtime) {
		return condNone
	}

	t, err := http.ParseTime(ims)
	if err != nil {
		return condNone
	}

	// Last-Modified 头截断亚秒精度，所以 modtime 也需要截断
	modtime = modtime.Truncate(time.Second)
	if ret := modtime.Compare(t); ret <= 0 {
		return condFalse
	}

	return condTrue
}

// checkIfRange 检查 If-Range 头
func checkIfRange(w http.ResponseWriter, r *http.Request, modtime time.Time) condResult {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return condNone
	}

	ir := r.Header.Get("If-Range")
	if ir == "" {
		return condNone
	}
	r.Header.Del("If-Range")

	etag, _ := scanETag(ir)
	if etag != "" {
		if etagStrongMatch(etag, w.Header().Get("Etag")) {
			return condTrue
		}
		return condFalse
	}

	// If-Range 值通常是 ETag 值，但也可能是 modtime 日期
	// 参见 golang.org/issue/8367
	if modtime.IsZero() {
		return condFalse
	}

	t, err := http.ParseTime(ir)
	if err != nil {
		return condFalse
	}

	if t.Unix() == modtime.Unix() {
		return condTrue
	}

	return condFalse
}

// Unix 纪元时间常量
var unixEpochTime = time.Unix(0, 0)

// isZeroTime 报告 t 是否明显未指定（零值或 Unix()=0）
func isZeroTime(t time.Time) bool {
	return t.IsZero() || t.Equal(unixEpochTime)
}

// setLastModified 设置 Last-Modified 响应头
func setLastModified(w http.ResponseWriter, modtime time.Time) {
	if !isZeroTime(modtime) {
		w.Header().Set("Last-Modified", modtime.UTC().Format(http.TimeFormat))
	}
}

// writeNotModified 写入 304 Not Modified 响应
func writeNotModified(w http.ResponseWriter) {
	// RFC 7232 第 4.1 节:
	// 发送者不应生成除上述列出的字段之外的表示元数据
	// 除非该元数据存在于指导缓存更新的目的
	// （例如，如果响应没有 ETag 字段，Last-Modified 可能有用）
	h := w.Header()
	delete(h, "Content-Type")
	delete(h, "Content-Length")
	delete(h, "Content-Encoding")
	if h.Get("Etag") != "" {
		delete(h, "Last-Modified")
	}
	w.WriteHeader(http.StatusNotModified)
}

// checkPreconditions 评估请求前提条件并报告是否有前提条件
// 导致发送 StatusNotModified 或 StatusPreconditionFailed
func checkPreconditions(w http.ResponseWriter, r *http.Request, modtime time.Time) (done bool, rangeHeader string) {
	// 此函数仔细遵循 RFC 7232 第 6 节
	ch := checkIfMatch(w, r)
	if ch == condNone {
		ch = checkIfUnmodifiedSince(r, modtime)
	}
	if ch == condFalse {
		w.WriteHeader(http.StatusPreconditionFailed)
		return true, ""
	}

	ch = checkIfNoneMatch(w, r)
	if ch == condFalse {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			writeNotModified(w)
			return true, ""
		}
		w.WriteHeader(http.StatusPreconditionFailed)
		return true, ""
	}

	if ch == condNone {
		ch = checkIfModifiedSince(r, modtime)
		if ch == condFalse {
			writeNotModified(w)
			return true, ""
		}
	}

	rangeHeader = r.Header.Get("Range")
	if rangeHeader != "" && checkIfRange(w, r, modtime) == condFalse {
		rangeHeader = ""
	}

	return false, rangeHeader
}

// sumRangesSize 计算所有范围的总大小
func sumRangesSize(ranges []http_range.Range) (size int64) {
	for _, ra := range ranges {
		size += ra.Length
	}
	return
}

// countingWriter 是一个计数写入字节数的 io.Writer
type countingWriter int64

// Write 实现 io.Writer 接口
func (w *countingWriter) Write(p []byte) (n int, err error) {
	*w += countingWriter(len(p))
	return len(p), nil
}

// rangesMIMESize 返回所有范围的 MIME 编码大小
func rangesMIMESize(ranges []http_range.Range, contentType string, contentSize int64) (encSize int64, err error) {
	var w countingWriter
	mw := multipart.NewWriter(&w)

	for _, ra := range ranges {
		mimeHeader := ra.MimeHeader(contentType, contentSize)
		part, err := mw.CreatePart(mimeHeader)
		if err != nil {
			return 0, err
		}

		// 写入范围数据的大小
		_, err = io.CopyN(part, io.LimitReader(strings.NewReader(""), ra.Length), ra.Length)
		if err != nil {
			return 0, err
		}
	}

	err = mw.Close()
	if err != nil {
		return 0, err
	}

	return int64(w), nil
}

// LimitedReadCloser 是一个有限制的读取关闭器
type LimitedReadCloser struct {
	rc        io.ReadCloser
	remaining int
}

// Read 实现 io.Reader 接口
func (l *LimitedReadCloser) Read(buf []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, io.EOF
	}

	if len(buf) > l.remaining {
		buf = buf[:l.remaining]
	}

	n, err := l.rc.Read(buf)
	l.remaining -= n

	return n, err
}

// Close 实现 io.Closer 接口
func (l *LimitedReadCloser) Close() error {
	return l.rc.Close()
}

// GetRangedHTTPReader 获取指定范围的 HTTP 读取器
func GetRangedHTTPReader(readCloser io.ReadCloser, offset, length int64) (io.ReadCloser, error) {
	if offset > 0 {
		// 跳过偏移量
		n, err := io.CopyN(io.Discard, readCloser, offset)
		if err != nil {
			readCloser.Close()
			return nil, err
		}

		if n != offset {
			readCloser.Close()
			return nil, errors.Errorf("expected to skip %d bytes, but skipped %d", offset, n)
		}
	}

	// 如果长度为 -1，返回剩余所有内容
	if length < 0 {
		return readCloser, nil
	}

	// 返回有限制的读取器
	return &LimitedReadCloser{
		rc:        readCloser,
		remaining: int(length),
	}, nil
}
