package confclient

import (
	"errors"

	internalerror "code.byted.org/bcc/bcc-go-client/internal/error"
)

const (
	keyCallbackError  = "exec key callback fail"
	pathCallbackError = "exec path callback fail"
)

var (
	errConfigNotAvailable    = errors.New("config not available") //一般是由于创建了配置但还没发布引起了
	errConfigUnknownType     = errors.New("unknown conf type")
	errDynamicConfigNotMatch = errors.New("scope don't match any rule of the config")
	errMissModel             = errors.New("missing model to unmarshal conf")
	errModelParse            = errors.New("parse model fail")
	errConfigInternalError   = errors.New("internal error")
)

// IsFatalError internal error, retry is useless
func IsFatalError(err error) bool {
	//conf client error
	if errors.Is(err, errConfigInternalError) {
		return true
	}

	//core client error
	if multiError, ok := err.(internalerror.Error); ok && multiError.ClassifyFatal() {
		return true
	}

	if e, ok := err.(internalerror.BaseError); ok && e.ClassifyFatal() {
		return true
	}

	return false
}

// IsTemporaryError not the fatal error, can be handled, should modify input param and retry
func IsTemporaryError(err error) bool {
	//core client error
	if multiError, ok := err.(internalerror.Error); ok {
		return multiError.ClassifyTemporary()
	}
	//conf client error
	return !errors.Is(err, errConfigInternalError)
}

func IsReuseError(err error) bool {
	//core client error
	if multiError, ok := err.(internalerror.Error); ok {
		return multiError.IsReuse()
	}

	return false
}

func ReuseKey(err error) (keys map[string]bool) { //目录监听也能调用本函数
	if multiError, ok := err.(internalerror.Error); ok {
		keys = multiError.ReuseKey()
	}
	return
}

func IsConfigNotAvailableError(err error) bool {
	return errors.Is(err, errConfigNotAvailable)
}

func IsConfigUnknownTypeError(err error) bool {
	return errors.Is(err, errConfigUnknownType)
}

func IsDynamicConfigNotMatchError(err error) bool {
	return errors.Is(err, errDynamicConfigNotMatch)
}

func IsMissModelError(err error) bool {
	return errors.Is(err, errMissModel)
}

func IsModelParseError(err error) bool {
	return errors.Is(err, errModelParse)
}

func IsConfNotExistError(err error) bool {
	return errors.Is(err, internalerror.ErrNotExist)
}

func IsConfigInternalError(err error) bool {
	return errors.Is(err, errConfigInternalError)
}

func IsConfigAuthFail(err error) bool {
	return errors.Is(err, internalerror.ErrAuthFailed)
}

// 用于打点
func errorType(err error) string {
	if err == nil {
		return "nil"
	}

	errorCheckers := []struct {
		checker func(error) bool
		typ     string
	}{
		{IsReuseError, "reuse"},
		{IsConfigAuthFail, "auth"},
		{IsConfNotExistError, "not_exist"},
		{IsTemporaryError, "temp_err"},
		{IsFatalError, "fatal_err"},
	}

	for _, checker := range errorCheckers {
		if checker.checker(err) {
			return checker.typ
		}
	}

	return "unknown"
}
