package bytedgorm

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type options struct {
	Options []gorm.Option
}

func (opts options) Apply(conf *gorm.Config) error {
	for _, opt := range opts.Options {
		if err := opt.Apply(conf); err != nil {
			return err
		}
	}
	return nil
}

func (opts options) AfterInitialize(db *gorm.DB) error {
	for _, opt := range opts.Options {
		if err := opt.AfterInitialize(db); err != nil {
			return err
		}
	}
	return nil
}

func WithDefaults() *options {
	return &options{
		Options: []gorm.Option{
			ConnPool{
				ConnMaxIdleTime: 300 * time.Second,
				ConnMaxLifetime: 300 * time.Second,
				MaxIdleConns:    100,
				MaxOpenConns:    200,
			},
			Logger{LogLevel: logger.Error},
			WithStressTestSupport(),
		},
	}
}
