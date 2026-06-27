package expr

import (
	"context"
	"fmt"

	exprservice "code.byted.org/bcc/conf_engine/jsonconf/expr"
	json "code.byted.org/bcc/conf_engine/jsoniter"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/gopkg/logs"
)

func CompileForest(ctx context.Context, t *model.JsonTree, varMap map[string]model.JSONVarInfo) ([]*exprservice.Conf, error) {
	fs, err := model.ToFormatTree(ctx, t, varMap, []model.FormatFunc{exprservice.FormatCondNode})
	if err != nil {
		logs.CtxError(ctx, "[CompileForest] to expr FormatForest err:%v", err)
		return nil, err
	}
	buf, err := json.Marshal(fs.Nodes)
	if err != nil {
		logs.CtxError(ctx, "[CompileForest] expr json marshal err:%v", err)
		return nil, err
	}
	return Compile(buf)
}

func Compile(formatCode []byte) ([]*exprservice.Conf, error) {
	var res []*exprservice.Conf
	err := json.Unmarshal(formatCode, &res)
	if err != nil {
		return nil, fmt.Errorf("unmarshal error: %v", err)
	}
	err = exprservice.Compile(res)
	if err != nil {
		return nil, fmt.Errorf("compile error: %v", err)
	}
	return res, nil
}

// RunItem 必须经过编译后才能运行
func RunItem(ctx context.Context, oList []*exprservice.Conf, scope map[string]interface{}) (string, []int, error) {
	pathIndex := make([]int, 0)
	hit, res, err := exprservice.Run(oList, scope, &pathIndex)
	if hit {
		return res, pathIndex, nil
	}
	if err != nil {
		return "", pathIndex, err
	}
	return "", nil, nil
}
