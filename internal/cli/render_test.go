package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arrayofone/coverreport/internal/report"
)

// render over the golden report writes all four surfaces, each what its
// consumer expects at the top.
func TestRenderWritesFourSurfaces(t *testing.T) {
	out := t.TempDir()
	r := invoke(t, nil, nil, "render", "--report", golden, "--out", out, "--source-root", repo)
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "wrote comment.md, summary.md, annotations.txt") {
		t.Errorf("stdout %q", r.stdout)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if c := read(CommentFile); !strings.HasPrefix(c, "<!-- coverreport:v1 -->\n<!-- coverreport:state ") || !strings.Contains(c, "demo coverage: 1 floor broken") {
		t.Errorf("comment.md head: %q", c[:min(len(c), 200)])
	}
	if s := read(SummaryFile); !strings.HasPrefix(s, "## 🔴 demo coverage") {
		t.Errorf("summary.md head: %q", s[:min(len(s), 80)])
	}
	for _, line := range strings.Split(strings.TrimSpace(read(AnnotationsFile)), "\n") {
		if !strings.HasPrefix(line, "::warning file=") && !strings.HasPrefix(line, "::notice file=") {
			t.Errorf("not a workflow command: %q", line)
		}
	}
	if h := read(HTMLFile); !strings.HasPrefix(h, "<!doctype html>") || !strings.Contains(h, "</html>") {
		t.Error("report.html is not a page")
	}
}

func TestRenderFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"render", "--out", t.TempDir()}, "--report and --out are required"},
		{[]string{"render", "--report", golden}, "--report and --out are required"},
		{[]string{"render", "--report", golden, "--out", t.TempDir(), "--source-root", "/nonexistent-root"}, "--source-root"},
		{[]string{"render", "--report", golden, "--out", t.TempDir(), "--previous", "/nonexistent.md"}, "--previous"},
		{[]string{"render", "--report", "/nonexistent.json", "--out", t.TempDir()}, "nonexistent.json"},
	} {
		r := invoke(t, nil, nil, tc.args...)
		if r.code != ExitError || !strings.Contains(r.stderr, tc.want) {
			t.Errorf("%v: exit %d stderr %q, want %q", tc.args, r.code, r.stderr, tc.want)
		}
	}
}

// A second render reads the first one's comment back and carries its
// pushes forward.
func TestRenderCarriesThePushHistory(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	if r := invoke(t, nil, nil, "render", "--report", golden, "--out", first); r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	rep, err := report.Load(golden)
	if err != nil {
		t.Fatal(err)
	}
	rep.Metadata.HeadSHA = "c2b19f4a8e7d6c5b4a39281706f5e4d3c2b1a098"
	data, _ := rep.Marshal()
	next := filepath.Join(t.TempDir(), "report.json")
	os.WriteFile(next, data, 0o644)
	if r := invoke(t, nil, nil, "render", "--report", next, "--out", second, "--previous", filepath.Join(first, CommentFile)); r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	b, _ := os.ReadFile(filepath.Join(second, CommentFile))
	if !strings.Contains(string(b), `{"n":2,"pushes":[["3f2a9c1",46.7],["c2b19f4",46.7]]}`) || !strings.Contains(string(b), "#42 push 2") {
		t.Errorf("history not carried:\n%s", string(b)[:300])
	}
}

// ghFake is a minimal comments API for the comment command.
type ghFake struct {
	mu     sync.Mutex
	bodies map[int64]string
	order  []int64
	auths  []string
}

func (f *ghFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/issues/42/comments":
		var cs []map[string]any
		for _, id := range f.order {
			cs = append(cs, map[string]any{"id": id, "body": f.bodies[id], "html_url": "https://github.com/acme/demo/pull/42#c"})
		}
		json.NewEncoder(w).Encode(cs)
	case r.Method == http.MethodPost:
		var in struct{ Body string }
		json.NewDecoder(r.Body).Decode(&in)
		id := int64(len(f.order) + 1)
		f.order = append(f.order, id)
		f.bodies[id] = in.Body
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "body": in.Body, "html_url": "https://github.com/acme/demo/pull/42#new"})
	case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/demo/issues/comments/1":
		var in struct{ Body string }
		json.NewDecoder(r.Body).Decode(&in)
		f.bodies[1] = in.Body
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "body": in.Body, "html_url": "https://github.com/acme/demo/pull/42#updated"})
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"message":"Not Found"}`)
	}
}

func invokeHTTP(t *testing.T, env map[string]string, srv *httptest.Server, args ...string) run {
	t.Helper()
	var out, errb bytes.Buffer
	e := Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Getenv: func(k string) string { return env[k] },
		Now: time.Now, HTTP: srv.Client(), Sleep: func(time.Duration) {}}
	return run{Main(args, e), out.String(), errb.String()}
}

// fetch, render, upsert, then the same again: the first run creates the
// comment, the second updates it in place and hands back what it replaced.
// The token appears in the Authorization header and nowhere in the output.
func TestCommentFetchAndUpsert(t *testing.T) {
	const token = "ghs_secretTOKENvalue123"
	f := &ghFake{bodies: map[int64]string{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	env := map[string]string{"GITHUB_TOKEN": token, "GITHUB_REPOSITORY": "acme/demo", "GITHUB_REF": "refs/pull/42/merge", "GITHUB_API_URL": srv.URL}
	dir := t.TempDir()
	prev := filepath.Join(dir, "prev.md")
	var all []string
	for push := 1; push <= 2; push++ {
		r := invokeHTTP(t, env, srv, "comment", "--fetch", prev)
		if r.code != ExitOK {
			t.Fatalf("fetch: %s", r.stderr)
		}
		all = append(all, r.stdout, r.stderr)
		out := filepath.Join(dir, "out")
		if r := invoke(t, nil, nil, "render", "--report", golden, "--out", out, "--previous", prev); r.code != ExitOK {
			t.Fatal(r.stderr)
		}
		replaced := filepath.Join(dir, "replaced.md")
		r = invokeHTTP(t, env, srv, "comment", "--body", filepath.Join(out, CommentFile), "--previous", replaced)
		if r.code != ExitOK {
			t.Fatalf("upsert %d: %s", push, r.stderr)
		}
		all = append(all, r.stdout, r.stderr)
		want := map[int]string{1: "created https://github.com/acme/demo/pull/42#new", 2: "updated https://github.com/acme/demo/pull/42#updated"}[push]
		if !strings.Contains(r.stdout, want) {
			t.Errorf("push %d: %q", push, r.stdout)
		}
		old, _ := os.ReadFile(replaced)
		if (push == 1) != (len(old) == 0) {
			t.Errorf("push %d: replaced body %d bytes", push, len(old))
		}
	}
	if len(f.order) != 1 {
		t.Errorf("%d comments, want the one sticky comment", len(f.order))
	}
	for _, a := range f.auths {
		if a != "Bearer "+token {
			t.Errorf("Authorization %q", a)
		}
	}
	for _, s := range all {
		if strings.Contains(s, token) {
			t.Errorf("the token was printed: %q", s)
		}
	}
}

func TestCommentRefusals(t *testing.T) {
	srv := httptest.NewServer(&ghFake{bodies: map[int64]string{}})
	defer srv.Close()
	good := map[string]string{"GITHUB_TOKEN": "t", "GITHUB_REPOSITORY": "acme/demo", "GITHUB_REF": "refs/pull/42/merge", "GITHUB_API_URL": srv.URL}
	notOurs := filepath.Join(t.TempDir(), "body.md")
	os.WriteFile(notOurs, []byte("# hand-written\n"), 0o644)
	big := filepath.Join(t.TempDir(), "big.md")
	os.WriteFile(big, []byte("<!-- coverreport:v1 -->\n"+strings.Repeat("x", 70000)), 0o644)
	without := func(k string) map[string]string {
		m := map[string]string{}
		for a, b := range good {
			if a != k {
				m[a] = b
			}
		}
		return m
	}
	for _, tc := range []struct {
		env  map[string]string
		args []string
		want string
	}{
		{without("GITHUB_TOKEN"), []string{"comment", "--fetch", "x"}, "GITHUB_TOKEN is not set"},
		{without("GITHUB_REF"), []string{"comment", "--fetch", "x"}, "not a pull request number"},
		{good, []string{"comment", "--fetch", "x", "--repo", "not-a-repo"}, "is not owner/name"},
		{good, []string{"comment"}, "exactly one of --body"},
		{good, []string{"comment", "--body", notOurs}, "does not start with a coverreport marker"},
		{good, []string{"comment", "--body", big}, "GitHub's limit is 65536"},
		{good, []string{"comment", "--fetch", "x", "--pr", "7"}, "404"},
	} {
		r := invokeHTTP(t, tc.env, srv, tc.args...)
		if r.code != ExitError || !strings.Contains(r.stderr, tc.want) {
			t.Errorf("%v: exit %d stderr %q, want %q", tc.args, r.code, r.stderr, tc.want)
		}
	}
}

// analyze folds in a base-branch report (display only) and the endpoint
// registry (whose violations fail the check); a registry path that does
// not exist yet is a warning, not an error.
func TestAnalyzeBaselineAndEndpoints(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.json")
	if r := invoke(t, map[string]string{"GITHUB_SHA": "bb1595fe0d7c2a61e3f4b5a6978c0d1e2f3a4b5c"}, nil,
		append([]string{"analyze"}, fixtureArgs("--out", base)...)...); r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	// This run's inputs lack the mobile artifact.
	inputs := filepath.Join(dir, "inputs")
	os.CopyFS(inputs, os.DirFS(artifacts))
	os.RemoveAll(filepath.Join(inputs, "coverage-mobile"))
	regFile := filepath.Join(dir, "endpoints.json")
	reg, _ := os.ReadFile("../../testdata/render/endpoints.json")
	reg = bytes.Replace(reg, []byte(`"violations": []`), []byte(`"violations": [{"id": "svc-api:POST /sums", "detail": "gained a gap: authz"}]`), 1)
	os.WriteFile(regFile, reg, 0o644)
	out := filepath.Join(dir, "report.json")
	r := invoke(t, nil, nil, "analyze", "--root", repo, "--inputs", inputs, "--baseline", base, "--endpoints", regFile, "--out", out)
	if r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	rep, err := report.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	var mobile report.Layer
	for _, l := range rep.Layers {
		if l.ID == "mobile" {
			mobile = l
		}
	}
	if mobile.Status != report.StatusNotMeasured || mobile.Carried == nil || mobile.Carried.SHA != "bb1595fe0d7c2a61e3f4b5a6978c0d1e2f3a4b5c" || mobile.Carried.Totals["lines"].Covered != 2 {
		t.Errorf("mobile: %+v carried %+v", mobile.Status, mobile.Carried)
	}
	if rep.Baseline == nil || rep.Baseline.File != base || rep.Endpoints == nil || rep.Endpoints.Total != 7 {
		t.Errorf("baseline %+v endpoints %+v", rep.Baseline, rep.Endpoints)
	}
	found := false
	for _, f := range rep.Failures {
		if f.Scope == report.FailEndpoints && f.Key == "svc-api:POST /sums" {
			found = true
		}
	}
	if !found {
		t.Errorf("the violation is not a failure: %+v", rep.Failures)
	}
	// check on the saved report fails on it.
	if c := invoke(t, nil, nil, "check", "--root", repo, "--report", out); c.code != ExitFailed || !strings.Contains(c.stdout, "broke the endpoint baseline") {
		t.Errorf("check: %d %s", c.code, c.stdout)
	}
	// A registry path that is not there yet.
	r = invoke(t, nil, nil, "analyze", "--root", repo, "--inputs", artifacts, "--endpoints", filepath.Join(dir, "none.json"), "--out", out)
	if r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	rep, _ = report.Load(out)
	if rep.Endpoints != nil || !strings.Contains(strings.Join(rep.Warnings, "\n"), "none.json does not exist") {
		t.Errorf("missing registry: %+v %v", rep.Endpoints, rep.Warnings)
	}
	// A registry that is there but broken is an error.
	os.WriteFile(regFile, []byte(`{"version": 1, "endpoints": []}`), 0o644)
	if r := invoke(t, nil, nil, "analyze", "--root", repo, "--inputs", artifacts, "--endpoints", regFile); r.code != ExitError {
		t.Errorf("a broken registry: exit %d", r.code)
	}
}

// The pull_request payload supplies the title and the head commit.
func TestEventPayloadMetadata(t *testing.T) {
	ev := filepath.Join(t.TempDir(), "event.json")
	os.WriteFile(ev, []byte(`{"pull_request": {"title": "fix: a thing", "head": {"sha": "c2b19f4a8e7d6c5b4a39281706f5e4d3c2b1a098"}}}`), 0o644)
	out := filepath.Join(t.TempDir(), "r.json")
	r := invoke(t, map[string]string{"GITHUB_EVENT_PATH": ev}, nil, append([]string{"analyze"}, fixtureArgs("--out", out)...)...)
	if r.code != ExitOK {
		t.Fatal(r.stderr)
	}
	rep, _ := report.Load(out)
	if rep.Metadata.Title != "fix: a thing" || rep.Metadata.HeadSHA != "c2b19f4a8e7d6c5b4a39281706f5e4d3c2b1a098" {
		t.Errorf("metadata %+v", rep.Metadata)
	}
	// Flags win over the payload.
	r = invoke(t, map[string]string{"GITHUB_EVENT_PATH": ev}, nil, append([]string{"analyze"}, fixtureArgs("--title", "mine", "--out", out)...)...)
	rep, _ = report.Load(out)
	if rep.Metadata.Title != "mine" {
		t.Errorf("--title lost to the payload: %q", rep.Metadata.Title)
	}
}
