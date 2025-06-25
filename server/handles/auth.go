package handles

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"time"

	"github.com/Xhofe/go-cache"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"

	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/server/common"
)

const (
	// 登录尝试缓存过期时间
	defaultLoginCacheDuration = time.Minute * 5
	// 最大登录尝试次数，超过此次数将进行速率限制
	defaultMaxLoginAttempts = 5
	// 2FA OTP 发行方名称
	otpIssuerName = "OpenList"
	// QR 码尺寸
	qrCodeSize = 400
)

// 用于跟踪 IP 登录尝试次数的缓存
var loginAttemptsCache = cache.NewMemCache[int]()

// LoginRequest 登录请求参数
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	OtpCode  string `json:"otp_code"`
}

// Login 已弃用 - 请使用 LoginHash
// 处理明文密码登录（会被哈希处理）
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// 对密码进行哈希处理
	req.Password = model.StaticHash(req.Password)
	processLogin(c, &req)
}

// LoginHash 处理预哈希密码登录
func LoginHash(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	processLogin(c, &req)
}

// processLogin 处理 Login 和 LoginHash 的通用登录逻辑
func processLogin(c *gin.Context, req *LoginRequest) {
	// 检查速率限制
	ip := c.ClientIP()
	attempts, ok := loginAttemptsCache.Get(ip)
	if ok && attempts >= defaultMaxLoginAttempts {
		common.ErrorStrResp(c, "Too many unsuccessful sign-in attempts have been made using an incorrect username or password. Try again later.", 429)
		loginAttemptsCache.Expire(ip, defaultLoginCacheDuration)
		return
	}

	// 验证用户名和密码
	user, err := op.GetUserByName(req.Username)
	if err != nil {
		common.ErrorResp(c, err, 400)
		incrementLoginAttempts(ip)
		return
	}

	if err := user.ValidatePwdStaticHash(req.Password); err != nil {
		common.ErrorResp(c, err, 400)
		incrementLoginAttempts(ip)
		return
	}

	// 如果启用了 2FA，验证 OTP 码
	if user.OtpSecret != "" {
		if req.OtpCode == "" {
			common.ErrorStrResp(c, "2FA code is required", 400)
			incrementLoginAttempts(ip)
			return
		}

		if !totp.Validate(req.OtpCode, user.OtpSecret) {
			common.ErrorStrResp(c, "Invalid 2FA code", 402)
			incrementLoginAttempts(ip)
			return
		}
	}

	// 生成身份验证令牌
	token, err := common.GenerateToken(user)
	if err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}

	// 登录成功 - 清除速率限制计数器
	loginAttemptsCache.Del(ip)
	common.SuccessResp(c, gin.H{"token": token})
}

// incrementLoginAttempts 增加 IP 的失败登录尝试次数
func incrementLoginAttempts(ip string) {
	count, ok := loginAttemptsCache.Get(ip)
	if !ok {
		count = 0
	}
	loginAttemptsCache.Set(ip, count+1)
}

// UserResponse 扩展 User 模型，用于 API 响应
type UserResponse struct {
	model.User
	HasOTP bool `json:"otp"` // 指示是否启用了 2FA
}

// CurrentUser 返回当前认证用户的信息
func CurrentUser(c *gin.Context) {
	user := c.MustGet("user").(*model.User)

	// 创建包含净化用户数据的响应
	userResp := UserResponse{
		User: *user,
	}

	// 移除敏感信息
	userResp.Password = ""
	userResp.OtpSecret = ""

	// 设置 OTP 标志（如果启用了 2FA）
	if user.OtpSecret != "" {
		userResp.HasOTP = true
	}

	common.SuccessResp(c, userResp)
}

// UpdateUserRequest 更新用户请求参数
type UpdateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	SsoID    string `json:"sso_id"`
}

// UpdateCurrent 更新当前用户的个人资料信息
func UpdateCurrent(c *gin.Context) {
	var req UpdateUserRequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	user := c.MustGet("user").(*model.User)
	if user.IsGuest() {
		common.ErrorStrResp(c, "Guest user cannot update profile", 403)
		return
	}

	// 验证用户名不为空
	if req.Username != "" {
		user.Username = req.Username
	}

	// 更新 SSO ID（如果提供）
	if req.SsoID != "" {
		user.SsoID = req.SsoID
	}

	// 更新密码（如果提供）
	if req.Password != "" {
		user.SetPassword(req.Password)
	}

	if err := op.UpdateUser(user); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}

// Generate2FA 为当前用户创建新的 2FA 密钥和 QR 码
func Generate2FA(c *gin.Context) {
	user := c.MustGet("user").(*model.User)
	if user.IsGuest() {
		common.ErrorStrResp(c, "Guest user cannot enable 2FA", 403)
		return
	}

	// 生成新的 TOTP 密钥
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      otpIssuerName,
		AccountName: user.Username,
	})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 生成 QR 码图像
	img, err := key.Image(qrCodeSize, qrCodeSize)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 将图像转换为 base64
	var buf bytes.Buffer
	if err = png.Encode(&buf, img); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	qrCodeBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	common.SuccessResp(c, gin.H{
		"qr":     "data:image/png;base64," + qrCodeBase64,
		"secret": key.Secret(),
	})
}

// Verify2FARequest 验证并启用 2FA 的请求参数
type Verify2FARequest struct {
	Code   string `json:"code" binding:"required"`
	Secret string `json:"secret" binding:"required"`
}

// Verify2FA 验证 2FA 代码并为当前用户启用 2FA
func Verify2FA(c *gin.Context) {
	var req Verify2FARequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	user := c.MustGet("user").(*model.User)
	if user.IsGuest() {
		common.ErrorStrResp(c, "Guest user cannot enable 2FA", 403)
		return
	}

	// 验证提供的代码与密钥
	if !totp.Validate(req.Code, req.Secret) {
		common.ErrorStrResp(c, "Invalid 2FA code", 400)
		return
	}

	// 保存验证过的密钥
	user.OtpSecret = req.Secret
	if err := op.UpdateUser(user); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}

// LogOut 使当前用户的身份验证令牌失效
func LogOut(c *gin.Context) {
	token := c.GetHeader("Authorization")
	if token == "" {
		common.ErrorStrResp(c, "No authorization token provided", 400)
		return
	}

	err := common.InvalidateToken(token)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}
