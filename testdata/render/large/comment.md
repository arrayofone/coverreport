<!-- coverreport:v1 -->
<!-- coverreport:state {"n":1,"pushes":[["71a0d3e",53.1]]} -->
### 🟡 handipay coverage: floors hold, patch under 80%

> [!NOTE]
> **Go unit patch coverage is 53.1%**, under the 80% target: 135 of 288 changed lines never ran. Informational until 2026-10-05, then it blocks.

```diff
@@ #561 push 1  3f2a9c1 into main  patch 53.1%  153/288 @@                                            
#  state layer       now  ±floor  floor  target    patch         0        50      100                 
!  warn  go unit   53.13   +3.13   50.0    85.0    53.1% 153/288 ██████████▌······│··  ratchet to 53.1
```
<sub>Rows: <code>+</code> holds its floor, <code>!</code> patch under 80% (informational until 2026-10-05), <code>-</code> below its floor, <code>#</code> not gated here (report-only, untouched, not run, or no floor yet). Meters are 20 cells of 5%; <code>│</code> marks the target.</sub>

#### 135 changed lines have no test

| file | counts toward | changed lines | ran | no test yet |
|:--|:--|:--|--:|:--|
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit00/file01.go)<br><sub>pkg/area00/unit00</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit00/file01.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit00/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit00/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file00.go)<br><sub>pkg/area00/unit01</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file00.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file00.go#L28-L30) |
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file01.go)<br><sub>pkg/area00/unit01</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file01.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit01/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file00.go)<br><sub>pkg/area00/unit02</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file00.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file00.go#L28-L30) |
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file01.go)<br><sub>pkg/area00/unit02</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file01.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit02/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file00.go)<br><sub>pkg/area00/unit03</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file00.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file00.go#L28-L30) |
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file01.go)<br><sub>pkg/area00/unit03</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file01.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit03/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit04/file00.go)<br><sub>pkg/area01/unit04</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit04/file00.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit04/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit04/file00.go#L28-L30) |
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit05/file01.go)<br><sub>pkg/area01/unit05</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit05/file01.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit05/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit05/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file00.go)<br><sub>pkg/area01/unit06</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file00.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file00.go#L28-L30) |
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file01.go)<br><sub>pkg/area01/unit06</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file01.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit06/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file00.go)<br><sub>pkg/area01/unit07</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file00.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file00.go#L28-L30) |
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file01.go)<br><sub>pkg/area01/unit07</sub> | go unit | `■■■□□□■■■□□□■■■□□□` | 9/18 | [L8–10](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file01.go#L8-L10), [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit07/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit00/file00.go)<br><sub>pkg/area00/unit00</sub> | go unit | `■■■■■■■■■□□□■■■□□□` | 12/18 | [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit00/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area00/unit00/file00.go#L28-L30) |
| [`file01.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit04/file01.go)<br><sub>pkg/area01/unit04</sub> | go unit | `■■■■■■■■■□□□■■■□□□` | 12/18 | [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit04/file01.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit04/file01.go#L28-L30) |
| [`file00.go`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit05/file00.go)<br><sub>pkg/area01/unit05</sub> | go unit | `■■■■■■■■■□□□■■■□□□` | 12/18 | [L18–20](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit05/file00.go#L18-L20), [L28–30](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/pkg/area01/unit05/file00.go#L28-L30) |

<details>
<summary><code>file01.go</code> L8–10, L18–20, L28–30: 9 of 18 changed lines never ran</summary>

```diff
@@ file01.go  L5–12  F1_1 @@
+    5 │     return y + 0
     6 │ }
     7 │ 
!    8 │ func F1_1(x int) int {
!    9 │     y := x * 3
!   10 │     return y + 0
    11 │ }
    12 │ 
@@ file01.go  L15–22  F1_3 @@
+   15 │     return y + 0
    16 │ }
    17 │ 
!   18 │ func F1_3(x int) int {
!   19 │     y := x * 5
!   20 │     return y + 0
    21 │ }
    22 │ 
@@ file01.go  L25–32  F1_5 @@
+   25 │     return y + 0
    26 │ }
    27 │ 
!   28 │ func F1_5(x int) int {
!   29 │     y := x * 7
!   30 │     return y + 0
    31 │ }
    32 │ 
```

</details>
<details>
<summary><code>file00.go</code> L8–10, L18–20, L28–30: 9 of 18 changed lines never ran</summary>

```diff
@@ file00.go  L5–12  F0_1 @@
+    5 │     return y + 1
     6 │ }
     7 │ 
!    8 │ func F0_1(x int) int {
!    9 │     y := x * 3
!   10 │     return y + 1
    11 │ }
    12 │ 
@@ file00.go  L15–22  F0_3 @@
+   15 │     return y + 1
    16 │ }
    17 │ 
!   18 │ func F0_3(x int) int {
!   19 │     y := x * 5
!   20 │     return y + 1
    21 │ }
    22 │ 
@@ file00.go  L25–32  F0_5 @@
+   25 │     return y + 1
    26 │ }
    27 │ 
!   28 │ func F0_5(x int) int {
!   29 │     y := x * 7
!   30 │     return y + 1
    31 │ }
    32 │ 
```

</details>
<details>
<summary><code>file01.go</code> L8–10, L18–20, L28–30: 9 of 18 changed lines never ran</summary>

```diff
@@ file01.go  L5–12  F1_1 @@
+    5 │     return y + 1
     6 │ }
     7 │ 
!    8 │ func F1_1(x int) int {
!    9 │     y := x * 3
!   10 │     return y + 1
    11 │ }
    12 │ 
@@ file01.go  L15–22  F1_3 @@
+   15 │     return y + 1
    16 │ }
    17 │ 
!   18 │ func F1_3(x int) int {
!   19 │     y := x * 5
!   20 │     return y + 1
    21 │ }
    22 │ 
@@ file01.go  L25–32  F1_5 @@
+   25 │     return y + 1
    26 │ }
    27 │ 
!   28 │ func F1_5(x int) int {
!   29 │     y := x * 7
!   30 │     return y + 1
    31 │ }
    32 │ 
```

</details>
<details>
<summary><code>file00.go</code> L8–10, L18–20, L28–30: 9 of 18 changed lines never ran</summary>

```diff
@@ file00.go  L5–12  F0_1 @@
+    5 │     return y + 2
     6 │ }
     7 │ 
!    8 │ func F0_1(x int) int {
!    9 │     y := x * 3
!   10 │     return y + 2
    11 │ }
    12 │ 
@@ file00.go  L15–22  F0_3 @@
+   15 │     return y + 2
    16 │ }
    17 │ 
!   18 │ func F0_3(x int) int {
!   19 │     y := x * 5
!   20 │     return y + 2
    21 │ }
    22 │ 
@@ file00.go  L25–32  F0_5 @@
+   25 │     return y + 2
    26 │ }
    27 │ 
!   28 │ func F0_5(x int) int {
!   29 │     y := x * 7
!   30 │     return y + 2
    31 │ }
    32 │ 
```

</details>

<details>
<summary>Packages touched: 8, none with a floor yet</summary>

```diff
#  package             floor     now  ±floor           patch   0        50      100           
!  pkg/area00/unit00       —   58.33       —     58.3% 21/36   ███████████▌····│···  target 80
!  pkg/area00/unit01       —   50.00       —     50.0% 18/36   ██████████······│···  target 80
!  pkg/area00/unit02       —   50.00       —     50.0% 18/36   ██████████······│···  target 80
!  pkg/area00/unit03       —   50.00       —     50.0% 18/36   ██████████······│···  target 80
!  pkg/area01/unit04       —   58.33       —     58.3% 21/36   ███████████▌····│···  target 80
!  pkg/area01/unit05       —   58.33       —     58.3% 21/36   ███████████▌····│···  target 80
!  pkg/area01/unit06       —   50.00       —     50.0% 18/36   ██████████······│···  target 80
!  pkg/area01/unit07       —   50.00       —     50.0% 18/36   ██████████······│···  target 80
```

</details>

<details>
<summary>Ratchet available: 1 floor can rise, 8 floors can be created</summary>

Run `coverreport ratchet` and commit `coverage/floors.json`. These are the entries it rewrites:

```diff
@@ coverage/floors.json @@
   "layers": {
-    "go-unit": { "statements": 50.0 }
+    "go-unit": { "statements": 53.1 }
   "packages": {
     "go-unit": {
+      "pkg/area00/unit00": { "statements": 58.3 }
+      "pkg/area00/unit01": { "statements": 50.0 }
+      "pkg/area00/unit02": { "statements": 50.0 }
+      "pkg/area00/unit03": { "statements": 50.0 }
+      "pkg/area01/unit04": { "statements": 58.3 }
+      "pkg/area01/unit05": { "statements": 58.3 }
+      "pkg/area01/unit06": { "statements": 50.0 }
+      "pkg/area01/unit07": { "statements": 50.0 }
```

</details>

<details>
<summary>Left out of the denominator: nothing</summary>

| glob | why | size |
|:--|:--|--:|
| `**/*_templ.go` | templ codegen | nothing measured |
| `libs/go/svc/*/main.go` | thin wiring; the e2e lane measures it | nothing measured |
| `apps/web/src/components/ui/**` | vendored shadcn | nothing measured |
| `apps/mobile/lib/**/*.g.dart` | drift codegen | nothing measured |
| `apps/web/src/legacy/**` | deleted last quarter | nothing measured |

From `coverage/exclude.txt`: every rule is printed with its size, so an exclusion cannot hide code quietly.

</details>

<details>
<summary>How this was measured</summary>

| layer | how | inputs |
|:--|:--|:--|
| go unit | go (mode atomic) | `unit.out` |
| patch | the diff against the base: 16 files, 512 lines added | |

</details>

> [!NOTE]
> Shortened to fit GitHub's 65,536-character comment limit: left out 12 of 16 snippets for files that hold their floors. The report page and the [job summary](https://github.com/acme/demo/actions/runs/18342917305) have everything.

<sub>Line by line: the coverage-561.html artifact of [this run](https://github.com/acme/demo/actions/runs/18342917305). Every package and glob: [job summary](https://github.com/acme/demo/actions/runs/18342917305). Floors: [`coverage/floors.json`](https://github.com/acme/demo/blob/3f2a9c1d5e6f708192a3b4c5d6e7f8091a2b3c4d/coverage/floors.json). Updated for push 1 (71a0d3e) at 15:42 UTC, Sep 28 by coverreport v0.1.0.</sub>
