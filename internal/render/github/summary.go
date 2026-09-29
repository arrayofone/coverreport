package github

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DarrenBangsund/coverreport/internal/render/view"
)

// SummaryLimit is GitHub's cap on one step's job summary: 1 MiB.
const SummaryLimit = 1 << 20

const summaryBudget = SummaryLimit - 16<<10

// Summary renders the job summary ($GITHUB_STEP_SUMMARY): everything the
// comment has, uncut, plus every package, glob, the lowest files per layer
// and the endpoint surfaces.
func Summary(v *view.View, o Options) string {
	budget := o.SummaryBudget
	if budget == 0 {
		budget = summaryBudget
	}
	var secs []section
	add := func(name string, prio int, text string) {
		if text != "" {
			secs = append(secs, section{name: name, priority: prio, text: text})
		}
	}
	add("heading", 0, fmt.Sprintf("## %s %s coverage: %s\n\n", v.Light(), esc(v.Brand.Name), v.Heading))
	add("intro", 0, summaryIntro(v))
	add("alert", 0, alert(v, "this job"))
	add("layers", 0, "### Layers\n\n"+block("diff", hud(v))+legend(v)+"\n"+chart(v))
	if t := uncoveredTable(v, 200, "###"); t != "" {
		add("changed lines", 0, t)
		shrinkable(secs, uncoveredTable(v, 50, "###"), uncoveredTable(v, 10, "###"))
	}
	for _, f := range v.UncoveredFiles() {
		s := snippet(f, 4, 3, 80)
		if s == "" {
			continue
		}
		prio := 6
		if f.AnyFailing() {
			prio = 4
		}
		add("snippets", prio, fmt.Sprintf("<details open>\n<summary><code>%s</code> %s</summary>\n\n%s\n</details>\n", htmlEsc(f.Path),
			view.RangesLabel(uranges(f.Uncovered)), block("diff", s)))
	}
	if len(v.UncoveredFiles()) > 0 {
		add("spacer", 0, "\n")
	}
	for _, t := range v.Packages {
		add("package tables", 3, packagesSection(v, t))
	}
	add("ratchet", 2, ratchetDetails(v, 0))
	for _, t := range v.Globs {
		add("glob tables", 3, globsSection(t))
	}
	add("endpoints", 2, endpointsSection(v))
	add("lowest files", 5, lowestSection(v))
	add("exclusions", 5, exclusionsDetails(v, true))
	add("how measured", 5, measuredDetails(v, true))
	add("footer", 0, summaryFooter(v))
	out, _ := assemble(secs, budget, byteLen, func(dropped []string) string {
		return truncationNote(v, dropped, "GitHub's 1 MiB job summary limit", false)
	})
	return out
}

func summaryIntro(v *view.View) string {
	m := v.R.Metadata
	var b strings.Builder
	if m.PR != 0 {
		b.WriteString(link(fmt.Sprintf("#%d", m.PR), v.Links.PRURL()))
		if m.Title != "" {
			b.WriteString(" " + esc(m.Title) + ".")
		}
		b.WriteString(" ")
	}
	if m.SHA != "" {
		if m.PR != 0 {
			fmt.Fprintf(&b, "Merge commit `%s`", view.Short7(m.SHA))
		} else {
			fmt.Fprintf(&b, "Commit `%s`", view.Short7(m.SHA))
		}
		if m.BaseRef != "" {
			fmt.Fprintf(&b, " into `%s`", m.BaseRef)
		}
		if m.Base != "" {
			fmt.Fprintf(&b, " at `%s`", view.Short7(m.Base))
		}
		if v.PushNo > 0 {
			fmt.Fprintf(&b, ", push %d", v.PushNo)
		}
		b.WriteString(". ")
	}
	if p := v.Patch; p != nil && p.Overall.Total > 0 {
		fmt.Fprintf(&b, "Patch coverage %s, %d of %s, against the %s%% target.", view.FracPct(p.Overall.Covered, p.Overall.Total),
			p.Overall.Covered, view.Plural(p.Overall.Total, "changed line", "changed lines"), view.Trim(p.Target))
	} else if p != nil {
		b.WriteString("This change adds no coverable line.")
	} else {
		b.WriteString("No diff was analysed, so there is no patch coverage.")
	}
	b.WriteString("\n\n")
	return b.String()
}

// chart is a mermaid bullet chart of the gated layers: target as a grey
// track, now as a green bar. GitHub renders mermaid in job summaries; where
// it does not, it degrades to a readable code block.
func chart(v *view.View) string {
	var labels, targets, nows []string
	for _, r := range v.Rows {
		if r.Layer.ReportOnly || r.Target == nil || r.Pct == nil || r.State == view.StateNotRun {
			continue
		}
		labels = append(labels, strconv.Quote(strings.ReplaceAll(r.Label, `"`, "'")))
		targets = append(targets, view.Trim(*r.Target))
		nows = append(nows, view.Pct2(r.Pct))
	}
	if len(labels) == 0 {
		return ""
	}
	return block("mermaid", fmt.Sprintf(`---
config:
  xyChart:
    width: 760
    height: %d
  themeVariables:
    xyChart:
      plotColorPalette: "#8c959f, #1f9d55"
---
xychart-beta horizontal
    title "Layers: now (green) inside target (grey)"
    x-axis [%s]
    y-axis "percent" 0 --> 100
    bar [%s]
    bar [%s]`, 120+30*len(labels), strings.Join(labels, ", "), strings.Join(targets, ", "), strings.Join(nows, ", "))) + "\n"
}

func packagesSection(v *view.View, t *view.PkgTable) string {
	var rows []*view.PkgRow
	for _, p := range t.Rows {
		if p.M.Total > 0 && p.M.Covered < p.M.Total {
			rows = append(rows, p)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	l := t.Layer
	m := l.Metrics[0]
	tot := l.Totals[m]
	open := ""
	if v.Treemap == t {
		open = " open"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### %s: %s%% over %d packages, most %s without a test first\n\n", esc(l.Label), view.Pct2(tot.Pct), len(t.Rows), m)
	fmt.Fprintf(&b, "<details%s>\n<summary>%d packages", open, len(rows))
	if t.Full > 0 {
		fmt.Fprintf(&b, " (%d more at 100%% are not listed)", t.Full)
	}
	fmt.Fprintf(&b, "</summary>\n\n%s\n</details>\n\n", block("diff", pkgBlock(v, rows, false)))
	return b.String()
}

func globsSection(t *view.GlobTable) string {
	l := t.Layer
	var b strings.Builder
	fmt.Fprintf(&b, "### %s by glob\n\n| glob |", esc(l.Label))
	for _, m := range l.Metrics {
		fmt.Fprintf(&b, " %s |", m)
	}
	b.WriteString(" floors | |\n|:--|")
	for range l.Metrics {
		b.WriteString("--:|")
	}
	b.WriteString("--:|:--|\n")
	for _, g := range t.Rows {
		name := code(g.Key)
		if g.Label != "" {
			name += "<br><sub>" + htmlEsc(g.Label) + "</sub>"
		}
		fmt.Fprintf(&b, "| %s |", name)
		var fl []string
		for _, m := range l.Metrics {
			mr := g.Totals[m]
			s := "—"
			if mr.Pct != nil {
				s = view.Pct2(mr.Pct) + "%"
			}
			if mr.Status == "fail" {
				s = "**" + s + " FAIL**"
			}
			fmt.Fprintf(&b, " %s |", s)
			fl = append(fl, view.Floor(mr.Floor))
		}
		p := g.Totals[l.Metrics[0]]
		pct := 0.0
		if p.Pct != nil {
			pct = *p.Pct
		}
		fmt.Fprintf(&b, " %s | %s |\n", strings.Join(fl, " / "), code(view.Meter(pct, p.Target, false)))
	}
	b.WriteString("\n")
	return b.String()
}

func endpointsSection(v *view.View) string {
	e := v.Endpoints
	if e == nil {
		return ""
	}
	var b strings.Builder
	var head []string
	for _, k := range e.Kinds {
		head = append(head, fmt.Sprintf("%d of %d %s", k.Best.Full, k.Total, k.Label))
	}
	fmt.Fprintf(&b, "### Endpoints fully tested: %s\n\n", strings.Join(head, ", "))
	b.WriteString(block("diff", endpointsBlock(v, e.Kinds, "kind", true)))
	b.WriteString("\n| surface | endpoints | full | partial | none |")
	for _, l := range e.Layers {
		fmt.Fprintf(&b, " %s |", l)
	}
	b.WriteString(" most missed |\n|:--|--:|--:|--:|--:|")
	for range e.Layers {
		b.WriteString("--:|")
	}
	b.WriteString(":--|\n")
	for _, s := range e.Surfaces {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |", esc(s.Label), s.Total, s.Best.Full, s.Best.Partial, s.Best.None)
		for _, l := range e.Layers {
			fmt.Fprintf(&b, " %d |", s.Layers[l].Full)
		}
		mm := "—"
		if s.MostMissed != "" {
			mm = fmt.Sprintf("%s (%d)", esc(s.MostMissed), s.MostMissedCount)
		}
		fmt.Fprintf(&b, " %s |\n", mm)
	}
	b.WriteString("\n<sub>full, partial and none count each endpoint once, at its best layer; the layer columns count endpoints fully tested in that layer.</sub>\n\n")
	if len(e.Violations) > 0 {
		fmt.Fprintf(&b, "**%s broke the endpoint baseline:**\n\n", view.Plural(int64(len(e.Violations)), "endpoint", "endpoints"))
		for _, vi := range e.Violations {
			fmt.Fprintf(&b, "- %s", code(vi.ID))
			if vi.Detail != "" {
				b.WriteString(": " + esc(vi.Detail))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func lowestSection(v *view.View) string {
	if len(v.Lowest) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Lowest files per layer\n\nMost units without a test first: where one test buys the most.\n\n")
	for _, t := range v.Lowest {
		m := t.Layer.Metrics[0]
		fmt.Fprintf(&b, "<details>\n<summary>%s</summary>\n\n| file | ran | of | |\n|:--|--:|--:|:--|\n", esc(t.Layer.Label))
		for _, f := range t.Files {
			c := f.Counts[m]
			fmt.Fprintf(&b, "| %s | %s | %s %s | %s |\n", code(f.Path), view.Thousands(c.Covered), view.Thousands(c.Total), view.UnitShort(m),
				code(view.Meter(f.Pct, nil, false)))
		}
		b.WriteString("\n</details>\n")
	}
	b.WriteString("\n")
	return b.String()
}

func summaryFooter(v *view.View) string {
	var parts []string
	if v.Links.Artifact != "" {
		parts = append(parts, "Line by line: "+link(v.Links.ArtifactName, v.Links.Artifact)+" (opens in the browser for signed-in readers).")
	}
	if v.Links.Run != "" {
		parts = append(parts, link("Run", v.Links.Run)+".")
	}
	when := ""
	if !v.At.IsZero() {
		when = v.At.UTC().Format("15:04 UTC, Jan 2") + ". "
	}
	parts = append(parts, fmt.Sprintf("%scoverreport %s.", when, esc(v.Version)))
	return "<sub>" + strings.Join(parts, " ") + "</sub>\n"
}

// Annotations renders annotations.txt: one workflow command per line, at
// most view.MaxAnnotations. The Coverage job cats it to stdout, so the
// annotations need no checks: write permission.
func Annotations(v *view.View) string {
	var b strings.Builder
	for _, a := range v.Annotations {
		b.WriteString(a.Command())
		b.WriteString("\n")
	}
	return b.String()
}
