package handles

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/consts"
	_115 "github.com/dongdio/OpenList/v4/drivers/115"
	_115_open "github.com/dongdio/OpenList/v4/drivers/115_open"
	"github.com/dongdio/OpenList/v4/drivers/pikpak"
	"github.com/dongdio/OpenList/v4/drivers/thunder"
	"github.com/dongdio/OpenList/v4/drivers/thunder_browser"
	"github.com/dongdio/OpenList/v4/drivers/thunderx"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/task"
)

// SetAria2Req Aria2设置请求
type SetAria2Req struct {
	Uri    string `json:"uri" form:"uri" binding:"required"`
	Secret string `json:"secret" form:"secret"`
}

// SetAria2 设置Aria2离线下载
func SetAria2(c *gin.Context) {
	var req SetAria2Req
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 保存设置
	items := []model.SettingItem{
		{Key: consts.Aria2Uri, Value: req.Uri, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
		{Key: consts.Aria2Secret, Value: req.Secret, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 初始化工具
	aria2Tool, err := tool.Tools.Get("aria2")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	version, err := aria2Tool.Init()
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, version)
}

// SetQbittorrentReq qBittorrent设置请求
type SetQbittorrentReq struct {
	Url      string `json:"url" form:"url" binding:"required"`
	Seedtime string `json:"seedtime" form:"seedtime"`
}

// SetQbittorrent 设置qBittorrent离线下载
func SetQbittorrent(c *gin.Context) {
	var req SetQbittorrentReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 保存设置
	items := []model.SettingItem{
		{Key: consts.QbittorrentUrl, Value: req.Url, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
		{Key: consts.QbittorrentSeedtime, Value: req.Seedtime, Type: consts.TypeNumber, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 初始化工具
	qbTool, err := tool.Tools.Get("qBittorrent")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	if _, err = qbTool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, "ok")
}

// SetTransmissionReq Transmission设置请求
type SetTransmissionReq struct {
	Uri      string `json:"uri" form:"uri" binding:"required"`
	Seedtime string `json:"seedtime" form:"seedtime"`
}

// SetTransmission 设置Transmission离线下载
func SetTransmission(c *gin.Context) {
	var req SetTransmissionReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 保存设置
	items := []model.SettingItem{
		{Key: consts.TransmissionUri, Value: req.Uri, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
		{Key: consts.TransmissionSeedtime, Value: req.Seedtime, Type: consts.TypeNumber, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 初始化工具
	transmissionTool, err := tool.Tools.Get("Transmission")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	if _, err = transmissionTool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, "ok")
}

type Set115OpenReq struct {
	TempDir string `json:"temp_dir" form:"temp_dir"`
}

func Set115Open(c *gin.Context) {
	var req Set115OpenReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if req.TempDir != "" {
		storage, _, err := op.GetStorageAndActualPath(req.TempDir)
		if err != nil {
			common.ErrorStrResp(c, "storage does not exists", 400)
			return
		}
		if storage.Config().CheckStatus && storage.GetStorage().Status != op.WORK {
			common.ErrorStrResp(c, "storage not init: "+storage.GetStorage().Status, 400)
			return
		}
		if _, ok := storage.(*_115_open.Open115); !ok {
			common.ErrorStrResp(c, "unsupported storage driver for offline download, only 115 Open is supported", 400)
			return
		}
	}
	items := []model.SettingItem{
		{Key: consts.Pan115OpenTempDir, Value: req.TempDir, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	_tool, err := tool.Tools.Get("115 Open")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if _, err = _tool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, "ok")
}

// Set115Req 115云设置请求
type Set115Req struct {
	TempDir string `json:"temp_dir" form:"temp_dir"`
}

// Set115 设置115云离线下载
func Set115(c *gin.Context) {
	var req Set115Req
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证临时目录
	if req.TempDir != "" {
		storage, _, err := op.GetStorageAndActualPath(req.TempDir)
		if err != nil {
			common.ErrorStrResp(c, "storage does not exist", 400)
			return
		}

		if storage.Config().CheckStatus && storage.GetStorage().Status != op.WORK {
			common.ErrorStrResp(c, "storage not initialized: "+storage.GetStorage().Status, 400)
			return
		}

		if _, ok := storage.(*_115.Pan115); !ok {
			common.ErrorStrResp(c, "unsupported storage driver for offline download, only 115 Cloud is supported", 400)
			return
		}
	}

	// 保存设置
	items := []model.SettingItem{
		{Key: consts.Pan115TempDir, Value: req.TempDir, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 初始化工具
	cloudTool, err := tool.Tools.Get("115 Cloud")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	if _, err = cloudTool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, "ok")
}

// SetPikPakReq PikPak设置请求
type SetPikPakReq struct {
	TempDir string `json:"temp_dir" form:"temp_dir"`
}

// SetPikPak 设置PikPak离线下载
func SetPikPak(c *gin.Context) {
	var req SetPikPakReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证临时目录
	if req.TempDir != "" {
		storage, _, err := op.GetStorageAndActualPath(req.TempDir)
		if err != nil {
			common.ErrorStrResp(c, "storage does not exist", 400)
			return
		}

		if storage.Config().CheckStatus && storage.GetStorage().Status != op.WORK {
			common.ErrorStrResp(c, "storage not initialized: "+storage.GetStorage().Status, 400)
			return
		}

		if _, ok := storage.(*pikpak.PikPak); !ok {
			common.ErrorStrResp(c, "unsupported storage driver for offline download, only PikPak is supported", 400)
			return
		}
	}

	// 保存设置
	items := []model.SettingItem{
		{Key: consts.PikPakTempDir, Value: req.TempDir, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 初始化工具
	pikpakTool, err := tool.Tools.Get("PikPak")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	if _, err = pikpakTool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, "ok")
}

type SetThunderXReq struct {
	TempDir string `json:"temp_dir" form:"temp_dir"`
}

func SetThunderX(c *gin.Context) {
	var req SetThunderXReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if req.TempDir != "" {
		storage, _, err := op.GetStorageAndActualPath(req.TempDir)
		if err != nil {
			common.ErrorStrResp(c, "storage does not exists", 400)
			return
		}
		if storage.Config().CheckStatus && storage.GetStorage().Status != op.WORK {
			common.ErrorStrResp(c, "storage not init: "+storage.GetStorage().Status, 400)
			return
		}
		if _, ok := storage.(*thunderx.ThunderX); !ok {
			common.ErrorStrResp(c, "unsupported storage driver for offline download, only ThunderX is supported", 400)
			return
		}
	}
	items := []model.SettingItem{
		{
			Key:   consts.ThunderXTempDir,
			Value: req.TempDir,
			Type:  consts.TypeString,
			Group: model.OFFLINE_DOWNLOAD,
			Flag:  model.PRIVATE,
		},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	_tool, err := tool.Tools.Get("ThunderX")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if _, err = _tool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, "ok")
}

// SetThunderReq 迅雷设置请求
type SetThunderReq struct {
	TempDir string `json:"temp_dir" form:"temp_dir"`
}

// SetThunder 设置迅雷离线下载
func SetThunder(c *gin.Context) {
	var req SetThunderReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证临时目录
	if req.TempDir != "" {
		storage, _, err := op.GetStorageAndActualPath(req.TempDir)
		if err != nil {
			common.ErrorStrResp(c, "storage does not exist", 400)
			return
		}

		if storage.Config().CheckStatus && storage.GetStorage().Status != op.WORK {
			common.ErrorStrResp(c, "storage not initialized: "+storage.GetStorage().Status, 400)
			return
		}

		if _, ok := storage.(*thunder.Thunder); !ok {
			common.ErrorStrResp(c, "unsupported storage driver for offline download, only Thunder is supported", 400)
			return
		}
	}

	// 保存设置
	items := []model.SettingItem{
		{Key: consts.ThunderTempDir, Value: req.TempDir, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 初始化工具
	thunderTool, err := tool.Tools.Get("Thunder")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	if _, err = thunderTool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, "ok")
}

// SetThunderBrowserReq 迅雷浏览器设置请求
type SetThunderBrowserReq struct {
	TempDir string `json:"temp_dir" form:"temp_dir"`
}

// SetThunderBrowser 设置迅雷浏览器离线下载
func SetThunderBrowser(c *gin.Context) {
	var req SetThunderBrowserReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证临时目录
	if req.TempDir != "" {
		storage, _, err := op.GetStorageAndActualPath(req.TempDir)
		if err != nil {
			common.ErrorStrResp(c, "storage does not exist", 400)
			return
		}

		if storage.Config().CheckStatus && storage.GetStorage().Status != op.WORK {
			common.ErrorStrResp(c, "storage not initialized: "+storage.GetStorage().Status, 400)
			return
		}

		// 检查存储类型
		switch storage.(type) {
		case *thunder_browser.ThunderBrowser, *thunder_browser.ThunderBrowserExpert:
			// 支持的存储类型
		default:
			common.ErrorStrResp(c, "unsupported storage driver for offline download, only ThunderBrowser is supported", 400)
			return
		}
	}

	// 保存设置
	items := []model.SettingItem{
		{Key: consts.ThunderBrowserTempDir, Value: req.TempDir, Type: consts.TypeString, Group: model.OFFLINE_DOWNLOAD, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 初始化工具
	browserTool, err := tool.Tools.Get("ThunderBrowser")
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if _, err = browserTool.Init(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, "ok")
}

// OfflineDownloadTools 获取可用的离线下载工具列表
func OfflineDownloadTools(c *gin.Context) {
	tools := tool.Tools.Names()
	common.SuccessResp(c, tools)
}

// AddOfflineDownloadReq 添加离线下载请求
type AddOfflineDownloadReq struct {
	Urls         []string `json:"urls" binding:"required"`
	Path         string   `json:"path" binding:"required"`
	Tool         string   `json:"tool" binding:"required"`
	DeletePolicy string   `json:"delete_policy"`
}

// AddOfflineDownload 添加离线下载任务
func AddOfflineDownload(c *gin.Context) {
	user := c.Value(consts.UserKey).(*model.User)
	if !user.CanAddOfflineDownloadTasks() {
		common.ErrorStrResp(c, "permission denied", 403)
		return
	}

	var req AddOfflineDownloadReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取完整路径
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 为每个URL创建下载任务
	tasks := make([]task.TaskExtensionInfo, 0, len(req.Urls))
	for _, url := range req.Urls {
		// Filter out empty lines and whitespace-only strings
		trimmedUrl := strings.TrimSpace(url)
		if trimmedUrl == "" {
			continue
		}
		t, err := tool.AddURL(c, &tool.AddURLArgs{
			URL:          trimmedUrl,
			DstDirPath:   reqPath,
			Tool:         req.Tool,
			DeletePolicy: tool.DeletePolicy(req.DeletePolicy),
		})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		if t != nil {
			tasks = append(tasks, t)
		}
	}

	common.SuccessResp(c, gin.H{
		"tasks": getTaskInfos(tasks),
	})
}