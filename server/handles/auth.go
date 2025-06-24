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
	// Default duration for login attempt cache expiry
	defaultLoginCacheDuration = time.Minute * 5
	// Default maximum number of login attempts before rate limiting
	defaultMaxLoginAttempts = 5
	// OTP issuer name for 2FA
	otpIssuerName = "OpenList"
)

// Cache for tracking login attempts by IP
var loginAttemptsCache = cache.NewMemCache[int]()

// LoginRequest represents the login request payload
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password"`
	OtpCode  string `json:"otp_code"`
}

// Login is deprecated - use LoginHash instead
// Handles login with plaintext password (which gets hashed)
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	// Hash the password before processing
	req.Password = model.StaticHash(req.Password)
	processLogin(c, &req)
}

// LoginHash handles login with pre-hashed password
func LoginHash(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	processLogin(c, &req)
}

// processLogin handles the common login logic for both Login and LoginHash
func processLogin(c *gin.Context, req *LoginRequest) {
	// Check for rate limiting
	ip := c.ClientIP()
	attempts, ok := loginAttemptsCache.Get(ip)
	if ok && attempts >= defaultMaxLoginAttempts {
		common.ErrorStrResp(c, "Too many unsuccessful sign-in attempts have been made using an incorrect username or password. Try again later.", 429)
		loginAttemptsCache.Expire(ip, defaultLoginCacheDuration)
		return
	}

	// Validate username and password
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

	// Validate 2FA if enabled
	if user.OtpSecret != "" {
		if !totp.Validate(req.OtpCode, user.OtpSecret) {
			common.ErrorStrResp(c, "Invalid 2FA code", 402)
			incrementLoginAttempts(ip)
			return
		}
	}

	// Generate authentication token
	token, err := common.GenerateToken(user)
	if err != nil {
		common.ErrorResp(c, err, 400, true)
		return
	}

	// Login successful - clear rate limiting counter
	loginAttemptsCache.Del(ip)
	common.SuccessResp(c, gin.H{"token": token})
}

// incrementLoginAttempts increases the count of failed login attempts for an IP
func incrementLoginAttempts(ip string) {
	count, ok := loginAttemptsCache.Get(ip)
	if !ok {
		count = 0
	}
	loginAttemptsCache.Set(ip, count+1)
}

// UserResponse extends the User model with additional fields for API responses
type UserResponse struct {
	model.User
	HasOTP bool `json:"otp"` // Indicates if 2FA is enabled
}

// CurrentUser returns information about the currently authenticated user
func CurrentUser(c *gin.Context) {
	user := c.MustGet("user").(*model.User)

	// Create response with sanitized user data
	userResp := UserResponse{
		User: *user,
	}

	// Remove sensitive information
	userResp.Password = ""

	// Set OTP flag if 2FA is enabled
	if userResp.OtpSecret != "" {
		userResp.HasOTP = true
	}

	common.SuccessResp(c, userResp)
}

// UpdateCurrent updates the current user's profile information
func UpdateCurrent(c *gin.Context) {
	var req model.User
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	user := c.MustGet("user").(*model.User)
	if user.IsGuest() {
		common.ErrorStrResp(c, "Guest user cannot update profile", 403)
		return
	}

	// Update allowed fields
	user.Username = req.Username
	user.SsoID = req.SsoID

	// Update password if provided
	if req.Password != "" {
		user.SetPassword(req.Password)
	}

	if err := op.UpdateUser(user); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}

// Generate2FA creates a new 2FA secret and QR code for the current user
func Generate2FA(c *gin.Context) {
	user := c.MustGet("user").(*model.User)
	if user.IsGuest() {
		common.ErrorStrResp(c, "Guest user cannot enable 2FA", 403)
		return
	}

	// Generate new TOTP key
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      otpIssuerName,
		AccountName: user.Username,
	})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// Generate QR code image
	img, err := key.Image(400, 400)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// Convert image to base64
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

// Verify2FARequest represents the request to verify and enable 2FA
type Verify2FARequest struct {
	Code   string `json:"code" binding:"required"`
	Secret string `json:"secret" binding:"required"`
}

// Verify2FA verifies a 2FA code and enables 2FA for the current user
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

	// Verify the provided code against the secret
	if !totp.Validate(req.Code, req.Secret) {
		common.ErrorStrResp(c, "Invalid 2FA code", 400)
		return
	}

	// Save the verified secret
	user.OtpSecret = req.Secret
	if err := op.UpdateUser(user); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}

// LogOut invalidates the current user's authentication token
func LogOut(c *gin.Context) {
	err := common.InvalidateToken(c.GetHeader("Authorization"))
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c)
}
