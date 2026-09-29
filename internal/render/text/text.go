// Package text renders a report as plain text: the development and CI-log
// view. It reads only the report model, like every renderer, so what it
// prints is exactly what the PR comment and job summary will be built from.
package text

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/floors"
	"github.com/arrayofone/coverreport/internal/report"
)

// Options controls how much is printed.
type Options struct {
	// Detail adds every package row and each layer's lowest files.
	Detail bool
}

// Render writes the report.
func Render(w io.Writer, r *report.Report, o Options) error {
	p := &printer{w: w}
	p.header(r)
	p.layers(r)
	p.failures(r)
	p.patch(r)
	p.globs(r)
	if o.Detail {
		p.packages(r)
		p.lowest(r)
	}
	p.ratchet(r)
	p.exclusions(r)
	p.stale(r)
	p.warnings(r)
	return p.err
}

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

func (p *printer) table(write func(tw *tabwriter.Writer)) {
	if p.err != nil {
		return
	}
	tw := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
	write(tw)
	p.err = tw.Flush()
}

func (p *printer) header(r *report.Report) {
	p.f("coverreport: %s", strings.ToUpper(r.Status))
	m := r.Metadata
	var id []string
	if m.Repo != "" {
		id = append(id, m.Repo)
	}
	if m.PR != 0 {
		id = append(id, fmt.Sprintf("#%d", m.PR))
	}
	if m.SHA != "" {
		id = append(id, "@ "+short(m.SHA))
	}
	if m.Base != "" {
		id = append(id, "(base "+short(m.Base)+")")
	}
	if len(id) > 0 {
		p.f("  %s", strings.Join(id, " "))
	}
	p.f("\n")
	if m.RunURL != "" {
		p.f("run: %s\n", m.RunURL)
	}
	floorsNote := r.FloorsFile
	if !r.FloorsFound {
		floorsNote += " (not found: no floors checked)"
	}
	p.f("floors: %s, tolerance %s pts\n\n", floorsNote, trim(r.TolerancePts))
}

func (p *printer) layers(r *report.Report) {
	p.table(func(tw *tabwriter.Writer) {
		fmt.Fprintln(tw, "LAYER\tMETRIC\tCOVERED/TOTAL\tNOW\tFLOOR\tTARGET\tDELTA\tSTATUS")
		for _, l := range r.Layers {
			for i, m := range l.Metrics {
				mr := l.Totals[m]
				name := ""
				if i == 0 {
					name = l.ID
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					name, m, frac(mr), pct(mr.Pct), floorStr(mr.Floor), num(mr.Target), delta(mr.Delta), mr.Status)
			}
		}
	})
	p.f("\n")
}

func (p *printer) failures(r *report.Report) {
	if len(r.Failures) == 0 {
		return
	}
	p.f("Failures (%d):\n", len(r.Failures))
	for _, f := range r.Failures {
		p.f("  - %s\n", f.Message)
	}
	p.f("\n")
}

func (p *printer) patch(r *report.Report) {
	pt := r.Patch
	if pt.Status == report.StatusNotComputed {
		p.f("Patch: not computed (no diff)\n\n")
		return
	}
	mode := "informational"
	if pt.Blocking {
		mode = "blocking"
	}
	p.f("Patch: %s overall (%d/%d), target %s%%, %s, %s\n", pct(pt.Overall.Pct), pt.Overall.Covered, pt.Overall.Total, trim(pt.Target), mode, pt.Status)
	if d := pt.Diff; d != nil {
		p.f("  diff: %s, %s, %d deleted, %d renamed, %d binary, %d unmeasured\n",
			plural(d.Files, "file", "files"), plural(d.AddedLines, "added line", "added lines"), len(d.Deleted), len(d.Renamed), len(d.Binary), len(d.Unmeasured))
	}
	for _, l := range pt.Layers {
		if l.Status == report.StatusNotMeasured {
			p.f("  %s: not measured\n", l.Layer)
			continue
		}
		p.f("  %s: %s (%d/%d) %s\n", l.Layer, pct(l.Pct), l.Covered, l.Total, l.Status)
		for _, f := range l.Files {
			if len(f.UncoveredChanged) > 0 {
				p.f("    %s %d/%d, uncovered %s\n", f.Path, f.Covered, f.Total, ranges(f.UncoveredChanged))
			}
		}
	}
	p.f("\n")
}

func (p *printer) globs(r *report.Report) {
	for _, l := range r.Layers {
		if len(l.Globs) == 0 {
			continue
		}
		p.f("Globs in %s:\n", l.ID)
		p.groupTable(l, l.Globs)
		p.f("\n")
	}
}

func (p *printer) packages(r *report.Report) {
	for _, l := range r.Layers {
		if len(l.Packages) == 0 {
			continue
		}
		p.f("Packages in %s:\n", l.ID)
		p.groupTable(l, l.Packages)
		p.f("\n")
	}
}

func (p *printer) groupTable(l report.Layer, gs []report.Group) {
	p.table(func(tw *tabwriter.Writer) {
		fmt.Fprintln(tw, "  KEY\tFILES\tMETRIC\tCOVERED/TOTAL\tNOW\tFLOOR\tTARGET\tSTATUS")
		for _, g := range gs {
			for i, m := range l.Metrics {
				mr := g.Totals[m]
				key, files := "", ""
				if i == 0 {
					key, files = g.Key, fmt.Sprint(g.Files)
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", key, files, m, frac(mr), pct(mr.Pct), floorStr(mr.Floor), num(mr.Target), mr.Status)
			}
		}
	})
}

func (p *printer) lowest(r *report.Report) {
	for _, l := range r.Layers {
		if len(l.LowestFiles) == 0 {
			continue
		}
		p.f("Lowest files in %s (by uncovered %s):\n", l.ID, l.Metrics[0])
		p.table(func(tw *tabwriter.Writer) {
			for _, f := range l.LowestFiles {
				c := f.Counts[l.Metrics[0]]
				fmt.Fprintf(tw, "  %s\t%d/%d\t%.2f%%\t%s\n", f.Path, c.Covered, c.Total, f.Pct, ranges(f.UncoveredLines))
			}
		})
		p.f("\n")
	}
}

func (p *printer) ratchet(r *report.Report) {
	if len(r.Ratchet) == 0 {
		return
	}
	p.f("Ratchet (%s; coverreport ratchet writes them):\n", plural(len(r.Ratchet), "floor can rise or be created", "floors can rise or be created"))
	p.table(func(tw *tabwriter.Writer) {
		for _, c := range r.Ratchet {
			from := "new"
			if c.From != nil {
				from = floors.FormatFloor(*c.From)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s -> %s\n", c.Scope, c.Layer, strings.TrimSpace(c.Key+" "+c.Metric), from, floors.FormatFloor(c.To))
		}
	})
	p.f("\n")
}

func (p *printer) exclusions(r *report.Report) {
	if len(r.Exclusions) == 0 {
		return
	}
	p.f("Excluded (%s):\n", r.ExcludeFile)
	p.table(func(tw *tabwriter.Writer) {
		for _, e := range r.Exclusions {
			var sizes []string
			for _, s := range e.Layers {
				sizes = append(sizes, fmt.Sprintf("%s: %s %s", s.Layer, plural(s.Files, "file", "files"), countsStr(s.Counts)))
			}
			if len(sizes) == 0 {
				sizes = []string{"matches nothing measured"}
			}
			fmt.Fprintf(tw, "  %s\t# %s\t%s\n", e.Glob, e.Reason, strings.Join(sizes, "; "))
		}
	})
	p.f("\n")
	for _, l := range r.Layers {
		for _, s := range l.Scope {
			if s.Files == 0 {
				continue
			}
			what := "not included"
			if s.Kind == report.ScopeExcluded {
				what = "layer exclude " + s.Glob
			}
			p.f("  %s scope: %s: %s %s\n", l.ID, what, plural(s.Files, "file", "files"), countsStr(s.Counts))
		}
	}
}

func (p *printer) stale(r *report.Report) {
	if len(r.StaleFloors) == 0 {
		return
	}
	p.f("\nStale floors (match nothing measured; ratchet --prune removes them):\n")
	for _, s := range r.StaleFloors {
		p.f("  %s %s %s\n", s.Scope, s.Layer, s.Key)
	}
}

func (p *printer) warnings(r *report.Report) {
	if len(r.Warnings) == 0 {
		return
	}
	p.f("\nWarnings:\n")
	for _, w := range r.Warnings {
		p.f("  - %s\n", w)
	}
}

func frac(mr report.MetricResult) string {
	if mr.Status == report.StatusNotMeasured {
		return "-"
	}
	return fmt.Sprintf("%d/%d", mr.Covered, mr.Total)
}

func pct(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *v)
}

func floorStr(v *float64) string {
	if v == nil {
		return "-"
	}
	return floors.FormatFloor(*v)
}

func num(v *float64) string {
	if v == nil {
		return "-"
	}
	return trim(*v)
}

func delta(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%+.2f", *v)
}

func trim(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func ranges(rs []coverage.Range) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		if r[0] == r[1] {
			parts[i] = fmt.Sprintf("L%d", r[0])
		} else {
			parts[i] = fmt.Sprintf("L%d-%d", r[0], r[1])
		}
	}
	return strings.Join(parts, ", ")
}

func countsStr(c map[string]coverage.Count) string {
	ks := make([]string, 0, len(c))
	for k := range c {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	parts := make([]string, 0, len(ks))
	for _, k := range ks {
		parts = append(parts, plural(c[k].Total, singular[k], k))
	}
	return strings.Join(parts, ", ")
}

// singular names one unit of each metric, for plural.
var singular = map[string]string{"statements": "statement", "lines": "line", "branches": "branch", "functions": "function"}

// plural is "1 file" or "2 files": a count is never printed beside a
// noun that disagrees with it, or hedged as "file(s)".
func plural[N int | int64](n N, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
