package search

import (
	_ "github.com/dongdio/OpenList/pkg/search/bleve"
	_ "github.com/dongdio/OpenList/pkg/search/db"
	_ "github.com/dongdio/OpenList/pkg/search/db_non_full_text"
	_ "github.com/dongdio/OpenList/pkg/search/meilisearch"
)