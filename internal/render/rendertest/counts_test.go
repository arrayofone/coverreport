package rendertest

import (
	"strings"
	"testing"
)

// BadCounts reads what a person reads: a count its noun is marked up apart
// from, and a count inside a label attribute, are both found; a bigger
// number and a singular are not; and a comparison in code ("n < 10") is not
// markup, so nothing between it and the next ">" is skipped.
func TestBadCounts(t *testing.T) {
	for _, tc := range []struct {
		text string
		want string
	}{
		{`<p><b>0 of 1</b> changed lines ran</p>`, " 1 changed lines"},
		{`> **0 of 1** changed lines ran`, " 1 changed lines"},
		{`<svg role="img" aria-label="0 of 1 changed lines ran"></svg>`, " 1 changed lines"},
		{`<figure title="1 files"></figure>`, "1 files"},
		{`<span class="frac">0/1 changed lines ran</span>`, "/1 changed lines"},
		{`checked 3 file(s)`, "file(s)"},
		{`<b>0 of 1</b> changed line ran, 11 lines, 2.1 lines, 1,1 lines`, ""},
		{"if n < 10 { skip(1 files) } else if n > 20 {", "(1 files"},
	} {
		if got := strings.Join(BadCounts(tc.text), "|"); got != tc.want {
			t.Errorf("BadCounts(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
