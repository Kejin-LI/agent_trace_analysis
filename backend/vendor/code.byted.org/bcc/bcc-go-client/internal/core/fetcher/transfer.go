package fetcher

import (
	"code.byted.org/bcc/bcc-go-client/coreclient/model"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	cmodel "code.byted.org/bcc/pull_json_model/bccgrpc"
)

func ToServerItem(item *cmodel.SvrItem) *common.ServerItem {
	var contents []*common.ContentBlock
	if len(item.Contents) > 0 {
		contents = make([]*common.ContentBlock, 0, len(item.Contents))
		for _, one := range item.Contents {
			contents = append(contents, &common.ContentBlock{
				Content:       one.Content,
				Compressor:    one.Compressor,
				ContentSize:   one.Size,
				ContentMD5:    one.Md5,
				ContentDesc:   one.ContentDesc,
				DownloadInfos: toDownloadInfos(one.DownloadInfos),
			})
		}
	}

	svrItem := common.NewServerItem(&common.SvrKeyInfo{
		Key:           item.Key,
		Value:         item.Value,
		Version:       item.Version,
		Md5:           item.Md5,
		Desc:          item.Desc,
		Compressor:    item.Compressor,
		Status:        common.ItemStatus(item.Status),
		HitEnv:        item.HitEnv,
		Size:          item.Size,
		UpdateTime:    item.UpdateTime,
		UpdateID:      item.UpdateId,
		SDKGrayRule:   item.SDKGrayRule,
		DownloadInfos: toDownloadInfos(item.DownloadInfos),
		Serialization: model.SerializationType(item.Serialization),
		Contents:      contents,
	})
	return svrItem
}

func toDownloadInfos(infos []*cmodel.DownloadInfo) []*common.DownloadInfo {
	var downloadInfos []*common.DownloadInfo
	for _, info := range infos {
		downloadInfos = append(downloadInfos, &common.DownloadInfo{
			Url:    info.Url,
			Source: common.DownloadItemSource(info.Source),
			Agent:  info.Agent,
		})
	}
	return downloadInfos
}
