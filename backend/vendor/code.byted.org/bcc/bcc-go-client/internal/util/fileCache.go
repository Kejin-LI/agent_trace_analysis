package util

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/env"
)

//todo 要处理文件删除问题，直接删除老版本容易出问题，在多进程环境下

var (
	dfMutex          sync.Mutex
	defaultFileCache *FileCache
)

func initFileCache() {
	//在启动的时候执行一下
	_ = CheckFileCache()
}

func getAvailablePath() (dir string) {
	psm := env.PSM()
	if psm == "" || psm == env.PSMUnknown {
		psm = "unknown"
	}

	if os.Getenv("IS_FAAS_ENV") == "True" { //faas，tmp目录是容器独占的，默认限制500MB，可以配置
		dir = fmt.Sprintf("/tmp/bcc/%v", psm)
	} else {
		if d, err := os.UserCacheDir(); err == nil { //用户缓存目录，防止切换账号导致没权限
			dir = fmt.Sprintf("%v/bcc/%v", d, psm)
		} else {
			logger.Error("find user cache dir fail. err:%v", err)
			dir = fmt.Sprintf("/var/tmp/bcc/big_key/%v", psm)
		}
	}

	return dir
}

func CheckFileCache() error {
	dfMutex.Lock()
	defer dfMutex.Unlock()
	if defaultFileCache != nil {
		return nil
	}

	dir := getAvailablePath()
	//syscall.Umask(0) //linux调用了才能让文件有0777权限 //调用了会全局生效，为了减少影响，还是panic让用户rm
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		emitCacheDirError(dir, 1)
		logger.Error("bcc sdk MkdirAll fail dir=%v err=%v", dir, err)
		return fmt.Errorf("bcc sdk MkdirAll fail dir=%v err=%v", dir, err)
	}
	logger.Notice("bcc sdk temp dir=%v", dir)
	t := &FileCache{dir: dir}
	t.status = true
	if err := t.writeModifyTime(); err != nil {
		return fmt.Errorf("bcc sdk cannot write dir[%v] file, err:%v", dir, err)
	}
	defaultFileCache = t

	return nil
}

type FileCache struct {
	dir    string
	status bool
	mu     sync.RWMutex
}

func NewFileCache() *FileCache {
	return defaultFileCache
}

func (t *FileCache) writeModifyTime() error {
	if !t.status {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	file := fmt.Sprintf("%v/%v", t.dir, "MODIFY_TIME")
	data := tools.ToBytes(time.Now().Unix())
	return t.write(file, data, false)
}

func (t *FileCache) TryWrite(file string, b []byte) error {
	if !t.status {
		return errors.New("cache close")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.write(file, b, true)
}

func (t *FileCache) TryRead(file string) []byte {
	if !t.status {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	s, _ := ioutil.ReadFile(file)
	if len(s) == 0 {
		return nil
	}
	logger.Debug("bcc sdk cache read file=%v", file)
	//return tools.String2Bytes(s) //导致野指针
	return s
}

func (t *FileCache) TryRemove(file string) {
	if !t.status {
		return
	}
	if file == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := tools.FileRemove(file); err == nil {
		logger.Debug("bcc sdk cache remove file=%v", file)
	}
}

func (t *FileCache) TryOpen(file string) bool {
	f, err := os.OpenFile(file, os.O_RDONLY, os.ModePerm)
	if f != nil {
		_ = f.Close()
	}
	return err == nil
}

func (t *FileCache) GenName(key string, version int64, md5 string) string {
	//不同namespace下的key都不能同名
	key = strings.ReplaceAll(key, "/", "@") //key的'/'会替换为'@'
	return fmt.Sprintf("%v/%v@%v@%v", t.dir, key, version, md5)
}

func (t *FileCache) WriteFileFromReader(file string, reader io.Reader) error {
	tmpfile := t.genTmpFile(file)
	defer func() {
		_ = tools.FileRemove(tmpfile)
	}()

	f, err := tools.FileOpen(tmpfile)
	if err != nil {
		return err
	}
	defer f.Close()

	size, err := io.Copy(f, reader)
	if err != nil {
		logger.Error("bcc sdk cache WriteFileFromReader fail file=%v size=%v err=%v", file, size, err)
		return err
	}
	if err := tools.FileRename(tmpfile, file); err != nil {
		logger.Error("bcc sdk cache WriteFileFromReader fail file=%v len=%v err=%v", file, size, err)
		_ = tools.FileRemove(file)
		return err
	}
	return nil
}

func (t *FileCache) write(file string, b []byte, printLog bool) error {
	tmpfile := t.genTmpFile(file)
	defer func() {
		_ = tools.FileRemove(tmpfile)
	}()

	err := tools.FileWrite(tmpfile, b)
	if err != nil {
		logger.Error("bcc sdk cache write fail file=%v len=%v err=%v", file, len(b), err)
		return err
	} else {
		if err := tools.FileRename(tmpfile, file); err != nil {
			logger.Error("bcc sdk cache write fail file=%v len=%v err=%v", file, len(b), err)
			return err
		} else {
			if printLog {
				logger.Debug("bcc sdk cache write file=%v", file)
			}
			return nil
		}
	}
}

func (t *FileCache) genTmpFile(file string) string {
	return fmt.Sprintf("%v.pid%v.rand%v.tmp", file, os.Getpid(), tools.RandInt()) //防止多进程并发写
}
