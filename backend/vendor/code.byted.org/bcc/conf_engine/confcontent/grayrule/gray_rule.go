package grayrule

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"

	exprservice "code.byted.org/bcc/conf_engine/jsonconf/expr"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/bcc/conf_engine/service"
	"code.byted.org/bcc/conf_engine/service/expr"
)

type GrayRuleType int

const (
	GrayRuleTypeRate      GrayRuleType = 1 // 放量
	GrayRuleTypeWhiteList GrayRuleType = 2 // 白名单
)

// 白名单灰度与流量灰度规则
//新旧SDK都会读取，旧SDK使用celgo解析，新SDK使用expr解析
type GrayRule struct {
	RuleType GrayRuleType `json:"rule_type"`
	// 最终版，由前端构造, 叶子节点的conf存储的是boolean
	// 保证最外层的最左节点为永真的叶子节点，且conf=false，用于表示灰度未命中时return false
	ConditionRule *model.JsonTree `json:"cel_rule"` //流量灰度或白名单灰度的条件节点
	FlowTag       string          `json:"flow_tag"` // 主维度，流量灰度时使用
	/** 流量灰度比例区间参数 **/
	HashMod100Min int                          `json:"hash_mod100_min"`
	HashMod100Max int                          `json:"max_mod100_max"`
	VarMap        map[string]model.JSONVarInfo `json:"var_map"`        // 多维度白名单的变量列表记录 ： 白名单维度 -> 类型属性, 来源于条件变量
	RuleByteCode  []byte                       `json:"rule_byte_code"` // 白名单条件规则通过编译后的byte code,cel-go格式,旧SDK读取，新SDK忽略
	compileList   []*exprservice.Conf          `json:"-"`              // sdk端使用，编译bytecode成cellist，避免每一次hit操作都要编译bytecode，该字段无法序列化
}

// 是否需要重新编译规则
func (g *GrayRule) IsCompiled() bool {
	return len(g.compileList) != 0
}

// IsGray 判断规则是否在灰度中，灰度中存在两个版本配置，一个是灰度中，一个是线上配置
func (g *GrayRule) IsGray() bool {
	if g == nil {
		return false
	}

	//白名单，或者按比例灰度中
	return g.RuleType != GrayRuleTypeRate ||
		g.HashMod100Max-g.HashMod100Min < 100
}

// sdk端调用，用于编译bytecode生成celList，加快计算
func (g *GrayRule) Compile() error {
	if g.IsCompiled() {
		return nil
	}
	g.fillDefaultNode()
	compileList, err := expr.CompileForest(context.Background(), g.ConditionRule, g.VarMap)
	if err != nil {
		return fmt.Errorf("compile gray rule condition err:%v", err)
	}
	g.compileList = compileList
	return nil
}

func (g *GrayRule) HasSafeCelRule() error {
	if len(g.ConditionRule.Nodes) == 0 {
		return fmt.Errorf("invalid cel_rule, cel_rule should not be empty")
	}

	// 检查所有的叶子节点的conf，必须是boolean json-string
	for _, each := range g.ConditionRule.Nodes {
		if !g.checkCelNodeConf(each) {
			return fmt.Errorf("invalid cel_rule, cel_rule content should be json string of boolean")
		}
	}

	// 检查cel-rule的合法性
	_, err := expr.CompileForest(context.Background(), g.ConditionRule, g.VarMap)
	if err != nil {
		return fmt.Errorf("format gray_rule error, err:%v", err)
	}

	return nil
}
func (g *GrayRule) checkCelNodeConf(node *model.JSONNode) bool {
	// 叶子节点
	if len(node.Children) == 0 {
		return node.Conf.OriText == "true" || node.Conf.OriText == "false"
	}

	for _, each := range node.Children {
		if !g.checkCelNodeConf(each) {
			return false
		}
	}
	return true
}

// SelfCompile 由admin传入celgo的编译函数，不直接依赖celgo，避免sdk直接依赖cel-go库
// 生成celgo格式的预编译二进制表示，旧SDK读取使用
func (g *GrayRule) SelfCompile(generateByteCode func(formatCode string) ([]byte, error)) error {
	var err error

	// 填充default节点
	g.fillDefaultNode()

	formatCode, err := service.GetFormatCode(context.Background(), g.ConditionRule, g.VarMap)
	if err != nil {
		return fmt.Errorf("compile gray_rule error, err:%v", err)
	}
	byteCode, err := generateByteCode(string(formatCode))
	if err != nil {
		return fmt.Errorf("generate bytecode for gray_rule error, err:%v", err)
	}

	g.RuleByteCode = byteCode

	return nil
}
func (g *GrayRule) fillDefaultNode() {
	fs := g.ConditionRule
	if fs == nil {
		g.ConditionRule = model.NewJsonTree()
		fs = g.ConditionRule
	}
	if len(fs.Nodes) > 0 {
		firstNode := fs.Nodes[0]
		// 如果是default节点
		if isDefaultNode(firstNode) {
			return
		}
	}

	fs.AddDefaultValue("false")
}

// Hit 空值情况会报错，返回false, error
func (g *GrayRule) Hit(abKeys map[string]interface{}) (bool, error) {
	if abKeys == nil || !g.IsCompiled() {
		return false, nil
	}

	var result string
	var celPath []int
	var err error
	var ret = false

	/* 流量灰度时，会先判断白名单条件，不命中再判断比例*/

	// JSON空条件场景下恒等于true，需要特殊处理
	if g.isEmptyCondition() {
		ret = false
	} else {
		if !g.IsCompiled() {
			return false, fmt.Errorf("gray rule not compiled")
		}

		result, celPath, err = expr.RunItem(context.Background(), g.compileList, abKeys)
		if err != nil {
			return false, err
		}

		if len(celPath) != 0 && result != "" {
			err = json.Unmarshal([]byte(result), &ret)
			if err != nil {
				return false, err
			}
		}
	}

	// 白名单阶段, 或者白名单已经命中（ret=true）
	if g.RuleType == GrayRuleTypeWhiteList || ret == true {
		return ret, nil
	}

	abKey, ok := abKeys[g.FlowTag]
	if !ok {
		return false, nil
	}
	val := crc32.ChecksumIEEE([]byte(fmt.Sprintf("%v", abKey)))
	// ensure that max >= min, [0, 0) means the grayRule can not be hit
	// [min, max)  =>  val >= min && val < max
	return int(val)%100 >= g.HashMod100Min && int(val)%100 < g.HashMod100Max, nil
}

// 条件树为空的情况，表达式引擎运行会默认返回true
func (g *GrayRule) isEmptyCondition() bool {
	if len(g.ConditionRule.Nodes) == 1 {
		firstNode := g.ConditionRule.Nodes[0]
		if len(firstNode.Cond.CondMatrix) == 1 && len(firstNode.Cond.CondMatrix[0]) == 0 {
			return true
		}
	}
	return false
}
func isDefaultNode(node *model.JSONNode) bool {
	if node == nil {
		return false
	}
	if node.Conf.OriText == "false" &&
		len(node.Cond.CondMatrix) == 0 {
		return true
	}
	return false
}
