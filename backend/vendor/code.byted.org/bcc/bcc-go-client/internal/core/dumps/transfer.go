package dumps

import (
	"fmt"
	"os"

	cmodel "code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/conf_engine/confcontent"
	"code.byted.org/bcc/tools"
	"github.com/pkg/errors"
)

type DumpItemCBRes string

const (
	SuccessCBRes DumpItemCBRes = "succ"
	FailCBRes    DumpItemCBRes = "fail"
)

type SaveItem struct {
	DownloadTime   string                   `json:"ts"`                     //sdk获取到该item的时间
	CallBackStatus DumpItemCBRes            `json:"cb_res"`                 //回调结果
	Version        string                   `json:"version"`                //之所以是字符串，是因为要长度一致，方面grep查看
	FullBaseSize   string                   `json:"online_size"`            // online size
	MD5            string                   `json:"md5"`                    //配置内容md5
	UpdateID       int64                    `json:"update_id"`              //updateID
	WritePID       string                   `json:"pid"`                    //写入该配置的pid,用于判断是否重启过
	Keyname        string                   `json:"keyname"`                //配置名
	BigFilepath    string                   `json:"big_filepath,omitempty"` //大文件路径
	Serializer     cmodel.SerializationType `json:"serializer"`             //序列化算法
	BaseSize       string                   `json:"gray_size,omitempty"`    //gray size
	FullBase       string                   `json:"online_value"`           //online_value
	Base           string                   `json:"gray_value,omitempty"`   //gray_value
}

func transferItem(srcItem common.DumpItem) string {
	si := transferBaseInfo(srcItem)
	if err := decode(srcItem, &si); err != nil {
		si.FullBase = "decode fail: " + err.Error() + ". oriData:" + string(srcItem.Val)
	}

	return tools.ToJson(si)
}

func transferBaseInfo(srcItem common.DumpItem) SaveItem {
	cbStatus := SuccessCBRes
	if !srcItem.IsSuccCallback {
		cbStatus = FailCBRes
	}

	si := SaveItem{
		DownloadTime:   srcItem.DownloadTime.Format("01-02 15:04:05"),
		CallBackStatus: cbStatus,
		Keyname:        srcItem.Keyname,
		UpdateID:       srcItem.UpdateID,
		Serializer:     srcItem.Serializer,
		WritePID:       fmt.Sprintf("%07d", os.Getpid()),
		BigFilepath:    srcItem.BigFilepath,
	}

	return si
}

func decode(srcItem common.DumpItem, siItem *SaveItem) (err error) {
	var c confcontent.Content

	if srcItem.Serializer.IsJsonSerialization() {
		err = tools.JsonUnmarshal(srcItem.Val, &c)
	} else if srcItem.Serializer.IsMsgpackSerialization() {
		err = tools.MsgPackUnmarshal(srcItem.Val, &c)
	} else {
		err = fmt.Errorf("unknown serialize[%v]", srcItem.Serializer)
	}
	if err != nil {
		err = errors.Wrapf(err, "fail to unmarshal")
		return
	}

	siItem.FullBase = string(c.FullBase)
	siItem.FullBaseSize = fmt.Sprintf("%08d", len(c.FullBase))
	if c.GrayRule.IsGray() {
		siItem.BaseSize = fmt.Sprintf("%08d", len(c.Base))
		siItem.Base = string(c.Base)
	}
	siItem.Version = fmt.Sprintf("%04d", c.Version)
	siItem.MD5 = tools.Md5(c.FullBase) //TODO: 直接读取字段得到

	//大文件只写前面100字节。不然容易撑爆
	if len(siItem.FullBase) > 100*1024 {
		siItem.FullBase = siItem.FullBase[:100]
	}
	if len(siItem.Base) > 100*1024 {
		siItem.Base = siItem.Base[:100]
	}

	return nil
}
