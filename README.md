# coverreport

One coverage gate for a repository whose test layers produce Go coverprofiles
and LCOV tracefiles (vitest, flutter, node --test all write LCOV). It merges
each layer's artifacts, applies a reviewed exclusion list, computes totals per
layer, per package and per glob, checks them against committed floors that
only ever ratchet up, computes patch coverage from a git diff, and writes one
versioned `report.json`. From that one report it renders the four GitHub
surfaces: a sticky PR comment, the job summary, up to ten annotations on
changed lines no test reaches, and a single self-contained HTML page (no
script, no network) with every changed file line by line and a treemap of
where the untested code lives.

It is a single Go module with **no dependencies** (standard library only), so
any repository can run it at a pinned commit:

```sh
go run github.com/arrayofone/coverreport/cmd/coverreport@<sha> check
```

The file formats (`coverage/config.json`, `coverage/floors.json`,
`coverage/exclude.txt`, `report.json`) are specified in
[SCHEMA.md](SCHEMA.md), completely enough to adopt them without reading the
code.

## Why it exists

The alternatives were priced in handipay's coverage audit (2026-09-28): SaaS
reporters cost money, need an org-level GitHub App and send data out of
GitHub; octocov merges Go and LCOV as lines, so Go's statement percentage
becomes a line percentage, and per-layer gating needs a config per layer.
What was wanted is small and exact: per-layer floors that block, a ratchet
that is a reviewed commit, patch coverage per layer, and every exclusion
printed with its size so nothing leaves a denominator quietly.

## Commands

```
coverreport analyze [flags]   compute report.json (to --out, default stdout)
coverreport check   [flags]   exit 1 if any gate fails; prints the failures
coverreport ratchet [flags]   raise floors.json to the current measurement
coverreport text    [flags]   print a report as plain text (--detail: packages, lowest files)
coverreport render  [flags]   write comment.md, summary.md, annotations.txt, report.html
coverreport comment [flags]   upsert the sticky PR comment (or --fetch the current one)
coverreport version
```

`check`, `ratchet` and `text` analyze afresh unless `--report` names a
`report.json` from an earlier `analyze`, which is how CI runs them (analyze
once, render and check the same report).

| Flag | Default | Meaning |
|:--|:--|:--|
| `--root` | `.` | repository root; config values are relative to it |
| `--config` | `coverage/config.json` | relative to `--root` unless absolute |
| `--inputs` | `--root` | where the layers' `inputs` globs are resolved (the directory CI downloaded the `coverage-*` artifacts into) |
| `--floors`, `--exclude` | from the config | override the files (paths relative to the working directory) |
| `--diff` | none | a unified git diff for patch coverage; `-` reads stdin |
| `--diff-base` / `--diff-head` | none / `HEAD` | run git for the diff instead (see below) |
| `--require` | none | layer ids that must have been measured, comma-separated or repeated. A required layer whose inputs matched nothing fails the check; any other unmeasured layer is "not affected" |
| `--repo`, `--pr`, `--sha`, `--base`, `--head-ref`, `--base-ref`, `--run-url` | from the environment | report metadata. Defaults: `$GITHUB_REPOSITORY`, the PR number in `$GITHUB_REF` (`refs/pull/<n>/merge`), `$GITHUB_SHA`, the resolved `--diff-base`, `$GITHUB_HEAD_REF`, `$GITHUB_BASE_REF`, and `$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID` |
| `--report` | none | `check`, `ratchet`, `text`: use this report instead of analyzing |
| `--out` | `-` | `analyze`: where to write the report |
| `--dry-run` | false | `ratchet`: print the changes, write nothing |
| `--prune` | false | `ratchet`: also delete stale package/glob floors |
| `--title`, `--head-sha` | from `$GITHUB_EVENT_PATH` | the PR's title and head commit (the `pull_request` payload carries both) |
| `--baseline` | none | a `report.json` from the base branch: a layer this run did not measure (its collector was path-filtered out) shows the base branch's numbers, labelled as such. Display only: nothing carried is checked or ratcheted |
| `--endpoints` | none | the endpoint registry's `endpoints.json`: its completeness rows join the report and its baseline violations fail the check. A path that does not exist is a warning |

`render` and `comment` take their own flags:

| Flag | Default | Meaning |
|:--|:--|:--|
| `render --report` | required | the `report.json` to render |
| `render --out` | required | directory for the four files (created) |
| `render --source-root` | none | a checkout of the measured commit. With it the surfaces quote the lines that never ran; without it they name and link them only |
| `render --previous` | none | the sticky comment being replaced (from `comment --fetch`): its hidden state is the push-by-push patch history |
| `render --artifact-url`, `--artifact-name` | none, `report.html` | where the page was uploaded, so the comment, summary and annotations can link it |
| `comment --body` | | upsert this file (render's `comment.md`), found by its first line, the marker |
| `comment --fetch` | | instead, write the current sticky comment's body to this file (empty when there is none) |
| `comment --previous` | none | with `--body`: also write the body it replaced |
| `comment --repo`, `--pr` | `$GITHUB_REPOSITORY`, from `$GITHUB_REF` | the pull request |
| `comment --api-url` | `$GITHUB_API_URL`, else `https://api.github.com` | GitHub Enterprise Server, or a test double |

`comment` reads its token from `GITHUB_TOKEN` and sends it only as the
`Authorization` header: it is never printed, and every error is scrubbed of it.
403, 404 and 401 answers come with what to change (the job's
`permissions:`, the flags, the token). Reads and updates are retried on a 5xx;
the create is not, so a timeout cannot post the comment twice.

`SOURCE_DATE_EPOCH`, when set, replaces the clock in `generated_at`, so a
report can be reproduced byte for byte.

### Exit codes

| Code | Meaning |
|:--|:--|
| 0 | success; for `check`, every gate passed |
| 1 | `check`: a gate failed (a floor broken, a floored metric with no data, a required layer missing, or a patch miss when `patch.blocking` is true) |
| 2 | usage, configuration or input error: nothing was judged |

`analyze`, `ratchet` and `text` never exit 1: the status is in the report.
`ratchet` never lowers a floor, including a floor that is currently failing.

### The diff

`--diff-base <rev>` runs exactly:

```sh
git -C <root> -c core.quotePath=false diff -U0 --no-color --no-ext-diff \
    --src-prefix=a/ --dst-prefix=b/ -M <rev> <diff-head>
```

The explicit prefixes and quoting make the parse immune to a user's
`diff.noprefix` / `diff.mnemonicPrefix` / `core.quotePath` settings. On a
`pull_request` workflow the checkout is the merge commit, so
`--diff-base HEAD^1` (with `fetch-depth: 2`) is the PR's change against its
base. A diff given with `--diff` may use any context size; combined diffs
(`diff --cc`) are refused.

## In CI

The collectors upload artifacts (`coverage-go`, `coverage-app`,
`coverage-mobile`, ...; JSON job outputs are not a safe channel for this
data), and one job downloads them all and runs this repository's composite
action. The action is the whole recipe in one step, so every repository that
adopts it runs the same one rather than a copy:

```yaml
- uses: arrayofone/coverreport@<commit-sha>
  with:
    require: go-unit,go-live
```

Pin a full commit SHA, never a branch or a tag, so a consumer's CI only
changes when it is bumped on purpose. The action builds coverreport from its
own checkout: the module is stdlib-only, so nothing is downloaded, and the
one thing it needs from the job is a Go toolchain on PATH
([below](#go-on-path)). Its steps are bash, and it is tested on Linux.

The steps, in order ([`action/run.sh`](action/run.sh) holds every command):

1. `go build ./cmd/coverreport` in the action's directory, with `GOWORK=off`,
   `GOTOOLCHAIN=local`, `GOPROXY=off`, `CGO_ENABLED=0` and the job's
   `GOFLAGS` cleared, so neither the workspace's go.work nor the job's Go
   settings can reach the build, and an older Go fails saying so instead of
   fetching a newer one.
2. `comment --fetch`: on a `pull_request` run, the current sticky comment,
   whose hidden state is the push-by-push history. A comment that cannot be
   read is a warning, and the history starts again.
3. `analyze`: `report.json`, from the artifacts, the `diff-base..HEAD` diff,
   `baseline`, `endpoints` and `require`. The pull request's title and head
   commit come from `$GITHUB_EVENT_PATH`.
4. `render`: the four surfaces, quoting the lines that never ran from the
   checkout.
5. `actions/upload-artifact` (pinned by commit): the page, unarchived so it
   opens in the browser, kept for `retention-days`.
6. `render` again with the page's URL, so the comment, the summary and every
   annotation link it. Then the annotations go to stdout, the sticky comment
   is upserted, and `summary.md` is appended to `$GITHUB_STEP_SUMMARY`. The
   comment is skipped, with a notice, on a fork's pull request (its token is
   read-only) and on any run that is not a `pull_request` one.
7. `check --require`: the gate. The step fails when a gate fails, with one
   error annotation per failed gate. A sticky comment that could not be
   posted fails it too, but only here, after the gate has run and everything
   else was published.

`action/action_test.go` runs `action.yml`'s steps as written (their env, their
order, the upload in the middle) against the CLI built from the same commit,
over `testdata/`: a pull request, a fork's, a push, every optional input, a
shallow checkout, no Go, the job's own Go settings, a comment the API refuses.
The action cannot drift from the commands it runs without that test failing.
Another CI system runs the same commands; `action/run.sh` is the reference.

### Inputs

| Input | Default | Meaning |
|:--|:--|:--|
| `config` | `coverage/config.json` | the config, relative to `root` unless absolute |
| `root` | `.` | the checkout of the commit measured (`--root`) |
| `artifacts` | `coverage-artifacts` | the directory the `coverage-*` artifacts were downloaded into (`--inputs`). One that does not exist is a warning: every layer is then not measured, and every required one fails |
| `source-root` | none | a checkout to quote the lines that never ran from; empty means `root` |
| `diff-base` | `HEAD^1` | patch coverage is `<diff-base>..HEAD`. `HEAD^1` is the merge commit's base, so check out with `fetch-depth: 2`; empty skips patch coverage |
| `diff` | none | a unified diff file to use instead; wins over `diff-base` |
| `baseline` | none | a `report.json` the base branch saved (`--baseline`). A path that does not exist yet is a notice |
| `endpoints` | none | the endpoint registry's `endpoints.json` (`--endpoints`) |
| `require` | none | layer ids that must have been measured, comma-separated; given to both `analyze` and `check` |
| `github-token` | `${{ github.token }}` | reads and writes the sticky comment (`pull-requests: write`). Only the `comment` command's environment carries it |
| `comment` | `true` | `false` skips the sticky comment and its fetch |
| `page-name` | none | the page's artifact name; empty means `coverreport-<PR number>.html`, or `coverreport-<run id>.html` outside a pull request |
| `retention-days` | `1` | how long the page is kept: an expired artifact is still billed until it is deleted |
| `out-dir` | none | where `report.json`, the four surfaces and `previous.md` go; empty means `$RUNNER_TEMP/coverreport` |

Paths are relative to the workspace, except `config`, which is relative to
`root`.

### Outputs

| Output | Meaning |
|:--|:--|
| `report` | the path of `report.json`: a file, never the report itself as a job output. Set once `analyze` has run, whatever the check says |
| `dir` | the directory holding `report.json`, `comment.md`, `summary.md`, `annotations.txt` and `report.html` |
| `page-url` | the uploaded page's URL |

### Go on PATH

The action needs Go 1.25 or newer on PATH, and its first step fails saying so
when there is none. On a hosted runner, `actions/setup-go` before it is
enough (the workflow below does that). A job whose tools come from a nix dev
shell has to export the shell, once, before the action: a composite action's
steps set their own `shell:`, so they never run inside `defaults.run.shell`.
Exporting the whole PATH brings the shell's git along for `diff-base` too:

```yaml
- name: Enter the dev shell (once)
  run: |
    # $PATH expands in the inner shell, the one nix develop starts.
    # shellcheck disable=SC2016
    nix develop .#go --command bash -c 'echo "PATH=$PATH" >> "$GITHUB_ENV"'
```

### A complete workflow

This is [`examples/coverage.yml`](examples/coverage.yml): one collector job,
then the coverage job, which also keeps the base branch's last passing report
as the baseline.

```yaml
# A complete workflow around the coverreport action: a collector job uploads
# a coverage-* artifact, and the coverage job downloads every one of them and
# runs the action. Replace <commit-sha> with the full commit you pin.
name: CI

on:
  pull_request:
  push:
    branches:
      - main

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      # coverage/config.json's go-unit layer reads coverage-go/unit.out.
      - run: go test -covermode=atomic -coverpkg=./... -coverprofile=unit.out ./...
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        with:
          name: coverage-go
          path: unit.out
          retention-days: 1

  coverage:
    needs:
      - test
    # Run even when a collector failed or was skipped: its layer is then
    # "not affected", and require names the layers that must have reported.
    if: ${{ !cancelled() }}
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write # the sticky comment
    steps:
      # The merge commit and its first parent: patch coverage is HEAD^1..HEAD.
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 2
      # The action builds coverreport with the Go on PATH (1.25 or newer).
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: stable
          cache: false
      - uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
        with:
          pattern: coverage-*
          path: coverage-artifacts
      # The base branch's last passing report: a layer this pull request did
      # not run shows its numbers, labelled as carried.
      - if: github.event_name == 'pull_request'
        uses: actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: coverreport-baseline/report.json
          key: coverreport-baseline-${{ github.event.pull_request.base.sha }}
          restore-keys: coverreport-baseline-
      - id: coverage
        uses: arrayofone/coverreport@<commit-sha>
        with:
          require: go-unit
          baseline: coverreport-baseline/report.json
      # On main, a report whose gates held becomes the next baseline. The
      # cache path is part of the cache's version, so it is copied to exactly
      # the path the restore above names.
      - if: github.event_name == 'push'
        run: mkdir -p coverreport-baseline && cp "$REPORT" coverreport-baseline/report.json
        env:
          REPORT: ${{ steps.coverage.outputs.report }}
      - if: github.event_name == 'push'
        uses: actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: coverreport-baseline/report.json
          key: coverreport-baseline-${{ github.sha }}
```

Notes:

- **Forks.** On a `pull_request` from a fork, `GITHUB_TOKEN` is read-only and
  cannot comment, so the action skips the comment with a notice. The summary,
  annotations, page and check still work.
- **The base branch's numbers.** A layer whose collector did not run on this
  PR shows as "not run". The workflow above saves each passing `main` run's
  `report.json` in the Actions cache and restores the newest into `baseline`
  on a pull request. The cache's `path` is part of its version, so the save
  and the restore must name byte-identical paths: that is why the report is
  copied to a fixed one first.
- **Permissions.** `pull-requests: write` for the comment, `contents: read`
  for the checkout. Downloading this run's own artifacts needs none.
- **Annotations** are workflow commands on stdout, so the job needs no
  `checks: write`.
- **The page** is its artifact: upload-artifact names an unarchived artifact
  after its file (the `name` input is ignored), which is why the action
  uploads a copy called `page-name`. The default stays clear of `coverage-*`,
  so a re-run's download step never pulls the first attempt's page in as an
  input. It needs a signed-in reader with access to the repository, like any
  artifact, and makes no network request, so no content-security policy can
  break it beyond its inline stylesheet.

Locally, the same commands work over whatever artifacts are on disk:
`coverreport render --report report.json --out /tmp/cov --source-root .` and
open `/tmp/cov/report.html`; `coverreport ratchet` writes
`coverage/floors.json` for review.

## Theming

The page takes a `brand` block from `coverage/config.json`: a name, two font
stacks and one colour per semantic token for each of the dark and light
schemes ([SCHEMA.md, "Brand"](SCHEMA.md#brand) lists them, with handipay's
Paper/Dim block as the example). Tokens name states, not colours: `ok` holds,
`caution` needs a test, `warning` is below a floor, `target` is a goal or the
line a link jumped to. Anything a brand leaves out takes the neutral default
(GitHub's Primer greys). GitHub markdown carries no colour, so the comment and
summary use only the name; their colour comes from GitHub's own `diff`
highlighter, which colours each HUD row by its first character.

## What it measures, exactly

The rules are in [SCHEMA.md, "How numbers are computed"](SCHEMA.md#how-numbers-are-computed).
The short version:

- **Go statements** are coverprofile blocks merged by exact position (the
  heavy duplication `-coverpkg=./...` produces is one block, covered if any
  binary ran it). The figure is exactly what `go test -cover` prints.
- **LCOV** lines, branches and functions come from the detail records
  (`DA`, `BRDA`, `FN`/`FNDA`); the summary records are recomputed, never
  trusted, exactly as `lcov --summary` does.
- **Floors** are compared in integer arithmetic (a check fails when measured
  < floor - tolerance) and written rounded down to one decimal.
- **Patch coverage** is per layer over added lines; Go lines are derived from
  blocks (a line is covered when any block on it ran; blank, comment-only and
  closing-bracket lines are dropped when the source is present).

### Checked against the reference tools

On handipay's real artifacts (2026-09-28 baseline):

| Input | coverreport | Reference |
|:--|:--|:--|
| `go-live.out` (1.5 MB, 20,344 blocks) | 21,717 / 28,594 = 75.95% | `go tool cover -func`: 75.9%; all 51 per-package figures equal `go test -cover`'s |
| `go-unit.out` | 19,204 / 28,594 = 67.16% | `go tool cover -func`: 67.1%; 51/51 packages equal |
| landing, `-coverpkg=./...` (2.3 MB, 25,017 block lines, 3,823 blocks) | 4,062 / 6,153 = 66.02% | `go tool cover -func`: 66.1%, because it only counts the 6,136 statements inside function declarations (4,055 / 6,136 = 66.09%); the 17 others are package-level function literals |
| `mobile-lcov.info` (173 files) | 14,721 / 16,053 lines = 91.70% | `lcov --summary`: 91.7% (14721 of 16053 lines) |

A full five-layer analysis of those artifacts (4 MB of profiles, the Go source
line filter over the whole checkout, a 62-file diff) takes about 0.15 s.

## Developing

```sh
go vet ./... && go test ./...
go test ./... -update   # rewrite testdata/golden after a deliberate change
```

`testdata/` holds real-world excerpts (five files of handipay's `go-live.out`,
three of its landing `-coverpkg` profile, four of its flutter tracefile) and a
small demo repository (`testdata/repo`, `testdata/artifacts`,
`testdata/change.diff`) whose coverprofiles came from a real `go test
-coverpkg=./...` run and whose diff is real `git diff` output. A golden diff is
a behaviour change: review it before regenerating.

The renderers' goldens are in `testdata/render/<state>/` (`comment.md`,
`summary.md`, `annotations.txt`, `report.html`), one directory per state a PR
can be in: `ok`, `fail`, `warn` (patch under target, informational),
`first-run` (no floors file), `carried` (a layer not run, carried from main,
rendered without a source checkout), `e2e` (a report-only layer reaching code
no gated layer does), `multi` (every kind of failure at once) and `large`
(rendered into small budgets to show the shortening). Each is built by the
real analysis over the fixture (`internal/render/rendertest`). The page
goldens carry a placeholder for the stylesheet, which is pinned once per brand
in `testdata/render/css/`.

The action's tests (`action/`) run `action.yml`'s steps in a small stand-in
for the Actions runner: they need `bash` and `git` on PATH, build the CLI the
way the action does, and serve the comments API from an `httptest` server.
`action.yml` and the example are read by a YAML subset parser there (the
module takes no dependencies), which refuses anything outside the subset
rather than half-reading it. Before a change to either file lands, also run
`actionlint examples/coverage.yml` and validate `action.yml` against the
published schema (`check-jsonschema --builtin-schema vendor.github-actions
action.yml`); neither tool is a dependency of the tests.

Layout: `action.yml` and `action/` (the composite action and its tests),
`examples/` (the consumer workflow the README embeds), `cmd/coverreport` (entry
point), `internal/cli` (flags, exit codes), `internal/report` (the analysis and
the report model), `internal/render/view` (every rendering decision, made
once), `internal/render/github` (comment, summary, annotations),
`internal/render/html` (the page and its treemap), `internal/render/text`,
`internal/ghapi` (the comment upsert), `internal/brand`, `internal/endpoints`,
`internal/source`, and one package per input: `gocov`, `lcov`, `diff`,
`config`, `floors`, `exclude`, `glob`, `paths`, `coverage` (the shared
arithmetic).

## License

MIT; see [LICENSE](LICENSE).
