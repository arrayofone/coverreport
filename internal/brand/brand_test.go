package brand

import (
	"strings"
	"testing"
)

// A brand's values are written into the page's <style>. Anything that could
// end a declaration, open a rule or load a resource must be refused before
// a renderer sees it.
func TestValidateRefusesWhatCouldEscapeTheStylesheet(t *testing.T) {
	bad := []string{
		"red;}body{display:none",
		"#fff;background:url(https://evil.example/x.png)",
		"url(https://evil.example/x.png)",
		"expression(alert(1))",
		"rgb(1,2,3);",
		"rgb(1,2,3)}",
		"var(--x)",
		"color-mix(in oklab, red, blue)",
		"</style><script>alert(1)</script>",
		"#12345",
		"",
	}
	for _, v := range bad {
		c := &Config{Dark: map[string]string{"ok": v}}
		if err := c.Validate(); err == nil {
			t.Errorf("colour %q was accepted", v)
		}
	}
	for _, v := range []string{"#0c0e0c", "#fff", "#ffffff80", "rgba(255, 255, 255, 0.085)", "oklch(.13 0 0)", "oklch(0.99 0.003 80 / 50%)", "hsl(120 50% 40%)", "transparent"} {
		c := &Config{Light: map[string]string{"ok": v}}
		if err := c.Validate(); err != nil {
			t.Errorf("colour %q was refused: %v", v, err)
		}
	}
}

func TestValidateNamesAndFonts(t *testing.T) {
	for _, c := range []*Config{
		{Name: "<b>x</b>"},
		{Name: "a`b"},
		{Name: strings.Repeat("x", 41)},
		{Mono: `mono; } body { color: red`},
		{Sans: `"unbalanced, sans-serif`},
		{Dark: map[string]string{"no-such-token": "#fff"}},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("%+v was accepted", c)
		}
	}
	ok := &Config{Name: "handipay", Mono: `"Intel One Mono", ui-monospace, monospace`, Sans: `'IBM Plex Sans', system-ui`}
	if err := ok.Validate(); err != nil {
		t.Error(err)
	}
	var none *Config
	if err := none.Validate(); err != nil {
		t.Error("a missing brand block must be valid:", err)
	}
}

// A brand overrides what it names and inherits the rest, per scheme.
func TestResolveMergesOverTheDefault(t *testing.T) {
	r, err := Resolve(&Config{Name: "gitian", Dark: map[string]string{"ok": "#00ff00"}}, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "gitian" || r.Dark["ok"] != "#00ff00" || r.Dark["warning"] != Default.Dark["warning"] || r.Light["ok"] != Default.Light["ok"] {
		t.Errorf("merge wrong: %+v", r)
	}
	if Default.Dark["ok"] == "#00ff00" {
		t.Fatal("Resolve wrote through to the default theme")
	}
	r, _ = Resolve(nil, "demo")
	if r.Name != "demo" || r.Mono != DefaultMono {
		t.Errorf("fallback name/fonts not used: %+v", r)
	}
	r, _ = Resolve(nil, "")
	if r.Name != "coverage" {
		t.Errorf("empty fallback gave %q", r.Name)
	}
	if _, err := Resolve(&Config{Dark: map[string]string{"ok": "red;"}}, "x"); err == nil {
		t.Error("Resolve accepted an invalid brand")
	}
}

// Every token is declared once per scheme, in order, and the default theme
// defines every token in both schemes (a missing one would be an empty
// custom property: silently transparent).
func TestDeclarationsCoverEveryToken(t *testing.T) {
	for _, scheme := range []map[string]string{Default.Dark, Default.Light} {
		for _, tk := range Tokens {
			if !ValidColour(scheme[tk]) {
				t.Errorf("default theme: token %s = %q", tk, scheme[tk])
			}
		}
	}
	r, _ := Resolve(nil, "x")
	d := r.Declarations(true, "  ")
	last := -1
	for _, tk := range Tokens {
		i := strings.Index(d, "  --"+tk+": ")
		if i < 0 || strings.Count(d, "  --"+tk+": ") != 1 {
			t.Fatalf("token %s declared %d times", tk, strings.Count(d, "  --"+tk+": "))
		}
		if i < last {
			t.Errorf("token %s out of order", tk)
		}
		last = i
	}
}
