// Package report renders an audit.Report as a human table, JSON, or SARIF 2.1.0.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/moveeeax/ebs-gp3-audit/internal/audit"
)

// JSON writes the report as indented JSON.
func JSON(w io.Writer, r audit.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Table writes an aligned, human-readable summary.
func Table(w io.Writer, r audit.Report) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "VOLUME\tTYPE\tSIZE(GiB)\tNOW($/mo)\tGP3($/mo)\tSAVING($/mo)\tGP3 SIZE(GiB)\tGP3 IOPS\tGP3 TPUT(MiB/s)\tSTATUS")
	findings := append([]audit.Finding(nil), r.Findings...)
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].MonthlySaving > findings[j].MonthlySaving
	})
	for _, f := range findings {
		status := "skip"
		switch {
		case f.Candidate:
			status = "MIGRATE"
		case f.Compliant:
			status = "ok-gp3"
		}
		gSize, gIOPS, gTput := "-", "-", "-"
		if f.Gp3IOPS > 0 {
			gSize = fmt.Sprint(f.Gp3SizeGiB)
			gIOPS = fmt.Sprint(f.Gp3IOPS)
			gTput = fmt.Sprint(f.Gp3Throughput)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%.2f\t%.2f\t%.2f\t%s\t%s\t%s\t%s\n",
			f.VolumeID, f.Type, f.SizeGiB, f.CurrentMonthly, f.Gp3Monthly, f.MonthlySaving,
			gSize, gIOPS, gTput, status)
	}
	fmt.Fprintf(tw, "\nCandidates: %d/%d\tUnrealized saving: $%.2f/mo\n",
		r.CandidateCount, r.VolumeCount, r.TotalSaving)
	return tw.Flush()
}

// --- SARIF 2.1.0 ---

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Help struct {
		Text string `json:"text"`
	} `json:"help"`
}

type sarifResult struct {
	RuleID  string `json:"ruleId"`
	Level   string `json:"level"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Locations  []sarifLocation `json:"locations"`
	Properties map[string]any  `json:"properties"`
}

type sarifLocation struct {
	LogicalLocations []struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	} `json:"logicalLocations"`
}

const ruleID = "gp3-migration-candidate"

// SARIF writes the candidate findings as SARIF so the audit can gate CI and
// surface in code-scanning dashboards.
func SARIF(w io.Writer, r audit.Report, version string) error {
	rule := sarifRule{ID: ruleID, Name: "Gp3MigrationCandidate"}
	rule.Help.Text = "Volume is on a legacy EBS type and would cost less on gp3."

	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "ebs-gp3-audit",
				InformationURI: "https://github.com/moveeeax/ebs-gp3-audit",
				Version:        version,
				Rules:          []sarifRule{rule},
			}},
			Results: []sarifResult{},
		}},
	}

	for _, f := range r.Findings {
		if !f.Candidate {
			continue
		}
		res := sarifResult{RuleID: ruleID, Level: "warning"}
		res.Message.Text = fmt.Sprintf("%s (%s, %dGiB): migrate to gp3 to save $%.2f/mo",
			f.VolumeID, f.Type, f.SizeGiB, f.MonthlySaving)
		loc := sarifLocation{}
		loc.LogicalLocations = append(loc.LogicalLocations, struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		}{Name: f.VolumeID, Kind: "resource"})
		res.Locations = []sarifLocation{loc}
		res.Properties = map[string]any{
			"currentMonthlyUsd": f.CurrentMonthly,
			"gp3MonthlyUsd":     f.Gp3Monthly,
			"monthlySavingUsd":  f.MonthlySaving,
			"gp3SizeGib":        f.Gp3SizeGiB,
			"gp3Iops":           f.Gp3IOPS,
			"gp3Throughput":     f.Gp3Throughput,
		}
		log.Runs[0].Results = append(log.Runs[0].Results, res)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
