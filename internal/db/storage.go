package db

import (
	"fmt"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/internal/model"
)

// why don't need `cache` for storage?
// because all storage store in `op.storagesMap`
// the most of the read operation is from `op.storagesMap`
// just for persistence in database

// CreateStorage just insert storage to database
func CreateStorage(storage *model.Storage) error {
	return errs.WithStack(db.Create(storage).Error)
}

// UpdateStorage just update storage in database
func UpdateStorage(storage *model.Storage) error {
	return errs.WithStack(db.Save(storage).Error)
}

// DeleteStorageByID just delete storage from database by id
func DeleteStorageByID(id uint) error {
	return errs.WithStack(db.Delete(&model.Storage{}, id).Error)
}

// GetStorages Get all storages from database order by index
func GetStorages(pageIndex, pageSize int) ([]model.Storage, int64, error) {
	storageDB := db.Model(&model.Storage{})
	var count int64
	if err := storageDB.Count(&count).Error; err != nil {
		return nil, 0, errs.Wrap(err, "failed get storages count")
	}
	var storages []model.Storage
	if err := addStorageOrder(storageDB).
		Order(columnName("order")).
		Offset((pageIndex - 1) * pageSize).
		Limit(pageSize).
		Find(&storages).
		Error; err != nil {
		return nil, 0, errs.WithStack(err)
	}
	return storages, count, nil
}

// GetStorageByID Get Storage by id, used to update storage usually
func GetStorageByID(id uint) (*model.Storage, error) {
	var storage model.Storage
	storage.ID = id
	if err := db.First(&storage).Error; err != nil {
		return nil, errs.WithStack(err)
	}
	return &storage, nil
}

// GetStorageByMountPath Get Storage by mountPath, used to update storage usually
func GetStorageByMountPath(mountPath string) (*model.Storage, error) {
	var storage model.Storage
	if err := db.Where("mount_path = ?", mountPath).First(&storage).Error; err != nil {
		return nil, errs.WithStack(err)
	}
	return &storage, nil
}

func GetEnabledStorages() ([]model.Storage, error) {
	var storages []model.Storage
	err := addStorageOrder(db).
		Where(fmt.Sprintf("%s = ?", columnName("disabled")), false).
		Find(&storages).Error
	if err != nil {
		return nil, errs.WithStack(err)
	}
	return storages, nil
}