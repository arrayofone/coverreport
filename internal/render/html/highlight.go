package html

import (
	htmpl "html/template"
	"strings"
)

var keywords = map[string]map[string]bool{
	"go":   set("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false"),
	"ts":   set("as async await break case catch class const continue default delete do else enum export extends false finally for from function if implements import in instanceof interface let new null of return static super switch this throw true try type typeof undefined var void while yield"),
	"dart": set("abstract as async await break case catch class const continue default else enum extends factory false final finally for if implements import in is late library mixin new null on override required return static super switch this throw true try var void while with yield"),
}

func set(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// highlight marks up one line of code, deliberately quietly: comments and
// strings recede, keywords get weight, and colour stays reserved for
// coverage state. It is line-local (a block comment's middle lines read as
// code), which is the right trade for a listing that shows a few windows
// of a file, never the whole of it.
func highlight(code, lang string) htmpl.HTML {
	kw := keywords[lang]
	var b strings.Builder
	esc := htmpl.HTMLEscapeString
	if t := strings.TrimSpace(code); kw != nil && (t == "*" || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "*/")) {
		// The middle of a block comment: the highlighter is line-local,
		// and a line that starts with "*" is one far more often than it
		// is a dereference.
		return htmpl.HTML(`<span class="t-c">` + esc(code) + `</span>`)
	}
	rs := []rune(code)
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case kw != nil && r == '/' && i+1 < len(rs) && (rs[i+1] == '/' || rs[i+1] == '*'):
			b.WriteString(`<span class="t-c">` + esc(string(rs[i:])) + `</span>`)
			return htmpl.HTML(b.String())
		case kw != nil && (r == '"' || r == '`' || r == '\'' && (i == 0 || !isWord(rs[i-1]))):
			j := i + 1
			for j < len(rs) && rs[j] != r {
				if rs[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(rs))
			b.WriteString(`<span class="t-s">` + esc(string(rs[i:j])) + `</span>`)
			i = j
		case kw != nil && (r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			j := i
			for j < len(rs) && (isWord(rs[j]) || rs[j] == '$') {
				j++
			}
			w := string(rs[i:j])
			if kw[w] {
				b.WriteString(`<span class="t-k">` + esc(w) + `</span>`)
			} else {
				b.WriteString(esc(w))
			}
			i = j
		default:
			b.WriteString(esc(string(r)))
			i++
		}
	}
	return htmpl.HTML(b.String())
}

func isWord(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}
