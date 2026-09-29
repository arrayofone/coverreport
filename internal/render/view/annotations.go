package view

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DarrenBangsund/coverreport/internal/report"
)

// MaxAnnotations is how many annotations a run emits. GitHub shows at most
// 10 of each level per step, and 50 per job; ten, chosen by what matters
// most, beats forty the Files tab drops at random.
const MaxAnnotations = 10

// Annotation is one workflow-command annotation on a range of changed lines
// that never ran.
type Annotation struct {
	// Level is "warning" when the range counts toward a failure (a floor
	// it breaks, a blocking patch miss), "notice" otherwise.
	Level         string
	File          string
	Line, EndLine int
	Title         string
	Message       string
}

// Command is the annotation as the runner reads it from stdout.
func (a Annotation) Command() string {
	return fmt.Sprintf("::%s file=%s,line=%d,endLine=%d,title=%s::%s",
		a.Level, escapeProp(a.File), a.Line, a.EndLine, escapeProp(a.Title), escapeData(a.Message))
}

// escapeData and escapeProp are the runner's workflow-command encoding
// (actions/toolkit's command.ts): data escapes % CR LF, properties also
// ":" and ",".
func escapeData(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	return strings.ReplaceAll(s, "\n", "%0A")
}

func escapeProp(s string) string {
	s = escapeData(s)
	s = strings.ReplaceAll(s, ":", "%3A")
	return strings.ReplaceAll(s, ",", "%2C")
}

func (v *View) annotations() {
	type cand struct {
		f *File
		u URange
	}
	var cs []cand
	for _, f := range v.Files {
		if f.ReportOnly {
			continue
		}
		for _, u := range f.Uncovered {
			cs = append(cs, cand{f, u})
		}
	}
	// Ranges that break something first, then the longest (the biggest
	// block with no test), then file order.
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.u.Fails != b.u.Fails {
			return a.u.Fails
		}
		la, lb := a.u.Range[1]-a.u.Range[0], b.u.Range[1]-b.u.Range[0]
		if la != lb {
			return la > lb
		}
		if a.f.Idx != b.f.Idx {
			return a.f.Idx < b.f.Idx
		}
		return a.u.Range[0] < b.u.Range[0]
	})
	for i, c := range cs {
		if i == MaxAnnotations {
			break
		}
		v.Annotations = append(v.Annotations, v.annotation(c.f, c.u))
	}
}

func (v *View) annotation(f *File, u URange) Annotation {
	a := Annotation{File: f.Path, Line: u.Range[0], EndLine: u.Range[1], Level: "notice"}
	n := u.lines(f)
	var names []string
	for _, l := range u.Layers {
		names = append(names, l.ShortLabel)
	}
	in := ""
	if u.Func != "" {
		in = " in " + u.Func
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "%s%s never ran under %s", Capitalize(Plural(n, "changed line", "changed lines")), in, strings.Join(names, " or "))
	if u.First != "" {
		fmt.Fprintf(&msg, ", starting with:\n    %s", Cut(u.First, 90))
	} else {
		msg.WriteString(".")
	}
	msg.WriteString("\n")

	why, title := v.why(f, u)
	a.Title = fmt.Sprintf("No test reaches %s (%s)", RangeLabel(u.Range), title)
	if u.Fails {
		a.Level = "warning"
	}
	msg.WriteString(why)
	if v.Links.Artifact != "" {
		fmt.Fprintf(&msg, "\nLine by line (%s): %s#f%d-L%d", v.Links.ArtifactName, v.Links.Artifact, f.Idx, u.Range[0])
	} else if v.Links.Run != "" {
		fmt.Fprintf(&msg, "\nLine by line: the %s artifact of %s", v.Links.ArtifactName, v.Links.Run)
	}
	a.Message = msg.String()
	return a
}

// why explains what the range costs, and gives the title's parenthesis.
func (v *View) why(f *File, u URange) (string, string) {
	for _, l := range u.Layers {
		if !f.Failing[l.ID] {
			continue
		}
		for _, p := range v.Problems {
			if p.Layer == nil || p.Layer.ID != l.ID || !p.problemCovers(v, f) {
				continue
			}
			switch p.Kind {
			case "floor":
				return fmt.Sprintf("%s is at %s%%, %s under its %s floor, so the Coverage check fails until a test reaches this code or the floor is lowered in %s.",
					Capitalize(p.Row.Label), Pct2(p.Row.Pct), strings.TrimPrefix(Signed(p.Row.Delta), "-"), Floor(p.Row.Floor), v.R.FloorsFile), p.Row.Label + " is below its floor"
			case "package", "glob":
				what := "package " + p.Failure.Key
				if p.Kind == "glob" {
					what = p.Failure.Key
				}
				return fmt.Sprintf("%s (%s) is at %s%%, under its %s floor, so the Coverage check fails until a test reaches this code or the floor is lowered in %s.",
					Capitalize(what), l.ShortLabel, Pct2(p.Failure.Measured), Floor(p.Failure.Floor), v.R.FloorsFile), what + " is below its floor"
			case "patch":
				pl := v.patchOf(l.ID)
				return fmt.Sprintf("%s patch coverage is %s, under the %s%% target, and the patch gate blocks the merge.",
					Capitalize(l.ShortLabel), FracPct(pl.Covered, pl.Total), Trim(v.R.Patch.Target)), l.ShortLabel + " patch is under target"
			}
		}
	}
	l := u.Layers[0]
	pl := v.patchOf(l.ID)
	if pl != nil && pl.Status == report.StatusFail {
		until := ""
		if d := v.R.Patch.InformationalUntil; d != "" {
			until = " until " + d
		}
		return fmt.Sprintf("%s patch coverage is %s, under the %s%% target. That is informational%s: it does not fail the check.",
			Capitalize(l.ShortLabel), FracPct(pl.Covered, pl.Total), Trim(v.R.Patch.Target), until), l.ShortLabel + " patch under target"
	}
	patch := ""
	if pl != nil && pl.Total > 0 {
		patch = fmt.Sprintf(", and its patch coverage is %s", FracPct(pl.Covered, pl.Total))
	}
	holds := "holds its floor"
	if l.Totals[l.Metrics[0]].Floor == nil {
		holds = "has no floor yet"
	}
	return fmt.Sprintf("This does not fail the check: %s %s%s. A test here takes %s to %d of %d changed lines.",
		l.ShortLabel, holds, patch, f.Base, f.Ran+u.lines(f), f.Total), "informational"
}

func (u URange) lines(f *File) int64 {
	n := int64(0)
	for k := u.Range[0]; k <= u.Range[1]; k++ {
		if f.flagged[k] {
			n++
		}
	}
	return n
}

func (v *View) patchOf(id string) *report.PatchLayer {
	for i := range v.R.Patch.Layers {
		if v.R.Patch.Layers[i].Layer == id {
			return &v.R.Patch.Layers[i]
		}
	}
	return nil
}
