package op

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/generic"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// storagesMap maintains a mapping of mount paths to their corresponding storage drivers
// Although labeled as a driver, it actually represents a storage wrapped by a driver
var storagesMap generic.MapOf[string, driver.Driver]

// GetAllStorages returns all active storage drivers
func GetAllStorages() []driver.Driver {
	return storagesMap.Values()
}

// HasStorage checks if a storage exists at the given mount path
func HasStorage(mountPath string) bool {
	return storagesMap.Has(utils.FixAndCleanPath(mountPath))
}

// GetStorageByMountPath retrieves a storage driver by its mount path
func GetStorageByMountPath(mountPath string) (driver.Driver, error) {
	mountPath = utils.FixAndCleanPath(mountPath)
	storageDriver, ok := storagesMap.Load(mountPath)
	if !ok {
		return nil, errors.Errorf("no storage found at mount path: %s", mountPath)
	}
	return storageDriver, nil
}

// CreateStorage saves a storage configuration to the database and instantiates the driver
// Returns the storage ID and any error that occurred
func CreateStorage(ctx context.Context, storage model.Storage) (uint, error) {
	storage.Modified = time.Now()
	storage.MountPath = utils.FixAndCleanPath(storage.MountPath)

	// Check if driver exists
	driverName := storage.Driver
	driverConstructor, err := GetDriver(driverName)
	if err != nil {
		return 0, errors.WithMessage(err, "failed to get driver constructor")
	}

	storageDriver := driverConstructor()

	// Insert storage to database
	if err = db.CreateStorage(&storage); err != nil {
		return storage.ID, errors.WithMessage(err, "failed to create storage in database")
	}

	// Initialize the storage with its driver
	if err = initStorage(ctx, storage, storageDriver); err != nil {
		log.Warnf("storage created in database but initialization failed: %v", err)
	}

	// Call hooks asynchronously to avoid blocking
	go CallStorageHooks("add", storageDriver)

	log.Debugf("storage created: %v", storageDriver)
	return storage.ID, err
}

// LoadStorage loads an existing storage from the database into memory
func LoadStorage(ctx context.Context, storage model.Storage) error {
	storage.MountPath = utils.FixAndCleanPath(storage.MountPath)

	// Check if driver exists
	driverName := storage.Driver
	driverConstructor, err := GetDriver(driverName)
	if err != nil {
		return errors.WithMessage(err, "failed to get driver constructor")
	}

	storageDriver := driverConstructor()

	// Initialize the storage
	if err = initStorage(ctx, storage, storageDriver); err != nil {
		return err
	}

	// Call hooks asynchronously
	go CallStorageHooks("add", storageDriver)

	log.Debugf("storage loaded: %v", storageDriver)
	return nil
}

// getCurrentGoroutineStack captures the current goroutine's stack trace
// Used for debugging when a panic occurs
func getCurrentGoroutineStack() string {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// initStorage initializes a storage driver with its configuration
// This function is resilient to panics and will save the error state if one occurs
func initStorage(ctx context.Context, storage model.Storage, storageDriver driver.Driver) (err error) {
	storageDriver.SetStorage(storage)
	driverStorage := storageDriver.GetStorage()

	// Capture panics during initialization
	defer func() {
		if r := recover(); r != nil {
			errInfo := fmt.Sprintf("[panic] err: %v\nstack: %s\n", r, getCurrentGoroutineStack())
			log.Errorf("panic during storage initialization: %s", errInfo)
			driverStorage.SetStatus(errInfo)
			MustSaveDriverStorage(storageDriver)
			storagesMap.Store(driverStorage.MountPath, storageDriver)
		}
	}()

	// Unmarshal additional configuration
	err = utils.JSONTool.UnmarshalFromString(driverStorage.Addition, storageDriver.GetAddition())
	if err == nil {
		// Handle reference storages
		if ref, ok := storageDriver.(driver.Reference); ok {
			if strings.HasPrefix(driverStorage.Remark, "ref:/") {
				refMountPath := driverStorage.Remark
				i := strings.Index(refMountPath, "\n")
				if i > 0 {
					refMountPath = refMountPath[4:i]
				} else {
					refMountPath = refMountPath[4:]
				}

				refStorage, refErr := GetStorageByMountPath(refMountPath)
				if refErr != nil {
					err = errors.Errorf("reference error: %w", refErr)
				} else {
					if initErr := ref.InitReference(refStorage); initErr != nil {
						if errs.IsNotSupportError(initErr) {
							err = errors.Errorf("reference error: storage is not compatible with %s", storageDriver.Config().Name)
						} else {
							err = initErr
						}
					}
				}
			}
		}
	}

	// Initialize the driver if no errors occurred
	if err == nil {
		err = storageDriver.Init(ctx)
	}

	// Store the driver in memory regardless of initialization status
	storagesMap.Store(driverStorage.MountPath, storageDriver)

	// Update status based on initialization result
	if err != nil {
		driverStorage.SetStatus(err.Error())
		err = errors.Wrap(err, "failed to initialize storage")
	} else {
		driverStorage.SetStatus(WORK)
	}

	// Save updated status to database
	MustSaveDriverStorage(storageDriver)
	return err
}

// EnableStorage enables a previously disabled storage
func EnableStorage(ctx context.Context, id uint) error {
	storage, err := db.GetStorageByID(id)
	if err != nil {
		return errors.WithMessage(err, "failed to get storage")
	}

	if !storage.Disabled {
		return errors.New("storage is already enabled")
	}

	storage.Disabled = false
	if err = db.UpdateStorage(storage); err != nil {
		return errors.WithMessage(err, "failed to update storage in database")
	}

	if err = LoadStorage(ctx, *storage); err != nil {
		return errors.WithMessage(err, "failed to load storage")
	}

	return nil
}

// DisableStorage disables an active storage
func DisableStorage(ctx context.Context, id uint) error {
	storage, err := db.GetStorageByID(id)
	if err != nil {
		return errors.WithMessage(err, "failed to get storage")
	}

	if storage.Disabled {
		return errors.New("storage is already disabled")
	}

	storageDriver, err := GetStorageByMountPath(storage.MountPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get storage driver")
	}

	// Drop the storage from the driver
	if err = storageDriver.Drop(ctx); err != nil {
		return errors.Wrap(err, "failed to drop storage")
	}

	// Update status and save to database
	storage.Disabled = true
	storage.SetStatus(DISABLED)
	if err = db.UpdateStorage(storage); err != nil {
		return errors.WithMessage(err, "failed to update storage in database")
	}

	// Remove from memory
	storagesMap.Delete(storage.MountPath)

	// Call hooks asynchronously
	go CallStorageHooks("del", storageDriver)

	return nil
}

// UpdateStorage updates an existing storage configuration
// This includes re-initializing the driver with the new configuration
func UpdateStorage(ctx context.Context, storage model.Storage) error {
	oldStorage, err := db.GetStorageByID(storage.ID)
	if err != nil {
		return errors.WithMessage(err, "failed to get old storage")
	}

	// Driver cannot be changed for existing storage
	if oldStorage.Driver != storage.Driver {
		return errors.New("driver type cannot be changed for existing storage")
	}

	storage.Modified = time.Now()
	storage.MountPath = utils.FixAndCleanPath(storage.MountPath)

	// Update in database
	if err = db.UpdateStorage(&storage); err != nil {
		return errors.WithMessage(err, "failed to update storage in database")
	}

	// If disabled, no need to update in memory
	if storage.Disabled {
		return nil
	}

	// Get the current driver
	storageDriver, err := GetStorageByMountPath(oldStorage.MountPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get storage driver")
	}

	// If mount path changed, remove the old path
	if oldStorage.MountPath != storage.MountPath {
		storagesMap.Delete(oldStorage.MountPath)
	}

	// Drop the current instance
	if err = storageDriver.Drop(ctx); err != nil {
		return errors.Wrap(err, "failed to drop storage")
	}

	// Re-initialize with new configuration
	if err = initStorage(ctx, storage, storageDriver); err != nil {
		return err
	}

	// Call hooks asynchronously
	go CallStorageHooks("update", storageDriver)

	log.Debugf("storage updated: %v", storageDriver)
	return nil
}

// DeleteStorageByID completely removes a storage
func DeleteStorageByID(ctx context.Context, id uint) error {
	storage, err := db.GetStorageByID(id)
	if err != nil {
		return errors.WithMessage(err, "failed to get storage")
	}

	// If the storage is active, drop it first
	if !storage.Disabled {
		var storageDriver driver.Driver
		storageDriver, err = GetStorageByMountPath(storage.MountPath)
		if err != nil {
			return errors.WithMessage(err, "failed to get storage driver")
		}

		// Drop the storage from the driver
		if err = storageDriver.Drop(ctx); err != nil {
			return errors.Wrap(err, "failed to drop storage")
		}

		// Remove from memory
		storagesMap.Delete(storage.MountPath)

		// Call hooks asynchronously
		go CallStorageHooks("del", storageDriver)
	}

	// Delete from database
	if err = db.DeleteStorageByID(id); err != nil {
		return errors.WithMessage(err, "failed to delete storage from database")
	}

	return nil
}

// MustSaveDriverStorage saves driver storage configuration
// and logs any errors without returning them
func MustSaveDriverStorage(driver driver.Driver) {
	if err := saveDriverStorage(driver); err != nil {
		log.Errorf("failed to save driver storage: %s", err)
	}
}

// saveDriverStorage saves a driver's storage configuration to the database
func saveDriverStorage(driver driver.Driver) error {
	storage := driver.GetStorage()
	addition := driver.GetAddition()

	// Serialize addition data
	additionJSON, err := utils.JSONTool.MarshalToString(addition)
	if err != nil {
		return errors.Wrap(err, "failed to marshal addition data")
	}

	storage.Addition = additionJSON

	// Update in database
	if err = db.UpdateStorage(storage); err != nil {
		return errors.WithMessage(err, "failed to update storage in database")
	}

	return nil
}

// getStoragesByPath gets storages by longest matching path, including balance storages
// For example, given paths /a/b, /a/c, /a/d/e, and /a/d/e.balance:
// getStoragesByPath(/a/d/e/f) => [/a/d/e, /a/d/e.balance]
func getStoragesByPath(path string) []driver.Driver {
	storages := make([]driver.Driver, 0)
	curSlashCount := 0

	storagesMap.Range(func(mountPath string, value driver.Driver) bool {
		actualMountPath := utils.GetActualMountPath(mountPath)

		// Check if this path is a parent of the requested path
		if utils.IsSubPath(actualMountPath, path) {
			slashCount := strings.Count(utils.PathAddSeparatorSuffix(actualMountPath), "/")

			// If this is a longer match, replace the current matches
			if slashCount > curSlashCount {
				storages = storages[:0]
				curSlashCount = slashCount
			}

			// If this is at the same level as current matches, add it
			if slashCount == curSlashCount {
				storages = append(storages, value)
			}
		}
		return true
	})

	// Sort storages for consistent results
	sort.Slice(storages, func(i, j int) bool {
		return storages[i].GetStorage().MountPath < storages[j].GetStorage().MountPath
	})

	return storages
}

// GetStorageVirtualFilesByPath returns virtual files/folders generated by storages under a path
// For example, given storages at: /a/b, /a/c, /a/d/e, /a/b.balance1, /av
// GetStorageVirtualFilesByPath(/a) => virtual folders b, c, d
func GetStorageVirtualFilesByPath(prefix string) []model.Obj {
	files := make([]model.Obj, 0)
	storages := storagesMap.Values()

	// Sort storages by order then by mount path
	sort.Slice(storages, func(i, j int) bool {
		if storages[i].GetStorage().Order == storages[j].GetStorage().Order {
			return storages[i].GetStorage().MountPath < storages[j].GetStorage().MountPath
		}
		return storages[i].GetStorage().Order < storages[j].GetStorage().Order
	})

	prefix = utils.FixAndCleanPath(prefix)
	uniqueNames := mapset.NewSet[string]()

	// Find all unique first-level folders under the prefix
	for _, storage := range storages {
		mountPath := utils.GetActualMountPath(storage.GetStorage().MountPath)

		// Skip paths that aren't under the prefix
		if len(prefix) >= len(mountPath) || !utils.IsSubPath(prefix, mountPath) {
			continue
		}

		// Extract the first folder name under the prefix
		relativePath := mountPath[len(prefix):]
		if relativePath[0] == '/' {
			relativePath = relativePath[1:]
		}

		firstDir := strings.Split(relativePath, "/")[0]

		// Add unique folder names
		if uniqueNames.Add(firstDir) {
			files = append(files, &model.Object{
				Name:     firstDir,
				Size:     0,
				Modified: storage.GetStorage().Modified,
				IsFolder: true,
			})
		}
	}

	return files
}

// balanceMap tracks the round-robin state for load balancing multiple storages
var balanceMap generic.MapOf[string, int]

// GetBalancedStorage returns a storage for a path, using round-robin if multiple exist
func GetBalancedStorage(path string) driver.Driver {
	path = utils.FixAndCleanPath(path)
	storages := getStoragesByPath(path)

	switch len(storages) {
	case 0:
		return nil
	case 1:
		return storages[0]
	default:
		// Get the virtual path (common prefix) for these storages
		virtualPath := utils.GetActualMountPath(storages[0].GetStorage().MountPath)

		// Load or initialize the round-robin counter
		counter, _ := balanceMap.LoadOrStore(virtualPath, 0)

		// Increment the counter with wraparound
		nextCounter := (counter + 1) % len(storages)
		balanceMap.Store(virtualPath, nextCounter)

		return storages[counter]
	}
}
