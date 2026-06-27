package common

import (
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/pb"
	"code.byted.org/bcc/bcc-go-client/logger"
	"code.byted.org/bcc/tools"
)

//主要是做一些pb到common结构体的相互转换

func CommonDownloadItem2PbItem(item *DownloadItemInfo) (pbItem *pb.DownloadItem) {
	if item == nil {
		return
	}

	pbItem = &pb.DownloadItem{
		Key:        item.Key,
		Version:    item.Version,
		CreateTime: item.CreateTime,
		UpdateTime: item.UpdateTime,
		Source:     pb.SOURCE(item.Source),
		Result:     pb.RESULT(item.Result),
		FailCount:  item.FailCount,
		FailMsg:    item.FailMsg,
		Flow:       item.Flow,
	}

	return
}

func CommonCtlItem2PbItem(item *CltItem) (pbItem *pb.CltItem) {
	if item == nil {
		return
	}

	pbItem = &pb.CltItem{
		Key:          item.Key,
		EnableListen: item.EnableListen,
		AllowEmpty:   item.EnableEmpty,
		UpdateId:     item.UpdateID,
		Version:      item.Version,
		Md5:          item.Md5,
		UpdateTime:   item.UpdateTime,
		Valid:        item.Valid,
		Target:       CommonDownloadItem2PbItem(item.Target),
	}

	return
}

func CommonDirItem2PbItem(item *CltDir) (pbItem *pb.CltDir) {
	if item == nil {
		return
	}

	pbItem = &pb.CltDir{
		Path:         item.Path,
		EnableListen: item.EnableListen,
		FirstTime:    item.FirstTime,
		Items:        make(map[string]*pb.CltItem, len(item.Items)),
	}

	for key, it := range item.Items {
		pbItem.Items[key] = CommonCtlItem2PbItem(it)
	}

	return
}

//========================================================================================================================

func getDlInfos(dls []*pb.DownloadInfo) []*DownloadInfo {
	dlInfos := make([]*DownloadInfo, 0, len(dls))
	for _, one := range dls {
		dlInfos = append(dlInfos, &DownloadInfo{
			Url:    one.Url,
			Source: DownloadItemSource(one.Source),
			Agent:  one.Agent,
		})
	}
	return dlInfos
}

func PBItem2CommmonKeyItem(item *pb.SvrItem) *ServerItem {
	if item == nil {
		return nil
	}
	logger.Debug("svritem:%v", tools.ToJsonStringer(item))

	info := &SvrKeyInfo{
		Key:           item.Key,
		Value:         item.Value,
		Version:       item.Version,
		Md5:           item.Md5,
		Desc:          item.Desc,
		Compressor:    item.Compressor,
		Status:        ItemStatus(item.Status),
		HitEnv:        item.HitEnv,
		Size:          item.Size,
		UpdateTime:    item.UpdateTime,
		UpdateID:      item.UpdateId,
		SDKGrayRule:   item.SDKGrayRule,
		FailMsg:       item.FailMsg,
		Serialization: model.SerializationType(item.Serialization),
	}
	info.DownloadInfos = getDlInfos(item.GetDownloadInfos())
	contents := make([]*ContentBlock, 0, len(item.Contents))
	for _, one := range item.Contents {
		contents = append(contents, &ContentBlock{
			Content:       one.Content,
			Compressor:    one.Compressor,
			ContentSize:   one.Size,
			ContentMD5:    one.Md5,
			ContentDesc:   one.ContentDesc,
			DownloadInfos: getDlInfos(one.DownloadInfos),
		})
	}
	info.Contents = contents

	comItem := NewServerItem(info)

	return comItem
}
