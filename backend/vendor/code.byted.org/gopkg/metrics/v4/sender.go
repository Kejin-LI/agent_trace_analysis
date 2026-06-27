package metrics

import (
	"io"

	"code.byted.org/aiops/apm_vendor_byted/writer/tcp"
	"code.byted.org/gopkg/metrics_core/logs"
	"code.byted.org/gopkg/metrics_core/writers"
)

// NewAgentWriter creates a new metrics agent(server) writer.
func NewAgentWriter() (io.WriteCloser, error) {
	return writers.NewAgentWriter()
}

// Deprecated:
// use tcp.NewTcpWriter instead
var NewAgentlessWriter = NewTcpWriter

// NewTcpWriter creates a new tcp writer.
func NewTcpWriter() io.WriteCloser {
	tcpSender, err := tcp.NewTcpWriter()
	if err != nil {
		logs.Error("failed to create tcp writer: %v, using a noop writer to discard all data", err)
	}
	return tcpSender
}
