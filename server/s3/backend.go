package s3

// Credits: https://pkg.go.dev/github.com/rclone/rclone@v1.65.2/cmd/serve/s3
// Package s3 implements a fake s3 server for alist

import (
	"context"
	"encoding/hex"
	"io"
	"maps"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/itsHenry35/gofakes3"
	"github.com/ncw/swift/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

var (
	// emptyPrefix represents an empty S3 prefix
	emptyPrefix = &gofakes3.Prefix{}
	// timeFormat defines the time format used in S3 responses
	timeFormat = "Mon, 2 Jan 2006 15:04:05 GMT"
)

// s3Backend implements the gofakes3.Backend interface to provide
// an S3 compatible backend for OpenList
type s3Backend struct {
	meta *sync.Map // Stores object metadata
}

// newBackend creates a new S3 backend instance
func newBackend() gofakes3.Backend {
	return &s3Backend{
		meta: new(sync.Map),
	}
}

// ListBuckets returns all configured buckets
func (b *s3Backend) ListBuckets(ctx context.Context) ([]gofakes3.BucketInfo, error) {
	buckets, err := getAndParseBuckets()
	if err != nil {
		return nil, err
	}

	var response []gofakes3.BucketInfo
	for _, bucket := range buckets {
		node, _ := fs.Get(ctx, bucket.Path, &fs.GetArgs{})
		response = append(response, gofakes3.BucketInfo{
			Name:         bucket.Name,
			CreationDate: gofakes3.NewContentTime(node.ModTime()),
		})
	}
	return response, nil
}

// ListBucket lists objects in the specified bucket with optional prefix and pagination
func (b *s3Backend) ListBucket(ctx context.Context, bucketName string, prefix *gofakes3.Prefix, page gofakes3.ListBucketPage) (*gofakes3.ObjectList, error) {
	bucket, err := getBucketByName(bucketName)
	if err != nil {
		return nil, err
	}
	bucketPath := bucket.Path

	// Use empty prefix if none provided
	if prefix == nil {
		prefix = emptyPrefix
	}

	// Handle empty prefix and delimiter cases
	if strings.TrimSpace(prefix.Prefix) == "" {
		prefix.HasPrefix = false
	}
	if strings.TrimSpace(prefix.Delimiter) == "" {
		prefix.HasDelimiter = false
	}

	response := gofakes3.NewObjectList()
	pathTmp, remaining := prefixParser(prefix)

	// List entries recursively
	err = b.entryListR(bucketPath, pathTmp, remaining, prefix.HasDelimiter, response)
	if errors.Is(err, gofakes3.ErrNoSuchKey) {
		// AWS returns an empty list for non-existent paths
		response = gofakes3.NewObjectList()
	} else if err != nil {
		return nil, err
	}

	// Apply pagination
	return b.pager(response, page)
}

// HeadObject returns metadata for the specified object without retrieving its contents
func (b *s3Backend) HeadObject(ctx context.Context, bucketName, objectName string) (*gofakes3.Object, error) {
	bucket, err := getBucketByName(bucketName)
	if err != nil {
		return nil, err
	}

	// Construct full path to the object
	objectPath := path.Join(bucket.Path, objectName)

	// Get file metadata and information
	fileMeta, _ := op.GetNearestMeta(objectPath)
	node, err := fs.Get(context.WithValue(ctx, "meta", fileMeta), objectPath, &fs.GetArgs{})
	if err != nil {
		return nil, gofakes3.KeyNotFound(objectName)
	}

	// Directories are not valid S3 objects
	if node.IsDir() {
		return nil, gofakes3.KeyNotFound(objectName)
	}

	// Prepare metadata
	size := node.GetSize()
	meta := map[string]string{
		"Last-Modified": node.ModTime().Format(timeFormat),
		"Content-Type":  utils.GetMimeType(objectPath),
	}

	// Add custom metadata if available
	if val, ok := b.meta.Load(objectPath); ok {
		metaMap := val.(map[string]string)
		maps.Copy(meta, metaMap)
	}

	return &gofakes3.Object{
		Name:     objectName,
		Metadata: meta,
		Size:     size,
		Contents: noOpReadCloser{},
	}, nil
}

// GetObject retrieves an object from the filesystem
func (b *s3Backend) GetObject(ctx context.Context, bucketName, objectName string, rangeRequest *gofakes3.ObjectRangeRequest) (obj *gofakes3.Object, err error) {
	bucket, err := getBucketByName(bucketName)
	if err != nil {
		return nil, err
	}

	// Construct full path to the object
	objectPath := path.Join(bucket.Path, objectName)

	// Get file metadata and information
	fileMeta, _ := op.GetNearestMeta(objectPath)
	ctxWithMeta := context.WithValue(ctx, "meta", fileMeta)
	node, err := fs.Get(ctxWithMeta, objectPath, &fs.GetArgs{})
	if err != nil {
		return nil, gofakes3.KeyNotFound(objectName)
	}

	// Directories are not valid S3 objects
	if node.IsDir() {
		return nil, gofakes3.KeyNotFound(objectName)
	}

	// Get file link for reading
	link, file, err := fs.Link(ctx, objectPath, model.LinkArgs{})
	if err != nil {
		return nil, err
	}

	// Process range request if present
	size := file.GetSize()
	var fileRange *gofakes3.ObjectRange
	if rangeRequest != nil {
		fileRange, err = rangeRequest.Range(size)
		if err != nil {
			return nil, err
		}
	}

	// Ensure the storage driver supports required functionality
	if link.RangeReadCloser == nil && link.MFile == nil && len(link.URL) == 0 {
		return nil, errors.New("the remote storage driver need to be enhanced to support s3")
	}

	// Set up the reader based on available options
	var reader io.ReadCloser
	startOffset := int64(0)
	length := int64(-1)

	if fileRange != nil {
		startOffset, length = fileRange.Start, fileRange.Length
	}

	if link.MFile != nil {
		// Use memory-mapped file if available
		_, err = link.MFile.Seek(startOffset, io.SeekStart)
		if err != nil {
			return nil, err
		}
		if rdr2, ok := link.MFile.(io.ReadCloser); ok {
			reader = rdr2
		} else {
			reader = io.NopCloser(link.MFile)
		}
	} else {
		// Use range reader for remote files
		// Adjust length if it would exceed file size
		remoteFileSize := file.GetSize()
		if length >= 0 && startOffset+length >= remoteFileSize {
			length = -1
		}

		rangeReadCloser := link.RangeReadCloser

		// Convert URL to range reader if needed
		if len(link.URL) > 0 {
			converted, err := stream.GetRangeReadCloserFromLink(remoteFileSize, link)
			if err != nil {
				return nil, err
			}
			rangeReadCloser = converted
		}

		if rangeReadCloser != nil {
			remoteReader, err := rangeReadCloser.RangeRead(ctx, http_range.Range{Start: startOffset, Length: length})
			if err != nil {
				return nil, err
			}
			reader = utils.ReadCloser{Reader: remoteReader, Closer: rangeReadCloser}
		} else {
			return nil, errs.NotSupport
		}
	}

	// Prepare metadata
	meta := map[string]string{
		"Last-Modified": node.ModTime().Format(timeFormat),
		"Content-Type":  utils.GetMimeType(objectPath),
	}

	// Add custom metadata if available
	if val, ok := b.meta.Load(objectPath); ok {
		metaMap := val.(map[string]string)
		maps.Copy(meta, metaMap)
	}

	return &gofakes3.Object{
		Name:     objectName,
		Metadata: meta,
		Size:     size,
		Range:    fileRange,
		Contents: reader,
	}, nil
}

// TouchObject creates or updates metadata on specified object
// Currently not implemented
func (b *s3Backend) TouchObject(ctx context.Context, objectPath string, meta map[string]string) (result gofakes3.PutObjectResult, err error) {
	// TODO: implement
	return result, gofakes3.ErrNotImplemented
}

// PutObject creates or overwrites an object
func (b *s3Backend) PutObject(
	ctx context.Context, bucketName, objectName string,
	meta map[string]string,
	input io.Reader, size int64,
) (result gofakes3.PutObjectResult, err error) {
	bucket, err := getBucketByName(bucketName)
	if err != nil {
		return result, err
	}

	// Check if this is a directory object (ends with '/')
	isDir := strings.HasSuffix(objectName, "/")
	log.Debugf("isDir: %v", isDir)

	// Construct full path to the object
	objectPath := path.Join(bucket.Path, objectName)
	log.Debugf("objectPath: %s, bucketPath: %s, objectName: %s", objectPath, bucket.Path, objectName)

	// Determine the target path for the operation
	var targetPath string
	if isDir {
		targetPath = objectPath + "/"
	} else {
		targetPath = path.Dir(objectPath)
	}
	log.Debugf("targetPath: %s", targetPath)

	// Get metadata and prepare context
	fileMeta, _ := op.GetNearestMeta(objectPath)
	ctxWithMeta := context.WithValue(ctx, "meta", fileMeta)

	// Check if the target path exists
	_, err = fs.Get(ctxWithMeta, targetPath, &fs.GetArgs{})
	if err != nil {
		if errs.IsObjectNotFound(err) && strings.Contains(objectName, "/") {
			// Create parent directories if needed
			log.Debugf("targetPath: %s not found and objectName contains /, need to makeDir", targetPath)
			err = fs.MakeDir(ctxWithMeta, targetPath, true)
			if err != nil {
				return result, errors.WithMessagef(err, "failed to makeDir, targetPath: %s", targetPath)
			}
		} else {
			return result, gofakes3.KeyNotFound(objectName)
		}
	}

	// For directory objects, just ensure the directory exists
	if isDir {
		return result, nil
	}

	// Extract modification time from metadata if available
	var modTime time.Time
	if val, ok := meta["X-Amz-Meta-Mtime"]; ok {
		modTime, _ = swift.FloatStringToTime(val)
	} else if val, ok = meta["mtime"]; ok {
		modTime, _ = swift.FloatStringToTime(val)
	}

	// Prepare object for upload
	obj := model.Object{
		Name:     path.Base(objectPath),
		Size:     size,
		Modified: modTime,
		Ctime:    time.Now(),
	}

	// Set up stream parameters
	streamParam := &stream.FileStream{
		Obj:      &obj,
		Reader:   input,
		Mimetype: meta["Content-Type"],
	}

	// Upload the file
	err = fs.PutDirectly(ctxWithMeta, targetPath, streamParam)
	if err != nil {
		return result, err
	}

	// Close the stream and handle any errors
	if err = streamParam.Close(); err != nil {
		// Remove file if close operation failed
		_ = fs.Remove(ctxWithMeta, objectPath)
		return result, err
	}

	// Store metadata for future reference
	b.meta.Store(objectPath, meta)

	return result, nil
}

// DeleteMulti deletes multiple objects in a single request
func (b *s3Backend) DeleteMulti(ctx context.Context, bucketName string, objects ...string) (result gofakes3.MultiDeleteResult, rerr error) {
	for _, objectName := range objects {
		err := b.deleteObject(ctx, bucketName, objectName)
		if err == nil {
			result.Deleted = append(result.Deleted, gofakes3.ObjectID{
				Key: objectName,
			})
			continue
		}
		log.Errorf("serve s3, delete object failed: %v", err)
		result.Error = append(result.Error, gofakes3.ErrorResult{
			Code:    gofakes3.ErrInternal,
			Message: gofakes3.ErrInternal.Message(),
			Key:     objectName,
		})
	}

	return result, nil
}

// DeleteObject deletes a single object
func (b *s3Backend) DeleteObject(ctx context.Context, bucketName, objectName string) (result gofakes3.ObjectDeleteResult, rerr error) {
	return result, b.deleteObject(ctx, bucketName, objectName)
}

// deleteObject is a helper method to delete an object from the filesystem
func (b *s3Backend) deleteObject(ctx context.Context, bucketName, objectName string) error {
	bucket, err := getBucketByName(bucketName)
	if err != nil {
		return err
	}

	// Construct full path to the object
	objectPath := path.Join(bucket.Path, objectName)

	// Get metadata and prepare context
	fileMeta, _ := op.GetNearestMeta(objectPath)
	ctxWithMeta := context.WithValue(ctx, "meta", fileMeta)

	// S3 does not report an error when attempting to delete a key that does not exist
	// So we need to skip IsNotExist errors
	if _, err = fs.Get(ctxWithMeta, objectPath, &fs.GetArgs{}); err != nil && !errs.IsObjectNotFound(err) {
		return err
	}

	// Remove the object
	return fs.Remove(ctx, objectPath)
}

// CreateBucket creates a new bucket (not implemented)
func (b *s3Backend) CreateBucket(ctx context.Context, name string) error {
	return gofakes3.ErrNotImplemented
}

// DeleteBucket deletes a bucket (not implemented)
func (b *s3Backend) DeleteBucket(ctx context.Context, name string) error {
	return gofakes3.ErrNotImplemented
}

// BucketExists checks if the specified bucket exists
func (b *s3Backend) BucketExists(ctx context.Context, name string) (exists bool, err error) {
	buckets, err := getAndParseBuckets()
	if err != nil {
		return false, err
	}

	for _, bucket := range buckets {
		if bucket.Name == name {
			return true, nil
		}
	}

	return false, nil
}

// CopyObject copies an object from source to destination
func (b *s3Backend) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, meta map[string]string) (result gofakes3.CopyObjectResult, err error) {
	// If source and destination are the same, just update metadata (not implemented yet)
	if srcBucket == dstBucket && srcKey == dstKey {
		// TODO: update metadata
		return result, nil
	}

	// Get source bucket
	srcBucketObj, err := getBucketByName(srcBucket)
	if err != nil {
		return result, err
	}

	// Construct full path to the source object
	srcPath := path.Join(srcBucketObj.Path, srcKey)

	// Get metadata and prepare context
	fileMeta, _ := op.GetNearestMeta(srcPath)
	ctxWithMeta := context.WithValue(ctx, "meta", fileMeta)

	// Get source object info
	srcNode, err := fs.Get(ctxWithMeta, srcPath, &fs.GetArgs{})
	if err != nil {
		return result, err
	}

	// Get the object content
	sourceObj, err := b.GetObject(ctx, srcBucket, srcKey, nil)
	if err != nil {
		return result, err
	}
	defer func() {
		_ = sourceObj.Contents.Close()
	}()

	// Merge metadata from source and destination
	for k, v := range sourceObj.Metadata {
		if _, found := meta[k]; !found && k != "X-Amz-Acl" {
			meta[k] = v
		}
	}

	// Set modification time if not provided
	if _, ok := meta["mtime"]; !ok {
		meta["mtime"] = swift.TimeToFloatString(srcNode.ModTime())
	}

	// Put the object at the destination
	_, err = b.PutObject(ctx, dstBucket, dstKey, meta, sourceObj.Contents, sourceObj.Size)
	if err != nil {
		return result, err
	}

	// Return success result
	return gofakes3.CopyObjectResult{
		ETag:         `"` + hex.EncodeToString(sourceObj.Hash) + `"`,
		LastModified: gofakes3.NewContentTime(srcNode.ModTime()),
	}, nil
}