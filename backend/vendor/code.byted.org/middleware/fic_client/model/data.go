package model

type Metadata struct {
	PSM     string `json:"psm"`
	Cluster string `json:"cluster"`
	IDC     string `json:"idc"`
}

type Framework struct {
	Name    string                 `json:"name"`
	Version string                 `json:"version"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

type Data struct {
	Metadata   Metadata               `json:"metadata"`
	Frameworks []Framework            `json:"frameworks"`
	Extra      map[string]interface{} `json:"extra,omitempty"`

	LogID string `json:"log_id,omitempty"`
}
