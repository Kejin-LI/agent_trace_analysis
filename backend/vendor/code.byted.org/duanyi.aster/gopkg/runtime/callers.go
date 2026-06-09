package runtime

import (
	"runtime"

	"code.byted.org/duanyi.aster/gopkg/caching"
)

func init() {
	p := make(map[int64]interface{}, LineCacheDefaultSize)
	flsCache = caching.NewRCUI64(&p, MaxLineCacheRCUTry)
	pl := make(map[int64]interface{}, LineCacheDefaultSize)
	isInlineCache = caching.NewRCUI64(&pl, MaxLineCacheRCUTry)
}

var (
	flsCache caching.IncRCU

	isInlineCache caching.IncRCU

	// LineCacheDefaultSize is the initial size of line info cache used in `FileLineForPC`
	LineCacheDefaultSize = 256

	// MaxLineCacheRCUTry is the max CAS trial times when updating line info cache (RCU).
	// Lower value means better stability at pretouch phase of the cache,
	// as well as longer time to finish pretouch.
	MaxLineCacheRCUTry = 1000
)

type lineInfo struct {
	File   string
	Line   int
	Column int
}

// fileLineForPC returns the corresponding file name and line number of given PC
func fileLineForPC(pc uintptr) (file string, line int) {
	// seek pc value from cache
	if v := flsCache.GetByI64(int64(pc)); v != nil {

		li := v.(lineInfo)
		return li.File, li.Line

	} else {

		//not found, get line info from runtime
		li := lineInfo{}
		li.File, li.Line = runtime.FuncForPC(pc).FileLine(pc)

		// cache it
		flsCache.SetByI64(int64(pc), li)

		return li.File, li.Line
	}
}

// func MoreStack(size uintptr)

// var count uintptr = 1

type inlineInfo struct {
	depth   int
	targets []uintptr
}

// countInlined count and returns inlined layers
// if left is smaller than the inlined depth, return the target pc
// WARN: left CANNOT be zero
func countInlined(pc uintptr, left int) (skip int, target uintptr) {

	// MoreStack(1024 * count)
	// count += 10
	// f := findfunc(pc)
	// println("[countInlined] funcname ", funcname(f))

	inl := isInlineCache.GetByI64(int64(pc))

	if inl != nil {

		info := inl.(inlineInfo)
		skip = left - info.depth
		if skip <= 0 {
			target = info.targets[left]
			// println("[countInlined] end target pc", target)
			// println(runtime.FuncForPC(target).FileLine(target))
		}

	} else {
		info := inlineInfo{}
		info.targets = append(info.targets, pc)

		lastFuncID := funcID_normal
		f := findfunc(pc)
		t := funcdata(f, _FUNCDATA_InlTree)
		if t != nil {

			inltree := (*[1 << 20]inlinedCall)(t)
			// println("[countInlined] start pc", pc, "left", left)
			for {
				ix := pcdatavalue(f, _PCDATA_InlTreeIndex, pc, nil)
				if ix < 0 {
					break
				}
				// println("[countInlined] inlined ix ", ix, ", file ", inltree[ix].file, ", line ", inltree[ix].line, " funcid", inltree[ix].funcID)
				if inltree[ix].funcID != funcID_wrapper || !elideWrapperCalling(lastFuncID) {
					info.depth++
					// println("[countInlined] depth++ =", info.depth)
				}

				lastFuncID = inltree[ix].funcID
				pc = func_entry(f) + uintptr(inltree[ix].parentPc)
				// println("[countInlined] next pc", pc)
				info.targets = append(info.targets, pc+1)

			}
		}

		skip = left - info.depth
		if skip <= 0 {
			target = info.targets[left]
			// println("[countInlined] end target pc", target)
			// println(runtime.FuncForPC(target).FileLine(target))
		}

		isInlineCache.SetByI64(int64(pc), info)
	}

	return
}

// func CallerPC2(skip int) uintptr {
// 	var b [2]byte
// 	var a unsafe.Pointer
// 	if skip <= 0 {
// 		a = unsafe.Pointer(&b[0])
// 		return uintptr(a)
// 	}
// 	a = unsafe.Pointer(&b[1])
// 	for skip > 1 {
// 		a = *(*unsafe.Pointer)(a)
// 		skip--
// 	}
// 	return *(*uintptr)(unsafe.Pointer(uintptr(a) + uintptr(8)))
// }
