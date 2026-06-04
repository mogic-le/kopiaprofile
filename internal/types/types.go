// Package types contains the data structures shared between kopiaprofile
// packages. It exists to break an import cycle:
//
//   - profile defines a Result.
//   - monitor consumes a Result and writes status files / pushes
//     Prometheus metrics.
//   - cmd wires both together.
//
// If monitor imported profile directly, and profile imported monitor
// (to obtain its Manager), the import graph would be cyclic. The
// shared types are placed in this third package to keep the
// graph acyclic.
package types

import "time"

// RunResult is the outcome of a single profile run. The
// definition mirrors profile.Result; we keep them in sync via
// converter functions.
type RunResult struct {
	Profile  string
	Action   string
	ExitCode int
	StartAt  time.Time
	EndAt    time.Time
	Duration time.Duration
	Error    string
	Hostname string
	Hooks    []RunHookResult
	Kopia    *KopiaResult
}

// RunHookResult records the outcome of a single run-* hook.
type RunHookResult struct {
	Phase    string
	Command  string
	ExitCode int
	Error    string
}

// KopiaResult is the (masked) summary of the kopia invocation.
type KopiaResult struct {
	ExitCode   int
	StderrTail string
	Argv       string
}
