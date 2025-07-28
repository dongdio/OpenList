package op

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	stdpath "path"
	"strings"
	"time"

	"github.com/OpenListTeam/go-cache"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	tool2 "github.com/dongdio/OpenList/v4/utility/archive/tool"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/singleflight"
	"github.com/dongdio/OpenList/v4/utility/stream"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Cache for archive metadata to reduce repeated processing
var archiveMetaCache = cache.NewMemCache(cache.WithShards[*model.ArchiveMetaProvider](64))

// Group to prevent duplicate concurrent metadata fetches
var archiveMetaG singleflight.Group[*model.ArchiveMetaProvider]

// GetArchiveMeta retrieves archive metadata for a given file
// Uses cache when available unless refresh is requested
func GetArchiveMeta(ctx context.Context, storage driver.Driver, path string, args model.ArchiveMetaArgs) (*model.ArchiveMetaProvider, error) {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return nil, errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	path = utils.FixAndCleanPath(path)
	key := Key(storage, path)

	// Function to retrieve metadata
	fetchMetaFn := func() (*model.ArchiveMetaProvider, error) {
		_, metaProvider, err := getArchiveMeta(ctx, storage, path, args)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get archive metadata for %s", path)
		}

		// Cache the result if expiration is set
		if metaProvider.Expiration != nil {
			archiveMetaCache.Set(key, metaProvider, cache.WithEx[*model.ArchiveMetaProvider](*metaProvider.Expiration))
		}

		return metaProvider, nil
	}

	// Skip singleflight for local-only operations
	if storage.Config().OnlyLinkMFile {
		return fetchMetaFn()
	}

	if !args.Refresh {
		if meta, ok := archiveMetaCache.Get(key); ok {
			log.Debugf("use cache when get %s archive meta", path)
			return meta, nil
		}
	}

	// Use singleflight to prevent duplicate fetches
	meta, err, _ := archiveMetaG.Do(key, fetchMetaFn)
	return meta, err
}

// GetArchiveToolAndStream retrieves the appropriate archive tool and streams for a file
// Returns the file object, tool, streams, and any error
func GetArchiveToolAndStream(ctx context.Context, storage driver.Driver, path string, args model.LinkArgs) (model.Obj, tool2.Tool, []*stream.SeekableStream, error) {
	// Get file link
	link, obj, err := Link(ctx, storage, path, args)
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, "failed to get link for %s", path)
	}

	// Extract file extension
	baseName, ext, found := strings.Cut(obj.GetName(), ".")
	if !found {
		_ = link.Close()
		return nil, nil, nil, errors.New("failed to get archive tool: file has no extension")
	}

	// Try to get archive tool by extension
	partExt, archiveTool, err := tool2.GetArchiveTool("." + ext)
	if err != nil {
		// Try with full extension as fallback
		var fallbackErr error
		partExt, archiveTool, fallbackErr = tool2.GetArchiveTool(stdpath.Ext(obj.GetName()))
		if fallbackErr != nil {
			_ = link.Close()
			return nil, nil, nil, errors.WithMessagef(errors.WithStack(fallbackErr), "failed to get archive tool for extension: %s", ext)
		}
	}

	fileStream, err := stream.NewSeekableStream(&stream.FileStream{Ctx: ctx, Obj: obj}, link)
	if err != nil {
		_ = link.Close()
		return nil, nil, nil, errors.WithMessagef(err, "failed to create stream for %s", path)
	}

	// Initialize streams array with the primary file
	streams := []*stream.SeekableStream{fileStream}

	// If this is a multi-part archive, load the additional parts
	if partExt != nil {
		streams = appendPartStreams(ctx, storage, path, baseName, partExt, args, streams)
	}

	return obj, archiveTool, streams, nil
}

// appendPartStreams loads additional parts of a multi-part archive
// Returns the complete list of streams including the primary stream
func appendPartStreams(ctx context.Context, storage driver.Driver, path string, baseName string,
	partExt *tool2.MultipartExtension, args model.LinkArgs,
	streams []*stream.SeekableStream) []*stream.SeekableStream {
	dir := stdpath.Dir(path)
	index := partExt.SecondPartIndex

	// Keep loading parts until we encounter an error
	for {
		partPath := stdpath.Join(dir, baseName+fmt.Sprintf(partExt.PartFileFormat, index))
		link, partObj, err := Link(ctx, storage, partPath, args)
		if err != nil {
			break // No more parts found
		}
		partStream, err := stream.NewSeekableStream(&stream.FileStream{Ctx: ctx, Obj: partObj}, link)
		if err != nil {
			_ = link.Close()
			closeAllStreams(streams)
			return nil
		}

		streams = append(streams, partStream)
		index++
	}

	return streams
}

// closeAllStreams closes all streams in a slice
func closeAllStreams(streams []*stream.SeekableStream) {
	for _, s := range streams {
		_ = s.Close()
	}
}

// getArchiveMeta retrieves archive metadata from either the driver or an archive tool
func getArchiveMeta(ctx context.Context, storage driver.Driver, path string, args model.ArchiveMetaArgs) (model.Obj, *model.ArchiveMetaProvider, error) {
	// Try to use driver's built-in archive reader if available
	if storageAr, ok := storage.(driver.ArchiveReader); ok {
		obj, err := GetUnwrap(ctx, storage, path)
		if err != nil {
			return nil, nil, errors.WithMessage(err, "failed to get file")
		}

		if obj.IsDir() {
			return nil, nil, errors.WithStack(errs.NotFile)
		}

		meta, err := storageAr.GetArchiveMeta(ctx, obj, args.ArchiveArgs)
		if !errors.Is(err, errs.NotImplement) {
			// Driver supports archive metadata
			metaProvider := &model.ArchiveMetaProvider{
				ArchiveMeta:     meta,
				DriverProviding: true,
			}

			// Set sort settings if tree is available
			if meta != nil && meta.GetTree() != nil {
				metaProvider.Sort = &storage.GetStorage().Sort
			}

			// Set expiration for caching if enabled
			if !storage.Config().NoCache {
				expiration := time.Minute * time.Duration(storage.GetStorage().CacheExpiration)
				metaProvider.Expiration = &expiration
			}

			return obj, metaProvider, err
		}
	}

	// Fall back to using archive tools
	obj, archiveTool, streams, err := GetArchiveToolAndStream(ctx, storage, path, args.LinkArgs)
	if err != nil {
		return nil, nil, err
	}

	// Ensure streams are closed after use
	defer func() {
		var closeErr error
		for _, s := range streams {
			closeErr = stderrors.Join(closeErr, s.Close())
		}
		if closeErr != nil {
			log.Errorf("failed to close file streams: %v", closeErr)
		}
	}()

	// Get metadata using the archive tool
	meta, err := archiveTool.GetMeta(streams, args.ArchiveArgs)
	if err != nil {
		return nil, nil, err
	}

	// Create metadata provider
	metaProvider := &model.ArchiveMetaProvider{
		ArchiveMeta:     meta,
		DriverProviding: false,
	}

	// Set sort settings if tree is available
	if meta.GetTree() != nil {
		metaProvider.Sort = &storage.GetStorage().Sort
	}

	// Set expiration for caching
	if !storage.Config().NoCache {
		expiration := time.Minute * time.Duration(storage.GetStorage().CacheExpiration)
		metaProvider.Expiration = &expiration
	}

	return obj, metaProvider, err
}

// Cache for archive listing results
var archiveListCache = cache.NewMemCache(cache.WithShards[[]model.Obj](64))

// Group to prevent duplicate concurrent archive listings
var archiveListG singleflight.Group[[]model.Obj]

// ListArchive lists the contents of an archive file
// Uses cache when available unless refresh is requested
func ListArchive(ctx context.Context, storage driver.Driver, path string, args model.ArchiveListArgs) ([]model.Obj, error) {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return nil, errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	path = utils.FixAndCleanPath(path)
	metaKey := Key(storage, path)
	key := stdpath.Join(metaKey, args.InnerPath)

	// Return cached results if available and refresh not requested
	if !args.Refresh {
		if files, ok := archiveListCache.Get(key); ok {
			log.Debugf("using cached archive listing for %s:%s", path, args.InnerPath)
			return files, nil
		}
	}

	// Use singleflight to prevent duplicate listings
	files, err, _ := archiveListG.Do(key, func() ([]model.Obj, error) {
		obj, fileList, err := listArchive(ctx, storage, path, args)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to list archive %s:%s", path, args.InnerPath)
		}

		// Set path for file objects
		for _, f := range fileList {
			if setter, ok := f.(model.SetPath); ok && f.GetPath() == "" && obj.GetPath() != "" {
				setter.SetPath(stdpath.Join(obj.GetPath(), args.InnerPath, f.GetName()))
			}
		}

		// Process file names
		model.WrapObjsName(fileList)

		// Sort files if enabled
		if storage.Config().LocalSort {
			model.SortFiles(fileList, storage.GetStorage().OrderBy, storage.GetStorage().OrderDirection)
		}

		// Process folders based on configuration
		model.ExtractFolder(fileList, storage.GetStorage().ExtractFolder)

		// Cache results if enabled
		if !storage.Config().NoCache {
			if len(fileList) > 0 {
				expiration := time.Minute * time.Duration(storage.GetStorage().CacheExpiration)
				archiveListCache.Set(key, fileList, cache.WithEx[[]model.Obj](expiration))
				log.Debugf("cached archive listing for %s", key)
			} else {
				archiveListCache.Del(key)
				log.Debugf("deleted empty cache for %s", key)
			}
		}

		return fileList, nil
	})

	return files, err
}

// _listArchive is the low-level function that lists archive contents
// It tries to use the driver's built-in support first, then falls back to archive tools
func _listArchive(ctx context.Context, storage driver.Driver, path string, args model.ArchiveListArgs) (model.Obj, []model.Obj, error) {
	// Try to use driver's built-in archive reader if available
	if storageAr, ok := storage.(driver.ArchiveReader); ok {
		obj, err := GetUnwrap(ctx, storage, path)
		if err != nil {
			return nil, nil, errors.WithMessage(err, "failed to get file")
		}

		if obj.IsDir() {
			return nil, nil, errors.WithStack(errs.NotFile)
		}

		files, err := storageAr.ListArchive(ctx, obj, args.ArchiveInnerArgs)
		if !errors.Is(err, errs.NotImplement) {
			return obj, files, err
		}
	}

	// Fall back to using archive tools
	obj, archiveTool, streams, err := GetArchiveToolAndStream(ctx, storage, path, args.LinkArgs)
	if err != nil {
		return nil, nil, err
	}

	// Ensure streams are closed after use
	defer func() {
		var closeErr error
		for _, s := range streams {
			closeErr = stderrors.Join(closeErr, s.Close())
		}
		if closeErr != nil {
			log.Errorf("failed to close file streams: %v", closeErr)
		}
	}()

	// List files using the archive tool
	files, err := archiveTool.List(streams, args.ArchiveInnerArgs)
	return obj, files, err
}

// listArchive is a wrapper around _listArchive that adds support for metadata-based listing
func listArchive(ctx context.Context, storage driver.Driver, path string, args model.ArchiveListArgs) (model.Obj, []model.Obj, error) {
	obj, files, err := _listArchive(ctx, storage, path, args)

	// If direct listing is not supported, try using metadata
	if errors.Is(err, errs.NotSupport) {
		meta, metaErr := GetArchiveMeta(ctx, storage, path, model.ArchiveMetaArgs{
			ArchiveArgs: args.ArchiveArgs,
			Refresh:     args.Refresh,
		})
		if metaErr != nil {
			return nil, nil, metaErr
		}

		files, err = getChildrenFromArchiveMeta(meta, args.InnerPath)
		if err != nil {
			return nil, nil, err
		}
	}

	// If we have files but no object, try to get the object
	if err == nil && obj == nil {
		obj, err = GetUnwrap(ctx, storage, path)
	}

	if err != nil {
		return nil, nil, err
	}

	return obj, files, err
}

// getChildrenFromArchiveMeta extracts child objects at a specific path from archive metadata
func getChildrenFromArchiveMeta(meta model.ArchiveMeta, innerPath string) ([]model.Obj, error) {
	tree := meta.GetTree()
	if tree == nil {
		return nil, errors.WithStack(errs.NotImplement)
	}

	// Navigate to the requested inner path
	pathParts := splitPath(innerPath)
	for _, part := range pathParts {
		var nextNode model.ObjTree

		// Find the child with matching name
		for _, child := range tree {
			if child.GetName() == part {
				nextNode = child
				break
			}
		}

		if nextNode == nil {
			return nil, errors.WithStack(errs.ObjectNotFound)
		}

		if !nextNode.IsDir() || nextNode.GetChildren() == nil {
			return nil, errors.WithStack(errs.NotFolder)
		}

		tree = nextNode.GetChildren()
	}

	// Convert ObjTree to Obj
	return utils.SliceConvert(tree, func(src model.ObjTree) (model.Obj, error) {
		return src, nil
	})
}

// splitPath splits a path into its component parts
func splitPath(path string) []string {
	var parts []string

	for {
		dir, file := stdpath.Split(path)
		if file == "" {
			break
		}
		parts = append([]string{file}, parts...)
		path = strings.TrimSuffix(dir, "/")
	}

	return parts
}

// ArchiveGet gets a specific file or folder from within an archive
func ArchiveGet(ctx context.Context, storage driver.Driver, path string, args model.ArchiveListArgs) (model.Obj, model.Obj, error) {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return nil, nil, errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	path = utils.FixAndCleanPath(path)

	// Get the archive file
	archiveFile, err := GetUnwrap(ctx, storage, path)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "failed to get file")
	}

	if archiveFile.IsDir() {
		return nil, nil, errors.WithStack(errs.NotFile)
	}

	// Try to use driver's built-in archive getter if available
	if getter, ok := storage.(driver.ArchiveGetter); ok {
		obj, err := getter.ArchiveGet(ctx, archiveFile, args.ArchiveInnerArgs)
		if err == nil {
			return archiveFile, model.WrapObjName(obj), nil
		}
	}

	// Handle root path specially
	if utils.PathEqual(args.InnerPath, "/") {
		return archiveFile, &model.ObjWrapName{
			Name: RootName,
			Obj: &model.Object{
				Name:     archiveFile.GetName(),
				Path:     archiveFile.GetPath(),
				ID:       archiveFile.GetID(),
				Size:     archiveFile.GetSize(),
				Modified: archiveFile.ModTime(),
				IsFolder: true,
			},
		}, nil
	}

	// Split the inner path to get parent directory and filename
	innerDir, name := stdpath.Split(args.InnerPath)

	// List the parent directory
	listArgs := args
	listArgs.InnerPath = strings.TrimSuffix(innerDir, "/")
	files, err := ListArchive(ctx, storage, path, listArgs)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "failed to get parent directory listing")
	}

	// Find the requested file
	for _, file := range files {
		if file.GetName() == name {
			return archiveFile, file, nil
		}
	}

	return nil, nil, errors.WithStack(errs.ObjectNotFound)
}

// extractLink holds a link and object together for caching
type extractLink struct {
	*model.Link
	Obj model.Obj
}

// Cache for extracted file links
var extractCache = cache.NewMemCache(cache.WithShards[*extractLink](16))

// Group to prevent duplicate concurrent extractions
var extractG = singleflight.Group[*extractLink]{Remember: true}

// DriverExtract extracts a file from an archive using the driver's capabilities
func DriverExtract(ctx context.Context, storage driver.Driver, path string, args model.ArchiveInnerArgs) (*model.Link, model.Obj, error) {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return nil, nil, errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	// Create cache key
	key := stdpath.Join(Key(storage, path), args.InnerPath)

	// Check cache
	if link, ok := extractCache.Get(key); ok {
		return link.Link, link.Obj, nil
	}

	var forget utils.CloseFunc
	// Function to perform extraction
	extractFn := func() (*extractLink, error) {
		link, err := driverExtract(ctx, storage, path, args)
		if err != nil {
			return nil, errors.Wrap(err, "failed to extract from archive")
		}

		// Cache result if expiration is set
		if link.Link.Expiration != nil {
			extractCache.Set(key, link, cache.WithEx[*extractLink](*link.Link.Expiration))
		}
		link.Add(forget)
		return link, nil
	}

	// Skip singleflight for local-only operations
	if storage.Config().OnlyLinkMFile {
		link, err := extractFn()
		if err != nil {
			return nil, nil, err
		}
		return link.Link, link.Obj, nil
	}
	forget = func() error {
		if forget != nil {
			forget = nil
			linkG.Forget(key)
		}
		return nil
	}

	// Use singleflight to prevent duplicate extractions
	link, err, _ := extractG.Do(key, extractFn)
	if err == nil && !link.AcquireReference() {
		link, err, _ = extractG.Do(key, extractFn)
		if err == nil {
			link.AcquireReference()
		}
	}
	if err != nil {
		return nil, nil, err
	}

	return link.Link, link.Obj, nil
}

// driverExtract handles the actual extraction using the driver
func driverExtract(ctx context.Context, storage driver.Driver, path string, args model.ArchiveInnerArgs) (*extractLink, error) {
	// Check if driver supports archive extraction
	storageAr, ok := storage.(driver.ArchiveReader)
	if !ok {
		return nil, errs.DriverExtractNotSupported
	}

	// Get both the archive file and the inner file
	archiveFile, extractedFile, err := ArchiveGet(ctx, storage, path, model.ArchiveListArgs{
		ArchiveInnerArgs: args,
		Refresh:          false,
	})
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get file from archive")
	}

	if extractedFile.IsDir() {
		return nil, errors.WithStack(errs.NotFile)
	}

	// Extract the file
	link, err := storageAr.Extract(ctx, archiveFile, args)
	return &extractLink{Link: link, Obj: extractedFile}, err
}

// streamWithParent wraps a ReadCloser with its parent streams to ensure proper cleanup
type streamWithParent struct {
	rc      io.ReadCloser
	parents []*stream.SeekableStream
}

// Read implements io.Reader
func (s *streamWithParent) Read(p []byte) (int, error) {
	return s.rc.Read(p)
}

// Close implements io.Closer and ensures all parent streams are closed
func (s *streamWithParent) Close() error {
	err := s.rc.Close()
	for _, ss := range s.parents {
		err = stderrors.Join(err, ss.Close())
	}
	return err
}

// InternalExtract extracts a file from an archive using archive tools
func InternalExtract(ctx context.Context, storage driver.Driver, path string, args model.ArchiveInnerArgs) (io.ReadCloser, int64, error) {
	// Get archive tool and streams
	_, archiveTool, streams, err := GetArchiveToolAndStream(ctx, storage, path, args.LinkArgs)
	if err != nil {
		return nil, 0, err
	}

	// Extract the file
	readCloser, size, err := archiveTool.Extract(streams, args)
	if err != nil {
		// Clean up streams on error
		var closeErr error
		for _, s := range streams {
			closeErr = stderrors.Join(closeErr, s.Close())
		}
		if closeErr != nil {
			log.Errorf("failed to close file streams: %v", closeErr)
			err = stderrors.Join(err, closeErr)
		}
		return nil, 0, err
	}

	// Wrap reader to ensure proper cleanup
	return &streamWithParent{rc: readCloser, parents: streams}, size, nil
}

// ArchiveDecompress decompresses an archive file to a destination directory
func ArchiveDecompress(ctx context.Context, storage driver.Driver, srcPath, dstDirPath string, args model.ArchiveDecompressArgs, lazyCache ...bool) error {
	// Check if storage is initialized
	if storage.Config().CheckStatus && storage.GetStorage().Status != WORK {
		return errors.Errorf("storage not initialized: %s", storage.GetStorage().Status)
	}

	srcPath = utils.FixAndCleanPath(srcPath)
	dstDirPath = utils.FixAndCleanPath(dstDirPath)

	// Get source and destination objects
	srcObj, err := GetUnwrap(ctx, storage, srcPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get source file")
	}

	dstDir, err := GetUnwrap(ctx, storage, dstDirPath)
	if err != nil {
		return errors.WithMessage(err, "failed to get destination directory")
	}

	// Use appropriate decompression method based on driver capabilities
	switch s := storage.(type) {
	case driver.ArchiveDecompressResult:
		// Driver that returns new objects after decompression
		var newObjs []model.Obj
		newObjs, err = s.ArchiveDecompress(ctx, srcObj, dstDir, args)
		if err == nil {
			if len(newObjs) > 0 {
				// Add new objects to cache
				for _, newObj := range newObjs {
					addCacheObj(storage, dstDirPath, model.WrapObjName(newObj))
				}
			} else if !utils.IsBool(lazyCache...) {
				// Clear cache if no objects returned
				DeleteCache(storage, dstDirPath)
			}
		}

	case driver.ArchiveDecompress:
		// Driver that only performs decompression
		err = s.ArchiveDecompress(ctx, srcObj, dstDir, args)
		if err == nil && !utils.IsBool(lazyCache...) {
			DeleteCache(storage, dstDirPath)
		}

	default:
		return errs.NotImplement
	}

	return errors.WithStack(err)
}