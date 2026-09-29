package report

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arrayofone/coverreport/internal/config"
	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/diff"
	"github.com/arrayofone/coverreport/internal/exclude"
	"github.com/arrayofone/coverreport/internal/floors"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

// FixedNow is the generated_at every golden report carries.
var FixedNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// FixtureMetadata is the metadata every golden report carries.
var FixtureMetadata = Metadata{
	Repo:    "acme/demo",
	PR:      42,
	SHA:     "3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d",
	Base:    "1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d",
	HeadRef: "feat/calc",
	BaseRef: "main",
	RunURL:  "https://github.com/acme/demo/actions/runs/1234",
}

// fixture loads testdata/repo exactly as the CLI would: its config, floors
// and exclude.txt, the artifacts in testdata/artifacts, and the real git
// diff in testdata/change.diff.
func fixture(t *testing.T) Input {
	t.Helper()
	cfg, err := config.Load("../../testdata/repo/coverage/config.json")
	if err != nil {
		t.Fatal(err)
	}
	fl, found, err := floors.Load("../../testdata/repo/coverage/floors.json")
	if err != nil || !found {
		t.Fatal(err, found)
	}
	rules, err := exclude.Load("../../testdata/repo/coverage/exclude.txt")
	if err != nil {
		t.Fatal(err)
	}
	df, err := os.Open("../../testdata/change.diff")
	if err != nil {
		t.Fatal(err)
	}
	defer df.Close()
	d, err := diff.Parse(df)
	if err != nil {
		t.Fatal(err)
	}
	return Input{
		Root: "../../testdata/repo", InputsDir: "../../testdata/artifacts",
		Config: cfg, Floors: fl, FloorsFound: true, Excludes: rules, Diff: d,
		Metadata: FixtureMetadata, Now: FixedNow,
	}
}

func analyze(t *testing.T, in Input) *Report {
	t.Helper()
	r, err := Analyze(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Golden compares got with a golden file, or rewrites it under -update.
func Golden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./... -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		gl, wl := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var g, w string
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if g != w {
				t.Fatalf("%s differs at line %d:\n got: %s\nwant: %s\n(a golden diff is a behaviour change: review it, then -update)", path, i+1, g, w)
			}
		}
	}
}

// The whole model, byte for byte. Everything below it asserts the same
// numbers by name, so a regenerated golden cannot quietly absorb a change
// in meaning.
func TestGoldenReport(t *testing.T) {
	data, err := analyze(t, fixture(t)).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	Golden(t, "../../testdata/golden/report.json", data)
}

func layer(r *Report, id string) *Layer {
	for i := range r.Layers {
		if r.Layers[i].ID == id {
			return &r.Layers[i]
		}
	}
	return nil
}

func TestFixtureTotalsAndStatuses(t *testing.T) {
	r := analyze(t, fixture(t))
	cases := []struct {
		layer, metric  string
		covered, total int64
		status         string
	}{
		// calc.go only: store_pg.go is out of go-unit's scope, gen_templ.go
		// and svc/api/main.go are excluded.
		{"go-unit", "statements", 8, 13, StatusFail},
		{"go-sql", "statements", 2, 3, StatusNoFloor},
		{"go-live", "statements", 10, 16, StatusOK},
		{"go-e2e", "statements", 10, 16, StatusReportOnly},
		// Two shards of money.ts merged; ui/card.tsx excluded.
		{"web", "lines", 7, 8, StatusOK},
		{"web", "branches", 3, 4, StatusOK},
		{"web", "functions", 3, 4, StatusNoFloor},
		// db.g.dart excluded; the /opt/flutter file unmapped and dropped.
		{"mobile", "lines", 2, 3, StatusNoFloor},
		{"edge", "lines", 0, 0, StatusNotMeasured},
	}
	for _, tc := range cases {
		l := layer(r, tc.layer)
		mr := l.Totals[tc.metric]
		if mr.Covered != tc.covered || mr.Total != tc.total || mr.Status != tc.status {
			t.Errorf("%s %s = %d/%d %s, want %d/%d %s", tc.layer, tc.metric, mr.Covered, mr.Total, mr.Status, tc.covered, tc.total, tc.status)
		}
	}
	// A layer that was not measured still shows its floor and target, and
	// being absent is not a failure (a path-filtered job did not run).
	edge := layer(r, "edge").Totals["lines"]
	if edge.Floor == nil || *edge.Floor != 98 || edge.Target == nil || *edge.Target != 95 {
		t.Errorf("edge = %+v", edge)
	}
	if r.Status != StatusFail || len(r.Failures) != 1 {
		t.Fatalf("status %s, failures %+v", r.Status, r.Failures)
	}
	f := r.Failures[0]
	if f.Scope != FailLayer || f.Layer != "go-unit" || *f.Threshold != 61.9 || *f.Measured != 61.54 {
		t.Errorf("failure = %+v", f)
	}
	if got := layer(r, "go-unit").Status; got != StatusFail {
		t.Errorf("go-unit layer status %s", got)
	}
	if got := layer(r, "go-live").Status; got != StatusOK {
		t.Errorf("go-live layer status %s", got)
	}
	if got := layer(r, "go-unit").Packages[0].Totals["statements"].Target; got == nil || *got != 80 {
		t.Errorf("package target = %v", got)
	}
}

func TestFixturePatch(t *testing.T) {
	r := analyze(t, fixture(t))
	p := r.Patch
	want := map[string]struct {
		covered, total int64
		status         string
	}{
		"go-unit": {0, 6, StatusFail},        // calc.go L17-18, L35-37, L39
		"go-sql":  {3, 4, StatusFail},        // store_pg.go: 75% < 80
		"go-live": {3, 10, StatusFail},       // both
		"go-e2e":  {3, 10, StatusReportOnly}, // counted, never gated
		"web":     {4, 5, StatusPass},        // exactly 80%: the target is inclusive
		"mobile":  {0, 0, StatusExempt},
		"edge":    {0, 0, StatusNotMeasured},
	}
	for _, pl := range p.Layers {
		w := want[pl.Layer]
		if pl.Covered != w.covered || pl.Total != w.total || pl.Status != w.status {
			t.Errorf("patch %s = %d/%d %s, want %d/%d %s", pl.Layer, pl.Covered, pl.Total, pl.Status, w.covered, w.total, w.status)
		}
	}
	// The union over gated layers: calc.go 6 + store_pg.go 4 + web 5 = 15
	// distinct changed coverable lines, 7 covered. go-e2e is not in it.
	if p.Overall.Covered != 7 || p.Overall.Total != 15 {
		t.Errorf("overall = %+v", p.Overall)
	}
	if p.Status != StatusFail || p.Blocking {
		t.Errorf("patch status %s blocking %v", p.Status, p.Blocking)
	}
	for _, f := range r.Failures {
		if f.Scope == FailPatch {
			t.Errorf("non-blocking patch produced a failure: %+v", f)
		}
	}
	unit := p.Layers[0].Files[0]
	if unit.Path != "libs/go/calc/calc.go" || rangesString(unit.UncoveredChanged) != "17-18,35-37,39" {
		t.Errorf("go-unit calc.go uncovered changed = %v", unit.UncoveredChanged)
	}
	d := p.Diff
	if d.Files != 8 || d.AddedLines != 38 || len(d.Binary) != 1 || len(d.Deleted) != 1 || len(d.Renamed) != 1 {
		t.Errorf("diff summary = %+v", d)
	}
	if strings.Join(d.Unmeasured, ",") != "apps/web/src/components/ui/card.tsx,docs/README.md" {
		t.Errorf("unmeasured = %v", d.Unmeasured)
	}
	if d.Renamed[0] != (Rename{From: "apps/web/src/lib/old.ts", To: "apps/web/src/lib/money.ts"}) {
		t.Errorf("renamed = %+v", d.Renamed)
	}
}

func rangesString(rs []coverage.Range) string {
	var parts []string
	for _, r := range rs {
		if r[0] == r[1] {
			parts = append(parts, fmt.Sprint(r[0]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", r[0], r[1]))
		}
	}
	return strings.Join(parts, ",")
}

func TestBlockingPatchFails(t *testing.T) {
	in := fixture(t)
	in.Config.Blocking = true
	r := analyze(t, in)
	var layers []string
	for _, f := range r.Failures {
		if f.Scope == FailPatch {
			layers = append(layers, f.Layer)
		}
	}
	if strings.Join(layers, ",") != "go-unit,go-sql,go-live" {
		t.Errorf("blocking patch failures = %v (report-only and passing layers must not fail)", layers)
	}
}

func TestNoDiffMeansNotComputed(t *testing.T) {
	in := fixture(t)
	in.Diff = nil
	r := analyze(t, in)
	if r.Patch.Status != StatusNotComputed || r.Patch.Diff != nil || len(r.Patch.Layers) != 0 {
		t.Errorf("patch = %+v", r.Patch)
	}
}

func TestRatchetProposals(t *testing.T) {
	r := analyze(t, fixture(t))
	got := map[string]string{}
	for _, c := range r.Ratchet {
		from := "new"
		if c.From != nil {
			from = floors.FormatFloor(*c.From)
			if floors.Tenths(c.To) <= floors.Tenths(*c.From) {
				t.Errorf("proposal %+v does not rise", c)
			}
		}
		got[string(c.Scope)+" "+c.Layer+" "+c.Key+" "+c.Metric] = from + "->" + floors.FormatFloor(c.To)
	}
	want := map[string]string{
		"layer go-live  statements":               "60.0->62.5",
		"package go-live libs/go/calc statements": "62.4->62.5",
		"layer go-sql  statements":                "new->66.6", // 2/3 rounds DOWN
		"layer web  lines":                        "70.0->87.5",
		"layer web  branches":                     "50.0->75.0",
		"layer web  functions":                    "new->75.0",
		"package go-unit libs/go/calc statements": "new->61.5",
		"glob go-unit libs/go/calc/** statements": "new->61.5",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ratchet %q = %q, want %q", k, got[k], v)
		}
	}
	// A failing floor has nothing to propose, and report-only and
	// unmeasured layers never get floors.
	for k := range got {
		for _, never := range []string{"layer go-unit ", " go-e2e ", " edge "} {
			if strings.HasPrefix(k, never) || strings.Contains(k, never) {
				t.Errorf("unexpected proposal %q", k)
			}
		}
	}
	// go-live's packages track only those with >= min_size (5) statements.
	if _, ok := got["package go-sql libs/go/calc statements"]; ok {
		t.Error("go-sql has no package floors configured, yet one was proposed")
	}
}

func TestPackageMinSize(t *testing.T) {
	in := fixture(t)
	in.Config.Layer("go-live").PkgMinSize = 17 // libs/go/calc has 16
	in.Floors.Delete(floors.Ref{Scope: floors.ScopePackage, Layer: "go-live", Key: "libs/go/calc"})
	r := analyze(t, in)
	for _, c := range r.Ratchet {
		if c.Scope == floors.ScopePackage && c.Layer == "go-live" {
			t.Errorf("a package below min_size got a new floor: %+v", c)
		}
	}
}

func TestStaleFloorsAndWarnings(t *testing.T) {
	r := analyze(t, fixture(t))
	if len(r.StaleFloors) != 1 || r.StaleFloors[0] != (floors.Ref{Scope: floors.ScopePackage, Layer: "go-live", Key: "libs/go/gone"}) {
		t.Errorf("stale = %+v", r.StaleFloors)
	}
	joined := strings.Join(r.Warnings, "\n")
	for _, want := range []string{
		"layers.retired names no configured layer",
		"layer mobile: 1 file(s) could not be mapped",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings lack %q:\n%s", want, joined)
		}
	}
	// With the edge layer unmeasured, a glob that matches nothing might
	// match the missing layer: no "delete the line" warning yet.
	if strings.Contains(joined, "legacy") {
		t.Errorf("premature unused-exclusion warning:\n%s", joined)
	}
}

func TestExclusionSizes(t *testing.T) {
	r := analyze(t, fixture(t))
	byGlob := map[string]Exclusion{}
	for _, e := range r.Exclusions {
		byGlob[e.Glob] = e
	}
	templ := byGlob["**/*_templ.go"]
	if len(templ.Layers) != 3 || templ.Layers[0].Layer != "go-unit" || templ.Layers[0].Counts["statements"].Total != 2 || templ.Reason != "templ codegen" {
		t.Errorf("templ exclusion = %+v", templ)
	}
	if len(byGlob["apps/web/src/legacy/**"].Layers) != 0 {
		t.Errorf("legacy = %+v", byGlob["apps/web/src/legacy/**"])
	}
	// Layer scope is reported too: go-unit's @sql-adapters exclude, and
	// go-sql's "not included" remainder.
	unit := layer(r, "go-unit").Scope
	if len(unit) != 2 || unit[1].Kind != ScopeExcluded || unit[1].Glob != "libs/go/**/store_pg.go" || unit[1].Counts["statements"].Total != 3 {
		t.Errorf("go-unit scope = %+v", unit)
	}
	sql := layer(r, "go-sql").Scope
	if len(sql) != 1 || sql[0].Kind != ScopeNotIncluded || sql[0].Files != 3 {
		t.Errorf("go-sql scope = %+v", sql)
	}
}

// writeRepo builds a throwaway repo with one Go layer and returns its
// root. stmts is the profile body after the mode line.
func writeRepo(t *testing.T, profile string) string {
	t.Helper()
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/tiny\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "cov.out"), []byte(profile), 0o644))
	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func tinyConfig(t *testing.T) *config.Resolved {
	t.Helper()
	cfg, err := config.Parse([]byte(`{"version":1,"layers":[{"id":"u","format":"go","inputs":["cov.out"],"metrics":["statements"]}]}`))
	must(t, err)
	return cfg
}

// The tolerance boundary, exactly: 777 of 1000 statements is 77.7%. With a
// 0.1-point tolerance a floor of 77.8 passes (77.7 is not below 77.7) and a
// floor of 77.9 fails.
func TestToleranceBoundary(t *testing.T) {
	root := writeRepo(t, "mode: set\nexample.com/tiny/a.go:1.1,2.2 777 1\nexample.com/tiny/a.go:3.1,4.2 223 0\n")
	for _, tc := range []struct {
		floor float64
		want  string
	}{{77.7, StatusOK}, {77.8, StatusOK}, {77.9, StatusFail}, {80.0, StatusFail}} {
		fl := floors.New()
		fl.Set(floors.Ref{Scope: floors.ScopeLayer, Layer: "u", Metric: "statements"}, tc.floor)
		r := analyze(t, Input{Root: root, Config: tinyConfig(t), Floors: fl, FloorsFound: true, Now: FixedNow})
		if got := r.Layers[0].Totals["statements"].Status; got != tc.want {
			t.Errorf("floor %.1f: status %s, want %s", tc.floor, got, tc.want)
		}
	}
	// Tolerance zero: the floor itself is the line.
	fl := floors.New()
	fl.TolerancePts = 0
	fl.Set(floors.Ref{Scope: floors.ScopeLayer, Layer: "u", Metric: "statements"}, 77.8)
	r := analyze(t, Input{Root: root, Config: tinyConfig(t), Floors: fl, Now: FixedNow})
	if r.Status != StatusFail {
		t.Errorf("tolerance 0, floor 77.8, measured 77.7: %s", r.Status)
	}
}

func TestLayerFloorWithNoDataFails(t *testing.T) {
	root := writeRepo(t, "mode: set\nexample.com/tiny/a.go:1.1,2.2 0 0\n")
	fl := floors.New()
	fl.Set(floors.Ref{Scope: floors.ScopeLayer, Layer: "u", Metric: "statements"}, 50)
	r := analyze(t, Input{Root: root, Config: tinyConfig(t), Floors: fl, Now: FixedNow})
	if r.Status != StatusFail || !strings.Contains(r.Failures[0].Message, "no data") {
		t.Errorf("status %s, failures %+v", r.Status, r.Failures)
	}
}

func TestRequire(t *testing.T) {
	in := fixture(t)
	in.Require = []string{"web", "edge"}
	r := analyze(t, in)
	var req []string
	for _, f := range r.Failures {
		if f.Scope == FailRequired {
			req = append(req, f.Layer)
		}
	}
	if strings.Join(req, ",") != "edge" {
		t.Errorf("required failures = %v, want only edge", req)
	}
	in = fixture(t)
	in.Require = []string{"nope"}
	if _, err := Analyze(in); err == nil {
		t.Error("--require of an unknown layer: want an error")
	}
	// Applying it again to a saved report is idempotent.
	before := len(r.Failures)
	must(t, r.Require("edge", nil))
	if len(r.Failures) != before {
		t.Error("Require added a duplicate failure")
	}
}

func TestUnusedExclusionWarnsWhenEveryLayerMeasured(t *testing.T) {
	root := writeRepo(t, "mode: set\nexample.com/tiny/a.go:1.1,2.2 1 1\n")
	rules, err := exclude.Parse(strings.NewReader("gone/**  # removed\n"), "exclude.txt")
	must(t, err)
	r := analyze(t, Input{Root: root, Config: tinyConfig(t), Excludes: rules, Now: FixedNow})
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "coverage/exclude.txt:1: gone/** matches no file") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestInconsistentProfilesAreAnError(t *testing.T) {
	root := writeRepo(t, "mode: set\nexample.com/tiny/a.go:1.1,2.2 1 1\n")
	must(t, os.WriteFile(filepath.Join(root, "cov2.out"), []byte("mode: set\nexample.com/tiny/a.go:1.1,2.2 2 1\n"), 0o644))
	cfg, err := config.Parse([]byte(`{"version":1,"layers":[{"id":"u","format":"go","inputs":["cov*.out"],"metrics":["statements"]}]}`))
	must(t, err)
	if _, err := Analyze(Input{Root: root, Config: cfg, Now: FixedNow}); err == nil || !strings.Contains(err.Error(), "inconsistent statement count") {
		t.Errorf("err = %v", err)
	}
}

func TestLowestFilesOrder(t *testing.T) {
	root := writeRepo(t, "mode: set\n"+
		"example.com/tiny/a.go:1.1,2.2 1 0\n"+ // 0/1: one uncovered
		"example.com/tiny/b.go:1.1,2.2 10 1\nexample.com/tiny/b.go:3.1,4.2 5 0\n"+ // 10/15: five uncovered
		"example.com/tiny/c.go:1.1,2.2 5 0\n"+ // 0/5: five uncovered, lower pct
		"example.com/tiny/d.go:1.1,2.2 3 1\n") // fully covered: never listed
	r := analyze(t, Input{Root: root, Config: tinyConfig(t), Now: FixedNow})
	var got []string
	for _, f := range r.Layers[0].LowestFiles {
		got = append(got, f.Path)
	}
	if strings.Join(got, ",") != "c.go,b.go,a.go" {
		t.Errorf("lowest = %v, want most uncovered first, lower pct breaking ties", got)
	}
}

func TestLoadRoundTripAndVersion(t *testing.T) {
	r := analyze(t, fixture(t))
	data, err := r.Marshal()
	must(t, err)
	p := filepath.Join(t.TempDir(), "report.json")
	must(t, os.WriteFile(p, data, 0o644))
	back, err := Load(p)
	must(t, err)
	again, err := back.Marshal()
	must(t, err)
	if !bytes.Equal(data, again) {
		t.Error("report.json does not round-trip")
	}
	must(t, os.WriteFile(p, []byte(`{"version":99}`), 0o644))
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Errorf("Load(v99) = %v", err)
	}
}

// min_lines is inclusive: a layer with exactly min_lines changed coverable
// lines is gated, one with fewer is exempt.
func TestPatchMinLinesBoundary(t *testing.T) {
	root := writeRepo(t, "mode: set\nexample.com/tiny/a.go:1.1,5.2 5 0\n")
	for _, tc := range []struct {
		lines []int
		want  string
	}{{[]int{1, 2, 3, 4}, StatusExempt}, {[]int{1, 2, 3, 4, 5}, StatusFail}} {
		cfg := tinyConfig(t) // min_lines defaults to 5
		d := []diff.File{{Path: "a.go", Status: diff.Modified, Added: tc.lines}}
		r := analyze(t, Input{Root: root, Config: cfg, Diff: d, Now: FixedNow})
		if got := r.Patch.Layers[0].Status; got != tc.want {
			t.Errorf("%d changed lines with min_lines 5: %s, want %s", len(tc.lines), got, tc.want)
		}
	}
}
