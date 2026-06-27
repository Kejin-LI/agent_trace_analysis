// Package logid is ByteDance Go LogID SDK
//
// how to use：
//
//   import (
//     "code.byted.org/gopkg/logid"
//     "code.byted.org/gopkg/ctxvalues"
//   )
//
//   logID := logid.GenLogID() // generate id
//
//   logID := ctxvalues.LogID(ctx) // get log_id from context
//
//   ctx = ctxvalues.SetLogID(ctx, logid.GenLogID()) // generate and set log_id to context
//
package logid
