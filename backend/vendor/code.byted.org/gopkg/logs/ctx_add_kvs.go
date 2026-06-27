package logs

import (
	"context"

	"code.byted.org/gopkg/metrics"
)

const (
	kvCtxKey        = "K_KVs"
	stackInfoCtxKey = "K_STACK_INFO"
	metricTagKey    = "K_Metrics_Tag"
)

type StackInfo byte

const (
	NoPrint StackInfo = iota
	CurrGoroutine
	AllGoroutines
)

type ctxKVs struct {
	kvs []interface{}
	pre *ctxKVs
}

func ctxAddKVs(ctx context.Context, kvs ...interface{}) context.Context {
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

// CtxStackInfo marks whether to print the stack in the current log.
func CtxStackInfo(ctx context.Context, stackInfo StackInfo) context.Context {
	return context.WithValue(ctx, stackInfoCtxKey, stackInfo)
}

// CtxAddMetricTags adds extra tags to the metric
func CtxAddMetricTags(ctx context.Context, kvs ...interface{}) context.Context {
	if len(kvs) == 0 || (len(kvs)&1 == 1) { // ignore odd kvs
		return ctx
	}

	kvList := make([]interface{}, 0, len(kvs))
	kvList = append(kvList, kvs...)

	return context.WithValue(ctx, metricTagKey, &ctxKVs{
		kvs: kvList,
		pre: getKVsByKeyName(ctx, metricTagKey),
	})
}

func getMetricTags(ctx context.Context) []metrics.T {
	if ctx == nil {
		return nil
	}
	kvs := getKVsByKeyName(ctx, metricTagKey)
	if kvs == nil {
		return nil
	}

	var result []interface{}
	recursiveAllKVs(&result, kvs, 0)

	res := make([]metrics.T, 0, len(result)/2)

	for i := 0; i+1 < len(result); i += 2 {
		n, ok1 := result[i].(string)
		v, ok2 := result[i+1].(string)
		if !ok1 || !ok2 {
			return res
		}
		res = append(res, metrics.T{
			Name:  n,
			Value: v,
		})

	}
	return res
}

func getKVsByKeyName(ctx context.Context, keyName string) *ctxKVs {
	if ctx == nil {
		return nil
	}
	i := ctx.Value(keyName)
	if i == nil {
		return nil
	}
	if kvs, ok := i.(*ctxKVs); ok {
		return kvs
	}
	return nil
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

// to keep FIFO order
func recursiveAllKVs(result *[]interface{}, kvs *ctxKVs, total int) {
	if kvs == nil {
		*result = make([]interface{}, 0, total)
		return
	}
	recursiveAllKVs(result, kvs.pre, total+len(kvs.kvs))
	*result = append(*result, kvs.kvs...)
}
