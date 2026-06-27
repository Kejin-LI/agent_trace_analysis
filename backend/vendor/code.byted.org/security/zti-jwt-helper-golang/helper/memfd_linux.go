//go:build linux
// +build linux

package helper

import (
	"fmt"
	"os"
	"sync"

	"code.byted.org/security/memfd"
)

const (
	MEMFD_DEFAULT_NAME = "zti_jwt_helper"
)

var (
	// mfd is used to store the identity in memory
	mfd *memfd.Memfd

	// mutex is used to protect mfd
	mutex sync.Mutex

	// memfdName is used to store the name of memfd
	memfdName = fmt.Sprintf("%s_%d", MEMFD_DEFAULT_NAME, os.Getpid())
)

func unMapMemfd() {
	mutex.Lock()
	defer mutex.Unlock()
	if mfd == nil {
		return
	}

	mfd.Close()
	mfd.Unmap()
	mfd = nil
}

func writeContentToMemfd(content []byte) (err error) {
	mutex.Lock()
	defer mutex.Unlock()
	if mfd != nil {
		mfd.Close()
		mfd.Unmap()
	}
	mfd, err = memfd.CreateNameFlags(memfdName, memfd.Cloexec|memfd.AllowSealing)
	if err != nil {
		return
	}

	defer mfd.SetImmutable()

	_, err = mfd.Write(content)
	return
}
