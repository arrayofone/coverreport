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
go run github.com/DarrenBangsund/coverreport/cmd/coverreport@<sha> check
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
data), and one job downloads them all and runs the reporter:

```yaml
coverage:
  needs: [go-services, check, mobile]
  # Run even when a collector was path-filtered out: its layer is then
  # "not affected", and --require names the ones that must have reported.
  if: ${{ !cancelled() }}
  runs-on: ubuntu-latest
  permissions:
    contents: read
    pull-requests: write # the sticky comment
    actions: read        # download-artifact
  env:
    CR: github.com/DarrenBangsund/coverreport/cmd/coverreport@<pinned-sha>
    GITHUB_TOKEN: ${{ github.token }}
  steps:
    - uses: actions/checkout@v5
      with: { fetch-depth: 2 } # the merge commit and its first parent
    - uses: actions/setup-go@v6 # any Go >= 1.25; a repo's own dev shell works too
      with: { go-version: stable }
    - uses: actions/download-artifact@v5
      with: { pattern: coverage-*, path: coverage-artifacts }

    - name: Analyze
      run: |
        go run "$CR" analyze --inputs coverage-artifacts --diff-base HEAD^1 \
          --endpoints coverage-artifacts/coverage-endpoints/endpoints.json --out report.json

    # The push history lives in the sticky comment itself: read it back first.
    - name: Previous comment
      if: github.event_name == 'pull_request'
      run: go run "$CR" comment --fetch previous.md

    - name: Render
      run: |
        touch previous.md
        go run "$CR" render --report report.json --out coverage-out --source-root . --previous previous.md

    - name: Upload the page
      id: page
      uses: actions/upload-artifact@v7
      with:
        name: coverage-${{ github.event.pull_request.number || github.run_id }}.html
        path: coverage-out/report.html
        archive: false     # a single HTML file opens in the browser
        retention-days: 1  # an expired artifact still bills until deleted

    # Render again now that the page has a URL, so everything links it.
    - name: Publish
      run: |
        go run "$CR" render --report report.json --out coverage-out --source-root . --previous previous.md \
          --artifact-url "${{ steps.page.outputs.artifact-url }}" --artifact-name "coverage-${{ github.event.pull_request.number || github.run_id }}.html"
        cat coverage-out/summary.md >> "$GITHUB_STEP_SUMMARY"
        cat coverage-out/annotations.txt
        if [ "${{ github.event_name }}" = pull_request ]; then
          go run "$CR" comment --body coverage-out/comment.md
        fi

    # The gate itself; --require only the layers whose collector ran.
    - name: Check
      run: go run "$CR" check --report report.json --require go-unit,go-live
```

Notes:

- **Forks.** On a `pull_request` from a fork, `GITHUB_TOKEN` is read-only and
  cannot comment; the comment step then fails with a message saying so. The
  summary, annotations, page and check still work.
- **The base branch's numbers.** A layer whose collector did not run on this
  PR shows as "not run". To show main's numbers instead, save the
  `report.json` of each main run (an `actions/cache` entry keyed by the
  commit, restored with a `restore-keys` prefix) and pass it as
  `analyze --baseline`.
- **Annotations** are workflow commands on stdout, so the job needs no
  `checks: write`.
- **The page** needs a signed-in reader with access to the repository, like
  any artifact. It makes no network request, so no content-security policy
  can break it beyond its inline stylesheet.
- With the module private, `go run pkg@sha` needs
  `GOPRIVATE=github.com/DarrenBangsund/*` and a token git can use;
  alternatively vendor a copy of the module at a pinned commit.

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

Layout: `cmd/coverreport` (entry point), `internal/cli` (flags, exit codes),
`internal/report` (the analysis and the report model), `internal/render/view`
(every rendering decision, made once), `internal/render/github` (comment,
summary, annotations), `internal/render/html` (the page and its treemap),
`internal/render/text`, `internal/ghapi` (the comment upsert), `internal/brand`,
`internal/endpoints`, `internal/source`, and one package per input: `gocov`,
`lcov`, `diff`, `config`, `floors`, `exclude`, `glob`, `paths`, `coverage`
(the shared arithmetic).

## License

MIT; see [LICENSE](LICENSE).
