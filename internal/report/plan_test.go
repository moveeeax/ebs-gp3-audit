package report

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moveeeax/ebs-gp3-audit/internal/audit"
)

func planReport() audit.Report {
	return audit.Audit([]audit.Volume{
		{ID: "vol-gp2", Type: "gp2", SizeGiB: 500, Region: "eu-west-1"},
		{ID: "vol-io2", Type: "io2", SizeGiB: 100, IOPS: 80000},
		{ID: "vol-gp3", Type: "gp3", SizeGiB: 100, IOPS: 3000},
		{ID: "vol-st1", Type: "st1", SizeGiB: 500},
	}, audit.DefaultPrices(), audit.Options{})
}

func TestPlanScriptCommands(t *testing.T) {
	var buf bytes.Buffer
	if err := PlanScript(&buf, planReport(), "v0.0.0"); err != nil {
		t.Fatalf("PlanScript: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"up to 4 times in a rolling 24-hour period",
		"modify --region 'eu-west-1' --volume-id 'vol-gp2' --volume-type gp3 --iops 3000 --throughput 250\n",
		"modify --volume-id 'vol-io2' --volume-type gp3 --size 160 --iops 80000 --throughput 125\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("script missing %q:\n%s", want, out)
		}
	}
	for _, skip := range []string{"vol-gp3", "vol-st1"} {
		if strings.Contains(out, skip) {
			t.Fatalf("script must only cover candidates, found %s:\n%s", skip, out)
		}
	}
}

func TestPlanScriptQuotesIDs(t *testing.T) {
	r := audit.Audit([]audit.Volume{{ID: "vol-'x", Type: "gp2", SizeGiB: 500}},
		audit.DefaultPrices(), audit.Options{})
	var buf bytes.Buffer
	if err := PlanScript(&buf, r, "v0.0.0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `--volume-id 'vol-'\''x'`) {
		t.Fatalf("volume ID not shell-quoted:\n%s", buf.String())
	}
}

// TestPlanScriptDryRunGuard runs the script against a fake aws binary: by
// default every call carries --dry-run and the DryRunOperation error does not
// stop the script; APPLY=1 drops --dry-run.
func TestPlanScriptDryRunGuard(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	fake := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"case \"$*\" in *--dry-run*) echo 'An error occurred (DryRunOperation) when calling the ModifyVolume operation: Request would have succeeded, but DryRun flag is set.' >&2; exit 254 ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "aws"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "plan.sh")
	var buf bytes.Buffer
	if err := PlanScript(&buf, planReport(), "v0.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, buf.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}

	runScript := func(env ...string) []string {
		t.Helper()
		os.Remove(log)
		cmd := exec.Command(bash, script)
		cmd.Env = append(os.Environ(), append([]string{"PATH=" + dir + ":" + os.Getenv("PATH")}, env...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}

	calls := runScript()
	if len(calls) != 2 {
		t.Fatalf("want 2 calls, got %d: %q", len(calls), calls)
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "ec2 modify-volume --dry-run ") {
			t.Fatalf("default run must use --dry-run: %q", c)
		}
	}

	calls = runScript("APPLY=1")
	if len(calls) != 2 {
		t.Fatalf("want 2 calls, got %d: %q", len(calls), calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "--dry-run") {
			t.Fatalf("APPLY=1 must not use --dry-run: %q", c)
		}
	}
}

func TestPlanScriptNoCandidates(t *testing.T) {
	r := audit.Audit([]audit.Volume{{ID: "vol-gp3", Type: "gp3", SizeGiB: 100}},
		audit.DefaultPrices(), audit.Options{})
	var buf bytes.Buffer
	if err := PlanScript(&buf, r, "v0.0.0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No gp3 migration candidates") || strings.Contains(buf.String(), "\nmodify ") {
		t.Fatalf("empty plan should only carry a comment:\n%s", buf.String())
	}
}

func TestPlanTerraform(t *testing.T) {
	var buf bytes.Buffer
	if err := PlanTerraform(&buf, planReport(), "v0.0.0"); err != nil {
		t.Fatalf("PlanTerraform: %v", err)
	}
	out := buf.String()
	want := `    "vol-io2" = {
      volume_type = "gp3"
      volume_size = 160
      iops        = 80000
      throughput  = 125
    }
`
	if !strings.Contains(out, want) || !strings.Contains(out, `"vol-gp2" = {`) {
		t.Fatalf("terraform plan missing entries:\n%s", out)
	}
	if strings.Contains(out, "vol-gp3") || strings.Contains(out, "vol-st1") {
		t.Fatalf("terraform plan must only cover candidates:\n%s", out)
	}
}

func TestHCLQuoteEscapesTemplates(t *testing.T) {
	if got := hclQuote("a${b}%{c}\""); got != `"a$${b}%%{c}\""` {
		t.Fatalf("hclQuote = %s", got)
	}
}
