package op_test

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/db"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/pkg/utils"
)

// Initialize the testing environment with an in-memory SQLite database
func init() {
	// Set up in-memory database for testing
	dB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Use default configuration for testing
	conf.Conf = conf.DefaultConfig()

	// Initialize database
	db.Init(dB)
}

// TestCreateStorage tests storage creation functionality
func TestCreateStorage(t *testing.T) {
	var testCases = []struct {
		name    string
		storage model.Storage
		wantErr bool
	}{
		{
			name:    "valid storage",
			storage: model.Storage{Driver: "Local", MountPath: "/local", Addition: `{"root_folder_path":"."}`},
			wantErr: false,
		},
		{
			name:    "duplicate mount path",
			storage: model.Storage{Driver: "Local", MountPath: "/local", Addition: `{"root_folder_path":"."}`},
			wantErr: true,
		},
		{
			name:    "invalid driver",
			storage: model.Storage{Driver: "None", MountPath: "/none", Addition: `{"root_folder_path":"."}`},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := op.CreateStorage(context.Background(), tc.storage)
			if (err != nil) != tc.wantErr {
				t.Errorf("CreateStorage() error = %v, wantErr %v", err, tc.wantErr)
			} else if err != nil && tc.wantErr {
				t.Logf("Expected error received: %v", err)
			}
		})
	}
}

// TestGetStorageVirtualFilesByPath tests the retrieval of virtual files
func TestGetStorageVirtualFilesByPath(t *testing.T) {
	// Set up test storage entries
	setupStorages(t)

	// Get virtual files at path /a
	virtualFiles := op.GetStorageVirtualFilesByPath("/a")

	// Extract file names for comparison
	var actualNames []string
	for _, virtualFile := range virtualFiles {
		actualNames = append(actualNames, virtualFile.GetName())
	}

	// Expected directory names under /a
	expectedNames := []string{"b", "c", "d"}

	// Compare results
	if utils.SliceEqual(actualNames, expectedNames) {
		t.Logf("Virtual files test passed")
	} else {
		t.Errorf("Expected virtual files: %v, got: %v", expectedNames, actualNames)
	}
}

// TestGetBalancedStorage tests the load balancing functionality
func TestGetBalancedStorage(t *testing.T) {
	// Create a set to collect mount paths returned by the balancer
	storageSet := mapset.NewSet[string]()

	// Call the balancer multiple times and collect the results
	for i := 0; i < 5; i++ {
		storage := op.GetBalancedStorage("/a/d/e1")
		storageSet.Add(storage.GetStorage().MountPath)
	}

	// We expect to see both the primary and balance storage paths
	expectedSet := mapset.NewSet([]string{"/a/d/e1", "/a/d/e1.balance"}...)

	// Compare results
	if !expectedSet.Equal(storageSet) {
		t.Errorf("Expected balanced storage paths: %v, got: %v", expectedSet, storageSet)
	}
}

// setupStorages creates a set of test storages with specific paths
func setupStorages(t *testing.T) {
	var storages = []model.Storage{
		{Driver: "Local", MountPath: "/a/b", Order: 0, Addition: `{"root_folder_path":"."}`},
		{Driver: "Local", MountPath: "/adc", Order: 0, Addition: `{"root_folder_path":"."}`},
		{Driver: "Local", MountPath: "/a/c", Order: 1, Addition: `{"root_folder_path":"."}`},
		{Driver: "Local", MountPath: "/a/d", Order: 2, Addition: `{"root_folder_path":"."}`},
		{Driver: "Local", MountPath: "/a/d/e1", Order: 3, Addition: `{"root_folder_path":"."}`},
		{Driver: "Local", MountPath: "/a/d/e", Order: 4, Addition: `{"root_folder_path":"."}`},
		{Driver: "Local", MountPath: "/a/d/e1.balance", Order: 4, Addition: `{"root_folder_path":"."}`},
	}

	for _, storage := range storages {
		_, err := op.CreateStorage(context.Background(), storage)
		if err != nil {
			t.Fatalf("Failed to create test storage: %v", err)
		}
	}
}
