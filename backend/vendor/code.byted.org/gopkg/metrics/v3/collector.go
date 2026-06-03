package metrics

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"

	"code.byted.org/gopkg/metrics/v3/msgpack"
)

const (
	nodeStatusDefault = iota
	nodeStatusRebuilt
	nodeStatusFinalizing
)

type collector interface {
	Get(index []string) (*cursor, error)
	Send()
	GC()
}

type node struct {
	left  *node
	right *node
	value cursor

	previousGen *node
	writingL    int32
	writingR    int32
	writing     int32

	status int32
}

func (n *node) is(index []string) bool {
	for i, coordinate := range n.value.index {
		if coordinate != index[i] {
			return false
		}
	}
	return true
}

type sortableNodes struct {
	axis  int
	nodes []*node
}

func (s *sortableNodes) Len() int {
	return len(s.nodes)
}

func (s *sortableNodes) Less(i, j int) bool {
	return s.nodes[i].value.index[s.axis] < s.nodes[j].value.index[s.axis]
}

func (s *sortableNodes) Swap(i, j int) {
	s.nodes[i], s.nodes[j] = s.nodes[j], s.nodes[i]
}

type rootNode struct {
	root        *node
	rootWriting int32
}

type kdTree struct {
	nodeNum    int64
	hotRoot    *rootNode
	coldRoot   *rootNode
	d          int
	curGenNode *node

	metric    *metric
	forceDrop int64
	print     sync.Once
	validator func(index []string) error

	hcLock sync.RWMutex
	sdLock sync.RWMutex
}

func newKDTree(dimension int, metric *metric, forceDrop int, validator func(index []string) error) collector {
	t := &kdTree{
		d:         dimension,
		metric:    metric,
		forceDrop: int64(forceDrop),
		validator: validator,
	}
	t.hotRoot = &rootNode{}
	t.coldRoot = &rootNode{}
	return t
}

func (t *kdTree) Get(index []string) (*cursor, error) {
	node, err := t.getOrCreateNode(index, true, t.newNode)
	if err != nil {
		return nil, err
	}
	return &node.value, nil
}

// GC not thread safe
func (t *kdTree) GC() {
	// do a fast walk on hotRoot, to:
	// 1. gc expired cursor values
	// 2. stats the expired nodes
	// 3. could also check the tree-balance factor in future
	var expiredCount, totalCount int
	hotRoot := (*rootNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&t.hotRoot))))
	totalCount = t.walkNodes(&hotRoot.root, func(n *node, leftSize, rightSize int) {
		n.value.GC()
		if n.value.IsExpired() {
			expiredCount++
		}
	})
	rebuildFactor := t.metric.client.metricGCThreshold
	if totalCount == 0 || expiredCount == 0 || float64(expiredCount)/float64(totalCount) < rebuildFactor {
		return
	}

	// 1. switch hot-cold
	t.switchHotColdRoot()

	// 2. rebuild current cold tree
	coldRoot := (*rootNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&t.coldRoot))))
	t.rebuild(&coldRoot.root, &coldRoot.rootWriting, 0)

	// 3. switch back hot-cold
	t.switchHotColdRoot()

	// 4. remove the expired nodes
	t.doFinalizing()

	// 5. insert cold nodes to hot tree
	t.cleanColdRoot()
}

func (t *kdTree) cleanColdRoot() {
	// insert cold nodes back to hot-root
	coldRoot := (*rootNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&t.coldRoot))))
	var newNodes []*node
	t.walkNodes(&coldRoot.root, func(n *node, leftSize, rightSize int) {
		atomic.StoreInt32(&n.status, nodeStatusRebuilt)
		node, err := t.getOrCreateNode(n.value.index, false, func([]string) *node {
			// must create new copy
			nn := &node{
				value: n.value,
			}
			newNodes = append(newNodes, nn)
			return nn
		})
		if err != nil {
			return
		}
		if node.previousGen != nil {
			warnLog("unexpected state: found duplicate node(ptr:%v, status:%v) on hot-root for cold node(ptr:%v, status:%v)",
				unsafe.Pointer(node), atomic.LoadInt32(&node.status),
				unsafe.Pointer(n), atomic.LoadInt32(&n.status),
			)
			return
		}
	})

	t.sdLock.Lock()
	defer t.sdLock.Unlock()

	for i := range newNodes {
		t.addToGenChain(&t.curGenNode, newNodes[i])
	}
	t.compressGenChain(&t.curGenNode, func(n *node) bool {
		return atomic.LoadInt32(&n.status) == nodeStatusRebuilt
	}, nil)

	// reset coldRoot
	atomic.StoreInt32(&coldRoot.rootWriting, 0)
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&coldRoot.root)), nil)
}

func (t *kdTree) doFinalizing() {
	t.compressGenChain(&t.curGenNode, func(n *node) bool {
		return atomic.LoadInt32(&n.status) == nodeStatusFinalizing
	}, func(n *node) {
		// reset references
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&n.left)), nil)
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&n.right)), nil)
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&n.previousGen)), nil)
		if gcCallback != nil {
			gcCallback(t, n)
		}
	})
}

// rebuild not thread safe
func (t *kdTree) rebuild(root **node, writingFlag *int32, axis int) {
	var nonExpired, expired []*node
	t.walkNodes(root, func(n *node, leftSize, rightSize int) {
		if n.value.IsExpired() {
			expired = append(expired, n)
		} else {
			nonExpired = append(nonExpired, n)
		}
	})

	// build new tree without modify the exists node
	// we only copy its value
	newRoot := t.buildNode(nonExpired, axis)

	// replace the root and decrease the nodeNum
	if newRoot == nil {
		atomic.StoreInt32(writingFlag, 0)
	}
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(root)), unsafe.Pointer(newRoot))
	atomic.AddInt64(&t.nodeNum, -int64(len(expired)))

	// mark old nodes as deleted
	for _, n := range nonExpired {
		atomic.StoreInt32(&n.status, nodeStatusRebuilt)
	}
	for _, n := range expired {
		atomic.StoreInt32(&n.status, nodeStatusFinalizing)
	}

	if enableDebug {
		// check the rebuilt tree result
		for _, n := range nonExpired {
			nn := t.findColdNode(n.value.index)
			if nn == nil {
				warnLog("unexpected state: node with index %v not found in rebuilt kdTree", n.value.index)
			}
		}
	}

	t.sdLock.Lock()
	defer t.sdLock.Unlock()

	// add new created nodes to the GenNodes chain, to make them visible
	// to the metric sender.
	t.walkNodes(&newRoot, func(n *node, leftSize, rightSize int) {
		t.addToGenChain(&t.curGenNode, n)
	})

	// compress the GenNodes chain, to delete node marked as nodeStatusRebuilt
	// notice: we skipped nodes with nodeStatusFinalizing status, leave it to
	// the finalizing step.
	t.compressGenChain(&t.curGenNode, func(n *node) bool {
		return atomic.LoadInt32(&n.status) == nodeStatusRebuilt
	}, nil)
}

func (t *kdTree) addToGenChain(headPtr **node, n *node) {
	if n.previousGen != nil {
		warnLog(" unexpected state: adding node with none-nil previousGen to gen-chain")
	}
	curP := (*unsafe.Pointer)(unsafe.Pointer(headPtr))
	old := (*node)(atomic.SwapPointer(curP, unsafe.Pointer(n)))
	if old != nil {
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&n.previousGen)), unsafe.Pointer(old))
	}
}

func (t *kdTree) compressGenChain(headPtr **node, condition func(*node) bool, callback func(*node)) {
BEGIN:
	var visited map[uintptr]struct{}
	if enableDebug {
		visited = make(map[uintptr]struct{}, t.nodeNum)
	}
	var prevNode, nextNode *node
	curNode := (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(headPtr))))
	for curNode != nil {
		if visited != nil {
			key := uintptr(unsafe.Pointer(curNode))
			if _, ok := visited[key]; ok {
				warnLog("unexpected state: circle detected on gen-chain, duplicate index: %v", curNode.value.index)
			}
			visited[key] = struct{}{}
		}
		nextNode = (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&curNode.previousGen))))
		if condition(curNode) {
			if prevNode != nil {
				atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&prevNode.previousGen)), unsafe.Pointer(curNode), unsafe.Pointer(nextNode))
			} else {
				if !atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(headPtr)), unsafe.Pointer(curNode), unsafe.Pointer(nextNode)) {
					// if failed, other goroutine insert new node at head, re-loop
					goto BEGIN
				}
			}
			if callback != nil {
				callback(curNode)
			}
			curNode = nextNode
			continue
		}
		prevNode = curNode
		curNode = nextNode
	}
}

func (t *kdTree) walkGenChain(headPtr **node, callback func(*node)) {
	curNode := (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(headPtr))))
	for curNode != nil {
		callback(curNode)
		curNode = (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&curNode.previousGen))))
	}
}

func (t *kdTree) buildNode(values []*node, axis int) *node {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 {
		return &node{
			value: values[0].value,
		}
	}

	sort.Sort(&sortableNodes{axis: axis, nodes: values})
	mid := len(values) / 2

	// shift to the first-left value equals with mid
	minIdx := mid - 1
	for minIdx >= 0 {
		if values[minIdx].value.index[axis] != values[mid].value.index[axis] {
			break
		}
		mid--
		minIdx--
	}

	root := values[mid]
	nextAxis := (axis + 1) % t.d

	left := t.buildNode(values[:mid], nextAxis)
	right := t.buildNode(values[mid+1:], nextAxis)

	return &node{
		value: root.value,
		left:  left,
		right: right,
	}
}

func (t *kdTree) walkNodes(root **node, visitFunc func(n *node, leftSize, rightSize int)) int {
	curNodePtr := root
	curNode := (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(curNodePtr))))
	if curNode == nil {
		return 0
	}
	lSize := t.walkNodes(&curNode.left, visitFunc)
	rSize := t.walkNodes(&curNode.right, visitFunc)
	visitFunc(curNode, lSize, rSize)
	return lSize + rSize + 1
}

func (t *kdTree) findColdNode(index []string) *node {
	return t.findNode(index, &t.coldRoot)
}

func (t *kdTree) findNode(index []string, rn **rootNode) *node {
	root := (*rootNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(rn))))
	curNodePtr := &root.root

	curAxis := 0
	var curNode *node
	for {
		curPtr := (*unsafe.Pointer)(unsafe.Pointer(curNodePtr))
		curNode = (*node)(atomic.LoadPointer(curPtr))
		if curNode == nil {
			return nil
		}
		if (*curNode).is(index) {
			return curNode
		}
		if index[curAxis] >= curNode.value.index[curAxis] {
			curNodePtr = &curNode.right
		} else {
			curNodePtr = &curNode.left
		}
		if curAxis == t.d-1 {
			curAxis = 0
		} else {
			curAxis += 1
		}
	}
}

func (t *kdTree) switchHotColdRoot() {
	t.hcLock.Lock()
	hot := atomic.SwapPointer((*unsafe.Pointer)(unsafe.Pointer(&t.hotRoot)), unsafe.Pointer(t.coldRoot))
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&t.coldRoot)), hot)
	t.hcLock.Unlock()
}

func (t *kdTree) getOrCreateNode(index []string, tryFindCold bool, createNodeFunc func([]string) *node) (*node, error) {
BEGIN:
	root := (*rootNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&t.hotRoot))))
	curNodePtr := &root.root
	writing := &root.rootWriting

	curAxis := 0
	var curNode *node
	for {
		curPtr := (*unsafe.Pointer)(unsafe.Pointer(curNodePtr))
		if atomic.LoadPointer(curPtr) == nil {
			err := t.validator(index)
			if err != nil {
				return nil, err
			}
			if t.forceDrop > 0 && atomic.LoadInt64(&t.nodeNum) >= t.forceDrop {
				err := fmt.Errorf("%s watermark at %d, dropped", t.metric.name, t.forceDrop)
				t.print.Do(func() {
					log("%s", err)
				})
				return nil, err
			}

			t.hcLock.RLock()

			currRoot := (*rootNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&t.hotRoot))))
			if currRoot != root {
				// if hot root changed, we should switch to new root
				t.hcLock.RUnlock()
				goto BEGIN
			}

			if curNode != nil {
				// we should recheck if we are on the deleted part,
				// to avoid ABA problem
				parentDeleted := atomic.LoadInt32(&curNode.status)
				if parentDeleted != nodeStatusDefault {
					t.hcLock.RUnlock()
					goto BEGIN
				}
			}

			var newNode *node
			if tryFindCold {
				newNode = t.findColdNode(index)
				if newNode != nil {
					t.hcLock.RUnlock()
					return newNode, nil
				}
			}

			if atomic.CompareAndSwapInt32(writing, 0, 1) {
				newNode = createNodeFunc(index)
				atomic.StorePointer(curPtr, unsafe.Pointer(newNode))
				t.hcLock.RUnlock()
				return newNode, nil
			}
			t.hcLock.RUnlock()
		}
		// escaping from above means that there has been already another goroutine in creating status
		// therefore loop until creation complete
		for {
			curNode = (*node)(atomic.LoadPointer(curPtr))
			if curNode != nil {
				break
			}
			if atomic.LoadInt32(writing) == 0 {
				goto BEGIN
			}
			// avoid STW above causing infinity loop
			runtime.Gosched()
		}
		if (*curNode).is(index) {
			return curNode, nil
		}
		if index[curAxis] >= curNode.value.index[curAxis] {
			curNodePtr = &curNode.right
			writing = &curNode.writingR
		} else {
			curNodePtr = &curNode.left
			writing = &curNode.writingL
		}
		if curAxis == t.d-1 {
			curAxis = 0
		} else {
			curAxis += 1
		}
	}
}

func (t *kdTree) newNode(index []string) *node {
	atomic.AddInt64(&t.nodeNum, 1)
	newNode := &node{
		value: cursor{
			values: newQueue(),
			metric: t.metric,
			index:  make([]string, t.d),
		},
	}
	copy(newNode.value.index, index)
	t.metric.storeCursor(&newNode.value)

	curP := (*unsafe.Pointer)(unsafe.Pointer(&t.curGenNode))
	old := (*node)(atomic.SwapPointer(curP, unsafe.Pointer(newNode)))
	if old != nil {
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&newNode.previousGen)), unsafe.Pointer(old))
	}
	return newNode
}

func (t *kdTree) Send() {
	t.sdLock.RLock()
	defer t.sdLock.RUnlock()

	currentAt := (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&t.curGenNode))))
	// intentionally make p *packet instead of packet to avoid allocation.
	p := packetPool.Get().(*packet)

	var count int
	var sum int
	for currentAt != nil {
		*p, count = currentAt.value.send(*p)
		sum += count
		if len(*p) > maxSendSize || sum == math.MaxUint16 {
			msgpack.FillArrayHeader(*p, 0, uint16(sum))
			t.metric.client.sender.emit(*p)
			*p = (*p)[:3]
			sum = 0
		}
		currentAt = (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&currentAt.previousGen))))
	}
	if len(*p) > 3 {
		msgpack.FillArrayHeader(*p, 0, uint16(sum))
		t.metric.client.sender.emit(*p)
	}
	p.Recycle()
}

type shardingCollector struct {
	shards []collector
}

func newShardingCollector(dimension int, metric *metric, forceDrop int, validator func(index []string) error) collector {
	trees := make([]collector, 1024)
	for i := range trees {
		trees[i] = newKDTree(dimension, metric, forceDrop, validator)
	}
	return &shardingCollector{
		shards: trees,
	}
}

func (m *shardingCollector) Get(index []string) (*cursor, error) {
	h := hashNew()
	for _, str := range index {
		h = hashAdd(h, str)
	}
	return m.shards[h%1024].Get(index)
}

func (m *shardingCollector) Send() {
	for _, c := range m.shards {
		c.Send()
	}
}

func (m *shardingCollector) GC() {
	for _, shard := range m.shards {
		shard.GC()
	}
}

var mapKeyPool = sync.Pool{
	New: func() interface{} {
		key := make([]byte, 0, 512)
		return &key
	},
}

// mMap is a baseline collector, it is used to compare with performance of k-d tree.
type mMap struct {
	sync.Map
	nodeNum   int64
	metric    *metric
	validator func([]string) error
}

func newMap(_ int, m *metric, _ int, validator func([]string) error) collector {
	return &mMap{
		metric:    m,
		validator: validator,
	}
}

func (m *mMap) Get(index []string) (*cursor, error) {
	if err := m.validator(index); err != nil {
		return nil, err
	}
	key := mapKeyPool.Get().(*[]byte)
	*key = (*key)[:0]
	for _, i := range index {
		*key = append(*key, i...)
	}
	k := *(*string)(unsafe.Pointer(key))
	value := &cursor{
		index:  make([]string, len(index)),
		values: newQueue(),
		metric: m.metric,
	}
	copy(value.index, index)
	m.metric.storeCursor(value)
	acV, loaded := m.LoadOrStore(k, value)
	if !loaded {
		atomic.AddInt64(&m.nodeNum, 1)
	} else {
		mapKeyPool.Put(key)
	}
	return acV.(*cursor), nil
}

func (m *mMap) Send() {
	m.Range(func(key, value interface{}) bool {
		p := *packetPool.Get().(*packet)
		p, count := value.(*cursor).send(p)
		if len(p) > 3 {
			msgpack.FillArrayHeader(p, 0, uint16(count))
			m.metric.client.sender.emit(p)
		}
		p = p[:3]
		packetPool.Put(&p)
		return true
	})
}

func (m *mMap) GC() {

}
