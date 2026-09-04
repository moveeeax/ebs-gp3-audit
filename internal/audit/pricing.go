package audit

import (
	"encoding/json"
	"fmt"
	"os"
)

// Prices holds the monthly EBS unit prices used to estimate migration savings.
// Defaults are us-east-1 on-demand list prices; override per region with a file.
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
		Gp3MaxIOPS:            16000,
		Gp3MaxThroughput:      1000,
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
	if p.Gp3BaselineIOPS <= 0 {
		p.Gp3BaselineIOPS = 3000
	}
	if p.Gp3BaselineThroughput <= 0 {
		p.Gp3BaselineThroughput = 125
	}
	if p.Gp3MaxIOPS <= 0 {
		p.Gp3MaxIOPS = 16000
	}
	return p, nil
}
