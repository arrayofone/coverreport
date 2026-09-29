package rendertest

import "regexp"

// countRE finds a count of one printed beside a plural ("1 lines", "1
// packages", "1 hold") and a plural hedged instead of decided ("file(s)").
// The nouns are the ones the surfaces count; a digit, a point or a comma
// before the 1 makes it part of a bigger number ("11 lines", "2.1 lines").
var countRE = regexp.MustCompile(`(?:^|[^0-9.,])1 (?:lines|statements|stmts|branches|functions|files|packages|gaps|layers|floors|gates|entries|annotations|comments|endpoints|routes|events|pages|layouts|cron entrypoints|server actions|hold|more files|more lines|changed lines|coverable lines|unchanged lines)\b|(?:file|line|floor|gate|layer|comment|annotation|package|entr)\((?:s|es|ies)\)`)

var (
	// markupRE is what can stand between a count and its noun: an HTML
	// tag ("<b>0 of 1</b> changed lines") or a Markdown strong or code
	// marker ("**0 of 1** changed lines"). A tag starts with a letter, so
	// a comparison in a code excerpt ("n < 10") is left alone.
	markupRE = regexp.MustCompile("</?[a-zA-Z][^>]*>|\\*\\*|`")
	// labelRE is an attribute a person reads (a screen reader's label, a
	// tooltip), which taking the tags out would take with them.
	labelRE = regexp.MustCompile(`\s(?:aria-label|title|alt)="([^"]*)"`)
)

// BadCounts returns every count in text whose noun disagrees with it. Each
// surface pluralizes by the count; a renderer's test runs this over
// everything it renders, so a new count printed with a fixed plural is
// caught by the first state that happens to count one.
//
// It reads what a person reads: the text with its markup taken out, then
// each labelling attribute on its own. Read as written instead, a count
// marked up apart from its noun would pass unseen.
func BadCounts(text string) []string {
	bad := countRE.FindAllString(markupRE.ReplaceAllString(text, ""), -1)
	for _, m := range labelRE.FindAllStringSubmatch(text, -1) {
		bad = append(bad, countRE.FindAllString(m[1], -1)...)
	}
	return bad
}
