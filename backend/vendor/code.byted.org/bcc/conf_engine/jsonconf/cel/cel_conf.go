package cel

import (
	"fmt"

	"code.byted.org/bcc/conf_engine/model"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"code.byted.org/ttarch/byteconf-cel-go/cel"

	"github.com/golang/protobuf/proto"
)

type Conf struct {
	CEL      *model.CelData `json:"cond"`
	Conf     *model.CelData `json:"conf"`
	Children []Conf         `json:"children"`
}

func Program(o *model.CelData, env *cel.Env) error {
	var ast *cel.Ast
	if len(o.Expr) > 0 {
		var rExpr exprpb.ParsedExpr
		if err := proto.Unmarshal(o.Expr, &rExpr); err != nil {
			return fmt.Errorf("unmarshal error: %v", err)
		}
		ast = cel.ParsedExprToAst(&rExpr)
	} else {
		var errI *cel.Issues
		ast, errI = env.Compile(o.Txt)
		if errI != nil {
			return fmt.Errorf("compile error: %v", errI)
		}
	}
	var err error
	o.Prg, err = env.Program(ast)
	if err != nil {
		return fmt.Errorf("Program error: %v", err)
	}
	return nil
}

func (o *Conf) TProgram(env *cel.Env) error {
	if o == nil {
		return nil
	}
	if err := Program(o.CEL, env); err != nil {
		return fmt.Errorf("CEL Program error: %v", err)
	}
	if len(o.Children) == 0 {
		if err := Program(o.Conf, env); err != nil {
			return fmt.Errorf("Conf Program error: %v", err)
		}
		return nil
	}
	for i := len(o.Children) - 1; i >= 0; i-- {
		if err := o.Children[i].TProgram(env); err != nil {
			return fmt.Errorf("ch[%v] TProgram error: %v", i, err)
		}
	}
	return nil
}

func InitProgram(cList []Conf, env *cel.Env) error {
	if len(cList) == 0 {
		return nil
	}
	for i := len(cList) - 1; i >= 0; i-- {
		if err := cList[i].TProgram(env); err != nil {
			return fmt.Errorf("{%v} TProgram error: %v", i, err)
		}
	}
	return nil
}

// Marshal 序列化编译的结果
func Marshal(o *model.CelData, env *cel.Env) error {
	ast, errI := env.Compile(o.Txt)
	if errI != nil {
		return fmt.Errorf("compile error: %v", errI)
	}
	expr, err := cel.AstToParsedExpr(ast)
	if err != nil {
		return fmt.Errorf("astToParsedExpr error: %v", err)
	}
	b, err := proto.Marshal(expr)
	if err != nil {
		return fmt.Errorf("Marshal error: %v", err)
	}
	o.Expr = b
	return nil
}

func (o *Conf) TMarshal(env *cel.Env) error {
	if o == nil {
		return nil
	}
	if err := Marshal(o.CEL, env); err != nil {
		return fmt.Errorf("CEL Marshal error: %v", err)
	}
	if len(o.Children) == 0 {
		if err := Marshal(o.Conf, env); err != nil {
			return fmt.Errorf("Conf Marshal error: %v", err)
		}
		return nil
	}
	for i := len(o.Children) - 1; i >= 0; i-- {
		if err := o.Children[i].TMarshal(env); err != nil {
			return fmt.Errorf("ch[%v] TMarshal error: %v", i, err)
		}
	}
	return nil
}

func InitMarshal(cList []*Conf, env *cel.Env) error {
	if len(cList) == 0 {
		return nil
	}
	for i := len(cList) - 1; i >= 0; i-- {
		if err := cList[i].TMarshal(env); err != nil {
			return fmt.Errorf("ch[%v] TMarshal error: %v", i, err)
		}
	}
	return nil
}
