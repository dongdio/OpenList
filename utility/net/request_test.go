package net

// no http range
//

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/dongdio/OpenList/v4/utility/http_range"
)

// 测试数据
var testData22MB = make([]byte, 1024*1024*22)

func init() {
	// 配置日志格式
	formatter := new(logrus.TextFormatter)
	formatter.TimestampFormat = "2006-01-02T15:04:05.999999999"
	formatter.FullTimestamp = true
	formatter.ForceColors = true
	logrus.SetFormatter(formatter)
	logrus.SetLevel(logrus.DebugLevel)
}

// containsString 检查切片中是否包含指定字符串
func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// dummyHttpRequest 创建一个模拟的 HTTP 响应读取器
func dummyHttpRequest(data []byte, p http_range.Range) io.ReadCloser {
	end := p.Start + p.Length
	if end > int64(len(data)) {
		end = int64(len(data))
	}

	bodyBytes := data[p.Start:end]
	return io.NopCloser(bytes.NewReader(bodyBytes))
}

// TestDownloadOrder 测试分块下载的顺序
func TestDownloadOrder(t *testing.T) {
	// 准备测试数据
	testData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	downloader, invocations, ranges := newDownloadRangeClient(testData)

	// 配置下载器
	concurrency, partSize := 3, 3
	d := NewDownloader(func(d *Downloader) {
		d.Concurrency = concurrency
		d.PartSize = partSize
		d.HttpClient = downloader.HttpRequest
	})

	// 执行下载
	start, length := int64(2), int64(10)
	req := &HttpRequestParams{
		Range: http_range.Range{Start: start, Length: length},
		Size:  int64(len(testData)),
	}
	readCloser, err := d.Download(context.Background(), req)

	// 验证结果
	assert.NoError(t, err, "expect no error")

	resultBuf, err := io.ReadAll(readCloser)
	assert.NoError(t, err, "expect no error reading result")
	assert.Equal(t, int(length), len(resultBuf), "buffer length should match expected length")

	// 计算预期的分块数量
	expectedChunks := int(length)/partSize + 1
	if int(length)%partSize == 0 {
		expectedChunks--
	}
	assert.Equal(t, expectedChunks, *invocations, "API call count should match expected chunks")

	// 验证请求的范围
	expectedRanges := []string{"2-3", "5-3", "8-3", "11-1"}
	for _, rng := range expectedRanges {
		assert.True(t, containsString(*ranges, rng), "expected range %s should be present", rng)
	}
	assert.Equal(t, len(expectedRanges), len(*ranges), "range count should match expected")
}

// TestDownloadSingle 测试单块下载
func TestDownloadSingle(t *testing.T) {
	// 准备测试数据
	testData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	downloader, invocations, ranges := newDownloadRangeClient(testData)

	// 配置下载器为单并发
	concurrency, partSize := 1, 3
	d := NewDownloader(func(d *Downloader) {
		d.Concurrency = concurrency
		d.PartSize = partSize
		d.HttpClient = downloader.HttpRequest
	})

	// 执行下载
	start, length := int64(2), int64(10)
	req := &HttpRequestParams{
		Range: http_range.Range{Start: start, Length: length},
		Size:  int64(len(testData)),
	}
	readCloser, err := d.Download(context.Background(), req)

	// 验证结果
	assert.NoError(t, err, "expect no error")

	resultBuf, err := io.ReadAll(readCloser)
	assert.NoError(t, err, "expect no error reading result")
	assert.Equal(t, int(length), len(resultBuf), "buffer length should match expected length")

	// 单并发应该只有一次 API 调用
	assert.Equal(t, 1, *invocations, "single API call expected")

	// 验证请求的范围
	expectedRanges := []string{"2-10"}
	for _, rng := range expectedRanges {
		assert.True(t, containsString(*ranges, rng), "expected range %s should be present", rng)
	}
	assert.Equal(t, len(expectedRanges), len(*ranges), "range count should match expected")
}

// downloadCaptureClient 是一个捕获下载请求的模拟客户端
type downloadCaptureClient struct {
	mockedHttpRequest    func(params *HttpRequestParams) (*http.Response, error)
	GetObjectInvocations int
	RetrievedRanges      []string
	lock                 sync.Mutex
}

// HttpRequest 实现 HttpRequestFunc 接口
func (c *downloadCaptureClient) HttpRequest(ctx context.Context, params *HttpRequestParams) (*http.Response, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.GetObjectInvocations++

	if params.Range.Length > 0 {
		c.RetrievedRanges = append(c.RetrievedRanges, fmt.Sprintf("%d-%d", params.Range.Start, params.Range.Length))
	}

	return c.mockedHttpRequest(params)
}

// newDownloadRangeClient 创建一个新的下载范围客户端
func newDownloadRangeClient(data []byte) (*downloadCaptureClient, *int, *[]string) {
	capture := &downloadCaptureClient{}

	capture.mockedHttpRequest = func(params *HttpRequestParams) (*http.Response, error) {
		start, end := params.Range.Start, params.Range.Start+params.Range.Length
		if params.Range.Length == -1 || end >= int64(len(data)) {
			end = int64(len(data))
		}
		bodyBytes := data[start:end]

		header := &http.Header{}
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(data)))
		return &http.Response{
			Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
			Header:        *header,
			ContentLength: int64(len(bodyBytes)),
			StatusCode:    http.StatusPartialContent,
		}, nil
	}

	return capture, &capture.GetObjectInvocations, &capture.RetrievedRanges
}
