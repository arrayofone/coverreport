package config

import (
	"strings"
	"testing"

	"github.com/DarrenBangsund/coverreport/internal/coverage"
)

func TestFixtureConfigResolves(t *testing.T) {
	r, err := Load("../../testdata/repo/coverage/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if r.Floors != DefaultFloors || r.Exclude != DefaultExclude {
		t.Errorf("defaults not applied: %q %q", r.Floors, r.Exclude)
	}
	if r.PatchTarget != 80 || r.MinLines != 3 || r.Blocking || r.LowestFiles != 3 {
		t.Errorf("patch/lowest = %v %v %v %v", r.PatchTarget, r.MinLines, r.Blocking, r.LowestFiles)
	}
	unit := r.Layer("go-unit")
	if unit == nil || unit.Label != "Go unit (logic)" || !unit.PkgFloors {
		t.Fatalf("go-unit = %+v", unit)
	}
	// "@sql-adapters" expanded into its globs.
	if len(unit.ExcludeGlobs) != 1 || unit.ExcludeGlobs[0].String() != "libs/go/**/store_pg.go" {
		t.Errorf("go-unit exclude = %v", unit.ExcludeGlobs)
	}
	if unit.TargetMap[coverage.Statements] != 85 || unit.PkgTargets[coverage.Statements] != 80 {
		t.Errorf("targets = %v %v", unit.TargetMap, unit.PkgTargets)
	}
	web := r.Layer("web")
	if web.Prefix != "apps/web" || len(web.MetricList) != 3 || web.MetricList[0] != coverage.Lines {
		t.Errorf("web = %+v", web)
	}
	if r.Layer("nope") != nil {
		t.Error("Layer(nope) != nil")
	}
}

func TestDefaultsWhenAbsent(t *testing.T) {
	r, err := Parse([]byte(`{"version":1,"layers":[{"id":"a","format":"go","inputs":["x.out"],"metrics":["statements"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.PatchTarget != 80 || r.MinLines != 5 || r.Blocking || r.LowestFiles != 25 {
		t.Errorf("defaults = %v %v %v %v", r.PatchTarget, r.MinLines, r.Blocking, r.LowestFiles)
	}
	if r.Layers[0].Label != "a" {
		t.Errorf("label defaults to id, got %q", r.Layers[0].Label)
	}
}

func TestInvalidConfigs(t *testing.T) {
	layer := func(extra string) string {
		return `{"version":1,"layers":[{"id":"a","format":"go","inputs":["x.out"],"metrics":["statements"]` + extra + `}]}`
	}
	cases := []struct{ name, in, want string }{
		{"not json", `{`, "config:"},
		{"unknown top-level key", `{"version":1,"exlcude":"x","layers":[]}`, "unknown field"},
		{"unknown layer key", layer(`,"inclde":["x"]`), "unknown field"},
		{"wrong version", `{"version":2,"layers":[]}`, "version 2"},
		{"no layers", `{"version":1,"layers":[]}`, "no layers"},
		{"bad id", `{"version":1,"layers":[{"id":"Go Unit","format":"go","inputs":["x"],"metrics":["statements"]}]}`, "id"},
		{"duplicate id", `{"version":1,"layers":[{"id":"a","format":"go","inputs":["x"],"metrics":["statements"]},{"id":"a","format":"go","inputs":["x"],"metrics":["statements"]}]}`, "duplicate layer id"},
		{"bad format", `{"version":1,"layers":[{"id":"a","format":"cobertura","inputs":["x"],"metrics":["lines"]}]}`, "format"},
		{"go cannot measure lines", `{"version":1,"layers":[{"id":"a","format":"go","inputs":["x"],"metrics":["lines"]}]}`, "cannot be measured from go"},
		{"lcov cannot measure statements", `{"version":1,"layers":[{"id":"a","format":"lcov","inputs":["x"],"metrics":["statements"]}]}`, "cannot be measured from lcov"},
		{"unknown metric", `{"version":1,"layers":[{"id":"a","format":"go","inputs":["x"],"metrics":["loc"]}]}`, "unknown metric"},
		{"metric twice", layer(`,"metrics":["statements","statements"]`), "listed twice"},
		{"no metrics", `{"version":1,"layers":[{"id":"a","format":"go","inputs":["x"],"metrics":[]}]}`, "metrics is empty"},
		{"no inputs", `{"version":1,"layers":[{"id":"a","format":"go","inputs":[],"metrics":["statements"]}]}`, "inputs is empty"},
		{"target for unmeasured metric", layer(`,"targets":{"lines":80}`), "not one of this layer's metrics"},
		{"target out of range", layer(`,"targets":{"statements":180}`), "not a percentage"},
		{"bad include glob", layer(`,"include":["/abs/**"]`), "absolute"},
		{"unknown set", layer(`,"exclude":["@nope"]`), "unknown set"},
		{"paths on a go layer", layer(`,"paths":{"prefix":"x"}`), "lcov layers only"},
		{"relative strip", `{"version":1,"layers":[{"id":"a","format":"lcov","inputs":["x"],"metrics":["lines"],"paths":{"strip":["home/x"]}}]}`, "must be absolute"},
		{"prefix leaves repo", `{"version":1,"layers":[{"id":"a","format":"lcov","inputs":["x"],"metrics":["lines"],"paths":{"prefix":"../x"}}]}`, "leaves the repository"},
		{"report_only with floors", layer(`,"report_only":true,"packages":{"floors":true}`), "report_only"},
		{"glob twice", layer(`,"globs":[{"glob":"a/**"},{"glob":"a/**"}]`), "listed twice"},
		{"negative min_size", layer(`,"packages":{"floors":true,"min_size":-1}`), "min_size"},
		{"patch target out of range", `{"version":1,"patch":{"target":101},"layers":[]}`, "patch.target"},
		{"negative min_lines", `{"version":1,"patch":{"min_lines":-1},"layers":[]}`, "min_lines"},
		{"absolute floors path", `{"version":1,"floors":"/etc/floors.json","layers":[]}`, "absolute"},
		{"bad set name", `{"version":1,"sets":{"Bad Name":["x"]},"layers":[]}`, "set name"},
		{"empty set", `{"version":1,"sets":{"s":[]},"layers":[]}`, "is empty"},
		{"trailing data", layer(``) + `{}`, "trailing data"},
	}
	for _, tc := range cases {
		_, err := Parse([]byte(tc.in))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Parse = %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}

// The presentation keys: defaults from the id, and refusals for values
// that would break a grid, a table or the page's stylesheet.
func TestPresentationKeys(t *testing.T) {
	layer := func(extra string) string {
		return `{"version":1,"layers":[{"id":"go-unit","format":"go","inputs":["a.out"],"metrics":["statements"]` + extra + `}]}`
	}
	r, err := Parse([]byte(layer("")))
	if err != nil {
		t.Fatal(err)
	}
	if l := r.Layer("go-unit"); l.ShortLabel != "go unit" || l.Group != "go" || l.Treemap || r.RatchetCommand != DefaultRatchetCommand || r.Brand != nil {
		t.Errorf("defaults: %q %q %v %q %v", l.ShortLabel, l.Group, l.Treemap, r.RatchetCommand, r.Brand)
	}
	r, err = Parse([]byte(`{"version":1,"ratchet_command":"make coverage-ratchet","patch":{"informational_until":"2026-10-05"},
	  "brand":{"name":"handipay","dark":{"ok":"#00e055"}},
	  "layers":[{"id":"go-live","short_label":"go unit+pg","group":"go","treemap":true,"format":"go","inputs":["a.out"],"metrics":["statements"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if l := r.Layer("go-live"); l.ShortLabel != "go unit+pg" || !l.Treemap || r.InformationalUntil != "2026-10-05" || r.Brand.Name != "handipay" || r.RatchetCommand != "make coverage-ratchet" {
		t.Errorf("explicit: %+v", r)
	}
	for name, doc := range map[string]string{
		"short label too long": layer(`,"short_label":"an extremely long label"`),
		"pipe in a label":      layer(`,"label":"a | b"`),
		"backtick in group":    layer("," + `"group":"g` + "`" + `"`),
		"bad soak date":        `{"version":1,"patch":{"informational_until":"next week"},"layers":[{"id":"a","format":"go","inputs":["a"],"metrics":["statements"]}]}`,
		"bad brand token":      `{"version":1,"brand":{"dark":{"ok":"red;}"}},"layers":[{"id":"a","format":"go","inputs":["a"],"metrics":["statements"]}]}`,
		"unknown brand key":    `{"version":1,"brand":{"colour":"red"},"layers":[{"id":"a","format":"go","inputs":["a"],"metrics":["statements"]}]}`,
		"multi-line ratchet":   `{"version":1,"ratchet_command":"a\nb","layers":[{"id":"a","format":"go","inputs":["a"],"metrics":["statements"]}]}`,
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
