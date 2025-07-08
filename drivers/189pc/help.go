package _189pc

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils/random"
)

// clientSuffix 生成客户端请求后缀参数
// 返回包含客户端类型、版本、渠道ID和随机数的参数映射
func clientSuffix() map[string]string {
	rand := random.Rand
	return map[string]string{
		"clientType": PC,
		"version":    _version,
		"channelId":  _channelID,
		"rand":       fmt.Sprintf("%d_%d", rand.Int63n(1e5), rand.Int63n(1e10)),
	}
}

// signatureOfHmac 计算HMAC签名
// 根据会话密钥、会话ID、操作、URL、GMT时间和参数生成签名
// 参数:
//   - sessionSecret: 会话密钥
//   - sessionKey: 会话ID
//   - operate: 操作类型
//   - fullUrl: 完整URL
//   - dateOfGmt: GMT格式的日期
//   - param: 参数字符串
//
// 返回值: 大写的十六进制签名字符串
func signatureOfHmac(sessionSecret, sessionKey, operate, fullUrl, dateOfGmt, param string) string {
	urlpath := regexp.MustCompile(`://[^/]+((/[^/\s?#]+)*)`).FindStringSubmatch(fullUrl)[1]
	mac := hmac.New(sha1.New, []byte(sessionSecret))
	data := fmt.Sprintf("SessionKey=%s&Operate=%s&RequestURI=%s&Date=%s", sessionKey, operate, urlpath, dateOfGmt)
	if param != "" {
		data += fmt.Sprintf("&params=%s", param)
	}
	mac.Write([]byte(data))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

// RsaEncrypt 使用RSA加密数据
// 使用公钥对原始数据进行加密
// 参数:
//   - publicKey: PEM格式的RSA公钥
//   - origData: 要加密的原始数据
//
// 返回值: 大写的十六进制加密字符串
func RsaEncrypt(publicKey, origData string) string {
	block, _ := pem.Decode([]byte(publicKey))
	pubInterface, _ := x509.ParsePKIXPublicKey(block.Bytes)
	data, _ := rsa.EncryptPKCS1v15(rand.Reader, pubInterface.(*rsa.PublicKey), []byte(origData))
	return strings.ToUpper(hex.EncodeToString(data))
}

// AesECBEncrypt 使用AES-ECB模式加密数据
// 参数:
//   - data: 要加密的数据
//   - key: 加密密钥
//
// 返回值: 大写的十六进制加密字符串
func AesECBEncrypt(data, key string) string {
	block, _ := aes.NewCipher([]byte(key))
	paddingData := PKCS7Padding([]byte(data), block.BlockSize())
	decrypted := make([]byte, len(paddingData))
	size := block.BlockSize()
	for src, dst := paddingData, decrypted; len(src) > 0; src, dst = src[size:], dst[size:] {
		block.Encrypt(dst[:size], src[:size])
	}
	return strings.ToUpper(hex.EncodeToString(decrypted))
}

// PKCS7Padding 实现PKCS#7填充
// 参数:
//   - ciphertext: 要填充的数据
//   - blockSize: 块大小
//
// 返回值: 填充后的数据
func PKCS7Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

// getHttpDateStr 获取HTTP规范的时间字符串
// 返回值: 符合HTTP规范的GMT时间字符串
func getHttpDateStr() string {
	return time.Now().UTC().Format(http.TimeFormat)
}

// timestamp 获取当前时间戳（毫秒）
// 返回值: 毫秒级时间戳
func timestamp() int64 {
	return time.Now().UTC().UnixNano() / 1e6
}

// MustParseTime 解析时间字符串为时间对象
// 参数:
//   - str: 时间字符串，格式为 "2006-01-02 15:04:05"
//
// 返回值: 解析后的时间对象指针
func MustParseTime(str string) *time.Time {
	lastOpTime, _ := time.ParseInLocation("2006-01-02 15:04:05 -07", str+" +08", time.Local)
	return &lastOpTime
}

// Time 自定义时间类型，用于处理多种格式的时间解析
type Time time.Time

// UnmarshalJSON 实现JSON反序列化接口
func (t *Time) UnmarshalJSON(b []byte) error { return t.Unmarshal(b) }

// UnmarshalXML 实现XML反序列化接口
func (t *Time) UnmarshalXML(e *xml.Decoder, ee xml.StartElement) error {
	b, err := e.Token()
	if err != nil {
		return err
	}
	if b, ok := b.(xml.CharData); ok {
		if err = t.Unmarshal(b); err != nil {
			return err
		}
	}
	return e.Skip()
}

// Unmarshal 通用反序列化方法，支持多种时间格式
func (t *Time) Unmarshal(b []byte) error {
	bs := strings.Trim(string(b), "\"")
	var v time.Time
	var err error
	for _, f := range []string{"2006-01-02 15:04:05 -07", "Jan 2, 2006 15:04:05 PM -07"} {
		v, err = time.ParseInLocation(f, bs+" +08", time.Local)
		if err == nil {
			break
		}
	}
	*t = Time(v)
	return err
}

// String 自定义字符串类型，用于处理特殊格式的字符串解析
type String string

// UnmarshalJSON 实现JSON反序列化接口
func (t *String) UnmarshalJSON(b []byte) error { return t.Unmarshal(b) }

// UnmarshalXML 实现XML反序列化接口
func (t *String) UnmarshalXML(e *xml.Decoder, ee xml.StartElement) error {
	b, err := e.Token()
	if err != nil {
		return err
	}
	if b, ok := b.(xml.CharData); ok {
		if err = t.Unmarshal(b); err != nil {
			return err
		}
	}
	return e.Skip()
}

// Unmarshal 通用反序列化方法，去除字符串两端的引号
func (s *String) Unmarshal(b []byte) error {
	*s = String(bytes.Trim(b, "\""))
	return nil
}

// toFamilyOrderBy 将排序字段转换为家庭云API支持的格式
// 参数:
//   - o: 排序字段名称
//
// 返回值: 转换后的排序字段值
func toFamilyOrderBy(o string) string {
	switch o {
	case "filename":
		return "1"
	case "filesize":
		return "2"
	case "lastOpTime":
		return "3"
	default:
		return "1"
	}
}

// toDesc 将排序方向转换为API支持的格式
// 参数:
//   - o: 排序方向
//
// 返回值: 转换后的排序方向值
func toDesc(o string) string {
	switch o {
	case "desc":
		return "true"
	case "asc":
		fallthrough
	default:
		return "false"
	}
}

// ParseHttpHeader 解析HTTP头部字符串为映射
// 参数:
//   - str: HTTP头部字符串，格式为 "key1=value1&key2=value2"
//
// 返回值: 解析后的头部映射
func ParseHttpHeader(str string) map[string]string {
	header := make(map[string]string)
	for _, value := range strings.Split(str, "&") {
		if k, v, found := strings.Cut(value, "="); found {
			header[k] = v
		}
	}
	return header
}

// MustString 忽略错误获取字符串
// 参数:
//   - str: 字符串值
//   - err: 错误对象（被忽略）
//
// 返回值: 原始字符串
func MustString(str string, err error) string {
	return str
}

// BoolToNumber 将布尔值转换为数字
// 参数:
//   - b: 布尔值
//
// 返回值: 布尔值对应的数字（true=1, false=0）
func BoolToNumber(b bool) int {
	if b {
		return 1
	}
	return 0
}

// partSize 计算分片大小
// 根据文件大小计算合适的分片大小，遵循API对分片数量的限制
// 参数:
//   - size: 文件大小
//
// 返回值: 计算得到的分片大小
func partSize(size int64) int64 {
	const DEFAULT = 1024 * 1024 * 10 // 10MIB
	if size > DEFAULT*2*999 {
		return int64(math.Max(math.Ceil((float64(size)/1999) /*=单个切片大小*/ /float64(DEFAULT)) /*=倍率*/, 5) * DEFAULT)
	}
	if size > DEFAULT*999 {
		return DEFAULT * 2 // 20MIB
	}
	return DEFAULT
}

// isBool 检查多个布尔值中是否有一个为true
// 参数:
//   - bs: 布尔值列表
//
// 返回值: 如果任一值为true则返回true，否则返回false
func isBool(bs ...bool) bool {
	for _, b := range bs {
		if b {
			return true
		}
	}
	return false
}

// IF 泛型条件选择函数
// 根据条件选择两个值中的一个
// 参数:
//   - o: 条件
//   - t: 条件为true时返回的值
//   - f: 条件为false时返回的值
//
// 返回值: 根据条件选择的值
func IF[V any](o bool, t V, f V) V {
	if o {
		return t
	}
	return f
}

// WrapFileStreamer 文件流包装器
type WrapFileStreamer struct {
	model.FileStreamer
	Name string
}

// GetName 获取文件名
// 返回值: 文件名
func (w *WrapFileStreamer) GetName() string {
	return w.Name
}
