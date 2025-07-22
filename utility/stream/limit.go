package stream

import (
	"context"
	"io"
	"time"

	"golang.org/x/time/rate"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Limiter 接口定义了速率限制器的行为
// 它扩展了 golang.org/x/time/rate.Limiter 接口，添加了一些额外的方法
type Limiter interface {
	// 基本速率限制方法
	Limit() rate.Limit
	Burst() int
	TokensAt(time.Time) float64
	Tokens() float64
	Allow() bool
	AllowN(time.Time, int) bool
	Reserve() *rate.Reservation
	ReserveN(time.Time, int) *rate.Reservation
	Wait(context.Context) error
	WaitN(context.Context, int) error

	// 设置限制方法
	SetLimit(rate.Limit)
	SetLimitAt(time.Time, rate.Limit)
	SetBurst(int)
	SetBurstAt(time.Time, int)
}

// 全局速率限制器
var (
	// ClientDownloadLimit 客户端下载速率限制器
	ClientDownloadLimit Limiter
	// ClientUploadLimit 客户端上传速率限制器
	ClientUploadLimit Limiter
	// ServerDownloadLimit 服务器下载速率限制器
	ServerDownloadLimit Limiter
	// ServerUploadLimit 服务器上传速率限制器
	ServerUploadLimit Limiter
)

// RateLimitReader 实现了一个带速率限制的读取器
// 它在每次读取操作后等待适当的时间，以确保不超过指定的速率
type RateLimitReader struct {
	io.Reader                 // 底层读取器
	Limiter   Limiter         // 速率限制器
	Ctx       context.Context // 上下文，用于取消操作
}

// Read 实现了 io.Reader 接口，增加了速率限制
// 它首先检查上下文是否已取消，然后从底层读取器读取数据，
// 最后等待足够的时间以确保不超过速率限制
func (r *RateLimitReader) Read(p []byte) (n int, err error) {
	// 检查上下文是否已取消
	if r.Ctx != nil && utils.IsCanceled(r.Ctx) {
		return 0, r.Ctx.Err()
	}

	// 从底层读取器读取数据
	n, err = r.Reader.Read(p)
	if err != nil {
		return
	}

	// 应用速率限制
	if r.Limiter != nil {
		if r.Ctx == nil {
			r.Ctx = context.Background()
		}
		err = r.Limiter.WaitN(r.Ctx, n)
	}
	return
}

// Close 实现了 io.Closer 接口
// 如果底层读取器支持关闭，则关闭它
func (r *RateLimitReader) Close() error {
	if c, ok := r.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// RateLimitWriter 实现了一个带速率限制的写入器
// 它在每次写入操作后等待适当的时间，以确保不超过指定的速率
type RateLimitWriter struct {
	io.Writer                 // 底层写入器
	Limiter   Limiter         // 速率限制器
	Ctx       context.Context // 上下文，用于取消操作
}

// Write 实现了 io.Writer 接口，增加了速率限制
// 它首先检查上下文是否已取消，然后向底层写入器写入数据，
// 最后等待足够的时间以确保不超过速率限制
func (w *RateLimitWriter) Write(p []byte) (n int, err error) {
	// 检查上下文是否已取消
	if w.Ctx != nil && utils.IsCanceled(w.Ctx) {
		return 0, w.Ctx.Err()
	}

	// 向底层写入器写入数据
	n, err = w.Writer.Write(p)
	if err != nil {
		return
	}

	// 应用速率限制
	if w.Limiter != nil {
		if w.Ctx == nil {
			w.Ctx = context.Background()
		}
		err = w.Limiter.WaitN(w.Ctx, n)
	}
	return
}

// Close 实现了 io.Closer 接口
// 如果底层写入器支持关闭，则关闭它
func (w *RateLimitWriter) Close() error {
	if c, ok := w.Writer.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// RateLimitFile 实现了一个带速率限制的文件接口
// 它在每次读取操作后等待适当的时间，以确保不超过指定的速率
type RateLimitFile struct {
	model.File                 // 底层文件
	Limiter    Limiter         // 速率限制器
	Ctx        context.Context // 上下文，用于取消操作
}

// Read 实现了 io.Reader 接口，增加了速率限制
func (r *RateLimitFile) Read(p []byte) (n int, err error) {
	// 检查上下文是否已取消
	if r.Ctx != nil && utils.IsCanceled(r.Ctx) {
		return 0, r.Ctx.Err()
	}

	// 从底层文件读取数据
	n, err = r.File.Read(p)
	if err != nil {
		return
	}

	// 应用速率限制
	if r.Limiter != nil {
		if r.Ctx == nil {
			r.Ctx = context.Background()
		}
		err = r.Limiter.WaitN(r.Ctx, n)
	}
	return
}

// ReadAt 实现了 io.ReaderAt 接口，增加了速率限制
func (r *RateLimitFile) ReadAt(p []byte, off int64) (n int, err error) {
	// 检查上下文是否已取消
	if r.Ctx != nil && utils.IsCanceled(r.Ctx) {
		return 0, r.Ctx.Err()
	}

	// 从底层文件的指定位置读取数据
	n, err = r.File.ReadAt(p, off)
	if err != nil {
		return
	}

	// 应用速率限制
	if r.Limiter != nil {
		if r.Ctx == nil {
			r.Ctx = context.Background()
		}
		err = r.Limiter.WaitN(r.Ctx, n)
	}
	return
}

// Close 实现了 io.Closer 接口
func (r *RateLimitFile) Close() error {
	if c, ok := r.File.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// RateLimitRangeReaderFunc 是一个带速率限制的范围读取函数
type RateLimitRangeReaderFunc RangeReaderFunc

// RangeRead 实现了 model.RangeReaderIF 接口，增加了速率限制
// 它首先调用底层的范围读取函数，然后将结果包装在一个带速率限制的读取器中
func (f RateLimitRangeReaderFunc) RangeRead(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
	// 调用底层范围读取函数
	rc, err := f(ctx, httpRange)
	if err != nil {
		return nil, err
	}

	// 如果设置了服务器下载限制，应用它
	if ServerDownloadLimit != nil {
		rc = &RateLimitReader{
			Ctx:     ctx,
			Reader:  rc,
			Limiter: ServerDownloadLimit,
		}
	}

	return rc, nil
}
