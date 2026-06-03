package mask_pack

import "encoding/json"

type JSONMarshaller func(v interface{}) ([]byte, error)

func SensitiveJsonMarshal(v interface{}) ([]byte, error) {
	return SensitiveJsonMarshalCustom(v, json.Marshal)
}

func SensitiveJsonMarshalCustom(v interface{}, fn JSONMarshaller) ([]byte, error) {
	v = MaskInterface(v)
	return fn(v)
}
