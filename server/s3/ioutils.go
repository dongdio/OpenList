package s3

// Credits: https://pkg.go.dev/github.com/rclone/rclone@v1.65.2/cmd/serve/s3
// Package s3 implements a fake s3 server for OpenList

import "io"

// noOpReadCloser implements a no-operation ReadCloser that always returns EOF
type noOpReadCloser struct{}

// readerWithCloser combines an io.Reader with a custom close function
type readerWithCloser struct {
	io.Reader
	closer func() error
}

// Ensure readerWithCloser implements io.ReadCloser
var _ io.ReadCloser = &readerWithCloser{}

// Read implements io.Reader interface for noOpReadCloser
// Always returns EOF without reading any data
func (d noOpReadCloser) Read(b []byte) (n int, err error) {
	return 0, io.EOF
}

// Close implements io.Closer interface for noOpReadCloser
// Does nothing and returns nil
func (d noOpReadCloser) Close() error {
	return nil
}

// limitReadCloser creates a ReadCloser that reads at most sz bytes from rdr
// and calls closer when closed
func limitReadCloser(rdr io.Reader, closer func() error, sz int64) io.ReadCloser {
	return &readerWithCloser{
		Reader: io.LimitReader(rdr, sz),
		closer: closer,
	}
}

// Close implements io.Closer interface for readerWithCloser
// Calls the provided closer function if not nil
func (rwc *readerWithCloser) Close() error {
	if rwc.closer != nil {
		return rwc.closer()
	}
	return nil
}