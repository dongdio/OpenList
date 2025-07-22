package stream

import (
	"bytes"
	"context"
	"io"
	"math"
	"os"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go4.org/readerutil"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 错误定义
var (
	ErrInvalidStream        = errors.New("invalid stream")
	ErrInvalidRange         = errors.New("invalid range request")
	ErrOffsetOutOfRange     = errors.New("offset out of range")
	ErrNegativeSeekPosition = errors.New("invalid seek: negative position")
	ErrRangeReadIncomplete  = errors.New("range read did not get all data")
)

// 缓冲池，用于减少内存分配
var bufferPool = sync.Pool{
	New: func() any {
		buffer := make([]byte, 32*1024) // 32KB
		return &buffer
	},
}

// FileStream 表示文件流
type FileStream struct {
	Ctx context.Context
	model.Obj
	io.Reader
	Mimetype          string
	WebPutAsTask      bool
	ForceStreamUpload bool
	Exist             model.Obj // the file existed in the destination, we can reuse some info since we wil overwrite it
	utils.Closers
	tmpFile  *os.File // if present, tmpFile has full content, it will be deleted at last
	peekBuff *bytes.Reader
	mu       sync.Mutex // 保护对 tmpFile 和 peekBuff 的并发访问
}

// GetSize 返回文件的大小
func (f *FileStream) GetSize() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.tmpFile != nil {
		info, err := f.tmpFile.Stat()
		if err == nil {
			return info.Size()
		}
	}
	return f.Obj.GetSize()
}

// GetMimetype 返回文件的MIME类型
func (f *FileStream) GetMimetype() string {
	return f.Mimetype
}

// NeedStore 返回是否需要存储
func (f *FileStream) NeedStore() bool {
	return f.WebPutAsTask
}

// IsForceStreamUpload 返回是否强制流式上传
func (f *FileStream) IsForceStreamUpload() bool {
	return f.ForceStreamUpload
}

// Close 关闭流并清理资源
func (f *FileStream) Close() error {
	var err1, err2 error

	err1 = f.Closers.Close()
	if errors.Is(err1, os.ErrClosed) {
		err1 = nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.tmpFile != nil {
		err2 = os.RemoveAll(f.tmpFile.Name())
		if err2 != nil {
			err2 = errs.NewErr(err2, "failed to remove tmpFile [%s]", f.tmpFile.Name())
		} else {
			f.tmpFile = nil
		}
		if err2 != nil {
			logrus.WithError(err2).WithField("file", f.tmpFile.Name()).Error("failed to remove file")
		}
	}
	return errors.Wrap(err1, "failed to close stream")
}

// GetExist 返回目标中已存在的文件
func (f *FileStream) GetExist() model.Obj {
	return f.Exist
}

// SetExist 设置目标中已存在的文件
func (f *FileStream) SetExist(obj model.Obj) {
	f.Exist = obj
}

// CacheFullInTempFile save all data into tmpFile. Not recommended since it wears disk,
// and can't start upload until the file is written. It's not thread-safe!
func (f *FileStream) CacheFullInTempFile() (model.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if file := f.getFileLocked(); file != nil {
		return file, nil
	}

	// 获取缓冲区
	buffer := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(buffer)

	tmpF, err := utils.CreateTempFile(f.Reader, f.GetSize())
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temp file")
	}

	f.Add(tmpF)
	f.tmpFile = tmpF
	f.Reader = tmpF
	return tmpF, nil
}

// SetTmpFile 设置临时文件
func (f *FileStream) SetTmpFile(r *os.File) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Add(r)
	f.tmpFile = r
	f.Reader = r
}

// getFileLocked 返回文件接口，如果可用（已加锁版本）
func (f *FileStream) getFileLocked() model.File {
	if f.tmpFile != nil {
		return f.tmpFile
	}
	if file, ok := f.Reader.(model.File); ok {
		return file
	}
	return nil
}

// GetFile 返回文件接口，如果可用
func (f *FileStream) GetFile() model.File {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getFileLocked()
}

const InMemoryBufMaxSize = 10 // Megabytes
const InMemoryBufMaxSizeBytes = InMemoryBufMaxSize * 1024 * 1024

// RangeRead have to cache all data first since only Reader is provided.
// also support a peeking RangeRead at very start, but won't buffer more than 10MB data in memory
func (f *FileStream) RangeRead(httpRange http_range.Range) (io.Reader, error) {
	if httpRange.Start < 0 {
		return nil, errors.Wrap(ErrInvalidRange, "start position cannot be negative")
	}

	if httpRange.Length < 0 || httpRange.Start+httpRange.Length > f.GetSize() {
		httpRange.Length = f.GetSize() - httpRange.Start
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// 如果已经有文件缓存，直接使用
	if cache := f.getFileLocked(); cache != nil {
		return io.NewSectionReader(cache, httpRange.Start, httpRange.Length), nil
	}

	// 检查是否可以从预读缓冲区读取
	size := httpRange.Start + httpRange.Length
	if f.peekBuff != nil && size <= int64(f.peekBuff.Len()) {
		return io.NewSectionReader(f.peekBuff, httpRange.Start, httpRange.Length), nil
	}

	// 如果请求的数据量较小，使用内存缓冲
	if size <= InMemoryBufMaxSizeBytes {
		bufSize := min(size, f.GetSize())
		// 使用bytes.Buffer作为io.CopyBuffer的写入对象，CopyBuffer会调用Buffer.ReadFrom
		// 即使被写入的数据量与Buffer.Cap一致，Buffer也会扩大
		buf := make([]byte, bufSize)
		n, err := io.ReadFull(f.Reader, buf)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read data to buffer")
		}
		if n != int(bufSize) {
			return nil, errors.Wrapf(ErrRangeReadIncomplete, "expect=%d, actual=%d", bufSize, n)
		}

		f.peekBuff = bytes.NewReader(buf)
		f.Reader = io.MultiReader(f.peekBuff, f.Reader)
		return io.NewSectionReader(f.peekBuff, httpRange.Start, httpRange.Length), nil
	}

	// 对于大文件，缓存到临时文件
	cache, err := f.CacheFullInTempFile()
	if err != nil {
		return nil, errors.Wrap(err, "failed to cache to temp file")
	}

	return io.NewSectionReader(cache, httpRange.Start, httpRange.Length), nil
}

var _ model.FileStreamer = (*SeekableStream)(nil)
var _ model.FileStreamer = (*FileStream)(nil)

// var _ seekableStream = (*FileStream)(nil)

// SeekableStream for most internal stream, which is either RangeReadCloser or MFile
// Any functionality implemented based on SeekableStream should implement a Close method,
// whose only purpose is to close the SeekableStream object. If such functionality has
// additional resources that need to be closed, they should be added to the Closer property of
// the SeekableStream object and be closed together when the SeekableStream object is closed.
type SeekableStream struct {
	*FileStream
	// should have one of belows to support rangeRead
	rangeReadCloser model.RangeReadCloserIF
	size            int64
	mu              sync.Mutex // 保护并发访问
}

// NewSeekableStream 创建一个新的可定位流
func NewSeekableStream(fs *FileStream, link *model.Link) (*SeekableStream, error) {
	if fs == nil {
		return nil, errors.New("file stream cannot be nil")
	}

	if link == nil {
		return nil, errors.New("link cannot be nil")
	}

	if len(fs.Mimetype) == 0 {
		fs.Mimetype = utils.GetMimeType(fs.Obj.GetName())
	}

	if fs.Reader != nil {
		fs.Add(link)
		return &SeekableStream{FileStream: fs}, nil
	}

	size := link.ContentLength
	if size <= 0 {
		size = fs.GetSize()
	}

	rr, err := GetRangeReaderFromLink(size, link)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create range reader from link")
	}

	if _, ok := rr.(*model.FileRangeReader); ok {
		fs.Reader, err = rr.RangeRead(fs.Ctx, http_range.Range{Length: -1})
		if err != nil {
			return nil, errors.Wrap(err, "failed to read entire file")
		}
		fs.Add(link)
		return &SeekableStream{FileStream: fs, size: size}, nil
	}

	rrc := &model.RangeReadCloser{
		RangeReader: rr,
	}
	fs.Add(link)
	fs.Add(rrc)

	return &SeekableStream{
		FileStream:      fs,
		rangeReadCloser: rrc,
		size:            size,
	}, nil
}

// GetSize 返回流的大小
func (ss *SeekableStream) GetSize() int64 {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.size > 0 {
		return ss.size
	}
	return ss.FileStream.GetSize()
}

// func (ss *SeekableStream) Peek(length int) {
//
// }

// RangeRead is not thread-safe, pls use it in single thread only.
func (ss *SeekableStream) RangeRead(httpRange http_range.Range) (io.Reader, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if httpRange.Start < 0 {
		return nil, errors.Wrap(ErrInvalidRange, "start position cannot be negative")
	}

	if ss.tmpFile == nil && ss.rangeReadCloser != nil {
		rc, err := ss.rangeReadCloser.RangeRead(ss.Ctx, httpRange)
		if err != nil {
			return nil, errors.Wrap(err, "failed to perform range read")
		}
		return rc, nil
	}

	return ss.FileStream.RangeRead(httpRange)
}

// func (f *FileStream) GetReader() io.Reader {
//	return f.Reader
// }

// Read only provide Reader as full stream when it's demanded. in rapid-upload, we can skip this to save memory
func (ss *SeekableStream) Read(p []byte) (n int, err error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.Reader == nil {
		if ss.rangeReadCloser == nil {
			return 0, ErrInvalidStream
		}

		rc, err := ss.rangeReadCloser.RangeRead(ss.Ctx, http_range.Range{Length: -1})
		if err != nil {
			return 0, errors.Wrap(err, "failed to read entire stream")
		}
		ss.Reader = rc
	}

	return ss.Reader.Read(p)
}

// CacheFullInTempFile 将所有数据缓存到临时文件中
func (ss *SeekableStream) CacheFullInTempFile() (model.File, error) {
	if file := ss.GetFile(); file != nil {
		return file, nil
	}

	tmpF, err := utils.CreateTempFile(ss, ss.GetSize())
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temp file")
	}

	ss.Add(tmpF)
	ss.FileStream.mu.Lock()
	defer ss.FileStream.mu.Unlock()

	ss.tmpFile = tmpF
	ss.Reader = tmpF
	return tmpF, nil
}

// ReaderWithSize 是具有大小信息的读取器接口
type ReaderWithSize interface {
	io.ReadCloser
	GetSize() int64
}

// SimpleReaderWithSize 是ReaderWithSize的简单实现
type SimpleReaderWithSize struct {
	io.Reader
	Size int64
}

// GetSize 返回读取器的大小
func (r *SimpleReaderWithSize) GetSize() int64 {
	return r.Size
}

// Close 关闭读取器（如果支持）
func (r *SimpleReaderWithSize) Close() error {
	if c, ok := r.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// ReaderUpdatingProgress 是一个在读取时更新进度的读取器
type ReaderUpdatingProgress struct {
	Reader ReaderWithSize
	model.UpdateProgress
	offset int64
	mu     sync.Mutex // 保护offset的并发访问
}

// Read 读取数据并更新进度
func (r *ReaderUpdatingProgress) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)

	r.mu.Lock()
	r.offset += int64(n)
	progress := math.Min(100.0, float64(r.offset)/float64(r.Reader.GetSize())*100.0)
	r.mu.Unlock()

	r.UpdateProgress(progress)
	return n, err
}

// Close 关闭底层读取器
func (r *ReaderUpdatingProgress) Close() error {
	return r.Reader.Close()
}

// SStreamReadAtSeeker 是可读取、定位的流接口
type SStreamReadAtSeeker interface {
	model.File
	GetRawStream() *SeekableStream
}

// readerCur 表示一个读取器及其当前位置
type readerCur struct {
	reader io.Reader
	cur    int64
}

// RangeReadReadAtSeeker 实现了基于范围读取的读取器
type RangeReadReadAtSeeker struct {
	ss        *SeekableStream
	masterOff int64
	readers   []*readerCur
	headCache *headCache
	mu        sync.Mutex // 保护并发访问
}

// headCache 用于缓存文件头部数据
type headCache struct {
	*readerCur
	bufs [][]byte
	mu   sync.Mutex // 保护并发访问
}

// read 从缓存读取数据
func (c *headCache) read(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pL := len(p)
	logrus.Debugf("headCache read_%d", pL)

	if c.cur < int64(pL) {
		bufL := int64(pL) - c.cur
		buf := make([]byte, bufL)
		lr := io.LimitReader(c.reader, bufL)
		off := 0

		for c.cur < int64(pL) {
			n, err = lr.Read(buf[off:])
			off += n
			c.cur += int64(n)
			if err == io.EOF && off == int(bufL) {
				err = nil
			}
			if err != nil {
				break
			}
		}

		c.bufs = append(c.bufs, buf)
	}

	n = 0
	if c.cur >= int64(pL) {
		for i := 0; n < pL; i++ {
			buf := c.bufs[i]
			r := len(buf)
			if n+r > pL {
				r = pL - n
			}
			n += copy(p[n:], buf[:r])
		}
	}

	return
}

// Close 清理缓存
func (c *headCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.bufs {
		c.bufs[i] = nil
	}
	c.bufs = nil
	return nil
}

// InitHeadCache 初始化头部缓存
func (r *RangeReadReadAtSeeker) InitHeadCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ss.GetFile() == nil && r.masterOff == 0 && len(r.readers) > 0 {
		reader := r.readers[0]
		r.readers = r.readers[1:]
		r.headCache = &headCache{readerCur: reader}
		r.ss.Closers.Add(r.headCache)
	}
}

// NewReadAtSeeker 创建一个新的可读取、定位的读取器
func NewReadAtSeeker(ss *SeekableStream, offset int64, forceRange ...bool) (model.File, error) {
	if ss == nil {
		return nil, errors.New("seekable stream cannot be nil")
	}

	f := ss.GetFile()
	if f != nil {
		_, err := f.Seek(offset, io.SeekStart)
		if err != nil {
			return nil, errors.Wrap(err, "failed to seek file")
		}
		return f, nil
	}

	r := &RangeReadReadAtSeeker{
		ss:        ss,
		masterOff: offset,
	}

	if offset != 0 || utils.IsBool(forceRange...) {
		if offset < 0 || offset > ss.GetSize() {
			return nil, ErrOffsetOutOfRange
		}
		_, err := r.getReaderAtOffset(offset)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get reader at offset")
		}
	} else {
		rc := &readerCur{reader: ss, cur: offset}
		r.readers = append(r.readers, rc)
	}

	return r, nil
}

// NewMultiReaderAt 创建一个多源读取器
func NewMultiReaderAt(ss []*SeekableStream) (readerutil.SizeReaderAt, error) {
	if len(ss) == 0 {
		return nil, errors.New("stream list cannot be empty")
	}

	readers := make([]readerutil.SizeReaderAt, 0, len(ss))

	for _, s := range ss {
		ra, err := NewReadAtSeeker(s, 0)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create reader for stream %s", s.GetName())
		}
		readers = append(readers, io.NewSectionReader(ra, 0, s.GetSize()))
	}

	return readerutil.NewMultiReaderAt(readers...), nil
}

// getReaderAtOffset 获取指定偏移位置的读取器
func (r *RangeReadReadAtSeeker) getReaderAtOffset(off int64) (*readerCur, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var rc *readerCur
	for _, reader := range r.readers {
		if reader.cur == -1 {
			continue
		}
		if reader.cur == off {
			// logrus.Debugf("getReaderAtOffset match_%d", off)
			return reader, nil
		}
		if reader.cur > 0 && off >= reader.cur && (rc == nil || reader.cur < rc.cur) {
			rc = reader
		}
	}

	if rc != nil && off-rc.cur <= utils.MB {
		// 获取缓冲区
		buffer := bufferPool.Get().(*[]byte)
		defer bufferPool.Put(buffer)

		n, err := utils.CopyWithBufferN(io.Discard, rc.reader, off-rc.cur)
		rc.cur += n
		if err == io.EOF && rc.cur == off {
			err = nil
		}
		if err == nil {
			logrus.Debugf("getReaderAtOffset old_%d", off)
			return rc, nil
		}
		rc.cur = -1
	}

	logrus.Debugf("getReaderAtOffset new_%d", off)

	// Range请求不能超过文件大小，有些云盘处理不了就会返回整个文件
	reader, err := r.ss.RangeRead(http_range.Range{Start: off, Length: r.ss.GetSize() - off})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create range reader")
	}

	rc = &readerCur{reader: reader, cur: off}
	r.readers = append(r.readers, rc)
	return rc, nil
}

// ReadAt 从指定位置读取数据
func (r *RangeReadReadAtSeeker) ReadAt(p []byte, off int64) (int, error) {
	if off == 0 && r.headCache != nil {
		return r.headCache.read(p)
	}

	rc, err := r.getReaderAtOffset(off)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get reader at offset %d", off)
	}

	n, num := 0, 0
	for num < len(p) {
		n, err = rc.reader.Read(p[num:])
		rc.cur += int64(n)
		num += n

		if err == nil {
			continue
		}

		if err == io.EOF {
			// io.EOF是reader读取完了
			rc.cur = -1
			// yeka/zip包 没有处理EOF，我们要兼容
			// https://github.com/yeka/zip/blob/03d6312748a9d6e0bc0c9a7275385c09f06d9c14/reader.go#L433
			if num == len(p) {
				err = nil
			}
		}
		break
	}

	return num, err
}

// Seek 定位到指定位置
func (r *RangeReadReadAtSeeker) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch whence {
	case io.SeekStart:
		// 从头开始
	case io.SeekCurrent:
		// 从当前位置
		if offset == 0 {
			return r.masterOff, nil
		}
		offset += r.masterOff
	case io.SeekEnd:
		// 从末尾
		offset += r.ss.GetSize()
	default:
		return 0, errs.NotSupport
	}

	if offset < 0 {
		return r.masterOff, ErrNegativeSeekPosition
	}

	if offset > r.ss.GetSize() {
		offset = r.ss.GetSize()
	}

	r.masterOff = offset
	return offset, nil
}

// Read 读取数据
func (r *RangeReadReadAtSeeker) Read(p []byte) (n int, err error) {
	if r.masterOff == 0 && r.headCache != nil {
		return r.headCache.read(p)
	}

	rc, err := r.getReaderAtOffset(r.masterOff)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get reader at offset %d", r.masterOff)
	}

	n, err = rc.reader.Read(p)
	rc.cur += int64(n)
	r.masterOff += int64(n)
	return n, err
}
