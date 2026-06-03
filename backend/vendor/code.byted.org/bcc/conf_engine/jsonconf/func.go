package jsonconf

import (
	"context"
	"fmt"

	"code.byted.org/bcc/conf_engine/model"
)

type JSONFuncParamInfo struct {
	HasDefParam  bool          `json:"has_default_param"` //是否存在默认参数
	MinParamsNum int           `json:"min_params_num"`    //不包含默认参数的最小个数
	MaxParamsNum int           `json:"max_params_num"`    //不包含默认参数的最大个数
	ParamType    model.VarType `json:"param_type"`        //所有参数统一的取值类型
	ParamDesc    string        `json:"param_desc"`        //参数描述
}

type JSONFuncInfo struct {
	Name          model.JsonFuncName                                                        `json:"name"`
	FuncDesc      string                                                                    `json:"func_desc"`
	FuncType      model.JSONFuncType                                                        `json:"func_type"`
	HasParams     bool                                                                      `json:"has_params"`
	ParamsInfo    JSONFuncParamInfo                                                         `json:"params_info"`
	HasReturnType bool                                                                      `json:"has_return_type"`
	FormatCEL     func(context.Context, model.JSONFunc, string) (string, error)             `json:"-"` //在cel模块里进行注入
	FormatEXPR    func(model.JSONFunc, string) (string, error)                              `json:"-"` //在expr模块注入
	FilterType    model.VarType                                                             `json:"filter_type"`
	ReturnType    model.VarType                                                             `json:"return_type"`
	CustomCel     bool                                                                      `json:"-"` //是否自定义表达式
	VarCheck      func(context.Context, model.JSONFunc, map[string]model.JSONVarInfo) error `json:"-"` //参数变量校验
}

var JSONFuncInfoList = []*JSONFuncInfo{
	{
		Name:      model.Get,
		FuncDesc:  `实现输入的dict变量的get接口，参数key为常量字符串`,
		FuncType:  model.JSONFuncInstance,
		HasParams: true,
		ParamsInfo: JSONFuncParamInfo{
			HasDefParam:  true,
			MinParamsNum: 1,
			MaxParamsNum: 4,
			ParamType:    model.VarTypeString,
			ParamDesc:    `例如，输入a和b，表示获取$dict_var["a"]["b"]的value`,
		},
		HasReturnType: true,
		FilterType:    model.VarTypeDict,
		ReturnType:    model.VarTypeUnknown,
	},
	{
		Name:      model.BinaryGet, // get: a[b], a and b are both variables
		FuncDesc:  `实现输入的dict变量的get接口，且参数key为条件变量`,
		FuncType:  model.JSONFuncInstance,
		HasParams: true,
		ParamsInfo: JSONFuncParamInfo{
			HasDefParam:  true,
			MinParamsNum: 1,
			MaxParamsNum: 1,
			ParamType:    model.VarTypeString,
			ParamDesc:    `输入必须为已定义的条件变量，例如，输入a，表示获取$dict_var[a]的value`,
		},
		HasReturnType: true,
		FilterType:    model.VarTypeDict,
		ReturnType:    model.VarTypeUnknown,
		VarCheck:      BinaryGetVarCheck,
	},
	{
		Name:          model.StrToLower,
		FuncDesc:      `实现输入的string变量转成小写`,
		FuncType:      model.JSONFuncInstance,
		HasParams:     false,
		HasReturnType: false,
		FilterType:    model.VarTypeString,
		ReturnType:    model.VarTypeString,
	},
	{
		Name:          model.StrToUpper,
		FuncDesc:      `实现输入的string变量转成大写`,
		FuncType:      model.JSONFuncInstance,
		HasParams:     false,
		HasReturnType: false,
		FilterType:    model.VarTypeString,
		ReturnType:    model.VarTypeString,
	},
	{
		Name:          model.CurrentUnixTime,
		FuncDesc:      `获取当前Unix时间戳`,
		FuncType:      model.JSONFuncNormal,
		HasParams:     false,
		HasReturnType: false,
		FilterType:    model.VarTypeUnknown,
		ReturnType:    model.VarTypeInt,
		CustomCel:     true,
	},
	{
		Name:          model.UnixTimeSince,
		FuncDesc:      `获取相对参数的Unix时间戳`,
		FuncType:      model.JSONFuncNormal,
		HasParams:     false,
		HasReturnType: false,
		FilterType:    model.VarTypeUnknown,
		ReturnType:    model.VarTypeInt,
	},
	{
		Name:          model.ConvertNumber,
		FuncDesc:      `实现输入的string变量转成数字`,
		FuncType:      model.JSONFuncInstance,
		HasParams:     false,
		HasReturnType: false,
		FilterType:    model.VarTypeString,
		ReturnType:    model.VarTypeInt,
	},
	{
		Name:          model.ConvertString,
		FuncDesc:      `实现输入的整形变量转成字符串`,
		FuncType:      model.JSONFuncNormal,
		HasParams:     false,
		HasReturnType: false,
		FilterType:    model.VarTypeInt,
		ReturnType:    model.VarTypeString,
		CustomCel:     true,
	},
	{
		Name:      model.Substring,
		FuncDesc:  `输入的string变量获取子串，格式：str[start_pos, end_pos]，end_pos可不填`,
		FuncType:  model.JSONFuncInstance,
		HasParams: true,
		ParamsInfo: JSONFuncParamInfo{
			HasDefParam:  false,
			MinParamsNum: 1,
			MaxParamsNum: 2,
			ParamType:    model.VarTypeInt,
			ParamDesc:    `参数(start_pos, end_pos), end_pos可不填`,
		},
		HasReturnType: false,
		FilterType:    model.VarTypeString,
		ReturnType:    model.VarTypeString,
	},
	{
		Name:      model.ModBy,
		FuncDesc:  `输入的整形变量取模`,
		FuncType:  model.JSONFuncInstance,
		HasParams: true,
		ParamsInfo: JSONFuncParamInfo{
			HasDefParam:  false,
			MinParamsNum: 1,
			MaxParamsNum: 1,
			ParamType:    model.VarTypeInt,
			ParamDesc:    `取模参数`,
		},
		HasReturnType: false,
		FilterType:    model.VarTypeInt,
		ReturnType:    model.VarTypeInt,
		CustomCel:     true,
	},
	{
		Name:      model.SemverCmp,
		FuncDesc:  `实现Semantic Version的大小比较; 0:相等, 1:大于, -1:小于`,
		FuncType:  model.JSONFuncInstance,
		HasParams: true,
		ParamsInfo: JSONFuncParamInfo{
			HasDefParam:  false,
			MinParamsNum: 1,
			MaxParamsNum: 1,
			ParamType:    model.VarTypeString,
			ParamDesc:    `需要比对的版本参数，例如，2.0.0`,
		},
		HasReturnType: false,
		FilterType:    model.VarTypeString,
		ReturnType:    model.VarTypeInt,
	},
	{
		Name:      model.SemverRange,
		FuncDesc:  `实现Semantic Version是否满足range规则判断`,
		FuncType:  model.JSONFuncInstance,
		HasParams: true,
		ParamsInfo: JSONFuncParamInfo{
			HasDefParam:  false,
			MinParamsNum: 1,
			MaxParamsNum: 1,
			ParamType:    model.VarTypeString,
			ParamDesc:    `range规则参数，例如，>1.0.0`,
		},
		HasReturnType: false,
		FilterType:    model.VarTypeString,
		ReturnType:    model.VarTypeBool,
	},
	{
		Name:      model.RegularMatch,
		FuncDesc:  `正则匹配`,
		FuncType:  model.JSONFuncInstance,
		HasParams: true,
		ParamsInfo: JSONFuncParamInfo{
			HasDefParam:  false,
			MinParamsNum: 1,
			MaxParamsNum: 1,
			ParamType:    model.VarTypeString,
			ParamDesc:    `正则表达式`,
		},
		HasReturnType: false,
		FilterType:    model.VarTypeString,
		ReturnType:    model.VarTypeBool,
	},
}

func GetJSONFuncInfo(funcName model.JsonFuncName) *JSONFuncInfo {
	for _, f := range JSONFuncInfoList {
		if f.Name == funcName {
			return f
		}
	}
	return nil
}

func BinaryGetVarCheck(ctx context.Context, f model.JSONFunc, vMap map[string]model.JSONVarInfo) error {
	if len(f.Args) != 2 {
		return fmt.Errorf("[binary_get] invalid args size")
	}
	varOfArg0, ok := f.Args[0].(string)
	if !ok {
		return fmt.Errorf("[binary_get] invalid args[0], not string type")
	}
	_, found := vMap[varOfArg0]
	if !found {
		return fmt.Errorf("[binary_get] var of args[0] not found, var_name=%+v", f.Args[0])
	}
	return nil
}
