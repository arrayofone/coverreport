package github

import (
	"regexp"
	"strings"
	"testing"

	"github.com/arrayofone/coverreport/internal/render/rendertest"
	"github.com/arrayofone/coverreport/internal/render/view"
	"github.com/arrayofone/coverreport/internal/source"
)

func states(t *testing.T) map[string]*view.View {
	t.Helper()
	out := map[string]*view.View{}
	for _, s := range rendertest.States(t) {
		out[s.Name] = build(t, s)
	}
	return out
}

var fenceRE = regexp.MustCompile("(?m)^(`{3,})")

// wellFormed checks what GitHub cannot recover from: an unclosed fence
// swallows the rest of the comment, an unclosed <details> hides it.
func wellFormed(t *testing.T, name, md string) {
	t.Helper()
	open := ""
	for _, line := range strings.Split(md, "\n") {
		m := fenceRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch {
		case open == "":
			open = m[1]
		case m[1] == open && strings.TrimRight(line, " ") == open:
			open = ""
		}
	}
	if open != "" {
		t.Errorf("%s: a %s fence is never closed", name, open)
	}
	if o, c := strings.Count(md, "<details"), strings.Count(md, "</details>"); o != c {
		t.Errorf("%s: %d <details> but %d </details>", name, o, c)
	}
}

// The comment's first line is the marker the upsert finds it by, its
// second carries the history the next render reads back, and every state
// renders well-formed markdown.
func TestCommentMarkerStateAndStructure(t *testing.T) {
	for name, v := range states(t) {
		c := Comment(v, Options{})
		lines := strings.SplitN(c, "\n", 3)
		if lines[0] != view.Marker {
			t.Errorf("%s: first line %q", name, lines[0])
		}
		pushes, n := view.ParseState(c)
		if n != v.PushNo || len(pushes) != len(v.Pushes) {
			t.Errorf("%s: state reads back as %d pushes, n %d; want %d, %d", name, len(pushes), n, len(v.Pushes), v.PushNo)
		}
		wellFormed(t, name+"/comment", c)
		wellFormed(t, name+"/summary", Summary(v, Options{}))
		if strings.Contains(c, "coverage-endpoints/endpoints.json") != (v.Endpoints != nil) {
			t.Errorf("%s: endpoint rows present=%v with a registry=%v", name, !(v.Endpoints == nil), v.Endpoints != nil)
		}
	}
}

// Without a registry the endpoint rows are omitted, not an error; with one
// they show per kind, and a violation turns its row red.
func TestEndpointRows(t *testing.T) {
	vs := states(t)
	if s := Comment(vs["warn"], Options{}); strings.Contains(s, "routes") || strings.Contains(s, "Endpoints") {
		t.Error("the warn state has no registry but renders endpoint rows")
	}
	ok := Comment(vs["ok"], Options{})
	for _, want := range []string{"+  ok    routes", "2/4", "<summary>Endpoints fully tested: 2 of 4 routes, 0 of 1 events, 0 of 1 pages, 1 of 1 server actions</summary>"} {
		if !strings.Contains(ok, want) {
			t.Errorf("ok comment lacks %q", want)
		}
	}
	multi := Comment(vs["multi"], Options{})
	for _, want := range []string{"-  FAIL  routes", "-  FAIL  events", "`svc-api:POST /sums` (gained a gap: authz)"} {
		if !strings.Contains(multi, want) {
			t.Errorf("multi comment lacks %q", want)
		}
	}
}

// Rendered without a checkout, the surfaces still name every range and
// link it; they just cannot quote it.
func TestNoSourceDegradesToLineLinks(t *testing.T) {
	var s rendertest.State
	for _, x := range rendertest.States(t) {
		if x.Name == "warn" {
			s = x
		}
	}
	s.SourceDir = ""
	v := build(t, s)
	c := Comment(v, Options{})
	if strings.Contains(c, " │ ") || strings.Contains(c, "<summary><code>calc.go</code>") {
		t.Error("a snippet was rendered with no source")
	}
	if !strings.Contains(c, "calc.go#L35-L39") || !strings.Contains(c, "8 changed lines have no test") {
		t.Error("the ranges were lost with the source")
	}
	for _, a := range v.Annotations {
		if strings.Contains(a.Message, "starting with") {
			t.Errorf("an annotation quotes code it cannot have read: %q", a.Message)
		}
	}
}

// A very large change still yields a comment GitHub accepts, with the
// heading, the HUD, the uncovered-lines table and the footer intact, and a
// note saying what was left out and where it is.
func TestCommentFitsGitHubsLimit(t *testing.T) {
	root, inputs, text := rendertest.Large(t, 40, 10, 10)
	s := rendertest.LargeState(t, root, inputs, text)
	src, _ := source.Open(root)
	v, err := view.Build(s.Report, view.Options{Source: src, ArtifactURL: "https://example.com/a", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	c := Comment(v, Options{})
	if n := utf16Len(c); n > CommentLimit {
		t.Fatalf("comment is %d characters, over GitHub's %d", n, CommentLimit)
	}
	for _, want := range []string{view.Marker, "### 🟡", "```diff\n@@ #561", "changed lines have no test", "> [!NOTE]\n> Shortened to fit", "<sub>Line by line"} {
		if !strings.Contains(c, want) {
			t.Errorf("the shortened comment lost %q", want)
		}
	}
	wellFormed(t, "large comment", c)
	if n := len(Summary(v, Options{})); n > SummaryLimit {
		t.Errorf("summary is %d bytes, over %d", n, SummaryLimit)
	}
	if n := len(v.Annotations); n > view.MaxAnnotations {
		t.Errorf("%d annotations", n)
	}
}

// When nothing droppable is left and the rest still does not fit, the cut
// lands on a line boundary, closes what it cut open, and keeps the footer.
func TestHardCutClosesWhatItCuts(t *testing.T) {
	huge := "<details open>\n<summary>x</summary>\n\n```diff\n" + strings.Repeat("! a line that never ran\n", 400) + "```\n\n</details>\n"
	secs := []section{{name: "head", text: "# head\n\n"}, {name: "body", text: huge}, {name: "footer", text: "<sub>footer</sub>\n"}}
	out, dropped := assemble(secs, 2000, utf16Len, func(d []string) string { return "NOTE " + strings.Join(d, ",") + "\n" })
	if utf16Len(out) > 2000 {
		t.Fatalf("%d over the limit", utf16Len(out))
	}
	if !strings.HasPrefix(out, "# head") || !strings.HasSuffix(out, "<sub>footer</sub>\n") || !strings.Contains(out, "Cut here") {
		t.Errorf("head/footer/cut notice missing:\n%s", out)
	}
	if len(dropped) != 0 {
		t.Errorf("nothing was droppable, yet %v", dropped)
	}
	wellFormed(t, "hard cut", out)
}

func TestDropOrder(t *testing.T) {
	secs := []section{
		{name: "head", text: strings.Repeat("h", 100)},
		{name: "snippets", priority: 5, text: strings.Repeat("s", 300)},
		{name: "snippets", priority: 5, text: strings.Repeat("t", 300)},
		{name: "packages", priority: 2, text: strings.Repeat("p", 300)},
		{name: "lines", text: strings.Repeat("z", 400), alts: []string{strings.Repeat("z", 100)}},
		{name: "footer", text: "f"},
	}
	note := func(d []string) string { return "[" + strings.Join(d, "; ") + "]" }
	out, dropped := assemble(secs, 1150, byteLen, note)
	// The later snippet goes first, then the earlier; packages survive.
	if strings.Contains(out, "ttt") || !strings.Contains(out, "sss") || !strings.Contains(out, "ppp") || strings.Join(dropped, ";") != "1 of 2 snippets" {
		t.Errorf("dropped %v; out %q", dropped, out)
	}
	out, dropped = assemble(secs, 520, byteLen, note)
	if strings.Contains(out, "ppp") || strings.Contains(out, "sss") || strings.Count(out, "z") != 100 {
		t.Errorf("dropped %v; out %q", dropped, out)
	}
	if strings.Join(dropped, ";") != "2 of 2 snippets;packages;rows of the lines table" {
		t.Errorf("dropped %v", dropped)
	}
}

func TestInlineEscaping(t *testing.T) {
	for in, want := range map[string]string{
		"a|b":   "`a\\|b`",
		"a`b":   "`` a`b ``",
		"``x``": "``` ``x`` ```",
		"plain": "`plain`",
	} {
		if got := code(in); got != want {
			t.Errorf("code(%q) = %q, want %q", in, got, want)
		}
	}
	if got := esc("*bold* _x_ [l](u) <b> a|b"); got != `\*bold\* \_x\_ \[l\](u) &lt;b&gt; a\|b` {
		t.Errorf("esc: %q", got)
	}
	if got := fence("a ``` b ````"); got != "`````" {
		t.Errorf("fence %q", got)
	}
	if got := link("x", "https://g/a (b).go"); got != "[x](https://g/a%20%28b%29.go)" {
		t.Errorf("link %q", got)
	}
}

// With one failure the alert is prose, and its second line says what to do
// about that kind of failure.
func TestSingleFailureAlerts(t *testing.T) {
	var multi rendertest.State
	for _, s := range rendertest.States(t) {
		if s.Name == "multi" {
			multi = s
		}
	}
	want := map[string][]string{
		"layer":     {"> **Go unit is at 61.54%, 0.46 below its 62.0 floor**, so the Coverage check fails. None of the 6 changed lines it measures ran.", "or lower `layers.go-unit.statements` in `coverage/floors.json`"},
		"package":   {"> **Package libs/go/calc (go live) is at 62.50%, below its 70.0 floor**", "or lower `packages.go-live.\"libs/go/calc\".statements`", "[`store_pg.go` L9]"},
		"patch":     {"> **Go unit patch coverage is 0.0%, under the 80% target, and the patch gate blocks**", "set `patch.blocking` to false"},
		"endpoints": {"> **The endpoint baseline is broken: 2 endpoints gained a gap**", "> Gaps: `svc-api:POST /sums` (gained a gap: authz); `svc-api:event sum.done`"},
		"required":  {"> **Edge did not report: none of its inputs exist (coverage-edge/lcov.info)**", "> Check that its collector job ran and uploaded its artifact."},
	}
	for scope, lines := range want {
		r := *multi.Report
		r.Failures = nil
		for _, f := range multi.Report.Failures {
			if f.Scope == scope && (scope != "patch" || f.Layer == "go-unit") {
				r.Failures = append(r.Failures, f)
			}
		}
		v, err := view.Build(&r, view.Options{})
		if err != nil {
			t.Fatal(err)
		}
		c := Comment(v, Options{})
		if !strings.Contains(c, "> [!CAUTION]") {
			t.Fatalf("%s: no CAUTION alert", scope)
		}
		for _, w := range lines {
			if !strings.Contains(c, w) {
				t.Errorf("%s: the alert lacks %q\n%s", scope, w, c[:min(len(c), 900)])
			}
		}
	}
}
