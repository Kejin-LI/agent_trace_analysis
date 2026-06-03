package logid

import (
	"encoding/hex"
	"net"
	"time"

	"code.byted.org/gopkg/net2"
)

const (
	// IPUnknown represents unknown ip
	// 32 * 0
	IPUnknown = "00000000000000000000000000000000"
)

var localIP []byte

func init() {
	localIP = formatIP(net2.GetLocalIP())
}

// getMSTimestamp return the millisecond timestamp
func getMSTimestamp() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func formatIP(ip net.IP) []byte {
	if ip == nil {
		return []byte(IPUnknown)
	}
	dst := make([]byte, 32)
	i := ip.To16()
	hex.Encode(dst, i)
	return dst
}
