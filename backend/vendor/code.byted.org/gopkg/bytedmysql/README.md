# Go-BytedMySQL

[![](https://codebase-view.bytedance.net/api/invoke/badge?app_id=gopkg/bytedmysql)]()
[![](https://coverage-badge.bytedance.net/api/invoke/badge?app_id=gopkg/bytedmysql)]()
[![](https://img.shields.io/badge/godoc-reference-blue)](https://codebase.byted.org/godoc/code.byted.org/gopkg/bytedmysql)

A lightweight wrapper to [go-mysql-driver](https://github.com/go-sql-driver/mysql) package for bytedance


---------------------------------------
  * [Features](#features)
  * [Requirements](#requirements)
  * [Installation](#installation)
  * [Usage](#usage)
    * [DSN (Data Source Name)](#dsn-data-source-name)
      * [Protocol](#protocol)
      * [Examples](#examples)
  * [ChangeLog](#change-log)

---------------------------------------

## Features
  * Lightweight. A Lightweight wrapper to [go-mysql-driver](https://github.com/go-sql-driver/mysql) package instead of modifing source
  * High availability. Connections over consul
  * Secure. Support auth

## Requirements

---------------------------------------

## Installation
Simple install the package to your [$GOPATH](https://github.com/golang/go/wiki/GOPATH "GOPATH") with the [go tool](https://golang.org/cmd/go/ "go command") from shell:
```bash
$ go get -u code.byted.org/gopkg/bytedmysql
```
Make sure [Git is installed](https://git-scm.com/downloads) on your machine and in your system's **PATH**.

## Usage
_Go BytedDriver_ is an implementation of Go's **database/sql/driver** interface. You only need to import the driver and can use the full [**database/sql**](https://golang.org/pkg/database/sql/) API then.

Use **bytedmysql** as **driverName** and a valid [DSN](#dsn-data-source-name)  as **dataSourceName**:
```go
import "database/sql"
import _ "code.byted.org/gopkg/bytedmysql"

db, err := sql.Open("bytedmysql", "user:password@/dbname")
```

### With [GOPKG/GORM](https://github.com/go-gorm/gorm)

Projects using GORM are encouraged to use GOPKG/GORM for better integration of PSM

```go
import "code.byted.org/gopkg/gorm"

DBProxy, err := gorm.POpen("bytedmysql", "dbDSN")

DBProxy.NewRequest(ctx).First(&product, 1)
```

GOPKG/GORM works with both GORM V1 and V2, please check out [the integration guide](https://bytedance.feishu.cn/wiki/wikcn6dTiDInygIzMwsR1mmoMd8)

### With [GORM V2](https://github.com/go-gorm/gorm)

```go
import (
  _ "code.byted.org/gopkg/bytedmysql"
  "gorm.io/driver/mysql"
)

db, err := gorm.Open(mysql.New(mysql.Config{
  DriverName: "bytedmysql",
  DSN:        "@sd(toutiao.mysql.testpublicdb_write)/?timeout=5s&use_gdpr_auth=true",
}), &gorm.Config{})
```

### With [GORM](https://github.com/jinzhu/gorm)

```go
import _ "code.byted.org/gopkg/bytedmysql"

// register MySQL dialect, you can put these code in init() function
mysqlDialect, _ := gorm.GetDialect("mysql")
gorm.RegisterDialect("bytedmysql", mysqlDialect)
// connect to MySQL
db, err := gorm.Open("bytedmysql", "@sd(toutiao.mysql.testpublicdb_write)/?timeout=5s&use_gdpr_auth=true")
```

### With [XORM](https://github.com/go-xorm/xorm)
```go
import _ "code.byted.org/gopkg/bytedmysql"

// register MySQL driver, you can put these code in init() function
driver := core.QueryDriver("mysql")
core.RegisterDriver("bytedmysql", driver)
// connect to MySQL
db, err := xorm.NewEngine("bytedmysql", "@sd(toutiao.mysql.testpublicdb_write)/?timeout=5s&use_gdpr_auth=true")
```

### With [SQLX](https://github.com/jmoiron/sqlx)
```go
import _ "code.byted.org/gopkg/bytedmysql"
db, err := sqlx.Connect("bytedmysql", "@sd(toutiao.mysql.testpublicdb_write)/?timeout=5s&use_gdpr_auth=true")
```

### DSN (Data Source Name)

The Data Source Name has a common format, like e.g. [PEAR DB](http://pear.php.net/manual/en/package.database.db.intro-dsn.php) uses it, but without type-prefix (optional parts marked by squared brackets):
```
[username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
```

A DSN in its fullest form:
```
username:password@protocol(address)/dbname?param=value
```

Except for the databasename, all values are optional. So the minimal DSN is:
```
/dbname
```

If you do not want to preselect a database, leave **dbname** empty:
```
/
```
This has the same effect as an empty DSN string:
```

```

Alternatively, [Config.FormatDSN](https://godoc.org/github.com/go-sql-driver/mysql#Config.FormatDSN) can be used to create a DSN string by filling a struct.


#### Protocol
If **Protocol** is sd, the address must be service name of database which is in form of **p.s.m**


#### Examples
**use unix socket**
```
user@unix(/path/to/socket)/dbname
```
**use service discovery**
```
user:password@sd(dbServiceName)/dbname
```
**use gdpr auth**
```
@sd(dbServiceName)/?use_gdpr_auth=true
```

## ChangeLog

### v1.1.21
sql hint 信息中新增 k_env, tce_env, cluster 等信息

### v1.1.17 
新增 MYSQL_AUTH_HTTP_TIMEOUT 环境变量用于配置 Auth HTTP 超时时间

### v1.1.3
revert 1.0.0 带来的breaking

### v1.0.0 ~ v1.1.2
Breaking:  
- makeConn 函数签名改动
- statement对象未实现如下接口(原本有实现):
    - `driver.ColumnConverter`: deprecated接口
- rows 对象未实现如下接口(原本有实现):
    - `driver.RowsColumnTypeDatabaseTypeName`:
        - 说明: 获取字段在数据库中的类型
        - 用户影响: 无
    - `driver.RowsColumnTypeNullable`:
        - 说明: 获取字段是否nullable
        - 用户影响: 无 
    - `driver.RowsColumnTypePrecisionScale`:
        - 说明: 获取decimal 字段的精度
        - 用户影响: 无
    - `driver.RowsColumnTypeScanType`:
        - 说明: 返回字段对应的go 类型
        - 用户影响: 使用gorm的用户在 用`map[string]interface{}` 接收值的情况下, 对于varchar类型, 返回的类型为`[]byte` (原本是`string`)
    - `driver.RowsNextResultSet`: 
        - 说明: 支持语句中包含 多条select sql的接口
        - 影响: 不支持sql语句中包含多个select

### v0.2.0 2020.08.20
ADD: 将 logid 注入 sql
