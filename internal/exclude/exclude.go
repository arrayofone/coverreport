// Package exclude parses coverage/exclude.txt: the human-maintained list of
// files taken out of every coverage denominator, one glob per line with a
// required reason.
//
//	# Full-line comments start with "#". Blank lines are fine.
//	**/*_templ.go  # templ codegen
//	libs/go/svc/*/main.go  # thin wiring; the e2e lane measures it
//
// The reason is required because every exclusion is printed with its size in
// every report: "5,174 statements excluded" next to a reason a reviewer can
// argue with is the whole defence against coverage improved by deletion from
// the denominator. The canonical separator is two spaces then "#"; any run of
// whitespace before the "#" is accepted, so a hand-aligned column still
// parses. A glob cannot contain whitespace (match a literal space with "?")
// or "#" (everything after the first "#" is the reason).
package exclude

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DarrenBangsund/coverreport/internal/glob"
)

// Rule is one exclusion.
type Rule struct {
	Glob    string
	Pattern *glob.Pattern
	Reason  string
	Line    int
}

// Load reads an exclude file. A file that does not exist is an empty list:
// a repo with no exclusions needs no file.
func Load(path string) ([]Rule, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, path)
}

// Parse reads exclude-file lines. name is used in error messages.
func Parse(r io.Reader, name string) ([]Rule, error) {
	var out []Rule
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := strings.TrimRight(sc.Text(), "\r")
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		g, rest, found := strings.Cut(line, "#")
		g = strings.TrimSpace(g)
		reason := strings.TrimSpace(rest)
		switch {
		case !found || reason == "":
			return nil, fmt.Errorf("%s:%d: %q has no reason; write \"<glob>  # <why this is not measured>\"", name, lineNo, line)
		case g == "" || strings.ContainsAny(g, " \t"):
			return nil, fmt.Errorf("%s:%d: %q: want exactly one glob before the reason (no whitespace inside a glob; use ? for a space)", name, lineNo, line)
		case line[len(g)] == '#':
			return nil, fmt.Errorf("%s:%d: %q: separate the glob from \"# reason\" with spaces", name, lineNo, line)
		}
		p, err := glob.Compile(g)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, lineNo, err)
		}
		if prev, dup := seen[g]; dup {
			return nil, fmt.Errorf("%s:%d: %q is already listed on line %d", name, lineNo, g, prev)
		}
		seen[g] = lineNo
		out = append(out, Rule{Glob: g, Pattern: p, Reason: reason, Line: lineNo})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// First returns the index of the first rule matching path, or -1. A file is
// charged to its first matching rule only, so the sizes printed per rule add
// up to the total excluded instead of double counting.
func First(rules []Rule, path string) int {
	for i := range rules {
		if rules[i].Pattern.Match(path) {
			return i
		}
	}
	return -1
}
