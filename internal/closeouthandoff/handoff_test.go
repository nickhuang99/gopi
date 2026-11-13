// Close-out hand-off protocol tests (internal/closeouthandoff).
//
// THE CONTRACT these pin, in one sentence: "the chain was already reported for
// THIS row" is a ONE-SHOT statement — consumed by the first consumer that asks
// about that row, never a second time, and never readable about a row it was
// not made for. Every shape below is a way the report-once fix could silently
// degrade into either duplication (a latch that cannot be consumed) or a
// dropped close-out (a latch consumed by the wrong asker).
package closeouthandoff

import "testing"

// TestTakeConsumesOnce: the declaration covers ONE consumer. A second ask about
// the same row must be false — otherwise the far side would believe a report
// was made that nothing will make.
func TestTakeConsumesOnce(t *testing.T) {
	var s State
	s.Declare("t-1", "worktree in use by live process pid 42")
	if ok, detail := s.Take("t-1"); !ok || detail != "worktree in use by live process pid 42" {
		t.Fatalf("first take = (%v, %q), want (true, the armed detail)", ok, detail)
	}
	if ok, detail := s.Take("t-1"); ok || detail != "" {
		t.Fatalf("second take = (%v, %q), want (false, \"\") — the declaration is spent", ok, detail)
	}
}

// TestTakeIsRowScoped is the safety property that keeps a decline honest: a
// consumer asking about a DIFFERENT row must not consume (nor be told) the
// declaration, because its own row may still be un-reported.
func TestTakeIsRowScoped(t *testing.T) {
	var s State
	s.Declare("t-1", "reason")
	if ok, _ := s.Take("t-2"); ok {
		t.Fatal("a declaration for t-1 must not answer an ask about t-2")
	}
	if armed, row := s.Declared(); !armed || row != "t-1" {
		t.Fatalf("the declaration must survive the unrelated ask: armed=%v row=%q", armed, row)
	}
	if ok, _ := s.Take("t-1"); !ok {
		t.Fatal("the row's own ask must still find its declaration")
	}
}

// TestWithdrawRetractsAndIsIdempotent: the declaring writer withdraws its own
// claim when it never reached the report (a return before the print, a panic).
// Withdraw is not a consumer's verb (it takes no row), and a second call is a
// no-op rather than an error.
func TestWithdrawRetractsAndIsIdempotent(t *testing.T) {
	var s State
	s.Declare("t-1", "reason")
	s.Withdraw()
	if ok, _ := s.Take("t-1"); ok {
		t.Fatal("a retracted declaration must not be takeable")
	}
	s.Withdraw() // must not panic
}

// TestZeroValueIsUndeclared is the "every other run is unaffected" property: a
// fresh process (a bare prompt, a chat, a `task run` child) has no resume tail,
// so nothing it does may be muted by this package.
func TestZeroValueIsUndeclared(t *testing.T) {
	var s State
	if armed, row := s.Declared(); armed || row != "" {
		t.Fatalf("zero value = armed=%v row=%q, want disarmed", armed, row)
	}
	if ok, _ := s.Take("t-1"); ok {
		t.Fatal("no declaration exists before one is armed")
	}
}

// TestDeclareKeepsTheNewestReport: declaring twice keeps the NEWEST detail —
// the report that stands is the last one made, not the first remembered.
func TestDeclareKeepsTheNewestReport(t *testing.T) {
	var s State
	s.Declare("t-1", "older")
	s.Declare("t-1", "newer")
	if ok, detail := s.Take("t-1"); !ok || detail != "newer" {
		t.Fatalf("take = (%v, %q), want (true, \"newer\")", ok, detail)
	}
}

// TestPackageLevelLatchIsTheSameOneProcessWide guards the ONE fact keeping this
// design sound: the package-level functions the two surfaces use reach the SAME
// State. Two latches named after one hand-off would arm one and read the other,
// and the duplication this epic removes would come straight back.
func TestPackageLevelLatchIsTheSameOneProcessWide(t *testing.T) {
	Declare("t-shared", "detail")
	if ok, row := Declared(); !ok || row != "t-shared" {
		t.Fatalf("the armed declaration must be observable: armed=%v row=%q", ok, row)
	}
	if ok, _ := Take("t-shared"); !ok {
		t.Fatal("Take must consume the declaration Arm made")
	}
	Withdraw() // leave the process as it was found
}
