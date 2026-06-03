package util

func init() {
	//FileCache里面会调用metrics打点函数。因此必须先初始化metrics。不然会出现未定义Metrics就使用，从而panic
	InitMetricsClient()
	initFileCache()
}
