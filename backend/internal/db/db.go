package db

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	_ "code.byted.org/gopkg/bytedmysql"
	mysqlDriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/secret"
)

// defaultTCCDBConfigItem 是存放完整 DB 配置的 TCC 配置项名（地址元数据，非敏感）。
// 可由 DB_TCC_CONFIG env 覆盖。TCC 上该配置项内容为 JSON：
//
//	{
//	  "db_psm":      "toutiao.mysql.agent_trace_staging_write",
//	  "db_name":     "agent_trace_staging",
//	  "db_user":     "age...w-...",
//	  "db_password": "xxxx"
//	}
const defaultTCCDBConfigItem = "agentic_trace_server.db.config"

// dbConf 是连库所需的四元组凭证。
type dbConf struct {
	psm  string
	name string
	user string
	pass string
}

// Open 建立 Gorm 连接。凭证按以下优先级加载，绝不硬编码：
//
//  1. TCC 加密配置（推荐，合规首选）：从 TCC 配置项读完整 DB 配置 JSON
//     （db_psm/db_name/db_user/db_password），通过 PSM 服务发现 + RDS 鉴权连接。
//     TCC Space 由 TCC_SERVICE 控制（默认 aidp.turing.config），配置项名由
//     DB_TCC_CONFIG 控制（默认 agentic_trace_server.db.config）。
//
//  2. 环境变量兜底（本地调试）：
//     - PSM 模式：DB_PSM + DB_USER + DB_PASSWORD + DB_NAME
//     - 直连模式：DB_DSN，或 DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME
func Open() (*gorm.DB, error) {
	// 优先尝试 TCC：读到完整 PSM 四元组则直接用。
	if conf, ok := loadDBConfFromTCC(); ok {
		log.Printf("✅ [DB] 使用 TCC 加密配置连接 (psm=%s, db=%s)", conf.psm, conf.name)
		return openPSM(conf)
	}

	// 兜底：环境变量。
	if psm := os.Getenv("DB_PSM"); psm != "" {
		conf := dbConf{
			psm:  psm,
			user: os.Getenv("DB_USER"),
			pass: os.Getenv("DB_PASSWORD"),
			name: os.Getenv("DB_NAME"),
		}
		if miss := conf.missing(); len(miss) > 0 {
			return nil, fmt.Errorf("PSM 模式缺少环境变量: %v", miss)
		}
		log.Printf("⚠️ [DB] 使用环境变量 PSM 模式连接（建议改用 TCC 加密配置）")
		return openPSM(conf)
	}
	return openDirect()
}

// loadDBConfFromTCC 尝试从 TCC 读取完整 DB 配置。
// 成功且四元组齐全才返回 ok=true，否则交由环境变量兜底。
func loadDBConfFromTCC() (dbConf, bool) {
	secret.InitTCC()
	if !secret.TCCReady() {
		return dbConf{}, false
	}

	configKey := os.Getenv("DB_TCC_CONFIG")
	if configKey == "" {
		configKey = defaultTCCDBConfigItem
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	m, err := secret.GetEncryptedJSON(ctx, configKey)
	if err != nil {
		log.Printf("⚠️ [DB] TCC 拉取 %s 失败: %v，fallback 到环境变量", configKey, err)
		return dbConf{}, false
	}

	conf := dbConf{
		psm:  m["db_psm"],
		name: m["db_name"],
		user: m["db_user"],
		pass: m["db_password"],
	}
	if miss := conf.missing(); len(miss) > 0 {
		log.Printf("⚠️ [DB] TCC 配置 %s 缺少字段: %v，fallback 到环境变量", configKey, miss)
		return dbConf{}, false
	}
	return conf, true
}

// missing 返回四元组中为空的字段名。
func (c dbConf) missing() []string {
	var miss []string
	if c.psm == "" {
		miss = append(miss, "db_psm")
	}
	if c.user == "" {
		miss = append(miss, "db_user")
	}
	if c.pass == "" {
		miss = append(miss, "db_password")
	}
	if c.name == "" {
		miss = append(miss, "db_name")
	}
	return miss
}

// openPSM 通过 bytedmysql 驱动 + PSM 服务发现连接。
func openPSM(conf dbConf) (*gorm.DB, error) {
	psm := conf.psm
	user := conf.user
	pass := conf.pass
	name := conf.name

	// 使用 mysql.Config 生成 DSN，避免密码中的特殊字符破坏连接串解析。
	cfg := mysqlDriver.NewConfig()
	cfg.User = user
	cfg.Passwd = pass
	cfg.Net = "sd"
	cfg.Addr = psm
	cfg.DBName = name
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.Timeout = 10 * time.Second
	cfg.Params = map[string]string{
		"charset":       "utf8mb4",
		"use_gdpr_auth": "true",
	}
	dsn := cfg.FormatDSN()

	gdb, err := gorm.Open(gormmysql.New(gormmysql.Config{
		DriverName: "bytedmysql",
		DSN:        dsn,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("PSM 连接数据库失败: %w", err)
	}
	return tune(gdb)
}

// openDirect 通过标准 tcp DSN 连接（本地端口转发或显式 host:port）。
func openDirect() (*gorm.DB, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		host := os.Getenv("DB_HOST")
		port := getEnvDefault("DB_PORT", "3306")
		user := os.Getenv("DB_USER")
		pass := os.Getenv("DB_PASSWORD")
		name := os.Getenv("DB_NAME")

		var missing []string
		if host == "" {
			missing = append(missing, "DB_HOST")
		}
		if user == "" {
			missing = append(missing, "DB_USER")
		}
		if pass == "" {
			missing = append(missing, "DB_PASSWORD")
		}
		if name == "" {
			missing = append(missing, "DB_NAME")
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("缺少数据库环境变量: %v（或设置 DB_PSM / DB_DSN）", missing)
		}

		cfg := mysqlDriver.NewConfig()
		cfg.User = user
		cfg.Passwd = pass
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(host, port)
		cfg.DBName = name
		cfg.ParseTime = true
		cfg.Loc = time.Local
		cfg.Timeout = 10 * time.Second
		cfg.ReadTimeout = 30 * time.Second
		cfg.WriteTimeout = 30 * time.Second
		cfg.Params = map[string]string{
			"charset": "utf8mb4",
		}
		dsn = cfg.FormatDSN()
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
