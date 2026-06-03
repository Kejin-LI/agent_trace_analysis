/*
Copyright 2014 Workiva, LLC
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
 http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package metrics

import (
	"errors"
	"runtime"
	"sync/atomic"
)

var (
	// ErrDisposed is returned when an operation is performed on a disposed
	// queue.
	ErrDisposed = errors.New(`queue: disposed`)

	// ErrEmpty is returned when an applicable queue operation times out.
	ErrEmpty = errors.New(`queue: poll empty`)
	empty    = timerPacket{nil, nil}
)

// roundUp takes a uint64 greater than 0 and rounds it up to the next
// power of 2.
func roundUp(v uint64) uint64 {
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v |= v >> 32
	v++
	return v
}

type ringNode struct {
	position uint64
	data     timerPacket
}

type nodes []ringNode

// ringBuffer is a MPMC buffer that achieves threadsafety with CAS operations
// only.  A put on full or get on empty call will block until an item
// is put or retrieved.  Calling Dispose on the ringBuffer will unblock
// any blocked threads with an error.  This buffer is similar to the buffer
// described here: http://www.1024cores.net/home/lock-free-algorithms/queues/bounded-mpmc-queue
// with some minor additions.
type ringBuffer struct {
	_padding0      [8]uint64
	queue          uint64
	_padding1      [8]uint64
	dequeue        uint64
	_padding2      [8]uint64
	mask, disposed uint64
	_padding3      [8]uint64
	nodes          nodes
}

func (rb *ringBuffer) init(size uint64) {
	size = roundUp(size)
	rb.nodes = make(nodes, size)
	for i := uint64(0); i < size; i++ {
		rb.nodes[i] = ringNode{position: i}
	}
	rb.mask = size - 1 // so we don't have to do this with every put/get operation
}

// offer adds the provided item to the queue if there is space.  If the queue
// is full, this call will return false.  An error will be returned if the
// queue is disposed.
func (rb *ringBuffer) offer(item timerPacket) (bool, error) {
	return rb.put(item, true)
}

func (rb *ringBuffer) put(item timerPacket, offer bool) (bool, error) {
	var n *ringNode
	pos := atomic.LoadUint64(&rb.queue)
L:
	for {
		if atomic.LoadUint64(&rb.disposed) == 1 {
			return false, ErrDisposed
		}

		n = &rb.nodes[pos&rb.mask]
		seq := atomic.LoadUint64(&n.position)
		switch dif := seq - pos; {
		case dif == 0:
			if atomic.CompareAndSwapUint64(&rb.queue, pos, pos+1) {
				break L
			}
		case dif < 0:
			panic(`Ring buffer in a compromised state during a put operation.`)
		default:
			pos = atomic.LoadUint64(&rb.queue)
		}

		if offer {
			return false, nil
		}

		runtime.Gosched() // free up the cpu before the next iteration
	}

	n.data = item
	atomic.StoreUint64(&n.position, pos+1)
	return true, nil
}

// poll will return the next item in the queue.  This call will block
// if the queue is empty.  This call will unblock when an item is added
// to the queue, Dispose is called on the queue, or the timeout is reached. An
// error will be returned if the queue is disposed.
func (rb *ringBuffer) poll() (timerPacket, error) {
	var (
		n     *ringNode
		pos   = atomic.LoadUint64(&rb.dequeue)
	)
	if atomic.LoadUint64(&rb.disposed) == 1 {
		return empty, ErrDisposed
	}

	n = &rb.nodes[pos&rb.mask]
	seq := atomic.LoadUint64(&n.position)
	switch dif := seq - (pos + 1); {
	case dif == 0:
		if atomic.CompareAndSwapUint64(&rb.dequeue, pos, pos+1) {
			data := n.data
			n.data = empty
			atomic.StoreUint64(&n.position, pos+rb.mask+1)
			return data, nil
		}
	case dif < 0:
		panic(`Ring buffer in compromised state during a get operation.`)
	default:
		pos = atomic.LoadUint64(&rb.dequeue)
	}
	return empty, ErrEmpty
}

// newRingBuffer will allocate, initialize, and return a ring buffer
// with the specified size.
func newRingBuffer(size uint64) *ringBuffer {
	rb := &ringBuffer{}
	rb.init(size)
	return rb
}
