package errs

import (
	"fmt"
	"github.com/pkg/errors"
)

var (
	EmptyToken = errors.New("empty token")
)

var (
	NotImplement = errors.New("not implement")
	NotSupport   = errors.New("not support")
	RelativePath = errors.New("using relative path is not allowed")

	MoveBetweenTwoStorages = errors.New("can't move files between two storages, try to copy")
	UploadNotSupported     = errors.New("upload not supported")

	MetaNotFound     = errors.New("meta not found")
	StorageNotFound  = errors.New("storage not found")
	StreamIncomplete = errors.New("upload/download stream incomplete, possible network issue")
	StreamPeekFail   = errors.New("StreamPeekFail")

	UnknownArchiveFormat      = errors.New("unknown archive format")
	WrongArchivePassword      = errors.New("wrong archive password")
	DriverExtractNotSupported = errors.New("driver extraction not supported")
)

var (
	ObjectNotFound = errors.New("object not found")
	NotFolder      = errors.New("not a folder")
	NotFile        = errors.New("not a file")
)

var (
	PermissionDenied = errors.New("permission denied")
)

var (
	SearchNotAvailable  = errors.Errorf("search not available")
	BuildIndexIsRunning = errors.Errorf("build index is running, please try later")
)

var (
	EmptyUsername      = errors.New("username is empty")
	EmptyPassword      = errors.New("password is empty")
	WrongPassword      = errors.New("password is incorrect")
	DeleteAdminOrGuest = errors.New("cannot delete admin or guest")
)

// NewErr wrap constant error with an extra message
// use errors.Is(err1, StorageNotFound) to check if err belongs to any internal error
func NewErr(err error, format string, a ...any) error {
	return errors.Errorf("%v; %s", err, fmt.Sprintf(format, a...))
}

func IsNotFoundError(err error) bool {
	return errors.Is(errors.Cause(err), ObjectNotFound) || errors.Is(errors.Cause(err), StorageNotFound)
}

func IsObjectNotFound(err error) bool {
	return errors.Is(errors.Cause(err), ObjectNotFound)
}

func IsNotSupportError(err error) bool {
	return errors.Is(errors.Cause(err), NotSupport)
}

func IsNotImplement(err error) bool {
	return errors.Is(errors.Cause(err), NotImplement)
}