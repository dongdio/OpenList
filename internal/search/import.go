package search

import (
	_ "github.com/dongdio/OpenList/internal/search/bleve"
	_ "github.com/dongdio/OpenList/internal/search/db"
	_ "github.com/dongdio/OpenList/internal/search/db_non_full_text"
	_ "github.com/dongdio/OpenList/internal/search/meilisearch"
)