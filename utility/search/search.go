package search

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/utility/errs"
	searcher2 "github.com/dongdio/OpenList/utility/search/searcher"
)

var instance searcher2.Searcher = nil

// initSearcher or reset index
func initSearcher(mode string) error {
	if instance != nil {
		// unchanged, do nothing
		if instance.Config().Name == mode {
			return nil
		}
		err := instance.Release(context.Background())
		if err != nil {
			log.Errorf("release instance err: %+v", err)
		}
		instance = nil
	}
	if Running() {
		return fmt.Errorf("index is running")
	}
	if mode == "none" {
		log.Warnf("not enable search")
		return nil
	}
	s, ok := searcher2.NewMap[mode]
	if !ok {
		return fmt.Errorf("not support index: %s", mode)
	}
	i, err := s()
	if err != nil {
		log.Errorf("init searcher error: %+v", err)
	} else {
		instance = i
	}
	return err
}

func Search(ctx context.Context, req model.SearchReq) ([]model.SearchNode, int64, error) {
	return instance.Search(ctx, req)
}

func indexSearch(ctx context.Context, parent string, obj model.Obj) error {
	if instance == nil {
		return errs.SearchNotAvailable
	}
	return instance.Index(ctx, model.SearchNode{
		Parent: parent,
		Name:   obj.GetName(),
		IsDir:  obj.IsDir(),
		Size:   obj.GetSize(),
	})
}

type ObjWithParent struct {
	Parent string
	model.Obj
}

func batchIndex(ctx context.Context, objs []ObjWithParent) error {
	if instance == nil {
		return errs.SearchNotAvailable
	}
	if len(objs) == 0 {
		return nil
	}
	var searchNodes []model.SearchNode
	for i := range objs {
		searchNodes = append(searchNodes, model.SearchNode{
			Parent: objs[i].Parent,
			Name:   objs[i].GetName(),
			IsDir:  objs[i].IsDir(),
			Size:   objs[i].GetSize(),
		})
	}
	return instance.BatchIndex(ctx, searchNodes)
}

func init() {
	op.RegisterSettingItemHook(consts.SearchIndex, func(item *model.SettingItem) error {
		log.Debugf("searcher init, mode: %s", item.Value)
		return initSearcher(item.Value)
	})
}