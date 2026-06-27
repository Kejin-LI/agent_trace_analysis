package model

type SnapshotData struct {
	KeyCallback  KeyCallback  `msgpack:"-" json:"-"`
	PathCallback PathCallback `msgpack:"-" json:"-"`
	Reason       string       `json:"reason"`
	SnapKeys     [][]byte     `json:"snap_keys"`  // keyInfo 序列化结果
	SnapPaths    [][]byte     `json:"snap_paths"` // pathInfo 序列化结果
}
