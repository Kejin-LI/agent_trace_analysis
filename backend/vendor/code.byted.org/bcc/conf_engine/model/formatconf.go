package model

import (
	"context"

	"code.byted.org/gopkg/logs"

	"code.byted.org/ttarch/byteconf-cel-go/cel"

	"github.com/antonmedv/expr/vm"
)

//cel跟expr的数据表达之所以不定义在各自仓库，是因为避免sdk同时引用cel expr 两个仓库
//也是为了更好管理，避免冲突

//旧sdk使用cel，为了兼容需要继续下发
type CelData struct {
	Prg  cel.Program `json:"-"`
	Txt  string      `json:"t"`    //cel 格式的表达式
	Expr []byte      `json:"expr"` //ast转化的表达式，经过pb序列化后的结果

}
type FormatFunc func(ctx context.Context, t *FormatData, n *CondNode, vMap map[string]JSONVarInfo) error

//新sdk只使用expr，不引入cel避免冲突
type ExprData struct {
	Prg  *vm.Program `json:"-"`
	Txt  string      `json:"e_t"`    //cel 格式的表达式
	Expr []byte      `json:"e_expr"` //ast转化的表达式，经过pb序列化后的结果
}
type FormatData struct {
	CelData
	ExprData
}
type FormatConf struct {
	Txt string `json:"t"` //配置数据，因为不支持配置中使用条件变量，所以不需要编译,expr逻辑会直接返回配置不会编译运行
	// 兼容旧格式，旧逻辑cel go对于配置节点仍然会编译并运行，实际没有作用
	Prg  cel.Program `json:"-"`
	Expr []byte      `json:"expr"` //ast转化的表达式，经过pb序列化后的结果
}

type FormatNode struct {
	Cond     *FormatData   `json:"cond"`
	Conf     *FormatConf   `json:"conf"`
	Children []*FormatNode `json:"children"`
}
type FormatConfTree struct {
	Nodes []*FormatNode
}

func ToFormatTree(ctx context.Context, t *JsonTree, varMap map[string]JSONVarInfo, formatFunc []FormatFunc) (FormatConfTree, error) {
	forest := FormatConfTree{}
	for _, node := range t.Nodes {
		c, err := node.format(ctx, varMap, formatFunc)
		if err != nil {
			logs.CtxError(ctx, "[JsonTree] child toFormatForest err:%v", err)
			return forest, err
		}
		forest.Nodes = append(forest.Nodes, c)
	}
	return forest, nil
}
func (n *JSONNode) format(ctx context.Context, varMap map[string]JSONVarInfo, formatFunc []FormatFunc) (*FormatNode, error) {
	res := &FormatNode{}
	cond, err := condNodeFormat(ctx, n.Cond, varMap, formatFunc)
	if err != nil {
		logs.CtxError(ctx, "[toFormatForest] err:%v", err)
		return nil, err
	}
	res.Cond = cond
	conf, err := confNodeFormat(ctx, n.Conf)
	if err != nil {
		logs.CtxError(ctx, "[toFormatForest] err:%v", err)
		return nil, err
	}
	res.Conf = conf
	for _, child := range n.Children {
		c, err := child.format(ctx, varMap, formatFunc)
		if err != nil {
			logs.CtxError(ctx, "[toFormatForest] err:%v", err)
			return nil, err
		}
		res.Children = append(res.Children, c)
	}
	return res, nil
}

func confNodeFormat(ctx context.Context, n *ConfNode) (*FormatConf, error) {
	if n == nil {
		return nil, nil
	}
	res := &FormatConf{}
	FormatConfStr(res, n)

	return res, nil

}

func condNodeFormat(ctx context.Context, n *CondNode, varMap map[string]JSONVarInfo, formatFunc []FormatFunc) (*FormatData, error) {
	res := &FormatData{}
	for _, f := range formatFunc {
		err := f(ctx, res, n, varMap)
		if err != nil {
			logs.CtxError(ctx, "[condNode] format err:%v", err)
			return nil, err
		}
	}

	return res, nil
}
