package bytedtracer

import "github.com/opentracing/opentracing-go"

type TagKV struct {
	key   string
	value interface{}
}

// Return a new TagKV.
func NewTagKV(key string, value interface{}) TagKV {
	return TagKV{key, value}
}

func (t *TagKV) GetKey() string {
	return t.key
}

func (t *TagKV) GetValue() interface{} {
	return t.value
}

func (t TagKV) Apply(op *opentracing.StartSpanOptions) {
	if op == nil {
		return
	}
	if op.Tags == nil {
		op.Tags = make(map[string]interface{})
	}
	op.Tags[t.key] = t.value
}

type GlobalTag struct {
	Key                string
	Value              string
	AsMetricsGlobalTag bool
}
