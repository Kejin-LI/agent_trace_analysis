package tccclient

import (
	"context"
	"path"
	"strings"

	bccsdk "code.byted.org/bcc/bcc-go-client/confclient"
)

type RealtimeCallback func(item RealtimeCallbackItem) error

type RealtimeCallbackItem interface {
	Namespace() string           // TCC 服务名
	Directory() string           // 目录名
	ConfName() string            // 配置名
	ClientType() string          // client 类型: KEYS 或 PATH
	Value() (string, error)      // 获取配置内容
	Version() (int64, error)     // 配置的版本号
	PublishTime() (int64, error) // 配置的发布时间
}

type internalItem struct {
	ctx        context.Context
	namespace  string     // 服务名
	clientType clientType // client 类型
	client     bccsdk.ConfClient

	bccKey string // 举例: /default/configKey
}

func newInternalItem(ctx context.Context, client bccsdk.ConfClient, bccKey string) internalItem {
	return internalItem{
		ctx:    ctx,
		client: client,
		bccKey: bccKey,
	}
}

func (item internalItem) Namespace() string {
	return item.namespace
}

func (item internalItem) Directory() string {
	ss := strings.Split(item.bccKey, dirSeparator)
	if len(ss) >= 2 {
		return rootDir + path.Join(ss[:len(ss)-1]...)
	}
	// should not occur
	return ""
}

func (item internalItem) ConfName() string {
	ss := strings.Split(item.bccKey, dirSeparator)
	if len(ss) >= 2 {
		return ss[len(ss)-1]
	}
	// should not occur
	return ""
}

func (item internalItem) ClientType() string {
	return string(item.clientType)
}

func (item internalItem) Value() (string, error) {
	return item.client.Get(item.ctx, item.bccKey)
}

func (item internalItem) Version() (int64, error) {
	return item.client.GetVersion(item.bccKey)
}

func (item internalItem) PublishTime() (int64, error) {
	return item.client.GetPublishTime(item.bccKey)
}
