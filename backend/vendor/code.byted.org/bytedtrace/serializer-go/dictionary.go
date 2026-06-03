package serializer

import (
	"errors"
)

var (
	maxDictionarySize        = 255
	OverDictionarySizeError  = errors.New("key number is not allowed over than 256")
	DictionaryHasNotKeyError = errors.New("dictionary doesn't have such key")
	FlattenDictionaryError   = errors.New("dictionary flatten error")
)

// 字典接口.
type Dictionary interface {
	// 数据清零
	Reset()

	// 清除CommonKey以外的数据
	Clear()

	// 获取长度
	Len() int

	// 将字典展平
	Flatten() ([]string, error)

	// 添加一个公有的key
	AddCommonKey(key string) error

	// 添加一个普通的key
	AddKey(key string) error

	// 获取一个key的编码
	Coding(key string) (byte, error)

	// 获取一个id对应的key
	Decode(id byte) (string, error)

	// 根据平铺的keys构建字典
	Build(keys []string) error
}

func NewDictionary() Dictionary {
	return &dictionary{
		commonDic: make(map[string]byte),
		dic:       make(map[string]byte),
		decodeMap: make(map[byte]string),
	}
}

type dictionary struct {
	// 公有字典
	commonDic map[string]byte
	// 普通字段字典
	dic map[string]byte

	decodeMap map[byte]string
}

// 数据清零
func (d *dictionary) Reset() {
	d.commonDic = make(map[string]byte)
	d.dic = make(map[string]byte)
	d.decodeMap = make(map[byte]string)
}

// 清除CommonKey以外的数据 todo 池化
func (d *dictionary) Clear() {
	d.dic = make(map[string]byte)
}

func (d *dictionary) Len() int {
	return len(d.commonDic) + len(d.dic)
}

func (d *dictionary) Flatten() ([]string, error) {
	l := byte(d.Len())
	result := make([]string, l)
	for key, value := range d.commonDic {
		if value >= l {
			return nil, FlattenDictionaryError
		}
		result[value] = key
	}
	for key, value := range d.dic {
		if value >= l {
			return nil, FlattenDictionaryError
		}
		result[value] = key
	}
	return result, nil
}

// 添加一个共有的key.
func (d *dictionary) AddCommonKey(key string) error {
	_, ok := d.commonDic[key]
	if !ok {
		l := d.Len()
		if l >= maxDictionarySize {
			return OverDictionarySizeError
		}
		d.commonDic[key] = byte(l)
	}
	return nil
}

// 添加一个普通的key.
func (d *dictionary) AddKey(key string) error {
	if _, ok := d.commonDic[key]; ok {
		return nil
	}
	if _, ok := d.dic[key]; !ok {
		l := d.Len()
		if l >= maxDictionarySize {
			return OverDictionarySizeError
		}
		d.dic[key] = byte(l)
	}
	return nil
}

// 获取一个key的编码.
func (d *dictionary) Coding(key string) (byte, error) {
	if v, ok := d.commonDic[key]; ok {
		return v, nil
	}
	if v, ok := d.dic[key]; ok {
		return v, nil
	}
	return 0, DictionaryHasNotKeyError
}

// 根据平铺的keys构建字典.
func (d *dictionary) Build(keys []string) error {
	if len(keys) > maxDictionarySize {
		return OverDictionarySizeError
	}
	for i := 0; i < len(keys); i++ {
		d.decodeMap[byte(i)] = keys[i]
	}
	return nil
}

// 获取一个id对应的key.
func (d *dictionary) Decode(id byte) (string, error) {
	if key, ok := d.decodeMap[id]; ok {
		return key, nil
	}
	return "", DictionaryHasNotKeyError
}
