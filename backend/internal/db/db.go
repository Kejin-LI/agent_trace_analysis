package db

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"code.byted.org/gorm/bytedgorm"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	defaultTargetRDSPSM = "toutiao.mysql.aids_design_d2c"
	defaultDBName       = "aids_design_d2c"
)

// Open 建立 Gorm 连接。
//
// 默认采用 figma tracker 同款 bytedgorm SDK 连接方式：
//   - PSM 前缀：`toutiao.mysql.aids_design_d2c`
//   - 库名：`aids_design_d2c`
//   - `.WithReadReplicas()` 自动派生 `_read / _write`
//
// 允许通过环境变量覆盖：
//   - `DB_PSM_PREFIX`
//   - `DB_NAME`
//   - `DB_READ_TIMEOUT_MS`
//   - `DB_WRITE_TIMEOUT_MS`
//
// 若显式提供 `DB_DSN`，则仍保留直连模式作为本地调试兜底。
func Open() (*gorm.DB, error) {
	if strings.TrimSpace(os.Getenv("DB_DSN")) != "" {
		log.Printf("⚠️ [DB] 检测到 DB_DSN，使用直连模式（仅建议本地调试）")
		return openDirect()
	}
	return openSDK()
}

func openSDK() (*gorm.DB, error) {
	targetPSM := getEnvDefault("DB_PSM_PREFIX", defaultTargetRDSPSM)
	dbName := getEnvDefault("DB_NAME", defaultDBName)
	readTimeout := getDurationMS("DB_READ_TIMEOUT_MS", 3000)
	writeTimeout := getDurationMS("DB_WRITE_TIMEOUT_MS", 3000)

	log.Printf("🔌 [bytedgorm] 初始化 RDS 连接, TargetRDSPSM=%s, DB=%s", targetPSM, dbName)

	dialector := bytedgorm.MySQL(targetPSM, dbName).
		With(func(conf *bytedgorm.DBConfig) {
			conf.ReadTimeout = readTimeout
			conf.WriteTimeout = writeTimeout
		}).
		WithReadReplicas()

	gdb, err := gorm.Open(
		dialector,
		bytedgorm.ConnPool{
			MaxIdleConns: 50,
			MaxOpenConns: 100,
		},
		&gorm.Config{
			Logger: logger.Default.LogMode(logger.Warn),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("bytedgorm 初始化失败: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层 *sql.DB 失败: %w", err)
	}
	sqlDB.SetConnMaxLifetime(3 * time.Minute)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping RDS 失败 (PSM 鉴权或网络): %w", err)
	}
	log.Printf("✅ [bytedgorm] 成功连接 RDS（读写分离已启用）")
	return gdb, nil
}

// openDirect 保留给本地端口转发/显式 host:port 调试，不用于线上。
func openDirect() (*gorm.DB, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("缺少 DB_DSN；线上请使用 bytedgorm SDK 模式")
	}
	gdb, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return tune(gdb)
}

func tune(gdb *gorm.DB) (*gorm.DB, error) {
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return gdb, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDurationMS(key string, def int) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return time.Duration(def) * time.Millisecond
	}
	var ms int
	if _, err := fmt.Sscanf(v, "%d", &ms); err != nil || ms <= 0 {
		return time.Duration(def) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}
