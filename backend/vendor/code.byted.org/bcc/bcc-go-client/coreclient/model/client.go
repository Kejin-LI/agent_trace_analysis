package model

// Client 所有接口都是同步的，直到所有监听的函数回调完成才返回，不要在回调函数里执行耗时的操作
type Client interface {
	Name() string
	WatchKey(key string, callback KeyCallback, opts ...WatchOption) error
	WatchKeys(keys []string, callback KeyCallback, opts ...WatchOption) error
	WatchPath(path string, callback PathCallback, opts ...WatchOption) error
	CancelKey(key string, opt ...CancelOption)
	CancelKeys(keys []string, opt ...CancelOption)
	CancelPath(path string, opt ...CancelOption)
	Close()
}
