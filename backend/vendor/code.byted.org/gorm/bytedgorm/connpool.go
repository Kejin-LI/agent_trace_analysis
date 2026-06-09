package bytedgorm

import (
	"context"
	"time"

	"code.byted.org/gopkg/logs/v2/log"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

type ConnPool struct {
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
	MaxIdleConns    int
	MaxOpenConns    int
}

func (connPool ConnPool) Apply(*gorm.Config) error {
	return nil
}

func (connPool ConnPool) AfterInitialize(db *gorm.DB) (err error) {
	var (
		dbConnPool = db.ConnPool
		dbResolver *dbresolver.DBResolver
		ok         bool
	)
	if sqlDB, err := db.DB(); err == nil && sqlDB != nil {
		dbConnPool = sqlDB
	}
	for _, plugin := range db.Plugins {
		if dbResolver, ok = plugin.(*dbresolver.DBResolver); ok {
			break
		}
	}

	ctx := context.Background()
	if connPool.ConnMaxIdleTime > 0 {
		if conn, ok := dbConnPool.(interface{ SetConnMaxIdleTime(time.Duration) }); ok && conn != nil {
			conn.SetConnMaxIdleTime(connPool.ConnMaxIdleTime)
		} else {
			log.CompatLogger.CtxNotice(ctx, "GORM Connection Pool: failed to set max idle time, go 1.15+ support this option", err)
		}
		if dbResolver != nil {
			dbResolver.SetConnMaxIdleTime(connPool.ConnMaxIdleTime)
		}
	}

	if connPool.ConnMaxLifetime > 0 {
		if conn, ok := dbConnPool.(interface{ SetConnMaxLifetime(time.Duration) }); ok && conn != nil {
			conn.SetConnMaxLifetime(connPool.ConnMaxLifetime)
		} else {
			log.CompatLogger.CtxNotice(ctx, "GORM Connection Pool: failed to set max lifetime")
		}
		if dbResolver != nil {
			dbResolver.SetConnMaxLifetime(connPool.ConnMaxLifetime)
		}
	}

	if connPool.MaxIdleConns > 0 {
		if conn, ok := dbConnPool.(interface{ SetMaxIdleConns(int) }); ok && conn != nil {
			conn.SetMaxIdleConns(connPool.MaxIdleConns)
		} else {
			log.CompatLogger.CtxNotice(ctx, "GORM Connection Pool: failed to set max idle conns")
		}
		if dbResolver != nil {
			dbResolver.SetMaxIdleConns(connPool.MaxIdleConns)
		}
	}

	if connPool.MaxOpenConns > 0 {
		if conn, ok := dbConnPool.(interface{ SetMaxOpenConns(int) }); ok && conn != nil {
			conn.SetMaxOpenConns(connPool.MaxOpenConns)
		} else {
			log.CompatLogger.CtxNotice(ctx, "GORM Connection Pool: failed to set max open conns")
		}
		if dbResolver != nil {
			dbResolver.SetMaxOpenConns(connPool.MaxOpenConns)
		}
	}

	// ignore conn pool error
	return nil
}
