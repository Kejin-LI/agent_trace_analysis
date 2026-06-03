package metainfo

import (
	"context"

	"github.com/bytedance/gopkg/cloud/metainfo"
)

// HTTP header prefixes.
const (
	HTTPPrefixTransient  = metainfo.HTTPPrefixTransient
	HTTPPrefixPersistent = metainfo.HTTPPrefixPersistent
)

// HTTPHeaderToCGIVariable performs an CGI variable conversion.
// For example, an HTTP header key `abc-def` will result in `ABC_DEF`.
func HTTPHeaderToCGIVariable(key string) string {
	return metainfo.HTTPHeaderToCGIVariable(key)
}

// CGIVariableToHTTPHeader converts a CGI variable into an HTTP header key.
// For example, `ABC_DEF` will be converted to `abc-def`.
func CGIVariableToHTTPHeader(key string) string {
	return metainfo.CGIVariableToHTTPHeader(key)
}

// HTTPHeaderSetter sets a key with a value into a HTTP header.
type HTTPHeaderSetter = metainfo.HTTPHeaderSetter

// HTTPHeaderCarrier accepts a visitor to access all key value pairs in an HTTP header.
type HTTPHeaderCarrier = metainfo.HTTPHeaderCarrier

// HTTPHeader is provided to wrap an http.Header into an HTTPHeaderCarrier.
type HTTPHeader = metainfo.HTTPHeader

// FromHTTPHeader reads metainfo from a given HTTP header and sets them into the context.
// Note that this function does not call TransferForward inside.
func FromHTTPHeader(ctx context.Context, h HTTPHeaderCarrier) context.Context {
	return metainfo.FromHTTPHeader(ctx, h)
}

// ToHTTPHeader writes all metainfo into the given HTTP header.
// Note that this function does not call TransferForward inside.
func ToHTTPHeader(ctx context.Context, h HTTPHeaderSetter) {
	metainfo.ToHTTPHeader(ctx, h)
}
