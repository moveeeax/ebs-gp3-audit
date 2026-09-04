// Package audit contains the read-only cost model that decides whether an EBS
// volume is a gp3 migration candidate and how much it would save per month.
package audit

// Volume is a normalized view of an EBS volume, independent of the AWS SDK.
type Volume struct {
	ID         string `json:"id"`
	Type       string `json:"type"`       // gp2, gp3, io1, io2, st1, sc1, standard
	SizeGiB    int64  `json:"size_gib"`   // provisioned size
	IOPS       int64  `json:"iops"`       // provisioned IOPS (0 if not applicable)
	Throughput int64  `json:"throughput"` // provisioned MB/s (0 if not applicable)
	AZ         string `json:"az,omitempty"`
	Region     string `json:"region,omitempty"`
}

// Finding is the per-volume result of the audit.
type Finding struct {
	VolumeID       string  `json:"volume_id"`
	Type           string  `json:"type"`
	SizeGiB        int64   `json:"size_gib"`
	CurrentMonthly float64 `json:"current_monthly_usd"`
	Gp3Monthly     float64 `json:"gp3_monthly_usd"`
	MonthlySaving  float64 `json:"monthly_saving_usd"`
	Candidate      bool    `json:"candidate"`
	Compliant      bool    `json:"compliant"`
	Reason         string  `json:"reason"`
}

// Report aggregates the findings and fleet totals.
type Report struct {
	Findings       []Finding `json:"findings"`
	TotalCurrent   float64   `json:"total_current_monthly_usd"`
	TotalGp3       float64   `json:"total_gp3_monthly_usd"`
	TotalSaving    float64   `json:"total_monthly_saving_usd"`
	CandidateCount int       `json:"candidate_count"`
	VolumeCount    int       `json:"volume_count"`
}

func clamp(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// gp3Cost returns the monthly cost of a gp3 volume provisioned to hold the given
// size and to serve the requested IOPS/throughput, billing only above baseline.
func gp3Cost(size, iops, tput int64, p Prices) float64 {
	c := float64(size) * p.Gp3GB
	if iops > p.Gp3BaselineIOPS {
		c += float64(iops-p.Gp3BaselineIOPS) * p.Gp3IOPS
	}
	if tput > p.Gp3BaselineThroughput {
		c += float64(tput-p.Gp3BaselineThroughput) * p.Gp3Throughput
	}
	return c
}

// gp2BaselineIOPS is the sustained IOPS a gp2 volume delivers: 3 IOPS per GiB,
// floored at 100 and capped at 16000.
func gp2BaselineIOPS(size int64) int64 {
	return clamp(size*3, 100, 16000)
}

// assess computes the finding for a single volume.
func assess(v Volume, p Prices) Finding {
	f := Finding{VolumeID: v.ID, Type: v.Type, SizeGiB: v.SizeGiB}
	switch v.Type {
	case "gp3":
		cost := gp3Cost(v.SizeGiB, v.IOPS, v.Throughput, p)
		f.CurrentMonthly = round2(cost)
		f.Gp3Monthly = round2(cost)
		f.Compliant = true
		f.Reason = "already on gp3"
		return f

	case "gp2":
		current := float64(v.SizeGiB) * p.Gp2GB
		// Preserve the gp2 sustained IOPS; gp2 throughput stays within gp3 baseline.
		targetIOPS := clamp(gp2BaselineIOPS(v.SizeGiB), 0, p.Gp3MaxIOPS)
		gp3 := gp3Cost(v.SizeGiB, targetIOPS, p.Gp3BaselineThroughput, p)
		f.CurrentMonthly = round2(current)
		f.Gp3Monthly = round2(gp3)
		f.MonthlySaving = round2(current - gp3)
		if f.MonthlySaving > 0 {
			f.Candidate = true
			f.Reason = "gp2 volume; migrate to gp3"
		} else {
			f.Reason = "gp2 volume with no positive saving at current prices"
		}
		return f

	case "io1", "io2":
		gbPrice, iopsPrice := p.Io1GB, p.Io1IOPS
		if v.Type == "io2" {
			gbPrice, iopsPrice = p.Io2GB, p.Io2IOPS
		}
		current := float64(v.SizeGiB)*gbPrice + float64(v.IOPS)*iopsPrice
		f.CurrentMonthly = round2(current)
		if v.IOPS > p.Gp3MaxIOPS {
			f.Reason = "provisioned IOPS exceed gp3 maximum; not a gp3 target"
			f.Gp3Monthly = 0
			return f
		}
		tput := v.Throughput
		if tput == 0 {
			tput = p.Gp3BaselineThroughput
		}
		gp3 := gp3Cost(v.SizeGiB, v.IOPS, tput, p)
		f.Gp3Monthly = round2(gp3)
		f.MonthlySaving = round2(current - gp3)
		if f.MonthlySaving > 0 {
			f.Candidate = true
			f.Reason = "over-provisioned " + v.Type + "; migrate to gp3"
		} else {
			f.Reason = v.Type + " volume with no positive saving at current prices"
		}
		return f

	default:
		f.Reason = "volume type " + v.Type + " is not a gp3 migration target"
		return f
	}
}

// Audit runs the cost model over every volume and aggregates the totals.
func Audit(vols []Volume, p Prices) Report {
	r := Report{VolumeCount: len(vols)}
	for _, v := range vols {
		f := assess(v, p)
		r.Findings = append(r.Findings, f)
		r.TotalCurrent += f.CurrentMonthly
		r.TotalGp3 += f.Gp3Monthly
		if f.Candidate {
			r.CandidateCount++
			r.TotalSaving += f.MonthlySaving
		}
	}
	r.TotalCurrent = round2(r.TotalCurrent)
	r.TotalGp3 = round2(r.TotalGp3)
	r.TotalSaving = round2(r.TotalSaving)
	return r
}
