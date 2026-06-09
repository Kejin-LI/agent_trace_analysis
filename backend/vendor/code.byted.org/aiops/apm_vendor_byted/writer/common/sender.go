package common

import (
	"fmt"
	"time"

	"code.byted.org/aiops/metrics_codec"
)

type MetricsPacketSender interface {
	IsDeepCopyData() bool
	IsCodecV4Supported() bool
	IsDebugMode() bool

	GetDone() <-chan struct{}
	GetPacketPool() *PacketPool
	GetPacketChan() chan *MetricPacket

	DirectSend(*MetricPacket) error
	Compress(*MetricPacket, []byte) error
}

func ConvertAndSend(sender MetricsPacketSender, rawData []byte) (n int, err error) {
	n = len(rawData)
	if n == 0 {
		return n, nil
	}
	packet := sender.GetPacketPool().GetPacket()
	packet, err = convertToPacket(packet, rawData, sender.IsCodecV4Supported(), sender.IsDeepCopyData(), sender.Compress)
	if err != nil {
		sender.GetPacketPool().PutPacket(packet)
		return n, err // Return the error if conversion fails
	}
	// Send the packet to the channel, report an error if the channel is closed.
	if sender.IsDebugMode() {
		LogFunc("send metrics packet of original length: %d to the channel", packet.OriginSize)
	}
	select {
	case sender.GetPacketChan() <- packet:
	default:
		return n, fmt.Errorf("failed to send packet, channel is closed")
	}

	return n, nil // Successfully written
}

func RunSendTask(sender MetricsPacketSender, taskID, retryCount int, done func()) {
	if sender.IsDebugMode() {
		LogFunc("start send task[%d]", taskID)
	}
	defer func() {
		done()
		if sender.IsDebugMode() {
			LogFunc("exit send task[%d]", taskID)
		}
	}()

	for {
		select {
		case packet := <-sender.GetPacketChan():
			err := sendWithRetry(sender, retryCount, packet)
			if err != nil {
				LogFunc("flush error: %v", err)
			}
			sender.GetPacketPool().PutPacket(packet)
			continue
		case <-sender.GetDone():
		}

		// flush the remaining data.
		for {
			select {
			case remainingPacket := <-sender.GetPacketChan():
				err := sendWithRetry(sender, retryCount, remainingPacket)
				if err != nil {
					LogFunc("flush error: %v", err)
				}
				sender.GetPacketPool().PutPacket(remainingPacket)
			default:
				return
			}
		}
	}
}

func sendWithRetry(sender MetricsPacketSender, retryTime int, packet *MetricPacket) (err error) {
	if sender.IsDeepCopyData() {
		err = sender.Compress(packet, packet.RawData)
		if err != nil {
			return err
		}
	}

	for i := 0; i < retryTime; i++ {
		err = sender.DirectSend(packet)
		if err == nil {
			return nil
		}
		time.Sleep(time.Millisecond * 100)
	}
	return err
}

func convertToPacket(packet *MetricPacket, data []byte, isCodecV4Supported, isDeepCopyData bool,
	compress func(*MetricPacket, []byte) error) (*MetricPacket, error) {
	var err error
	if len(data) <= CodecVersionOffset {
		return nil, metrics_codec.ErrInvalidMessageHeader
	}

	codecVersion := metrics_codec.DecodeUint8(data[CodecVersionOffset:])
	var isCodecV4 bool
	switch codecVersion {
	case CodecV3Code:
		isCodecV4 = false
	case CodecV4Code:
		if !isCodecV4Supported {
			return packet, fmt.Errorf("producer proxy not support codec-v4")
		}
		isCodecV4 = true
	default:
		return nil, metrics_codec.ErrUnsupportedCodecVersion
	}
	packet.IsCodecV4 = isCodecV4

	if isDeepCopyData {
		_ = packet.Append(data)
	} else {
		err = compress(packet, data)
		packet.OriginSize = len(data)
	}
	return packet, err
}
