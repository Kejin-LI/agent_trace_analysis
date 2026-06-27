package ucompress

import (
	"bytes"
	"io"
	"io/ioutil"
	"sync"

	"code.byted.org/gopkg/logs"
	"github.com/klauspost/compress/zstd"
)

type Zstd struct {
	level            zstd.EncoderLevel
	compressBufPool  sync.Pool
	compressPool     sync.Pool
	decompresBufPool sync.Pool
	decompressPool   sync.Pool
}

//
func NewZstd(levels ...zstd.EncoderLevel) *Zstd {
	level := zstd.SpeedDefault
	if len(levels) == 1 {
		level = levels[0]
	}

	t := &Zstd{
		level: level,
	}
	t.compressBufPool.New = func() interface{} {
		return bytes.NewBuffer(nil)
	}
	t.compressPool.New = func() interface{} {
		if enc, err := zstd.NewWriter(ioutil.Discard, zstd.WithEncoderLevel(level)); err != nil {
			logs.Error("compressPool new err=%v", err)
			return err
		} else {
			return enc
		}
	}

	t.decompresBufPool.New = func() interface{} {
		return bytes.NewBuffer(nil)
	}

	t.decompressPool.New = func() interface{} {
		if dec, err := zstd.NewReader(nil); err != nil {
			logs.Error("decompressPool new err=%v", err)
			return err
		} else {
			return dec
		}
	}

	return t
}

//
func (t *Zstd) Compress(src []byte) ([]byte, error) {
	raw := t.compressPool.Get()
	if err, ok := raw.(error); ok {
		return nil, err
	}
	enc := raw.(*zstd.Encoder)
	buf := t.compressBufPool.Get().(*bytes.Buffer)
	enc.Reset(buf)
	defer func() {
		buf.Reset()
		t.compressPool.Put(enc)
		t.compressBufPool.Put(buf)
	}()

	if _, err := enc.Write(src); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}

func (t *Zstd) Decompress(src []byte) ([]byte, error) {
	raw := t.decompressPool.Get()
	if err, ok := raw.(error); ok {
		return nil, err
	}

	dec := raw.(*zstd.Decoder)
	buf := t.decompresBufPool.Get().(*bytes.Buffer)

	defer func() {
		buf.Reset()
		t.decompresBufPool.Put(buf)
		t.decompressPool.Put(dec)
	}()

	in := bytes.NewReader(src)
	if err := dec.Reset(in); err != nil {
		return nil, err
	}

	if _, err := io.Copy(buf, dec); err != nil {
		return nil, err
	}

	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}

//------------------------------------------------------------
//zstd压缩
func ZstdCompress(src []byte) ([]byte, error) {
	return getZstdDef().Compress(src)
}

//zstd解压
func ZstdDecompress(src []byte) ([]byte, error) {
	return getZstdDef().Decompress(src)
}

func getZstdDef() *Zstd {
	zstdOnce.Do(func() {
		zstdDef = NewZstd(zstd.SpeedFastest)
	})
	return zstdDef
}

var zstdDef *Zstd
var zstdOnce = sync.Once{}
