package metrics

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"code.byted.org/aiops/metrics_codec"
	v3 "code.byted.org/aiops/metrics_codec/v3"
	v4 "code.byted.org/aiops/metrics_codec/v4"
	"code.byted.org/gopkg/metrics_core/logs"
	"code.byted.org/gopkg/metrics_core/utils"
)

const (
	defaultMessageSizeLimit = (1 << 17) - 1 // default length for uds agent
	largeMessageSizeLimit   = (1 << 20) - 1 // length for tcp and rpc sender
	messageRecycleThreshold = (1 << 21) - 1 // recycle threshold
)

const (
	nodeStatusDefault = iota
	nodeStatusRebuilt
	nodeStatusFinalizing
)

var SyncMapCollectorFactory Collector = newMap
var ShardingKdTreeCollectorFactory Collector = newShardingKDTree
var ShardingSyncMapCollectorFactory Collector = newShardingSyncMap

type collector interface {
	/*
		The Get function retrieves the cursor based on the input tag values.
	*/
	Get(index []string) (*cursor, error)

	/*
			The Pack function is responsible for packaging the metrics into the destination messages.
			The necessity of the first message is mandatory, while the inclusion of the second message is optional.
			The first message pertains to the current message, whereas the second message pertains to
		    the message from the previous time window.
	*/
	Pack(sendTime time.Time, message ...metrics_codec.Message)

	/*
		The Send function is responsible for transmitting the collector's metric.
		It is utilized exclusively prior to the removal of the metric.
	*/
	Send(sendTime time.Time)

	/*
		The GC function performs garbage collection on expired metric series.
	*/
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
	var err error
	if t.validator != nil {
		err = t.validator(index)
		if err != nil {
			if !t.metric.client.discardInvalidTag {
				return nil, err
			}

			for i := range index {
				if !utils.IsValidTagValue(index[i]) {
					index[i] = "-"
				}
			}
		}
	}
	node, err := t.getOrCreateNode(index, true, t.newNode)
	if err != nil {
		return nil, err
	}
	return &node.value, nil
}

// GC not thread safe
func (t *kdTree) GC() {
	defer func() {
		if err := recover(); err != nil {
			logs.Error("panic on do metrics gc, err: %s", err)
		}
	}()
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
			logs.Warn("unexpected state: found duplicate node(ptr:%v, status:%v) on hot-root for cold node(ptr:%v, status:%v)",
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
		if t.metric.client.sdk.sdkConfig.SelfMonitor.Enabled {
			t.metric.status.addCacheMem(-int64(n.size()))
		}
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

	if t.metric.client.sdk.sdkConfig.DebugMode {
		// check the rebuilt tree result
		for _, n := range nonExpired {
			nn := t.findColdNode(n.value.index)
			if nn == nil {
				logs.Warn("unexpected state: node with index %v not found in rebuilt kdTree", n.value.index)
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
		logs.Warn(" unexpected state: adding node with none-nil previousGen to gen-chain")
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
	if t.metric.client.sdk.sdkConfig.DebugMode {
		visited = make(map[uintptr]struct{}, t.nodeNum)
	}
	var prevNode, nextNode *node
	curNode := (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(headPtr))))
	for curNode != nil {
		if visited != nil {
			key := uintptr(unsafe.Pointer(curNode))
			if _, ok := visited[key]; ok {
				logs.Warn("unexpected state: circle detected on gen-chain, duplicate index: %v", curNode.value.index)
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
	if root == nil {
		return 0
	}
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

// getOrCreateNode gets or creates the node for a specific value index.
// Note that the values must be all valid.
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
			if t.forceDrop > 0 && atomic.LoadInt64(&t.nodeNum) >= t.forceDrop {
				err := fmt.Errorf("%s watermark at %d, dropped", t.metric.name, t.forceDrop)
				t.print.Do(func() {
					logs.Info("%s", err)
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
				if t.metric.client.sdk.sdkConfig.SelfMonitor.Enabled {
					t.metric.status.addCacheMem(int64(newNode.size()))
				}
				t.hcLock.RUnlock()
				return newNode, nil
			}
			t.hcLock.RUnlock()
		}
		// escaping from above means that there has been already another goroutine in creating clientStatus
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
			values: newQueue(t.metric),
			metric: t.metric,
			index:  make([]string, t.d),
		},
	}

	if t.metric.client.deepCopyTagIndex {
		_ = deepCopyStringSlice(newNode.value.index, index)
	} else {
		copy(newNode.value.index, index)
	}
	t.metric.storeCursor(&newNode.value)

	curP := (*unsafe.Pointer)(unsafe.Pointer(&t.curGenNode))
	old := (*node)(atomic.SwapPointer(curP, unsafe.Pointer(newNode)))
	if old != nil {
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&newNode.previousGen)), unsafe.Pointer(old))
	}
	return newNode
}

func (n *node) size() int {
	size := 0
	size += 8  // the pointer to the node itself
	size += 36 // pointers in node

	//compute size of the interval cursor
	size += 24 // slice header
	size += len(n.value.index) * (8 + 8)
	size += 32 // int64 + string + pointer

	size += 8 + (4 + 8 + 8)
	return size
}

func (t *kdTree) Pack(sendTime time.Time, messages ...metrics_codec.Message) {
	defer func() {
		if err := recover(); err != nil {
			logs.Error("panic on pack metrics, err: %s", err)
		}
	}()
	currentAt := (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&t.curGenNode))))

	for currentAt != nil {
		checkMessages(messages, t.metric)
		currentAt.value.flatPack(sendTime, messages...)
		currentAt = (*node)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&currentAt.previousGen))))
	}
}

// Send is only called when removing metrics.
func (t *kdTree) Send(sendTime time.Time) {
	t.sdLock.RLock()
	defer t.sdLock.RUnlock()
	sendMetric(t.metric, sendTime, t.Pack)
}

type shardingCollector struct {
	shards    []collector
	metric    *metric
	validator func(index []string) error
}

func newShardingKDTree(dimension int, metric *metric, forceDrop int, validator func(index []string) error) collector {
	trees := make([]collector, 1024)
	for i := range trees {
		trees[i] = newKDTree(dimension, metric, forceDrop, nil)
	}
	return &shardingCollector{
		shards:    trees,
		metric:    metric,
		validator: validator,
	}
}

func newShardingSyncMap(dimension int, metric *metric, forceDrop int, validator func(index []string) error) collector {
	maps := make([]collector, 1024)
	for i := range maps {
		maps[i] = newMap(dimension, metric, forceDrop, nil)
	}
	return &shardingCollector{
		shards:    maps,
		metric:    metric,
		validator: validator,
	}
}

func (m *shardingCollector) Get(index []string) (*cursor, error) {
	var err error
	if m.validator != nil {
		err = m.validator(index)
		if err != nil {
			if !m.metric.client.discardInvalidTag {
				return nil, err
			}

			for i := range index {
				if !utils.IsValidTagValue(index[i]) {
					index[i] = "-"
				}
			}
		}
	}

	h := utils.HashNew()
	for _, str := range index {
		h = utils.HashAdd(h, str)
	}
	return m.shards[h%1024].Get(index)
}

func (m *shardingCollector) Pack(sendTime time.Time, messages ...metrics_codec.Message) {
	checkMessages(messages, m.metric)
	for _, c := range m.shards {
		c.Pack(sendTime, messages...)
	}
}

func (m *shardingCollector) Send(sendTime time.Time) {
	sendMetric(m.metric, sendTime, m.Pack)
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
	nodeNum     int64
	metric      *metric
	validator   func([]string) error
	renamingMap sync.Map
}

func newMap(_ int, m *metric, _ int, validator func([]string) error) collector {
	return &mMap{
		metric:    m,
		validator: validator,
	}
}

func (m *mMap) Get(index []string) (*cursor, error) {
	buf := mapKeyPool.Get().(*[]byte)
	*buf = (*buf)[:0]
	for _, item := range index {
		*buf = append(*buf, '|')
		*buf = append(*buf, item...)
	}
	k := *(*string)(unsafe.Pointer(buf))
	acV, loaded := m.Load(k)
	if loaded {
		mapKeyPool.Put(buf)
		return acV.(*cursor), nil
	}

	// try find using the fixed key
	if m.metric.client.discardInvalidTag {
		fixedK, ok := m.renamingMap.Load(k)
		if ok {
			acV, loaded = m.Load(fixedK)
			if loaded {
				mapKeyPool.Put(buf)
				return acV.(*cursor), nil
			}
		}
	}

	if m.validator != nil {
		if err := m.validator(index); err != nil {
			if !m.metric.client.discardInvalidTag {
				return nil, err
			}
			// building the fixedK
			var invalidIdx []int
			var fixedK string
			fixedBuf := mapKeyPool.Get().(*[]byte)
			*fixedBuf = (*fixedBuf)[:0]
			for i, item := range index {
				*fixedBuf = append(*fixedBuf, '|')
				if !utils.IsValidTagValue(item) {
					invalidIdx = append(invalidIdx, i)
					*fixedBuf = append(*fixedBuf, '-')
					continue
				}
				*fixedBuf = append(*fixedBuf, item...)
			}
			fixedK = *(*string)(unsafe.Pointer(fixedBuf))

			// prepare the map value
			value := &cursor{
				index:  make([]string, len(index)),
				values: newQueue(m.metric),
				metric: m.metric,
			}
			if m.metric.client.deepCopyTagIndex {
				_ = deepCopyStringSlice(value.index, index)
			} else {
				copy(value.index, index)
			}
			for _, i := range invalidIdx {
				value.index[i] = "-"
			}
			m.metric.storeCursor(value)

			// try store to map
			acV, loaded = m.LoadOrStore(fixedK, value)
			if !loaded {
				m.renamingMap.Store(k, fixedK)
				atomic.AddInt64(&m.nodeNum, 1)
			} else {
				mapKeyPool.Put(buf)
				mapKeyPool.Put(fixedBuf)
			}
			return acV.(*cursor), nil
		}
	}

	value := &cursor{
		index:  make([]string, len(index)),
		values: newQueue(m.metric),
		metric: m.metric,
	}
	if m.metric.client.deepCopyTagIndex {
		_ = deepCopyStringSlice(value.index, index)
	} else {
		copy(value.index, index)
	}
	m.metric.storeCursor(value)
	acV, loaded = m.LoadOrStore(k, value)
	if !loaded {
		atomic.AddInt64(&m.nodeNum, 1)
	} else {
		mapKeyPool.Put(buf)
	}
	return acV.(*cursor), nil
}

func (m *mMap) Pack(sendTime time.Time, messages ...metrics_codec.Message) {
	m.Range(func(key, value interface{}) bool {
		checkMessages(messages, m.metric)
		value.(*cursor).flatPack(sendTime, messages...)
		return true
	})
}

func (m *mMap) Send(sendTime time.Time) {
	sendMetric(m.metric, sendTime, m.Pack)
}

func (m *mMap) GC() {
	m.Range(func(key, value interface{}) bool {
		cur := value.(*cursor)
		cur.GC()
		if cur.IsExpired() {
			m.Delete(key)
		}
		return true
	})
}

func sendMetric(m *metric, sendTime time.Time,
	collectorPacker func(sendTime time.Time, message ...metrics_codec.Message)) {
	messages := make([]metrics_codec.Message, 0)
	if m.client.withCodecV4 {
		messages = append(messages,
			v4.NewFlatMessage(v4.WithProperties(m.client.messageProperties...), v4.SetSkipCheck(), v4.WithMergeSeries()))
	} else {
		messages = append(messages,
			v3.NewFlatMessage(v3.WithProperties(m.client.messageProperties...), v3.SetSkipCheck(), v3.WithMergeSeries()),
			v3.NewFlatMessage(v3.WithProperties(m.client.messageProperties...), v3.SetSkipCheck(), v3.WithMergeSeries()),
		)
	}
	defer func() {
		for _, v := range messages {
			v.Recycle()
		}
	}()

	for _, v := range messages {
		_ = v.SetFlags(m.flags)
	}

	collectorPacker(sendTime, messages...)

	// Send from the end to the start since messages with higher indices have smaller timestamps.
	for i := len(messages) - 1; i >= 0; i-- {
		v := messages[i]
		sendMessage(m.client, v)
	}
}

func sendMessage(cli *client, message metrics_codec.Message) {
	if message == nil {
		return
	}

	if message.GetMetricCount() > 0 {
		p := packetPool.Get().(*packet)
		*p = (*p)[:0]
		var err error
		_ = message.SetClientTimestamp(time.Now())
		*p, err = message.Encode(*p)
		if err == nil {
			durationInUs := cli.sender.emit(*p, message.GetMetricCount(), message.GetSeriesCount())
			if cli.sdk.sdkConfig.SelfMonitor.Enabled {
				if !cli.isMonitorClient {
					emitAndLog(cli.sdk.senderLatencyMetric.WithTagValues(cli.tenant, "package"),
						WithSuffix("").Observe(int(durationInUs)))
				}
			}
		} else {
			logs.Error("error when encoding the message %v", err)
		}
		p.Recycle()
	}
}

func checkMessages(messages []metrics_codec.Message, m *metric) {
	// Check from the end to the start since messages with higher indices have smaller timestamps.
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message == nil {
			continue
		}

		if message.Size() >= m.client.messageSizeLimit || message.GetFlags() != m.flags {
			sendTime := message.GetTimestamp()
			sendMessage(m.client, message)
			message.Clear()
			_ = message.SetTimestamp(sendTime)
			_ = message.SetFlags(m.flags)
		}
	}
}

func deepCopyStringSlice(dst []string, src []string) error {
	if len(dst) != len(src) {
		return fmt.Errorf("must same length")
	}

	length := 0
	for i := range src {
		length += len(src[i])
	}
	buf := make([]byte, length)

	offset := 0
	for i := range src {
		length = len(src[i])
		copy(buf[offset:], src[i])
		data := buf[offset : offset+length]
		dst[i] = *(*string)(unsafe.Pointer(&data))
		offset += length
	}
	return nil
}
