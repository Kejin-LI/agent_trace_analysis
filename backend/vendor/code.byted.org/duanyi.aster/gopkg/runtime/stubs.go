/**
 * Copyright 2023 ByteDance Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package runtime

import (
	"unsafe"
)

//go:linkname callers runtime.callers
func callers(skip int, pcbuf []uintptr) int

type funcFlag uint8

type funcID uint8
type funcInfo struct {
	*_func
	datap unsafe.Pointer
}

//go:linkname findfunc runtime.findfunc
func findfunc(pc uintptr) funcInfo

//go:linkname funcdata runtime.funcdata
func funcdata(f funcInfo, i uint8) unsafe.Pointer

//go:linkname funcname runtime.funcname
func funcname(f funcInfo) string

//go:linkname pcdatavalue runtime.pcdatavalue
func pcdatavalue(f funcInfo, table uint32, targetpc uintptr, cache *unsafe.Pointer) int32

// elideWrapperCalling reports whether a wrapper function that called
// function id should be elided from stack traces.
//
//go:linkname elideWrapperCalling runtime.elideWrapperCalling
func elideWrapperCalling(id funcID) bool

func debug1(a uint64) {
	println("debug1: ", a)
}
