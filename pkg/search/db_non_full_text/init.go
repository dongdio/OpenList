package db_non_full_text

import (
	searcher2 "github.com/dongdio/OpenList/pkg/search/searcher"
)

var config = searcher2.Config{
	Name:       "database_non_full_text",
	AutoUpdate: true,
}

func init() {
	searcher2.RegisterSearcher(config, func() (searcher2.Searcher, error) {
		return &DB{}, nil
	})
}