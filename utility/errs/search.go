package errs

import (
	"github.com/pkg/errors"
)

var (
	SearchNotAvailable  = errors.Errorf("search not available")
	BuildIndexIsRunning = errors.Errorf("build index is running, please try later")
)