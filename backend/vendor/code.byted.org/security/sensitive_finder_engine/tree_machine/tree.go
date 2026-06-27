package tree_machine

import (
	"time"
)

const (
	modeNodeDefault = iota
	modeNodeKey
	modeNodeConnect
	modeNodeValue
)

type TreeNode struct {
	Name     string                    `json:"name"`      // 节点名称，或者键值
	Pattern  interface{}               `json:"pattern"`   // 节点模式，可能为正则
	Value    interface{}               `json:"value"`     // 节点存储的数据
	IsLeaf   bool                      `json:"is_leaf"`   // 是否为叶子节点
	TreeMode int                       `json:"tree_mode"` // 树的类型
	Children map[interface{}]*TreeNode `json:"children"`  // 树的子孙
}

func (t *TreeNode) GetChildren() map[interface{}]*TreeNode {
	return t.Children
}

func (t *TreeNode) NextChild() (interface{}, *TreeNode, bool) {
	return nil, nil, true
}

func NewTree() *TreeNode {
	t := TreeNode{
		Name:     "",
		Pattern:  nil,
		IsLeaf:   false,
		Children: nil,
	}
	t.Children = map[interface{}]*TreeNode{}
	return &t
}

func appendNode(slc []*TreeNode, value *TreeNode) {
	slc = append(slc, value)
}

type Pattern struct {
	Elements []uint8 `json:"elements"`  // 字符列表
	IsAnti   bool    `json:"is_anti"`   // 不包含
	IsNumber bool    `json:"is_number"` // 是数字
	IsLetter bool    `json:"is_letter"` // 是字母
	IsSpace  bool    `json:"is_space"`  // 是空格
	IsAny    bool    `json:"is_any"`    // 任何字符
	MinCount int     `json:"min_count"` // 出现最少次数
	MaxCount int     `json:"max_count"` // 出现最多次数
}

// 命中单个字符
func (p Pattern) Match(value uint8) bool {
	//fmt.Println(string(value), 48 <= value && value <= 57 , value)
	if p.IsAny {
		return true
	}
	for _, e := range p.Elements {
		if value == e {
			return !p.IsAnti
		}
	}
	if p.IsNumber && isNumber(value) {
		return !p.IsAnti
	}
	if p.IsLetter && (isLetter(value) || value > 127) {
		return !p.IsAnti
	}
	if p.IsSpace && (value == 0 || value == 10 || value == 13 || value == 32) {
		return !p.IsAnti
	}
	return p.IsAnti
}

func isNumber(value uint8) bool {
	return 48 <= value && value <= 57
}

func isLetter(value uint8) bool {
	return (97 <= value && value <= 122) || (65 <= value && value <= 90)
}

// 比较是否相同
func (p Pattern) IsEqual(ap Pattern) bool {
	// todo: use reflect
	f := p.IsAnti == ap.IsAnti && p.IsNumber == ap.IsNumber && p.IsLetter == ap.IsLetter && p.IsSpace == ap.IsSpace &&
		p.IsAny == ap.IsAny && p.MinCount == ap.MinCount && p.MaxCount == ap.MaxCount && len(p.Elements) == len(ap.Elements)
	if f == true {
		for _, e := range p.Elements {
			b := false
			for _, ee := range ap.Elements {
				if e == ee {
					b = true
					break
				}
			}
			if b == false {
				f = false
				break
			}
		}
	}
	return f
}

func (t *TreeNode) AddNode(key string, isLeaf bool) *TreeNode {
	return addNodeToTree(t, key, isLeaf)
}

func addNodeToTree(rootNode *TreeNode, key string, isLeaf bool) *TreeNode {
	var nowNode *TreeNode
	nowNode = rootNode
	for _, part := range []uint8(key) {
		n, ok := nowNode.Children[part]
		if ok {
			nowNode = n
		} else {
			newNode := NewTree()
			newNode.Name = string(part)
			nowNode.Children[part] = newNode
			nowNode = newNode
		}
	}
	// 不修改已有内容
	if nowNode.IsLeaf == false {
		nowNode.IsLeaf = isLeaf
	}
	return nowNode
}

//func GetNode(key string, isLeaf bool) (*TreeNode, *TreeNode) {
//	var nowNode *TreeNode
//	startNode := NewTree()
//	startNode.Name = string(key[0])
//	nowNode = startNode
//	for _, part := range []rune(key) {
//		n, ok := nowNode.Children[part]
//		if ok {
//			nowNode = n
//		} else {
//			newNode := NewTree()
//			newNode.Name = string(part)
//			nowNode.Children[part] = newNode
//			nowNode = newNode
//		}
//	}
//	//nowNode.Next = next
//	nowNode.IsLeaf = isLeaf
//	return startNode, nowNode
//}

func (t *TreeNode) AddNode1(key []interface{}, value string) []*TreeNode {
	var nowNode *TreeNode
	var childrenNode []*TreeNode
	nowNode = t
	for _, part := range key {
		switch part.(type) {
		case uint8:
			{
				if nowNode == nil {
					newNode := NewTree()
					newNode.Name = string(part.(uint8))
					for _, cld := range childrenNode {
						cld.Children[part] = newNode
					}
					nowNode = newNode
					childrenNode = []*TreeNode{}
				} else {
					n, ok := nowNode.Children[part]
					if ok {
						nowNode = n
					} else {
						newNode := NewTree()
						newNode.Name = string(part.(uint8))
						nowNode.Children[part] = newNode
						nowNode = newNode
					}
				}
			}
		case string:
			{
				if nowNode == nil {
					nowStartNode := NewTree()
					nowStartNode.Name = string(string(part.(uint8))[0])
					nowNode = addNodeToTree(nowStartNode, string(part.(uint8)), false)
					for _, cld := range childrenNode {
						cld.Children[nowStartNode.Name[0]] = nowStartNode
					}
				} else {
					nowNode = nowNode.AddNode(part.(string), false)
				}
				childrenNode = []*TreeNode{}
			}
		case Pattern:
			{
				newNode := NewTree()
				newNode.Pattern = part
				//newNode.IsRegexp = true
				if nowNode == nil {
					for _, cld := range childrenNode {
						for _, p := range cld.Children {
							pattern, isRegexp := p.Pattern.(Pattern)
							if isRegexp && pattern.IsEqual(part.(Pattern)) == true {
								nowNode = p
								break
							}
						}
						cld.Children[time.Now().UnixNano()] = newNode
					}
				} else {
					ifContains := false
					for _, p := range nowNode.Children {
						pattern, isRegexp := p.Pattern.(Pattern)
						if isRegexp && pattern.IsEqual(part.(Pattern)) == true {
							nowNode = p
							ifContains = true
							break
						}
					}
					if !ifContains {
						nowNode.Children[time.Now().UnixNano()] = newNode
					}
				}
				nowNode = newNode
				childrenNode = []*TreeNode{}
			}
		case []interface{}:
			{
				var childrenNode1 []*TreeNode
				for _, p := range part.([]interface{}) {
					var ntn []*TreeNode
					if nowNode == nil {
						for _, child := range childrenNode {
							switch p.(type) {
							case []interface{}:
								{
									ntn = child.AddNode1(p.([]interface{}), "")
								}
							default:
								ntn = child.AddNode1([]interface{}{p}, "")
							}
							childrenNode1 = append(childrenNode1, ntn...)
						}
					} else {
						switch p.(type) {
						case []interface{}:
							{
								ntn = nowNode.AddNode1(p.([]interface{}), "")
							}
						default:
							ntn = nowNode.AddNode1([]interface{}{p}, "")
						}
						childrenNode1 = append(childrenNode1, ntn...)
					}
				}
				childrenNode = childrenNode1
				nowNode = nil
			}
		default:
			{
				panic("unknown kind.")
			}
		}
	}
	if len(childrenNode) == 0 {
		childrenNode = append(childrenNode, nowNode)
	}
	//for _, np := range childrenNode {
	//	np.IsLeaf = true
	//	np.Value = value
	//}
	return childrenNode
}

func (t *TreeNode) Tidy() {
	// todo: 剪枝
}

func mapTreeNodeContains(nodes map[interface{}]*TreeNode, key uint8) (*TreeNode, bool) {
	node, ok := nodes[key]
	if ok {
		return node, true
	} else {
		//isDigit := unicode.IsDigit(key)
		//if isDigit {
		//	node, ok = t.Children["\\d"]
		//	if ok {
		//		return node, true
		//	}
		//}
		//isLetter := unicode.IsLetter(key)
		//if isLetter {
		//	key1 := unicode.ToLower(key)
		//	if key1 != key {
		//		node, ok = nodes[key1]
		//		if ok {
		//			return node, true
		//		}
		//	}
		//}
		//if isLetter || isDigit {
		//	node, ok = t.Children["\\w"]
		//	if ok {
		//		return node, true
		//	}
		//}
		return nil, false
	}
}

// 通过 key 进行查找
func (t *TreeNode) Contains(key uint8) (*TreeNode, bool) {
	return mapTreeNodeContains(t.Children, key)
}

func (t *TreeNode) ContainsAll(key uint8) ([]*TreeNode, bool) {
	var nodes []*TreeNode
	node, ok := t.Children[key]
	if ok {
		nodes = append(nodes, node)
	}
	//isDigit := unicode.IsDigit(key)
	//if isDigit {
	//	node, ok = t.Children["\\d"]
	//	if ok {
	//		nodes = append(nodes, node)
	//	}
	//}
	//isLetter := unicode.IsLetter(key)
	//if isLetter {
	//	key1 := unicode.ToLower(key)
	//	if key1 != key {
	//		node, ok = t.Children[key1]
	//		if ok {
	//			nodes = append(nodes, node)
	//		}
	//	}
	//}
	//if isLetter || isDigit {
	//	node, ok = t.Children["\\w"]
	//	if ok {
	//		nodes = append(nodes, node)
	//	}
	//}
	return nodes, len(nodes) == 0
}
