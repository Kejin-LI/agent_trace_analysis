package json

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"unsafe"

	jsoniter "github.com/json-iterator/go"
)

//json序列化（一行）
func JsonMarshal(input interface{}) ([]byte, error) {
	return standardApi.Marshal(input)
}
func JsonMarshalToString(input interface{}) (string, error) {
	return standardApi.MarshalToString(input)
}

//json序列化（格式化）
func JsonMarshalIndent(input interface{}, params ...string) ([]byte, error) {
	if len(params) != 2 {
		//return standardApi.MarshalIndent(input, "", "    ") //不好看
		return json.MarshalIndent(input, "", "    ")
	} else {
		//return standardApi.MarshalIndent(input, params[0], params[1])
		return json.MarshalIndent(input, params[0], params[1])
	}
}

//json序列化（快速）//适用于内部通讯，不要返回给客户端 //比Marshal快一倍
func JsonMarshalUnordered(input interface{}) ([]byte, error) {
	return unorderedApi.Marshal(input)
}
func JsonMarshalToStringUnordered(input interface{}) (string, error) {
	return unorderedApi.MarshalToString(input)
}
func JsonMarshalUnorderedDisableHTML(input interface{}) ([]byte, error) {
	return disableHtmlApi.Marshal(input)
}

//string转换为[]byte (只读的，临时使用，外部保证生命周期）
func string2Bytes(s string) []byte {
	x := (*[2]uintptr)(unsafe.Pointer(&s))
	h := [3]uintptr{x[0], x[1], x[1]}
	//runtime.KeepAlive(&s) //作用有限
	return *(*[]byte)(unsafe.Pointer(&h))
}

//json反序列化（固定结构）
func JsonUnmarshal(input interface{}, output interface{}) error {
	switch input.(type) {
	case []byte:
		return standardApi.Unmarshal(input.([]byte), output)
	case string:
		return standardApi.Unmarshal(string2Bytes(input.(string)), output)
	case io.Reader:
		dec := standardApi.NewDecoder(input.(io.Reader))
		return dec.Decode(output)
	default:
		return fmt.Errorf("invalid_%v", reflect.TypeOf(input).Name())
	}
}

//json反序列化（number）
func JsonUnmarshalWithNumber(input interface{}, output interface{}) error {
	switch input.(type) {
	case []byte:
		return numberApi.Unmarshal(input.([]byte), output)
	case string:
		return numberApi.Unmarshal(string2Bytes(input.(string)), output)
	case io.Reader:
		dec := numberApi.NewDecoder(input.(io.Reader))
		return dec.Decode(output)
	default:
		return fmt.Errorf("invalid_%v", reflect.TypeOf(input).Name())
	}
}

//[]byte转换为string (只读的，临时使用，外部保证生命周期）
func bytes2String(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

//任意类型->json字符串  //logs使用 ToJsonStringer。
func ToJson(input interface{}, indent ...bool) string {
	if len(indent) > 0 && indent[0] {
		r, _ := json.MarshalIndent(input, "", "    ")
		return bytes2String(r)
	} else {
		r, _ := json.Marshal(input)
		return bytes2String(r)
	}
}

//任意类型->json stringer //防止性能损耗，例如 logs.Debug("XXX", ToJson(a)) 就算debug日志不打印但也执行ToJson
func ToJsonStringer(input interface{}, indent ...bool) fmt.Stringer {
	r := jsonStu{input, false}
	if len(indent) > 0 && indent[0] {
		r.indent = true
	}
	return r
}

type jsonStu struct {
	in     interface{}
	indent bool
}

func (t jsonStu) String() string {
	return ToJson(t.in, t.indent)
}

//------------------------------------------------------------------------------
var (
	standardApi = jsoniter.ConfigCompatibleWithStandardLibrary

	numberApi = jsoniter.Config{
		EscapeHTML:             true,
		SortMapKeys:            true,
		ValidateJsonRawMessage: true,
		UseNumber:              true,
	}.Froze()

	unorderedApi = jsoniter.Config{
		EscapeHTML:             true,
		SortMapKeys:            false,
		ValidateJsonRawMessage: true,
	}.Froze()

	disableHtmlApi = jsoniter.Config{
		EscapeHTML:             false,
		SortMapKeys:            false,
		ValidateJsonRawMessage: true,
	}.Froze()
)
