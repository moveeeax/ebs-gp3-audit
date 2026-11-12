package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(args ...string) (string, error) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestAuditTableFromFixture(t *testing.T) {
	out, err := run("audit", "--from-file", "../examples/volumes.json")
	if err != nil {
		t.Fatalf("audit table: %v", err)
	}
	if !strings.Contains(out, "MIGRATE") || !strings.Contains(out, "Unrealized saving") {
		t.Fatalf("table output missing expected content:\n%s", out)
	}
}

func TestAuditJSONFromFixture(t *testing.T) {
	out, err := run("audit", "--from-file", "../examples/volumes.json", "--output", "json")
	if err != nil {
		t.Fatalf("audit json: %v", err)
	}
	if !strings.Contains(out, "\"candidate_count\"") {
		t.Fatalf("json output missing candidate_count:\n%s", out)
	}
}

func TestFailOnGatesPipeline(t *testing.T) {
	// The fixture has hundreds of dollars of unrealized saving, so a low
	// threshold must trip the gate and return the coded exit.
	_, err := run("audit", "--from-file", "../examples/volumes.json", "--fail-on", "1")
	if err == nil {
		t.Fatal("expected --fail-on to return an error")
	}
	ce, ok := err.(codedError)
	if !ok || ce.code != 2 {
		t.Fatalf("expected codedError{code:2}, got %#v", err)
	}
}

func TestFailOnPassesWhenUnderThreshold(t *testing.T) {
	out, err := run("audit", "--from-file", "../examples/volumes.json", "--fail-on", "1000000")
	if err != nil {
		t.Fatalf("high threshold should not fail: %v (%s)", err, out)
	}
}

func TestUnknownOutputRejected(t *testing.T) {
	if _, err := run("audit", "--from-file", "../examples/volumes.json", "--output", "yaml"); err == nil {
		t.Fatal("expected unknown --output to error")
	}
}

func TestMatchPerformanceBaselineLowersGp3Cost(t *testing.T) {
	gp2, err := run("audit", "--from-file", "../examples/volumes.json")
	if err != nil {
		t.Fatal(err)
	}
	base, err := run("audit", "--from-file", "../examples/volumes.json", "--match-performance", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	// 500 GiB gp2: $45 keeping 250 MiB/s, $40 at the 125 MiB/s baseline.
	// 4000 IOPS io1: $36 keeping 500 MiB/s, $21 at the baseline.
	if !strings.Contains(gp2, "Unrealized saving: $274.00/mo") || !strings.Contains(base, "Unrealized saving: $299.00/mo") {
		t.Fatalf("unexpected totals:\ngp2:\n%s\nbaseline:\n%s", gp2, base)
	}
}

func TestUnknownMatchPerformanceRejected(t *testing.T) {
	if _, err := run("audit", "--from-file", "../examples/volumes.json", "--match-performance", "io1"); err == nil {
		t.Fatal("expected unknown --match-performance to error")
	}
}

func TestEmitWritesScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.sh")
	if _, err := run("audit", "--from-file", "../examples/volumes.json", "--emit", path); err != nil {
		t.Fatalf("audit --emit: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "modify --volume-id 'vol-0e4f5a6b' --volume-type gp3 --iops 6000 --throughput 250") {
		t.Fatalf("script missing the 2000 GiB gp2 volume:\n%s", b)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("script should be executable, mode %v", st.Mode())
	}
}

func TestEmitTerraform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gp3.tf")
	if _, err := run("audit", "--from-file", "../examples/volumes.json", "--emit", path, "--emit-format", "terraform"); err != nil {
		t.Fatalf("audit --emit-format terraform: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"vol-0c7d8e9f" = {`) || !strings.Contains(string(b), "ebs_gp3_migration") {
		t.Fatalf("terraform plan missing content:\n%s", b)
	}
}

func TestEmitWrittenEvenWhenFailOnTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.sh")
	_, err := run("audit", "--from-file", "../examples/volumes.json", "--emit", path, "--fail-on", "1")
	if ce, ok := err.(codedError); !ok || ce.code != 2 {
		t.Fatalf("expected codedError{code:2}, got %#v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plan should be written before the gate: %v", err)
	}
}

func TestEmitFormatValidation(t *testing.T) {
	if _, err := run("audit", "--from-file", "../examples/volumes.json", "--emit", filepath.Join(t.TempDir(), "x"), "--emit-format", "pulumi"); err == nil {
		t.Fatal("expected unknown --emit-format to error")
	}
	if _, err := run("audit", "--from-file", "../examples/volumes.json", "--emit-format", "terraform"); err == nil {
		t.Fatal("expected --emit-format without --emit to error")
	}
}
