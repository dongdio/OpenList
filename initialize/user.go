package initialize

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/utility/utils"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

func initUser() {
	admin, err := op.GetAdmin()
	adminPassword := random.String(8)
	envpass := os.Getenv("OPENLIST_ADMIN_PASSWORD")
	if global.Dev {
		adminPassword = "admin"
	} else if len(envpass) > 0 {
		adminPassword = envpass
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			salt := random.String(16)
			admin = &model.User{
				Username: "admin",
				Salt:     salt,
				PwdHash:  model.TwoHashPwd(adminPassword, salt),
				Role:     model.ADMIN,
				BasePath: "/",
				Authn:    "[]",
				// 0(can see hidden) - 7(can remove) & 12(can read archives) - 13(can decompress archives)
				Permission: 0x31FF,
			}
			if err = op.CreateUser(admin); err != nil {
				panic(err)
			} else {
				fmt.Printf("Successfully created the admin user and the initial password is: %s\n", adminPassword)
				// fmt.Printf("\033[36mINFO\033[39m[%s] Successfully created the admin user and the initial password is: %s\n", time.Now().Format("2006-01-02 15:04:05"), adminPassword)
			}
		} else {
			utils.Log.Fatalf("[init user] Failed to get admin user: %v", err)
		}
	}
	_, err = op.GetGuest()
	if err == nil {
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		utils.Log.Fatalf("[init user] Failed to get guest user: %v", err)
		os.Exit(1)
	}

	salt := random.String(16)
	guest := &model.User{
		Username:   "guest",
		PwdHash:    model.TwoHashPwd("guest", salt),
		Salt:       salt,
		Role:       model.GUEST,
		BasePath:   "/",
		Permission: 0,
		Disabled:   true,
		Authn:      "[]",
	}
	if err = db.CreateUser(guest); err != nil {
		utils.Log.Fatalf("[init user] Failed to create guest user: %v", err)
	}
}