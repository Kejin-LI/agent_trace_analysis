package ucrypto

import (
	"crypto/md5"
	"encoding/hex"
)

//md5(32位小写字母)
func Md5(b []byte) (r string) {
	src := md5.Sum(b)
	return hex.EncodeToString(src[:]) //内部[]byte转换为string
}
