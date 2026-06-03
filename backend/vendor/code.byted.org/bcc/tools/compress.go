package tools

import (
	"code.byted.org/bcc/tools/ucompress"
)

//gzip
func NewGzip(levels ...int) *ucompress.Gzip {
	return ucompress.NewGzip(levels...)
}

func GzipCompress(src []byte) ([]byte, error) {
	return ucompress.GzipCompress(src)
}

func GzipCompressWithoutCompressPattern(src []byte) ([]byte, error) {
	return ucompress.GzipCompressWithoutCompressPattern(src)
}

func GzipDecompress(src []byte) ([]byte, error) {
	return ucompress.GzipDecompress(src)
}

//bzip2
func Bzip2Decompress(src []byte) ([]byte, error) {
	return ucompress.Bzip2Decompress(src)
}

//flate
func NewFlate(levels ...int) *ucompress.Flate {
	return ucompress.NewFlate(levels...)
}

func FlateCompress(src []byte) ([]byte, error) {
	return ucompress.FlateCompress(src)
}

func FlateDecompress(src []byte) ([]byte, error) {
	return ucompress.FlateDecompress(src)
}

//zlib
func NewZlib(levels ...int) *ucompress.Zlib {
	return ucompress.NewZlib(levels...)
}

func ZlibCompress(src []byte) ([]byte, error) {
	return ucompress.ZlibCompress(src)
}

func ZlibDecompress(src []byte) ([]byte, error) {
	return ucompress.ZlibDecompress(src)
}

func ZstdCompress(src []byte) (dst []byte, err error) {
	return ucompress.ZstdCompress(src)
}

func ZstdDeCompress(src []byte) (dst []byte, err error) {
	return ucompress.ZstdDecompress(src)
}
