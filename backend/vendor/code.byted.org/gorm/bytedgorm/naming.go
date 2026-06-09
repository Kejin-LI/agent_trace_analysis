package bytedgorm

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type singularTable struct {
}

func (*singularTable) Apply(conf *gorm.Config) error {
	if conf.NamingStrategy == nil {
		conf.NamingStrategy = schema.NamingStrategy{SingularTable: true}
	} else if ns, ok := conf.NamingStrategy.(schema.NamingStrategy); ok {
		ns.SingularTable = true
		conf.NamingStrategy = ns
	} else {
		return errors.New("failed to set singularTable")
	}
	return nil
}

func (*singularTable) AfterInitialize(db *gorm.DB) error {
	return nil
}

func WithSingularTable() *singularTable {
	return &singularTable{}
}
