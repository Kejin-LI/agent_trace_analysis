package ucompress

import (
	"bytes"
	"compress/zlib"
	"io"
	"io/ioutil"
	"sync"

	"code.byted.org/gopkg/logs"
)

type Zlib struct {
	level        int
	bufPool      sync.Pool
	compressPool sync.Pool
}

//
func NewZlib(levels ...int) *Zlib {
	level := zlib.DefaultCompression
	if len(levels) == 1 {
		level = levels[0]
	}

	t := &Zlib{
		level: level,
	}
	t.bufPool.New = func() interface{} {
		return bytes.NewBuffer(nil)
	}
	t.compressPool.New = func() interface{} {
		w, err := zlib.NewWriterLevel(ioutil.Discard, t.level)
		if err != nil {
			logs.Error("compressPool new err=%v", err) //todo
			return err
		}
		return w
	}
	return t
}

//
func (t *Zlib) Compress(src []byte) ([]byte, error) {
	raw := t.compressPool.Get()
	if err, ok := raw.(error); ok {
		return nil, err
	}
	w := raw.(*zlib.Writer)
	buf := t.bufPool.New().(*bytes.Buffer)
	w.Reset(buf)
	defer func() {
		buf.Reset()
		t.compressPool.Put(w)
		t.bufPool.Put(buf)
	}()

	_, err := w.Write(src)
	if err != nil {
		return nil, err
	}
	err = w.Close()
	if err != nil {
		return nil, err
	}

	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())

	return out, nil
}

func (t *Zlib) Decompress(compressSrc []byte) ([]byte, error) {
	b := bytes.NewReader(compressSrc)

	var out bytes.Buffer
	r, err := zlib.NewReader(b)
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(&out, r)
	if err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

//------------------------------------------------------------
//zlib压缩
func ZlibCompress(src []byte) ([]byte, error) {
	return getZlibDefault().Compress(src)
}

//zlib解压
func ZlibDecompress(src []byte) ([]byte, error) {
	return getZlibDefault().Decompress(src)
}

func getZlibDefault() *Zlib {
	zlibOnce.Do(func() {
		zlibDefault = NewZlib()
	})
	return zlibDefault
}

var zlibDefault *Zlib
var zlibOnce = sync.Once{}
