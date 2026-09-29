package github

import (
	"testing"

	"github.com/arrayofone/coverreport/internal/render/rendertest"
	"github.com/arrayofone/coverreport/internal/render/view"
	"github.com/arrayofone/coverreport/internal/source"
)

// build decides one golden state the way the render command does.
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

// Every surface in every state, byte for byte. The tests beside this one
// assert the same behaviour by name, so a regenerated golden cannot
// quietly absorb a change in meaning.
func TestGoldenSurfaces(t *testing.T) {
	for _, s := range rendertest.States(t) {
		t.Run(s.Name, func(t *testing.T) {
			v := build(t, s)
			o := Options{CommentBudget: s.CommentBudget, SummaryBudget: s.SummaryBudget}
			rendertest.Golden(t, s.Name+"/comment.md", []byte(Comment(v, o)))
			rendertest.Golden(t, s.Name+"/summary.md", []byte(Summary(v, o)))
			rendertest.Golden(t, s.Name+"/annotations.txt", []byte(Annotations(v)))
		})
	}
}
