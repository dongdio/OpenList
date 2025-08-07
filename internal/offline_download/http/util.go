package http

import (
	"mime"

	"github.com/dongdio/OpenList/v4/utility/errs"
)

func parseFilenameFromContentDisposition(contentDisposition string) (string, error) {
	if contentDisposition == "" {
		return "", errs.New("Content-Disposition is empty")
	}
	_, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil {
		return "", err
	}
	filename := params["filename"]
	if filename == "" {
		return "", errs.New("filename not found in Content-Disposition: " + contentDisposition)
	}
	return filename, nil
}