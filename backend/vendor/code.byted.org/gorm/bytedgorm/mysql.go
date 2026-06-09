package bytedgorm

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/utils"
	"gorm.io/plugin/dbresolver"

	_ "code.byted.org/gopkg/bytedmysql"
	"code.byted.org/gopkg/logs/v2/log"
)

var _ gorm.Dialector = &DBConfig{}

type DBConfig struct {
	PSM               string        `yaml:"PSM"`
	DriverName        string        `yaml:"DriverName"`
	Timeout           time.Duration `yaml:"Timeout"`
	ReadTimeout       time.Duration `yaml:"ReadTimeout"`
	WriteTimeout      time.Duration `yaml:"WriteTimeout"`
	User              string        `yaml:"User"`
	Password          string        `yaml:"Password"`
	Loc               string        `yaml:"Loc"`
	DBName            string        `yaml:"DBName"`
	DBCharset         string        `yaml:"DBCharset"`
	DBHostname        string        `yaml:"DBHostname"`
	ReadDBHostname    string        `yaml:"ReadDBHostname"`
	ReadDSN           string        `yaml:"ReadDSN"`
	DBPort            string        `yaml:"DBPort"`
	InterpolateParams bool          `yaml:"InterpolateParams"`
	DSNParams         url.Values    `yaml:"DSNParms"`
	WithReturning     bool          `yaml:"WithReturning"`
	Cluster           string        `yaml:"Cluster"`
	TraceResolverMode bool          `yaml:"TraceResolverMode"`

	mysql.Dialector
	enabledReadReplicas bool
}

func (conf *DBConfig) SetDSNParam(key string, value string) {
	conf.DSNParams.Set(key, value)
}

func MySQL(psm string, dbname string) *DBConfig {
	return &DBConfig{
		PSM:               psm,
		Timeout:           5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		DBHostname:        psm + "_write",
		ReadDBHostname:    psm + "_read",
		DBName:            dbname,
		DBCharset:         "utf8mb4",
		Loc:               "Local",
		InterpolateParams: true,
		DSNParams:         url.Values{},
		Dialector: mysql.Dialector{
			Config: &mysql.Config{
				DriverName: "bytedmysql",
			},
		},
	}
}

func (config *DBConfig) With(fc func(*DBConfig)) *DBConfig {
	fc(config)
	return config
}

func (config *DBConfig) WithReadReplicas() *DBConfig {
	config.enabledReadReplicas = true
	return config
}

var (
	// Looks like IP
	ipRegexp = regexp.MustCompile(`(\d+\.){3}\d+`)
	// Looks like IPV6 IP
	ipIPV6Regexp = regexp.MustCompile(`^([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}$`)
	// Looks like url
	urlRegexp = regexp.MustCompile(`(?i)([a-z0-9]+.){2,}[a-z]+$`)
)

func (config *DBConfig) Initialize(db *gorm.DB) (err error) {
	if config.DriverName != "" {
		config.Dialector.Config.DriverName = config.DriverName
	}

	if config.WithReturning {
		//if !utils.Contains(mysql.CreateClauses, "RETURNING") {
		//	mysql.CreateClauses = append(mysql.CreateClauses, "RETURNING")
		//}

		if !utils.Contains(mysql.UpdateClauses, "RETURNING") {
			mysql.UpdateClauses = append(mysql.UpdateClauses, "RETURNING")
		}

		//if !utils.Contains(mysql.DeleteClauses, "RETURNING") {
		//	mysql.DeleteClauses = append(mysql.DeleteClauses, "RETURNING")
		//}
	}

	var (
		defaultParams = map[string]string{
			"charset":           config.DBCharset,
			"parseTime":         "True",
			"loc":               config.Loc,
			"timeout":           fmt.Sprint(config.Timeout),
			"readTimeout":       fmt.Sprint(config.ReadTimeout),
			"writeTimeout":      fmt.Sprint(config.WriteTimeout),
			"interpolateParams": fmt.Sprint(config.InterpolateParams),
			"use_gdpr_auth":     "true",
		}
		dsnFormat     = "%s:%s@sd(%s)/%s?%s"
		readDSNFormat = "%s:%s@sd(%s)/%s?%s"
		hostname      = config.DBHostname
		readHostname  = config.ReadDBHostname
	)

	// if env.IDC() == env.UnknownIDC {
	// 	if _, err := helper.GetJwtSVID(); err != nil {
	// 		delete(defaultParams, "use_gdpr_auth")
	// 	}
	// }

	if config.DriverName == "mysql2" {
		dsnFormat = "%s:%s@tcp(consul:%s)/%s?%s"
		readDSNFormat = "%s:%s@tcp(consul:%s)/%s?%s"
		delete(defaultParams, "use_gdpr_auth")
	}

	for k, v := range defaultParams {
		if config.DSNParams.Get(k) == "" {
			config.DSNParams.Set(k, v)
		}
	}

	if config.Cluster != "" {
		hostname = hostname + ".service." + strings.TrimSpace(config.Cluster)
		readHostname = readHostname + ".service." + strings.TrimSpace(config.Cluster)
	}

	if config.DBPort != "" {
		if urlRegexp.MatchString(hostname) || ipRegexp.MatchString(hostname) {
			dsnFormat = "%s:%s@tcp(%s)/%s?%s"
			config.DSNParams.Del("use_gdpr_auth")
			hostname = fmt.Sprintf("%s:%s", config.DBHostname, config.DBPort)
		}
		if ipIPV6Regexp.MatchString(hostname) {
			dsnFormat = "%s:%s@tcp(%s)/%s?%s"
			config.DSNParams.Del("use_gdpr_auth")
			hostname = fmt.Sprintf("[%s]:%s", config.DBHostname, config.DBPort)
		}

		if urlRegexp.MatchString(readHostname) || ipRegexp.MatchString(readHostname) {
			readDSNFormat = "%s:%s@tcp(%s)/%s?%s"
			readHostname = fmt.Sprintf("%s:%s", config.ReadDBHostname, config.DBPort)
		}
		if ipIPV6Regexp.MatchString(readHostname) {
			readDSNFormat = "%s:%s@tcp(%s)/%s?%s"
			readHostname = fmt.Sprintf("[%s]:%s", config.ReadDBHostname, config.DBPort)
		}
	}

	if config.DSN == "" {
		config.DSN = fmt.Sprintf(dsnFormat, config.User, config.Password, hostname, config.DBName, config.DSNParams.Encode())
	}

	for i := 0; i < 4; i++ {
		if err = config.Dialector.Initialize(db); err == nil {
			log.CompatLogger.CtxDebug(context.Background(), "connected database with primary dsn %v", config.DSN)
			break
		} else {
			if strings.Contains(config.PSM, "_write") {
				log.CompatLogger.CtxError(context.Background(), "invalid psm name %v, `_write` is not necessary, use %v", config.PSM, strings.TrimSuffix(config.PSM, "_write"))
			}

			err = fmt.Errorf("failed to connect database with dsn %v, got error %w", config.DSN, err)
		}

		if i > 2 && strings.Contains(err.Error(), "consul returned empty endpoint") {
			os.Setenv("BYTED_HOST_IPV6", "::1")
		}
	}

	if config.enabledReadReplicas && err == nil {
		readConfig := *config.Config
		if config.ReadDSN != "" {
			readConfig.DSN = config.ReadDSN
		} else {
			readConfig.DSN = fmt.Sprintf(readDSNFormat, config.User, config.Password, readHostname, config.DBName, config.DSNParams.Encode())
		}

		dbResolver := dbresolver.Register(dbresolver.Config{
			Replicas:          []gorm.Dialector{mysql.New(readConfig)},
			TraceResolverMode: config.TraceResolverMode,
		})

		err = db.Use(dbResolver)
	}

	return
}

func (config DBConfig) Migrator(db *gorm.DB) gorm.Migrator {
	m := config.Dialector.Migrator(db)
	if mysqlM, ok := m.(mysql.Migrator); ok {
		return Migrator{mysqlM}
	}
	return m
}
