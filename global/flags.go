package global

import (
	"github.com/robfig/cron/v3"
)

var (
	DataDir     string
	Debug       bool
	NoPrefix    bool
	Dev         bool
	ForceBinDir bool
	LogStd      bool
)

var CronConfig *cron.Cron

var jobs = make(map[string]func())

func AddJob(t string, job func()) {
	jobs[t] = job
}

func GetJobs() map[string]func() {
	return jobs
}