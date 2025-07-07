package _189

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	myrand "github.com/dongdio/OpenList/v4/utility/utils/random"
)

// random 生成随机数字字符串
// 返回格式为 "0.xxx"，其中xxx为17位随机数字
func random() string {
	return fmt.Sprintf("0.%017d", myrand.Rand.Int63n(100000000000000000))
}

// RsaEncode 使用RSA公钥加密数据
// 参数:
//   - origData: 原始数据
//   - j_rsakey: RSA公钥字符串
//   - hex: 是否转换为十六进制格式
//
// 返回:
//   - 加密后的字符串
func RsaEncode(origData []byte, j_rsakey string, hex bool) string {
	// 构造PEM格式的公钥
	publicKey := []byte("-----BEGIN PUBLIC KEY-----\n" + j_rsakey + "\n-----END PUBLIC KEY-----")

	// 解析公钥
	block, _ := pem.Decode(publicKey)
	if block == nil {
		log.Error("解析RSA公钥失败")
		return ""
	}

	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		log.Errorf("解析PKIX公钥失败: %s", err.Error())
		return ""
	}

	pub, ok := pubInterface.(*rsa.PublicKey)
	if !ok {
		log.Error("公钥类型转换失败")
		return ""
	}

	// 使用公钥加密
	b, err := rsa.EncryptPKCS1v15(rand.Reader, pub, origData)
	if err != nil {
		log.Errorf("RSA加密失败: %s", err.Error())
		return ""
	}

	// 转换为Base64编码
	res := base64.StdEncoding.EncodeToString(b)

	// 如果需要，转换为十六进制格式
	if hex {
		return b64tohex(res)
	}
	return res
}

// Base64字符映射表
var b64map = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// 十六进制字符映射表
var BI_RM = "0123456789abcdefghijklmnopqrstuvwxyz"

// int2char 将整数转换为字符
// 参数:
//   - a: 整数值
//
// 返回:
//   - 对应的字符
func int2char(a int) string {
	if a < 0 || a >= len(BI_RM) {
		return "0" // 返回默认值，避免越界
	}
	return string(BI_RM[a])
}

// b64tohex 将Base64编码转换为十六进制编码
// 参数:
//   - a: Base64编码的字符串
//
// 返回:
//   - 十六进制编码的字符串
func b64tohex(a string) string {
	d := ""
	e := 0
	c := 0

	for i := 0; i < len(a); i++ {
		m := string(a[i])
		if m == "=" {
			continue
		}
		v := strings.Index(b64map, m)
		if v == -1 {
			// 处理无效字符
			continue
		}

		switch e {
		case 0:
			e = 1
			d += int2char(v >> 2)
			c = 3 & v
		case 1:
			e = 2
			d += int2char(c<<2 | v>>4)
			c = 15 & v
		case 2:
			e = 3
			d += int2char(c)
			d += int2char(v >> 2)
			c = 3 & v
		case 3:
			e = 0
			d += int2char(c<<2 | v>>4)
			d += int2char(15 & v)
		}
	}

	// 处理末尾
	if e == 1 {
		d += int2char(c << 2)
	}

	return d
}

// qs 将表单数据转换为查询字符串
// 参数:
//   - form: 表单数据映射
//
// 返回:
//   - 编码后的查询字符串
func qs(form map[string]string) string {
	f := make(url.Values)
	for k, v := range form {
		f.Set(k, v)
	}
	return EncodeParam(f)
}

// EncodeParam 将URL值编码为查询字符串
// 参数:
//   - v: URL值
//
// 返回:
//   - 编码后的查询字符串
func EncodeParam(v url.Values) string {
	if v == nil {
		return ""
	}

	var buf strings.Builder
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}

	for _, k := range keys {
		vs := v[k]
		for _, v := range vs {
			if buf.Len() > 0 {
				buf.WriteByte('&')
			}
			buf.WriteString(k)
			buf.WriteByte('=')
			// 注释掉的特殊处理代码
			// if k == "fileName" {
			//	buf.WriteString(encode(v))
			// } else {
			buf.WriteString(v)
			// }
		}
	}

	return buf.String()
}

// encode URL编码字符串
// 参数:
//   - str: 要编码的字符串
//
// 返回:
//   - 编码后的字符串
func encode(str string) string {
	// 注释掉的手动替换代码
	// str = strings.ReplaceAll(str, "%", "%25")
	// str = strings.ReplaceAll(str, "&", "%26")
	// str = strings.ReplaceAll(str, "+", "%2B")
	// return str
	return url.QueryEscape(str)
}

// AesEncrypt AES加密数据
// 参数:
//   - data: 要加密的数据
//   - key: 加密密钥
//
// 返回:
//   - 加密后的数据
func AesEncrypt(data, key []byte) []byte {
	// 创建加密块
	block, err := aes.NewCipher(key)
	if err != nil {
		log.Errorf("创建AES加密块失败: %s", err.Error())
		return []byte{}
	}

	// 填充数据
	data = PKCS7Padding(data, block.BlockSize())

	// 加密数据
	encrypted := make([]byte, len(data))
	size := block.BlockSize()

	for bs, be := 0, size; bs < len(data); bs, be = bs+size, be+size {
		block.Encrypt(encrypted[bs:be], data[bs:be])
	}

	return encrypted
}

// PKCS7Padding 实现PKCS#7填充
// 参数:
//   - ciphertext: 原始数据
//   - blockSize: 块大小
//
// 返回:
//   - 填充后的数据
func PKCS7Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

// hmacSha1 计算HMAC-SHA1签名
// 参数:
//   - data: 要签名的数据
//   - secret: 签名密钥
//
// 返回:
//   - 十六进制编码的签名
func hmacSha1(data string, secret string) string {
	h := hmac.New(sha1.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// getMd5 计算数据的MD5哈希值
// 参数:
//   - data: 要计算哈希的数据
//
// 返回:
//   - MD5哈希值
func getMd5(data []byte) []byte {
	h := md5.New()
	h.Write(data)
	return h.Sum(nil)
}

// decodeURIComponent 解码URL编码的组件
// 参数:
//   - str: 要解码的字符串
//
// 返回:
//   - 解码后的字符串
func decodeURIComponent(str string) string {
	r, err := url.PathUnescape(str)
	if err != nil {
		log.Errorf("URL解码失败: %s", err.Error())
		return str
	}
	// 注释掉的替换代码
	// r = strings.ReplaceAll(r, " ", "+")
	return r
}

// Random 根据模板生成随机字符串
// 参数:
//   - v: 模板字符串，其中'x'和'y'将被随机十六进制字符替换
//
// 返回:
//   - 生成的随机字符串
func Random(v string) string {
	reg := regexp.MustCompilePOSIX("[xy]")
	data := reg.ReplaceAllFunc([]byte(v), func(msg []byte) []byte {
		var i int64
		t := int64(16 * myrand.Rand.Float32())
		if msg[0] == 'x' {
			i = t
		} else {
			i = 3&t | 8
		}
		return []byte(strconv.FormatInt(i, 16))
	})
	return string(data)
}
