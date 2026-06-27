package ucompress

import (
	"bytes"
	"compress/bzip2"
	"io/ioutil"
)

//基础库只提供了解压函数
func Bzip2Decompress(src []byte) ([]byte, error) {
	w := bzip2.NewReader(bytes.NewReader(src))
	return ioutil.ReadAll(w)
}
