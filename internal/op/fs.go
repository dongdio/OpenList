package op

import (
	"context"
	stdpath "path"
	"slices"
	"time"

	"github.com/OpenListTeam/go-cache"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/generic"
	"github.com/dongdio/OpenList/v4/utility/singleflight"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Cache for file listings to reduce repeated processing
var listCache = cache.NewMemCache(cache.WithShards[[]model.Obj](64))

// Group to prevent duplicate concurrent listings
var listG singleflight.Group[[]model.Obj]

// updateCacheObj updates an object in the cache when it's renamed
// It finds and replaces the object with the same name as oldObj with newObj
func updateCacheObj(storage driver.Driver, path string, oldObj model.Obj, newObj model.Obj) {
	key := Key(storage, path)
	objs, ok := listCache.Get(key)
	if ok {
		// First remove any object with the same name as the new object
		for i, obj := range objs {
			if obj.GetName() == newObj.GetName() {
				objs = slices.Delete(objs, i, i+1)
				break
			}
		}

		// Then replace the old object with the new one
		for i, obj := range objs {
			if obj.GetName() == oldObj.GetName() {
				objs[i] = newObj
				break
			}
		}

		// Update the cache
		expiration := time.Minute * time.Duration(storage.GetStorage().CacheExpiration)
		listCache.Set(key, objs, cache.WithEx[[]model.Obj](expiration))
	}
}

// delCacheObj removes an object from the cache when it's deleted
func delCacheObj(storage driver.Driver, path string, obj model.Obj) {
	key := Key(storage, path)
	objs, ok := listCache.Get(key)
	if ok {
		// Find and remove the object with the matching name
		for i, cachedObj := range objs {
			if cachedObj.GetName() == obj.GetName() {
				objs = append(objs[:i], objs[i+1:]...)
				break
			}
		}

		// Update the cache
		expiration := time.Minute * time.Duration(storage.GetStorage().CacheExpiration)
		listCache.Set(key, objs, cache.WithEx[[]model.Obj](expiration))
	}
}

// Map to debounce sort operations for the same directory
var addSortDebounceMap generic.MapOf[string, func(func())]

// addCacheObj adds a new object to the cache
// If an object with the same name already exists, it will be replaced
func addCacheObj(storage driver.Driver, path string, newObj model.Obj) {
	key := Key(storage, path)
	objs, ok := listCache.Get(key)
	if !ok {
		// Cache not initialized for this path, nothing to do
		return
	}

	// Check if object with same name already exists
	for i, obj := range objs {
		if obj.GetName() == newObj.GetName() {
			objs[i] = newObj
			return
		}
	}

	// Add new object based on whether it's a directory
	if len(objs) > 0 && objs[len(objs)-1].IsDir() == newObj.IsDir() {
		// Add to the end of the list if same type (file or folder)
		objs = append(objs, newObj)
	} else {
		// Add to the beginning if different type
		objs = append([]model.Obj{newObj}, objs...)
	}

	// Debounce sorting operations to avoid excessive CPU usage when adding multiple files
	if storage.Config().LocalSort {
		debounce, _ := addSortDebounceMap.LoadOrStore(key, utils.NewDebounce(time.Minute))
		log.Debug("addCacheObj: waiting to sort directory contents")
		debounce(func() {
			log.Debug("addCacheObj: sorting directory contents")
			model.SortFiles(objs, storage.GetStorage().OrderBy, storage.GetStorage().OrderDirection)
			addSortDebounceMap.Delete(key)
		})
	}

	// Update the cache
	expiration := time.Minute * time.Duration(storage.GetStorage().CacheExpiration)
	listCache.Set(key, objs, cache.WithEx[[]model.Obj](expiration))
}

// ClearCache recursively clears the cache for a path and all its subdirectories
func ClearCache(storage driver.Driver, path string) {
	objs, ok := listCache.Get(Key(storage, path))
	if ok {
		// Recursively clear cache for all subdirectories
		for _, obj := range objs {
			if obj.IsDir() {
				ClearCache(storage, stdpath.Join(path, obj.GetName()))
			}
		}
	}

	// Delete the cache entry for this path
	listCache.Del(Key(storage, path))
}

// Key generates a unique cache key for a storage and path combination
// The key incorporates the storage mount path to ensure uniqueness across storages
func Key(storage driver.Driver, path string) string {
	return stdpath.Join(storage.GetStorage().MountPath, utils.FixAndCleanPath(path))
}

// List returns a list of files and directories at the specified path
// Uses cache when available unless refresh is requested
func List(ctx context.Context, storage driver.Driver, path string, args model.ListArgs) ([]model.Obj, error) {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return nil, errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	path = utils.FixAndCleanPath(path)
	log.Debugf("op.List %s", path)

	key := Key(storage, path)

	// Return cached results if available and refresh not requested
	if !args.Refresh {
		if files, ok := listCache.Get(key); ok {
			log.Debugf("using cached listing for %s", path)
			return files, nil
		}
	}

	// Get the directory object
	dir, err := GetUnwrap(ctx, storage, path)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get directory")
	}

	log.Debugf("listing directory: %+v", dir)

	if !dir.IsDir() {
		return nil, errors.WithStack(errs.NotFolder)
	}

	// Use singleflight to prevent duplicate listings
	objs, err, _ := listG.Do(key, func() ([]model.Obj, error) {
		files, err := storage.List(ctx, dir, args)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to list directory contents")
		}

		// Set path for file objects
		for _, f := range files {
			if setter, ok := f.(model.SetPath); ok && f.GetPath() == "" && dir.GetPath() != "" {
				setter.SetPath(stdpath.Join(dir.GetPath(), f.GetName()))
			}
		}

		// Process file names
		model.WrapObjsName(files)

		// Trigger hooks asynchronously
		go func(reqPath string, files []model.Obj) {
			HandleObjsUpdateHook(reqPath, files)
		}(utils.GetFullPath(storage.GetStorage().MountPath, path), files)

		// Sort files if enabled
		if storage.Config().LocalSort {
			model.SortFiles(files, storage.GetStorage().OrderBy, storage.GetStorage().OrderDirection)
		}

		// Process folders based on configuration
		model.ExtractFolder(files, storage.GetStorage().ExtractFolder)

		// Cache results if enabled
		if !storage.Config().NoCache {
			if len(files) > 0 {
				log.Debugf("caching directory listing for %s", key)
				expiration := time.Minute * time.Duration(storage.GetStorage().CacheExpiration)
				listCache.Set(key, files, cache.WithEx[[]model.Obj](expiration))
			} else {
				log.Debugf("deleting empty cache for %s", key)
				listCache.Del(key)
			}
		}

		return files, nil
	})

	return objs, err
}

// Get retrieves an object (file or directory) from storage by path
// First tries to use storage's direct Get method if available, otherwise uses List
func Get(ctx context.Context, storage driver.Driver, path string) (model.Obj, error) {
	path = utils.FixAndCleanPath(path)
	log.Debugf("op.Get %s", path)

	// Try to get the object directly if the driver supports it
	// This can avoid listing all files in a directory just to find one
	if getter, ok := storage.(driver.Getter); ok {
		obj, err := getter.Get(ctx, path)
		if err == nil {
			return model.WrapObjName(obj), nil
		}
	}

	// Handle root folder specially
	if utils.PathEqual(path, "/") {
		return getRootObject(ctx, storage)
	}

	// For non-root paths, get the parent directory listing and find the object
	dir, name := stdpath.Split(path)
	files, err := List(ctx, storage, dir, model.ListArgs{})
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get parent directory listing")
	}

	// Find the object in the listing
	for _, f := range files {
		if f.GetName() == name {
			return f, nil
		}
	}

	log.Debugf("object not found with name: %s", name)
	return nil, errors.WithStack(errs.ObjectNotFound)
}

// getRootObject retrieves the root folder object for a storage
func getRootObject(ctx context.Context, storage driver.Driver) (model.Obj, error) {
	// Try to use GetRooter interface if implemented
	if getRooter, ok := storage.(driver.GetRooter); ok {
		obj, err := getRooter.GetRoot(ctx)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to get root object")
		}
		return &model.ObjWrapName{
			Name: RootName,
			Obj:  obj,
		}, nil
	}

	// Fall back to using driver additions for root info
	var rootObj model.Obj

	switch r := storage.GetAddition().(type) {
	case driver.IRootId:
		rootObj = &model.Object{
			ID:       r.GetRootId(),
			Name:     RootName,
			Size:     0,
			Modified: storage.GetStorage().Modified,
			IsFolder: true,
		}
	case driver.IRootPath:
		rootObj = &model.Object{
			Path:     r.GetRootPath(),
			Name:     RootName,
			Size:     0,
			Modified: storage.GetStorage().Modified,
			IsFolder: true,
		}
	default:
		return nil, errors.New("storage driver must implement IRootPath, IRootId, or GetRooter method")
	}

	if rootObj == nil {
		return nil, errors.New("storage driver must implement IRootPath, IRootId, or GetRooter method")
	}

	return &model.ObjWrapName{
		Name: RootName,
		Obj:  rootObj,
	}, nil
}

// GetUnwrap retrieves an object and unwraps it to get the underlying object
// This removes any name wrappers that might have been added
func GetUnwrap(ctx context.Context, storage driver.Driver, path string) (model.Obj, error) {
	obj, err := Get(ctx, storage, path)
	if err != nil {
		return nil, err
	}
	return model.UnwrapObj(obj), err
}

// Cache for file links to reduce repeated processing
var linkCache = cache.NewMemCache(cache.WithShards[*model.Link](16))

// Group to prevent duplicate concurrent link fetches
var linkG = singleflight.Group[*model.Link]{Remember: true}

// Link gets a download link for a file
// Uses cache when available to avoid repeated requests
func Link(ctx context.Context, storage driver.Driver, path string, args model.LinkArgs) (*model.Link, model.Obj, error) {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return nil, nil, errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	// Get the file object
	file, err := GetUnwrap(ctx, storage, path)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "failed to get file")
	}

	// Check that it's a file, not a directory
	if file.IsDir() {
		return nil, nil, errors.WithStack(errs.NotFile)
	}

	// Create cache key
	key := Key(storage, path)

	// Check cache
	if link, ok := linkCache.Get(key); ok {
		return link, file, nil
	}

	var forget utils.CloseFunc
	// Function to fetch link
	fetchLinkFn := func() (*model.Link, error) {
		link, err := storage.Link(ctx, file, args)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get link")
		}

		// Cache result if expiration is set
		if link.Expiration != nil {
			cacheKey := key
			linkCache.Set(cacheKey, link, cache.WithEx[*model.Link](*link.Expiration))
		}

		return link, nil
	}

	// Skip singleflight for local-only operations
	if storage.Config().OnlyLinkMFile {
		link, err := fetchLinkFn()
		return link, file, err
	}

	forget = func() error {
		if forget != nil {
			forget = nil
			linkG.Forget(key)
		}
		return nil
	}

	// Use singleflight to prevent duplicate fetches
	link, err, _ := linkG.Do(key, fetchLinkFn)
	if err == nil && !link.AcquireReference() {
		link, err, _ = linkG.Do(key, fetchLinkFn)
		if err == nil {
			link.AcquireReference()
		}
	}
	return link, file, err
}

// Other executes a driver-specific operation on an object
func Other(ctx context.Context, storage driver.Driver, args model.FsOtherArgs) (any, error) {
	// Get the object
	obj, err := GetUnwrap(ctx, storage, args.Path)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to get object")
	}

	// Check if driver supports Other interface
	if otherHandler, ok := storage.(driver.Other); ok {
		return otherHandler.Other(ctx, model.OtherArgs{
			Obj:    obj,
			Method: args.Method,
			Data:   args.Data,
		})
	} else {
		return nil, errs.NotImplement
	}
}

// Group to prevent duplicate concurrent directory creations
var mkdirG singleflight.Group[any]

// MakeDir creates a directory at the specified path
// If the parent directory doesn't exist, it will be created recursively
func MakeDir(ctx context.Context, storage driver.Driver, path string, lazyCache ...bool) error {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	path = utils.FixAndCleanPath(path)
	key := Key(storage, path)

	// Use singleflight to prevent duplicate directory creations
	_, err, _ := mkdirG.Do(key, func() (any, error) {
		// Check if directory already exists
		existingObj, err := GetUnwrap(ctx, storage, path)
		if err != nil {
			if errs.IsObjectNotFound(err) {
				// Directory doesn't exist, create it
				return createDirectory(ctx, storage, path, lazyCache...)
			}
			return nil, errors.WithMessage(err, "failed to check if directory exists")
		}

		// Directory exists, check if it's actually a directory
		if existingObj.IsDir() {
			return nil, nil // Directory already exists, nothing to do
		}

		// Path exists but is a file, not a directory
		return nil, errors.New("cannot create directory: file exists at the same path")
	})

	return err
}

// createDirectory creates a directory and its parent directories if needed
func createDirectory(ctx context.Context, storage driver.Driver, path string, lazyCache ...bool) (any, error) {
	parentPath, dirName := stdpath.Split(path)

	// Recursively create parent directory if needed
	err := MakeDir(ctx, storage, parentPath)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to create parent directory [%s]", parentPath)
	}

	// Get parent directory object
	parentDir, err := GetUnwrap(ctx, storage, parentPath)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to get parent directory [%s]", parentPath)
	}

	// Create directory using appropriate driver interface
	switch s := storage.(type) {
	case driver.MkdirResult:
		// Driver that returns the created directory object
		newObj, err := s.MakeDir(ctx, parentDir, dirName)
		if err == nil {
			if newObj != nil {
				// Add new directory to cache
				addCacheObj(storage, parentPath, model.WrapObjName(newObj))
			} else if !utils.IsBool(lazyCache...) {
				// Clear cache if no object returned
				ClearCache(storage, parentPath)
			}
		}
		return nil, errors.WithStack(err)

	case driver.Mkdir:
		// Driver that only reports success/failure
		err = s.MakeDir(ctx, parentDir, dirName)
		if err == nil && !utils.IsBool(lazyCache...) {
			// Clear cache to force refresh
			ClearCache(storage, parentPath)
		}
		return nil, errors.WithStack(err)

	default:
		return nil, errs.NotImplement
	}
}

// Move moves a file or directory from one location to another
func Move(ctx context.Context, storage driver.Driver, srcPath, dstDirPath string, lazyCache ...bool) error {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	srcPath = utils.FixAndCleanPath(srcPath)
	dstDirPath = utils.FixAndCleanPath(dstDirPath)

	// Get source object and its wrapped version (for cache handling)
	srcRawObj, err := Get(ctx, storage, srcPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get source object")
	}
	srcObj := model.UnwrapObj(srcRawObj)

	// Get destination directory
	dstDir, err := GetUnwrap(ctx, storage, dstDirPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get destination directory")
	}

	// Get source directory path for cache updates
	srcDirPath := stdpath.Dir(srcPath)

	// Execute the move using the appropriate driver interface
	switch s := storage.(type) {
	case driver.MoveResult:
		// Driver that returns the moved object
		newObj, err := s.Move(ctx, srcObj, dstDir)
		if err == nil {
			// Update caches
			delCacheObj(storage, srcDirPath, srcRawObj)
			if newObj != nil {
				// Add the moved object to destination cache
				addCacheObj(storage, dstDirPath, model.WrapObjName(newObj))
			} else if !utils.IsBool(lazyCache...) {
				// Clear destination cache if no object returned
				ClearCache(storage, dstDirPath)
			}
		}
		return errors.WithStack(err)

	case driver.Move:
		// Driver that only reports success/failure
		err = s.Move(ctx, srcObj, dstDir)
		if err == nil {
			// Update caches
			delCacheObj(storage, srcDirPath, srcRawObj)
			if !utils.IsBool(lazyCache...) {
				// Clear destination cache to force refresh
				ClearCache(storage, dstDirPath)
			}
		}
		return errors.WithStack(err)

	default:
		return errs.NotImplement
	}
}

// Rename changes the name of a file or directory
func Rename(ctx context.Context, storage driver.Driver, srcPath, dstName string, lazyCache ...bool) error {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	srcPath = utils.FixAndCleanPath(srcPath)

	// Get source object and its wrapped version (for cache handling)
	srcRawObj, err := Get(ctx, storage, srcPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get source object")
	}
	srcObj := model.UnwrapObj(srcRawObj)

	// Get source directory path for cache updates
	srcDirPath := stdpath.Dir(srcPath)

	// Execute the rename using the appropriate driver interface
	switch s := storage.(type) {
	case driver.RenameResult:
		// Driver that returns the renamed object
		newObj, err := s.Rename(ctx, srcObj, dstName)
		if err == nil {
			// Update cache
			if newObj != nil {
				updateCacheObj(storage, srcDirPath, srcRawObj, model.WrapObjName(newObj))
			} else if !utils.IsBool(lazyCache...) {
				// Clear cache if no object returned
				ClearCache(storage, srcDirPath)
			}
		}
		return errors.WithStack(err)

	case driver.Rename:
		// Driver that only reports success/failure
		err = s.Rename(ctx, srcObj, dstName)
		if err == nil && !utils.IsBool(lazyCache...) {
			// Clear cache to force refresh
			ClearCache(storage, srcDirPath)
		}
		return errors.WithStack(err)

	default:
		return errs.NotImplement
	}
}

// Copy copies a file or directory to another location
func Copy(ctx context.Context, storage driver.Driver, srcPath, dstDirPath string, lazyCache ...bool) error {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	srcPath = utils.FixAndCleanPath(srcPath)
	dstDirPath = utils.FixAndCleanPath(dstDirPath)

	// Get source object
	srcObj, err := GetUnwrap(ctx, storage, srcPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get source object")
	}

	// Get destination directory
	dstDir, err := GetUnwrap(ctx, storage, dstDirPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get destination directory")
	}

	// Execute the copy using the appropriate driver interface
	switch s := storage.(type) {
	case driver.CopyResult:
		// Driver that returns the copied object
		newObj, err := s.Copy(ctx, srcObj, dstDir)
		if err == nil {
			// Update cache
			if newObj != nil {
				addCacheObj(storage, dstDirPath, model.WrapObjName(newObj))
			} else if !utils.IsBool(lazyCache...) {
				// Clear cache if no object returned
				ClearCache(storage, dstDirPath)
			}
		}
		return errors.WithStack(err)

	case driver.Copy:
		// Driver that only reports success/failure
		err = s.Copy(ctx, srcObj, dstDir)
		if err == nil && !utils.IsBool(lazyCache...) {
			// Clear cache to force refresh
			ClearCache(storage, dstDirPath)
		}
		return errors.WithStack(err)

	default:
		return errs.NotImplement
	}
}

// Remove deletes a file or directory
func Remove(ctx context.Context, storage driver.Driver, path string) error {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	// Prevent accidental root folder deletion
	if utils.PathEqual(path, "/") {
		return errors.New("deleting root folder is not allowed, please go to the manage page to delete the storage instead")
	}

	path = utils.FixAndCleanPath(path)

	// Get object and its wrapped version (for cache handling)
	rawObj, err := Get(ctx, storage, path)
	if err != nil {
		// If object not found, consider deletion successful
		if errs.IsObjectNotFound(err) {
			log.Debugf("object %s is already removed", path)
			return nil
		}
		return errors.WithMessage(err, "failed to get object")
	}

	// Get parent directory path for cache updates
	dirPath := stdpath.Dir(path)

	// Execute the removal using the driver
	if remover, ok := storage.(driver.Remove); ok {
		err = remover.Remove(ctx, model.UnwrapObj(rawObj))
		if err == nil {
			// Update caches
			delCacheObj(storage, dirPath, rawObj)

			// If deleted object was a directory, recursively clear its cache
			if rawObj.IsDir() {
				ClearCache(storage, path)
			}
		}
		return errors.WithStack(err)
	}

	return errs.NotImplement
}

// Put uploads a file to the specified directory
func Put(ctx context.Context, storage driver.Driver, dstDirPath string, file model.FileStreamer, up driver.UpdateProgress, lazyCache ...bool) error {
	closeFunc := file.Close
	// Ensure file is closed after upload
	defer func() {
		if err := closeFunc(); err != nil {
			log.Errorf("failed to close file streamer: %v", err)
		}
	}()

	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not init: %s", storage.GetStorage().Status)
	}

	// Special handling for UrlTree driver
	if storage.GetStorage().Driver == "UrlTree" {
		return handleURLTreePut(ctx, storage, dstDirPath, file, up, lazyCache...)
	}

	dstDirPath = utils.FixAndCleanPath(dstDirPath)
	dstPath := stdpath.Join(dstDirPath, file.GetName())

	// Handle existing file at destination
	err := handleExistingFile(ctx, storage, dstPath, file)
	if err != nil {
		return err
	}

	// Ensure destination directory exists
	err = MakeDir(ctx, storage, dstDirPath)
	if err != nil {
		return errors.WithMessagef(err, "failed to create directory [%s]", dstDirPath)
	}

	// Get parent directory object
	parentDir, err := GetUnwrap(ctx, storage, dstDirPath)
	if err != nil {
		return errors.WithMessagef(err, "failed to get directory [%s]", dstDirPath)
	}

	// Set default progress callback if none provided
	if up == nil {
		up = func(p float64) {}
	}

	// Execute the upload using the appropriate driver interface
	switch s := storage.(type) {
	case driver.PutResult:
		// Driver that returns the uploaded object
		newObj, err := s.Put(ctx, parentDir, file, up)
		if err == nil {
			// Update cache
			if newObj != nil {
				addCacheObj(storage, dstDirPath, model.WrapObjName(newObj))
			} else if !utils.IsBool(lazyCache...) {
				// Clear cache if no object returned
				ClearCache(storage, dstDirPath)
			}
		}
	case driver.Put:
		// Driver that only reports success/failure
		err = s.Put(ctx, parentDir, file, up)
		if err == nil && !utils.IsBool(lazyCache...) {
			// Clear cache to force refresh
			ClearCache(storage, dstDirPath)
		}
	default:
		return errs.NotImplement
	}
	return finalizePut(ctx, storage, dstDirPath, file, err)
}

// handleURLTreePut handles uploading to the UrlTree driver
func handleURLTreePut(ctx context.Context, storage driver.Driver, dstDirPath string, file model.FileStreamer, up driver.UpdateProgress, lazyCache ...bool) error {
	var link string
	dstDirPath, link = URLTreeSplitPathAndURL(stdpath.Join(dstDirPath, file.GetName()))
	file = &stream.FileStream{Obj: &model.Object{Name: link}}

	// Continue with normal put process
	return Put(ctx, storage, dstDirPath, file, up, lazyCache...)
}

// handleExistingFile checks if a file already exists at the destination path and handles it accordingly
func handleExistingFile(ctx context.Context, storage driver.Driver, dstPath string, file model.FileStreamer) error {
	// Check if file already exists
	fi, err := GetUnwrap(ctx, storage, dstPath)
	if err == nil {
		// File exists, handle based on configuration and size
		switch {
		case fi.GetSize() == 0:
			// Remove zero-size files
			err = Remove(ctx, storage, dstPath)
			if err != nil {
				return errors.WithMessagef(err, "failed to remove existing empty file")
			}
		case storage.Config().NoOverwriteUpload:
			// Rename existing file to temp name if overwrite not allowed
			err = Rename(ctx, storage, dstPath, file.GetName()+".openlist_to_delete")
			if err != nil {
				return err
			}
		default:
			// Set existing file info for potential optimization by driver
			file.SetExist(fi)
		}
	}

	return nil
}

// finalizePut performs final cleanup after a put operation
func finalizePut(ctx context.Context, storage driver.Driver, dstDirPath string, file model.FileStreamer, uploadErr error) error {
	log.Debugf("put file [%s] complete", file.GetName())

	// Handle rename cleanup for NoOverwriteUpload mode
	tempPath := stdpath.Join(dstDirPath, file.GetName()+".openlist_to_delete")

	// Check if we were in NoOverwriteUpload mode and have a temp file
	fi, err := GetUnwrap(ctx, storage, tempPath)
	if err == nil && storage.Config().NoOverwriteUpload && fi.GetSize() > 0 {
		if uploadErr != nil {
			// Upload failed, recover the original file
			err = Rename(ctx, storage, tempPath, file.GetName())
			if err != nil {
				log.Errorf("failed to recover original file: %+v", err)
			}
		} else {
			// Upload succeeded, remove the temp file
			err = Remove(ctx, storage, tempPath)
			if err != nil {
				return err
			}

			// Clear link cache
			key := Key(storage, stdpath.Join(dstDirPath, file.GetName()))
			linkCache.Del(key)
		}
	}

	return errors.WithStack(uploadErr)
}

// PutURL uploads a file from a URL to the specified directory
func PutURL(ctx context.Context, storage driver.Driver, dstDirPath, dstName, url string, lazyCache ...bool) error {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	dstDirPath = utils.FixAndCleanPath(dstDirPath)

	// Check if file already exists
	_, err := GetUnwrap(ctx, storage, stdpath.Join(dstDirPath, dstName))
	if err == nil {
		return errors.New("object already exists")
	}

	// Ensure destination directory exists
	err = MakeDir(ctx, storage, dstDirPath)
	if err != nil {
		return errors.WithMessage(err, "failed to create directory")
	}

	// Get destination directory object
	dstDir, err := GetUnwrap(ctx, storage, dstDirPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get directory")
	}

	// Execute the upload using the appropriate driver interface
	switch s := storage.(type) {
	case driver.PutURLResult:
		// Driver that returns the uploaded object
		newObj, err := s.PutURL(ctx, dstDir, dstName, url)
		if err == nil {
			// Update cache
			if newObj != nil {
				addCacheObj(storage, dstDirPath, model.WrapObjName(newObj))
			} else if !utils.IsBool(lazyCache...) {
				// Clear cache if no object returned
				ClearCache(storage, dstDirPath)
			}
		}
		log.Debugf("put URL [%s](%s) complete", dstName, url)
		return errors.WithStack(err)

	case driver.PutURL:
		// Driver that only reports success/failure
		err = s.PutURL(ctx, dstDir, dstName, url)
		if err == nil && !utils.IsBool(lazyCache...) {
			// Clear cache to force refresh
			ClearCache(storage, dstDirPath)
		}
		log.Debugf("put URL [%s](%s) complete", dstName, url)
		return errors.WithStack(err)

	default:
		return errs.NotImplement
	}
}