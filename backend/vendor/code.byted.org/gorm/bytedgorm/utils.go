package bytedgorm

import (
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const mysqlErrCode_DuplicatedEntry = 1062

func isDuplicateEntryError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		if mysqlErr.Number == mysqlErrCode_DuplicatedEntry {
			return true
		}
	}

	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	return false
}
