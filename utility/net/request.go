package net

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws/awsutil"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// DefaultDownloadPartSize 是使用 Download() 时默认的字节范围大小
const DefaultDownloadPartSize = utils.MB * 10

// DefaultDownloadConcurrency 是使用 Download() 时默认的并发数
const DefaultDownloadConcurrency = 2

// DefaultPartBodyMaxRetries 是下载分片失败时的默认重试次数
const DefaultPartBodyMaxRetries = 3

// DefaultConcurrencyLimit 是默认的并发限制
var DefaultConcurrencyLimit *ConcurrencyLimit

// Downloader 是一个多线程HTTP下载器
type Downloader struct {
	// PartSize 是每个分片的大小
	PartSize int

	// PartBodyMaxRetries 是分片下载失败时的重试次数
	PartBodyMaxRetries int

	// Concurrency 是并行发送分片的goroutine数量
	// 如果设置为零，将使用DefaultDownloadConcurrency值
	// Concurrency为1时将按顺序下载分片
	Concurrency int

	// HTTPClient 是执行HTTP请求的函数
	HTTPClient HTTPRequestFunc

	// ConcurrencyLimit 是并发限制器
	*ConcurrencyLimit
}

// HTTPRequestFunc 是执行HTTP请求的函数类型
type HTTPRequestFunc func(ctx context.Context, params *HTTPRequestParams) (*http.Response, error)

// NewDownloader 创建一个新的下载器实例
func NewDownloader(options ...func(*Downloader)) *Downloader {
	d := &Downloader{
		PartBodyMaxRetries: DefaultPartBodyMaxRetries,
		ConcurrencyLimit:   DefaultConcurrencyLimit,
	}
	for _, option := range options {
		option(d)
	}
	return d
}

// Download 方法使用多线程HTTP请求从远程URL下载数据
// 每个分块(除了最后一个)的大小为PartSize，缓存部分数据，然后返回包含组装数据的Reader
// 支持范围请求，不支持未知文件大小，如果文件大小不正确将会失败
// 内存使用量约为Concurrency*PartSize，请谨慎使用
func (d Downloader) Download(ctx context.Context, p *HTTPRequestParams) (io.ReadCloser, error) {
	var finalP HTTPRequestParams
	awsutil.Copy(&finalP, p)
	if finalP.Range.Length < 0 || finalP.Range.Start+finalP.Range.Length > finalP.Size {
		finalP.Range.Length = finalP.Size - finalP.Range.Start
	}
	impl := downloader{params: &finalP, cfg: d, ctx: ctx}

	// 设置必需的选项默认值
	if impl.cfg.Concurrency == 0 {
		impl.cfg.Concurrency = DefaultDownloadConcurrency
	}
	if impl.cfg.PartSize == 0 {
		impl.cfg.PartSize = DefaultDownloadPartSize
	}
	if impl.cfg.HTTPClient == nil {
		impl.cfg.HTTPClient = DefaultHTTPRequestFunc
	}

	return impl.download()
}

// downloader 是Downloader内部使用的实现结构
type downloader struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	cfg    Downloader

	params       *HTTPRequestParams // HTTP请求参数
	chunkChannel chan chunk         // 分块通道

	m sync.Mutex

	nextChunk int // 下一个分块ID
	bufs      []*Buf
	written   int64 // 从远程下载的文件总字节数
	err       error

	concurrency int // 剩余的并发数，递减。到0时停止并发
	maxPart     int // 分片总数
	pos         int64
	maxPos      int64
	m2          sync.Mutex
	readingID   int // 正在被读取的ID
}

// ConcurrencyLimit 用于限制并发数
type ConcurrencyLimit struct {
	mu    sync.Mutex
	Limit int // 需要大于0
}

// ErrExceedMaxConcurrency 表示超出最大并发数的错误
var ErrExceedMaxConcurrency = ErrorHTTPStatusCode(http.StatusTooManyRequests)

// sub 减少并发限制计数
func (l *ConcurrencyLimit) sub() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Limit-1 < 0 {
		return ErrExceedMaxConcurrency
	}
	l.Limit--
	return nil
}

// add 增加并发限制计数
func (l *ConcurrencyLimit) add() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Limit++
}

// concurrencyCheck 检查是否超过并发限制
func (d *downloader) concurrencyCheck() error {
	if d.cfg.ConcurrencyLimit != nil {
		return d.cfg.ConcurrencyLimit.sub()
	}
	return nil
}

// concurrencyFinish 完成并发任务，释放并发计数
func (d *downloader) concurrencyFinish() {
	if d.cfg.ConcurrencyLimit != nil {
		d.cfg.ConcurrencyLimit.add()
	}
}

// download 执行对象下载的实现
func (d *downloader) download() (io.ReadCloser, error) {
	if err := d.concurrencyCheck(); err != nil {
		return nil, err
	}

	// 计算分片数量
	maxPart := int(d.params.Range.Length / int64(d.cfg.PartSize))
	if d.params.Range.Length%int64(d.cfg.PartSize) > 0 {
		maxPart++
	}

	// 调整并发数
	if maxPart < d.cfg.Concurrency {
		d.cfg.Concurrency = maxPart
	}
	if d.params.Range.Length == 0 {
		d.cfg.Concurrency = 1
	}
	log.Debugf("download cfgConcurrency:%d", d.cfg.Concurrency)

	// 单分片情况直接下载
	if maxPart == 1 {
		resp, err := d.cfg.HTTPClient(d.ctx, d.params)
		if err != nil {
			d.concurrencyFinish()
			return nil, err
		}
		closeFunc := resp.Body.Close
		resp.Body = utils.NewReadCloser(resp.Body, func() error {
			d.m.Lock()
			defer d.m.Unlock()
			if closeFunc != nil {
				d.concurrencyFinish()
				e := closeFunc()
				closeFunc = nil
				return e
			}
			return nil
		})
		return resp.Body, nil
	}

	// 多分片情况
	d.ctx, d.cancel = context.WithCancelCause(d.ctx)

	// 初始化workers
	d.chunkChannel = make(chan chunk, d.cfg.Concurrency)

	d.maxPart = maxPart
	d.pos = d.params.Range.Start
	d.maxPos = d.params.Range.Start + d.params.Range.Length
	d.concurrency = d.cfg.Concurrency
	_ = d.sendChunkTask(true)

	var rc io.ReadCloser = NewMultiReadCloser(d.bufs[0], d.interrupt, d.finishBuf)

	return rc, d.err
}

// sendChunkTask 发送分块任务
func (d *downloader) sendChunkTask(newConcurrency bool) error {
	d.m.Lock()
	defer d.m.Unlock()

	isNewBuf := d.concurrency > 0
	if newConcurrency {
		if d.concurrency <= 0 {
			return nil
		}
		if d.nextChunk > 0 { // 第一个不检查，因为已经检查过了
			if err := d.concurrencyCheck(); err != nil {
				return err
			}
		}
		d.concurrency--
		go d.downloadPart()
	}

	var buf *Buf
	if isNewBuf {
		buf = NewBuf(d.ctx, d.cfg.PartSize)
		d.bufs = append(d.bufs, buf)
	} else {
		buf = d.getBuf(d.nextChunk)
	}

	if d.pos < d.maxPos {
		finalSize := int64(d.cfg.PartSize)

		// 优化分片大小
		switch d.nextChunk {
		case 0:
			// 调整第一个分片大小以优化视频播放体验
			firstSize := d.params.Range.Length % finalSize
			if firstSize > 0 {
				minSize := finalSize / 2
				if firstSize < minSize { // 最小分片太小就调整到一半
					finalSize = minSize
				} else {
					finalSize = firstSize
				}
			}
		case 1:
			// 调整第二个分片大小
			firstSize := d.params.Range.Length % finalSize
			minSize := finalSize / 2
			if firstSize > 0 && firstSize < minSize {
				finalSize = d.params.Range.Length - firstSize
				if d.cfg.Concurrency > 2 {
					finalSize = finalSize / int64(d.cfg.Concurrency-1)
				}
			}
		}

		// 确保不超出范围
		if d.pos+finalSize > d.maxPos {
			finalSize = d.maxPos - d.pos
		}

		// 创建分块任务
		ch := chunk{
			start:          d.pos,
			size:           finalSize,
			buf:            buf,
			id:             d.nextChunk,
			newConcurrency: newConcurrency,
		}

		// 更新位置和下一个分块ID
		d.pos += finalSize
		d.nextChunk++

		// 发送分块任务
		select {
		case <-d.ctx.Done():
			return context.Cause(d.ctx)
		case d.chunkChannel <- ch:
		}
	}

	return nil
}

// interrupt 中断下载
func (d *downloader) interrupt() error {
	if d.cancel != nil {
		d.cancel(errors.New("download interrupted"))
	}
	return d.getErr()
}

// getBuf 获取指定ID的缓冲区
func (d *downloader) getBuf(id int) (b *Buf) {
	return d.bufs[id]
}

// finishBuf 完成缓冲区读取
func (d *downloader) finishBuf(id int) (isLast bool, nextBuf *Buf) {
	d.m.Lock()
	defer d.m.Unlock()

	nextID := id + 1
	if nextID >= len(d.bufs) {
		return true, nil
	}

	// 发送新的分块任务
	d.readingID = nextID
	if err := d.sendChunkTask(false); err != nil {
		d.setErr(err)
		return true, nil
	}

	return false, d.bufs[nextID]
}

// downloadPart 下载分片
func (d *downloader) downloadPart() {
	defer d.concurrencyFinish()

	for {
		select {
		case <-d.ctx.Done():
			return
		case ch, ok := <-d.chunkChannel:
			if !ok {
				return
			}

			// 下载分块
			if err := d.downloadChunk(&ch); err != nil {
				d.setErr(err)
				d.cancel(err)
				return
			}

			// 如果是新的并发任务，继续发送分块任务
			if ch.newConcurrency {
				if err := d.sendChunkTask(true); err != nil {
					if !errors.Is(err, ErrExceedMaxConcurrency) {
						d.setErr(err)
						d.cancel(err)
					}
					return
				}
			}
		}
	}
}

// downloadChunk 下载单个分块
func (d *downloader) downloadChunk(ch *chunk) error {
	var (
		n   int64
		err error
	)

	// 重试逻辑
	for retry := 0; retry <= d.cfg.PartBodyMaxRetries; retry++ {
		params := d.getParamsFromChunk(ch)
		n, err = d.tryDownloadChunk(params, ch)

		if err == nil {
			break
		}

		// 检查是否需要重试
		if retry == d.cfg.PartBodyMaxRetries || !errors.Is(errors.Unwrap(err), &errNeedRetry{}) {
			return err
		}

		// 重试延迟
		delay := time.Duration(retry+1) * 100 * time.Millisecond
		select {
		case <-time.After(delay):
		case <-d.ctx.Done():
			return context.Cause(d.ctx)
		}
	}

	d.incrWritten(n)
	return nil
}

var errCancelConcurrency = errors.New("cancel concurrency")
var errInfiniteRetry = errors.New("infinite retry")

// tryDownloadChunk 尝试下载分块
func (d *downloader) tryDownloadChunk(params *HTTPRequestParams, ch *chunk) (int64, error) {
	resp, err := d.cfg.HTTPClient(d.ctx, params)
	if err != nil {
		statusCode, ok := errors.Unwrap(err).(ErrorHTTPStatusCode)
		if !ok {
			return 0, err
		}
		if statusCode == http.StatusRequestedRangeNotSatisfiable {
			return 0, err
		}
		if ch.id == 0 { // 第1个任务 有限的重试，超过重试就会结束请求
			switch statusCode {
			default:
				return 0, err
			case http.StatusTooManyRequests:
			case http.StatusBadGateway:
			case http.StatusServiceUnavailable:
			case http.StatusGatewayTimeout:
			}
			<-time.After(time.Millisecond * 200)
			return 0, &errNeedRetry{err: err}
		}

		// 来到这 说明第1个分片下载 连接成功了
		// 后续分片下载出错都当超载处理
		log.Debugf("err chunk_%d, try downloading:%v", ch.id, err)

		d.m.Lock()
		isCancelConcurrency := ch.newConcurrency
		if d.concurrency > 0 { // 取消剩余的并发任务
			// 用于计算实际的并发数
			d.concurrency = -d.concurrency
			isCancelConcurrency = true
		}
		if isCancelConcurrency {
			d.concurrency--
			d.chunkChannel <- *ch
			d.m.Unlock()
			return 0, errCancelConcurrency
		}
		d.m.Unlock()
		if ch.id != d.readingID { // 正在被读取的优先重试
			d.m2.Lock()
			defer d.m2.Unlock()
			<-time.After(time.Millisecond * 200)
		}
		return 0, errInfiniteRetry
	}
	defer resp.Body.Close()

	// only check file size on the first task
	if ch.id == 0 {
		err = d.checkTotalBytes(resp)
		if err != nil {
			return 0, err
		}
	}
	_ = d.sendChunkTask(true)
	n, err := utils.CopyWithBuffer(ch.buf, resp.Body)

	if err != nil {
		return n, &errNeedRetry{err: err}
	}
	if n != ch.size {
		err = fmt.Errorf("chunk download size incorrect, expected=%d, got=%d", ch.size, n)
		return n, &errNeedRetry{err: err}
	}

	return n, nil
}

// getParamsFromChunk 从分块获取HTTP请求参数
func (d *downloader) getParamsFromChunk(ch *chunk) *HTTPRequestParams {
	params := *d.params
	params.Range.Start = ch.start
	params.Range.Length = ch.size
	return &params
}

// checkTotalBytes 检查响应中的总字节数
func (d *downloader) checkTotalBytes(resp *http.Response) error {
	if d.params.Size <= 0 {
		return nil
	}

	// 从Content-Range头解析总大小
	contentRange := resp.Header.Get("Content-Range")
	if contentRange == "" {
		return nil
	}

	// 解析Content-Range头
	parts := strings.Split(contentRange, "/")
	if len(parts) != 2 {
		return nil
	}

	totalBytes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil
	}

	// 验证总大小
	if totalBytes != d.params.Size {
		return errors.Errorf("expected file size %d, got %d from Content-Range", d.params.Size, totalBytes)
	}

	return nil
}

// incrWritten 增加已写入的字节数
func (d *downloader) incrWritten(n int64) {
	d.m.Lock()
	defer d.m.Unlock()
	d.written += n
}

// getErr 获取错误
func (d *downloader) getErr() error {
	d.m.Lock()
	defer d.m.Unlock()
	return d.err
}

// setErr 设置错误
func (d *downloader) setErr(e error) {
	d.m.Lock()
	defer d.m.Unlock()
	if d.err == nil {
		d.err = e
	}
}

// chunk 表示一个下载分块
type chunk struct {
	start          int64
	size           int64
	buf            *Buf
	id             int
	newConcurrency bool
}

// DefaultHTTPRequestFunc 是默认的HTTP请求函数
func DefaultHTTPRequestFunc(ctx context.Context, params *HTTPRequestParams) (*http.Response, error) {
	header := http_range.ApplyRangeToHTTPHeader(params.Range, params.HeaderRef)
	return RequestHTTP(ctx, "GET", header, params.URL)
}

func GetRangeReaderHTTPRequestFunc(rangeReader model.RangeReaderIF) HTTPRequestFunc {
	return func(ctx context.Context, params *HTTPRequestParams) (*http.Response, error) {
		rc, err := rangeReader.RangeRead(ctx, params.Range)
		if err != nil {
			return nil, err
		}

		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Status:     http.StatusText(http.StatusPartialContent),
			Body:       rc,
			Header: http.Header{
				"Content-Range": {params.Range.ContentRange(params.Size)},
			},
			ContentLength: params.Range.Length,
		}, nil
	}

}

// HTTPRequestParams 包含HTTP请求的参数
type HTTPRequestParams struct {
	URL       string
	Range     http_range.Range // 只需要此范围内的数据
	HeaderRef http.Header
	Size      int64 // 文件总大小
}

// errNeedRetry 表示需要重试的错误
type errNeedRetry struct {
	err error
}

// Error 实现error接口
func (e *errNeedRetry) Error() string {
	return fmt.Sprintf("need retry: %v", e.err)
}

// Unwrap 返回底层错误
func (e *errNeedRetry) Unwrap() error {
	return e.err
}

// MultiReadCloser 是一个多重读取关闭器
type MultiReadCloser struct {
	cfg    *cfg
	closer closerFunc
	finish finishBufFunc
}

// cfg 是MultiReadCloser的配置
type cfg struct {
	rPos   int // 当前读取位置，从0开始
	curBuf *Buf
}

// closerFunc 是关闭函数类型
type closerFunc func() error

// finishBufFunc 是完成缓冲区函数类型
type finishBufFunc func(id int) (isLast bool, buf *Buf)

// NewMultiReadCloser 创建一个新的多重读取关闭器
func NewMultiReadCloser(buf *Buf, c closerFunc, fb finishBufFunc) *MultiReadCloser {
	return &MultiReadCloser{cfg: &cfg{curBuf: buf}, closer: c, finish: fb}
}

// Read 实现io.Reader接口
func (mr MultiReadCloser) Read(p []byte) (n int, err error) {
	for {
		// 从当前缓冲区读取
		n, err = mr.cfg.curBuf.Read(p)
		if err == nil || err != io.EOF {
			return n, err
		}

		// 当前缓冲区已读完，获取下一个缓冲区
		isLast, nextBuf := mr.finish(mr.cfg.rPos)
		if isLast {
			return n, io.EOF
		}

		// 更新当前缓冲区和位置
		mr.cfg.rPos++
		mr.cfg.curBuf = nextBuf

		// 如果已读取了一些数据，先返回
		if n > 0 {
			return n, nil
		}
	}
}

// Close 实现io.Closer接口
func (mr MultiReadCloser) Close() error {
	return mr.closer()
}

// Buf 是一个缓冲区
type Buf struct {
	buffer *bytes.Buffer
	size   int // 预期大小
	ctx    context.Context
	off    int
	mu     sync.Mutex

	readSignal  chan struct{}
	readPending bool
}

// NewBuf 创建一个新的缓冲区
func NewBuf(ctx context.Context, maxSize int) *Buf {
	return &Buf{
		buffer:     bytes.NewBuffer(make([]byte, 0, maxSize)),
		size:       maxSize,
		ctx:        ctx,
		readSignal: make(chan struct{}, 1),
	}
}

// Reset 重置缓冲区
func (br *Buf) Reset(size int) {
	br.mu.Lock()
	defer br.mu.Unlock()

	br.buffer.Reset()
	br.off = 0
	br.size = size
	br.readPending = false

	// 清空读取信号
	select {
	case <-br.readSignal:
	default:
	}
}

// Read 实现io.Reader接口
func (br *Buf) Read(p []byte) (n int, err error) {
	br.mu.Lock()

	// 检查是否有足够的数据可读
	if br.buffer.Len() == 0 && br.off < br.size {
		// 标记为等待读取
		br.readPending = true
		br.mu.Unlock()

		// 等待数据写入
		select {
		case <-br.readSignal:
		case <-br.ctx.Done():
			return 0, context.Cause(br.ctx)
		}

		br.mu.Lock()
	}

	// 读取数据
	n, err = br.buffer.Read(p)
	br.off += n

	// 检查是否已读取完毕
	if br.off >= br.size && err == io.EOF {
		br.mu.Unlock()
		return n, io.EOF
	}

	// 如果缓冲区已空但未读取完毕，返回nil错误
	if err == io.EOF {
		err = nil
	}

	br.mu.Unlock()
	return n, err
}

// Write 实现io.Writer接口
func (br *Buf) Write(p []byte) (n int, err error) {
	br.mu.Lock()
	defer br.mu.Unlock()

	// 写入数据
	n, err = br.buffer.Write(p)

	// 如果有等待读取的操作，发送信号
	if br.readPending {
		br.readPending = false
		select {
		case br.readSignal <- struct{}{}:
		default:
		}
	}

	return n, err
}

// Close 关闭缓冲区
func (br *Buf) Close() {
	br.mu.Lock()
	defer br.mu.Unlock()

	// 标记为已读取完毕
	br.off = br.size

	// 如果有等待读取的操作，发送信号
	if br.readPending {
		br.readPending = false
		select {
		case br.readSignal <- struct{}{}:
		default:
		}
	}
}
