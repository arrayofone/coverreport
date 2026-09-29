package report

import (
	"bytes"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/arrayofone/coverreport/internal/config"
	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/diff"
	"github.com/arrayofone/coverreport/internal/endpoints"
	"github.com/arrayofone/coverreport/internal/exclude"
	"github.com/arrayofone/coverreport/internal/floors"
	"github.com/arrayofone/coverreport/internal/glob"
	"github.com/arrayofone/coverreport/internal/gocov"
	"github.com/arrayofone/coverreport/internal/lcov"
	"github.com/arrayofone/coverreport/internal/paths"
)

// Input is everything one analysis reads. The CLI fills it from flags and
// files; tests fill it directly.
type Input struct {
	// Root is the repository root: repo-relative paths, go.work discovery
	// and the Go source line filter all start here.
	Root string
	// InputsDir is where the layers' input globs are resolved ("" = Root).
	// In CI it is the directory the coverage-* artifacts were downloaded to.
	InputsDir   string
	Config      *config.Resolved
	Floors      *floors.Floors
	FloorsFound bool
	Excludes    []exclude.Rule
	// Diff is the parsed diff for patch coverage; nil means "not computed"
	// (an empty, non-nil slice is a diff with no changes).
	Diff     []diff.File
	Metadata Metadata
	// Require names layers that must have been measured; a required layer
	// whose inputs matched nothing is a failure instead of "not_measured".
	Require []string
	Now     time.Time
	// Baseline is an earlier report from the base branch; a layer not
	// measured in this run carries its totals from it, for display
	// (BaselineFile names it in the report).
	Baseline     *Report
	BaselineFile string
	// Endpoints is the endpoint registry; its baseline violations become
	// failures (EndpointsFile names it in the report).
	Endpoints     *endpoints.File
	EndpointsFile string
}

// layerData is one layer's files after scope and exclusions.
type layerData struct {
	cfg      *config.ResolvedLayer
	measured bool
	files    map[string]*coverage.File
	order    []string // sorted keys of files
}

type analyzer struct {
	in    Input
	fl    *floors.Floors
	res   *paths.Resolver
	rep   *Report
	data  []*layerData
	sizes []map[string]*ExclusionSize // per exclude rule, per layer id
}

// Analyze computes a report.
func Analyze(in Input) (*Report, error) {
	if in.Config == nil {
		return nil, fmt.Errorf("report: no config")
	}
	if in.Floors == nil {
		in.Floors = floors.New()
	}
	if in.InputsDir == "" {
		in.InputsDir = in.Root
	}
	res, warns, err := paths.NewResolver(in.Root, in.Config.GoModules)
	if err != nil {
		return nil, err
	}
	a := &analyzer{
		in:    in,
		fl:    in.Floors,
		res:   res,
		sizes: make([]map[string]*ExclusionSize, len(in.Excludes)),
		rep: &Report{
			Version:        Version,
			Tool:           "coverreport",
			GeneratedAt:    in.Now.UTC().Format(time.RFC3339),
			Metadata:       in.Metadata,
			Failures:       []Failure{},
			TolerancePts:   in.Floors.TolerancePts,
			FloorsFile:     in.Config.Floors,
			FloorsFound:    in.FloorsFound,
			ExcludeFile:    in.Config.Exclude,
			Layers:         []Layer{},
			Exclusions:     []Exclusion{},
			Ratchet:        []floors.Change{},
			StaleFloors:    []floors.Ref{},
			Warnings:       []string{},
			Brand:          in.Config.Brand,
			RatchetCommand: in.Config.RatchetCommand,
		},
	}
	for _, w := range in.Config.Warnings {
		a.warn("%s", w)
	}
	for _, w := range warns {
		a.warn("%s", w)
	}
	for i := range a.sizes {
		a.sizes[i] = map[string]*ExclusionSize{}
	}
	for _, l := range in.Config.Layers {
		if l.Format == config.FormatGo {
			a.rep.GoModules = res.Modules()
			break
		}
	}
	a.checkFloorsKnown()
	for _, l := range in.Config.Layers {
		ld, lr, err := a.loadLayer(l)
		if err != nil {
			return nil, fmt.Errorf("layer %s: %w", l.ID, err)
		}
		a.data = append(a.data, ld)
		a.rep.Layers = append(a.rep.Layers, *lr)
	}
	for i := range a.rep.Layers {
		a.evaluate(a.data[i], &a.rep.Layers[i])
	}
	a.rep.Patch = a.patch()
	a.exclusions()
	a.carry()
	a.endpoints()
	for _, id := range in.Require {
		if err := a.rep.Require(id, in.Config); err != nil {
			return nil, err
		}
	}
	a.rep.finalize()
	return a.rep, nil
}

func (a *analyzer) warn(format string, args ...any) {
	a.rep.Warnings = append(a.rep.Warnings, fmt.Sprintf(format, args...))
}

// Require marks a layer as required: when it was not measured, that becomes
// a failure. It is exported so check --report can apply --require to a
// report produced earlier.
func (r *Report) Require(id string, cfg *config.Resolved) error {
	var l *Layer
	for i := range r.Layers {
		if r.Layers[i].ID == id {
			l = &r.Layers[i]
		}
	}
	if l == nil {
		return fmt.Errorf("--require %s: no such layer", id)
	}
	if l.Status != StatusNotMeasured {
		return nil
	}
	for _, f := range r.Failures {
		if f.Scope == FailRequired && f.Layer == id {
			return nil
		}
	}
	msg := fmt.Sprintf("layer %s is required but none of its inputs exist", id)
	if cfg != nil {
		if cl := cfg.Layer(id); cl != nil {
			msg += " (" + strings.Join(cl.Inputs, ", ") + ")"
		}
	}
	r.Failures = append(r.Failures, Failure{Scope: FailRequired, Layer: id, Message: msg})
	r.finalize()
	return nil
}

func (r *Report) finalize() {
	r.Status = StatusPass
	if len(r.Failures) > 0 {
		r.Status = StatusFail
	}
	failed := map[string]bool{}
	for _, f := range r.Failures {
		switch f.Scope {
		case FailLayer, FailPackage, FailGlob:
			failed[f.Layer] = true
		}
	}
	for i := range r.Layers {
		l := &r.Layers[i]
		switch {
		case l.Status == StatusNotMeasured || l.Status == StatusReportOnly:
		case failed[l.ID]:
			l.Status = StatusFail
		default:
			l.Status = StatusOK
		}
	}
}

// checkFloorsKnown warns about floors that name a layer the config does not
// have: they are never checked, which a reader of floors.json would not
// guess.
func (a *analyzer) checkFloorsKnown() {
	cfg := a.in.Config
	check := func(section string, ids []string) {
		for _, id := range ids {
			l := cfg.Layer(id)
			switch {
			case l == nil:
				a.warn("%s: %s.%s names no configured layer; not checked", a.in.Config.Floors, section, id)
			case l.ReportOnly:
				a.warn("%s: %s.%s: layer is report_only; its floors are ignored", a.in.Config.Floors, section, id)
			}
		}
	}
	check("layers", keys(a.fl.Layers))
	check("packages", keys(a.fl.Packages))
	check("globs", keys(a.fl.Globs))
}

// rawFile is one resolved file before scoping: its counts, and a way to get
// its line map that is only paid for files that survive the filters.
type rawFile struct {
	counts map[coverage.Metric]coverage.Count
	lines  func() map[int]bool
}

func (a *analyzer) loadLayer(l *config.ResolvedLayer) (*layerData, *Layer, error) {
	lr := &Layer{
		ID:          l.ID,
		Label:       l.Label,
		Description: l.Description,
		Format:      l.Format,
		ReportOnly:  l.ReportOnly,
		Status:      StatusNotMeasured,
		Inputs:      []string{},
		Metrics:     metricStrings(l.MetricList),
		Totals:      map[string]MetricResult{},
		Packages:    []Group{},
		Globs:       []Group{},
		LowestFiles: []FileSummary{},
		Scope:       []ScopeExclusion{},
		ShortLabel:  l.ShortLabel,
		Group:       l.Group,
		Treemap:     l.Treemap,
	}
	ld := &layerData{cfg: l, files: map[string]*coverage.File{}}
	inputs, unmatched, err := findInputs(a.in.InputsDir, l.InputGlobs)
	if err != nil {
		return nil, nil, err
	}
	if len(inputs) == 0 {
		return ld, lr, nil
	}
	for _, u := range unmatched {
		a.warn("layer %s: input %s matched no files", l.ID, u)
	}
	ld.measured = true
	lr.Inputs = inputs
	if l.ReportOnly {
		lr.Status = StatusReportOnly
	} else {
		lr.Status = StatusOK
	}

	var raw map[string]*rawFile
	var unmapped []string
	switch l.Format {
	case config.FormatGo:
		raw, unmapped, lr.Modes, err = a.loadGo(inputs)
	case config.FormatLCOV:
		raw, unmapped, err = a.loadLCOV(l, inputs)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(unmapped) > 0 {
		hint := "check go_modules (or go.work)"
		if l.Format == config.FormatLCOV {
			hint = "check paths.prefix / paths.strip"
		}
		what := fmt.Sprintf("%d files could not be mapped to a repo path and were dropped", len(unmapped))
		if len(unmapped) == 1 {
			what = "1 file could not be mapped to a repo path and was dropped"
		}
		a.warn("layer %s: %s (e.g. %s); %s", l.ID, what, unmapped[0], hint)
	}

	notIncluded := &ScopeExclusion{Kind: ScopeNotIncluded, Counts: map[string]coverage.Count{}}
	scoped := make([]*ScopeExclusion, len(l.ExcludeGlobs))
	for i, g := range l.ExcludeGlobs {
		scoped[i] = &ScopeExclusion{Kind: ScopeExcluded, Glob: g.String(), Counts: map[string]coverage.Count{}}
	}
	for _, rel := range keys(raw) {
		rf := raw[rel]
		if len(l.IncludeGlobs) > 0 && firstMatch(l.IncludeGlobs, rel) < 0 {
			addCounts(&notIncluded.Files, notIncluded.Counts, rf.counts, l.MetricList)
			continue
		}
		if i := firstMatch(l.ExcludeGlobs, rel); i >= 0 {
			addCounts(&scoped[i].Files, scoped[i].Counts, rf.counts, l.MetricList)
			continue
		}
		if i := exclude.First(a.in.Excludes, rel); i >= 0 {
			sz := a.sizes[i][l.ID]
			if sz == nil {
				sz = &ExclusionSize{Layer: l.ID, Counts: map[string]coverage.Count{}}
				a.sizes[i][l.ID] = sz
			}
			addCounts(&sz.Files, sz.Counts, rf.counts, l.MetricList)
			continue
		}
		ld.files[rel] = &coverage.File{Path: rel, Counts: rf.counts, Lines: rf.lines()}
		ld.order = append(ld.order, rel)
	}
	if len(l.IncludeGlobs) > 0 {
		lr.Scope = append(lr.Scope, *notIncluded)
	}
	for _, s := range scoped {
		lr.Scope = append(lr.Scope, *s)
	}
	lr.Files = len(ld.order)
	return ld, lr, nil
}

func (a *analyzer) loadGo(inputs []string) (map[string]*rawFile, []string, []string, error) {
	set := gocov.NewSet()
	for _, in := range inputs {
		data, err := os.ReadFile(filepath.Join(a.in.InputsDir, filepath.FromSlash(in)))
		if err != nil {
			return nil, nil, nil, err
		}
		if err := set.Parse(bytes.NewReader(data), in); err != nil {
			return nil, nil, nil, err
		}
	}
	byRel := map[string][][]gocov.Block{}
	var unmapped []string
	for _, name := range set.Files() {
		rel, ok := a.res.GoFile(name)
		if !ok {
			unmapped = append(unmapped, name)
			continue
		}
		byRel[rel] = append(byRel[rel], set.Blocks(name))
	}
	raw := map[string]*rawFile{}
	for rel, lists := range byRel {
		blocks := lists[0]
		if len(lists) > 1 {
			var err error
			if blocks, err = gocov.MergeBlocks(lists...); err != nil {
				return nil, nil, nil, fmt.Errorf("%s: %w", rel, err)
			}
		}
		cov, tot := gocov.Statements(blocks)
		root := a.in.Root
		raw[rel] = &rawFile{
			counts: map[coverage.Metric]coverage.Count{coverage.Statements: {Covered: cov, Total: tot}},
			lines: func() map[int]bool {
				var trivial map[int]bool
				if src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
					trivial = gocov.TrivialLines(src)
				}
				return gocov.LineMap(blocks, trivial)
			},
		}
	}
	return raw, unmapped, keys(set.Modes), nil
}

func (a *analyzer) loadLCOV(l *config.ResolvedLayer, inputs []string) (map[string]*rawFile, []string, error) {
	set := lcov.NewSet()
	for _, in := range inputs {
		data, err := os.ReadFile(filepath.Join(a.in.InputsDir, filepath.FromSlash(in)))
		if err != nil {
			return nil, nil, err
		}
		if err := set.Parse(bytes.NewReader(data), in); err != nil {
			return nil, nil, err
		}
	}
	byRel := map[string]*lcov.File{}
	var unmapped []string
	for _, name := range set.Names() {
		rel, ok := a.res.LCOVFile(name, l.Prefix, l.Strip)
		if !ok {
			unmapped = append(unmapped, name)
			continue
		}
		if cur := byRel[rel]; cur != nil {
			cur.Merge(set.Files[name])
		} else {
			byRel[rel] = set.Files[name]
		}
	}
	raw := map[string]*rawFile{}
	for rel, f := range byRel {
		raw[rel] = &rawFile{
			counts: map[coverage.Metric]coverage.Count{
				coverage.Lines:     f.LineCount(),
				coverage.Branches:  f.BranchCount(),
				coverage.Functions: f.FuncCount(),
			},
			lines: func() map[int]bool {
				m := make(map[int]bool, len(f.Lines))
				for ln, h := range f.Lines {
					m[ln] = h > 0
				}
				return m
			},
		}
	}
	return raw, unmapped, nil
}

// findInputs resolves a layer's input globs under dir. Each glob walks only
// from its literal root, so "coverage-go/*.out" never descends into
// node_modules. Returns sorted, de-duplicated, dir-relative paths, and the
// globs that matched nothing.
func findInputs(dir string, globs []*glob.Pattern) ([]string, []string, error) {
	seen := map[string]bool{}
	var out, unmatched []string
	for _, g := range globs {
		n := 0
		if g.Literal() {
			if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(g.String()))); err == nil && st.Mode().IsRegular() {
				rel := path.Clean(strings.TrimPrefix(g.String(), "./"))
				if !seen[rel] {
					seen[rel] = true
					out = append(out, rel)
				}
				n++
			}
		} else {
			for _, root := range g.Roots() {
				start := filepath.Join(dir, filepath.FromSlash(root))
				if _, err := os.Stat(start); err != nil {
					continue
				}
				err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.Type().IsRegular() {
						return nil
					}
					rel, err := filepath.Rel(dir, p)
					if err != nil {
						return err
					}
					rel = filepath.ToSlash(rel)
					if g.Match(rel) {
						n++
						if !seen[rel] {
							seen[rel] = true
							out = append(out, rel)
						}
					}
					return nil
				})
				if err != nil {
					return nil, nil, err
				}
			}
		}
		if n == 0 {
			unmatched = append(unmatched, g.String())
		}
	}
	sort.Strings(out)
	return out, unmatched, nil
}

// evaluate fills a layer's totals, packages, globs, lowest files and ratchet
// proposals, and records failures.
func (a *analyzer) evaluate(ld *layerData, lr *Layer) {
	l := ld.cfg
	tot := map[coverage.Metric]coverage.Count{}
	for _, p := range ld.order {
		for _, m := range l.MetricList {
			tot[m] = tot[m].Add(ld.files[p].Counts[m])
		}
	}
	for _, m := range l.MetricList {
		ref := floors.Ref{Scope: floors.ScopeLayer, Layer: l.ID, Metric: string(m)}
		lr.Totals[string(m)] = a.result(ld, ref, tot[m], targetOf(l.TargetMap, m), true)
	}
	a.unknownMetrics(l, floors.ScopeLayer, "", a.fl.Layers[l.ID])
	if !ld.measured {
		return
	}

	// Packages: one row per directory holding at least one kept file.
	dirs := map[string][]string{}
	for _, p := range ld.order {
		d := path.Dir(p)
		dirs[d] = append(dirs[d], p)
	}
	for _, d := range keys(dirs) {
		g := a.group(ld, floors.ScopePackage, d, "", dirs[d], l.PkgTargets, l.PkgFloors)
		lr.Packages = append(lr.Packages, g)
	}
	for _, d := range keys(a.fl.Packages[l.ID]) {
		if _, ok := dirs[d]; !ok && !l.ReportOnly {
			a.rep.StaleFloors = append(a.rep.StaleFloors, floors.Ref{Scope: floors.ScopePackage, Layer: l.ID, Key: d})
		}
	}

	// Globs: the configured rows, then any glob that only floors.json names
	// (so a floor is checked even after its row left the config).
	type row struct {
		pat     *glob.Pattern
		label   string
		targets map[coverage.Metric]float64
		tracked bool
	}
	var rows []row
	inConfig := map[string]bool{}
	for _, g := range l.GlobRows {
		rows = append(rows, row{g.Pattern, g.Label, g.Targets, true})
		inConfig[g.Pattern.String()] = true
	}
	for _, g := range keys(a.fl.Globs[l.ID]) {
		if !inConfig[g] {
			rows = append(rows, row{glob.MustCompile(g), "", nil, false})
		}
	}
	for _, r := range rows {
		var members []string
		for _, p := range ld.order {
			if r.pat.Match(p) {
				members = append(members, p)
			}
		}
		g := a.group(ld, floors.ScopeGlob, r.pat.String(), r.label, members, r.targets, r.tracked)
		lr.Globs = append(lr.Globs, g)
		if len(members) == 0 && !l.ReportOnly {
			if _, has := a.fl.Globs[l.ID][r.pat.String()]; has {
				a.rep.StaleFloors = append(a.rep.StaleFloors, floors.Ref{Scope: floors.ScopeGlob, Layer: l.ID, Key: r.pat.String()})
			}
		}
	}

	lr.LowestFiles = a.lowest(ld)
}

// group builds one package or glob row.
func (a *analyzer) group(ld *layerData, scope floors.Scope, key, label string, members []string, targets map[coverage.Metric]float64, tracked bool) Group {
	l := ld.cfg
	g := Group{Key: key, Label: label, Files: len(members), Totals: map[string]MetricResult{}}
	tot := map[coverage.Metric]coverage.Count{}
	for _, p := range members {
		for _, m := range l.MetricList {
			tot[m] = tot[m].Add(ld.files[p].Counts[m])
		}
	}
	if scope == floors.ScopePackage && tracked && tot[l.MetricList[0]].Total < l.PkgMinSize {
		tracked = false
	}
	var entry floors.Metrics
	if scope == floors.ScopePackage {
		entry = a.fl.Packages[l.ID][key]
	} else {
		entry = a.fl.Globs[l.ID][key]
	}
	a.unknownMetrics(l, scope, key, entry)
	statuses := map[string]bool{}
	for _, m := range l.MetricList {
		ref := floors.Ref{Scope: scope, Layer: l.ID, Key: key, Metric: string(m)}
		mr := a.result(ld, ref, tot[m], targetOf(targets, m), tracked)
		g.Totals[string(m)] = mr
		statuses[mr.Status] = true
	}
	switch {
	case l.ReportOnly:
		g.Status = StatusReportOnly
	case statuses[StatusFail]:
		g.Status = StatusFail
	case statuses[StatusOK]:
		g.Status = StatusOK
	case statuses[StatusNoFloor]:
		g.Status = StatusNoFloor
	default:
		g.Status = StatusNoData
	}
	return g
}

// unknownMetrics warns about floors for metrics the layer does not measure.
func (a *analyzer) unknownMetrics(l *config.ResolvedLayer, scope floors.Scope, key string, entry floors.Metrics) {
	for _, m := range keys(entry) {
		if !slices.Contains(l.MetricList, coverage.Metric(m)) {
			where := string(scope) + " " + l.ID
			if key != "" {
				where += " " + key
			}
			a.warn("%s: %s floor for %q: the layer does not measure %s; not checked", a.in.Config.Floors, where, m, m)
		}
	}
}

// result evaluates one metric of one row against its floor, and proposes a
// ratchet. tracked says whether a missing floor should be created; an
// existing floor is always eligible to rise.
func (a *analyzer) result(ld *layerData, ref floors.Ref, c coverage.Count, target *float64, tracked bool) MetricResult {
	l := ld.cfg
	mr := MetricResult{Covered: c.Covered, Total: c.Total, Target: target}
	fv, hasFloor := a.fl.Get(ref)
	if l.ReportOnly {
		hasFloor = false
	}
	if hasFloor {
		mr.Floor = ptr(fv)
	}
	switch {
	case !ld.measured:
		mr.Status = StatusNotMeasured
		return mr
	case c.Total == 0:
		mr.Status = StatusNoData
		if hasFloor {
			if ref.Scope == floors.ScopeLayer {
				a.fail(ref, nil, mr.Floor, nil, fmt.Sprintf("%s %s has a floor of %s but no data: the collector stopped producing it", l.ID, ref.Metric, floors.FormatFloor(fv)))
			} else {
				a.warn("%s: %s %s %s %s: floor %s set but the row has no %s data", a.in.Config.Floors, ref.Scope, l.ID, ref.Key, ref.Metric, floors.FormatFloor(fv), ref.Metric)
			}
		}
		return mr
	}
	mr.Pct = ptr(c.Pct2())
	if l.ReportOnly {
		mr.Status = StatusReportOnly
		return mr
	}
	if hasFloor {
		mr.Delta = ptr(math.Round((c.Pct()-fv)*100) / 100)
		threshold := coverage.Hundredths(fv) - coverage.Hundredths(a.fl.TolerancePts)
		if c.AtLeast(threshold) {
			mr.Status = StatusOK
		} else {
			mr.Status = StatusFail
			thr := float64(threshold) / 100
			a.fail(ref, mr.Pct, mr.Floor, &thr, fmt.Sprintf("%s is %.2f%%, below its floor %s by more than the %s-point tolerance",
				describe(ref), c.Pct(), floors.FormatFloor(fv), trimFloat(a.fl.TolerancePts)))
		}
	} else {
		mr.Status = StatusNoFloor
	}
	down := c.FloorTenths()
	if (hasFloor && down > floors.Tenths(fv)) || (!hasFloor && tracked) {
		to := floors.FromTenths(down)
		mr.RatchetTo = ptr(to)
		ch := floors.Change{Ref: ref, To: to}
		if hasFloor {
			ch.From = ptr(fv)
		}
		a.rep.Ratchet = append(a.rep.Ratchet, ch)
	}
	return mr
}

func (a *analyzer) fail(ref floors.Ref, measured, floor, threshold *float64, msg string) {
	scope := FailLayer
	switch ref.Scope {
	case floors.ScopePackage:
		scope = FailPackage
	case floors.ScopeGlob:
		scope = FailGlob
	}
	a.rep.Failures = append(a.rep.Failures, Failure{
		Scope: scope, Layer: ref.Layer, Key: ref.Key, Metric: ref.Metric,
		Measured: measured, Floor: floor, Threshold: threshold, Message: msg,
	})
}

func describe(ref floors.Ref) string {
	switch ref.Scope {
	case floors.ScopePackage:
		return fmt.Sprintf("%s package %s %s", ref.Layer, ref.Key, ref.Metric)
	case floors.ScopeGlob:
		return fmt.Sprintf("%s glob %s %s", ref.Layer, ref.Key, ref.Metric)
	}
	return fmt.Sprintf("%s %s", ref.Layer, ref.Metric)
}

// lowest lists the files with the most uncovered units of the primary
// metric: the files where a test buys the most, which is what "lowest" is for
// (sorting by percentage would fill the list with one-statement files at 0%).
// Ties break on the lower percentage, then the path.
func (a *analyzer) lowest(ld *layerData) []FileSummary {
	primary := ld.cfg.MetricList[0]
	var cands []*coverage.File
	for _, p := range ld.order {
		f := ld.files[p]
		if c := f.Counts[primary]; c.Total > 0 && c.Uncovered() > 0 {
			cands = append(cands, f)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ci, cj := cands[i].Counts[primary], cands[j].Counts[primary]
		if ci.Uncovered() != cj.Uncovered() {
			return ci.Uncovered() > cj.Uncovered()
		}
		// ci.Covered/ci.Total < cj.Covered/cj.Total, exactly.
		if l, r := ci.Covered*cj.Total, cj.Covered*ci.Total; l != r {
			return l < r
		}
		return cands[i].Path < cands[j].Path
	})
	n := a.in.Config.LowestFiles
	if len(cands) < n {
		n = len(cands)
	}
	out := make([]FileSummary, 0, n)
	for _, f := range cands[:n] {
		c := f.Counts[primary]
		out = append(out, FileSummary{
			Path:           f.Path,
			Counts:         countsFor(f.Counts, ld.cfg.MetricList),
			Pct:            c.Pct2(),
			Uncovered:      c.Uncovered(),
			UncoveredLines: coverage.Ranges(f.LinesWhere(false)),
		})
	}
	return out
}

// exclusions assembles the exclude.txt table.
func (a *analyzer) exclusions() {
	allMeasured := true
	for _, ld := range a.data {
		if !ld.measured {
			allMeasured = false
		}
	}
	for i, rule := range a.in.Excludes {
		e := Exclusion{Glob: rule.Glob, Reason: rule.Reason, Line: rule.Line, Layers: []ExclusionSize{}}
		for _, ld := range a.data {
			if sz := a.sizes[i][ld.cfg.ID]; sz != nil {
				e.Layers = append(e.Layers, *sz)
			}
		}
		if len(e.Layers) == 0 && allMeasured {
			a.warn("%s:%d: %s matches no file in any layer; delete the line", a.in.Config.Exclude, rule.Line, rule.Glob)
		}
		a.rep.Exclusions = append(a.rep.Exclusions, e)
	}
}

// carry copies a baseline report's totals onto every layer this run did not
// measure, so a surface can say "92.73, from main" instead of nothing. It
// changes no status: carried numbers are never checked or ratcheted, because
// they describe a different commit.
func (a *analyzer) carry() {
	b := a.in.Baseline
	if b == nil {
		return
	}
	a.rep.Baseline = &BaselineRef{File: a.in.BaselineFile, SHA: b.Metadata.SHA, GeneratedAt: b.GeneratedAt}
	for i := range a.rep.Layers {
		l := &a.rep.Layers[i]
		if l.Status != StatusNotMeasured {
			continue
		}
		for _, bl := range b.Layers {
			if bl.ID != l.ID || bl.Status == StatusNotMeasured {
				continue
			}
			c := &Carried{SHA: b.Metadata.SHA, Totals: map[string]PatchCount{}}
			for _, m := range l.Metrics {
				if mr, ok := bl.Totals[m]; ok && mr.Total > 0 {
					c.Totals[m] = patchCount(coverage.Count{Covered: mr.Covered, Total: mr.Total})
				}
			}
			if len(c.Totals) > 0 {
				l.Carried = c
			}
		}
	}
}

// endpoints condenses the endpoint registry and turns each of its baseline
// violations into a failure: the registry's baseline is shrink-only, so an
// endpoint that gained a gap fails the same check a broken floor does.
func (a *analyzer) endpoints() {
	if a.in.Endpoints == nil {
		return
	}
	s := endpoints.Summarize(a.in.Endpoints, a.in.EndpointsFile)
	a.rep.Endpoints = s
	for _, v := range s.Violations {
		msg := "endpoint " + v.ID + " broke the endpoint baseline"
		if v.Detail != "" {
			msg += ": " + v.Detail
		}
		a.rep.Failures = append(a.rep.Failures, Failure{Scope: FailEndpoints, Key: v.ID, Message: msg})
	}
}

func firstMatch(ps []*glob.Pattern, p string) int {
	for i, g := range ps {
		if g.Match(p) {
			return i
		}
	}
	return -1
}

func addCounts(files *int, into map[string]coverage.Count, from map[coverage.Metric]coverage.Count, ms []coverage.Metric) {
	*files++
	for _, m := range ms {
		into[string(m)] = into[string(m)].Add(from[m])
	}
}

func countsFor(from map[coverage.Metric]coverage.Count, ms []coverage.Metric) map[string]coverage.Count {
	out := map[string]coverage.Count{}
	for _, m := range ms {
		out[string(m)] = from[m]
	}
	return out
}

func metricStrings(ms []coverage.Metric) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m)
	}
	return out
}

func targetOf(t map[coverage.Metric]float64, m coverage.Metric) *float64 {
	if v, ok := t[m]; ok {
		return ptr(v)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
