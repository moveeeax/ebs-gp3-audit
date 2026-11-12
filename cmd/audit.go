package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/moveeeax/ebs-gp3-audit/internal/audit"
	"github.com/moveeeax/ebs-gp3-audit/internal/report"
	"github.com/moveeeax/ebs-gp3-audit/internal/source"
)

func newAuditCmd() *cobra.Command {
	var (
		output   string
		region   string
		fromFile string
		prices   string
		failOn   float64
		match    string
		emit     string
		emitFmt  string
	)

	c := &cobra.Command{
		Use:   "audit",
		Short: "Audit EBS volumes and estimate gp3 migration savings",
		Long: "Audit enumerates EBS volumes (read-only DescribeVolumes, or a JSON\n" +
			"snapshot with --from-file), flags gp2/io1/io2 volumes that would cost\n" +
			"less on gp3, and reports per-volume and fleet-wide monthly savings.\n\n" +
			"With --emit it also writes a migration plan (an aws CLI script or\n" +
			"Terraform attribute suggestions). The tool itself never modifies volumes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := audit.ParseMatch(match)
			if err != nil {
				return fmt.Errorf("--match-performance: %w", err)
			}
			var writePlan func(io.Writer, audit.Report, string) error
			switch emitFmt {
			case "cli", "":
				writePlan = report.PlanScript
			case "terraform":
				writePlan = report.PlanTerraform
			default:
				return fmt.Errorf("unknown --emit-format %q (want cli or terraform)", emitFmt)
			}
			if cmd.Flags().Changed("emit-format") && emit == "" {
				return fmt.Errorf("--emit-format needs --emit <path>")
			}

			p := audit.DefaultPrices()
			if prices != "" {
				loaded, err := audit.LoadPrices(prices)
				if err != nil {
					return err
				}
				p = loaded
			}

			var vols []audit.Volume
			if fromFile != "" {
				vols, err = source.FromFile(fromFile)
			} else {
				vols, err = source.FromAWS(context.Background(), region)
			}
			if err != nil {
				return err
			}

			rep := audit.Audit(vols, p, audit.Options{Match: mode})

			switch output {
			case "table", "":
				err = report.Table(cmd.OutOrStdout(), rep)
			case "json":
				err = report.JSON(cmd.OutOrStdout(), rep)
			case "sarif":
				err = report.SARIF(cmd.OutOrStdout(), rep, Version)
			default:
				return fmt.Errorf("unknown --output %q (want table, json, or sarif)", output)
			}
			if err != nil {
				return err
			}

			if emit != "" {
				if err := writePlanFile(emit, emitFmt, func(w io.Writer) error {
					return writePlan(w, rep, Version)
				}); err != nil {
					return err
				}
			}

			if cmd.Flags().Changed("fail-on") && rep.TotalSaving > failOn {
				fmt.Fprintf(os.Stderr,
					"unrealized monthly saving $%.2f exceeds --fail-on $%.2f\n",
					rep.TotalSaving, failOn)
				return codedError{code: 2, msg: "fail-on threshold exceeded"}
			}
			return nil
		},
	}

	c.Flags().StringVarP(&output, "output", "o", "table", "output format: table, json, sarif")
	c.Flags().StringVar(&region, "region", "", "AWS region (defaults to the ambient config)")
	c.Flags().StringVar(&fromFile, "from-file", "", "read volumes from a JSON snapshot instead of AWS")
	c.Flags().StringVar(&prices, "prices", "", "JSON file overriding the built-in price table")
	c.Flags().Float64Var(&failOn, "fail-on", 0, "exit non-zero when unrealized monthly saving exceeds this many USD")
	c.Flags().StringVar(&match, "match-performance", "gp2",
		"gp2 target throughput: gp2 (keep the gp2 volume's throughput) or baseline (125 MiB/s)")
	c.Flags().StringVar(&emit, "emit", "", "write a migration plan for the candidates to this file")
	c.Flags().StringVar(&emitFmt, "emit-format", "cli", "migration plan format: cli (aws ec2 modify-volume script) or terraform")
	return c
}

// writePlanFile writes a migration plan to path. Scripts are made executable.
func writePlanFile(path, format string, write func(io.Writer) error) error {
	mode := os.FileMode(0o644)
	if format != "terraform" {
		mode = 0o755
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("write plan %q: %w", path, err)
	}
	if err := write(f); err != nil {
		f.Close()
		return fmt.Errorf("write plan %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write plan %q: %w", path, err)
	}
	return nil
}
