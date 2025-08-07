package stream

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/net"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 错误定义
var (
	ErrInvalidLink = errs.New("无效的链接")
)

// RangeReaderFunc 是一个函数类型，用于实现 model.RangeReaderIF 接口
// 它允许将普通函数转换为范围读取器
type RangeReaderFunc func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error)

// RangeRead 实现 model.RangeReaderIF 接口
// 它调用底层函数来执行范围读取
func (f RangeReaderFunc) RangeRead(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
	return f(ctx, httpRange)
}

// GetRangeReaderFromLink 从链接创建范围读取器
// 它支持多种类型的链接，包括文件、URL和自定义范围读取器
func GetRangeReaderFromLink(size int64, link *model.Link) (model.RangeReaderIF, error) {
	if link == nil {
		return nil, errs.Wrap(ErrInvalidLink, "链接不能为空")
	}

	// 如果链接包含文件，创建文件范围读取器
	if link.MFile != nil {
		return GetRangeReaderFromMFile(size, link.MFile), nil
	}

	// 如果链接指定了并发或分块大小，创建下载器
	if link.Concurrency > 0 || link.PartSize > 0 {
		down := net.NewDownloader(func(d *net.Downloader) {
			d.Concurrency = link.Concurrency
			d.PartSize = link.PartSize
		})

		var rangeReader RangeReaderFunc = func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
			var req *net.HTTPRequestParams
			if link.RangeReader != nil {
				req = &net.HTTPRequestParams{
					Range: httpRange,
					Size:  size,
				}
			} else {
				requestHeader, _ := ctx.Value(consts.RequestHeaderKey).(http.Header)
				header := net.ProcessHeader(requestHeader, link.Header)
				req = &net.HTTPRequestParams{
					Range:     httpRange,
					Size:      size,
					URL:       link.URL,
					HeaderRef: header,
				}
			}
			return down.Download(ctx, req)
		}

		if link.RangeReader != nil {
			down.HTTPClient = net.GetRangeReaderHTTPRequestFunc(link.RangeReader)
			return rangeReader, nil
		}
		return RateLimitRangeReaderFunc(rangeReader), nil
	}

	// 如果链接包含自定义范围读取器，直接使用
	if link.RangeReader != nil {
		return link.RangeReader, nil
	}

	// 如果链接包含URL，创建HTTP范围读取器
	if len(link.URL) == 0 {
		return nil, errs.Wrap(ErrInvalidLink, "链接必须至少包含MFile、URL或RangeReader之一")
	}

	rangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
		// 调整范围以确保不超出文件大小
		if httpRange.Length < 0 || httpRange.Start+httpRange.Length > size {
			httpRange.Length = size - httpRange.Start
		}

		// 准备HTTP请求头
		requestHeader, _ := ctx.Value(consts.RequestHeaderKey).(http.Header)
		header := net.ProcessHeader(requestHeader, link.Header)
		header = http_range.ApplyRangeToHTTPHeader(httpRange, header)

		// 发送HTTP请求
		response, err := net.RequestHTTP(ctx, "GET", header, link.URL)
		if err != nil {
			var errorHTTPStatusCode net.ErrorHTTPStatusCode
			if errs.As(errs.Unwrap(err), &errorHTTPStatusCode) {
				return nil, err
			}
			return nil, errs.Wrapf(err, "HTTP请求失败: %s", link.URL)
		}

		// 处理响应
		if httpRange.Start == 0 && (httpRange.Length == -1 || httpRange.Length == size) ||
			response.StatusCode == http.StatusPartialContent ||
			checkContentRange(&response.Header, httpRange.Start) {
			return response.Body, nil
		} else if response.StatusCode == http.StatusOK {
			log.Warnf("远程HTTP服务器不支持范围请求，性能可能较低!")
			readCloser, err := net.GetRangedHTTPReader(response.Body, httpRange.Start, httpRange.Length)
			if err != nil {
				return nil, errs.Wrap(err, "创建范围HTTP读取器失败")
			}
			return readCloser, nil
		}

		return response.Body, nil
	}

	return RateLimitRangeReaderFunc(rangeReader), nil
}

// GetRangeReaderFromMFile RangeReaderIF.RangeRead返回的io.ReadCloser保留file的签名。
func GetRangeReaderFromMFile(size int64, file model.File) model.RangeReaderIF {
	return &model.FileRangeReader{
		RangeReaderIF: RangeReaderFunc(func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
			length := httpRange.Length
			if length < 0 || httpRange.Start+length > size {
				length = size - httpRange.Start
			}
			return &model.FileCloser{File: io.NewSectionReader(file, httpRange.Start, length)}, nil
		}),
	}
}

// checkContentRange 检查Content-Range头是否与请求的偏移匹配
// 某些云服务（如139云）不正确地返回206状态码，需要这个额外的检查
func checkContentRange(header *http.Header, offset int64) bool {
	start, _, err := http_range.ParseContentRange(header.Get("Content-Range"))
	if err != nil {
		log.Warnf("解析Content-Range时出现异常，将忽略，错误=%s", err)
	}
	return start == offset
}

// ReaderWithCtx 是一个带有上下文的读取器
// 它在每次读取操作前检查上下文是否已取消
type ReaderWithCtx struct {
	io.Reader
	Ctx context.Context
}

// Read 实现io.Reader接口，增加了上下文取消检查
func (r *ReaderWithCtx) Read(p []byte) (n int, err error) {
	if utils.IsCanceled(r.Ctx) {
		return 0, r.Ctx.Err()
	}
	return r.Reader.Read(p)
}

// Close 实现io.Closer接口
func (r *ReaderWithCtx) Close() error {
	if c, ok := r.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// 缓冲池，用于优化文件操作
var copyBuffer = sync.Pool{
	New: func() any {
		buffer := make([]byte, 32*1024) // 32KB
		return &buffer
	},
}

// CacheFullInTempFileAndWriter 将流的内容缓存到临时文件并同时写入指定的写入器
// 如果流已经有缓存文件，则直接使用该文件
// 如果提供了进度更新函数，则在读取过程中更新进度
func CacheFullInTempFileAndWriter(stream model.FileStreamer, up model.UpdateProgress, w io.Writer) (model.File, error) {
	// 检查流是否已有缓存文件
	if cache := stream.GetFile(); cache != nil {
		if w != nil {
			_, err := cache.Seek(0, io.SeekStart)
			if err == nil {
				var reader io.Reader = stream
				if up != nil {
					reader = &ReaderUpdatingProgress{
						Reader:         stream,
						UpdateProgress: up,
					}
				}

				// 获取缓冲区
				buf := copyBuffer.Get().(*[]byte)
				defer copyBuffer.Put(buf)

				_, err = utils.CopyWithBuffer(w, reader)
				if err == nil {
					_, err = cache.Seek(0, io.SeekStart)
				}
			}
			return cache, err
		}

		if up != nil {
			up(100)
		}
		return cache, nil
	}

	// 准备读取器
	var reader io.Reader = stream
	if up != nil {
		reader = &ReaderUpdatingProgress{
			Reader:         stream,
			UpdateProgress: up,
		}
	}

	// 如果需要同时写入，使用TeeReader
	if w != nil {
		reader = io.TeeReader(reader, w)
	}

	// 创建临时文件并写入数据
	tmpF, err := utils.CreateTempFile(reader, stream.GetSize())
	if err == nil {
		stream.SetTmpFile(tmpF)
	}
	return tmpF, err
}

// CacheFullInTempFileAndHash 将流的内容缓存到临时文件并计算哈希值
// 如果提供了进度更新函数，则在读取过程中更新进度
func CacheFullInTempFileAndHash(stream model.FileStreamer, up model.UpdateProgress, hashType *utils.HashType, hashParams ...any) (model.File, string, error) {
	h := hashType.NewFunc(hashParams...)
	tmpF, err := CacheFullInTempFileAndWriter(stream, up, h)
	if err != nil {
		return nil, "", errs.Wrap(err, "缓存到临时文件并计算哈希失败")
	}
	return tmpF, hex.EncodeToString(h.Sum(nil)), nil
}

type StreamSectionReader struct {
	file    model.FileStreamer
	off     int64
	bufPool *sync.Pool
}

func NewStreamSectionReader(file model.FileStreamer, maxBufferSize int) (*StreamSectionReader, error) {
	ss := &StreamSectionReader{file: file}
	if file.GetFile() == nil {
		maxBufferSize = min(maxBufferSize, int(file.GetSize()))
		if maxBufferSize > conf.MaxBufferLimit {
			_, err := file.CacheFullInTempFile()
			if err != nil {
				return nil, err
			}
		} else {
			ss.bufPool = &sync.Pool{
				New: func() any {
					return make([]byte, maxBufferSize)
				},
			}
		}
	}
	return ss, nil
}

// 线程不安全

func (ss *StreamSectionReader) GetSectionReader(off, length int64) (*SectionReader, error) {
	var cache io.ReaderAt = ss.file.GetFile()
	var buf []byte
	if cache == nil {
		if off != ss.off {
			return nil, errs.Errorf("stream not cached: request offset %d != current offset %d", off, ss.off)
		}
		tempBuf := ss.bufPool.Get().([]byte)
		buf = tempBuf[:length]
		n, err := io.ReadFull(ss.file, buf)
		if int64(n) != length {
			return nil, errs.Wrapf(err, "failed to read all data: (expect =%d, actual =%d)", length, n)
		}
		ss.off += int64(n)
		off = 0
		cache = bytes.NewReader(buf)
	}
	return &SectionReader{io.NewSectionReader(cache, off, length), buf}, nil
}

func (ss *StreamSectionReader) RecycleSectionReader(sr *SectionReader) {
	if sr != nil {
		if sr.buf != nil {
			ss.bufPool.Put(sr.buf[0:cap(sr.buf)])
			sr.buf = nil
		}
		sr.ReadSeeker = nil
	}
}

type SectionReader struct {
	io.ReadSeeker
	buf []byte
}