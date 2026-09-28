// Package gocov reads Go coverprofiles (the -coverprofile output of go test
// and of go tool covdata textfmt) in any of the three modes, and merges many
// of them into one block set.
//
// The merge is the part that matters. -coverpkg=./... makes every test binary
// instrument every package in the module, so each package's blocks appear
// once per test binary: the landing module's profile has 25,017 block lines for
// 3,823 distinct blocks. A block is identified by its file and exact
// start/end position; duplicates are one block, and it is covered when ANY
// profile ran it (counts are summed, so atomic/count modes keep a meaningful
// hit count and set mode stays 0/1 in effect). That is the same rule
// go tool cover applies when it merges repeated blocks.
package gocov

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Block is one basic block of a coverprofile line:
// name.go:StartLine.StartCol,EndLine.EndCol NumStmt Count
type Block struct {
	StartLine, StartCol int
	EndLine, EndCol     int
	NumStmt             int
	Count               int64
}

type blockKey struct{ sl, sc, el, ec int }

type fileBlocks struct {
	blocks map[blockKey]*Block
	// source remembers which input first contributed each block, so an
	// inconsistent NumStmt names both profiles.
	source map[blockKey]string
}

// Set is the merged content of one or more coverprofiles, keyed by the file
// name as written in the profile (an import path + file name, usually).
type Set struct {
	Modes map[string]bool
	files map[string]*fileBlocks
}

// NewSet returns an empty set.
func NewSet() *Set {
	return &Set{Modes: map[string]bool{}, files: map[string]*fileBlocks{}}
}

// Parse reads one coverprofile into the set. name is used in error messages.
// "mode:" lines may appear anywhere (concatenated profiles are common and
// harmless); mixing modes is allowed because the only question asked of a
// count is whether it is > 0.
func (s *Set) Parse(r io.Reader, name string) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	sawMode := false
	for sc.Scan() {
		lineNo++
		line := bytes.TrimRight(sc.Bytes(), "\r")
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte("mode:")) {
			mode := strings.TrimSpace(string(line[len("mode:"):]))
			switch mode {
			case "set", "count", "atomic":
			default:
				return fmt.Errorf("%s:%d: unknown coverprofile mode %q", name, lineNo, mode)
			}
			s.Modes[mode] = true
			sawMode = true
			continue
		}
		if !sawMode {
			return fmt.Errorf("%s:%d: not a Go coverprofile (first line must be \"mode: set|count|atomic\")", name, lineNo)
		}
		file, b, err := parseLine(line)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", name, lineNo, err)
		}
		if err := s.add(file, b, name); err != nil {
			return fmt.Errorf("%s:%d: %w", name, lineNo, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if !sawMode {
		return fmt.Errorf("%s: empty coverprofile (no mode line)", name)
	}
	return nil
}

func (s *Set) add(file string, b Block, source string) error {
	fb := s.files[file]
	if fb == nil {
		fb = &fileBlocks{blocks: map[blockKey]*Block{}, source: map[blockKey]string{}}
		s.files[file] = fb
	}
	k := blockKey{b.StartLine, b.StartCol, b.EndLine, b.EndCol}
	if prev := fb.blocks[k]; prev != nil {
		if prev.NumStmt != b.NumStmt {
			return fmt.Errorf("inconsistent statement count for %s:%d.%d,%d.%d: %d (from %s) vs %d; the profiles were built from different sources",
				file, b.StartLine, b.StartCol, b.EndLine, b.EndCol, prev.NumStmt, fb.source[k], b.NumStmt)
		}
		prev.Count += b.Count
		return nil
	}
	nb := b
	fb.blocks[k] = &nb
	fb.source[k] = source
	return nil
}

// parseLine parses "file:sl.sc,el.ec n count". The file name is everything
// before the LAST colon, so a path containing ":" still parses.
func parseLine(line []byte) (string, Block, error) {
	var b Block
	s := string(line)
	sp2 := strings.LastIndexByte(s, ' ')
	if sp2 < 0 {
		return "", b, fmt.Errorf("malformed line %q", s)
	}
	sp1 := strings.LastIndexByte(s[:sp2], ' ')
	if sp1 < 0 {
		return "", b, fmt.Errorf("malformed line %q", s)
	}
	colon := strings.LastIndexByte(s[:sp1], ':')
	if colon <= 0 {
		return "", b, fmt.Errorf("malformed line %q (no file name)", s)
	}
	file := s[:colon]
	pos := s[colon+1 : sp1]
	var err error
	if b.NumStmt, err = strconv.Atoi(s[sp1+1 : sp2]); err != nil || b.NumStmt < 0 {
		return "", b, fmt.Errorf("malformed statement count in %q", s)
	}
	if b.Count, err = strconv.ParseInt(s[sp2+1:], 10, 64); err != nil || b.Count < 0 {
		return "", b, fmt.Errorf("malformed hit count in %q", s)
	}
	start, end, ok := strings.Cut(pos, ",")
	if !ok {
		return "", b, fmt.Errorf("malformed position %q", pos)
	}
	if b.StartLine, b.StartCol, err = lineCol(start); err != nil {
		return "", b, fmt.Errorf("malformed position %q", pos)
	}
	if b.EndLine, b.EndCol, err = lineCol(end); err != nil {
		return "", b, fmt.Errorf("malformed position %q", pos)
	}
	if b.StartLine < 1 || b.EndLine < b.StartLine {
		return "", b, fmt.Errorf("block %q ends before it starts", pos)
	}
	return file, b, nil
}

func lineCol(s string) (int, int, error) {
	l, c, ok := strings.Cut(s, ".")
	if !ok {
		return 0, 0, fmt.Errorf("no column")
	}
	ln, err := strconv.Atoi(l)
	if err != nil {
		return 0, 0, err
	}
	col, err := strconv.Atoi(c)
	if err != nil {
		return 0, 0, err
	}
	return ln, col, nil
}

// Files returns the profile file names in sorted order.
func (s *Set) Files() []string {
	out := make([]string, 0, len(s.files))
	for f := range s.files {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Blocks returns a file's merged blocks sorted by position.
func (s *Set) Blocks(file string) []Block {
	fb := s.files[file]
	if fb == nil {
		return nil
	}
	out := make([]Block, 0, len(fb.blocks))
	for _, b := range fb.blocks {
		out = append(out, *b)
	}
	SortBlocks(out)
	return out
}

// SortBlocks orders blocks by start then end position.
func SortBlocks(bs []Block) {
	sort.Slice(bs, func(i, j int) bool {
		a, b := bs[i], bs[j]
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		if a.StartCol != b.StartCol {
			return a.StartCol < b.StartCol
		}
		if a.EndLine != b.EndLine {
			return a.EndLine < b.EndLine
		}
		return a.EndCol < b.EndCol
	})
}

// MergeBlocks unions block lists that belong to the same source file but
// reached it under different profile names (two import paths resolving to one
// directory). Same rule as Parse: same position is one block, counts summed.
func MergeBlocks(lists ...[]Block) ([]Block, error) {
	idx := map[blockKey]int{}
	var out []Block
	for _, l := range lists {
		for _, b := range l {
			k := blockKey{b.StartLine, b.StartCol, b.EndLine, b.EndCol}
			if i, ok := idx[k]; ok {
				if out[i].NumStmt != b.NumStmt {
					return nil, fmt.Errorf("inconsistent statement count at %d.%d,%d.%d", b.StartLine, b.StartCol, b.EndLine, b.EndCol)
				}
				out[i].Count += b.Count
				continue
			}
			idx[k] = len(out)
			out = append(out, b)
		}
	}
	SortBlocks(out)
	return out, nil
}

// Statements totals a file's statements: every block's NumStmt, covered when
// the block's count is > 0. This is exactly what go test prints as
// "coverage: N% of statements".
func Statements(bs []Block) (covered, total int64) {
	for _, b := range bs {
		total += int64(b.NumStmt)
		if b.Count > 0 {
			covered += int64(b.NumStmt)
		}
	}
	return covered, total
}

// LineMap derives per-line coverage from blocks, for patch coverage and the
// uncovered-line ranges. A coverprofile has no line data, so this is a model,
// and it is spelled out here because patch numbers depend on it:
//
//   - Only blocks with at least one statement count. A zero-statement block
//     (an empty function body) makes no line coverable.
//   - Every line a block spans, first to last, is coverable.
//   - A line is covered when ANY block touching it ran. gofmt puts the
//     boundary between two blocks on a shared line (the "if cond {" line
//     ends the parent block and starts the body; "}" ends one block and
//     starts the next), so "all blocks must have run" would mark the
//     if-line of every untaken branch uncovered, and a closing brace after
//     every early return. "Any" marks exactly the body lines of the untaken
//     branch. The cost is a one-line "if x { return }", which gofmt never
//     produces.
//   - trivial, when non-nil, lists lines that carry no code (blank, "//"
//     comment only, closing brackets only; see TrivialLines). They are
//     dropped, so a blank line inside an uncovered function is not an
//     "uncovered changed line".
func LineMap(bs []Block, trivial map[int]bool) map[int]bool {
	lines := map[int]bool{}
	for _, b := range bs {
		if b.NumStmt == 0 {
			continue
		}
		ran := b.Count > 0
		for l := b.StartLine; l <= b.EndLine; l++ {
			if trivial[l] {
				continue
			}
			if ran {
				lines[l] = true
			} else if _, seen := lines[l]; !seen {
				lines[l] = false
			}
		}
	}
	return lines
}

// TrivialLines returns the 1-based lines of a Go source file that carry no
// code: blank, a "//" comment and nothing else, or only closing brackets and
// separators ("}", "})", "},", ")"). Block comments are deliberately not
// tracked: SQL in raw strings is full of "/*", and a line inside a raw string
// is part of the statement on its first line anyway.
func TrivialLines(src []byte) map[int]bool {
	out := map[int]bool{}
	for i, line := range bytes.Split(src, []byte("\n")) {
		t := bytes.TrimSpace(line)
		if len(t) == 0 || bytes.HasPrefix(t, []byte("//")) || onlyClosers(t) {
			out[i+1] = true
		}
	}
	return out
}

func onlyClosers(t []byte) bool {
	for _, c := range t {
		switch c {
		case '}', ')', ']', ',', ';':
		default:
			return false
		}
	}
	return true
}
