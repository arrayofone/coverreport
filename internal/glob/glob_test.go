package glob

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pat  string
		path string
		want bool
	}{
		// ** crosses directories, including zero of them.
		{"src/**/*.ts", "src/a.ts", true},
		{"src/**/*.ts", "src/x/y/a.ts", true},
		{"src/**/*.ts", "lib/a.ts", false},
		{"**/*_templ.go", "a_templ.go", true},
		{"**/*_templ.go", "apps/landing/internal/web/x_templ.go", true},
		{"**/*_templ.go", "apps/landing/x_templ.go.bak", false},
		{"libs/go/pgtest/**", "libs/go/pgtest/pgtest.go", true},
		{"libs/go/pgtest/**", "libs/go/pgtest/sub/x.go", true},
		{"libs/go/pgtest/**", "libs/go/pgtestx/x.go", false},
		{"a/**/b/**/c.go", "a/b/c.go", true},
		{"a/**/b/**/c.go", "a/x/b/y/z/c.go", true},
		{"a/**/**/c.go", "a/c.go", true},
		// * stays inside one segment and matches dotfiles.
		{"libs/go/svc/*/main.go", "libs/go/svc/work/main.go", true},
		{"libs/go/svc/*/main.go", "libs/go/svc/work/sub/main.go", false},
		{"*.go", ".hidden.go", true},
		{"src/*", "src/a/b", false},
		// ? [..] and escapes.
		{"file?.go", "file1.go", true},
		{"file?.go", "file10.go", false},
		{"v[0-9].go", "v7.go", true},
		{"v[^0-9].go", "v7.go", false},
		{`lit\*.go`, "lit*.go", true},
		{`lit\*.go`, "litx.go", false},
		// Braces, nested, and containing "/".
		{"src/app/**/{page,layout}.tsx", "src/app/pay/[id]/page.tsx", true},
		{"src/app/**/{page,layout}.tsx", "src/app/layout.tsx", true},
		{"src/app/**/{page,layout}.tsx", "src/app/route.ts", false},
		{"src/{lib,app/api}/**", "src/app/api/x.ts", true},
		{"src/{lib,app/api}/**", "src/app/x.ts", false},
		{"{a,b{c,d}}.go", "bd.go", true},
		{"{a}.go", "a.go", true},
		// "**" inside a segment is two stars.
		{"a**b.go", "axxb.go", true},
		{"a**b.go", "a/b.go", false},
		// A leading ./ is tolerated.
		{"./src/*.ts", "src/a.ts", true},
	}
	for _, tc := range cases {
		p, err := Compile(tc.pat)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pat, err)
		}
		if got := p.Match(tc.path); got != tc.want {
			t.Errorf("%q.Match(%q) = %v, want %v", tc.pat, tc.path, got, tc.want)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct{ pat, want string }{
		{"", "empty"},
		{"/abs/*.go", "absolute"},
		{"dir/", "dir/**"},
		{"a b.go", "whitespace"},
		{"a//b", "empty path segment"},
		{"a/../b", `".."`},
		{"[a-.go", "bad segment"},
		{"{a,b", "unmatched {"},
		{"a,b}", "unmatched }"},
		{strings.Repeat("{a,b}", 9), "exceeds"},
	}
	for _, tc := range cases {
		_, err := Compile(tc.pat)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Compile(%q) = %v, want an error containing %q", tc.pat, err, tc.want)
		}
	}
}

func TestRootsAndLiteral(t *testing.T) {
	cases := []struct {
		pat     string
		roots   []string
		literal bool
	}{
		{"coverage-go/*.out", []string{"coverage-go"}, false},
		{"**/lcov.info", []string{"."}, false},
		{"coverage-web/**/lcov.info", []string{"coverage-web"}, false},
		{"{a,b}/x.out", []string{"a", "b"}, false},
		{"coverage-go/unit.out", []string{"coverage-go"}, true},
		{"unit.out", []string{"."}, true},
	}
	for _, tc := range cases {
		p := MustCompile(tc.pat)
		if got := p.Roots(); !reflect.DeepEqual(got, tc.roots) {
			t.Errorf("%q.Roots() = %v, want %v", tc.pat, got, tc.roots)
		}
		if got := p.Literal(); got != tc.literal {
			t.Errorf("%q.Literal() = %v, want %v", tc.pat, got, tc.literal)
		}
	}
}
