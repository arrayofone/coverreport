package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLines(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "b.go"), "package b\r\n\r\nfunc F() {}\r\n")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := r.Lines("a/b.go")
	if strings.Join(got, "|") != "package b||func F() {}" {
		t.Errorf("lines %q", got)
	}
	if r.Lines("a/missing.go") != nil {
		t.Error("a missing file has lines")
	}
	var none *Root
	if none.Lines("a/b.go") != nil {
		t.Error("a nil root has lines")
	}
	if r, err := Open(""); r != nil || err != nil {
		t.Error(`Open("") must mean "no source", not an error`)
	}
	if _, err := Open(filepath.Join(dir, "nope")); err == nil {
		t.Error("a missing --source-root was accepted")
	}
}

// Excerpts land in a PR comment, and the checkout is whatever the PR put
// there: nothing outside the root may be read, whatever the path says.
func TestLinesNeverLeavesTheRoot(t *testing.T) {
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret"), "token=hunter2\n")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "ok.go"), "package ok\n")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "planted.go")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "ok.go"), filepath.Join(dir, "alias.go")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "bin.dat"), "a\x00b\n")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"planted.go", "sub/secret", "../" + filepath.Base(outside) + "/secret", "/etc/passwd", "./ok.go", "a/../ok.go", "", "bin.dat"} {
		if got := r.Lines(p); got != nil {
			t.Errorf("%q read %q", p, got)
		}
	}
	if got := r.Lines("alias.go"); len(got) != 1 {
		t.Errorf("a symlink inside the root should read: %q", got)
	}
}
