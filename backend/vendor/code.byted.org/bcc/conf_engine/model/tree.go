package model

import (
	"fmt"

	json "code.byted.org/bcc/conf_engine/jsoniter"
	"code.byted.org/gopkg/logs"
)

type JsonTree struct {
	Nodes []*JSONNode `json:"nodes"`
}

// NewForestWithDefaultValue
//
//	@Description: 基于json和兜底配置创建json决策树，会校验格式是否正确
//	@param base：json格式的决策树
//	@param defaultConfVal：兜底配置
//	@return *JsonTree：动态决策树
//	@return error：当格式不正确时返回error
func NewForestWithDefaultValue(base string, defaultConfVal string) (*JsonTree, error) {
	forest := NewJsonTree()
	if err := json.UnmarshalFromString(base, &forest.Nodes); err != nil {
		return nil, fmt.Errorf("unmarshalFromString error: %v", err)
	}
	forest.AddDefaultValue(defaultConfVal)
	return forest, nil
}
func NewJsonTree() *JsonTree {
	return &JsonTree{Nodes: make([]*JSONNode, 0)}
}

func (t *JsonTree) String() string {
	buf, err := json.Marshal(t.Nodes)
	if err != nil {
		//should not happen
		logs.Error("json marshal error: %v", err)
	}
	return string(buf)
}

// AddDefaultValue 添加默认节点配置，当所有节点没有命中时，返回默认配置
func (t *JsonTree) AddDefaultValue(val string) {
	t.Nodes = append([]*JSONNode{{
		Title:    "default",
		Children: nil,
		Cond:     &CondNode{},
		Conf: &ConfNode{
			OriText: val,
		},
	}}, t.Nodes...)
}

// GetAllConfNode 获取所有叶子结点，confNode只存在叶子结点
func (t *JsonTree) GetAllConfNode() []*ConfNode {
	res := make([]*ConfNode, 0)

	for _, node := range t.Nodes {
		getAllConfNodes(node, &res)
	}
	return res
}
func getAllConfNodes(node *JSONNode, res *[]*ConfNode) {
	if len(node.Children) == 0 {
		*res = append(*res, node.Conf)
		return
	}
	for _, child := range node.Children {
		getAllConfNodes(child, res)
	}
}
