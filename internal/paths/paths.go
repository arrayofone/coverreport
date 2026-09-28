// Package paths turns the file names coverage tools write into clean
// repo-relative paths, which is the only kind of path the rest of coverreport
// (globs, floors, diffs) ever compares.
//
// Go coverprofiles name files by import path
// ("github.com/org/repo/libs/go/x/y.go"); the module that owns the longest
// matching prefix decides the directory. The module map comes from the
// config's go_modules and, underneath it, from go.work (each "use" directory's
// go.mod) or a root go.mod, so a repo with a conventional layout needs no
// config at all and a repo with an unusual one can say so explicitly.
//
// LCOV tracefiles name files either relative to the directory the tool ran
// in (flutter: "lib/api/edge.dart" inside apps/mobile) or absolutely
// (vitest/istanbul: the collector's own checkout path, which is NOT the
// reporter's checkout path when the two run in different CI jobs). Each LCOV
// layer declares the repo-relative directory its tool ran in (prefix) and
// optional absolute prefixes to strip.
package paths

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type module struct {
	path string // import path
	dir  string // repo-relative dir, "." for the root
}

// Resolver maps profile file names to repo-relative paths.
type Resolver struct {
	root string // absolute, slash-separated
	mods []module
}

// NewResolver builds a resolver for the repo at root. configured maps module
// import paths to repo-relative directories and wins over anything discovered
// from go.work / go.mod. Discovery problems are returned as warnings, not
// errors: they only matter if a Go path later fails to resolve, and that is
// reported on its own.
func NewResolver(root string, configured map[string]string) (*Resolver, []string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	r := &Resolver{root: filepath.ToSlash(abs)}
	seen := map[string]bool{}
	for mp, dir := range configured {
		clean, err := CleanRel(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("go_modules[%q]: %w", mp, err)
		}
		r.mods = append(r.mods, module{path: mp, dir: clean})
		seen[mp] = true
	}
	discovered, warnings := discoverModules(abs)
	for _, m := range discovered {
		if !seen[m.path] {
			r.mods = append(r.mods, m)
			seen[m.path] = true
		}
	}
	// Longest import path first, so ".../libs/go/sub" (its own module) beats
	// ".../libs/go".
	sort.Slice(r.mods, func(i, j int) bool {
		if len(r.mods[i].path) != len(r.mods[j].path) {
			return len(r.mods[i].path) > len(r.mods[j].path)
		}
		return r.mods[i].path < r.mods[j].path
	})
	return r, warnings, nil
}

// Root is the absolute repo root.
func (r *Resolver) Root() string { return r.root }

// Modules returns the resolved module map (import path -> repo dir), for the
// report's "how measured" section.
func (r *Resolver) Modules() map[string]string {
	out := map[string]string{}
	for _, m := range r.mods {
		out[m.path] = m.dir
	}
	return out
}

// GoFile resolves a coverprofile file name. Absolute paths (profiles from
// non-module builds, or covdata output of a binary built from a path) are
// accepted when they are under the repo root.
func (r *Resolver) GoFile(name string) (string, bool) {
	if strings.HasPrefix(name, "_/") {
		// GOPATH-less "local import" form: _/abs/path/file.go
		name = name[1:]
	}
	if strings.HasPrefix(name, "/") {
		return r.underRoot(name)
	}
	for _, m := range r.mods {
		if name == m.path {
			continue // a file name is never the module path itself
		}
		if strings.HasPrefix(name, m.path+"/") {
			return path.Join(m.dir, name[len(m.path)+1:]), true
		}
	}
	return "", false
}

func (r *Resolver) underRoot(abs string) (string, bool) {
	abs = path.Clean(abs)
	if abs == r.root {
		return "", false
	}
	if strings.HasPrefix(abs, r.root+"/") {
		return abs[len(r.root)+1:], true
	}
	return "", false
}

// LCOVFile resolves an SF path for a layer whose tool ran in prefix (a
// repo-relative directory, "" or "." for the root), trying in order:
//
//  1. relative: prefix joined with the path;
//  2. absolute under the reporter's own repo root;
//  3. absolute under one of strip (absolute prefixes: the collector's
//     checkout directory, e.g. "/home/runner/work/repo/repo");
//  4. absolute containing "/<prefix>/": the path from that point on. This is
//     what makes an artifact built in another job's checkout resolve without
//     knowing where that checkout was. It needs a non-empty prefix; the LAST
//     occurrence wins so "/work/apps/app/tmp/apps/app/src/x.ts" still maps to
//     the inner tree.
func (r *Resolver) LCOVFile(sf, prefix string, strip []string) (string, bool) {
	sf = filepath.ToSlash(sf)
	pfx := strings.Trim(path.Clean("/"+prefix), "/")
	if !strings.HasPrefix(sf, "/") {
		rel := path.Clean(path.Join(pfx, sf))
		if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
			return "", false
		}
		return rel, true
	}
	if rel, ok := r.underRoot(sf); ok {
		return rel, true
	}
	clean := path.Clean(sf)
	for _, s := range strip {
		s = strings.TrimSuffix(path.Clean(filepath.ToSlash(s)), "/")
		if strings.HasPrefix(clean, s+"/") {
			rest := clean[len(s)+1:]
			return rest, true
		}
	}
	if pfx != "" {
		if i := strings.LastIndex(clean, "/"+pfx+"/"); i >= 0 {
			return clean[i+1:], true
		}
	}
	return "", false
}

// CleanRel validates and cleans a repo-relative directory or file path.
func CleanRel(p string) (string, error) {
	p = filepath.ToSlash(p)
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%q is absolute; want a repo-relative path", p)
	}
	c := path.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%q leaves the repository", p)
	}
	return c, nil
}

// discoverModules reads go.work at root (every "use" directory's go.mod), or
// failing that a root go.mod.
func discoverModules(root string) ([]module, []string) {
	var warnings []string
	work, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err == nil {
		var mods []module
		for _, dir := range ParseGoWorkUse(work) {
			rel, err := CleanRel(dir)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("go.work: use %s: %v", dir, err))
				continue
			}
			mp, err := readModulePath(filepath.Join(root, filepath.FromSlash(rel), "go.mod"))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("go.work: use %s: %v", dir, err))
				continue
			}
			mods = append(mods, module{path: mp, dir: rel})
		}
		return mods, warnings
	}
	if mp, err := readModulePath(filepath.Join(root, "go.mod")); err == nil {
		return []module{{path: mp, dir: "."}}, warnings
	}
	return nil, warnings
}

// ParseGoWorkUse returns the directories of a go.work's use directives, both
// the single-line and the parenthesised block form, with // comments removed
// and quoted paths unquoted.
func ParseGoWorkUse(src []byte) []string {
	var out []string
	inBlock := false
	sc := bufio.NewScanner(bytes.NewReader(src))
	for sc.Scan() {
		line := stripComment(sc.Text())
		if line == "" {
			continue
		}
		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			out = append(out, unquote(line))
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "use" {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "use"))
		switch {
		case rest == "(":
			inBlock = true
		case strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")"):
			for _, d := range strings.Fields(strings.TrimSuffix(strings.TrimPrefix(rest, "("), ")")) {
				out = append(out, unquote(d))
			}
		case rest != "":
			out = append(out, unquote(rest))
		}
	}
	return out
}

func readModulePath(gomod string) (string, error) {
	src, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(bytes.NewReader(src))
	for sc.Scan() {
		line := stripComment(sc.Text())
		if rest, ok := strings.CutPrefix(line, "module"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '"') {
			if mp := unquote(strings.TrimSpace(rest)); mp != "" {
				return mp, nil
			}
		}
	}
	return "", fmt.Errorf("%s has no module directive", gomod)
}

func stripComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}
