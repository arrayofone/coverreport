# coverreport

One coverage gate for a repository whose test layers produce Go coverprofiles
and LCOV tracefiles (vitest, flutter, node --test all write LCOV). It merges
each layer's artifacts, applies a reviewed exclusion list, computes totals per
layer, per package and per glob, checks them against committed floors that
only ever ratchet up, computes patch coverage from a git diff, and writes one
versioned `report.json` that every renderer (PR comment, job summary,
annotations, HTML page) is built from.

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
data), and one job downloads them all and runs the reporter once:

```yaml
coverage:
  needs: [go-services, check, mobile]
  if: ${{ !cancelled() }}
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v5
      with: { fetch-depth: 2 }
    - uses: actions/download-artifact@v5
      with: { pattern: coverage-*, path: coverage-artifacts }
    # Any Go >= 1.25 on PATH: actions/setup-go, or the repo's own dev shell.
    - name: Analyze
      env:
        CR: github.com/DarrenBangsund/coverreport/cmd/coverreport@<pinned-sha>
      run: |
        go run "$CR" analyze --inputs coverage-artifacts --diff-base HEAD^1 --out report.json
        go run "$CR" text --report report.json >> "$GITHUB_STEP_SUMMARY"
        # --require only the layers whose collector actually ran this time.
        go run "$CR" check --report report.json --require go-unit,go-live
```

With the module private, `go run pkg@sha` needs `GOPRIVATE=github.com/DarrenBangsund/*`
and a token git can use; alternatively vendor a copy of the module at a pinned
commit.

Locally, the same three commands work over whatever artifacts are on disk;
`coverreport ratchet` then writes `coverage/floors.json` for review.

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

Layout: `cmd/coverreport` (entry point), `internal/cli` (flags, exit codes),
`internal/report` (the analysis and the report model), `internal/render/text`,
and one package per input: `gocov`, `lcov`, `diff`, `config`, `floors`,
`exclude`, `glob`, `paths`, `coverage` (the shared arithmetic).
