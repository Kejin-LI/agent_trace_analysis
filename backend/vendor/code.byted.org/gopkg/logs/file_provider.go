package logs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const dateFormat = "2006-01-02_15"

type SegDuration string

const (
	HourDur SegDuration = "Hour"
	DayDur  SegDuration = "Day"
	NoDur   SegDuration = "No"
)

const (
	FilePageCacheSize int64 = 16 * 1024 * 1024
	FullPageCacheSize int64 = FilePageCacheSize * 16
)

type FileProvider struct {
	sync.Mutex
	currentTimeSeg time.Time
	duration       SegDuration

	fd            *os.File
	filename      string
	level         int
	fadvOffset    int64
	writeSize     int64
	fileKeepCount int

	currentFileName string
	currentSeqNum   int
	currentFileSize int64
	fileSizeLimit   int64
}

func NewFileProvider(filename string, dur SegDuration, size int64) *FileProvider {
	provider := &FileProvider{
		filename:      filename,
		level:         LevelDebug,
		duration:      dur,
		fileSizeLimit: size,
	}
	provider.setDefaultValuesFromEnv()
	return provider
}

func (fp *FileProvider) setDefaultValuesFromEnv() {
	fp.fileKeepCount, _ = strconv.Atoi(os.Getenv("DEFAULT_LOG_FILE_COUNT_LIMIT"))
	if fp.fileSizeLimit == 0 {
		fileSizeLimit, _ := strconv.Atoi(os.Getenv("DEFAULT_LOG_FILE_SIZE_LIMIT"))
		fp.fileSizeLimit = int64(fileSizeLimit)
	}
}

func (fp *FileProvider) Init() error {
	return fp.loadFile(true)
}

func (fp *FileProvider) loadFile(initState bool) error {
	var (
		fd  *os.File
		err error
	)
	fp.currentTimeSeg = time.Now()
	realFile, currentSeqNum, err := fp.timedFilename(initState)
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(realFile), 0755)
	if env := os.Getenv("IS_PROD_RUNTIME"); len(env) == 0 {
		fd, err = os.OpenFile(realFile, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	} else {
		fd, err = os.OpenFile(realFile, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	}
	fp.fd = fd
	_, err = os.Lstat(fp.filename)
	if err == nil || os.IsExist(err) {
		os.Remove(fp.filename)
	}
	os.Symlink("./"+filepath.Base(realFile), fp.filename)
	fp.currentFileName = realFile
	fp.currentSeqNum = currentSeqNum
	pos, err := fd.Seek(0, os.SEEK_CUR)
	if err != nil {
		fp.fadvOffset = -1
	} else {
		fp.fadvOffset = pos / FilePageCacheSize * FilePageCacheSize
		fp.writeSize = pos % FilePageCacheSize
		fp.currentFileSize = pos
	}
	err = fp.cleanElderFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "clean file %s error: %s\n", fp.filename, err)
	}
	return nil
}

func (fp *FileProvider) doCheck(logTime time.Time) error {
	fp.Lock()
	defer fp.Unlock()

	needRotate := false

	// assume that the application would log at least one message in each year
	switch fp.duration {
	case DayDur:
		if fp.currentTimeSeg.YearDay() != logTime.YearDay() {
			needRotate = true
		}
	case HourDur:
		if fp.currentTimeSeg.Hour() != logTime.Hour() || fp.currentTimeSeg.YearDay() != logTime.YearDay() {
			needRotate = true
		}
	}

	if fp.fileSizeLimit > 0 && fp.currentFileSize > fp.fileSizeLimit {
		needRotate = true
	}

	if needRotate {
		if err := fp.rotate(); err != nil {
			fmt.Fprintf(os.Stderr, "rotate file %s error: %s\n", fp.filename, err)
			return err
		}
		if err := fp.cleanElderFiles(); err != nil {
			fmt.Fprintf(os.Stderr, "clean file %s error: %s\n", fp.filename, err)
		}
	}
	return nil
}

func (fp *FileProvider) SetLevel(l int) {
	fp.level = l
}

func (fp *FileProvider) WriteMsg(msg string, level int) error {
	if level < fp.level {
		return nil
	}
	fp.doCheck(time.Now())
	written, err := fmt.Fprint(fp.fd, msg)
	if (err == nil) && (fp.fadvOffset >= 0) {
		fp.writeSize += int64(written)
		if fp.writeSize >= FilePageCacheSize {
			go func(fd int, offset int64, length int64) {
				TryToDropFilePageCache(fd, offset, length)
				/* full drop page cache every FullPageCacheSize */
				if ((offset + FilePageCacheSize) % FullPageCacheSize) <= FilePageCacheSize {
					TryToDropFilePageCache(int(fp.fd.Fd()), 0, fp.fadvOffset)
				}
			}(int(fp.fd.Fd()), fp.fadvOffset, FilePageCacheSize)

			fp.fadvOffset += FilePageCacheSize
			fp.writeSize -= FilePageCacheSize
		}
	}
	fp.currentFileSize += int64(written)
	return err
}

func (fp *FileProvider) Destroy() error {
	TryToDropFilePageCache(int(fp.fd.Fd()), 0, fp.fadvOffset+FilePageCacheSize)
	return fp.fd.Close()
}

func (fp *FileProvider) Flush() error {
	return fp.fd.Sync()
}

func (fp *FileProvider) rotate() error {
	fp.fd.Close()
	return fp.loadFile(false)
}

func (fp *FileProvider) timedFilename(initState bool) (string, int, error) {
	absPath, err := filepath.Abs(fp.filename)
	if err != nil {
		return "", 0, err
	}
	timedName := absPath + "." + fp.currentTimeSeg.Format(dateFormat)
	if fp.fileSizeLimit <= 0 {
		return timedName, 0, nil
	}

	if initState {
		seqFileName := timedName
		seqNum := 0
		_ = filepath.Walk(filepath.Dir(timedName), func(path string, info os.FileInfo, err error) error {
			if strings.HasPrefix(path, timedName) {
				logTime := getFileDate(path)
				if logTime.seqNum > seqNum {
					seqFileName = path
					seqNum = logTime.seqNum
				}
			}
			return nil
		})
		return seqFileName, seqNum, nil
	}

	if !strings.HasPrefix(fp.currentFileName, timedName) {
		return timedName, 0, nil
	}
	return fmt.Sprintf("%s.%d", timedName, fp.currentSeqNum+1), fp.currentSeqNum + 1, nil
}

func (fp *FileProvider) SetKeepFiles(count int) {
	if fp.fd == nil {
		fp.fileKeepCount = count
	}
}

func (fp *FileProvider) cleanElderFiles() error {
	if fp.fileKeepCount == 0 {
		return nil
	}
	absPath, err := filepath.Abs(fp.filename)
	if err != nil {
		return err
	}
	logDir := filepath.Dir(absPath)
	files, err := ioutil.ReadDir(logDir)
	if err != nil {
		return err
	}
	var logFiles []os.FileInfo
	for _, file := range files {
		if file.IsDir() || !strings.HasPrefix(file.Name(), filepath.Base(fp.filename)) {
			continue
		}
		logFiles = append(logFiles, file)
	}
	if len(logFiles) <= fp.fileKeepCount {
		return nil
	}
	sortableFiles := File(logFiles)

	sort.Sort(sortableFiles)

	for _, file := range logFiles[fp.fileKeepCount+1:] {
		relativePath := filepath.Join(filepath.Dir(fp.filename), file.Name())
		fullPath, err := filepath.Abs(relativePath)
		if err != nil {
			return err
		}
		err = os.Remove(fullPath)
		if err != nil {
			return err
		}
	}
	return nil
}

type File []os.FileInfo

func (a File) Len() int {
	return len(a)

}
func (a File) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]

}
func (a File) Less(i, j int) bool {
	return compareFileCreatedTime(a[i], a[j])
}

type logFileTime struct {
	timeSeg time.Time
	seqNum  int
}

func getFileDate(name string) logFileTime {
	var t time.Time
	var n int
	var err error
	sn := strings.Split(name, ".")
	t, err = time.Parse(dateFormat, sn[len(sn)-1])
	if err != nil {
		t, _ = time.Parse(dateFormat, sn[len(sn)-2])
		n, _ = strconv.Atoi(sn[len(sn)-1])
	}
	return logFileTime{
		timeSeg: t,
		seqNum:  n,
	}
}
