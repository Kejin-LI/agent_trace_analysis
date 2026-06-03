package dumps

import (
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
)

type Builder struct {
}

func (b Builder) Build(name string, opt *model.SdkOptions) common.Dumper {
	return newDumper(name, opt)
}
