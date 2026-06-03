package metrics

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"code.byted.org/gopkg/env"
	"code.byted.org/gopkg/metrics/v3/msgpack"
)

const (
	maxSendSize = 1<<16 - 1
)

var (
	inTce = env.InTCE()
)

type mType int

type packet []byte

func (p *packet) Recycle() {
	if cap(*p) <= 1<<16 {
		*p = (*p)[:3]
		packetPool.Put(p)
	}
}

var (
	packetPool = sync.Pool{
		New: func() interface{} {
			p := packet(make([]byte, 3, 128))
			return &p
		},
	}
	tagLiteralPool = sync.Pool{
		New: func() interface{} {
			bytes := make([]byte, 0, 256)
			return &bytes
		},
	}
)

const (
	emitMask = "emit"

	unknownType mType = iota
	counterType
	timerType
	rateCounterType
	storeType
	meterType
)

func getAgentSockPath() (net.Conn, error) {
	var paths []string
	network := "unixgram"
	if isAgentSidecar() {
		paths = []string{"/sidecar/sock/metric_sdk1_seqpacket.sock", "/sidecar/sock/metric.sock"}
	} else {
		paths = []string{"/opt/tmp/sock/metric_sdk1_seqpacket.sock", "/opt/tmp/sock/metric.sock", "/tmp/metric.sock"}
	}

	var conn net.Conn
	var err error
	for _, path := range paths {
		for i := 0; i < 3; i++ {
			if strings.HasSuffix(path, "seqpacket.sock") {
				network = "unixpacket"
			} else {
				network = "unixgram"
			}

			conn, err = net.Dial(network, path)
			if err == nil {
				infoLog("using socket path: %s", path)
				return conn, nil
			}
			if strings.Contains(err.Error(), "no such file") {
				if enableDebug {
					warnLog("didn't found the socket file %s", path)
				}
				break
			}
		}
	}
	return nil, fmt.Errorf("metrics sdk can not get agent socket path: %s", err.Error())
}

func (t mType) String() string {
	switch t {
	case counterType:
		return "counter"
	case timerType:
		return "timer"
	case storeType:
		return "store"
	case rateCounterType:
		return "rate_counter"
	case meterType:
		return "meter"
	default:
		return "unknown"
	}
}

type agentWriter struct {
	net.Conn
}

// NewAgentWriter creates a new metrics agent(server) writer.
func NewAgentWriter() (io.WriteCloser, error) {
	conn, err := getAgentSockPath()
	if err != nil {
		return &errorWriter{}, err
	}
	return &agentWriter{
		conn,
	}, nil
}

func (w *agentWriter) Write(p []byte) (n int, err error) {
	now := time.Now()
	_ = w.Conn.SetWriteDeadline(now.Add(10 * time.Millisecond))
	return w.Conn.Write(p)
}

func (c *client) sideSend(timers []timerPacket) {
	p := *packetPool.Get().(*packet)
	length := 0
	for _, packet := range timers {
		c, tv := packet.c, packet.timer
		length += 1
		p = c.marshallWithTypeFloat(p, tv)
		measurePool.Put((*measure)(unsafe.Pointer(tv)))
	}
	if length > 0 {
		msgpack.FillArrayHeader(p, 0, uint16(length))
		c.sender.emit(p)
	}
	p.Recycle()
}

func (c *cursor) send(p packet) (packet, int) {
	var count int
	// TODO: use walking method of queue and closure handler replace follow codes
	node := c.values.getHead()
	var ok bool
	for {
		node, ok = node.getNext()
		if !ok {
			break
		}
		vs := node.marshal()
		for _, v := range vs {
			p = c.marshallWithTypeFloat(p, &v)
		}
		count += len(vs)
	}
	return p, count
}

func (c *cursor) marshallWithTypeFloat(p []byte, v *Value) []byte {
	// protocol: 6 fields: emit $type $prefix.name  $value $tag ""
	p = msgpack.AppendArrayHeader(p, 6)
	p = msgpack.AppendString(p, emitMask)

	// process metric type
	p = msgpack.AppendString(p, v.mType.String())

	// process metric name
	var hasSuffix bool
	length := len(c.prefix)
	if v.suffix != "" {
		length += 1 + len(v.suffix)
		hasSuffix = true
	}
	p = msgpack.AppendStringHeader(p, uint16(length))
	p = append(p, c.prefix...)
	if hasSuffix {
		p = append(p, '.')
		p = append(p, v.suffix...)
	}

	// process metric value
	value := float64(v.value) + v.valuef
	intVal := int64(value)
	if float64(intVal) == value {
		p = msgpack.AppendStringHeader(p, uint16(int64StrSize(intVal)))
		p = appendInt64(p, intVal)
	} else {
		p = appendFloat64WithSize(p, value)
	}

	// process tag serialization
	tagLiteralPtr := tagLiteralPool.Get().(*[]byte)
	tagLiteral := *tagLiteralPtr
	tagLiteral = append(tagLiteral, c.globalTagLiteral...)
	for i, name := range c.metric.tags {
		value := c.index[i]
		tagLiteral = appendTagsWithoutValidationByRaw(tagLiteral, name, value)
	}
	p = msgpack.AppendStringHeader(p, uint16(len(tagLiteral)))
	p = append(p, tagLiteral...)

	// copy from v1, emit metric with specific time is deprecated
	p = msgpack.AppendString(p, "")

	if cap(tagLiteral) <= 1<<16 {
		tagLiteral = tagLiteral[:0]
		tagLiteralPool.Put(tagLiteralPtr)
	}
	return p
}

type sender struct {
	sync.RWMutex
	w io.WriteCloser
}

func newSender(writer io.WriteCloser) *sender {
	senderP := &sender{
		w: writer,
	}
	return senderP
}

func (s *sender) close() {
	s.RLock()
	_ = s.w.Close()
	s.RUnlock()
}

const retryCount = 3

func (s *sender) emit(p packet) {
	var err error
	for i := 0; i < retryCount; i++ {
		s.RLock()
		_, err = s.w.Write(p)
		s.RUnlock()

		if err == nil {
			break
		}
	}

	if err != nil {
		if inTce {
			errorLog(
				"metrics write message error: %s", err,
			)
		}
		s.Lock()
		switch s.w.(type) {
		case *agentWriter, *errorWriter:
			if inTce {
				errorLog("metrics agent socket re-connecting")
			}
			w, err := NewAgentWriter()
			if err != nil {
				if inTce {
					errorLog(
						"metrics socket re-connect error: %s", err,
					)
				}
			} else {
				_ = s.w.Close()
				s.w = w
			}
		default:
		}
		s.Unlock()
	}
}
