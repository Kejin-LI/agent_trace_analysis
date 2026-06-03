package uconv

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

//类型转换：interface->string //比直接调用strconv慢11%，比fmt快2倍
func ToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case bool:
		return strconv.FormatBool(v)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 64)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case json.Number:
		return v.String()
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	}
	return fmt.Sprintf("%v", value)
}

//类型转换：interface->[]byte
func ToBytes(value interface{}) []byte {
	if b, ok := value.([]byte); ok {
		return b
	}
	return String2Bytes(ToString(value))
}

//类型转换：interface->int //比直接调用strconv慢55%~300% //效率低不建议用
func ToInt(value interface{}) int {
	r, _ := ToIntEx(value)
	return r
}

func ToIntEx(value interface{}) (int, error) {
	switch v := value.(type) {
	case string:
		return strconv.Atoi(v)
	case []byte:
		return strconv.Atoi(string(v))
	case bool:
		if v {
			return 1, nil
		} else {
			return 0, nil
		}
	case float32:
		return int(v), nil
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		if v >= math.MaxInt64 {
			return 0, fmt.Errorf("invalid_range")
		} else {
			return int(v), nil
		}
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		if v >= math.MaxInt64 {
			return 0, fmt.Errorf("invalid_range")
		} else {
			return int(v), nil
		}
	case json.Number:
		res, err := v.Int64()
		return int(res), err
	}
	return 0, fmt.Errorf("invalid_type")
}

//类型转换：interface->bool //效率应该ok
func ToBool(value interface{}) bool {
	r, _ := ToBoolEx(value)
	return r
}

func ToBoolEx(value interface{}) (bool, error) {
	switch v := value.(type) {
	case string:
		return strconv.ParseBool(v)
	case []byte:
		return strconv.ParseBool(string(v))
	case bool:
		return v, nil
	case float32:
		return v != 0, nil
	case float64:
		return v != 0, nil
	case int:
		return v != 0, nil
	case int8:
		return v != 0, nil
	case int16:
		return v != 0, nil
	case int32:
		return v != 0, nil
	case int64:
		return v != 0, nil
	case uint:
		return v != 0, nil
	case uint8:
		return v != 0, nil
	case uint16:
		return v != 0, nil
	case uint32:
		return v != 0, nil
	case uint64:
		return v != 0, nil
	case json.Number:
		return strconv.ParseBool(string(v))
	}
	return false, fmt.Errorf("invalid_type")
}

//类型转换：interface->bool //待定
func ToFloat(value interface{}) float64 {
	r, _ := ToFloatEx(value)
	return r
}

func ToFloatEx(value interface{}) (float64, error) {
	switch v := value.(type) {
	case string:
		return strconv.ParseFloat(v, 64)
	case []byte:
		return strconv.ParseFloat(string(v), 64)
	case bool:
		if v {
			return 1, nil
		} else {
			return 0, nil
		}
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	}
	return 0, fmt.Errorf("invalid_type")
}
