package bytedgorm

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/hints"
)

type Migrator struct {
	mysql.Migrator
}

// ColumnTypes column types return columnTypes,error
func (m Migrator) ColumnTypes(value interface{}) ([]gorm.ColumnType, error) {
	m.DB = m.DB.Clauses(hints.CommentAfter("from", "table_name='',shard_key=0"))
	return m.Migrator.ColumnTypes(value)
}
