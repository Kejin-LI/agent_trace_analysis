package sensitive_finder_engine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"code.byted.org/security/sensitive_finder_engine/masker"
	"code.byted.org/security/sensitive_finder_engine/tree_machine"
	"code.byted.org/security/sensitive_finder_engine/utils"
)

var (
	logRateLimiter  *rateLimiter
	loggerCallDepth = 3
	sensitiveRate   = 0.5
	useRateLimit    = false
	maskCount       = 100
	maxLength       = -1
)

func SetLoggerCallDepth(depth int) {
	loggerCallDepth = depth
}

func SetSensitiveRate(rate float64) {
	sensitiveRate = rate
}

func SetUseLogRateLimit(use bool) {
	useRateLimit = use
}

func SetMaskCount(count int) {
	maskCount = count
}

func SetMaxLength(length int) {
	maxLength = length
}

/*
logs_processor provides wrapped processor of code.byted.org/gopkg/logs.Processor. When working with ginex/kite
and anyother endpoint services, it can be used as following before the engine starts. It is RECOMMENDED to be
used as the last processor, since processing kvs is not a good idea.


logs.DefaultLogger().AddProcessor(MaskSensitiveLogsProcessor)

OR

logs.DefaultLogger().AddProcessor(MaskChineseSensitiveLogsProcessor)

THINK TWICE (Generally the answer is DONT) when using it in libraries, at least DONT do it on defaultLogger
(logs.XXX does the exactly the same thing). The reason is every single log (mostly, regards level filterring)
calls the processors we added.

type Processor func(rawLog string, kvs ...interface{}) (string, []interface{}, bool)
*/

type logsProcessor = func(rawLog string, kvs ...interface{}) (string, []interface{}, bool)

type sensitiveMasker interface {
	MaskSensitive(string) string
	MatchAndMaskWithResult(string) (string, bool)
	Name() string
}
type fff struct{}

func (f fff) Name() string {
	return ""
}
func (f fff) MaskSensitive(a string) string {
	return a
}
func (f fff) MatchAndMaskWithResult(a string) (string, bool) {
	return a, false
}
func EmptyProcessor() logsProcessor {
	f := fff{}
	return MaskSensitiveLogsProcessorWithFinder(f, nil)
}

// MaskSensitiveLogsProcessor returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessor() logsProcessor {
	return MaskSensitiveLogsProcessorWithKindAndRulesAndSwitchAndMasker(nil, nil, nil, nil)
}

// MaskSensitiveLogsProcessorWithSwitch returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessorWithSwitch(switchFunc func() bool) logsProcessor {
	return MaskSensitiveLogsProcessorWithRulesAndSwitch(nil, switchFunc)
}

// MaskSensitiveLogsProcessorWithKind returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessorWithKind(kind []string) logsProcessor {
	return MaskSensitiveLogsProcessorWithKindAndRules(kind, nil)
}

// MaskSensitiveLogsProcessorWithRules returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessorWithRules(rules []StreamTextFindCustomRule) logsProcessor {
	return MaskSensitiveLogsProcessorWithKindAndRules(nil, rules)
}

// MaskSensitiveLogsProcessorWithSwitch returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessorWithKindAndSwitch(kind []string, switchFunc func() bool) logsProcessor {
	return MaskSensitiveLogsProcessorWithKindAndRulesAndSwitch(kind, nil, switchFunc)
}

// MaskSensitiveLogsProcessorWithRulesAndSwitch returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessorWithRulesAndSwitch(rules []StreamTextFindCustomRule, switchFunc func() bool) logsProcessor {
	return MaskSensitiveLogsProcessorWithKindAndRulesAndSwitch(nil, rules, switchFunc)
}

// MaskSensitiveLogsProcessorWithRules returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessorWithKindAndRules(kind []string, rules []StreamTextFindCustomRule) logsProcessor {
	return MaskSensitiveLogsProcessorWithKindAndRulesAndSwitch(kind, rules, nil)
}

func MaskSensitiveLogsProcessorWithKindAndRulesAndSwitch(kind []string, rules []StreamTextFindCustomRule, switchFunc func() bool) logsProcessor {
	return MaskSensitiveLogsProcessorWithKindAndRulesAndSwitchAndMasker(kind, rules, switchFunc, nil)
}

// MaskSensitiveLogsProcessorWithRules returns a logs processor to mask sensitive logs
func MaskSensitiveLogsProcessorWithKindAndRulesAndSwitchAndMasker(kind []string, rules []StreamTextFindCustomRule,
	switchFunc func() bool, customMaskFunc func(kind string) *masker.MaskFunc) logsProcessor {
	retry := 3
	var finder *StreamTextFinder
	var err error
	for retry > 0 {
		finder, err = NewStreamTextFinderWithKindAndRulesAndCustomMasker(kind, rules, customMaskFunc)
		if err != nil {
			utils.LogsErrorf("init failed, use local rules, reason=%v", err)
			retry -= 1
		} else {
			break
		}
	}
	if finder == nil {
		utils.LogsErrorf("init failed, use local rules, reason=%v", err)
		finder = &StreamTextFinder{
			ruleModule: &StreamRuleModule{},
		}
	}

	return MaskSensitiveLogsProcessorWithFinder(finder, switchFunc)
}

// MaskChineseSensitiveLogsProcessor returns a logs processor to mask sensitive logs in Chinese
//func MaskChineseSensitiveLogsProcessor() logsProcessor {
//	finder, err := NewChineseFinder()
//	if err != nil {
//		panic(err)
//	}
//
//	return MaskSensitiveLogsProcessorWithFinder(finder, nil)
//}

func SecdataEngineSensitiveLogsProcessor() logsProcessor {
	return SecdataEngineSensitiveLogsProcessorWithKind(nil)
}

func SecdataEngineSensitiveLogsProcessorWithKind(kind []string) logsProcessor {
	return SecdataEngineSensitiveLogsProcessorWithRules(kind, nil, nil)
}

func SecdataEngineSensitiveLogsProcessorWithRules(kind []string, customKey, customValue map[string][]string) logsProcessor {
	return SecdataEngineSensitiveLogsProcessorCustom(kind, customKey, customValue, false, nil)
}

func SecdataEngineSensitiveLogsProcessorCustom(kind []string, customKey, customValue map[string][]string,
	useTreeEngine bool, switchFunc func() bool) logsProcessor {
	tree_machine.Build()
	retry := 3
	var f *tree_machine.Finder
	for retry > 0 {
		f = tree_machine.NewFinderCustom(kind, customKey, customValue)
		if f == nil {
			retry -= 1
		} else {
			break
		}
	}
	if f != nil && useTreeEngine {
		f.SetUseTreeMachine(useTreeEngine)
	}
	return MaskSensitiveLogsProcessorWithFinder(f, switchFunc)
}

// MaskSensitiveLogsProcessorWithFinder returns a logs processor to mask sensitive logs with a given finder
// it can be used to create customized log masker
func MaskSensitiveLogsProcessorWithFinder(masker sensitiveMasker, switchFunc func() bool) logsProcessor {
	if masker == nil {
		utils.LogsErrorf("get nil sensitive masker")
		return func(rawLog string, kvs ...interface{}) (string, []interface{}, bool) {
			return rawLog, kvs, true
		}
	}
	return func(rawLog string, kvs ...interface{}) (text string, rkvs []interface{}, b bool) {
		if switchFunc != nil && switchFunc() {
			return rawLog, kvs, true
		}
		if maxLength > 0 && len(rawLog) > maxLength {
			return rawLog, kvs, true
		}

		var f string
		var l int
		var result *countResult
		if useRateLimit {
			f, l = codeLoc()
			logRateLimiter.listMutex.RLock()
			result = logRateLimiter.GetResult(f, l)
			logRateLimiter.listMutex.RUnlock()
			if result != nil {
				if result.Timestamp < time.Now().Unix() {
					logRateLimiter.listMutex.Lock()
					logRateLimiter.DeleteResult(f, l)
					logRateLimiter.listMutex.Unlock()
					result = nil
				} else if !result.IsHit {
					return rawLog, kvs, true
				}
			}
		}

		defer func() {
			if err := recover(); err != nil {
				fmt.Fprintf(os.Stderr, "sensitive_finder worker unexpected, err=%v, data=%v，stack=%v\n", err, rawLog, string(debug.Stack()))
				text = rawLog
				rkvs = kvs
				b = true
			}
		}()

		var isMask bool

		var skvs []interface{}
		if len(kvs) != 0 {
			// fixme: 可能有性能问题
			skvs = make([]interface{}, len(kvs))
			for i, kv := range kvs {
				var result interface{}
				switch kv.(type) {
				case []byte:
					result, isMask = masker.MatchAndMaskWithResult(utils.BytesToString(kv.([]byte)))
				case string:
					result, isMask = masker.MatchAndMaskWithResult(kv.(string))
				default:
					result = kv
				}
				skvs[i] = result
			}
		}

		data1, isMask1 := masker.MatchAndMaskWithResult(rawLog)

		if useRateLimit {
			if result == nil {
				logRateLimiter.countMutex.Lock()
				rc := logRateLimiter.AddCount(f, l, isMask1 || isMask)
				if rc.AllCount >= maskCount {
					logRateLimiter.listMutex.Lock()
					logRateLimiter.NewResult(f, l, float64(rc.HitCount)/float64(rc.AllCount) >= sensitiveRate)
					logRateLimiter.listMutex.Unlock()
				}
				logRateLimiter.countMutex.Unlock()
			}
		}

		return data1, skvs, true
	}
}

func codeLoc() (string, int) {
	// logger.callDepth+2
	_, file, line, ok := runtime.Caller(loggerCallDepth + 2)
	if !ok {
		file = "???"
		line = 0
	}
	// full path: file
	// base path: filepath.Base(file)
	//return fmt.Sprintf("%s:%d", filepath.Base(file), line)
	return filepath.Base(file), line
}

type logCount struct {
	HitCount int
	AllCount int
}

func (lc *logCount) Clear() {
	lc.HitCount = 0
	lc.AllCount = 0
}

func (lc *logCount) Add(isHit bool) {
	if isHit {
		lc.HitCount++
	}
	lc.AllCount++
}

type countResult struct {
	IsHit     bool
	Timestamp int64
}

type rateLimiter struct {
	counts        map[string]map[int]*logCount
	sensitiveList map[string]map[int]*countResult
	countMutex    sync.Mutex
	listMutex     sync.RWMutex
}

func initLogRateLimit() {
	logRateLimiter = &rateLimiter{
		counts:        map[string]map[int]*logCount{},
		sensitiveList: map[string]map[int]*countResult{},
	}
}

func (rl *rateLimiter) AddCount(file string, line int, isHit bool) logCount {
	_, ok := rl.counts[file]
	if !ok {
		rl.counts[file] = map[int]*logCount{}
	}
	_, ok = rl.counts[file][line]
	if !ok {
		rl.counts[file][line] = &logCount{
			HitCount: 0,
			AllCount: 0,
		}
	}
	rl.counts[file][line].Add(isHit)
	return *rl.counts[file][line]
}

func (rl *rateLimiter) Clear(file string, line int) {
	_, ok := rl.counts[file]
	if !ok {
		return
	}
	_, ok = rl.counts[file][line]
	if !ok {
		return
	}
	rl.counts[file][line].Clear()
}

func (rl *rateLimiter) GetResult(file string, line int) *countResult {
	_, ok := rl.sensitiveList[file]
	if !ok {
		return nil
	}
	r, ok := rl.sensitiveList[file][line]
	if !ok {
		return nil
	}
	return r
}

func (rl *rateLimiter) DeleteResult(file string, line int) {
	_, ok := rl.sensitiveList[file]
	if !ok {
		return
	}
	_, ok = rl.sensitiveList[file][line]
	if !ok {
		return
	}
	delete(rl.sensitiveList[file], line)
	if len(rl.sensitiveList[file]) == 0 {
		delete(rl.sensitiveList, file)
	}
	rl.Clear(file, line)
}

func (rl *rateLimiter) NewResult(file string, line int, isHit bool) {
	_, ok := rl.sensitiveList[file]
	if !ok {
		rl.sensitiveList[file] = map[int]*countResult{}
	}
	_, ok = rl.sensitiveList[file][line]
	if !ok {
		rl.sensitiveList[file][line] = &countResult{
			IsHit:     isHit,
			Timestamp: time.Now().Add(1 * time.Hour).Unix(),
		}
	}
}
