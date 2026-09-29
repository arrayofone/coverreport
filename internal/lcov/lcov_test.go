package lcov

import (
	"os"
	"strings"
	"testing"

	"github.com/arrayofone/coverreport/internal/coverage"
)

func parse(t *testing.T, inputs ...string) *Set {
	t.Helper()
	s := NewSet()
	for _, in := range inputs {
		if err := s.Parse(strings.NewReader(in), "in"); err != nil {
			t.Fatalf("Parse: %v", err)
		}
	}
	return s
}

// Four files cut from handipay's real flutter tracefile. lcov --summary on
// the same excerpt reports "source files: 4, lines: 96.7% (58 of 60
// lines)"; the reporter must say exactly the same.
func TestRealMobileExcerptMatchesLcovSummary(t *testing.T) {
	data, err := os.ReadFile("../../testdata/lcov/mobile-excerpt.info")
	if err != nil {
		t.Fatal(err)
	}
	s := parse(t, string(data))
	if len(s.Files) != 4 {
		t.Fatalf("files = %v", s.Names())
	}
	var lines coverage.Count
	for _, f := range s.Files {
		lines = lines.Add(f.LineCount())
	}
	if lines != (coverage.Count{Covered: 58, Total: 60}) {
		t.Errorf("lines = %+v, want 58/60", lines)
	}
	edge := s.Files["lib/api/edge.dart"]
	if edge == nil || edge.Lines[27] != 87 {
		t.Errorf("lib/api/edge.dart DA:27 = %v, want 87 hits", edge)
	}
}

// The istanbul (vitest) shape: TN, FN/FNDA, BRDA with "-", summary lines
// that lie (they must be ignored), and a second shard of the same file.
func TestIstanbulShapeAndShardMerge(t *testing.T) {
	shard1 := `TN:
SF:/w/apps/app/src/lib/money.ts
FN:1,format
FN:5,parse
FNDA:3,format
FNDA:0,parse
FNF:2
FNH:2
DA:1,3
DA:2,3
DA:5,0
LF:99
LH:99
BRDA:2,0,0,2
BRDA:2,0,1,-
BRF:9
BRH:9
end_of_record
`
	// Shard 2 ran line 1 twice more and never ran line 2: merging must sum
	// (1 has 5 hits) and must not let shard 2's zero erase shard 1's hits.
	shard2 := `SF:/w/apps/app/src/lib/money.ts
FN:5,parse
FNDA:1,parse
DA:1,2
DA:2,0
DA:5,4
BRDA:2,0,0,0
BRDA:2,0,1,1
end_of_record
`
	s := parse(t, shard1)
	f := s.Files["/w/apps/app/src/lib/money.ts"]
	if got := f.LineCount(); got != (coverage.Count{Covered: 2, Total: 3}) {
		t.Errorf("lines = %+v, want 2/3 (LF/LH are never trusted)", got)
	}
	if got := f.BranchCount(); got != (coverage.Count{Covered: 1, Total: 2}) {
		t.Errorf("branches = %+v, want 1/2 (\"-\" is not taken)", got)
	}
	if got := f.FuncCount(); got != (coverage.Count{Covered: 1, Total: 2}) {
		t.Errorf("functions = %+v, want 1/2", got)
	}
	s = parse(t, shard1, shard2)
	f = s.Files["/w/apps/app/src/lib/money.ts"]
	if got := f.LineCount(); got != (coverage.Count{Covered: 3, Total: 3}) {
		t.Errorf("merged lines = %+v, want 3/3", got)
	}
	if got := f.BranchCount(); got != (coverage.Count{Covered: 2, Total: 2}) {
		t.Errorf("merged branches = %+v, want 2/2", got)
	}
	if got := f.FuncCount(); got != (coverage.Count{Covered: 2, Total: 2}) {
		t.Errorf("merged functions = %+v, want 2/2", got)
	}
	if f.Lines[1] != 5 || f.Lines[2] != 3 || f.Lines[5] != 4 || f.Branches[BranchKey{2, "0", "0"}] != 2 ||
		f.Funcs["format"].Hits != 3 || f.Funcs["parse"].Hits != 1 || f.Funcs["parse"].Line != 5 {
		t.Errorf("merged detail wrong: %+v %+v", f.Lines, f.Funcs)
	}
}

func TestFunctionRecordVariants(t *testing.T) {
	s := parse(t, `SF:a.c
FN:10,20,with_end
FN:30,tmpl<a, b>
FN:40,50
FNDA:1,with_end
FNDA:2,tmpl<a, b>
FNL:0,60,70
FNA:0,5,indexed
FNDA:1,orphan
end_of_record
`)
	f := s.Files["a.c"]
	want := map[string]Func{
		"with_end":   {Line: 10, Hits: 1},
		"tmpl<a, b>": {Line: 30, Hits: 2},
		"50":         {Line: 40, Hits: 0}, // "FN:40,50" is line 40, name "50"
		"indexed":    {Line: 60, Hits: 5}, // lcov 2.2 FNL/FNA
		"orphan":     {Line: 0, Hits: 1},  // FNDA without FN keeps its hit
	}
	if len(f.Funcs) != len(want) {
		t.Fatalf("funcs = %v", f.Funcs)
	}
	for name, w := range want {
		if got := f.Funcs[name]; got == nil || *got != w {
			t.Errorf("func %q = %+v, want %+v", name, got, w)
		}
	}
}

func TestLenientShapes(t *testing.T) {
	// CRLF line endings, comments, unknown records, a missing final
	// end_of_record, and a missing end_of_record between two files.
	s := parse(t, "SF:a.dart\r\nVER:2\r\n# comment\r\nDA:1,1\r\nMCDC:1,2,t,1,1,x\r\nSF:b.dart\r\nDA:3,0\r\n")
	if len(s.Files) != 2 || s.Files["a.dart"].Lines[1] != 1 || s.Files["b.dart"].LineCount().Total != 1 {
		t.Errorf("files = %v", s.Names())
	}
	// DA with a checksum field.
	s = parse(t, "SF:c.c\nDA:4,2,abcdef\nend_of_record\n")
	if s.Files["c.c"].Lines[4] != 2 {
		t.Errorf("DA with checksum: %v", s.Files["c.c"].Lines)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "no SF records"},
		{"TN:x\n", "no SF records"},
		{"DA:1,1\n", "outside an SF record"},
		{"end_of_record\n", "outside a record"},
		{"SF:\n", "SF with no path"},
		{"SF:a\nDA:x,1\n", "malformed DA"},
		{"SF:a\nDA:0,1\n", "malformed DA"},
		{"SF:a\nDA:1,-2\n", "malformed DA"},
		{"SF:a\nBRDA:1,0,0\n", "malformed BRDA"},
		{"SF:a\nBRDA:1,0,0,x\n", "malformed BRDA"},
		{"SF:a\nFN:x,f\n", "malformed FN"},
		{"SF:a\nFN:1,\n", "no name"},
		{"SF:a\nFNDA:1\n", "malformed FNDA"},
		{"SF:a\nFNA:0,1\n", "malformed FNA"},
		{"SF:a\nnonsense\n", "malformed record"},
	}
	for _, tc := range cases {
		err := NewSet().Parse(strings.NewReader(tc.in), "in")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) = %v, want error containing %q", tc.in, err, tc.want)
		}
	}
}
