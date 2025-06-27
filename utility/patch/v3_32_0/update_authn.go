package v3_32_0

import (
	"github.com/dongdio/OpenList/internal/db"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/utility/utils"
)

// UpdateAuthnForOldVersion updates users' authn
// First published: bdfc159 fix: webauthn logspam (#6181) by itsHenry
func UpdateAuthnForOldVersion() {
	users, _, err := op.GetUsers(1, -1)
	if err != nil {
		utils.Log.Fatalf("[update authn for old version] failed get users: %v", err)
	}
	for i := range users {
		user := users[i]
		if user.Authn == "" {
			user.Authn = "[]"
			if err = db.UpdateUser(&user); err != nil {
				utils.Log.Fatalf("[update authn for old version] failed update user: %v", err)
			}
		}
	}
}