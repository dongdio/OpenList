package common

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"

	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/utility/http_range"
	net2 "github.com/dongdio/OpenList/utility/net"
	"github.com/dongdio/OpenList/utility/stream"
	"github.com/dongdio/OpenList/utility/utils"
)

// Proxy 处理文件代理请求
// 根据link中的不同信息，采用不同的代理方式：
// 1. 使用MFile直接提供文件
// 2. 使用RangeReadCloser处理范围请求
// 3. 使用分块下载处理大文件
// 4. 使用透明代理转发请求
//
// 参数:
//   - w: HTTP响应写入器
//   - r: HTTP请求
//   - link: 链接信息
//   - file: 文件对象
//
// 返回:
//   - error: 错误信息
func Proxy(w http.ResponseWriter, r *http.Request, link *model.Link, file model.Obj) error {
	// 参数检查
	if link == nil || file == nil {
		return fmt.Errorf("无效的链接或文件")
	}

	// 使用MFile直接提供文件内容
	if link.MFile != nil {
		defer link.MFile.Close()
		// 设置响应头
		attachHeader(w, file)

		// 设置内容类型
		contentType := link.Header.Get("Content-Type")
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		// 处理文件读取限速
		mFile := link.MFile
		if _, ok := mFile.(*os.File); !ok {
			mFile = &stream.RateLimitFile{
				File:    mFile,
				Limiter: stream.ServerDownloadLimit,
				Ctx:     r.Context(),
			}
		}

		// 使用ServeContent提供文件内容
		http.ServeContent(w, r, file.GetName(), file.ModTime(), mFile)
		return nil
	} else if link.RangeReadCloser != nil {
		// 使用RangeReadCloser处理范围请求
		attachHeader(w, file)
		return net2.ServeHTTP(w, r, file.GetName(), file.ModTime(), file.GetSize(), &stream.RateLimitRangeReadCloser{
			RangeReadCloserIF: link.RangeReadCloser,
			Limiter:           stream.ServerDownloadLimit,
		})
	} else if link.Concurrency > 0 || link.PartSize > 0 {
		// 使用分块下载处理大文件
		attachHeader(w, file)
		size := file.GetSize()

		// 创建范围读取函数
		rangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
			// 获取请求头
			requestHeader := ctx.Value("request_header")
			if requestHeader == nil {
				requestHeader = http.Header{}
			}
			header := net2.ProcessHeader(requestHeader.(http.Header), link.Header)

			// 创建下载器
			down := net2.NewDownloader(func(d *net2.Downloader) {
				d.Concurrency = link.Concurrency
				d.PartSize = link.PartSize
			})

			// 设置请求参数
			req := &net2.HttpRequestParams{
				URL:       link.URL,
				Range:     httpRange,
				Size:      size,
				HeaderRef: header,
			}

			// 执行下载
			rc, err := down.Download(ctx, req)
			return rc, err
		}

		// 提供HTTP服务
		return net2.ServeHTTP(w, r, file.GetName(), file.ModTime(), file.GetSize(), &stream.RateLimitRangeReadCloser{
			RangeReadCloserIF: &model.RangeReadCloser{RangeReader: rangeReader},
			Limiter:           stream.ServerDownloadLimit,
		})
	} else {
		// 透明代理
		// 处理请求头
		header := net2.ProcessHeader(r.Header, link.Header)

		// 发送HTTP请求
		res, err := net2.RequestHttp(r.Context(), r.Method, header, link.URL)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		// 复制响应头
		maps.Copy(w.Header(), res.Header)
		w.WriteHeader(res.StatusCode)

		// 处理HEAD请求
		if r.Method == http.MethodHead {
			return nil
		}

		// 复制响应体，带限速
		_, err = utils.CopyWithBuffer(w, &stream.RateLimitReader{
			Reader:  res.Body,
			Limiter: stream.ServerDownloadLimit,
			Ctx:     r.Context(),
		})
		return err
	}
}

// attachHeader 为响应添加附件相关的头信息
//
// 参数:
//   - w: HTTP响应写入器
//   - file: 文件对象
func attachHeader(w http.ResponseWriter, file model.Obj) {
	fileName := file.GetName()
	// 设置Content-Disposition头，使浏览器将内容作为附件处理
	w.Header().Set("Content-Disposition", utils.GenerateContentDisposition(fileName))
	// 设置内容类型
	w.Header().Set("Content-Type", utils.GetMimeType(fileName))
	// 设置ETag
	w.Header().Set("Etag", GetEtag(file))
}

// GetEtag 获取文件的ETag值
// 优先使用文件哈希，如果没有则使用修改时间和大小组合
//
// 参数:
//   - file: 文件对象
//
// 返回:
//   - string: ETag值
func GetEtag(file model.Obj) string {
	// 尝试使用文件哈希作为ETag
	hash := ""
	for _, v := range file.GetHash().Export() {
		if strings.Compare(v, hash) > 0 {
			hash = v
		}
	}
	if len(hash) > 0 {
		return fmt.Sprintf(`"%s"`, hash)
	}

	// 如果没有哈希，使用修改时间和大小组合（类似nginx的做法）
	return fmt.Sprintf(`"%x-%x"`, file.ModTime().Unix(), file.GetSize())
}

// ProxyRange 为链接设置范围读取器
// 如果链接已经有MFile，则不需要设置
// 如果链接的RangeReadCloser为NoProxyRange，则设置为nil
//
// 参数:
//   - link: 链接对象
//   - size: 文件大小
func ProxyRange(ctx context.Context, link *model.Link, size int64) {
	if link == nil {
		return
	}

	// 如果已经有MFile，不需要设置RangeReadCloser
	if link.MFile != nil {
		return
	}

	// 如果RangeReadCloser为nil，尝试从链接创建
	if link.RangeReadCloser == nil && !strings.HasPrefix(link.URL, GetApiUrl(ctx)+"/") {
		var rrc, err = stream.GetRangeReadCloserFromLink(size, link)
		if err != nil {
			return
		}
		link.RangeReadCloser = rrc
	}
}

// InterceptResponseWriter 拦截响应写入的ResponseWriter
// 允许将响应内容写入到指定的Writer
type InterceptResponseWriter struct {
	http.ResponseWriter
	io.Writer
}

// Write 实现ResponseWriter接口的Write方法
// 将内容写入到指定的Writer而非原始ResponseWriter
//
// 参数:
//   - p: 要写入的字节切片
//
// 返回:
//   - int: 写入的字节数
//   - error: 错误信息
func (iw *InterceptResponseWriter) Write(p []byte) (int, error) {
	return iw.Writer.Write(p)
}

// WrittenResponseWriter 跟踪是否已经写入内容的ResponseWriter
type WrittenResponseWriter struct {
	http.ResponseWriter
	written bool
}

// Write 实现ResponseWriter接口的Write方法
// 跟踪是否已经写入内容
//
// 参数:
//   - p: 要写入的字节切片
//
// 返回:
//   - int: 写入的字节数
//   - error: 错误信息
func (ww *WrittenResponseWriter) Write(p []byte) (int, error) {
	n, err := ww.ResponseWriter.Write(p)
	if !ww.written && n > 0 {
		ww.written = true
	}
	return n, err
}

// IsWritten 检查是否已经写入内容
//
// 返回:
//   - bool: 如果已经写入内容返回true，否则返回false
func (ww *WrittenResponseWriter) IsWritten() bool {
	return ww.written
}