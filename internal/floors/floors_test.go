package floors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/repo/coverage/floors.json")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.TolerancePts != 0.1 {
		t.Errorf("tolerance = %v", f.TolerancePts)
	}
	if v, ok := f.Get(Ref{Scope: ScopePackage, Layer: "go-live", Key: "libs/go/calc", Metric: "statements"}); !ok || v != 62.4 {
		t.Errorf("package floor = %v, %v", v, ok)
	}
	if v, ok := f.Get(Ref{Scope: ScopeGlob, Layer: "web", Key: "apps/web/src/lib/**", Metric: "lines"}); !ok || v != 100 {
		t.Errorf("glob floor = %v, %v", v, ok)
	}
	if _, ok := f.Get(Ref{Scope: ScopeLayer, Layer: "web", Metric: "functions"}); ok {
		t.Error("unset floor reported as set")
	}
}

// The canonical form, byte for byte: sorted keys, one entry per line, one
// decimal always (72.0, never 72), empty sections as {}. The fixture file is
// written in this form, so Parse then Marshal must reproduce it exactly;
// that is what keeps a ratchet's diff down to the lines that rose.
func TestMarshalIsCanonicalAndRoundTrips(t *testing.T) {
	data, err := os.ReadFile("../../testdata/repo/coverage/floors.json")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(f.Marshal()); got != string(data) {
		t.Errorf("Marshal differs from the canonical fixture:\n%s\nwant:\n%s", got, data)
	}
	empty := New().Marshal()
	want := "{\n  \"version\": 1,\n  \"tolerance_pts\": 0.1,\n  \"layers\": {},\n  \"packages\": {},\n  \"globs\": {}\n}\n"
	if string(empty) != want {
		t.Errorf("empty = %q, want %q", empty, want)
	}
	// Glob keys with characters encoding/json would HTML-escape stay
	// readable, and a two-decimal tolerance survives.
	g := New()
	g.TolerancePts = 0.05
	g.Set(Ref{Scope: ScopeGlob, Layer: "app", Key: "src/**/{a,b}&<c>.ts", Metric: "lines"}, 72)
	out := string(g.Marshal())
	if !strings.Contains(out, `"src/**/{a,b}&<c>.ts": { "lines": 72.0 }`) || !strings.Contains(out, `"tolerance_pts": 0.05,`) {
		t.Errorf("Marshal = %s", out)
	}
	if _, err := Parse([]byte(out)); err != nil {
		t.Errorf("re-parse: %v", err)
	}
}

// Apply is where "never lower" lives. Each case starts from the same floors.
func TestApplyNeverLowers(t *testing.T) {
	base := func() *Floors {
		f := New()
		f.Set(Ref{Scope: ScopeLayer, Layer: "go-unit", Metric: "statements"}, 83.2)
		f.Set(Ref{Scope: ScopePackage, Layer: "go-unit", Key: "libs/go/x", Metric: "statements"}, 50.0)
		return f
	}
	layer := Ref{Scope: ScopeLayer, Layer: "go-unit", Metric: "statements"}
	pkg := Ref{Scope: ScopePackage, Layer: "go-unit", Key: "libs/go/x", Metric: "statements"}
	newGlob := Ref{Scope: ScopeGlob, Layer: "app", Key: "src/lib/**", Metric: "lines"}
	cases := []struct {
		name     string
		proposal Change
		ref      Ref
		want     float64
		changed  bool
	}{
		{"a higher value raises", Change{Ref: layer, To: 83.5}, layer, 83.5, true},
		{"a lower value is ignored", Change{Ref: layer, To: 80.0}, layer, 83.2, false},
		{"an equal value is ignored", Change{Ref: layer, To: 83.2}, layer, 83.2, false},
		{"zero is ignored", Change{Ref: pkg, To: 0}, pkg, 50.0, false},
		{"a missing entry is created", Change{Ref: newGlob, To: 92.5}, newGlob, 92.5, true},
		{"float noise does not count as a rise", Change{Ref: layer, To: 83.20000000001}, layer, 83.2, false},
	}
	for _, tc := range cases {
		f := base()
		ch := f.Apply([]Change{tc.proposal})
		got, ok := f.Get(tc.ref)
		if !ok || got != tc.want {
			t.Errorf("%s: floor = %v (set %v), want %v", tc.name, got, ok, tc.want)
		}
		if (len(ch) == 1) != tc.changed {
			t.Errorf("%s: changes = %+v, want changed=%v", tc.name, ch, tc.changed)
		}
		if tc.changed && tc.ref == layer && (ch[0].From == nil || *ch[0].From != 83.2) {
			t.Errorf("%s: From = %v, want 83.2", tc.name, ch[0].From)
		}
		if tc.changed && tc.ref == newGlob && ch[0].From != nil {
			t.Errorf("%s: From = %v, want nil for a new entry", tc.name, *ch[0].From)
		}
	}
}

func TestDelete(t *testing.T) {
	f := New()
	a := Ref{Scope: ScopePackage, Layer: "l", Key: "a", Metric: "lines"}
	b := Ref{Scope: ScopePackage, Layer: "l", Key: "a", Metric: "branches"}
	f.Set(a, 10)
	f.Set(b, 20)
	f.Delete(a)
	if _, ok := f.Get(a); ok {
		t.Error("metric not deleted")
	}
	if _, ok := f.Get(b); !ok {
		t.Error("sibling metric deleted")
	}
	f.Delete(Ref{Scope: ScopePackage, Layer: "l", Key: "a"})
	if len(f.Packages) != 0 {
		t.Errorf("whole-key delete left %v", f.Packages)
	}
	f.Set(Ref{Scope: ScopeLayer, Layer: "l", Metric: "lines"}, 1)
	f.Delete(Ref{Scope: ScopeLayer, Layer: "l", Metric: "lines"})
	if _, ok := f.Layers["l"]; !ok {
		t.Error("Delete must never remove a layer floor")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"version":1,"tolerance_pts":0.1,"layers":{"a":{"lines":72.05}}}`, "more than one decimal"},
		{`{"version":1,"tolerance_pts":0.1,"layers":{"a":{"lines":101}}}`, "not a percentage"},
		{`{"version":1,"tolerance_pts":0.1,"layers":{"a":{"loc":50}}}`, "unknown metric"},
		{`{"version":1,"layers":{}}`, "tolerance_pts is required"},
		{`{"version":1,"tolerance_pts":0.001}`, "more than two decimals"},
		{`{"version":2,"tolerance_pts":0.1}`, "version 2"},
		{`{"version":1,"tolerance_pts":0.1,"extra":1}`, "unknown field"},
		{`{"version":1,"tolerance_pts":0.1,"packages":{"a":{"/abs":{"lines":1}}}}`, "repo-relative"},
		{`{"version":1,"tolerance_pts":0.1,"globs":{"a":{"x//y":{"lines":1}}}}`, "empty path segment"},
	}
	for _, tc := range cases {
		_, err := Parse([]byte(tc.in))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%s) = %v, want an error containing %q", tc.in, err, tc.want)
		}
	}
}

func TestLoadMissingAndWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "floors.json")
	f, found, err := Load(p)
	if err != nil || found || f.TolerancePts != DefaultTolerance {
		t.Fatalf("Load(missing) = %+v, %v, %v", f, found, err)
	}
	f.Set(Ref{Scope: ScopeLayer, Layer: "x", Metric: "lines"}, 50)
	if err := f.Write(p); err != nil {
		t.Fatal(err)
	}
	g, found, err := Load(p)
	if err != nil || !found || g.Layers["x"]["lines"] != 50 {
		t.Errorf("reload = %+v, %v, %v", g, found, err)
	}
}
