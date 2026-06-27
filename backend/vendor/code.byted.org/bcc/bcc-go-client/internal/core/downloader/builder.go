package downloader

import (
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
)

type Builder struct {
}

func (b Builder) Build(eventHandler common.MsgHandler, opt *model.SdkOptions) common.Loader {
	loader := NewDownloaderMgr(eventHandler, opt.DownloadOption)
	return loader
}
