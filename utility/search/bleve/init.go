package bleve

import (
	"github.com/blevesearch/bleve/v2"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/utility/errs"
	searcher2 "github.com/dongdio/OpenList/v4/utility/search/searcher"
)

var config = searcher2.Config{
	Name: "bleve",
}

func Init(indexPath *string) (bleve.Index, error) {
	log.Debugf("bleve path: %s", *indexPath)
	fileIndex, err := bleve.Open(*indexPath)
	if errs.Is(err, bleve.ErrorIndexPathDoesNotExist) {
		log.Infof("Creating new index...")
		indexMapping := bleve.NewIndexMapping()
		searchNodeMapping := bleve.NewDocumentMapping()
		searchNodeMapping.AddFieldMappingsAt("is_dir", bleve.NewBooleanFieldMapping())
		// TODO: appoint analyzer
		parentFieldMapping := bleve.NewTextFieldMapping()
		searchNodeMapping.AddFieldMappingsAt("parent", parentFieldMapping)
		// TODO: appoint analyzer
		nameFieldMapping := bleve.NewKeywordFieldMapping()
		searchNodeMapping.AddFieldMappingsAt("name", nameFieldMapping)
		indexMapping.AddDocumentMapping("SearchNode", searchNodeMapping)
		fileIndex, err = bleve.New(*indexPath, indexMapping)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return fileIndex, nil
}

func init() {
	searcher2.RegisterSearcher(config, func() (searcher2.Searcher, error) {
		b, err := Init(&conf.Conf.BleveDir)
		if err != nil {
			return nil, err
		}
		return &Bleve{BIndex: b}, nil
	})
}