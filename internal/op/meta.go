// Package op provides operations for OpenList's core functionality
package op

import (
	stdpath "path"
	"time"

	"github.com/Xhofe/go-cache"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/dongdio/OpenList/internal/db"
	"github.com/dongdio/OpenList/internal/errs"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/pkg/singleflight"
	"github.com/dongdio/OpenList/pkg/utils"
)

// Default cache expiration time for metadata
const metaCacheExpiration = time.Hour

// metaCache stores metadata to reduce database queries
var metaCache = cache.NewMemCache(cache.WithShards[*model.Meta](2))

// metaG prevents duplicate concurrent metadata fetches for the same path
var metaG singleflight.Group[*model.Meta]

// GetNearestMeta returns the nearest metadata for a given path
// It will traverse up the directory tree until it finds metadata or reaches the root
func GetNearestMeta(path string) (*model.Meta, error) {
	return getNearestMeta(utils.FixAndCleanPath(path))
}

// getNearestMeta is the internal implementation of GetNearestMeta
// It recursively searches for metadata starting from the given path and moving up
func getNearestMeta(path string) (*model.Meta, error) {
	meta, err := GetMetaByPath(path)
	if err == nil {
		return meta, nil
	}

	// If error is not "meta not found", return the error
	if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		return nil, err
	}

	// If we've reached the root without finding metadata, return not found
	if path == "/" {
		return nil, errs.MetaNotFound
	}

	// Try the parent directory
	return getNearestMeta(stdpath.Dir(path))
}

// GetMetaByPath returns metadata for the exact path specified
// It will clean the path before searching
func GetMetaByPath(path string) (*model.Meta, error) {
	return getMetaByPath(utils.FixAndCleanPath(path))
}

// getMetaByPath is the internal implementation of GetMetaByPath
// It checks the cache first, then falls back to the database
func getMetaByPath(path string) (*model.Meta, error) {
	// Try to get from cache first
	meta, ok := metaCache.Get(path)
	if ok {
		if meta == nil {
			return nil, errs.MetaNotFound
		}
		return meta, nil
	}

	// Use singleflight to prevent duplicate database queries
	metaRes, err, _ := metaG.Do(path, func() (*model.Meta, error) {
		metaTmp, err := db.GetMetaByPath(path)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Cache negative results to avoid repeated DB queries
				metaCache.Set(path, nil)
				return nil, errs.MetaNotFound
			}
			return nil, errors.Wrap(err, "failed to get meta from database")
		}

		// Cache positive results with expiration
		metaCache.Set(path, metaTmp, cache.WithEx[*model.Meta](metaCacheExpiration))
		return metaTmp, nil
	})

	return metaRes, err
}

// DeleteMetaByID deletes metadata by its ID and removes it from cache
func DeleteMetaByID(id uint) error {
	old, err := db.GetMetaById(id)
	if err != nil {
		return errors.Wrap(err, "failed to get meta before deletion")
	}

	// Remove from cache
	metaCache.Del(old.Path)

	// Delete from database
	return db.DeleteMetaById(id)
}

// UpdateMeta updates metadata and refreshes the cache
func UpdateMeta(meta *model.Meta) error {
	if meta == nil {
		return errors.New("cannot update nil meta")
	}

	meta.Path = utils.FixAndCleanPath(meta.Path)

	// Get the old metadata to find its path
	old, err := db.GetMetaById(meta.ID)
	if err != nil {
		return errors.Wrap(err, "failed to get old meta for update")
	}

	// Remove old path from cache
	metaCache.Del(old.Path)

	// Update in database
	return db.UpdateMeta(meta)
}

// CreateMeta creates new metadata and invalidates the cache for its path
func CreateMeta(meta *model.Meta) error {
	if meta == nil {
		return errors.New("cannot create nil meta")
	}

	meta.Path = utils.FixAndCleanPath(meta.Path)

	// Invalidate cache for this path
	metaCache.Del(meta.Path)

	// Create in database
	return db.CreateMeta(meta)
}

// GetMetaByID retrieves metadata by its ID directly from the database
func GetMetaByID(id uint) (*model.Meta, error) {
	meta, err := db.GetMetaById(id)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get meta by id")
	}
	return meta, nil
}

// GetMetas retrieves a paginated list of all metadata
func GetMetas(pageIndex, pageSize int) (metas []model.Meta, count int64, err error) {
	return db.GetMetas(pageIndex, pageSize)
}
