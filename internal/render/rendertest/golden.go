package rendertest

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Update rewrites golden files instead of comparing (go test ./... -update).
var Update = flag.Bool("update", false, "rewrite testdata/render goldens from the current output")

// Golden compares got with testdata/render/<rel>, or rewrites it under
// -update. A golden diff is a behaviour change: review it, then -update.
func Golden(t testing.TB, rel string, got []byte) {
	t.Helper()
	path := filepath.Join(Testdata(), "render", filepath.FromSlash(rel))
	if *Update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./... -update to create it)", err)
	}
	if bytes.Equal(got, want) {
		return
	}
	gl, wl := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(gl) || i < len(wl); i++ {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Fatalf("%s differs at line %d:\n got: %s\nwant: %s\n(a golden diff is a behaviour change: review it, then -update)", rel, i+1, g, w)
		}
	}
}
