package utils

import (
	"encoding/json"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"strings"
)

// todo: 保留位置/格式

// 数据是否为 json 格式
func IsJson(data []byte) bool {
	return jsoniter.Valid(data)
}

// 解析嵌套json
func ParseNestedJson(data string) map[string][]string {
	kvList := make(map[string][]string)

	cd := ContentDocker{
		KvList: kvList,
	}

	var err error
	var records map[string]interface{}

	records, err = JsonDecode(data)
	if err == nil {
		flattenJson(&cd, records, "")
	} else {
		cd.KvList["data"] = []string{data}
	}

	return cd.KvList
}

// kv_list, data, father/self key name, father param, self param
func flattenJson(cd *ContentDocker, content interface{}, name string) {
	switch content.(type) {
	case map[string]interface{}:
		for key, value := range content.(map[string]interface{}) {
			flattenJson(cd, value, key)
		}
	case []interface{}:
		for _, c := range content.([]interface{}) {
			flattenJson(cd, c, name)
		}
	default:
		contentString := fmt.Sprintf("%+v", content)
		data, err := JsonDecode(contentString)
		if err == nil {
			flattenJson(cd, data, name)
		} else {
			_, ok := cd.KvList[name]
			if ok {
				cd.KvList[name] = append(cd.KvList[name], contentString)
			} else {
				cd.KvList[name] = []string{contentString}
			}
		}
	}
}

type ContentDocker struct {
	KvList map[string][]string
}

// 解析 json
func JsonDecode(data string) (map[string]interface{}, error) {
	if strings.HasPrefix(data, "[") {
		data = fmt.Sprintf("{\"data\":%v}", data)
	}
	d := json.NewDecoder(strings.NewReader(data))
	d.UseNumber()
	var result map[string]interface{}
	err := d.Decode(&result)
	return result, err
}
