package s3

// Credits: https://pkg.go.dev/github.com/rclone/rclone@v1.65.2/cmd/serve/s3
// Package s3 implements a fake s3 server for OpenList

import (
	"context"
	"math/rand"
	"net/http"

	"github.com/itsHenry35/gofakes3"
)

// NewServer creates and configures a new S3 compatible server
// Returns an HTTP handler that can be used to serve S3 API requests
func NewServer(ctx context.Context) (http.Handler, error) {
	// Create logger for the S3 server
	s3Logger := logger{}

	// Configure and create the S3 server with appropriate options
	faker := gofakes3.New(
		newBackend(),
		gofakes3.WithLogger(s3Logger),
		gofakes3.WithRequestID(rand.Uint64()),
		gofakes3.WithoutVersioning(),
		gofakes3.WithV4Auth(authlistResolver()),
		gofakes3.WithIntegrityCheck(true), // Check Content-MD5 if supplied
	)

	return faker.Server(), nil
}
