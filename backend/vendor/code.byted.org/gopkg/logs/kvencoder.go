package logs

import (
	"bytes"
	"io"

	"code.byted.org/gopkg/logs/utils"
)

// KVEncoder .
type KVEncoder interface {
	io.Writer
	Reset()
	AppendKVs(kvs ...interface{})
	EndRecord()
	Bytes() []byte
	String() string
}

// TTLogKVEncoder .
// Key(VLen)=Val
//  Name(3)=zyj
type TTLogKVEncoder struct {
	buf *bytes.Buffer
}

// NewTTLogKVEncoder .
func NewTTLogKVEncoder() *TTLogKVEncoder {
	return &TTLogKVEncoder{
		buf: new(bytes.Buffer),
	}
}

func (tte *TTLogKVEncoder) Write(p []byte) (n int, err error) {
	return tte.buf.Write(p)
}

func (tte *TTLogKVEncoder) WriteString(p string) (n int, err error) {
	return tte.buf.WriteString(p)
}

// Reset .
func (tte *TTLogKVEncoder) Reset() {
	tte.buf.Reset()
}

// Bytes .
func (tte *TTLogKVEncoder) Bytes() []byte {
	return tte.buf.Bytes()
}

// String .
func (tte *TTLogKVEncoder) String() string {
	return tte.buf.String()
}

// AppendKVs .
func (tte *TTLogKVEncoder) AppendKVs(kvs ...interface{}) {
	for i := 0; i+1 < len(kvs); i += 2 {
		k := kvs[i]
		v := kvs[i+1]
		tte.buf.WriteString(utils.Value2Str(k))
		tte.buf.WriteByte(equalByte)
		tte.buf.WriteString(utils.Value2Str(v))
		tte.buf.Write(spaceBytes)
	}
}

// EndRecord .
func (tte *TTLogKVEncoder) EndRecord() {
	tte.buf.WriteByte(newlineByte)
}

var (
	equalByte   = byte('=')
	newlineByte = byte('\n')
)
