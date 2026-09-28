package paths

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGoFileDiscoversGoWork(t *testing.T) {
	// testdata/repo has a go.work using ./libs/go, whose go.mod says
	// module example.com/demo/libs/go. No config needed.
	r, warns, err := NewResolver("../../testdata/repo", nil)
	if err != nil || len(warns) > 0 {
		t.Fatalf("NewResolver: %v %v", err, warns)
	}
	got, ok := r.GoFile("example.com/demo/libs/go/calc/calc.go")
	if !ok || got != "libs/go/calc/calc.go" {
		t.Errorf("GoFile = %q, %v", got, ok)
	}
	if _, ok := r.GoFile("example.com/other/x.go"); ok {
		t.Error("a path outside every module resolved")
	}
	// A prefix that is not a path-segment boundary must not match.
	if _, ok := r.GoFile("example.com/demo/libs/gofer/x.go"); ok {
		t.Error("example.com/demo/libs/gofer matched module example.com/demo/libs/go")
	}
}

func TestGoFileConfiguredWinsAndLongestPrefix(t *testing.T) {
	r, _, err := NewResolver(t.TempDir(), map[string]string{
		"github.com/o/r/libs/go":     "libs/go",
		"github.com/o/r/libs/go/sub": "vendor/sub", // its own module, nested
		"github.com/o/r":             ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"github.com/o/r/libs/go/x/y.go":   "libs/go/x/y.go",
		"github.com/o/r/libs/go/sub/z.go": "vendor/sub/z.go",
		"github.com/o/r/cmd/main.go":      "cmd/main.go",
	}
	for in, want := range cases {
		if got, ok := r.GoFile(in); !ok || got != want {
			t.Errorf("GoFile(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestGoFileAbsolute(t *testing.T) {
	root := t.TempDir()
	r, _, _ := NewResolver(root, nil)
	abs := filepath.ToSlash(filepath.Join(root, "pkg", "a.go"))
	if got, ok := r.GoFile(abs); !ok || got != "pkg/a.go" {
		t.Errorf("GoFile(abs) = %q, %v", got, ok)
	}
	if got, ok := r.GoFile("_" + abs); !ok || got != "pkg/a.go" {
		t.Errorf("GoFile(_abs) = %q, %v", got, ok)
	}
	if _, ok := r.GoFile("/elsewhere/a.go"); ok {
		t.Error("an absolute path outside the root resolved")
	}
}

func TestGoModFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("// c\nmodule \"example.org/solo\" // trailing\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _, _ := NewResolver(root, nil)
	if got, ok := r.GoFile("example.org/solo/internal/x.go"); !ok || got != "internal/x.go" {
		t.Errorf("GoFile = %q, %v", got, ok)
	}
}

func TestGoWorkBrokenUseIsAWarning(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.25\nuse ./missing\n"), 0o644)
	_, warns, err := NewResolver(root, nil)
	if err != nil || len(warns) != 1 || !strings.Contains(warns[0], "use ./missing") {
		t.Errorf("err %v, warnings %v", err, warns)
	}
}

func TestParseGoWorkUse(t *testing.T) {
	src := `go 1.25.8

// a comment
use ./single
use (
	./apps/landing // trailing comment
	"./quoted dir"

	./libs/go
)
use (./a ./b)
toolchain go1.26
`
	want := []string{"./single", "./apps/landing", "./quoted dir", "./libs/go", "./a", "./b"}
	if got := ParseGoWorkUse([]byte(src)); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseGoWorkUse = %q, want %q", got, want)
	}
}

func TestLCOVFile(t *testing.T) {
	root := t.TempDir()
	r, _, _ := NewResolver(root, nil)
	rootSlash := filepath.ToSlash(root)
	cases := []struct {
		name, sf, prefix string
		strip            []string
		want             string
		ok               bool
	}{
		{"flutter: relative to the project dir", "lib/api/edge.dart", "apps/mobile", nil, "apps/mobile/lib/api/edge.dart", true},
		{"root-relative, no prefix", "apps/app/src/a.ts", "", nil, "apps/app/src/a.ts", true},
		{"relative with ./ and ..", "./lib/../lib/x.dart", "apps/mobile", nil, "apps/mobile/lib/x.dart", true},
		{"relative, up to the repo root", "../../etc/x", "apps/mobile", nil, "etc/x", true},
		{"relative escaping the repo", "../../../etc/passwd", "apps/mobile", nil, "", false},
		{"absolute under the reporter's root", rootSlash + "/apps/app/src/a.ts", "apps/app", nil, "apps/app/src/a.ts", true},
		{"absolute under a strip prefix", "/home/runner/work/r/r/apps/app/src/a.ts", "", []string{"/home/runner/work/r/r/"}, "apps/app/src/a.ts", true},
		{"absolute from another checkout, anchored on the prefix", "/tmp/cov-x/apps/app/src/a.ts", "apps/app", nil, "apps/app/src/a.ts", true},
		{"anchor takes the last occurrence", "/w/apps/app/tmp/apps/app/src/a.ts", "apps/app", nil, "apps/app/src/a.ts", true},
		{"absolute with no anchor", "/opt/flutter/lib/x.dart", "apps/mobile", nil, "", false},
		{"absolute, no prefix to anchor on", "/tmp/x/src/a.ts", "", nil, "", false},
	}
	for _, tc := range cases {
		got, ok := r.LCOVFile(tc.sf, tc.prefix, tc.strip)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: LCOVFile(%q, %q) = %q, %v; want %q, %v", tc.name, tc.sf, tc.prefix, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCleanRel(t *testing.T) {
	for in, want := range map[string]string{"a/b/": "a/b", "./a": "a", ".": "."} {
		if got, err := CleanRel(in); err != nil || got != want {
			t.Errorf("CleanRel(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"/abs", "..", "../x", "a/../../x"} {
		if _, err := CleanRel(bad); err == nil {
			t.Errorf("CleanRel(%q): want an error", bad)
		}
	}
}
