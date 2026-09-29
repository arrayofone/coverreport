package github

import (
	"fmt"
	"strings"

	"github.com/DarrenBangsund/coverreport/internal/coverage"
	"github.com/DarrenBangsund/coverreport/internal/endpoints"
	"github.com/DarrenBangsund/coverreport/internal/floors"
	"github.com/DarrenBangsund/coverreport/internal/render/view"
)

// The fixed-width grids. Each returns the body of a ```diff block, every
// line padded to the same width (so the highlighter's per-line bands form a
// clean rectangle), and every row starting with its state's marker.
//
// Column order is a degradation rule: marker, state, name, now and ±floor
// come first, so the verdict survives a phone (about 45 visible columns)
// and email; floor, target, patch, the meter and notes follow.

const axis = "0        50      100" // over a 20-cell meter

// hud is the layers grid: the comment's and summary's centrepiece.
func hud(v *view.View) string {
	lw := len("layer")
	for _, r := range v.Rows {
		lw = max(lw, view.Runes(r.Label))
	}
	if e := v.Endpoints; e != nil {
		for _, k := range e.Kinds {
			lw = max(lw, view.Runes(k.Label))
		}
	}
	lw += 2
	var lines []string
	lines = append(lines, hudHeader(v))
	lines = append(lines, "#  "+view.Col("state", 6, false)+view.Col("layer", lw, false)+view.Col("now", 6, true)+
		view.Col("±floor", 8, true)+view.Col("floor", 7, true)+view.Col("target", 8, true)+view.Col("patch", 9, true)+
		"         "+axis)
	for _, r := range v.Rows {
		lines = append(lines, hudRow(v, r, lw))
	}
	if e := v.Endpoints; e != nil {
		lines = append(lines, "#")
		for _, k := range e.Kinds {
			lines = append(lines, endpointRow(v, k, lw))
		}
	}
	return view.PadBlock(lines)
}

func hudHeader(v *view.View) string {
	m := v.R.Metadata
	var parts []string
	switch {
	case m.PR != 0:
		h := fmt.Sprintf("#%d", m.PR)
		if v.PushNo > 0 {
			h += fmt.Sprintf(" push %d", v.PushNo)
		}
		parts = append(parts, h)
		if m.SHA != "" {
			into := view.Short7(m.SHA)
			if m.BaseRef != "" {
				into += " into " + m.BaseRef
			}
			parts = append(parts, into)
		}
	case m.SHA != "":
		parts = append(parts, view.Short7(m.SHA))
	default:
		parts = append(parts, "coverage")
	}
	if p := v.Patch; p != nil {
		if p.Overall.Total > 0 {
			parts = append(parts, "patch "+view.FracPct(p.Overall.Covered, p.Overall.Total), fmt.Sprintf("%d/%d", p.Overall.Covered, p.Overall.Total))
		} else {
			parts = append(parts, "patch: no coverable changes")
		}
		if len(v.Pushes) > 1 {
			parts = append(parts, "by push "+v.PushSpark())
		}
	}
	return "@@ " + strings.Join(parts, "  ") + " @@"
}

func hudRow(v *view.View, r *view.Row, lw int) string {
	now, margin, floorS, target := view.Pct2(r.Pct), view.Signed(r.Delta), view.Floor(r.Floor), view.Target(r.Target)
	if r.Layer.ReportOnly {
		margin, floorS, target = "—", "—", "—"
	}
	row := r.Marker() + "  " + view.Col(r.Word(), 6, false) + view.Col(r.Label, lw, false) + view.Col(now, 6, true) +
		view.Col(margin, 8, true) + view.Col(floorS, 7, true) + view.Col(target, 8, true)
	switch {
	case r.State == view.StateNotRun || r.State == view.StateCarried:
		row += view.Col("not run", 17, true)
	case !r.Primary || v.Patch == nil || r.Patch == nil:
		row += view.Col("", 17, true)
	case r.Patch.Total == 0:
		row += view.Col("untouched", 17, true)
	default:
		row += view.Col(view.FracPct(r.Patch.Covered, r.Patch.Total), 9, true) + " " + view.Col(fmt.Sprintf("%d/%d", r.Patch.Covered, r.Patch.Total), 7, false)
	}
	pct := 0.0
	if r.Pct != nil {
		pct = *r.Pct
	}
	row += " " + view.Meter(pct, r.Target, r.Layer.ReportOnly)
	switch {
	case r.State == view.StateInfo:
		row += "  report-only"
	case r.State == view.StateCarried:
		row += "  from " + v.BaseRef() + " " + view.Short7(r.CarriedSHA)
	case r.State == view.StateNotRun:
		row += "  not affected"
	case r.RatchetTo != nil && r.Floor != nil:
		row += "  ratchet to " + floors.FormatFloor(*r.RatchetTo)
	case r.RatchetTo != nil:
		row += "  new floor " + floors.FormatFloor(*r.RatchetTo)
	}
	return row
}

func endpointRow(v *view.View, k endpoints.Group, lw int) string {
	state, word := "+", "ok"
	if n := v.EndpointViolations[k.Key]; n > 0 {
		state, word = "-", "FAIL"
	}
	frac := fmt.Sprintf("%d/%d", k.Best.Full, k.Total)
	pct := 100 * float64(k.Best.Full) / float64(max(1, k.Total))
	row := state + "  " + view.Col(word, 6, false) + view.Col(k.Label, lw, false) + view.Col(frac, 14, true) +
		view.Col("full, any layer", 32, true) + " " + view.Meter(pct, ptr(100.0), false)
	if n := v.EndpointViolations[k.Key]; n > 0 {
		row += fmt.Sprintf("  %s", view.Plural(int64(n), "gained a gap", "gained a gap"))
	} else {
		row += "  " + view.Pct1(pct) + " full"
	}
	return row
}

func ptr[T any](x T) *T { return &x }

// legend explains the HUD's markers, in a <sub> line under it.
func legend(v *view.View) string {
	soak := "informational"
	if v.Patch != nil && v.Patch.Blocking {
		soak = "blocking"
	} else if v.Patch != nil && v.Patch.Until != "" {
		soak = "informational until " + v.Patch.Until
	}
	target := "80"
	if v.Patch != nil {
		target = view.Trim(v.Patch.Target)
	}
	return fmt.Sprintf("<sub>Rows: <code>+</code> holds its floor, <code>!</code> patch under %s%% (%s), <code>-</code> below its floor, "+
		"<code>#</code> not gated here (report-only, untouched, not run, or no floor yet). Meters are 20 cells of 5%%; <code>│</code> marks the target.</sub>\n",
		target, soak)
}

// pkgBlock is a meter block of package rows.
func pkgBlock(v *view.View, rows []*view.PkgRow, withLayer bool) string {
	kw := len("package")
	for _, p := range rows {
		kw = max(kw, view.Runes(p.G.Key))
	}
	kw = min(kw, 44) + 2
	lw := 0
	if withLayer {
		for _, p := range rows {
			lw = max(lw, view.Runes(p.Layer.ShortLabel))
		}
		lw += 2
	}
	head := "#  " + view.Col("package", kw, false)
	if withLayer {
		head += view.Col("layer", lw, false)
	}
	lines := []string{head + view.Col("floor", 6, true) + view.Col("now", 8, true) + view.Col("±floor", 8, true) + view.Col("patch", 16, true) + "   " + axis}
	for _, p := range rows {
		mk := "+"
		switch {
		case p.Failing:
			mk = "-"
		case p.PatchTotal >= int64(v.R.Patch.MinLines) && p.PatchTotal > 0 && !(coverage.Count{Covered: p.PatchCovered, Total: p.PatchTotal}).AtLeast(coverage.Hundredths(v.R.Patch.Target)):
			mk = "!"
		case p.M.Floor == nil:
			mk = "#"
		}
		patch := "—"
		if p.PatchTotal > 0 {
			patch = fmt.Sprintf("%s %d/%d", view.FracPct(p.PatchCovered, p.PatchTotal), p.PatchCovered, p.PatchTotal)
		}
		pct := 0.0
		if p.M.Pct != nil {
			pct = *p.M.Pct
		}
		line := mk + "  " + view.Col(view.Cut(p.G.Key, kw-2), kw, false)
		if withLayer {
			line += view.Col(p.Layer.ShortLabel, lw, false)
		}
		line += view.Col(view.Floor(p.M.Floor), 6, true) + view.Col(view.Pct2(p.M.Pct), 8, true) + view.Col(view.Signed(p.M.Delta), 8, true) +
			view.Col(patch, 16, true) + "   " + view.Meter(pct, p.Target(), false)
		if t := p.Target(); t != nil {
			line += "  target " + view.Trim(*t)
		}
		lines = append(lines, line)
	}
	return view.PadBlock(lines)
}

// endpointsBlock is the completeness grid: one row per kind, the full
// count in each test layer and at best, and the class missed most.
func endpointsBlock(v *view.View, groups []endpoints.Group, what string, byKind bool) string {
	e := v.Endpoints
	kw := len(what)
	for _, g := range groups {
		kw = max(kw, view.Runes(g.Label))
	}
	kw += 2
	head := "#  " + view.Col(what, kw, false)
	for _, l := range e.Layers {
		head += view.Col(l, 13, true)
	}
	head += view.Col("best", 13, true) + "   most missed"
	lines := []string{head}
	for _, g := range groups {
		mk := "+"
		for _, vi := range e.Violations {
			if (byKind && view.EndpointKind(vi.ID) == g.Key) || (!byKind && strings.HasPrefix(vi.ID, g.Key+":")) {
				mk = "-"
			}
		}
		line := mk + "  " + view.Col(g.Label, kw, false)
		for _, l := range e.Layers {
			line += view.Col(fmt.Sprintf("%d/%d", g.Layers[l].Full, g.Total), 13, true)
		}
		line += view.Col(fmt.Sprintf("%d/%d", g.Best.Full, g.Total), 13, true)
		if g.MostMissed != "" {
			line += fmt.Sprintf("   %s (%d)", g.MostMissed, g.MostMissedCount)
		}
		lines = append(lines, line)
	}
	return view.PadBlock(lines)
}

// snippet renders the lines of a file around its uncovered ranges as a
// diff listing: "!" never ran, "+" changed and ran, " " anything else.
// "" when the source is unknown.
func snippet(f *view.File, before, after, maxLines int) string {
	if f.Source == nil {
		return ""
	}
	var out []string
	n := 0
	for _, w := range f.Windows(true, before, after) {
		head := fmt.Sprintf("@@ %s  %s", f.Base, view.RangeLabel([2]int{w.From, w.To}))
		if w.Func != "" {
			head += "  " + w.Func
		}
		out = append(out, head+" @@")
		for ln := w.From; ln <= w.To; ln++ {
			if n == maxLines {
				out = append(out, "#  … the report page lists the rest")
				return strings.Join(out, "\n")
			}
			mk := " "
			switch f.State(ln) {
			case view.LineChgCov:
				mk = "+"
			case view.LineChgUnc:
				mk = "!"
			}
			c := strings.ReplaceAll(f.Code(ln), "\t", "    ")
			out = append(out, fmt.Sprintf("%s %4d │ %s", mk, ln, view.Cut(c, 160)))
			n++
		}
	}
	return strings.Join(out, "\n")
}
