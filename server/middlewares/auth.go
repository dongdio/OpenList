package middlewares

import (
	"crypto/subtle"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/server/common"
)

// Auth 中间件，检查用户是否已登录
// 如果token为空，则将用户设置为访客
// 认证成功后，将用户对象设置到上下文中
func Auth(c *gin.Context) {
	token := c.GetHeader("Authorization")

	// 检查是否使用管理员令牌
	if subtle.ConstantTimeCompare([]byte(token), []byte(setting.GetStr(consts.Token))) == 1 {
		admin, err := op.GetAdmin()
		if err != nil {
			common.ErrorResp(c, err, 500)
			c.Abort()
			return
		}
		c.Set("user", admin)
		log.Debugf("使用管理员令牌: %+v", admin)
		c.Next()
		return
	}

	// 处理空令牌情况（访客）
	if token == "" {
		guest, err := op.GetGuest()
		if err != nil {
			common.ErrorResp(c, err, 500)
			c.Abort()
			return
		}
		if guest.Disabled {
			common.ErrorStrResp(c, "访客用户已禁用，请登录", 401)
			c.Abort()
			return
		}
		c.Set("user", guest)
		log.Debugf("使用访客: %+v", guest)
		c.Next()
		return
	}

	// 验证JWT令牌
	userClaims, err := common.ParseToken(token)
	if err != nil {
		common.ErrorResp(c, err, 401)
		c.Abort()
		return
	}

	// 获取用户信息并验证
	user, err := op.GetUserByName(userClaims.Username)
	if err != nil {
		common.ErrorResp(c, err, 401)
		c.Abort()
		return
	}

	// 验证密码时间戳（检测密码是否已更改）
	if userClaims.PwdTS != user.PwdTS {
		common.ErrorStrResp(c, "密码已更改，请重新登录", 401)
		c.Abort()
		return
	}

	// 检查用户是否已禁用
	if user.Disabled {
		common.ErrorStrResp(c, "当前用户已禁用", 401)
		c.Abort()
		return
	}

	c.Set("user", user)
	log.Debugf("使用登录令牌: %+v", user)
	c.Next()
}

// Authn 中间件，与Auth类似但不检查用户是否禁用
// 用于需要认证但允许禁用用户的场景
func Authn(c *gin.Context) {
	token := c.GetHeader("Authorization")

	// 检查是否使用管理员令牌
	if subtle.ConstantTimeCompare([]byte(token), []byte(setting.GetStr(consts.Token))) == 1 {
		admin, err := op.GetAdmin()
		if err != nil {
			common.ErrorResp(c, err, 500)
			c.Abort()
			return
		}
		c.Set("user", admin)
		log.Debugf("使用管理员令牌: %+v", admin)
		c.Next()
		return
	}

	// 处理空令牌情况（访客）
	if token == "" {
		guest, err := op.GetGuest()
		if err != nil {
			common.ErrorResp(c, err, 500)
			c.Abort()
			return
		}
		c.Set("user", guest)
		log.Debugf("使用访客: %+v", guest)
		c.Next()
		return
	}

	// 验证JWT令牌
	userClaims, err := common.ParseToken(token)
	if err != nil {
		common.ErrorResp(c, err, 401)
		c.Abort()
		return
	}

	// 获取用户信息并验证
	user, err := op.GetUserByName(userClaims.Username)
	if err != nil {
		common.ErrorResp(c, err, 401)
		c.Abort()
		return
	}

	// 验证密码时间戳（检测密码是否已更改）
	if userClaims.PwdTS != user.PwdTS {
		common.ErrorStrResp(c, "密码已更改，请重新登录", 401)
		c.Abort()
		return
	}

	// 检查用户是否已禁用
	if user.Disabled {
		common.ErrorStrResp(c, "当前用户已禁用", 401)
		c.Abort()
		return
	}

	c.Set("user", user)
	log.Debugf("使用登录令牌: %+v", user)
	c.Next()
}

// AuthNotGuest 中间件，确保用户不是访客
// 需要在Auth或Authn中间件之后使用
func AuthNotGuest(c *gin.Context) {
	user, ok := c.Get("user")
	if !ok {
		common.ErrorStrResp(c, "用户未认证", 401)
		c.Abort()
		return
	}

	userObj, ok := user.(*model.User)
	if !ok {
		common.ErrorStrResp(c, "用户数据类型错误", 500)
		c.Abort()
		return
	}

	if userObj.IsGuest() {
		common.ErrorStrResp(c, "您是访客用户，没有权限执行此操作", 403)
		c.Abort()
		return
	}

	c.Next()
}

// AuthAdmin 中间件，确保用户是管理员
// 需要在Auth或Authn中间件之后使用
func AuthAdmin(c *gin.Context) {
	user, ok := c.Get("user")
	if !ok {
		common.ErrorStrResp(c, "用户未认证", 401)
		c.Abort()
		return
	}

	userObj, ok := user.(*model.User)
	if !ok {
		common.ErrorStrResp(c, "用户数据类型错误", 500)
		c.Abort()
		return
	}

	if !userObj.IsAdmin() {
		common.ErrorStrResp(c, "您不是管理员，没有权限执行此操作", 403)
		c.Abort()
		return
	}

	c.Next()
}