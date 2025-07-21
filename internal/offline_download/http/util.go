package http

import (
	"mime"

	"github.com/pkg/errors"
)

func parseFilenameFromContentDisposition(contentDisposition string) (string, error) {
	if contentDisposition == "" {
		return "", errors.New("Content-Disposition is empty")
	}
	_, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil {
		return "", err
	}
	filename := params["filename"]
	if filename == "" {
		return "", errors.New("filename not found in Content-Disposition: " + contentDisposition)
	}
	return filename, nil
}
