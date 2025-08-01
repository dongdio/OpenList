package sevenzip

import (
	"errors"
	"io"
	"io/fs"

	"github.com/bodgit/sevenzip"

	"github.com/dongdio/OpenList/v4/utility/stream"

	"github.com/dongdio/OpenList/v4/utility/archive/tool"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

type WrapReader struct {
	Reader *sevenzip.Reader
}

func (r *WrapReader) Files() []tool.SubFile {
	ret := make([]tool.SubFile, 0, len(r.Reader.File))
	for _, f := range r.Reader.File {
		ret = append(ret, &WrapFile{f: f})
	}
	return ret
}

type WrapFile struct {
	f *sevenzip.File
}

func (f *WrapFile) Name() string {
	return f.f.Name
}

func (f *WrapFile) FileInfo() fs.FileInfo {
	return f.f.FileInfo()
}

func (f *WrapFile) Open() (io.ReadCloser, error) {
	return f.f.Open()
}

func getReader(ss []*stream.SeekableStream, password string) (*sevenzip.Reader, error) {
	readerAt, err := stream.NewMultiReaderAt(ss)
	if err != nil {
		return nil, err
	}
	sr, err := sevenzip.NewReaderWithPassword(readerAt, readerAt.Size(), password)
	if err != nil {
		return nil, filterPassword(err)
	}
	return sr, nil
}

func filterPassword(err error) error {
	if err != nil {
		var e *sevenzip.ReadError
		if errors.As(err, &e) && e.Encrypted {
			return errs.WrongArchivePassword
		}
	}
	return err
}