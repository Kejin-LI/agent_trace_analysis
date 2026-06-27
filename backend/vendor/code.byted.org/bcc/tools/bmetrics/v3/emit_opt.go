package metrics

type emitOption struct {
	suffix string
}
type EmitOption func(opt *emitOption)

func WithSuffix(suffix string) EmitOption {
	return func(o *emitOption) {
		o.suffix = suffix
	}
}
