package s3

// Credits: https://pkg.go.dev/github.com/rclone/rclone@v1.65.2/cmd/serve/s3
// Package s3 implements a fake s3 server for OpenList

import (
	"sort"

	"github.com/OpenListTeam/gofakes3"
)

// pager splits the object list into multiple pages based on the provided pagination parameters
// It handles sorting, marker-based pagination, and truncation
func (b *s3Backend) pager(list *gofakes3.ObjectList, page gofakes3.ListBucketPage) (*gofakes3.ObjectList, error) {
	// Sort directories (common prefixes) alphabetically
	sort.Slice(list.CommonPrefixes, func(i, j int) bool {
		return list.CommonPrefixes[i].Prefix < list.CommonPrefixes[j].Prefix
	})

	// Sort files (contents) by modification time
	sort.Slice(list.Contents, func(i, j int) bool {
		return list.Contents[i].LastModified.Before(list.Contents[j].LastModified.Time)
	})

	// Set default page size if not specified
	maxKeys := page.MaxKeys
	if maxKeys == 0 {
		maxKeys = 1000
	}

	// Handle marker-based pagination (skip items before the marker)
	if page.HasMarker {
		// Skip contents up to the marker
		for i, obj := range list.Contents {
			if obj.Key == page.Marker {
				list.Contents = list.Contents[i+1:]
				break
			}
		}

		// Skip common prefixes up to the marker
		for i, obj := range list.CommonPrefixes {
			if obj.Prefix == page.Marker {
				list.CommonPrefixes = list.CommonPrefixes[i+1:]
				break
			}
		}
	}

	// Create a new response with the paginated results
	response := gofakes3.NewObjectList()

	// Add directories first (limited by maxKeys)
	remainingItems := maxKeys
	for _, obj := range list.CommonPrefixes {
		if remainingItems <= 0 {
			break
		}
		response.AddPrefix(obj.Prefix)
		remainingItems--
	}

	// Then add files (limited by remaining maxKeys)
	for _, obj := range list.Contents {
		if remainingItems <= 0 {
			break
		}
		response.Add(obj)
		remainingItems--
	}

	// Set truncation flag and next marker if there are more items
	totalItems := len(list.CommonPrefixes) + len(list.Contents)
	if totalItems > int(maxKeys) {
		response.IsTruncated = true

		// Set the next marker based on the last item in the response
		if len(response.Contents) > 0 {
			response.NextMarker = response.Contents[len(response.Contents)-1].Key
		} else {
			response.NextMarker = response.CommonPrefixes[len(response.CommonPrefixes)-1].Prefix
		}
	}

	return response, nil
}
