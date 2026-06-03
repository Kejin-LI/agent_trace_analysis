package model

import "strings"

const (
	ScopeLoss = iota
	ScopeTypeErr
	VarNotFound
)

type JSONErr struct {
	ErrType int
	ErrMsg  string
}

func (e JSONErr) Error() string {
	return e.ErrMsg
}

func WrapJSONErr(e error) error {
	if e == nil {
		return nil
	}

	msg := e.Error()
	errType := -1

	if strings.Contains(msg, "no such key: ") {
		errType = ScopeLoss
		msg = msg[strings.Index(msg, "no such key: ")+13:]
	} else if strings.Contains(msg, "no such overload") {
		errType = ScopeTypeErr
		msg = "param type error"
	} else if strings.Contains(msg, "var not found, var_name=") {
		errType = VarNotFound
		msg = msg[strings.Index(msg, "var not found, var_name=")+24:]
	}

	return JSONErr{
		ErrType: errType,
		ErrMsg:  msg,
	}
}
