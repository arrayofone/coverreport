// Package action holds the tests of the composite action at the root of
// the repository (../action.yml) and the script its steps run (run.sh).
// There is no Go code to ship here: the action builds cmd/coverreport.
package action

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/arrayofone/coverreport/internal/render/view"
	"github.com/arrayofone/coverreport/internal/report"
)

const (
	repoFixture      = "../testdata/repo"
	artifactsFixture = "../testdata/artifacts"
	token            = "ghs_harnessTOKEN0123456789abcdef"
	// prHead is the pull request's head commit in the event payload.
	prHead  = "c2b19f4a8e7d6c5b4a39281706f5e4d3c2b1a098"
	pageURL = "https://github.com/acme/demo/actions/runs/1234/artifacts/9001"
)

// addedByHead are the files the fixture's head commit adds on top of its
// base, so HEAD^1..HEAD is a real diff with changed lines in two layers.
var addedByHead = []string{"libs/go/calc/store_pg.go", "apps/web/src/components/button.tsx"}

// scenario is one job running the action.
type scenario struct {
	event string // pull_request (the default) or push
	fork  bool   // the pull request's head is another repository
	with  map[string]string
	// prepare edits the fixture checkout before it is committed; artifacts
	// edits the downloaded artifacts directory.
	prepare, artifacts func(t *testing.T, ws string)
	api                *ghAPI
	shallow            bool // a one-commit checkout (fetch-depth 1)
	noGo               bool // no go on PATH
	// env edits the job's environment (the workspace is ws).
	env func(ws string, env map[string]string)
}

type result struct {
	*ghRun
	api        *ghAPI
	head, base string
	runnerTemp string
}

func runAction(t *testing.T, sc scenario) *result {
	t.Helper()
	ws := t.TempDir()
	if err := os.CopyFS(ws, os.DirFS(repoFixture)); err != nil {
		t.Fatal(err)
	}
	if sc.prepare != nil {
		sc.prepare(t, ws)
	}
	head, base := commitFixture(t, ws, sc.shallow)
	if err := os.CopyFS(filepath.Join(ws, "coverage-artifacts"), os.DirFS(artifactsFixture)); err != nil {
		t.Fatal(err)
	}
	if sc.artifacts != nil {
		sc.artifacts(t, filepath.Join(ws, "coverage-artifacts"))
	}
	api := sc.api
	if api == nil {
		api = &ghAPI{}
	}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	event, ref := sc.event, "refs/heads/main"
	if event == "" {
		event = "pull_request"
	}
	payload := map[string]any{"repository": map[string]any{"full_name": "acme/demo"}}
	if event == "pull_request" {
		ref = "refs/pull/42/merge"
		headRepo := "acme/demo"
		if sc.fork {
			headRepo = "someone/demo"
		}
		payload["pull_request"] = map[string]any{
			"number": 42, "title": "feat: sums",
			"head": map[string]any{"sha": prHead, "ref": "feat/sums", "repo": map[string]any{"full_name": headRepo}},
			"base": map[string]any{"sha": base, "ref": "main"},
		}
	}
	data, _ := json.Marshal(payload)
	eventFile := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	json.Unmarshal(data, &ev)

	actionPath, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	runnerTemp := t.TempDir()
	env := map[string]string{
		"GITHUB_ACTION_PATH": actionPath,
		"GITHUB_WORKSPACE":   ws,
		"GITHUB_EVENT_NAME":  event,
		"GITHUB_EVENT_PATH":  eventFile,
		"GITHUB_REPOSITORY":  "acme/demo",
		"GITHUB_REF":         ref,
		"GITHUB_SHA":         head,
		"GITHUB_RUN_ID":      "1234",
		"GITHUB_SERVER_URL":  "https://github.com",
		"GITHUB_API_URL":     srv.URL,
		"RUNNER_TEMP":        runnerTemp,
	}
	if event == "pull_request" {
		env["GITHUB_HEAD_REF"], env["GITHUB_BASE_REF"] = "feat/sums", "main"
	}
	// Only what a build needs comes from the test's own environment; in
	// particular no GITHUB_TOKEN, so the one the fake API sees can only
	// have come through the github-token input.
	for _, k := range []string{"PATH", "HOME", "GOCACHE", "GOPATH", "GOMODCACHE", "GOROOT", "XDG_CACHE_HOME", "TMPDIR"} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
	}
	if sc.noGo {
		env["PATH"] = withoutGo(t, env["PATH"])
	}
	if sc.env != nil {
		sc.env(ws, env)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("the action's steps run under bash, and there is none on PATH")
	}
	r := &ghRun{
		t: t, a: loadAction(t), with: sc.with, event: ev, env: env, ws: ws, tmp: t.TempDir(), bash: bash,
		github: map[string]string{
			"token": token, "repository": "acme/demo", "run_id": "1234", "server_url": "https://github.com",
			"event_name": event, "sha": head, "ref": ref,
		},
		outputs: map[string]map[string]string{}, stepEnv: map[string]map[string]string{},
		stdout: map[string]string{}, stderr: map[string]string{},
	}
	r.run()
	return &result{ghRun: r, api: api, head: head, base: base, runnerTemp: runnerTemp}
}

// commitFixture makes the checkout a git repository: a base commit without
// addedByHead, then the head commit with everything (or, shallow, only the
// head commit, as a fetch-depth 1 checkout sees it).
func commitFixture(t *testing.T, ws string, shallow bool) (head, base string) {
	t.Helper()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", ws, "-c", "user.name=coverreport", "-c", "user.email=coverreport@example.com",
			"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_DATE=2026-09-28T12:00:00Z", "GIT_COMMITTER_DATE=2026-09-28T12:00:00Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "-A")
	if !shallow {
		git(append([]string{"rm", "-q", "--cached"}, addedByHead...)...)
		git("commit", "-q", "-m", "base")
		git("add", "-A")
	}
	git("commit", "-q", "-m", "head")
	head = git("rev-parse", "HEAD")
	if !shallow {
		base = git("rev-parse", "HEAD^1")
	}
	return head, base
}

// withoutGo drops every PATH entry that holds a go binary.
func withoutGo(t *testing.T, path string) string {
	t.Helper()
	var keep []string
	for _, dir := range filepath.SplitList(path) {
		if _, err := os.Stat(filepath.Join(dir, "go")); err != nil {
			keep = append(keep, dir)
		}
	}
	return strings.Join(keep, string(filepath.ListSeparator))
}

func (r *result) dir() string { return r.outputs["setup"]["dir"] }

func (r *result) read(name string) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir(), name))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func (r *result) report() *report.Report {
	r.t.Helper()
	rep, err := report.Load(r.actionOutput("report"))
	if err != nil {
		r.t.Fatalf("the report output: %v", err)
	}
	return rep
}

// logs is everything every step printed.
func (r *result) logs() string {
	var b strings.Builder
	for _, id := range r.ran {
		b.WriteString(r.stdout[id])
		b.WriteString(r.stderr[id])
	}
	return b.String()
}

func (r *result) expectFailedAt(id string, code int) {
	r.t.Helper()
	if r.failed != id || r.code != code {
		r.t.Fatalf("failed at %q with %d, want %q with %d; ran %v\n%s", r.failed, r.code, id, code, r.ran, r.logs())
	}
}

func (r *result) expectPassed() {
	r.t.Helper()
	if r.failed != "" {
		r.t.Fatalf("step %s failed with %d; ran %v\n%s", r.failed, r.code, r.ran, r.logs())
	}
}

// passingFloors lowers the one floor the fixture breaks (go-unit measures
// 61.54 against 62.0) so every gate holds.
func passingFloors(t *testing.T, ws string) {
	t.Helper()
	p := filepath.Join(ws, "coverage", "floors.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	fixed := bytes.Replace(b, []byte(`"go-unit": { "statements": 62.0 }`), []byte(`"go-unit": { "statements": 61.5 }`), 1)
	if bytes.Equal(fixed, b) {
		t.Fatal("the fixture's go-unit floor moved; update passingFloors")
	}
	os.WriteFile(p, fixed, 0o644)
}

func abs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A same-repository pull request, every gate but one holding: every step
// runs in order, each one's product is what the next consumed, the comment
// carries the page's link and the push history forward, and the check step
// fails the action on the broken floor after everything was published.
func TestActionOnAPullRequest(t *testing.T) {
	t.Parallel()
	prior := view.Marker + "\n" + `<!-- coverreport:state {"n":3,"pushes":[["aaaaaaa",50.5],["bbbbbbb",60.5],["ccccccc",70.5]]} -->` + "\n### an older push\n"
	api := &ghAPI{comments: []map[string]any{
		{"id": 1, "body": "LGTM", "html_url": "https://github.com/acme/demo/pull/42#1"},
		{"id": 2, "body": prior, "html_url": "https://github.com/acme/demo/pull/42#2"},
	}}
	r := runAction(t, scenario{api: api, with: map[string]string{"require": "go-unit,go-live"}})
	r.expectFailedAt("check", 1)
	if want := []string{"setup", "fetch", "analyze", "render", "page", "publish", "check"}; !slices.Equal(r.ran, want) {
		t.Fatalf("ran %v, want %v", r.ran, want)
	}

	// fetch wrote the sticky comment it found, and nothing else.
	if got := r.read("previous.md"); got != prior {
		t.Errorf("previous.md is %q, want the sticky comment's body", got)
	}

	// analyze: the report output is the file, its diff is HEAD^1..HEAD of
	// the checkout, and the payload's title and head commit reached it.
	if got, want := r.actionOutput("report"), filepath.Join(r.dir(), "report.json"); got != want {
		t.Errorf("report output %q, want %q", got, want)
	}
	rep := r.report()
	m := rep.Metadata
	if m.PR != 42 || m.Title != "feat: sums" || m.HeadSHA != prHead || m.SHA != r.head || m.Base != r.base || m.Repo != "acme/demo" {
		t.Errorf("metadata %+v", m)
	}
	if rep.Patch.Diff == nil || rep.Patch.Diff.Files != len(addedByHead) {
		t.Errorf("patch diff %+v, want the %d files the head commit added", rep.Patch.Diff, len(addedByHead))
	}

	// The page went up under its artifact name, unarchived, for a day, and
	// is the page the second render left (links do not change it).
	if len(r.uploads) != 1 {
		t.Fatalf("%d uploads", len(r.uploads))
	}
	up := r.uploads[0].With
	if up["path"] != filepath.Join(r.dir(), "page", "coverreport-42.html") || r.outputs["render"]["page-name"] != "coverreport-42.html" {
		t.Errorf("uploaded %q as %q", up["path"], r.outputs["render"]["page-name"])
	}
	if up["archive"] != "false" || up["retention-days"] != "1" || up["overwrite"] != "true" || up["if-no-files-found"] != "error" {
		t.Errorf("upload inputs %v", up)
	}
	if html := r.read("report.html"); string(r.uploads[0].Data) != html || !strings.HasPrefix(html, "<!doctype html>") {
		t.Error("the uploaded page is not the rendered report.html")
	}

	// publish: the comment is comment.md, upserted in place with the token,
	// and it links the page and carries the history on (push 4 of 4).
	comment := r.read("comment.md")
	for _, want := range []string{
		`{"n":4,"pushes":[["aaaaaaa",50.5],["bbbbbbb",60.5],["ccccccc",70.5],["c2b19f4",`,
		pageURL, "coverreport-42.html",
	} {
		if !strings.Contains(comment, want) {
			t.Errorf("comment.md lacks %q", want)
		}
	}
	writes := r.api.writes()
	if len(writes) != 1 || writes[0].Method != http.MethodPatch || writes[0].Path != "/repos/acme/demo/issues/comments/2" {
		t.Fatalf("writes %+v, want one PATCH of comment 2", writes)
	}
	var sent struct{ Body string }
	json.Unmarshal([]byte(writes[0].Body), &sent)
	if sent.Body != comment {
		t.Error("the comment sent is not comment.md")
	}
	for _, q := range r.api.requests {
		if q.Auth != "Bearer "+token {
			t.Errorf("%s %s: Authorization %q", q.Method, q.Path, q.Auth)
		}
	}
	if len(r.api.requests) != 3 {
		t.Errorf("%d API requests, want 3 (the fetch, the upsert's read, its write)", len(r.api.requests))
	}

	// The job summary is summary.md, linking the page; the annotations are
	// on stdout, as workflow commands.
	summary := r.read("summary.md")
	if r.summary.String() != summary || !strings.Contains(summary, pageURL) {
		t.Errorf("job summary (%d bytes) is not summary.md (%d bytes) with the page's link", r.summary.Len(), len(summary))
	}
	ann := r.read("annotations.txt")
	if !strings.Contains(ann, "::notice file=apps/web/src/components/button.tsx") || !strings.Contains(r.stdout["publish"], ann) {
		t.Errorf("annotations %q not printed by publish:\n%s", ann, r.stdout["publish"])
	}

	// check: the broken floor, in the log and as an error annotation whose
	// % is escaped the way the runner unescapes it.
	if out := r.stdout["check"]; !strings.Contains(out, "  - go-unit statements is 61.54%, below its floor 62.0") ||
		!regexp.MustCompile(`(?m)^::error title=coverreport::go-unit statements is 61\.54%25, below its floor 62\.0`).MatchString(out) {
		t.Errorf("check output:\n%s", out)
	}

	if got := r.actionOutput("page-url"); got != pageURL {
		t.Errorf("page-url output %q", got)
	}
	if !strings.HasPrefix(r.actionOutput("dir"), r.runnerTemp+string(filepath.Separator)) {
		t.Errorf("dir output %q is not under RUNNER_TEMP", r.actionOutput("dir"))
	}
	if strings.Contains(r.logs(), token) {
		t.Error("the token was printed")
	}
}

// A fork's pull request: the comment is read (the history) but never
// written, with a notice saying why; everything else still happens.
func TestActionOnAForkPullRequest(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{fork: true})
	r.expectFailedAt("check", 1)
	if w := r.api.writes(); len(w) != 0 || len(r.api.requests) != 1 {
		t.Errorf("requests %+v, want the one read", r.api.requests)
	}
	if !strings.Contains(r.stdout["publish"], "::notice title=coverreport::No sticky comment: this pull request comes from someone/demo") {
		t.Errorf("publish:\n%s", r.stdout["publish"])
	}
	if r.summary.Len() == 0 || len(r.uploads) != 1 {
		t.Error("a fork's run still gets the summary and the page")
	}
}

// A push: no pull request, so no comment and no API call at all; the page
// is named after the run. A baseline that is not there yet is a notice.
func TestActionOnAPush(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{event: "push", with: map[string]string{"baseline": "coverreport-baseline/report.json"}})
	r.expectFailedAt("check", 1)
	if len(r.api.requests) != 0 {
		t.Errorf("requests %+v, want none", r.api.requests)
	}
	if !strings.Contains(r.stdout["publish"], "::notice title=coverreport::No sticky comment: this is a push run") {
		t.Errorf("publish:\n%s", r.stdout["publish"])
	}
	if !strings.Contains(r.stdout["analyze"], "::notice title=coverreport::No base-branch report at coverreport-baseline/report.json yet") {
		t.Errorf("analyze:\n%s", r.stdout["analyze"])
	}
	if got := filepath.Base(r.uploads[0].With["path"]); got != "coverreport-1234.html" {
		t.Errorf("page %q", got)
	}
	rep := r.report()
	if rep.Metadata.PR != 0 || rep.Baseline != nil || rep.Metadata.Base != r.base {
		t.Errorf("metadata %+v baseline %+v", rep.Metadata, rep.Baseline)
	}
}

// Every gate holding, with every optional input set: the action passes,
// and each input reached the command it belongs to.
func TestActionPassesWithEveryInput(t *testing.T) {
	t.Parallel()
	baseline, endpoints, diff := abs(t, "../testdata/golden/report.json"), abs(t, "../testdata/render/endpoints.json"), abs(t, "../testdata/change.diff")
	r := runAction(t, scenario{
		prepare: passingFloors,
		// This run's mobile collector did not run: the baseline fills it.
		artifacts: func(t *testing.T, dir string) { os.RemoveAll(filepath.Join(dir, "coverage-mobile")) },
		with: map[string]string{
			"require": "go-unit", "baseline": baseline, "endpoints": endpoints, "diff": diff,
			"out-dir": "cov-out", "page-name": "custom.html", "retention-days": "3",
		},
	})
	r.expectPassed()
	if !strings.Contains(r.stdout["check"], "coverreport: pass") {
		t.Errorf("check:\n%s", r.stdout["check"])
	}
	if got, want := r.dir(), filepath.Join(r.ws, "cov-out"); got != want {
		t.Errorf("dir %q, want %q (out-dir, made absolute)", got, want)
	}
	rep := r.report()
	var mobile *report.Layer
	for i := range rep.Layers {
		if rep.Layers[i].ID == "mobile" {
			mobile = &rep.Layers[i]
		}
	}
	if mobile == nil || mobile.Status != report.StatusNotMeasured || mobile.Carried == nil || rep.Baseline == nil || rep.Baseline.File != baseline {
		t.Errorf("baseline not carried: mobile %+v baseline %+v", mobile, rep.Baseline)
	}
	if rep.Endpoints == nil || rep.Endpoints.Total != 7 {
		t.Errorf("endpoints %+v", rep.Endpoints)
	}
	// The diff file won over diff-base's default: its rename is there, and
	// no base commit was resolved.
	if d := rep.Patch.Diff; d == nil || len(d.Renamed) != 1 || d.Renamed[0].To != "apps/web/src/lib/money.ts" || rep.Metadata.Base != "" {
		t.Errorf("diff %+v base %q", rep.Patch.Diff, rep.Metadata.Base)
	}
	up := r.uploads[0].With
	if filepath.Base(up["path"]) != "custom.html" || up["retention-days"] != "3" {
		t.Errorf("upload %v", up)
	}
	if !strings.Contains(r.read("comment.md"), "custom.html") {
		t.Error("the comment does not name the page by its artifact name")
	}
	if w := r.api.writes(); len(w) != 1 || w[0].Method != http.MethodPost {
		t.Errorf("writes %+v, want the comment created", w)
	}
}

// A required layer whose collector reported nothing fails the check, and
// the surfaces already say so (analyze got require too).
func TestActionRequiredLayerMissing(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{prepare: passingFloors, with: map[string]string{"require": "go-unit,edge"}})
	r.expectFailedAt("check", 1)
	if !strings.Contains(r.stdout["check"], "::error title=coverreport::layer edge is required") {
		t.Errorf("check:\n%s", r.stdout["check"])
	}
	found := false
	for _, f := range r.report().Failures {
		found = found || (f.Scope == report.FailRequired && f.Layer == "edge")
	}
	if !found {
		t.Error("analyze did not get require: the report has no required-layer failure")
	}
}

// No go on PATH: the first step says what to do, and nothing else runs.
func TestActionWithoutGo(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{noGo: true})
	r.expectFailedAt("setup", 1)
	if !strings.Contains(r.stdout["setup"], "::error title=coverreport::There is no go on PATH.") || !strings.Contains(r.stdout["setup"], "(Go 1.25 or newer") {
		t.Errorf("setup:\n%s%s", r.stdout["setup"], r.stderr["setup"])
	}
	if len(r.ran) != 1 || len(r.api.requests) != 0 {
		t.Errorf("ran %v, requests %d", r.ran, len(r.api.requests))
	}
}

// The job's own Go settings never reach the build: a go.work that does not
// list the action's module, a toolchain that does not exist, and a
// job-wide GOFLAGS=-race (which needs cgo, pinned off) would each fail it.
// GOPROXY=off only keeps the toolchain lookup off the network if that pin
// ever goes.
func TestActionBuildIgnoresTheJobsGoEnvironment(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{env: func(ws string, env map[string]string) {
		env["GOWORK"] = filepath.Join(ws, "go.work")
		env["GOTOOLCHAIN"] = "go1.99.0"
		env["GOFLAGS"] = "-race"
		env["GOPROXY"] = "off"
	}})
	r.expectFailedAt("check", 1)
	if _, err := os.Stat(r.outputs["setup"]["bin"]); err != nil {
		t.Errorf("no binary: %v\n%s", err, r.logs())
	}
}

// A comment that cannot be posted does not hide the gate: the summary and
// annotations are published, the check runs and passes, and the action
// still fails, naming the comment.
func TestActionCommentFailure(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{prepare: passingFloors, api: &ghAPI{failWrites: http.StatusForbidden}})
	r.expectFailedAt("check", 1)
	if !strings.Contains(r.stdout["publish"], "::error title=coverreport::The sticky comment could not be posted") || r.summary.Len() == 0 {
		t.Errorf("publish:\n%s", r.stdout["publish"])
	}
	if out := r.stdout["check"]; !strings.Contains(out, "coverreport: pass") || !strings.Contains(out, "::error title=coverreport::The sticky comment could not be posted (see the publish step)") {
		t.Errorf("check:\n%s", out)
	}
	if strings.Contains(r.logs(), token) {
		t.Error("the token was printed")
	}
}

// A fetch-depth 1 checkout has no HEAD^1: analyze says so and stops.
func TestActionShallowCheckout(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{shallow: true})
	r.expectFailedAt("analyze", 1)
	if !strings.Contains(r.stdout["analyze"], "::error title=coverreport::diff-base HEAD^1 is not a commit in .") || !strings.Contains(r.stdout["analyze"], "fetch-depth: 2") {
		t.Errorf("analyze:\n%s", r.stdout["analyze"])
	}
}

// comment: false on a pull request: no API call at all.
func TestActionCommentDisabled(t *testing.T) {
	t.Parallel()
	r := runAction(t, scenario{prepare: passingFloors, with: map[string]string{"comment": "false"}})
	r.expectPassed()
	if len(r.api.requests) != 0 || !strings.Contains(r.stdout["publish"], "comment is false; no sticky comment") {
		t.Errorf("requests %+v\n%s", r.api.requests, r.stdout["publish"])
	}
	if r := runAction(t, scenario{with: map[string]string{"comment": "yes"}}); r.failed != "fetch" || !strings.Contains(r.stdout["fetch"], "comment must be true or false, not yes") {
		t.Errorf("comment: yes ran %v:\n%s", r.ran, r.logs())
	}
}

// The file itself: a composite action with described inputs and outputs,
// whose outputs name real steps.
func TestActionMetadata(t *testing.T) {
	a := loadAction(t)
	if a.Name == "" || a.Description == "" || a.Author == "" || a.Using != "composite" {
		t.Errorf("name %q description %q author %q using %q", a.Name, a.Description, a.Author, a.Using)
	}
	if len(a.Inputs) == 0 || len(a.Outputs) == 0 || len(a.Steps) == 0 {
		t.Fatal("no inputs, outputs or steps")
	}
	for name, in := range a.Inputs {
		if in.Description == "" || (in.Required && in.Default != "") {
			t.Errorf("input %s: %+v", name, in)
		}
	}
	outRE := regexp.MustCompile(`^\$\{\{ steps\.([a-z0-9-]+)\.outputs\.([a-z0-9-]+) \}\}$`)
	for name, o := range a.Outputs {
		m := outRE.FindStringSubmatch(o.Value)
		if o.Description == "" || m == nil || !slices.ContainsFunc(a.Steps, func(s actionStep) bool { return s.ID == m[1] }) {
			t.Errorf("output %s: %+v", name, o)
		}
	}
}

// Every step is named and has an id; every upstream action is pinned to a
// full commit with its version beside it; every run: step is `bash run.sh
// <phase>` with no expression in it, the phases in run.sh's own order; and
// a step output is only read by a later step.
func TestActionSteps(t *testing.T) {
	a := loadAction(t)
	script, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^([a-z]+(?: \| [a-z]+)*)\) "\$1" ;;$`).FindSubmatch(script)
	if m == nil {
		t.Fatal("run.sh's phase dispatch not found")
	}
	phases := strings.Split(string(m[1]), " | ")
	runRE := regexp.MustCompile(`^bash "\$GITHUB_ACTION_PATH/action/run\.sh" ([a-z]+)$`)
	var used []string
	seen := map[string]bool{}
	for _, s := range a.Steps {
		if s.Name == "" || s.ID == "" || seen[s.ID] {
			t.Errorf("step %q: a name and a unique id are required", s.ID)
		}
		for _, v := range append(mapValues(s.Env), mapValues(s.With)...) {
			for _, ref := range regexp.MustCompile(`steps\.([a-z0-9-]+)\.outputs`).FindAllStringSubmatch(v, -1) {
				if !seen[ref[1]] {
					t.Errorf("step %s reads steps.%s, which has not run yet", s.ID, ref[1])
				}
			}
		}
		seen[s.ID] = true
		if s.Uses != "" {
			if !usesRE.MatchString(s.Uses) || !regexp.MustCompile(`(?m)^\s+uses: `+regexp.QuoteMeta(s.Uses)+` # v\d+\.\d+\.\d+$`).MatchString(a.raw) {
				t.Errorf("step %s: uses %q must be a full commit SHA followed by # vX.Y.Z", s.ID, s.Uses)
			}
			if s.Run != "" || s.Shell != "" || len(s.Env) != 0 {
				t.Errorf("step %s mixes uses with run, shell or env", s.ID)
			}
			continue
		}
		pm := runRE.FindStringSubmatch(s.Run)
		if s.Shell != "bash" || pm == nil || strings.Contains(s.Run, "${{") {
			t.Errorf("step %s: shell %q run %q", s.ID, s.Shell, s.Run)
			continue
		}
		used = append(used, pm[1])
	}
	if !slices.Equal(used, phases) {
		t.Errorf("action.yml runs phases %v, run.sh dispatches %v", used, phases)
	}
}

func mapValues(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// Every input is read by some step, and the token by exactly the two steps
// that talk to the comments API, which scope it to that one command.
func TestActionInputsAreAllWired(t *testing.T) {
	a := loadAction(t)
	refRE := regexp.MustCompile(`\$\{\{ inputs\.([a-z0-9-]+) \}\}`)
	used := map[string]bool{}
	var tokenSteps []string
	for _, s := range a.Steps {
		for k, v := range s.Env {
			if k == "GITHUB_TOKEN" {
				t.Errorf("step %s puts GITHUB_TOKEN in its whole environment", s.ID)
			}
			for _, m := range refRE.FindAllStringSubmatch(v, -1) {
				used[m[1]] = true
				if m[1] == "github-token" {
					tokenSteps = append(tokenSteps, s.ID)
				}
			}
		}
		for _, v := range s.With {
			for _, m := range refRE.FindAllStringSubmatch(v, -1) {
				used[m[1]] = true
			}
		}
	}
	for name := range a.Inputs {
		if !used[name] {
			t.Errorf("input %s is declared and never read", name)
		}
	}
	for name := range used {
		if _, ok := a.Inputs[name]; !ok {
			t.Errorf("inputs.%s is read and never declared", name)
		}
	}
	if !slices.Equal(tokenSteps, []string{"fetch", "publish"}) {
		t.Errorf("the token reaches steps %v, want fetch and publish", tokenSteps)
	}
}

// The README documents exactly the action's inputs (with their defaults)
// and outputs, and embeds examples/coverage.yml verbatim.
func TestReadmeDocumentsTheAction(t *testing.T) {
	a := loadAction(t)
	b, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)
	table := func(heading string) map[string]string {
		t.Helper()
		_, sec, ok := strings.Cut(readme, "\n"+heading+"\n")
		if !ok {
			t.Fatalf("README has no %q section", heading)
		}
		if i := strings.Index(sec, "\n#"); i >= 0 {
			sec = sec[:i]
		}
		rows := map[string]string{}
		for _, m := range regexp.MustCompile("(?m)^\\| `([a-z0-9-]+)` \\| ([^|]*?) \\|").FindAllStringSubmatch(sec, -1) {
			rows[m[1]] = m[2]
		}
		return rows
	}
	inputs := table("### Inputs")
	for name, in := range a.Inputs {
		want := "none"
		if in.Default != "" {
			want = "`" + in.Default + "`"
		}
		if got, ok := inputs[name]; !ok || got != want {
			t.Errorf("README input %s: default cell %q, want %q", name, got, want)
		}
	}
	for name := range inputs {
		if _, ok := a.Inputs[name]; !ok {
			t.Errorf("README documents input %s, which action.yml does not have", name)
		}
	}
	outputs := table("### Outputs")
	if got, want := sortedKeys(outputs), sortedKeys(a.Outputs); !slices.Equal(got, want) {
		t.Errorf("README outputs %v, action.yml outputs %v", got, want)
	}
	example, err := os.ReadFile("../examples/coverage.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readme, "```yaml\n"+string(example)+"```\n") {
		t.Error("README does not embed examples/coverage.yml verbatim")
	}
}

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The example workflow uses only inputs the action has, pins every other
// action to a full commit, and pins upload-artifact where action.yml does.
func TestExampleWorkflow(t *testing.T) {
	a := loadAction(t)
	raw, err := os.ReadFile("../examples/coverage.yml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parseYAML(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var ours []string
	for job, v := range asMap(t, asMap(t, doc, "workflow")["jobs"], "jobs") {
		steps, _ := asMap(t, v, job)["steps"].([]any)
		if len(steps) == 0 {
			t.Errorf("job %s has no steps", job)
		}
		for _, sv := range steps {
			s := asMap(t, sv, job+" step")
			uses := asString(s["uses"])
			switch {
			case uses == "":
			case strings.HasPrefix(uses, "arrayofone/coverreport@"):
				ours = append(ours, uses)
				for k := range asStrings(t, s["with"]) {
					if _, ok := a.Inputs[k]; !ok {
						t.Errorf("the example passes %s, which the action does not have", k)
					}
				}
			case !usesRE.MatchString(uses) || !strings.Contains(string(raw), "uses: "+uses+" # v"):
				t.Errorf("the example's %s is not pinned to a full commit with its version beside it", uses)
			case strings.HasPrefix(uses, "actions/upload-artifact@"):
				if !strings.Contains(a.raw, "uses: "+uses+" ") {
					t.Errorf("the example pins %s, action.yml another", uses)
				}
			}
		}
	}
	if len(ours) != 1 {
		t.Errorf("the example uses the action %d times", len(ours))
	}
}
