package metainfo

import (
	"context"

	"github.com/bytedance/gopkg/cloud/metainfo"
)

// WithBackwardValues returns a new context that allows passing key-value pairs backward from any derived context.
func WithBackwardValues(ctx context.Context) context.Context {
	return metainfo.WithBackwardValues(ctx)
}

// SetBackwardValue .
func SetBackwardValue(ctx context.Context, k, v string) (ok bool) {
	return metainfo.SetBackwardValue(ctx, k, v)
}

// GetBackwardValue .
func GetBackwardValue(ctx context.Context, key string) (val string, ok bool) {
	return metainfo.GetBackwardValue(ctx, key)
}

// GetAllBackwardValues retrieves all key-value pairs set by SetBackwardValue from the given context.
// If the context is not created by WithBackwardValues, the result will be nil.
func GetAllBackwardValues(ctx context.Context) map[string]string {
	return metainfo.GetAllBackwardValues(ctx)
}
