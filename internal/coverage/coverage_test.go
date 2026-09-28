package coverage

import (
	"reflect"
	"testing"
)

// A floor is written rounded DOWN so it is never above the measurement it
// came from; rounding to nearest would write 66.7 for 2/3 and the very next
// check of the same numbers would fail.
func TestFloorTenthsRoundsDown(t *testing.T) {
	cases := []struct {
		c    Count
		want int64
	}{
		{Count{2, 3}, 666},        // 66.666 -> 66.6, not 66.7
		{Count{1, 1}, 1000},       // 100.0
		{Count{9999, 10000}, 999}, // 99.99 -> 99.9, never 100.0
		{Count{0, 7}, 0},
		{Count{7789, 10000}, 778}, // 77.89 -> 77.8
		{Count{21717, 28594}, 759},
		{Count{0, 0}, 0},
	}
	for _, tc := range cases {
		if got := tc.c.FloorTenths(); got != tc.want {
			t.Errorf("%+v.FloorTenths() = %d, want %d", tc.c, got, tc.want)
		}
	}
}

// AtLeast is the gate. Floors minus tolerance are integers in hundredths,
// so the boundary is exact: 77.8 - 0.1 is 7770, and a float subtraction
// (77.69999999999999) never gets a say.
func TestAtLeastIsExactAtTheBoundary(t *testing.T) {
	cases := []struct {
		name string
		c    Count
		h    int64
		want bool
	}{
		{"exactly at threshold", Count{777, 1000}, 7770, true},
		{"one unit below", Count{7769, 10000}, 7770, false},
		{"one unit above", Count{7771, 10000}, 7770, true},
		{"80% of 5 lines passes an 80 target", Count{4, 5}, 8000, true},
		{"3 of 4 misses an 80 target", Count{3, 4}, 8000, false},
		{"negative threshold always passes", Count{0, 5}, -10, true},
		{"no data never passes", Count{0, 0}, 0, false},
	}
	for _, tc := range cases {
		if got := tc.c.AtLeast(tc.h); got != tc.want {
			t.Errorf("%s: %+v.AtLeast(%d) = %v, want %v", tc.name, tc.c, tc.h, got, tc.want)
		}
	}
}

func TestHundredths(t *testing.T) {
	for in, want := range map[float64]int64{77.8: 7780, 0.1: 10, 80: 8000, 99.95: 9995, 0: 0} {
		if got := Hundredths(in); got != want {
			t.Errorf("Hundredths(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestRanges(t *testing.T) {
	cases := []struct {
		in   []int
		want []Range
	}{
		{nil, []Range{}},
		{[]int{5}, []Range{{5, 5}}},
		{[]int{3, 4, 5, 9}, []Range{{3, 5}, {9, 9}}},
		{[]int{9, 3, 5, 4, 4}, []Range{{3, 5}, {9, 9}}}, // unsorted, duplicate
		{[]int{1, 3}, []Range{{1, 1}, {3, 3}}},          // a gap of one splits
	}
	for _, tc := range cases {
		if got := Ranges(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Ranges(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPct2(t *testing.T) {
	if got := (Count{2, 3}).Pct2(); got != 66.67 {
		t.Errorf("Pct2 = %v, want 66.67", got)
	}
	if got := (Count{0, 0}).Pct2(); got != 0 {
		t.Errorf("Pct2 of no data = %v, want 0", got)
	}
}
