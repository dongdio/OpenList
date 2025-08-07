// Package op provides operations for OpenList's core functionality
package op

import (
	"reflect"
	"strings"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/utility/errs"
)

type DriverConstructor func() driver.Driver

var driverMap = map[string]DriverConstructor{}
var driverInfoMap = map[string]driver.Info{}

func RegisterDriver(driver DriverConstructor) {
	// log.Infof("register driver: [%s]", config.Name)
	tempDriver := driver()
	tempConfig := tempDriver.Config()
	registerDriverItems(tempConfig, tempDriver.GetAddition())
	driverMap[tempConfig.Name] = driver
}

func GetDriver(name string) (DriverConstructor, error) {
	n, ok := driverMap[name]
	if !ok {
		return nil, errs.Errorf("no driver named: %s", name)
	}
	return n, nil
}

func GetDriverNames() []string {
	var driverNames []string
	for k := range driverInfoMap {
		driverNames = append(driverNames, k)
	}
	return driverNames
}

func GetDriverInfoMap() map[string]driver.Info {
	return driverInfoMap
}

func registerDriverItems(config driver.Config, addition driver.Additional) {
	tAddition := reflect.TypeOf(addition)
	for tAddition.Kind() == reflect.Pointer {
		tAddition = tAddition.Elem()
	}
	mainItems := getMainItems(config)
	additionalItems := getAdditionalItems(tAddition, config.DefaultRoot)
	driverInfoMap[config.Name] = driver.Info{
		Common:     mainItems,
		Additional: additionalItems,
		Config:     config,
	}
}

func getMainItems(config driver.Config) []driver.Item {
	items := []driver.Item{{
		Name:     "mount_path",
		Type:     consts.TypeString,
		Required: true,
		Help:     "The path you want to mount to, it is unique and cannot be repeated",
	}, {
		Name: "order",
		Type: consts.TypeNumber,
		Help: "use to sort",
	}, {
		Name: "remark",
		Type: consts.TypeText,
	}}
	if !config.NoCache {
		items = append(items, driver.Item{
			Name:     "cache_expiration",
			Type:     consts.TypeNumber,
			Default:  "30",
			Required: true,
			Help:     "The cache expiration time for this storage",
		})
	}
	if config.MustProxy() {
		items = append(items, driver.Item{
			Name:     "webdav_policy",
			Type:     consts.TypeSelect,
			Default:  "native_proxy",
			Options:  "use_proxy_url,native_proxy",
			Required: true,
		})
	} else {
		items = append(items, []driver.Item{{
			Name: "web_proxy",
			Type: consts.TypeBool,
		}, {
			Name:     "webdav_policy",
			Type:     consts.TypeSelect,
			Options:  "302_redirect,use_proxy_url,native_proxy",
			Default:  "302_redirect",
			Required: true,
		},
		}...)
		if config.ProxyRangeOption {
			item := driver.Item{
				Name: "proxy_range",
				Type: consts.TypeBool,
				Help: "Need to enable proxy",
			}
			if config.Name == "139Yun" {
				item.Default = "true"
			}
			items = append(items, item)
		}
	}
	items = append(items, driver.Item{
		Name: "down_proxy_url",
		Type: consts.TypeText,
	})
	items = append(items, driver.Item{
		Name:    "disable_proxy_sign",
		Type:    consts.TypeBool,
		Default: "false",
		Help:    "Disable sign for Download proxy URL",
	})
	if config.LocalSort {
		items = append(items, []driver.Item{{
			Name:    "order_by",
			Type:    consts.TypeSelect,
			Options: "name,size,modified",
		}, {
			Name:    "order_direction",
			Type:    consts.TypeSelect,
			Options: "asc,desc",
		}}...)
	}
	items = append(items, driver.Item{
		Name:    "extract_folder",
		Type:    consts.TypeSelect,
		Options: "front,back",
	})
	items = append(items, driver.Item{
		Name:     "disable_index",
		Type:     consts.TypeBool,
		Default:  "false",
		Required: true,
	})
	items = append(items, driver.Item{
		Name:     "enable_sign",
		Type:     consts.TypeBool,
		Default:  "false",
		Required: true,
	})
	return items
}

func getAdditionalItems(t reflect.Type, defaultRoot string) []driver.Item {
	var items []driver.Item
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type.Kind() == reflect.Struct {
			items = append(items, getAdditionalItems(field.Type, defaultRoot)...)
			continue
		}
		tag := field.Tag
		ignore, ok1 := tag.Lookup("ignore")
		name, ok2 := tag.Lookup("json")
		if (ok1 && ignore == "true") || !ok2 {
			continue
		}
		item := driver.Item{
			Name:     name,
			Type:     strings.ToLower(field.Type.Name()),
			Default:  tag.Get("default"),
			Options:  tag.Get("options"),
			Required: tag.Get("required") == "true",
			Help:     tag.Get("help"),
		}
		if tag.Get("type") != "" {
			item.Type = tag.Get("type")
		}
		if item.Name == "root_folder_id" || item.Name == "root_folder_path" {
			if item.Default == "" {
				item.Default = defaultRoot
			}
			item.Required = item.Default != ""
		}
		// set default type to string
		if item.Type == "" {
			item.Type = "string"
		}
		items = append(items, item)
	}
	return items
}