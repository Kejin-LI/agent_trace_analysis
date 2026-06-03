// Package debug provides information for debugging purpose.
//
// This package doesn't maintain any backward compatibility.
//
// Please dot not use it in production.
package debug

import "code.byted.org/bcc/bcc-go-client/internal/core/pb"

// Debugger is the interface for debug information. It's used by structs that
// expect to hide debug information in the public interface, but still provide a
// way to export debug information to developers by this interface.
//
// Do not use this interface in production. This interface is not guaranteed to
// be stable.
type Debugger interface {
	// Deprecated: This method is only for TCC internal tools' debug usage. Do not
	// use it in production.
	EnvParam() *pb.EnvParam
}

type DebuggerContainer interface {
	Debugger() Debugger
}
