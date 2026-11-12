package audit

import (
	"encoding/json"
	"fmt"
	"os"
)

// Prices holds the monthly EBS unit prices used to estimate migration savings,
// plus the gp3 limits the target volume must fit in. Defaults are us-east-1
// on-demand list prices and the gp3 limits AWS published in September 2025;
// override per region (or for regions and Outposts still on the old limits)
// with a file.
type Prices struct {
	Gp2GB                 float64 `json:"gp2_gb"`
	Gp3GB                 float64 `json:"gp3_gb"`
	Gp3IOPS               float64 `json:"gp3_iops"`
	Gp3Throughput         float64 `json:"gp3_throughput"`
	Io1GB                 float64 `json:"io1_gb"`
	Io1IOPS               float64 `json:"io1_iops"`
	Io2GB                 float64 `json:"io2_gb"`
	Io2IOPS               float64 `json:"io2_iops"`
	Gp3BaselineIOPS       int64   `json:"gp3_baseline_iops"`
	Gp3BaselineThroughput int64   `json:"gp3_baseline_throughput"`
	Gp3MaxIOPS            int64   `json:"gp3_max_iops"`
	Gp3MaxThroughput      int64   `json:"gp3_max_throughput"`
	Gp3MaxSizeGiB         int64   `json:"gp3_max_size_gib"`
	// Gp3IOPSPerGiB is the most IOPS a gp3 volume may provision per GiB.
	Gp3IOPSPerGiB int64 `json:"gp3_iops_per_gib"`
	// Gp3ThroughputPerIOPS is the most MiB/s a gp3 volume may provision per
	// provisioned IOPS.
	Gp3ThroughputPerIOPS float64 `json:"gp3_throughput_per_iops"`
}

// DefaultPrices returns the built-in us-east-1 price table.
func DefaultPrices() Prices {
	return Prices{
		Gp2GB:                 0.10,
		Gp3GB:                 0.08,
		Gp3IOPS:               0.005,
		Gp3Throughput:         0.04,
		Io1GB:                 0.125,
		Io1IOPS:               0.065,
		Io2GB:                 0.125,
		Io2IOPS:               0.065,
		Gp3BaselineIOPS:       3000,
		Gp3BaselineThroughput: 125,
		Gp3MaxIOPS:            80000,
		Gp3MaxThroughput:      2000,
		Gp3MaxSizeGiB:         65536,
		Gp3IOPSPerGiB:         500,
		Gp3ThroughputPerIOPS:  0.25,
	}
}

// LoadPrices reads a JSON price file and layers it over the defaults so a file
// may override only the fields it cares about.
func LoadPrices(path string) (Prices, error) {
	p := DefaultPrices()
	b, err := os.ReadFile(path)
	if err != nil {
		return p, fmt.Errorf("read prices %q: %w", path, err)
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("parse prices %q: %w", path, err)
	}
	// Zero or negative limits would make every volume fail the gp3 checks, so
	// treat them as "not set" and fall back to the defaults.
	d := DefaultPrices()
	if p.Gp3BaselineIOPS <= 0 {
		p.Gp3BaselineIOPS = d.Gp3BaselineIOPS
	}
	if p.Gp3BaselineThroughput <= 0 {
		p.Gp3BaselineThroughput = d.Gp3BaselineThroughput
	}
	if p.Gp3MaxIOPS <= 0 {
		p.Gp3MaxIOPS = d.Gp3MaxIOPS
	}
	if p.Gp3MaxThroughput <= 0 {
		p.Gp3MaxThroughput = d.Gp3MaxThroughput
	}
	if p.Gp3MaxSizeGiB <= 0 {
		p.Gp3MaxSizeGiB = d.Gp3MaxSizeGiB
	}
	if p.Gp3IOPSPerGiB <= 0 {
		p.Gp3IOPSPerGiB = d.Gp3IOPSPerGiB
	}
	if p.Gp3ThroughputPerIOPS <= 0 {
		p.Gp3ThroughputPerIOPS = d.Gp3ThroughputPerIOPS
	}
	return p, nil
}
