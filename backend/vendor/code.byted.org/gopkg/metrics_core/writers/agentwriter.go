package writers

import (
	"fmt"
	"io"
	"net"
	"time"

	"code.byted.org/aiops/monitoring-common-go/connpool"
)

const (
	agentTimeOut = time.Millisecond * 100
)

type WriterSignal int

const (
	WriteSigSlow WriterSignal = iota
)

type WriterSignalReceiver interface {
	io.WriteCloser
	Signal(sig WriterSignal)
}

type AgentWriterOption func(writer *agentWriter)

func WithAddrs(addrs ...string) AgentWriterOption {
	return func(writer *agentWriter) {
		writer.Addrs = addrs
	}
}

func WithConnNetwork(network string) AgentWriterOption {
	return func(writer *agentWriter) {
		writer.Network = network
	}
}

func WithPoolManager(poolManager ConnPoolManager) AgentWriterOption {
	return func(writer *agentWriter) {
		writer.poolMgr = poolManager
	}
}

type agentWriter struct {
	socketConfig
	poolMgr ConnPoolManager

	p connpool.Pool
}

// NewAgentWriter creates a new metrics agent(server) writer.
func NewAgentWriter(ops ...AgentWriterOption) (io.WriteCloser, error) {
	writer := &agentWriter{
		socketConfig: socketConfig{Network: "unixpacket"},
	}
	for _, op := range ops {
		op(writer)
	}

	if writer.Addrs == nil {
		return writer, fmt.Errorf("no sock addr set")
	}

	if writer.Network == "" {
		return writer, fmt.Errorf("no network type set")
	}

	if writer.poolMgr == nil {
		return writer, fmt.Errorf("no ConnPoolManager set")
	}

	p, err := writer.poolMgr.GetPool(&writer.socketConfig)
	if err != nil {
		return writer, err
	}
	writer.p = p
	return writer, nil
}

func (w *agentWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.p == nil {
		if w.poolMgr == nil {
			return 0, fmt.Errorf("pool manager not init")
		}
		w.p, err = w.poolMgr.GetPool(&w.socketConfig)
		if err != nil {
			return 0, err
		}
	}
	c, err := w.p.Get()
	if err != nil {
		return 0, err
	}
	conn := c.(net.Conn)
	_ = conn.SetWriteDeadline(time.Now().Add(agentTimeOut))
	n, err = conn.Write(p)
	if err != nil {
		_ = w.p.Close(c)
		return n, err
	}
	_ = w.p.Put(c)
	return n, err
}

func (w *agentWriter) Close() error {
	if w.p == nil || w.poolMgr == nil {
		return nil
	}
	w.poolMgr.ReleasePool(&w.socketConfig)
	return nil
}

func (w *agentWriter) Signal(sig WriterSignal) {
	if w.poolMgr == nil {
		return
	}
	switch sig {
	case WriteSigSlow:
		w.poolMgr.IncreasePoolConns(&w.socketConfig)
	}
}
