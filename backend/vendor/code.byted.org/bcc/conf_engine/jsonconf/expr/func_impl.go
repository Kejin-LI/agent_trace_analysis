package expr

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	json "code.byted.org/bcc/conf_engine/jsoniter"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/gopkg/logs"

	"github.com/blang/semver/v4"
)

var funcEnv = map[string]interface{}{
	string(model.StrToUpper):      strToUpper,
	string(model.Get):             get,
	string(model.StrToLower):      strToLower,
	string(model.CurrentUnixTime): currentUnixTime,
	string(model.UnixTimeSince):   unixTimeSince,
	string(model.ConvertNumber):   convertNumber,
	string(model.ConvertString):   convertString,
	string(model.SemverCmp):       semverCmp,
	string(model.SemverRange):     semVerRange,
}

func strToUpper(varName string) string {
	return strings.ToUpper(varName)
}

func strToLower(varName string) string {
	return strings.ToLower(varName)
}

func convertNumber(varName string) (int64, error) {
	i, err := strconv.ParseInt(varName, 10, 64)
	if err != nil {
		return 0, err
	}
	return i, nil
}
func semverCmp(left string, right string) (int, error) {
	lv, leftErr := semver.Make(left)
	if leftErr != nil {
		return 0, fmt.Errorf("invalid value semver_cmp(%v, %v), err:%v", left, right, leftErr)
	}
	rv, rightErr := semver.Make(right)
	if rightErr != nil {
		return 0, fmt.Errorf("invalid value semver_cmp(%v, %v), err:%v", left, right, rightErr)
	}

	return lv.Compare(rv), nil
}

func semVerRange(left string, right string) (bool, error) {
	lv, err := semver.Parse(left)
	if err != nil {

		return false, fmt.Errorf("invalid value semver_range(%v, %v) err:%v", left, right, err)
	}

	expectedRange, err := semver.ParseRange(right)
	if err != nil {
		return false, fmt.Errorf("invalid value semver_range(%v, %v) err:%v", left, right, err)
	}

	return expectedRange(lv), nil
}
func unixTimeSince(val interface{}) int64 {
	return time.Now().Unix() - toInt64(val)
}

func currentUnixTime() int64 {
	return time.Now().Unix()
}
func get(val map[string]interface{}, argStr string, devVal interface{}) (interface{}, error) {
	//argStr是类似 "k1.k2.k3,false"这类字符串
	//args 至少有两个参数。这个是解析的时候就已经限制了

	if strings.Count(argStr, ".") == 0 {
		return getOnePath(val, argStr, devVal), nil
	} else {
		keys := strings.Split(argStr, ".")
		return getManyPath(val, keys, 0, devVal)
	}
}
func getOnePath(amap map[string]interface{}, key string, defVal interface{}) interface{} {
	if val, exist := amap[key]; exist {
		return val
	} else {
		return defVal
	}
}

func getManyPath(amap map[string]interface{}, keys []string, index int, defVal interface{}) (interface{}, error) {
	//fmt.Printf("keys: %v, index = %d\n", keys, index)
	if val, exist := amap[keys[index]]; !exist {
		return defVal, nil
	} else if len(keys) == index+1 { //已经到了最后一个key
		return val, nil
	} else { //继续往下遍历
		newMap, flag := val.(map[string]interface{})
		if !flag {
			return nil, fmt.Errorf("key[%v] val is not a map", keys[0:index+1])
		} else {
			return getManyPath(newMap, keys, index+1, defVal)
		}
	}
}
func convertString(v interface{}) (string, error) {
	str, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("convertString err=%v", err)
	}
	return string(str), nil
}

func parseRetVal(val string) interface{} {
	val = delDoubleQuote(val)
	var ret interface{}
	json.Unmarshal([]byte(val), ret)
	return ret
}

//之所以要用toInt64，是因为expr在调用函数的时候区分int和int64，不能混用。但在编译的时候并不知道到时传进来的类型是int还是int64
//如果类型不对直接执行出错。 为此对于整型直接用interface{}接收参数值，再用toInt64统一转换成int64
func toInt64(val interface{}) (val64 int64) {
	//这里不用reflect，因为耗时比字符串转换要慢
	valStr := fmt.Sprintf("%v", val)
	if v, e := strconv.ParseInt(valStr, 10, 64); e != nil {
		logs.Error("val=[%v],valStr=[%v],err=[%v]", val, valStr, e)
		panic("invalid type")
	} else {
		return v
	}
}
