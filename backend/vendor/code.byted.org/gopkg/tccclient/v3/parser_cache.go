package tccclient

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"unsafe"
)

type (
	// Unmarshaler is concrete function implement the unmarshal capability. For example: json.Unmarshal. We don't
	// recommend you hand-write these functions or coupling business logic in the unmarshal implementation.
	// If you need to write business logic, implement BizCaster instead.
	// 任何实现了Unmarshal的功能的函数皆可，比如json.Unmarshal;不推荐重载Unmarshaler，或在其中实现业务逻辑。
	// 如果你需要实现业务逻辑，请使用BizCaster，我们将会对您业务逻辑的返回进行缓存。
	// @post: @param:[]byte == []byte
	// 注意：Unmarshal实现不能更改传入的[]btye
	Unmarshaler func([]byte, interface{}) error
	// parserV2 is the interal api for parsing value
	// @post: err != nil <=> @ret:err != nil
	// @post: @ret:interface{} != emptyInterface{nil, nil}
	parserFunc func(value []byte, err error) (interface{}, error)
	// Getter gets the marshaled value of the given key.
	// Getter 返回一个对Key解析后的值或指针，保证任何情况下可以直接对其类型强转。
	// @post: reflect.Type(@ret:interface{}) != nil
	// @post: err == nil <=> useful(@ret:interface{})
	Getter func(ctx context.Context) (interface{}, error)
	// BizCaster implements simple business logic, which cast from type T to type U. You need have special care of
	// zero-value, when using pointer types, such as `*T(nil)` to `*U(nil)`.
	// To check and make use of zero value, you must check the nil value with typed-pointer, because we have zero-value
	// useful contract and typed-nil interface{} are not equal to nil, such as (interface{})((*int)(nil)) != nil
	// BizCaster 可以实现简单的业务逻辑，将对返回值缓存。注意，这里需要处理零值的情况，比如`*T(nil)`到`*U(nil)`，这需要使用有类型的指针
	// 来判空，并且返回一个可用的零值。
	BizCaster func(interface{}) interface{}

	// getterOption store the extended feature of getter api.
	getterOption struct {
		subkeys []string
		tag     string
	}
	// GetterOption is extended tweak of Getter API.
	GetterOption func(*getterOption)
)

// WithSubKey is useful to pop the subkey or sub-level inside config string. In default, we support json and yaml, if
// you are using toml or other serialize method, please specify by `WithTag("toml")`.
// WithSubKey 是用于提取在配置内容中的子配置。在默认场景下，我们支持json和yaml的反序列化的提取，如果你的配置序列化方式不是这两种，请配合
// `WithTag`方法使用，比如toml需要额外传递`WithTag("toml")`
func WithSubKey(keys ...string) GetterOption {
	return func(o *getterOption) {
		o.subkeys = keys
	}
}

// WithTag specify the tag key of unmarshal method. If you are using json or yaml, you don't need to call this method.
// Otherwise, insert this option during getter creation with the unmarshaler's tag-key. Only if you specified any
// subkeys, this method will takes effect.
// WithTag 显式的指定unmarshal实现所使用的字段tag key。如果你使用的是json或yaml，或者没有指定subkey，你不需要调用这个方法。仅当你使用类
// 似toml，并且你需要获取相应的subkey时你需要额外传递`WithTag("toml")`
func WithTag(tagName string) GetterOption {
	return func(o *getterOption) {
		o.tag = tagName
	}
}

func (o getterOption) getTagStr(key string) reflect.StructTag {
	if o.tag == "" {
		return reflect.StructTag(`json:"` + key + `" yaml:"` + key + `" xml:"` + key + `"`)
	}
	return reflect.StructTag(o.tag + `:"` + key + `"`)
}

func newGetterOpt(opts []GetterOption) getterOption {
	o := getterOption{}
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// newParserGetter implements the value container, which always return the lastest result of paser function and tcc.
func (c *ClientV3) newParserGetter(directory, confName string, parser parserFunc) Getter {
	// make a value proxy
	type Proxy struct {
		sync.RWMutex
		version string
		val     interface{}
		err     error
	}
	proxy := Proxy{}
	unsafeGetBytes := func(s string) (b []byte) {
		*(*string)(unsafe.Pointer(&b)) = s
		(*reflect.SliceHeader)(unsafe.Pointer(&b)).Cap = len(s)

		return
	}

	return func(ctx context.Context) (interface{}, error) {
		value, versionCode, err := c.getWithBccClient(ctx, directory, confName)
		var readCopy Proxy
		{
			// CRITICAL REGION
			// make a short copy to simplify the code
			proxy.RLock()
			readCopy.version = proxy.version
			readCopy.err = proxy.err
			readCopy.val = proxy.val
			proxy.RUnlock()
		}

		if err != nil {
			if readCopy.version != "" {
				// useful cache, but we get latest value failed
				return readCopy.val, err
			}

			// makes zero value useful
			return parser(nil, ErrFallbackDefault{Err: err})
		}

		if readCopy.version == versionCode {
			// hoist these variables to protect the value accessing
			return readCopy.val, readCopy.err
		}

		// CRITICAL REGION
		// write new version of cache
		proxy.Lock()
		defer proxy.Unlock()
		proxy.version = versionCode
		val, err := parser(unsafeGetBytes(value), err)
		proxy.err = err
		if readCopy.version == "" || err == nil {
			// (there is not a valid cache || unmarshal success)
			// we update the cache value
			proxy.val = val
		}

		return proxy.val, proxy.err
	}
}

// NewGetter creates a value container of given type. Each time you call the getter gets the lastest version
// of instance. You have to give in the unmarshaler implementation such as `json.Unmarshal` or `yaml.Unmarshal`.
// Moreover, you can get the subkey instance by directly append subkey to key with '#' as sperator, such as
// `client.NewGetter("/default", "key#sub#conf", json.Unmarshal, Conf{})`. If you need a default value on first error gets, you can
// use non-pointer instance with value to do so, such as `client.NewGetter("/default", "key#sub#conf", json.Unmarshal, Conf{"sth"})`
// NewGetter 创建了一个值容器，每次调用Getter会获取最新版本的对象；您需要传入一个unmarshal的实现，比如json或者yaml的。如果你的内容放在大
// Json中，你可以利用subkey功能直接取出对应值，例如
// `client.NewGetter("/directory", "key", json.Unmarshal, Conf{}, tccclient.WithSubKey("sub","conf"))`。如果你需要指定首次
// 获取时的错误值，你可以用非指针变量指定，例如
// `client.NewGetter("/directory", "key", json.Unmarshal, Conf{"sth"}, tccclient.WithSubKey("sub","conf"))`
func (c *ClientV3) NewGetter(directory, confName string, unmarshaler Unmarshaler, ins interface{}, opts ...GetterOption) Getter {
	return c.NewCastGetter(directory, confName, unmarshaler, ins, nil, opts...)
}

// NewCastGetter creates a value container of given type. Each time you call the getter gets the lastest version
// of instance. You have to give in the unmarshaler implementation such as `json.Unmarshal` or `yaml.Unmarshal`.
// Also, you need to implement the cast function, if you want to process the unmarshaled object.
// Notice that, the getter result will have the type of the return from caster function.
// NewCastGetter 创建了一个值容器，每次调用Getter会获取最新版本的对象；您需要传入一个unmarshal的实现，比如json或者yaml的;
// 您同时需要实现一个Caster函数，对于Unmarshal之后的对象进行类型转换和处理。这里需要注意的是，这个容器将不再返回您传入的类型，而是您
// Caster中返回的类型。
func (c *ClientV3) NewCastGetter(
	directory, confName string,
	unmarshaler Unmarshaler,
	ins interface{},
	caster BizCaster,
	opts ...GetterOption,
) Getter {
	// helper function copied from runtime-static
	type emptyInterface struct {
		typ  uintptr
		word uintptr
	}
	getTypPtr := func(inf interface{}) uintptr {
		return (*emptyInterface)(unsafe.Pointer(&inf)).typ
	}

	getValPtr := func(inf interface{}) uintptr {
		return (*emptyInterface)(unsafe.Pointer(&inf)).word
	}

	getInfFromPtrPair := func(typ, word uintptr) interface{} {
		ret := emptyInterface{typ: typ, word: word}
		return *(*interface{})(unsafe.Pointer(&ret))
	}

	if caster == nil {
		// identity function
		caster = func(val interface{}) interface{} {
			return val
		}
	}
	option := newGetterOpt(opts)

	typ := reflect.TypeOf(ins)
	isPtr := typ.Kind() == reflect.Ptr
	// get the zero value in by the given type
	// {*type, nil} in pointer type
	// {type, zeros} in concrete type
	zeroVal := reflect.Zero(typ).Interface()
	if len(option.subkeys) != 0 {
		// these two variable is a helper to check non-ptr fallback
		ptrIns := ins
		// localIsPtr capture the isPtr state, since we'll change it later
		localIsPtr := isPtr
		if !localIsPtr {
			// non-ptr we need to wrap a ptr to tell whether we need a fallback val
			ptrIns = reflect.New(typ).Interface()
			typ = reflect.PtrTo(typ)
		}
		// build multi-layered struct
		for i := len(option.subkeys) - 1; i >= 0; i-- {
			typ = reflect.StructOf([]reflect.StructField{{
				Name: "A", // field name doesn't matter
				Type: typ,
				Tag:  option.getTagStr(option.subkeys[i]),
			}})
		}
		// caster will unwrap the struct
		// because we know the struct layout here
		oldCaster := caster
		caster = func(val interface{}) interface{} {
			ret := getInfFromPtrPair(getTypPtr(ptrIns), getValPtr(val))
			if !localIsPtr {
				// use reflection to do the non-ptr type fallback
				rval := reflect.ValueOf(ret)
				if rval.IsNil() {
					return oldCaster(ins)
				}
				return oldCaster(rval.Elem().Interface())
			}
			return oldCaster(getInfFromPtrPair(getTypPtr(ins), getValPtr(val)))
		}
		// reset the is ptr because we have a non-ptr struct type here
		isPtr = false
		zeroVal = reflect.Zero(typ).Interface()
	} else if isPtr {
		// non-multi-leveled key && is ptr
		typ = typ.Elem()
	}

	if reflect.TypeOf(ins).Kind() != reflect.Ptr {
		// when using non-ptr type, fallback to given value
		// reloaded the type ptr here if needed
		zeroVal = getInfFromPtrPair(getTypPtr(zeroVal), getValPtr(ins))
	}

	// not unmarshal-able kinds, we panic
	switch typ.Kind() {
	case reflect.Interface, reflect.Chan, reflect.Func:
		panic("kind: " + typ.Kind().String() + " is not unmarshal of type: " + typ.Name())
	default:
		// explicit do nothing
	}

	// create a value getter
	return c.newParserGetter(directory, confName, func(value []byte, err error) (interface{}, error) {
		if err != nil {
			// redo it for each failure call, to prevent a side-effect in caster implementation
			return caster(zeroVal), err
		}
		// create an instance for input type
		// Notice: empty interface is a pointer to underlying data, unmarshal the cache value will affect
		// the distributed instance
		val := reflect.New(typ)
		err = unmarshaler(value, val.Interface())
		if err != nil {
			// reflect.New will create pointer to an object: emptyInterface{typ, ptr}
			//  but we want pointer with type: emptyInterface{typ, nil}
			return caster(zeroVal), err
		}
		if !isPtr {
			// if given is not a pointer type, we need return a non-pointer interface
			// to meet the expectation from user.
			val = val.Elem()
		}
		// cast the unmarshal type to wanted type
		return caster(val.Interface()), err
	})
}

// ErrFallbackDefault is Getter api use an default value or zero value as fallback, the value returned maybe not
// useful enough in some sinario. Thus, you may choose to panic if you meet this error.
type ErrFallbackDefault struct {
	Err error
}

func (e ErrFallbackDefault) Error() string {
	return e.Err.Error()
}

func (e ErrFallbackDefault) Unwrap() error {
	return e.Err
}

// DummyUnmarshal direct copy the data to dest value.
func DummyUnmarshal(data []byte, v interface{}) error {
	switch vType := v.(type) {
	case *string:
		*vType = string(data)
		return nil
	case *[]byte:
		*vType = make([]byte, len(data))
		copy(*vType, data)
		return nil
	default:
		return ErrUnsupportedType{Val: v}
	}
}

type ErrUnsupportedType struct {
	Val interface{}
}

func (e ErrUnsupportedType) Error() string {
	return fmt.Sprintf("unsupported type %T", e.Val)
}

// NewParserGetter returns a Getter built from a custom parser.
//
// Usage:
//   - parser receives the latest raw config bytes and fetch error from TCC.
//   - parser must return a value that is always type-assertable by callers, even when err != nil.
//   - parser should treat value as read-only.
//
// Typical pattern:
//   - if err != nil, return typed zero/default value with err (for example: Conf{}, err).
//   - if parse failed, also return typed zero/default value with err.
//   - if success, return parsed value with nil error.
//
// Hazards of incorrect usage:
//   - Returning nil interface on error (for example: return nil, err) may panic at caller side
//     when they do direct type assertion on Getter result.
//   - Mutating value is unsafe and can corrupt internal data because value may share memory with
//     internal string storage.
//   - Heavy/slow parser logic runs in cache update critical section and may increase latency.
//
// NewParserGetter 返回一个基于自定义 parser 的 Getter。
//
// 使用姿势：
//   - parser 的入参是从 TCC 获取到的原始配置的byte数组和可能的错误。
//   - 无论 err 是否为空，parser 都应返回“可被调用方直接类型断言”的值。
//   - parser 只能读取 value，禁止修改 value 内容。
//
// 推荐实现：
//   - 获取失败时返回“类型化零值/默认值 + err”（例如 Conf{}, err）。
//   - 解析失败时同样返回“类型化零值/默认值 + err”。
//   - 解析成功时返回“解析结果 + nil”。
//
// 错误使用隐患：
//   - 若在错误分支返回 nil interface（如 return nil, err），调用方若直接做类型断言可能 panic。
//   - 若修改 value，可能破坏内部共享内存数据（该 []byte 可能与内部 string 存储共享底层内存）。
//   - parser 逻辑过重会占用缓存更新临界区，放大并发场景下的调用延迟。
func (c *ClientV3) NewParserGetter(directory, confName string, parser parserFunc) Getter {
	return c.newParserGetter(directory, confName, parser)
}
