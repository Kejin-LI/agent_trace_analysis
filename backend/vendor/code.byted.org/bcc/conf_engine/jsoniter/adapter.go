package jsoniter

import (
	"encoding/json"
	"io"
	"strconv"
	"unsafe"

	jsoniter2 "github.com/json-iterator/go"
	"github.com/modern-go/reflect2"
)

type myDecoderExtension map[reflect2.Type]jsoniter2.ValDecoder

// UpdateStructDescriptor No-op
func (extension myDecoderExtension) UpdateStructDescriptor(structDescriptor *jsoniter2.StructDescriptor) {
}

// CreateMapKeyDecoder No-op
func (extension myDecoderExtension) CreateMapKeyDecoder(typ reflect2.Type) jsoniter2.ValDecoder {
	return nil
}

// CreateMapKeyEncoder No-op
func (extension myDecoderExtension) CreateMapKeyEncoder(typ reflect2.Type) jsoniter2.ValEncoder {
	return nil
}

// CreateDecoder get decoder from map
func (extension myDecoderExtension) CreateDecoder(typ reflect2.Type) jsoniter2.ValDecoder {
	return extension[typ]
}

// CreateEncoder No-op
func (extension myDecoderExtension) CreateEncoder(typ reflect2.Type) jsoniter2.ValEncoder {
	return nil
}

// DecorateDecoder No-op
func (extension myDecoderExtension) DecorateDecoder(typ reflect2.Type, decoder jsoniter2.ValDecoder) jsoniter2.ValDecoder {
	return decoder
}

// DecorateEncoder No-op
func (extension myDecoderExtension) DecorateEncoder(typ reflect2.Type, encoder jsoniter2.ValEncoder) jsoniter2.ValEncoder {
	return encoder
}

type valDecoder struct{}

func (d valDecoder) Decode(ptr unsafe.Pointer, iter *jsoniter2.Iterator) {
	switch iter.WhatIsNext() {
	case jsoniter2.NumberValue:
		var number json.Number
		iter.ReadVal(&number)
		i, err := strconv.ParseInt(string(number), 10, 64)
		if err == nil {
			*(*interface{})(ptr) = i
			return
		}
		f, err := strconv.ParseFloat(string(number), 64)
		if err == nil {
			*(*interface{})(ptr) = f
			return
		}
		// Not much we can do here.
	default:
		*(*interface{})(ptr) = iter.Read()
	}
}

var configPrivate = jsoniter2.Config{
	EscapeHTML: true,
}.Froze()

func JSONDecodeNumberInit() {
	extension := make(myDecoderExtension)
	extension[reflect2.TypeOfPtr((*interface{})(nil)).Elem()] = valDecoder{}
	configPrivate.RegisterExtension(extension)
}

func init() {
	JSONDecodeNumberInit()
}

func MarshalToString(v interface{}) (string, error) {
	return configPrivate.MarshalToString(v)
}

func Marshal(v interface{}) ([]byte, error) {
	return configPrivate.Marshal(v)
}
func MustMarshal(v interface{}) string {
	res, _ := configPrivate.Marshal(v)
	return string(res)
}

func MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return configPrivate.MarshalIndent(v, prefix, indent)
}

func UnmarshalFromString(str string, v interface{}) error {
	return configPrivate.UnmarshalFromString(str, v)
}

func Unmarshal(data []byte, v interface{}) error {
	return configPrivate.Unmarshal(data, v)
}

func Get(data []byte, path ...interface{}) jsoniter2.Any {
	return configPrivate.Get(data, path...)
}

func NewEncoder(writer io.Writer) *jsoniter2.Encoder {
	return configPrivate.NewEncoder(writer)
}

func NewDecoder(reader io.Reader) *jsoniter2.Decoder {
	return configPrivate.NewDecoder(reader)
}

func Valid(data []byte) bool {
	return configPrivate.Valid(data)
}

func RegisterExtension(extension jsoniter2.Extension) {
	configPrivate.RegisterExtension(extension)
}

func DecoderOf(typ reflect2.Type) jsoniter2.ValDecoder {
	return configPrivate.DecoderOf(typ)
}

func EncoderOf(typ reflect2.Type) jsoniter2.ValEncoder {
	return configPrivate.EncoderOf(typ)
}
