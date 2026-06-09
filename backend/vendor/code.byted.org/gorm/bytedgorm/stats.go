package bytedgorm

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sync"
	"time"

	"code.byted.org/gopkg/metrics/v4"
	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm/logger"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	defaultMetricPrefix = "inf.gorm"
	metricName          = "conn"
)

var (
	defaultMetricsCli     metrics.Client
	defaultMetricsCliOnce sync.Once
	metricsCliOnceMetric  sync.Map // key: metrics.Client, value: *onceMetric
)

type stats struct {
	Option StatsOption
	Metric metrics.Metric
}

type onceMetric struct {
	once   sync.Once
	metric metrics.Metric
	err    error
}

func (m *onceMetric) getMetric(cli metrics.Client) (metrics.Metric, error) {
	m.once.Do(func() {
		m.metric, m.err = cli.NewMetric(metricName, "db", "addr")
		if m.err != nil {
			return
		}
	})

	return m.metric, m.err
}

type dsnInfo struct {
	DB   string
	Addr string
}

type StatsOption struct {
	Duration          time.Duration
	MetricsClient     metrics.Client // option 1: provided metrics client, will create metric with "conn" name by it.
	WithDefaultMetric bool           // option 2: create metric with default "inf.gorm" prefix and "conn" name.
}

func WithStatsWithOption(config StatsOption) *stats {
	if config.Duration == 0 {
		config.Duration = time.Minute
	}

	if config.MetricsClient != nil {
		metricsCliOnceMetric.LoadOrStore(config.MetricsClient, new(onceMetric))
	}

	return &stats{Option: config}
}

func WithStats() *stats {
	return WithStatsWithOption(StatsOption{})
}

func (s *stats) Apply(*gorm.Config) (err error) {
	if s.Option.MetricsClient == nil && !s.Option.WithDefaultMetric {
		return nil
	}

	var cli metrics.Client

	if s.Option.MetricsClient != nil {
		cli = s.Option.MetricsClient
	} else {
		cli, err = getDefaultMetricsClient()
		if err != nil {
			return err
		}
	}

	once, _ := metricsCliOnceMetric.Load(cli)
	s.Metric, err = once.(*onceMetric).getMetric(cli)

	return err
}

func (s *stats) AfterInitialize(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	var (
		connPools  = []gorm.ConnPool{sqlDB}
		dsnInfoMap = map[gorm.ConnPool]*dsnInfo{sqlDB: parseDSNInfo(sqlDB)}
	)

	for _, plugin := range db.Plugins {
		if dbResolver, ok := plugin.(*dbresolver.DBResolver); ok {
			_ = dbResolver.Call(func(connPool gorm.ConnPool) error {
				if _, ok := dsnInfoMap[connPool]; !ok {
					connPools = append(connPools, connPool)
					dsnInfoMap[connPool] = parseDSNInfo(connPool)
				}

				return nil
			})

			break
		}
	}

	go func() {
		ticker := time.NewTicker(s.Option.Duration)

		for range ticker.C {
			for _, connPool := range connPools {
				s.doStats(db.Logger, connPool, dsnInfoMap[connPool])
			}
		}
	}()

	return nil
}

func (s *stats) doStats(logger logger.Interface, connPool gorm.ConnPool, dsn *dsnInfo) {
	statser, ok := connPool.(interface{ Stats() sql.DBStats })
	if !ok {
		return
	}

	var (
		st  = statser.Stats()
		ctx = context.Background()
	)

	// log
	label := "db stats"
	if dsn != nil {
		label = fmt.Sprintf("%s - %s", label, dsn.Addr)
	}

	logger.Info(ctx, "[%s] max open connections: %d, open: %d, inuse: %d, idle: %d, wait count: %d, wait duration: %v, wait idle closed: %d, max idle time closed: %d, max life time closed: %d", label, st.MaxOpenConnections, st.OpenConnections, st.InUse, st.Idle, st.WaitCount, st.WaitDuration, st.MaxIdleClosed, st.MaxIdleTimeClosed, st.MaxLifetimeClosed)

	// metrics
	db, addr := "-", "-"
	if dsn != nil {
		db, addr = dsn.DB, dsn.Addr
	}

	if s.Metric != nil {
		if err := s.Metric.WithTags(metrics.Tag("db", db), metrics.Tag("addr", addr)).Emit(
			metrics.WithSuffix("max").Store(st.MaxOpenConnections),
			metrics.WithSuffix("open").Store(st.OpenConnections),
			metrics.WithSuffix("inuse").Store(st.InUse),
			metrics.WithSuffix("idle").Store(st.Idle),
			metrics.WithSuffix("wait_count").Store(int(st.WaitCount)),
			metrics.WithSuffix("wait_duration").Store(int(st.WaitDuration.Microseconds())),
			metrics.WithSuffix("max_idle_closed").Store(int(st.MaxIdleClosed)),
			metrics.WithSuffix("max_idle_time_closed").Store(int(st.MaxIdleTimeClosed)),
			metrics.WithSuffix("max_life_time_closed").Store(int(st.MaxLifetimeClosed)),
		); err != nil {
			logger.Warn(ctx, "[db stats] emit metrics failed: %v", err)
		}
	}
}

func getDefaultMetricsClient() (metrics.Client, error) {
	var err error

	defaultMetricsCliOnce.Do(func() {
		defaultMetricsCli, err = metrics.NewClient(defaultMetricPrefix)
		if err != nil {
			return
		}
		metricsCliOnceMetric.LoadOrStore(defaultMetricsCli, new(onceMetric))
	})

	return defaultMetricsCli, err
}

func parseDSNInfo(pool gorm.ConnPool) *dsnInfo {
	db, ok := pool.(*sql.DB)
	if !ok {
		return nil
	}

	connectorVal := reflect.ValueOf(db).Elem().FieldByName("connector")
	if connectorVal.Kind() != reflect.Interface {
		return nil
	}

	connectorImplVal := reflect.Indirect(connectorVal.Elem())
	if connectorImplVal.Kind() != reflect.Struct {
		return nil
	}

	dsnVal := connectorImplVal.FieldByName("dsn")
	if dsnVal.Kind() != reflect.String {
		return nil
	}

	// currently only support mysql
	if cfg, err := mysql.ParseDSN(dsnVal.String()); err == nil {
		return &dsnInfo{
			DB:   cfg.DBName,
			Addr: cfg.Addr,
		}
	}

	return nil
}
