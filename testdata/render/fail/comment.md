<!-- coverreport:v1 -->
<!-- coverreport:state {"n":4,"pushes":[["8e41d07",58.3],["c2b19f4",76.9],["5d0e7aa",83.1],["71a0d3e",46.7]]} -->
### 🔴 handipay coverage: 1 floor broken

> [!CAUTION]
> **Go unit is at 61.54%, 0.46 below its 62.0 floor**, so the Coverage check fails. None of the 6 changed lines it measures ran.
> Cover [`calc.go` L17–18, L35–39](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go#L17-L18), or lower `layers.go-unit.statements` in `coverage/floors.json` in this PR and say why.

```diff
@@ #561 push 4  3f2a9c1 into main  patch 46.7%  7/15  by push ▃▇█▁ @@                                       
#  state layer             now  ±floor  floor  target    patch         0        50      100                 
-  FAIL  go unit         61.54   -0.46   62.0    85.0     0.0% 0/6     ████████████·····│··                 
!  warn  go sql          66.67   +0.07   66.6    80.0    75.0% 3/4     █████████████···│···                 
!  warn  go live         62.50   +2.50   60.0       —    30.0% 3/10    ████████████▌·······  ratchet to 62.5
#  info  go e2e          62.50       —      —       —    30.0% 3/10    ▒▒▒▒▒▒▒▒▒▒▒▒········  report-only    
+  ok    web lines       87.50   +7.50   80.0    80.0    80.0% 4/5     █████████████████▌··  ratchet to 87.5
+  ok    web branches    75.00   +5.00   70.0    75.0                  ███████████████·····  ratchet to 75.0
+  ok    web functions   75.00    0.00   75.0       —                  ███████████████·····                 
#  --    mobile          66.67   +0.07   66.6       —        untouched █████████████·······                 
#  --    edge                —       —   98.0    95.0          not run ···················│  not affected   
```
<sub>Rows: <code>+</code> holds its floor, <code>!</code> patch under 80% (informational until 2026-10-05), <code>-</code> below its floor, <code>#</code> not gated here (report-only, untouched, not run, or no floor yet). Meters are 20 cells of 5%; <code>│</code> marks the target.</sub>

#### 8 changed lines have no test

| file | counts toward | changed lines | ran | no test yet |
|:--|:--|:--|--:|:--|
| [`calc.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go)<br><sub>libs/go/calc</sub> | **go unit, below floor**<br>go live | `□□□□□□` | 0/6 | [L17–18](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go#L17-L18), [L35–39](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go#L35-L39) |
| [`button.tsx`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/apps/web/src/components/button.tsx)<br><sub>apps/web/src/components</sub> | web | `■■□` | 2/3 | [L7](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/apps/web/src/components/button.tsx#L7) |
| [`store_pg.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/store_pg.go)<br><sub>libs/go/calc</sub> | go sql<br>go live | `■■□■` | 3/4 | [L9](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/store_pg.go#L9) |

<details open>
<summary><code>calc.go</code> L17–18, L35–39: 6 of 6 changed lines never ran</summary>

```diff
@@ calc.go  L14–20  Classify @@
    14 │ 
    15 │     // Small numbers get a name.
    16 │     switch {
!   17 │     case n == 0:
!   18 │         return "zero", nil
    19 │     case n < 10:
    20 │         return "small", nil
@@ calc.go  L32–40  Unused @@
    32 │ }
    33 │ 
    34 │ // Unused is never called.
!   35 │ func Unused() int {
!   36 │     a := 1
!   37 │     b := 2
    38 │ 
!   39 │     return a + b
    40 │ }
```

</details>
<details>
<summary><code>button.tsx</code> L7: 1 of 3 changed lines never ran</summary>

```diff
@@ button.tsx  L4–8  IconButton @@
     4 │ }
     5 │ 
     6 │ export function IconButton() {
!    7 │   return <Button primary />;
     8 │ }
```

</details>
<details>
<summary><code>store_pg.go</code> L9: 1 of 4 changed lines never ran</summary>

```diff
@@ store_pg.go  L6–11  Load @@
     6 │ // Load reads rows.
+    7 │ func (s *Store) Load() []int {
+    8 │     if s == nil {
!    9 │         return nil
    10 │     }
+   11 │     return s.rows
```

</details>

<details>
<summary>Packages touched: 5, none with a floor yet</summary>

```diff
#  package                  layer     floor     now  ±floor           patch   0        50      100           
!  libs/go/calc             go unit       —   61.54       —        0.0% 0/6   ████████████····│···  target 80
!  libs/go/calc             go sql        —   66.67       —       75.0% 3/4   █████████████···│···  target 80
!  libs/go/calc             go live       —   62.50       —      30.0% 3/10   ████████████▌·······           
!  apps/web/src/components  web           —   66.67       —       66.7% 2/3   █████████████···│···  target 80
#  apps/web/src/lib         web           —  100.00       —      100.0% 2/2   ████████████████████  target 80
```

</details>

<details>
<summary>Ratchet available: 2 floors can rise, 5 floors can be created</summary>

Run `make coverage-ratchet` and commit `coverage/floors.json`. These are the entries it rewrites:

```diff
@@ coverage/floors.json @@
   "layers": {
-    "go-live": { "statements": 60.0 }
+    "go-live": { "statements": 62.5 }
-    "web": { "branches": 70.0, "functions": 75.0, "lines": 80.0 }
+    "web": { "branches": 75.0, "functions": 75.0, "lines": 87.5 }
   "packages": {
     "go-live": {
+      "libs/go/calc": { "statements": 62.5 }
     "go-unit": {
+      "libs/go/calc": { "statements": 61.5 }
   "globs": {
     "go-unit": {
+      "libs/go/calc/**": { "statements": 61.5 }
     "web": {
+      "apps/web/src/lib/**": { "branches": 100.0, "functions": 100.0, "lines": 100.0 }
+      "apps/web/src/{components,app}/**": { "branches": 50.0, "functions": 50.0, "lines": 66.6 }
```

</details>

<details>
<summary>Left out of the denominator: 12 statements and 3 lines</summary>

| glob | why | size |
|:--|:--|--:|
| `**/*_templ.go` | templ codegen | 2 stmts in go-unit<br>2 stmts in go-live<br>2 stmts in go-e2e |
| `libs/go/svc/*/main.go` | thin wiring; the e2e lane measures it | 2 stmts in go-unit<br>2 stmts in go-live<br>2 stmts in go-e2e |
| `apps/web/src/components/ui/**` | vendored shadcn | 1 line in web |
| `apps/mobile/lib/**/*.g.dart` | drift codegen | 2 lines in mobile |
| `apps/web/src/legacy/**` | deleted last quarter | nothing measured |

From `coverage/exclude.txt`: every rule is printed with its size, so an exclusion cannot hide code quietly.

Layer boundaries (each layer's own include/exclude):

| layer | rule | size |
|:--|:--|--:|
| go unit | exclude `libs/go/**/store_pg.go` | 3 stmts |
| go sql | not included | 17 stmts |

</details>

<details>
<summary>How this was measured</summary>

| layer | how | inputs |
|:--|:--|:--|
| go unit | go test -covermode=atomic -coverpkg=./..., database unset; SQL adapters are go-sql's (mode atomic) | `coverage-go/unit.out` |
| go sql | go (mode atomic) | `coverage-go/live.out` |
| go live | go (mode atomic) | `coverage-go/live.out` |
| go e2e | go (mode atomic) | `coverage-go-e2e/e2e.out` |
| web | lcov | `coverage-web/shard-1/lcov.info`<br>`coverage-web/shard-2/lcov.info` |
| mobile | lcov | `coverage-mobile/lcov.info` |
| patch | the diff against the base: 8 files, 38 lines added | |

</details>

<sub>Line by line, with every changed file: [coverage-561.html](https://github.com/acme/demo/actions/runs/18342917305/artifacts/4127736190). Every package and glob: [job summary](https://github.com/acme/demo/actions/runs/18342917305). Floors: [`coverage/floors.json`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/coverage/floors.json). Updated for push 4 (71a0d3e) at 15:42 UTC, Sep 28 by coverreport v0.1.0.</sub>
