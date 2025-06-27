package initialize

import (
	"github.com/xhofe/tache"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/db"
	"github.com/dongdio/OpenList/internal/fs"
	"github.com/dongdio/OpenList/internal/offline_download/tool"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/internal/setting"
)

func taskFilterNegative(num int) int64 {
	if num < 0 {
		num = 0
	}
	return int64(num)
}

func initTaskManager() {
	fs.UploadTaskManager = tache.NewManager[*fs.UploadTask](tache.WithWorks(setting.GetInt(consts.TaskUploadThreadsNum, conf.Conf.Tasks.Upload.Workers)), tache.WithMaxRetry(conf.Conf.Tasks.Upload.MaxRetry)) // upload will not support persist
	op.RegisterSettingChangingCallback(func() {
		fs.UploadTaskManager.SetWorkersNumActive(taskFilterNegative(setting.GetInt(consts.TaskUploadThreadsNum, conf.Conf.Tasks.Upload.Workers)))
	})

	fs.CopyTaskManager = tache.NewManager[*fs.CopyTask](tache.WithWorks(setting.GetInt(consts.TaskCopyThreadsNum, conf.Conf.Tasks.Copy.Workers)), tache.WithPersistFunction(db.GetTaskDataFunc("copy", conf.Conf.Tasks.Copy.TaskPersistant), db.UpdateTaskDataFunc("copy", conf.Conf.Tasks.Copy.TaskPersistant)), tache.WithMaxRetry(conf.Conf.Tasks.Copy.MaxRetry))
	op.RegisterSettingChangingCallback(func() {
		fs.CopyTaskManager.SetWorkersNumActive(taskFilterNegative(setting.GetInt(consts.TaskCopyThreadsNum, conf.Conf.Tasks.Copy.Workers)))
	})

	fs.MoveTaskManager = tache.NewManager[*fs.MoveTask](tache.WithWorks(setting.GetInt(consts.TaskMoveThreadsNum, conf.Conf.Tasks.Move.Workers)), tache.WithPersistFunction(db.GetTaskDataFunc("move", conf.Conf.Tasks.Move.TaskPersistant), db.UpdateTaskDataFunc("move", conf.Conf.Tasks.Move.TaskPersistant)), tache.WithMaxRetry(conf.Conf.Tasks.Move.MaxRetry))
	op.RegisterSettingChangingCallback(func() {
		fs.MoveTaskManager.SetWorkersNumActive(taskFilterNegative(setting.GetInt(consts.TaskMoveThreadsNum, conf.Conf.Tasks.Move.Workers)))
	})

	tool.DownloadTaskManager = tache.NewManager[*tool.DownloadTask](tache.WithWorks(setting.GetInt(consts.TaskOfflineDownloadThreadsNum, conf.Conf.Tasks.Download.Workers)), tache.WithPersistFunction(db.GetTaskDataFunc("download", conf.Conf.Tasks.Download.TaskPersistant), db.UpdateTaskDataFunc("download", conf.Conf.Tasks.Download.TaskPersistant)), tache.WithMaxRetry(conf.Conf.Tasks.Download.MaxRetry))
	op.RegisterSettingChangingCallback(func() {
		tool.DownloadTaskManager.SetWorkersNumActive(taskFilterNegative(setting.GetInt(consts.TaskOfflineDownloadThreadsNum, conf.Conf.Tasks.Download.Workers)))
	})

	tool.TransferTaskManager = tache.NewManager[*tool.TransferTask](tache.WithWorks(setting.GetInt(consts.TaskOfflineDownloadTransferThreadsNum, conf.Conf.Tasks.Transfer.Workers)), tache.WithPersistFunction(db.GetTaskDataFunc("transfer", conf.Conf.Tasks.Transfer.TaskPersistant), db.UpdateTaskDataFunc("transfer", conf.Conf.Tasks.Transfer.TaskPersistant)), tache.WithMaxRetry(conf.Conf.Tasks.Transfer.MaxRetry))
	op.RegisterSettingChangingCallback(func() {
		tool.TransferTaskManager.SetWorkersNumActive(taskFilterNegative(setting.GetInt(consts.TaskOfflineDownloadTransferThreadsNum, conf.Conf.Tasks.Transfer.Workers)))
	})
	if len(tool.TransferTaskManager.GetAll()) == 0 { // prevent offline downloaded files from being deleted
		CleanTempDir()
	}

	fs.ArchiveDownloadTaskManager = tache.NewManager[*fs.ArchiveDownloadTask](tache.WithWorks(setting.GetInt(consts.TaskDecompressDownloadThreadsNum, conf.Conf.Tasks.Decompress.Workers)), tache.WithPersistFunction(db.GetTaskDataFunc("decompress", conf.Conf.Tasks.Decompress.TaskPersistant), db.UpdateTaskDataFunc("decompress", conf.Conf.Tasks.Decompress.TaskPersistant)), tache.WithMaxRetry(conf.Conf.Tasks.Decompress.MaxRetry))
	op.RegisterSettingChangingCallback(func() {
		fs.ArchiveDownloadTaskManager.SetWorkersNumActive(taskFilterNegative(setting.GetInt(consts.TaskDecompressDownloadThreadsNum, conf.Conf.Tasks.Decompress.Workers)))
	})

	fs.ArchiveContentUploadTaskManager.Manager = tache.NewManager[*fs.ArchiveContentUploadTask](tache.WithWorks(setting.GetInt(consts.TaskDecompressUploadThreadsNum, conf.Conf.Tasks.DecompressUpload.Workers)), tache.WithMaxRetry(conf.Conf.Tasks.DecompressUpload.MaxRetry)) // decompress upload will not support persist
	op.RegisterSettingChangingCallback(func() {
		fs.ArchiveContentUploadTaskManager.SetWorkersNumActive(taskFilterNegative(setting.GetInt(consts.TaskDecompressUploadThreadsNum, conf.Conf.Tasks.DecompressUpload.Workers)))
	})
}