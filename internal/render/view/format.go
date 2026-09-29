package view

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DarrenBangsund/coverreport/internal/coverage"
	"github.com/DarrenBangsund/coverreport/internal/floors"
)

// Number formats shared by every surface, so a value reads the same in the
// comment, the summary and the page.

// Pct2 is a percentage with two decimals ("83.21"), or "—".
func Pct2(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', 2, 64)
}

// Pct1 is a percentage with one decimal and a percent sign ("86.4%").
func Pct1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) + "%" }

// FracPct is a covered/total pair's one-decimal percentage, "—" with no data.
func FracPct(covered, total int64) string {
	if total == 0 {
		return "—"
	}
	return Pct1(pctOf(covered, total))
}

func pctOf(covered, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(covered) * 100 / float64(total)
}

// Floor prints a floor as floors.json stores it (one decimal), or "—".
func Floor(v *float64) string {
	if v == nil {
		return "—"
	}
	return floors.FormatFloor(*v)
}

// Target prints a target with one decimal ("85.0"), or "—".
func Target(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', 1, 64)
}

// Trim prints a number without trailing zeros ("80", "92.5").
func Trim(v float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
}

// Signed is a two-decimal delta with its sign ("+0.13", "-0.16", " 0.00").
func Signed(v *float64) string {
	if v == nil {
		return "—"
	}
	if math.Abs(*v) < 0.005 {
		return "0.00"
	}
	return fmt.Sprintf("%+.2f", *v)
}

// Thousands groups digits: 27694 is "27,694".
func Thousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Plural returns "1 line" / "2 lines".
func Plural(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return Thousands(n) + " " + many
}

// Unit is how a metric's units are named in prose.
func Unit(metric string, n int64) string {
	switch metric {
	case "statements":
		return Plural(n, "statement", "statements")
	case "branches":
		return Plural(n, "branch", "branches")
	case "functions":
		return Plural(n, "function", "functions")
	}
	return Plural(n, "line", "lines")
}

// UnitShort is a metric's unit abbreviation for table cells ("stmts").
func UnitShort(metric string) string {
	switch metric {
	case "statements":
		return "stmts"
	case "branches":
		return "branches"
	case "functions":
		return "functions"
	}
	return "lines"
}

// RangeLabel is "L12" or "L12–14".
func RangeLabel(r coverage.Range) string {
	if r[0] == r[1] {
		return fmt.Sprintf("L%d", r[0])
	}
	return fmt.Sprintf("L%d–%d", r[0], r[1])
}

// RangesLabel joins range labels: "L101, L140".
func RangesLabel(rs []coverage.Range) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = RangeLabel(r)
	}
	return strings.Join(parts, ", ")
}

// Short7 is the conventional seven-character commit abbreviation.
func Short7(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Runes is the display width of s in a monospace grid (one cell per rune;
// every glyph the grids use is single-width in WGL4 fonts).
func Runes(s string) int { return utf8.RuneCountInString(s) }

// Cut shortens s to at most n runes, ending in "…" when it had to cut.
func Cut(s string, n int) string {
	if Runes(s) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n-1]) + "…"
}

// Capitalize upper-cases the first rune.
func Capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return strings.ToUpper(string(r)) + s[n:]
}

// ── glyph kit ──────────────────────────────────────────────────────────────
//
// Only WGL4 glyphs go inside a grid (█ ▌ · │ ▒ ■ □), because Consolas, Menlo,
// SF Mono and DejaVu Sans Mono all draw them one cell wide, so the columns
// after them hold on every platform. The sparkline ramp is not WGL4 and so
// only ever ends a line, where a fallback font can shift nothing after it.

// MeterCells is the width of every text meter: 20 cells of 5 points.
const MeterCells = 20

// Meter draws v (0-100) as 20 cells: █ full, ▌ a half cell, · empty, and │
// at the target's cell while the target is unmet. Report-only rows fill
// with ▒ so they visibly are not the same kind of bar.
func Meter(v float64, target *float64, reportOnly bool) string {
	const step = 100.0 / MeterCells
	cells := []rune(strings.Repeat("·", MeterCells))
	full := int(math.Floor(v/step + 1e-9))
	if full > MeterCells {
		full = MeterCells
	}
	if full < 0 {
		full = 0
	}
	fill := '█'
	if reportOnly {
		fill = '▒'
	}
	for i := 0; i < full; i++ {
		cells[i] = fill
	}
	if !reportOnly && full < MeterCells && v-float64(full)*step >= step/2 {
		cells[full] = '▌'
	}
	if target != nil && !reportOnly && v < *target {
		t := int(math.Floor(*target/step + 1e-9))
		if t >= MeterCells {
			t = MeterCells - 1
		}
		if cells[t] == '·' {
			cells[t] = '│'
		}
	}
	return string(cells)
}

// SparkSpan is the smallest vertical span a sparkline autoscales to, in
// points: four times the gate's usual 0.1 tolerance, so one step of the ramp
// is about the smallest move the gate acts on and a 0.01 wobble stays flat.
const SparkSpan = 0.4

// Spark maps values onto ▁▂▃▄▅▆▇█, autoscaled with a minimum span.
func Spark(vs []float64) string {
	if len(vs) == 0 {
		return ""
	}
	lo, hi := vs[0], vs[0]
	for _, v := range vs {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if hi-lo < SparkSpan {
		mid := (hi + lo) / 2
		lo, hi = mid-SparkSpan/2, mid+SparkSpan/2
	}
	ramp := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range vs {
		i := int(math.Round((v - lo) / (hi - lo) * 7))
		b.WriteRune(ramp[max(0, min(7, i))])
	}
	return b.String()
}

// Col pads (or right-aligns) s to w cells.
func Col(s string, w int, right bool) string {
	n := Runes(s)
	if n >= w {
		return s
	}
	if right {
		return strings.Repeat(" ", w-n) + s
	}
	return s + strings.Repeat(" ", w-n)
}

// PadBlock right-pads every line to the widest, so the diff highlighter's
// per-line backgrounds form one clean rectangle instead of a ragged edge.
func PadBlock(lines []string) string {
	w := 0
	for _, l := range lines {
		w = max(w, Runes(l))
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = Col(l, w, false)
	}
	return strings.Join(out, "\n")
}
