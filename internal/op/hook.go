// Package op provides operations for OpenList's core functionality
package op

import (
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// ObjsUpdateHook is a function type for hooks that process objects after they are updated
type ObjsUpdateHook = func(parent string, objs []model.Obj)

var (
	// objsUpdateHooks stores registered hooks for object updates
	objsUpdateHooks = make([]ObjsUpdateHook, 0)
)

// RegisterObjsUpdateHook registers a new hook to be called when objects are updated
func RegisterObjsUpdateHook(hook ObjsUpdateHook) {
	if hook == nil {
		log.Warn("attempted to register nil ObjsUpdateHook")
		return
	}
	objsUpdateHooks = append(objsUpdateHooks, hook)
}

// HandleObjsUpdateHook calls all registered object update hooks
func HandleObjsUpdateHook(parent string, objs []model.Obj) {
	for _, hook := range objsUpdateHooks {
		hook(parent, objs)
	}
}

// SettingItemHook is a function type for hooks that process setting items
type SettingItemHook func(item *model.SettingItem) error

// settingItemHooks maps setting keys to their processing hooks
var settingItemHooks = map[string]SettingItemHook{
	// Process file type settings
	consts.VideoTypes: func(item *model.SettingItem) error {
		conf.SlicesMap[consts.VideoTypes] = strings.Split(item.Value, ",")
		return nil
	},
	consts.AudioTypes: func(item *model.SettingItem) error {
		conf.SlicesMap[consts.AudioTypes] = strings.Split(item.Value, ",")
		return nil
	},
	consts.ImageTypes: func(item *model.SettingItem) error {
		conf.SlicesMap[consts.ImageTypes] = strings.Split(item.Value, ",")
		return nil
	},
	consts.TextTypes: func(item *model.SettingItem) error {
		conf.SlicesMap[consts.TextTypes] = strings.Split(item.Value, ",")
		return nil
	},

	// Process proxy settings
	consts.ProxyTypes: func(item *model.SettingItem) error {
		conf.SlicesMap[consts.ProxyTypes] = strings.Split(item.Value, ",")
		return nil
	},
	consts.ProxyIgnoreHeaders: func(item *model.SettingItem) error {
		conf.SlicesMap[consts.ProxyIgnoreHeaders] = strings.Split(item.Value, ",")
		return nil
	},

	// Process privacy settings
	consts.PrivacyRegs: func(item *model.SettingItem) error {
		regStrs := strings.Split(item.Value, "\n")
		regs := make([]*regexp.Regexp, 0, len(regStrs))

		for _, regStr := range regStrs {
			regStr = strings.TrimSpace(regStr)
			if regStr == "" {
				continue
			}

			reg, err := regexp.Compile(regStr)
			if err != nil {
				return errs.Wrapf(err, "invalid regex pattern: %s", regStr)
			}
			regs = append(regs, reg)
		}

		conf.PrivacyReg = regs
		return nil
	},

	// Process filename character mapping
	consts.FilenameCharMapping: func(item *model.SettingItem) error {
		err := utils.JSONTool.UnmarshalFromString(item.Value, &conf.FilenameCharMap)
		if err != nil {
			return errs.Wrap(err, "failed to parse filename character mapping")
		}
		log.Debugf("filename char mapping: %+v", conf.FilenameCharMap)
		return nil
	},

	// Process direct link parameters
	consts.IgnoreDirectLinkParams: func(item *model.SettingItem) error {
		conf.SlicesMap[consts.IgnoreDirectLinkParams] = strings.Split(item.Value, ",")
		return nil
	},
}

// RegisterSettingItemHook registers a hook for a specific setting key
func RegisterSettingItemHook(key string, hook SettingItemHook) {
	if key == "" {
		log.Warn("attempted to register hook with empty key")
		return
	}
	if hook == nil {
		log.Warnf("attempted to register nil hook for key: %s", key)
		return
	}
	settingItemHooks[key] = hook
}

// HandleSettingItemHook processes a setting item with its registered hook
// Returns:
//   - hasHook: true if a hook was found and executed for the item
//   - err: any error that occurred during hook execution
func HandleSettingItemHook(item *model.SettingItem) (hasHook bool, err error) {
	if item == nil {
		return false, errs.New("cannot handle nil setting item")
	}

	hook, ok := settingItemHooks[item.Key]
	if !ok {
		return false, nil
	}

	return true, hook(item)
}

// StorageHook is a function type for hooks that process storage operations
type StorageHook func(typ string, storage driver.Driver)

// storageHooks stores registered hooks for storage operations
var storageHooks = make([]StorageHook, 0)

// CallStorageHooks calls all registered storage hooks
func CallStorageHooks(typ string, storage driver.Driver) {
	for _, hook := range storageHooks {
		hook(typ, storage)
	}
}

// RegisterStorageHook registers a new hook for storage operations
func RegisterStorageHook(hook StorageHook) {
	if hook == nil {
		log.Warn("attempted to register nil StorageHook")
		return
	}
	storageHooks = append(storageHooks, hook)
}