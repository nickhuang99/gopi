// The temp-root HEADROOM criterion — red→green on the smallest sample.
//
// WHAT IS UNDER TEST: `judgeTmpDirHeadroom` (internal/scratch/tmproot.go), the
// core both the stderr warning and `gopi config show` read. Its floor AND its
// measuring function are parameters, so the criterion is exercised with an
// INJECTED reading: no disk is filled and no load is generated to produce a
// number (Rule 27 forbids that, and a real ENOSPC is not reproducible on demand
// anyway).
//
// THE RED PROOF (recorded in the report): before the criterion existed there was
// no function to call at all — the observation this test makes was impossible,
// which is why the defect it guards (a nearly-full temp root reported as
// nothing) shipped in the first place. The green half is the table below, run
// against the tree.

package scratch

import (
	"errors"
	"strings"
	"testing"
)

// headroomInjectedFree / headroomInjectedTotal are the readings the injected
// measurer returns unless a case overrides them. Named (Rule 1b).
const (
	headroomInjectedFree  = uint64(2_300 << 20) // the 2.3 GiB the epic was briefed in
	headroomInjectedTotal = uint64(3_600 << 30)
	headroomFloorFixture  = uint64(4 << 30)
	headroomPlentyFree    = uint64(91 << 30)
	headroomErrFixture    = "statfs fixture: synthetic failure"
)

// TestJudgeTmpDirHeadroomVerdictAndMessage pins the verdict AND the sentence: a
// verdict with no numbers behind it is what a reader cannot act on, so every
// assertion below reads the printed line, not a boolean.
func TestJudgeTmpDirHeadroomVerdictAndMessage(t *testing.T) {
	measure := func(uint64) func(string) (uint64, uint64, error) {
		return func(string) (uint64, uint64, error) { return headroomInjectedFree, headroomInjectedTotal, nil }
	}

	t.Run("below_floor_is_low_and_says_where_and_how_to_reclaim", func(t *testing.T) {
		dir := t.TempDir() // a REAL directory: the device half is measured, never injected
		h := judgeTmpDirHeadroom(dir, "from the built-in default", headroomFloorFixture, measure(0))
		if !h.Low {
			t.Fatalf("2.3 GiB free against a 4 GiB floor must be LOW; got %+v", h)
		}
		if h.Device == 0 {
			t.Fatalf("the reading carries no device — the partition question must be MEASURED (Rule 22): %+v", h)
		}
		msg := h.Warning()
		for _, want := range []string{
			ErrTmpDirLowHeadroom.Error(),
			dir,
			FormatBytes(headroomInjectedFree),
			FormatBytes(headroomFloorFixture),
			"no space left on device",
			"scratch-gc gocache",
			"NOT deleted",
		} {
			if !strings.Contains(msg, want) {
				t.Fatalf("the warning does not carry %q — a reader cannot act on it:\n%s", want, msg)
			}
		}
		report := h.Report()
		if !strings.HasPrefix(report, tmpDirHeadroomLowMarker+":") {
			t.Fatalf("the report row must open with the %q marker so a skim finds it; got %q",
				tmpDirHeadroomLowMarker, report)
		}
	})

	t.Run("above_floor_is_ok_and_silent", func(t *testing.T) {
		plenty := func(string) (uint64, uint64, error) {
			return headroomPlentyFree, headroomInjectedTotal, nil
		}
		h := judgeTmpDirHeadroom(t.TempDir(), "from the built-in default", headroomFloorFixture, plenty)
		if h.Low {
			t.Fatalf("91 GiB free against a 4 GiB floor must NOT be LOW; got %+v", h)
		}
		if !strings.HasPrefix(h.Report(), tmpDirHeadroomOKMarker+":") {
			t.Fatalf("the report row must open with the %q marker; got %q", tmpDirHeadroomOKMarker, h.Report())
		}
	})

	t.Run("zero_floor_disables_the_arm", func(t *testing.T) {
		h := judgeTmpDirHeadroom(t.TempDir(), "", 0, measure(0))
		if h.Low {
			t.Fatalf("a zero floor must disable the criterion (the explicit measure-only form), got %+v", h)
		}
	})

	t.Run("unmeasurable_root_is_a_fault_never_a_pass", func(t *testing.T) {
		failing := func(string) (uint64, uint64, error) {
			return 0, 0, errors.New(headroomErrFixture)
		}
		h := judgeTmpDirHeadroom(t.TempDir(), "", headroomFloorFixture, failing)
		if h.MeasureErr == nil {
			t.Fatal("a failed measurement must be reported, not read as 'no problem' (Rule 15)")
		}
		if !strings.Contains(h.Report(), headroomErrFixture) {
			t.Fatalf("the report must name the failure's own text; got %q", h.Report())
		}
	})
}

// TestTmpDirHeadroomFloorResolvesAndRefusesGarbage pins the ONE resolver of the
// floor: the constant by default, the env override when it parses, and the
// constant again (with a printed line) when it does not — a typo must never read
// as a zero floor that silently disables the criterion.
func TestTmpDirHeadroomFloorResolvesAndRefusesGarbage(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv(TmpDirHeadroomFloorEnvName, "")
		if got := TmpDirHeadroomFloor(); got != TmpDirHeadroomFloorBytes {
			t.Fatalf("no override must resolve to the named floor %d, got %d", TmpDirHeadroomFloorBytes, got)
		}
	})
	t.Run("override_in_bytes", func(t *testing.T) {
		t.Setenv(TmpDirHeadroomFloorEnvName, "1024")
		if got := TmpDirHeadroomFloor(); got != 1024 {
			t.Fatalf("a valid override must be used verbatim, got %d", got)
		}
	})
	t.Run("garbage_falls_back_to_the_floor", func(t *testing.T) {
		t.Setenv(TmpDirHeadroomFloorEnvName, "2GiB")
		if got := TmpDirHeadroomFloor(); got != TmpDirHeadroomFloorBytes {
			t.Fatalf("an unparseable override must fall back to %d, not to 0 (which would disable the "+
				"criterion silently); got %d", TmpDirHeadroomFloorBytes, got)
		}
	})
}

// TestReportTmpDirHeadroomIfLowGate pins the non-fatal contract the publisher
// depends on: a root with room returns nil and writes nothing, a low root
// returns the sentinel (so a caller can match it with errors.Is) — and, because
// the stderr print is once-per-process, this test asserts the RETURN VALUE
// rather than capturing stderr, which a sibling test would have consumed.
func TestReportTmpDirHeadroomIfLowGate(t *testing.T) {
	t.Setenv(TmpDirHeadroomFloorEnvName, "0")
	if err := ReportTmpDirHeadroomIfLow(t.TempDir(), "from a test fixture"); err != nil {
		t.Fatalf("a zero floor must make the check silent, got %v", err)
	}

	t.Setenv(TmpDirHeadroomFloorEnvName, "1125899906842624") // 1 PiB: this host's disks are 3.6T
	err := ReportTmpDirHeadroomIfLow(t.TempDir(), "from a test fixture")
	if !errors.Is(err, ErrTmpDirLowHeadroom) {
		t.Fatalf("an unreachable floor must return ErrTmpDirLowHeadroom; got %v", err)
	}
}
