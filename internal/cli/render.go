package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arrayofone/coverreport/internal/ghapi"
	"github.com/arrayofone/coverreport/internal/render/github"
	"github.com/arrayofone/coverreport/internal/render/html"
	"github.com/arrayofone/coverreport/internal/render/view"
	"github.com/arrayofone/coverreport/internal/report"
	"github.com/arrayofone/coverreport/internal/source"
)

// Surface file names, written into render's --out directory.
const (
	CommentFile     = "comment.md"
	SummaryFile     = "summary.md"
	AnnotationsFile = "annotations.txt"
	HTMLFile        = "report.html"
)

// runRender writes the four surfaces of one report. It is safe to run
// twice: CI renders once to get report.html, uploads it, then renders again
// with --artifact-url so the comment and annotations can link the page
// (the page itself does not change).
func runRender(args []string, env Env) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	reportPath := fs.String("report", "", "report.json from coverreport analyze (required)")
	out := fs.String("out", "", "directory for comment.md, summary.md, annotations.txt and report.html (required; created)")
	srcRoot := fs.String("source-root", "", "a checkout of the measured commit, for code excerpts (without it the surfaces show line numbers only)")
	previous := fs.String("previous", "", "the sticky comment this render replaces (coverreport comment --fetch writes it), for the push-by-push patch history")
	artURL := fs.String("artifact-url", "", "where report.html was uploaded, for the links in the comment, summary and annotations")
	artName := fs.String("artifact-name", "report.html", "what those links call the uploaded page")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *reportPath == "" || *out == "" {
		return errors.New("--report and --out are required")
	}
	r, err := report.Load(*reportPath)
	if err != nil {
		return err
	}
	root, err := source.Open(*srcRoot)
	if err != nil {
		return fmt.Errorf("--source-root: %w", err)
	}
	prev := ""
	if *previous != "" {
		data, err := os.ReadFile(*previous)
		if err != nil {
			return fmt.Errorf("--previous: %w", err)
		}
		prev = string(data)
	}
	v, err := view.Build(r, view.Options{Source: root, Previous: prev, ArtifactURL: *artURL, ArtifactName: *artName, Version: shortVersion()})
	if err != nil {
		return err
	}
	var page bytes.Buffer
	if err := html.Render(&page, v); err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
	}{
		{CommentFile, []byte(github.Comment(v, github.Options{}))},
		{SummaryFile, []byte(github.Summary(v, github.Options{}))},
		{AnnotationsFile, []byte(github.Annotations(v))},
		{HTMLFile, page.Bytes()},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(*out, f.name), f.data, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(env.Stdout, "coverreport: %s (%s); wrote %s, %s, %s (%d annotations), %s (%d KB)\n", v.Verdict, v.Heading,
		CommentFile, SummaryFile, AnnotationsFile, len(v.Annotations), HTMLFile, (page.Len()+1023)/1024)
	return nil
}

// shortVersion is the module version for footers ("v0.1.0", a pseudo
// version, or "devel").
func shortVersion() string {
	v := strings.TrimPrefix(version(), "coverreport ")
	if f := strings.Fields(v); len(f) > 0 {
		v = f[0]
	}
	if v == "" || v == "(devel)" || strings.HasPrefix(v, "(") {
		return "devel"
	}
	return v
}

// runComment upserts the sticky comment, or with --fetch only reads it.
//
//	coverreport comment --fetch previous.md          (before render)
//	coverreport comment --body out/comment.md        (after render)
//
// The comment is found by its marker (the body's first line); the token
// is GITHUB_TOKEN from the environment and is never printed.
func runComment(args []string, env Env) error {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	repo := fs.String("repo", "", "owner/name (default $GITHUB_REPOSITORY)")
	prFlag := fs.String("pr", "", "pull request number (default: from $GITHUB_REF)")
	body := fs.String("body", "", "the comment to upsert (render's comment.md)")
	fetch := fs.String("fetch", "", "only fetch: write the current sticky comment's body to this file (empty when there is none)")
	prevOut := fs.String("previous", "", "with --body: also write the body it replaced to this file")
	marker := fs.String("marker", view.Marker, "with --fetch: the marker to look for (with --body the body's first line is the marker)")
	api := fs.String("api-url", "", "GitHub API base URL (default $GITHUB_API_URL, else "+ghapi.DefaultAPI+")")
	if err := parse(fs, args); err != nil {
		return err
	}
	if (*body == "") == (*fetch == "") {
		return errors.New("give exactly one of --body (upsert) and --fetch (read only)")
	}
	if *repo == "" {
		*repo = env.Getenv("GITHUB_REPOSITORY")
	}
	if !ghapi.ValidRepo(*repo) {
		return fmt.Errorf("--repo %q is not owner/name", *repo)
	}
	if *prFlag == "" {
		if ref := env.Getenv("GITHUB_REF"); strings.HasPrefix(ref, "refs/pull/") {
			*prFlag = strings.SplitN(strings.TrimPrefix(ref, "refs/pull/"), "/", 2)[0]
		}
	}
	pr, err := strconv.Atoi(*prFlag)
	if err != nil || pr <= 0 {
		return fmt.Errorf("--pr %q is not a pull request number (none in $GITHUB_REF either: is this a pull_request run?)", *prFlag)
	}
	token := env.Getenv("GITHUB_TOKEN")
	if token == "" {
		return errors.New("GITHUB_TOKEN is not set: pass the job's token in the environment (env: GITHUB_TOKEN: ${{ github.token }})")
	}
	if *api == "" {
		*api = env.Getenv("GITHUB_API_URL")
	}
	if *api == "" {
		*api = ghapi.DefaultAPI
	}
	c := &ghapi.Client{API: *api, Token: token, HTTP: env.HTTP, UserAgent: "coverreport/" + shortVersion(), Retries: 2, Sleep: env.Sleep}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if *fetch != "" {
		cs, err := c.Comments(ctx, *repo, pr)
		if err != nil {
			return err
		}
		prev := ""
		if cm, _ := ghapi.Find(cs, *marker); cm != nil {
			prev = cm.Body
			fmt.Fprintf(env.Stdout, "coverreport: fetched comment %d (%d characters)\n", cm.ID, len(prev))
		} else {
			fmt.Fprintf(env.Stdout, "coverreport: no comment carries %s yet\n", *marker)
		}
		return os.WriteFile(*fetch, []byte(prev), 0o644)
	}

	data, err := os.ReadFile(*body)
	if err != nil {
		return err
	}
	text := string(data)
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "<!-- coverreport:") {
		return fmt.Errorf("%s does not start with a coverreport marker line; render writes one", *body)
	}
	if n := utf16Len(text); n > github.CommentLimit {
		return fmt.Errorf("%s is %d characters; GitHub's limit is %d (render keeps under it: was the file edited?)", *body, n, github.CommentLimit)
	}
	res, err := c.Upsert(ctx, *repo, pr, strings.TrimSpace(first), text)
	if err != nil {
		return err
	}
	if res.Created {
		fmt.Fprintf(env.Stdout, "coverreport: created %s\n", res.Comment.HTMLURL)
	} else {
		fmt.Fprintf(env.Stdout, "coverreport: updated %s\n", res.Comment.HTMLURL)
	}
	if res.Duplicates > 0 {
		fmt.Fprintf(env.Stderr, "coverreport: %d more comment(s) carry the same marker; the oldest was updated and the rest left alone\n", res.Duplicates)
	}
	if *prevOut != "" {
		return os.WriteFile(*prevOut, []byte(res.Previous), 0o644)
	}
	return nil
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
