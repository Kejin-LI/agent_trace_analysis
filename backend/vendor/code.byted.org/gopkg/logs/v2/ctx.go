package logs

import (
	"sync"

	"context"
)

const (
	kvCtxKey        = "K_KVs_V2"
	noticeCtxKey    = "K_NOTICE"
	stackInfoCtxKey = "K_STACK_INFO"
)

type StackInfo byte

const (
	NoPrint StackInfo = iota
	CurrGoroutine
	AllGoroutines
)

// CtxAddKVs works like logs.CtxAddKVs, it provides compatible way to add kv into context.
// But it cannot keep the order of these key-value pairs
func CtxAddKVs(ctx context.Context, kvs ...interface{}) context.Context {
	kvMap := ctx.Value(kvCtxKey)
	if kvMap == nil {
		kvMap = &sync.Map{}
		ctx = context.WithValue(ctx, kvCtxKey, kvMap)
	}
	for i := 0; i+1 < len(kvs); i += 2 {
		k := kvs[i]
		v := kvs[i+1]
		kvMap.(*sync.Map).LoadOrStore(k, v)
	}
	return ctx
}

func GetKVMapFrom(ctx context.Context) *sync.Map {
	if ctx == nil {
		return nil
	}
	i := ctx.Value(kvCtxKey)
	if i == nil {
		return nil
	}
	if kvMap, ok := i.(*sync.Map); ok {
		return kvMap
	}
	return nil
}

func GetAllKVs(ctx context.Context) []interface{} {
	if ctx == nil {
		return nil
	}
	kvMap := GetKVMapFrom(ctx)
	if kvMap == nil {
		return nil
	}
	res := make([]interface{}, 0, 4)
	kvMap.Range(func(key, value interface{}) bool {
		res = append(res, key, value)
		return true
	})
	if len(res) == 0 {
		return nil
	}
	return res
}

type NoticeKVs struct {
	kvs []interface{}
	sync.Mutex
}

func (l *NoticeKVs) PushNotice(k, v interface{}) {
	l.Lock()
	l.kvs = append(l.kvs, k, v)
	l.Unlock()
}

func (l *NoticeKVs) KVs() []interface{} {
	l.Lock()
	kvs := l.kvs
	l.Unlock()
	return kvs
}

func newNoticeKVs() *NoticeKVs {
	return &NoticeKVs{
		kvs: make([]interface{}, 0, 16),
	}
}

func NewNoticeCtx(ctx context.Context) context.Context {
	ntc := newNoticeKVs()
	return context.WithValue(ctx, noticeCtxKey, ntc)
}

func GetNotice(ctx context.Context) *NoticeKVs {
	i := ctx.Value(noticeCtxKey)
	if ntc, ok := i.(*NoticeKVs); ok {
		return ntc
	}
	return nil
}

func CtxPushNotice(ctx context.Context, k, v interface{}) {
	ntc := GetNotice(ctx)
	if ntc == nil {
		return
	}
	ntc.PushNotice(k, v)
}

// CtxStackInfo marks whether to print the stack in the current log.
func CtxStackInfo(ctx context.Context, stackInfo StackInfo) context.Context {
	return context.WithValue(ctx, stackInfoCtxKey, stackInfo)
}
