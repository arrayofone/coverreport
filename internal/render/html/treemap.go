package html

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/arrayofone/coverreport/internal/render/view"
)

// The package map is a nested squarified treemap (Bruls, Huizing and van
// Wijk, 2000), laid out here, at render time, into plain SVG rectangles: no
// script and no layout at view time, so it survives the strictest CSP an
// artifact host could send. Area is a package's size in the layer's primary
// metric; fill is how far it sits below its target, in amber steps (colour
// means state: amber is "needs a test"), with a red outline when the
// package is below its floor and a target-coloured one when this change
// touched it. Two layouts are computed, one wide and one tall (not one
// scaled), and CSS shows the one that fits the viewport.

type rect struct{ X, Y, W, H float64 }

func (r rect) inset(d float64) rect {
	return rect{r.X + d, r.Y + d, math.Max(r.W-2*d, 0), math.Max(r.H-2*d, 0)}
}

type tile struct {
	rect
	Row                        *view.PkgRow
	Label                      string
	ShowName, ShowPct, ShowLen bool
	Heat                       string
	Title                      string
	Outline                    rect
}

type tmGroup struct {
	rect
	Name     string
	Header   bool
	Pct      string
	LabelFit bool
}

type treemap struct {
	W, H   float64
	Groups []tmGroup
	Tiles  []tile
}

type treemaps struct {
	Wide, Tall treemap
	Layer      string
	Metric     string
	Prefix     string
}

// heat is a package's distance below its target, in points of the
// primary metric: at or past it, up to 10 short, 10 to 25, more than 25.
func heat(p *view.PkgRow) string {
	t := p.Target()
	if t == nil || p.M.Pct == nil {
		return "ok"
	}
	gap := *t - *p.M.Pct
	switch {
	case gap <= 0:
		return "ok"
	case gap <= 10:
		return "h1"
	case gap <= 25:
		return "h2"
	}
	return "h3"
}

func buildTreemaps(t *view.PkgTable) *treemaps {
	if t == nil {
		return nil
	}
	var rows []*view.PkgRow
	for _, p := range t.Rows {
		if p.M.Total > 0 {
			rows = append(rows, p)
		}
	}
	if len(rows) < 2 {
		return nil
	}
	prefix := commonDir(rows)
	return &treemaps{
		Wide: layout(rows, prefix, 1056, 440), Tall: layout(rows, prefix, 358, 760),
		Layer: t.Layer.Label, Metric: t.Layer.Metrics[0], Prefix: prefix,
	}
}

// commonDir is the longest directory prefix shared by every package, so
// "libs/go/svc/work" groups under "svc" with the tiles named "work".
func commonDir(rows []*view.PkgRow) string {
	parts := strings.Split(rows[0].G.Key, "/")
	for _, p := range rows[1:] {
		q := strings.Split(p.G.Key, "/")
		n := 0
		for n < len(parts) && n < len(q) && parts[n] == q[n] {
			n++
		}
		parts = parts[:n]
	}
	// Keep at least one segment below the prefix for every package.
	for len(parts) > 0 {
		ok := true
		for _, p := range rows {
			if p.G.Key == strings.Join(parts, "/") {
				ok = false
			}
		}
		if ok {
			break
		}
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "/")
}

type node struct {
	name  string
	value float64
	rows  []*view.PkgRow
}

func rel(key, prefix string) string {
	if prefix == "" {
		return key
	}
	return strings.TrimPrefix(key, prefix+"/")
}

func layout(rows []*view.PkgRow, prefix string, w, h float64) treemap {
	byTop := map[string]*node{}
	var order []*node
	for _, p := range rows {
		top, _, _ := strings.Cut(rel(p.G.Key, prefix), "/")
		n := byTop[top]
		if n == nil {
			n = &node{name: top}
			byTop[top] = n
			order = append(order, n)
		}
		n.value += float64(p.M.Total)
		n.rows = append(n.rows, p)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].value != order[j].value {
			return order[i].value > order[j].value
		}
		return order[i].name < order[j].name
	})
	tm := treemap{W: w, H: h}
	vals := make([]float64, len(order))
	for i, n := range order {
		vals[i] = n.value
	}
	for i, gr := range squarify(vals, rect{0, 0, w, h}) {
		n := order[i]
		g := tmGroup{rect: gr.inset(1.5), Name: n.name}
		var cov, tot int64
		for _, p := range n.rows {
			cov += p.M.Covered
			tot += p.M.Total
		}
		g.Pct = view.FracPct(cov, tot)
		inner := g.rect
		grouped := len(n.rows) > 1
		if grouped && g.H > 60 && g.W > 60 {
			g.Header = true
			g.LabelFit = textW(n.name+"/ "+g.Pct, 12) < g.W-12
			inner = rect{g.X, g.Y + 22, g.W, g.H - 22}
		}
		tm.Groups = append(tm.Groups, g)
		sort.SliceStable(n.rows, func(a, b int) bool {
			if n.rows[a].M.Total != n.rows[b].M.Total {
				return n.rows[a].M.Total > n.rows[b].M.Total
			}
			return n.rows[a].G.Key < n.rows[b].G.Key
		})
		cv := make([]float64, len(n.rows))
		for k, p := range n.rows {
			cv[k] = float64(p.M.Total)
		}
		rects := []rect{inner}
		if grouped {
			rects = squarify(cv, inner)
		}
		for k, p := range n.rows {
			r := rects[k]
			if grouped {
				r = r.inset(1)
			}
			label := rel(p.G.Key, prefix)
			if grouped && g.Header {
				label = strings.TrimPrefix(label, n.name+"/")
			}
			label = fitLabel(label, r.W-14)
			t := tile{rect: r, Row: p, Label: label, Heat: heat(p), Outline: r.inset(1.5)}
			t.ShowName = label != "" && r.H >= 22
			t.ShowPct = t.ShowName && r.H >= 40
			size := view.Unit(p.Layer.Metrics[0], p.M.Total)
			t.ShowLen = t.ShowPct && r.H >= 58 && r.W >= textW(size, 11)+12
			t.Title = fmt.Sprintf("%s: %s%% of %s", p.G.Key, view.Pct2(p.M.Pct), size)
			if tg := p.Target(); tg != nil {
				t.Title += ", target " + view.Trim(*tg)
			}
			if p.Failing {
				t.Title += ", below its floor"
			}
			if p.Touched {
				t.Title += ", touched by this change"
			}
			tm.Tiles = append(tm.Tiles, t)
		}
	}
	return tm
}

// fitLabel returns the label, or its head plus an ellipsis, that fits in w
// pixels; "" when fewer than four characters would survive.
func fitLabel(s string, w float64) string {
	if textW(s, 12) <= w {
		return s
	}
	rs := []rune(s)
	for n := len(rs) - 1; n >= 4; n-- {
		c := string(rs[:n]) + "…"
		if textW(c, 12) <= w {
			return c
		}
	}
	return ""
}

// textW estimates rendered width for the fit test: a monospace face is
// 0.6em per character, and every font stack the page names is monospace.
func textW(s string, size float64) float64 {
	return float64(len([]rune(s))) * size * 0.6
}

// squarify lays values out in r, largest first, keeping tiles as close to
// square as it can (the worst aspect ratio of each row never gets worse).
func squarify(values []float64, r rect) []rect {
	total := 0.0
	for _, v := range values {
		total += v
	}
	out := make([]rect, len(values))
	if total == 0 || r.W <= 0 || r.H <= 0 {
		return out
	}
	scale := r.W * r.H / total
	areas := make([]float64, len(values))
	for i, v := range values {
		areas[i] = v * scale
	}
	x, y, w, h := r.X, r.Y, r.W, r.H
	i := 0
	for i < len(areas) {
		short := math.Min(w, h)
		j := i + 1
		for j < len(areas) && worst(areas[i:j+1], short) <= worst(areas[i:j], short) {
			j++
		}
		row := areas[i:j]
		sum := 0.0
		for _, a := range row {
			sum += a
		}
		if w >= h {
			cw := sum / h
			cy := y
			for k, a := range row {
				rh := a / cw
				out[i+k] = rect{x, cy, cw, rh}
				cy += rh
			}
			x += cw
			w -= cw
		} else {
			rh := sum / w
			cx := x
			for k, a := range row {
				cw := a / rh
				out[i+k] = rect{cx, y, cw, rh}
				cx += cw
			}
			y += rh
			h -= rh
		}
		i = j
	}
	return out
}

func worst(row []float64, short float64) float64 {
	sum, mx, mn := 0.0, 0.0, math.Inf(1)
	for _, a := range row {
		sum += a
		mx = math.Max(mx, a)
		mn = math.Min(mn, a)
	}
	s2, sum2 := short*short, sum*sum
	return math.Max(s2*mx/sum2, sum2/(s2*mn))
}
