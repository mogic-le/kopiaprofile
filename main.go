// Package main is the entry point for the kopiaprofile CLI.
//
// kopiaprofile is a configuration wrapper around the Kopia backup tool,
// inspired by resticprofile's ergonomics: a single YAML file describing
// multiple backup profiles with inheritance, hooks, locks and secret-aware
// logging.
package main

import "github.com/mogic-le/kopiaprofile/cmd"

func main() {
	cmd.Execute()
}
