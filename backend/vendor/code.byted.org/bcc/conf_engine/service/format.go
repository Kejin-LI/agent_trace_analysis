package service

import (
	"context"
	"encoding/json"

	"code.byted.org/bcc/conf_engine/jsonconf/cel"
	"code.byted.org/bcc/conf_engine/jsonconf/expr"
	"code.byted.org/bcc/conf_engine/model"
	"code.byted.org/gopkg/logs"
)

//
// GetFormatCode
//  @Description: 创建格式化后的决策树内容，用于数据传输和表达式引擎编译
//  @receiver t
//  @param varMap：全局条件变量
//  @return string：格式化后的决策树内容，可通过expr或cel-go进行编译计算
//
func GetFormatCode(ctx context.Context, t *model.JsonTree, varMap map[string]model.JSONVarInfo) ([]byte, error) {
	//先format成expr与cel的格式
	//再转化成cel与expr的confnode 序列化返回
	//sdk拿到后需要先进行编译再执行
	res, err := model.ToFormatTree(ctx, t, varMap, []model.FormatFunc{cel.FormatCondNodeCel, expr.FormatCondNode})
	if err != nil {
		logs.CtxError(ctx, "[JsonTree] toFormatForest err:%v", err)
		return nil, err
	}
	buf, err := json.Marshal(res.Nodes)
	if err != nil {
		logs.CtxError(ctx, "[JsonTree] json marshal err:%v", err)
		return nil, err
	}
	return buf, err
}
