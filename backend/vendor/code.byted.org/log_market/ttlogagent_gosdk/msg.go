package ttlogagent_gosdk

import (
	"errors"
	"sync"

	"github.com/gogo/protobuf/proto"
)

type msgBatch interface {
	proto.Message
	appendMsg(msg Msg) error
	cleanMsgs()
	msgNumber() int
}

type Msg interface {
	proto.Marshaler
	Size() int
	Truncate()
}

var (
	msgV3Pool = sync.Pool{
		New: func() interface{} {
			return &MsgV3{}
		},
	}

	traceLogPool = sync.Pool{
		New: func() interface{} {
			return &TraceLog{}
		},
	}
)

func NewMsgV3(data []byte, header *MsgV3Header, tags ...string) *MsgV3 {
	m := msgV3Pool.Get().(*MsgV3)
	m.Data = data
	m.Header = header

	tagSize := len(tags)

	if tagSize%2 != 0 {
		tags = append(tags, "")
		tagSize++
	}
	KVSize := tagSize / 2
	if cap(m.Tags) < KVSize {
		m.Tags = make([]*KeyValueV3, KVSize)
	}

	m.Tags = m.Tags[:0]
	for i := 0; i < KVSize; i++ {
		m.Tags = append(m.Tags, &KeyValueV3{
			Key:   tags[i*2],
			Value: tags[i*2+1],
		})
	}
	return m
}

func NewTraceLog() *TraceLog {
	return traceLogPool.Get().(*TraceLog)
}

func (b *MsgV3Batch) appendMsg(msg Msg) error {
	if m, ok := msg.(*MsgV3); ok {
		b.Msgs = append(b.Msgs, m)
		return nil
	} else {
		return errors.New("only *MsgV3 accepted in MsgV3Batch")
	}
}

func (b *MsgV3Batch) cleanMsgs() {
	for _, msg := range b.Msgs {
		msg.Header = nil
		msg.Tags = msg.Tags[:0]
		msg.Data = msg.Data[:0]
		msgV3Pool.Put(msg)
	}
	b.Msgs = b.Msgs[:0]
}

func (b *MsgV3Batch) msgNumber() int {
	return len(b.Msgs)
}

func (b *TraceLogBatch) appendMsg(msg Msg) error {
	if m, ok := msg.(*TraceLog); ok {
		b.TraceLogs = append(b.TraceLogs, m)
		return nil
	} else {
		return errors.New("only *TraceLog accepted in TraceLogBatch")
	}
}

func (b *TraceLogBatch) cleanMsgs() {
	for _, log := range b.TraceLogs {
		log.Tags = log.Tags[:0]
		log.LogID = ""
		log.Level = ""
		log.SpanID = 0
		log.ParentSpanID = 0
		log.Location = ""
		log.Type = ""
		log.RemoteMethod = ""
		log.LocalMethod = ""
		log.RemoteCluster = ""
		log.RemoteService = ""
		log.CostInUs = 0
		log.RemoteAddress = nil
		log.StatusCode = 0
		log.Ts = 0
		traceLogPool.Put(log)
	}
	b.TraceLogs = b.TraceLogs[:0]
}

func (b *TraceLogBatch) msgNumber() int {
	return len(b.TraceLogs)
}

var truncatedReason = []byte(" [ truncated by ttlogagent_gosdk")

func (m *MsgV3) Truncate() {
	if m.Data == nil {
		return
	}

	if len(m.Data) >= oneMessageLimitByte {
		m.Data = m.Data[:oneMessageLimitByte-1024]
		m.Data = append(m.Data, truncatedReason...)
	}

	newSize := m.Size()
	if newSize >= oneMessageLimitByte {
		m.truncateKVs()
	}

}

func (m *MsgV3) truncateKVs() {
	if len(m.Tags) == 0 {
		return
	}
	i := len(m.Tags)

	averageSize := (oneMessageLimitByte - 1 - len(m.Data) - sovMsgV3(uint64(len(m.Data))) -
		1 - m.Header.Size() - sovMsgV3(uint64(m.Header.Size()))) / i
	if averageSize <= 0 {
		m.Tags = m.Tags[:0]
		return
	}

	length := len(truncatedReason)
	for i, _ := range m.Tags {
		l := m.Tags[i].Size()
		if l >= averageSize {
			l1 := len(m.Tags[i].Key)
			tagRealLen := 1 + l1 + sovMsgV3(uint64(l1))
			l2 := len(m.Tags[i].Value)
			valRealLen := 1 + l2 + sovMsgV3(uint64(l2))

			if valRealLen+tagRealLen > averageSize {
				valueLen := len(m.Tags[i].Value)
				if averageSize-tagRealLen-length >= 0 && averageSize-tagRealLen-length <= valueLen {
					m.Tags[i].Value = m.Tags[i].Value[:averageSize-tagRealLen-length] + string(truncatedReason)
				}

				if averageSize-tagRealLen-length < 0 {
					m.Tags[i].Value = string(truncatedReason)
				}
			}

		}
	}
}

func (t *TraceLog) Truncate() {

}
