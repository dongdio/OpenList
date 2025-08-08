package errs

import (
	"fmt"
)

var (
	EmptyToken = New("empty token")
)

var (
	NotImplement = New("not implement")
	NotSupport   = New("not support")
	RelativePath = New("using relative path is not allowed")

	MoveBetweenTwoStorages = New("can't move files between two storages, try to copy")
	UploadNotSupported     = New("upload not supported")

	MetaNotFound     = New("meta not found")
	StorageNotFound  = New("storage not found")
	StreamIncomplete = New("upload/download stream incomplete, possible network issue")
	StreamPeekFail   = New("StreamPeekFail")

	UnknownArchiveFormat      = New("unknown archive format")
	WrongArchivePassword      = New("wrong archive password")
	DriverExtractNotSupported = New("driver extraction not supported")
)

var (
	ObjectNotFound = New("object not found")
	NotFolder      = New("not a folder")
	NotFile        = New("not a file")
)

var (
	PermissionDenied = New("permission denied")
)

var (
	SearchNotAvailable  = Errorf("search not available")
	BuildIndexIsRunning = Errorf("build index is running, please try later")
)

var (
	EmptyUsername      = New("username is empty")
	EmptyPassword      = New("password is empty")
	WrongPassword      = New("password is incorrect")
	DeleteAdminOrGuest = New("cannot delete admin or guest")
)

// NewErr wrap constant error with an extra message
// use Is(err1, StorageNotFound) to check if err belongs to any internal error
func NewErr(err error, format string, a ...any) error {
	return Errorf("%v; %s", err, fmt.Sprintf(format, a...))
}

func IsNotFoundError(err error) bool {
	return Is(Cause(err), ObjectNotFound) || Is(Cause(err), StorageNotFound)
}

func IsObjectNotFound(err error) bool {
	return Is(Cause(err), ObjectNotFound)
}

func IsNotSupportError(err error) bool {
	return Is(Cause(err), NotSupport)
}

func IsNotImplement(err error) bool {
	return Is(Cause(err), NotImplement)
}