// Package diff reads unified git diffs (git diff output) and extracts, per
// file, the line numbers on the NEW side that the change added or modified.
// Those are the lines patch coverage asks about.
//
// Recommended input, which is what the CLI's --diff-base runs:
//
//	git -c core.quotePath=false diff -U0 --no-color --no-ext-diff \
//	    --src-prefix=a/ --dst-prefix=b/ -M <base> <head>
//
// Any context size works (context lines are walked, not assumed absent); the
// explicit prefixes make the parse immune to a user's diff.noprefix or
// diff.mnemonicPrefix config (a hand-made diff without a/ b/ prefixes is read
// literally). Handled: added, modified, deleted, renamed and
// copied files (with or without content changes), mode-only changes, binary
// files (both "Binary files ... differ" and "GIT binary patch"), C-quoted
// paths, and "\ No newline at end of file". A combined diff (diff --cc, from
// git show of a merge) is refused: diff the merge against its first parent.
package diff

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Status of a file in the diff.
const (
	Added    = "added"
	Modified = "modified"
	Deleted  = "deleted"
	Renamed  = "renamed"
	Copied   = "copied"
)

// File is one file of the diff.
type File struct {
	// Path is the new-side path ("" for nothing; for a deletion it is the
	// old path so the file can still be named).
	Path    string
	OldPath string
	Status  string
	Binary  bool
	// Added lists the new-side line numbers the diff adds, sorted. Empty
	// for deletions, binary files and pure renames.
	Added []int
}

type parser struct {
	files []*File
	cur   *File
	// hunk state
	inHunk  bool
	newLine int
	oldLeft int
	newLeft int
	lineNo  int
}

// Parse reads a unified git diff.
func Parse(r io.Reader) ([]File, error) {
	p := &parser{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		p.lineNo++
		if err := p.line(sc.Text()); err != nil {
			return nil, fmt.Errorf("diff:%d: %w", p.lineNo, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	out := make([]File, 0, len(p.files))
	for _, f := range p.files {
		if f.Status == "" {
			f.Status = Modified
		}
		if f.Status == Deleted && f.Path == "" {
			f.Path = f.OldPath
		}
		sort.Ints(f.Added)
		out = append(out, *f)
	}
	return out, nil
}

func (p *parser) line(s string) error {
	if p.inHunk {
		if p.oldLeft > 0 || p.newLeft > 0 {
			return p.hunkLine(s)
		}
		p.inHunk = false
	}
	switch {
	case strings.HasPrefix(s, "diff --git "):
		f := &File{}
		p.files = append(p.files, f)
		p.cur = f
		if a, b, ok := splitGitHeader(s[len("diff --git "):]); ok {
			f.OldPath, f.Path = a, b
		}
	case strings.HasPrefix(s, "diff --cc ") || strings.HasPrefix(s, "diff --combined "):
		return fmt.Errorf("combined (merge) diffs are not supported; diff the merge commit against its first parent")
	case p.cur == nil:
		// Preamble (a commit message from git show / format-patch): skip.
	case strings.HasPrefix(s, "new file mode "):
		p.cur.Status = Added
	case strings.HasPrefix(s, "deleted file mode "):
		p.cur.Status = Deleted
	case strings.HasPrefix(s, "rename from "):
		p.cur.Status = Renamed
		p.cur.OldPath = unquote(s[len("rename from "):])
	case strings.HasPrefix(s, "rename to "):
		p.cur.Status = Renamed
		p.cur.Path = unquote(s[len("rename to "):])
	case strings.HasPrefix(s, "copy from "):
		p.cur.Status = Copied
		p.cur.OldPath = unquote(s[len("copy from "):])
	case strings.HasPrefix(s, "copy to "):
		p.cur.Status = Copied
		p.cur.Path = unquote(s[len("copy to "):])
	case strings.HasPrefix(s, "--- "):
		if name := side(s[4:], "a/"); name != "" {
			p.cur.OldPath = name
		} else {
			p.cur.Status = Added
		}
	case strings.HasPrefix(s, "+++ "):
		if name := side(s[4:], "b/"); name != "" {
			p.cur.Path = name
		} else {
			p.cur.Status = Deleted
			p.cur.Path = ""
		}
	case strings.HasPrefix(s, "Binary files ") || s == "GIT binary patch":
		p.cur.Binary = true
	case strings.HasPrefix(s, "@@ "):
		return p.hunkHeader(s)
	case s != "" && strings.IndexByte("+- ", s[0]) >= 0:
		// A content line with no hunk to belong to: the previous hunk's
		// header declared fewer lines than it had. Ignoring it would drop an
		// added line from patch coverage without a word.
		return fmt.Errorf("content line outside a hunk (a hunk header's line counts are wrong): %q", s)
	}
	return nil
}

// hunkHeader parses "@@ -a[,b] +c[,d] @@ optional section heading".
func (p *parser) hunkHeader(s string) error {
	end := strings.Index(s[3:], " @@")
	if end < 0 {
		return fmt.Errorf("malformed hunk header %q", s)
	}
	fields := strings.Fields(s[3 : 3+end])
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "-") || !strings.HasPrefix(fields[1], "+") {
		return fmt.Errorf("malformed hunk header %q", s)
	}
	_, oldCount, err := rangeOf(fields[0][1:])
	if err != nil {
		return fmt.Errorf("malformed hunk header %q", s)
	}
	newStart, newCount, err := rangeOf(fields[1][1:])
	if err != nil {
		return fmt.Errorf("malformed hunk header %q", s)
	}
	p.inHunk = true
	p.newLine = newStart
	p.oldLeft = oldCount
	p.newLeft = newCount
	return nil
}

func rangeOf(s string) (start, count int, err error) {
	a, b, ok := strings.Cut(s, ",")
	if start, err = strconv.Atoi(a); err != nil {
		return 0, 0, err
	}
	count = 1
	if ok {
		if count, err = strconv.Atoi(b); err != nil {
			return 0, 0, err
		}
	}
	return start, count, nil
}

func (p *parser) hunkLine(s string) error {
	if s == "" {
		// Some tools strip the single space of an empty context line.
		s = " "
	}
	switch s[0] {
	case '+':
		p.cur.Added = append(p.cur.Added, p.newLine)
		p.newLine++
		p.newLeft--
	case '-':
		p.oldLeft--
	case ' ':
		p.newLine++
		p.newLeft--
		p.oldLeft--
	case '\\':
		// "\ No newline at end of file"
	default:
		return fmt.Errorf("unexpected line inside a hunk: %q", s)
	}
	if p.oldLeft < 0 || p.newLeft < 0 {
		return fmt.Errorf("hunk has more lines than its header declares")
	}
	return nil
}

// side returns the path of a ---/+++ line with its prefix removed, or "" for
// /dev/null. A trailing tab-separated timestamp (non-git diffs) is dropped.
func side(s, prefix string) string {
	if i := strings.IndexByte(s, '\t'); i >= 0 && !strings.HasPrefix(s, `"`) {
		s = s[:i]
	}
	s = unquote(s)
	if s == "/dev/null" {
		return ""
	}
	// Only the a/ b/ prefixes are stripped. Guessing at mnemonic prefixes
	// (c/ i/ o/ w/) would corrupt a --no-prefix diff of a real "w/" directory;
	// --diff-base always asks git for a/ and b/ explicitly.
	return strings.TrimPrefix(s, prefix)
}

// splitGitHeader splits the "a/x b/y" of a "diff --git" line. With quoting
// or with paths containing " b/" it can be ambiguous; the ---/+++ and
// rename lines that follow override it whenever they exist, so this only
// decides the path of a mode-only or binary change.
func splitGitHeader(s string) (string, string, bool) {
	if strings.HasPrefix(s, `"`) {
		a, rest, ok := cutQuoted(s)
		if !ok {
			return "", "", false
		}
		b := strings.TrimSpace(rest)
		return strip(a, "a/"), strip(unquote(b), "b/"), true
	}
	if i := strings.LastIndex(s, ` "`); i >= 0 && strings.HasSuffix(s, `"`) {
		return strip(s[:i], "a/"), strip(unquote(s[i+1:]), "b/"), true
	}
	// Unquoted on both sides: for an unrenamed file the halves are equal,
	// so split in the middle.
	if len(s)%2 == 1 {
		mid := len(s) / 2
		if s[mid] == ' ' {
			a, b := s[:mid], s[mid+1:]
			if strip(a, "a/") == strip(b, "b/") {
				return strip(a, "a/"), strip(b, "b/"), true
			}
		}
	}
	if i := strings.Index(s, " b/"); i >= 0 {
		return strip(s[:i], "a/"), strip(s[i+1:], "b/"), true
	}
	return "", "", false
}

func strip(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

func cutQuoted(s string) (string, string, bool) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			u, err := strconv.Unquote(s[:i+1])
			if err != nil {
				return "", "", false
			}
			return u, s[i+1:], true
		}
	}
	return "", "", false
}

// unquote undoes git's C-style path quoting ("t\303\251st.go"). Go's string
// literal syntax is a superset of what git emits (octal byte escapes, \t, \n,
// \", \\), so strconv.Unquote decodes it exactly.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}
