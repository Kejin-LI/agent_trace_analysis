// Package metainfo provides APIs to manipulate information with context.Context in a form of key value string pairs.
package metainfo

import (
	"context"

	"github.com/bytedance/gopkg/cloud/metainfo"
)

// The prefix listed below may be used to tag the types of values when there is no context to carry them.
const (
	PrefixPersistent        = metainfo.PrefixPersistent
	PrefixTransient         = metainfo.PrefixTransient
	PrefixTransientUpstream = metainfo.PrefixTransientUpstream
)

// **Using empty string as key or value is not support.**

// TransferForward converts transient values to transient-upstream values and filters out original transient-upstream values.
// It should be used before the context is passing from server to client.
func TransferForward(ctx context.Context) context.Context {
	return metainfo.TransferForward(ctx)
}

// GetValue retrieves the value set into the context by the given key.
func GetValue(ctx context.Context, k string) (string, bool) {
	return metainfo.GetValue(ctx, k)
}

// GetAllValues retrieves all transient values
func GetAllValues(ctx context.Context) map[string]string {
	return metainfo.GetAllValues(ctx)
}

// RangeValues calls f sequentially for each transient kv.
// If f returns false, range stops the iteration.
func RangeValues(ctx context.Context, f func(k, v string) bool) {
	metainfo.RangeValues(ctx, f)
}

// WithValue sets the value into the context by the given key.
// This value will be propagated to the next service/endpoint through an RPC call.
//
// Notice that it will not propagate any further beyond the next service/endpoint,
// Use WithPersistentValue if you want to pass a key/value pair all the way.
func WithValue(ctx context.Context, k, v string) context.Context {
	return metainfo.WithValue(ctx, k, v)
}

// DelValue deletes a key/value from the current context.
// Since empty string value is not valid, we could just set the value to be empty.
func DelValue(ctx context.Context, k string) context.Context {
	return metainfo.DelValue(ctx, k)
}

// GetPersistentValue retrieves the persistent value set into the context by the given key.
func GetPersistentValue(ctx context.Context, k string) (string, bool) {
	return metainfo.GetPersistentValue(ctx, k)
}

// GetAllPersistentValues retrieves all persistent values.
func GetAllPersistentValues(ctx context.Context) map[string]string {
	return metainfo.GetAllPersistentValues(ctx)
}

// RangePersistentValues calls f sequentially for each persistent kv.
// If f returns false, range stops the iteration.
func RangePersistentValues(ctx context.Context, f func(k string, v string) bool) {
	metainfo.RangePersistentValues(ctx, f)
}

// WithPersistentValue sets the value info the context by the given key.
// This value will be propagated to the services along the RPC call chain.
func WithPersistentValue(ctx context.Context, k, v string) context.Context {
	return metainfo.WithPersistentValue(ctx, k, v)
}

// DelPersistentValue deletes a persistent key/value from the current context.
// Since empty string value is not valid, we could just set the value to be empty.
func DelPersistentValue(ctx context.Context, k string) context.Context {
	return metainfo.DelPersistentValue(ctx, k)
}
