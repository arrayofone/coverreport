<!-- coverreport:v1 -->
<!-- coverreport:state {"n":1,"pushes":[["71a0d3e",100]]} -->
### 🟢 demo coverage: every floor holds

> [!TIP]
> **Every floor holds, and 3 can rise.** Run `coverreport ratchet` and commit `coverage/floors.json` to lock the gain in.

```diff
@@ #561 push 1  3f2a9c1 into main  patch 100.0%  8/8 @@                                                       
#  state layer             now  ±floor  floor  target    patch         0        50      100                   
+  ok    go unit         61.54   +1.54   60.0    85.0   100.0% 5/5     ████████████·····│··  ratchet to 61.5  
#  --    go sql          66.67   +0.07   66.6    80.0        untouched █████████████···│···                   
+  ok    go live         62.50   +2.50   60.0       —   100.0% 5/5     ████████████▌·······  ratchet to 62.5  
#  info  go e2e          62.50       —      —       —   100.0% 5/5     ▒▒▒▒▒▒▒▒▒▒▒▒········  report-only      
+  ok    web lines       87.50   +7.50   80.0    80.0   100.0% 3/3     █████████████████▌··  ratchet to 87.5  
+  ok    web branches    75.00   +5.00   70.0    75.0                  ███████████████·····  ratchet to 75.0  
+  ok    web functions   75.00    0.00   75.0       —                  ███████████████·····                   
#  --    mobile          66.67   +0.07   66.6       —          not run █████████████·······  from main bb1595f
#  --    edge                —       —   98.0    95.0          not run ···················│  not affected     
```
<sub>Rows: <code>+</code> holds its floor, <code>!</code> patch under 80% (informational until 2026-10-05), <code>-</code> below its floor, <code>#</code> not gated here (report-only, untouched, not run, or no floor yet). Meters are 20 cells of 5%; <code>│</code> marks the target.</sub>

#### Every changed line ran

All 8 coverable lines this change adds ran under a test.

<details>
<summary>Packages touched: 3, none with a floor yet</summary>

```diff
#  package           layer     floor     now  ±floor           patch   0        50      100           
#  libs/go/calc      go unit       —   61.54       —      100.0% 5/5   ████████████····│···  target 80
#  libs/go/calc      go live       —   62.50       —      100.0% 5/5   ████████████▌·······           
#  apps/web/src/lib  web           —  100.00       —      100.0% 3/3   ████████████████████  target 80
```

</details>

<details>
<summary>Ratchet available: 3 floors can rise, 5 floors can be created</summary>

Run `coverreport ratchet` and commit `coverage/floors.json`. These are the entries it rewrites:

```diff
@@ coverage/floors.json @@
   "layers": {
-    "go-live": { "statements": 60.0 }
+    "go-live": { "statements": 62.5 }
-    "go-unit": { "statements": 60.0 }
+    "go-unit": { "statements": 61.5 }
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
<summary>Left out of the denominator: 12 statements and 1 line</summary>

| glob | why | size |
|:--|:--|--:|
| `**/*_templ.go` | templ codegen | 2 stmts in go-unit<br>2 stmts in go-live<br>2 stmts in go-e2e |
| `libs/go/svc/*/main.go` | thin wiring; the e2e lane measures it | 2 stmts in go-unit<br>2 stmts in go-live<br>2 stmts in go-e2e |
| `apps/web/src/components/ui/**` | vendored shadcn | 1 lines in web |
| `apps/mobile/lib/**/*.g.dart` | drift codegen | nothing measured |
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
| patch | the diff against the base: 2 files, 9 lines added | |

</details>

<sub>Line by line: the coverage-561.html artifact of [this run](https://github.com/acme/demo/actions/runs/18342917305). Every package and glob: [job summary](https://github.com/acme/demo/actions/runs/18342917305). Floors: [`coverage/floors.json`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/coverage/floors.json). Updated for push 1 (71a0d3e) at 15:42 UTC, Sep 28 by coverreport v0.1.0.</sub>
