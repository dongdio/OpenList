package common

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/net"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
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
	if link.MFile != nil {
		attachHeader(w, file, link)
		http.ServeContent(w, r, file.GetName(), file.ModTime(), link.MFile)
		return nil
	}

	if link.Concurrency > 0 || link.PartSize > 0 {
		attachHeader(w, file, link)
		size := link.ContentLength
		if size <= 0 {
			size = file.GetSize()
		}
		rrf, _ := stream.GetRangeReaderFromLink(size, link)
		if link.RangeReader == nil {
			r = r.WithContext(context.WithValue(r.Context(), consts.RequestHeaderKey, r.Header))
		}
		return net.ServeHTTP(w, r, file.GetName(), file.ModTime(), size, &model.RangeReadCloser{
			RangeReader: rrf,
		})
	}

	if link.RangeReader != nil {
		attachHeader(w, file, link)
		size := link.ContentLength
		if size <= 0 {
			size = file.GetSize()
		}
		return net.ServeHTTP(w, r, file.GetName(), file.ModTime(), size, &model.RangeReadCloser{
			RangeReader: link.RangeReader,
		})
	}

	// transparent proxy
	header := net.ProcessHeader(r.Header, link.Header)
	res, err := net.RequestHTTP(r.Context(), r.Method, header, link.URL)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	maps.Copy(w.Header(), res.Header)
	w.WriteHeader(res.StatusCode)
	if r.Method == http.MethodHead {
		return nil
	}
	_, err = utils.CopyWithBuffer(w, &stream.RateLimitReader{
		Reader:  res.Body,
		Limiter: stream.ServerDownloadLimit,
		Ctx:     r.Context(),
	})
	return err
}

// attachHeader 为响应添加附件相关的头信息
//
// 参数:
//   - w: HTTP响应写入器
//   - file: 文件对象
func attachHeader(w http.ResponseWriter, file model.Obj, link *model.Link) {
	fileName := file.GetName()
	// 设置Content-Disposition头，使浏览器将内容作为附件处理
	w.Header().Set("Content-Disposition", utils.GenerateContentDisposition(fileName))
	// 设置内容类型
	w.Header().Set("Content-Type", utils.GetMimeType(fileName))
	size := link.ContentLength
	if size <= 0 {
		size = file.GetSize()
	}
	w.Header().Set("Etag", GetEtag(file, size))
	contentType := link.Header.Get("Content-Type")
	if len(contentType) > 0 {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", utils.GetMimeType(fileName))
	}
}

// GetEtag 获取文件的ETag值
// 优先使用文件哈希，如果没有则使用修改时间和大小组合
//
// 参数:
//   - file: 文件对象
//   - size: 文件大小
//
// 返回:
//   - string: ETag值
func GetEtag(file model.Obj, size int64) string {
	// 尝试使用文件哈希作为ETag
	hash := ""
	for _, v := range file.GetHash().Export() {
		if v > hash {
			hash = v
		}
	}
	if len(hash) > 0 {
		return fmt.Sprintf(`"%s"`, hash)
	}

	// 如果没有哈希，使用修改时间和大小组合（类似nginx的做法）
	return fmt.Sprintf(`"%x-%x"`, file.ModTime().Unix(), size)
}

// ProxyRange 为链接设置范围读取器
// 如果链接已经有MFile，则不需要设置
// 如果链接的RangeReadCloser为NoProxyRange，则设置为nil
//
// 参数:
//   - link: 链接对象
//   - size: 文件大小
//
// 返回:
//   - *model.Link: 修改后的链接对象
func ProxyRange(ctx context.Context, link *model.Link, size int64) *model.Link {
	if link.MFile == nil && link.RangeReader == nil && !strings.HasPrefix(link.URL, GetApiURL(ctx)+"/") {
		if link.ContentLength > 0 {
			size = link.ContentLength
		}
		rrf, err := stream.GetRangeReaderFromLink(size, link)
		if err == nil {
			return &model.Link{
				RangeReader:   rrf,
				ContentLength: size,
			}
		}

	}
	return link
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
