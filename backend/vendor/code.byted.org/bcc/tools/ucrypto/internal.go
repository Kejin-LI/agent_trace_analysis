package ucrypto

var gPadding [128]byte

func init() {
	for i := 0; i < len(gPadding); i++ {
		gPadding[i] = byte(i + 1)
	}
}

//填充
func packPadding(input []byte, blockSize int) []byte {
	if blockSize > len(gPadding) {
		panic("invalid size")
	}
	add := blockSize - len(input)%blockSize
	return append(input, gPadding[:add]...)
}

//取消填充
func unpackPadding(input []byte) []byte {
	add := int(input[len(input)-1])
	return input[:len(input)-add]
}
