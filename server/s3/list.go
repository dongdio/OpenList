package s3

// Credits: https://pkg.go.dev/github.com/rclone/rclone@v1.65.2/cmd/serve/s3
// Package s3 implements a fake s3 server for OpenList

import (
	"path"
	"strings"
	"time"

	"github.com/itsHenry35/gofakes3"
	log "github.com/sirupsen/logrus"
)

// entryListR recursively lists entries in a directory and adds them to the response
// Parameters:
//   - bucket: the bucket path
//   - fdPath: the current directory path relative to the bucket
//   - name: filter string to match object names
//   - addPrefix: whether to add directories as prefixes or recurse into them
//   - response: the object list to populate
func (b *s3Backend) entryListR(bucket, fdPath, name string, addPrefix bool, response *gofakes3.ObjectList) error {
	// Construct the full path by joining bucket and relative path
	fullPath := path.Join(bucket, fdPath)

	// Get directory entries at the current path
	dirEntries, err := getDirEntries(fullPath)
	if err != nil {
		return err
	}

	if len(dirEntries) == 0 {
		item := &gofakes3.Content{
			// Key:          gofakes3.URLEncode(path.Join(fdPath, emptyObjectName)),
			Key:          path.Join(fdPath, emptyObjectName),
			LastModified: gofakes3.NewContentTime(time.Now()),
			ETag:         getFileHash(nil), // No entry, so no hash
			Size:         0,
			StorageClass: gofakes3.StorageStandard,
		}
		response.Add(item)
		log.Debugf("Adding empty object %s to response", item.Key)
		return nil
	}

	// Process each entry in the directory
	for _, entry := range dirEntries {
		objectName := entry.GetName()

		// Skip entries that don't match the filter
		if !strings.HasPrefix(objectName, name) {
			continue
		}

		// Construct the object path relative to the bucket
		objectPath := path.Join(fdPath, objectName)

		if entry.IsDir() {
			if addPrefix {
				// Add directory as a prefix (for non-recursive listing)
				response.AddPrefix(objectPath)
			} else {
				// Recursively list the subdirectory
				err := b.entryListR(bucket, objectPath, "", false, response)
				if err != nil {
					return err
				}
			}
		} else {
			// Add file as a content item
			item := &gofakes3.Content{
				Key:          objectPath,
				LastModified: gofakes3.NewContentTime(entry.ModTime()),
				ETag:         getFileHash(entry),
				Size:         entry.GetSize(),
				StorageClass: gofakes3.StorageStandard,
			}
			response.Add(item)
		}
	}
	return nil
}
