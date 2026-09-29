package html

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DarrenBangsund/coverreport/internal/render/rendertest"
	"github.com/DarrenBangsund/coverreport/internal/render/view"
	"github.com/DarrenBangsund/coverreport/internal/source"
)

func build(t *testing.T, s rendertest.State) *view.View {
	t.Helper()
	root, err := source.Open(s.SourceDir)
	if err != nil {
		t.Fatal(err)
	}
	v, err := view.Build(s.Report, view.Options{Source: root, Previous: s.Previous, ArtifactURL: s.ArtifactURL,
		ArtifactName: "coverage-561.html", Version: "v0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func render(t *testing.T, v *view.View) string {
	t.Helper()
	var b bytes.Buffer
	if err := Render(&b, v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The page in every state. The stylesheet is the same for every state of
// one brand, so the page goldens carry a placeholder for it and each
// brand's stylesheet is pinned once, in css/<brand>.css.
func TestGoldenPages(t *testing.T) {
	css := map[string]string{}
	for _, s := range rendertest.States(t) {
		t.Run(s.Name, func(t *testing.T) {
			v := build(t, s)
			page := render(t, v)
			sheet := CSS(v)
			if !strings.Contains(page, "<style>\n"+sheet+"\n</style>") {
				t.Fatal("the page does not carry CSS(v) verbatim in its one <style>")
			}
			css[v.Brand.Name] = sheet
			page = strings.Replace(page, sheet, "/* css/"+v.Brand.Name+".css */", 1)
			rendertest.Golden(t, s.Name+"/report.html", []byte(page))
		})
	}
	for name, sheet := range css {
		rendertest.Golden(t, "css/"+name+".css", []byte(sheet))
	}
}
