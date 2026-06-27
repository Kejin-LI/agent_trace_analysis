package ucompress

import (
	"bytes"
	"compress/flate"
	"io/ioutil"
	"sync"

	"code.byted.org/gopkg/logs"
)

type Flate struct {
	level        int
	bufPool      sync.Pool
	compressPool sync.Pool
}

//
func NewFlate(levels ...int) *Flate {
	level := flate.DefaultCompression
	if len(levels) == 1 {
		level = levels[0]
	}

	t := &Flate{
		level: level,
	}
	t.bufPool.New = func() interface{} {
		return bytes.NewBuffer(nil)
	}
	t.compressPool.New = func() interface{} {
		w, err := flate.NewWriter(ioutil.Discard, t.level)
		if err != nil {
			logs.Error("compressPool new err=%v", err)
			return err
		}
		return w
	}
	return t
}

//
func (t *Flate) Compress(src []byte) ([]byte, error) {
	raw := t.compressPool.Get()
	if err, ok := raw.(error); ok {
		return nil, err
	}

	w := raw.(*flate.Writer)
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
	//todo flush?
	err = w.Close()
	if err != nil {
		return nil, err
	}

	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())

	return out, nil
}

func (t *Flate) Decompress(src []byte) ([]byte, error) {
	reader := flate.NewReader(bytes.NewReader(src))
	defer reader.Close()
	return ioutil.ReadAll(reader) //todo pool
}

//------------------------------------------------------------
//flate压缩
func FlateCompress(src []byte) ([]byte, error) {
	return getFlateDefault().Compress(src)
}

//flate解压
func FlateDecompress(src []byte) ([]byte, error) {
	return getFlateDefault().Decompress(src)
}

func getFlateDefault() *Flate {
	flateOnce.Do(func() {
		flateDefault = NewFlate()
	})
	return flateDefault
}

var flateDefault *Flate
var flateOnce = sync.Once{}
