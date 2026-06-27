package expr

import (
	"fmt"
	"strconv"

	"code.byted.org/bcc/conf_engine/model"

	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/file"
)

type Conf struct {
	Cond     *model.ExprData   `json:"cond"`
	Conf     *model.FormatConf `json:"conf"`
	Children []*Conf           `json:"children"`
}

func (c *Conf) compile(options []expr.Option) error {
	err := compile(c.Cond, options)
	if err != nil {
		return err
	}
	if len(c.Children) == 0 {
		return nil
	}
	for i := len(c.Children) - 1; i >= 0; i-- {
		if err = c.Children[i].compile(options); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conf) run(scope map[string]interface{}, pathIndex *[]int) (hit bool, res string, err error) {
	if hit, err = runMustBool(c.Cond, scope); !hit || err != nil { //没有命中条件或者出错。
		return false, "", err
	}

	if len(c.Children) == 0 {
		t, err := strconv.Unquote(c.Conf.Txt)
		return true, t, err
	}

	//需要逆序遍历子树
	for i := len(c.Children) - 1; i >= 0; i-- {
		*pathIndex = append(*pathIndex, i)
		if hit, res, err = c.Children[i].run(scope, pathIndex); hit || err != nil {
			return hit, res, err
		}
		*pathIndex = (*pathIndex)[:len(*pathIndex)-1]
	}
	return
}
func runMustBool(d *model.ExprData, scope map[string]interface{}) (bool, error) {
	res, err := run(d, scope)
	if err != nil {
		return false, err
	}
	ret, ok := res.(bool)
	if !ok {
		return false, fmt.Errorf("result not Bool: ret=%+v, type=%T", res, res)
	}
	return ret, nil
}
func runMustString(d *model.ExprData, scope map[string]interface{}) (string, error) {
	res, err := run(d, scope)
	if err != nil {
		return "", err
	}
	ret, ok := res.(string)
	if !ok {
		return "", fmt.Errorf("result not String: ret=%+v, type=%T", res, res)
	}
	return ret, nil
}
func run(d *model.ExprData, scope map[string]interface{}) (interface{}, error) {

	res, err := expr.Run(d.Prg, scope)
	if err != nil {
		return "", err
	}
	return res, nil
}

func compile(data *model.ExprData, options []expr.Option) error {
	var err error

	data.Prg, err = expr.Compile(data.Txt, options...)
	if err != nil {
		switch err.(type) {
		case *file.Error:
			fileErr := err.(*file.Error)
			return fmt.Errorf("json格式错误,line = %v , snippet = %v", fileErr.Line, fileErr.Snippet)
		default:
			return err
		}
	}

	return err
}
