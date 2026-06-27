//go:build !linux
// +build !linux

package helper

func unMapMemfd() {
}

func writeContentToMemfd(content []byte) (err error) {
	return
}
