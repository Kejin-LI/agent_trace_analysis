package internalerror

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"code.byted.org/bcc/tools"
)

var (
	ErrInvalidParam  = errors.New("errInvalidParam")
	ErrReuse         = errors.New("errReuse")
	ErrNotExist      = errors.New("errNotExist")
	ErrCallback      = errors.New("errCallback")
	ErrClose         = errors.New("errClose")
	ErrWriteFile     = errors.New("errWriteFile")
	ErrBigFileFail   = errors.New("errBigFileFail")
	ErrCancel        = errors.New("errCancel")
	ErrTimeout       = errors.New("errTimeout")
	ErrTosRetry      = errors.New("errTosRetry")
	ErrUnstart       = errors.New("errUnstart")
	ErrCallbackRetry = errors.New("errCallbackRetry")
	ErrUnknown       = errors.New("errUnknown")
	ErrAuthFailed    = errors.New("errAuthFailed")
	ErrItemError     = errors.New("errItemErr")
	ErrItemContent   = errors.New("errItemContent")

	ErrAuthFailedMsg = "Auth failed because your psm has no permission to access this config, you should add acl rule on your namespace."
)

// 错误定义
type Error interface {
	error                        //错误描述，每个类型最多5个，格式为 [err:count:key11;key12],[err:count:key21;key22]
	ClassifyFatal() bool         //【归类】严重错误，无法恢复，内部终止监听 //外部没必要重试，通常需要修复配置或重启或oncall
	ClassifyTemporary() bool     //【归类】暂时错误，不断重试，内部继续监听 //外部不需要重试，如果不想监听要调用 CancelXXX()
	IsInvalidParam() bool        //【严重错误】无效参数（key格式问题、callback为空、参数冲突等）（WatchKey可能会部分成功）
	IsReuse() bool               //【严重错误】重复监听（WatchKey可能会部分成功）
	ReuseKey() map[string]bool   //【其他】获取那些key是重复监听失败的
	IsNotExist() bool            //【严重错误】不存在key，通过WithWatchEmpty不会出现该错误
	IsCallback() bool            //【严重错误】回调失败
	IsClose() bool               //【严重错误】被关闭，被调用Close()，后续调用者都会收到错误
	IsWriteFile() bool           //【严重错误】写文件失败，很可能目录没权限
	IsTosFail() bool             //【严重错误】tos异常，无法恢复
	IsAuthFail() bool            //【严重错误】鉴权失败
	IsCancel() bool              //【严重错误】被取消，被调用CancelKey()或CancelPath()
	IsTimeout() bool             //【暂时错误】超时错误，使用了WithWatchTimeout()才会出现，内部重试
	IsTosRetry() bool            //【暂时错误】tos异常，内部重试
	IsUnstart() bool             //【暂时错误】未开始，被其他key失败所连累，内部会继续下载（设置了参数WithWatchFastTerminate）
	IsCallbackRetry() bool       //【暂时错误】回调失败，会重试
	IsUnknown() bool             //【暂时错误】未知错误
	AsMap() map[string]BaseError //出现错误的key（timeout/unstart等情况map会很多kv）
	json.Unmarshaler
	json.Marshaler
}

type BaseError interface {
	error
	ClassifyFatal() bool
	Unwrap() error
	ClassifyTemporary() bool
	json.Unmarshaler
	json.Marshaler
	Is(target error) bool
}

var _ error = (*baseError)(nil)
var _ BaseError = (*baseError)(nil)

type baseError struct {
	err error
	msg string
}

func (e *baseError) UnmarshalJSON(i []byte) error {
	type tmp struct {
		Err string `json:"err"`
		Msg string `json:"msg"`
	}
	t := &tmp{}
	err := tools.JsonUnmarshal(i, t)
	if err != nil {
		return err
	}
	e.err = errors.New(t.Err)
	e.msg = t.Msg
	return nil
}

// 不支持嵌套error类型
func (e *baseError) MarshalJSON() (data []byte, err error) {
	return tools.JsonMarshalUnordered(map[string]interface{}{
		"err": e.err.Error(),
		"msg": e.msg,
	})
}

func (e *baseError) Error() string {
	return fmt.Sprintf("%v %v", e.msg, e.err)
}

// Is 序列化和反序列化过程中，error与model定义的error不是一个了，需要实现 is 接口，保证判断 error 类型一致
func (e *baseError) Is(tar error) bool {
	return e.err.Error() == tar.Error()
}

func (e *baseError) ClassifyFatal() bool {
	switch e.err {
	case ErrInvalidParam, ErrReuse, ErrNotExist, ErrAuthFailed, ErrCallback, ErrClose, ErrWriteFile, ErrBigFileFail, ErrCancel:
		return true
	}
	return false
}

func (e *baseError) Unwrap() error {
	return e.err
}

func (e *baseError) ClassifyTemporary() bool {
	switch e.err {
	case ErrTimeout, ErrTosRetry, ErrUnstart, ErrCallbackRetry, ErrUnknown:
		return true
	}
	return false
}

func NewError(err error, format string, a ...interface{}) BaseError {
	return &baseError{
		err: err,
		msg: fmt.Sprintf(format, a...),
	}
}

var _ error = (*MultiError)(nil)
var _ Error = (*MultiError)(nil)

type MultiError struct {
	errs map[string]BaseError //key->error
}

func (t *MultiError) UnmarshalJSON(i []byte) error {
	type tmp struct {
		Errs map[string]*baseError `json:"errs"`
	}
	p := new(tmp)
	err := tools.JsonUnmarshal(i, p)
	if err != nil {
		return err
	}

	t.errs = make(map[string]BaseError)
	for k, v := range p.Errs {
		t.errs[k] = v

	}
	return nil
}

func (t *MultiError) MarshalJSON() ([]byte, error) {
	return tools.JsonMarshalUnordered(map[string]interface{}{
		"errs": t.errs,
	})
}

func (t *MultiError) ClassifyTemporary() bool {
	for _, v := range t.errs {
		if v.ClassifyTemporary() {
			return true
		}
	}
	return false
}

func (t *MultiError) IsInvalidParam() bool {
	return t.checkError(ErrInvalidParam)
}

func (t *MultiError) IsReuse() bool {
	return t.checkError(ErrReuse)
}

func (t *MultiError) ReuseKey() (keys map[string]bool) { //监听目录时的失败也可以使用这个接口
	keys = map[string]bool{}
	for key, v := range t.errs {
		if errors.Is(v, ErrReuse) {
			keys[key] = true
		}
	}

	return
}

func (t *MultiError) IsNotExist() bool {
	return t.checkError(ErrNotExist)

}

func (t *MultiError) IsCallback() bool {
	return t.checkError(ErrCallback)

}

func (t *MultiError) IsClose() bool {
	return t.checkError(ErrClose)
}

func (t *MultiError) IsWriteFile() bool {
	return t.checkError(ErrWriteFile)

}

func (t *MultiError) IsTosFail() bool {
	return t.checkError(ErrBigFileFail)

}

func (t *MultiError) IsCancel() bool {
	return t.checkError(ErrCancel)

}

func (t *MultiError) IsTimeout() bool {
	return t.checkError(ErrTimeout)

}

func (t *MultiError) IsTosRetry() bool {
	return t.checkError(ErrTosRetry)

}

func (t *MultiError) IsUnstart() bool {
	return t.checkError(ErrUnstart)
}

func (t *MultiError) IsAuthFail() bool {
	return t.checkError(ErrAuthFailed)
}

func (t *MultiError) IsCallbackRetry() bool {
	return t.checkError(ErrCallbackRetry)
}

func (t *MultiError) IsItemErr() bool {
	return t.checkError(ErrItemError)
}

func (t *MultiError) IsUnknown() bool {
	return t.checkError(ErrUnknown)

}

func NewMultiError() *MultiError {
	err := &MultiError{
		errs: make(map[string]BaseError),
	}

	return err
}

func (t *MultiError) Add(key string, err BaseError) {
	t.errs[key] = err
}

func (t *MultiError) HasError() bool {
	return len(t.errs) != 0 //判断逻辑参考前面的Add方法
}

func (t *MultiError) ClassifyFatal() bool {
	for _, v := range t.errs {
		if v.ClassifyFatal() {
			return true
		}
	}
	return false
}

func (t *MultiError) AsMap() map[string]BaseError {
	return t.errs
}
func (t *MultiError) Del(key string) {
	delete(t.errs, key)
}

func (t *MultiError) Empty() bool {
	return len(t.errs) == 0
}
func (t *MultiError) Error() string {
	if t.Empty() {
		return ""
	}

	s := "Errors:"

	errKeysMap := make(map[string][]string)
	for k, v := range t.errs {
		errMsg := v.Error()
		errKeysMap[errMsg] = append(errKeysMap[errMsg], k)
	}

	var (
		i        = 0
		indexStr func() string
	)
	if len(errKeysMap) == 1 {
		indexStr = func() string { return "" }
	} else {
		indexStr = func() string { i++; return fmt.Sprintf("[%v] ", i) }
	}

	for errMsg, keys := range errKeysMap {
		s += "\n" + indexStr() + fmt.Sprintf("fetch keys 「%s」 failed with error: %v", strings.Join(keys, " "), errMsg)
	}

	return s
}

func (t *MultiError) checkError(err error) bool {
	for _, v := range t.errs {
		if errors.Is(v, err) {
			return true
		}
	}
	return false
}
