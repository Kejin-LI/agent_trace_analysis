package tree_machine

type ChildNodeCache struct {
	NodeIndex int
	ChildName interface{}
	Count     int
}

type matchCacheChildren struct {
	Nodes       []*TreeNode
	Children    []*ChildNodeCache
	nodeLength  int
	childLength int
}

func (m *matchCacheChildren) Valid() bool {
	for i, c := range m.Children {
		if i >= m.childLength {
			break
		}
		if c.Count >= 0 {
			return true
		}
	}
	return false
}

func (m *matchCacheChildren) Count() int {
	var i int
	for ii, c := range m.Children {
		if ii >= m.childLength {
			break
		}
		if c.Count >= 0 {
			i++
		}
	}
	return i
}

func (m *matchCacheChildren) NextChild() (interface{}, *TreeNode, bool) {
	return nil, nil, true
}

func (m *matchCacheChildren) AddNode(node *TreeNode) int {
	if m.nodeLength >= 100 {
		l := len(m.Nodes)
		m.Nodes = append(m.Nodes, node)
		return l
	}
	m.Nodes[m.nodeLength] = node
	m.nodeLength++
	return m.nodeLength - 1
}

func (m *matchCacheChildren) AddNodeAllChild(node *TreeNode) {
	nodeIndex := m.AddNode(node)
	for key := range node.Children {
		m.AddNodeChildWithIndex(nodeIndex, key)
	}
}

func (m *matchCacheChildren) AddNodeChild(node *TreeNode, name interface{}) {
	nodeIndex := m.AddNode(node)
	m.AddNodeChildWithIndex(nodeIndex, name)
}

func (m *matchCacheChildren) AddNodeChildWithIndex(nodeIndex int, name interface{}) {
	//c := mapPool.Get().(*ChildNodeCache)
	c := &ChildNodeCache{}
	c.NodeIndex = nodeIndex
	c.ChildName = name
	c.Count = 0
	if m.childLength >= 100 {
		m.Children = append(m.Children, c)
		return
	}
	m.Children[m.childLength] = c
	m.childLength++
}

func (m *matchCacheChildren) DeleteChild(key1 int, key2 interface{}) {
	for _, child := range m.Children {
		if child.Count != -1 && child.NodeIndex == key1 && child.ChildName == key2 {
			child.Count = -1
		}
	}
}

//func (m *matchCacheChildren) GetChildCount(key1, key2 interface{}) int {
//	return m.NowCount[key1][key2]
//}

//func (m *matchCacheChildren) AddNodeChildCount(key1, key2 interface{}) {
//	_, ok := m.NowCount[key1]
//	if !ok {
//		m.NowCount[key1] = make(map[interface{}]int)
//	}
//	m.NowCount[key1][key2] += 1
//}

func newMatchCacheChildren() *matchCacheChildren {
	mc := &matchCacheChildren{
		Nodes:    make([]*TreeNode, 100),
		Children: make([]*ChildNodeCache, 100),
	}
	mc.AddNodeAllChild(connect)
	return mc
}

type MatchCache struct {
	//HistoryLength int
	//HistoryIndex int
	//HistoryText []rune
	State        *matchCacheChildren
	ReturnFather bool
	Node         *TreeNode
	Done         bool
	IsEnd        bool
	IndexAdd     int
	Kind         string
	Length       int
	EndIndex     int
}

func NewMatchCache(kind string, node *TreeNode) *MatchCache {
	mc := &matchCacheChildren{
		Nodes:    make([]*TreeNode, 100),
		Children: make([]*ChildNodeCache, 100),
	}
	mc.AddNodeAllChild(node)
	return &MatchCache{
		State:        mc,
		ReturnFather: true,
		Kind:         kind,
	}
}
