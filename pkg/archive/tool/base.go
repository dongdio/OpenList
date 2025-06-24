package tool

import (
	"io"

	"github.com/dongdio/OpenList/pkg/stream"

	"github.com/dongdio/OpenList/internal/model"
)

type MultipartExtension struct {
	PartFileFormat  string
	SecondPartIndex int
}

type Tool interface {
	AcceptedExtensions() []string
	AcceptedMultipartExtensions() map[string]MultipartExtension
	GetMeta(ss []*stream.SeekableStream, args model.ArchiveArgs) (model.ArchiveMeta, error)
	List(ss []*stream.SeekableStream, args model.ArchiveInnerArgs) ([]model.Obj, error)
	Extract(ss []*stream.SeekableStream, args model.ArchiveInnerArgs) (io.ReadCloser, int64, error)
	Decompress(ss []*stream.SeekableStream, outputPath string, args model.ArchiveInnerArgs, up model.UpdateProgress) error
}