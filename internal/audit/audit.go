// Package audit contains the read-only cost model that decides whether an EBS
// volume is a gp3 migration candidate and how much it would save per month.
package audit

import (
	"fmt"
	"math"
)

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

// Finding is the per-volume result of the audit. The Gp3* fields describe the
// gp3 configuration the cost model priced; they are zero when the volume has
// no gp3 equivalent.
type Finding struct {
	VolumeID       string  `json:"volume_id"`
	Type           string  `json:"type"`
	SizeGiB        int64   `json:"size_gib"`
	Region         string  `json:"region,omitempty"`
	CurrentMonthly float64 `json:"current_monthly_usd"`
	Gp3Monthly     float64 `json:"gp3_monthly_usd"`
	MonthlySaving  float64 `json:"monthly_saving_usd"`
	Gp3SizeGiB     int64   `json:"gp3_size_gib"`
	Gp3IOPS        int64   `json:"gp3_iops"`
	Gp3Throughput  int64   `json:"gp3_throughput"`
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

// Match selects which performance a gp2 volume's gp3 target must preserve.
type Match string

const (
	// MatchGp2 provisions gp3 throughput to the gp2 volume's size-based
	// throughput, so the migration does not lower it. This is the default.
	MatchGp2 Match = "gp2"
	// MatchBaseline prices gp2 volumes against gp3 at the free 125 MiB/s
	// baseline throughput.
	MatchBaseline Match = "baseline"
)

// ParseMatch validates a --match-performance value.
func ParseMatch(s string) (Match, error) {
	switch Match(s) {
	case MatchGp2, MatchBaseline:
		return Match(s), nil
	case "":
		return MatchGp2, nil
	}
	return "", fmt.Errorf("unknown match mode %q (want gp2 or baseline)", s)
}

// Options tunes the cost model.
type Options struct {
	Match Match // zero value means MatchGp2
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

// gp3Config is a gp3 volume configuration: size, IOPS and throughput.
type gp3Config struct {
	SizeGiB    int64
	IOPS       int64
	Throughput int64
}

// gp3Target returns the smallest gp3 configuration that keeps at least the
// given size and serves the requested IOPS and throughput. It applies the gp3
// ratio rules: throughput may not exceed Gp3ThroughputPerIOPS per provisioned
// IOPS (so high throughput can force extra IOPS), and IOPS may not exceed
// Gp3IOPSPerGiB per GiB (so high IOPS can force a larger volume). notes lists
// the adjustments made; ok is false when no gp3 volume can serve the request,
// with the reason in notes[0].
func gp3Target(size, iops, tput int64, p Prices) (c gp3Config, notes []string, ok bool) {
	c = gp3Config{
		SizeGiB:    size,
		IOPS:       max(iops, p.Gp3BaselineIOPS),
		Throughput: max(tput, p.Gp3BaselineThroughput),
	}
	if c.Throughput > p.Gp3MaxThroughput {
		return gp3Config{}, []string{fmt.Sprintf(
			"throughput %d MiB/s exceeds gp3 maximum %d MiB/s; not a gp3 target",
			c.Throughput, p.Gp3MaxThroughput)}, false
	}
	if p.Gp3ThroughputPerIOPS > 0 {
		need := int64(math.Ceil(float64(c.Throughput) / p.Gp3ThroughputPerIOPS))
		if need > c.IOPS {
			c.IOPS = need
			notes = append(notes, fmt.Sprintf("needs %d IOPS for %d MiB/s", c.IOPS, c.Throughput))
		}
	}
	if c.IOPS > p.Gp3MaxIOPS {
		return gp3Config{}, []string{fmt.Sprintf(
			"IOPS %d exceed gp3 maximum %d; not a gp3 target", c.IOPS, p.Gp3MaxIOPS)}, false
	}
	if p.Gp3IOPSPerGiB > 0 {
		need := (c.IOPS + p.Gp3IOPSPerGiB - 1) / p.Gp3IOPSPerGiB
		if need > c.SizeGiB {
			c.SizeGiB = need
			notes = append(notes, fmt.Sprintf("needs size bump to %d GiB for %d IOPS", c.SizeGiB, c.IOPS))
		}
	}
	if p.Gp3MaxSizeGiB > 0 && c.SizeGiB > p.Gp3MaxSizeGiB {
		return gp3Config{}, []string{fmt.Sprintf(
			"size %d GiB exceeds gp3 maximum %d GiB; not a gp3 target", c.SizeGiB, p.Gp3MaxSizeGiB)}, false
	}
	return c, notes, true
}

// gp2BaselineIOPS is the sustained IOPS a gp2 volume delivers: 3 IOPS per GiB,
// floored at 100 and capped at 16000.
func gp2BaselineIOPS(size int64) int64 {
	return clamp(size*3, 100, 16000)
}

// gp2Throughput is the throughput a gp2 volume delivers per the AWS gp2 table:
// 128 MiB/s up to 170 GiB, 250 MiB/s above that (volumes between 170 and
// 334 GiB reach 250 MiB/s while bursting, larger ones sustain it).
func gp2Throughput(size int64) int64 {
	if size <= 170 {
		return 128
	}
	return 250
}

// withNotes appends the target adjustments to a reason.
func withNotes(reason string, notes []string) string {
	for _, n := range notes {
		reason += "; " + n
	}
	return reason
}

// assess computes the finding for a single volume.
func assess(v Volume, p Prices, o Options) Finding {
	f := Finding{VolumeID: v.ID, Type: v.Type, SizeGiB: v.SizeGiB, Region: v.Region}
	switch v.Type {
	case "gp3":
		cost := gp3Cost(v.SizeGiB, v.IOPS, v.Throughput, p)
		f.CurrentMonthly = round2(cost)
		f.Gp3Monthly = round2(cost)
		f.Gp3SizeGiB = v.SizeGiB
		f.Gp3IOPS = max(v.IOPS, p.Gp3BaselineIOPS)
		f.Gp3Throughput = max(v.Throughput, p.Gp3BaselineThroughput)
		f.Compliant = true
		f.Reason = "already on gp3"
		return f

	case "gp2":
		current := float64(v.SizeGiB) * p.Gp2GB
		f.CurrentMonthly = round2(current)
		// Preserve the gp2 sustained IOPS; burst up to 3000 IOPS is covered by
		// the gp3 baseline. Throughput follows the --match-performance mode.
		tput := p.Gp3BaselineThroughput
		if o.Match != MatchBaseline {
			tput = gp2Throughput(v.SizeGiB)
		}
		c, notes, ok := gp3Target(v.SizeGiB, gp2BaselineIOPS(v.SizeGiB), tput, p)
		if !ok {
			f.Reason = notes[0]
			return f
		}
		gp3 := gp3Cost(c.SizeGiB, c.IOPS, c.Throughput, p)
		f.Gp3Monthly = round2(gp3)
		f.MonthlySaving = round2(current - gp3)
		f.Gp3SizeGiB, f.Gp3IOPS, f.Gp3Throughput = c.SizeGiB, c.IOPS, c.Throughput
		if f.MonthlySaving > 0 {
			f.Candidate = true
			f.Reason = withNotes("gp2 volume; migrate to gp3", notes)
		} else {
			f.Reason = withNotes("gp2 volume with no positive saving at current prices", notes)
		}
		return f

	case "io1", "io2":
		gbPrice, iopsPrice := p.Io1GB, p.Io1IOPS
		if v.Type == "io2" {
			gbPrice, iopsPrice = p.Io2GB, p.Io2IOPS
		}
		current := float64(v.SizeGiB)*gbPrice + float64(v.IOPS)*iopsPrice
		f.CurrentMonthly = round2(current)
		c, notes, ok := gp3Target(v.SizeGiB, v.IOPS, v.Throughput, p)
		if !ok {
			f.Reason = "provisioned " + notes[0]
			return f
		}
		gp3 := gp3Cost(c.SizeGiB, c.IOPS, c.Throughput, p)
		f.Gp3Monthly = round2(gp3)
		f.MonthlySaving = round2(current - gp3)
		f.Gp3SizeGiB, f.Gp3IOPS, f.Gp3Throughput = c.SizeGiB, c.IOPS, c.Throughput
		if f.MonthlySaving > 0 {
			f.Candidate = true
			f.Reason = withNotes("over-provisioned "+v.Type+"; migrate to gp3", notes)
		} else {
			f.Reason = withNotes(v.Type+" volume with no positive saving at current prices", notes)
		}
		return f

	default:
		f.Reason = "volume type " + v.Type + " is not a gp3 migration target"
		return f
	}
}

// Audit runs the cost model over every volume and aggregates the totals.
func Audit(vols []Volume, p Prices, o Options) Report {
	r := Report{VolumeCount: len(vols)}
	for _, v := range vols {
		f := assess(v, p, o)
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
