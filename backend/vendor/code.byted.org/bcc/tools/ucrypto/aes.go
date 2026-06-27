package ucrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

//aes加密，key长度[16,24,32]
func AesEncrypt(key []byte, input []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	input = packPadding(input, blockSize)

	// 对IV有随机性要求，但没有保密性要求，所以常见的做法是将IV包含在加密文本当中
	output := make([]byte, blockSize+len(input))
	//随机一个block大小作为IV
	//采用不同的IV时相同的秘钥将会产生不同的密文，可以理解为一次加密的session
	iv := output[:blockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		panic(err)
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(output[blockSize:], input)

	return output, nil
}

//aes解密
func AesDecrypt(key []byte, input []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()

	if len(input) < blockSize {
		return nil, errors.New("invalid_size")
	}

	iv := input[:blockSize]
	data := input[blockSize:]
	output := make([]byte, len(data))

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(output, data)

	output = unpackPadding(output)
	return output, nil
}

////---------------------------------------------------------
//type AESMode int
//
//const (
//	AESModeCBC AESMode = 0
//	AESModeECB AESMode = 1
//)
//
//type AESOptions struct {
//	Mode  AESMode
//	Block cipher.Block
//	IV    []byte
//}
//type AESOption func(*AESOptions)
//
//// 设置加密模式
//func WithMode(mode AESMode) AESOption {
//	return func(o *AESOptions) {
//		o.Mode = mode
//	}
//}
//
//// 设置密码块，对于固定密钥的场景，使用提前生成的密码块可以提高一些性能
//func WithBlock(block cipher.Block) AESOption {
//	return func(o *AESOptions) {
//		o.Block = block
//	}
//}
//
//// 设置初始化向量，自动对齐到 aes 块大小
//func WithIV(iv []byte) AESOption {
//	return func(o *AESOptions) {
//		if iv != nil && len(iv) != aes.BlockSize {
//			tmp := make([]byte, aes.BlockSize)
//			copy(tmp, iv[:aes.BlockSize])
//			iv = tmp
//		}
//		o.IV = iv
//	}
//}
//
//var defaultAESOptions = AESOptions{
//	Mode:  AESModeCBC,
//	Block: nil,
//	IV:    nil,
//}
//
//func makeOpts(key []byte, opts ...AESOption) (*AESOptions, error) {
//	opt := defaultAESOptions
//	for _, o := range opts {
//		o(&opt)
//	}
//	if opt.Block == nil {
//		var err error
//		if opt.Block, err = aes.NewCipher(key); err != nil {
//			return nil, err
//		}
//	}
//	if opt.IV == nil {
//		WithIV(key)(&opt)
//	}
//	return &opt, nil
//}
//
//func AESEncrypt(input []byte, key []byte, opts ...AESOption) (output []byte, err error) {
//	// 检查
//	if input == nil || len(input) == 0 {
//		return nil, fmt.Errorf("AESEncrypt fail, empty input")
//	}
//	if key == nil || len(key) == 0 {
//		return nil, fmt.Errorf("AESEncrypt fail, empty key")
//	}
//	// 构造参数
//	opt, e := makeOpts(key, opts...)
//	if e != nil {
//		return nil, fmt.Errorf("AESEncrypt fail, %v", e)
//	}
//	// 填充
//	input = PKCS7Pack(input, aes.BlockSize)
//	// 块加密
//	output = make([]byte, len(input))
//	{
//		if opt.Mode == AESModeCBC {
//			cipher.NewCBCEncrypter(opt.Block, opt.IV).CryptBlocks(output, input)
//		} else if opt.Mode == AESModeECB {
//			opt.Block.Encrypt(output, input)
//		} else {
//			return nil, fmt.Errorf("AESEncrypt fail, unsupport mode %v", opt.Mode)
//		}
//	}
//	return output, nil
//}
//
//func AESDecrypt(input []byte, key []byte, opts ...AESOption) (output []byte, err error) {
//	// 检查
//	if input == nil || len(input) == 0 {
//		return nil, fmt.Errorf("AESDecrypt fail, empty input")
//	}
//	if len(input)%aes.BlockSize != 0 {
//		return nil, fmt.Errorf("AESDecrypt fail, invalid input len %v", len(input))
//	}
//	if key == nil || len(key) == 0 {
//		return nil, fmt.Errorf("AESDecrypt fail, empty key")
//	}
//	// 构造参数
//	opt, e := makeOpts(key, opts...)
//	if e != nil {
//		return nil, fmt.Errorf("AESDecrypt fail, %v", e)
//	}
//	// 块解密
//	output = make([]byte, len(input))
//	{
//		if opt.Mode == AESModeCBC {
//			cipher.NewCBCDecrypter(opt.Block, opt.IV).CryptBlocks(output, input)
//		} else if opt.Mode == AESModeECB {
//			opt.Block.Decrypt(output, input)
//		} else {
//			return nil, fmt.Errorf("AESDecrypt fail, unsupport mode %v", opt.Mode)
//		}
//	}
//	// 提取
//	output, err = PKCS7Unpack(output)
//	if err != nil {
//		return nil, fmt.Errorf("AESDecrypt fail, %v", err)
//	}
//	return output, err
//}
//
////-----
//
//func init() {
//	initPKCS7Table()
//}
//
//var (
//	_PKCS7Table [][]byte
//	_PKCS7Range int
//)
//
//func initPKCS7Table() {
//	_PKCS7Range = 16
//	_PKCS7Table = make([][]byte, _PKCS7Range)
//	for i := 0; i < _PKCS7Range; i++ {
//		_PKCS7Table[i] = bytes.Repeat([]byte{byte(i + 1)}, i+1)
//	}
//}
//
//func PKCS7Pack(input []byte, blockSize int) []byte {
//	if blockSize > _PKCS7Range {
//		panic(fmt.Sprintf("PKCS7Pack fail, unsupported block size %v", blockSize))
//	}
//	padding := blockSize - len(input)%blockSize
//	return append(input, _PKCS7Table[padding-1]...)
//}
//
//func PKCS7Unpack(input []byte) ([]byte, error) {
//	if input == nil || len(input) == 0 {
//		return nil, fmt.Errorf("PKCS7Unpack fail, input empty")
//	}
//	iLen := len(input)
//	padding := int(input[iLen-1])
//	if padding > _PKCS7Range {
//		return nil, fmt.Errorf("PKCS7Unpack fail, padding %v out of range", padding)
//	} else if padding > iLen {
//		return nil, fmt.Errorf("PKCS7Unpack fail, padding %v > input %v", padding, iLen)
//	}
//	return input[:iLen-padding], nil
//}
