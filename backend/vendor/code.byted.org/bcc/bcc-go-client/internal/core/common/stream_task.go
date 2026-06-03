package common

type StreamPingTask struct {
	Idx int64
}

type StreamWatchPathTask struct {
	CltDir *CltDir
}
type StreamWatchKeyTask struct {
	Items map[string]*CltItem
}

func (t *StreamWatchKeyTask) Keys() []string {
	keys := make([]string, 0, len(t.Items))
	for k, _ := range t.Items {
		keys = append(keys, k)
	}
	return keys
}

func NewStreamWatchKeyTask() *StreamWatchKeyTask {
	task := &StreamWatchKeyTask{
		Items: make(map[string]*CltItem),
	}

	return task
}

type StreamReportPathTask struct {
	Path string
	Item *CltItem
}
type StreamReportKeyTask struct {
	Item *CltItem
}

type StreamCancelKeyTask struct {
	Keys []string
}

type StreamCancelPathTask struct {
	Path string
}

type RegisterTask struct {
	Info RegisterInfo
}
