package model

type JSONOpType int

const (
	JSONOpTypeEQ     JSONOpType = 1
	JSONOpTypeNEQ    JSONOpType = 2
	JSONOpTypeGT     JSONOpType = 3
	JSONOpTypeGE     JSONOpType = 4
	JSONOpTypeLT     JSONOpType = 5
	JSONOpTypeLE     JSONOpType = 6
	JSONOpTypeIN     JSONOpType = 7
	JSONOpTypeNIN    JSONOpType = 8
	JSONOpTypeRange  JSONOpType = 9
	JSONOpTypeLVFunc JSONOpType = 10 // 忽略op和rv，func(lv)
)

type JSONScope int

const (
	JSONGlobal JSONScope = 1
	JSONLocal  JSONScope = 2
)

type JSONFuncType int

const (
	JSONFuncNormal   JSONFuncType = 1
	JSONFuncInstance JSONFuncType = 2
)

type JsonFuncName string

const (
	Get             = JsonFuncName("get")
	BinaryGet       = JsonFuncName("binary_get")
	StrToLower      = JsonFuncName("str_to_lower")
	StrToUpper      = JsonFuncName("str_to_upper")
	CurrentUnixTime = JsonFuncName("cur_unix_time")
	UnixTimeSince   = JsonFuncName("unix_time_since")
	ConvertNumber   = JsonFuncName("convert_number")
	ConvertString   = JsonFuncName("to_string")
	Substring       = JsonFuncName("substring")
	ModBy           = JsonFuncName("mod_by")
	SemverCmp       = JsonFuncName("semver_cmp")
	SemverRange     = JsonFuncName("semver_range")
	RegularMatch    = JsonFuncName("regular_match")
)

type JSONNode struct {
	Title    string      `json:"title"`
	Children []*JSONNode `json:"children"`
	Cond     *CondNode   `json:"cond"`
	Conf     *ConfNode   `json:"conf"` //叶子结点才有
}

type CondNode struct {
	CondMatrix [][]*JSONCond `json:"cond_matrix"` // CondMatrix行与行代表&&，同一行的列代表||

}

type ConfNode struct {
	OriText string `json:"origin_txt"` //配置内容
}

// 展示diff用，序列化会忽略cel字段。去掉一些字段。
// 需要和JSONNode同步修改
type JSONNodeForShowDiff struct {
	Title    string                 `json:"title"`
	Children []*JSONNodeForShowDiff `json:"children"`
	Cond     CondNodeForShowDiff    `json:"cond"`
	Conf     ConfNodeForShowDiff    `json:"conf"`
}

type CondNodeForShowDiff struct {
	CondMatrix [][]*JSONCond `json:"cond_matrix"` // 和CondList是二选一的策略，如果ConfMatrix非空，优先使用ConfMatrix，否则使用ConfList
}

type ConfNodeForShowDiff struct {
	OriText string `json:"origin_txt"`
}

type JSONFunc struct {
	FuncName JsonFuncName  `json:"name"`
	Args     []interface{} `json:"args"`
	RetType  VarType       `json:"ret_type"` //返回值类型
}

type JSONCond struct {
	LV   string      `json:"lv"`   // 条件变量名字
	OP   JSONOpType  `json:"op"`   // 条件操作符
	RV   interface{} `json:"rv"`   // 操作符右值
	Func JSONFunc    `json:"func"` //作用于运算符左侧的函数
}

type JSONVarInfo struct {
	Scope JSONScope
	Type  VarType
}
