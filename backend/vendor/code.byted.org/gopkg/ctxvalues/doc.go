// Package ctxvalues is reader and writer for `context.Context`
//
// how to use：
//
//   first, import `ctxvalues` package
//
//   import "code.byted.org/gopkg/ctxvalues"
//
// logid
//
//   // get logid from ctx
//   logid, ok := ctxvalues.LogID(ctx)
//
//   // set logid to ctx
//   ctx = ctxvalues.SetLogID(ctx, "<logid>")
//
// stress_tag
//
//   // get stress_tag from ctx
//   stressTag, ok := ctxvalues.StressTag(ctx)
//
// rpc method
//
//   // get rpc method from ctx
//   method, ok := ctxvalues.Method(ctx)
//
// for others, please use ide to auto use
package ctxvalues
