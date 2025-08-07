package model

import (
	"io"

	"github.com/dongdio/OpenList/v4/utility/errs"
)

// File is basic file level accessing interface
type File interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

type FileCloser struct {
	File
	io.Closer
}

func (f *FileCloser) Close() error {
	var errors []error
	if clr, ok := f.File.(io.Closer); ok {
		errors = append(errors, clr.Close())
	}
	if f.Closer != nil {
		errors = append(errors, f.Closer.Close())
	}
	return errs.Join(errors...)
}

type FileRangeReader struct {
	RangeReaderIF
}