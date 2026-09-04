// Package source loads EBS volumes either from a live AWS account (read-only
// DescribeVolumes) or from a JSON snapshot for offline runs, CI, and tests.
package source

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/moveeeax/ebs-gp3-audit/internal/audit"
)

type fileDoc struct {
	Volumes []audit.Volume `json:"volumes"`
}

// FromFile reads a JSON snapshot of volumes. This is the offline path used by
// examples, CI and pre-commit where hitting AWS is undesirable.
func FromFile(path string) ([]audit.Volume, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read volumes %q: %w", path, err)
	}
	var doc fileDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse volumes %q: %w", path, err)
	}
	return doc.Volumes, nil
}
