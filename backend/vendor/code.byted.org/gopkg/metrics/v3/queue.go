package metrics

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	qNodeFlagDefault int32 = iota
	qNodeFlagHasValue
	qNodeFlagNoValue
)

var (
	qNodePool = sync.Pool{
		New: func() interface{} {
			return &qNode{nState: &nState{}}
		},
	}

	nStatePool = sync.Pool{
		New: func() interface{} {
			return &nState{}
		},
	}
)

type nState struct {
	deleted bool
	next    *qNode
}

func (n *nState) recycle() {
	n.next = nil
	nStatePool.Put(n)
}

type qNode struct {
	*nState
	init           int32
	res            reservoir
	lastActiveTime int64
}

type queue struct {
	head *qNode
}

func newQueue() *queue {
	dummy := &qNode{nState: &nState{}}
	return &queue{
		dummy,
	}
}

func (q *queue) getHead() *qNode {
	return q.head
}

// insert insert node after the head node
func (q *queue) insert(node *qNode) bool {
	if node == q.head {
		return false
	}
	for {
		currState := (*nState)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&q.head.nState))))
		node.nState = currState // deleted = false
		newState := nStatePool.Get().(*nState)
		newState.deleted, newState.next = false, node
		if atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&q.head.nState)), unsafe.Pointer(currState), unsafe.Pointer(newState)) {
			return true
		}
		newState.recycle()
	}
}

func (q *queue) remove(node *qNode, prev *qNode) bool {
	if node == q.head {
		return false
	}
	for {
		oldState := (*nState)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&node.nState))))
		if oldState.deleted {
			return false
		}
		newState := nStatePool.Get().(*nState)
		newState.deleted, newState.next = true, oldState.next
		if atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&node.nState)), unsafe.Pointer(oldState), unsafe.Pointer(newState)) {
			break
		}
		newState.recycle()
	}
	if prev == nil {
		return true
	}
	// try to change the prev.next -> node.next
	// we may fail if the prev node is also deleting
	// if that, the result would be prev.prev.next -> node
	// so a further unlink op need be done: prev.prev.next -> node.next
	// we will leave that to the getNext() path to do.
	oldState := (*nState)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&prev.nState))))
	nextState := (*nState)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&node.nState))))
	newState := nStatePool.Get().(*nState)
	newState.deleted, newState.next = oldState.deleted, nextState.next
	if !atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&prev.nState)), unsafe.Pointer(oldState), unsafe.Pointer(newState)) {
		newState.recycle()
	}
	return true
}

func (q *queue) mergeOrInsert(v *Value) {
	if v.suffix != "" && !isValidString(v.suffix) {
		panic(fmt.Sprintf("invalid metric name suffix: %s", v.suffix))
	}
	p := (*qNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&q.head))))
	for {
		currState := (*nState)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&p.nState))))
		next := currState.next
		if next == nil {
			if currState.deleted {
				p = (*qNode)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&q.head))))
				continue
			}
			n := qNodePool.Get().(*qNode)
			newState := nStatePool.Get().(*nState)
			newState.deleted, newState.next = false, n
			if atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&p.nState)), unsafe.Pointer(currState), unsafe.Pointer(newState)) {
				n.set(v)
				return
			}
			newState.recycle()
			qNodePool.Put(n)
			continue
		}
		// we won't do the chain compression here for performance reason
		// so there's little chance that we merged value on an expire-deleted node in theory.
		if next.merge(v) {
			// hack: unsafe type assertion to measure
			measurePool.Put((*measure)(unsafe.Pointer(v)))
			return
		}
		// the next node may be already deleted now, but it's ok,
		// for a deleted node, it's next pointer has three cases:
		// 1. next is a non-deleted node
		// 2. next is nil
		// 3. next is also a deleted node, this case could be ignored as it's a recursive one
		// for case 1, it's fine, we won't miss-scan any currently created new node
		// for case 2, it's also ok, other goroutine may insert node after some preceding node
		// which we will miss-scan in current iteration, but next step, p will jump to q.head,
		// we will see the new created node in the next iteration begin from q.head.
		p = next
	}
}

func (n *qNode) getReservoir() reservoir {
	for {
		if atomic.LoadInt32(&n.init) != qNodeFlagDefault {
			return n.res
		}
		runtime.Gosched()
	}
}

// shallowCopy create a new node with same reservoir instance
func (n *qNode) shallowCopy() *qNode {
	node := qNodePool.Get().(*qNode)
	atomic.StoreInt64(&node.lastActiveTime, atomic.LoadInt64(&n.lastActiveTime))
	node.res = n.res
	atomic.StoreInt32(&node.init, n.init)
	return node
}

func (n *qNode) set(value *Value) {
	n.res = newReservoir(value)
	atomic.StoreInt32(&n.init, qNodeFlagHasValue)
}

func (n *qNode) merge(value *Value) bool {
	ok := n.getReservoir().merge(value)
	if ok {
		atomic.CompareAndSwapInt32(&n.init, qNodeFlagNoValue, qNodeFlagHasValue)
	}
	return ok
}

func (n *qNode) marshal() []Value {
	if atomic.CompareAndSwapInt32(&n.init, qNodeFlagHasValue, qNodeFlagNoValue) {
		atomic.StoreInt64(&n.lastActiveTime, time.Now().UnixNano())
	}
	vs := n.getReservoir().marshal()
	return vs
}

func (n *qNode) getNext() (*qNode, bool) {
	for {
		currState := (*nState)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&n.nState))))
		if currState.next == nil {
			return nil, false
		}

		nextState := (*nState)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&currState.next.nState))))
		if !nextState.deleted {
			return currState.next, true
		}
		newState := nStatePool.Get().(*nState)
		newState.deleted, newState.next = currState.deleted, nextState.next
		if !atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&n.nState)), unsafe.Pointer(currState), unsafe.Pointer(newState)) {
			newState.recycle()
		}
	}
}

func (n *qNode) getLastActiveTime() int64 {
	return atomic.LoadInt64(&n.lastActiveTime)
}
