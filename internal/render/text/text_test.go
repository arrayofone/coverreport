package text

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/arrayofone/coverreport/internal/report"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

// The text renderer reads the golden report.json the report package's own
// golden test writes, so the two goldens can only move together.
func load(t *testing.T) *report.Report {
	t.Helper()
	r, err := report.Load("../../../testdata/golden/report.json")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestGoldenText(t *testing.T) {
	for _, tc := range []struct {
		file   string
		detail bool
	}{{"report.txt", false}, {"report-detail.txt", true}} {
		var b bytes.Buffer
		if err := Render(&b, load(t), Options{Detail: tc.detail}); err != nil {
			t.Fatal(err)
		}
		path := "../../../testdata/golden/" + tc.file
		if *update {
			if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := b.String(); got != string(want) {
			t.Errorf("%s differs (review, then go test ./... -update):\n%s", tc.file, got)
		}
	}
}

// What a CI log reader must be able to see without the JSON.
func TestTextSaysWhatFailedAndWhy(t *testing.T) {
	var b bytes.Buffer
	if err := Render(&b, load(t), Options{}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"coverreport: FAIL  acme/demo #42 @ 3f2a9c1 (base 1a2b3c4)",
		"run: https://github.com/acme/demo/actions/runs/1234",
		"go-unit statements is 61.54%, below its floor 62.0 by more than the 0.1-point tolerance",
		"libs/go/calc/calc.go 0/6, uncovered L17-18, L35-37, L39",
		"edge: not measured",
		"**/*_templ.go",
		"# templ codegen",
		"package go-live libs/go/gone",
		"Warnings:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output lacks %q", want)
		}
	}
	if strings.Contains(out, "Packages in") {
		t.Error("package tables printed without Detail")
	}
	for i, line := range strings.Split(out, "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("line %d has trailing spaces: %q", i+1, line)
		}
	}
}

type failWriter struct{ n int }

func (w *failWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n > 2 {
		return 0, os.ErrClosed
	}
	return len(p), nil
}

func TestWriteErrorIsReturned(t *testing.T) {
	if err := Render(&failWriter{}, load(t), Options{Detail: true}); err == nil {
		t.Error("Render swallowed a write error")
	}
}
