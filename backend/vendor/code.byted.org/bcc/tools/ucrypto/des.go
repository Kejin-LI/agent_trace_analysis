package ucrypto

import (
	"crypto/cipher"
	"crypto/des"
)

//des加密，len(key)=8
func DesEncrypt(key []byte, input []byte) ([]byte, error) {
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}
	input = packPadding(input, block.BlockSize())

	output := make([]byte, len(input))
	mode := cipher.NewCBCEncrypter(block, key)
	mode.CryptBlocks(output, input)

	return output, nil
}

//des解密
func DesDecrypt(key []byte, input []byte) ([]byte, error) {
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}

	output := make([]byte, len(input))
	mode := cipher.NewCBCDecrypter(block, key)
	mode.CryptBlocks(output, input)

	output = unpackPadding(output)
	return output, nil
}

//3des加解密，des.NewTripleDESCipher，des.NewTripleDESCipher
