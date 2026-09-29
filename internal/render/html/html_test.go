package html

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/arrayofone/coverreport/internal/render/rendertest"
	"github.com/arrayofone/coverreport/internal/render/view"
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
		`<figure class="g g-fail d0">`,
		`<figure class="g g-info d3">`,
		`<figure class="g g-notrun d8">`,
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
