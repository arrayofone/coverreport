package html

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/arrayofone/coverreport/internal/render/rendertest"
	"github.com/arrayofone/coverreport/internal/render/view"
	"github.com/arrayofone/coverreport/internal/report"
)

func pages(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, s := range rendertest.States(t) {
		out[s.Name] = render(t, build(t, s))
	}
	return out
}

var (
	tagRE   = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)\b((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	idRE    = regexp.MustCompile(`\sid="([^"]+)"`)
	hrefRE  = regexp.MustCompile(`\shref="#([^"]*)"`)
	urlAttr = regexp.MustCompile(`\s(src|srcset|action|formaction|poster|data|background|xlink:href)=`)
)

// The page is one file that loads nothing and runs nothing: no script, no
// external stylesheet or font, no image, no style attribute, and every
// http(s) URL is a link someone clicks, never a resource the page fetches.
func TestPageIsSelfContained(t *testing.T) {
	for name, page := range pages(t) {
		lower := strings.ToLower(page)
		for _, bad := range []string{"<script", "<link", "<img", "<iframe", "<object", "<embed", "@import", "@font-face", " style=", "javascript:"} {
			if strings.Contains(lower, bad) {
				t.Errorf("%s: contains %q", name, bad)
			}
		}
		if m := urlAttr.FindString(page); m != "" {
			t.Errorf("%s: a resource attribute %q", name, m)
		}
		for _, u := range regexp.MustCompile(`url\(([^)]*)\)`).FindAllStringSubmatch(page, -1) {
			if u[1] != "#hatch" {
				t.Errorf("%s: CSS url(%s) is not the in-page hatch pattern", name, u[1])
			}
		}
		// Every http(s) occurrence must be inside an <a href="...">.
		stripped := regexp.MustCompile(`<a\s[^>]*href="https?://[^"]*"`).ReplaceAllString(page, "<a")
		if i := strings.Index(stripped, "http://"); i >= 0 {
			t.Errorf("%s: an http:// outside a link: %q", name, stripped[max(0, i-60):i+40])
		}
		if i := strings.Index(stripped, "https://"); i >= 0 {
			t.Errorf("%s: an https:// outside a link: %q", name, stripped[max(0, i-60):i+40])
		}
		if strings.Count(page, "<style>") != 1 || !strings.Contains(page, "@media (prefers-color-scheme: light)") {
			t.Errorf("%s: want exactly one inline <style> with a light scheme", name)
		}
	}
}

var void = map[string]bool{"meta": true, "br": true, "hr": true, "input": true, "wbr": true, "img": true, "link": true}

// The page is well formed: every element closes, in order, every id is
// unique, and every in-page link lands on an id that exists.
func TestPageIsWellFormedAndItsAnchorsResolve(t *testing.T) {
	for name, page := range pages(t) {
		body := regexp.MustCompile(`(?s)<style>.*?</style>`).ReplaceAllString(page, "<style></style>")
		var stack []string
		for _, m := range tagRE.FindAllStringSubmatch(body, -1) {
			closing, tag, attrs := m[1] == "/", strings.ToLower(m[2]), m[3]
			switch {
			case closing:
				if len(stack) == 0 || stack[len(stack)-1] != tag {
					t.Fatalf("%s: </%s> closes %v", name, tag, stack[max(0, len(stack)-3):])
				}
				stack = stack[:len(stack)-1]
			case void[tag] || strings.HasSuffix(strings.TrimSpace(attrs), "/"):
			default:
				stack = append(stack, tag)
			}
		}
		if len(stack) != 0 {
			t.Errorf("%s: unclosed %v", name, stack)
		}
		ids := map[string]int{}
		for _, m := range idRE.FindAllStringSubmatch(page, -1) {
			ids[m[1]]++
		}
		for id, n := range ids {
			if n > 1 {
				t.Errorf("%s: id %q appears %d times", name, id, n)
			}
		}
		for _, m := range hrefRE.FindAllStringSubmatch(page, -1) {
			if ids[m[1]] == 0 {
				t.Errorf("%s: #%s links nowhere", name, m[1])
			}
		}
	}
}

// What the design puts on the page, per state.
func TestPageContent(t *testing.T) {
	p := pages(t)
	fail := p["fail"]
	for _, want := range []string{
		`<a class="ann ann-fail" href="#attention"><span class="lamp"></span><b>1 floor broken</b>`,
		`<figure class="g g-fail">`,
		`<figure class="g g-info">`,
		`<figure class="g g-notrun">`,
		`Go unit is at 61.54%, 0.46 below its 62.0 floor.`,
		`<a href="#f1-L17">cover calc.go L17–18, L35–39</a>`,
		`<tr class="unc" id="f1-L17">`,
		`<tr class="cov" id="f3-L7">`,
		`aria-label="Patch coverage by push: push 1 58.3%, push 2 76.9%, push 3 83.1%, push 4 46.7%"`,
		`<span class="wordmark">handipay</span>`,
	} {
		if !strings.Contains(fail, want) {
			t.Errorf("fail page lacks %q", want)
		}
	}
	if !strings.Contains(p["carried"], `<figure class="g g-carried`) || !strings.Contains(p["carried"], "The source is not available") {
		t.Error("carried page: no carried gauge, or no note about the missing source")
	}
	if !strings.Contains(p["first-run"], "no floors yet") || !strings.Contains(p["first-run"], `class="ann ann-none"`) {
		t.Error("first-run page does not say nothing is gated")
	}
	if !strings.Contains(p["multi"], `id="endpoints"`) || !strings.Contains(p["multi"], "<code>svc-api:event sum.done</code>") {
		t.Error("multi page lacks the endpoint section or its violations")
	}
	if strings.Contains(p["warn"], `id="endpoints"`) {
		t.Error("the warn state has no registry but the page has an endpoint section")
	}
	large := p["large"]
	if !strings.Contains(large, `class="tm tm-wide"`) || !strings.Contains(large, `class="tm tm-tall"`) || strings.Count(large, `<rect class="tm-tile`) != 16 {
		t.Errorf("large page: want both treemap layouts with 8 tiles each, got %d tiles", strings.Count(large, `<rect class="tm-tile`))
	}
	// Listings start open for files with lines that never ran, closed for
	// files where every changed line ran.
	if !strings.Contains(p["ok"], `<details class="lst" id="f1">`) || !strings.Contains(fail, `<details class="lst" id="f1" open>`) {
		t.Error("listing open/closed state wrong")
	}
}

func TestSquarifyTilesTheRectangle(t *testing.T) {
	vals := []float64{4259, 2871, 2128, 2042, 2013, 1417, 975, 555, 40, 1}
	r := rect{10, 20, 1056, 440}
	rs := squarify(vals, r)
	total, area := 0.0, 0.0
	for _, v := range vals {
		total += v
	}
	for i, x := range rs {
		area += x.W * x.H
		if x.X < r.X-1e-6 || x.Y < r.Y-1e-6 || x.X+x.W > r.X+r.W+1e-6 || x.Y+x.H > r.Y+r.H+1e-6 {
			t.Errorf("tile %d %+v leaves the rectangle", i, x)
		}
		// Each tile's area is its share of the rectangle.
		want := vals[i] / total * r.W * r.H
		if math.Abs(x.W*x.H-want) > 1e-6*want+1e-9 {
			t.Errorf("tile %d area %.3f, want %.3f", i, x.W*x.H, want)
		}
		for j := 0; j < i; j++ {
			y := rs[j]
			if x.X+1e-6 < y.X+y.W && y.X+1e-6 < x.X+x.W && x.Y+1e-6 < y.Y+y.H && y.Y+1e-6 < x.Y+x.H {
				t.Errorf("tiles %d and %d overlap", i, j)
			}
		}
	}
	if math.Abs(area-r.W*r.H) > 1e-6 {
		t.Errorf("tiles cover %.3f of %.3f", area, r.W*r.H)
	}
	if got := squarify([]float64{0, 0}, r); got[0] != (rect{}) {
		t.Error("an all-zero layout should be empty")
	}
}

func TestHeatAndLabels(t *testing.T) {
	v := build(t, rendertest.States(t)[len(rendertest.States(t))-1])
	for _, p := range v.Treemap.Rows {
		want := "h2" // the synthetic packages sit 22 to 30 points under an 80 target
		if gap := 80 - *p.M.Pct; gap > 25 {
			want = "h3"
		}
		if got := heat(p); got != want {
			t.Errorf("%s at %.2f: heat %s, want %s", p.G.Key, *p.M.Pct, got, want)
		}
	}
	if fitLabel("merchantops", 30) != "" || fitLabel("merchantops", 60) != "merchan…" || fitLabel("pdf", 60) != "pdf" {
		t.Errorf("fitLabel: %q %q %q", fitLabel("merchantops", 30), fitLabel("merchantops", 60), fitLabel("pdf", 60))
	}
	tm := buildTreemaps(v.Treemap)
	if tm.Prefix != "pkg" {
		t.Errorf("common prefix %q", tm.Prefix)
	}
	if buildTreemaps(nil) != nil {
		t.Error("no layer, no treemap")
	}
}

func TestHighlight(t *testing.T) {
	for _, tc := range []struct{ code, lang, want string }{
		{`return "a<b", nil // done`, "go", `<span class="t-k">return</span> <span class="t-s">&#34;a&lt;b&#34;</span>, <span class="t-k">nil</span> <span class="t-c">// done</span>`},
		{` * a doc line`, "ts", `<span class="t-c"> * a doc line</span>`},
		{`if x < y { return }`, "", `if x &lt; y { return }`},
	} {
		if got := string(highlight(tc.code, tc.lang)); got != tc.want {
			t.Errorf("highlight(%q):\n got %s\nwant %s", tc.code, got, tc.want)
		}
	}
}

func TestInvalidBrandNeverReachesTheStylesheet(t *testing.T) {
	s := rendertest.States(t)[0]
	bad := *s.Report
	b := *rendertest.Handipay
	b.Dark = map[string]string{"ok": "#fff;}body{background:url(https://evil.example/)"}
	bad.Brand = &b
	if _, err := view.Build(&bad, view.Options{}); err == nil {
		t.Fatal("an injected token was accepted")
	}
}

// state is the golden state called name.
func state(t *testing.T, name string) rendertest.State {
	t.Helper()
	for _, s := range rendertest.States(t) {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no state %q", name)
	return rendertest.State{}
}

// edited is s with its report changed by edit, on a copy: the states'
// reports are the goldens' inputs and stay as they are.
func edited(s rendertest.State, edit func(r *report.Report)) rendertest.State {
	r := *s.Report
	r.Layers = append([]report.Layer(nil), s.Report.Layers...)
	edit(&r)
	s.Report = &r
	return s
}

// cssRules parses a stylesheet's top-level rules (those outside any
// @-block) into selector -> property -> value, one entry per selector of a
// list. Enough to pin what a rule declares; not a CSS parser.
func cssRules(sheet string) map[string]map[string]string {
	sheet = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(sheet, "")
	out := map[string]map[string]string{}
	for len(sheet) > 0 {
		open := strings.IndexByte(sheet, '{')
		if open < 0 {
			break
		}
		sel := strings.TrimSpace(sheet[:open])
		depth, end := 0, open
		for ; end < len(sheet); end++ {
			if sheet[end] == '{' {
				depth++
			} else if sheet[end] == '}' {
				if depth--; depth == 0 {
					break
				}
			}
		}
		body := sheet[open+1 : min(end, len(sheet))]
		sheet = sheet[min(end+1, len(sheet)):]
		if strings.HasPrefix(sel, "@") {
			continue
		}
		for _, one := range strings.Split(sel, ",") {
			one = strings.Join(strings.Fields(one), " ")
			if out[one] == nil {
				out[one] = map[string]string{}
			}
			for _, decl := range strings.Split(body, ";") {
				if k, v, ok := strings.Cut(decl, ":"); ok {
					out[one][strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
		}
	}
	return out
}

var refsRE = regexp.MustCompile(`(?s)<div class="pr">.*?</div>\s*(<div class="refs">(.*?)</div>\s*)?</div>`)

// The subline under the title is built from the parts the metadata has: a
// local run with no repository or commit is its time alone, never ", 03:39
// UTC", and a report with nothing to say has no subline at all.
func TestHeaderSublineHasOnlyThePartsItHas(t *testing.T) {
	ok := state(t, "ok")
	m := ok.Report.Metadata
	tags := regexp.MustCompile(`<[^>]+>`)
	for _, tc := range []struct {
		name string
		meta report.Metadata
		at   string
		want string // "" is no subline
	}{
		{"a pull request", m, "", "3f2a9c1 into main at bb1595f, push 1, 15:42 UTC, Sep 28"},
		{"a local run", report.Metadata{}, "", "15:42 UTC, Sep 28"},
		{"a head commit only", report.Metadata{HeadSHA: m.HeadSHA}, "", "push 1, 15:42 UTC, Sep 28"},
		{"a commit only", report.Metadata{SHA: m.SHA}, "", "3f2a9c1, push 1, 15:42 UTC, Sep 28"},
		{"a base only", report.Metadata{Base: m.Base, BaseRef: m.BaseRef}, "", "against main at bb1595f, 15:42 UTC, Sep 28"},
		{"nothing at all", report.Metadata{}, "not a time", ""},
	} {
		s := edited(ok, func(r *report.Report) {
			r.Metadata = tc.meta
			if tc.at != "" {
				r.GeneratedAt = tc.at
			}
		})
		mm := refsRE.FindStringSubmatch(render(t, build(t, s)))
		if mm == nil {
			t.Fatalf("%s: no header", tc.name)
		}
		if tc.want == "" {
			if mm[1] != "" {
				t.Errorf("%s: an empty subline %q", tc.name, mm[1])
			}
			continue
		}
		if got := tags.ReplaceAllString(mm[2], ""); got != tc.want {
			t.Errorf("%s: subline %q, want %q", tc.name, got, tc.want)
		}
	}
}

var mhRE = regexp.MustCompile(`<p class="m-h">(.*?)</p>`)

// A message's label never wraps a word per line beside a long value: it
// takes its own width (flex: none), capped so a very long one still leaves
// the value room, and a list value (the "not in play here" layers) wraps
// between its items instead, each item held on one line.
func TestAttentionLabelsKeepTheirLine(t *testing.T) {
	rules := cssRules(CSS(build(t, state(t, "ok"))))
	if l := rules[".m-l"]; l["flex"] != "none" || l["max-width"] == "" {
		t.Errorf(".m-l = %v, want flex: none and a max-width", l)
	}
	if l := rules[".m-list"]; l["white-space"] != "normal" {
		t.Errorf(".m-list = %v, want white-space: normal", l)
	}
	if l := rules[".m-list > span"]; l["white-space"] != "nowrap" {
		t.Errorf(".m-list > span = %v, want white-space: nowrap", l)
	}
	lists := 0
	for name, page := range pages(t) {
		for _, h := range mhRE.FindAllStringSubmatch(page, -1) {
			if !strings.HasPrefix(h[1], `<span class="sq"></span><span class="m-l">`) {
				t.Errorf("%s: a label outside .m-l: %s", name, h[1])
			}
			if strings.Contains(h[1], "not in play here") {
				lists++
				if !regexp.MustCompile(`<span class="m-v m-list">(<span>[^<,]+</span>(, )?)+</span>$`).MatchString(h[1]) {
					t.Errorf("%s: the not-in-play list is not one item per span: %s", name, h[1])
				}
			}
		}
	}
	if lists == 0 {
		t.Error("no state has a not-in-play list to check")
	}
}

var (
	figRE = regexp.MustCompile(`(?s)<figure class="g [^"]*"[^>]*>.*?</figure>`)
	filRE = regexp.MustCompile(`<rect class="fil" x="46" y="([0-9.]+)" width="14" height="([0-9.]+)"`)
	ptrRE = regexp.MustCompile(`<path class="pt" d="M35 ([0-9.]+) l7 4.5 l-7 4.5 z"`)
)

// A tape is drawn at its reading, and the page never draws it anywhere
// else: nothing animates, because a capture records the first frame, and a
// sweep from zero put tapes at a tenth of their reading (or at nothing)
// beside a pointer at the true value. The geometry is checked in each case
// a floor can be in: none yet, under the reading, over it, and not run.
func TestTapesStandAtTheirReadingInEveryFrame(t *testing.T) {
	sheet := CSS(build(t, state(t, "ok")))
	rules := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(sheet, "")
	for _, moving := range []string{"@keyframes", "animation", "transition"} {
		if strings.Contains(rules, moving) {
			t.Errorf("the stylesheet has %q: the first frame must be the picture", moving)
		}
	}
	for sel, decls := range cssRules(sheet) {
		if strings.Contains(sel, ".fil") || strings.Contains(sel, ".pt") {
			if v, ok := decls["transform"]; ok {
				t.Errorf("%s moves the tape or its pointer: transform %s", sel, v)
			}
		}
	}
	seen := map[string]bool{}
	for _, s := range rendertest.States(t) {
		v := build(t, s)
		figs := figRE.FindAllString(render(t, v), -1)
		if len(figs) != len(v.Rows) {
			t.Fatalf("%s: %d gauges for %d rows", s.Name, len(figs), len(v.Rows))
		}
		for i, r := range v.Rows {
			fil, ptr := filRE.FindStringSubmatch(figs[i]), ptrRE.FindStringSubmatch(figs[i])
			if r.Pct == nil {
				seen["not run"] = true
				if fil != nil || ptr != nil {
					t.Errorf("%s %s: no reading, but a tape or a pointer", s.Name, r.Label)
				}
				continue
			}
			switch {
			case r.Floor == nil:
				seen["no floor"] = true
			case *r.Floor <= *r.Pct:
				seen["floor below"] = true
			default:
				seen["floor above"] = true
			}
			if fil == nil || ptr == nil {
				t.Errorf("%s %s: %.2f%% but no tape or no pointer", s.Name, r.Label, *r.Pct)
				continue
			}
			top, _ := strconv.ParseFloat(fil[1], 64)
			h, _ := strconv.ParseFloat(fil[2], 64)
			p, _ := strconv.ParseFloat(ptr[1], 64)
			want := 1.8 * math.Max(0, math.Min(100, *r.Pct))
			if math.Abs(h-want) > 0.051 || math.Abs(top+h-250) > 0.051 || math.Abs(p+4.5-top) > 0.051 {
				t.Errorf("%s %s at %.2f%%: tape y=%v h=%v pointer %v, want h=%.1f from the zero line at 250 with the pointer at its top",
					s.Name, r.Label, *r.Pct, top, h, p, want)
			}
		}
	}
	for _, c := range []string{"no floor", "floor below", "floor above", "not run"} {
		if !seen[c] {
			t.Errorf("no state draws a gauge with %s", c)
		}
	}
}

var (
	lbRE = regexp.MustCompile(`<text class="lb" x="52" y="([0-9.]+)"[^>]*>([^<]*)</text>`)
	paRE = regexp.MustCompile(`<text class="pa" x="52" y="([0-9.]+)"`)
	vbRE = regexp.MustCompile(`<svg viewBox="0 0 104 ([0-9.]+)"`)
)

// A gauge's label stays inside its own gauge: a line holds at most 16
// characters (config.GaugeLabelChars; over 12 takes the smaller size), a
// longer label wraps onto a second line with the foot and the figure moved
// down to make room, and a word too wide even for that is cut with an
// ellipsis, the whole label kept as the figure's title. It used to be one
// line whatever its length: "integration lines" ran into "integration
// branches" beside it.
func TestGaugeLabelsStayInsideTheirGauge(t *testing.T) {
	s := edited(state(t, "ok"), func(r *report.Report) {
		for i := range r.Layers {
			switch r.Layers[i].ID {
			case "web":
				r.Layers[i].ShortLabel = "integration" // "integration branches", 20 characters
			case "mobile":
				// Longer than the config allows, as a hand-written report can be.
				r.Layers[i].ShortLabel = "integrationpostgres"
			}
		}
	})
	all := pages(t)
	all["wrapped"] = render(t, build(t, s))
	for name, page := range all {
		for _, fig := range figRE.FindAllString(page, -1) {
			lbs := lbRE.FindAllStringSubmatch(fig, -1)
			if len(lbs) == 0 || len(lbs) > 2 {
				t.Errorf("%s: %d label lines in %s", name, len(lbs), fig[:80])
				continue
			}
			last := 0.0
			for i, lb := range lbs {
				n := len([]rune(lb[2]))
				if n > 16 {
					t.Errorf("%s: label line %q is %d characters", name, lb[2], n)
				}
				if n > 12 && !strings.Contains(fig[:60], " g-long") {
					t.Errorf("%s: %q needs the smaller size", name, lb[2])
				}
				y, _ := strconv.ParseFloat(lb[1], 64)
				if i > 0 && y-last < 12 {
					t.Errorf("%s: label lines %v apart", name, y-last)
				}
				last = y
			}
			for _, pa := range paRE.FindAllStringSubmatch(fig, -1) {
				if y, _ := strconv.ParseFloat(pa[1], 64); y-last < 12 {
					t.Errorf("%s: the foot at %v overlaps the label at %v", name, y, last)
				}
				last, _ = strconv.ParseFloat(pa[1], 64)
			}
			if vb := vbRE.FindStringSubmatch(fig); vb == nil {
				t.Errorf("%s: no viewBox", name)
			} else if h, _ := strconv.ParseFloat(vb[1], 64); h < last+3 {
				t.Errorf("%s: viewBox height %v cuts the text at %v", name, h, last)
			}
		}
	}
	page := all["wrapped"]
	for _, want := range []string{">integration</text>", ">branches</text>", ">functions</text>",
		`title="integrationpostgres">`, ">integrationpost…</text>"} {
		if !strings.Contains(page, want) {
			t.Errorf("wrapped page lacks %s", want)
		}
	}
	if strings.Contains(page, ">integration branches</text>") {
		t.Error("a 20-character label is still one line")
	}
	if strings.Count(page, " title=") != 1 {
		t.Error("only the cut label carries a title")
	}
}

// Every count on the page agrees with its noun: "1 line in web", not "1
// lines in web", in every state.
func TestPageCountsAgreeWithTheirNouns(t *testing.T) {
	for name, page := range pages(t) {
		body := regexp.MustCompile(`(?s)<style>.*?</style>`).ReplaceAllString(page, "")
		if bad := rendertest.BadCounts(body); len(bad) > 0 {
			t.Errorf("%s: %q", name, bad)
		}
	}
}
