package op

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/go-cache"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/singleflight"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Cache for storing individual setting items
var settingCache = cache.NewMemCache(cache.WithShards[*model.SettingItem](4))
var settingG singleflight.Group[*model.SettingItem]

// Function to cache an individual setting item
var settingCacheF = func(item *model.SettingItem) {
	settingCache.Set(item.Key, item, cache.WithEx[*model.SettingItem](time.Hour))
}

// Cache for storing groups of setting items
var settingGroupCache = cache.NewMemCache(cache.WithShards[[]model.SettingItem](4))
var settingGroupG singleflight.Group[[]model.SettingItem]

// Function to cache a group of setting items
var settingGroupCacheF = func(key string, items []model.SettingItem) {
	settingGroupCache.Set(key, items, cache.WithEx[[]model.SettingItem](time.Hour))
}

// Callbacks to be executed when settings change
var settingChangingCallbacks = make([]func(), 0)

// RegisterSettingChangingCallback registers a function to be called
// when settings are updated
func RegisterSettingChangingCallback(f func()) {
	settingChangingCallbacks = append(settingChangingCallbacks, f)
}

// SettingCacheUpdate clears all setting caches and executes
// registered change callbacks
func SettingCacheUpdate() {
	settingCache.Clear()
	settingGroupCache.Clear()
	for _, cb := range settingChangingCallbacks {
		cb()
	}
}

// GetPublicSettingsMap returns a map of all public settings
// with key-value pairs
func GetPublicSettingsMap() map[string]string {
	items, _ := GetPublicSettingItems()
	publicSettings := make(map[string]string)
	for _, item := range items {
		publicSettings[item.Key] = item.Value
	}
	return publicSettings
}

// GetSettingsMap returns a map of all settings with key-value pairs
func GetSettingsMap() map[string]string {
	items, _ := GetSettingItems()
	settings := make(map[string]string)
	for _, item := range items {
		settings[item.Key] = item.Value
	}
	return settings
}

// GetSettingItems retrieves all setting items, using cache when available
func GetSettingItems() ([]model.SettingItem, error) {
	// Check cache first
	if items, ok := settingGroupCache.Get("ALL_SETTING_ITEMS"); ok {
		return items, nil
	}

	// Use singleflight to prevent duplicate database queries
	items, err, _ := settingGroupG.Do("ALL_SETTING_ITEMS", func() ([]model.SettingItem, error) {
		items, err := db.GetSettingItems()
		if err != nil {
			return nil, err
		}
		settingGroupCacheF("ALL_SETTING_ITEMS", items)
		return items, nil
	})
	return items, err
}

// GetPublicSettingItems retrieves all public setting items, using cache when available
func GetPublicSettingItems() ([]model.SettingItem, error) {
	// Check cache first
	if items, ok := settingGroupCache.Get("ALL_PUBLIC_SETTING_ITEMS"); ok {
		return items, nil
	}

	// Use singleflight to prevent duplicate database queries
	items, err, _ := settingGroupG.Do("ALL_PUBLIC_SETTING_ITEMS", func() ([]model.SettingItem, error) {
		items, err := db.GetPublicSettingItems()
		if err != nil {
			return nil, err
		}
		settingGroupCacheF("ALL_PUBLIC_SETTING_ITEMS", items)
		return items, nil
	})
	return items, err
}

// GetSettingItemByKey retrieves a setting item by its key, using cache when available
func GetSettingItemByKey(key string) (*model.SettingItem, error) {
	// Check cache first
	if item, ok := settingCache.Get(key); ok {
		return item, nil
	}

	// Use singleflight to prevent duplicate database queries
	item, err, _ := settingG.Do(key, func() (*model.SettingItem, error) {
		item, err := db.GetSettingItemByKey(key)
		if err != nil {
			return nil, err
		}
		settingCacheF(item)
		return item, nil
	})
	return item, err
}

// GetSettingItemInKeys retrieves multiple setting items by their keys
func GetSettingItemInKeys(keys []string) ([]model.SettingItem, error) {
	var items []model.SettingItem
	for _, key := range keys {
		item, err := GetSettingItemByKey(key)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

// GetSettingItemsByGroup retrieves all setting items in a specific group
func GetSettingItemsByGroup(group int) ([]model.SettingItem, error) {
	key := strconv.Itoa(group)

	// Check cache first
	if items, ok := settingGroupCache.Get(key); ok {
		return items, nil
	}

	// Use singleflight to prevent duplicate database queries
	items, err, _ := settingGroupG.Do(key, func() ([]model.SettingItem, error) {
		items, err := db.GetSettingItemsByGroup(group)
		if err != nil {
			return nil, err
		}
		settingGroupCacheF(key, items)
		return items, nil
	})
	return items, err
}

// GetSettingItemsInGroups retrieves all setting items in multiple groups
func GetSettingItemsInGroups(groups []int) ([]model.SettingItem, error) {
	// Sort groups for consistent cache key generation
	sort.Ints(groups)
	key := strings.Join(utils.MustSliceConvert(groups, func(i int) string {
		return strconv.Itoa(i)
	}), ",")

	// Check cache first
	if items, ok := settingGroupCache.Get(key); ok {
		return items, nil
	}

	// Use singleflight to prevent duplicate database queries
	items, err, _ := settingGroupG.Do(key, func() ([]model.SettingItem, error) {
		items, err := db.GetSettingItemsInGroups(groups)
		if err != nil {
			return nil, err
		}
		settingGroupCacheF(key, items)
		return items, nil
	})
	return items, err
}

// SaveSettingItems saves multiple setting items, handling hooks as needed
func SaveSettingItems(items []model.SettingItem) error {
	// Process items with hooks first
	for i := range items {
		item := &items[i]
		if it, ok := MigrationSettingItems[item.Key]; ok &&
			item.Value == it.MigrationValue {
			item.Value = it.Value
		}
		if ok, err := HandleSettingItemHook(item); ok && err != nil {
			return errors.Errorf("failed to execute hook on %s: %+v", item.Key, err)
		}
	}
	err := db.SaveSettingItems(items)
	if err != nil {
		return errors.Errorf("failed save setting: %+v", err)
	}
	return nil
}

// SaveSettingItem saves a single setting item, handling hooks as needed
func SaveSettingItem(item *model.SettingItem) error {
	if it, ok := MigrationSettingItems[item.Key]; ok &&
		item.Value == it.MigrationValue {
		item.Value = it.Value
	}
	// Process hook if applicable
	if _, err := HandleSettingItemHook(item); err != nil {
		return errors.Errorf("failed to execute hook on %s: %+v", item.Key, err)
	}

	// Save to database
	if err := db.SaveSettingItem(item); err != nil {
		return err
	}

	// Update cache
	SettingCacheUpdate()
	return nil
}

// DeleteSettingItemByKey deletes a setting item by its key
// Only deprecated items can be deleted
func DeleteSettingItemByKey(key string) error {
	// Verify the item exists and is deprecated
	old, err := GetSettingItemByKey(key)
	if err != nil {
		return errors.WithMessage(err, "failed to get setting item")
	}

	if !old.IsDeprecated() {
		return errors.Errorf("setting [%s] is not deprecated", key)
	}

	// Update cache
	SettingCacheUpdate()

	// Delete from database
	return db.DeleteSettingItemByKey(key)
}

type MigrationValueItem struct {
	MigrationValue, Value string
}

var MigrationSettingItems map[string]MigrationValueItem