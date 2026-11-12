package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGp2CandidateHasPositiveSaving(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-gp2", Type: "gp2", SizeGiB: 500}}, p, Options{Match: MatchBaseline})
	if len(r.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(r.Findings))
	}
	f := r.Findings[0]
	if !f.Candidate {
		t.Fatalf("gp2 volume should be a migration candidate: %+v", f)
	}
	if f.MonthlySaving <= 0 {
		t.Fatalf("candidate saving must be positive, got %v", f.MonthlySaving)
	}
	// 500 GiB gp2 = $50; gp3 = 500*0.08 = $40 (baseline IOPS 1500 < 3000).
	if f.CurrentMonthly != 50 || f.Gp3Monthly != 40 || f.MonthlySaving != 10 {
		t.Fatalf("unexpected costs: %+v", f)
	}
}

func TestGp3AlreadyCompliantZeroSaving(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-gp3", Type: "gp3", SizeGiB: 200, IOPS: 3000, Throughput: 125}}, p, Options{})
	f := r.Findings[0]
	if f.Candidate {
		t.Fatalf("gp3 volume must not be a candidate")
	}
	if !f.Compliant {
		t.Fatalf("gp3 volume must be reported compliant")
	}
	if f.MonthlySaving != 0 {
		t.Fatalf("compliant volume saving must be 0, got %v", f.MonthlySaving)
	}
}

func TestLargeGp2PaysForExtraIOPS(t *testing.T) {
	p := DefaultPrices()
	// 4000 GiB gp2: baseline IOPS 12000; gp3 must buy 9000 IOPS over baseline.
	r := Audit([]Volume{{ID: "vol-big", Type: "gp2", SizeGiB: 4000}}, p, Options{Match: MatchBaseline})
	f := r.Findings[0]
	// current = 4000*0.10 = 400; gp3 = 4000*0.08 + 9000*0.005 = 320 + 45 = 365.
	if f.CurrentMonthly != 400 || f.Gp3Monthly != 365 || f.MonthlySaving != 35 {
		t.Fatalf("extra-IOPS pricing wrong: %+v", f)
	}
	if !f.Candidate {
		t.Fatalf("large gp2 should still be a candidate")
	}
}

func TestIo1WithinBoundsIsCandidate(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-io1", Type: "io1", SizeGiB: 100, IOPS: 2000}}, p, Options{})
	f := r.Findings[0]
	// current = 100*0.125 + 2000*0.065 = 12.5 + 130 = 142.5; gp3 = 100*0.08 = 8.
	if f.CurrentMonthly != 142.5 || f.Gp3Monthly != 8 {
		t.Fatalf("io1 costs wrong: %+v", f)
	}
	if !f.Candidate || f.MonthlySaving <= 0 {
		t.Fatalf("io1 within gp3 bounds should be a candidate: %+v", f)
	}
}

func TestIo2AboveGp3MaxIsNotCandidate(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-io2-big", Type: "io2", SizeGiB: 200, IOPS: 100000}}, p, Options{})
	f := r.Findings[0]
	if f.Candidate {
		t.Fatalf("io2 above gp3 max IOPS must not be a candidate")
	}
	if f.Gp3IOPS != 0 || f.Gp3Monthly != 0 {
		t.Fatalf("non-target must not carry a gp3 config: %+v", f)
	}
	if f.Reason == "" {
		t.Fatalf("expected an explanatory reason")
	}
}

func TestUnknownTypeIsIgnored(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-st1", Type: "st1", SizeGiB: 500}}, p, Options{})
	f := r.Findings[0]
	if f.Candidate || f.Compliant {
		t.Fatalf("st1 should be neither candidate nor compliant: %+v", f)
	}
}

func TestFleetTotals(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{
		{ID: "a", Type: "gp2", SizeGiB: 500},             // saving 5 (250 MiB/s kept)
		{ID: "b", Type: "gp3", SizeGiB: 100, IOPS: 3000}, // saving 0
		{ID: "c", Type: "io1", SizeGiB: 100, IOPS: 2000}, // big saving
		{ID: "d", Type: "st1", SizeGiB: 1000},            // ignored
	}, p, Options{})
	if r.VolumeCount != 4 {
		t.Fatalf("volume count wrong: %d", r.VolumeCount)
	}
	if r.CandidateCount != 2 {
		t.Fatalf("candidate count wrong: %d", r.CandidateCount)
	}
	if r.TotalSaving <= 10 {
		t.Fatalf("total saving should exceed the gp2-only saving, got %v", r.TotalSaving)
	}
}

func TestDefaultGp3Limits(t *testing.T) {
	p := DefaultPrices()
	if p.Gp2GB != 0.10 {
		t.Fatalf("default gp2 price changed unexpectedly")
	}
	if p.Gp3MaxIOPS != 80000 || p.Gp3MaxThroughput != 2000 || p.Gp3MaxSizeGiB != 65536 ||
		p.Gp3IOPSPerGiB != 500 || p.Gp3ThroughputPerIOPS != 0.25 {
		t.Fatalf("gp3 limits do not match the September 2025 values: %+v", p)
	}
}

func TestLoadPricesOverridesLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prices.json")
	body := `{"gp2_gb": 0.116, "gp3_max_iops": 16000, "gp3_max_throughput": 1000, "gp3_max_size_gib": 16384, "gp3_iops_per_gib": 0}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPrices(path)
	if err != nil {
		t.Fatalf("LoadPrices: %v", err)
	}
	if p.Gp2GB != 0.116 || p.Gp3MaxIOPS != 16000 || p.Gp3MaxThroughput != 1000 || p.Gp3MaxSizeGiB != 16384 {
		t.Fatalf("overrides not applied: %+v", p)
	}
	// Fields not in the file, or set to zero, keep the defaults.
	if p.Gp3GB != 0.08 || p.Gp3IOPSPerGiB != 500 || p.Gp3ThroughputPerIOPS != 0.25 {
		t.Fatalf("defaults lost: %+v", p)
	}
}

func TestGp2MatchesGp2Throughput(t *testing.T) {
	p := DefaultPrices()
	cases := []struct {
		size        int64
		iops, tput  int64
		gp3, saving float64
	}{
		// 100 GiB: 128 MiB/s; gp3 = 8 + 3*0.04 = 8.12.
		{size: 100, iops: 3000, tput: 128, gp3: 8.12, saving: 1.88},
		// 500 GiB: 250 MiB/s; gp3 = 40 + 125*0.04 = 45.
		{size: 500, iops: 3000, tput: 250, gp3: 45, saving: 5},
		// 4000 GiB: 12000 IOPS, 250 MiB/s; gp3 = 320 + 9000*0.005 + 125*0.04 = 370.
		{size: 4000, iops: 12000, tput: 250, gp3: 370, saving: 30},
	}
	for _, tc := range cases {
		f := Audit([]Volume{{ID: "v", Type: "gp2", SizeGiB: tc.size}}, p, Options{}).Findings[0]
		if f.Gp3SizeGiB != tc.size || f.Gp3IOPS != tc.iops || f.Gp3Throughput != tc.tput {
			t.Errorf("%d GiB: target %d GiB/%d IOPS/%d MiB/s, want %d/%d/%d",
				tc.size, f.Gp3SizeGiB, f.Gp3IOPS, f.Gp3Throughput, tc.size, tc.iops, tc.tput)
		}
		if f.Gp3Monthly != tc.gp3 || f.MonthlySaving != tc.saving || !f.Candidate {
			t.Errorf("%d GiB: gp3 $%v saving $%v, want $%v/$%v: %+v",
				tc.size, f.Gp3Monthly, f.MonthlySaving, tc.gp3, tc.saving, f)
		}
	}
}

func TestGp2BaselineModeKeeps125(t *testing.T) {
	f := Audit([]Volume{{ID: "v", Type: "gp2", SizeGiB: 500}}, DefaultPrices(),
		Options{Match: MatchBaseline}).Findings[0]
	if f.Gp3Throughput != 125 || f.Gp3IOPS != 3000 {
		t.Fatalf("baseline mode should target 3000 IOPS / 125 MiB/s: %+v", f)
	}
}

func TestParseMatch(t *testing.T) {
	for in, want := range map[string]Match{"": MatchGp2, "gp2": MatchGp2, "baseline": MatchBaseline} {
		got, err := ParseMatch(in)
		if err != nil || got != want {
			t.Errorf("ParseMatch(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMatch("io1"); err == nil {
		t.Error("ParseMatch should reject unknown modes")
	}
}

func TestIo1Between16kAnd80kIsCandidate(t *testing.T) {
	p := DefaultPrices()
	f := Audit([]Volume{{ID: "v", Type: "io1", SizeGiB: 1000, IOPS: 40000}}, p, Options{}).Findings[0]
	// current = 1000*0.125 + 40000*0.065 = 2725; gp3 = 80 + 37000*0.005 = 265.
	if !f.Candidate || f.CurrentMonthly != 2725 || f.Gp3Monthly != 265 || f.Gp3IOPS != 40000 {
		t.Fatalf("io1 at 40k IOPS should migrate under the 80k gp3 limit: %+v", f)
	}

	// A price file pinned to the pre-2025 limit keeps the old verdict.
	p.Gp3MaxIOPS = 16000
	f = Audit([]Volume{{ID: "v", Type: "io1", SizeGiB: 1000, IOPS: 40000}}, p, Options{}).Findings[0]
	if f.Candidate || !strings.Contains(f.Reason, "exceed gp3 maximum 16000") {
		t.Fatalf("io1 above an overridden 16k limit must not be a candidate: %+v", f)
	}
}

func TestHighIOPSNeedsSizeBump(t *testing.T) {
	p := DefaultPrices()
	f := Audit([]Volume{{ID: "v", Type: "io2", SizeGiB: 100, IOPS: 80000}}, p, Options{}).Findings[0]
	// 80000 IOPS at 500 IOPS/GiB needs 160 GiB.
	// current = 100*0.125 + 80000*0.065 = 5212.5; gp3 = 160*0.08 + 77000*0.005 = 397.8.
	if f.Gp3SizeGiB != 160 || f.Gp3IOPS != 80000 {
		t.Fatalf("expected 160 GiB / 80000 IOPS target: %+v", f)
	}
	if f.Gp3Monthly != 397.8 || f.MonthlySaving != 4814.7 || !f.Candidate {
		t.Fatalf("size-bumped costs wrong: %+v", f)
	}
	if !strings.Contains(f.Reason, "needs size bump to 160 GiB for 80000 IOPS") {
		t.Fatalf("reason should explain the size bump: %q", f.Reason)
	}
}

func TestHighThroughputNeedsExtraIOPS(t *testing.T) {
	p := DefaultPrices()
	f := Audit([]Volume{{ID: "v", Type: "io1", SizeGiB: 100, IOPS: 3000, Throughput: 2000}}, p, Options{}).Findings[0]
	// 2000 MiB/s at 0.25 MiB/s per IOPS needs 8000 IOPS, which needs 16 GiB.
	if f.Gp3IOPS != 8000 || f.Gp3Throughput != 2000 || f.Gp3SizeGiB != 100 {
		t.Fatalf("expected 8000 IOPS / 2000 MiB/s target: %+v", f)
	}
	if !strings.Contains(f.Reason, "needs 8000 IOPS for 2000 MiB/s") {
		t.Fatalf("reason should explain the IOPS bump: %q", f.Reason)
	}

	f = Audit([]Volume{{ID: "v", Type: "io1", SizeGiB: 100, IOPS: 3000, Throughput: 2500}}, p, Options{}).Findings[0]
	if f.Candidate || !strings.Contains(f.Reason, "throughput 2500 MiB/s exceeds gp3 maximum") {
		t.Fatalf("throughput above gp3 max must not be a candidate: %+v", f)
	}
}

func TestSizeAboveGp3MaxIsNotCandidate(t *testing.T) {
	p := DefaultPrices()
	p.Gp3MaxSizeGiB = 16384 // e.g. Outposts
	f := Audit([]Volume{{ID: "v", Type: "io1", SizeGiB: 20000, IOPS: 5000}}, p, Options{}).Findings[0]
	if f.Candidate || !strings.Contains(f.Reason, "size 20000 GiB exceeds gp3 maximum 16384 GiB") {
		t.Fatalf("volume above gp3 max size must not be a candidate: %+v", f)
	}
}
