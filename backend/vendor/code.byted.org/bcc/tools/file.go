package tools

import (
	"bufio"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

//如果创建的文件或目录权限不对，可以先调用 syscall.Umask(0) //linux系统问题

//删除文件
func FileRemove(name string) error {
	return os.Remove(name)
}
func MustFileRemove(name string) {
	err := FileRemove(name)
	if err != nil {
		Panic("MustFileRemove err=%v", err)
	}
}

//读取文件
func FileRead(filename string) (r string, err error) {
	b, err := ioutil.ReadFile(filename)
	return string(b), err
}
func MustFileRead(filename string) (r string) {
	r, err := FileRead(filename)
	if err != nil {
		Panic("MustFileRead err=%v", err)
	}
	return r
}

//写入文件，如果不存在就创建，如果存在就覆盖
func FileWrite(filename string, anyData interface{}) (err error) {
	_ = os.Remove(filename)
	b := ToBytes(anyData)
	return ioutil.WriteFile(filename, b, os.ModePerm)
}
func MustFileWrite(filename string, anyData interface{}) {
	err := FileWrite(filename, anyData)
	if err != nil {
		Panic("MustFileWrite err=%v", err)
	}
}

//打开文件，如果不存在就新建，写到文件尾
func FileOpen(filename string) (r *os.File, err error) {
	return os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, os.ModePerm)
}
func MustFileOpen(filename string) (r *os.File) {
	r, err := FileOpen(filename)
	if err != nil {
		Panic("MustFileOpen fail filename=%v err=%v", filename, err)
	}
	return
}

//新建文件，如果存在就先删除
func FileNew(filename string) (r *os.File, err error) {
	_ = os.Remove(filename)
	return FileOpen(filename)
}
func MustFileNew(filename string) (r *os.File) {
	_ = os.Remove(filename)
	return MustFileOpen(filename)
}

//判断文件是否存在
func FileExist(filename string) (bool, error) {
	if stat, err := os.Stat(filename); err != nil {
		if os.IsNotExist(err) { // file does not exist
			return false, nil
		} else { // other error
			return false, err
		}
	} else {
		if stat.IsDir() {
			return false, errors.New("isDir")
		}
		return true, nil
	}
}

//拷贝文件
func FileCopy(dstName, srcName string) (written int, err error) {
	src, err := os.Open(srcName)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	dst, err := os.OpenFile(dstName, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	r, err := io.Copy(dst, src)
	if err != nil {
		return 0, err
	}
	return int(r), nil
}
func MustFileCopy(dstName, srcName string) (written int) {
	r, err := FileCopy(dstName, srcName)
	if err != nil {
		Panic("MustFileCopy err=%v", err)
	}
	return r
}

//文件改名
func FileRename(oldpath string, newpath string) error {
	return os.Rename(oldpath, newpath)
}
func MustFileRename(oldpath string, newpath string) {
	err := FileRename(oldpath, newpath)
	if err != nil {
		Panic("MustFileRename err=%v", err)
	}
}

//文件md5
func FileMd5(file string) string {
	if f, e := os.Stat(file); e != nil || !f.Mode().IsRegular() {
		return ""
	}

	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()

	const BufferSize = 8 * 1024 * 1024
	r := bufio.NewReaderSize(f, BufferSize)
	h := md5.New()

	_, err = io.Copy(h, r)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func FileSize(file string) (int64, error) {
	if f, err := os.Stat(file); err != nil {
		return 0, err
	} else {
		return f.Size(), nil
	}
}

// 读取文件并分割（跳过空行、删除最后的\r)
func FileReadLine(filename string, sep string) ([]string, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	arr := strings.Split(string(b), sep)
	r := make([]string, 0, len(arr))
	if sep == "\n" {
		for _, line := range arr {
			//删除最后的\r
			for len(line) > 0 {
				lastCh := line[len(line)-1]
				if lastCh == '\r' { // lastCh == '\n'
					line = line[:len(line)-1]
				} else {
					break
				}
			}
			if len(line) == 0 {
				continue
			}
			r = append(r, line)
		}
	}
	return r, nil
}
