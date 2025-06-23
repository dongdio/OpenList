// Package op provides operations for OpenList's core functionality
package op

import (
	stdpath "path"
	"strings"

	"github.com/dongdio/OpenList/internal/errs"

	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/internal/driver"
	"github.com/dongdio/OpenList/pkg/utils"
)

// GetStorageAndActualPath returns the corresponding storage and actual path
// For path: removes the mount path prefix and joins the actual root folder if exists
// Returns:
//   - storage: the driver instance for the storage
//   - actualPath: the path within the storage
//   - err: error if any occurred
func GetStorageAndActualPath(rawPath string) (storage driver.Driver, actualPath string, err error) {
	// Clean and normalize the path
	rawPath = utils.FixAndCleanPath(rawPath)

	// Get the appropriate storage for this path
	storage = GetBalancedStorage(rawPath)
	if storage == nil {
		if rawPath == "/" {
			err = errs.NewErr(errs.StorageNotFound, "please add a storage first")
			return
		}
		err = errs.NewErr(errs.StorageNotFound, "storage not found for path: %s", rawPath)
		return
	}

	log.Debugf("using storage: %s", storage.GetStorage().MountPath)

	// Convert the mount path to the actual path within the storage
	mountPath := utils.GetActualMountPath(storage.GetStorage().MountPath)
	actualPath = utils.FixAndCleanPath(strings.TrimPrefix(rawPath, mountPath))
	return
}

// URLTreeSplitPathAndURL splits a path into directory path and URL components for URL-Tree driver
// Handles different URL-Tree formats:
// 1. /url_tree_driver/file_name[:size[:time]]:https://example.com/file
// 2. /url_tree_driver/https://example.com/file
// Returns:
//   - dirPath: the directory path component
//   - urlPart: the URL or file component
func URLTreeSplitPathAndURL(path string) (dirPath string, urlPart string) {
	// Fix double slashes in URLs that might have been removed
	path = strings.Replace(path, "https:/", "https://", 1)
	path = strings.Replace(path, "http:/", "http://", 1)

	if strings.Contains(path, ":https:/") || strings.Contains(path, ":http:/") {
		// Format: /url_tree_driver/file_name[:size[:time]]:https://example.com/file
		filePath := strings.SplitN(path, ":", 2)[0]
		dirPath, _ = stdpath.Split(filePath)
		urlPart = path[len(dirPath):]
	} else if strings.Contains(path, "/https:/") || strings.Contains(path, "/http:/") {
		// Format: /url_tree_driver/https://example.com/file
		index := strings.Index(path, "/http://")
		if index == -1 {
			index = strings.Index(path, "/https://")
		}
		dirPath = path[:index]
		urlPart = path[index+1:]
	} else {
		// Regular path format
		dirPath, urlPart = stdpath.Split(path)
	}

	// Ensure dirPath is at least "/"
	if dirPath == "" {
		dirPath = "/"
	}

	return
}
