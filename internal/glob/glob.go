// Package glob matches repo-relative, slash-separated paths against
// doublestar-style patterns: the one glob dialect every file coverreport reads
// (config.json, floors.json, exclude.txt) is written in.
//
// Semantics, chosen to match github.com/bmatcuk/doublestar (which is what
// people reach for in Go and what vitest/picomatch users already expect):
//
//   - "**" as a whole path segment matches zero or more segments, so
//     "src/**/*.ts" matches "src/a.ts" and "src/x/y/a.ts".
//   - "*" matches any run of characters inside one segment (never "/"),
//     including a leading dot: there is no dotfile special case.
//   - "?" matches one character, "[a-z]" / "[^a-z]" a class, "\" escapes.
//   - "{a,b}" alternation, nestable, may contain "/" ("src/{lib,app/api}/**").
//   - "**" inside a segment ("a**b") is just two "*"s.
//
// There is no negation: an exclusion list that can un-exclude is a list whose
// meaning depends on reading order, and exclude.txt is meant to be read by a
// reviewer one line at a time.
package glob

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// maxAlternatives bounds brace expansion. "{a,b}{c,d}..." is exponential; a
// real pattern has a handful of alternatives, so a pattern that expands past
// this is a typo, not a need.
const maxAlternatives = 256

// Pattern is a compiled glob.
type Pattern struct {
	src  string
	alts [][]string // brace-expanded alternatives, each split on "/"
}

// Compile validates and compiles a repo-relative glob.
func Compile(src string) (*Pattern, error) {
	p := strings.TrimPrefix(src, "./")
	switch {
	case p == "":
		return nil, errors.New("empty glob")
	case strings.HasPrefix(p, "/"):
		return nil, fmt.Errorf("glob %q is absolute; globs are repo-relative", src)
	case strings.HasSuffix(p, "/"):
		return nil, fmt.Errorf("glob %q ends in /; write %q to match everything under a directory", src, strings.TrimSuffix(p, "/")+"/**")
	case strings.ContainsAny(p, " \t\r\n"):
		return nil, fmt.Errorf("glob %q contains whitespace; match a literal space with ?", src)
	}
	expanded, err := expandBraces(p)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", src, err)
	}
	pat := &Pattern{src: src}
	for _, alt := range expanded {
		segs := strings.Split(alt, "/")
		for _, s := range segs {
			switch s {
			case "":
				return nil, fmt.Errorf("glob %q has an empty path segment (//)", src)
			case ".", "..":
				return nil, fmt.Errorf("glob %q has a %q segment; globs are clean repo-relative paths", src, s)
			}
			if s == "**" {
				continue
			}
			// path.Match validates the whole pattern before matching, so
			// matching against "" is a syntax check.
			if _, err := path.Match(s, ""); err != nil {
				return nil, fmt.Errorf("glob %q: bad segment %q: %w", src, s, err)
			}
		}
		pat.alts = append(pat.alts, segs)
	}
	return pat, nil
}

// MustCompile is Compile for patterns known at compile time (tests).
func MustCompile(src string) *Pattern {
	p, err := Compile(src)
	if err != nil {
		panic(err)
	}
	return p
}

// String returns the pattern as written.
func (p *Pattern) String() string { return p.src }

// Match reports whether a clean, slash-separated, repo-relative path matches.
func (p *Pattern) Match(name string) bool {
	segs := strings.Split(name, "/")
	for _, alt := range p.alts {
		if matchSegs(alt, segs) {
			return true
		}
	}
	return false
}

func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// Roots returns, for each alternative, the leading directory made only of
// literal segments: where a filesystem walk for this pattern can start
// without missing a match. "coverage-go/*.out" -> "coverage-go";
// "**/lcov.info" -> ".".
func (p *Pattern) Roots() []string {
	seen := map[string]bool{}
	var out []string
	for _, alt := range p.alts {
		var lit []string
		for _, s := range alt[:len(alt)-1] {
			if HasMeta(s) {
				break
			}
			lit = append(lit, s)
		}
		root := "."
		if len(lit) > 0 {
			root = strings.Join(lit, "/")
		}
		if !seen[root] {
			seen[root] = true
			out = append(out, root)
		}
	}
	return out
}

// Literal reports whether the pattern names exactly one path (no wildcard in
// any alternative and a single alternative).
func (p *Pattern) Literal() bool {
	if len(p.alts) != 1 {
		return false
	}
	for _, s := range p.alts[0] {
		if HasMeta(s) || s == "**" {
			return false
		}
	}
	return true
}

// HasMeta reports whether s contains any glob syntax.
func HasMeta(s string) bool {
	return strings.ContainsAny(s, `*?[{\`)
}

// expandBraces turns "a{b,c{d,e}}f" into ["abf", "acdf", "acef"]. A backslash
// escapes the next character (kept, so path.Match sees the same escape).
func expandBraces(s string) ([]string, error) {
	open := -1
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			if depth == 0 {
				open = i
			}
			depth++
		case '}':
			if depth == 0 {
				return nil, errors.New("unmatched }")
			}
			depth--
			if depth == 0 {
				prefix, body, suffix := s[:open], s[open+1:i], s[i+1:]
				parts := splitTopLevel(body)
				rest, err := expandBraces(suffix)
				if err != nil {
					return nil, err
				}
				var out []string
				for _, part := range parts {
					inner, err := expandBraces(part)
					if err != nil {
						return nil, err
					}
					for _, in := range inner {
						for _, r := range rest {
							out = append(out, prefix+in+r)
							if len(out) > maxAlternatives {
								return nil, fmt.Errorf("brace expansion exceeds %d alternatives", maxAlternatives)
							}
						}
					}
				}
				return out, nil
			}
		}
	}
	if depth != 0 {
		return nil, errors.New("unmatched {")
	}
	return []string{s}, nil
}

// splitTopLevel splits a brace body on commas that are not inside a nested
// brace.
func splitTopLevel(body string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, body[start:i])
				start = i + 1
			}
		}
	}
	// "{a}" (one alternative) is legal in doublestar and stays legal here.
	return append(parts, body[start:])
}
