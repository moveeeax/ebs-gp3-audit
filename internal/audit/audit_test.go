package audit

import "testing"

func TestGp2CandidateHasPositiveSaving(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-gp2", Type: "gp2", SizeGiB: 500}}, p)
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
	r := Audit([]Volume{{ID: "vol-gp3", Type: "gp3", SizeGiB: 200, IOPS: 3000, Throughput: 125}}, p)
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
	r := Audit([]Volume{{ID: "vol-big", Type: "gp2", SizeGiB: 4000}}, p)
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
	r := Audit([]Volume{{ID: "vol-io1", Type: "io1", SizeGiB: 100, IOPS: 2000}}, p)
	f := r.Findings[0]
	// current = 100*0.125 + 2000*0.065 = 12.5 + 130 = 142.5; gp3 = 100*0.08 = 8.
	if f.CurrentMonthly != 142.5 || f.Gp3Monthly != 8 {
		t.Fatalf("io1 costs wrong: %+v", f)
	}
	if !f.Candidate || f.MonthlySaving <= 0 {
		t.Fatalf("io1 within gp3 bounds should be a candidate: %+v", f)
	}
}

func TestIo1AboveGp3MaxIsNotCandidate(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-io1-big", Type: "io1", SizeGiB: 100, IOPS: 40000}}, p)
	f := r.Findings[0]
	if f.Candidate {
		t.Fatalf("io1 above gp3 max IOPS must not be a candidate")
	}
	if f.Reason == "" {
		t.Fatalf("expected an explanatory reason")
	}
}

func TestUnknownTypeIsIgnored(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{{ID: "vol-st1", Type: "st1", SizeGiB: 500}}, p)
	f := r.Findings[0]
	if f.Candidate || f.Compliant {
		t.Fatalf("st1 should be neither candidate nor compliant: %+v", f)
	}
}

func TestFleetTotals(t *testing.T) {
	p := DefaultPrices()
	r := Audit([]Volume{
		{ID: "a", Type: "gp2", SizeGiB: 500},             // saving 10
		{ID: "b", Type: "gp3", SizeGiB: 100, IOPS: 3000}, // saving 0
		{ID: "c", Type: "io1", SizeGiB: 100, IOPS: 2000}, // big saving
		{ID: "d", Type: "st1", SizeGiB: 1000},            // ignored
	}, p)
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

func TestLoadPricesOverride(t *testing.T) {
	p := DefaultPrices()
	if p.Gp2GB != 0.10 {
		t.Fatalf("default gp2 price changed unexpectedly")
	}
}
