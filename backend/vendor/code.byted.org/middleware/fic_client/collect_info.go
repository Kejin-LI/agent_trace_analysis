package fic_client

import (
	"reflect"
	"sync"

	"code.byted.org/middleware/fic_client/model"
)

type collectInfo struct {
	lock      sync.Mutex
	psm       string
	cluster   string
	idc       string
	framework map[string]model.Framework
	extra     map[string]interface{}
}

func newCollectInfo(psm, cluster, idc string) *collectInfo {
	return &collectInfo{
		psm:       psm,
		cluster:   cluster,
		idc:       idc,
		framework: make(map[string]model.Framework, 3),
		extra:     make(map[string]interface{}, 3),
	}
}

func (c *collectInfo) AddFramework(name, version string, data map[string]interface{}) bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	if old, ok := c.framework[name]; ok && reflect.DeepEqual(old.Data, data) {
		return false
	}
	c.framework[name] = model.Framework{
		Name:    name,
		Version: version,
		Data:    data,
	}
	return true
}

func (c *collectInfo) GetFramework(name string) (fw model.Framework, ok bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	fw, ok = c.framework[name]
	return
}

func (c *collectInfo) SetExtra(key string, value interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.extra[key] = value
}

func (c *collectInfo) GetExtra(key string) (value interface{}, ok bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	value, ok = c.extra[key]
	return
}

func (c *collectInfo) GetModelData() *model.Data {
	c.lock.Lock()
	defer c.lock.Unlock()
	return &model.Data{
		Metadata: model.Metadata{
			PSM:     c.psm,
			Cluster: c.cluster,
			IDC:     c.idc,
		},
		Frameworks: c.copyFrameworkAsList(),
		Extra:      c.copyExtraAsMap(),
	}
}

func (c *collectInfo) copyFrameworkAsList() []model.Framework {
	result := make([]model.Framework, 0, len(c.framework))
	for _, value := range c.framework {
		result = append(result, value)
	}
	return result
}

func (c *collectInfo) copyExtraAsMap() map[string]interface{} {
	result := make(map[string]interface{}, len(c.extra))
	for key, value := range c.extra {
		result[key] = value
	}
	return result
}
