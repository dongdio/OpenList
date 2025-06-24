package common

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/global"
	"github.com/dongdio/OpenList/internal/conf"
)

// hidePrivacy 隐藏消息中的隐私信息
// 使用正则表达式替换敏感信息为*
//
// 参数:
//   - msg: 原始消息
//
// 返回:
//   - 处理后的消息，敏感信息被*替换
func hidePrivacy(msg string) string {
	for _, r := range conf.PrivacyReg {
		msg = r.ReplaceAllStringFunc(msg, func(s string) string {
			return strings.Repeat("*", len(s))
		})
	}
	return msg
}

// ErrorResp 返回错误响应
// 封装了ErrorWithDataResp函数，不包含额外数据
//
// 参数:
//   - c: Gin上下文
//   - err: 错误信息
//   - code: 错误码
//   - l: 可选参数，是否记录日志，默认为不记录
func ErrorResp(c *gin.Context, err error, code int, l ...bool) {
	ErrorWithDataResp(c, err, code, nil, l...)
}

// ErrorWithDataResp 返回带数据的错误响应
// 如果指定记录日志，会根据调试模式决定日志详细程度
//
// 参数:
//   - c: Gin上下文
//   - err: 错误信息
//   - code: 错误码
//   - data: 额外数据
//   - l: 可选参数，是否记录日志，默认为不记录
func ErrorWithDataResp(c *gin.Context, err error, code int, data any, l ...bool) {
	// 判断是否需要记录日志
	if len(l) > 0 && l[0] {
		if global.Debug || global.Dev {
			// 调试模式下记录详细错误
			log.Errorf("%+v", err)
		} else {
			// 生产模式下记录简单错误
			log.Errorf("%v", err)
		}
	}

	// 返回JSON响应
	c.JSON(200, Resp[any]{
		Code:    code,
		Message: hidePrivacy(err.Error()),
		Data:    data,
	})
	c.Abort()
}

// ErrorStrResp 返回字符串错误响应
// 与ErrorResp类似，但接受字符串而非error类型
//
// 参数:
//   - c: Gin上下文
//   - str: 错误消息字符串
//   - code: 错误码
//   - l: 可选参数，是否记录日志，默认为不记录
func ErrorStrResp(c *gin.Context, str string, code int, l ...bool) {
	if len(l) != 0 && l[0] {
		log.Error(str)
	}
	c.JSON(200, Resp[any]{
		Code:    code,
		Message: hidePrivacy(str),
		Data:    nil,
	})
	c.Abort()
}

// SuccessResp 返回成功响应
// 默认消息为"success"
//
// 参数:
//   - c: Gin上下文
//   - data: 可选参数，响应数据
func SuccessResp(c *gin.Context, data ...any) {
	SuccessWithMsgResp(c, "success", data...)
}

// SuccessWithMsgResp 返回带自定义消息的成功响应
//
// 参数:
//   - c: Gin上下文
//   - msg: 成功消息
//   - data: 可选参数，响应数据
func SuccessWithMsgResp(c *gin.Context, msg string, data ...any) {
	var respData any
	if len(data) > 0 {
		respData = data[0]
	}

	c.JSON(200, Resp[any]{
		Code:    200,
		Message: msg,
		Data:    respData,
	})
}

// Pluralize 根据数量返回单数或复数形式的字符串
//
// 参数:
//   - count: 数量
//   - singular: 单数形式
//   - plural: 复数形式
//
// 返回:
//   - 根据count选择的字符串
func Pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

// GetHttpReq 从上下文中获取HTTP请求
// 如果上下文是gin.Context类型，则返回其中的Request
//
// 参数:
//   - ctx: 上下文
//
// 返回:
//   - HTTP请求对象，如果无法获取则返回nil
func GetHttpReq(ctx context.Context) *http.Request {
	if c, ok := ctx.(*gin.Context); ok {
		return c.Request
	}
	return nil
}
