package handles

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/authn"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// WebAuthn 相关常量
const (
	// 会话数据头名称
	HeaderSessionData = "Session"
	// 用户名查询参数
	QueryUsername = "username"
	// 错误消息
	ErrWebAuthnNotEnabled = "WebAuthn 未启用"
	// 成功消息
	MsgRegisteredSuccess = "注册成功"
	MsgDeletedSuccess    = "删除成功"
)

// DeleteAuthnRequest WebAuthn 凭证删除请求
type DeleteAuthnRequest struct {
	ID string `json:"id" binding:"required"` // 凭证ID
}

// WebAuthnCredential WebAuthn 凭证信息
type WebAuthnCredential struct {
	ID          []byte `json:"id"`          // 凭证ID
	FingerPrint string `json:"fingerprint"` // 指纹（AAGUID的十六进制表示）
}

// BeginAuthnLogin 开始 WebAuthn 登录流程
// 支持两种模式：
// 1. 用户名登录：通过查询参数提供用户名
// 2. 可发现凭证登录：不提供用户名，由客户端选择凭证
func BeginAuthnLogin(c *gin.Context) {
	// 检查 WebAuthn 是否启用
	if !setting.GetBool(consts.WebauthnLoginEnabled) {
		common.ErrorStrResp(c, ErrWebAuthnNotEnabled, 403)
		return
	}

	// 创建 WebAuthn 实例
	authnInstance, err := authn.NewAuthnInstance(c)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "创建 WebAuthn 实例失败"), 400)
		return
	}

	var (
		options     *protocol.CredentialAssertion
		sessionData *webauthn.SessionData
	)

	// 根据是否提供用户名选择登录模式
	username := c.Query(QueryUsername)
	if username != "" {
		// 用户名登录模式
		user, err := db.GetUserByName(username)
		if err != nil {
			common.ErrorResp(c, errs.Wrap(err, "failed to get user info"), 400)
			return
		}
		options, sessionData, err = authnInstance.BeginLogin(user)
	} else {
		// 可发现凭证登录模式
		options, sessionData, err = authnInstance.BeginDiscoverableLogin()
	}

	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to begin login"), 400)
		return
	}

	// 序列化会话数据
	sessionBytes, err := utils.JSONTool.Marshal(sessionData)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to marshal session data"), 400)
		return
	}

	// 返回登录选项和会话数据
	common.SuccessResp(c, gin.H{
		"options": options,
		"session": sessionBytes,
	})
}

// FinishAuthnLogin 完成 WebAuthn 登录流程
// 验证客户端提供的凭证，成功后生成登录令牌
func FinishAuthnLogin(c *gin.Context) {
	// 检查 WebAuthn 是否启用
	if !setting.GetBool(consts.WebauthnLoginEnabled) {
		common.ErrorStrResp(c, ErrWebAuthnNotEnabled, 403)
		return
	}

	// 创建 WebAuthn 实例
	authnInstance, err := authn.NewAuthnInstance(c)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to create authn instance"), 400)
		return
	}

	// 获取并解析会话数据
	sessionDataString := c.GetHeader(HeaderSessionData)
	if sessionDataString == "" {
		common.ErrorStrResp(c, "session data is missing", 400)
		return
	}

	sessionDataBytes, err := base64.StdEncoding.DecodeString(sessionDataString)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to decode session data"), 400)
		return
	}

	var sessionData webauthn.SessionData
	if err = utils.JSONTool.Unmarshal(sessionDataBytes, &sessionData); err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to unmarshal session data"), 400)
		return
	}

	var user *model.User
	username := c.Query(QueryUsername)
	if username != "" {
		// 用户名登录模式
		user, err = db.GetUserByName(username)
		if err != nil {
			common.ErrorResp(c, errs.Wrap(err, "failed to get user info"), 400)
			return
		}
		_, err = authnInstance.FinishLogin(user, sessionData, c.Request)
	} else {
		// 可发现凭证登录模式
		_, err = authnInstance.FinishDiscoverableLogin(func(_, userHandle []byte) (webauthn.User, error) {
			// userHandle 参数等同于 (User).WebAuthnID()
			userID := uint(binary.LittleEndian.Uint64(userHandle))
			user, err = db.GetUserByID(userID)
			if err != nil {
				return nil, errs.Wrap(err, "failed to get user by id")
			}
			return user, nil
		}, sessionData, c.Request)
	}

	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to finish login"), 400)
		return
	}

	// 生成登录令牌
	token, err := common.GenerateToken(user)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to generate token"), 400)
		return
	}

	common.SuccessResp(c, gin.H{"token": token})
}

// BeginAuthnRegistration 开始 WebAuthn 注册流程
// 为当前用户生成注册选项
func BeginAuthnRegistration(c *gin.Context) {
	// 检查 WebAuthn 是否启用
	if !setting.GetBool(consts.WebauthnLoginEnabled) {
		common.ErrorStrResp(c, ErrWebAuthnNotEnabled, 403)
		return
	}

	// 获取当前用户
	user := c.Value(consts.UserKey).(*model.User)

	// 创建 WebAuthn 实例
	authnInstance, err := authn.NewAuthnInstance(c)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to create authn instance"), 400)
		return
	}

	// 开始注册流程
	options, sessionData, err := authnInstance.BeginRegistration(user)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to begin registration"), 400)
		return
	}

	// 序列化会话数据
	sessionBytes, err := utils.JSONTool.Marshal(sessionData)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to marshal session data"), 400)
		return
	}

	// 返回注册选项和会话数据
	common.SuccessResp(c, gin.H{
		"options": options,
		"session": sessionBytes,
	})
}

// FinishAuthnRegistration 完成 WebAuthn 注册流程
// 验证并保存客户端提供的凭证
func FinishAuthnRegistration(c *gin.Context) {
	// 检查 WebAuthn 是否启用
	if !setting.GetBool(consts.WebauthnLoginEnabled) {
		common.ErrorStrResp(c, ErrWebAuthnNotEnabled, 403)
		return
	}

	// 获取当前用户
	user := c.Value(consts.UserKey).(*model.User)

	// 获取会话数据
	sessionDataString := c.GetHeader(HeaderSessionData)
	if sessionDataString == "" {
		common.ErrorStrResp(c, "缺少会话数据", 400)
		return
	}

	// 创建 WebAuthn 实例
	authnInstance, err := authn.NewAuthnInstance(c)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to create authn instance"), 400)
		return
	}

	// 解码会话数据
	sessionDataBytes, err := base64.StdEncoding.DecodeString(sessionDataString)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to decode session data"), 400)
		return
	}

	// 解析会话数据
	var sessionData webauthn.SessionData
	if err = utils.JSONTool.Unmarshal(sessionDataBytes, &sessionData); err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to unmarshal session data"), 400)
		return
	}

	// 完成注册流程
	credential, err := authnInstance.FinishRegistration(user, sessionData, c.Request)
	if err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to finish registration"), 400)
		return
	}

	// 保存凭证
	if err = db.RegisterAuthn(user, credential); err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to register authn"), 400)
		return
	}

	// 清除用户缓存
	if err = op.DelUserCache(user.Username); err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to delete user cache"), 400)
		return
	}

	common.SuccessResp(c, MsgRegisteredSuccess)
}

// DeleteAuthnLogin 删除 WebAuthn 登录凭证
func DeleteAuthnLogin(c *gin.Context) {
	// 获取当前用户
	user := c.Value(consts.UserKey).(*model.User)

	// 解析请求
	var req DeleteAuthnRequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to bind request"), 400)
		return
	}

	// 检查ID是否为空
	if req.ID == "" {
		common.ErrorStrResp(c, "credential id is empty", 400)
		return
	}

	// 删除凭证
	if err := db.RemoveAuthn(user, req.ID); err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to remove authn"), 400)
		return
	}

	// 清除用户缓存
	if err := op.DelUserCache(user.Username); err != nil {
		common.ErrorResp(c, errs.Wrap(err, "failed to delete user cache"), 400)
		return
	}

	common.SuccessResp(c, MsgDeletedSuccess)
}

// GetAuthnCredentials 获取用户的 WebAuthn 凭证列表
func GetAuthnCredentials(c *gin.Context) {
	// 获取当前用户
	user := c.Value(consts.UserKey).(*model.User)

	// 获取用户凭证
	credentials := user.WebAuthnCredentials()

	// 预分配结果切片容量
	result := make([]WebAuthnCredential, 0, len(credentials))

	// 转换凭证格式
	for _, credential := range credentials {
		result = append(result, WebAuthnCredential{
			ID:          credential.ID,
			FingerPrint: fmt.Sprintf("% X", credential.Authenticator.AAGUID),
		})
	}

	common.SuccessResp(c, result)
}