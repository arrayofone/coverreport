// Package cli is coverreport's command line: analyze, check, ratchet, text.
//
// Exit codes are part of the interface CI scripts branch on:
//
//	0  success (check: every gate passed)
//	1  check: a gate failed (a floor broken, a required layer missing, or a
//	   blocking patch target missed)
//	2  usage, configuration or input error: nothing was judged
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/DarrenBangsund/coverreport/internal/config"
	"github.com/DarrenBangsund/coverreport/internal/diff"
	"github.com/DarrenBangsund/coverreport/internal/endpoints"
	"github.com/DarrenBangsund/coverreport/internal/exclude"
	"github.com/DarrenBangsund/coverreport/internal/floors"
	"github.com/DarrenBangsund/coverreport/internal/render/text"
	"github.com/DarrenBangsund/coverreport/internal/report"
)

// Exit codes.
const (
	ExitOK     = 0
	ExitFailed = 1
	ExitError  = 2
)

// Env is the process environment, injectable for tests.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	Now    func() time.Time
	// Git runs git with args in dir and returns stdout.
	Git func(dir string, args ...string) ([]byte, error)
	// HTTP is the client the comment command talks to GitHub with.
	HTTP *http.Client
	// Sleep waits between retries.
	Sleep func(time.Duration)
}

// OSEnv is the real environment.
func OSEnv() Env {
	return Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		Now:    time.Now,
		Git: func(dir string, args ...string) ([]byte, error) {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
			}
			return out, nil
		},
		HTTP:  &http.Client{Timeout: 30 * time.Second},
		Sleep: time.Sleep,
	}
}

const usage = `coverreport: one coverage gate for Go coverprofiles and LCOV.

Usage:
  coverreport analyze [flags]   compute report.json (to --out, default stdout)
  coverreport check   [flags]   exit 1 if any gate fails; prints the failures
  coverreport ratchet [flags]   raise floors.json to the current measurement
  coverreport text    [flags]   print a report as plain text
  coverreport render  [flags]   write comment.md, summary.md, annotations.txt, report.html
  coverreport comment [flags]   upsert the sticky PR comment (or --fetch the current one)
  coverreport version

check, ratchet and text analyze afresh unless --report names a report.json
from an earlier analyze. Run "coverreport <command> -h" for its flags.

Exit codes: 0 ok, 1 a gate failed (check), 2 usage/config/input error.
`

// Main runs the CLI and returns the exit code.
func Main(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return ExitError
	}
	cmd, rest := args[0], args[1:]
	var err error
	code := ExitOK
	switch cmd {
	case "analyze":
		err = runAnalyze(rest, env)
	case "check":
		code, err = runCheck(rest, env)
	case "ratchet":
		err = runRatchet(rest, env)
	case "text":
		err = runText(rest, env)
	case "render":
		err = runRender(rest, env)
	case "comment":
		err = runComment(rest, env)
	case "version":
		fmt.Fprintln(env.Stdout, version())
	case "help", "-h", "-help", "--help":
		fmt.Fprint(env.Stdout, usage)
	default:
		fmt.Fprintf(env.Stderr, "coverreport: unknown command %q\n\n%s", cmd, usage)
		return ExitError
	}
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK // -h: the flag package already printed the usage
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "coverreport %s: %v\n", cmd, err)
		return ExitError
	}
	return code
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		v := bi.Main.Version
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				v += " " + s.Value
			}
		}
		return "coverreport " + strings.TrimSpace(v)
	}
	return "coverreport (unknown version)"
}

// common holds the flags every analyzing command shares.
type common struct {
	root, config, inputs, floorsPath, excludePath string
	diffPath, diffBase, diffHead                  string
	require                                       listFlag
	report                                        string
	meta                                          report.Metadata
	pr                                            string
	baseline, endpoints                           string
}

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error {
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

func newFlags(name string, env Env, c *common, withReport bool) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.StringVar(&c.root, "root", ".", "repository root; config values are relative to it")
	fs.StringVar(&c.config, "config", "coverage/config.json", "config file (relative to --root unless absolute)")
	fs.StringVar(&c.inputs, "inputs", "", "directory the layers' input globs are resolved in (default: --root)")
	fs.StringVar(&c.floorsPath, "floors", "", "floors file, overriding the config's")
	fs.StringVar(&c.excludePath, "exclude", "", "exclude file, overriding the config's")
	fs.StringVar(&c.diffPath, "diff", "", "unified git diff for patch coverage (a file, or - for stdin)")
	fs.StringVar(&c.diffBase, "diff-base", "", "compute the diff with git: <diff-base>..<diff-head>")
	fs.StringVar(&c.diffHead, "diff-head", "HEAD", "head revision for --diff-base")
	fs.Var(&c.require, "require", "layer id(s) that must have been measured (comma-separated, repeatable)")
	fs.StringVar(&c.meta.Repo, "repo", "", "repository (default $GITHUB_REPOSITORY)")
	fs.StringVar(&c.pr, "pr", "", "pull request number (default: from $GITHUB_REF)")
	fs.StringVar(&c.meta.SHA, "sha", "", "commit measured (default $GITHUB_SHA)")
	fs.StringVar(&c.meta.Base, "base", "", "base commit (default: --diff-base resolved)")
	fs.StringVar(&c.meta.HeadRef, "head-ref", "", "head branch (default $GITHUB_HEAD_REF)")
	fs.StringVar(&c.meta.BaseRef, "base-ref", "", "base branch (default $GITHUB_BASE_REF)")
	fs.StringVar(&c.meta.RunURL, "run-url", "", "CI run URL (default: from $GITHUB_SERVER_URL, $GITHUB_REPOSITORY, $GITHUB_RUN_ID)")
	fs.StringVar(&c.meta.Title, "title", "", "pull request title (default: from the $GITHUB_EVENT_PATH payload)")
	fs.StringVar(&c.meta.HeadSHA, "head-sha", "", "pull request head commit (default: from the $GITHUB_EVENT_PATH payload)")
	fs.StringVar(&c.baseline, "baseline", "", "a report.json from the base branch; layers not measured here carry its numbers (display only)")
	fs.StringVar(&c.endpoints, "endpoints", "", "the endpoint registry's endpoints.json; a missing file is a warning, its baseline violations are failures")
	if withReport {
		fs.StringVar(&c.report, "report", "", "use this report.json instead of analyzing")
	}
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

// loaded is a config plus the files it points at.
type loaded struct {
	cfg         *config.Resolved
	floorsPath  string
	fl          *floors.Floors
	floorsFound bool
}

func (c *common) load() (*loaded, error) {
	cfgPath := c.config
	if !filepath.IsAbs(cfgPath) {
		cfgPath = filepath.Join(c.root, cfgPath)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	fp := c.floorsPath
	if fp == "" {
		fp = filepath.Join(c.root, filepath.FromSlash(cfg.Floors))
	} else {
		// The report names the file actually read, not the one the config
		// would have pointed at.
		cfg.Floors = filepath.ToSlash(fp)
	}
	fl, found, err := floors.Load(fp)
	if err != nil {
		return nil, err
	}
	return &loaded{cfg: cfg, floorsPath: fp, fl: fl, floorsFound: found}, nil
}

func (c *common) analyze(env Env, ld *loaded) (*report.Report, error) {
	ep := c.excludePath
	if ep == "" {
		ep = filepath.Join(c.root, filepath.FromSlash(ld.cfg.Exclude))
	} else {
		ld.cfg.Exclude = filepath.ToSlash(ep)
	}
	rules, err := exclude.Load(ep)
	if err != nil {
		return nil, err
	}
	meta, err := c.metadata(env)
	if err != nil {
		return nil, err
	}
	d, err := c.readDiff(env, &meta)
	if err != nil {
		return nil, err
	}
	var base *report.Report
	if c.baseline != "" {
		if base, err = report.Load(c.baseline); err != nil {
			return nil, fmt.Errorf("--baseline: %w", err)
		}
	}
	var eps *endpoints.File
	var epsMissing bool
	if c.endpoints != "" {
		eps, err = endpoints.Load(c.endpoints)
		if errors.Is(err, os.ErrNotExist) {
			// The registry's job may not have run (or landed) yet; the
			// rows are then omitted, loudly, rather than failing the gate
			// on a file nothing promised.
			eps, err, epsMissing = nil, nil, true
		}
		if err != nil {
			return nil, fmt.Errorf("--endpoints: %w", err)
		}
	}
	r, err := report.Analyze(report.Input{
		Root:        c.root,
		InputsDir:   c.inputs,
		Config:      ld.cfg,
		Floors:      ld.fl,
		FloorsFound: ld.floorsFound,
		Excludes:    rules,
		Diff:        d,
		Metadata:    meta,
		Require:     c.require,
		Now:         now(env),

		Baseline:      base,
		BaselineFile:  c.baseline,
		Endpoints:     eps,
		EndpointsFile: c.endpoints,
	})
	if err == nil && epsMissing {
		r.Warnings = append(r.Warnings, fmt.Sprintf("--endpoints %s does not exist; endpoint completeness is omitted from this report", c.endpoints))
	}
	return r, err
}

// now honours SOURCE_DATE_EPOCH, so a report can be reproduced byte for byte.
func now(env Env) time.Time {
	if s := env.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return env.Now()
}

func (c *common) metadata(env Env) (report.Metadata, error) {
	m := c.meta
	def := func(v *string, key string) {
		if *v == "" {
			*v = env.Getenv(key)
		}
	}
	def(&m.Repo, "GITHUB_REPOSITORY")
	def(&m.SHA, "GITHUB_SHA")
	def(&m.HeadRef, "GITHUB_HEAD_REF")
	def(&m.BaseRef, "GITHUB_BASE_REF")
	if m.RunURL == "" {
		server, repo, run := env.Getenv("GITHUB_SERVER_URL"), env.Getenv("GITHUB_REPOSITORY"), env.Getenv("GITHUB_RUN_ID")
		if server != "" && repo != "" && run != "" {
			m.RunURL = fmt.Sprintf("%s/%s/actions/runs/%s", strings.TrimSuffix(server, "/"), repo, run)
		}
	}
	if ev := env.Getenv("GITHUB_EVENT_PATH"); ev != "" && (m.Title == "" || m.HeadSHA == "") {
		// The pull_request payload is the only place Actions exposes the
		// PR's title and head commit. A payload that cannot be read is not
		// an error: the title is decoration.
		if data, err := os.ReadFile(ev); err == nil {
			var p struct {
				PullRequest *struct {
					Title string `json:"title"`
					Head  struct {
						SHA string `json:"sha"`
					} `json:"head"`
				} `json:"pull_request"`
			}
			if json.Unmarshal(data, &p) == nil && p.PullRequest != nil {
				if m.Title == "" {
					m.Title = p.PullRequest.Title
				}
				if m.HeadSHA == "" {
					m.HeadSHA = p.PullRequest.Head.SHA
				}
			}
		}
	}
	pr := c.pr
	if pr == "" {
		// refs/pull/<n>/merge (pull_request) or refs/pull/<n>/head.
		if ref := env.Getenv("GITHUB_REF"); strings.HasPrefix(ref, "refs/pull/") {
			pr = strings.SplitN(strings.TrimPrefix(ref, "refs/pull/"), "/", 2)[0]
		}
	}
	if pr != "" {
		n, err := strconv.Atoi(pr)
		if err != nil || n <= 0 {
			return m, fmt.Errorf("--pr %q is not a pull request number", pr)
		}
		m.PR = n
	}
	return m, nil
}

func (c *common) readDiff(env Env, meta *report.Metadata) ([]diff.File, error) {
	switch {
	case c.diffPath != "" && c.diffBase != "":
		return nil, errors.New("--diff and --diff-base are mutually exclusive")
	case c.diffPath == "-":
		return diff.Parse(env.Stdin)
	case c.diffPath != "":
		f, err := os.Open(c.diffPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return diff.Parse(f)
	case c.diffBase != "":
		out, err := env.Git(c.root, "-c", "core.quotePath=false", "diff", "-U0", "--no-color", "--no-ext-diff",
			"--src-prefix=a/", "--dst-prefix=b/", "-M", c.diffBase, c.diffHead)
		if err != nil {
			return nil, err
		}
		if meta.Base == "" {
			if sha, err := env.Git(c.root, "rev-parse", "--verify", c.diffBase+"^{commit}"); err == nil {
				meta.Base = strings.TrimSpace(string(sha))
			}
		}
		if meta.SHA == "" {
			if sha, err := env.Git(c.root, "rev-parse", "--verify", c.diffHead+"^{commit}"); err == nil {
				meta.SHA = strings.TrimSpace(string(sha))
			}
		}
		return diff.Parse(bytes.NewReader(out))
	}
	return nil, nil
}

// reportFor returns the --report file if given, else a fresh analysis.
func (c *common) reportFor(env Env, ld *loaded) (*report.Report, error) {
	if c.report == "" {
		return c.analyze(env, ld)
	}
	r, err := report.Load(c.report)
	if err != nil {
		return nil, err
	}
	for _, id := range c.require {
		if err := r.Require(id, ld.cfg); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func runAnalyze(args []string, env Env) error {
	var c common
	fs := newFlags("analyze", env, &c, false)
	out := fs.String("out", "-", "where to write report.json (- for stdout)")
	if err := parse(fs, args); err != nil {
		return err
	}
	ld, err := c.load()
	if err != nil {
		return err
	}
	r, err := c.analyze(env, ld)
	if err != nil {
		return err
	}
	data, err := r.Marshal()
	if err != nil {
		return err
	}
	if *out == "-" {
		_, err = env.Stdout.Write(data)
		return err
	}
	return os.WriteFile(*out, data, 0o644)
}

func runCheck(args []string, env Env) (int, error) {
	var c common
	fs := newFlags("check", env, &c, true)
	if err := parse(fs, args); err != nil {
		return ExitError, err
	}
	ld, err := c.load()
	if err != nil {
		return ExitError, err
	}
	r, err := c.reportFor(env, ld)
	if err != nil {
		return ExitError, err
	}
	if r.Status == report.StatusPass {
		fmt.Fprintf(env.Stdout, "coverreport: pass (%d layer(s); patch %s)\n", len(r.Layers), r.Patch.Status)
		return ExitOK, nil
	}
	fmt.Fprintf(env.Stdout, "coverreport: FAIL, %d gate(s) failed:\n", len(r.Failures))
	for _, f := range r.Failures {
		fmt.Fprintf(env.Stdout, "  - %s\n", f.Message)
	}
	return ExitFailed, nil
}

func runRatchet(args []string, env Env) error {
	var c common
	fs := newFlags("ratchet", env, &c, true)
	dry := fs.Bool("dry-run", false, "print what would change; write nothing")
	prune := fs.Bool("prune", false, "also delete stale package/glob floors (rows that match nothing measured)")
	if err := parse(fs, args); err != nil {
		return err
	}
	ld, err := c.load()
	if err != nil {
		return err
	}
	r, err := c.reportFor(env, ld)
	if err != nil {
		return err
	}
	changes := ld.fl.Apply(r.Ratchet)
	pruned := 0
	if *prune {
		for _, s := range r.StaleFloors {
			if s.Scope == floors.ScopePackage || s.Scope == floors.ScopeGlob {
				ld.fl.Delete(s)
				pruned++
			}
		}
	}
	for _, ch := range changes {
		from := "new"
		if ch.From != nil {
			from = floors.FormatFloor(*ch.From)
		}
		fmt.Fprintf(env.Stdout, "%s %s %s: %s -> %s\n", ch.Scope, ch.Layer, strings.TrimSpace(ch.Key+" "+ch.Metric), from, floors.FormatFloor(ch.To))
	}
	if pruned > 0 {
		fmt.Fprintf(env.Stdout, "pruned %d stale floor entr(ies)\n", pruned)
	}
	if len(changes) == 0 && pruned == 0 {
		fmt.Fprintln(env.Stdout, "coverreport: no floor can rise; floors unchanged")
		return nil
	}
	if *dry {
		fmt.Fprintf(env.Stdout, "dry run: %s not written\n", ld.floorsPath)
		return nil
	}
	if err := ld.fl.Write(ld.floorsPath); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "wrote %s (%d raised or added, %d pruned)\n", ld.floorsPath, len(changes), pruned)
	return nil
}

func runText(args []string, env Env) error {
	var c common
	fs := newFlags("text", env, &c, true)
	detail := fs.Bool("detail", false, "also print every package row and each layer's lowest files")
	if err := parse(fs, args); err != nil {
		return err
	}
	var r *report.Report
	var err error
	if c.report != "" {
		// A saved report renders without a config.
		if r, err = report.Load(c.report); err != nil {
			return err
		}
	} else {
		ld, err := c.load()
		if err != nil {
			return err
		}
		if r, err = c.analyze(env, ld); err != nil {
			return err
		}
	}
	return text.Render(env.Stdout, r, text.Options{Detail: *detail})
}
