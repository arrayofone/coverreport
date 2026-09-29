// Package source reads the text of changed files from a checkout of the
// measured commit (render --source-root), so the surfaces can quote the
// lines that never ran. It is deliberately suspicious: excerpts land in a PR
// comment anyone with read access sees, and the checkout is whatever the PR
// put there, so a path that climbs out of the root, or a symlink the PR
// planted to point at a runner file, reads as "no source" rather than
// leaking that file into the comment.
package source

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaxFileBytes bounds what is read per file; bigger files (generated
// bundles, minified code) are treated as unavailable.
const MaxFileBytes = 4 << 20

// Root is a checkout. The zero value (and a nil *Root) has no source.
type Root struct {
	dir   string
	real  string
	cache map[string][]string
}

// Open returns a Root for dir, or nil when dir is "" (no source wanted). An
// unreadable dir is an error: asking for excerpts from a checkout that is
// not there is a CI mistake worth failing on.
func Open(dir string) (*Root, error) {
	if dir == "" {
		return nil, nil
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(real)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, &os.PathError{Op: "open", Path: dir, Err: os.ErrInvalid}
	}
	return &Root{dir: dir, real: abs, cache: map[string][]string{}}, nil
}

// Lines returns the file's lines (without terminators; index 0 is line 1),
// or nil when the file is unavailable: no root, a path that is not a clean
// repo-relative path, a path that resolves outside the root, a missing,
// oversized or binary file.
func (r *Root) Lines(rel string) []string {
	if r == nil {
		return nil
	}
	if ls, ok := r.cache[rel]; ok {
		return ls
	}
	ls := r.read(rel)
	r.cache[rel] = ls
	return ls
}

func (r *Root) read(rel string) []string {
	if rel == "" || strings.HasPrefix(rel, "/") || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil
	}
	full, err := filepath.EvalSymlinks(filepath.Join(r.real, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	inside, err := filepath.Rel(r.real, full)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return nil
	}
	st, err := os.Stat(full)
	if err != nil || !st.Mode().IsRegular() || st.Size() > MaxFileBytes {
		return nil
	}
	data, err := os.ReadFile(full)
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
