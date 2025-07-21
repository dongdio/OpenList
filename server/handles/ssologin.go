package handles

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/OpenListTeam/go-cache"
	"github.com/coreos/go-oidc"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/db"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/utils"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

// SSO相关常量
const (
	// 状态字符串长度
	stateLength = 16
	// 状态过期时间
	stateExpire = time.Minute * 5
)

// 支持的SSO平台
const (
	PlatformGithub    = "Github"
	PlatformMicrosoft = "Microsoft"
	PlatformGoogle    = "Google"
	PlatformDingtalk  = "Dingtalk"
	PlatformCasdoor   = "Casdoor"
	PlatformOIDC      = "OIDC"
)

// SSO方法类型
const (
	MethodGetSSOID = "get_sso_id"
	MethodGetToken = "sso_get_token"
)

// 状态缓存，用于防止CSRF攻击
var stateCache = cache.NewMemCache[string](cache.WithShards[string](stateLength))

// HTTP客户端，用于SSO请求
var ssoClient = resty.New().
	SetRetryCount(3).
	SetTimeout(10*time.Second).
	SetHeader("Accept", "application/json")

// 生成状态缓存键
func generateStateKey(clientID, state string) string {
	return fmt.Sprintf("%s_%s", clientID, state)
}

// 生成随机状态并缓存
func generateState(clientID, ip string) string {
	state := random.String(stateLength)
	stateCache.Set(generateStateKey(clientID, state), ip, cache.WithEx[string](stateExpire))
	return state
}

// 验证状态是否有效
func verifyState(clientID, ip, state string) bool {
	if state == "" {
		return false
	}
	value, ok := stateCache.Get(generateStateKey(clientID, state))
	return ok && value == ip
}

// 构建SSO重定向URI
func ssoRedirectURI(c *gin.Context, useCompatibility bool, method string) string {
	if useCompatibility {
		return common.GetApiURL(c) + "/api/auth/" + method
	}
	return common.GetApiURL(c) + "/api/auth/sso_callback" + "?method=" + method
}

// SSOLoginRedirect 处理SSO登录重定向
// 根据不同的SSO平台，构建相应的授权URL并重定向
func SSOLoginRedirect(c *gin.Context) {
	// 获取请求参数和配置
	method := c.Query("method")
	if method == "" {
		common.ErrorStrResp(c, "no method provided", 400)
		return
	}

	// 检查SSO是否启用
	enabled := setting.GetBool(consts.SSOLoginEnabled)
	if !enabled {
		common.ErrorStrResp(c, "Single sign-on is not enabled", 403)
		return
	}

	// 获取SSO配置
	useCompatibility := setting.GetBool(consts.SSOCompatibilityMode)
	clientId := setting.GetStr(consts.SSOClientId)
	platform := setting.GetStr(consts.SSOLoginPlatform)

	// 构建重定向URL
	redirectUri := ssoRedirectURI(c, useCompatibility, method)

	// 构建URL参数
	urlValues := url.Values{}
	urlValues.Add("response_type", "code")
	urlValues.Add("redirect_uri", redirectUri)
	urlValues.Add("client_id", clientId)

	// 根据不同平台处理
	switch platform {
	case PlatformGithub:
		authURL := "https://github.com/login/oauth/authorize?"
		urlValues.Add("scope", "read:user")
		c.Redirect(http.StatusFound, authURL+urlValues.Encode())

	case PlatformMicrosoft:
		authURL := "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?"
		urlValues.Add("scope", "user.read")
		urlValues.Add("response_mode", "query")
		c.Redirect(http.StatusFound, authURL+urlValues.Encode())

	case PlatformGoogle:
		authURL := "https://accounts.google.com/o/oauth2/v2/auth?"
		urlValues.Add("scope", "https://www.googleapis.com/auth/userinfo.profile")
		c.Redirect(http.StatusFound, authURL+urlValues.Encode())

	case PlatformDingtalk:
		authURL := "https://login.dingtalk.com/oauth2/auth?"
		urlValues.Add("scope", "openid")
		urlValues.Add("prompt", "consent")
		urlValues.Add("response_type", "code")
		c.Redirect(http.StatusFound, authURL+urlValues.Encode())

	case PlatformCasdoor:
		endpoint := strings.TrimSuffix(setting.GetStr(consts.SSOEndpointName), "/")
		authURL := endpoint + "/login/oauth/authorize?"
		urlValues.Add("scope", "profile")
		urlValues.Add("state", endpoint)
		c.Redirect(http.StatusFound, authURL+urlValues.Encode())

	case PlatformOIDC:
		oauth2Config, err := GetOIDCClient(c, useCompatibility, redirectUri, method)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		state := generateState(clientId, c.ClientIP())
		c.Redirect(http.StatusFound, oauth2Config.AuthCodeURL(state))

	default:
		common.ErrorStrResp(c, "invalid platform: "+platform, 400)
	}
}

// GetOIDCClient 获取OIDC客户端配置
func GetOIDCClient(c *gin.Context, useCompatibility bool, redirectUri, method string) (*oauth2.Config, error) {
	// 如果未提供重定向URI，则构建一个
	if redirectUri == "" {
		redirectUri = ssoRedirectURI(c, useCompatibility, method)
	}

	// 获取OIDC配置
	endpoint := setting.GetStr(consts.SSOEndpointName)
	if endpoint == "" {
		return nil, errors.New("OIDC endpoint not configured")
	}

	// 创建OIDC提供者
	provider, err := oidc.NewProvider(c, endpoint)
	if err != nil {
		return nil, errors.Errorf("failed to create OIDC provider: %w", err)
	}

	// 获取客户端配置
	clientId := setting.GetStr(consts.SSOClientId)
	clientSecret := setting.GetStr(consts.SSOClientSecret)

	// 处理额外的作用域
	var extraScopes []string
	if extraScopesStr := setting.GetStr(consts.SSOExtraScopes); extraScopesStr != "" {
		extraScopes = strings.Split(extraScopesStr, " ")
	}

	// 创建OAuth2配置
	return &oauth2.Config{
		ClientID:     clientId,
		ClientSecret: clientSecret,
		RedirectURL:  redirectUri,
		Endpoint:     provider.Endpoint(),
		Scopes:       append([]string{oidc.ScopeOpenID, "profile"}, extraScopes...),
	}, nil
}

// autoRegister 根据SSO信息自动注册用户
// 当用户不存在且启用了自动注册时，创建新用户
func autoRegister(username, userID string, err error) (*model.User, error) {
	// 如果错误不是"记录未找到"或者未启用自动注册，则返回错误
	if !errors.Is(err, gorm.ErrRecordNotFound) || !setting.GetBool(consts.SSOAutoRegister) {
		return nil, err
	}

	// 验证用户名
	if username == "" {
		return nil, errors.New("cannot get username from SSO provider")
	}

	// 创建新用户
	user := &model.User{
		ID:         0,
		Username:   username,
		Password:   random.String(16),
		Permission: int32(setting.GetInt(consts.SSODefaultPermission, 0)),
		BasePath:   setting.GetStr(consts.SSODefaultDir),
		Role:       0,
		Disabled:   false,
		SsoID:      userID,
	}

	// 尝试保存用户
	if err = db.CreateUser(user); err != nil {
		// 处理用户名冲突
		if strings.HasPrefix(err.Error(), "UNIQUE constraint failed") && strings.HasSuffix(err.Error(), "username") {
			user.Username = user.Username + "_" + userID
			if err = db.CreateUser(user); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return user, nil
}

// parseJWT 解析JWT令牌的载荷部分
func parseJWT(p string) ([]byte, error) {
	// 分割JWT令牌
	parts := strings.Split(p, ".")
	if len(parts) < 2 {
		return nil, errors.Errorf("oidc: malformed jwt, expected 3 parts got %d", len(parts))
	}

	// 解码载荷部分
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.Wrap(err, "oidc: malformed jwt payload")
	}

	return payload, nil
}

// 生成HTML响应，用于向打开的窗口发送消息
func generatePostMessageHTML(messageData map[string]string) string {
	// 构建JavaScript对象字符串
	messageJSON := "{"
	for key, value := range messageData {
		messageJSON += fmt.Sprintf(`"%s":"%s",`, key, value)
	}
	// 移除最后一个逗号
	if len(messageData) > 0 {
		messageJSON = messageJSON[:len(messageJSON)-1]
	}
	messageJSON += "}"

	return fmt.Sprintf(`<!DOCTYPE html>
<head></head>
<body>
<script>
window.opener.postMessage(%s, "*")
window.close()
</script>
</body>`, messageJSON)
}

// OIDCLoginCallback 处理OIDC登录回调
func OIDCLoginCallback(c *gin.Context) {
	// 获取配置
	useCompatibility := setting.GetBool(consts.SSOCompatibilityMode)
	method := c.Query("method")
	if useCompatibility {
		method = path.Base(c.Request.URL.Path)
	}

	// 验证方法
	if method != MethodGetSSOID && method != MethodGetToken {
		common.ErrorStrResp(c, "invalid method: "+method, 400)
		return
	}

	// 获取OIDC配置
	clientId := setting.GetStr(consts.SSOClientId)
	endpoint := setting.GetStr(consts.SSOEndpointName)

	// 创建OIDC提供者
	provider, err := oidc.NewProvider(c, endpoint)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取OAuth2配置
	oauth2Config, err := GetOIDCClient(c, useCompatibility, "", method)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 验证状态参数
	if !verifyState(clientId, c.ClientIP(), c.Query("state")) {
		common.ErrorStrResp(c, "incorrect or expired state parameter", 400)
		return
	}

	// 交换授权码获取令牌
	oauth2Token, err := oauth2Config.Exchange(c, c.Query("code"))
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取ID令牌
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		common.ErrorStrResp(c, "no id_token found in oauth2 token", 400)
		return
	}

	// 验证ID令牌
	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientId,
	})
	_, err = verifier.Verify(c, rawIDToken)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 解析JWT获取用户信息
	payload, err := parseJWT(rawIDToken)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 获取用户ID
	usernameKey := setting.GetStr(consts.SSOOIDCUsernameKey, "name")
	userID := utils.GetBytes(payload, usernameKey).String()
	if userID == "" {
		common.ErrorStrResp(c, "cannot get username from OIDC provider", 400)
		return
	}

	// 处理获取SSO ID请求
	if method == MethodGetSSOID {
		if useCompatibility {
			c.Redirect(http.StatusFound, common.GetApiURL(c)+"/@manage?sso_id="+userID)
			return
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8",
			[]byte(generatePostMessageHTML(map[string]string{"sso_id": userID})))
		return
	}

	// 处理获取令牌请求
	// 获取用户信息
	user, err := db.GetUserBySSOID(userID)
	if err != nil {
		// 尝试自动注册
		user, err = autoRegister(userID, userID, err)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
	}

	// 生成令牌
	token, err := common.GenerateToken(user)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 返回令牌
	if useCompatibility {
		c.Redirect(http.StatusFound, common.GetApiURL(c)+"/@login?token="+token)
		return
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8",
		[]byte(generatePostMessageHTML(map[string]string{"token": token})))
}

// SSOLoginCallback 处理SSO登录回调
func SSOLoginCallback(c *gin.Context) {
	// 检查SSO是否启用
	enabled := setting.GetBool(consts.SSOLoginEnabled)
	if !enabled {
		common.ErrorStrResp(c, "single sign-on is disabled", 403)
		return
	}

	// 获取配置
	useCompatibility := setting.GetBool(consts.SSOCompatibilityMode)

	// 获取方法
	method := c.Query("method")
	if useCompatibility {
		method = path.Base(c.Request.URL.Path)
	}

	// 验证方法
	if method != MethodGetSSOID && method != MethodGetToken {
		common.ErrorStrResp(c, "invalid method: "+method, 400)
		return
	}

	// 获取SSO配置
	clientId := setting.GetStr(consts.SSOClientId)
	platform := setting.GetStr(consts.SSOLoginPlatform)
	clientSecret := setting.GetStr(consts.SSOClientSecret)

	// 对于OIDC平台，使用专门的处理函数
	if platform == PlatformOIDC {
		OIDCLoginCallback(c)
		return
	}

	// 配置不同平台的参数
	var tokenUrl, userUrl, scope, authField, idField, usernameField string
	additionalForm := make(map[string]string)

	switch platform {
	case PlatformGithub:
		tokenUrl = "https://github.com/login/oauth/access_token"
		userUrl = "https://api.github.com/user"
		authField = "code"
		scope = "read:user"
		idField = "id"
		usernameField = "login"

	case PlatformMicrosoft:
		tokenUrl = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
		userUrl = "https://graph.microsoft.com/v1.0/me"
		additionalForm["grant_type"] = "authorization_code"
		scope = "user.read"
		authField = "code"
		idField = "id"
		usernameField = "displayName"

	case PlatformGoogle:
		tokenUrl = "https://oauth2.googleapis.com/token"
		userUrl = "https://www.googleapis.com/oauth2/v1/userinfo"
		additionalForm["grant_type"] = "authorization_code"
		scope = "https://www.googleapis.com/auth/userinfo.profile"
		authField = "code"
		idField = "id"
		usernameField = "name"

	case PlatformDingtalk:
		tokenUrl = "https://api.dingtalk.com/v1.0/oauth2/userAccessToken"
		userUrl = "https://api.dingtalk.com/v1.0/contact/users/me"
		authField = "authCode"
		idField = "unionId"
		usernameField = "nick"

	case PlatformCasdoor:
		endpoint := strings.TrimSuffix(setting.GetStr(consts.SSOEndpointName), "/")
		tokenUrl = endpoint + "/api/login/oauth/access_token"
		userUrl = endpoint + "/api/userinfo"
		additionalForm["grant_type"] = "authorization_code"
		scope = "profile"
		authField = "code"
		idField = "sub"
		usernameField = "preferred_username"

	default:
		common.ErrorStrResp(c, "invalid platform: "+platform, 400)
		return
	}

	// 获取授权码
	callbackCode := c.Query(authField)
	if callbackCode == "" {
		common.ErrorStrResp(c, "no code provided", 400)
		return
	}

	// 交换授权码获取访问令牌
	var resp *resty.Response
	var err error

	if platform == PlatformDingtalk {
		// 钉钉使用JSON格式
		resp, err = ssoClient.R().
			SetHeader("content-type", "application/json").
			SetBody(map[string]string{
				"clientId":     clientId,
				"clientSecret": clientSecret,
				"code":         callbackCode,
				"grantType":    "authorization_code",
			}).
			Post(tokenUrl)
	} else {
		// 构建重定向URI
		redirectUri := ssoRedirectURI(c, useCompatibility, method)

		// 其他平台使用表单格式
		formData := map[string]string{
			"client_id":     clientId,
			"client_secret": clientSecret,
			"code":          callbackCode,
			"redirect_uri":  redirectUri,
			"scope":         scope,
		}

		// 添加额外参数
		for k, v := range additionalForm {
			formData[k] = v
		}

		resp, err = ssoClient.R().
			SetFormData(formData).
			Post(tokenUrl)
	}

	if err != nil {
		common.ErrorResp(c, errors.Errorf("failed to exchange token: %w", err), 400)
		return
	}

	// 获取用户信息
	var userResp *resty.Response

	if platform == PlatformDingtalk {
		// 钉钉使用特殊的认证头
		accessToken := utils.GetBytes(resp.Bytes(), "accessToken").String()
		if accessToken == "" {
			common.ErrorStrResp(c, "failed to get access token", 400)
			return
		}

		userResp, err = ssoClient.R().
			SetHeader("x-acs-dingtalk-access-token", accessToken).
			Get(userUrl)
	} else {
		// 其他平台使用Bearer令牌
		accessToken := utils.GetBytes(resp.Bytes(), "access_token").String()
		if accessToken == "" {
			common.ErrorStrResp(c, "failed to get access token", 400)
			return
		}

		userResp, err = ssoClient.R().
			SetHeader("Authorization", "Bearer "+accessToken).
			Get(userUrl)
	}

	if err != nil {
		common.ErrorResp(c, errors.Errorf("failed to get user info: %w", err), 400)
		return
	}

	// 获取用户ID
	userID := utils.GetBytes(userResp.Bytes(), idField).String()
	if userID == "" || userID == "0" {
		common.ErrorStrResp(c, "failed to get user ID from provider", 400)
		return
	}

	// 处理获取SSO ID请求
	if method == MethodGetSSOID {
		if useCompatibility {
			c.Redirect(http.StatusFound, common.GetApiURL(c)+"/@manage?sso_id="+userID)
			return
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8",
			[]byte(generatePostMessageHTML(map[string]string{"sso_id": userID})))
		return
	}

	// 处理获取令牌请求
	// 获取用户名
	username := utils.GetBytes(userResp.Bytes(), usernameField).String()

	// 获取用户信息
	user, err := db.GetUserBySSOID(userID)
	if err != nil {
		// 尝试自动注册
		user, err = autoRegister(username, userID, err)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
	}

	// 生成令牌
	token, err := common.GenerateToken(user)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 返回令牌
	if useCompatibility {
		c.Redirect(http.StatusFound, common.GetApiURL(c)+"/@login?token="+token)
		return
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8",
		[]byte(generatePostMessageHTML(map[string]string{"token": token})))
}
