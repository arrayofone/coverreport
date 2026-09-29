// Package view derives everything the four rendered surfaces show (the
// sticky PR comment, the job summary, the annotations and the HTML page)
// from one report.json, once. Every decision (a row's state, the verdict,
// which lines never ran, which ranges get an annotation, what can ratchet)
// is made here and nowhere else, so the surfaces cannot disagree: the
// renderers only lay out what this package decided.
//
// The state vocabulary is the whole design. Colour means state and nothing
// else, and each state has one marker in the markdown HUD (GitHub's diff
// highlighter colours a line by its first character):
//
//	ok        + green   at or above its floor, patch at or above target
//	warn      ! amber   floor holds, patch under target
//	fail      - red     below its floor (the only word written in capitals)
//	info      # grey    report-only: measured, never gated
//	untouched # grey    measured, holds, and this change touched none of it
//	carried   # grey    not run here; its numbers come from the base branch
//	notrun    # grey    not run here, nothing to carry
//	new       # grey    measured, no floor yet (the first run)
package view

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/arrayofone/coverreport/internal/brand"
	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/endpoints"
	"github.com/arrayofone/coverreport/internal/floors"
	"github.com/arrayofone/coverreport/internal/glob"
	"github.com/arrayofone/coverreport/internal/report"
	"github.com/arrayofone/coverreport/internal/source"
)

// Row states.
const (
	StateOK        = "ok"
	StateWarn      = "warn"
	StateFail      = "fail"
	StateInfo      = "info"
	StateUntouched = "untouched"
	StateCarried   = "carried"
	StateNotRun    = "notrun"
	StateNew       = "new"
)

// Verdicts.
const (
	VerdictFail = "fail"
	VerdictWarn = "warn"
	VerdictOK   = "ok"
	// VerdictNone: nothing is gated yet (no floors file, or no floor for
	// anything that was measured).
	VerdictNone = "none"
)

// Options are the render-time inputs that are not in the report.
type Options struct {
	// Source is a checkout of the measured commit; nil renders without
	// code excerpts.
	Source *source.Root
	// Previous is the body of the sticky comment this render replaces, ""
	// when there is none. Its hidden state carries the patch history.
	Previous string
	// ArtifactURL is where report.html was uploaded, "" when not (yet)
	// known. ArtifactName is what the links call it.
	ArtifactURL  string
	ArtifactName string
	// Version is coverreport's version, for the footers.
	Version string
}

// View is one report, decided.
type View struct {
	R       *report.Report
	Brand   brand.Resolved
	Version string
	At      time.Time
	Links   Links

	Verdict string
	// Heading is the verdict in words: "1 floor broken", "every floor
	// holds", "floors hold, patch under 80%", "no floors yet".
	Heading string
	Rows    []*Row
	Groups  []*Group

	// Problems are the failures, one per failure except that endpoint
	// violations are gathered into one.
	Problems []*Problem
	Patch    *Patch
	Pushes   []Push
	PushNo   int

	Files       []*File
	Annotations []Annotation
	Ratchets    []*RatchetEntry
	RatchetCmd  string
	Touched     []*PkgRow
	Packages    []*PkgTable
	Globs       []*GlobTable
	Lowest      []*LowestTable
	Exclusions  []*ExclusionRow
	Scope       []*ScopeRow
	Excluded    map[string]int64
	Measured    []*report.Layer
	Endpoints   *endpoints.Summary
	// EndpointViolations counts violations per endpoint kind.
	EndpointViolations map[string]int
	Treemap            *PkgTable

	// failing marks the layers with a layer, package, glob or blocking
	// patch failure, with the package dirs and globs that failed.
	failing map[string]*layerFailure
	layers  map[string]*report.Layer
}

type layerFailure struct {
	layer bool
	patch bool
	pkgs  map[string]bool
	globs []*glob.Pattern
}

// Links builds every URL a surface prints. Empty strings mean "unknown":
// the surfaces then print the text without a link.
type Links struct {
	Server, Repo, Run, Artifact, ArtifactName string
	SHA                                       string
	PR                                        int
}

// RepoURL is the repository's web URL.
func (l Links) RepoURL() string {
	if l.Repo == "" {
		return ""
	}
	return l.Server + "/" + l.Repo
}

// PRURL is the pull request's URL.
func (l Links) PRURL() string {
	if l.Repo == "" || l.PR == 0 {
		return ""
	}
	return fmt.Sprintf("%s/pull/%d", l.RepoURL(), l.PR)
}

// Blob is a file's URL at the measured commit.
func (l Links) Blob(p string) string {
	if l.Repo == "" || l.SHA == "" {
		return ""
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return l.RepoURL() + "/blob/" + l.SHA + "/" + strings.Join(segs, "/")
}

// Lines is a file's URL anchored at a line range.
func (l Links) Lines(p string, r coverage.Range) string {
	b := l.Blob(p)
	if b == "" {
		return ""
	}
	if r[0] == r[1] {
		return fmt.Sprintf("%s#L%d", b, r[0])
	}
	return fmt.Sprintf("%s#L%d-L%d", b, r[0], r[1])
}

// Row is one gauge: one metric of one layer.
type Row struct {
	Layer   *report.Layer
	Metric  string
	Primary bool
	// Label is the short name ("go unit+pg", "app lines"); Long the full
	// one ("Go unit + Postgres", "App (vitest) lines").
	Label, Long string
	Group       string
	Index       int

	Covered, Total int64
	// Pct is the measurement (for a carried row, the base branch's).
	Pct                  *float64
	Floor, Target, Delta *float64
	RatchetTo            *float64
	State                string
	CarriedSHA           string
	// Patch is the layer's patch coverage, on its primary row only, when
	// a diff was analysed.
	Patch *report.PatchLayer
}

// Marker is the row's first character in a markdown diff block.
func (r *Row) Marker() string { return marker(r.State) }

func marker(state string) string {
	switch state {
	case StateOK:
		return "+"
	case StateWarn:
		return "!"
	case StateFail:
		return "-"
	}
	return "#"
}

// Word is the row's state as a short word for a grid column.
func (r *Row) Word() string {
	switch r.State {
	case StateFail:
		return "FAIL"
	case StateOK, StateWarn, StateInfo, StateNew:
		return r.State
	}
	return "--"
}

// Words is the row's state in a phrase.
func (r *Row) Words() string {
	switch r.State {
	case StateFail:
		return "below floor"
	case StateWarn:
		return "patch under target"
	case StateInfo:
		return "report-only"
	case StateUntouched:
		return "holds, untouched by this change"
	case StateCarried:
		return "not run here, carried"
	case StateNotRun:
		return "not run here"
	case StateNew:
		return "no floor yet"
	}
	if r.RatchetTo != nil {
		return "holds, floor can rise"
	}
	return "holds"
}

// Met reports whether the row is at or past its target.
func (r *Row) Met() bool {
	return r.Target != nil && r.Pct != nil && *r.Pct >= *r.Target
}

// Group is a run of adjacent rows sharing a group name.
type Group struct {
	Name string
	Rows []*Row
}

// Problem is one reason the check fails.
type Problem struct {
	// Kind: floor (a layer metric), package, glob, nodata, required,
	// patch, endpoints.
	Kind    string
	Failure report.Failure
	Row     *Row
	Layer   *report.Layer
	// Files are the changed files counting toward this problem with lines
	// that never ran.
	Files []*File
	// Violations (endpoints only).
	Violations []endpoints.Violation
	// Repeat is an earlier problem on the same layer counting the same
	// files: surfaces point at it instead of listing the lines twice.
	Repeat *Problem
}

// Patch is patch coverage, decided.
type Patch struct {
	Overall  report.PatchCount
	Target   float64
	Blocking bool
	Until    string
	MinLines int
	Rows     []*PatchRow
}

// PatchRow is one layer's patch coverage.
type PatchRow struct {
	Layer *report.Layer
	PL    *report.PatchLayer
	// State: ok (at target), warn (under), info (report-only), exempt.
	State string
}

// Push is one push's overall patch coverage, oldest first.
type Push struct {
	SHA string
	Pct float64
}

// Build decides a report.
func Build(r *report.Report, o Options) (*View, error) {
	fallback := ""
	if i := strings.LastIndex(r.Metadata.Repo, "/"); i >= 0 {
		fallback = r.Metadata.Repo[i+1:]
	}
	b, err := brand.Resolve(r.Brand, fallback)
	if err != nil {
		return nil, fmt.Errorf("report brand: %w", err)
	}
	// A shallow copy with every layer's presentation fields filled in, so
	// a report written before they existed renders the same and the
	// caller's report is left alone.
	rc := *r
	rc.Layers = append([]report.Layer(nil), r.Layers...)
	for i := range rc.Layers {
		l := &rc.Layers[i]
		if l.ShortLabel == "" {
			l.ShortLabel = strings.ReplaceAll(l.ID, "-", " ")
		}
		if l.Group == "" {
			l.Group, _, _ = strings.Cut(l.ID, "-")
		}
	}
	r = &rc
	v := &View{R: r, Brand: b, Version: o.Version, layers: map[string]*report.Layer{}, failing: map[string]*layerFailure{},
		Excluded: map[string]int64{}, EndpointViolations: map[string]int{}}
	if v.Version == "" {
		v.Version = "dev"
	}
	v.At, _ = time.Parse(time.RFC3339, r.GeneratedAt)
	v.RatchetCmd = r.RatchetCommand
	if v.RatchetCmd == "" {
		v.RatchetCmd = "coverreport ratchet"
	}
	v.links(r, o)
	for i := range r.Layers {
		v.layers[r.Layers[i].ID] = &r.Layers[i]
	}
	v.failures()
	v.rows()
	v.patch()
	v.files(o.Source)
	v.problems()
	v.verdict()
	v.history(o.Previous)
	v.annotations()
	v.ratchets()
	v.packages()
	v.rest()
	return v, nil
}

func (v *View) links(r *report.Report, o Options) {
	l := Links{Server: "https://github.com", Repo: r.Metadata.Repo, Run: r.Metadata.RunURL, SHA: r.Metadata.SHA, PR: r.Metadata.PR,
		Artifact: o.ArtifactURL, ArtifactName: o.ArtifactName}
	if u, err := url.Parse(r.Metadata.RunURL); err == nil && u.Scheme != "" && u.Host != "" {
		l.Server = u.Scheme + "://" + u.Host
	}
	if l.ArtifactName == "" {
		l.ArtifactName = "report.html"
	}
	v.Links = l
}

func (v *View) failures() {
	for _, f := range v.R.Failures {
		lf := v.failing[f.Layer]
		if lf == nil {
			lf = &layerFailure{pkgs: map[string]bool{}}
		}
		switch f.Scope {
		case report.FailLayer:
			lf.layer = true
		case report.FailPackage:
			lf.pkgs[f.Key] = true
		case report.FailGlob:
			if g, err := glob.Compile(f.Key); err == nil {
				lf.globs = append(lf.globs, g)
			}
		case report.FailPatch:
			lf.patch = true
		default:
			continue
		}
		v.failing[f.Layer] = lf
	}
}

// fileFails reports whether a file's lines count toward a failure in layer
// id: the layer's own floor, its blocking patch gate, the package holding
// the file, or a glob matching it.
func (v *View) fileFails(id, p string) bool {
	lf := v.failing[id]
	if lf == nil {
		return false
	}
	if lf.layer || lf.patch || lf.pkgs[path.Dir(p)] {
		return true
	}
	for _, g := range lf.globs {
		if g.Match(p) {
			return true
		}
	}
	return false
}

func (v *View) rows() {
	failed := map[string]bool{}
	for _, f := range v.R.Failures {
		if f.Scope == report.FailLayer {
			failed[f.Layer+"/"+f.Metric] = true
		}
	}
	patches := map[string]*report.PatchLayer{}
	if v.R.Patch.Status != report.StatusNotComputed {
		for i := range v.R.Patch.Layers {
			patches[v.R.Patch.Layers[i].Layer] = &v.R.Patch.Layers[i]
		}
	}
	for li := range v.R.Layers {
		l := &v.R.Layers[li]
		short, group := l.ShortLabel, l.Group
		for i, m := range l.Metrics {
			mr := l.Totals[m]
			row := &Row{Layer: l, Metric: m, Primary: i == 0, Label: short, Long: l.Label, Group: group, Index: len(v.Rows),
				Covered: mr.Covered, Total: mr.Total, Pct: mr.Pct, Floor: mr.Floor, Target: mr.Target, Delta: mr.Delta, RatchetTo: mr.RatchetTo}
			if len(l.Metrics) > 1 {
				row.Label += " " + m
				row.Long += " " + m
			}
			if i == 0 {
				row.Patch = patches[l.ID]
			}
			switch {
			case l.Status == report.StatusNotMeasured:
				row.State = StateNotRun
				if c := l.Carried; c != nil {
					if pc, ok := c.Totals[m]; ok {
						row.State = StateCarried
						row.Covered, row.Total, row.Pct, row.CarriedSHA = pc.Covered, pc.Total, pc.Pct, c.SHA
						if row.Floor != nil && pc.Pct != nil {
							d := math.Round((*pc.Pct-*row.Floor)*100) / 100
							row.Delta = &d
						}
					}
				}
			case l.ReportOnly:
				row.State = StateInfo
			case failed[l.ID+"/"+m] || mr.Status == report.StatusFail:
				row.State = StateFail
			case row.Patch != nil && row.Patch.Status == report.StatusFail:
				// The patch gate is per layer and independent of floors,
				// so a layer with no floor yet can still need a test.
				row.State = StateWarn
			case mr.Status == report.StatusNoFloor || mr.Status == report.StatusNoData:
				row.State = StateNew
			case row.Patch != nil && row.Patch.Total == 0:
				row.State = StateUntouched
			default:
				row.State = StateOK
			}
			v.Rows = append(v.Rows, row)
			if n := len(v.Groups); n == 0 || v.Groups[n-1].Name != group {
				v.Groups = append(v.Groups, &Group{Name: group})
			}
			g := v.Groups[len(v.Groups)-1]
			g.Rows = append(g.Rows, row)
		}
	}
}

// Count is how many rows are in a state.
func (v *View) Count(state string) int {
	n := 0
	for _, r := range v.Rows {
		if r.State == state {
			n++
		}
	}
	return n
}

// Rows in a state.
func (v *View) RowsIn(states ...string) []*Row {
	var out []*Row
	for _, r := range v.Rows {
		for _, s := range states {
			if r.State == s {
				out = append(out, r)
			}
		}
	}
	return out
}

func (v *View) patch() {
	p := v.R.Patch
	if p.Status == report.StatusNotComputed {
		return
	}
	v.Patch = &Patch{Overall: p.Overall, Target: p.Target, Blocking: p.Blocking, Until: p.InformationalUntil, MinLines: p.MinLines}
	for i := range p.Layers {
		pl := &p.Layers[i]
		l := v.layers[pl.Layer]
		if l == nil || pl.Status == report.StatusNotMeasured || pl.Total == 0 {
			continue
		}
		pr := &PatchRow{Layer: l, PL: pl}
		switch pl.Status {
		case report.StatusPass:
			pr.State = StateOK
		case report.StatusFail:
			pr.State = StateWarn
			if p.Blocking {
				pr.State = StateFail
			}
		case report.StatusReportOnly:
			pr.State = StateInfo
		default:
			pr.State = "exempt"
		}
		v.Patch.Rows = append(v.Patch.Rows, pr)
	}
}

// WarnRows are the gated patch rows under target (informational misses, or
// blocking ones, which are also problems).
func (v *View) WarnRows() []*PatchRow {
	if v.Patch == nil {
		return nil
	}
	var out []*PatchRow
	for _, pr := range v.Patch.Rows {
		if pr.State == StateWarn || pr.State == StateFail {
			out = append(out, pr)
		}
	}
	return out
}

func (v *View) problems() {
	var ep *Problem
	for _, f := range v.R.Failures {
		p := &Problem{Failure: f, Layer: v.layers[f.Layer]}
		switch f.Scope {
		case report.FailLayer:
			p.Kind = "floor"
			if f.Measured == nil {
				p.Kind = "nodata"
			}
			for _, r := range v.Rows {
				if r.Layer.ID == f.Layer && r.Metric == f.Metric {
					p.Row = r
				}
			}
		case report.FailPackage:
			p.Kind = "package"
		case report.FailGlob:
			p.Kind = "glob"
		case report.FailRequired:
			p.Kind = "required"
		case report.FailPatch:
			p.Kind = "patch"
		case report.FailEndpoints:
			if ep == nil {
				ep = &Problem{Kind: "endpoints", Failure: f}
				v.Problems = append(v.Problems, ep)
			}
			ep.Violations = append(ep.Violations, endpoints.Violation{ID: f.Key, Detail: strings.TrimPrefix(f.Message, "endpoint "+f.Key+" broke the endpoint baseline: ")})
			continue
		default:
			p.Kind = f.Scope
		}
		if p.Layer != nil {
			for _, fv := range v.Files {
				if len(fv.Uncovered) == 0 {
					continue
				}
				if p.problemCovers(v, fv) {
					p.Files = append(p.Files, fv)
				}
			}
		}
		for _, q := range v.Problems {
			if q.Layer != nil && p.Layer != nil && q.Layer.ID == p.Layer.ID && len(p.Files) > 0 && sameFiles(q.Files, p.Files) {
				p.Repeat = q
				break
			}
		}
		v.Problems = append(v.Problems, p)
	}
}

func sameFiles(a, b []*File) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// StateCounts is the rows' states in words: "3 hold, 1 below floor, 2
// untouched", and "1 holds" (the one verb among them agrees with its count).
func (v *View) StateCounts() string {
	var parts []string
	for _, s := range []struct{ state, words string }{
		{StateOK, "hold"}, {StateFail, "below floor"}, {StateWarn, "patch under target"}, {StateUntouched, "untouched"},
		{StateCarried, "carried from " + v.BaseRef()}, {StateNotRun, "not run"}, {StateNew, "without a floor"}, {StateInfo, "report-only"},
	} {
		n := v.Count(s.state)
		if n == 0 {
			continue
		}
		if n == 1 && s.state == StateOK {
			s.words = "holds"
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, s.words))
	}
	return strings.Join(parts, ", ")
}

func (p *Problem) problemCovers(v *View, f *File) bool {
	if !f.InLayer(p.Layer.ID) {
		return false
	}
	switch p.Kind {
	case "floor", "patch":
		return true
	case "package":
		return path.Dir(f.Path) == p.Failure.Key
	case "glob":
		g, err := glob.Compile(p.Failure.Key)
		return err == nil && g.Match(f.Path)
	}
	return false
}

// FloorsBroken counts layer, package and glob floor failures.
func (v *View) FloorsBroken() int {
	n := 0
	for _, p := range v.Problems {
		switch p.Kind {
		case "floor", "nodata", "package", "glob":
			n++
		}
	}
	return n
}

func (v *View) verdict() {
	gated := v.Gated()
	switch {
	case v.R.Status == report.StatusFail:
		v.Verdict = VerdictFail
		var parts []string
		if n := v.FloorsBroken(); n > 0 {
			parts = append(parts, Plural(int64(n), "floor broken", "floors broken"))
		}
		var req, patch []string
		endpointsBroken := false
		for _, p := range v.Problems {
			switch p.Kind {
			case "required":
				req = append(req, p.Layer.ShortLabel)
			case "patch":
				patch = append(patch, p.Layer.ShortLabel)
			case "endpoints":
				endpointsBroken = true
			}
		}
		if len(patch) == 1 {
			parts = append(parts, fmt.Sprintf("%s patch under %s%%", patch[0], Trim(v.R.Patch.Target)))
		} else if len(patch) > 1 {
			parts = append(parts, fmt.Sprintf("patch under %s%% in %d layers", Trim(v.R.Patch.Target), len(patch)))
		}
		if endpointsBroken {
			parts = append(parts, "endpoint baseline broken")
		}
		if len(req) == 1 {
			parts = append(parts, req[0]+" did not report")
		} else if len(req) > 1 {
			parts = append(parts, fmt.Sprintf("%d required layers did not report", len(req)))
		}
		v.Heading = strings.Join(parts, ", ")
	case !gated:
		// Nothing is gated, so a patch under target is not a warning
		// about a gate either: it is folded into the first-run note.
		v.Verdict = VerdictNone
		v.Heading = "no floors yet"
		if len(v.WarnRows()) > 0 {
			v.Heading += fmt.Sprintf(", patch under %s%%", Trim(v.R.Patch.Target))
		}
	case len(v.WarnRows()) > 0:
		v.Verdict = VerdictWarn
		v.Heading = fmt.Sprintf("floors hold, patch under %s%%", Trim(v.R.Patch.Target))
	default:
		v.Verdict = VerdictOK
		v.Heading = "every floor holds"
	}
}

// Light is the heading's single status light: the only emoji on any
// surface, because it survives email, the mobile app and notifications.
func (v *View) Light() string {
	switch v.Verdict {
	case VerdictFail:
		return "🔴"
	case VerdictWarn:
		return "🟡"
	case VerdictNone:
		return "⚪"
	}
	return "🟢"
}

// Gated reports whether any measured layer has a floor.
func (v *View) Gated() bool {
	for _, r := range v.Rows {
		if r.Floor != nil && r.Layer.Status != report.StatusNotMeasured && !r.Layer.ReportOnly {
			return true
		}
	}
	return false
}

// RatchetEntry is one floors.json entry that a ratchet would write, with
// every metric of that entry before and after.
type RatchetEntry struct {
	Scope, Layer, Key string
	Before, After     map[string]float64
	// Label names the entry for prose: "go live", "libs/go/calc (go live)".
	Label string
	New   bool
}

// Moves says what the entry's floors become: "39.9 to 40.0", or for a new
// entry "new at 61.5". Metrics in name order.
func (e *RatchetEntry) Moves() string {
	names := make([]string, 0, len(e.After))
	for m := range e.After {
		names = append(names, m)
	}
	sort.Strings(names)
	var parts []string
	for _, m := range names {
		to := e.After[m]
		from, had := e.Before[m]
		switch {
		case !had:
			parts = append(parts, "new at "+floors.FormatFloor(to))
		case from != to:
			parts = append(parts, floors.FormatFloor(from)+" to "+floors.FormatFloor(to))
		default:
			continue
		}
		if len(names) > 1 {
			parts[len(parts)-1] = m + " " + parts[len(parts)-1]
		}
	}
	return strings.Join(parts, ", ")
}

func (v *View) ratchets() {
	type k struct{ scope, layer, key string }
	idx := map[k]*RatchetEntry{}
	var order []k
	for _, c := range v.R.Ratchet {
		kk := k{string(c.Scope), c.Layer, c.Key}
		e := idx[kk]
		if e == nil {
			e = &RatchetEntry{Scope: kk.scope, Layer: c.Layer, Key: c.Key, Before: map[string]float64{}, After: map[string]float64{}}
			// Every metric the entry has today, so the diff shows the
			// whole line the ratchet rewrites.
			for m, mr := range v.entryTotals(e) {
				if mr.Floor != nil {
					e.Before[m] = *mr.Floor
					e.After[m] = *mr.Floor
				}
			}
			e.New = len(e.Before) == 0
			idx[kk] = e
			order = append(order, kk)
		}
		e.After[c.Metric] = c.To
	}
	// floors.json order: layers, packages, globs; then by layer and key,
	// as the canonical file sorts them.
	rank := map[string]int{"layer": 0, "package": 1, "glob": 2}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if rank[a.scope] != rank[b.scope] {
			return rank[a.scope] < rank[b.scope]
		}
		if a.layer != b.layer {
			return a.layer < b.layer
		}
		return a.key < b.key
	})
	for _, kk := range order {
		e := idx[kk]
		l := v.layers[e.Layer]
		short := e.Layer
		if l != nil && l.ShortLabel != "" {
			short = l.ShortLabel
		}
		e.Label = short
		if e.Key != "" {
			e.Label = e.Key + " (" + short + ")"
		}
		v.Ratchets = append(v.Ratchets, e)
	}
}

func (v *View) entryTotals(e *RatchetEntry) map[string]report.MetricResult {
	l := v.layers[e.Layer]
	if l == nil {
		return nil
	}
	switch e.Scope {
	case "layer":
		return l.Totals
	case "package":
		for _, g := range l.Packages {
			if g.Key == e.Key {
				return g.Totals
			}
		}
	case "glob":
		for _, g := range l.Globs {
			if g.Key == e.Key {
				return g.Totals
			}
		}
	}
	return nil
}

// Rises counts ratchet entries that raise an existing floor (as opposed to
// creating one).
func (v *View) Rises() int {
	n := 0
	for _, e := range v.Ratchets {
		if !e.New {
			n++
		}
	}
	return n
}

// PkgRow is one package (directory) row.
type PkgRow struct {
	Layer *report.Layer
	G     *report.Group
	// Primary metric result.
	M report.MetricResult
	// Patch sums the layer's changed coverable lines in this directory.
	PatchCovered, PatchTotal int64
	Touched                  bool
	Failing                  bool
	ID                       string
}

// Target is the row's target, falling back to the layer's.
func (p *PkgRow) Target() *float64 {
	if p.M.Target != nil {
		return p.M.Target
	}
	return p.Layer.Totals[p.Layer.Metrics[0]].Target
}

// PkgTable is every package of one layer, most uncovered first.
type PkgTable struct {
	Layer *report.Layer
	Rows  []*PkgRow
	// Full counts the packages at 100%, which tables may leave out.
	Full int
}

func (v *View) packages() {
	changedIn := map[string]map[string][2]int64{} // layer -> dir -> covered,total
	if v.R.Patch.Status != report.StatusNotComputed {
		for _, pl := range v.R.Patch.Layers {
			for _, f := range pl.Files {
				d := path.Dir(f.Path)
				if changedIn[pl.Layer] == nil {
					changedIn[pl.Layer] = map[string][2]int64{}
				}
				c := changedIn[pl.Layer][d]
				changedIn[pl.Layer][d] = [2]int64{c[0] + f.Covered, c[1] + f.Total}
			}
		}
	}
	var best *PkgTable
	var bestTotal int64
	for li := range v.R.Layers {
		l := &v.R.Layers[li]
		if l.Status == report.StatusNotMeasured || len(l.Packages) == 0 {
			continue
		}
		t := &PkgTable{Layer: l}
		primary := l.Metrics[0]
		for gi := range l.Packages {
			g := &l.Packages[gi]
			pr := &PkgRow{Layer: l, G: g, M: g.Totals[primary], ID: "pkg-" + l.ID + "-" + anchor(g.Key)}
			pr.Failing = g.Status == report.StatusFail
			if c, ok := changedIn[l.ID][g.Key]; ok {
				pr.Touched = true
				pr.PatchCovered, pr.PatchTotal = c[0], c[1]
			}
			if pr.M.Total > 0 && pr.M.Covered == pr.M.Total {
				t.Full++
			}
			t.Rows = append(t.Rows, pr)
			if pr.Touched && !l.ReportOnly {
				v.Touched = append(v.Touched, pr)
			}
		}
		sort.SliceStable(t.Rows, func(i, j int) bool {
			a, b := t.Rows[i].M, t.Rows[j].M
			if uncovered(a) != uncovered(b) {
				return uncovered(a) > uncovered(b)
			}
			return t.Rows[i].G.Key < t.Rows[j].G.Key
		})
		v.Packages = append(v.Packages, t)
		if l.ReportOnly || len(l.Packages) < 2 {
			continue
		}
		if l.Treemap && (best == nil || !best.Layer.Treemap) {
			best, bestTotal = t, 1<<62
			continue
		}
		if tot := l.Totals[primary].Total; tot > bestTotal {
			best, bestTotal = t, tot
		}
	}
	v.Treemap = best
}

// Uncovered on a metric result.
func uncovered(m report.MetricResult) int64 { return m.Total - m.Covered }

// anchor makes an id-safe fragment from a path.
func anchor(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// GlobTable is one layer's glob rows.
type GlobTable struct {
	Layer *report.Layer
	Rows  []*report.Group
}

// LowestTable is one layer's lowest files.
type LowestTable struct {
	Layer *report.Layer
	Files []report.FileSummary
}

// ExclusionRow is one exclude.txt rule with its size per layer.
type ExclusionRow struct {
	Glob, Reason string
	Sizes        []Size
}

// Size is how much one rule or scope glob removed from one layer.
type Size struct {
	Layer  string
	Files  int
	Metric string
	N      int64
}

// ScopeRow is what a layer's own include/exclude kept out.
type ScopeRow struct {
	Layer *report.Layer
	Kind  string
	Glob  string
	Size  Size
}

func (v *View) rest() {
	for li := range v.R.Layers {
		l := &v.R.Layers[li]
		if l.Status == report.StatusNotMeasured {
			continue
		}
		v.Measured = append(v.Measured, l)
		if len(l.Globs) > 0 {
			t := &GlobTable{Layer: l}
			for gi := range l.Globs {
				t.Rows = append(t.Rows, &l.Globs[gi])
			}
			v.Globs = append(v.Globs, t)
		}
		if len(l.LowestFiles) > 0 {
			v.Lowest = append(v.Lowest, &LowestTable{Layer: l, Files: l.LowestFiles})
		}
		for _, s := range l.Scope {
			if s.Files == 0 {
				continue
			}
			m := l.Metrics[0]
			v.Scope = append(v.Scope, &ScopeRow{Layer: l, Kind: s.Kind, Glob: s.Glob, Size: Size{Layer: l.ID, Files: s.Files, Metric: m, N: s.Counts[m].Total}})
		}
	}
	for _, e := range v.R.Exclusions {
		row := &ExclusionRow{Glob: e.Glob, Reason: e.Reason}
		for _, s := range e.Layers {
			m := "lines"
			if l := v.layers[s.Layer]; l != nil {
				m = l.Metrics[0]
			}
			row.Sizes = append(row.Sizes, Size{Layer: s.Layer, Files: s.Files, Metric: m, N: s.Counts[m].Total})
			v.Excluded[m] += s.Counts[m].Total
		}
		v.Exclusions = append(v.Exclusions, row)
	}
	if e := v.R.Endpoints; e != nil {
		v.Endpoints = e
		for _, p := range v.Problems {
			if p.Kind != "endpoints" {
				continue
			}
			for _, vi := range p.Violations {
				v.EndpointViolations[EndpointKind(vi.ID)]++
			}
		}
	}
}

// EndpointKind reads the kind out of an endpoint id ("svc-x:POST /a" is a
// route; "svc-x:event y" an event; "app:page /p" a page): the registry's id
// grammar is <surface>:<METHOD-or-KIND> <target>.
func EndpointKind(id string) string {
	_, rest, ok := strings.Cut(id, ":")
	if !ok {
		return ""
	}
	word, _, _ := strings.Cut(rest, " ")
	switch word {
	case "event", "cron", "page", "layout", "action":
		return word
	}
	return "route"
}

// ExcludedSummary is "6,539 statements and 759 lines".
func (v *View) ExcludedSummary() string {
	var parts []string
	for _, m := range []string{"statements", "lines", "branches", "functions"} {
		if n := v.Excluded[m]; n > 0 {
			parts = append(parts, Unit(m, n))
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// Title is the PR (or commit) this report is for, in words.
func (v *View) Subject() string {
	m := v.R.Metadata
	switch {
	case m.PR != 0:
		return fmt.Sprintf("#%d", m.PR)
	case m.SHA != "":
		return Short7(m.SHA)
	}
	return "this run"
}

// PushSHA identifies a push: the PR's head commit when known, else the
// commit measured.
func (v *View) PushSHA() string {
	if v.R.Metadata.HeadSHA != "" {
		return v.R.Metadata.HeadSHA
	}
	return v.R.Metadata.SHA
}

// InThisChange is " in this PR" on a pull request run, "" otherwise: where
// a floor would be lowered, in the surfaces' advice.
func (v *View) InThisChange() string {
	if v.R.Metadata.PR != 0 {
		return " in this PR"
	}
	return ""
}

// BaseRef is the branch compared against, "main" when unknown.
func (v *View) BaseRef() string {
	if v.R.Metadata.BaseRef != "" {
		return v.R.Metadata.BaseRef
	}
	return "the base branch"
}

// FloorKey is where a failure's floor lives in floors.json, for prose.
func FloorKey(f report.Failure) string {
	switch f.Scope {
	case report.FailPackage:
		return fmt.Sprintf("packages.%s.%q.%s", f.Layer, f.Key, f.Metric)
	case report.FailGlob:
		return fmt.Sprintf("globs.%s.%q.%s", f.Layer, f.Key, f.Metric)
	}
	return fmt.Sprintf("layers.%s.%s", f.Layer, f.Metric)
}

// Subject and Predicate phrase a problem for prose: "app lines" / "is at
// 71.84%, 0.16 below its 72.0 floor". Every surface builds its sentence
// from these two, bold or plain, so they cannot word one failure two ways.
func (p *Problem) Subject() string {
	short := p.Failure.Layer
	if p.Layer != nil && p.Layer.ShortLabel != "" {
		short = p.Layer.ShortLabel
	}
	switch p.Kind {
	case "floor":
		if p.Row != nil {
			return p.Row.Label
		}
	case "nodata":
		return short + " " + p.Failure.Metric
	case "package", "glob":
		return p.Kind + " " + p.Failure.Key + " (" + short + ")"
	case "patch":
		return short + " patch coverage"
	case "endpoints":
		return "the endpoint baseline"
	}
	return short
}

// Predicate is the problem's verb phrase.
func (p *Problem) Predicate(target float64) string {
	f := p.Failure
	switch p.Kind {
	case "floor":
		if p.Row != nil {
			return fmt.Sprintf("is at %s%%, %s below its %s floor", Pct2(p.Row.Pct), strings.TrimPrefix(Signed(p.Row.Delta), "-"), Floor(p.Row.Floor))
		}
	case "nodata":
		return fmt.Sprintf("has a %s floor but no data: its collector stopped producing it", Floor(f.Floor))
	case "package", "glob":
		return fmt.Sprintf("is at %s%%, below its %s floor", Pct2(f.Measured), Floor(f.Floor))
	case "patch":
		return fmt.Sprintf("is %s, under the %s%% target, and the patch gate blocks", Pct1(*f.Measured), Trim(target))
	case "required":
		what := "did not report: none of its inputs exist"
		if i := strings.LastIndex(f.Message, " ("); i >= 0 && strings.HasSuffix(f.Message, ")") {
			what += " " + f.Message[i+1:]
		}
		return what
	case "endpoints":
		return fmt.Sprintf("is broken: %s", Plural(int64(len(p.Violations)), "endpoint gained a gap", "endpoints gained a gap"))
	}
	return strings.TrimPrefix(f.Message, f.Layer+" ")
}

// PatchNote says how the changed lines a problem's layer measures fared:
// "4 of the 16 changed lines it measures have no test". "" when the layer
// had no changed lines or no diff was analysed.
func (v *View) PatchNote(p *Problem) string {
	if p.Layer == nil || v.Patch == nil {
		return ""
	}
	pl := v.patchOf(p.Layer.ID)
	if pl == nil || pl.Total == 0 {
		if p.Kind == "floor" || p.Kind == "package" || p.Kind == "glob" {
			return "This change adds no line it measures, so the drop comes from code it removed or tests it changed."
		}
		return ""
	}
	if pl.Covered == pl.Total {
		return fmt.Sprintf("All %s it measures ran, so the drop comes from code it removed or tests it changed.", Plural(pl.Total, "changed line", "changed lines"))
	}
	if pl.Covered == 0 {
		if pl.Total == 1 {
			return "The one changed line it measures never ran."
		}
		return fmt.Sprintf("None of the %s it measures ran.", Plural(pl.Total, "changed line", "changed lines"))
	}
	return fmt.Sprintf("%d of the %s it measures have no test.", pl.Total-pl.Covered, Plural(pl.Total, "changed line", "changed lines"))
}

// Toward is one layer a file's changed lines count toward.
type Toward struct {
	Label string
	Fails bool
}

// CountsToward names the layers a file counts toward, marking the ones its
// lines break: "app lines, below floor".
func (v *View) CountsToward(f *File) []Toward {
	var out []Toward
	for _, l := range f.Layers {
		t := Toward{Label: l.ShortLabel}
		if f.Failing[l.ID] {
			t.Fails = true
			for _, p := range v.Problems {
				if p.Layer == nil || p.Layer.ID != l.ID || !p.problemCovers(v, f) {
					continue
				}
				switch p.Kind {
				case "floor":
					t.Label = p.Row.Label + ", below floor"
				case "package":
					t.Label = l.ShortLabel + ", package below floor"
				case "glob":
					t.Label = l.ShortLabel + ", glob below floor"
				case "patch":
					t.Label = l.ShortLabel + ", patch blocks"
				}
				break
			}
		}
		if f.ReportOnly {
			t.Label += " (report-only)"
		}
		out = append(out, t)
	}
	return out
}

// Uncovered counts the changed lines that never ran, over every file.
func (v *View) Uncovered() int64 {
	n := int64(0)
	for _, f := range v.Files {
		if !f.ReportOnly {
			n += f.Missing()
		}
	}
	return n
}

// UncoveredFiles are the files with changed lines that never ran.
func (v *View) UncoveredFiles() []*File {
	var out []*File
	for _, f := range v.Files {
		if f.Missing() > 0 && !f.ReportOnly {
			out = append(out, f)
		}
	}
	return out
}

// RatchetDiff is the floors.json change a ratchet makes, as a diff of the
// entries it rewrites (at most max entries; 0 is all).
func (v *View) RatchetDiff(maxN int) string {
	lines := []string{"@@ " + v.R.FloorsFile + " @@"}
	section, layer := "", ""
	for i, e := range v.Ratchets {
		if maxN > 0 && i == maxN {
			lines = append(lines, fmt.Sprintf("#  … and %d more (the job summary lists every one)", len(v.Ratchets)-maxN))
			break
		}
		sec := map[string]string{"layer": "layers", "package": "packages", "glob": "globs"}[e.Scope]
		if sec != section {
			lines = append(lines, fmt.Sprintf(`   "%s": {`, sec))
			section, layer = sec, ""
		}
		indent := "    "
		key := e.Layer
		if e.Scope != "layer" {
			if e.Layer != layer {
				lines = append(lines, fmt.Sprintf(`     "%s": {`, e.Layer))
				layer = e.Layer
			}
			indent = "      "
			key = e.Key
		}
		if !e.New {
			lines = append(lines, "-"+indent+entryLine(key, e.Before))
		}
		lines = append(lines, "+"+indent+entryLine(key, e.After))
	}
	return strings.Join(lines, "\n")
}

// entryLine is one floors.json entry in the canonical form the ratchet
// writes: "key": { "metric": 1.0, ... } with sorted metrics.
func entryLine(key string, ms map[string]float64) string {
	names := make([]string, 0, len(ms))
	for m := range ms {
		names = append(names, m)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, m := range names {
		parts[i] = fmt.Sprintf("%q: %s", m, floors.FormatFloor(ms[m]))
	}
	return fmt.Sprintf("%q: { %s }", key, strings.Join(parts, ", "))
}
