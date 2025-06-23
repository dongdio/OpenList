package op_test

import (
	"testing"

	_ "github.com/dongdio/OpenList/drivers"
	"github.com/dongdio/OpenList/internal/op"
)

// TestDriverItemsMap verifies that the driver information map is properly populated
// This test depends on the drivers package being imported to register the drivers
func TestDriverItemsMap(t *testing.T) {
	// Get the driver information map
	driverInfoMap := op.GetDriverInfoMap()

	// Verify that drivers have been registered
	if len(driverInfoMap) == 0 {
		t.Errorf("expected driver info map to contain drivers, but it is empty")
	} else {
		// Log number of registered drivers for informational purposes
		t.Logf("found %d registered drivers", len(driverInfoMap))

		// Optionally log driver names for debugging
		if testing.Verbose() {
			t.Logf("registered drivers: %v", op.GetDriverNames())
		}
	}
}
