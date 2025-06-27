package handles

import (
	"math"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xhofe/tache"

	"github.com/dongdio/OpenList/internal/fs"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/offline_download/tool"
	"github.com/dongdio/OpenList/server/common"
	task2 "github.com/dongdio/OpenList/utility/task"
	"github.com/dongdio/OpenList/utility/utils"
)

// TaskInfo 任务信息结构体
type TaskInfo struct {
	ID          string      `json:"id"`           // 任务ID
	Name        string      `json:"name"`         // 任务名称
	Creator     string      `json:"creator"`      // 创建者用户名
	CreatorRole int         `json:"creator_role"` // 创建者角色
	State       tache.State `json:"state"`        // 任务状态
	Status      string      `json:"status"`       // 任务状态描述
	Progress    float64     `json:"progress"`     // 任务进度(0-100)
	StartTime   *time.Time  `json:"start_time"`   // 开始时间
	EndTime     *time.Time  `json:"end_time"`     // 结束时间
	TotalBytes  int64       `json:"total_bytes"`  // 总字节数
	Error       string      `json:"error"`        // 错误信息
}

// getTaskInfo 从任务对象获取任务信息
func getTaskInfo[T task2.TaskExtensionInfo](task T) TaskInfo {
	errMsg := ""
	if task.GetErr() != nil {
		errMsg = task.GetErr().Error()
	}

	// 处理进度值
	progress := task.GetProgress()
	if math.IsNaN(progress) {
		progress = 100
	}

	// 获取创建者信息
	creatorName := ""
	creatorRole := -1
	if task.GetCreator() != nil {
		creatorName = task.GetCreator().Username
		creatorRole = task.GetCreator().Role
	}

	return TaskInfo{
		ID:          task.GetID(),
		Name:        task.GetName(),
		Creator:     creatorName,
		CreatorRole: creatorRole,
		State:       task.GetState(),
		Status:      task.GetStatus(),
		Progress:    progress,
		StartTime:   task.GetStartTime(),
		EndTime:     task.GetEndTime(),
		TotalBytes:  task.GetTotalBytes(),
		Error:       errMsg,
	}
}

// getTaskInfos 批量获取任务信息
func getTaskInfos[T task2.TaskExtensionInfo](tasks []T) []TaskInfo {
	return utils.MustSliceConvert(tasks, getTaskInfo[T])
}

// argsContains 检查值是否在切片中
func argsContains[T comparable](v T, slice ...T) bool {
	return utils.SliceContains(slice, v)
}

// getUserInfo 获取当前用户信息
// 返回值: isAdmin, userID, isValid
func getUserInfo(c *gin.Context) (bool, uint, bool) {
	user, ok := c.Value("user").(*model.User)
	if !ok {
		return false, 0, false
	}
	return user.IsAdmin(), user.ID, true
}

// getTargetedHandler 获取针对单个任务的处理函数
func getTargetedHandler[T task2.TaskExtensionInfo](manager task2.Manager[T], callback func(c *gin.Context, task T)) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取用户信息
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user authentication failed", 401)
			return
		}

		// 获取任务ID
		taskID := c.Query("tid")
		if taskID == "" {
			common.ErrorStrResp(c, "task ID is required", 400)
			return
		}

		// 获取任务对象
		task, ok := manager.GetByID(taskID)
		if !ok {
			common.ErrorStrResp(c, "task not found", 404)
			return
		}

		// 验证权限
		if !isAdmin && (task.GetCreator() == nil || uid != task.GetCreator().ID) {
			// 为避免攻击者通过错误消息猜测有效的任务ID，返回404而不是403
			common.ErrorStrResp(c, "task not found", 404)
			return
		}

		// 执行回调
		callback(c, task)
	}
}

// BatchTaskRequest 批量任务请求
type BatchTaskRequest []string

// getBatchHandler 获取批量任务处理函数
func getBatchHandler[T task2.TaskExtensionInfo](manager task2.Manager[T], callback func(task T)) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取用户信息
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user authentication failed", 401)
			return
		}

		// 获取任务ID列表
		var taskIDs BatchTaskRequest
		if err := c.ShouldBind(&taskIDs); err != nil {
			common.ErrorStrResp(c, "invalid request format", 400)
			return
		}

		if len(taskIDs) == 0 {
			common.ErrorStrResp(c, "no task IDs provided", 400)
			return
		}

		// 处理每个任务
		errors := make(map[string]string)
		for _, taskID := range taskIDs {
			task, ok := manager.GetByID(taskID)
			if !ok || (!isAdmin && (task.GetCreator() == nil || uid != task.GetCreator().ID)) {
				errors[taskID] = "task not found"
				continue
			}
			callback(task)
		}

		common.SuccessResp(c, errors)
	}
}

// taskRoute 设置任务相关路由
func taskRoute[T task2.TaskExtensionInfo](g *gin.RouterGroup, manager task2.Manager[T]) {
	// 获取未完成任务列表
	g.GET("/undone", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user authentication failed", 401)
			return
		}

		// 获取未完成任务
		tasks := manager.GetByCondition(func(task T) bool {
			// 避免直接传递用户对象到函数中以减少闭包大小
			return (isAdmin || (task.GetCreator() != nil && uid == task.GetCreator().ID)) &&
				argsContains(task.GetState(),
					tache.StatePending,
					tache.StateRunning,
					tache.StateCanceling,
					tache.StateErrored,
					tache.StateFailing,
					tache.StateWaitingRetry,
					tache.StateBeforeRetry)
		})

		common.SuccessResp(c, getTaskInfos(tasks))
	})

	// 获取已完成任务列表
	g.GET("/done", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user authentication failed", 401)
			return
		}

		// 获取已完成任务
		tasks := manager.GetByCondition(func(task T) bool {
			return (isAdmin || (task.GetCreator() != nil && uid == task.GetCreator().ID)) &&
				argsContains(task.GetState(),
					tache.StateCanceled,
					tache.StateFailed,
					tache.StateSucceeded)
		})

		common.SuccessResp(c, getTaskInfos(tasks))
	})

	// 获取任务信息
	g.POST("/info", getTargetedHandler(manager, func(c *gin.Context, task T) {
		common.SuccessResp(c, getTaskInfo(task))
	}))

	// 取消任务
	g.POST("/cancel", getTargetedHandler(manager, func(c *gin.Context, task T) {
		manager.Cancel(task.GetID())
		common.SuccessResp(c)
	}))

	// 删除任务
	g.POST("/delete", getTargetedHandler(manager, func(c *gin.Context, task T) {
		manager.Remove(task.GetID())
		common.SuccessResp(c)
	}))

	// 重试任务
	g.POST("/retry", getTargetedHandler(manager, func(c *gin.Context, task T) {
		manager.Retry(task.GetID())
		common.SuccessResp(c)
	}))

	// 批量取消任务
	g.POST("/cancel_some", getBatchHandler(manager, func(task T) {
		manager.Cancel(task.GetID())
	}))

	// 批量删除任务
	g.POST("/delete_some", getBatchHandler(manager, func(task T) {
		manager.Remove(task.GetID())
	}))

	// 批量重试任务
	g.POST("/retry_some", getBatchHandler(manager, func(task T) {
		manager.Retry(task.GetID())
	}))

	// 清除所有已完成任务
	g.POST("/clear_done", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user authentication failed", 401)
			return
		}

		manager.RemoveByCondition(func(task T) bool {
			return (isAdmin || (task.GetCreator() != nil && uid == task.GetCreator().ID)) &&
				argsContains(task.GetState(),
					tache.StateCanceled,
					tache.StateFailed,
					tache.StateSucceeded)
		})

		common.SuccessResp(c)
	})

	// 清除所有成功任务
	g.POST("/clear_succeeded", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user authentication failed", 401)
			return
		}

		manager.RemoveByCondition(func(task T) bool {
			return (isAdmin || (task.GetCreator() != nil && uid == task.GetCreator().ID)) &&
				task.GetState() == tache.StateSucceeded
		})

		common.SuccessResp(c)
	})

	// 重试所有失败任务
	g.POST("/retry_failed", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user authentication failed", 401)
			return
		}

		tasks := manager.GetByCondition(func(task T) bool {
			return (isAdmin || (task.GetCreator() != nil && uid == task.GetCreator().ID)) &&
				task.GetState() == tache.StateFailed
		})

		for _, task := range tasks {
			manager.Retry(task.GetID())
		}

		common.SuccessResp(c)
	})
}

// SetupTaskRoute 设置所有任务相关路由
func SetupTaskRoute(g *gin.RouterGroup) {
	// 上传任务
	taskRoute(g.Group("/upload"), fs.UploadTaskManager)
	// 复制任务
	taskRoute(g.Group("/copy"), fs.CopyTaskManager)
	// 移动任务
	taskRoute(g.Group("/move"), fs.MoveTaskManager)
	// 离线下载任务
	taskRoute(g.Group("/offline_download"), tool.DownloadTaskManager)
	// 离线下载传输任务
	taskRoute(g.Group("/offline_download_transfer"), tool.TransferTaskManager)
	// 解压任务
	taskRoute(g.Group("/decompress"), fs.ArchiveDownloadTaskManager)
	// 解压上传任务
	taskRoute(g.Group("/decompress_upload"), fs.ArchiveContentUploadTaskManager)
}