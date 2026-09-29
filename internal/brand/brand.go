// Package brand is the whole themeable surface of coverreport: a name, two
// font stacks and one token set per colour scheme. The HTML page takes every
// token (as CSS custom properties in its one inline <style>); the markdown
// surfaces take only the name, because GitHub markdown cannot carry colour.
//
// Token NAMES are the page's semantic roles, never raw colours in a
// template: `ok` holds, `caution` needs a test, `warning` is below a floor,
// `target` is a goal or the line a link jumped to, and everything else is
// ink on a panel. A second theme is a token file, not a fork of the CSS.
//
// Values end up inside a <style> element, so they are validated here, before
// any renderer sees them: a colour is a hex literal or one CSS colour
// function with nothing but numbers, units and separators inside, and a font
// stack is family names only. Nothing that could close a declaration or a
// rule (";", "{", "}", "<", a backslash) gets through.
package brand

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Config is the brand block of coverage/config.json, carried verbatim into
// report.json so a renderer needs nothing but the report. Every key is
// optional: a missing token falls back to the default theme's value for the
// same scheme, so a repository can restyle three colours and keep the rest.
type Config struct {
	// Name is the wordmark and the word in every heading ("handipay
	// coverage"). Default: the repository's name, else "coverage".
	Name string `json:"name,omitempty"`
	// Mono and Sans are CSS font-family stacks. The page embeds no fonts:
	// the stacks name faces a reader may have and end in generic families.
	Mono string `json:"mono,omitempty"`
	Sans string `json:"sans,omitempty"`
	// Light and Dark map token name to CSS colour. Dark is the page's
	// default; Light applies under prefers-color-scheme: light.
	Light map[string]string `json:"light,omitempty"`
	Dark  map[string]string `json:"dark,omitempty"`
}

// Tokens lists every token name, in the order the stylesheet declares them.
// SCHEMA.md documents what each one colours.
var Tokens = []string{
	"bg", "panel", "glass", "bezel", "bezel-2",
	"ink", "ink-soft", "ink-ghost", "fill", "mark",
	"ok", "caution", "warning", "target",
	"t-ok", "t-caution", "t-warning", "t-target",
	"grid-a", "grid-b",
	"tm-ok", "tm-ok-fg", "tm-h1", "tm-h1-fg", "tm-h2", "tm-h2-fg", "tm-h3", "tm-h3-fg",
}

// DefaultMono and DefaultSans are the stacks used when the brand names none.
const (
	DefaultMono = `ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, "DejaVu Sans Mono", "Liberation Mono", monospace`
	DefaultSans = `system-ui, -apple-system, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif`
)

// Default is the neutral theme every repository gets unless it configures
// one: GitHub's own Primer greys, with its green, amber, red and blue in the
// four state roles, so an unconfigured page looks at home next to the PR it
// reports on. Every state colour clears 4.5:1 on its panel in both schemes.
var Default = Resolved{
	Mono: DefaultMono,
	Sans: DefaultSans,
	Dark: map[string]string{
		"bg": "#0d1117", "panel": "#151b23", "glass": "#010409",
		"bezel": "rgba(240, 246, 252, 0.1)", "bezel-2": "rgba(240, 246, 252, 0.2)",
		"ink": "#f0f6fc", "ink-soft": "#9198a1", "ink-ghost": "#656c76", "fill": "#c9d1d9", "mark": "#f0f6fc",
		"ok": "#3fb950", "caution": "#d29922", "warning": "#f85149", "target": "#4493f8",
		"t-ok": "rgba(63, 185, 80, 0.1)", "t-caution": "rgba(210, 153, 34, 0.16)",
		"t-warning": "rgba(248, 81, 73, 0.14)", "t-target": "rgba(68, 147, 248, 0.16)",
		"grid-a": "rgba(255, 255, 255, 0.02)", "grid-b": "transparent",
		"tm-ok": "#1b222b", "tm-ok-fg": "#9198a1",
		"tm-h1": "#3d2e0c", "tm-h1-fg": "#f0f6fc",
		"tm-h2": "#7d5a12", "tm-h2-fg": "#f0f6fc",
		"tm-h3": "#d29922", "tm-h3-fg": "#0d1117",
	},
	Light: map[string]string{
		"bg": "#f6f8fa", "panel": "#ffffff", "glass": "#ffffff",
		"bezel": "#d1d9e0", "bezel-2": "#b7c0c9",
		"ink": "#1f2328", "ink-soft": "#59636e", "ink-ghost": "#8c959f", "fill": "#353c44", "mark": "#1f2328",
		"ok": "#1a7f37", "caution": "#9a6700", "warning": "#cf222e", "target": "#0969da",
		"t-ok": "rgba(26, 127, 55, 0.08)", "t-caution": "rgba(212, 167, 44, 0.2)",
		"t-warning": "rgba(207, 34, 46, 0.08)", "t-target": "rgba(9, 105, 218, 0.1)",
		"grid-a": "rgba(31, 35, 40, 0.045)", "grid-b": "rgba(31, 35, 40, 0.025)",
		"tm-ok": "#eff2f5", "tm-ok-fg": "#59636e",
		"tm-h1": "#f5e0a3", "tm-h1-fg": "#1f2328",
		"tm-h2": "#e0b44a", "tm-h2-fg": "#1f2328",
		"tm-h3": "#9a6700", "tm-h3-fg": "#ffffff",
	},
}

// Resolved is a brand with every token filled in.
type Resolved struct {
	Name        string
	Mono, Sans  string
	Light, Dark map[string]string
}

var (
	hexRE = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	// One colour function; inside, only what a colour's arguments are made
	// of (numbers, units, the "/" alpha separator, commas, spaces, "none"
	// and the "in <space>" of color-mix is deliberately NOT allowed).
	fnRE      = regexp.MustCompile(`^(?:rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\([0-9a-z.,%/ +-]*\)$`)
	keywordRE = regexp.MustCompile(`^(?:transparent|currentColor)$`)
	// A font stack: family names, quoted or bare, separated by commas.
	fontRE = regexp.MustCompile(`^[A-Za-z0-9 ,"'_-]+$`)
)

// ValidColour reports whether v is a colour the stylesheet may carry.
func ValidColour(v string) bool {
	return hexRE.MatchString(v) || fnRE.MatchString(v) || keywordRE.MatchString(v)
}

// Validate checks a brand block: known token names, valid colours, safe font
// stacks and a name without markup. The errors name the offending key.
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}
	if strings.ContainsAny(c.Name, "<>`*_[]\\\n\r") || len(c.Name) > 40 {
		return fmt.Errorf("brand.name %q: at most 40 characters, no markdown or HTML metacharacters", c.Name)
	}
	for what, s := range map[string]string{"mono": c.Mono, "sans": c.Sans} {
		if s != "" && (!fontRE.MatchString(s) || strings.Count(s, `"`)%2 != 0 || strings.Count(s, `'`)%2 != 0) {
			return fmt.Errorf("brand.%s %q is not a font-family list (names, quotes, commas and spaces only)", what, s)
		}
	}
	known := map[string]bool{}
	for _, t := range Tokens {
		known[t] = true
	}
	for _, scheme := range []struct {
		name string
		m    map[string]string
	}{{"light", c.Light}, {"dark", c.Dark}} {
		for _, k := range sortedKeys(scheme.m) {
			if !known[k] {
				return fmt.Errorf("brand.%s: unknown token %q (known: %s)", scheme.name, k, strings.Join(Tokens, ", "))
			}
			if v := scheme.m[k]; !ValidColour(v) {
				return fmt.Errorf("brand.%s.%s = %q is not a colour (a #hex literal or one rgb()/hsl()/oklch()-style function)", scheme.name, k, v)
			}
		}
	}
	return nil
}

// Resolve merges a brand block over the default theme. fallbackName is used
// when the block names no brand (the repository's short name, typically).
func Resolve(c *Config, fallbackName string) (Resolved, error) {
	r := Resolved{Name: fallbackName, Mono: Default.Mono, Sans: Default.Sans,
		Light: map[string]string{}, Dark: map[string]string{}}
	if r.Name == "" {
		r.Name = "coverage"
	}
	for k, v := range Default.Light {
		r.Light[k] = v
	}
	for k, v := range Default.Dark {
		r.Dark[k] = v
	}
	if c == nil {
		return r, nil
	}
	if err := c.Validate(); err != nil {
		return Resolved{}, err
	}
	if c.Name != "" {
		r.Name = c.Name
	}
	if c.Mono != "" {
		r.Mono = c.Mono
	}
	if c.Sans != "" {
		r.Sans = c.Sans
	}
	for k, v := range c.Light {
		r.Light[k] = v
	}
	for k, v := range c.Dark {
		r.Dark[k] = v
	}
	return r, nil
}

// Declarations renders one scheme's tokens as custom-property declarations,
// one per line, in Tokens order, indented for a :root block.
func (r Resolved) Declarations(dark bool, indent string) string {
	m := r.Light
	if dark {
		m = r.Dark
	}
	var b strings.Builder
	for _, t := range Tokens {
		fmt.Fprintf(&b, "%s--%s: %s;\n", indent, t, m[t])
	}
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
