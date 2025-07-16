package handles

import (
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/sign"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/server/static"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

// ResetToken 重置系统令牌
func ResetToken(c *gin.Context) {
	// 生成新的随机令牌
	token := random.Token()

	// 创建设置项
	item := model.SettingItem{
		Key:   "token",
		Value: token,
		Type:  consts.TypeString,
		Group: model.SINGLE,
		Flag:  model.PRIVATE,
	}

	// 保存设置
	if err := op.SaveSettingItem(&item); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 重新初始化签名实例
	sign.Instance()
	common.SuccessResp(c, token)
}

// GetSetting 获取设置项
func GetSetting(c *gin.Context) {
	key := c.Query("key")
	keys := c.Query("keys")

	// 获取单个设置项
	if key != "" {
		item, err := op.GetSettingItemByKey(key)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		common.SuccessResp(c, item)
		return
	}

	// 获取多个设置项
	if keys == "" {
		common.ErrorStrResp(c, "key or keys parameter is required", 400)
		return
	}

	keyList := strings.Split(keys, ",")
	items, err := op.GetSettingItemInKeys(keyList)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	common.SuccessResp(c, items)
}

// SaveSettings 保存多个设置项
func SaveSettings(c *gin.Context) {
	var req []model.SettingItem
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 检查是否有设置项
	if len(req) == 0 {
		common.ErrorStrResp(c, "no settings to save", 400)
		return
	}

	// 保存设置项
	if err := op.SaveSettingItems(req); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 更新静态HTML
	static.UpdateIndexHTML()
	common.SuccessResp(c)
}

// ListSettings 列出设置项
func ListSettings(c *gin.Context) {
	groupStr := c.Query("group")
	groupsStr := c.Query("groups")

	var settings []model.SettingItem
	var err error

	// 获取所有设置项
	if groupsStr == "" && groupStr == "" {
		settings, err = op.GetSettingItems()
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		common.SuccessResp(c, settings)
		return
	}

	// 按组获取设置项
	var groupStrings []string
	if groupsStr != "" {
		groupStrings = strings.Split(groupsStr, ",")
	} else {
		groupStrings = []string{groupStr}
	}

	// 转换组ID为整数
	groups := make([]int, 0, len(groupStrings))
	for _, str := range groupStrings {
		group, err := strconv.Atoi(str)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		groups = append(groups, group)
	}

	// 获取指定组的设置项
	settings, err = op.GetSettingItemsInGroups(groups)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	common.SuccessResp(c, settings)
}

func DefaultSettings(c *gin.Context) {
	groupStr := c.Query("group")
	groupsStr := c.Query("groups")
	settings := model.InitialSettings()
	if groupsStr == "" && groupStr == "" {
		for i := range settings {
			(&settings[i]).Index = uint(i)
		}
		common.SuccessResp(c, settings)
	} else {
		var groupStrings []string
		if groupsStr != "" {
			groupStrings = strings.Split(groupsStr, ",")
		} else {
			groupStrings = append(groupStrings, groupStr)
		}
		var groups []int
		for _, str := range groupStrings {
			group, err := strconv.Atoi(str)
			if err != nil {
				common.ErrorResp(c, err, 400)
				return
			}
			groups = append(groups, group)
		}
		sort.Ints(groups)
		var resultItems []model.SettingItem
		for _, group := range groups {
			for i := range settings {
				item := settings[i]
				if group == item.Group {
					item.Index = uint(i)
					resultItems = append(resultItems, item)
				}
			}
		}
		common.SuccessResp(c, resultItems)
	}
}

// DeleteSetting 删除设置项
func DeleteSetting(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		common.ErrorStrResp(c, "key parameter is required", 400)
		return
	}

	if err := op.DeleteSettingItemByKey(key); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}

// PublicSettings 获取公开设置
func PublicSettings(c *gin.Context) {
	common.SuccessResp(c, op.GetPublicSettingsMap())
}