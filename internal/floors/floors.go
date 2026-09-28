// Package floors reads and writes coverage/floors.json: the machine-maintained
// minimums a check fails beneath. Floors only ever move up, by a ratchet
// commit; nothing in this package can lower one (Apply takes the max), and
// the file is written in one canonical form (sorted keys, one entry per line,
// exactly one decimal) so a ratchet's diff is precisely the lines whose value
// rose.
package floors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/DarrenBangsund/coverreport/internal/coverage"
	"github.com/DarrenBangsund/coverreport/internal/glob"
)

// Version is the only floors version this build reads or writes.
const Version = 1

// DefaultTolerance is written into a floors file the ratchet creates.
const DefaultTolerance = 0.1

// Metrics maps metric -> percentage.
type Metrics map[string]float64

// Floors is coverage/floors.json.
type Floors struct {
	Version      int                           `json:"version"`
	TolerancePts float64                       `json:"tolerance_pts"`
	Layers       map[string]Metrics            `json:"layers"`
	Packages     map[string]map[string]Metrics `json:"packages"`
	Globs        map[string]map[string]Metrics `json:"globs"`
}

// New returns an empty floors file at the default tolerance.
func New() *Floors {
	return &Floors{
		Version:      Version,
		TolerancePts: DefaultTolerance,
		Layers:       map[string]Metrics{},
		Packages:     map[string]map[string]Metrics{},
		Globs:        map[string]map[string]Metrics{},
	}
}

// Load reads a floors file. A missing file is (New(), false, nil): the first
// run of a new repo has nothing to fail beneath, and its first ratchet
// creates the file.
func Load(path string) (*Floors, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	f, err := Parse(data)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", path, err)
	}
	return f, true, nil
}

// Parse decodes and validates floors JSON.
func Parse(data []byte) (*Floors, error) {
	f := New()
	f.TolerancePts = -1 // detect absence
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(f); err != nil {
		return nil, fmt.Errorf("floors: %w", err)
	}
	if f.Version != Version {
		return nil, fmt.Errorf("floors: version %d is not supported (want %d)", f.Version, Version)
	}
	if f.TolerancePts < 0 {
		return nil, errors.New("floors: tolerance_pts is required and must be >= 0")
	}
	if !twoDecimals(f.TolerancePts) {
		return nil, fmt.Errorf("floors: tolerance_pts %v has more than two decimals", f.TolerancePts)
	}
	if f.Layers == nil {
		f.Layers = map[string]Metrics{}
	}
	if f.Packages == nil {
		f.Packages = map[string]map[string]Metrics{}
	}
	if f.Globs == nil {
		f.Globs = map[string]map[string]Metrics{}
	}
	for layer, ms := range f.Layers {
		if err := checkMetrics("layers."+layer, ms); err != nil {
			return nil, err
		}
	}
	for layer, dirs := range f.Packages {
		for dir, ms := range dirs {
			if dir == "" || strings.HasPrefix(dir, "/") {
				return nil, fmt.Errorf("floors: packages.%s: %q is not a repo-relative directory", layer, dir)
			}
			if err := checkMetrics("packages."+layer+"."+dir, ms); err != nil {
				return nil, err
			}
		}
	}
	for layer, globs := range f.Globs {
		for g, ms := range globs {
			if _, err := glob.Compile(g); err != nil {
				return nil, fmt.Errorf("floors: globs.%s: %w", layer, err)
			}
			if err := checkMetrics("globs."+layer+"."+g, ms); err != nil {
				return nil, err
			}
		}
	}
	return f, nil
}

func checkMetrics(where string, ms Metrics) error {
	for m, v := range ms {
		if _, err := coverage.ParseMetric(m); err != nil {
			return fmt.Errorf("floors: %s: %w", where, err)
		}
		if v < 0 || v > 100 {
			return fmt.Errorf("floors: %s.%s = %v is not a percentage", where, m, v)
		}
		if !oneDecimal(v) {
			return fmt.Errorf("floors: %s.%s = %v has more than one decimal (floors are tenths of a point, rounded down)", where, m, v)
		}
	}
	return nil
}

func oneDecimal(v float64) bool  { return math.Abs(v*10-math.Round(v*10)) < 1e-6 }
func twoDecimals(v float64) bool { return math.Abs(v*100-math.Round(v*100)) < 1e-6 }

// Tenths converts a one-decimal floor to integer tenths.
func Tenths(v float64) int64 { return int64(math.Round(v * 10)) }

// FromTenths converts integer tenths back to a percentage.
func FromTenths(t int64) float64 { return float64(t) / 10 }

// Scope is where a floor lives.
type Scope string

const (
	ScopeLayer   Scope = "layer"
	ScopePackage Scope = "package"
	ScopeGlob    Scope = "glob"
)

// Ref names one floor value: floors.<scope>.<layer>[.<key>].<metric>. Key is
// empty for layer scope.
type Ref struct {
	Scope  Scope  `json:"scope"`
	Layer  string `json:"layer"`
	Key    string `json:"key,omitempty"`
	Metric string `json:"metric,omitempty"`
}

// Get returns a floor and whether it is set.
func (f *Floors) Get(r Ref) (float64, bool) {
	var ms Metrics
	switch r.Scope {
	case ScopeLayer:
		ms = f.Layers[r.Layer]
	case ScopePackage:
		ms = f.Packages[r.Layer][r.Key]
	case ScopeGlob:
		ms = f.Globs[r.Layer][r.Key]
	}
	v, ok := ms[r.Metric]
	return v, ok
}

// Set stores a floor value unconditionally. Only Apply and tests call it;
// Apply is where "never lower" is enforced.
func (f *Floors) Set(r Ref, v float64) {
	switch r.Scope {
	case ScopeLayer:
		if f.Layers[r.Layer] == nil {
			f.Layers[r.Layer] = Metrics{}
		}
		f.Layers[r.Layer][r.Metric] = v
	case ScopePackage:
		setNested(f.Packages, r, v)
	case ScopeGlob:
		setNested(f.Globs, r, v)
	}
}

func setNested(m map[string]map[string]Metrics, r Ref, v float64) {
	if m[r.Layer] == nil {
		m[r.Layer] = map[string]Metrics{}
	}
	if m[r.Layer][r.Key] == nil {
		m[r.Layer][r.Key] = Metrics{}
	}
	m[r.Layer][r.Key][r.Metric] = v
}

// Delete removes one entry (package or glob scope; a whole key when metric is
// empty). Used only by an explicit ratchet --prune of stale entries.
func (f *Floors) Delete(r Ref) {
	var m map[string]map[string]Metrics
	switch r.Scope {
	case ScopePackage:
		m = f.Packages
	case ScopeGlob:
		m = f.Globs
	default:
		return
	}
	if r.Metric == "" {
		delete(m[r.Layer], r.Key)
	} else {
		delete(m[r.Layer][r.Key], r.Metric)
		if len(m[r.Layer][r.Key]) == 0 {
			delete(m[r.Layer], r.Key)
		}
	}
	if len(m[r.Layer]) == 0 {
		delete(m, r.Layer)
	}
}

// Change is one floor a ratchet moved.
type Change struct {
	Ref
	From *float64 `json:"from,omitempty"` // nil: a new entry
	To   float64  `json:"to"`
}

// Apply raises floors to the proposed values and returns what changed. A
// proposal at or below the current floor is ignored: this is the one place
// that decides "raise, never lower", and it holds even when a proposal was
// computed against an older floors file than the one on disk.
func (f *Floors) Apply(proposals []Change) []Change {
	var changed []Change
	for _, p := range proposals {
		cur, ok := f.Get(p.Ref)
		to := Tenths(p.To)
		if ok && Tenths(cur) >= to {
			continue
		}
		c := Change{Ref: p.Ref, To: FromTenths(to)}
		if ok {
			v := cur
			c.From = &v
		}
		f.Set(p.Ref, FromTenths(to))
		changed = append(changed, c)
	}
	return changed
}

// Marshal writes the canonical form:
//
//	{
//	  "version": 1,
//	  "tolerance_pts": 0.1,
//	  "layers": {
//	    "go-unit": { "statements": 83.2 }
//	  },
//	  "packages": {
//	    "go-unit": {
//	      "libs/go/accountsync": { "statements": 47.0 }
//	    }
//	  },
//	  "globs": {}
//	}
//
// Keys sorted bytewise at every level, one floor entry per line, every
// percentage with exactly one decimal (72.0, never 72), a trailing newline.
// A ratchet that raises one package's floor is a one-line diff.
func (f *Floors) Marshal() []byte {
	var b bytes.Buffer
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"version\": %d,\n", f.Version)
	fmt.Fprintf(&b, "  \"tolerance_pts\": %s,\n", formatTol(f.TolerancePts))
	b.WriteString("  \"layers\": ")
	writeFlat(&b, f.Layers, "  ")
	b.WriteString(",\n  \"packages\": ")
	writeNested(&b, f.Packages)
	b.WriteString(",\n  \"globs\": ")
	writeNested(&b, f.Globs)
	b.WriteString("\n}\n")
	return b.Bytes()
}

func writeFlat(b *bytes.Buffer, m map[string]Metrics, indent string) {
	keys := sortedKeys(m)
	if len(keys) == 0 {
		b.WriteString("{}")
		return
	}
	b.WriteString("{\n")
	for i, k := range keys {
		fmt.Fprintf(b, "%s  %s: %s", indent, quote(k), metricsInline(m[k]))
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(indent + "}")
}

func writeNested(b *bytes.Buffer, m map[string]map[string]Metrics) {
	layers := sortedKeys(m)
	// A layer key with no entries is dropped rather than written as {}.
	var keep []string
	for _, l := range layers {
		if len(m[l]) > 0 {
			keep = append(keep, l)
		}
	}
	if len(keep) == 0 {
		b.WriteString("{}")
		return
	}
	b.WriteString("{\n")
	for i, l := range keep {
		fmt.Fprintf(b, "    %s: ", quote(l))
		writeFlat(b, m[l], "    ")
		if i < len(keep)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  }")
}

func metricsInline(ms Metrics) string {
	keys := sortedKeys(ms)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s: %s", quote(k), formatTenths(Tenths(ms[k])))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func formatTenths(t int64) string { return fmt.Sprintf("%d.%d", t/10, t%10) }

// FormatFloor renders a floor the way the file does (one decimal).
func FormatFloor(v float64) string { return formatTenths(Tenths(v)) }

func formatTol(v float64) string {
	h := int64(math.Round(v * 100))
	if h%10 == 0 {
		return formatTenths(h / 10)
	}
	return fmt.Sprintf("%d.%02d", h/100, h%100)
}

// quote is a JSON string literal without encoding/json's HTML escaping, so a
// glob containing "&" or "<" stays readable in the file.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Write writes the canonical form to path.
func (f *Floors) Write(path string) error {
	return os.WriteFile(path, f.Marshal(), 0o644)
}
