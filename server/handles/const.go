package handles

// File operation conflict resolution strategies
const (
	// CANCEL aborts the operation when a conflict is detected
	CANCEL = "cancel"

	// OVERWRITE replaces the existing file/folder with the new one
	OVERWRITE = "overwrite"

	// SKIP ignores the current file/folder and continues with the next item
	SKIP = "skip"
)
