package _123Link

import (
	"context"
	stdpath "path"
	"time"

	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/errs"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type Pan123Link struct {
	model.Storage
	Addition
	root *Node
}

func (d *Pan123Link) Config() driver.Config {
	return config
}

func (d *Pan123Link) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Pan123Link) Init(ctx context.Context) error {
	node, err := BuildTree(d.OriginURLs)
	if err != nil {
		return err
	}
	node.calSize()
	d.root = node
	return nil
}

func (d *Pan123Link) Drop(ctx context.Context) error {
	return nil
}

func (d *Pan123Link) Get(ctx context.Context, path string) (model.Obj, error) {
	node := GetNodeFromRootByPath(d.root, path)
	return nodeToObj(node, path)
}

func (d *Pan123Link) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	node := GetNodeFromRootByPath(d.root, dir.GetPath())
	if node == nil {
		return nil, errs.ObjectNotFound
	}
	if node.isFile() {
		return nil, errs.NotFolder
	}
	return utils.SliceConvert(node.Children, func(node *Node) (model.Obj, error) {
		return nodeToObj(node, stdpath.Join(dir.GetPath(), node.Name))
	})
}

func (d *Pan123Link) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	node := GetNodeFromRootByPath(d.root, file.GetPath())
	if node == nil {
		return nil, errs.ObjectNotFound
	}
	if node.isFile() {
		signUrl, err := SignURL(node.Url, d.PrivateKey, d.UID, time.Duration(d.ValidDuration)*time.Minute)
		if err != nil {
			return nil, err
		}
		return &model.Link{
			URL: signUrl,
		}, nil
	}
	return nil, errs.NotFile
}

var _ driver.Driver = (*Pan123Link)(nil)