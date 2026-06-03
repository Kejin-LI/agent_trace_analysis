# Go LogID SDK

[![](https://badge.byted.org/view_count/gl-gopkg-logid)]()
[![](https://badge.byted.org/ci/coverage/gopkg/logid)]()
[![](https://badge.byted.org/go/package/depended_count/gopkg/logid)]()
[![](https://badge.byted.org/go/package/version/gopkg/logid)]()
[![](https://badge.byted.org/go/doc/gopkg/logid)](https://codebase.byted.org/godoc/code.byted.org/gopkg/logid/)

ByteDance Go LogID SDK

## How to install

install go module

```go
go get code.byted.org/gopkg/logid
```

install command line tool

```bash
go get code.byted.org/gopkg/logid/cmd/logid
```

## How to use

- generate log_id

```go
package main

import (
    "fmt"

    "code.byted.org/gopkg/logid"
)

func example()  {
    logID := logid.GenLogID()
    fmt.Println("logID", logID) 
}
```

- get or set log_id with context

```go
package main

import (
    "context"

    "code.byted.org/gopkg/ctxvalues"
    "code.byted.org/gopkg/logid"
)

func example(ctx context.Context)  {
    // get log_id from context
    logID := ctxvalues.LogID(ctx)

    // set log_id to context
    ctx = ctxvalues.SetLogID(ctx, logid.GenLogID()) 
}
```
