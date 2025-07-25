// Package op provides operations for OpenList's core functionality
package op

import (
	"reflect"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/driver"
)

// DriverConstructor is a function type that creates a new driver instance
type DriverConstructor func() driver.Driver

// Maps to store registered drivers and their information
var (
	// driverMap maps driver names to their constructor functions
	driverMap = map[string]DriverConstructor{}

	// driverInfoMap maps driver names to their metadata and configuration
	driverInfoMap = map[string]driver.Info{}
)

// RegisterDriver registers a new storage driver with the system
// It extracts the driver's configuration and additional information
func RegisterDriver(constructor DriverConstructor) {
	if constructor == nil {
		log.Error("attempted to register nil driver constructor")
		return
	}

	// Create a temporary instance to get configuration
	tempDriver := constructor()
	if tempDriver == nil {
		log.Error("driver constructor returned nil")
		return
	}

	tempConfig := tempDriver.Config()
	log.Debugf("registering driver: [%s]", tempConfig.Name)

	// Register driver items and store the constructor
	registerDriverItems(tempConfig, tempDriver.GetAddition())
	driverMap[tempConfig.Name] = constructor
}

// GetDriver returns the constructor for a driver by name
// Returns an error if the driver is not found
func GetDriver(name string) (DriverConstructor, error) {
	constructor, ok := driverMap[name]
	if !ok {
		return nil, errors.Errorf("driver not found: %s", name)
	}
	return constructor, nil
}

// GetDriverNames returns a list of all registered driver names
func GetDriverNames() []string {
	driverNames := make([]string, 0, len(driverInfoMap))
	for name := range driverInfoMap {
		driverNames = append(driverNames, name)
	}
	return driverNames
}

// GetDriverInfoMap returns the map of driver information for all registered drivers
func GetDriverInfoMap() map[string]driver.Info {
	return driverInfoMap
}

// registerDriverItems processes driver configuration and additional information
// to build the driver info structure
func registerDriverItems(config driver.Config, addition driver.Additional) {
	log.Debugf("processing addition for %s: %+v", config.Name, addition)

	// Get the underlying type of the addition
	additionType := reflect.TypeOf(addition)
	for additionType.Kind() == reflect.Pointer {
		additionType = additionType.Elem()
	}

	// Generate driver items
	mainItems := getMainItems(config)
	additionalItems := getAdditionalItems(additionType, config.DefaultRoot)

	// Store driver info
	driverInfoMap[config.Name] = driver.Info{
		Common:     mainItems,
		Additional: additionalItems,
		Config:     config,
	}
}

// getMainItems generates the common configuration items for all drivers
// based on the driver's capabilities
func getMainItems(config driver.Config) []driver.Item {
	// Basic items that all drivers have
	items := []driver.Item{
		{
			Name:     "mount_path",
			Type:     consts.TypeString,
			Required: true,
			Help:     "The path you want to mount to, it is unique and cannot be repeated",
		},
		{
			Name: "order",
			Type: consts.TypeNumber,
			Help: "Used to sort storages",
		},
		{
			Name: "remark",
			Type: consts.TypeText,
			Help: "Optional notes about this storage",
		},
	}

	// Add cache expiration if the driver supports caching
	if !config.NoCache {
		items = append(items, driver.Item{
			Name:     "cache_expiration",
			Type:     consts.TypeNumber,
			Default:  "30",
			Required: true,
			Help:     "The cache expiration time in minutes for this storage",
		})
	}

	// Add proxy options if the driver supports it
	if config.MustProxy() {
		items = append(items, driver.Item{
			Name:     "webdav_policy",
			Type:     consts.TypeSelect,
			Default:  "native_proxy",
			Options:  "use_proxy_url,native_proxy",
			Required: true,
		})
	} else {
		items = append(items, []driver.Item{
			{
				Name: "web_proxy",
				Type: consts.TypeBool,
				Help: "Enable web proxy for this storage",
			},
			{
				Name:     "webdav_policy",
				Type:     consts.TypeSelect,
				Options:  "302_redirect,use_proxy_url,native_proxy",
				Default:  "302_redirect",
				Required: true,
				Help:     "WebDAV access policy for this storage",
			},
		}...)

		// Add proxy range option if supported
		if config.ProxyRangeOption {
			item := driver.Item{
				Name: "proxy_range",
				Type: consts.TypeBool,
				Help: "Enable range requests via proxy (requires web_proxy to be enabled)",
			}
			if config.Name == "139Yun" {
				item.Default = "true"
			}
			items = append(items, item)
		}
	}

	// Add download proxy URL option
	items = append(items, driver.Item{
		Name: "down_proxy_url",
		Type: consts.TypeText,
		Help: "Optional proxy URL for downloads",
	})

	items = append(items, driver.Item{
		Name:    "disable_proxy_sign",
		Type:    consts.TypeBool,
		Default: "false",
		Help:    "Disable sign for Download proxy URL",
	})

	// Add sorting options if supported
	if config.LocalSort {
		items = append(items, []driver.Item{
			{
				Name:    "order_by",
				Type:    consts.TypeSelect,
				Options: "name,size,modified",
				Help:    "Field to sort items by",
			},
			{
				Name:    "order_direction",
				Type:    consts.TypeSelect,
				Options: "asc,desc",
				Help:    "Sort direction (ascending or descending)",
			},
		}...)
	}

	// Add extract folder option
	items = append(items, driver.Item{
		Name:    "extract_folder",
		Type:    consts.TypeSelect,
		Options: "front,back",
		Help:    "Where to place folders when listing (front or back)",
	})

	// Add index and sign options
	items = append(items, driver.Item{
		Name:     "disable_index",
		Type:     consts.TypeBool,
		Default:  "false",
		Required: true,
		Help:     "Disable indexing for this storage",
	})

	items = append(items, driver.Item{
		Name:     "enable_sign",
		Type:     consts.TypeBool,
		Default:  "false",
		Required: true,
		Help:     "Enable request signing for this storage",
	})

	return items
}

// getAdditionalItems extracts driver-specific configuration items
// from the Additional struct using reflection
func getAdditionalItems(t reflect.Type, defaultRoot string) []driver.Item {
	items := make([]driver.Item, 0)

	// Process each field in the struct
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Recursively process nested structs
		if field.Type.Kind() == reflect.Struct {
			items = append(items, getAdditionalItems(field.Type, defaultRoot)...)
			continue
		}

		// Get field tags
		tag := field.Tag
		ignore, hasIgnore := tag.Lookup("ignore")
		name, hasName := tag.Lookup("json")

		// Skip fields that should be ignored or don't have a json tag
		if (hasIgnore && ignore == "true") || !hasName {
			continue
		}

		// Create item from field metadata
		item := driver.Item{
			Name:     name,
			Type:     strings.ToLower(field.Type.Name()),
			Default:  tag.Get("default"),
			Options:  tag.Get("options"),
			Required: tag.Get("required") == "true",
			Help:     tag.Get("help"),
		}

		// Override type if specified
		if tag.Get("type") != "" {
			item.Type = tag.Get("type")
		}

		// Special handling for root folder fields
		if item.Name == "root_folder_id" || item.Name == "root_folder_path" {
			if item.Default == "" {
				item.Default = defaultRoot
			}
			item.Required = item.Default != ""
		}

		// Set default type to string if not specified
		if item.Type == "" {
			item.Type = "string"
		}

		items = append(items, item)
	}

	return items
}
