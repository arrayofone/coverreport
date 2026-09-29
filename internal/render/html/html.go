// Package html renders report.html: the single self-contained page a
// Coverage job uploads as a non-zipped artifact (it opens in the browser for
// signed-in readers). One file, no script, no network request, no style
// attribute: one inline <style>, light and dark through prefers-color-scheme,
// and every chart drawn as inline SVG whose coordinates are attributes and
// whose colours are classes (each SVG also carries presentation-attribute
// colours, so a page served under a CSP that blocks inline styles still
// shows readable instruments).
//
// The layout is the "mission control" design: an upper display of tape
// gauges (one per layer and metric, grouped), a lower display with what
// needs attention beside the patch readout and its push history, the
// treemap of where the untested code lives, then the changed files as
// line listings with every line addressable (#f3-L120), and the reference
// tables under hairline heads.
package html

import (
	"bytes"
	_ "embed"
	"fmt"
	htmpl "html/template"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/arrayofone/coverreport/internal/render/view"
)

//go:embed report.html.tmpl
var pageTemplate string

//go:embed report.css
var stylesheet string

var tmpl = htmpl.Must(htmpl.New("report").Funcs(htmpl.FuncMap{
	"pct2":   view.Pct2,
	"pct1":   view.Pct1,
	"frac":   view.FracPct,
	"floor":  view.Floor,
	"target": view.Target,
	"trim":   view.Trim,
	"signed": view.Signed,
	"thou":   view.Thousands,
	"plural": plural,
	"unit":   view.Unit,
	"ushort": view.UnitShort,
	"short7": view.Short7,
	"rlabel": view.RangeLabel,
	"hl":     highlight,
	"fx":     func(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) },
	"add":    func(a, b int) int { return a + b },
	"deref":  func(v *float64) float64 { return *v },
	"cap":    view.Capitalize,
	"join":   strings.Join,
	"addf":   func(a, b float64) float64 { return a + b },
	"sub":    func(a, b int64) int64 { return a - b },
	"int64":  func(n int) int64 { return int64(n) },
}).Parse(pageTemplate))

// plural is view.Plural for any integer the template holds.
func plural(n any, one, many string) string {
	switch x := n.(type) {
	case int:
		return view.Plural(int64(x), one, many)
	case int64:
		return view.Plural(x, one, many)
	}
	return fmt.Sprint(n) + " " + many
}

// Render writes report.html.
func Render(w io.Writer, v *view.View) error {
	p := newPage(v)
	var b bytes.Buffer
	if err := tmpl.Execute(&b, p); err != nil {
		return err
	}
	_, err := w.Write(b.Bytes())
	return err
}

// CSS is the page's stylesheet for a brand: the static rules with the
// brand's tokens and font stacks declared at the top. Exported so a test
// can pin it once instead of in every page golden.
func CSS(v *view.View) string {
	b := v.Brand
	var s strings.Builder
	s.WriteString("/* coverreport, brand \"" + b.Name + "\". Dim is the default (the cockpit); Paper applies under\n   prefers-color-scheme: light (the chart recorder). Colour carries state only. */\n\n")
	s.WriteString(":root {\n  color-scheme: dark;\n")
	s.WriteString(b.Declarations(true, "  "))
	fmt.Fprintf(&s, "  --mono: %s;\n  --sans: %s;\n}\n\n", b.Mono, b.Sans)
	s.WriteString("@media (prefers-color-scheme: light) {\n  :root {\n    color-scheme: light;\n")
	s.WriteString(b.Declarations(false, "    "))
	s.WriteString("  }\n}\n\n")
	s.WriteString(stylesheet)
	return s.String()
}

type page struct {
	V        *view.View
	CSS      htmpl.CSS
	Groups   []gaugeGroup
	Push     *pushChart
	Map      *treemaps
	Listings []*listing
	// dark tokens, for SVG presentation attributes
	tok map[string]string
}

// Tok is a dark-scheme token value, used as an SVG presentation attribute
// (the fallback when the stylesheet cannot apply).
func (p *page) Tok(name string) string { return p.tok[name] }

func newPage(v *view.View) *page {
	p := &page{V: v, CSS: htmpl.CSS(CSS(v)), tok: v.Brand.Dark}
	for _, g := range v.Groups {
		gg := gaugeGroup{Name: g.Name}
		for _, r := range g.Rows {
			gg.Gauges = append(gg.Gauges, gauge{Row: r, V: v})
		}
		p.Groups = append(p.Groups, gg)
	}
	p.Push = newPushChart(v)
	p.Map = buildTreemaps(v.Treemap)
	for _, f := range v.Files {
		p.Listings = append(p.Listings, newListing(f))
	}
	return p
}

// ── gauges ─────────────────────────────────────────────────────────────────

type gaugeGroup struct {
	Name   string
	Gauges []gauge
}

// gauge is one vertical tape. Scale: 0 at y=250, 100 at y=70 (1.8 px per
// point).
type gauge struct {
	*view.Row
	V *view.View
}

func y(v float64) float64 { return 250 - 1.8*math.Max(0, math.Min(100, v)) }

func (g gauge) now() float64 {
	if g.Pct == nil {
		return 0
	}
	return *g.Pct
}

func (g gauge) HasValue() bool   { return g.Pct != nil }
func (g gauge) YNow() float64    { return y(g.now()) }
func (g gauge) HNow() float64    { return 250 - y(g.now()) }
func (g gauge) YPtr() float64    { return y(g.now()) - 4.5 }
func (g gauge) HasFloor() bool   { return g.Floor != nil && !g.Layer.ReportOnly }
func (g gauge) YFloor() float64  { return y(*g.Floor) }
func (g gauge) HasTarget() bool  { return g.Target != nil && !g.Layer.ReportOnly }
func (g gauge) YTarget() float64 { return y(*g.Target) }
func (g gauge) YTargetText() float64 {
	return y(*g.Target) + 3.5
}

// Band is the amber floor-to-target band; OkH the green band above target.
func (g gauge) BandY() float64 { return y(*g.Target) }
func (g gauge) BandH() float64 {
	if g.Floor == nil {
		return 0
	}
	return math.Max(0, y(*g.Floor)-y(*g.Target))
}
func (g gauge) OkH() float64 { return y(*g.Target) - 70 }

// Class is the figure's state classes.
func (g gauge) Class() string {
	c := "g g-" + g.State
	if g.Met() && g.State != view.StateFail {
		c += " g-met"
	}
	if len([]rune(g.Label)) > 12 {
		c += " g-long"
	}
	return c + fmt.Sprintf(" d%d", min(g.Index, 11))
}

// Margin is the line under the readout.
func (g gauge) Margin() string {
	switch g.State {
	case view.StateInfo:
		return "report-only"
	case view.StateCarried:
		return "from " + g.V.BaseRef()
	case view.StateNotRun:
		return "not run"
	case view.StateNew:
		return "no floor yet"
	}
	if g.Floor == nil {
		return ""
	}
	return view.Signed(g.Delta) + " vs " + view.Floor(g.Floor)
}

// Foot is the one or two lines under the label.
func (g gauge) Foot() []string {
	switch {
	case g.State == view.StateCarried:
		return []string{"not run here", view.Short7(g.CarriedSHA)}
	case g.State == view.StateNotRun:
		return []string{"not affected"}
	case !g.Primary || g.Patch == nil:
		return nil
	case g.Patch.Total == 0:
		return []string{"untouched"}
	}
	return []string{"patch " + view.FracPct(g.Patch.Covered, g.Patch.Total), fmt.Sprintf("%d/%d", g.Patch.Covered, g.Patch.Total)}
}

// Aria is the gauge's accessible name.
func (g gauge) Aria() string {
	s := fmt.Sprintf("%s: %s%%", g.Long, view.Pct2(g.Pct))
	if g.Layer.ReportOnly {
		return s + ", report-only, not gated"
	}
	if g.Floor != nil {
		s += ", floor " + view.Floor(g.Floor)
	}
	if g.Target != nil {
		s += ", target " + view.Target(g.Target)
	}
	s += ", " + g.Words()
	if f := g.Foot(); len(f) > 0 && g.Patch != nil && g.Patch.Total > 0 {
		s += ", " + f[0]
	}
	return s
}

type tick struct {
	Y     float64
	Major bool
}

func (g gauge) Ticks() []tick {
	var t []tick
	for v := 0; v <= 100; v += 10 {
		t = append(t, tick{y(float64(v)), v%50 == 0})
	}
	return t
}

// ── patch panel ────────────────────────────────────────────────────────────

type pushPoint struct {
	X, Y float64
	N    int
	Pct  string
	Last bool
	// Below puts the value label under the dot, when a label above it
	// would leave the plot.
	Below bool
}

type pushChart struct {
	Points  []pushPoint
	Path    string
	TargetY float64
	Target  string
	Label   string
}

// newPushChart plots overall patch coverage per push, autoscaled around the
// values and the target so a climb from 58 to 86 is visible.
func newPushChart(v *view.View) *pushChart {
	if v.Patch == nil || len(v.Pushes) == 0 {
		return nil
	}
	lo, hi := v.Patch.Target, v.Patch.Target
	for _, p := range v.Pushes {
		lo, hi = math.Min(lo, p.Pct), math.Max(hi, p.Pct)
	}
	lo, hi = math.Max(0, lo-5), math.Min(100, hi+5)
	if hi-lo < 10 {
		hi = math.Min(100, lo+10)
		lo = hi - 10
	}
	yv := func(p float64) float64 { return 70 - (p-lo)/(hi-lo)*56 }
	c := &pushChart{TargetY: yv(v.Patch.Target), Target: view.Trim(v.Patch.Target)}
	n := len(v.Pushes)
	first := v.PushNo - n + 1
	var path strings.Builder
	var parts []string
	for i, p := range v.Pushes {
		x := 140.0
		if n > 1 {
			x = 24 + float64(i)*(232/float64(n-1))
		}
		pt := pushPoint{X: x, Y: yv(p.Pct), N: first + i, Pct: view.Trim(p.Pct), Last: i == n-1}
		pt.Below = pt.Y < 24
		c.Points = append(c.Points, pt)
		if i == 0 {
			fmt.Fprintf(&path, "M%.1f %.1f", pt.X, pt.Y)
		} else {
			fmt.Fprintf(&path, " L%.1f %.1f", pt.X, pt.Y)
		}
		parts = append(parts, fmt.Sprintf("push %d %s%%", pt.N, pt.Pct))
	}
	c.Path = path.String()
	c.Label = "Patch coverage by push: " + strings.Join(parts, ", ")
	return c
}

// ShowPushLabels reports whether every push can be labelled (up to 8).
func (c *pushChart) ShowPushLabels() bool { return len(c.Points) <= 8 }

// Bar is a horizontal meter: 0-100 over 160 px.
type bar struct {
	Fill, Floor, Target float64
	HasFloor, HasTarget bool
}

func (p *page) Bar(now, floor, target *float64) bar {
	b := bar{}
	if now != nil {
		b.Fill = 1.6 * math.Max(0, math.Min(100, *now))
	}
	if floor != nil {
		b.Floor, b.HasFloor = 1.6**floor, true
	}
	if target != nil {
		b.Target, b.HasTarget = 1.6**target, true
	}
	return b
}

// Informational are the patch rows under target that do not block.
func (p *page) Informational() []*view.PatchRow {
	var out []*view.PatchRow
	for _, pr := range p.V.WarnRows() {
		if pr.State == view.StateWarn {
			out = append(out, pr)
		}
	}
	return out
}

// PatchBar is a patch row's meter, with the patch target as its tick.
func (p *page) PatchBar(pr *view.PatchRow) bar {
	t := p.V.Patch.Target
	return p.Bar(pr.PL.Pct, nil, &t)
}

type tmView struct {
	T         treemap
	Class, ID string
	P         *page
}

// Treemaps are the two layouts, wide and tall; CSS shows one.
func (p *page) Treemaps() []tmView {
	if p.Map == nil {
		return nil
	}
	return []tmView{{p.Map.Wide, "tm-wide", "w", p}, {p.Map.Tall, "tm-tall", "t", p}}
}

// ── listings ───────────────────────────────────────────────────────────────

// MaxListingRows bounds one file's listing; the rest is summarised.
const MaxListingRows = 1500

type row struct {
	Hunk  bool
	Gap   int
	Func  string
	Start int
	No    int
	Class string
	Code  string
	ID    string
}

type listing struct {
	F      *view.File
	Rows   []row
	Cut    int
	Cells  []cell
	CellsW int
}

type cell struct {
	X   int
	Unc bool
}

func newListing(f *view.File) *listing {
	l := &listing{F: f}
	prevEnd := 0
	n := 0
	for i, w := range f.Windows(false, 3, 3) {
		if i > 0 && w.From > prevEnd+1 {
			l.Rows = append(l.Rows, row{Gap: w.From - prevEnd - 1})
		}
		l.Rows = append(l.Rows, row{Hunk: true, Start: w.From, Func: w.Func})
		for ln := w.From; ln <= w.To; ln++ {
			if n == MaxListingRows {
				l.Cut++
				continue
			}
			l.Rows = append(l.Rows, row{No: ln, Class: lineClass(f.State(ln)), Code: f.Code(ln), ID: fmt.Sprintf("f%d-L%d", f.Idx, ln)})
			n++
		}
		prevEnd = w.To
	}
	for i, ran := range f.Strip() {
		if i == 120 {
			break
		}
		l.Cells = append(l.Cells, cell{X: i * 7, Unc: !ran})
	}
	l.CellsW = max(7, len(l.Cells)*7-2)
	return l
}

func lineClass(s byte) string {
	switch s {
	case view.LineChgCov:
		return "cov"
	case view.LineChgUnc:
		return "unc"
	case view.LineCtxUnc:
		return "old"
	case view.LineChg:
		return "chg"
	}
	return "ctx"
}

// Open reports whether a listing starts expanded: files with lines that
// never ran, the first 30 of them (a big change keeps the page skimmable).
func (l *listing) Open() bool { return l.F.Missing() > 0 && l.F.Idx <= 30 }

// Toward is the listing's "counts toward" chip text.
func (p *page) Toward(f *view.File) []view.Toward { return p.V.CountsToward(f) }

// RatchetLines is the ratchet diff as classed lines.
func (p *page) RatchetLines() []diffLine {
	var out []diffLine
	for _, ln := range strings.Split(p.V.RatchetDiff(0), "\n") {
		c := "h"
		switch {
		case strings.HasPrefix(ln, "+"):
			c = "a"
		case strings.HasPrefix(ln, "-"):
			c = "d"
		case strings.HasPrefix(ln, " "):
			c = "x"
		}
		out = append(out, diffLine{c, ln})
	}
	return out
}

type diffLine struct{ Class, Text string }

// Stack is a stacked full/partial/none bar over 200 px, scaled to the
// widest group.
type stack struct{ F, P, Z, PX, ZX float64 }

func (p *page) Stack(full, partial, none, widest int) stack {
	scale := 200.0 / float64(max(1, widest))
	f, pp, z := float64(full)*scale, float64(partial)*scale, float64(none)*scale
	return stack{F: f, P: pp, Z: z, PX: f, ZX: f + pp}
}

// Widest is the largest group total among the endpoint surfaces.
func (p *page) Widest() int {
	n := 1
	if e := p.V.Endpoints; e != nil {
		for _, s := range e.Surfaces {
			n = max(n, s.Total)
		}
	}
	return n
}
