package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Marshal renders report.json: two-space indent, no HTML escaping (paths
// and globs stay readable), trailing newline.
func (r *Report) Marshal() ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Load reads a report.json, refusing a version this build does not know.
func Load(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if probe.Version != Version {
		return nil, fmt.Errorf("%s: report version %d is not supported (want %d)", path, probe.Version, Version)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &r, nil
}
