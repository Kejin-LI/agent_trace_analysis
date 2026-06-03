package expr

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"code.byted.org/bcc/conf_engine/jsonconf"
	json "code.byted.org/bcc/conf_engine/jsoniter"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/gopkg/logs"
)

type formatFunc func(model.JSONFunc, string) (string, error)

var formatFuncMap = map[model.JsonFuncName]formatFunc{
	model.Get:             FormatGet,
	model.BinaryGet:       FormatBinaryGet,
	model.StrToLower:      FormatFuncStrToLower,
	model.StrToUpper:      FormatFuncStrToUpper,
	model.CurrentUnixTime: FormatCurrentUnixTime,
	model.UnixTimeSince:   FormatUnixTimeSince,
	model.ConvertNumber:   FormatConvertNumber,
	model.ConvertString:   FormatConvertString,
	model.Substring:       FormatSubString,
	model.ModBy:           FormatModBy,
	model.SemverCmp:       FormatVersionCmp,
	model.SemverRange:     FormatSemverRange,
	model.RegularMatch:    FormatRegularMatch,
}

func getJSONFuncInfo(funcName model.JsonFuncName) *jsonconf.JSONFuncInfo {
	for _, f := range getJSONFuncList() {
		if f.Name == funcName {
			return f
		}
	}
	return nil
}

var jsonFuncOnce sync.Once

//必须通过该函数获取func list，否则不会初始化
func getJSONFuncList() []*jsonconf.JSONFuncInfo {

	jsonFuncOnce.Do(func() {
		for _, f := range jsonconf.JSONFuncInfoList {
			fc, ok := formatFuncMap[f.Name]
			if !ok {
				logs.Error("expr format FuncMap not found funcName=%s", f.Name)
				panic("cel format FuncMap not found funcName=" + f.Name)
			}
			f.FormatEXPR = fc
		}
	})

	return jsonconf.JSONFuncInfoList
}

func checkArgsLen(f model.JSONFunc, needLen int) error {
	if len(f.Args) != needLen {
		return fmt.Errorf("args%s's len[%d] is invalid for function %s", json.MustMarshal(f.Args), len(f.Args), f.FuncName)
	}

	return nil
}

func FormatFuncStrToUpper(f model.JSONFunc, varName string) (string, error) {
	if len(f.Args) != 0 {
		return "", fmt.Errorf("str_to_upper should not has args. args=%+v", f.Args)
	}

	return fmt.Sprintf(`%s(%s)`, model.StrToUpper, varName), nil
}

func FormatFuncStrToLower(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 0); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s(%s)`, model.StrToLower, varName), nil
}

func FormatConvertNumber(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 0); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s(%s)`, model.ConvertNumber, varName), nil
}

func FormatSubString(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLenRange(f, [2]int{1, 2}); err != nil {
		return "", err
	}

	if err := checkArgsIsInt(f); err != nil {
		return "", err
	}

	if len(f.Args) == 1 {
		return fmt.Sprintf(`%s[%v:]`, varName, f.Args[0]), nil
	} else {

		return fmt.Sprintf(`%s[%v:%v]`, varName, f.Args[0], f.Args[1]), nil
	}
}

func FormatVersionCmp(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 1); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s(%s, %v)`, model.SemverCmp, varName, addDoubleQuote(fmt.Sprintf("%s", f.Args[0]))), nil
}

func FormatModBy(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 1); err != nil {
		return "", err
	}

	if err := checkArgsIsInt(f); err != nil {
		return "", err
	}
	if f.Args[0] == 0 {
		return "", fmt.Errorf("can not mod by 0")
	}

	return fmt.Sprintf(`%s%%%v`, varName, f.Args[0]), nil
}

func FormatConvertString(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 0); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s(%s)`, model.ConvertString, varName), nil
}

func FormatCurrentUnixTime(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 0); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s()`, model.CurrentUnixTime), nil
}

func FormatUnixTimeSince(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 0); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s(%s)`, model.UnixTimeSince, varName), nil
}

//todo get函数default value的类型
func FormatGet(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLenRange(f, [2]int{2, 5}); err != nil {
		return "", err
	}
	var args []string
	for i := 0; i < len(f.Args)-1; i++ {
		args = append(args, f.Args[i].(string))
	}
	defVal := json.MustMarshal(f.Args[len(f.Args)-1])
	//得到k1.k2.k3,defVal这样的形式。逗号前面为key，逗号后面为默认值
	//默认值类型：int,string,float,bool 在函数中会根据类型进行处理
	argStr := strings.Join(args, ".")

	return fmt.Sprintf(`%s(%s, "%s", %s)`, model.Get, varName, argStr, defVal), nil
}

func FormatBinaryGet(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLenRange(f, [2]int{2, 3}); err != nil {
		return "", err
	}

	defVal := addDoubleQuote(json.MustMarshal(f.Args[len(f.Args)-1]))

	return fmt.Sprintf(`%s(%s, %s,%s)`, model.Get, varName, f.Args[0].(string), defVal), nil
}

func FormatSemverRange(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 1); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s(%s, %s)`, model.SemverRange, varName, addDoubleQuote(fmt.Sprintf("%s", f.Args[0]))), nil
}
func FormatRegularMatch(f model.JSONFunc, varName string) (string, error) {
	if err := checkArgsLen(f, 1); err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s matches %s`, varName, addDoubleQuote(fmt.Sprintf("%s", f.Args[0]))), nil
}

func checkArgsLenRange(f model.JSONFunc, lenRange [2]int) error {
	argsNum := len(f.Args)
	if argsNum < lenRange[0] || argsNum > lenRange[1] {
		return fmt.Errorf("args%s's len[%d] is invalid for function %s", json.MustMarshal(f.Args), len(f.Args), f.FuncName)
	}
	return nil
}
func delDoubleQuote(str string) string {
	return strings.Trim(str, `"`)
}
func addDoubleQuote(str string) string {
	return `"` + strings.Trim(str, `"`) + `"`
}
func checkArgsIsInt(f model.JSONFunc) error {

	for _, arg := range f.Args {
		switch arg.(type) {
		case string:
			a := arg.(string)
			if _, err := strconv.Atoi(a); err != nil {
				if _, err = strconv.ParseInt(a, 10, 64); err != nil {
					err = fmt.Errorf("function %s's args[%s] not number", f.FuncName, arg)
					break
				}
			}
		case int, int64:
		default:
			return fmt.Errorf("args%s's type is invalid for function %s", json.MustMarshal(f.Args), f.FuncName)
		}

	}

	return nil

}
