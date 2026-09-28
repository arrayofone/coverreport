// Package coverage is the format-neutral model every parser lowers into: a
// repo-relative file with a count per metric and a per-line covered/uncovered
// map. The totals, floors and patch code only ever see this shape, so a Go
// coverprofile and an LCOV tracefile are gated by exactly the same arithmetic.
package coverage

import (
	"fmt"
	"math"
	"sort"
)

// Metric names one coverage measure. Go layers carry statements only (a Go
// coverprofile has no line, branch or function data); LCOV layers carry lines,
// branches and functions (an LCOV tracefile has no statement data).
type Metric string

const (
	Lines      Metric = "lines"
	Statements Metric = "statements"
	Branches   Metric = "branches"
	Functions  Metric = "functions"
)

// All is every metric in the order reports list them when no order is given.
var All = []Metric{Lines, Statements, Branches, Functions}

// ParseMetric validates a metric name from a config or floors file.
func ParseMetric(s string) (Metric, error) {
	for _, m := range All {
		if string(m) == s {
			return m, nil
		}
	}
	return "", fmt.Errorf("unknown metric %q (want one of lines, statements, branches, functions)", s)
}

// Count is a covered/total pair. Everything is kept as integers until the very
// last step so that a floor comparison is exact rather than a float
// comparison that can flip on the 15th digit.
type Count struct {
	Covered int64 `json:"covered"`
	Total   int64 `json:"total"`
}

// Add returns the element-wise sum.
func (c Count) Add(o Count) Count {
	return Count{Covered: c.Covered + o.Covered, Total: c.Total + o.Total}
}

// Uncovered is Total - Covered.
func (c Count) Uncovered() int64 { return c.Total - c.Covered }

// Pct is the percentage covered, 0 when there is nothing to cover. Callers
// that need to distinguish "0%" from "no data" check Total first.
func (c Count) Pct() float64 {
	if c.Total == 0 {
		return 0
	}
	return float64(c.Covered) * 100 / float64(c.Total)
}

// Pct2 is Pct rounded to two decimals: the display value the report carries.
// Gating never uses it; see AtLeast.
func (c Count) Pct2() float64 {
	return math.Round(c.Pct()*100) / 100
}

// FloorTenths is the percentage in tenths of a point, rounded DOWN: the value
// a ratchet writes, so a floor is never above the measurement it came from.
// 2/3 is 666 (66.6), 1/1 is 1000 (100.0).
func (c Count) FloorTenths() int64 {
	if c.Total == 0 {
		return 0
	}
	return c.Covered * 1000 / c.Total
}

// AtLeast reports whether the percentage is >= hundredths/100 percent, in
// exact integer arithmetic: covered/total*100 >= h/100 is
// covered*10000 >= h*total. Floors and tolerances are held in hundredths of a
// point so "77.8 - 0.1" is the integer 7770, never 77.69999999999999.
func (c Count) AtLeast(hundredths int64) bool {
	if c.Total == 0 {
		return false
	}
	return c.Covered*10000 >= hundredths*c.Total
}

// Hundredths converts a percentage with at most two decimals (a floor, a
// tolerance, a target) into integer hundredths of a point.
func Hundredths(pct float64) int64 {
	return int64(math.Round(pct * 100))
}

// File is one source file's coverage inside one layer.
type File struct {
	// Path is repo-relative, slash-separated, cleaned.
	Path string
	// Counts has an entry for every metric the format can measure, even when
	// the total is zero (an LCOV file with no BRDA records has Branches
	// {0, 0}), so "no data" is distinguishable from "not this format".
	Counts map[Metric]Count
	// Lines maps each coverable line to whether it ran. It is what patch
	// coverage and the uncovered-line ranges are computed from. For LCOV it is
	// the DA records; for Go it is derived from blocks (see gocov.LineMap).
	Lines map[int]bool
}

// Range is an inclusive line range, [start, end].
type Range [2]int

// Ranges compresses a set of line numbers into sorted inclusive ranges:
// {3,4,5,9} is [[3,5],[9,9]]. Adjacency is by line number only; a gap of one
// non-coverable line splits a range, which keeps the ranges honest about
// exactly which lines ran.
func Ranges(lines []int) []Range {
	if len(lines) == 0 {
		return []Range{}
	}
	ls := append([]int(nil), lines...)
	sort.Ints(ls)
	out := []Range{{ls[0], ls[0]}}
	for _, l := range ls[1:] {
		last := &out[len(out)-1]
		switch {
		case l == last[1]:
			// duplicate
		case l == last[1]+1:
			last[1] = l
		default:
			out = append(out, Range{l, l})
		}
	}
	return out
}

// LinesWhere returns the sorted line numbers whose covered state equals want.
func (f *File) LinesWhere(want bool) []int {
	var out []int
	for l, cov := range f.Lines {
		if cov == want {
			out = append(out, l)
		}
	}
	sort.Ints(out)
	return out
}
