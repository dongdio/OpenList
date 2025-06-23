package initialize

import (
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/internal/search"
)

func initIndex() {
	progress, err := search.Progress()
	if err != nil {
		log.Errorf("init index error: %+v", err)
		return
	}
	if !progress.IsDone {
		progress.IsDone = true
		search.WriteProgress(progress)
	}
}