package net

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// this file is inspired by GO_SDK net.http.ServeContent

// type RangeReadCloser struct {
//	GetReaderForRange RangeReaderFunc
// }

// ServeHTTP replies to the request using the content in the
// provided RangeReadCloser. The main benefit of ServeHTTP
// is that it handles Range requests properly, sets the MIME type, and
// handles If-Match, If-Unmodified-Since, If-None-Match, If-Modified-Since,
// and If-Range requests.
//
// If the response's Content-Type header is not set, ServeHTTP
// first tries to deduce the type from name's file extension and,
// if that fails, falls back to reading the first block of the content
// and passing it to DetectContentType.
// The name is otherwise unused; in particular it can be empty and is
// never sent in the response.
//
// If modtime is not the zero time or Unix epoch, ServeHTTP
// includes it in a Last-Modified header in the response. If the
// request includes an If-Modified-Since header, ServeHTTP uses
// modtime to decide whether the content needs to be sent at all.
//
// The content's RangeReadCloser method must work: ServeHTTP gives a range,
// caller will give the reader for that Range.
//
// If the caller has set w's ETag header formatted per RFC 7232, section 2.3,
// ServeHTTP uses it to handle requests using If-Match, If-None-Match, or If-Range.
func ServeHTTP(w http.ResponseWriter, r *http.Request, name string, modTime time.Time, size int64, rangeReadCloser model.RangeReadCloserIF) error {
	defer rangeReadCloser.Close()
	setLastModified(w, modTime)
	done, rangeReq := checkPreconditions(w, r, modTime)
	if done {
		return nil
	}

	if size < 0 {
		// since too many functions need file size to work,
		// will not implement the support of unknown file size here
		http.Error(w, "negative content size not supported", http.StatusInternalServerError)
		return nil
	}

	code := http.StatusOK

	// If Content-Type isn't set, use the file's extension to find it, but
	// if the Content-Type is unset explicitly, do not sniff the type.
	contentTypes, haveType := w.Header()["Content-Type"]
	var contentType string
	if !haveType {
		contentType = utils.GetMimeType(name)
		w.Header().Set("Content-Type", contentType)
	} else if len(contentTypes) > 0 {
		contentType = contentTypes[0]
	}

	// handle Content-Range header.
	sendSize := size
	var sendContent io.ReadCloser
	ranges, err := http_range.ParseRange(rangeReq, size)
	switch {
	case err == nil:
	case errors.Is(err, http_range.ErrNoOverlap):
		if size == 0 {
			// Some clients add a Range header to all requests to
			// limit the size of the response. If the file is empty,
			// ignore the range header and respond with a 200 rather
			// than a 416.
			ranges = nil
			break
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		fallthrough
	default:
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	if sumRangesSize(ranges) > size {
		// The total number of bytes in all the ranges is larger than the size of the file
		// or unknown file size, ignore the range request.
		ranges = nil
	}

	// 使用请求的Context
	// 不然从sendContent读不到数据，即使请求断开CopyBuffer也会一直堵塞
	ctx := r.Context()
	switch {
	case len(ranges) == 0:
		reader, err := rangeReadCloser.RangeRead(ctx, http_range.Range{Length: -1})
		if err != nil {
			code = http.StatusRequestedRangeNotSatisfiable
			if statusCode, ok := errors.Unwrap(err).(ErrorHTTPStatusCode); ok {
				code = int(statusCode)
			}
			http.Error(w, err.Error(), code)
			return nil
		}
		sendContent = reader
	case len(ranges) == 1:
		// RFC 7233, Section 4.1:
		// "If a single part is being transferred, the server
		// generating the 206 response MUST generate a
		// Content-Range header field, describing what range
		// of the selected representation is enclosed, and a
		// payload consisting of the range.
		// ...
		// A server MUST NOT generate a multipart response to
		// a request for a single range, since a client that
		// does not request multiple parts might not support
		// multipart responses."
		ra := ranges[0]
		sendContent, err = rangeReadCloser.RangeRead(ctx, ra)
		if err != nil {
			code = http.StatusRequestedRangeNotSatisfiable
			if statusCode, ok := errors.Unwrap(err).(ErrorHTTPStatusCode); ok {
				code = int(statusCode)
			}
			http.Error(w, err.Error(), code)
			return nil
		}
		sendSize = ra.Length
		code = http.StatusPartialContent
		w.Header().Set("Content-Range", ra.ContentRange(size))
	case len(ranges) > 1:
		sendSize, err = rangesMIMESize(ranges, contentType, size)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		}
		code = http.StatusPartialContent
		if statusCode, ok := errors.Unwrap(err).(ErrorHTTPStatusCode); ok {
			code = int(statusCode)
		}
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		w.Header().Set("Content-Type", "multipart/byteranges; boundary="+mw.Boundary())
		sendContent = pr
		defer pr.Close() // cause writing goroutine to fail and exit if CopyN doesn't finish.
		go func() {
			for _, ra := range ranges {
				part, err := mw.CreatePart(ra.MimeHeader(contentType, size))
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				reader, err := rangeReadCloser.RangeRead(ctx, ra)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				if _, err := utils.CopyWithBufferN(part, reader, ra.Length); err != nil {
					pw.CloseWithError(err)
					return
				}
				reader.Close() // 确保每个范围读取器都被关闭
			}

			mw.Close()
			pw.Close()
		}()
	}

	w.Header().Set("Accept-Ranges", "bytes")
	if w.Header().Get("Content-Encoding") == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))
	}

	w.WriteHeader(code)

	if r.Method != "HEAD" {
		written, err := utils.CopyWithBufferN(w, sendContent, sendSize)
		if err != nil {
			if errors.Is(context.Cause(ctx), context.Canceled) {
				return nil
			}
			log.Warnf("ServeHttp error: %s", err)
			if written != sendSize {
				log.Warnf("Size mismatch: written=%d, expected=%d - possible causes: incorrect size, reader not providing full data, or connection closed prematurely", written, sendSize)
			}
			code = http.StatusInternalServerError
			if errors.Is(err, ErrExceedMaxConcurrency) {
				code = http.StatusTooManyRequests
			}
			w.WriteHeader(code)
			return err
		}
	}
	return nil
}
func ProcessHeader(origin, override http.Header) http.Header {
	result := http.Header{}
	// client header
	for h, val := range origin {
		if utils.SliceContains(conf.SlicesMap[consts.ProxyIgnoreHeaders], strings.ToLower(h)) {
			continue
		}
		result[h] = val
	}
	// needed header
	for h, val := range override {
		result[h] = val
	}
	return result
}

// RequestHTTP deal with Header properly then send the request
func RequestHTTP(ctx context.Context, method string, headerOverride http.Header, URL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = headerOverride
	res, err := GetHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	// TODO clean header with blocklist or passlist
	res.Header.Del("set-cookie")
	var reader io.Reader
	if res.StatusCode >= 400 {
		// 根据 Content-Encoding 判断 Body 是否压缩
		switch res.Header.Get("Content-Encoding") {
		case "gzip":
			// 使用gzip.NewReader解压缩
			reader, _ = gzip.NewReader(res.Body)
			defer reader.(*gzip.Reader).Close()
		default:
			// 没有Content-Encoding，直接读取
			reader = res.Body
		}
		all, _ := io.ReadAll(reader)
		_ = res.Body.Close()
		msg := string(all)
		log.Debugln(msg)
		return nil, fmt.Errorf("http request [%s] failure,status: %w response:%s", URL, ErrorHTTPStatusCode(res.StatusCode), msg)
	}
	return res, nil
}

type ErrorHTTPStatusCode int

func (e ErrorHTTPStatusCode) Error() string {
	return fmt.Sprintf("%d|%s", e, http.StatusText(int(e)))
}

var once sync.Once
var httpClient *http.Client

func GetHTTPClient() *http.Client {
	once.Do(func() {
		httpClient = NewHTTPClient()
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			req.Header.Del("Referer")
			return nil
		}
	})
	return httpClient
}

func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: time.Hour * 48,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: conf.Conf.TLSInsecureSkipVerify},
		},
	}
}