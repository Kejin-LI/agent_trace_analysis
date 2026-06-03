package confcontent

import (
	"code.byted.org/bcc/conf_engine/confcontent/grayrule"
	"code.byted.org/bcc/conf_engine/model"
)

type ConfType int

const (
	ConfTypeRegular ConfType = iota
	ConfTypeDynamicJson
)

const (
	ConfVersionEmpty = -1 // the version will be set to -1, indicating that the online version or gray version does not exist
	ConfVersionInit  = -9
)

// 多块文件传输时，描述每分块的作用，以此组装成content
const (
	ConfDescMeta     = "meta"
	ConfDescBase     = "base"
	ConfDescFullBase = "fullBase"
	ConfDescGrayRule = "grayRule"
)

type Content struct {
	NsKey       string                       `json:"ns_key"`
	Path        string                       `json:"biz_tree_path"`
	Name        string                       `json:"name"`
	ConfType    ConfType                     `json:"type"`         // config type
	PublishTime int64                        `json:"publish_time"` // config publish time
	Version     int64                        `json:"version"`      // current online version. -1: not exist config, creating a config for the first time.
	GrayVersion int64                        `json:"gray_version"` // gray version
	VarMap      map[string]model.JSONVarInfo `json:"var_map"`      // conditional variables, used in gray-publish or json-config
	GrayRule    *grayrule.GrayRule           `json:"gray_rule"`    // gray rule
	Base        []byte                       `json:"base"`         // current or gray content for the config
	FullBase    []byte                       `json:"full_base"`    // previous version of config content
	SchemaName  string                       `json:"schema_name"`  // schema name
}
