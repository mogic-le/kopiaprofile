// Package main is the entry point for the kopiaprofile CLI.
//
// kopiaprofile is a configuration wrapper around the Kopia backup tool,
// inspired by resticprofile's ergonomics: a single YAML file describing
// multiple backup profiles with inheritance, hooks, locks and secret-aware
// logging.
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/mogic-le/kopiaprofile/cmd"
)

func main() {
	// Ignore SIGPIPE so that piping our output to a head/tail/grep
	// that closes its stdin early doesn't kill us with exit 141.
	// The Go runtime otherwise delivers SIGPIPE on a write() to a
	// closed pipe, terminating the process before the command
	// returns normally. Stdlib tooling (git, gh, kubectl) does the
	// same dance.
	signal.Ignore(syscall.SIGPIPE)

	cmd.Execute()
	// Use os.Exit rather than letting main fall off the end so that
	// any goroutine that might still be running cannot affect our
	// exit code.
	os.Exit(0)
}
