package cmd

import (
	"context"
	"fmt"
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
	)

	c := &cobra.Command{
		Use:   "audit",
		Short: "Audit EBS volumes and estimate gp3 migration savings",
		Long: "Audit enumerates EBS volumes (read-only DescribeVolumes, or a JSON\n" +
			"snapshot with --from-file), flags gp2/io1/io2 volumes that would cost\n" +
			"less on gp3, and reports per-volume and fleet-wide monthly savings.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := audit.DefaultPrices()
			if prices != "" {
				loaded, err := audit.LoadPrices(prices)
				if err != nil {
					return err
				}
				p = loaded
			}

			var (
				vols []audit.Volume
				err  error
			)
			if fromFile != "" {
				vols, err = source.FromFile(fromFile)
			} else {
				vols, err = source.FromAWS(context.Background(), region)
			}
			if err != nil {
				return err
			}

			rep := audit.Audit(vols, p)

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
	return c
}
