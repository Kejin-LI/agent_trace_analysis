package logs

import (
	"bytes"
	"context"
	"sync"

	"code.byted.org/log_market/ttlogagent_gosdk/v4/codec"
)

const (
	kvCtxKey        = "K_KVs_V2"
	noticeCtxKey    = "K_NOTICE_V2"
	stackInfoCtxKey = "K_STACK_INFO_V2"
)

var (
	convertCtxKVListToStr = false
	spaceBytes            = []byte{' '}
)

type StackInfo byte

type ctxKVs struct {
	kvs []interface{}
	pre *ctxKVs
}

const (
	NoPrint StackInfo = iota
	CurrGoroutine
	AllGoroutines
)

// CtxAddKVs works like logs.CtxAddKVs in logs 1.0
func CtxAddKVs(ctx context.Context, kvs ...interface{}) context.Context {
	if len(kvs) == 0 || (len(kvs)&1 == 1) { // ignore odd kvs
		return ctx
	}

	kvList := make([]interface{}, 0, len(kvs))
	kvList = append(kvList, kvs...)

	return context.WithValue(ctx, kvCtxKey, &ctxKVs{
		kvs: kvList,
		pre: getKVs(ctx),
	})
}

func GetAllKVs(ctx context.Context) []interface{} {
	if ctx == nil {
		return nil
	}
	kvs := getKVs(ctx)
	if kvs == nil {
		return nil
	}

	var result []interface{}
	recursiveAllKVs(&result, kvs, 0)
	return result
}

func GetAllKVsStr(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	kvs := getKVs(ctx)
	if kvs == nil {
		return ""
	}

	var result []interface{}
	recursiveAllKVs(&result, kvs, 0)

	var buf bytes.Buffer
	for i := 0; i+1 < len(result); i += 2 {
		kv := codec.NewOmniKeyValue(result[i], result[i+1])
		if i != 0 {
			buf.Write(spaceBytes)
		}
		buf.WriteString(kv.String())
	}
	return buf.String()
}

func getKVs(ctx context.Context) *ctxKVs {
	if ctx == nil {
		return nil
	}
	i := ctx.Value(kvCtxKey)
	if i == nil {
		return nil
	}
	if kvs, ok := i.(*ctxKVs); ok {
		return kvs
	}
	return nil
}

// to keep FIFO order
func recursiveAllKVs(result *[]interface{}, kvs *ctxKVs, total int) {
	if kvs == nil {
		*result = make([]interface{}, 0, total)
		return
	}
	recursiveAllKVs(result, kvs.pre, total+len(kvs.kvs))
	*result = append(*result, kvs.kvs...)
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
