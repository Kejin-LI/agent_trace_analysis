package bytedmysql

import (
	"context"
	"github.com/go-sql-driver/mysql"
)

// Config .
type Config struct {
	*mysql.Config
	To      string
	ConnCtx context.Context

	Params map[string]string
}
