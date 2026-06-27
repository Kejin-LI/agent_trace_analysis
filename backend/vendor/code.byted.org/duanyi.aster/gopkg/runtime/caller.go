//go:build (amd64 && go1.15 && !go1.22) || (arm64 && go1.15 && !go1.22)
// +build amd64,go1.15,!go1.22 arm64,go1.15,!go1.22

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

// Caller return specific caller's pc, file name and line number about function invocations on
// the calling goroutine's stack. The argument skip is the number of stack frames
// to ascend, with 0 identifying the caller of Caller.
//
// WARN: skip CANNOT exceeds the depth of current stack, otherwist it will return exist=false
func Caller(skip int) (pc uintptr, file string, line int, exist bool) {
	// calculate specific PC
	pc = logicCallerPC(skip + 1)
	if pc == 0 {
		exist = false
		return
	}
	exist = true
	file, line = fileLineForPC(pc)
	return
}

// PhysicCaller return specific caller's pc, file name and line number about function invocations on
// the calling goroutine's stack. The argument skip is the number of stack frames
// to ascend, with 0 identifying the caller of Caller.
//
// NOTICE: this API trace **Physical** stack instead of Logic stack,
// which means inlined function will be ignored and skip shouldn't be count
// WARN: skip CANNOT exceeds the depth of current stack, otherwist it will return exist=false
//
//go:noinline
func PhysicCaller(skip int) (pc uintptr, file string, line int, exist bool) {
	// calculate specific PC
	pc = physicalCallerPC(skip + 1)
	if pc == 0 {
		exist = false
		return
	}
	exist = true
	file, line = fileLineForPC(pc)
	return
}

// physicalCallerPC returns the PC address of specific caller.
// The argument skip is the number of stack frames
// to ascend, with 0 identifying the caller of Caller.
//
// NOTICE: this API trace **physical** stack instead of logic stack,
// which means inlined function will be ignored and the skip arg shouldn't include inlined calls.
// WARN: skip CANNOT exceeds current stack depth, otherwise it will return 0
//
//go:nosplit
func physicalCallerPC(skip int) (ret uintptr)

// logicCallerPC returns the PC address of specific caller.
// The argument skip is the number of stack frames
// to ascend, with 0 identifying the caller of Caller.
//
//go:nosplit
func logicCallerPC(skip int) uintptr
