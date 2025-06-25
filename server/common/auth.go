package common

import (
	"time"

	"github.com/Xhofe/go-cache"
	"github.com/golang-jwt/jwt/v4"
	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/model"
)

// SecretKey 用于JWT签名的密钥
var SecretKey []byte

// UserClaims JWT中包含的用户信息
type UserClaims struct {
	Username string `json:"username"` // 用户名
	PwdTS    int64  `json:"pwd_ts"`   // 密码时间戳（用于在密码更改时使token失效）
	jwt.RegisteredClaims
}

// validTokenCache 有效token的缓存，用于支持主动使token失效
var validTokenCache = cache.NewMemCache[bool]()

// GenerateToken 为指定用户生成JWT令牌
//
// 参数:
//   - user: 用户对象
//
// 返回:
//   - tokenString: 生成的令牌字符串
//   - err: 错误信息
func GenerateToken(user *model.User) (tokenString string, err error) {
	// 检查用户是否为nil
	if user == nil {
		return "", errors.New("用户不能为空")
	}

	// 创建JWT声明
	claim := UserClaims{
		Username: user.Username,
		PwdTS:    user.PwdTS,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(conf.Conf.TokenExpiresIn) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	// 创建并签名令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claim)
	tokenString, err = token.SignedString(SecretKey)
	if err != nil {
		return "", errors.Wrap(err, "签名令牌失败")
	}

	// 将令牌加入有效缓存
	validTokenCache.Set(tokenString, true)
	return tokenString, nil
}

// ParseToken 解析JWT令牌
//
// 参数:
//   - tokenString: 令牌字符串
//
// 返回:
//   - *UserClaims: 解析出的用户声明
//   - error: 错误信息
func ParseToken(tokenString string) (*UserClaims, error) {
	// 检查令牌是否为空
	if tokenString == "" {
		return nil, errors.New("令牌不能为空")
	}

	// 检查令牌是否已被注销
	if IsTokenInvalidated(tokenString) {
		return nil, errors.New("令牌已被注销")
	}

	// 解析令牌
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (any, error) {
		return SecretKey, nil
	})

	// 处理解析错误
	if err != nil {
		var ve *jwt.ValidationError
		if errors.As(err, &ve) {
			if ve.Errors&jwt.ValidationErrorMalformed != 0 {
				return nil, errors.New("令牌格式不正确")
			} else if ve.Errors&jwt.ValidationErrorExpired != 0 {
				return nil, errors.New("令牌已过期")
			} else if ve.Errors&jwt.ValidationErrorNotValidYet != 0 {
				return nil, errors.New("令牌尚未生效")
			} else {
				return nil, errors.New("无法处理此令牌")
			}
		}
		return nil, errors.Wrap(err, "解析令牌失败")
	}

	// 检查令牌是否为nil
	if token == nil {
		return nil, errors.New("无法解析令牌")
	}

	// 提取声明
	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("无法处理此令牌")
}

// InvalidateToken 使令牌失效
//
// 参数:
//   - tokenString: 令牌字符串
//
// 返回:
//   - error: 错误信息
func InvalidateToken(tokenString string) error {
	// 空令牌不需要处理
	if tokenString == "" {
		return nil
	}

	// 从有效缓存中移除
	validTokenCache.Del(tokenString)
	return nil
}

// IsTokenInvalidated 检查令牌是否已被注销
//
// 参数:
//   - tokenString: 令牌字符串
//
// 返回:
//   - bool: 如果令牌已被注销返回true，否则返回false
func IsTokenInvalidated(tokenString string) bool {
	_, ok := validTokenCache.Get(tokenString)
	return !ok
}