package common

type PingMsg struct {
}

type RegisterRspCode int32

const (
	REGISTERCODE_OK       RegisterRspCode = 0 //成功
	REGISTERCODE_RETRY    RegisterRspCode = 1 //客户端可以重试
	REGISTERCODE_PARAM    RegisterRspCode = 2 //输入参数有误
	REGISTERCODE_AUTH     RegisterRspCode = 3 //鉴权失败，拒绝访问
	REGISTERCODE_STRESS   RegisterRspCode = 4 //服务器压力过大，拒绝访问
	REGISTERCODE_VERSION  RegisterRspCode = 5 //版本过低
	REGISTERCODE_FORBID   RegisterRspCode = 6 //命中forbid规则
	REGISTERCODE_UNKNOWED RegisterRspCode = 9 //未知错误
)

var RegisterCode_name = map[int32]string{
	0: "REGISTERCODE_OK",
	1: "REGISTERCODE_RETRY",
	2: "REGISTERCODE_PARAM",
	3: "REGISTERCODE_AUTH",
	4: "REGISTERCODE_STRESS",
	5: "REGISTERCODE_VERSION",
	6: "REGISTERCODE_FORBID",
	9: "REGISTERCODE_UNKNOWED",
}

func (c RegisterRspCode) String() string {
	if str, exist := RegisterCode_name[int32(c)]; exist {
		return str
	} else {
		return "REGISTERCODE_UNKNOWED"
	}
}

type RegisterMsg struct {
	Code         RegisterRspCode `json:"code"`                    //状态码，0为成功
	CodeMsg      string          `json:"code_msg"`                //描述
	WarnMsg      string          `json:"warn_msg,omitempty"`      //让客户端打印消息
	ErrorMsg     string          `json:"error_msg,omitempty"`     //让客户端打印消息
	PingInterval int64           `json:"ping_interval,omitempty"` //ping的发送间隔（秒）
}

type UpdateKeyMsg struct {
	KeyItem *ServerItem
}

type UpdatePathMsg struct {
	Path    string
	KeyItem *ServerItem
}

type PathFinishCode int

const ( //取值要跟pb中的一致
	PathFinishSucc        PathFinishCode = 0
	PathFinishInvalidPath PathFinishCode = 1
	PathFinishDbErr       PathFinishCode = 2
)

type FinishPathMsg struct {
	Path    string
	Total   int64
	FailMsg string
	Code    PathFinishCode
}

func NewFinishPathMsg(path string, total int64, failMsg string, code PathFinishCode) *FinishPathMsg {
	msg := &FinishPathMsg{
		Path:    path,
		Total:   total,
		FailMsg: failMsg,
		Code:    code,
	}

	return msg
}

type UpdateIntervalMsg struct {
	Interval int64
}
