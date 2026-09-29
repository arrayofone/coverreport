// Package endpoints reads the endpoint registry's endpoints.json (the
// contract shared with the sibling repositories: one entry per route, event,
// cron entrypoint, page, layout or server action, with the rubric classes
// each test layer proves for it) and condenses it into the completeness rows
// a report carries: per kind and per surface, how many endpoints are fully,
// partially or not at all tested in each layer, which class is missed most,
// and the baseline violations that fail the check.
//
// The registry is produced elsewhere (a scanner walking the real route
// tables); this package only reads it. Unknown fields are ignored, because
// the contract grows additively on the producer's side, but the version,
// the statuses and the shape of every field used here are checked, so a
// half-written or renamed file fails loudly instead of rendering zeros.
package endpoints

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

// Version is the only endpoints.json version this build reads.
const Version = 1

// Statuses of an endpoint in one layer, and its best status overall.
const (
	Full    = "full"
	Partial = "partial"
	None    = "none"
)

// Layers are the registry's test layers, in display order. A layer name the
// registry uses beyond these is shown after them, sorted.
var Layers = []string{"unit", "integration", "e2e"}

// File is endpoints.json.
type File struct {
	Version       int                `json:"version"`
	GeneratedFrom string             `json:"generated_from"`
	Classes       []string           `json:"classes"`
	Surfaces      map[string]Surface `json:"surfaces"`
	Endpoints     []Endpoint         `json:"endpoints"`
	Baseline      struct {
		Violations []Violation `json:"violations"`
	} `json:"baseline"`
}

// Surface is one surface's display data.
type Surface struct {
	Label string `json:"label"`
}

// Endpoint is one registered endpoint.
type Endpoint struct {
	ID         string                 `json:"id"`
	Surface    string                 `json:"surface"`
	Kind       string                 `json:"kind"`
	File       string                 `json:"file"`
	Applicable []string               `json:"applicable"`
	NA         map[string]string      `json:"na"`
	Layers     map[string]LayerResult `json:"layers"`
	Best       string                 `json:"best"`
}

// LayerResult is what one test layer proves about one endpoint.
type LayerResult struct {
	Classes []string `json:"classes"`
	Status  string   `json:"status"`
}

// Violation is one baseline breach: an endpoint that gained a gap, or a new
// endpoint that is not fully tested in any layer.
type Violation struct {
	ID     string `json:"id"`
	Detail string `json:"detail"`
}

// Load reads and validates an endpoints.json.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Parse decodes and validates endpoints.json.
func Parse(data []byte) (*File, error) {
	var f File
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
		return nil, fmt.Errorf("endpoints: %w", err)
	}
	if f.Version != Version {
		return nil, fmt.Errorf("endpoints: version %d is not supported (want %d)", f.Version, Version)
	}
	if len(f.Endpoints) == 0 {
		// The registry's enumerators each have a minimum-count floor, so an
		// empty list means the producer broke, not that there is nothing
		// to test.
		return nil, errors.New("endpoints: no endpoints (a broken enumerator, not an empty registry)")
	}
	seen := map[string]bool{}
	for i, e := range f.Endpoints {
		if e.ID == "" || e.Surface == "" || e.Kind == "" {
			return nil, fmt.Errorf("endpoints: entry %d lacks an id, surface or kind", i)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("endpoints: %s is listed twice", e.ID)
		}
		seen[e.ID] = true
		if !validStatus(e.Best) {
			return nil, fmt.Errorf("endpoints: %s: best %q is not full, partial or none", e.ID, e.Best)
		}
		for name, l := range e.Layers {
			if !validStatus(l.Status) {
				return nil, fmt.Errorf("endpoints: %s: layer %s status %q is not full, partial or none", e.ID, name, l.Status)
			}
		}
	}
	for i, v := range f.Baseline.Violations {
		if v.ID == "" {
			return nil, fmt.Errorf("endpoints: baseline violation %d has no id", i)
		}
	}
	return &f, nil
}

func validStatus(s string) bool { return s == Full || s == Partial || s == None }

// Counts is how many endpoints of a group are at each status.
type Counts struct {
	Full    int `json:"full"`
	Partial int `json:"partial"`
	None    int `json:"none"`
}

func (c *Counts) add(status string) {
	switch status {
	case Full:
		c.Full++
	case Partial:
		c.Partial++
	default:
		c.None++
	}
}

// Group is one completeness row: every endpoint of one kind, or of one
// surface.
type Group struct {
	// Key is the kind ("route") or the surface id ("svc-invoices").
	Key   string `json:"key"`
	Label string `json:"label"`
	Total int    `json:"total"`
	// Best counts each endpoint once, at its best status in any layer.
	Best Counts `json:"best"`
	// Layers counts per test layer; an endpoint with no entry for a layer
	// counts as none there.
	Layers map[string]Counts `json:"layers"`
	// MostMissed is the applicable class missing most often from the
	// endpoints' best layer, with how many endpoints miss it. Absent when
	// nothing is missing.
	MostMissed      string `json:"most_missed,omitempty"`
	MostMissedCount int    `json:"most_missed_count,omitempty"`
}

// Summary is what a report carries of the registry.
type Summary struct {
	// File is the endpoints.json read, as given on the command line.
	File          string   `json:"file"`
	GeneratedFrom string   `json:"generated_from,omitempty"`
	Total         int      `json:"total"`
	Layers        []string `json:"layers"`
	// Kinds has one row per endpoint kind, in first-seen order of the
	// registry (which walks routes before events before pages).
	Kinds []Group `json:"kinds"`
	// Surfaces has one row per surface, in the same order.
	Surfaces   []Group     `json:"surfaces"`
	Violations []Violation `json:"violations"`
}

// Summarize condenses a registry. file is recorded as given.
func Summarize(f *File, file string) *Summary {
	s := &Summary{File: file, GeneratedFrom: f.GeneratedFrom, Total: len(f.Endpoints),
		Kinds: []Group{}, Surfaces: []Group{}, Violations: []Violation{}}
	s.Violations = append(s.Violations, f.Baseline.Violations...)
	seenLayer := map[string]bool{}
	for _, e := range f.Endpoints {
		for l := range e.Layers {
			seenLayer[l] = true
		}
	}
	for _, l := range Layers {
		if seenLayer[l] {
			s.Layers = append(s.Layers, l)
			delete(seenLayer, l)
		}
	}
	var extra []string
	for l := range seenLayer {
		extra = append(extra, l)
	}
	sort.Strings(extra)
	s.Layers = append(s.Layers, extra...)
	if s.Layers == nil {
		s.Layers = []string{}
	}

	classOrder := map[string]int{}
	for i, c := range f.Classes {
		classOrder[c] = i
	}
	type acc struct {
		g      *Group
		missed map[string]int
	}
	build := func(key func(Endpoint) string, label func(string) string) []Group {
		var order []string
		byKey := map[string]*acc{}
		for _, e := range f.Endpoints {
			k := key(e)
			a := byKey[k]
			if a == nil {
				a = &acc{g: &Group{Key: k, Label: label(k), Layers: map[string]Counts{}}, missed: map[string]int{}}
				byKey[k] = a
				order = append(order, k)
			}
			a.g.Total++
			a.g.Best.add(e.Best)
			for _, l := range s.Layers {
				c := a.g.Layers[l]
				c.add(e.Layers[l].Status)
				a.g.Layers[l] = c
			}
			for _, c := range missing(e) {
				a.missed[c]++
			}
		}
		out := make([]Group, 0, len(order))
		for _, k := range order {
			a := byKey[k]
			best, n := "", 0
			for c, m := range a.missed {
				rc, rb := rank(classOrder, c), rank(classOrder, best)
				if m > n || m == n && (rc < rb || rc == rb && c < best) {
					best, n = c, m
				}
			}
			a.g.MostMissed, a.g.MostMissedCount = best, n
			out = append(out, *a.g)
		}
		return out
	}
	s.Kinds = build(func(e Endpoint) string { return e.Kind }, KindLabel)
	s.Surfaces = build(func(e Endpoint) string { return e.Surface }, func(k string) string {
		if l := f.Surfaces[k].Label; l != "" {
			return l
		}
		return k
	})
	return s
}

// missing is the applicable classes absent from the endpoint's best layer:
// the one whose status is the endpoint's best, with the most classes.
func missing(e Endpoint) []string {
	var have map[string]bool
	n := -1
	names := make([]string, 0, len(e.Layers))
	for l := range e.Layers {
		names = append(names, l)
	}
	sort.Strings(names)
	for _, l := range names {
		lr := e.Layers[l]
		if lr.Status == e.Best && len(lr.Classes) > n {
			n = len(lr.Classes)
			have = map[string]bool{}
			for _, c := range lr.Classes {
				have[c] = true
			}
		}
	}
	var out []string
	for _, c := range e.Applicable {
		if !have[c] {
			out = append(out, c)
		}
	}
	return out
}

func rank(order map[string]int, c string) int {
	if c == "" {
		return 1 << 30
	}
	if i, ok := order[c]; ok {
		return i
	}
	return len(order)
}

// KindLabel is a kind's plural display name.
func KindLabel(kind string) string {
	switch kind {
	case "route":
		return "routes"
	case "event":
		return "events"
	case "cron":
		return "cron entrypoints"
	case "page":
		return "pages"
	case "layout":
		return "layouts"
	case "action":
		return "server actions"
	}
	return kind + "s"
}

// KindNoun is the display name for n endpoints of a kind: "1 route" reads
// as a count, "1 routes" does not. Every label above is its singular plus
// an "s".
func KindNoun(kind string, n int) string {
	l := KindLabel(kind)
	if n == 1 {
		return l[:len(l)-1]
	}
	return l
}
