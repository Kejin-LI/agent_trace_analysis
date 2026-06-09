package bytedgorm

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"code.byted.org/gopkg/metainfo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ContextSkipStressForRead = "K_SKIP_STRESS"

type mode string

var (
	readMode   mode = "read"
	updateMode mode = "update"
	createMode mode = "create"
	deleteMode mode = "delete"
)

var (
	defaultContextStressKey      = "K_STRESS"
	defaultStressTestTableSuffix = "_stress" // default stress table suffix

	tableAsRegexp         = regexp.MustCompile("(?i)((?: |^)`?)([a-zA-Z0-9_.]+)(`?\\s+AS )")
	tableAliasOrTableName = regexp.MustCompile("(?i)(`?\\w+`?)\\s*(AS\\s*`?(\\w+)`?)?")
	tableNameRegexp       = regexp.MustCompile("^(?i)()`?([a-zA-Z0-9_.]+)`?()$")
	fromTableRegexp       = regexp.MustCompile("(?i)(FROM `?)([a-zA-Z0-9_.]+)(`?( |\\)))")
	insertStatementRegexp = regexp.MustCompile("(?i)(INSERT.+?INTO `?)([a-zA-Z0-9_.]+)(`?[ (])")
	updateStatementRegexp = regexp.MustCompile("(?i)(UPDATE `?)([a-zA-Z0-9_.]+)(`? SET)")
)

func WithStressTestSupport() *stressTest {
	return WithStressTestWithOption(StressTestOption{})
}

type StressTestOption struct {
	StressTestTableSuffix        string
	ContextStressKey             string
	AllowFallbackToOriginalTable bool
	CallbackSuffix               string // default will be empty
	// DontSupportTableAliasInDeleteStatement do not support `DELETE FROM tbl AS tbl_alias`
	// https://dev.mysql.com/doc/refman/8.0/en/delete.html =>false
	// https://dev.mysql.com/doc/refman/5.7/en/delete.html =>true
	DontSupportTableAliasInDeleteStatement bool

	// DontSupportTableAliasInDeleteStatementV2
	// Transform `DELETE FROM t1 AS a1 WHERE a1.field_1 = v1` To `DELETE a1 FROM t1 AS a1 WHERE a1.field_1 = v1`
	// in case of some exception, DontSupportTableAliasInDeleteStatement and DontSupportTableAliasInDeleteStatementV2 should not be set both
	DontSupportTableAliasInDeleteStatementV2 bool

	// enable checker
	EnableChecker func(ctx context.Context) bool
}

// WithStressTestWithOption: for some of the cases, such as config data, we don't necessarily create stress table
// for gray testing scenario, we can re-use this stress table solution
// Note: support different stress_key with different option, and run together
func WithStressTestWithOption(config StressTestOption) *stressTest {
	if config.ContextStressKey == "" {
		config.ContextStressKey = defaultContextStressKey
	}
	if config.StressTestTableSuffix == "" {
		config.StressTestTableSuffix = defaultStressTestTableSuffix
	}
	return &stressTest{
		StressTestOption:           config,
		ContextSkipForRead:         ContextSkipStressForRead,
		tableNameCache:             sync.Map{},
		stressTestTableSuffixBytes: []byte(config.StressTestTableSuffix),
	}
}

type stressTest struct {
	StressTestOption

	ContextSkipForRead         string
	stressTestTableSuffixBytes []byte
	tableNameCache             sync.Map
}

func (*stressTest) Apply(*gorm.Config) error {
	return nil
}

func (s *stressTest) AfterInitialize(db *gorm.DB) error {
	if !s.DontSupportTableAliasInDeleteStatement {
		if dbConfig, ok := db.Dialector.(*DBConfig); ok {
			s.DontSupportTableAliasInDeleteStatement = strings.HasPrefix(dbConfig.ServerVersion, "5.")
		}
	}
	// register stress test callbacks
	//   read operations
	name := "bytedance:stress_test" + s.CallbackSuffix
	db.Callback().Query().Before("gorm:query").Register(name, s.stressTestCallback(readMode))
	db.Callback().Row().Before("gorm:row").Register(name, s.stressTestCallback(readMode))
	//   write operations
	db.Callback().Create().Before("gorm:before_create").Register(name, s.stressTestCallback(createMode))
	db.Callback().Delete().Before("gorm:before_delete").Register(name, s.stressTestCallback(deleteMode))
	db.Callback().Update().Before("gorm:before_update").Register(name, s.stressTestCallback(updateMode))
	db.Callback().Raw().Before("gorm:raw").Register(name, s.stressTestCallback(readMode))
	return nil
}

func (s *stressTest) stressTestCallback(curMode mode) func(db *gorm.DB) {
	return func(db *gorm.DB) {
		if !s.isTestRequest(db.Statement.Context) {
			return
		}
		if curMode == readMode && s.shouldSkipStressTestsForRead(db.Statement.Context) {
			// Skip shadow table when ContextSkipStressForRead is true
			return
		}
		// Raw SQL, for example:
		//   DB.Raw("select sum(age) from users where name = ?", "name").Scan(&ages)
		// Skip shadow table when SELECT SQL & ContextSkipStressForRead is true
		rawSQL := db.Statement.SQL.String()
		if s.shouldSkipStressTestsForRead(db.Statement.Context) && isSelectStatement(rawSQL) {
			return
		}
		// Use shadow table
		if rawSQL != "" {
			db.Statement.SQL.Reset()
			db.Statement.SQL.WriteString(s.replaceWithShadowTable(db, rawSQL, curMode))
			return
		}

		// update/delete SQL doesn't support table alias
		if curMode != readMode && db.Statement.Table != "" {
			if _, ok := s.getSuffixedTableNameFromBytes(db, []byte(db.Statement.Table)); ok {
				rs := s.replaceWithShadowTable(db, db.Statement.Table, curMode)
				if db.Statement.TableExpr == nil {
					db.Statement.TableExpr = &clause.Expr{
						SQL: rs,
					}
				}
				if curMode == deleteMode && s.DontSupportTableAliasInDeleteStatementV2 {
					matches := tableAliasOrTableName.FindStringSubmatch(rs)
					switch {
					case len(matches) > 3 && matches[3] != "": // 如果存在别名，打印别名
						db.Statement.AddClauseIfNotExists(clause.Delete{
							Modifier: matches[3],
						})
					case len(matches) > 1: // 否则，则是原始表名 无需替换

					}

				}
			}
		}

		if db.Statement.TableExpr != nil {
			// Irregular Table, for example:
			//   DB.Table("users as u").Find(&users)
			//   DB.Table("`users` as u").Find(&users)
			//   DB.Table("(?) as u", db.Model(User{}).Select("Name")).Find(&users)
			db.Statement.TableExpr = &clause.Expr{
				SQL:                s.replaceWithShadowTable(db, db.Statement.TableExpr.SQL, curMode),
				Vars:               db.Statement.TableExpr.Vars,
				WithoutParentheses: db.Statement.TableExpr.WithoutParentheses,
			}
			return
		}

		if db.Statement.Table != "" {
			db.Statement.TableExpr = &clause.Expr{SQL: s.replaceWithShadowTable(db, db.Statement.Table, curMode)}
		}
	}
}

func (s *stressTest) isTestRequest(ctx context.Context) bool {
	if value, ok := metainfo.GetPersistentValue(ctx, s.ContextSkipForRead); ok && value != "" && value != "false" {
		return true
	}

	if stressTag, ok := ctx.Value(s.ContextStressKey).(string); ok {
		if stressTag == "skip" {
			return false
		}

		if stressTag != "" {
			return true
		}
	}
	// also try load from meta-info
	// see details: https://bytedance.feishu.cn/wiki/wikcnYzleW1Egpnh3GeGc36Dw5g
	if value, ok := metainfo.GetPersistentValue(ctx, s.ContextStressKey); ok {
		if value != "" {
			return true
		}
	}

	if s.EnableChecker != nil {
		return s.EnableChecker(ctx)
	}

	return false
}

func (s *stressTest) replaceWithShadowTable(db *gorm.DB, name string, curMode mode) string {
	nameBytes := []byte(name)
	for idx, re := range []*regexp.Regexp{tableAsRegexp, tableNameRegexp, fromTableRegexp, insertStatementRegexp, updateStatementRegexp} {
		matches := re.FindAllSubmatchIndex(nameBytes, -1)
		if len(matches) > 0 {
			newName := make([]byte, 0, len(nameBytes))
			newName = append(newName, nameBytes[:matches[0][0]]...)

			// considered the join multiple table cases
			for i, match := range matches {
				newName = append(newName, nameBytes[match[0]:match[3]]...)
				suffixedName, changed := s.getSuffixedTableNameFromBytes(db, nameBytes[match[4]:match[5]])
				newName = append(newName, suffixedName...)

				// setup alias table
				if changed && idx >= 1 {
					supportTableAlias := curMode == readMode
					switch {
					case isSelectStatement(name):
						supportTableAlias = true
					case isUpdateStatement(name) || curMode == updateMode:
						supportTableAlias = true
					case isInsertStatement(name) || curMode == createMode:
						supportTableAlias = false
					case isDeleteStatement(name) || curMode == deleteMode:
						supportTableAlias = !s.DontSupportTableAliasInDeleteStatement
					}

					if supportTableAlias {
						if string(nameBytes[match[6]:match[7]]) == "` " {
							newName = append(newName, []byte("` AS `")...)
						} else {
							newName = append(newName, []byte(" AS ")...)
						}
						newName = append(newName, []byte(nameBytes[match[4]:match[5]])...)
					}
				}

				newName = append(newName, nameBytes[match[6]:match[7]]...)
				if i == len(matches)-1 {
					newName = append(newName, nameBytes[match[7]:]...)
				} else {
					newName = append(newName, nameBytes[match[7]:matches[i+1][0]]...)
				}
			}
			nameBytes = newName
			break
		}
	}
	return string(nameBytes)
}

func isUpdateStatement(sql string) bool {
	return len(sql) > 6 && strings.EqualFold(sql[:6], "update")
}

func isSelectStatement(sql string) bool {
	return len(sql) > 6 && strings.EqualFold(sql[:6], "select")
}

func isInsertStatement(sql string) bool {
	return len(sql) > 6 && strings.EqualFold(sql[:6], "insert")
}

func isDeleteStatement(sql string) bool {
	return len(sql) > 11 && strings.EqualFold(sql[:11], "delete from")
}

func (s *stressTest) getSuffixedTableNameFromBytes(db *gorm.DB, tableName []byte) (newName []byte, changed bool) {
	if bytes.HasSuffix(tableName, s.stressTestTableSuffixBytes) {
		return tableName, false // already has suffix, no change
	}
	newName = make([]byte, len(tableName)+len(s.stressTestTableSuffixBytes))
	copy(newName, tableName)
	copy(newName[len(tableName):], s.stressTestTableSuffixBytes)

	// if can use original table as fallback when stress suffix table not exist , skip
	if s.AllowFallbackToOriginalTable && !s.hasStressTable(db, string(newName)) {
		return tableName, false // use original, no change
	}
	return newName, true // change to new table
}

func (s *stressTest) hasStressTable(db *gorm.DB, tableName string) bool {
	// cache
	value, ok := s.tableNameCache.Load(tableName) // with_suffix
	if ok {
		return value.(bool)
	}
	skipCtx := context.WithValue(context.Background(), s.ContextStressKey, "skip")
	v := db.Session(&gorm.Session{NewDB: true, Context: skipCtx}).Migrator().HasTable(tableName) // with_suffix
	s.tableNameCache.Store(tableName, v)
	return v
}

func (s *stressTest) shouldSkipStressTestsForRead(ctx context.Context) bool {
	if skip, ok := ctx.Value(s.ContextSkipForRead).(bool); ok {
		return skip
	}

	if v := ctx.Value(s.ContextSkipForRead); v != nil && fmt.Sprint(v) != "false" {
		return true
	}

	// also try load from meta-info
	// see details: https://bytedance.feishu.cn/wiki/wikcnYzleW1Egpnh3GeGc36Dw5g
	if value, ok := metainfo.GetPersistentValue(ctx, s.ContextSkipForRead); ok && value != "" && value != "false" {
		return true
	}

	return false
}
