package aliyundrive

import (
	"crypto/ecdsa"

	"github.com/dongdio/OpenList/utility/generic"
)

type State struct {
	deviceID   string
	signature  string
	retry      int
	privateKey *ecdsa.PrivateKey
}

var global = generic.MapOf[string, *State]{}