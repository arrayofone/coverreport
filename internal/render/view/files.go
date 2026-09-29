package view

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/DarrenBangsund/coverreport/internal/coverage"
	"github.com/DarrenBangsund/coverreport/internal/report"
	"github.com/DarrenBangsund/coverreport/internal/source"
)

// Line states in a listing.
const (
	LineCtx    = '.' // unchanged, not coverable
	LineCtxCov = 'c' // unchanged, ran
	LineCtxUnc = 'o' // unchanged, never ran (older debt)
	LineChg    = '~' // changed, not coverable (a blank line, a comment)
	LineChgCov = '+' // changed, ran
	LineChgUnc = '!' // changed, never ran: what the surfaces are about
)

// File is one changed file that a measured layer contains.
type File struct {
	// Idx numbers files in display order; the page's anchors are f<Idx>
	// and f<Idx>-L<line>.
	Idx       int
	Path      string
	Dir, Base string
	Lang      string
	// Layers are the gated layers measuring this file (report-only ones
	// when no gated layer does), in config order.
	Layers []*report.Layer
	// Failing marks the layers whose failure this file's lines count
	// toward.
	Failing map[string]bool
	// ReportOnly: only report-only layers measure the file.
	ReportOnly bool

	// Changed and Coverable/Covered are line sets: the added lines, and
	// whether a line can run and did (a union over Layers).
	changed   map[int]bool
	coverable map[int]bool
	covered   map[int]bool
	// flagged are the changed lines shown as never run: coverable, and not
	// run by any gated layer or not run by a layer this file fails.
	flagged map[int]bool
	// maxLine is the highest line the report knows of.
	maxLine int

	// Ran/Total count the changed coverable lines; Uncovered are the
	// flagged ones as ranges.
	Ran, Total int64
	Uncovered  []URange
	Source     []string
}

// URange is one run of changed lines that never ran.
type URange struct {
	coverage.Range
	// Func is the enclosing declaration's name, when the source is known.
	Func string
	// First is the first uncovered line's code, trimmed.
	First string
	// Layers are the gated layers in which some line of the range did not
	// run; Fails the ones among them whose failure it counts toward.
	Layers []*report.Layer
	Fails  bool
}

// From is the range's first line.
func (u URange) From() int { return u.Range[0] }

// Missing is how many changed coverable lines never ran.
func (f *File) Missing() int64 { return f.Total - f.Ran }

// InLayer reports whether a layer measures the file.
func (f *File) InLayer(id string) bool {
	for _, l := range f.Layers {
		if l.ID == id {
			return true
		}
	}
	return false
}

// AnyFailing reports whether the file counts toward any failure.
func (f *File) AnyFailing() bool { return len(f.Failing) > 0 }

// State is a line's listing state.
func (f *File) State(n int) byte {
	switch {
	case f.changed[n] && f.flagged[n]:
		return LineChgUnc
	case f.changed[n] && f.coverable[n]:
		return LineChgCov
	case f.changed[n]:
		return LineChg
	case f.covered[n]:
		return LineCtxCov
	case f.coverable[n]:
		return LineCtxUnc
	}
	return LineCtx
}

// Code is line n's text, "" when the source is unknown.
func (f *File) Code(n int) string {
	if n < 1 || n > len(f.Source) {
		return ""
	}
	return f.Source[n-1]
}

// Strip is one cell per changed coverable line in line order: true ran.
func (f *File) Strip() []bool {
	var lines []int
	for n := range f.changed {
		if f.coverable[n] {
			lines = append(lines, n)
		}
	}
	sort.Ints(lines)
	out := make([]bool, len(lines))
	for i, n := range lines {
		out[i] = !f.flagged[n]
	}
	return out
}

// StripText draws Strip with ■ (ran) and □ (never ran).
func (f *File) StripText(max int) string {
	var b strings.Builder
	s := f.Strip()
	for i, ran := range s {
		if i == max {
			b.WriteString("…")
			break
		}
		if ran {
			b.WriteRune('■')
		} else {
			b.WriteRune('□')
		}
	}
	return b.String()
}

func (v *View) files(src *source.Root) {
	if v.R.Patch.Status == report.StatusNotComputed {
		return
	}
	byPath := map[string]*File{}
	var order []string
	type entry struct {
		l  *report.Layer
		pf *report.PatchFile
	}
	gated := map[string][]entry{}
	reportOnly := map[string][]entry{}
	for i := range v.R.Patch.Layers {
		pl := &v.R.Patch.Layers[i]
		l := v.layers[pl.Layer]
		if l == nil || pl.Status == report.StatusNotMeasured {
			continue
		}
		for j := range pl.Files {
			pf := &pl.Files[j]
			if _, ok := byPath[pf.Path]; !ok {
				byPath[pf.Path] = &File{Path: pf.Path}
				order = append(order, pf.Path)
			}
			if l.ReportOnly {
				reportOnly[pf.Path] = append(reportOnly[pf.Path], entry{l, pf})
			} else {
				gated[pf.Path] = append(gated[pf.Path], entry{l, pf})
			}
		}
	}
	for _, p := range order {
		f := byPath[p]
		es := gated[p]
		if len(es) == 0 {
			es, f.ReportOnly = reportOnly[p], true
		}
		f.Dir, f.Base = path.Dir(p), path.Base(p)
		if f.Dir == "." {
			f.Dir = ""
		}
		f.Lang = langOf(p)
		f.changed, f.coverable, f.covered, f.flagged = map[int]bool{}, map[int]bool{}, map[int]bool{}, map[int]bool{}
		f.Failing = map[string]bool{}
		uncoveredIn := map[int][]*report.Layer{}
		for _, e := range es {
			f.Layers = append(f.Layers, e.l)
			fails := !f.ReportOnly && v.fileFails(e.l.ID, p)
			if fails {
				f.Failing[e.l.ID] = true
			}
			for _, r := range e.pf.Changed {
				for n := r[0]; n <= r[1]; n++ {
					f.changed[n] = true
					f.maxLine = max(f.maxLine, n)
				}
			}
			for _, r := range e.pf.CoveredLines {
				for n := r[0]; n <= r[1]; n++ {
					f.coverable[n], f.covered[n] = true, true
					f.maxLine = max(f.maxLine, n)
				}
			}
			for _, r := range e.pf.UncoveredLines {
				for n := r[0]; n <= r[1]; n++ {
					f.coverable[n] = true
					f.maxLine = max(f.maxLine, n)
				}
			}
			for _, r := range e.pf.UncoveredChanged {
				for n := r[0]; n <= r[1]; n++ {
					uncoveredIn[n] = append(uncoveredIn[n], e.l)
					if fails {
						f.flagged[n] = true
					}
				}
			}
		}
		for n := range f.changed {
			if !f.coverable[n] {
				continue
			}
			f.Total++
			if !f.covered[n] {
				f.flagged[n] = true
			}
			if !f.flagged[n] {
				f.Ran++
			}
		}
		f.Source = src.Lines(p)
		f.ranges(uncoveredIn)
		v.Files = append(v.Files, f)
	}
	// Files that count toward a failure first, then the most lines that
	// never ran, then by path: the order a reviewer should read them in.
	sort.SliceStable(v.Files, func(i, j int) bool {
		a, b := v.Files[i], v.Files[j]
		if a.AnyFailing() != b.AnyFailing() {
			return a.AnyFailing()
		}
		if a.Missing() != b.Missing() {
			return a.Missing() > b.Missing()
		}
		return a.Path < b.Path
	})
	for i, f := range v.Files {
		f.Idx = i + 1
	}
}

// ranges groups flagged lines into ranges. Two flagged lines join when
// every line between them is changed but not coverable (a blank line, a
// comment), so a range reads as the block a test would reach.
func (f *File) ranges(uncoveredIn map[int][]*report.Layer) {
	var lines []int
	for n := range f.flagged {
		lines = append(lines, n)
	}
	sort.Ints(lines)
	var cur *URange
	for _, n := range lines {
		if cur != nil {
			joinable := true
			for k := cur.Range[1] + 1; k < n; k++ {
				if !f.changed[k] || f.coverable[k] {
					joinable = false
					break
				}
			}
			if joinable {
				cur.Range[1] = n
				f.addLayers(cur, uncoveredIn[n])
				continue
			}
			f.Uncovered = append(f.Uncovered, *cur)
		}
		cur = &URange{Range: coverage.Range{n, n}}
		f.addLayers(cur, uncoveredIn[n])
	}
	if cur != nil {
		f.Uncovered = append(f.Uncovered, *cur)
	}
	for i := range f.Uncovered {
		u := &f.Uncovered[i]
		u.Func = FuncContext(f.Source, u.Range[0])
		u.First = strings.TrimSpace(f.Code(u.Range[0]))
		for _, l := range u.Layers {
			if f.Failing[l.ID] {
				u.Fails = true
			}
		}
	}
}

func (f *File) addLayers(u *URange, ls []*report.Layer) {
	for _, l := range ls {
		dup := false
		for _, x := range u.Layers {
			if x.ID == l.ID {
				dup = true
			}
		}
		if !dup {
			u.Layers = append(u.Layers, l)
		}
	}
}

// Window is one excerpt of a file: lines From..To, and the declaration
// enclosing the first line the window is about (its first changed line, or
// first line that never ran), which is what a reader wants to see named.
type Window struct {
	From, To int
	Func     string
	anchor   int
}

// Windows returns excerpts around the changed lines (all of them when
// onlyUncovered is false, else only the flagged ranges), with before/after
// lines of context, merged when they touch. Without source they cover the
// changed lines only.
func (f *File) Windows(onlyUncovered bool, before, after int) []Window {
	var spans []coverage.Range
	if onlyUncovered {
		for _, u := range f.Uncovered {
			spans = append(spans, u.Range)
		}
	} else {
		var lines []int
		for n := range f.changed {
			lines = append(lines, n)
		}
		spans = coverage.Ranges(lines)
	}
	last := f.maxLine
	if f.Source != nil {
		last = len(f.Source)
	} else {
		before, after = 0, 0
	}
	var out []Window
	for _, s := range spans {
		a, b := max(1, s[0]-before), min(last, s[1]+after)
		if b < a {
			b = a
		}
		if n := len(out); n > 0 && a <= out[n-1].To+1 {
			out[n-1].To = max(out[n-1].To, b)
			continue
		}
		out = append(out, Window{From: a, To: b, anchor: s[0]})
	}
	for i := range out {
		out[i].Func = FuncContext(f.Source, out[i].anchor)
	}
	return out
}

func langOf(p string) string {
	switch path.Ext(p) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts":
		return "ts"
	case ".dart":
		return "dart"
	}
	return ""
}

var (
	goFuncRE    = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)
	tsFuncRE    = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:async\s+)?function\*?\s+([A-Za-z_$][\w$]*)`)
	tsConstRE   = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)`)
	classRE     = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?(?:class|interface|enum|mixin|extension|type)\s+([A-Za-z_$][\w$]*)`)
	goTypeRE    = regexp.MustCompile(`^type\s+([A-Za-z_]\w*)`)
	dartFuncRE  = regexp.MustCompile(`^[A-Za-z_][\w<>?, ]*\s+([A-Za-z_]\w*)\s*\(`)
	declStartRE = regexp.MustCompile(`^[A-Za-z_$]`)
)

// FuncContext names the declaration enclosing line n, the way git's default
// hunk header does: the nearest line at or above n that starts in column
// one with a letter, "_" or "$". That line is then reduced to the declared
// name (a Go func or method, a TS/JS function, const or class, a Dart
// function or class). "" when the source is unknown or nothing precedes.
func FuncContext(src []string, n int) string {
	for i := min(n, len(src)) - 1; i >= 0; i-- {
		line := src[i]
		if !declStartRE.MatchString(line) {
			continue
		}
		t := strings.TrimSpace(line)
		for _, re := range []*regexp.Regexp{goFuncRE, tsFuncRE, classRE, goTypeRE, tsConstRE} {
			if m := re.FindStringSubmatch(t); m != nil {
				return m[1]
			}
		}
		for _, skip := range []string{"package ", "import ", "export {", "export *", "library ", "part "} {
			if strings.HasPrefix(t, skip) {
				return ""
			}
		}
		if m := dartFuncRE.FindStringSubmatch(t); m != nil {
			return m[1]
		}
		return Cut(t, 40)
	}
	return ""
}
