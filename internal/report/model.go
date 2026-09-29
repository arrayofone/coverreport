// Package report computes the report model (report.json) from a config, the
// coverage artifacts, floors, exclusions and an optional diff, and defines
// that model. The model is the contract between the analysis and every
// renderer (the plain-text one here; the PR comment, job summary,
// annotations and HTML page elsewhere): a renderer reads report.json and
// nothing else, so everything one needs is in it, and nothing in it depends
// on how it will be drawn. SCHEMA.md documents every field.
package report

import (
	"github.com/DarrenBangsund/coverreport/internal/brand"
	"github.com/DarrenBangsund/coverreport/internal/coverage"
	"github.com/DarrenBangsund/coverreport/internal/endpoints"
	"github.com/DarrenBangsund/coverreport/internal/floors"
)

// Version is report.json's schema version. Additive changes (a new
// optional field) keep it; anything a renderer written against the old
// shape would misread bumps it.
const Version = 1

// Overall and per-row statuses.
const (
	StatusPass = "pass"
	StatusFail = "fail"

	// Metric, layer and group statuses.
	StatusOK          = "ok"
	StatusNoFloor     = "no_floor"
	StatusNoData      = "no_data"
	StatusNotMeasured = "not_measured"
	StatusReportOnly  = "report_only"

	// Patch statuses.
	StatusExempt      = "exempt"
	StatusNotComputed = "not_computed"
)

// Failure scopes.
const (
	FailLayer    = "layer"
	FailPackage  = "package"
	FailGlob     = "glob"
	FailPatch    = "patch"
	FailRequired = "required"
	// FailEndpoints is one endpoint-registry baseline violation (Key is the
	// endpoint id).
	FailEndpoints = "endpoints"
)

// Report is report.json.
type Report struct {
	Version     int      `json:"version"`
	Tool        string   `json:"tool"`
	GeneratedAt string   `json:"generated_at"`
	Metadata    Metadata `json:"metadata"`
	// Status is "pass" or "fail": fail when Failures is non-empty.
	Status       string    `json:"status"`
	Failures     []Failure `json:"failures"`
	TolerancePts float64   `json:"tolerance_pts"`
	// FloorsFile / ExcludeFile are repo-relative; FloorsFound is false when
	// the floors file did not exist (every metric is then "no_floor").
	FloorsFile  string            `json:"floors_file"`
	FloorsFound bool              `json:"floors_found"`
	ExcludeFile string            `json:"exclude_file"`
	GoModules   map[string]string `json:"go_modules,omitempty"`
	Layers      []Layer           `json:"layers"`
	Patch       Patch             `json:"patch"`
	Exclusions  []Exclusion       `json:"exclusions"`
	// Ratchet lists every floor that can rise (or be created) at the
	// current measurement, rounded down to tenths.
	Ratchet []floors.Change `json:"ratchet"`
	// StaleFloors lists package/glob floors that no longer match anything
	// in a measured layer (metric empty: the whole entry). Never a failure;
	// ratchet --prune deletes them.
	StaleFloors []floors.Ref `json:"stale_floors"`
	Warnings    []string     `json:"warnings"`

	// The fields below are presentation inputs carried for the renderers
	// (added after version 1 shipped; all optional, so version stays 1).

	// Brand is the config's brand block, verbatim.
	Brand *brand.Config `json:"brand,omitempty"`
	// RatchetCommand is what the surfaces tell a reader to run.
	RatchetCommand string `json:"ratchet_command,omitempty"`
	// Baseline names the base-branch report layers were carried from
	// (analyze --baseline).
	Baseline *BaselineRef `json:"baseline,omitempty"`
	// Endpoints is the endpoint registry's completeness summary
	// (analyze --endpoints); its violations are also failures.
	Endpoints *endpoints.Summary `json:"endpoints,omitempty"`
}

// BaselineRef identifies the report that not-measured layers' values were
// carried from.
type BaselineRef struct {
	File        string `json:"file"`
	SHA         string `json:"sha,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"`
}

// Carried is a not-measured layer's totals as the baseline report measured
// them. Display only: the layer's own status stays not_measured and nothing
// carried is ever checked or ratcheted.
type Carried struct {
	SHA    string                `json:"sha,omitempty"`
	Totals map[string]PatchCount `json:"totals"`
}

// Metadata is passed in by flags or the GitHub Actions environment and
// carried through untouched.
type Metadata struct {
	Repo    string `json:"repo,omitempty"`
	PR      int    `json:"pr,omitempty"`
	SHA     string `json:"sha,omitempty"`
	Base    string `json:"base,omitempty"`
	HeadRef string `json:"head_ref,omitempty"`
	BaseRef string `json:"base_ref,omitempty"`
	RunURL  string `json:"run_url,omitempty"`
	// Title and HeadSHA come from the pull_request event payload
	// ($GITHUB_EVENT_PATH) or flags: the PR's title and its head commit
	// (SHA is the commit measured, the merge commit on a pull_request run).
	Title   string `json:"title,omitempty"`
	HeadSHA string `json:"head_sha,omitempty"`
}

// Failure is one reason the report's status is "fail".
type Failure struct {
	Scope  string `json:"scope"`
	Layer  string `json:"layer"`
	Key    string `json:"key,omitempty"`
	Metric string `json:"metric,omitempty"`
	// Measured is the percentage (2 decimals); Floor the floor; Threshold
	// what the measurement had to reach (floor - tolerance, or the patch
	// target).
	Measured  *float64 `json:"measured,omitempty"`
	Floor     *float64 `json:"floor,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
	Message   string   `json:"message"`
}

// MetricResult is one metric of one row (layer, package or glob).
type MetricResult struct {
	Covered int64 `json:"covered"`
	Total   int64 `json:"total"`
	// Pct is covered/total*100 rounded to 2 decimals; absent when there is
	// no data. Status is computed from Covered/Total exactly, never from Pct.
	Pct    *float64 `json:"pct,omitempty"`
	Floor  *float64 `json:"floor,omitempty"`
	Target *float64 `json:"target,omitempty"`
	// Delta is Pct - Floor (2 decimals).
	Delta *float64 `json:"delta,omitempty"`
	// Status: ok | fail | no_floor | no_data | not_measured | report_only.
	Status string `json:"status"`
	// RatchetTo is the value a ratchet would write (rounded down), present
	// only when it is above the current floor or there is no floor yet and
	// the row is tracked.
	RatchetTo *float64 `json:"ratchet_to,omitempty"`
}

// Layer is one configured layer.
type Layer struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Format      string `json:"format"`
	ReportOnly  bool   `json:"report_only"`
	// Status: ok | fail | not_measured | report_only. fail when any
	// layer-, package- or glob-scoped failure names this layer.
	Status string `json:"status"`
	// Inputs are the artifact files read, relative to the inputs directory.
	Inputs []string `json:"inputs"`
	// Modes are the coverprofile modes seen (go layers).
	Modes []string `json:"modes,omitempty"`
	// Metrics is the configured metric order; the first is the layer's
	// primary metric (used for sorting and sizes).
	Metrics  []string                `json:"metrics"`
	Files    int                     `json:"files"`
	Totals   map[string]MetricResult `json:"totals"`
	Packages []Group                 `json:"packages"`
	Globs    []Group                 `json:"globs"`
	// LowestFiles: the files with the most uncovered units of the primary
	// metric, at most config lowest_files.
	LowestFiles []FileSummary `json:"lowest_files"`
	// Scope reports what the layer's own include/exclude kept out, with
	// sizes, so a layer boundary cannot hide code any more quietly than
	// exclude.txt can.
	Scope []ScopeExclusion `json:"scope"`
	// ShortLabel, Group and Treemap are presentation (see config).
	ShortLabel string `json:"short_label,omitempty"`
	Group      string `json:"group,omitempty"`
	Treemap    bool   `json:"treemap,omitempty"`
	// Carried is set on a not_measured layer when a baseline report
	// measured it.
	Carried *Carried `json:"carried,omitempty"`
}

// Group is a package (directory) or glob row.
type Group struct {
	// Key is the repo-relative directory or the glob as written.
	Key    string                  `json:"key"`
	Label  string                  `json:"label,omitempty"`
	Files  int                     `json:"files"`
	Totals map[string]MetricResult `json:"totals"`
	// Status: ok | fail | no_floor | no_data (the worst of its metrics).
	Status string `json:"status"`
}

// FileSummary is one file in a layer's lowest-files list.
type FileSummary struct {
	Path   string                    `json:"path"`
	Counts map[string]coverage.Count `json:"counts"`
	// Pct and Uncovered are for the layer's primary metric.
	Pct            float64          `json:"pct"`
	Uncovered      int64            `json:"uncovered"`
	UncoveredLines []coverage.Range `json:"uncovered_lines"`
}

// Scope kinds.
const (
	ScopeNotIncluded = "not_included"
	ScopeExcluded    = "exclude"
)

// ScopeExclusion is what the layer's own include/exclude kept out: one row
// for files no include glob matched (kind "not_included", present only when
// the layer has include globs), then one per layer exclude glob (kind
// "exclude"), in config order, including globs that removed nothing.
type ScopeExclusion struct {
	Kind   string                    `json:"kind"`
	Glob   string                    `json:"glob,omitempty"`
	Files  int                       `json:"files"`
	Counts map[string]coverage.Count `json:"counts"`
}

// Exclusion is one exclude.txt rule and what it removed, per layer.
type Exclusion struct {
	Glob   string          `json:"glob"`
	Reason string          `json:"reason"`
	Line   int             `json:"line"`
	Layers []ExclusionSize `json:"layers"`
}

// ExclusionSize is one rule's reach in one measured layer. Each excluded
// file is charged to the FIRST rule matching it.
type ExclusionSize struct {
	Layer  string                    `json:"layer"`
	Files  int                       `json:"files"`
	Counts map[string]coverage.Count `json:"counts"`
}

// Patch is patch (diff) coverage.
type Patch struct {
	// Status: pass | fail | exempt | not_computed. fail when any gated
	// (not report-only, measured, not exempt) layer is below target; only a
	// failure when Blocking.
	Status   string  `json:"status"`
	Target   float64 `json:"target"`
	MinLines int     `json:"min_lines"`
	Blocking bool    `json:"blocking"`
	// InformationalUntil is the config's planned end of the soak (display).
	InformationalUntil string `json:"informational_until,omitempty"`
	// Overall is the union over gated layers: a changed line is coverable
	// when any layer can cover it and covered when any layer did.
	// Informational; the gate is per layer.
	Overall PatchCount   `json:"overall"`
	Layers  []PatchLayer `json:"layers"`
	Diff    *DiffSummary `json:"diff,omitempty"`
}

// PatchCount is a covered/total pair with its percentage.
type PatchCount struct {
	Covered int64    `json:"covered"`
	Total   int64    `json:"total"`
	Pct     *float64 `json:"pct,omitempty"`
}

// PatchLayer is patch coverage in one layer.
type PatchLayer struct {
	Layer string `json:"layer"`
	Label string `json:"label"`
	PatchCount
	// Status: pass | fail | exempt | report_only | not_measured.
	Status string      `json:"status"`
	Files  []PatchFile `json:"files"`
}

// PatchFile is one changed file inside one layer. Changed is every added
// line; UncoveredChanged the coverable changed lines that did not run.
// CoveredLines/UncoveredLines are the WHOLE file's line coverage, so a
// renderer can annotate the full file without re-reading any artifact.
type PatchFile struct {
	Path string `json:"path"`
	PatchCount
	Changed          []coverage.Range `json:"changed"`
	UncoveredChanged []coverage.Range `json:"uncovered_changed"`
	CoveredLines     []coverage.Range `json:"covered_lines"`
	UncoveredLines   []coverage.Range `json:"uncovered_lines"`
}

// DiffSummary describes the diff patch coverage was computed from.
type DiffSummary struct {
	Files      int      `json:"files"`
	AddedLines int      `json:"added_lines"`
	Binary     []string `json:"binary"`
	Deleted    []string `json:"deleted"`
	Renamed    []Rename `json:"renamed"`
	// Unmeasured are changed (non-deleted, non-binary) files with added
	// lines that no measured layer contains.
	Unmeasured []string `json:"unmeasured"`
}

// Rename is one renamed or copied file.
type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}
