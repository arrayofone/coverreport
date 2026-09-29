// Package config loads and validates coverage/config.json, the
// human-maintained file that says what the layers are, where their artifacts
// land, which files each one owns and what its targets are. SCHEMA.md is the
// reference; this file is the enforcement, and every rule it enforces is one
// that would otherwise fail silently (a typo'd key, a metric the format
// cannot measure, two layers sharing an id) rather than loudly.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arrayofone/coverreport/internal/brand"
	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/glob"
	"github.com/arrayofone/coverreport/internal/paths"
)

// Version is the only config version this build reads.
const Version = 1

// Defaults, applied when a key is absent.
const (
	DefaultFloors      = "coverage/floors.json"
	DefaultExclude     = "coverage/exclude.txt"
	DefaultPatchTarget = 80.0
	DefaultMinLines    = 5
	DefaultLowestFiles = 25
	// DefaultRatchetCommand is what the surfaces tell a reader to run.
	DefaultRatchetCommand = "coverreport ratchet"
)

// GaugeLabelChars is how many characters of a label fit one line under a
// report page gauge (at its smaller label size, in any common monospace
// font) without reaching the next gauge's label. A short_label may be no
// longer; a layer with several metrics labels each gauge "<short_label>
// <metric>", and a label over this wraps onto a second line.
const GaugeLabelChars = 16

// Formats.
const (
	FormatGo   = "go"
	FormatLCOV = "lcov"
)

// Config is coverage/config.json.
type Config struct {
	Version     int                 `json:"version"`
	Floors      string              `json:"floors,omitempty"`
	Exclude     string              `json:"exclude,omitempty"`
	GoModules   map[string]string   `json:"go_modules,omitempty"`
	Sets        map[string][]string `json:"sets,omitempty"`
	Patch       *Patch              `json:"patch,omitempty"`
	LowestFiles *int                `json:"lowest_files,omitempty"`
	Layers      []Layer             `json:"layers"`
	// Brand themes the rendered surfaces (see internal/brand); presentation
	// only, never read by the gate.
	Brand *brand.Config `json:"brand,omitempty"`
	// RatchetCommand is what the rendered surfaces tell a reader to run to
	// raise the floors ("make coverage-ratchet"); default "coverreport
	// ratchet".
	RatchetCommand string `json:"ratchet_command,omitempty"`
}

// Patch configures patch (diff) coverage.
type Patch struct {
	Target   *float64 `json:"target,omitempty"`
	MinLines *int     `json:"min_lines,omitempty"`
	Blocking bool     `json:"blocking"`
	// InformationalUntil (YYYY-MM-DD) is when a non-blocking patch gate is
	// planned to start blocking. Shown in the rendered copy only: blocking
	// is still exactly Blocking, flipped by a reviewed commit.
	InformationalUntil string `json:"informational_until,omitempty"`
}

// Layer is one row of the coverage table.
type Layer struct {
	ID          string             `json:"id"`
	Label       string             `json:"label,omitempty"`
	Description string             `json:"description,omitempty"`
	Format      string             `json:"format"`
	Inputs      []string           `json:"inputs"`
	Include     []string           `json:"include,omitempty"`
	Exclude     []string           `json:"exclude,omitempty"`
	Metrics     []string           `json:"metrics"`
	Targets     map[string]float64 `json:"targets,omitempty"`
	ReportOnly  bool               `json:"report_only,omitempty"`
	Paths       *Paths             `json:"paths,omitempty"`
	Packages    *Packages          `json:"packages,omitempty"`
	Globs       []Glob             `json:"globs,omitempty"`
	// ShortLabel is the name in tight places (a gauge, a 13-column table
	// cell): "go unit+pg". Default: the id with dashes as spaces.
	ShortLabel string `json:"short_label,omitempty"`
	// Group gathers adjacent layers under one heading on the report page
	// (the four Go layers under "go"). Default: the id up to its first dash.
	Group string `json:"group,omitempty"`
	// Treemap draws this layer's packages as the report page's treemap of
	// where the untested code lives. Default: the measured, gated layer
	// with the most units of its primary metric spread over packages.
	Treemap bool `json:"treemap,omitempty"`
}

// Paths is LCOV path handling (see paths.Resolver.LCOVFile).
type Paths struct {
	Prefix string   `json:"prefix,omitempty"`
	Strip  []string `json:"strip,omitempty"`
}

// Packages configures the per-directory table and its floors.
type Packages struct {
	// Floors: a ratchet writes a floor for every directory of this layer
	// (with at least MinSize coverable units of the layer's first metric).
	Floors  bool               `json:"floors"`
	MinSize int64              `json:"min_size,omitempty"`
	Targets map[string]float64 `json:"targets,omitempty"`
}

// Glob is one aggregate row: every file of the layer matching the glob.
type Glob struct {
	Glob    string             `json:"glob"`
	Label   string             `json:"label,omitempty"`
	Targets map[string]float64 `json:"targets,omitempty"`
}

// Resolved is a validated config with every default applied and every glob
// compiled; it is what the analyzer consumes.
type Resolved struct {
	Floors             string
	Exclude            string
	GoModules          map[string]string
	PatchTarget        float64
	MinLines           int
	Blocking           bool
	InformationalUntil string
	LowestFiles        int
	Layers             []*ResolvedLayer
	Brand              *brand.Config
	RatchetCommand     string
	// Warnings are things the config says that work but are probably
	// not what was meant; the report carries them with its own.
	Warnings []string
}

// ResolvedLayer is a validated layer.
type ResolvedLayer struct {
	Layer
	MetricList   []coverage.Metric
	TargetMap    map[coverage.Metric]float64
	InputGlobs   []*glob.Pattern
	IncludeGlobs []*glob.Pattern
	ExcludeGlobs []*glob.Pattern
	Prefix       string
	Strip        []string
	PkgFloors    bool
	PkgMinSize   int64
	PkgTargets   map[coverage.Metric]float64
	GlobRows     []ResolvedGlob
}

// ResolvedGlob is a validated aggregate row.
type ResolvedGlob struct {
	Pattern *glob.Pattern
	Label   string
	Targets map[coverage.Metric]float64
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Load reads and validates a config file.
func Load(path string) (*Resolved, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// Parse decodes and validates config JSON. Unknown keys are an error: a
// misspelt "exlcude" must not silently widen a layer.
func Parse(data []byte) (*Resolved, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if dec.More() {
		return nil, errors.New("config: trailing data after the JSON object")
	}
	return c.Resolve()
}

// Resolve validates c and applies defaults.
func (c *Config) Resolve() (*Resolved, error) {
	if c.Version != Version {
		return nil, fmt.Errorf("config: version %d is not supported (want %d)", c.Version, Version)
	}
	r := &Resolved{
		Floors:      orDefault(c.Floors, DefaultFloors),
		Exclude:     orDefault(c.Exclude, DefaultExclude),
		GoModules:   c.GoModules,
		PatchTarget: DefaultPatchTarget,
		MinLines:    DefaultMinLines,
		LowestFiles: DefaultLowestFiles,
	}
	for _, p := range []string{r.Floors, r.Exclude} {
		if _, err := paths.CleanRel(p); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
	}
	if c.Patch != nil {
		if c.Patch.Target != nil {
			r.PatchTarget = *c.Patch.Target
		}
		if c.Patch.MinLines != nil {
			r.MinLines = *c.Patch.MinLines
		}
		r.Blocking = c.Patch.Blocking
		if d := c.Patch.InformationalUntil; d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return nil, fmt.Errorf("config: patch.informational_until %q is not a YYYY-MM-DD date", d)
			}
			r.InformationalUntil = d
		}
	}
	if err := checkPct("patch.target", r.PatchTarget); err != nil {
		return nil, err
	}
	if r.MinLines < 0 {
		return nil, errors.New("config: patch.min_lines must be >= 0")
	}
	if c.LowestFiles != nil {
		if *c.LowestFiles < 0 {
			return nil, errors.New("config: lowest_files must be >= 0")
		}
		r.LowestFiles = *c.LowestFiles
	}
	for mp, dir := range c.GoModules {
		if mp == "" || strings.ContainsAny(mp, " \t") {
			return nil, fmt.Errorf("config: go_modules key %q is not an import path", mp)
		}
		if _, err := paths.CleanRel(dir); err != nil {
			return nil, fmt.Errorf("config: go_modules[%q]: %w", mp, err)
		}
	}
	for name, globs := range c.Sets {
		if !idRE.MatchString(name) {
			return nil, fmt.Errorf("config: set name %q must match %s", name, idRE)
		}
		if len(globs) == 0 {
			return nil, fmt.Errorf("config: set %q is empty", name)
		}
	}
	if err := c.Brand.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	r.Brand = c.Brand
	if strings.ContainsAny(c.RatchetCommand, "`\n\r") {
		return nil, errors.New("config: ratchet_command must be one line without backticks")
	}
	r.RatchetCommand = orDefault(c.RatchetCommand, DefaultRatchetCommand)
	if len(c.Layers) == 0 {
		return nil, errors.New("config: no layers")
	}
	seen := map[string]bool{}
	for i := range c.Layers {
		l, err := c.resolveLayer(&c.Layers[i])
		if err != nil {
			id := c.Layers[i].ID
			if id == "" {
				id = fmt.Sprintf("#%d", i)
			}
			return nil, fmt.Errorf("config: layer %s: %w", id, err)
		}
		if seen[l.ID] {
			return nil, fmt.Errorf("config: duplicate layer id %q", l.ID)
		}
		seen[l.ID] = true
		r.Layers = append(r.Layers, l)
		if w := l.gaugeLabelWarning(); w != "" {
			r.Warnings = append(r.Warnings, w)
		}
	}
	return r, nil
}

// gaugeLabelWarning is non-empty when one of the layer's gauge labels,
// "<short_label> <metric>", is too wide for one line under its gauge. The
// page copes (the label wraps onto a second line), but those gauges then
// stand taller than their neighbours, and a shorter short_label is almost
// always what was meant: "integration branches" wraps, "integ branches"
// does not.
func (l *ResolvedLayer) gaugeLabelWarning() string {
	if len(l.MetricList) < 2 {
		return ""
	}
	longest := ""
	for _, m := range l.MetricList {
		if len(m) > len(longest) {
			longest = string(m)
		}
	}
	label := l.ShortLabel + " " + longest
	n := utf8.RuneCountInString(label)
	if n <= GaugeLabelChars {
		return ""
	}
	return fmt.Sprintf("config: layer %s: its gauge label %q is %d characters and one line under a gauge fits %d, so the report page wraps it onto two; a short_label of at most %d characters keeps it on one",
		l.ID, label, n, GaugeLabelChars, GaugeLabelChars-1-len(longest))
}

// allowed lists the metrics each format can measure.
var allowed = map[string][]coverage.Metric{
	FormatGo:   {coverage.Statements},
	FormatLCOV: {coverage.Lines, coverage.Branches, coverage.Functions},
}

func (c *Config) resolveLayer(l *Layer) (*ResolvedLayer, error) {
	if !idRE.MatchString(l.ID) {
		return nil, fmt.Errorf("id %q must match %s", l.ID, idRE)
	}
	ok := allowed[l.Format]
	if ok == nil {
		return nil, fmt.Errorf("format %q (want %q or %q)", l.Format, FormatGo, FormatLCOV)
	}
	rl := &ResolvedLayer{Layer: *l, TargetMap: map[coverage.Metric]float64{}, PkgTargets: map[coverage.Metric]float64{}}
	if rl.Label == "" {
		rl.Label = l.ID
	}
	if rl.ShortLabel == "" {
		rl.ShortLabel = strings.ReplaceAll(l.ID, "-", " ")
	}
	if rl.Group == "" {
		rl.Group, _, _ = strings.Cut(l.ID, "-")
	}
	for what, s := range map[string]string{"label": rl.Label, "short_label": rl.ShortLabel, "group": rl.Group} {
		// Labels land in markdown tables, fixed-width grids and HTML: one
		// line, and nothing that would open a code span or a cell.
		if strings.ContainsAny(s, "|`\n\r<>") {
			return nil, fmt.Errorf("%s %q may not contain |, `, <, > or a newline", what, s)
		}
	}
	if n := utf8.RuneCountInString(rl.ShortLabel); n > GaugeLabelChars {
		return nil, fmt.Errorf("short_label %q is %d characters; at most %d fit a gauge", rl.ShortLabel, n, GaugeLabelChars)
	}
	if len(l.Metrics) == 0 {
		return nil, errors.New("metrics is empty")
	}
	for _, s := range l.Metrics {
		m, err := coverage.ParseMetric(s)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ok, m) {
			return nil, fmt.Errorf("metric %q cannot be measured from %s input (a %s layer measures %v)", m, l.Format, l.Format, ok)
		}
		if slices.Contains(rl.MetricList, m) {
			return nil, fmt.Errorf("metric %q listed twice", m)
		}
		rl.MetricList = append(rl.MetricList, m)
	}
	var err error
	if rl.TargetMap, err = metricMap("targets", l.Targets, rl.MetricList); err != nil {
		return nil, err
	}
	if len(l.Inputs) == 0 {
		return nil, errors.New("inputs is empty")
	}
	for _, in := range l.Inputs {
		p, err := glob.Compile(in)
		if err != nil {
			return nil, fmt.Errorf("inputs: %w", err)
		}
		rl.InputGlobs = append(rl.InputGlobs, p)
	}
	if rl.IncludeGlobs, err = c.compileList("include", l.Include); err != nil {
		return nil, err
	}
	if rl.ExcludeGlobs, err = c.compileList("exclude", l.Exclude); err != nil {
		return nil, err
	}
	if l.Paths != nil {
		if l.Format != FormatLCOV {
			return nil, errors.New("paths applies to lcov layers only (Go paths resolve through go_modules)")
		}
		if l.Paths.Prefix != "" {
			if rl.Prefix, err = paths.CleanRel(l.Paths.Prefix); err != nil {
				return nil, fmt.Errorf("paths.prefix: %w", err)
			}
			if rl.Prefix == "." {
				rl.Prefix = ""
			}
		}
		for _, s := range l.Paths.Strip {
			if !strings.HasPrefix(s, "/") {
				return nil, fmt.Errorf("paths.strip entry %q must be absolute", s)
			}
			rl.Strip = append(rl.Strip, s)
		}
	}
	if l.Packages != nil {
		rl.PkgFloors = l.Packages.Floors
		rl.PkgMinSize = l.Packages.MinSize
		if rl.PkgMinSize < 0 {
			return nil, errors.New("packages.min_size must be >= 0")
		}
		if rl.PkgTargets, err = metricMap("packages.targets", l.Packages.Targets, rl.MetricList); err != nil {
			return nil, err
		}
	}
	if l.ReportOnly && l.Packages != nil && l.Packages.Floors {
		return nil, errors.New("a report_only layer has no floors; drop packages.floors")
	}
	seenGlob := map[string]bool{}
	for _, g := range l.Globs {
		p, err := glob.Compile(g.Glob)
		if err != nil {
			return nil, fmt.Errorf("globs: %w", err)
		}
		if seenGlob[g.Glob] {
			return nil, fmt.Errorf("globs: %q listed twice", g.Glob)
		}
		seenGlob[g.Glob] = true
		t, err := metricMap("globs["+g.Glob+"].targets", g.Targets, rl.MetricList)
		if err != nil {
			return nil, err
		}
		rl.GlobRows = append(rl.GlobRows, ResolvedGlob{Pattern: p, Label: g.Label, Targets: t})
	}
	return rl, nil
}

// compileList compiles include/exclude entries, expanding "@set" references.
func (c *Config) compileList(what string, entries []string) ([]*glob.Pattern, error) {
	var out []*glob.Pattern
	for _, e := range entries {
		src := []string{e}
		if name, ok := strings.CutPrefix(e, "@"); ok {
			set, ok := c.Sets[name]
			if !ok {
				return nil, fmt.Errorf("%s: unknown set %q", what, e)
			}
			src = set
		}
		for _, s := range src {
			p, err := glob.Compile(s)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", what, err)
			}
			out = append(out, p)
		}
	}
	return out, nil
}

func metricMap(what string, in map[string]float64, ms []coverage.Metric) (map[coverage.Metric]float64, error) {
	out := map[coverage.Metric]float64{}
	for k, v := range in {
		m, err := coverage.ParseMetric(k)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		if !slices.Contains(ms, m) {
			return nil, fmt.Errorf("%s: %q is not one of this layer's metrics", what, k)
		}
		if err := checkPct(what+"."+k, v); err != nil {
			return nil, err
		}
		out[m] = v
	}
	return out, nil
}

func checkPct(what string, v float64) error {
	if v < 0 || v > 100 {
		return fmt.Errorf("config: %s = %v is not a percentage in [0, 100]", what, v)
	}
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Layer returns the resolved layer with the given id, or nil.
func (r *Resolved) Layer(id string) *ResolvedLayer {
	for _, l := range r.Layers {
		if l.ID == id {
			return l
		}
	}
	return nil
}
