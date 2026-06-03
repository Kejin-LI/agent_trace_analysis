package ext

import bt "code.byted.org/bytedtrace/interface-go"

type stringTagName string

func (t stringTagName) Set(span bt.Span, value string) {
	span.SetTag(string(t), value)
}

func (t stringTagName) GetTagName() string {
	return string(t)
}

type int64TagName string

func (t int64TagName) Set(span bt.Span, value int64) {
	span.SetTag(string(t), value)
}

func (t int64TagName) GetTagName() string {
	return string(t)
}

// Note: snake-case tag key is recommanded
var (
	StressTag = stringTagName("stress_tag")
	MeshTag   = stringTagName("mesh")
	// language: go,java,python,cpp...
	LanguageTag = stringTagName("language")
	// version of the component using bytedtrace sdk
	ComponentVersionTag = stringTagName("component_version")

	// for biz
	DeviceID = int64TagName("device_id")
	AppID    = int64TagName("app_id")

	// for rds
	TableTag = stringTagName("table")
)

// Note: Do not use `.` or camel-case in tag name. Tags below is to keep compatibility with trace1.0
var (
	// value of `x-tt-env` in request. see: https://bytedance.feishu.cn/wiki/wikcn4ugSGAHoJhbQthT5ehuPJd
	RequestEnv = stringTagName("request.env")

	// http relative tags
	HTTPMethod     = stringTagName("http.method")
	HTTPURL        = stringTagName("http.url")
	HTTPParam      = stringTagName("http.param")
	HTTPHost       = stringTagName("http.host")
	HTTPStatusCode = int64TagName("http.status_code")

	// HTTPRoute represents HTTP route info registered in web framework.
	HTTPRoute = stringTagName("_http_route")
	// HTTPMethodV2 represents HTTP method as a metrics tag.
	HTTPMethodV2 = stringTagName("_http_method")
)
