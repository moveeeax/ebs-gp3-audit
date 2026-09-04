package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moveeeax/ebs-gp3-audit/internal/audit"
)

func sampleReport() audit.Report {
	return audit.Audit([]audit.Volume{
		{ID: "vol-gp2", Type: "gp2", SizeGiB: 500},
		{ID: "vol-gp3", Type: "gp3", SizeGiB: 100, IOPS: 3000},
	}, audit.DefaultPrices())
}

func TestJSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleReport()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got audit.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("re-parse JSON: %v", err)
	}
	if got.CandidateCount != 1 || got.VolumeCount != 2 {
		t.Fatalf("json report lost data: %+v", got)
	}
}

func TestTableHasHeaderAndTotals(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, sampleReport()); err != nil {
		t.Fatalf("Table: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"VOLUME", "SAVING($/mo)", "MIGRATE", "Unrealized saving"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
}

func TestSARIFResultsMatchCandidates(t *testing.T) {
	var buf bytes.Buffer
	if err := SARIF(&buf, sampleReport(), "v0.1.0"); err != nil {
		t.Fatalf("SARIF: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("SARIF is not valid JSON: %v", err)
	}
	if doc["version"] != "2.1.0" {
		t.Fatalf("unexpected sarif version: %v", doc["version"])
	}
	runs := doc["runs"].([]any)
	results := runs[0].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("want 1 sarif result (one candidate), got %d", len(results))
	}
	res := results[0].(map[string]any)
	if res["level"] != "warning" {
		t.Fatalf("candidate level should be warning, got %v", res["level"])
	}
	if res["ruleId"] != ruleID {
		t.Fatalf("unexpected ruleId %v", res["ruleId"])
	}
}
