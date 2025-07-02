package http

import (
	"github.com/pkg/errors"
	"mime"
)

func parseFilenameFromContentDisposition(contentDisposition string) (string, error) {
	if contentDisposition == "" {
		return "", errors.Errorf("Content-Disposition is empty")
	}
	_, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil {
		return "", err
	}
	filename := params["filename"]
	if filename == "" {
		return "", errors.Errorf("filename not found in Content-Disposition: [%s]", contentDisposition)
	}
	return filename, nil
}