// Package rendertest builds the reports every renderer's golden tests draw:
// one per state a PR can be in. Each is produced by the real analysis over
// the fixture repository (testdata/repo and testdata/artifacts), varying
// only the floors, the diff, the config and the optional inputs, so a
// golden is always a picture of a report the tool can actually write.
//
// It imports nothing from the renderers, so their tests (internal and
// external) can all use it.
package rendertest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/arrayofone/coverreport/internal/brand"
	"github.com/arrayofone/coverreport/internal/config"
	"github.com/arrayofone/coverreport/internal/diff"
	"github.com/arrayofone/coverreport/internal/endpoints"
	"github.com/arrayofone/coverreport/internal/exclude"
	"github.com/arrayofone/coverreport/internal/floors"
	"github.com/arrayofone/coverreport/internal/glob"
	"github.com/arrayofone/coverreport/internal/report"
)

// Now is every golden report's generated_at.
var Now = time.Date(2026, 9, 28, 15, 42, 0, 0, time.UTC)

// State is one golden input.
type State struct {
	Name   string
	Report *report.Report
	// SourceDir is the checkout for excerpts ("" renders without).
	SourceDir string
	// Previous is the sticky comment the render replaces.
	Previous    string
	ArtifactURL string
	// CommentBudget / SummaryBudget, when non-zero, shrink the size
	// budgets so a golden can show truncation at a readable size.
	CommentBudget, SummaryBudget int
}

// Testdata is the repository's testdata directory.
func Testdata() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata")
}

// Handipay is handipay's Paper/Dim brand, as its config carries it.
var Handipay = &brand.Config{
	Name: "handipay",
	Mono: `"Intel One Mono", ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, "DejaVu Sans Mono", monospace`,
	Sans: `"IBM Plex Sans", system-ui, -apple-system, "Segoe UI", sans-serif`,
	Dark: map[string]string{
		"bg": "#0c0e0c", "panel": "#121512", "glass": "#080a08",
		"bezel": "rgba(255, 255, 255, 0.085)", "bezel-2": "rgba(255, 255, 255, 0.16)",
		"ink": "#ecefe9", "ink-soft": "#8d958c", "ink-ghost": "#59615a", "fill": "#d9ded6", "mark": "#ecefe9",
		"ok": "#00e055", "caution": "#ffb627", "warning": "#ff5a4e", "target": "#5ad8ff",
		"t-ok": "rgba(0, 224, 85, 0.075)", "t-caution": "rgba(255, 182, 39, 0.14)",
		"t-warning": "rgba(255, 90, 78, 0.14)", "t-target": "rgba(90, 216, 255, 0.16)",
		"grid-a": "rgba(255, 255, 255, 0.022)", "grid-b": "transparent",
		"tm-ok": "#171b17", "tm-ok-fg": "#8d958c", "tm-h1": "#3b2f14", "tm-h1-fg": "#ecefe9",
		"tm-h2": "#7a5a16", "tm-h2-fg": "#ecefe9", "tm-h3": "#ffb627", "tm-h3-fg": "#0c0e0c",
	},
	Light: map[string]string{
		"bg": "#f6f4ee", "panel": "#fffefa", "glass": "#fffefa",
		"bezel": "#e3dfd2", "bezel-2": "#cfcaba",
		"ink": "#21251f", "ink-soft": "#6e756a", "ink-ghost": "#9aa093", "fill": "#2e342c", "mark": "#21251f",
		"ok": "#0b7a3c", "caution": "#9a5a00", "warning": "#b3261e", "target": "#0a6a84",
		"t-ok": "rgba(11, 122, 60, 0.07)", "t-caution": "rgba(214, 140, 0, 0.16)",
		"t-warning": "rgba(179, 38, 30, 0.09)", "t-target": "rgba(10, 106, 132, 0.11)",
		"grid-a": "rgba(92, 122, 100, 0.055)", "grid-b": "rgba(92, 122, 100, 0.035)",
		"tm-ok": "#efece3", "tm-ok-fg": "#6e756a", "tm-h1": "#f3e0bb", "tm-h1-fg": "#21251f",
		"tm-h2": "#deb46a", "tm-h2-fg": "#21251f", "tm-h3": "#9a5a00", "tm-h3-fg": "#fffefa",
	},
}

// meta is the golden PR's metadata.
var meta = report.Metadata{
	Repo:    "acme/demo",
	PR:      561,
	SHA:     "3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d",
	Base:    "bb1595fe0d7c2a61e3f4b5a6978c0d1e2f3a4b5c",
	HeadRef: "fix/webhook-retry",
	BaseRef: "main",
	RunURL:  "https://github.com/acme/demo/actions/runs/18342917305",
	Title:   "fix(calc): zero gets a name, and a store for the rows",
	HeadSHA: "71a0d3e9b2c4f6a8d0e1f2a3b4c5d6e7f8091a2b",
}

// in is one analysis's inputs, starting from the fixture.
type in struct {
	cfg     *config.Resolved
	floors  map[string]map[string]float64
	pkgs    map[string]map[string]map[string]float64
	found   bool
	diff    string // a diff text; "fixture" is testdata/change.diff
	inputs  string // the artifacts directory
	root    string
	require []string
	eps     *endpoints.File
	base    *report.Report
}

func fixtureConfig(t testing.TB) *config.Resolved {
	t.Helper()
	cfg, err := config.Load(filepath.Join(Testdata(), "repo", "coverage", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.InformationalUntil = "2026-10-05"
	return cfg
}

func analyze(t testing.TB, x in) *report.Report {
	t.Helper()
	td := Testdata()
	fl := floors.New()
	for l, ms := range x.floors {
		for m, v := range ms {
			fl.Set(floors.Ref{Scope: floors.ScopeLayer, Layer: l, Metric: m}, v)
		}
	}
	for l, ks := range x.pkgs {
		for k, ms := range ks {
			for m, v := range ms {
				fl.Set(floors.Ref{Scope: floors.ScopePackage, Layer: l, Key: k, Metric: m}, v)
			}
		}
	}
	rules, err := exclude.Load(filepath.Join(td, "repo", "coverage", "exclude.txt"))
	if err != nil {
		t.Fatal(err)
	}
	text := x.diff
	if text == "fixture" {
		b, err := os.ReadFile(filepath.Join(td, "change.diff"))
		if err != nil {
			t.Fatal(err)
		}
		text = string(b)
	}
	var d []diff.File
	if text != "" {
		if d, err = diff.Parse(strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
	}
	root := x.root
	if root == "" {
		root = filepath.Join(td, "repo")
	}
	inputs := x.inputs
	if inputs == "" {
		inputs = filepath.Join(td, "artifacts")
	}
	r, err := report.Analyze(report.Input{
		Root: root, InputsDir: inputs, Config: x.cfg, Floors: fl, FloorsFound: x.found,
		Excludes: rules, Diff: d, Metadata: meta, Require: x.require, Now: Now,
		Baseline: x.base, BaselineFile: "baseline/report.json",
		Endpoints: x.eps, EndpointsFile: "coverage-endpoints/endpoints.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// holding floors sit under every measurement, so every floor holds and
// several can rise.
var holding = map[string]map[string]float64{
	"go-unit": {"statements": 60.0},
	"go-sql":  {"statements": 66.6},
	"go-live": {"statements": 60.0},
	"web":     {"lines": 80.0, "branches": 70.0, "functions": 75.0},
	"mobile":  {"lines": 66.6},
	"edge":    {"lines": 98.0},
}

// coveredDiff touches only lines that ran: Sum in calc.go, and format in
// money.ts.
const coveredDiff = `diff --git a/libs/go/calc/calc.go b/libs/go/calc/calc.go
--- a/libs/go/calc/calc.go
+++ b/libs/go/calc/calc.go
@@ -25,0 +26,6 @@ func Classify(n int) (string, error) {
+func Sum(xs []int) int {
+	total := 0
+	for _, x := range xs {
+		total += x
+	}
+	return total
diff --git a/apps/web/src/lib/money.ts b/apps/web/src/lib/money.ts
--- a/apps/web/src/lib/money.ts
+++ b/apps/web/src/lib/money.ts
@@ -0,0 +1,3 @@
+export function format(cents: number): string {
+  const sign = cents < 0 ? "-" : "";
+  return sign + (Math.abs(cents) / 100).toFixed(2);
`

// oneLineDiff edits the one line of Classify neither Go run reaches.
const oneLineDiff = `diff --git a/libs/go/calc/calc.go b/libs/go/calc/calc.go
--- a/libs/go/calc/calc.go
+++ b/libs/go/calc/calc.go
@@ -22 +22 @@ func Classify(n int) (string, error) {
-	return "big", nil
+	return "large", nil
`

// Endpoints is a small registry: two surfaces, three kinds, every status.
func Endpoints(t testing.TB, violations ...endpoints.Violation) *endpoints.File {
	t.Helper()
	f, err := endpoints.Load(filepath.Join(Testdata(), "render", "endpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Baseline.Violations = append(f.Baseline.Violations, violations...)
	return f
}

func previous(t testing.TB) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Testdata(), "render", "previous.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// States is every golden state, in a fixed order.
func States(t testing.TB) []State {
	t.Helper()
	td := Testdata()
	src := filepath.Join(td, "repo")
	var out []State

	// ok: every floor holds, the patch is fully covered, floors can rise,
	// and the endpoint registry reports without violations.
	{
		cfg := fixtureConfig(t)
		r := analyze(t, in{cfg: cfg, floors: holding, found: true, diff: coveredDiff, eps: Endpoints(t)})
		out = append(out, State{Name: "ok", Report: r, SourceDir: src})
	}
	// fail: the fixture as it stands: go-unit is under its 62.0 floor, in
	// handipay's brand, on its fourth push, with the page uploaded.
	{
		cfg := fixtureConfig(t)
		cfg.Brand = Handipay
		cfg.RatchetCommand = "make coverage-ratchet"
		fl := copyFloors(holding)
		fl["go-unit"]["statements"] = 62.0
		r := analyze(t, in{cfg: cfg, floors: fl, found: true, diff: "fixture"})
		out = append(out, State{Name: "fail", Report: r, SourceDir: src, Previous: previous(t),
			ArtifactURL: "https://github.com/acme/demo/actions/runs/18342917305/artifacts/4127736190"})
	}
	// warn: every floor holds, but the change adds lines no test reaches:
	// patch under target, informational during the soak.
	{
		cfg := fixtureConfig(t)
		r := analyze(t, in{cfg: cfg, floors: holding, found: true, diff: "fixture"})
		out = append(out, State{Name: "warn", Report: r, SourceDir: src})
	}
	// first-run: no floors file at all.
	{
		cfg := fixtureConfig(t)
		r := analyze(t, in{cfg: cfg, found: false, diff: "fixture"})
		out = append(out, State{Name: "first-run", Report: r, SourceDir: src})
	}
	// carried: the mobile collector did not run on this change (its
	// artifact is absent), so mobile carries main's numbers from a
	// baseline report; rendered without a source checkout.
	{
		cfg := fixtureConfig(t)
		base := analyze(t, in{cfg: fixtureConfig(t), floors: holding, found: true})
		base.Metadata = report.Metadata{Repo: "acme/demo", SHA: "bb1595fe0d7c2a61e3f4b5a6978c0d1e2f3a4b5c"}
		mobile := cfg.Layer("mobile")
		mobile.Inputs = []string{"coverage-mobile-absent/lcov.info"}
		mobile.InputGlobs = []*glob.Pattern{glob.MustCompile(mobile.Inputs[0])}
		r := analyze(t, in{cfg: cfg, floors: holding, found: true, diff: coveredDiff, base: base})
		out = append(out, State{Name: "carried", Report: r})
	}
	// e2e: a report-only e2e layer that reaches code no gated layer does
	// (Unused in calc.go): shown with its own patch row, and never counted.
	{
		cfg := fixtureConfig(t)
		dir := copyArtifacts(t)
		profile := "mode: atomic\n" + strings.Join([]string{
			"example.com/demo/libs/go/calc/calc.go:10.38,11.11 1 1",
			"example.com/demo/libs/go/calc/calc.go:11.11,13.3 1 0",
			"example.com/demo/libs/go/calc/calc.go:16.2,16.9 1 1",
			"example.com/demo/libs/go/calc/calc.go:17.14,18.21 1 1",
			"example.com/demo/libs/go/calc/calc.go:19.14,20.22 1 0",
			"example.com/demo/libs/go/calc/calc.go:22.2,22.21 1 0",
			"example.com/demo/libs/go/calc/calc.go:26.24,28.23 2 0",
			"example.com/demo/libs/go/calc/calc.go:28.23,30.3 1 0",
			"example.com/demo/libs/go/calc/calc.go:31.2,31.14 1 0",
			"example.com/demo/libs/go/calc/calc.go:35.19,40.2 3 4",
		}, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, "coverage-go-e2e", "e2e.out"), []byte(profile), 0o644); err != nil {
			t.Fatal(err)
		}
		r := analyze(t, in{cfg: cfg, floors: holding, found: true, diff: "fixture", inputs: dir})
		out = append(out, State{Name: "e2e", Report: r, SourceDir: src})
	}
	// multi: every kind of failure at once: a layer floor, a package
	// floor, a required layer that did not report, a blocking patch miss
	// and an endpoint baseline violation.
	{
		cfg := fixtureConfig(t)
		cfg.Blocking = true
		fl := copyFloors(holding)
		fl["go-unit"]["statements"] = 62.0
		pk := map[string]map[string]map[string]float64{"go-live": {"libs/go/calc": {"statements": 70.0}}}
		eps := Endpoints(t, endpoints.Violation{ID: "svc-api:POST /sums", Detail: "gained a gap: authz"},
			endpoints.Violation{ID: "svc-api:event sum.done", Detail: "added after the freeze and not full in any layer"})
		r := analyze(t, in{cfg: cfg, floors: fl, pkgs: pk, found: true, diff: "fixture", require: []string{"edge"}, eps: eps})
		out = append(out, State{Name: "multi", Report: r, SourceDir: src})
	}
	// one-line: the smallest change there is, one edited line that no test
	// reaches, so every count of changed lines on every surface is one (the
	// state a fixed plural gets wrong), and the patch, under min_lines, is
	// exempt rather than failing.
	{
		cfg := fixtureConfig(t)
		r := analyze(t, in{cfg: cfg, floors: holding, found: true, diff: oneLineDiff})
		out = append(out, State{Name: "one-line", Report: r, SourceDir: src})
	}
	// large: a change touching 16 files in 8 packages, every file with
	// lines that never ran, rendered into small budgets so the golden
	// shows what the comment and summary drop and how they say so (the
	// real 65,536-character limit is exercised by a property test).
	{
		root, inputs, text := Large(t, 8, 2, 6)
		s := LargeState(t, root, inputs, text)
		s.CommentBudget, s.SummaryBudget = 16000, 24000
		out = append(out, s)
	}
	return out
}

// LargeState analyses a repository written by Large.
func LargeState(t testing.TB, root, inputs, diffText string) State {
	t.Helper()
	cfg := largeConfig(t)
	cfg.Brand = Handipay
	r := analyze(t, in{cfg: cfg, floors: map[string]map[string]float64{"go-unit": {"statements": 50.0}}, found: true,
		diff: diffText, root: root, inputs: inputs})
	return State{Name: "large", Report: r, SourceDir: root}
}

func copyFloors(m map[string]map[string]float64) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for l, ms := range m {
		out[l] = map[string]float64{}
		for k, v := range ms {
			out[l][k] = v
		}
	}
	return out
}

// copyArtifacts copies testdata/artifacts into a temporary directory a
// state can change.
func copyArtifacts(t testing.TB) string {
	t.Helper()
	src := filepath.Join(Testdata(), "artifacts")
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func largeConfig(t testing.TB) *config.Resolved {
	t.Helper()
	cfg, err := config.Parse([]byte(`{
  "version": 1,
  "go_modules": { "example.com/big": "." },
  "patch": { "target": 80, "min_lines": 5, "informational_until": "2026-10-05" },
  "layers": [
    { "id": "go-unit", "label": "Go unit", "format": "go", "inputs": ["unit.out"], "metrics": ["statements"],
      "targets": { "statements": 85 }, "packages": { "floors": true, "targets": { "statements": 80 } }, "treemap": true }
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Large writes a synthetic repository of pkgs packages with files files
// each: every file is funcs small functions, and the profile runs the even
// ones. It returns the root, the inputs directory and a diff adding every
// line of every file. Deterministic, so goldens built from it are stable.
func Large(t testing.TB, pkgs, files, funcs int) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	inputs := t.TempDir()
	var prof strings.Builder
	prof.WriteString("mode: atomic\n")
	var d strings.Builder
	for p := 0; p < pkgs; p++ {
		dir := fmt.Sprintf("pkg/area%02d/unit%02d", p/4, p)
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < files; f++ {
			name := fmt.Sprintf("%s/file%02d.go", dir, f)
			var src strings.Builder
			fmt.Fprintf(&src, "package unit%02d\n\n", p)
			line := 3
			for fn := 0; fn < funcs; fn++ {
				fmt.Fprintf(&src, "func F%d_%d(x int) int {\n\ty := x * %d\n\treturn y + %d\n}\n\n", f, fn, fn+2, p)
				// A block spans the body: from the { on line `line` to
				// the } three lines down.
				hits := 0
				if fn%2 == 0 || (p+f)%5 == 0 && fn == 1 {
					hits = 1 + fn
				}
				fmt.Fprintf(&prof, "example.com/big/%s:%d.%d,%d.2 2 %d\n", name, line, 24, line+3, hits)
				line += 5
			}
			if err := os.WriteFile(filepath.Join(root, name), []byte(src.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			body := strings.TrimSuffix(src.String(), "\n")
			n := strings.Count(body, "\n") + 1
			fmt.Fprintf(&d, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", name, name, name, n)
			for _, l := range strings.Split(body, "\n") {
				d.WriteString("+" + l + "\n")
			}
		}
	}
	if err := os.WriteFile(filepath.Join(inputs, "unit.out"), []byte(prof.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, inputs, d.String()
}
