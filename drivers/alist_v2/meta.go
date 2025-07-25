package alist_v2

import (
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/op"
)

type Addition struct {
	driver.RootPath
	Address     string `json:"url" required:"true"`
	Password    string `json:"password"`
	AccessToken string `json:"access_token"`
}

var config = driver.Config{
	Name:        "AList V2",
	LocalSort:   true,
	NoUpload:    true,
	DefaultRoot: "/",
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return new(AListV2)
	})
}