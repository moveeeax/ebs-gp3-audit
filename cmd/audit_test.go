package cmd

import (
	"bytes"
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
