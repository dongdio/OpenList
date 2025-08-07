package errs

import (
	"testing"
)

func TestErrs(t *testing.T) {

	err1 := NewErr(StorageNotFound, "please add a storage first")
	t.Logf("err1: %s", err1)
	if !Is(err1, StorageNotFound) {
		t.Errorf("failed, expect %s is %s", err1, StorageNotFound)
	}
	if !Is(Cause(err1), StorageNotFound) {
		t.Errorf("failed, expect %s is %s", err1, StorageNotFound)
	}
	err2 := WithMessage(err1, "failed get storage")
	t.Logf("err2: %s", err2)
	if !Is(err2, StorageNotFound) {
		t.Errorf("failed, expect %s is %s", err2, StorageNotFound)
	}
	if !Is(Cause(err2), StorageNotFound) {
		t.Errorf("failed, expect %s is %s", err2, StorageNotFound)
	}
}