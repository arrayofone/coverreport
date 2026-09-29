<!-- coverreport:v1 -->
<!-- coverreport:state {"n":1,"pushes":[["71a0d3e",46.7]]} -->
### 🔴 demo coverage: 2 floors broken, patch under 80% in 3 layers, endpoint baseline broken, edge did not report

> [!CAUTION]
> **7 gates fail**, so the Coverage check fails:
> - **go unit** is at 61.54%, 0.46 below its 62.0 floor. Cover [`calc.go` L17–18, L35–39](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go#L17-L18).
> - **package libs/go/calc (go live)** is at 62.50%, below its 70.0 floor. Cover [`calc.go` L17–18, L35–39](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go#L17-L18) and [`store_pg.go` L9](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/store_pg.go#L9).
> - **go unit patch coverage** is 0.0%, under the 80% target, and the patch gate blocks. The same changed lines as go unit.
> - **go sql patch coverage** is 75.0%, under the 80% target, and the patch gate blocks. Cover [`store_pg.go` L9](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/store_pg.go#L9).
> - **go live patch coverage** is 30.0%, under the 80% target, and the patch gate blocks. The same changed lines as package libs/go/calc (go live).
> - **the endpoint baseline** is broken: 2 endpoints gained a gap. Gaps: `svc-api:POST /sums` (gained a gap: authz); `svc-api:event sum.done` (added after the freeze and not full in any layer).
> - **edge** did not report: none of its inputs exist (coverage-edge/lcov.info).
>
> Add the tests, or lower the floors in `coverage/floors.json` in this PR and say why.

```diff
@@ #561 push 1  3f2a9c1 into main  patch 46.7%  7/15 @@                                                      
#  state layer              now  ±floor  floor  target    patch         0        50      100                 
-  FAIL  go unit          61.54   -0.46   62.0    85.0     0.0% 0/6     ████████████·····│··                 
!  warn  go sql           66.67   +0.07   66.6    80.0    75.0% 3/4     █████████████···│···                 
!  warn  go live          62.50   +2.50   60.0       —    30.0% 3/10    ████████████▌·······  ratchet to 62.5
#  info  go e2e           62.50       —      —       —    30.0% 3/10    ▒▒▒▒▒▒▒▒▒▒▒▒········  report-only    
+  ok    web lines        87.50   +7.50   80.0    80.0    80.0% 4/5     █████████████████▌··  ratchet to 87.5
+  ok    web branches     75.00   +5.00   70.0    75.0                  ███████████████·····  ratchet to 75.0
+  ok    web functions    75.00    0.00   75.0       —                  ███████████████·····                 
#  --    mobile           66.67   +0.07   66.6       —        untouched █████████████·······                 
#  --    edge                 —       —   98.0    95.0          not run ···················│  not affected   
#                                                                                                            
-  FAIL  routes                     2/4                 full, any layer ██████████·········│  1 gained a gap 
-  FAIL  events                     0/1                 full, any layer ···················│  1 gained a gap 
+  ok    pages                      0/1                 full, any layer ···················│  0.0% full      
+  ok    server actions             1/1                 full, any layer ████████████████████  100.0% full    
```
<sub>Rows: <code>+</code> holds its floor, <code>!</code> patch under 80% (blocking), <code>-</code> below its floor, <code>#</code> not gated here (report-only, untouched, not run, or no floor yet). Meters are 20 cells of 5%; <code>│</code> marks the target.</sub>

#### 8 changed lines have no test

| file | counts toward | changed lines | ran | no test yet |
|:--|:--|:--|--:|:--|
| [`calc.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go)<br><sub>libs/go/calc</sub> | **go unit, below floor**<br>**go live, package below floor** | `□□□□□□` | 0/6 | [L17–18](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go#L17-L18), [L35–39](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/calc.go#L35-L39) |
| [`store_pg.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/store_pg.go)<br><sub>libs/go/calc</sub> | **go sql, patch blocks**<br>**go live, package below floor** | `■■□■` | 3/4 | [L9](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/libs/go/calc/store_pg.go#L9) |
| [`button.tsx`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/apps/web/src/components/button.tsx)<br><sub>apps/web/src/components</sub> | web | `■■□` | 2/3 | [L7](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/apps/web/src/components/button.tsx#L7) |

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
<details open>
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

<details open>
<summary>Packages touched: 5, 1 below its floor</summary>

```diff
#  package                  layer     floor     now  ±floor           patch   0        50      100           
!  libs/go/calc             go unit       —   61.54       —        0.0% 0/6   ████████████····│···  target 80
!  libs/go/calc             go sql        —   66.67       —       75.0% 3/4   █████████████···│···  target 80
-  libs/go/calc             go live    70.0   62.50   -7.50      30.0% 3/10   ████████████▌·······           
!  apps/web/src/components  web           —   66.67       —       66.7% 2/3   █████████████···│···  target 80
#  apps/web/src/lib         web           —  100.00       —      100.0% 2/2   ████████████████████  target 80
```

</details>

<details>
<summary>Ratchet available: 2 floors can rise, 4 floors can be created</summary>

Run `coverreport ratchet` and commit `coverage/floors.json`. These are the entries it rewrites:

```diff
@@ coverage/floors.json @@
   "layers": {
-    "go-live": { "statements": 60.0 }
+    "go-live": { "statements": 62.5 }
-    "web": { "branches": 70.0, "functions": 75.0, "lines": 80.0 }
+    "web": { "branches": 75.0, "functions": 75.0, "lines": 87.5 }
   "packages": {
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

<details open>
<summary>Endpoints fully tested: 2 of 4 routes, 0 of 1 event, 0 of 1 page, 1 of 1 server action</summary>

```diff
#  kind                     unit  integration          e2e         best   most missed   
-  routes                    2/4          0/4          1/4          2/4   authz (2)     
-  events                    0/1          0/1          0/1          0/1   happy (1)     
+  pages                     0/1          0/1          0/1          0/1   dependency (1)
+  server actions            1/1          0/1          0/1          1/1                 
```

2 endpoints broke the endpoint baseline (it only shrinks; a new endpoint must be fully tested in some layer):

- `svc-api:POST /sums`: gained a gap: authz
- `svc-api:event sum.done`: added after the freeze and not full in any layer

An endpoint is fully tested when some layer proves the happy path and every class of the rubric that applies to it. Read from `coverage-endpoints/endpoints.json`.

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

<sub>Line by line: the coverage-561.html artifact of [this run](https://github.com/acme/demo/actions/runs/18342917305). Every package and glob: [job summary](https://github.com/acme/demo/actions/runs/18342917305). Floors: [`coverage/floors.json`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/coverage/floors.json). Updated for push 1 (71a0d3e) at 15:42 UTC, Sep 28 by coverreport v0.1.0.</sub>
