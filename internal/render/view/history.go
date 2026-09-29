package view

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// The sticky comment carries its own history, so nothing outside GitHub
// stores anything: a hidden second line holds each push's overall patch
// coverage, the next render reads it back from the comment it replaces,
// appends this push and writes it out again.
//
//	<!-- coverreport:v1 -->
//	<!-- coverreport:state {"pushes":[["8e41d07",58.3],["3f2a9c1",86.4]]} -->

// Marker is the sticky comment's first line: the upsert finds the comment
// by it, so it never changes within a major version.
const Marker = "<!-- coverreport:v1 -->"

const statePrefix = "<!-- coverreport:state "

// MaxPushes bounds the history kept in the comment (and drawn).
const MaxPushes = 30

type state struct {
	// N is how many pushes there have been, which outgrows Pushes once
	// the history is capped. Absent means len(Pushes).
	N      int                  `json:"n"`
	Pushes [][2]json.RawMessage `json:"pushes"`
}

// ParseState reads the push history out of a previous comment body, with
// the number of pushes so far. A body without state, or with state this
// version cannot read, is an empty history: losing a sparkline must never
// fail a run.
func ParseState(body string) ([]Push, int) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, statePrefix) || !strings.HasSuffix(line, "-->") {
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(line, statePrefix), "-->")
		var s state
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, 0
		}
		var out []Push
		for _, p := range s.Pushes {
			var sha string
			var pct float64
			if json.Unmarshal(p[0], &sha) != nil || json.Unmarshal(p[1], &pct) != nil {
				return nil, 0
			}
			if !validSHA(sha) || math.IsNaN(pct) || pct < 0 || pct > 100 {
				return nil, 0
			}
			out = append(out, Push{SHA: sha, Pct: pct})
		}
		n := s.N
		if n < len(out) {
			n = len(out)
		}
		return out, n
	}
	return nil, 0
}

func validSHA(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// StateLine is the hidden history line: the push count and the pushes.
func StateLine(n int, pushes []Push) string {
	parts := make([]string, len(pushes))
	for i, p := range pushes {
		parts[i] = fmt.Sprintf("[%q,%s]", p.SHA, Trim(p.Pct))
	}
	return fmt.Sprintf(`%s{"n":%d,"pushes":[%s]} -->`, statePrefix, n, strings.Join(parts, ","))
}

func (v *View) history(previous string) {
	pushes, n := ParseState(previous)
	cur := Short7(v.PushSHA())
	if v.Patch != nil && v.Patch.Overall.Pct != nil && validSHA(cur) {
		pct := math.Round(*v.Patch.Overall.Pct*10) / 10
		if k := len(pushes); k > 0 && pushes[k-1].SHA == cur {
			// A re-run of the same push replaces its point.
			pushes[k-1].Pct = pct
		} else {
			pushes = append(pushes, Push{SHA: cur, Pct: pct})
			n++
		}
	}
	if len(pushes) > MaxPushes {
		pushes = pushes[len(pushes)-MaxPushes:]
	}
	v.Pushes = pushes
	v.PushNo = n
}

// PushSpark is the per-push sparkline.
func (v *View) PushSpark() string {
	vs := make([]float64, len(v.Pushes))
	for i, p := range v.Pushes {
		vs[i] = p.Pct
	}
	return Spark(vs)
}
