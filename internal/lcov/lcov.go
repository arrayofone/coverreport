// Package lcov reads LCOV tracefiles: what vitest (istanbul), flutter test
// --coverage and node --test-reporter=lcov all emit.
//
// Recognised records: TN, SF, FN (both "FN:line,name" and lcov 2.x
// "FN:start,end,name"), FNDA, FNL/FNA (lcov 2.2's indexed function records),
// BRDA, DA, and end_of_record. The summary records LF/LH, BRF/BRH and FNF/FNH
// are read past and never trusted: totals are recomputed from the detail
// records, which is also what lcov --summary does, so a tracefile whose
// summary lines disagree with its details cannot skew a gate. Any other record
// (VER, LN, MCDC, ...) is ignored for forward compatibility.
//
// Merging is by file, then by line (DA hits summed), by branch identity
// (line, block, branch; taken summed, "-" is 0) and by function name (hits
// summed), which is lcov's own merge rule. A file split across several
// tracefiles, or repeated inside one (sharded vitest runs), is one file.
package lcov

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/DarrenBangsund/coverreport/internal/coverage"
)

// BranchKey identifies one branch outcome. Block and Branch are kept as the
// strings the tracefile wrote: lcov 2.x allows an "e" prefix on the block
// (exception branches) and an expression string as the branch.
type BranchKey struct {
	Line   int
	Block  string
	Branch string
}

// Func is one function's first-seen line and summed hit count.
type Func struct {
	Line int
	Hits int64
}

// File is one SF record's merged data.
type File struct {
	Lines    map[int]int64
	Branches map[BranchKey]int64
	Funcs    map[string]*Func
}

func newFile() *File {
	return &File{Lines: map[int]int64{}, Branches: map[BranchKey]int64{}, Funcs: map[string]*Func{}}
}

// Merge folds o into f.
func (f *File) Merge(o *File) {
	for l, h := range o.Lines {
		f.Lines[l] += h
	}
	for k, t := range o.Branches {
		f.Branches[k] += t
	}
	for name, fn := range o.Funcs {
		if cur := f.Funcs[name]; cur != nil {
			cur.Hits += fn.Hits
		} else {
			f.Funcs[name] = &Func{Line: fn.Line, Hits: fn.Hits}
		}
	}
}

// LineCount: DA records, covered when hits > 0.
func (f *File) LineCount() coverage.Count {
	var c coverage.Count
	for _, h := range f.Lines {
		c.Total++
		if h > 0 {
			c.Covered++
		}
	}
	return c
}

// BranchCount: BRDA records, covered when taken > 0.
func (f *File) BranchCount() coverage.Count {
	var c coverage.Count
	for _, t := range f.Branches {
		c.Total++
		if t > 0 {
			c.Covered++
		}
	}
	return c
}

// FuncCount: distinct function names, covered when hits > 0.
func (f *File) FuncCount() coverage.Count {
	var c coverage.Count
	for _, fn := range f.Funcs {
		c.Total++
		if fn.Hits > 0 {
			c.Covered++
		}
	}
	return c
}

// Set is the merged content of one or more tracefiles, keyed by SF path as
// written.
type Set struct {
	Files map[string]*File
}

// NewSet returns an empty set.
func NewSet() *Set { return &Set{Files: map[string]*File{}} }

// Names returns the SF paths in sorted order.
func (s *Set) Names() []string {
	out := make([]string, 0, len(s.Files))
	for n := range s.Files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Parse reads one tracefile into the set. name is used in error messages.
func (s *Set) Parse(r io.Reader, name string) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var (
		cur     *File
		curName string
		fnl     map[string]int // FNL index -> line, per record
		lineNo  int
		records int
	)
	flush := func() {
		if cur == nil {
			return
		}
		if prev := s.Files[curName]; prev != nil {
			prev.Merge(cur)
		} else {
			s.Files[curName] = cur
		}
		cur, curName, fnl = nil, "", nil
	}
	errf := func(format string, a ...any) error {
		return fmt.Errorf("%s:%d: %s", name, lineNo, fmt.Sprintf(format, a...))
	}
	for sc.Scan() {
		lineNo++
		line := string(bytes.TrimSpace(sc.Bytes()))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "end_of_record" {
			if cur == nil {
				return errf("end_of_record outside a record")
			}
			flush()
			records++
			continue
		}
		tag, val, ok := strings.Cut(line, ":")
		if !ok {
			return errf("malformed record %q", line)
		}
		switch tag {
		case "TN", "VER":
			continue
		case "SF":
			if cur != nil {
				// A missing end_of_record: lcov itself tolerates it, and a
				// truncated shard should still count what it has.
				flush()
				records++
			}
			if val == "" {
				return errf("SF with no path")
			}
			cur, curName, fnl = newFile(), val, map[string]int{}
			continue
		}
		if cur == nil {
			return errf("%s record outside an SF record", tag)
		}
		switch tag {
		case "DA":
			f := strings.Split(val, ",")
			if len(f) < 2 {
				return errf("malformed DA %q", val)
			}
			ln, err1 := strconv.Atoi(f[0])
			hits, err2 := strconv.ParseInt(f[1], 10, 64)
			if err1 != nil || err2 != nil || ln < 1 || hits < 0 {
				return errf("malformed DA %q", val)
			}
			cur.Lines[ln] += hits
		case "BRDA":
			f := strings.Split(val, ",")
			if len(f) < 4 {
				return errf("malformed BRDA %q", val)
			}
			ln, err := strconv.Atoi(f[0])
			if err != nil || ln < 1 {
				return errf("malformed BRDA %q", val)
			}
			takenS := f[len(f)-1]
			var taken int64
			if takenS != "-" {
				taken, err = strconv.ParseInt(takenS, 10, 64)
				if err != nil || taken < 0 {
					return errf("malformed BRDA %q", val)
				}
			}
			k := BranchKey{Line: ln, Block: f[1], Branch: strings.Join(f[2:len(f)-1], ",")}
			cur.Branches[k] += taken
		case "FN":
			// "FN:line,name" or lcov 2.x "FN:start,end,name". A name may
			// contain commas (C++ templates), so the end line is only
			// recognised when the second field is all digits AND a name
			// follows it.
			first, rest, ok := strings.Cut(val, ",")
			if !ok {
				return errf("malformed FN %q", val)
			}
			ln, err := strconv.Atoi(first)
			if err != nil || ln < 0 {
				return errf("malformed FN %q", val)
			}
			if second, name, ok := strings.Cut(rest, ","); ok && isDigits(second) && name != "" {
				rest = name
			}
			if rest == "" {
				return errf("FN with no name %q", val)
			}
			if cur.Funcs[rest] == nil {
				cur.Funcs[rest] = &Func{Line: ln}
			}
		case "FNDA":
			hitsS, fname, ok := strings.Cut(val, ",")
			if !ok || fname == "" {
				return errf("malformed FNDA %q", val)
			}
			hits, err := strconv.ParseInt(hitsS, 10, 64)
			if err != nil || hits < 0 {
				return errf("malformed FNDA %q", val)
			}
			fn := cur.Funcs[fname]
			if fn == nil {
				// FNDA before (or without) its FN: keep the hit, line unknown.
				fn = &Func{}
				cur.Funcs[fname] = fn
			}
			fn.Hits += hits
		case "FNL":
			// FNL:index,line[,endline]
			f := strings.Split(val, ",")
			if len(f) < 2 {
				return errf("malformed FNL %q", val)
			}
			ln, err := strconv.Atoi(f[1])
			if err != nil {
				return errf("malformed FNL %q", val)
			}
			fnl[f[0]] = ln
		case "FNA":
			// FNA:index,hits,name
			f := strings.SplitN(val, ",", 3)
			if len(f) < 3 || f[2] == "" {
				return errf("malformed FNA %q", val)
			}
			hits, err := strconv.ParseInt(f[1], 10, 64)
			if err != nil || hits < 0 {
				return errf("malformed FNA %q", val)
			}
			fn := cur.Funcs[f[2]]
			if fn == nil {
				fn = &Func{Line: fnl[f[0]]}
				cur.Funcs[f[2]] = fn
			}
			fn.Hits += hits
		default:
			// LF/LH, BRF/BRH, FNF/FNH (recomputed, never trusted) and any
			// record this version does not know.
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if cur != nil {
		flush()
		records++
	}
	if records == 0 {
		return fmt.Errorf("%s: no SF records; not an LCOV tracefile", name)
	}
	return nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
