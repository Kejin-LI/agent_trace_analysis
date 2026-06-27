package metrics

import (
	"code.byted.org/bcc/tools/uconv"
	m "code.byted.org/gopkg/metrics/v3"
)

func ptrTo(src string) *string {
	return &src
}

func Tag(name string, value interface{}) m.T {
	return m.T{Name: name, Value: uconv.ToString(value)}
}
