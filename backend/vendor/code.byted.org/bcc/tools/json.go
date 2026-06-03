package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"

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

//json反序列化（固定结构）
func JsonUnmarshal(input interface{}, output interface{}) error {
	switch input.(type) {
	case []byte:
		return standardApi.Unmarshal(input.([]byte), output)
	case string:
		return standardApi.Unmarshal(String2Bytes(input.(string)), output)
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
		return numberApi.Unmarshal(String2Bytes(input.(string)), output)
	case io.Reader:
		dec := numberApi.NewDecoder(input.(io.Reader))
		return dec.Decode(output)
	default:
		return fmt.Errorf("invalid_%v", reflect.TypeOf(input).Name())
	}
}

//任意类型->json字符串  //logs使用 ToJsonStringer
func ToJson(input interface{}, indent ...bool) string {
	if len(indent) > 0 && indent[0] {
		r, _ := json.MarshalIndent(input, "", "    ")
		return Bytes2String(r)
	} else {
		r, _ := json.Marshal(input)
		return Bytes2String(r)
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
)
