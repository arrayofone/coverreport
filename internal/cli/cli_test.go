package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arrayofone/coverreport/internal/floors"
	"github.com/arrayofone/coverreport/internal/report"
)

const (
	repo      = "../../testdata/repo"
	artifacts = "../../testdata/artifacts"
	diffFile  = "../../testdata/change.diff"
	golden    = "../../testdata/golden/report.json"
)

type run struct {
	code           int
	stdout, stderr string
}

// ghEnv is the GitHub Actions environment of the fixture's PR run.
var ghEnv = map[string]string{
	"GITHUB_REPOSITORY": "acme/demo",
	"GITHUB_SHA":        "3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d",
	"GITHUB_REF":        "refs/pull/42/merge",
	"GITHUB_HEAD_REF":   "feat/calc",
	"GITHUB_BASE_REF":   "main",
	"GITHUB_SERVER_URL": "https://github.com",
	"GITHUB_RUN_ID":     "1234",
	// 2026-09-28T12:00:00Z, the goldens' generated_at.
	"SOURCE_DATE_EPOCH": "1790596800",
}

func invoke(t *testing.T, env map[string]string, git func(string, ...string) ([]byte, error), args ...string) run {
	t.Helper()
	var out, errb bytes.Buffer
	e := Env{
		Stdin:  strings.NewReader(""),
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string { return env[k] },
		Now:    func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) },
		Git: func(dir string, a ...string) ([]byte, error) {
			if git == nil {
				return nil, errors.New("git not available in this test")
			}
			return git(dir, a...)
		},
	}
	code := Main(args, e)
	return run{code, out.String(), errb.String()}
}

func fixtureArgs(extra ...string) []string {
	return append([]string{"--root", repo, "--inputs", artifacts}, extra...)
}

// analyze through the CLI, with metadata coming only from the Actions
// environment, reproduces the report package's golden byte for byte: the
// CLI adds nothing and loses nothing.
func TestAnalyzeFromActionsEnvMatchesGolden(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	env := map[string]string{}
	for k, v := range ghEnv {
		env[k] = v
	}
	r := invoke(t, env, nil, append([]string{"analyze"}, fixtureArgs("--diff", diffFile, "--base", "1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d", "--out", out)...)...)
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	got, _ := os.ReadFile(out)
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("CLI analyze differs from %s", golden)
	}
}

func TestExitCodes(t *testing.T) {
	passing := filepath.Join(t.TempDir(), "floors.json")
	// go-unit measures 61.54; a 61.5 floor passes.
	os.WriteFile(passing, []byte(`{"version":1,"tolerance_pts":0.1,"layers":{"go-unit":{"statements":61.5}}}`), 0o644)
	cases := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"no command", nil, ExitError, "", "Usage:"},
		{"unknown command", []string{"frobnicate"}, ExitError, "", `unknown command "frobnicate"`},
		{"help", []string{"help"}, ExitOK, "Usage:", ""},
		{"version", []string{"version"}, ExitOK, "coverreport", ""},
		{"check fails on a broken floor", append([]string{"check"}, fixtureArgs()...), ExitFailed, "go-unit statements is 61.54%", ""},
		{"check passes", append([]string{"check"}, fixtureArgs("--floors", passing)...), ExitOK, "coverreport: pass", ""},
		{"check: a required layer that is missing fails", append([]string{"check"}, fixtureArgs("--floors", passing, "--require", "go-unit,edge")...), ExitFailed, "layer edge is required", ""},
		{"check: --require of an unknown layer is an error", append([]string{"check"}, fixtureArgs("--require", "nope")...), ExitError, "", "no such layer"},
		{"bad flag", []string{"check", "--nope"}, ExitError, "", "flag provided but not defined"},
		{"stray argument", []string{"check", "extra"}, ExitError, "", `unexpected argument "extra"`},
		{"missing config", []string{"check", "--root", t.TempDir()}, ExitError, "", "config.json"},
		{"--diff with --diff-base", append([]string{"analyze"}, fixtureArgs("--diff", diffFile, "--diff-base", "HEAD^1")...), ExitError, "", "mutually exclusive"},
		{"bad --pr", append([]string{"analyze"}, fixtureArgs("--pr", "x")...), ExitError, "", "not a pull request number"},
		{"missing diff file", append([]string{"analyze"}, fixtureArgs("--diff", "/nonexistent.diff")...), ExitError, "", "nonexistent.diff"},
		{"-h prints the flags and succeeds", []string{"check", "-h"}, ExitOK, "", "-diff-base"},
	}
	for _, tc := range cases {
		r := invoke(t, map[string]string{}, nil, tc.args...)
		if r.code != tc.code || !strings.Contains(r.stdout, tc.stdout) || !strings.Contains(r.stderr, tc.stderr) {
			t.Errorf("%s: exit %d stdout %q stderr %q; want exit %d, stdout containing %q, stderr containing %q",
				tc.name, r.code, r.stdout, r.stderr, tc.code, tc.stdout, tc.stderr)
		}
	}
}

func TestCheckWithSavedReport(t *testing.T) {
	r := invoke(t, nil, nil, "check", "--root", repo, "--report", golden)
	if r.code != ExitFailed || !strings.Contains(r.stdout, "1 gate(s) failed") {
		t.Errorf("exit %d: %s %s", r.code, r.stdout, r.stderr)
	}
	// --require applies to a saved report too.
	r = invoke(t, nil, nil, "check", "--root", repo, "--report", golden, "--require", "edge")
	if r.code != ExitFailed || !strings.Contains(r.stdout, "2 gate(s) failed") {
		t.Errorf("exit %d: %s %s", r.code, r.stdout, r.stderr)
	}
}

func copyFloors(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "coverage", "floors.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "floors.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func loadFloors(t *testing.T, p string) *floors.Floors {
	t.Helper()
	f, _, err := floors.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRatchetRaisesNeverLowersAndIsIdempotent(t *testing.T) {
	p := copyFloors(t)
	r := invoke(t, nil, nil, append([]string{"ratchet"}, fixtureArgs("--floors", p)...)...)
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	f := loadFloors(t, p)
	get := func(scope floors.Scope, layer, key, metric string) float64 {
		v, _ := f.Get(floors.Ref{Scope: scope, Layer: layer, Key: key, Metric: metric})
		return v
	}
	checks := []struct {
		what string
		got  float64
		want float64
	}{
		// go-unit measured 61.54 against a 62.0 floor: failing, and the
		// ratchet must leave it exactly where it was.
		{"go-unit floor kept", get(floors.ScopeLayer, "go-unit", "", "statements"), 62.0},
		{"go-live raised", get(floors.ScopeLayer, "go-live", "", "statements"), 62.5},
		{"package raised", get(floors.ScopePackage, "go-live", "libs/go/calc", "statements"), 62.5},
		{"new layer floor, rounded down", get(floors.ScopeLayer, "go-sql", "", "statements"), 66.6},
		{"glob floor at 100 kept", get(floors.ScopeGlob, "web", "apps/web/src/lib/**", "lines"), 100.0},
		{"unmeasured layer's floor kept", get(floors.ScopeLayer, "edge", "", "lines"), 98.0},
		{"stale package kept without --prune", get(floors.ScopePackage, "go-live", "libs/go/gone", "statements"), 50.0},
		{"unknown layer's floor kept", get(floors.ScopeLayer, "retired", "", "lines"), 50.0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: %v, want %v", c.what, c.got, c.want)
		}
	}
	if _, ok := f.Layers["go-e2e"]; ok {
		t.Error("a report-only layer got a floor")
	}
	first, _ := os.ReadFile(p)
	r = invoke(t, nil, nil, append([]string{"ratchet"}, fixtureArgs("--floors", p)...)...)
	if r.code != ExitOK || !strings.Contains(r.stdout, "no floor can rise") {
		t.Errorf("second ratchet: exit %d %q", r.code, r.stdout)
	}
	second, _ := os.ReadFile(p)
	if !bytes.Equal(first, second) {
		t.Error("an idempotent ratchet rewrote the file")
	}
	// After the ratchet, the same measurement checks clean apart from the
	// one floor it refused to lower.
	r = invoke(t, nil, nil, append([]string{"check"}, fixtureArgs("--floors", p)...)...)
	if r.code != ExitFailed || strings.Count(r.stdout, "\n  - ") != 1 {
		t.Errorf("check after ratchet: exit %d %q", r.code, r.stdout)
	}
}

// A ratchet from a saved report can never lower a floor that rose after the
// report was made.
func TestRatchetFromReportAgainstNewerFloors(t *testing.T) {
	p := copyFloors(t)
	f := loadFloors(t, p)
	f.Set(floors.Ref{Scope: floors.ScopeLayer, Layer: "go-live", Metric: "statements"}, 70.0)
	if err := f.Write(p); err != nil {
		t.Fatal(err)
	}
	r := invoke(t, nil, nil, "ratchet", "--root", repo, "--floors", p, "--report", golden)
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if v := loadFloors(t, p).Layers["go-live"]["statements"]; v != 70.0 {
		t.Errorf("go-live = %v; the report proposed 62.5 and the floor on disk was 70.0", v)
	}
}

func TestRatchetDryRunAndPrune(t *testing.T) {
	p := copyFloors(t)
	before, _ := os.ReadFile(p)
	r := invoke(t, nil, nil, append([]string{"ratchet"}, fixtureArgs("--floors", p, "--dry-run", "--prune")...)...)
	if r.code != ExitOK || !strings.Contains(r.stdout, "dry run") || !strings.Contains(r.stdout, "layer go-live statements: 60.0 -> 62.5") {
		t.Errorf("dry run: exit %d %q", r.code, r.stdout)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Error("--dry-run wrote the floors file")
	}
	r = invoke(t, nil, nil, append([]string{"ratchet"}, fixtureArgs("--floors", p, "--prune")...)...)
	if r.code != ExitOK || !strings.Contains(r.stdout, "pruned 1") {
		t.Errorf("prune: exit %d %q", r.code, r.stdout)
	}
	if _, ok := loadFloors(t, p).Packages["go-live"]["libs/go/gone"]; ok {
		t.Error("--prune kept the stale package floor")
	}
}

// With no floors file at all, the first ratchet creates one in canonical
// form with the default tolerance.
func TestRatchetCreatesFloors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "floors.json")
	r := invoke(t, nil, nil, append([]string{"ratchet"}, fixtureArgs("--floors", p)...)...)
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	f := loadFloors(t, p)
	if f.TolerancePts != 0.1 || f.Layers["go-unit"]["statements"] != 61.5 {
		t.Errorf("created floors = %+v", f)
	}
	data, _ := os.ReadFile(p)
	if !bytes.Equal(data, f.Marshal()) {
		t.Error("created file is not canonical")
	}
}

// --diff-base shells out to git with flags that pin the prefixes, quoting
// and rename detection, and fills base/sha from rev-parse when not given.
func TestDiffBase(t *testing.T) {
	diffData, err := os.ReadFile(diffFile)
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	git := func(dir string, args ...string) ([]byte, error) {
		calls = append(calls, args)
		switch args[0] {
		case "rev-parse":
			if strings.HasPrefix(args[2], "HEAD^1") {
				return []byte("1111111111111111111111111111111111111111\n"), nil
			}
			return []byte("2222222222222222222222222222222222222222\n"), nil
		}
		return diffData, nil
	}
	out := filepath.Join(t.TempDir(), "r.json")
	r := invoke(t, map[string]string{}, git, append([]string{"analyze"}, fixtureArgs("--diff-base", "HEAD^1", "--out", out)...)...)
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	want := "-c core.quotePath=false diff -U0 --no-color --no-ext-diff --src-prefix=a/ --dst-prefix=b/ -M HEAD^1 HEAD"
	if len(calls) == 0 || strings.Join(calls[0], " ") != want {
		t.Errorf("git call = %v, want %q", calls, want)
	}
	rep, err := report.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Metadata.Base != "1111111111111111111111111111111111111111" || rep.Metadata.SHA != "2222222222222222222222222222222222222222" {
		t.Errorf("metadata = %+v", rep.Metadata)
	}
	if rep.Patch.Status == report.StatusNotComputed {
		t.Error("patch not computed from --diff-base")
	}
	// A git failure is an input error, not a silent "no patch".
	failing := func(string, ...string) ([]byte, error) { return nil, errors.New("fatal: bad revision") }
	r = invoke(t, map[string]string{}, failing, append([]string{"analyze"}, fixtureArgs("--diff-base", "nope")...)...)
	if r.code != ExitError || !strings.Contains(r.stderr, "bad revision") {
		t.Errorf("git failure: exit %d %q", r.code, r.stderr)
	}
}

func TestDiffFromStdin(t *testing.T) {
	diffData, _ := os.ReadFile(diffFile)
	var out bytes.Buffer
	e := Env{Stdin: bytes.NewReader(diffData), Stdout: &out, Stderr: &bytes.Buffer{}, Getenv: func(string) string { return "" }, Now: time.Now}
	if code := Main(append([]string{"analyze"}, fixtureArgs("--diff", "-")...), e); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var rep report.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || rep.Patch.Diff == nil || rep.Patch.Diff.Files != 8 {
		t.Errorf("stdin diff: %v %+v", err, rep.Patch.Diff)
	}
}

func TestMetadataFlagsBeatEnv(t *testing.T) {
	out := filepath.Join(t.TempDir(), "r.json")
	r := invoke(t, ghEnv, nil, append([]string{"analyze"}, fixtureArgs("--repo", "x/y", "--pr", "7", "--run-url", "https://ci/1", "--out", out)...)...)
	if r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	rep, _ := report.Load(out)
	m := rep.Metadata
	if m.Repo != "x/y" || m.PR != 7 || m.RunURL != "https://ci/1" || m.SHA != ghEnv["GITHUB_SHA"] || m.HeadRef != "feat/calc" {
		t.Errorf("metadata = %+v", m)
	}
	if rep.FloorsFile != "coverage/floors.json" {
		t.Errorf("floors_file = %q", rep.FloorsFile)
	}
	if rep.GeneratedAt != "2026-09-28T12:00:00Z" {
		t.Errorf("generated_at = %s (SOURCE_DATE_EPOCH ignored)", rep.GeneratedAt)
	}
	// Without SOURCE_DATE_EPOCH the clock is used.
	r = invoke(t, nil, nil, append([]string{"analyze"}, fixtureArgs("--out", out)...)...)
	rep, _ = report.Load(out)
	if r.code != ExitOK || rep.GeneratedAt != "2030-01-01T00:00:00Z" {
		t.Errorf("generated_at = %s", rep.GeneratedAt)
	}
}

func TestTextCommand(t *testing.T) {
	r := invoke(t, nil, nil, "text", "--report", golden, "--detail")
	if r.code != ExitOK || !strings.HasPrefix(r.stdout, "coverreport: FAIL  acme/demo #42") || !strings.Contains(r.stdout, "Packages in go-unit:") {
		t.Errorf("text --report: exit %d\n%s", r.code, r.stdout)
	}
	r = invoke(t, nil, nil, append([]string{"text"}, fixtureArgs()...)...)
	if r.code != ExitOK || !strings.Contains(r.stdout, "Patch: not computed") {
		t.Errorf("text (fresh analysis): exit %d\n%s", r.code, r.stdout)
	}
}

func TestOverriddenFilesAreNamedInTheReport(t *testing.T) {
	p := copyFloors(t)
	out := filepath.Join(t.TempDir(), "r.json")
	ex := filepath.Join(t.TempDir(), "exclude.txt")
	os.WriteFile(ex, []byte("**/*_templ.go  # codegen\n"), 0o644)
	r := invoke(t, nil, nil, append([]string{"analyze"}, fixtureArgs("--floors", p, "--exclude", ex, "--out", out)...)...)
	if r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	rep, _ := report.Load(out)
	if rep.FloorsFile != filepath.ToSlash(p) || rep.ExcludeFile != filepath.ToSlash(ex) || len(rep.Exclusions) != 1 {
		t.Errorf("floors_file %q exclude_file %q exclusions %d", rep.FloorsFile, rep.ExcludeFile, len(rep.Exclusions))
	}
}
