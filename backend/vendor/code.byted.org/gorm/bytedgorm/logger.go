package bytedgorm

import (
	"context"
	"errors"
	"time"

	logs "code.byted.org/gopkg/logs/v2"
	"code.byted.org/gopkg/logs/v2/log"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Logger struct {
	logger.LogLevel
	SlowThreshold             time.Duration // when sql execute time beyond limit, log warn to trace
	IgnoreRecordNotFoundError bool          // ignore gorm.ErrRecordNotFound as error
	IgnoreDuplicateEntryError bool          // ignore duplicate(1062) error
	ParameterizedQueries      bool
	*logs.CompatLogger
}

func (l Logger) Apply(config *gorm.Config) error {
	if l.LogLevel == 0 {
		l.LogLevel = logger.Error
	}

	if l.CompatLogger == nil {
		logOptions := append(log.V2.GetOptions(), logs.SetCallDepth(5))
		l.CompatLogger = logs.NewCompatLoggerFrom(logs.NewByteDLogger(logOptions...))
	}

	config.Logger = l
	return nil
}

func (l Logger) AfterInitialize(db *gorm.DB) error {
	return nil
}

func (l Logger) LogMode(level logger.LogLevel) logger.Interface {
	l.LogLevel = level
	return l
}

func (l Logger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= logger.Info {
		l.CtxInfo(ctx, "GORM LOG "+msg, data...)
	}
}

func (l Logger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= logger.Warn {
		l.CtxWarn(ctx, "GORM LOG "+msg, data...)
	}
}

func (l Logger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= logger.Error {
		l.CtxError(ctx, "GORM LOG "+msg, data...)
	}
}

func (l Logger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.LogLevel > logger.Silent {
		costDuration := time.Since(begin)
		cost := float64(costDuration.Nanoseconds()/1e4) / 100.0
		switch {
		case err != nil && l.LogLevel >= logger.Error && (!l.IgnoreRecordNotFoundError || !errors.Is(err, gorm.ErrRecordNotFound)) &&
			(!l.IgnoreDuplicateEntryError || !isDuplicateEntryError(err)):
			sql, _ := fc()
			l.CtxError(ctx, "GORM LOG %s, Error: %s Cost:%.2fms", sql, err.Error(), cost)
		case costDuration > l.SlowThreshold && l.SlowThreshold != 0 && l.LogLevel >= logger.Warn:
			sql, rows := fc()
			l.CtxWarn(ctx, "GORM LOG SLOW SQL:%s Rows: %d Cost:%.2fms Limit:%s", sql, rows, cost, l.SlowThreshold.String())
		case l.LogLevel >= logger.Info:
			sql, rows /* affected rows */ := fc()
			l.CtxInfo(ctx, "GORM LOG SQL:%s Rows: %d Cost:%.2fms", sql, rows, cost)
		}
	}
}

// Trace print sql message
func (l Logger) ParamsFilter(ctx context.Context, sql string, params ...interface{}) (string, []interface{}) {
	if l.ParameterizedQueries {
		return sql, nil
	}
	return sql, params
}
