// dump_tailflush_test.go — the throttle WINDOW's own closure: the artifact must
// install the frame a window ended on, without waiting for the render stream to
// produce another byte.
//
// WHY THIS IS SEPARATE FROM THE SETTLE CASE (dump_settle_test.go). Settle closes
// the window when a CALLER knows the screen is final — the model-turn path,
// where cmd/agent calls SettleDump() at the turn boundary. A REPL-native slash
// line (`/info`, `/help`, `/queue`) has no such caller: it is answered by the
// REPL's own switch, in-process, and the REPL then parks in ReadLine. Settle's
// own comment argues a timer would have to GUESS the producer's moment — true
// for a turn boundary, and still true; this case is not that moment. It closes
// the window the THROTTLE left open, which is this sink's private bookkeeping
// and which no producer can know about.
//
// THE MEASUREMENT (2026-10-02, task 01a0fd34 — a real pty, this sink's own
// trace). The paint that OPENS a window flushes with the line's answer block
// still in flight: the block's bytes arrive 1–35 ms later, every one of them
// `due=false` because the window had just opened. The REPL then parks, so no
// later write ever closes the window. Measured: `frames=89 dumps=7`, artifact
// byte-identical across 50 samples over 10 s, row 036/037 still the previous
// lines, while the pty stream carried all 14 `session info:` rows. ONE later
// keystroke closed the window and installed the block (`frames=140 dumps=8`) —
// which is what named the mechanism: not a lost write, a window that never closed.
//
// The clock is the sink's own (ScreenDumper.now), advanced by the test rather
// than slept through, so the case is deterministic on any machine.

package uiclient

import (
	"strings"
	"testing"
	"time"
)

// dumpTailFlushWait bounds the poll for the WINDOW's own closure. It is a
// test-side deadline (the timer runs on the real clock by design — a timer that
// could be driven by the injected clock would not be testing the timer), an
// order of magnitude above dumpMinInterval so a loaded machine does not fail
// the case. It is a bound, never a sleep: the loop exits the moment the file
// carries the row.
const dumpTailFlushWait = 20 * dumpMinInterval

// dumpTailFlushPoll is the poll interval of that loop.
const dumpTailFlushPoll = 2 * time.Millisecond

// TestScreenDumperInstallsTheFrameAWindowEndedOn pins the defect: a burst whose
// tail lands INSIDE the window it opened must reach the artifact with no further
// write, no keystroke and no Settle.
func TestScreenDumperInstallsTheFrameAWindowEndedOn(t *testing.T) {
	d, path := newTestDumper(t, DumpModeANSI, 24, 3)

	// The producer's measured state: a window that just OPENED. The clock is
	// parked PAST `d.last` (the helper re-anchors it to `base`), so the opening
	// paint below is DUE and installs; it then stays still, so every later frame
	// is `due=false` and the throttle can install nothing more on its own.
	base := time.Unix(1_700_000_000, 0)
	clock := base.Add(dumpMinInterval + time.Millisecond)
	d.now = func() time.Time { return clock }
	openBefore := d.Writes() //screen-ok: reads the install COUNT that the opening paint earns — no row is awaited here, the counter is the subject

	// 1) The paint that OPENS the window: it is due, so it flushes — carrying
	//    the screen as it stood BEFORE the line was answered.
	if _, err := d.Write([]byte("\x1b[1;1Hyou> /info")); err != nil {
		t.Fatalf("Write (opening paint): %v", err)
	}
	if got := d.Writes(); got <= openBefore { //screen-ok: measures that the opening paint DID install (the window is open) — the install count is the awaited fact, asserted synchronously
		t.Fatalf("the due paint did not open the window (dumps %d -> %d)", openBefore, got)
	}
	if body := readFile(t, path); strings.Contains(body, "the session identity") {
		t.Fatalf("the artifact already carries the block before it was painted — this case would not be measuring the window:\n%s", body)
	}

	// 2) The burst's TAIL: the answer block, arriving inside the just-opened
	//    window. Every one of these is `due=false` — the throttle holds them all,
	//    which is exactly what it should do.
	tail := "\x1b[3;1Hthe session identity\x1b[2;1Hsession f83d6a7f"
	if _, err := d.Write([]byte(tail)); err != nil {
		t.Fatalf("Write (burst tail): %v", err)
	}
	if got := d.Snapshot().Lines[2]; got != "the session identity" {
		t.Fatalf("the grid did not interpret the tail: row 3 = %q", got)
	}

	// 3) NO FURTHER WRITE AND NO SETTLE — the whole point. The window's own
	//    closure must install the frame the window ended on.
	deadline := time.Now().Add(dumpTailFlushWait)
	for {
		if strings.Contains(readFile(t, path), "the session identity") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the artifact never installed the frame the window ended on — the window was left open and no future write can close it (the REPL's parked state):\n%s", readFile(t, path))
		}
		time.Sleep(dumpTailFlushPoll) //stylegate:allow-sleep bounded poll (dumpTailFlushWait deadline) for the window's own one-shot timer to fire; the loop exits the moment the row lands
	}
}

// TestScreenDumperTailFlushDoesNotOutliveClose pins the freeze's contract: a
// CLOSED sink keeps the frame Close installed, so a window closure armed before
// the freeze can never re-open it (the artifact a reader quotes must be the
// frozen screen, never a later frame installed by a stray timer).
func TestScreenDumperTailFlushDoesNotOutliveClose(t *testing.T) {
	d, path := newTestDumper(t, DumpModeANSI, 24, 3)

	// A clock that is permanently PAST the interval: every write below is due,
	// so each one opens (and re-arms) the window.
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base.Add(dumpMinInterval + time.Millisecond) }

	if _, err := d.Write([]byte("\x1b[1;1Hyou> /info")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	d.mu.Lock()
	armed := d.tail != nil
	d.mu.Unlock()
	if !armed {
		t.Fatal("a due paint did not arm the window's closure — this case needs one armed at Close")
	}

	// Freeze the sink. Close installs the current frame and must take the armed
	// closure down with it: a timer that fired afterwards would rewrite the
	// artifact a reader is quoting.
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	frozen := readFile(t, path)
	d.mu.Lock()
	stillArmed := d.tail != nil
	d.mu.Unlock()
	if stillArmed {
		t.Fatal("Close left the window's closure armed: a stray timer can re-open a frozen sink")
	}

	// Outlast the interval the timer would have fired in, and prove the artifact
	// did not move.
	time.Sleep(2 * dumpMinInterval) //stylegate:allow-sleep proves a STOPPED timer stays stopped: the assertion is an absence, so the case must outlast the interval it would have fired in
	if after := readFile(t, path); after != frozen {
		t.Fatalf("the frozen artifact was rewritten after Close:\nbefore:\n%s\nafter:\n%s", frozen, after)
	}
}

// TestScreenDumperArmsOneWindowClosurePerWindow pins the bound that keeps this
// from becoming a writer that runs on its own: a window closure is armed by a
// real paint and is one-shot — the timer slot clears when it fires, so a longer
// burst re-arms ONE closure per window rather than one per write.
func TestScreenDumperArmsOneWindowClosurePerWindow(t *testing.T) {
	d, _ := newTestDumper(t, DumpModeANSI, 24, 3)

	base := time.Unix(1_700_000_000, 0)
	clock := base.Add(dumpMinInterval + time.Millisecond)
	d.now = func() time.Time { return clock }

	// Open a window, then paint a long burst inside it.
	if _, err := d.Write([]byte("\x1b[1;1Hrow one")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	armed := func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.tail != nil
	}
	if !armed() {
		t.Fatal("the opening paint did not arm the window's closure")
	}
	for i := 0; i < 64; i++ {
		if _, err := d.Write([]byte("\x1b[2;1Hmore burst text")); err != nil {
			t.Fatalf("Write (burst %d): %v", i, err)
		}
	}
	if !armed() {
		t.Fatal("the closure was disarmed mid-window: a held burst would freeze again")
	}

	// Let it fire, and prove the slot clears — a second window other than a real
	// paint must never leave a live timer behind.
	deadline := time.Now().Add(dumpTailFlushWait)
	for armed() {
		if time.Now().After(deadline) {
			t.Fatal("the window closure never fired: the slot is still armed")
		}
		time.Sleep(dumpTailFlushPoll) //stylegate:allow-sleep bounded poll (dumpTailFlushWait deadline) for the one-shot closure to fire and clear its slot
	}

	// A NEW paint past the interval opens a NEW window and arms exactly one more.
	// The clock must move a full interval past the closure's own flush (which
	// stamped `d.last` at the clock's value when it ran).
	clock = clock.Add(dumpMinInterval + time.Millisecond)
	if _, err := d.Write([]byte("\x1b[3;1Ha second window")); err != nil {
		t.Fatalf("Write (second window): %v", err)
	}
	if !armed() {
		t.Fatal("a due paint after a closed window did not arm a new closure")
	}
}

// TestScreenDumperHasNoTimerWhenThereIsNoFile pins the memory-only
// configuration (Path == ""): a sink with no artifact to keep current must not
// leave a timer behind — the snapshot is the deliverable, and there is nothing
// for a closure to write.
func TestScreenDumperHasNoTimerWhenThereIsNoFile(t *testing.T) {
	d, err := NewScreenDumper(DumpConfig{Mode: DumpModeANSI, Cols: 24, Rows: 3})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	if _, err := d.Write([]byte("\x1b[1;1Ha frame here")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	d.mu.Lock()
	armed := d.tail != nil
	d.mu.Unlock()
	if armed {
		t.Fatal("a path-less sink armed a window closure: there is no artifact to keep current")
	}
	if got := d.Snapshot().Lines[0]; got != "a frame here" {
		t.Fatalf("the path-less sink did not interpret the frame: row = %q", got)
	}
}
