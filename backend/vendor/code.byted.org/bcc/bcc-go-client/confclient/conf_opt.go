package confclient

// Option for config
type (
	ConfOption func(o *confOption)
	confOption struct {
		scope    map[string]interface{}
		deepcopy bool
		model    interface{}
	}
)

var defaultConfOption = &confOption{}

func newConfOption(opts ...ConfOption) *confOption {
	if len(opts) == 0 {
		return defaultConfOption
	}
	t := &confOption{}
	for _, o := range opts {
		o(t)
	}
	return t
}

func WithConfScope(scope map[string]interface{}) ConfOption {
	return func(o *confOption) {
		o.scope = scope
	}
}

// Conf model
func WithConfModel(model interface{}) ConfOption {
	return func(o *confOption) {
		o.model = model
	}
}

func WithConfDeepcopy(deepcopy bool) ConfOption {
	return func(o *confOption) {
		o.deepcopy = deepcopy
	}
}
