package bytedgorm

import (
	"context"
	"fmt"

	"code.byted.org/gopkg/logs"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RdsCtxEnvOption struct {
	Tables []string
}

type rdsCtxEnv struct {
	all            bool
	tableWhiteList map[string]bool
	columnName     string
}

// WithBinlogEnvOpt tables insert SQL will inject the byte_rds_ctx column.
// by default, if no any tables passed in, then all tables will take effect
//
// Notice, if you use raw insert SQL, the plugin will not work
func WithBinlogEnvOpt(config RdsCtxEnvOption) *rdsCtxEnv {
	b := &rdsCtxEnv{
		all:            false,
		tableWhiteList: nil,
		columnName:     "byte_rds_ctx",
	}

	if len(config.Tables) > 0 {
		b.tableWhiteList = make(map[string]bool, len(config.Tables))
		for _, table := range config.Tables {
			b.tableWhiteList[table] = true
		}
	} else {
		b.all = true
	}

	return b
}

func (b *rdsCtxEnv) buildColumnName() clause.Column {
	return clause.Column{
		Table: "",
		Name:  b.columnName,
		Alias: "",
		Raw:   false,
	}
}

func (b *rdsCtxEnv) buildColumnValue(ctx context.Context) (isTestCtx bool, result string) {
	env, _ := ctx.Value("K_ENV").(string)
	if env == "" || env == "prod" || env == "-" {
		return
	}
	return true, fmt.Sprintf("{\"K_ENV\": \"%s\"}", env)
}

func (b *rdsCtxEnv) Apply(config *gorm.Config) error {
	return nil
}

func (b *rdsCtxEnv) AfterInitialize(db *gorm.DB) error {
	oriValueBuilder, ok := db.Statement.Statement.ClauseBuilders[mysql.ClauseValues]
	if !ok {
		logs.Warn(
			"[rdsCtxEnv Plugin] Dialector:%v, no default value clause builder, "+
				"will use default clause builder: c.Build(builder)",
			db.Dialector.Name(),
		)
		oriValueBuilder = func(c clause.Clause, builder clause.Builder) {
			c.Build(builder)
		}
	}

	newVB := func(c clause.Clause, builder clause.Builder) {
		valuesPlugin := func() {
			stat, ok := builder.(*gorm.Statement)
			if !ok {
				return
			}
			values, ok := c.Expression.(clause.Values)
			if !ok {
				return
			}
			if !b.all && !b.tableWhiteList[stat.Table] {
				return
			}
			if len(values.Columns) == 0 {
				return
			}
			isTestCtx, v := b.buildColumnValue(stat.Context)
			if !isTestCtx {
				return
			}
			for i, column := range values.Columns {
				if column.Name == b.columnName {
					for j, value := range values.Values {
						if value[i] == nil || value[i] == (*string)(nil) || value[i] == "" {
							values.Values[j][i] = v
						}
					}
					return
				}
			}
			values.Columns = append(values.Columns, b.buildColumnName())
			for i := range values.Values {
				values.Values[i] = append(values.Values[i], v)
			}
			c.Expression = values
		}
		valuesPlugin()
		oriValueBuilder(c, builder)
	}
	db.Statement.Statement.ClauseBuilders[mysql.ClauseValues] = newVB
	return nil
}
