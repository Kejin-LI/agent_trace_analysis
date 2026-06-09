package common

import (
	"sync"

	"code.byted.org/aiops/metrics_codec"
)

type MetricPacket struct {
	IsCodecV4      bool
	RawData        []byte
	CompressData   []byte
	CompressHeader []byte
	OriginSize     int
}

func (packet *MetricPacket) Compress(rawData []byte) error {
	var err error
	packet.CompressData, err = metrics_codec.Compress(packet.CompressData, rawData, metrics_codec.Zlib)
	return err
}

func (packet *MetricPacket) Append(rawData []byte) error {
	var err error
	packet.RawData = append(packet.RawData, rawData...)
	packet.OriginSize = len(rawData)
	return err
}

type PacketPool struct {
	sync.Pool
}

func NewPacketPool() *PacketPool {
	return &PacketPool{
		Pool: sync.Pool{
			New: func() interface{} {
				return &MetricPacket{
					RawData:        make([]byte, 0, 1024),
					CompressData:   make([]byte, 0, 1024),
					CompressHeader: make([]byte, 0, 20),
				}
			},
		},
	}
}

func (p *PacketPool) GetPacket() *MetricPacket {
	m := p.Get().(*MetricPacket)
	m.IsCodecV4 = false
	m.RawData = m.RawData[:0]
	m.CompressData = m.CompressData[:0]
	m.CompressHeader = m.CompressHeader[:0]
	m.OriginSize = 0
	return m
}

func (p *PacketPool) PutPacket(m *MetricPacket) {
	if m != nil {
		p.Put(m)
	}
}
