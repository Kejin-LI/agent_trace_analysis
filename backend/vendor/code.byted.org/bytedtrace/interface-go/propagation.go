package bytedtracer

import (
	"github.com/opentracing/opentracing-go"
	"net/http"
)

const (
	// Propagator type
	TextMap               = "textMap"
	HTTPHeader            = "HTTPHeader"
	TextMapOpentracing    = opentracing.TextMap
	HTTPHeaderOpentracing = opentracing.HTTPHeaders
)

// TextMapCarrier allows the use of regular map[string]string
// as both TextMapWriter and TextMapReader.
// Copy from github.com/opentracing/opentracing-go@v1.1.0/propagation.go:126.
type TextMapCarrier map[string]string

// ForeachKey conforms to the TextMapReader interface.
func (c TextMapCarrier) ForeachKey(handler func(key, val string) error) error {
	for k, v := range c {
		if err := handler(k, v); err != nil {
			return err
		}
	}
	return nil
}

// Set implements Set() of opentracing.TextMapWriter.
func (c TextMapCarrier) Set(key, val string) {
	c[key] = val
}

// HTTPHeadersCarrier satisfies both TextMapWriter and TextMapReader.
//
// Example usage for server side:
//
//     carrier := bytedtrace.HTTPHeadersCarrier(httpReq.Header)
//     clientContext, err := bytedtrace.BytedExtract(bytedtrace.HTTPHeader, carrier)
//
// Example usage for client side:
//
//     carrier := bytedtrace.HTTPHeadersCarrier(httpReq.Header)
//     err := bytedtrace.BytedInject(
//         span.GetContext(),
//         bytedtrace.HTTPHeader,
//         carrier)
// Copy from github.com/opentracing/opentracing-go@v1.1.0/propagation.go:158.
type HTTPHeadersCarrier http.Header

// Set conforms to the TextMapWriter interface.
func (c HTTPHeadersCarrier) Set(key, val string) {
	h := http.Header(c)
	h.Set(key, val)
}

// ForeachKey conforms to the TextMapReader interface.
func (c HTTPHeadersCarrier) ForeachKey(handler func(key, val string) error) error {
	for k, vals := range c {
		for _, v := range vals {
			if err := handler(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}
