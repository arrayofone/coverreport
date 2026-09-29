package action

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// parseYAML reads the YAML subset action.yml and examples/coverage.yml are
// written in, because the standard library has no YAML and the module takes
// no dependencies. It is deliberately narrow rather than lenient: block
// mappings and sequences, plain and quoted scalars, the four block-scalar
// styles, and comments. Anything else (flow collections, anchors, tags,
// multi-line plain scalars, tabs) is an error naming the line, so a file
// that drifts outside the subset fails these tests instead of being
// half-read. Every scalar is a string, which is also how GitHub hands
// inputs and env values to a step; an empty value is nil.
func parseYAML(src string) (v any, err error) {
	p := &yamlParser{lines: strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")}
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(yamlError)
			if !ok {
				panic(r)
			}
			v, err = nil, e
		}
	}()
	v = p.block(-1, false)
	if idx, _, _, ok := p.peek(); ok {
		p.fail(idx, "unexpected content")
	}
	return v, nil
}

type yamlError string

func (e yamlError) Error() string { return string(e) }

type yamlParser struct {
	lines []string
	i     int
}

func (p *yamlParser) fail(idx int, format string, a ...any) {
	panic(yamlError(fmt.Sprintf("line %d: %s", idx+1, fmt.Sprintf(format, a...))))
}

// peek finds the next line that is neither blank nor a comment.
func (p *yamlParser) peek() (idx, indent int, text string, ok bool) {
	for j := p.i; j < len(p.lines); j++ {
		l := p.lines[j]
		t := strings.TrimLeft(l, " ")
		if strings.HasPrefix(t, "\t") {
			p.fail(j, "a tab in the indentation")
		}
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if j == 0 && t == "---" {
			p.i = 1
			continue
		}
		return j, len(l) - len(t), t, true
	}
	return 0, 0, "", false
}

func isSeqItem(t string) bool { return t == "-" || strings.HasPrefix(t, "- ") }

// block reads the collection that starts on the next significant line when
// it is indented past parent (or, for a mapping value, a sequence at the
// parent's own indentation, which YAML allows); nil when there is none.
func (p *yamlParser) block(parent int, seqAtParent bool) any {
	_, ind, text, ok := p.peek()
	if !ok || ind < parent || (ind == parent && !(seqAtParent && isSeqItem(text))) {
		return nil
	}
	if isSeqItem(text) {
		return p.seq(ind)
	}
	return p.mapping(ind)
}

func (p *yamlParser) mapping(ind int) map[string]any {
	m := map[string]any{}
	for {
		idx, i2, text, ok := p.peek()
		if !ok || i2 < ind || (i2 == ind && isSeqItem(text)) {
			return m
		}
		if i2 > ind {
			p.fail(idx, "unexpected indentation (a multi-line plain scalar? use a block scalar)")
		}
		key, rest, isKey := splitKey(text)
		if !isKey {
			p.fail(idx, "expected key: value, got %q", text)
		}
		if _, dup := m[key]; dup {
			p.fail(idx, "duplicate key %q", key)
		}
		p.i = idx + 1
		m[key] = p.value(idx, ind, rest)
	}
}

func (p *yamlParser) seq(ind int) []any {
	var s []any
	for {
		idx, i2, text, ok := p.peek()
		if !ok || i2 < ind || !isSeqItem(text) {
			return s
		}
		if i2 > ind {
			p.fail(idx, "unexpected indentation")
		}
		after := text[1:]
		rest := strings.TrimLeft(after, " ")
		col := ind + 1 + len(after) - len(rest)
		switch _, _, isKey := splitKey(rest); {
		case rest == "" || strings.HasPrefix(rest, "#"):
			p.i = idx + 1
			s = append(s, p.block(ind, false))
		case isKey:
			// "- key: value" opens a mapping whose first entry sits where
			// the key starts; read it as if the dash were a space.
			p.lines[idx] = strings.Repeat(" ", col) + rest
			s = append(s, p.mapping(col))
		default:
			p.i = idx + 1
			s = append(s, p.value(idx, ind, rest))
		}
	}
}

// value is what follows "key:" or "- " on line idx.
func (p *yamlParser) value(idx, ind int, rest string) any {
	switch {
	case rest == "" || strings.HasPrefix(rest, "#"):
		return p.block(ind, true)
	case rest[0] == '|' || rest[0] == '>':
		return p.blockScalar(idx, ind, rest)
	}
	return scalar(p, idx, rest)
}

// splitKey splits "key: value" at the first ": " (or a trailing ":").
func splitKey(t string) (key, rest string, ok bool) {
	if t == "" || strings.ContainsRune(`'"[{&*!|>%@`+"`", rune(t[0])) {
		return "", "", false
	}
	for i := 0; i < len(t); i++ {
		if t[i] == ':' && (i == len(t)-1 || t[i+1] == ' ') {
			return t[:i], strings.TrimLeft(t[i+1:], " "), i > 0
		}
		if t[i] == ' ' && i+1 < len(t) && t[i+1] == '#' {
			return "", "", false
		}
	}
	return "", "", false
}

func scalar(p *yamlParser, idx int, s string) string {
	tail := func(rest string) {
		if rest = strings.TrimLeft(rest, " "); rest != "" && !strings.HasPrefix(rest, "#") {
			p.fail(idx, "text after a quoted scalar: %q", rest)
		}
	}
	switch s[0] {
	case '\'':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				tail(s[i+1:])
				return b.String()
			}
			b.WriteByte(s[i])
		}
		p.fail(idx, "unterminated single-quoted scalar")
	case '"':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '"':
				tail(s[i+1:])
				return b.String()
			case '\\':
				if i+1 == len(s) {
					p.fail(idx, "a backslash at the end of a double-quoted scalar")
				}
				i++
				r, ok := map[byte]string{'"': `"`, '\\': `\`, '/': "/", 'n': "\n", 't': "\t"}[s[i]]
				if !ok {
					p.fail(idx, `unsupported escape \%c`, s[i])
				}
				b.WriteString(r)
			default:
				b.WriteByte(s[i])
			}
		}
		p.fail(idx, "unterminated double-quoted scalar")
	case '[', '{', '&', '*', '!', '%', '@', '`':
		p.fail(idx, "unsupported YAML (flow collection, anchor, alias, tag or reserved indicator): %q", s)
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, " ")
}

// blockScalar reads a |, |-, |+, >, >- or >+ scalar whose header is on line
// idx and whose parent node is indented ind.
func (p *yamlParser) blockScalar(idx, ind int, header string) string {
	if i := strings.Index(header, " #"); i >= 0 {
		header = header[:i]
	}
	header = strings.TrimRight(header, " ")
	style, chomp := header[0], header[1:]
	if chomp != "" && chomp != "-" && chomp != "+" {
		p.fail(idx, "unsupported block scalar header %q", header)
	}
	var lines []string
	content := -1
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		t := strings.TrimLeft(l, " ")
		if t == "" {
			lines = append(lines, "")
			p.i++
			continue
		}
		n := len(l) - len(t)
		if content < 0 {
			if n <= ind {
				break
			}
			content = n
		}
		if n < content {
			break
		}
		lines = append(lines, l[content:])
		p.i++
	}
	trailing := 0
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
		trailing++
	}
	var body string
	if style == '|' {
		body = strings.Join(lines, "\n")
	} else {
		var b strings.Builder
		for i, l := range lines {
			switch {
			case strings.HasPrefix(l, " "):
				p.fail(idx, "a more-indented line in a folded scalar")
			case l == "":
				b.WriteByte('\n')
			default:
				if i > 0 && lines[i-1] != "" {
					b.WriteByte(' ')
				}
				b.WriteString(l)
			}
		}
		body = b.String()
	}
	switch {
	case chomp == "-" || body == "" && chomp == "":
		return body
	case chomp == "+":
		return body + "\n" + strings.Repeat("\n", trailing)
	}
	return body + "\n"
}

// Each expected value here is what PyYAML's safe_load returns for the same
// document, scalars stringified.
func TestParseYAML(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      any
	}{
		{"mapping, comments, plain scalars", "# head\na: 1 # one\nb: x#y\nc:\n", map[string]any{"a": "1", "b": "x#y", "c": nil}},
		{"nested mapping", "a:\n  b:\n    c: d\n  e: f\ng: h\n", map[string]any{"a": map[string]any{"b": map[string]any{"c": "d"}, "e": "f"}, "g": "h"}},
		{"value with colons", "d: One gate: floors, patch\nu: https://x.test/a\n", map[string]any{"d": "One gate: floors, patch", "u": "https://x.test/a"}},
		{"expressions", "t: ${{ github.token }}\ni: ${{ !cancelled() }}\n", map[string]any{"t": "${{ github.token }}", "i": "${{ !cancelled() }}"}},
		{"quoted", "a: \"true\"\nb: ''\nc: 'it''s # not a comment'\nd: \"x\\\"y\\n\" # c\n", map[string]any{"a": "true", "b": "", "c": "it's # not a comment", "d": "x\"y\n"}},
		{"sequence of scalars, indented and not", "a:\n  - x\n  - y\nb:\n- z\n", map[string]any{"a": []any{"x", "y"}, "b": []any{"z"}}},
		{"sequence of mappings", "s:\n  - name: one\n    id: a\n    env:\n      K: v\n  - uses: x/y@z # pin\n    with:\n      p: q\n", map[string]any{"s": []any{
			map[string]any{"name": "one", "id": "a", "env": map[string]any{"K": "v"}},
			map[string]any{"uses": "x/y@z", "with": map[string]any{"p": "q"}},
		}}},
		{"literal block", "r: |\n  echo a\n  # kept\n\n  echo b\n\nn: 1\n", map[string]any{"r": "echo a\n# kept\n\necho b\n", "n": "1"}},
		{"literal strip and keep", "a: |-\n  x\n\nb: |+\n  y\n\nc: z\n", map[string]any{"a": "x", "b": "y\n\n", "c": "z"}},
		{"folded", "d: >-\n  one\n  two\n\n  three\ne: >\n  four\n", map[string]any{"d": "one two\nthree", "e": "four\n"}},
		{"block scalar in a sequence", "- |\n  a\n- b\n", []any{"a\n", "b"}},
		{"document marker", "---\na: b\n", map[string]any{"a": "b"}},
	} {
		got, err := parseYAML(tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %#v\nwant %#v", tc.name, got, tc.want)
		}
	}
}

func TestParseYAMLRefusesWhatItDoesNotModel(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"a: [x, y]\n", "line 1: unsupported YAML"},
		{"a: {x: y}\n", "line 1: unsupported YAML"},
		{"a: &anchor x\n", "line 1: unsupported YAML"},
		{"a: b\na: c\n", `line 2: duplicate key "a"`},
		{"a: one\n  two\n", "line 2: unexpected indentation"},
		{"a:\n\tb: c\n", "line 2: a tab"},
		{"a: 'open\n", "line 1: unterminated single-quoted"},
		{"a: \"x\" y\n", "line 1: text after a quoted scalar"},
		{"a: |2\n  x\n", "line 1: unsupported block scalar header"},
		{"just a scalar line\n", `line 1: expected key: value`},
		{"a: b\n- c\n", "line 2: unexpected content"},
	} {
		_, err := parseYAML(tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %v, want %q", tc.src, err, tc.want)
		}
	}
}
