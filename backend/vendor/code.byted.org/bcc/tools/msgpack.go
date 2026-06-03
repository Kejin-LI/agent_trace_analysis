package tools

import (
	"fmt"
	"io"
	"reflect"

	"github.com/vmihailenco/msgpack/v5"
)

func MsgPackMarshal(input interface{}) ([]byte, error) {
	return msgpack.Marshal(input)
}

func MsgPackUnmarshal(input interface{}, output interface{}) error {
	switch in := input.(type) {
	case []byte:
		return msgpack.Unmarshal(in, output)
	case string:
		return msgpack.Unmarshal(String2Bytes(in), output)
	case io.Reader:
		dec := msgpack.NewDecoder(in)
		return dec.Decode(output)
	default:
		return fmt.Errorf("invalid_%v", reflect.TypeOf(input).Name())
	}
}
