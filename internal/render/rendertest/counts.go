package rendertest

import "regexp"

// countRE finds a count of one printed beside a plural ("1 lines", "1
// packages", "1 hold") and a plural hedged instead of decided ("file(s)").
// The nouns are the ones the surfaces count; a digit, a point or a comma
// before the 1 makes it part of a bigger number ("11 lines", "2.1 lines").
var countRE = regexp.MustCompile(`(?:^|[^0-9.,])1 (?:lines|statements|stmts|branches|functions|files|packages|gaps|layers|floors|gates|entries|annotations|comments|endpoints|routes|events|pages|layouts|cron entrypoints|server actions|hold|more files|more lines|changed lines|coverable lines|unchanged lines)\b|(?:file|line|floor|gate|layer|comment|annotation|package|entr)\((?:s|es|ies)\)`)

// BadCounts returns every count in text whose noun disagrees with it. Each
// surface pluralizes by the count; a renderer's test runs this over
// everything it renders, so a new count printed with a fixed plural is
// caught by the first state that happens to count one.
func BadCounts(text string) []string {
	return countRE.FindAllString(text, -1)
}
