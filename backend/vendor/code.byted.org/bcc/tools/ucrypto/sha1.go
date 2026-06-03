package ucrypto

import (
	"crypto/sha1"
	"encoding/hex"
)

func Sha1(b []byte) string {
	r := sha1.Sum(b)
	return hex.EncodeToString(r[:])
}
