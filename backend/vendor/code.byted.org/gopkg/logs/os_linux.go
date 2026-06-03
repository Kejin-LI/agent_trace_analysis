//go:build linux
// +build linux

package logs

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
	"time"
)

const ioctlReadTermios = 0x5401 // syscall.TCGETS
const fadviseDontneed = 4

/* defined in linux-4.14/include/uapi/linux/fadvise.h
 * #define POSIX_FADV_DONTNEED 4
 */

func fadvise(fd int, offset int64, length int64, advice int) (err error) {
	return unix.Fadvise(fd, offset, length, advice)
}

func TryToDropFilePageCache(fd int, offset int64, length int64) {
	fadvise(fd, offset, length, fadviseDontneed)
}

func compareFileCreatedTime(a, b os.FileInfo) bool {
	stati := a.Sys().(*syscall.Stat_t)
	statj := b.Sys().(*syscall.Stat_t)
	ctimei := time.Unix(int64(stati.Ctim.Sec), int64(stati.Ctim.Nsec))
	ctimej := time.Unix(int64(statj.Ctim.Sec), int64(statj.Ctim.Nsec))
	return ctimei.After(ctimej)
}
