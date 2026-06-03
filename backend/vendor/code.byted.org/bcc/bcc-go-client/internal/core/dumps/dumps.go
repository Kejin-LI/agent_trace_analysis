package dumps

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/logger"
)

//为了方便排查配置下发问题，SDK将配置保存到磁盘中。排查问题时直接查看磁盘文件即可判断SDK是否获取到某个特定版本的配置
//考虑到：1.业务方可能监听根目录，从而导致会有较多的文件；2. 公共SDK监听很多不同的配置。连接复用的bcc sdk可能会同时
//监听上万个配置。为此，不能每个配置就存储到一个文件中。但如果将配置都打包保存到同一个文件中，又会有何时打包的问题、文件膨胀等文件
//为此，这里采用文件追加形式解决何时打包，并限制文件的最大尺寸避免无限膨胀，比如128MB
//考虑到99%的业务方不会频繁发布大配置，所以限制文件大小并不会影响记录。

var (
	dumpPath        = "/var/tmp/bcc/"
	dumpCount int64 = 0                 //考虑到业务方会new多个Client，所以进程ID不足于区分
	limitSize int64 = 128 * 1024 * 1024 //128MB
)

type dumper struct {
	mutex     sync.Mutex
	itemQueue []common.DumpItem
	filename  string
	fout      *os.File
	writeSize int64 //已经写入的字节数
	msgChan   chan bool
	stopChan  chan bool
}

func openDumpFile(dumpFilename string) *os.File {
	if err := os.MkdirAll(dumpPath, os.ModePerm); err != nil {
		logger.Error("fail to mkdir[%v], err:%v", dumpPath, err)
		return nil
	}

	fout, err := os.OpenFile(dumpFilename, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		logger.Error("fail to open dumpFile[%v], err:%v", dumpFilename, err)
		return nil
	}

	return fout
}

// 这个name是业务方NewClient传进来的。考虑到业务方可能会new多次client并且监听同一个key/path，因此还需要有办法区分不同Client的返回结果
func newDumper(name string, opt *model.SdkOptions) *dumper {

	curIndex := atomic.AddInt64(&dumpCount, 1)
	dumpFilename := dumpPath + "config_data" + fmt.Sprintf("_%v_%v", os.Getpid(), curIndex) //考虑到业务方会new多个Client，所以进程ID不足于区分
	d := &dumper{
		filename: dumpFilename,
		fout:     openDumpFile(dumpFilename),
		msgChan:  make(chan bool, 2),
		stopChan: make(chan bool, 1),
	}

	//考虑到这个name可能会有特殊符号，无法作为文件名的一部分。为此，这里将name写入到dump文件的第一行中
	d.saveClientNameToFile(name)
	go d.loop()
	return d
}

func (d *dumper) saveClientNameToFile(name string) {
	writer := bufio.NewWriter(d.fout)
	writeStr := fmt.Sprintf("dump file for client[%v]\n", name)
	_, _ = writer.Write([]byte(writeStr))
	writer.Flush()
	atomic.AddInt64(&d.writeSize, int64(len(writeStr)))
}

func (d *dumper) stroageFilename() string {
	return d.filename
}

func (d *dumper) Stop() {
	d.stopChan <- true
}

func (d *dumper) loop() {
	for {
		select {
		case <-d.stopChan:
			return
		case <-d.msgChan:
			d.handleItems()
		}
	}
}

func (d *dumper) handleItems() {
	items := d.swapMsg()

	writer := bufio.NewWriter(d.fout)

	var size int
	for _, item := range items {
		si := transferItem(item)
		writeStr := si + "\n"
		_, _ = writer.Write([]byte(writeStr))
		size += len(writeStr)
	}

	writer.Flush()
	atomic.AddInt64(&d.writeSize, int64(size))
}

func (d *dumper) StorageItem(item common.DumpItem) {

	d.mutex.Lock()
	defer d.mutex.Unlock()

	//dump文件已经足够大，不需要再写入了
	if d.isReachLimitSize() {
		return
	}

	if d.fout == nil { //没有打开目标文件，也不需要写入了
		return
	}

	d.itemQueue = append(d.itemQueue, item)
	if len(d.itemQueue) == 1 {
		d.msgChan <- true
	}
}

func (d *dumper) swapMsg() []common.DumpItem {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	mq := d.itemQueue
	d.itemQueue = nil

	return mq
}

func (d *dumper) isReachLimitSize() bool {
	return atomic.LoadInt64(&d.writeSize) > limitSize
}

func (d *dumper) hasWritenSize() int {
	return int(atomic.LoadInt64(&d.writeSize))
}
