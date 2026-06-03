/*
Package bytedmysql is a lightweight wrapper to go-mysql-driver package for bytedance.

Introduction

A lightweight wrapper to [go-mysql-driver](https://github.com/go-sql-driver/mysql) package for bytedance.

Installation

	$ go get -u code.byted.org/gopkg/bytedmysql

Basic Example

	package main

	import (
		"log"

		"github.com/go-xorm/xorm"
		"github.com/jmoiron/sqlx"
		"github.com/jinzhu/gorm"
		"xorm.io/core"
	)

	type BytedMySQLPO struct {
		ID   int64  `gorm:"column:id" xorm:"pk 'id'"`
		User string `gorm:"column:user"`
	}

	func (po *BytedMySQLPO) TableName() string {
		return "bytedmysql"
	}

	func main() {
		WithGORM()
		WithXORM()
		WithSQLX()
		WithGORMV2()
	}

	func WithGORM() {
		// 如果是公司的 GORM 不需要加下面两行
		mysqlDialect, _ := gorm.GetDialect("mysql")
		gorm.RegisterDialect("bytedmysql", mysqlDialect)
		// use_gdpr_auth 使用 auth 认证，其他的参数自定义，不要跟着抄
		db, err := gorm.Open("bytedmysql", "@sd(toutiao.mysql.testpublicdb_write)/?timeout=5s&use_gdpr_auth=true")
		if err != nil {
			log.Fatalf("open db failed, err is %s", err.Error())
		}
		if err := db.Create(&BytedMySQLPO{ID: 1, User: "testuser"}).Error; err != nil {
			log.Fatalf("create failed, err is %s", err.Error())
		}
		po := new(BytedMySQLPO)
		if err := db.Where("id = ? and user = ?", 1, "testuser").First(po).Error; err != nil {
			log.Fatalf("get failed, err is %s", err.Error())
		}
		if po.ID != 1 && po.User != "testuser" {
			log.Fatalf("want ID=1 User=testuser, but ID=%d User=%s got", po.ID, po.User)
		}
		po.User = "testupdateuser"
		if err := db.Save(po).Error; err != nil {
			log.Fatalf("update failed, err is %s", err.Error())
		}
		if err := db.Delete(po).Error; err != nil {
			log.Fatalf("delete failed, err is %s", err.Error())
		}
	}

	func WithXORM() {
		driver := core.QueryDriver("mysql")
		core.RegisterDriver("bytedmysql", driver)
		// use_gdpr_auth 使用 auth 认证，其他的参数自定义，不要跟着抄
		db, err := xorm.NewEngine("bytedmysql", "@sd(toutiao.mysql.testpublicdb_write)/?timeout=5s&use_gdpr_auth=true")
		if err != nil {
			log.Fatalf("open xorm db failed, err is %s", err.Error())
		}
		if _, err := db.Insert(&BytedMySQLPO{ID: 1, User: "testuser"}); err != nil {
			log.Fatalf("insert failed, err is %s", err.Error())
		}
		po := new(BytedMySQLPO)
		if has, err := db.Where("id = ? and user = ?", 1, "testuser").Get(po); err != nil || !has {
			if err != nil {
				log.Fatalf("get fialed, err is %s", err.Error())
			}
			if !has {
				log.Fatalf("get faield, not found")
			}
		}
		po.User = "testupdateuser"
		if _, err := db.ID(po.ID).Update(po); err != nil {
			log.Fatalf("update failed, err is %s", err.Error())
		}
		if _, err := db.ID(po.ID).Delete(po); err != nil {
			log.Fatalf("delete failed, err is %s", err.Error())
		}
	}

	func WithSQLX() {
		// use_gdpr_auth 使用 auth 认证，其他的参数自定义，不要跟着抄
		db, err := sqlx.Connect("bytedmysql", "@sd(toutiao.mysql.testpublicdb_write)/?timeout=5s&use_gdpr_auth=true")
		if err != nil {
			log.Fatalf("open sqlx failed, err is %s", err.Error())
		}
		if _, err := db.Exec("INSERT INTO `bytedmysql` (`id`, `user`) VALUES (1, 'testuser')"); err != nil {
			log.Fatalf("insert failed, err is %s", err.Error())
		}
		po := new(BytedMySQLPO)
		err = db.Get(po, "SELECT * FROM `bytedmysql` WHERE `id` = 1 AND `user` = 'testuser'")
		if err != nil {
			log.Fatalf("get failed, err is %s", err.Error())
		}
		if _, err := db.Exec("UPDATE `bytedmysql` SET `user` = 'testupdateuser' WHERE `id` = 1"); err != nil {
			log.Fatalf("update failed, err is %s", err.Error())
		}
		if _, err := db.Exec("DELETE FROM `bytedmysql` WHERE `id` = 1"); err != nil {
			log.Fatalf("delete failed, err is %s", err.Error())
		}
	}

	func WithGORMV2() {
		infsecc.SetTokenStr(token)
		// use_gdpr_auth 使用 auth 认证，其他的参数自定义，不要跟着抄
		db, err := gorm.Open(mysql.New(mysql.Config{
			DriverName: "bytedmysql",
			DSN:        "@sd(toutiao.mysql.bytedmysql_write)/?timeout=5s&use_gdpr_auth=true",
		}), &gorm.Config{})

		if err != nil {
			t.Fatalf("open gorm v2 failed, err is %s", err.Error())
		}
		if err := db.Create(&BytedMySQLPO{ID: 1, User: "testuser"}).Error; err != nil {
			t.Fatalf("create failed, err is %s", err.Error())
		}
		po := new(BytedMySQLPO)
		err = db.Take(po, "id = ? and user = ?", 1, "testuser").Error
		if err != nil {
			t.Fatalf("get failed, err is %s", err.Error())
		}
		if po.ID != 1 && po.User != "testuser" {
			t.Fatalf("want ID=1 User=testuser, but ID=%d User=%s got", po.ID, po.User)
		}
		po.User = "testupdateuser"
		if err := db.Save(po).Error; err != nil {
			t.Fatalf("update failed, err is %s", err.Error())
		}
		if err := db.Delete(po).Error; err != nil {
			t.Fatalf("delete failed, err is %s", err.Error())
		}
	}

*/
package bytedmysql
