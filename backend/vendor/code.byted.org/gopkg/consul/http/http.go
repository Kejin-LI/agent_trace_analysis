package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"code.byted.org/gopkg/consul"
)

const clusterSep = "$"

type ctxKey struct{}

type ClientOption func(t *http.Transport)

var dialer = net.Dialer{Timeout: 3 * time.Second}

func WithMaxIdleConnsPerCluster(n int) ClientOption {
	return func(t *http.Transport) {
		t.MaxIdleConnsPerHost = n
	}
}

func WithMaxIdleConns(n int) ClientOption {
	return func(t *http.Transport) {
		t.MaxIdleConns = n
	}
}

func WithIdleConnTimeout(timeout time.Duration) ClientOption {
	return func(t *http.Transport) {
		t.IdleConnTimeout = timeout
	}
}

func WithKeepAlives(b bool) ClientOption {
	return func(t *http.Transport) {
		t.DisableKeepAlives = !b
	}
}

type HttpClient struct {
	http.Client
}

func isIPAddr(s string) bool {
	h, _, _ := net.SplitHostPort(s)
	return h != "" && net.ParseIP(h) != nil
}

var DefaultTransport = NewTransport()

// NewTransport returns a http transport with consul dialer
func NewTransport(opts ...ClientOption) *http.Transport {
	transport := NewTransportWithDialer(dialer, opts...)
	return transport
}

func NewTransportWithDialer(d net.Dialer, opts ...ClientOption) *http.Transport {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if idx := strings.Index(addr, clusterSep); idx > 0 {
				addr = addr[:idx] // rm cluster name
			}
			callopts, ok := ctx.Value(ctxKey{}).(callOptions)
			if !ok || isIPAddr(addr) {
				return d.DialContext(ctx, network, addr)
			}
			if callopts.addr != "" {
				return d.DialContext(ctx, network, callopts.addr)
			}
			name := addr
			if idx := strings.Index(addr, ":"); idx > 0 { // rm :80
				name = addr[:idx]
			}
			oo := make([]consul.LookupOptions, 0, 2)
			oo = append(oo, consul.WithLimit(200))
			if callopts.cluster != "" {
				oo = append(oo, consul.WithCluster(callopts.cluster))
			}
			if callopts.idc != "" {
				oo = append(oo, consul.WithIDC(consul.IDC(callopts.idc)))
			}
			ee, err := consul.LookupName(name, oo...)
			if err != nil {
				return nil, err
			}
			for tries := 3; ; tries-- {
				conn, err := d.DialContext(ctx, network, ee.GetOne().Addr)
				if err == nil {
					return conn, nil
				}
				if tries <= 0 || ctx.Err() != nil {
					return nil, err
				}
			}
		},
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     10 * time.Second,
	}
	for _, op := range opts {
		op(transport)
	}
	return transport
}

var DefaultClient = NewHttpClient()

// NewHttpClient returns HttpClient with `http://{YOUR_SERVICE}/path/to/your/api` support
func NewHttpClient(opts ...ClientOption) *HttpClient {
	transport := NewTransport(opts...)
	c := http.Client{Transport: transport}
	return &HttpClient{Client: c}
}

type callOptions struct {
	ctx     context.Context
	cluster string
	addr    string
	idc     string
}

type CallOption func(opts *callOptions)

func WithContext(ctx context.Context) CallOption {
	return func(opts *callOptions) {
		opts.ctx = ctx
	}
}

func WithCluster(cluster string) CallOption {
	return func(opts *callOptions) {
		opts.cluster = cluster
	}
}

// force use the addr
func WithAddr(addr string) CallOption {
	return func(opts *callOptions) {
		opts.addr = addr
	}
}

func WithIDC(idc string) CallOption {
	return func(opts *callOptions) {
		opts.idc = idc
	}
}

func InjectCallOptionsIntoReq(req *http.Request, opts ...CallOption) *http.Request {
	var copts callOptions
	for _, op := range opts {
		op(&copts)
	}
	if copts.cluster != "" && !isIPAddr(req.URL.Host) {
		// add cluster name as a part of host
		//	in order to identity http connection pool
		req.URL.Host += clusterSep + copts.cluster
	}
	ctx := req.Context()
	if copts.ctx != nil {
		ctx = copts.ctx
	}
	ctx = context.WithValue(ctx, ctxKey{}, copts)
	return req.WithContext(ctx)
}

func (c *HttpClient) Do(req *http.Request, opts ...CallOption) (*http.Response, error) {
	return c.Client.Do(InjectCallOptionsIntoReq(req, opts...))
}

func (c *HttpClient) Get(url string, opts ...CallOption) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req, opts...)
}

func (c *HttpClient) Head(url string, opts ...CallOption) (*http.Response, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req, opts...)
}

var ua = fmt.Sprintf("Gopkg-consul/1.0 (Pid %d)", os.Getpid())

func (c *HttpClient) Post(url string, contentType string, body io.Reader, opts ...CallOption) (resp *http.Response, err error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", ua)
	return c.Do(req, opts...)
}

func (c *HttpClient) PostForm(url string, data url.Values, opts ...CallOption) (resp *http.Response, err error) {
	return c.Post(url, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()), opts...)
}
