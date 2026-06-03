package confclient

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/gopkg/logs"
)

var emitterOnce sync.Once
var gEmitter *emitter

type emitter struct {
	clientList []*client
	mutex      sync.Mutex
}
type emitCache struct {
	namespace  string
	identifier string   // 标识引用sdk的渠道，e.g: tcc, byteconf
	getCache   sync.Map // namespace+path+keyname -> emitCacheItem
}
type emitStatus string

const (
	emitStatusSucc     emitStatus = "success"
	emitStatusErr      emitStatus = "failed"
	emitStatusNotFound emitStatus = "notFound"
)

type emitCacheItem struct {
	Succ     int64
	Err      int64
	NotFound int64
}

func initGEmiterOnce() {
	emitterOnce.Do(func() {
		gEmitter = &emitter{}
		go gEmitter.loopMetrics()
	})
}
func loopMetricsAddClient(c *client) {
	initGEmiterOnce()
	gEmitter.mutex.Lock()
	defer gEmitter.mutex.Unlock()
	gEmitter.clientList = append(gEmitter.clientList, c)

	addTCCClientMetrics(c.namespace, c.option.identifier, c.option.tccSdkVersion)
}

func (e *emitter) loopMetrics() {
	var counter int64 = 0

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("Panic occurred in emitGetInfo: %v", r)
				}
			}()
			e.emitGetInfo()
		}()

		if counter == 0 {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Panic occurred in emitConfVersion: %v", r)
					}
				}()
				e.emitConfVersion()
			}()
		}
		counter = (counter + 1) % 12
	}
}

func (e *emitter) emitConfVersion() { //频率不需要那么高
	cliList := e.copyClients()
	for _, cli := range cliList {
		for key, version := range cli.getAllKeyVersion() {
			emitConfVersion(cli.Name(), cli.namespace, key, version) //这个namespace一旦初始化之后都不会修改。所以都是并发读操作。
		}
	}
}

func (e *emitter) emitGetInfo() {
	cliList := e.copyClients()
	for _, cli := range cliList {
		cli.emitGetInfo()
	}
}

func (e *emitter) copyClients() []*client {
	res := make([]*client, 0)
	e.mutex.Lock()
	defer e.mutex.Unlock()
	res = append(res, e.clientList...)

	return res
}

func (e *emitCache) recordGet(bcKey string, err error) {
	status := emitStatusSucc
	if errors.Is(err, internalerror.ErrNotExist) {
		status = emitStatusNotFound
	} else if err != nil {
		status = emitStatusErr
	}
	var item *emitCacheItem
	if itemI, ok := e.getCache.Load(bcKey); !ok {
		itemI, _ = e.getCache.LoadOrStore(bcKey, &emitCacheItem{})
		item = itemI.(*emitCacheItem)
	} else {
		item = itemI.(*emitCacheItem)
	}
	item.add(status)
}

func (e *emitCache) emitGetInfo() {
	//input: /a/b/c
	//output: /a/b , c
	parseKey := func(bcKey string) (path, keyName string) {
		arr := strings.Split(bcKey, "/")
		keyName = arr[len(arr)-1]
		path = "/" + strings.Join(arr[1:len(arr)-1], "/")
		return path, keyName
	}
	e.getCache.Range(func(k, v interface{}) bool {
		bcKey, ok := k.(string)
		if !ok {
			logs.Warn("key [%v] is not type of string", k)
			return true
		}

		item, ok := v.(*emitCacheItem)
		if !ok {
			logs.Warn("value [%+v] is not type of content", v)
			return true
		}

		var path, keyName string

		// 处理 get 一个非法的key
		if checkKeyValidity(bcKey) != nil {
			path = "-"
			keyName = bcKey
		} else {
			path, keyName = parseKey(bcKey)
		}

		succCnt := item.get(emitStatusSucc)
		failCnt := item.get(emitStatusErr)
		notFoundCnt := item.get(emitStatusNotFound)
		gTCCMetrics.emit(e.identifier, e.namespace, path, keyName, emitStatusSucc, succCnt)
		gTCCMetrics.emit(e.identifier, e.namespace, path, keyName, emitStatusErr, failCnt)
		gTCCMetrics.emit(e.identifier, e.namespace, path, keyName, emitStatusNotFound, notFoundCnt)
		emitSLARequestThroughput(e.namespace, int(succCnt+failCnt+notFoundCnt))
		item.done(emitStatusSucc, succCnt)
		item.done(emitStatusErr, failCnt)
		item.done(emitStatusNotFound, notFoundCnt)
		return true
	})
}
func (e *emitCacheItem) add(status emitStatus) {
	switch status {
	case emitStatusSucc:
		atomic.AddInt64(&e.Succ, 1)
	case emitStatusErr:
		atomic.AddInt64(&e.Err, 1)
	case emitStatusNotFound:
		atomic.AddInt64(&e.NotFound, 1)
	default:
		logger.Error("unknown status: %v", status)
	}
}
func (e *emitCacheItem) done(status emitStatus, cnt int64) {
	switch status {
	case emitStatusSucc:
		atomic.AddInt64(&e.Succ, -1*cnt)
	case emitStatusErr:
		atomic.AddInt64(&e.Err, -1*cnt)
	case emitStatusNotFound:
		atomic.AddInt64(&e.NotFound, -1*cnt)
	default:
		logger.Error("unknown status: %v", status)
	}
}
func (e *emitCacheItem) get(status emitStatus) int64 {
	switch status {
	case emitStatusSucc:
		return atomic.LoadInt64(&e.Succ)
	case emitStatusErr:
		return atomic.LoadInt64(&e.Err)
	case emitStatusNotFound:
		return atomic.LoadInt64(&e.NotFound)
	default:
		logger.Error("unknown status: %v", status)
		return 0
	}
}
