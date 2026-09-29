package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/arrayofone/coverreport/internal/brand"
	"github.com/arrayofone/coverreport/internal/coverage"
	"github.com/arrayofone/coverreport/internal/render/rendertest"
	"github.com/arrayofone/coverreport/internal/report"
	"github.com/arrayofone/coverreport/internal/source"
)

func states(t *testing.T) map[string]*View {
	t.Helper()
	out := map[string]*View{}
	for _, s := range rendertest.States(t) {
		root, err := source.Open(s.SourceDir)
		if err != nil {
			t.Fatal(err)
		}
		v, err := Build(s.Report, Options{Source: root, Previous: s.Previous, ArtifactURL: s.ArtifactURL, Version: "test"})
		if err != nil {
			t.Fatal(err)
		}
		out[s.Name] = v
	}
	return out
}

func rowStates(v *View) string {
	var parts []string
	for _, r := range v.Rows {
		parts = append(parts, r.Label+"="+r.State)
	}
	return strings.Join(parts, " ")
}

// The whole state vocabulary, per golden state: what each row is, and what
// the verdict and heading say.
func TestRowStatesVerdictsAndHeadings(t *testing.T) {
	vs := states(t)
	for _, tc := range []struct {
		state, rows, verdict, heading string
	}{
		{"ok", "go unit=ok go sql=untouched go live=ok go e2e=info web lines=ok web branches=ok web functions=ok mobile=untouched edge=notrun",
			VerdictOK, "every floor holds"},
		{"fail", "go unit=fail go sql=warn go live=warn go e2e=info web lines=ok web branches=ok web functions=ok mobile=untouched edge=notrun",
			VerdictFail, "1 floor broken"},
		{"warn", "go unit=warn go sql=warn go live=warn go e2e=info web lines=ok web branches=ok web functions=ok mobile=untouched edge=notrun",
			VerdictWarn, "floors hold, patch under 80%"},
		{"first-run", "go unit=warn go sql=warn go live=warn go e2e=info web lines=new web branches=new web functions=new mobile=new edge=notrun",
			VerdictNone, "no floors yet, patch under 80%"},
		{"carried", "go unit=ok go sql=untouched go live=ok go e2e=info web lines=ok web branches=ok web functions=ok mobile=carried edge=notrun",
			VerdictOK, "every floor holds"},
		{"multi", "go unit=fail go sql=warn go live=warn go e2e=info web lines=ok web branches=ok web functions=ok mobile=untouched edge=notrun",
			VerdictFail, "2 floors broken, patch under 80% in 3 layers, endpoint baseline broken, edge did not report"},
		{"large", "go unit=warn", VerdictWarn, "floors hold, patch under 80%"},
	} {
		v := vs[tc.state]
		if got := rowStates(v); got != tc.rows {
			t.Errorf("%s rows:\n got %s\nwant %s", tc.state, got, tc.rows)
		}
		if v.Verdict != tc.verdict || v.Heading != tc.heading {
			t.Errorf("%s: verdict %q heading %q, want %q %q", tc.state, v.Verdict, v.Heading, tc.verdict, tc.heading)
		}
	}
	// Markers follow the states: the HUD's colour is the diff grammar's.
	for _, r := range vs["fail"].Rows {
		want := map[string]string{StateOK: "+", StateWarn: "!", StateFail: "-"}[r.State]
		if want == "" {
			want = "#"
		}
		if r.Marker() != want {
			t.Errorf("%s: marker %q for state %s", r.Label, r.Marker(), r.State)
		}
	}
}

// A carried row shows the base branch's number, and nothing about it is
// gated: the layer stays not_measured in the report.
func TestCarriedRowShowsTheBaselineNumber(t *testing.T) {
	v := states(t)["carried"]
	var mobile *Row
	for _, r := range v.Rows {
		if r.Layer.ID == "mobile" {
			mobile = r
		}
	}
	if mobile.Pct == nil || *mobile.Pct != 66.67 || mobile.CarriedSHA != "bb1595fe0d7c2a61e3f4b5a6978c0d1e2f3a4b5c" || mobile.Delta == nil || *mobile.Delta != 0.07 {
		t.Fatalf("carried row: %+v", mobile)
	}
	if mobile.Layer.Status != report.StatusNotMeasured || v.R.Status != report.StatusPass {
		t.Error("carrying changed a status")
	}
}

func TestProblemsAreClassifiedAndLinked(t *testing.T) {
	v := states(t)["multi"]
	var kinds []string
	for _, p := range v.Problems {
		kinds = append(kinds, p.Kind)
	}
	if got := strings.Join(kinds, ","); got != "floor,package,patch,patch,patch,endpoints,required" {
		t.Fatalf("kinds %s", got)
	}
	floor, pkg := v.Problems[0], v.Problems[1]
	if floor.Subject() != "go unit" || floor.Predicate(80) != "is at 61.54%, 0.46 below its 62.0 floor" {
		t.Errorf("floor: %q %q", floor.Subject(), floor.Predicate(80))
	}
	if len(floor.Files) != 1 || floor.Files[0].Path != "libs/go/calc/calc.go" {
		t.Errorf("floor files: %v", floor.Files)
	}
	// The package floor is libs/go/calc in go-live: both changed Go files
	// live in it.
	if pkg.Subject() != "package libs/go/calc (go live)" || len(pkg.Files) != 2 {
		t.Errorf("package: %q, %d files", pkg.Subject(), len(pkg.Files))
	}
	ep := v.Problems[5]
	if len(ep.Violations) != 2 || ep.Violations[0].Detail != "gained a gap: authz" {
		t.Errorf("endpoint violations: %+v", ep.Violations)
	}
	if v.EndpointViolations["route"] != 1 || v.EndpointViolations["event"] != 1 {
		t.Errorf("per kind: %v", v.EndpointViolations)
	}
	if req := v.Problems[6]; req.Predicate(80) != "did not report: none of its inputs exist (coverage-edge/lcov.info)" {
		t.Errorf("required: %q", req.Predicate(80))
	}
}

// Which changed lines are shown as never run: coverable and run by no gated
// layer, or not run by a layer the file fails. Ranges join across changed
// lines that carry no code.
func TestChangedLinesThatNeverRan(t *testing.T) {
	v := states(t)["fail"]
	byPath := map[string]*File{}
	for _, f := range v.Files {
		byPath[f.Path] = f
	}
	calc := byPath["libs/go/calc/calc.go"]
	if calc.Idx != 1 || !calc.Failing["go-unit"] || calc.Failing["go-live"] {
		t.Errorf("calc.go: idx %d failing %v", calc.Idx, calc.Failing)
	}
	var rs []string
	for _, u := range calc.Uncovered {
		rs = append(rs, fmt.Sprintf("%s %s %v", RangeLabel(u.Range), u.Func, u.Fails))
	}
	if got := strings.Join(rs, "; "); got != "L17–18 Classify true; L35–39 Unused true" {
		t.Errorf("calc.go ranges: %s", got)
	}
	if calc.Ran != 0 || calc.Total != 6 || calc.State(38) != LineChg || calc.State(36) != LineChgUnc || calc.State(12) != LineCtxCov || calc.State(22) != LineCtxUnc {
		t.Errorf("calc.go counts/states: %d/%d %c %c %c %c", calc.Ran, calc.Total, calc.State(38), calc.State(36), calc.State(12), calc.State(22))
	}
	store := byPath["libs/go/calc/store_pg.go"]
	if store.Ran != 3 || store.Total != 4 || len(store.Uncovered) != 1 || store.Uncovered[0].Range != (coverage.Range{9, 9}) || store.AnyFailing() {
		t.Errorf("store_pg.go: %d/%d %+v", store.Ran, store.Total, store.Uncovered)
	}
	if got := store.StripText(10); got != "■■□■" {
		t.Errorf("strip %q", got)
	}
}

// A line a gated layer did run is still flagged when a layer the file
// fails did not run it: that is the line holding the broken floor down.
func TestFailingLayerFlagsLinesAnotherLayerRan(t *testing.T) {
	pct := func(v float64) *float64 { return &v }
	r := &report.Report{
		Version: 1, Status: report.StatusFail, Metadata: report.Metadata{Repo: "o/r", SHA: "abc1234"},
		FloorsFile: "coverage/floors.json",
		Failures:   []report.Failure{{Scope: report.FailLayer, Layer: "unit", Metric: "statements", Measured: pct(10), Floor: pct(50)}},
		Layers: []report.Layer{
			{ID: "unit", Label: "unit", Format: "go", Status: report.StatusFail, Metrics: []string{"statements"},
				Totals: map[string]report.MetricResult{"statements": {Covered: 1, Total: 10, Pct: pct(10), Floor: pct(50), Status: report.StatusFail}}},
			{ID: "live", Label: "live", Format: "go", Status: report.StatusOK, Metrics: []string{"statements"},
				Totals: map[string]report.MetricResult{"statements": {Covered: 9, Total: 10, Pct: pct(90), Status: report.StatusNoFloor}}},
		},
		Patch: report.Patch{Status: report.StatusFail, Target: 80, MinLines: 1, Layers: []report.PatchLayer{
			{Layer: "unit", Status: report.StatusFail, PatchCount: report.PatchCount{Covered: 0, Total: 2}, Files: []report.PatchFile{{
				Path: "a/x.go", Changed: []coverage.Range{{3, 4}}, UncoveredChanged: []coverage.Range{{3, 4}}, UncoveredLines: []coverage.Range{{3, 4}}}}},
			{Layer: "live", Status: report.StatusPass, PatchCount: report.PatchCount{Covered: 2, Total: 2}, Files: []report.PatchFile{{
				Path: "a/x.go", Changed: []coverage.Range{{3, 4}}, UncoveredChanged: []coverage.Range{}, CoveredLines: []coverage.Range{{3, 4}}}}},
		}},
	}
	v, err := Build(r, Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := v.Files[0]
	if f.Ran != 0 || f.Missing() != 2 || len(f.Uncovered) != 1 || !f.Uncovered[0].Fails {
		t.Fatalf("flagged: ran %d missing %d %+v", f.Ran, f.Missing(), f.Uncovered)
	}
	if len(f.Uncovered[0].Layers) != 1 || f.Uncovered[0].Layers[0].ID != "unit" {
		t.Errorf("the range is charged to %v, want only unit", f.Uncovered[0].Layers)
	}
	if len(v.Annotations) != 1 || v.Annotations[0].Level != "warning" {
		t.Errorf("annotations: %+v", v.Annotations)
	}
	// Once the unit floor holds, the live run's coverage stands: nothing
	// is flagged.
	r.Failures, r.Status = nil, report.StatusPass
	r.Layers[0].Totals["statements"] = report.MetricResult{Covered: 1, Total: 10, Pct: pct(10), Floor: pct(5), Status: report.StatusOK}
	v, _ = Build(r, Options{})
	if f := v.Files[0]; f.Missing() != 0 || len(v.Annotations) != 0 {
		t.Errorf("with the floor holding: missing %d, %d annotations", f.Missing(), len(v.Annotations))
	}
}

func TestAnnotationsAreCappedAndOrdered(t *testing.T) {
	vs := states(t)
	if n := len(vs["large"].Annotations); n != MaxAnnotations {
		t.Errorf("large: %d annotations, want the cap %d", n, MaxAnnotations)
	}
	fail := vs["fail"].Annotations
	if len(fail) != 4 || fail[0].Level != "warning" || fail[1].Level != "warning" || fail[2].Level != "notice" {
		t.Fatalf("fail: %+v", fail)
	}
	// Longest failing range first.
	if fail[0].Line != 35 || fail[0].EndLine != 39 || fail[1].Line != 17 {
		t.Errorf("order: %+v", fail[:2])
	}
	if !strings.Contains(fail[0].Message, "4 changed lines in Unused never ran under go unit or go live, starting with:\n    func Unused() int {") ||
		!strings.Contains(fail[0].Message, "#f1-L35") {
		t.Errorf("message: %q", fail[0].Message)
	}
	for _, a := range vs["ok"].Annotations {
		t.Errorf("ok state annotates %+v", a)
	}
}

func TestAnnotationCommandEscaping(t *testing.T) {
	a := Annotation{Level: "notice", File: "a,b:c.go", Line: 3, EndLine: 4, Title: "L3–4 (x: y, z)", Message: "100% done\nnext\r"}
	want := "::notice file=a%2Cb%3Ac.go,line=3,endLine=4,title=L3–4 (x%3A y%2C z)::100%25 done%0Anext%0D"
	if got := a.Command(); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	pushes := []Push{{"8e41d07", 58.3}, {"c2b19f4", 76.9}}
	body := Marker + "\n" + StateLine(7, pushes) + "\n### text"
	got, n := ParseState(body)
	if n != 7 || len(got) != 2 || got[1] != (Push{"c2b19f4", 76.9}) {
		t.Fatalf("round trip: %v %d", got, n)
	}
	// Anything unreadable is an empty history, never an error.
	for _, bad := range []string{
		"", "no state", `<!-- coverreport:state {"pushes":[["zz",1]]} -->`, `<!-- coverreport:state {"pushes":[["abc",101]]} -->`,
		`<!-- coverreport:state {"pushes":[["abc","x"]]} -->`, `<!-- coverreport:state not json -->`,
		`<!-- coverreport:state {"pushes":[["<script>",1]]} -->`,
	} {
		if p, n := ParseState(bad); p != nil || n != 0 {
			t.Errorf("%q parsed as %v %d", bad, p, n)
		}
	}
	// The design's original state line (no "n") still reads.
	if p, n := ParseState(`<!-- coverreport:state {"pushes":[["8e41d07",58.3],["3f2a9c1",86.4]]} -->`); n != 2 || len(p) != 2 {
		t.Errorf("no n: %v %d", p, n)
	}
}

func TestHistoryAppendsReplacesAndCaps(t *testing.T) {
	v := states(t)["fail"]
	if v.PushNo != 4 || len(v.Pushes) != 4 || v.Pushes[3] != (Push{"71a0d3e", 46.7}) {
		t.Fatalf("appended: %d %v", v.PushNo, v.Pushes)
	}
	// Re-running the same push replaces its point instead of adding one.
	again := &View{R: v.R, Patch: v.Patch}
	again.history(Marker + "\n" + StateLine(v.PushNo, v.Pushes))
	if again.PushNo != 4 || len(again.Pushes) != 4 {
		t.Errorf("a re-run added a push: %d %v", again.PushNo, again.Pushes)
	}
	var many []Push
	for i := 0; i < MaxPushes; i++ {
		many = append(many, Push{fmt.Sprintf("%07x", i+1), float64(i)})
	}
	capped := &View{R: v.R, Patch: v.Patch}
	capped.history(StateLine(45, many))
	if capped.PushNo != 46 || len(capped.Pushes) != MaxPushes || capped.Pushes[0].SHA != "0000002" {
		t.Errorf("cap: n %d, %d kept, first %s", capped.PushNo, len(capped.Pushes), capped.Pushes[0].SHA)
	}
}

func TestFuncContext(t *testing.T) {
	src := strings.Split(`package x

func (s *Server) handleChargeRefunded(ctx context.Context) error {
	if err != nil {
		return err
	}
}

export async function invokePaymentsRoute(route: Route) {
  return 1;
}
export const executeAction = async <T>(a: T) => {
  return a;
};
export default class DisputeNotice extends Component {
  render() {
    return null;
  }
}
class _QuoteDetailScreenState extends State<QuoteDetailScreen> {
  Widget build(BuildContext context) {
Widget _totalRow(Tokens t, String label) {
  return Text(label);
}`, "\n")
	for n, want := range map[int]string{1: "", 2: "", 5: "handleChargeRefunded", 10: "invokePaymentsRoute", 13: "executeAction", 17: "DisputeNotice", 21: "_QuoteDetailScreenState", 23: "_totalRow"} {
		if got := FuncContext(src, n); got != want {
			t.Errorf("line %d: %q, want %q", n, got, want)
		}
	}
	if FuncContext(nil, 5) != "" {
		t.Error("no source, no context")
	}
}

func TestGlyphs(t *testing.T) {
	target := 85.0
	for _, tc := range []struct {
		v      float64
		target *float64
		report bool
		want   string
	}{
		{83.21, &target, false, "████████████████▌│··"},
		{40.07, &target, false, "████████·········│··"},
		{100, &target, false, "████████████████████"},
		{0, nil, false, "····················"},
		{58.6, nil, true, "▒▒▒▒▒▒▒▒▒▒▒·········"},
		{92.5, &target, false, "██████████████████▌·"},
	} {
		if got := Meter(tc.v, tc.target, tc.report); got != tc.want {
			t.Errorf("Meter(%v) = %s, want %s", tc.v, got, tc.want)
		}
	}
	if got := Spark([]float64{58.3, 76.9, 83.1, 86.4}); got != "▁▆▇█" {
		t.Errorf("spark %s", got)
	}
	// A wobble under the gate's resolution stays flat.
	if got := Spark([]float64{77.80, 77.81, 77.79, 77.80}); got != "▄▅▄▄" && got != "▄▄▄▄" {
		t.Errorf("wobble %s", got)
	}
	if got := PadBlock([]string{"ab", "abcd", ""}); got != "ab  \nabcd\n    " {
		t.Errorf("pad %q", got)
	}
}

func TestLinksEscapePaths(t *testing.T) {
	l := Links{Server: "https://github.com", Repo: "o/r", SHA: "abc", PR: 5}
	if got := l.Lines("src/app/(auth)/pay/[id]/a b#.tsx", coverage.Range{3, 4}); got != "https://github.com/o/r/blob/abc/src/app/%28auth%29/pay/%5Bid%5D/a%20b%23.tsx#L3-L4" &&
		got != "https://github.com/o/r/blob/abc/src/app/(auth)/pay/%5Bid%5D/a%20b%23.tsx#L3-L4" {
		t.Errorf("lines url %s", got)
	}
	if (Links{}).Blob("x") != "" || (Links{Repo: "o/r"}).PRURL() != "" {
		t.Error("unknown links must be empty, not half-built")
	}
}

func TestRatchetEntriesFollowFloorsOrder(t *testing.T) {
	v := states(t)["ok"]
	var keys []string
	for _, e := range v.Ratchets {
		keys = append(keys, e.Scope+":"+e.Layer+":"+e.Key)
	}
	want := "layer:go-live:,layer:go-unit:,layer:web:,package:go-live:libs/go/calc,package:go-unit:libs/go/calc,glob:go-unit:libs/go/calc/**,glob:web:apps/web/src/lib/**,glob:web:apps/web/src/{components,app}/**"
	if got := strings.Join(keys, ","); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
	web := v.Ratchets[2]
	if web.New || web.Moves() != "branches 70.0 to 75.0, lines 80.0 to 87.5" {
		t.Errorf("web moves %q", web.Moves())
	}
	if v.Rises() != 3 {
		t.Errorf("rises %d", v.Rises())
	}
}

func TestBrandErrorsSurface(t *testing.T) {
	r := states(t)["ok"].R
	bad := *r
	bad.Brand = &brand.Config{Dark: map[string]string{"ok": "red;}"}}
	if _, err := Build(&bad, Options{}); err == nil {
		t.Error("an invalid brand in a report was rendered")
	}
}

func TestEndpointKind(t *testing.T) {
	for id, want := range map[string]string{"svc-a:POST /x/{id}": "route", "svc-worker:event email.send": "event", "app:page /pay/[n]": "page",
		"app:layout /m": "layout", "app:action createInvoice": "action", "svc-worker:cron sweep": "cron", "nocolon": ""} {
		if got := EndpointKind(id); got != want {
			t.Errorf("%s: %q", id, got)
		}
	}
}

// The layers heading counts rows by state, and its one verb agrees with
// the count: "1 holds", "2 hold".
func TestStateCountsAgreeWithTheirCounts(t *testing.T) {
	v := &View{R: &report.Report{}}
	for _, tc := range []struct {
		states []string
		want   string
	}{
		{[]string{StateOK, StateFail}, "1 holds, 1 below floor"},
		{[]string{StateOK, StateOK, StateNotRun}, "2 hold, 1 not run"},
		{[]string{StateNew, StateNew, StateNew}, "3 without a floor"},
	} {
		v.Rows = nil
		for _, s := range tc.states {
			v.Rows = append(v.Rows, &Row{State: s})
		}
		if got := v.StateCounts(); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.states, got, tc.want)
		}
	}
}

// A count in a table cell carries its unit in the count's number: "1
// stmt", "6,539 stmts", "1 branch", never "1 lines".
func TestCountShort(t *testing.T) {
	for _, tc := range []struct {
		metric string
		n      int64
		want   string
	}{
		{"statements", 1, "1 stmt"}, {"statements", 6539, "6,539 stmts"}, {"lines", 1, "1 line"}, {"lines", 0, "0 lines"},
		{"branches", 1, "1 branch"}, {"functions", 2, "2 functions"},
	} {
		if got := CountShort(tc.metric, tc.n); got != tc.want {
			t.Errorf("CountShort(%s, %d) = %q, want %q", tc.metric, tc.n, got, tc.want)
		}
	}
}
