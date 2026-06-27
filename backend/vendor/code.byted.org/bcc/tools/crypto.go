package tools

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"

	"code.byted.org/bcc/tools/ucrypto"
)

//md5(32位小写字母)
func Md5(b []byte) (r string) {
	src := md5.Sum(b)
	return hex.EncodeToString(src[:]) //内部[]byte转换为string
}

//shal
func Sha1(b []byte) string {
	r := sha1.Sum(b)
	return hex.EncodeToString(r[:])
}

//aes
func AesEncrypt(key []byte, input []byte) ([]byte, error) {
	return ucrypto.AesEncrypt(key, input)
}
func AesDecrypt(key []byte, input []byte) ([]byte, error) {
	return ucrypto.AesDecrypt(key, input)
}

//des
func DesEncrypt(key []byte, input []byte) ([]byte, error) {
	return ucrypto.DesEncrypt(key, input)
}
func DesDecrypt(key []byte, input []byte) ([]byte, error) {
	return ucrypto.DesDecrypt(key, input)
}

//rc4
func Rc4Encrypt(key []byte, input []byte) ([]byte, error) {
	return ucrypto.Rc4Encrypt(key, input)
}
func Rc4Decrypt(key []byte, input []byte) ([]byte, error) {
	return ucrypto.Rc4Decrypt(key, input)
}
