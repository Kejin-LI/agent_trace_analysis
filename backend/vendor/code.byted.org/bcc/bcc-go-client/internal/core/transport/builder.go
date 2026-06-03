package transport

import (
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
)

type Builder struct {
}

func (b Builder) Build(eventHandler common.MsgHandler, name string, opt *model.SdkOptions) common.Connection {
	return newTransportClient(eventHandler, name, opt)
}
