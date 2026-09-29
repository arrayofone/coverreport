# coverreport file formats

This is the reference for the files coverreport reads and writes. It is
written so a repository can adopt them without reading the code: every key,
every default, every rule the tool enforces, and the exact arithmetic behind a
pass or a fail.

| File | Who writes it | Purpose |
|:--|:--|:--|
| `coverage/config.json` | a human | what the layers are, where their artifacts are, what each one owns, targets |
| `coverage/floors.json` | the ratchet (`coverreport ratchet`), reviewed in a commit | the minimum each layer, package and glob may fall to |
| `coverage/exclude.txt` | a human | files taken out of every denominator, each with a reason |
| `report.json` | `coverreport analyze` | everything a renderer (PR comment, job summary, HTML page) needs |
| `endpoints.json` | the endpoint registry (another tool; optional input) | per endpoint, the rubric classes each test layer proves |
| `comment.md`, `summary.md`, `annotations.txt`, `report.html` | `coverreport render` | the four surfaces a CI job publishes |

All paths inside these files are **repo-relative, slash-separated and clean**
(`libs/go/x`, never `./libs/go/x/`, never absolute). The config's `floors` and
`exclude` keys are resolved against `--root`; the layers' `inputs` against
`--inputs` (default: `--root`).

Contents: [Globs](#globs) · [config.json](#coverageconfigjson) ·
[floors.json](#coveragefloorsjson) · [exclude.txt](#coverageexcludetxt) ·
[How numbers are computed](#how-numbers-are-computed) ·
[report.json](#reportjson) · [endpoints.json](#endpointsjson-input) ·
[Rendered surfaces](#rendered-surfaces)

---

## Globs

One glob dialect is used everywhere (config `include`/`exclude`/`globs`/`sets`/
`inputs`, `floors.json` glob keys, `exclude.txt`). It is doublestar
(github.com/bmatcuk/doublestar) semantics:

| Syntax | Meaning | Example |
|:--|:--|:--|
| `**` (a whole segment) | zero or more directories | `src/**/*.ts` matches `src/a.ts` and `src/x/y/a.ts` |
| `*` | any run of characters inside one segment, never `/`; dotfiles included | `libs/go/svc/*/main.go` |
| `?` | one character | `file?.go` |
| `[a-z]`, `[^a-z]` | a character class | `v[0-9].go` |
| `{a,b}` | alternatives; nestable; may contain `/` | `src/app/**/{page,layout}.tsx`, `src/{lib,app/api}/**` |
| `\x` | a literal `x` | `lit\*.go` |

Rules: no negation; no leading `/`; no `.` or `..` segments; no empty segments
(`a//b`); no trailing `/` (write `dir/**`); no whitespace (use `?` for a
space); at most 256 alternatives after brace expansion. `a**b` inside a
segment is just two `*`. A leading `./` is tolerated.

---

## coverage/config.json

```json
{
  "version": 1,
  "floors": "coverage/floors.json",
  "exclude": "coverage/exclude.txt",
  "go_modules": {
    "github.com/handixyz/handipay/libs/go": "libs/go",
    "github.com/handixyz/handipay/apps/landing": "apps/landing"
  },
  "sets": {
    "sql-adapters": ["libs/go/**/store_pg.go", "libs/go/**/*_pg.go"]
  },
  "patch": { "target": 80, "min_lines": 5, "blocking": false },
  "lowest_files": 25,
  "layers": [
    {
      "id": "go-unit",
      "label": "Go unit (logic)",
      "description": "go test -covermode=atomic -coverpkg=./..., LIVE_TEST_DATABASE_URL unset",
      "format": "go",
      "inputs": ["coverage-go/go-unit.out"],
      "include": ["libs/go/**"],
      "exclude": ["@sql-adapters"],
      "metrics": ["statements"],
      "targets": { "statements": 85 },
      "packages": { "floors": true, "min_size": 0, "targets": { "statements": 80 } },
      "globs": [
        { "glob": "libs/go/settlement/**", "label": "settlement", "targets": { "statements": 90 } }
      ]
    },
    {
      "id": "go-sql",
      "label": "Go SQL adapters (Postgres)",
      "format": "go",
      "inputs": ["coverage-go/go-live.out"],
      "include": ["@sql-adapters"],
      "metrics": ["statements"],
      "targets": { "statements": 80 }
    },
    {
      "id": "go-e2e",
      "label": "Go e2e (10 services)",
      "format": "go",
      "report_only": true,
      "inputs": ["coverage-go-e2e/*.out"],
      "metrics": ["statements"]
    },
    {
      "id": "app",
      "label": "App (vitest)",
      "format": "lcov",
      "inputs": ["coverage-app/lcov.info"],
      "metrics": ["lines", "branches", "functions"],
      "targets": { "lines": 80, "branches": 75, "functions": 80 },
      "paths": { "prefix": "apps/app", "strip": ["/home/runner/work/handipay/handipay"] },
      "globs": [
        { "glob": "apps/app/src/lib/**", "targets": { "lines": 90, "branches": 85 } },
        { "glob": "apps/app/src/app/**/{page,layout}.tsx" }
      ]
    },
    {
      "id": "mobile",
      "label": "Mobile (flutter)",
      "format": "lcov",
      "inputs": ["coverage-mobile/lcov.info"],
      "metrics": ["lines"],
      "targets": { "lines": 90 },
      "paths": { "prefix": "apps/mobile" }
    }
  ]
}
```

Unknown keys anywhere are an error (a misspelt `"exlcude"` must not silently
widen a layer), as is trailing data after the object.

### Top level

| Key | Type | Default | Meaning |
|:--|:--|:--|:--|
| `version` | int | required | must be `1` |
| `floors` | path | `coverage/floors.json` | the floors file, repo-relative |
| `exclude` | path | `coverage/exclude.txt` | the exclusion list, repo-relative |
| `go_modules` | object | `{}` | Go module import path to repo-relative directory. Optional: see [Go paths](#go-paths) |
| `sets` | object | `{}` | named glob lists, referenced from a layer's `include`/`exclude` as `"@name"`. Names match `^[a-z0-9][a-z0-9_-]*$`; a set may not be empty |
| `patch.target` | number 0-100 | `80` | patch coverage target, percent, inclusive |
| `patch.min_lines` | int >= 0 | `5` | a layer with fewer changed coverable lines than this is `exempt` |
| `patch.blocking` | bool | `false` | whether a patch miss is a failure (exit 1) or informational |
| `patch.informational_until` | `YYYY-MM-DD` | none | when a non-blocking patch gate is planned to start blocking. Shown in the rendered copy ("informational until 2026-10-05"); it changes nothing by itself, `blocking` still decides |
| `lowest_files` | int >= 0 | `25` | how many files each layer's `lowest_files` list keeps |
| `layers` | array | required, non-empty | the rows of the table, in display order |
| `brand` | object | the neutral theme | how the rendered surfaces look; see [Brand](#brand). Presentation only |
| `ratchet_command` | string | `coverreport ratchet` | what the surfaces tell a reader to run to raise the floors (`make coverage-ratchet`). One line, no backticks |

Why `sets`: a boundary between two layers (the SQL adapter files leave the
unit row and form their own) must be written once. Two hand-copied lists drift,
and a file excluded from one row but missing from the other silently vanishes
from both.

### Layer

| Key | Type | Default | Meaning |
|:--|:--|:--|:--|
| `id` | string | required | `^[a-z0-9][a-z0-9_-]*$`, unique. The key in `floors.json` and in `--require` |
| `label` | string | the id | display name |
| `description` | string | | how it is measured (flags, environment); carried to the report for "how measured" |
| `format` | `"go"` or `"lcov"` | required | the input format |
| `inputs` | globs | required, non-empty | artifact files, resolved under `--inputs`. Several files (or a glob matching several) are merged. **No match at all means the layer was not measured** (`not_measured`, not a failure unless `--require`d); a glob that matches nothing while a sibling matches is a warning |
| `include` | globs / `@set` | all files | if present, a file must match one to belong to the layer |
| `exclude` | globs / `@set` | none | files matching any are out of the layer's scope (reported with sizes, see `scope` in the report) |
| `metrics` | array | required | `go`: `["statements"]` only. `lcov`: any of `lines`, `branches`, `functions`. The first is the layer's **primary** metric (sorting, `min_size`, sizes). No duplicates |
| `targets` | object | `{}` | metric to percent. Informational goal shown next to the floor; never gates |
| `report_only` | bool | `false` | measured and shown, never checked, never ratcheted, never in the patch gate or the overall patch union (for e2e rows) |
| `paths.prefix` | dir | `""` | lcov only: the repo-relative directory the tool ran in; see [LCOV paths](#lcov-paths) |
| `paths.strip` | absolute paths | `[]` | lcov only: absolute prefixes to remove from SF paths (the collector job's checkout directory) |
| `packages.floors` | bool | `false` | a ratchet creates a floor for every directory of the layer |
| `packages.min_size` | int >= 0 | `0` | directories with fewer primary-metric units than this get no NEW floor (existing floors are still checked) |
| `packages.targets` | object | `{}` | a target applied to every directory row |
| `globs[]` | array | `[]` | aggregate rows: `{ "glob": ..., "label"?: ..., "targets"?: {...} }`. Each is always reported and, when measured, ratcheted |
| `short_label` | string, at most 16 characters | the id, dashes as spaces | the name in tight places: a gauge, the 13-column HUD cell (`go unit+pg`). A layer with several metrics shows as `<short_label> <metric>` (`app lines`, `app branches`); one line under a gauge holds 16 characters, so a longer one wraps onto two, and the config warns |
| `group` | string | the id up to its first dash | adjacent layers with the same group sit under one heading on the page (`go-unit`, `go-sql`, `go-live` and `go-e2e` all default to `go`) |
| `treemap` | bool | `false` | draw this layer's packages as the page's treemap. With no layer set, the measured gated layer with the largest primary-metric total and at least two packages is drawn |

Validation errors (all fatal, exit 2): an unsupported version; an unknown key;
a bad or duplicate id; an unknown format; a metric the format cannot measure;
a target for a metric the layer does not measure, or outside 0-100; an invalid
glob; an unknown `@set`; `paths` on a go layer; a relative `paths.strip`; a
`paths.prefix` leaving the repository; `report_only` with `packages.floors`; a
glob row listed twice; a `label`, `short_label` or `group` containing `|`, a
backtick, `<`, `>` or a newline; a `short_label` over 16 characters; a
`patch.informational_until` that is not a date; a `brand` that fails the rules
below.

Warnings (never fatal; they join the report's `warnings`): a layer with
several metrics whose `<short_label> <longest metric>` is over 16 characters,
named with the short_label length that would fit (`integration functions`
wraps under its gauge; `integ functions` does not).

### Brand

```json
"brand": {
  "name": "handipay",
  "mono": "\"Intel One Mono\", ui-monospace, \"SF Mono\", SFMono-Regular, Menlo, Consolas, \"DejaVu Sans Mono\", monospace",
  "sans": "\"IBM Plex Sans\", system-ui, -apple-system, \"Segoe UI\", sans-serif",
  "dark": {
    "bg": "#0c0e0c", "panel": "#121512", "glass": "#080a08",
    "bezel": "rgba(255, 255, 255, 0.085)", "bezel-2": "rgba(255, 255, 255, 0.16)",
    "ink": "#ecefe9", "ink-soft": "#8d958c", "ink-ghost": "#59615a", "fill": "#d9ded6", "mark": "#ecefe9",
    "ok": "#00e055", "caution": "#ffb627", "warning": "#ff5a4e", "target": "#5ad8ff",
    "t-ok": "rgba(0, 224, 85, 0.075)", "t-caution": "rgba(255, 182, 39, 0.14)",
    "t-warning": "rgba(255, 90, 78, 0.14)", "t-target": "rgba(90, 216, 255, 0.16)",
    "grid-a": "rgba(255, 255, 255, 0.022)", "grid-b": "transparent",
    "tm-ok": "#171b17", "tm-ok-fg": "#8d958c", "tm-h1": "#3b2f14", "tm-h1-fg": "#ecefe9",
    "tm-h2": "#7a5a16", "tm-h2-fg": "#ecefe9", "tm-h3": "#ffb627", "tm-h3-fg": "#0c0e0c"
  },
  "light": {
    "bg": "#f6f4ee", "panel": "#fffefa", "glass": "#fffefa",
    "bezel": "#e3dfd2", "bezel-2": "#cfcaba",
    "ink": "#21251f", "ink-soft": "#6e756a", "ink-ghost": "#9aa093", "fill": "#2e342c", "mark": "#21251f",
    "ok": "#0b7a3c", "caution": "#9a5a00", "warning": "#b3261e", "target": "#0a6a84",
    "t-ok": "rgba(11, 122, 60, 0.07)", "t-caution": "rgba(214, 140, 0, 0.16)",
    "t-warning": "rgba(179, 38, 30, 0.09)", "t-target": "rgba(10, 106, 132, 0.11)",
    "grid-a": "rgba(92, 122, 100, 0.055)", "grid-b": "rgba(92, 122, 100, 0.035)",
    "tm-ok": "#efece3", "tm-ok-fg": "#6e756a", "tm-h1": "#f3e0bb", "tm-h1-fg": "#21251f",
    "tm-h2": "#deb46a", "tm-h2-fg": "#21251f", "tm-h3": "#9a5a00", "tm-h3-fg": "#fffefa"
  }
}
```

That is handipay's block: the Paper (light) and Dim (dark) tokens of its
landing site, with the three state colours Paper/Dim does not define (amber,
red, cyan) added. Every key is optional; a missing token takes the neutral
default's value for the same scheme (GitHub's own Primer greys and state
colours), so a repository can restyle three colours and keep the rest.
Markdown carries no colour, so the comment and summary use only `name`.

| Key | Meaning |
|:--|:--|
| `name` | the wordmark and the first word of every heading ("handipay coverage"), used exactly as written. Default: the repository's short name, else `coverage`. At most 40 characters, no markdown or HTML metacharacters |
| `mono`, `sans` | CSS `font-family` lists (family names, quotes, commas, spaces only). The page embeds no fonts: name faces readers may have and end in a generic family |
| `dark`, `light` | token name to colour. Dark is the page's default scheme; light applies under `prefers-color-scheme: light` |

A colour is a `#rgb`/`#rgba`/`#rrggbb`/`#rrggbbaa` literal, one of the CSS
colour functions `rgb() rgba() hsl() hsla() hwb() lab() lch() oklab() oklch()`
with only numbers, units, `%`, `/`, commas and spaces inside, or
`transparent`/`currentColor`. Anything else (`var()`, `url()`, `color-mix()`,
a `;` or `}`) is refused: the values are written into the page's stylesheet.

| Token | Colours |
|:--|:--|
| `bg`, `panel`, `glass` | the page, the two framed displays and tables, the gauge faces and code blocks |
| `bezel`, `bezel-2` | hairlines and borders, light and strong |
| `ink`, `ink-soft`, `ink-ghost` | text, secondary text, scales and decoration (never body text) |
| `fill` | a gauge's "now" tape before a state recolours it |
| `mark` | the wordmark |
| `ok`, `caution`, `warning`, `target` | the four states: holds, needs a test, below a floor, a goal or the line a link jumped to. Colour means state and nothing else |
| `t-ok`, `t-caution`, `t-warning`, `t-target` | translucent washes of the four (listing rows, the annunciator, `:target`) |
| `grid-a`, `grid-b` | the canvas grid (dark: a fine masked grid; light: 120/24 px graph paper) |
| `tm-ok`, `tm-h1`, `tm-h2`, `tm-h3` and each `-fg` | treemap tiles at or above target, up to 10, 10 to 25, and more than 25 points below it, and their text |

### Go paths

A coverprofile names files by import path
(`github.com/org/repo/libs/go/svc/work/router.go`). The module owning the
**longest** matching import-path prefix (on a `/` boundary) decides the
directory. The module map is `go_modules` plus, underneath it, what is
discovered at `--root`: every `use` directory of `go.work` (its `go.mod`'s
`module` line), or, with no `go.work`, the root `go.mod`. Configured entries
win on conflict. Absolute file names (and the `_/abs/path` form) are accepted
when under `--root`. A file that maps nowhere is dropped with a warning naming
one example.

### LCOV paths

An SF path is resolved by the first rule that applies:

1. **relative**: `prefix` joined with the path, cleaned (`lib/api/edge.dart`
   with prefix `apps/mobile` is `apps/mobile/lib/api/edge.dart`). A path that
   climbs out of the repository is dropped;
2. **absolute under `--root`**: made relative to it;
3. **absolute under a `paths.strip` entry**: the entry removed;
4. **absolute containing `/<prefix>/`**: everything from the LAST such
   occurrence on. This is what lets an artifact written in another CI job's
   checkout (`/home/runner/work/x/x/apps/app/src/a.ts`, or a scratch directory)
   resolve without configuring where that checkout was. Needs a non-empty
   prefix.

Anything else is dropped with a warning (a tool's own sources, e.g. the Flutter
SDK, land here).

---

## coverage/floors.json

```json
{
  "version": 1,
  "tolerance_pts": 0.1,
  "layers": {
    "app": { "branches": 65.7, "functions": 66.1, "lines": 72.0 },
    "go-unit": { "statements": 83.2 }
  },
  "packages": {
    "go-unit": {
      "libs/go/accountsync": { "statements": 47.0 },
      "libs/go/settlement": { "statements": 82.4 }
    }
  },
  "globs": {
    "app": {
      "apps/app/src/lib/**": { "branches": 88.7, "lines": 92.5 }
    }
  }
}
```

| Key | Meaning |
|:--|:--|
| `version` | must be `1` |
| `tolerance_pts` | required, >= 0, at most two decimals. How far a measurement may sit below its floor before it fails |
| `layers.<layer-id>.<metric>` | a layer floor |
| `packages.<layer-id>.<dir>.<metric>` | a directory floor (dir is the exact repo-relative directory holding the files, not recursive) |
| `globs.<layer-id>.<glob>.<metric>` | a glob floor, over every file of the layer matching the glob |

Every value is a percentage in 0-100 with **at most one decimal**; unknown
metrics, unknown keys and more decimals are errors. A missing file is not an
error: nothing is checked (the report says `floors_found: false`) and the first
ratchet creates it. `metric` is one of `lines`, `statements`, `branches`,
`functions`; Go layers use `statements` only.

**Check.** A metric fails when `measured < floor - tolerance_pts`. The
comparison is exact integer arithmetic, not floating point: with floor and
tolerance in hundredths of a point, it fails when
`covered * 10000 < (floor*100 - tolerance*100) * total`. 777/1000 against a
77.8 floor with 0.1 tolerance passes; 7769/10000 fails.

**Floors are rounded down.** A ratchet writes `floor(covered * 1000 / total) /
10`: 2/3 is 66.6, never 66.7, and 99.99% is 99.9, never 100.0. A floor is
therefore never above the measurement it was taken from, so the ratchet's own
commit always passes its check.

**Ratchet rules.**
- A floor only ever rises: the new value is the larger of the file's current
  value and the rounded-down measurement. This holds even when ratcheting from
  a saved report older than the floors file.
- New entries are created for: every metric of every measured, non-report-only
  layer; every directory of a layer with `packages.floors` (and at least
  `min_size` primary units); every configured `globs` row. A metric with no
  data (total 0) gets no entry.
- Report-only and unmeasured layers are never touched. A failing floor is
  never touched (there is nothing above it to propose).
- An entry whose package or glob no longer matches any file of a measured
  layer is **stale**: reported, never a failure, and removed only by
  `ratchet --prune`.

**Canonical form** (what the ratchet writes, byte for byte): keys sorted
bytewise at every level; `version`, `tolerance_pts`, `layers`, `packages`,
`globs` in that order; one floor entry per line as `"key": { "m": 1.0, ... }`;
every percentage with exactly one decimal (`72.0`, never `72`); empty sections
as `{}`; two-space indent; a trailing newline. A ratchet that raises one
package's floor is a one-line diff.

Floors for a layer id the config does not have are ignored with a warning; so
are floors for a metric the layer does not measure.

---

## coverage/exclude.txt

```
# Generated and vendored code is never measured. Every line needs a reason.
**/*_templ.go  # templ codegen
libs/go/svc/*/main.go  # thin wiring after the buildServer refactor; the e2e lane measures it
apps/app/src/components/ui/**  # vendored shadcn

apps/mobile/lib/**/*.g.dart  # drift codegen
```

- One glob per line, then whitespace, then `#` and a **required** reason. The
  canonical separator is two spaces; any run of spaces or tabs is accepted.
- A line starting with `#` (after leading whitespace) is a comment; blank lines
  are fine. There is no negation.
- A glob cannot contain whitespace or `#`. Listing the same glob twice is an
  error, as is a glob glued to its `#`.
- Exclusions apply to every layer, after the layer's own `include`/`exclude`.
- A file matching several rules is charged to the **first**, so the sizes
  printed per rule add up to the total excluded.
- Every rule is reported with its size in every layer it touched (files, and
  covered/total per metric). When every layer was measured, a rule matching
  nothing is a warning ("delete the line").
- A missing file means no exclusions.

---

## How numbers are computed

**Go statements.** A coverprofile line is one block:
`file:startLine.startCol,endLine.endCol numStmt count`. Profiles are merged
by block identity (file and exact position): `-coverpkg=./...` writes each
block once per test binary, and a block is one block no matter how many
binaries wrote it. Counts are summed; a block is covered when the sum is > 0.
The same position with a different `numStmt` is an error (the profiles came
from different sources). A file's statements are the sum of its blocks'
`numStmt`. This is exactly the figure `go test -cover` prints per package.
`go tool cover -func` can differ slightly: it only counts blocks inside
function declarations, so statements in package-level function literals
(`var h = func() {...}`) are left out of its total. coverreport counts them.

**Go lines** (used only for patch coverage and uncovered-line ranges; Go
layers are gated on statements):
- only blocks with at least one statement count;
- every line a block spans is coverable;
- a line is covered when **any** block touching it ran (gofmt puts block
  boundaries on shared lines: the `if cond {` line ends the parent block, and
  `}` ends one block and starts the next; "any" marks exactly the untaken
  body);
- when the source file exists under `--root`, lines that carry no code are
  dropped: blank lines, lines that are only a `//` comment, and lines made only
  of `}`, `)`, `]`, `,`, `;`.

**LCOV.** Lines are `DA` records (covered when hits > 0); branches are `BRDA`
records (covered when taken > 0, `-` is 0); functions are distinct names from
`FN`/`FNDA` (and lcov 2.2 `FNL`/`FNA`), covered when hits > 0. `FN:line,name`
and lcov 2.x `FN:start,end,name` are both read. The summary records
(`LF`/`LH`, `BRF`/`BRH`, `FNF`/`FNH`) are ignored and recomputed from the
details, as `lcov --summary` does. Records for the same file, in one tracefile
or several, are merged: DA hits summed per line, BRDA summed per (line, block,
branch), FNDA summed per name. Unknown records (`VER`, `MCDC`, ...) are ignored.
LCOV has no statement data: an lcov layer cannot declare `statements`.

**Scope order**, per file: the layer's `include` (if any), then the layer's
`exclude`, then `exclude.txt`. What each step removed is reported.

**Rows.** A layer total sums every kept file. A package row is one directory
(not recursive). A glob row sums the kept files matching the glob; the rows are
the config's `globs` then any glob that only `floors.json` names.

**Patch coverage.** From a unified git diff (see the README for the exact
command), each file's added lines on the new side. Deleted and binary files
contribute nothing; renames and copies use the new path; mode-only changes have
no lines. In each measured layer, a changed line counts when the layer has
line data for it (coverable) and is covered when that line ran. Per layer:
`exempt` when it has fewer than `min_lines` changed coverable lines (or none),
`pass` when covered/total >= target (inclusive, exact), else `fail`.
Report-only layers are computed and shown but never gated. The overall number
is the union over gated layers (a line is coverable if any layer can cover it,
covered if any layer did) and is informational. `patch.status` is `fail` if any
gated layer failed, `pass` if at least one passed, else `exempt`. Only with
`patch.blocking: true` does a failing layer become a failure.

**Overall status.** `fail` when there is at least one failure: a layer,
package or glob floor broken; a layer metric with a floor but no data (the
collector stopped producing it); a `--require`d layer that was not measured;
a blocking patch miss. Otherwise `pass`.

---

## report.json

Written by `coverreport analyze`, read by `check --report`, `ratchet
--report`, `text --report` and by renderers. Two-space indented, UTF-8, no
HTML escaping. `version` is bumped on any change a renderer written against
the old shape would misread; additive optional fields keep it. `Load` refuses
an unknown version.

Conventions: percentages (`pct`, `delta`, `measured`, ...) are rounded to two
decimals for display; every status is computed from the integer
`covered`/`total`, never from `pct`. Optional values are omitted rather than
`null`. Line ranges are inclusive `[start, end]` pairs, sorted. Lists are
never `null` (empty lists are `[]`).

### Top level

| Field | Type | Meaning |
|:--|:--|:--|
| `version` | int | `1` |
| `tool` | string | `"coverreport"` |
| `generated_at` | RFC 3339 UTC | analysis time; `SOURCE_DATE_EPOCH` overrides it for reproducible output |
| `metadata` | object | `repo`, `pr` (int), `sha`, `base`, `head_ref`, `base_ref`, `run_url`: from flags or the GitHub Actions environment; `title` and `head_sha` from flags or the `pull_request` payload at `$GITHUB_EVENT_PATH`; each omitted when unknown |
| `status` | `pass` / `fail` | fail iff `failures` is non-empty |
| `failures[]` | [Failure](#failure) | every reason for `fail` |
| `tolerance_pts` | number | from floors.json |
| `floors_file`, `exclude_file` | path | as configured |
| `floors_found` | bool | false when floors.json did not exist |
| `go_modules` | object | the resolved module map (only when a go layer exists) |
| `layers[]` | [Layer](#layer-1) | in config order |
| `patch` | [Patch](#patch) | |
| `exclusions[]` | [Exclusion](#exclusion) | exclude.txt, in file order |
| `ratchet[]` | [Change](#change) | every floor that can rise or be created now |
| `stale_floors[]` | `{scope, layer, key}` | package/glob floors matching nothing measured |
| `warnings[]` | string | non-fatal problems, in a stable order |
| `brand` | object | the config's `brand` block, verbatim (omitted when none) |
| `ratchet_command` | string | the config's `ratchet_command` |
| `baseline` | object | `{file, sha, generated_at}` of the `analyze --baseline` report layers were carried from (omitted without one) |
| `endpoints` | [Endpoints](#endpoints) | the endpoint registry's summary (`analyze --endpoints`; omitted without one) |

The last four were added after version 1 shipped; they are optional, so the
version stays 1 and a renderer that ignores them still reads the report.

### Failure

| Field | Meaning |
|:--|:--|
| `scope` | `layer`, `package`, `glob`, `patch`, `required` or `endpoints` (one per endpoint-registry baseline violation; `key` is the endpoint id, `layer` is empty) |
| `layer` | layer id |
| `key` | the directory or glob (package/glob scope) |
| `metric` | the metric (floor scopes) |
| `measured` | the percentage measured, when there was one |
| `floor` | the floor |
| `threshold` | what had to be reached: `floor - tolerance_pts`, or the patch target |
| `message` | one human sentence, ready to print |

### Layer

| Field | Meaning |
|:--|:--|
| `id`, `label`, `description`, `format`, `report_only` | from the config |
| `status` | `ok`, `fail` (any layer/package/glob failure names this layer), `not_measured` (no input matched) or `report_only` |
| `inputs[]` | artifact files read, relative to `--inputs` |
| `modes[]` | coverprofile modes seen (go layers) |
| `metrics[]` | configured order; the first is primary |
| `files` | files in the layer after scope and exclusions |
| `totals` | metric to [MetricResult](#metricresult) |
| `packages[]` | [Group](#group) per directory, sorted by path |
| `globs[]` | [Group](#group) per glob row, config order, then floors-only globs sorted |
| `lowest_files[]` | [FileSummary](#filesummary): up to `lowest_files` files with the most uncovered primary units, ties broken by the lower percentage, then the path. Files with nothing uncovered are never listed |
| `scope[]` | [ScopeExclusion](#scopeexclusion) |
| `short_label`, `group`, `treemap` | from the config (defaults applied) |
| `carried` | on a `not_measured` layer when the `--baseline` report measured it: `{sha, totals}` with `totals` metric to `{covered, total, pct}`. Display only: the layer's `status` stays `not_measured`, and carried numbers are never checked or ratcheted (they describe another commit) |

A `not_measured` layer still carries `totals` with its floors and targets (and
`covered`/`total` of 0), so a renderer can show "not affected" next to the
floor. Its lists are empty.

### MetricResult

| Field | Meaning |
|:--|:--|
| `covered`, `total` | integers |
| `pct` | covered/total*100, two decimals; omitted when there is no data |
| `floor` | the floor, when one is set (never for report-only layers) |
| `target` | the target, when configured |
| `delta` | `pct - floor`, two decimals |
| `status` | `ok`, `fail`, `no_floor`, `no_data` (total 0), `not_measured` or `report_only` |
| `ratchet_to` | the value a ratchet would write, when it is above the floor (or there is none and the row is tracked) |

### Group

`key` (directory or glob), `label` (glob rows, when configured), `files`,
`totals` (metric to MetricResult), `status`: `fail` if any metric failed, else
`ok` if any metric passed a floor, else `no_floor`, else `no_data`
(`report_only` in a report-only layer).

### FileSummary

`path`; `counts` (metric to `{covered, total}`); `pct` and `uncovered` for the
primary metric; `uncovered_lines` (ranges; for Go, per the line model above).

### ScopeExclusion

`kind` is `not_included` (files no `include` glob matched; present only when
the layer has `include`) or `exclude` (one per layer `exclude` glob, config
order, `@set`s expanded, including globs that removed nothing); `glob`;
`files`; `counts`. `not_included` counts files before `exclude.txt` is applied.

### Exclusion

`glob`, `reason`, `line` (in exclude.txt), `layers[]`: one
`{layer, files, counts}` per layer the rule removed something from.

### Patch

| Field | Meaning |
|:--|:--|
| `status` | `pass`, `fail`, `exempt` or `not_computed` (no diff given) |
| `target`, `min_lines`, `blocking`, `informational_until` | from the config |
| `overall` | `{covered, total, pct}`: the union over gated layers |
| `layers[]` | one per layer, config order: `layer`, `label`, `covered`, `total`, `pct`, `status` (`pass`, `fail`, `exempt`, `report_only`, `not_measured`), `files[]` |
| `layers[].files[]` | each changed file with added lines that the layer contains: `path`, `covered`, `total`, `pct` (changed coverable lines), `changed` (every added line), `uncovered_changed`, and the WHOLE file's `covered_lines` / `uncovered_lines`, so a renderer can annotate the full file without the artifacts |
| `diff` | `files` (every file in the diff), `added_lines` (non-deleted, non-binary), `binary[]`, `deleted[]`, `renamed[]` (`{from, to}`, renames and copies), `unmeasured[]` (changed files with added lines no measured layer contains) |

### Change

`scope` (`layer`, `package`, `glob`), `layer`, `key` (package/glob),
`metric`, `from` (omitted for a new entry), `to`.

### Endpoints

| Field | Meaning |
|:--|:--|
| `file` | the endpoints.json read, as given to `--endpoints` |
| `generated_from` | the registry's `generated_from` (the commit it was built at) |
| `total` | how many endpoints the registry lists |
| `layers[]` | the test layers the registry uses: `unit`, `integration`, `e2e` in that order, then any other, sorted |
| `kinds[]`, `surfaces[]` | completeness rows, one per endpoint kind and one per surface, in the registry's order: `key`, `label` (`routes`; the surface's label), `total`, `best` (`{full, partial, none}`: each endpoint once, at its best status), `layers` (layer to `{full, partial, none}`; an endpoint a layer never mentions counts as `none` there), `most_missed` and `most_missed_count` (the applicable class most often missing from the endpoints' best layer; ties go to the registry's class order) |
| `violations[]` | the registry's `baseline.violations`, `{id, detail}`; each is also a [Failure](#failure) of scope `endpoints` |

### Example

Trimmed from `testdata/golden/report.json` (one measured layer, one
unmeasured layer, one patch layer):

```json
{
  "version": 1,
  "tool": "coverreport",
  "generated_at": "2026-09-28T12:00:00Z",
  "metadata": {
    "repo": "acme/demo",
    "pr": 42,
    "sha": "3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d",
    "base": "1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d",
    "head_ref": "feat/calc",
    "base_ref": "main",
    "run_url": "https://github.com/acme/demo/actions/runs/1234"
  },
  "status": "fail",
  "failures": [
    {
      "scope": "layer",
      "layer": "go-unit",
      "metric": "statements",
      "measured": 61.54,
      "floor": 62,
      "threshold": 61.9,
      "message": "go-unit statements is 61.54%, below its floor 62.0 by more than the 0.1-point tolerance"
    }
  ],
  "tolerance_pts": 0.1,
  "floors_file": "coverage/floors.json",
  "floors_found": true,
  "exclude_file": "coverage/exclude.txt",
  "go_modules": { "example.com/demo/libs/go": "libs/go" },
  "layers": [
    {
      "id": "go-unit",
      "label": "Go unit (logic)",
      "description": "go test -covermode=atomic -coverpkg=./..., database unset; SQL adapters are go-sql's",
      "format": "go",
      "report_only": false,
      "status": "fail",
      "inputs": ["coverage-go/unit.out"],
      "modes": ["atomic"],
      "metrics": ["statements"],
      "files": 1,
      "totals": {
        "statements": { "covered": 8, "total": 13, "pct": 61.54, "floor": 62, "target": 85, "delta": -0.46, "status": "fail" }
      },
      "packages": [
        {
          "key": "libs/go/calc",
          "files": 1,
          "totals": {
            "statements": { "covered": 8, "total": 13, "pct": 61.54, "target": 80, "status": "no_floor", "ratchet_to": 61.5 }
          },
          "status": "no_floor"
        }
      ],
      "globs": [
        {
          "key": "libs/go/calc/**",
          "label": "money path",
          "files": 1,
          "totals": {
            "statements": { "covered": 8, "total": 13, "pct": 61.54, "target": 90, "status": "no_floor", "ratchet_to": 61.5 }
          },
          "status": "no_floor"
        }
      ],
      "lowest_files": [
        {
          "path": "libs/go/calc/calc.go",
          "counts": { "statements": { "covered": 8, "total": 13 } },
          "pct": 61.54,
          "uncovered": 5,
          "uncovered_lines": [[17, 18], [22, 22], [35, 37], [39, 39]]
        }
      ],
      "scope": [
        { "kind": "not_included", "files": 0, "counts": {} },
        { "kind": "exclude", "glob": "libs/go/**/store_pg.go", "files": 1, "counts": { "statements": { "covered": 0, "total": 3 } } }
      ]
    },
    {
      "id": "edge",
      "label": "Edge Worker",
      "format": "lcov",
      "report_only": false,
      "status": "not_measured",
      "inputs": [],
      "metrics": ["lines"],
      "files": 0,
      "totals": { "lines": { "covered": 0, "total": 0, "floor": 98, "target": 95, "status": "not_measured" } },
      "packages": [],
      "globs": [],
      "lowest_files": [],
      "scope": []
    }
  ],
  "patch": {
    "status": "fail",
    "target": 80,
    "min_lines": 3,
    "blocking": false,
    "overall": { "covered": 7, "total": 15, "pct": 46.67 },
    "layers": [
      {
        "layer": "go-sql",
        "label": "Go SQL adapters (Postgres)",
        "covered": 3,
        "total": 4,
        "pct": 75,
        "status": "fail",
        "files": [
          {
            "path": "libs/go/calc/store_pg.go",
            "covered": 3,
            "total": 4,
            "pct": 75,
            "changed": [[1, 12]],
            "uncovered_changed": [[9, 9]],
            "covered_lines": [[7, 8], [11, 11]],
            "uncovered_lines": [[9, 9]]
          }
        ]
      }
    ],
    "diff": {
      "files": 8,
      "added_lines": 38,
      "binary": ["apps/web/public/logo.png"],
      "deleted": ["libs/go/old/old.go"],
      "renamed": [{ "from": "apps/web/src/lib/old.ts", "to": "apps/web/src/lib/money.ts" }],
      "unmeasured": ["apps/web/src/components/ui/card.tsx", "docs/README.md"]
    }
  },
  "exclusions": [
    {
      "glob": "**/*_templ.go",
      "reason": "templ codegen",
      "line": 2,
      "layers": [
        { "layer": "go-unit", "files": 1, "counts": { "statements": { "covered": 1, "total": 2 } } }
      ]
    }
  ],
  "ratchet": [
    { "scope": "layer", "layer": "go-sql", "metric": "statements", "to": 66.6 },
    { "scope": "layer", "layer": "go-live", "metric": "statements", "from": 60, "to": 62.5 }
  ],
  "stale_floors": [{ "scope": "package", "layer": "go-live", "key": "libs/go/gone" }],
  "warnings": ["coverage/floors.json: layers.retired names no configured layer; not checked"]
}
```

---

## endpoints.json (input)

The endpoint registry's output, shared with the sibling repositories. Only
what the summary above needs is read; unknown fields are ignored (the
producer grows the contract additively), but these are checked, so a
half-written file fails `analyze` instead of rendering zeros:

| Field | Rule |
|:--|:--|
| `version` | must be `1` |
| `endpoints[]` | non-empty (the registry's enumerators each have a minimum-count floor, so an empty list is a broken producer). Each needs a unique `id`, a `surface` and a `kind`; `best` and every `layers.<name>.status` are `full`, `partial` or `none` |
| `endpoints[].applicable`, `.layers.<name>.classes` | used for "most missed" |
| `surfaces.<id>.label` | the surface's display name (default: the id) |
| `classes` | the class order that breaks "most missed" ties |
| `baseline.violations[]` | `{id, detail}`; each needs an `id` |

`analyze --endpoints <file>` where the file does not exist is a warning
(the rows are omitted): the registry's job may not have run. A file that
exists but is invalid is an error.

---

## Rendered surfaces

`coverreport render --report report.json --out <dir>` writes four files.
Every decision behind them (row states, the verdict, which lines never ran,
which ranges are annotated) is made once, from the report, so they cannot
disagree.

| File | What | Limit handled |
|:--|:--|:--|
| `comment.md` | the sticky PR comment | GitHub's 65,536 characters (counted as UTF-16 units, with a 1,024 margin) |
| `summary.md` | the job summary (`>> $GITHUB_STEP_SUMMARY`) | 1 MiB per step (16 KiB margin) |
| `annotations.txt` | workflow commands, one per line (`cat` it in the job) | at most 10, on changed lines that never ran only; GitHub shows 10 per level per step |
| `report.html` | one self-contained page | none, but each file's listing is capped at 1,500 lines |

**Row states.** Each gauge (one per layer and metric) is in one state, and
the HUD's first character is its state, so GitHub's `diff` highlighter
colours it: `+` ok (holds its floor), `!` warn (holds, but the layer's patch
coverage is under target), `-` FAIL (below its floor), and `#` for everything
not gated here: `info` (report-only), untouched (measured, holds, and the
change added no line it measures), carried (not run, numbers from the
baseline), not run, new (no floor yet).

**The sticky comment.** Its first line is exactly `<!-- coverreport:v1 -->`;
`coverreport comment` finds the comment to update by it. Its second line is
the push history:

```
<!-- coverreport:state {"n":4,"pushes":[["8e41d07",58.3],["c2b19f4",76.9],["5d0e7aa",83.1],["3f2a9c1",86.4]]} -->
```

`n` is the number of pushes so far; `pushes` the last 30 as
`[short head SHA, overall patch %]`. `render --previous <file>` reads it back
from the comment being replaced, appends this push (or replaces the last
point when the head SHA is the same, a re-run) and writes it into the new
comment. A missing or unreadable state is an empty history, never an error.
Nothing is stored anywhere else.

**Shortening.** When the comment is over its limit, sections are dropped in
this order until it fits: the snippets of files that hold their floors (last
first), how-measured and the exclusions, the packages, ratchet and endpoint
blocks, the snippets of files that break a floor, then the changed-lines
table shrinks to 10 and then 3 rows. The summary drops, in order, the
snippets of files that hold their floors, the lowest files, exclusions and
how-measured, the snippets of files that break a floor, the package and glob
tables, then the ratchet and endpoint blocks, and its table shrinks to 50 and
then 10 rows. The heading, the alert, the HUD and the footer are never
dropped, and a note says what was left out and links the page. As a last resort the body is cut at a line boundary with every fence
and `<details>` it cut through closed.

**Annotations.** One per range of changed lines that never ran: ranges that
count toward a failure first (level `warning`), then the rest (`notice`),
longest first. The title says what and why; the message names the enclosing
function, quotes the first line (with `--source-root`), states the
consequence, and ends with the page's URL and line anchor when
`--artifact-url` is given.

**The page** makes no network request and runs no script: one inline
`<style>` (the brand's tokens, then the static rules), no `style` attribute,
no `<script>`, `<link>`, `<img>`, `@import` or `@font-face`, and every
`http(s)://` URL is an `<a href>` a reader clicks. Charts are inline SVG whose
geometry is attributes and whose colours are classes, with the dark tokens
repeated as presentation attributes so a CSP that blocks inline styles still
leaves readable instruments. Every changed line has an id, `f<file>-L<line>`
(`#f3-L120`); the annotations link there. Light and dark follow
`prefers-color-scheme`. Nothing animates: a tape's height is its reading, and
a capture of the page (a screenshot, a PDF) records the first frame, so the
first frame is the picture.

