package cel

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"code.byted.org/bcc/conf_engine/jsonconf"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/gopkg/logs"
)

type formatFunc func(context.Context, model.JSONFunc, string) (string, error)

var formatFuncMap = map[model.JsonFuncName]formatFunc{
	model.Get:             FuncFormatGet,
	model.BinaryGet:       FuncFormatBinaryGet,
	model.StrToLower:      FuncFormatStrToLower,
	model.StrToUpper:      FuncFormatStrToUpper,
	model.CurrentUnixTime: FuncFormatCurrentUnixTime,
	model.UnixTimeSince:   FuncFormatUnixTimeSince,
	model.ConvertNumber:   FuncFormatConvertNumber,
	model.ConvertString:   FuncFormatConvertString,
	model.Substring:       FuncFormatSubstring,
	model.ModBy:           FuncFormatModBy,
	model.SemverCmp:       FuncFormatSemverCmp,
	model.SemverRange:     FuncFormatSemverRange,
	model.RegularMatch:    FuncFormatRegularMatch,
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
				logs.Error("cel format FuncMap not found funcName=%s", f.Name)
				panic("cel format FuncMap not found funcName=" + f.Name)
			}
			f.FormatCEL = fc
		}
	})

	return jsonconf.JSONFuncInfoList
}

func FuncFormatGet(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l < 2 || l > 5 { // get_1 ... get_4
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := fmt.Sprintf("get_%d", l-1)
	args := []string{}
	var typ model.VarType
	for i, arg := range f.Args {
		// 最后一个arg为默认值，类型为retType，其余参数为string
		typ = model.VarTypeString
		if i == l-1 {
			typ = f.RetType
		}
		argStr, err := FormatConst(ctx, arg, typ, -1)
		if err != nil {
			return "", fmt.Errorf("FormatConst error: %v", err)
		}
		args = append(args, argStr)
	}
	return fmt.Sprintf("%v(%s)", funcName, strings.Join(args, ",")), nil
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

func FuncFormatBinaryGet(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 2 { // get_1
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := fmt.Sprintf("get_%d", l-1)
	args := []string{}
	var typ model.VarType
	for i, arg := range f.Args {
		if i == 0 {
			argStr := fmt.Sprintf("%v%v", GlobalVarPrefix, arg)
			args = append(args, argStr)
			continue
		}
		// 最后一个arg为默认值，类型为retType，其余参数为string
		typ = model.VarTypeString
		if i == l-1 {
			typ = f.RetType
		}
		argStr, err := FormatConst(ctx, arg, typ, -1)
		if err != nil {
			return "", fmt.Errorf("FormatConst error: %v", err)
		}
		args = append(args, argStr)
	}
	return fmt.Sprintf("%v(%s)", funcName, strings.Join(args, ",")), nil
}

func FuncFormatStrToLower(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 0 { //
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := model.StrToLower
	return fmt.Sprintf("%s()", funcName), nil
}

func FuncFormatStrToUpper(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 0 { //
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := model.StrToUpper
	return fmt.Sprintf("%s()", funcName), nil
}

func FuncFormatConvertNumber(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 0 { //
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := model.ConvertNumber
	return fmt.Sprintf("%s()", funcName), nil
}

func FuncFormatConvertString(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 0 {
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	return fmt.Sprintf("string(%v)", lvText), nil
}

func FuncFormatSubstring(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 1 && l != 2 { //
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := "substring"
	if l == 1 {
		return fmt.Sprintf("%s_no_end(%v)", funcName, f.Args[0]), nil
	}
	return fmt.Sprintf("%s_end(%v, %v)", funcName, f.Args[0], f.Args[1]), nil
}

func FuncFormatModBy(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 1 {
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	return fmt.Sprintf("%s%%%v", lvText, f.Args[0]), nil
}

func FuncFormatUnixTimeSince(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 1 { //
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := "unix_time_since"
	return fmt.Sprintf("%s(%v)", funcName, f.Args[0]), nil
}

func FuncFormatCurrentUnixTime(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 0 {
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := "cur_unix_time"
	return fmt.Sprintf("%s()", funcName), nil
}

func FuncFormatSemverCmp(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 1 {
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := "semver_cmp"
	return fmt.Sprintf("%s(%s)", funcName, addQuote(fmt.Sprintf("%s", f.Args[0]))), nil
}

func FuncFormatSemverRange(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 1 {
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := "semver_range"
	return fmt.Sprintf("%s(%s)", funcName, addQuote(fmt.Sprintf("%s", f.Args[0]))), nil
}

func FuncFormatRegularMatch(ctx context.Context, f model.JSONFunc, lvText string) (string, error) {
	l := len(f.Args)
	if l != 1 {
		return "", fmt.Errorf("invalid args, args=%+v", f.Args)
	}
	funcName := "matches"
	return fmt.Sprintf("%s(%s)", funcName, addQuote(fmt.Sprintf("%s", f.Args[0]))), nil
}
func addQuote(s string) string {
	if strings.Index(s, `"`) == 0 && strings.LastIndex(s, `"`) == len(s)-1 {
		return s
	}
	return strconv.Quote(s)
}
