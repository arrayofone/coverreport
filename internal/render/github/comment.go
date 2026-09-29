package github

import (
	"fmt"
	"strings"

	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/endpoints"
	"github.com/arrayofone/coverreport/internal/render/view"
)

// CommentLimit is GitHub's cap on an issue comment body: 65,536
// characters. The budget keeps a margin under it, so an edit that counts a
// little differently from utf16Len still fits.
const CommentLimit = 65536

const commentBudget = CommentLimit - 1024

// Options tune the markdown surfaces. Zero values mean the defaults.
type Options struct {
	// CommentBudget / SummaryBudget override the size budgets (tests use
	// small ones to exercise truncation with a readable golden).
	CommentBudget int
	SummaryBudget int
}

// Comment renders the sticky PR comment. Its first line is view.Marker,
// by which the upsert finds it again; its second carries the push history.
func Comment(v *view.View, o Options) string {
	budget := o.CommentBudget
	if budget == 0 {
		budget = commentBudget
	}
	var secs []section
	add := func(name string, prio int, text string) {
		if text != "" {
			secs = append(secs, section{name: name, priority: prio, text: text})
		}
	}
	add("marker", 0, view.Marker+"\n"+view.StateLine(v.PushNo, v.Pushes)+"\n")
	add("heading", 0, fmt.Sprintf("### %s %s coverage: %s\n\n", v.Light(), esc(v.Brand.Name), v.Heading))
	add("alert", 0, alert(v, "the Coverage check"))
	add("layers", 0, block("diff", hud(v))+legend(v)+"\n")
	if t := uncoveredTable(v, 25, "####"); t != "" {
		add("changed lines", 0, t)
		shrinkable(secs, uncoveredTable(v, 10, "####"), uncoveredTable(v, 3, "####"))
	}
	failing, other := 0, 0
	for _, f := range v.UncoveredFiles() {
		s := snippet(f, 3, 2, 40)
		if s == "" {
			continue
		}
		open := ""
		prio := 7
		name := "snippets for files that hold their floors"
		if f.AnyFailing() {
			open, prio, name = " open", 2, "snippets for files below a floor"
			failing++
		} else {
			other++
		}
		add(name, prio, fmt.Sprintf("<details%s>\n<summary><code>%s</code> %s: %d of %s never ran</summary>\n\n%s\n</details>\n",
			open, htmlEsc(f.Base), view.RangesLabel(uranges(f.Uncovered)), f.Missing(), view.Plural(f.Total, "changed line", "changed lines"), block("diff", s)))
	}
	if failing+other > 0 {
		add("spacer", 0, "\n")
	}
	add("packages touched", 3, touchedDetails(v))
	add("ratchet", 3, ratchetDetails(v, 12))
	add("endpoints", 3, endpointsDetails(v, false))
	add("exclusions", 6, exclusionsDetails(v, false))
	add("how measured", 6, measuredDetails(v, false))
	add("footer", 0, commentFooter(v))
	out, _ := assemble(secs, budget, utf16Len, func(dropped []string) string {
		return truncationNote(v, dropped, "GitHub's 65,536-character comment limit", true)
	})
	return out
}

// shrinkable gives the last section shorter versions of itself.
func shrinkable(secs []section, alts ...string) {
	if len(secs) > 0 {
		secs[len(secs)-1].alts = alts
	}
}

// truncationNote says what a shortened surface left out and where the rest
// is (the job summary is pointed to only from the comment).
func truncationNote(v *view.View, dropped []string, limit string, toSummary bool) string {
	where := "The report page"
	if v.Links.Artifact != "" {
		where = link("The report page", v.Links.Artifact)
	}
	verb := " has"
	if toSummary && v.Links.Run != "" {
		where += " and the " + link("job summary", v.Links.Run)
		verb = " have"
	}
	return fmt.Sprintf("> [!NOTE]\n> Shortened to fit %s: left out %s. %s%s everything.\n\n", limit, strings.Join(dropped, ", "), where, verb)
}

// alert is the single GitHub alert under the heading, chosen in priority
// order: CAUTION for a failure, NOTE for a patch under target that does not
// block, TIP when every floor holds and some can rise, NOTE when nothing is
// gated yet. check names what fails ("the Coverage check", "this job").
func alert(v *view.View, check string) string {
	var b strings.Builder
	switch v.Verdict {
	case view.VerdictFail:
		b.WriteString("> [!CAUTION]\n")
		ps := v.Problems
		if len(ps) == 1 {
			p := ps[0]
			fmt.Fprintf(&b, "> **%s %s**, so %s fails.", view.Capitalize(esc(p.Subject())), esc(p.Predicate(v.R.Patch.Target)), check)
			if n := v.PatchNote(p); n != "" {
				b.WriteString(" " + n)
			}
			b.WriteString("\n")
			b.WriteString(problemFix(v, p))
			break
		}
		fmt.Fprintf(&b, "> **%d gates fail**, so %s fails:\n", len(ps), check)
		for _, p := range ps {
			fmt.Fprintf(&b, "> - **%s** %s.", esc(p.Subject()), esc(p.Predicate(v.R.Patch.Target)))
			if p.Repeat != nil {
				fmt.Fprintf(&b, " The same changed lines as %s.", esc(p.Repeat.Subject()))
			} else if c := coverLinks(v, p); c != "" {
				b.WriteString(" Cover " + c + ".")
			}
			if p.Kind == "endpoints" {
				b.WriteString(" " + violationsText(p, 5))
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, ">\n> Add the tests, or lower the floors in `%s`%s and say why.\n", v.R.FloorsFile, v.InThisChange())
	case view.VerdictWarn:
		b.WriteString("> [!NOTE]\n")
		warnLines(&b, v)
	case view.VerdictOK:
		if n := v.Rises(); n > 0 {
			fmt.Fprintf(&b, "> [!TIP]\n> **Every floor holds, and %s.** Run `%s` and commit `%s` to lock the gain in.\n",
				view.Plural(int64(n), "can rise", "can rise"), v.RatchetCmd, v.R.FloorsFile)
		}
	case view.VerdictNone:
		fmt.Fprintf(&b, "> [!NOTE]\n> **No floors yet**, so nothing is gated. Run `%s` and commit `%s` to start the gate at today's numbers: every floor after that only rises.\n",
			v.RatchetCmd, v.R.FloorsFile)
		warnLines(&b, v)
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("\n")
	return b.String()
}

// warnLines is one alert line per gated layer whose patch is under target.
func warnLines(b *strings.Builder, v *view.View) {
	for _, pr := range v.WarnRows() {
		fmt.Fprintf(b, "> **%s patch coverage is %s**, under the %s%% target: %d of %s never ran. %s\n",
			view.Capitalize(esc(pr.Layer.ShortLabel)), view.FracPct(pr.PL.Covered, pr.PL.Total), view.Trim(v.Patch.Target),
			pr.PL.Total-pr.PL.Covered, view.Plural(pr.PL.Total, "changed line", "changed lines"), soakSentence(v))
	}
}

func soakSentence(v *view.View) string {
	if v.Patch.Until != "" {
		return fmt.Sprintf("Informational until %s, then it blocks.", v.Patch.Until)
	}
	return "Informational: it does not fail the check."
}

// problemFix is the single-failure alert's second line: what to do.
func problemFix(v *view.View, p *view.Problem) string {
	switch p.Kind {
	case "floor", "package", "glob", "patch":
		c := coverLinks(v, p)
		lower := fmt.Sprintf("lower `%s` in `%s`%s and say why", view.FloorKey(p.Failure), v.R.FloorsFile, v.InThisChange())
		if p.Kind == "patch" {
			return "> Cover " + orText(c, "the changed lines") + ", or set `patch.blocking` to false in the coverage config.\n"
		}
		if c == "" {
			return "> Add tests to bring it back over the floor, or " + lower + ".\n"
		}
		return "> Cover " + c + ", or " + lower + ".\n"
	case "nodata":
		return fmt.Sprintf("> Check that the %s collector still writes its artifact, or remove the floor from `%s`.\n", esc(p.Failure.Layer), v.R.FloorsFile)
	case "required":
		return "> Check that its collector job ran and uploaded its artifact.\n"
	case "endpoints":
		return "> " + violationsText(p, 8) + "\n"
	}
	return ""
}

func orText(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

func violationsText(p *view.Problem, maxN int) string {
	var parts []string
	for i, vi := range p.Violations {
		if i == maxN {
			parts = append(parts, fmt.Sprintf("and %d more", len(p.Violations)-maxN))
			break
		}
		s := code(vi.ID)
		if vi.Detail != "" {
			s += " (" + esc(vi.Detail) + ")"
		}
		parts = append(parts, s)
	}
	return "Gaps: " + strings.Join(parts, "; ") + "."
}

// coverLinks links the uncovered ranges of the files a problem counts:
// [`svc-payments.ts` L791–792](...) and [`dispute-notice.tsx` L101, L140](...).
func coverLinks(v *view.View, p *view.Problem) string {
	var parts []string
	for i, f := range p.Files {
		if i == 4 {
			parts = append(parts, view.Plural(int64(len(p.Files)-4), "more file", "more files"))
			break
		}
		rs := uranges(f.Uncovered)
		text := code(f.Base) + " " + view.RangesLabel(firstN(rs, 3))
		if len(rs) > 3 {
			text += ", …"
		}
		parts = append(parts, link(text, v.Links.Lines(f.Path, rs[0])))
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

func firstN(rs []coverage.Range, n int) []coverage.Range {
	if len(rs) > n {
		return rs[:n]
	}
	return rs
}

// uncoveredTable is "N changed lines have no test" and its table.
func uncoveredTable(v *view.View, maxRows int, h string) string {
	if v.Patch == nil {
		return ""
	}
	files := v.UncoveredFiles()
	n := v.Uncovered()
	var b strings.Builder
	if n == 0 {
		total := int64(0)
		for _, f := range v.Files {
			total += f.Total
		}
		if total == 0 {
			fmt.Fprintf(&b, "%s No changed line is coverable\n\nThis change adds no line a measured layer can run (docs, config, or files no layer includes).\n\n", h)
			return b.String()
		}
		fmt.Fprintf(&b, "%s Every changed line ran\n\nAll %s this change adds ran under a test.\n\n", h, view.Plural(total, "coverable line", "coverable lines"))
		return b.String()
	}
	fmt.Fprintf(&b, "%s %s no test\n\n", h, view.Plural(n, "changed line has", "changed lines have"))
	b.WriteString("| file | counts toward | changed lines | ran | no test yet |\n|:--|:--|:--|--:|:--|\n")
	for i, f := range files {
		if i == maxRows {
			fmt.Fprintf(&b, "| … and %s | | | | the report page lists every one |\n", view.Plural(int64(len(files)-maxRows), "more file", "more files"))
			break
		}
		name := link(code(f.Base), v.Links.Blob(f.Path))
		if f.Dir != "" {
			name += "<br><sub>" + htmlEsc(f.Dir) + "</sub>"
		}
		var toward []string
		for _, t := range v.CountsToward(f) {
			if t.Fails {
				toward = append(toward, "**"+esc(t.Label)+"**")
			} else {
				toward = append(toward, esc(t.Label))
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d/%d | %s |\n", name, strings.Join(toward, "<br>"), code(f.StripText(40)), f.Ran, f.Total,
			rangesLinks(v, f, uranges(f.Uncovered), 6))
	}
	b.WriteString("\n")
	return b.String()
}

func touchedDetails(v *view.View) string {
	if len(v.Touched) == 0 {
		return ""
	}
	below, floored := 0, 0
	layers := map[string]bool{}
	for _, p := range v.Touched {
		if p.Failing {
			below++
		}
		if p.M.Floor != nil {
			floored++
		}
		layers[p.Layer.ID] = true
	}
	summary := "Packages touched: " + touchedWords(len(v.Touched), floored, below)
	rows := v.Touched
	more := ""
	if len(rows) > 20 {
		more = fmt.Sprintf("\n%d more in the job summary.\n", len(rows)-20)
		rows = rows[:20]
	}
	open := ""
	if below > 0 {
		open = " open"
	}
	return fmt.Sprintf("<details%s>\n<summary>%s</summary>\n\n%s%s\n</details>\n\n", open, summary, block("diff", pkgBlock(v, rows, len(layers) > 1)), more)
}

// touchedWords is "3, all hold their floors", "5, 1 below its floor",
// "4, the 2 with a floor hold", "2, none has a floor yet".
func touchedWords(n, floored, below int) string {
	s := fmt.Sprint(n)
	switch {
	case below == 1:
		return s + ", 1 below its floor"
	case below > 1:
		return fmt.Sprintf("%s, %d below their floors", s, below)
	case floored == 0:
		return s + ", none with a floor yet"
	case floored < n && floored == 1:
		return s + ", the 1 with a floor holds it"
	case floored < n:
		return fmt.Sprintf("%s, the %d with a floor hold", s, floored)
	case n == 1:
		return s + ", it holds its floor"
	case n == 2:
		return s + ", both hold their floors"
	}
	return s + ", all hold their floors"
}

func ratchetDetails(v *view.View, maxN int) string {
	if len(v.Ratchets) == 0 || v.Verdict == view.VerdictFail && v.Rises() == 0 {
		return ""
	}
	rises, created := v.Rises(), len(v.Ratchets)-v.Rises()
	var what []string
	if rises > 0 {
		what = append(what, view.Plural(int64(rises), "floor can rise", "floors can rise"))
	}
	if created > 0 {
		what = append(what, view.Plural(int64(created), "floor can be created", "floors can be created"))
	}
	return fmt.Sprintf("<details>\n<summary>Ratchet available: %s</summary>\n\nRun `%s` and commit `%s`. These are the entries it rewrites:\n\n%s\n</details>\n\n",
		strings.Join(what, ", "), v.RatchetCmd, v.R.FloorsFile, block("diff", v.RatchetDiff(maxN)))
}

func endpointsDetails(v *view.View, wide bool) string {
	e := v.Endpoints
	if e == nil {
		return ""
	}
	var head []string
	for _, k := range e.Kinds {
		head = append(head, fmt.Sprintf("%d of %d %s", k.Best.Full, k.Total, endpoints.KindNoun(k.Key, k.Total)))
	}
	open := ""
	if len(e.Violations) > 0 {
		open = " open"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<details%s>\n<summary>Endpoints fully tested: %s</summary>\n\n", open, strings.Join(head, ", "))
	b.WriteString(block("diff", endpointsBlock(v, e.Kinds, "kind", true)))
	if wide {
		b.WriteString("\n")
		b.WriteString(block("diff", endpointsBlock(v, e.Surfaces, "surface", false)))
	}
	if len(e.Violations) > 0 {
		fmt.Fprintf(&b, "\n%s broke the endpoint baseline (it only shrinks; a new endpoint must be fully tested in some layer):\n\n", view.Plural(int64(len(e.Violations)), "endpoint", "endpoints"))
		for i, vi := range e.Violations {
			if i == 20 {
				fmt.Fprintf(&b, "- and %d more\n", len(e.Violations)-20)
				break
			}
			fmt.Fprintf(&b, "- %s", code(vi.ID))
			if vi.Detail != "" {
				b.WriteString(": " + esc(vi.Detail))
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\nAn endpoint is fully tested when some layer proves the happy path and every class of the rubric that applies to it. Read from %s.\n\n</details>\n\n", code(e.File))
	return b.String()
}

func sizeText(s view.Size) string {
	return view.CountShort(s.Metric, s.N)
}

// opener starts a collapsed section in the comment, or a heading in the
// summary (which is read, not skimmed); closer ends it.
func opener(title string, heading bool) string {
	if heading {
		return "### " + title + "\n\n"
	}
	return "<details>\n<summary>" + title + "</summary>\n\n"
}

func closer(heading bool) string {
	if heading {
		return "\n"
	}
	return "\n</details>\n\n"
}

func exclusionsDetails(v *view.View, heading bool) string {
	if len(v.Exclusions) == 0 && len(v.Scope) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(opener("Left out of the denominator: "+v.ExcludedSummary(), heading))
	if len(v.Exclusions) > 0 {
		b.WriteString("| glob | why | size |\n|:--|:--|--:|\n")
		for _, e := range v.Exclusions {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", code(e.Glob), cell(esc(e.Reason)), exclusionSizes(e.Sizes))
		}
		fmt.Fprintf(&b, "\nFrom `%s`: every rule is printed with its size, so an exclusion cannot hide code quietly.\n", v.R.ExcludeFile)
	}
	if len(v.Scope) > 0 {
		b.WriteString("\nLayer boundaries (each layer's own include/exclude):\n\n| layer | rule | size |\n|:--|:--|--:|\n")
		for _, s := range v.Scope {
			rule := "not included"
			if s.Glob != "" {
				rule = "exclude " + code(s.Glob)
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", esc(s.Layer.ShortLabel), rule, sizeText(s.Size))
		}
	}
	b.WriteString(closer(heading))
	return b.String()
}

func exclusionSizes(ss []view.Size) string {
	if len(ss) == 0 {
		return "nothing measured"
	}
	var parts []string
	for _, s := range ss {
		parts = append(parts, fmt.Sprintf("%s in %s", sizeText(s), esc(s.Layer)))
	}
	return strings.Join(parts, "<br>")
}

func measuredDetails(v *view.View, heading bool) string {
	if len(v.Measured) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(opener("How this was measured", heading))
	b.WriteString("| layer | how | inputs |\n|:--|:--|:--|\n")
	for _, l := range v.Measured {
		var in []string
		for _, i := range l.Inputs {
			in = append(in, code(i))
		}
		how := esc(l.Description)
		if how == "" {
			how = l.Format
		}
		if len(l.Modes) > 0 {
			how += " (mode " + strings.Join(l.Modes, ", ") + ")"
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", esc(l.ShortLabel), cell(how), strings.Join(in, "<br>"))
	}
	if d := v.R.Patch.Diff; d != nil {
		fmt.Fprintf(&b, "| patch | the diff against the base: %s, %s added | |\n", view.Plural(int64(d.Files), "file", "files"), view.Plural(int64(d.AddedLines), "line", "lines"))
	}
	b.WriteString(closer(heading))
	return b.String()
}

func commentFooter(v *view.View) string {
	var parts []string
	if v.Links.Artifact != "" {
		parts = append(parts, "Line by line, with every changed file: "+link(v.Links.ArtifactName, v.Links.Artifact)+".")
	} else if v.Links.Run != "" {
		parts = append(parts, fmt.Sprintf("Line by line: the %s artifact of %s.", v.Links.ArtifactName, link("this run", v.Links.Run)))
	}
	if v.Links.Run != "" {
		parts = append(parts, "Every package and glob: "+link("job summary", v.Links.Run)+".")
	}
	parts = append(parts, "Floors: "+link(code(v.R.FloorsFile), v.Links.Blob(v.R.FloorsFile))+".")
	when := ""
	if !v.At.IsZero() {
		when = " at " + v.At.UTC().Format("15:04 UTC, Jan 2")
	}
	push := ""
	if v.PushNo > 0 {
		push = fmt.Sprintf(" for push %d (%s)", v.PushNo, view.Short7(v.PushSHA()))
	}
	parts = append(parts, fmt.Sprintf("Updated%s%s by coverreport %s.", push, when, esc(v.Version)))
	return "<sub>" + strings.Join(parts, " ") + "</sub>\n"
}

// htmlEsc escapes text placed inside inline HTML (<code>, <sub>).
func htmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "|", "&#124;").Replace(s)
}
