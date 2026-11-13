package pricing

import (
	"testing"
	"time"
)

// TestDeepseekPeakWindowsAreTheProvidersHours pins the ONE definition of
// DeepSeek's 高峰时段 (Beijing 09:00-12:00 and 14:00-18:00, billed at 2x on every
// dimension).
//
// WHY IT LIVES HERE: these windows used to be owned by an internal/peakhours
// package shared with the peak-hour START GUARD. The guard was removed outright
// (devdoc/peak-guard-removal.md) and the package with it; a rate window that can
// no longer refuse anything belongs to the RATE TABLE, which is the only
// consumer left (resolve → inPeakWindow). The boundary coverage the deleted
// package carried is re-established here, in the package that now owns the
// values — a deletion must not leave a fact unpinned (PROJECT.md Rule 5).
//
// The windows are the PROVIDER'S rate card, not a gopi policy: 2x billing is a
// fact about DeepSeek, so `gopi session stats --cost` must keep reporting it.
func TestDeepseekPeakWindowsAreTheProvidersHours(t *testing.T) {
	want := []peakWindow{{Start: 9, End: 12}, {Start: 14, End: 18}}
	if len(deepseekPeakWindows) != len(want) {
		t.Fatalf("deepseekPeakWindows = %+v, want %+v", deepseekPeakWindows, want)
	}
	for i, w := range want {
		if deepseekPeakWindows[i] != w {
			t.Errorf("deepseekPeakWindows[%d] = %+v, want %+v — the priced regime and "+
				"`gopi session stats --cost` must agree with the provider's 高峰时段", i, deepseekPeakWindows[i], w)
		}
	}
}

// TestInPeakWindowBoundaries pins the half-open semantics and the exact edges:
// Start inclusive, End exclusive, in Beijing civil time. The boundary is where a
// money number changes, so an off-by-one here silently doubles or halves a
// session's reported cost — it is asserted at every edge rather than sampled
// (PROJECT.md Rule 10.4: verify the smallest, most extreme sample).
func TestInPeakWindowBoundaries(t *testing.T) {
	cases := []struct {
		hour int
		want bool
	}{
		{0, false}, {8, false}, {9, true}, {10, true}, {11, true},
		{12, false}, {13, false}, {14, true}, {15, true}, {17, true},
		{18, false}, {19, false}, {23, false},
	}
	for _, c := range cases {
		at := time.Date(2026, time.September, 7, c.hour, 0, 0, 0, beijing)
		if got := inPeakWindow(at); got != c.want {
			t.Errorf("inPeakWindow(Beijing %02d:00) = %v, want %v — windows are inclusive-start, "+
				"exclusive-end (%+v)", c.hour, got, c.want, deepseekPeakWindows)
		}
	}
}

// TestBeijingIsThePricedZone pins the fixed +08:00 zone the regime clocks are
// expressed in. It is a FIXED zone rather than time.LoadLocation on purpose: a
// host whose tzdata is missing must still price a session identically, and the
// provider's clock never depends on the operator's locale.
func TestBeijingIsThePricedZone(t *testing.T) {
	_, offset := time.Date(2026, time.September, 7, 12, 0, 0, 0, Beijing()).Zone()
	if offset != beijingOffsetSec {
		t.Errorf("Beijing() offset = %ds, want %ds — every priced regime clock is Beijing civil time",
			offset, beijingOffsetSec)
	}
}
