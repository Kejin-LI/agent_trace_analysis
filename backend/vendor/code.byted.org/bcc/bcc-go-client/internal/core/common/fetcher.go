package common

type OnWatchMsg struct {
	Keys []string
	Path string
}

func NewOnWatchMsg(path string, keys []string) *OnWatchMsg {
	return &OnWatchMsg{
		Keys: keys,
		Path: path,
	}
}

type UpdateType string

const (
	UpdateType_Path UpdateType = "path"
	UpdateType_Key  UpdateType = "key"
)

type OnUpdateMsg interface {
	UpdateType() UpdateType
}
type OnUpdatePathMsg struct {
	Path   string
	Delete bool
}
type OnUpdateKeyMsg struct {
	Key      string
	UpdateId int64
	Delete   bool
}

func (p *OnUpdatePathMsg) UpdateType() UpdateType {
	return UpdateType_Path
}
func (p *OnUpdateKeyMsg) UpdateType() UpdateType {
	return UpdateType_Key
}
