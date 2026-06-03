package ucompress

import (
	"bytes"
	"compress/gzip"
	"io/ioutil"
	"sync"

	"code.byted.org/gopkg/logs"
)

type Gzip struct {
	level        int
	bufPool      sync.Pool
	compressPool sync.Pool
}

//
func NewGzip(levels ...int) *Gzip {
	level := gzip.DefaultCompression
	if len(levels) == 1 {
		level = levels[0]
	}

	t := &Gzip{
		level: level,
	}
	t.bufPool.New = func() interface{} {
		return bytes.NewBuffer(nil)
	}
	t.compressPool.New = func() interface{} {
		w, err := gzip.NewWriterLevel(ioutil.Discard, level)
		if err != nil {
			logs.Error("compressPool new err=%v", err)
			return err
		}
		return w
	}
	return t
}

//
func (t *Gzip) Compress(src []byte) ([]byte, error) {
	raw := t.compressPool.Get()
	if err, ok := raw.(error); ok {
		return nil, err
	}
	w := raw.(*gzip.Writer)
	buf := t.bufPool.Get().(*bytes.Buffer)
	w.Reset(buf)
	defer func() {
		buf.Reset()
		t.compressPool.Put(w)
		t.bufPool.Put(buf)
	}()

	if _, err := w.Write(src); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}

func (t *Gzip) Decompress(src []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return ioutil.ReadAll(reader) //todo pool
}

//------------------------------------------------------------
//gzip压缩
func GzipCompress(src []byte) ([]byte, error) {
	return getGzipDef().Compress(src)
}

//伪压缩模式。生成一个符合gzip压缩模式的数据，但实际上是没有进行压缩的。用于节省压缩端的cpu
func GzipCompressWithoutCompressPattern(src []byte) ([]byte, error) {
	noCompressGzipOnce.Do(func() {
		noCompressGzipDef = NewGzip(gzip.NoCompression)
	})

	return noCompressGzipDef.Compress(src)
}

//gzip解压
func GzipDecompress(src []byte) ([]byte, error) {
	return getGzipDef().Decompress(src)
}

func getGzipDef() *Gzip {
	gzipOnce.Do(func() {
		gzipDef = NewGzip()
	})
	return gzipDef
}

var gzipDef *Gzip
var gzipOnce = sync.Once{}

//之所以共用一个gzipDef，是因为一个服务可能需要同时需要压缩模式和伪压缩模式的gzipDef。此时，由使用方明确指定
var noCompressGzipDef *Gzip
var noCompressGzipOnce = sync.Once{}
