// Package github renders the three GitHub-hosted surfaces from a decided
// view: the sticky PR comment (comment.md), the job summary (summary.md)
// and the annotations (annotations.txt, workflow commands).
//
// GitHub markdown carries no colour, so the colour comes from its own diff
// highlighter: inside a ```diff block the first character of a line picks
// its class (+ green, - red, ! amber, # grey, @@ ... @@ purple). The HUD is
// built on that: a row's marker IS its state, and the state word repeats it
// in text so nothing depends on colour alone. Go builds every fixed-width
// grid (text/template is poor at column alignment); the prose is written
// out plainly beside it.
package github

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/DarrenBangsund/coverreport/internal/coverage"
	"github.com/DarrenBangsund/coverreport/internal/render/view"
)

// section is one droppable piece of a markdown document. When a document
// is over its budget, sections are dropped highest priority number first
// (and, within a priority, last first) until it fits; priority 0 is never
// dropped. A section that cannot be dropped can still offer shorter
// versions of itself (alts, longest first: the changed-lines table with
// fewer rows), which are tried only once every droppable section is gone.
type section struct {
	name     string
	priority int
	text     string
	alts     []string
}

// assemble joins sections, dropping what it must to fit limit (measured
// by size). notice renders the note about what was dropped; it is placed
// before the last section (the footer).
func assemble(secs []section, limit int, size func(string) int, notice func(dropped []string) string) (string, []string) {
	secs = append([]section(nil), secs...) // shrinking replaces texts
	keep := make([]bool, len(secs))
	total := map[string]int{}
	for i := range keep {
		keep[i] = true
		total[secs[i].name]++
	}
	var order []string
	gone := map[string]int{}
	// dropped names what is gone for the note: "3 of 16 snippets", or the
	// section's name when all of a one-section name went.
	dropped := func() []string {
		var out []string
		for _, n := range order {
			if total[n] > 1 {
				out = append(out, fmt.Sprintf("%d of %d %s", gone[n], total[n], n))
			} else {
				out = append(out, n)
			}
		}
		return out
	}
	body := func(upTo int) string {
		var b strings.Builder
		for i, s := range secs[:upTo] {
			if keep[i] {
				b.WriteString(s.text)
			}
		}
		return b.String()
	}
	tail := func() string {
		t := secs[len(secs)-1].text
		if len(order) > 0 {
			t = notice(dropped()) + t
		}
		return t
	}
	drop := func(name string) {
		if gone[name] == 0 {
			order = append(order, name)
		}
		gone[name]++
	}
	build := func() string { return body(len(secs)-1) + tail() }
	out := build()
	for size(out) > limit {
		victim := -1
		for i := len(secs) - 1; i >= 0; i-- {
			if keep[i] && secs[i].priority > 0 && (victim < 0 || secs[i].priority > secs[victim].priority) {
				victim = i
			}
		}
		if victim < 0 {
			break
		}
		keep[victim] = false
		drop(secs[victim].name)
		out = build()
	}
	for i := range secs {
		for _, alt := range secs[i].alts {
			if size(out) <= limit {
				break
			}
			secs[i].text = alt
			if n := "rows of the " + secs[i].name + " table"; gone[n] == 0 {
				drop(n)
			}
			out = build()
		}
	}
	if size(out) > limit {
		// Every droppable section is gone, every table is at its
		// shortest, and the rest is still too long: cut the body at a
		// line boundary, close whatever the cut left open, and keep the
		// footer (and the note) whole.
		t := tail()
		out = hardCut(body(len(secs)-1), limit-size(t), size) + t
	}
	return out, dropped()
}

// hardCut truncates a markdown document at a line boundary under limit,
// closing an open code fence and open <details>, and says so.
func hardCut(s string, limit int, size func(string) int) string {
	const tail = "\n\n**Cut here: the rest does not fit GitHub's size limit. The report page has everything.**\n\n"
	lines := strings.Split(s, "\n")
	for n := len(lines); n > 0; n-- {
		body := strings.Join(lines[:n], "\n")
		closing := closers(body)
		out := body + closing + tail
		if size(out) <= limit {
			return out
		}
	}
	return tail
}

// closers returns what closes the fences and <details> left open in s.
func closers(s string) string {
	var fence string
	details := 0
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.Trim(t, "`") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") {
			fence = t[:len(t)-len(strings.TrimLeft(t, "`"))]
			continue
		}
		details += strings.Count(t, "<details") - strings.Count(t, "</details>")
	}
	var b strings.Builder
	if fence != "" {
		b.WriteString("\n" + fence)
	}
	for ; details > 0; details-- {
		b.WriteString("\n\n</details>")
	}
	return b.String()
}

// utf16Len is how GitHub counts a comment against its 65,536-character
// limit (conservatively: an astral-plane emoji counts twice).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func byteLen(s string) int { return len(s) }

// fence returns a backtick fence longer than any backtick run in body.
func fence(body string) string {
	longest, run := 0, 0
	for _, r := range body {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// block wraps body in a fenced block with the given info string.
func block(info, body string) string {
	f := fence(body)
	return f + info + "\n" + body + "\n" + f + "\n"
}

// code is an inline code span that survives a table cell: the fence is
// longer than any backtick run inside, and "|" is escaped (GFM splits cells
// on "|" even inside a code span).
func code(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", longest+1)
	if longest > 0 || strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return f + " " + s + " " + f
	}
	return f + s + f
}

// cell escapes text for a table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

// esc escapes inline markdown in plain text (names, messages).
func esc(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", "&lt;", ">", "&gt;", "|", `\|`)
	return r.Replace(s)
}

// link is [text](url), or just text when url is unknown. text is markdown
// already (escaped by the caller).
func link(text, url string) string {
	if url == "" {
		return text
	}
	url = strings.NewReplacer("(", "%28", ")", "%29", " ", "%20").Replace(url)
	return "[" + text + "](" + url + ")"
}

// rangesLinks links each range of a file to its lines on GitHub, at most
// max of them.
func rangesLinks(v *view.View, f *view.File, rs []coverage.Range, maxN int) string {
	var parts []string
	for i, r := range rs {
		if i == maxN {
			parts = append(parts, fmt.Sprintf("%d more", len(rs)-maxN))
			break
		}
		parts = append(parts, link(view.RangeLabel(r), v.Links.Lines(f.Path, r)))
	}
	return strings.Join(parts, ", ")
}

func uranges(us []view.URange) []coverage.Range {
	out := make([]coverage.Range, len(us))
	for i, u := range us {
		out[i] = u.Range
	}
	return out
}
