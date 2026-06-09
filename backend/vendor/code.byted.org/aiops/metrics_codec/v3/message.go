package v3

import (
	"sync"

	"code.byted.org/aiops/metrics_codec"
)

const (
	magicCode int64 = 0x6d65747269637332
	version   int8  = 3
)

var messagePool = sync.Pool{
	New: func() interface{} {
		return &Message{
			Header: &MessageHeader{
				magicCode: magicCode,
				version:   version,
				Tenant:    "default",
				flags:     0x03,
			},
			Body: []*Metric{},
			dc:   "",
			host: "",
		}
	},
}

//Message
//+------------+---------+-----+--------+
//| Msg header | Metric  | ... | Metric |
//+------------+---------+-----+--------+

type Message struct {
	Header *MessageHeader
	Body   Body
	dc     string
	host   string
}

func NewMessage() *Message {
	m, ok := messagePool.Get().(*Message)
	if !ok {
		return nil
	}
	m.Header.Reset()
	m.Body = m.Body[:0]
	m.dc = ""
	m.host = ""
	return m
}

func PutMessage(m *Message) {
	m.Header.Reset()
	for _, v := range m.Body {
		for _, m := range v.Measurements {
			m.Recycle()
		}
	}
	messagePool.Put(m)
}

func (m *Message) SetTimestamp(ts int64) {
	m.Header.Timestamp = ts
}

func (m *Message) SetDcHost(dc string, host string) error {
	var err error
	if metrics_codec.IsValidTagValue(dc) {
		m.dc = dc
	} else {
		err = metrics_codec.ErrInvalidString
	}

	if metrics_codec.IsValidTagValue(host) {
		m.host = host
	} else {
		err = metrics_codec.ErrInvalidString
	}
	if m.dc != "" && m.host != "" {
		m.Header.flags |= 0x4
	}

	return err
}

func (m *Message) Encode(buf []byte) []byte {
	if len(m.Body) == 0 {
		return buf
	}
	buf = m.Header.Encode(buf)
	startAt := len(buf)
	for _, metric := range m.Body {
		buf = metric.Encode(buf, m.dc, m.host)
	}

	m.Header.body.FillBack(buf, int32(startAt), int32(len(buf)-startAt), int32(len(m.Body)))
	return buf
}

// MessageHeader
// +----------+-------+-----------+--------+----------------+-------------------+---------+-----+------------+------+-------+-------+-----------+
// |   8B     |  1B   |    4B     |   4B   |      1B        |         4B        |    8B   | 4B  |    4B      | vStr |  vStr | vStr  |    vStr   |
// +----------+-------+-----------+--------+----------------+-------------------+---------+-----+------------+------+-------+-------+-----------+
// |magic code|version|body offset|body len|body compression|body compressed len|timestamp|flags|metric count|tenant|account|project|sdk_version|
// +----------+-------+-----------+--------+----------------+-------------------+---------+-----+------------+------+-------+-------+-----------+
type MessageHeader struct {
	magicCode  int64
	version    int8
	body       BodyHeader
	Timestamp  int64
	flags      int32
	metricNum  int32
	Tenant     string
	Account    string
	Project    string
	SdkVersion string
}

func NewMessageHeader() *MessageHeader {
	return &MessageHeader{
		magicCode: magicCode,
		version:   version,
		Tenant:    "default",
		flags:     0x03,
	}
}

func (h *MessageHeader) Reset() {
	h.metricNum = 0
	h.Timestamp = 0
	h.flags = 0x03
	h.Tenant = "default"
	h.Account = ""
	h.Project = ""
	h.SdkVersion = ""
	h.body.offset = 0
}

type Body []*Metric

// Size returns the size of the message header.
func (h *MessageHeader) Size() int {
	return metrics_codec.INT64_LEN + metrics_codec.INT8_LEN +
		bodyHeaderSize + metrics_codec.INT64_LEN +
		metrics_codec.INT32_LEN + metrics_codec.INT32_LEN +
		metrics_codec.INT32_LEN + len(h.Tenant) +
		metrics_codec.INT32_LEN + len(h.Account) +
		metrics_codec.INT32_LEN + len(h.Project) +
		metrics_codec.INT32_LEN + len(h.SdkVersion)
}

func (h *MessageHeader) Encode(buf []byte) []byte {
	buf, _ = h.encode(buf)
	return buf
}

func (h *MessageHeader) encode(buf []byte) ([]byte, int) {
	if h == nil {
		h = NewMessageHeader()
	}
	buf = metrics_codec.EncodeInt64(buf, h.magicCode)
	buf = metrics_codec.EncodeInt8(buf, h.version)
	offset32 := len(buf)
	buf = h.body.Encode(buf)
	buf = metrics_codec.EncodeInt64(buf, h.Timestamp) // Timestamp
	buf = metrics_codec.EncodeInt32(buf, h.flags)
	buf = metrics_codec.EncodeInt32(buf, h.metricNum) // Just occupy the 4 bytes
	buf = metrics_codec.EncodeStr(buf, h.Tenant)
	buf = metrics_codec.EncodeStr(buf, h.Account)
	buf = metrics_codec.EncodeStr(buf, h.Project)
	buf = metrics_codec.EncodeStr(buf, h.SdkVersion)
	return buf, offset32
}

func (h *MessageHeader) Clone(h2 *MessageHeader) {
	h.magicCode = h2.magicCode
	h.version = h2.version
	h.Timestamp = h2.Timestamp
	h.flags = h2.flags

	h.metricNum = h2.metricNum
	h.Tenant = h2.Tenant
	h.Account = h2.Account
	h.Project = h2.Project
	h.SdkVersion = h2.SdkVersion
}

func (h *MessageHeader) GetFlags() int32 {
	return h.flags
}

func (h *MessageHeader) SetFlags(flags int32) {
	h.flags = flags
}

type BodyHeader struct {
	Offset       int32
	Len          int32
	CompressMode int8
	CompressLen  int32
	offset       int
}

func (bh *BodyHeader) Encode(buf []byte) []byte {
	bh.offset = len(buf)
	buf = metrics_codec.EncodeInt32(buf, bh.Offset)
	buf = metrics_codec.EncodeInt32(buf, bh.Len)
	buf = metrics_codec.EncodeInt8(buf, bh.CompressMode)
	buf = metrics_codec.EncodeInt32(buf, bh.CompressLen)
	return buf
}

func (bh *BodyHeader) FillBack(buf []byte, bodyOffset int32, bodyLen int32, metricCount int32) {
	metrics_codec.FillBackInt32(buf, bh.offset, bodyOffset)
	metrics_codec.FillBackInt32(buf, bh.offset+metrics_codec.INT32_LEN, bodyLen)
	metrics_codec.FillBackInt8(buf, bh.offset+comperssAlgOffset, 0)     // compress mod
	metrics_codec.FillBackInt32(buf, bh.offset+compressAlgLenOffset, 0) // compress len
	// Fill back the metric count
	// There are body offset, body len, body compress alg, body compressed len, timestamp, and flags before the metric account.
	// 4 + 4 + 1 + 4 + 8 + 4
	metrics_codec.FillBackInt32(buf, bh.offset+metricCountOffset, metricCount)
}
