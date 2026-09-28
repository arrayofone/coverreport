package exclude

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `# header comment

**/*_templ.go  # templ codegen, 5,174 stmts
libs/go/svc/*/main.go	# tab-separated is fine too
  apps/mobile/lib/**/*.g.dart   #   drift codegen
`
	rules, err := Parse(strings.NewReader(in), "exclude.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		glob, reason string
		line         int
	}{
		{"**/*_templ.go", "templ codegen, 5,174 stmts", 3},
		{"libs/go/svc/*/main.go", "tab-separated is fine too", 4},
		{"apps/mobile/lib/**/*.g.dart", "drift codegen", 5},
	}
	if len(rules) != len(want) {
		t.Fatalf("rules = %+v", rules)
	}
	for i, w := range want {
		r := rules[i]
		if r.Glob != w.glob || r.Reason != w.reason || r.Line != w.line {
			t.Errorf("rule %d = %q %q %d, want %q %q %d", i, r.Glob, r.Reason, r.Line, w.glob, w.reason, w.line)
		}
	}
	if First(rules, "apps/landing/x_templ.go") != 0 || First(rules, "libs/go/svc/work/main.go") != 1 || First(rules, "libs/go/x.go") != -1 {
		t.Error("First picked the wrong rule")
	}
}

// A file matched by several rules is charged to the first, so the sizes
// printed per rule never double count.
func TestFirstMatchWins(t *testing.T) {
	rules, err := Parse(strings.NewReader("apps/**  # everything\napps/a.go  # also this\n"), "x")
	if err != nil {
		t.Fatal(err)
	}
	if got := First(rules, "apps/a.go"); got != 0 {
		t.Errorf("First = %d, want 0", got)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**/*_templ.go\n", "has no reason"},
		{"**/*_templ.go  #\n", "has no reason"},
		{"**/*_templ.go  #   \n", "has no reason"},
		{"**/*_templ.go# codegen\n", "separate the glob"},
		{"two globs.go  # x\n", "exactly one glob"},
		{"  # reason with no glob\n", ""}, // a full-line comment after trimming: fine
		{"/abs/*.go  # x\n", "absolute"},
		{"a.go  # x\na.go  # y\n", "already listed on line 1"},
	}
	for _, tc := range cases {
		_, err := Parse(strings.NewReader(tc.in), "exclude.txt")
		if tc.want == "" {
			if err != nil {
				t.Errorf("Parse(%q) = %v, want no error", tc.in, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "exclude.txt:") {
			t.Errorf("Parse(%q) = %v, want an error containing %q naming exclude.txt:<line>", tc.in, err, tc.want)
		}
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	rules, err := Load(filepath.Join(t.TempDir(), "exclude.txt"))
	if err != nil || rules != nil {
		t.Errorf("Load(missing) = %v, %v", rules, err)
	}
}
