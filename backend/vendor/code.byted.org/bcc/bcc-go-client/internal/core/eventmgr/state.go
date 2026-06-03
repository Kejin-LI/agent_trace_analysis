package eventmgr

import "sync/atomic"

const (
	mgrRunning uint32 = 0
	mgrClosed  uint32 = 1
)

type mgrState struct {
	state uint32 //0 running,1 closed
}

func (m *mgrState) IsClosed() bool {
	return atomic.LoadUint32(&m.state) == mgrClosed
}

// 重复调用返回false
func (m *mgrState) Close() bool {
	return atomic.CompareAndSwapUint32(&m.state, mgrRunning, mgrClosed)
}
