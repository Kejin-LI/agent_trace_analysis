package confclient

type watchInfo struct {
	storageKeys []string
	storagePath string
}

func (w *watchInfo) addWatchKeys(keys []string) {
	w.storageKeys = append(w.storageKeys, keys...)
}

func (w *watchInfo) addWatchPath(path string) {
	w.storagePath = path
}

func (w watchInfo) getWatchKeys() []string {
	return w.storageKeys
}
func (w watchInfo) getWatchPath() string {
	return w.storagePath
}
