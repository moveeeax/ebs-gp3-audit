// Package cmd wires the ebs-gp3-audit command-line interface.
package cmd

import (
	"github.com/spf13/cobra"
)

// Version can be overridden at build time via -ldflags; it defaults to the
// current release version.
var Version = "v0.2.0"

// NewRootCmd builds the root command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ebs-gp3-audit",
		Short:         "Find EBS volumes still on gp2/io1 and estimate gp3 migration savings",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.AddCommand(newAuditCmd())
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		if ce, ok := err.(codedError); ok {
			return ce.code
		}
		return 1
	}
	return 0
}

// codedError carries a specific exit code (used by --fail-on).
type codedError struct {
	code int
	msg  string
}

func (e codedError) Error() string { return e.msg }
