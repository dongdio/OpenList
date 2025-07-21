package db

import (
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/model"
)

func GetTaskDataByType(taskType string) (*model.TaskItem, error) {
	task := model.TaskItem{Key: taskType}
	if err := db.Where(task).First(&task).Error; err != nil {
		return nil, errors.Wrapf(err, "failed find task")
	}
	return &task, nil
}

func UpdateTaskData(t *model.TaskItem) error {
	return errors.WithStack(db.Model(&model.TaskItem{}).Where("key = ?", t.Key).Update("persist_data", t.PersistData).Error)
}

func CreateTaskData(t *model.TaskItem) error {
	return errors.WithStack(db.Create(t).Error)
}

func GetTaskDataFunc(taskType string, enabled bool) func() ([]byte, error) {
	if !enabled {
		return nil
	}
	task, err := GetTaskDataByType(taskType)
	if err != nil {
		return nil
	}
	return func() ([]byte, error) {
		return []byte(task.PersistData), nil
	}
}

func UpdateTaskDataFunc(taskType string, enabled bool) func([]byte) error {
	if !enabled {
		return nil
	}
	return func(data []byte) error {
		s := string(data)
		if s == "null" || s == "" {
			s = "[]"
		}
		return UpdateTaskData(&model.TaskItem{Key: taskType, PersistData: s})
	}
}
