package downloader

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
)

type saveOption struct {
	timeout time.Duration
}

type downloader struct {
	opt       saveOption
	fileCache *util.FileCache
}

func newDownloader(opt saveOption) *downloader {
	return &downloader{
		fileCache: util.NewFileCache(),
		opt:       opt,
	}
}

func (d *downloader) downloadFile(task *downloadTask) {
	if task.isCanceled() { //中途被取消了
		return
	}

	result := task.getResult()
	b, backupFile, success := d.downloadProcess(task, result)
	if success {
		result.Result = common.DOWNLOAD_RESULT_OK
		result.FailMsg = ""
		result.Value = b
		if !task.opt.disableBackupFile {
			result.BackupFile = backupFile
		}
	} else {
		if result.Source == common.DOWNLOAD_SOURCE_INIT {
			result.Source = common.DOWNLOAD_SOURCE_FAIL
		}
	}

	task.setResult(result)
}

/*
总体逻辑：
- 不禁用内存保存、不禁用下载文件。
  - 则是先通过本地文件获取，判断是否有缓存文件存在，有则直接获取。
  - 没有文件缓存，则直接走tos等网络下载，下载完成保存都需要内存中，再写到文件中（允许写失败）。

- 禁用内存，不禁用下载文件（两者不能同时都禁用）
  - 先通过本地文件路径，获取其md5进行判断是否存在且正确，成功则直接返回。注意：禁用内存不会获取内容到内存中。
  - 没有，则直接走tos等网络下载，内容直接写到文件上。

- 不禁用内存，禁用下载文件
  - 不通过文件获取
  - tos直接下载获取到内存中
*/
func (d *downloader) downloadProcess(task *downloadTask, result *common.LoaderResult) (b []byte, backupFile string, success bool) {
	svrItem, opt := task.SvrItem, task.opt
	backupFile = d.fileCache.GenName(svrItem.Key(), svrItem.UpdateID(), svrItem.Md5())

	if b, success = d.downloadFromLocalFile(backupFile, svrItem.Md5(), opt); success {
		result.Source = common.DOWNLOAD_SOURCE_FILE
		return
	}
	return d.downloadFromRemote(task, backupFile, result)
}

func (d *downloader) downloadFromLocalFile(backupFile string, expectedMd5 string, opt taskOption) (b []byte, success bool) {
	if opt.disableBackupFile {
		return nil, false
	}
	if b = d.fileCache.TryRead(backupFile); len(b) > 0 {
		if md5Str := tools.Md5(b); md5Str != expectedMd5 { //提前验证md5
			logger.Warn("®file modified backupFile=%v local=%v require=%v", backupFile, md5Str, expectedMd5)
			d.fileCache.TryRemove(backupFile)
			b = nil
		} else {
			if opt.disableMemory {
				b = nil
			}
			return b, true
		}
	}
	return nil, false
}

// 对请求任务进行网络下载，并对下载内容进行相关的校验核对逻辑
func (d *downloader) downloadFromRemote(task *downloadTask, backupFile string, result *common.LoaderResult) (b []byte, newBackupFile string, success bool) {
	newBackupFile = backupFile // 写失败置空backupFile
	directFile := ""
	// 禁用了内存，只能直接下载到文件中
	if task.opt.disableMemory {
		directFile = backupFile
	}

	if b, success = d.downloadOrSaveFile(task.SvrItem, directFile, result); !success {
		// 下载请求失败返回
		return
	}
	//不禁用内存时和不禁用文件时，需要把内容保存到本地文件上，但允许写文件失败，只是会置空backupFile
	if !task.opt.disableMemory && !task.opt.disableBackupFile {
		if err := d.fileCache.TryWrite(backupFile, b); err != nil { //写文件失败必须报错
			logger.Error("write backup file fail name=%v err=%v", backupFile, err) //因为不强依赖文件，所以只是报错
			util.EmitError(task.keyName, "writefile")
			newBackupFile = ""
			return nil, newBackupFile, false
		}
	}

	return
}

// 对请求任务进行网络下载, 禁用内存时直接下载到文件中
func (d *downloader) downloadOrSaveFile(svrItem *common.ServerItem, directFile string, result *common.LoaderResult) (b []byte, success bool) {

	key, infos := svrItem.Key(), svrItem.DownloadInfos()
	var err error
	t0 := time.Now()
	for _, info := range infos {
		if b, err = d.download(info, directFile, result, svrItem.Md5()); err == nil {
			result.Source = info.Source
			util.EmitDownloadLatency(svrItem.Key(), int(svrItem.Version()), result.Source.String(), result.Result.String(), t0)
			return b, true
		} else {
			util.EmitDownloadBigFileError(key, info.HasAgent(), 1)
			logger.Info("load bigFile fail key=%v DownloadInfo=%v err=%v", key, info, err)
		}
	}
	logger.Error("load bigFile fail key=%v DownloadInfos=%v err=%v", key, infos, err)
	return nil, false
}

// 网络请求下载逻辑。directFile非空时，不保存到内存，直接把内容写到相应的文件路径上
func (d *downloader) download(info *common.DownloadInfo, directFile string, result *common.LoaderResult, expectedMd5 string) ([]byte, error) {
	transport := http.DefaultTransport
	if info.Agent != "" {
		proxyURL, _ := url.Parse(info.Agent)
		transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}

	httpCli := &http.Client{
		Transport: transport,
		Timeout:   d.opt.timeout,
	}

	req, err := http.NewRequest("GET", info.Url, strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	resp, err := httpCli.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		result.Result = common.DOWNLOAD_RESULT_TOS_RETRY
		return nil, err
	}

	if resp.StatusCode != 200 {
		if resp.StatusCode == 404 {
			result.Result = common.DOWNLOAD_RESULT_TOS_NOTFOUND
		} else {
			result.Result = common.DOWNLOAD_RESULT_TOS_RETRY
		}
		return nil, fmt.Errorf("statuscode=%v", resp.StatusCode)
	}
	if directFile == "" {
		r, err := poolReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		// 校验下载内容是否完整
		if success := d.checkBackupFile(expectedMd5, result, r); !success {
			return nil, fmt.Errorf(result.FailMsg)
		}
		return r, nil
	} else {
		// 不保存内容到内存中，直接写文件、返回空值。
		if err = d.fileCache.WriteFileFromReader(directFile, resp.Body); err != nil {
			return nil, err
		}
		if success := d.checkDirectFile(directFile, expectedMd5, result); !success {
			return nil, fmt.Errorf(result.FailMsg)
		}
		return nil, nil
	}
}

// 对获取到的文件内容进行长度和md5的校验
func (d *downloader) checkBackupFile(expectedMd5 string, result *common.LoaderResult, b []byte) bool {
	if len(b) == 0 {
		result.Result = common.DOWNLOAD_RESULT_TOS_ZERO
		result.FailMsg = fmt.Sprintf("size=0")
		return false
	}
	return checkMd5(tools.Md5(b), expectedMd5, result)
}

// 对禁用内存直接写文件的方式，进行检查md5是否完整（文件被修改，或tos存储的文件有问题）
func (d *downloader) checkDirectFile(filePath, expectedMd5 string, result *common.LoaderResult) bool {
	md5Str := tools.FileMd5(filePath)

	if success := checkMd5(md5Str, expectedMd5, result); !success {
		logger.Warn("file modified filePath=%v local=%v require=%v", filePath, md5Str, expectedMd5)
		d.fileCache.TryRemove(filePath)
		return false
	}
	return true
}

// 检查md5
func checkMd5(md5Str, expectedMd5 string, result *common.LoaderResult) bool {
	if md5Str != expectedMd5 {
		result.Result = common.DOWNLOAD_RESULT_TOS_MD5
		result.FailMsg = fmt.Sprintf("md5 unmatch. actual=%v expected=%v", md5Str, expectedMd5)
		return false
	}
	return true
}
