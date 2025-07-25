package s3

import (
	"context"
	"strings"

	"github.com/itsHenry35/gofakes3"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

const emptyObjectName = "ThisIsAnEmptyFolderInTheS3Bucket"

// Bucket represents an S3 bucket configuration
type Bucket struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// getAndParseBuckets retrieves and parses the S3 bucket configurations
func getAndParseBuckets() ([]Bucket, error) {
	var buckets []Bucket
	err := utils.JSONTool.Unmarshal([]byte(setting.GetStr(consts.S3Buckets)), &buckets)
	return buckets, err
}

// getBucketByName finds a bucket by its name
// Returns the bucket if found, or an error if not found or if parsing failed
func getBucketByName(name string) (Bucket, error) {
	buckets, err := getAndParseBuckets()
	if err != nil {
		return Bucket{}, err
	}

	for _, bucket := range buckets {
		if bucket.Name == name {
			return bucket, nil
		}
	}

	return Bucket{}, gofakes3.BucketNotFound(name)
}

// getDirEntries retrieves directory entries at the specified path
// Returns a list of objects or an error if the path doesn't exist or isn't a directory
func getDirEntries(path string) ([]model.Obj, error) {
	ctx := context.Background()
	meta, _ := op.GetNearestMeta(path)
	ctxWithMeta := context.WithValue(ctx, consts.MetaKey, meta)

	fileInfo, err := fs.Get(ctxWithMeta, path, &fs.GetArgs{})
	if err != nil {
		if errs.IsNotFoundError(err) {
			return nil, gofakes3.ErrNoSuchKey
		}
		return nil, gofakes3.ErrNoSuchKey
	}

	if !fileInfo.IsDir() {
		return nil, gofakes3.ErrNoSuchKey
	}

	dirEntries, err := fs.List(ctxWithMeta, path, &fs.ListArgs{})
	if err != nil {
		return nil, err
	}

	return dirEntries, nil
}

// getFileHash returns an empty string as hash calculation is not implemented
// This is a placeholder for future implementation
func getFileHash(node any) string {
	return ""
}

// prefixParser splits a prefix into path and remaining components
// For example, "foo/bar/baz" becomes "foo/bar" and "baz"
func prefixParser(p *gofakes3.Prefix) (path, remaining string) {
	idx := strings.LastIndexByte(p.Prefix, '/')
	if idx < 0 {
		return "", p.Prefix
	}
	return p.Prefix[:idx], p.Prefix[idx+1:]
}

// authlistResolver creates an authentication map from configuration
// Returns nil if no credentials are configured
func authlistResolver() map[string]string {
	accessKeyID := setting.GetStr(consts.S3AccessKeyId)
	secretAccessKey := setting.GetStr(consts.S3SecretAccessKey)

	if accessKeyID == "" && secretAccessKey == "" {
		return nil
	}

	authList := make(map[string]string)
	authList[accessKeyID] = secretAccessKey
	return authList
}
