package core

import (
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/core/downloader"
	"code.byted.org/bcc/bcc-go-client/internal/core/dumps"
	"code.byted.org/bcc/bcc-go-client/internal/core/eventmgr"
	"code.byted.org/bcc/bcc-go-client/internal/core/fetcher"
	"code.byted.org/bcc/bcc-go-client/internal/core/transport"
)

func NewEventMgr(name string, opt *model.SdkOptions) common.EventMgr {
	return eventmgr.NewEventMgr(name, transport.Builder{}, fetcher.Builder{}, downloader.Builder{}, dumps.Builder{}, opt)
}
