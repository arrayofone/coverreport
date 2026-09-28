package gocov

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func parseString(t *testing.T, profiles ...string) *Set {
	t.Helper()
	s := NewSet()
	for i, p := range profiles {
		if err := s.Parse(strings.NewReader(p), "p"+string(rune('0'+i))); err != nil {
			t.Fatalf("Parse: %v", err)
		}
	}
	return s
}

func total(s *Set) (int64, int64) {
	var c, t int64
	for _, f := range s.Files() {
		fc, ft := Statements(s.Blocks(f))
		c += fc
		t += ft
	}
	return c, t
}

// The expected numbers were computed independently of this package, with
// awk over the same lines (union by file+position, covered if any count >
// 0); see the task report. go-live-excerpt.out is five files cut from a
// real 1.5 MB handipay profile.
func TestRealGoLiveExcerpt(t *testing.T) {
	data, err := os.ReadFile("../../testdata/go/go-live-excerpt.out")
	if err != nil {
		t.Fatal(err)
	}
	s := parseString(t, string(data))
	want := map[string][2]int64{
		"github.com/handixyz/handipay/libs/go/accountsync/outcome.go":      {4, 4},
		"github.com/handixyz/handipay/libs/go/connectors/errors.go":        {2, 4},
		"github.com/handixyz/handipay/libs/go/consentcharge/seams.go":      {5, 6},
		"github.com/handixyz/handipay/libs/go/domain/constants/ceiling.go": {10, 10},
		"github.com/handixyz/handipay/libs/go/svc/webhook/router.go":       {6, 9},
	}
	if got := s.Files(); len(got) != len(want) {
		t.Fatalf("files = %v", got)
	}
	for f, w := range want {
		c, tot := Statements(s.Blocks(f))
		if c != w[0] || tot != w[1] {
			t.Errorf("%s: %d/%d, want %d/%d", f, c, tot, w[0], w[1])
		}
	}
	if c, tot := total(s); c != 27 || tot != 33 {
		t.Errorf("total %d/%d, want 27/33", c, tot)
	}
	if !s.Modes["atomic"] {
		t.Errorf("modes = %v", s.Modes)
	}
}

// -coverpkg=./... writes every block once per test binary: this excerpt of
// the landing module's real profile has 108 block lines for 17 blocks. A
// block that only some binaries ran is covered; one that no binary ran is
// not.
func TestRealCoverpkgDuplicatesMerge(t *testing.T) {
	data, err := os.ReadFile("../../testdata/go/landing-coverpkg-excerpt.out")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n") - 1; n != 108 {
		t.Fatalf("fixture has %d block lines, want 108", n)
	}
	s := parseString(t, string(data))
	blocks := 0
	for _, f := range s.Files() {
		blocks += len(s.Blocks(f))
	}
	if blocks != 17 {
		t.Errorf("distinct blocks = %d, want 17", blocks)
	}
	if c, tot := total(s); c != 23 || tot != 31 {
		t.Errorf("total %d/%d, want 23/31 (summing duplicates would give %d statements)", c, tot, 108)
	}
	icons := s.Blocks("github.com/handixyz/handipay/apps/landing/internal/web/icons.go")
	if c, tot := Statements(icons); c != 15 || tot != 22 {
		t.Errorf("icons.go %d/%d, want 15/22", c, tot)
	}
}

func TestMergeSemantics(t *testing.T) {
	a := "mode: count\nm/f.go:1.1,2.2 3 0\nm/f.go:3.1,4.2 2 5\n"
	b := "mode: count\nm/f.go:1.1,2.2 3 4\nm/f.go:3.1,4.2 2 1\nm/g.go:1.1,1.9 1 0\n"
	s := parseString(t, a, b)
	got := s.Blocks("m/f.go")
	want := []Block{{1, 1, 2, 2, 3, 4}, {3, 1, 4, 2, 2, 6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged blocks = %+v, want %+v", got, want)
	}
	if c, tot := total(s); c != 5 || tot != 6 {
		t.Errorf("total %d/%d, want 5/6", c, tot)
	}
	// Set mode: a block run by either profile is covered exactly once.
	s = parseString(t, "mode: set\nm/f.go:1.1,2.2 3 0\n", "mode: set\nm/f.go:1.1,2.2 3 1\n")
	if c, tot := total(s); c != 3 || tot != 3 {
		t.Errorf("set-mode union %d/%d, want 3/3", c, tot)
	}
	// Mixed modes are allowed: only "count > 0" is ever asked.
	s = parseString(t, "mode: set\nm/f.go:1.1,2.2 3 0\nmode: atomic\nm/f.go:1.1,2.2 3 9\n")
	if len(s.Modes) != 2 {
		t.Errorf("modes = %v", s.Modes)
	}
}

func TestMergeBlocksAcrossNames(t *testing.T) {
	got, err := MergeBlocks(
		[]Block{{1, 1, 2, 2, 1, 0}, {5, 1, 6, 2, 2, 0}},
		[]Block{{1, 1, 2, 2, 1, 3}},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []Block{{1, 1, 2, 2, 1, 3}, {5, 1, 6, 2, 2, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeBlocks = %+v, want %+v", got, want)
	}
	if _, err := MergeBlocks([]Block{{1, 1, 2, 2, 1, 0}}, []Block{{1, 1, 2, 2, 2, 0}}); err == nil {
		t.Error("inconsistent NumStmt across names: want an error")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "empty coverprofile"},
		{"m/f.go:1.1,2.2 1 1\n", "not a Go coverprofile"},
		{"mode: fancy\n", "unknown coverprofile mode"},
		{"mode: set\nm/f.go:1.1,2.2 1\n", "malformed"},
		{"mode: set\nm/f.go:1.1-2.2 1 1\n", "malformed position"},
		{"mode: set\nm/f.go:1,2.2 1 1\n", "malformed position"},
		{"mode: set\nm/f.go:3.1,2.2 1 1\n", "ends before it starts"},
		{"mode: set\nm/f.go:1.1,2.2 x 1\n", "statement count"},
		{"mode: set\nm/f.go:1.1,2.2 1 -1\n", "hit count"},
		{"mode: set\n1.1,2.2 1 1\n", "malformed"},
		{"mode: set\nm/f.go:1.1,2.2 1 1\nm/f.go:1.1,2.2 2 1\n", "inconsistent statement count"},
	}
	for _, tc := range cases {
		err := NewSet().Parse(strings.NewReader(tc.in), "in")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) = %v, want error containing %q", tc.in, err, tc.want)
		}
	}
	// The error names the input and line.
	err := NewSet().Parse(strings.NewReader("mode: set\nm/f.go:1.1,2.2 1 1\nbad\n"), "go-unit.out")
	if err == nil || !strings.HasPrefix(err.Error(), "go-unit.out:3:") {
		t.Errorf("error %v does not name go-unit.out:3", err)
	}
}

// A file name with a colon (rare, but legal on Linux) still parses: the
// position starts after the LAST colon.
func TestParseColonInName(t *testing.T) {
	s := parseString(t, "mode: set\nm/we:ird.go:1.1,2.2 1 1\n")
	if got := s.Files(); !reflect.DeepEqual(got, []string{"m/we:ird.go"}) {
		t.Errorf("files = %v", got)
	}
}

// The line model, block by block, on the shapes gofmt produces. Positions
// are real ones from the calc.go fixture's profile.
func TestLineMap(t *testing.T) {
	blocks := []Block{
		{10, 38, 11, 11, 1, 2}, // func Classify(...) {  /  if n < 0 {
		{11, 11, 13, 3, 1, 1},  // return "", ErrNegative  }
		{16, 2, 16, 9, 1, 1},   // switch {
		{17, 14, 18, 21, 1, 0}, // case n == 0: return "zero"   (never ran)
		{19, 14, 20, 22, 1, 1}, // case n < 10: return "small"
		{22, 2, 22, 21, 1, 0},  // return "large"                (never ran)
		{35, 19, 40, 2, 3, 0},  // func Unused() { ... }         (never ran)
		{50, 1, 52, 2, 0, 0},   // an empty body: no statements, no lines
	}
	src, err := os.ReadFile("../../testdata/repo/libs/go/calc/calc.go")
	if err != nil {
		t.Fatal(err)
	}
	got := LineMap(blocks, TrivialLines(src))
	want := map[int]bool{
		10: true, 11: true, // the if-line is covered by the block it closes
		12: true, // 13 is the branch's "}": no code, not coverable
		16: true,
		17: false, 18: false, // the untaken case, and only it
		19: true, 20: true,
		22: false,
		35: false, 36: false, 37: false, 39: false, // 38 is blank, 40 is "}"
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LineMap =\n %v\nwant\n %v", got, want)
	}
	// Without source, every spanned line of a counted block is coverable.
	noSrc := LineMap([]Block{{35, 19, 40, 2, 3, 0}}, nil)
	if len(noSrc) != 6 {
		t.Errorf("without source: %v, want lines 35-40", noSrc)
	}
}

func TestTrivialLines(t *testing.T) {
	src := "package x\n\n// doc\nfunc f() {\n\ta := g(\n\t\t1,\n\t)\n\t}, \n\treturn\n}\n"
	got := TrivialLines([]byte(src))
	want := map[int]bool{2: true, 3: true, 7: true, 8: true, 10: true, 11: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TrivialLines = %v, want %v", got, want)
	}
}
