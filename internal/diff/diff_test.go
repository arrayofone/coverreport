package diff

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func seq(a, b int) []int {
	var out []int
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}

// testdata/change.diff is real git output (the recommended -U0 -M
// invocation) over a commit with a binary change, two new files, a rename
// with edits, a text edit outside any layer, a multi-hunk edit and a
// deletion.
func TestRealGitDiff(t *testing.T) {
	f, err := os.Open("../../testdata/change.diff")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	files, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []File{
		{Path: "apps/web/public/logo.png", OldPath: "apps/web/public/logo.png", Status: Modified, Binary: true},
		{Path: "apps/web/src/components/button.tsx", Status: Added, Added: seq(1, 8)},
		{Path: "apps/web/src/components/ui/card.tsx", Status: Added, Added: seq(1, 3)},
		{Path: "apps/web/src/lib/money.ts", OldPath: "apps/web/src/lib/old.ts", Status: Renamed, Added: []int{5, 6}},
		{Path: "docs/README.md", OldPath: "docs/README.md", Status: Modified, Added: []int{3}},
		{Path: "libs/go/calc/calc.go", OldPath: "libs/go/calc/calc.go", Status: Modified, Added: append([]int{14, 15, 17, 18}, seq(33, 40)...)},
		{Path: "libs/go/calc/store_pg.go", Status: Added, Added: seq(1, 12)},
		{Path: "libs/go/old/old.go", OldPath: "libs/go/old/old.go", Status: Deleted},
	}
	if len(files) != len(want) {
		t.Fatalf("got %d files: %+v", len(files), files)
	}
	for i := range want {
		// An added file's OldPath comes from the diff --git header; it is
		// not load-bearing, so compare everything else.
		got := files[i]
		if want[i].Status == Added {
			got.OldPath = ""
		}
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("file %d:\n got %+v\nwant %+v", i, got, want[i])
		}
	}
}

func TestParseCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []File
	}{
		{
			name: "pure rename, no hunks",
			in:   "diff --git a/x/old.go b/x/new.go\nsimilarity index 100%\nrename from x/old.go\nrename to x/new.go\n",
			want: []File{{Path: "x/new.go", OldPath: "x/old.go", Status: Renamed}},
		},
		{
			// The header alone is ambiguous here (is the old path "x" or
			// "x b/y.go"?); only the rename lines say.
			name: "pure rename whose header cannot be split",
			in:   "diff --git a/x b/y.go b/z.go\nsimilarity index 100%\nrename from x b/y.go\nrename to z.go\n",
			want: []File{{Path: "z.go", OldPath: "x b/y.go", Status: Renamed}},
		},
		{
			// Belt and braces: git always writes "deleted file mode" too,
			// but "+++ /dev/null" alone must still mean a deletion.
			name: "deletion signalled only by +++ /dev/null",
			in:   "diff --git a/f.go b/f.go\n--- a/f.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n-b\n",
			want: []File{{Path: "f.go", OldPath: "f.go", Status: Deleted}},
		},
		{
			name: "copy with an edit",
			in:   "diff --git a/a.go b/b.go\nsimilarity index 90%\ncopy from a.go\ncopy to b.go\n--- a/a.go\n+++ b/b.go\n@@ -2 +2 @@\n-x\n+y\n",
			want: []File{{Path: "b.go", OldPath: "a.go", Status: Copied, Added: []int{2}}},
		},
		{
			name: "mode-only change",
			in:   "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n",
			want: []File{{Path: "run.sh", OldPath: "run.sh", Status: Modified}},
		},
		{
			name: "GIT binary patch",
			in:   "diff --git a/i.png b/i.png\nindex 1..2 100644\nGIT binary patch\nliteral 5\nMcmZ?wbhEHbM\n\nliteral 3\nKcmZ?w00001\n\n",
			want: []File{{Path: "i.png", OldPath: "i.png", Status: Modified, Binary: true}},
		},
		{
			name: "context lines (-U3), a removed line that looks like a header, no newline at end",
			in:   "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,4 +1,5 @@\n ctx1\n---- a sql comment removed\n+++ added, looks like a header\n+new\n ctx2\n ctx3\n\\ No newline at end of file\n",
			want: []File{{Path: "f.txt", OldPath: "f.txt", Status: Modified, Added: []int{2, 3}}},
		},
		{
			name: "hunk count omitted means one line; count zero means none",
			in:   "diff --git a/g.go b/g.go\n--- a/g.go\n+++ b/g.go\n@@ -3 +3 @@\n-a\n+b\n@@ -9,2 +8,0 @@\n-c\n-d\n",
			want: []File{{Path: "g.go", OldPath: "g.go", Status: Modified, Added: []int{3}}},
		},
		{
			name: "C-quoted paths (core.quotePath=true)",
			in:   "diff --git \"a/t\\303\\251st.go\" \"b/t\\303\\251st.go\"\n--- \"a/t\\303\\251st.go\"\n+++ \"b/t\\303\\251st.go\"\n@@ -0,0 +1 @@\n+x\n",
			want: []File{{Path: "tést.go", OldPath: "tést.go", Status: Modified, Added: []int{1}}},
		},
		{
			name: "path with a space, binary, no ---/+++ lines",
			in:   "diff --git a/my file.png b/my file.png\nBinary files a/my file.png and b/my file.png differ\n",
			want: []File{{Path: "my file.png", OldPath: "my file.png", Status: Modified, Binary: true}},
		},
		{
			name: "a commit-message preamble (git show / format-patch) is skipped",
			in:   "commit abc\nAuthor: x\n\n    msg\n\ndiff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n",
			want: []File{{Path: "a.go", OldPath: "a.go", Status: Modified, Added: []int{1}}},
		},
		{
			name: "empty diff",
			in:   "",
			want: []File{},
		},
	}
	for _, tc := range cases {
		got, err := Parse(strings.NewReader(tc.in))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"combined diff", "diff --cc m.go\nindex 1,2..3\n", "combined"},
		{"malformed hunk header", "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ nonsense @@\n", "malformed hunk header"},
		{"hunk longer than declared", "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-x\n+y\n+z\n", "outside a hunk"},
		{"garbage inside a hunk", "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,2 +1,2 @@\n-x\n?y\n", "unexpected line inside a hunk"},
	}
	for _, tc := range cases {
		_, err := Parse(strings.NewReader(tc.in))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Parse = %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}
