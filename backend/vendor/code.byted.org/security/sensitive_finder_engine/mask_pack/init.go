package mask_pack

import (
	"reflect"
	"sync"
)

var (
	maskFunc          map[string]MaskFunc
	RecursiveTag      map[reflect.Type]map[string]string
	RecursiveTagMutex sync.RWMutex
)

func init() {
	initMaskRule()
	RecursiveTag = make(map[reflect.Type]map[string]string)
}
