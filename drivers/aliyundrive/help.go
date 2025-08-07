package aliyundrive

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"math/big"

	"github.com/dustinxie/ecc"

	"github.com/dongdio/OpenList/v4/utility/errs"
)

// GeneratePrivateKey 生成新的ECDSA私钥
func GeneratePrivateKey() (*ecdsa.PrivateKey, error) {
	p256k1 := ecc.P256k1()
	return ecdsa.GenerateKey(p256k1, rand.Reader)
}

// PrivateKeyFromHex 从十六进制字符串创建私钥
func PrivateKeyFromHex(hexStr string) (*ecdsa.PrivateKey, error) {
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, errs.Wrap(err, "解码私钥失败")
	}
	return PrivateKeyFromBytes(data), nil
}

// PrivateKeyFromBytes 从字节数组创建私钥
func PrivateKeyFromBytes(priv []byte) *ecdsa.PrivateKey {
	p256k1 := ecc.P256k1()
	x, y := p256k1.ScalarBaseMult(priv)
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: p256k1,
			X:     x,
			Y:     y,
		},
		D: new(big.Int).SetBytes(priv),
	}
}

// PrivateKeyToHex 将私钥转换为十六进制字符串
func PrivateKeyToHex(private *ecdsa.PrivateKey) string {
	return hex.EncodeToString(PrivateKeyToBytes(private))
}

// PrivateKeyToBytes 将私钥转换为字节数组
func PrivateKeyToBytes(private *ecdsa.PrivateKey) []byte {
	return private.D.Bytes()
}

// PublicKeyToHex 将公钥转换为十六进制字符串
func PublicKeyToHex(public *ecdsa.PublicKey) string {
	return hex.EncodeToString(PublicKeyToBytes(public))
}

// PublicKeyToBytes 将公钥转换为字节数组
func PublicKeyToBytes(public *ecdsa.PublicKey) []byte {
	// 确保X坐标为32字节
	x := padTo32Bytes(public.X.Bytes())
	// 确保Y坐标为32字节
	y := padTo32Bytes(public.Y.Bytes())

	return append(x, y...)
}

// padTo32Bytes 将字节数组填充到32字节
func padTo32Bytes(data []byte) []byte {
	if len(data) >= 32 {
		return data
	}

	padded := make([]byte, 32)
	copy(padded[32-len(data):], data)
	return padded
}