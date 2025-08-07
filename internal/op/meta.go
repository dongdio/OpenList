// Package op provides operations for OpenList's core functionality
package op

import (
	stdpath "path"
	"time"

	"github.com/OpenListTeam/go-cache"
	"gorm.io/gorm"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/singleflight"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

var metaCache = cache.NewMemCache(cache.WithShards[*model.Meta](2))

// metaG maybe not needed
var metaG singleflight.Group[*model.Meta]

func GetNearestMeta(path string) (*model.Meta, error) {
	return getNearestMeta(utils.FixAndCleanPath(path))
}
func getNearestMeta(path string) (*model.Meta, error) {
	meta, err := GetMetaByPath(path)
	if err == nil {
		return meta, nil
	}
	err = errs.Cause(err)
	if !errs.Is(err, errs.MetaNotFound) {
		return nil, err
	}
	if path == "/" {
		return nil, errs.MetaNotFound
	}
	return getNearestMeta(stdpath.Dir(path))
}

func GetMetaByPath(path string) (*model.Meta, error) {
	return getMetaByPath(utils.FixAndCleanPath(path))
}

func getMetaByPath(path string) (*model.Meta, error) {
	meta, ok := metaCache.Get(path)
	if ok {
		if meta == nil {
			return meta, errs.MetaNotFound
		}
		return meta, nil
	}
	meta, err, _ := metaG.Do(path, func() (*model.Meta, error) {
		_meta, err := db.GetMetaByPath(path)
		if err != nil {
			if errs.Is(err, gorm.ErrRecordNotFound) {
				metaCache.Set(path, nil)
				return nil, errs.MetaNotFound
			}
			return nil, err
		}
		metaCache.Set(path, _meta, cache.WithEx[*model.Meta](time.Hour))
		return _meta, nil
	})
	return meta, err
}

func DeleteMetaById(id uint) error {
	old, err := db.GetMetaByID(id)
	if err != nil {
		return err
	}
	metaCache.Del(old.Path)
	return db.DeleteMetaByID(id)
}

func UpdateMeta(u *model.Meta) error {
	u.Path = utils.FixAndCleanPath(u.Path)
	old, err := db.GetMetaByID(u.ID)
	if err != nil {
		return err
	}
	metaCache.Del(old.Path)
	return db.UpdateMeta(u)
}

func CreateMeta(u *model.Meta) error {
	u.Path = utils.FixAndCleanPath(u.Path)
	metaCache.Del(u.Path)
	return db.CreateMeta(u)
}

func GetMetaById(id uint) (*model.Meta, error) {
	return db.GetMetaByID(id)
}

func GetMetas(pageIndex, pageSize int) (metas []model.Meta, count int64, err error) {
	return db.GetMetas(pageIndex, pageSize)
}