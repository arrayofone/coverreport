package action

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// This file is a small GitHub Actions runner for exactly one composite
// action, ../action.yml. It reads the steps from the file, resolves their
// env and with: expressions against a modelled github context, the inputs a
// workflow passed and the outputs earlier steps wrote to $GITHUB_OUTPUT,
// runs each run: step the way `shell: bash` does (bash --noprofile --norc
// -eo pipefail, in the workspace), and stands in for the one upstream action
// the composite uses, actions/upload-artifact. The CLI it drives is built by
// the action's own setup step from this checkout, so a renamed flag, a
// reordered step or a mistyped expression fails here, not in a consumer's
// CI.
//
// It models only what action.yml uses, and refuses the rest: an `if:`, an
// expression that is not a property path, an unknown context, an upstream
// action other than upload-artifact, or an upload-artifact input the
// pinned version does not have is a test failure, so extending action.yml
// past the model means extending the model too.

// actionFile is ../action.yml.
type actionFile struct {
	Name, Description, Author string
	Using                     string
	Inputs                    map[string]actionInput
	Outputs                   map[string]actionOutput
	Steps                     []actionStep
	raw                       string
}

type actionInput struct {
	Description, Default string
	Required             bool
}

type actionOutput struct{ Description, Value string }

type actionStep struct {
	Name, ID, Shell, Run, Uses, If, WorkingDirectory string
	Env, With                                        map[string]string
	Keys                                             []string
}

const actionYML = "../action.yml"

func loadAction(t *testing.T) *actionFile {
	t.Helper()
	data, err := os.ReadFile(actionYML)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parseYAML(string(data))
	if err != nil {
		t.Fatalf("%s: %v", actionYML, err)
	}
	a := &actionFile{raw: string(data), Inputs: map[string]actionInput{}, Outputs: map[string]actionOutput{}}
	m := asMap(t, doc, "action.yml")
	a.Name, a.Description, a.Author = asString(m["name"]), asString(m["description"]), asString(m["author"])
	for name, v := range asMap(t, m["inputs"], "inputs") {
		in := asMap(t, v, "inputs."+name)
		a.Inputs[name] = actionInput{Description: asString(in["description"]), Default: asString(in["default"]), Required: asString(in["required"]) == "true"}
	}
	for name, v := range asMap(t, m["outputs"], "outputs") {
		out := asMap(t, v, "outputs."+name)
		a.Outputs[name] = actionOutput{Description: asString(out["description"]), Value: asString(out["value"])}
	}
	runs := asMap(t, m["runs"], "runs")
	a.Using = asString(runs["using"])
	steps, ok := runs["steps"].([]any)
	if !ok {
		t.Fatalf("runs.steps is %T, want a list", runs["steps"])
	}
	for i, v := range steps {
		s := asMap(t, v, fmt.Sprintf("runs.steps[%d]", i))
		st := actionStep{
			Name: asString(s["name"]), ID: asString(s["id"]), Shell: asString(s["shell"]), Run: asString(s["run"]),
			Uses: asString(s["uses"]), If: asString(s["if"]), WorkingDirectory: asString(s["working-directory"]),
			Env: asStrings(t, s["env"]), With: asStrings(t, s["with"]),
		}
		for k := range s {
			st.Keys = append(st.Keys, k)
		}
		sort.Strings(st.Keys)
		a.Steps = append(a.Steps, st)
	}
	return a
}

func asMap(t *testing.T, v any, what string) map[string]any {
	t.Helper()
	if v == nil {
		return map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want a mapping", what, v)
	}
	return m
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asStrings(t *testing.T, v any) map[string]string {
	t.Helper()
	out := map[string]string{}
	for k, x := range asMap(t, v, "a map of strings") {
		s, ok := x.(string)
		if !ok && x != nil {
			t.Fatalf("%s is %T, want a string", k, x)
		}
		out[k] = s
	}
	return out
}

// uploadArtifactInputs are the inputs of the pinned actions/upload-artifact
// (v7.0.1's action.yml); a with: key outside them is a typo the real
// runner would only warn about.
var uploadArtifactInputs = map[string]bool{
	"name": true, "path": true, "if-no-files-found": true, "retention-days": true, "compression-level": true,
	"overwrite": true, "include-hidden-files": true, "archive": true,
}

var (
	exprRE = regexp.MustCompile(`\$\{\{(.*?)\}\}`)
	pathRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(\.[A-Za-z_][A-Za-z0-9_-]*)*$`)
	usesRE = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)(/[A-Za-z0-9_./-]+)?@([0-9a-f]{40})$`)
)

// upload is one actions/upload-artifact call.
type upload struct {
	With map[string]string
	// Data is the uploaded file as it was when the step ran.
	Data []byte
}

// ghRun is one job running the action once.
type ghRun struct {
	t      *testing.T
	a      *actionFile
	with   map[string]string // the workflow's with: block
	github map[string]string // github.* scalars
	event  map[string]any    // github.event
	env    map[string]string // the runner's environment for every step
	ws     string            // $GITHUB_WORKSPACE
	tmp    string            // where step scripts and command files go
	bash   string

	outputs map[string]map[string]string // steps.<id>.outputs
	stepEnv map[string]map[string]string // each run step's resolved env
	stdout  map[string]string
	stderr  map[string]string
	ran     []string
	summary strings.Builder
	uploads []upload
	// failed is the id of the step that failed ("" when every step passed)
	// and code its exit status.
	failed string
	code   int
}

func (r *ghRun) fatalf(format string, a ...any) {
	r.t.Helper()
	r.t.Fatalf(format, a...)
}

// interpolate replaces every ${{ … }} in s.
func (r *ghRun) interpolate(s string) string {
	r.t.Helper()
	return exprRE.ReplaceAllStringFunc(s, func(m string) string {
		return r.resolve(strings.TrimSpace(exprRE.FindStringSubmatch(m)[1]))
	})
}

// resolve looks up a property path the way the runner does: a missing
// property is null, which interpolates as the empty string.
func (r *ghRun) resolve(e string) string {
	r.t.Helper()
	if !pathRE.MatchString(e) {
		r.fatalf("expression %q is not a property path; the harness models no operators or functions", e)
	}
	p := strings.Split(e, ".")
	switch {
	case p[0] == "inputs" && len(p) == 2:
		in, ok := r.a.Inputs[p[1]]
		if !ok {
			r.fatalf("expression %q names an input action.yml does not declare", e)
		}
		if v, ok := r.with[p[1]]; ok {
			return v
		}
		return r.interpolate(in.Default)
	case p[0] == "steps" && len(p) == 4 && p[2] == "outputs":
		known := false
		for _, s := range r.a.Steps {
			known = known || s.ID == p[1]
		}
		if !known {
			r.fatalf("expression %q names a step action.yml does not have", e)
		}
		return r.outputs[p[1]][p[3]]
	case p[0] == "github" && len(p) >= 2 && p[1] == "event":
		var v any = r.event
		for _, k := range p[2:] {
			m, _ := v.(map[string]any)
			v = m[k]
		}
		switch x := v.(type) {
		case nil:
			return ""
		case string:
			return x
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(x)
		}
		r.fatalf("expression %q is an object; the harness interpolates scalars only", e)
	case p[0] == "github" && len(p) == 2:
		if v, ok := r.github[p[1]]; ok {
			return v
		}
	}
	r.fatalf("expression %q is not modelled", e)
	return ""
}

func (r *ghRun) run() {
	r.t.Helper()
	for _, s := range r.a.Steps {
		if s.If != "" {
			r.fatalf("step %s has an if:, which the harness does not model", s.ID)
		}
		if s.ID == "" {
			r.fatalf("step %q has no id", s.Name)
		}
		r.ran = append(r.ran, s.ID)
		if s.Uses != "" {
			r.uses(s)
		} else {
			r.runStep(s)
		}
		if r.failed != "" {
			return
		}
	}
}

func (r *ghRun) runStep(s actionStep) {
	r.t.Helper()
	if s.Shell != "bash" {
		r.fatalf("step %s: shell %q, the harness runs bash only", s.ID, s.Shell)
	}
	if s.WorkingDirectory != "" {
		r.fatalf("step %s: working-directory is not modelled", s.ID)
	}
	env := map[string]string{}
	for k, v := range r.env {
		env[k] = v
	}
	resolved := map[string]string{}
	for k, v := range s.Env {
		resolved[k] = r.interpolate(v)
		env[k] = resolved[k]
	}
	r.stepEnv[s.ID] = resolved
	outFile := filepath.Join(r.tmp, s.ID+".output")
	sumFile := filepath.Join(r.tmp, s.ID+".summary")
	script := filepath.Join(r.tmp, s.ID+".sh")
	for _, f := range []struct{ path, data string }{{outFile, ""}, {sumFile, ""}, {script, s.Run}} {
		if err := os.WriteFile(f.path, []byte(f.data), 0o644); err != nil {
			r.fatalf("%v", err)
		}
	}
	env["GITHUB_OUTPUT"], env["GITHUB_STEP_SUMMARY"] = outFile, sumFile
	cmd := exec.Command(r.bash, "--noprofile", "--norc", "-eo", "pipefail", script)
	cmd.Dir = r.ws
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r.stdout[s.ID], r.stderr[s.ID] = stdout.String(), stderr.String()
	outs, perr := parseCommandFile(outFile)
	if perr != nil {
		r.fatalf("step %s: $GITHUB_OUTPUT: %v", s.ID, perr)
	}
	r.outputs[s.ID] = outs
	sum, _ := os.ReadFile(sumFile)
	r.summary.Write(sum)
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		r.failed, r.code = s.ID, exit.ExitCode()
	case err != nil:
		r.fatalf("step %s: %v", s.ID, err)
	}
}

// parseCommandFile reads $GITHUB_OUTPUT: name=value lines and
// name<<DELIMITER heredocs.
func parseCommandFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if name, delim, ok := strings.Cut(line, "<<"); ok && !strings.Contains(name, "=") {
			var body []string
			for sc.Scan() && sc.Text() != delim {
				body = append(body, sc.Text())
			}
			out[name] = strings.Join(body, "\n")
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("malformed line %q", line)
		}
		out[name] = value
	}
	return out, sc.Err()
}

func (r *ghRun) uses(s actionStep) {
	r.t.Helper()
	m := usesRE.FindStringSubmatch(s.Uses)
	if m == nil {
		r.fatalf("step %s: uses %q is not pinned to a full commit SHA", s.ID, s.Uses)
	}
	if name := m[1] + m[2]; name != "actions/upload-artifact" {
		r.fatalf("step %s: %s is not modelled", s.ID, name)
	}
	with := map[string]string{}
	for k, v := range s.With {
		if !uploadArtifactInputs[k] {
			r.fatalf("step %s: upload-artifact has no input %q", s.ID, k)
		}
		with[k] = r.interpolate(v)
	}
	path := with["path"]
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.ws, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		r.fatalf("step %s: upload path: %v", s.ID, err)
	}
	r.uploads = append(r.uploads, upload{With: with, Data: data})
	id := strconv.Itoa(9000 + len(r.uploads))
	r.outputs[s.ID] = map[string]string{
		"artifact-id":  id,
		"artifact-url": fmt.Sprintf("%s/%s/actions/runs/%s/artifacts/%s", r.github["server_url"], r.github["repository"], r.github["run_id"], id),
	}
}

// actionOutput evaluates one of the action's own outputs.
func (r *ghRun) actionOutput(name string) string {
	r.t.Helper()
	o, ok := r.a.Outputs[name]
	if !ok {
		r.fatalf("action.yml has no output %q", name)
	}
	return r.interpolate(o.Value)
}

// ghAPI is the comments API the comment command talks to.
type ghAPI struct {
	mu       sync.Mutex
	comments []map[string]any
	requests []apiRequest
	// failWrites answers every POST and PATCH with this status when set.
	failWrites int
}

type apiRequest struct{ Method, Path, Auth, Body string }

func (f *ghAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, apiRequest{r.Method, r.URL.Path, r.Header.Get("Authorization"), string(body)})
	if f.failWrites != 0 && r.Method != http.MethodGet {
		w.WriteHeader(f.failWrites)
		io.WriteString(w, `{"message":"Resource not accessible by integration"}`)
		return
	}
	var in struct{ Body string }
	json.Unmarshal(body, &in)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/issues/42/comments":
		json.NewEncoder(w).Encode(f.comments)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/demo/issues/42/comments":
		c := map[string]any{"id": len(f.comments) + 1, "body": in.Body, "html_url": "https://github.com/acme/demo/pull/42#new"}
		f.comments = append(f.comments, c)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(c)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/acme/demo/issues/comments/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/demo/issues/comments/"))
		if id < 1 || id > len(f.comments) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.comments[id-1]["body"] = in.Body
		json.NewEncoder(w).Encode(f.comments[id-1])
	default:
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Not Found"}`)
	}
}

func (f *ghAPI) writes() []apiRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiRequest
	for _, q := range f.requests {
		if q.Method != http.MethodGet {
			out = append(out, q)
		}
	}
	return out
}
