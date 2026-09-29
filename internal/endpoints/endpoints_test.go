package endpoints

import (
	"strings"
	"testing"
)

const registry = `{
  "version": 1,
  "generated_from": "abc",
  "classes": ["happy", "authn", "authz", "validation", "dependency"],
  "surfaces": { "svc-a": { "label": "svc-a (Go)" } },
  "endpoints": [
    { "id": "svc-a:GET /x", "surface": "svc-a", "kind": "route", "applicable": ["happy", "authn"],
      "layers": { "unit": { "classes": ["happy", "authn"], "status": "full" } }, "best": "full" },
    { "id": "svc-a:POST /x", "surface": "svc-a", "kind": "route", "applicable": ["happy", "authn", "authz", "dependency"],
      "layers": { "unit": { "classes": ["happy"], "status": "partial" },
                  "integration": { "classes": ["happy", "authn"], "status": "partial" } }, "best": "partial" },
    { "id": "svc-a:event y", "surface": "svc-a", "kind": "event", "applicable": ["happy", "dependency"],
      "layers": {}, "best": "none" },
    { "id": "app:page /p", "surface": "app", "kind": "page", "applicable": ["happy", "dependency"],
      "layers": { "e2e": { "classes": ["happy"], "status": "partial" } }, "best": "partial", "future_field": 1 }
  ],
  "summary": {},
  "baseline": { "violations": [ { "id": "svc-a:POST /x", "detail": "gained a gap: authz" } ] }
}`

func TestSummarize(t *testing.T) {
	f, err := Parse([]byte(registry))
	if err != nil {
		t.Fatal(err)
	}
	s := Summarize(f, "e/endpoints.json")
	if s.Total != 4 || s.File != "e/endpoints.json" || s.GeneratedFrom != "abc" {
		t.Fatalf("header: %+v", s)
	}
	if got := strings.Join(s.Layers, ","); got != "unit,integration,e2e" {
		t.Errorf("layers %q: the registry's own order, not first-seen or sorted", got)
	}
	if len(s.Kinds) != 3 || s.Kinds[0].Key != "route" || s.Kinds[0].Label != "routes" || s.Kinds[1].Key != "event" {
		t.Fatalf("kinds: %+v", s.Kinds)
	}
	r := s.Kinds[0]
	if r.Total != 2 || r.Best != (Counts{Full: 1, Partial: 1}) {
		t.Errorf("routes best %+v", r.Best)
	}
	// Per layer: the unit layer has one full and one partial route; the
	// integration layer has one partial and one it never mentions (none).
	if r.Layers["unit"] != (Counts{Full: 1, Partial: 1}) || r.Layers["integration"] != (Counts{Partial: 1, None: 1}) || r.Layers["e2e"] != (Counts{None: 2}) {
		t.Errorf("routes per layer %+v", r.Layers)
	}
	// POST /x's best layer is integration (partial, more classes): it
	// misses authz and dependency; "authz" wins the tie by class order,
	// every time (the counts live in a map, so a tie broken by iteration
	// order would flip between runs; fifty summaries would catch it).
	for i := 0; i < 50; i++ {
		r := Summarize(f, "").Kinds[0]
		if r.MostMissed != "authz" || r.MostMissedCount != 1 {
			t.Fatalf("most missed %q (%d)", r.MostMissed, r.MostMissedCount)
		}
	}
	if s.Kinds[1].MostMissed != "happy" || s.Kinds[1].Best != (Counts{None: 1}) {
		t.Errorf("events: %+v", s.Kinds[1])
	}
	if len(s.Surfaces) != 2 || s.Surfaces[0].Label != "svc-a (Go)" || s.Surfaces[1].Label != "app" {
		t.Errorf("surfaces: %+v", s.Surfaces)
	}
	if len(s.Violations) != 1 || s.Violations[0].ID != "svc-a:POST /x" {
		t.Errorf("violations: %+v", s.Violations)
	}
}

func TestParseRefusesBrokenRegistries(t *testing.T) {
	for name, doc := range map[string]string{
		"version":   `{"version": 2, "endpoints": [{"id": "a:b c", "surface": "a", "kind": "route", "best": "none"}]}`,
		"empty":     `{"version": 1, "endpoints": []}`,
		"duplicate": `{"version": 1, "endpoints": [{"id": "a:b c", "surface": "a", "kind": "route", "best": "none"}, {"id": "a:b c", "surface": "a", "kind": "route", "best": "none"}]}`,
		"best":      `{"version": 1, "endpoints": [{"id": "a:b c", "surface": "a", "kind": "route", "best": "most"}]}`,
		"layer":     `{"version": 1, "endpoints": [{"id": "a:b c", "surface": "a", "kind": "route", "best": "none", "layers": {"unit": {"status": "done"}}}]}`,
		"fields":    `{"version": 1, "endpoints": [{"id": "a:b c", "kind": "route", "best": "none"}]}`,
		"violation": `{"version": 1, "endpoints": [{"id": "a:b c", "surface": "a", "kind": "route", "best": "none"}], "baseline": {"violations": [{"detail": "x"}]}}`,
		"json":      `{"version": 1,`,
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestKindLabel(t *testing.T) {
	for k, want := range map[string]string{"route": "routes", "cron": "cron entrypoints", "action": "server actions", "widget": "widgets"} {
		if got := KindLabel(k); got != want {
			t.Errorf("%s: %q", k, got)
		}
	}
}
