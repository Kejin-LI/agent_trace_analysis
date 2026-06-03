package ucrypto

import (
	"crypto/rc4"
)

//rc4加密，key任意长度
func Rc4Encrypt(key []byte, input []byte) ([]byte, error) {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil, err
	}

	output := make([]byte, len(input))
	c.XORKeyStream(output, input)
	return output, nil
}

//rc4解密
func Rc4Decrypt(key []byte, input []byte) ([]byte, error) {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil, err
	}

	output := make([]byte, len(input))
	c.XORKeyStream(output, input)
	return output, nil
}
