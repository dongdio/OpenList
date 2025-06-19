package base

import (
	"crypto/tls"
	"io"
	"net/http"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/net"
)

var (
	NoRedirectClient *resty.Client
	RestyClient      *resty.Client
	HttpClient       *http.Client
)

const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
const DefaultTimeout = time.Second * 30

func InitClient() {
	NoRedirectClient = resty.New().
		SetRetryCount(3).
		SetTimeout(DefaultTimeout).
		SetRetryWaitTime(2 * time.Second).
		SetRetryMaxWaitTime(3 * time.Second).
		SetRedirectPolicy(resty.NoRedirectPolicy()).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify})
	NoRedirectClient.SetHeader("User-Agent", UserAgent)

	RestyClient = NewRestyClient()
	HttpClient = net.NewHttpClient()
}

func NewRestyClient() *resty.Client {
	client := resty.New().
		SetRetryCount(3).
		SetTimeout(DefaultTimeout).
		SetRetryWaitTime(2*time.Second).
		SetRetryMaxWaitTime(3*time.Second).
		SetAllowMethodGetPayload(true).
		SetRedirectPolicy(resty.FlexibleRedirectPolicy(5)).
		SetHeaders(headers).
		AddContentDecompresser("zstd", decompressZSTD).
		AddContentDecompresser("br", decompressBrotli).
		SetTLSClientConfig(&tls.Config{
			InsecureSkipVerify: conf.Conf.TlsInsecureSkipVerify,
		})
	return client
}

var headers = map[string]string{
	"Sec-Ch-Ua-Platform": `"windows"`,
	"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
	"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Chromium";v="135", "Google Chrome";v="135"`,
	"User-Agent":         UserAgent,
	"Accept":             "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
}

// Create decompressZSTD decompress logic
func decompressZSTD(r io.ReadCloser) (io.ReadCloser, error) {
	zr, err := zstd.NewReader(r, nil)
	if err != nil {
		return nil, err
	}
	z := &zstdReader{s: r, r: zr}
	return z, nil
}

type zstdReader struct {
	s io.ReadCloser
	r *zstd.Decoder
}

func (b *zstdReader) Read(p []byte) (n int, err error) {
	return b.r.Read(p)
}

func (b *zstdReader) Close() error {
	b.r.Close()
	return b.s.Close()
}

// Create Brotli decompress logic
func decompressBrotli(r io.ReadCloser) (io.ReadCloser, error) {
	br := &brotliReader{s: r, r: brotli.NewReader(r)}
	return br, nil
}

type brotliReader struct {
	s io.ReadCloser
	r *brotli.Reader
}

func (b *brotliReader) Read(p []byte) (n int, err error) {
	return b.r.Read(p)
}

func (b *brotliReader) Close() error {
	return b.s.Close()
}