package aliyundrive

import (
	"crypto/ecdsa"

	"github.com/dongdio/OpenList/v4/utility/generic"
)

// State 存储用户状态信息
type State struct {
	deviceID   string            // 设备ID
	signature  string            // 签名
	retry      int               // 重试次数
	privateKey *ecdsa.PrivateKey // 私钥
}

// userStates 存储所有用户的状态信息，键为用户ID
var userStates = generic.MapOf[string, *State]{}

// 为了向后兼容，保留原变量名
var global = userStates
