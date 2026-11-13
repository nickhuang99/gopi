package uiclient

// dump_settle_test.go — the SETTLE seam's own guard: the artifact must be
// writable at a moment the CALLER judges the screen final, and not only when the
// paint stream happens to change.
//
// WHY THIS IS SEPARATE FROM THE PTY CASE (cmd/agent/chat_turn_dump_grid_pty_test.go).
// That case drives a real chat turn and asserts the READER's truth — the artifact
// carries the whole answer, with no T1 repetition and no T2 style break. Its
// clock is the real one, so on a machine fast enough the throttle's own writes
// land past the turn's tail and the artifact is correct WITHOUT a settle: the pty
// case therefore proves the frame is right, and cannot prove the settle is
// load-bearing. The clock here is the seam's own (ScreenDumper.now), held STILL,
// which makes every paint write fall inside the interval — the exact state the
// producer's freeze was measured in (`frames=35 dumps=28`, the answer absent over
// 40 samples). Under that clock the settle is the only thing that can install the
// frame, and Rule 31's requirement is met at the seam the change was made at.

import (
	"strings"
	"testing"
	"time"
)

// TestScreenDumperSettlesAFrameTheThrottleHeld pins the whole contract in one
// place: a held frame is installed by Settle, a second Settle changes nothing,
// and a CLOSED sink stays closed.
func TestScreenDumperSettlesAFrameTheThrottleHeld(t *testing.T) {
	d, path := newTestDumper(t, DumpModeANSI, 24, 3)

	// Hold the clock STILL: from here on, no paint write is ever "due", so the
	// throttle cannot install anything. This is the producer's measured state —
	// an observed run whose last frame the interval swallowed.
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base }
	settledBefore := d.Writes() //screen-ok: reads the install COUNT before the frame, to state the delta the settle is responsible for — no screen is awaited

	// A whole turn's tail, painted the way a chat REPL paints it: absolute
	// placement per row, ending at the answer's last line.
	tail := "\x1b[1;1Hline one\x1b[2;1Hline two\x1b[3;1Hthe answer ends here"
	if _, err := d.Write([]byte(tail)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The throttle held it: the grid knows the frame, the FILE does not.
	if got := d.Snapshot().Lines[2]; got != "the answer ends here" {
		t.Fatalf("the grid did not interpret the tail: row 3 = %q", got)
	}
	if body := readFile(t, path); strings.Contains(body, "the answer ends here") {
		t.Fatalf("the artifact already carries the tail before the settle — the throttle did not hold it, so this test would not be measuring the settle at all:\n%s", body)
	}
	if got := d.Writes(); got != settledBefore { //screen-ok: measures that the throttle SUPPRESSED an install — the awaited fact IS the install count, and it is asserted synchronously after Write returned
		t.Fatalf("a paint write inside the interval installed a dump (dumps %d -> %d, want no install)", settledBefore, got)
	}

	// THE SETTLE: the caller says the screen is final, and the frame becomes the
	// artifact's state even though it awaits no future change.
	if err := d.Settle(); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	body := readFile(t, path)
	if !strings.Contains(body, "the answer ends here") {
		t.Fatalf("Settle did not install the held frame — the artifact still reads:\n%s", body)
	}
	if got := d.Writes(); got != settledBefore+1 { //screen-ok: measures the install the SETTLE earned — asserted synchronously after Settle returned, so no producer is being waited on
		t.Fatalf("Settle installed %d dumps, want exactly 1", got-settledBefore)
	}

	// A SECOND settle is idempotent in the sense that matters: the artifact is
	// still correct, and the frame it states is the same one.
	if err := d.Settle(); err != nil {
		t.Fatalf("second Settle: %v", err)
	}
	if body2 := readFile(t, path); !strings.Contains(body2, "the answer ends here") {
		t.Fatalf("a second Settle changed the artifact's frame:\n%s", body2)
	}

	// A CLOSED sink stays closed: Close's install is the deliverable, and a
	// settle after it must not re-open or rewrite it (the frozen frame is the
	// thing the operator was promised).
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	frozen := readFile(t, path)
	if err := d.Settle(); err != nil {
		t.Fatalf("Settle after Close returned an error: %v", err)
	}
	if after := readFile(t, path); after != frozen {
		t.Fatalf("Settle after Close rewrote the frozen artifact:\nbefore:\n%s\nafter:\n%s", frozen, after)
	}
}
