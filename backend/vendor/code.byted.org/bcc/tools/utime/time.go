/***************************************************
	1、特殊转换需要自定义format，不过要先转换为utc或fullstr
	2、format格式： go/time/format.go:stdLongMonth
***************************************************/
package utime

import (
	"strings"
	"time"
)

const (
	formatUtc     = 1486465081            //时间戳，1970年1月1日到现在的秒数
	formatYmd     = "20060102"            //年月日
	formatYmdh    = "2006010215"          //年月日时
	formatYmdhms  = "20060102150405"      //年月日时分秒，只实现utc转换
	formatFullstr = "2006-01-02 15:04:05" //时间日期字符串
	formatDatestr = "2006-01-02"          //日期字符串
	formatTimestr = "15:04:05"            //时间字符串
)

//---------------------- utc -------------------------
//utc --> ymd
func UtcToYmd(utc int) string {
	return time.Unix(int64(utc), 0).Format(formatYmd)
}

//utc -> base
func UtcToBase(utc int) time.Time {
	return time.Unix(int64(utc), 0)
}

//utc --> ymdh
func UtcToYmdh(utc int) string {
	return time.Unix(int64(utc), 0).Format(formatYmdh)
}

//utc -> ymdhms
func UtcToYmdhms(utc int) string {
	return time.Unix(int64(utc), 0).Format(formatYmdhms)
}

//utc --> fullstr
func UtcToFullstr(utc int) string {
	return time.Unix(int64(utc), 0).Format(formatFullstr)
}

//utc -> datestr
func UtcToDatestr(utc int) string {
	return time.Unix(int64(utc), 0).Format(formatDatestr)
}

//utc -> timestr
func UtcToTimestr(utc int) string {
	return time.Unix(int64(utc), 0).Format(formatTimestr)
}

//utc -> format
func UtcToFormat(format string, utc int) string {
	return time.Unix(int64(utc), 0).Format(format)
}

//--------------------- now/offset --------------------------
//now --> utc
func NowUtc(offset ...int) int {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return utc
}

//now --> ymd
func NowYmd(offset ...int) string {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return UtcToYmd(utc)
}

//now --> ymdh
func NowYmdh(offset ...int) string {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return UtcToYmdh(utc)
}

//now --> ymdhms
func NowYmdhms(offset ...int) string {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return UtcToYmdhms(utc)
}

//now --> fullstr
func NowFullstr(offset ...int) string {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return UtcToFullstr(utc)
}

//now -> datestr
func NowDatestr(offset ...int) string {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return UtcToDatestr(utc)
}

//now -> timestr
func NowTimestr(offset ...int) string {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return UtcToTimestr(utc)
}

//now -> format
func NowFormat(format string, offset ...int) string {
	utc := int(time.Now().Unix())
	if len(offset) > 0 {
		utc += offset[0]
	}
	return UtcToFormat(format, utc)
}

//---------------------- fullstr -------------------------
//fullstr -> utc
func FullstrToUtc(str string) int {
	tm, _ := time.ParseInLocation(formatFullstr, str, time.Local)
	return int(tm.Unix())
}

//fullstr --> base
func FullstrToBase(str string) (time.Time, error) {
	return time.ParseInLocation(formatFullstr, str, time.Local)
}

//fullstr -> ymd
func FullstrToYmd(str string) string {
	tm, _ := time.ParseInLocation(formatFullstr, str, time.Local)
	return time.Unix(tm.Unix(), 0).Format(formatYmd)
}

//fullstr --> ymdh
func FullstrToYmdh(str string) string {
	tm, _ := time.ParseInLocation(formatFullstr, str, time.Local)
	return time.Unix(tm.Unix(), 0).Format(formatYmdh)
}

//fullstr --> ymdhms
func FullstrToYmdhms(str string) string {
	tm, _ := time.ParseInLocation(formatFullstr, str, time.Local)
	return time.Unix(tm.Unix(), 0).Format(formatYmdhms)
}

//fullstr --> datestr
func FullstrToDatestr(str string) string {
	d := strings.Split(str, " ")
	if len(d) >= 1 {
		return d[0]
	}
	return ""
}

//fullstr --> timestr
func FullstrToTimestr(str string) string {
	d := strings.Split(str, " ")
	if len(d) >= 2 {
		return d[1]
	}
	return ""
}

//fullstr --> format
func FullstrToFormat(format string, str string) string {
	tm, _ := time.ParseInLocation(formatFullstr, str, time.Local)
	return time.Unix(tm.Unix(), 0).Format(format)
}

//---------------------- datestr -------------------------
//datestr -> utc
func DatestrToUtc(str string) int {
	tm, _ := time.ParseInLocation(formatDatestr, str, time.Local)
	return int(tm.Unix())
}

//datestr --> base
func DatestrToBase(str string) (time.Time, error) {
	return time.ParseInLocation(formatDatestr, str, time.Local)
}

//datestr -> ymd
func DatestrToYmd(str string) string {
	tm, _ := time.ParseInLocation(formatDatestr, str, time.Local)
	return time.Unix(tm.Unix(), 0).Format(formatYmd)
}

//datestr --> ymdh
func DatestrToYmdh(str string) string {
	tm, _ := time.ParseInLocation(formatDatestr, str, time.Local)
	return time.Unix(tm.Unix(), 0).Format(formatYmdh)
}

//datestr --> fullstr
func DatestrToFullstr(str_time string) string {
	return str_time + " 00:00:00"
}

//datestr --> timestr
//不存在转换

//---------------------- timestr -------------------------
//不存在转换

//---------------------- ymd -------------------------
//ymd -> utc
func YmdToUtc(str string) int {
	tm, _ := time.ParseInLocation(formatYmd, str, time.Local)
	return int(tm.Unix())
}

//ymd -> base
func YmdToBase(str string) (time.Time, error) {
	return time.ParseInLocation(formatYmd, str, time.Local)
}

//ymd -> ymdh
func YmdToYmdh(str string) string {
	return str + "00"
}

//ymd -> fullstr
func YmdToFullstr(str string) string {
	return UtcToFullstr(YmdToUtc(str)) //todo待优化
}

//ymd -> datestr
func YmdToDatestr(str string) string {
	return UtcToDatestr(YmdToUtc(str)) //todo待优化
}

//ymd -> timestr
//不存在转换

//---------------------- ymdh -------------------------
//ymdh -> utc
func YmdhToUtc(str string) int {
	tm, _ := time.ParseInLocation(formatYmdh, str, time.Local)
	return int(tm.Unix())
}

//ymdh -> utc
func YmdhToBase(str string) (time.Time, error) {
	return time.ParseInLocation(formatYmdh, str, time.Local)
}

//ymdh -> ymd
func YmdhToYmd(str string) string {
	if len(str) >= 8 {
		return str[:8]
	}
	return str
}

//ymdh -> fullstr
func YmdhToFullstr(str string) string {
	return UtcToFullstr(YmdhToUtc(str)) //todo待优化
}

//ymdh -> datestr
func YmdhToDatestr(str string) string {
	return UtcToDatestr(YmdhToUtc(str)) //todo待优化
}

//ymdh -> timestr
//不存在转换

//---------------------- ymdhms -------------------------
//ymdhms -> utc
func YmdhmsToUtc(str string) int {
	tm, _ := time.ParseInLocation(formatYmdhms, str, time.Local)
	return int(tm.Unix())
}

//ymdhms -> base
func YmdhmsToBase(str string) (time.Time, error) {
	return time.ParseInLocation(formatYmdhms, str, time.Local)
}

//ymdhms -> ymd
func YmdhmsToYmd(str string) string {
	if len(str) >= 8 {
		return str[:8]
	}
	return str
}

//ymdhms -> fullstr
func YmdhmsToFullstr(str string) string {
	return UtcToFullstr(YmdhmsToUtc(str)) //todo待优化
}

//ymdhms -> datestr
func YmdhmsToDatestr(str string) string {
	return UtcToDatestr(YmdhmsToUtc(str)) //todo待优化
}

//ymdhms -> timestr
//不存在转换

//---------------------- utc relate -------------------------
//utc的当前分钟的0秒
func FirstMinute(utc int) int {
	return utc - utc%60
}

//utc的当前小时的0分0秒
func FirstHour(utc int) int {
	return utc - utc%3600
}

func utcToZeroTm(utc int) time.Time {
	str_time := time.Unix(int64(utc), 0).Format(formatDatestr) + " 00:00:00"
	//	loc, _ := time.LoadLocation("Local")
	tm, _ := time.ParseInLocation(formatFullstr, str_time, time.Local)
	return tm
}

//utc的当前日期的0点
func FirstDate(utc int) int {
	tm := utcToZeroTm(utc)
	return int(tm.Unix())
}

//utc的当前星期的周日0点（周日是第一天）
func FirstWeek(utc int) int {
	tm := utcToZeroTm(utc)
	return int(tm.Unix()) - int(tm.Weekday())*86400
}

//utc的当前月份的1号0点
func FirstMonth(utc int) int {
	tm := utcToZeroTm(utc)
	return int(tm.Unix()) - int(tm.Day()-1)*86400
}

//utc的当前年份的1月1日0点
func FirstYear(utc int) int {
	tm := utcToZeroTm(utc)
	return int(tm.Unix()) - int(tm.YearDay()-1)*86400
}

//utc是否同一天
func SameDate(utc1 int, utc2 int) bool {
	return FirstDate(utc1) == FirstDate(utc2)
}

//utc是否同一周
func SameWeek(utc1 int, utc2 int) bool {
	return FirstWeek(utc1) == FirstWeek(utc2)
}

//utc是否同一月
func SameMonth(utc1 int, utc2 int) bool {
	return FirstMonth(utc1) == FirstMonth(utc2)
}

//utc是否同一年
func SameYear(utc1 int, utc2 int) bool {
	return FirstYear(utc1) == FirstYear(utc2)
}

//-------------------------------------
func Str(ti time.Time) string {
	utc := int(ti.Unix())
	if utc <= 10 {
		return ""
	} else {
		return UtcToFullstr(int(utc))
	}
}
