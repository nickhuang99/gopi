package lineedit

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// valueowner_foreign_test.go — the guard for THE EDITING MODEL'S OWNER.
//
// What this guards (measured 2026-09-28, task 01a0e67f, on master 9aba625b): the
// read loop's Editor.Run does Value.Reset() at the start of each line, while a
// reader that is NOT the loop reads Value.Text() — in the epic's own measurement,
// cmd/agent's pty harness polling "has the run's editor taken the typed line
// yet?". `go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_'` reported it as
//
//	Write  by lineedit.(*Value).Reset (value.go) ← lineedit.(*Editor).Run ← the read loop
//	Previous read by lineedit.(*Value).Text (value.go) ← cmd/agent.keyE2EEditorText
//
// The invariant this file pins (valueowner.go): `Value` has ONE writer, and a
// reader that is not that goroutine reads the PUBLISHED SNAPSHOT. The guard states
// that in the invariant's own terms — WHICH ROAD a read took, and whether the copy
// it took is frozen — so it needs no `-race`, no interleaving and no sampling:
//
//  1. TestValueOwner_ForeignReaderReadsTheSnapshot — with a read loop owning the
//     buffer, a read from the test's own goroutine must be served by the SNAPSHOT
//     road (the road is the claim, and it is what the read touches), the CONTENTS
//     must be the model's, and an owner edit made afterwards must leave the copy
//     that read saw untouched — and the model's own publication must MOVE, so a
//     snapshot nothing updates cannot pass this.
//  2. TestValueOwner_TheOwnerReadsTheLiveBuffer — the other road, pinned so the
//     guard cannot be satisfied by routing EVERYBODY to the snapshot: the owner's
//     own reads stay live and copy-free.
//  3. TestValueOwner_GuardIsDeterministic — the sequence repeated 5×: the barrier
//     is a gate and the observable is a ROAD, never a sampled window.
//
// RED-PROOF (the one-line revert): make a foreign read take the owner's road —
// publish's identity test disabled, `if false && goid() != v.ownerGoroutine()` —
// and tests 1 and 3 fail: the foreign goroutine is handed the LIVE buffer, which
// is exactly the read `go test -race` reported (Value.Text on the harness's
// goroutine against Value.Reset on the read loop). Measured out-of-tree on the
// branch base: 3/3 red with the line reverted, green with it restored (the task
// report's red-then-green table).

// valueGuardText is what the owner types; valueGuardLate is typed by the owner
// AFTER a foreign read, to prove the earlier read was frozen.
const (
	valueGuardText = "owner-guard-line"
	valueGuardLate = "ZZ"
	// valueGuardOtherLoop stands in for the read loop's goroutine id in the
	// mechanism-level test: any value that is not the calling goroutine's.
	valueGuardOtherLoop = int64(0x5eed)
)

// valueGuardSteps / valueGuardStep bound a wait on a LEVEL or on a scripted step
// — a synchronization wait, never a measured duration.
const (
	valueGuardSteps = 600
	valueGuardStep  = 5 * time.Millisecond
)

// valueGuardHarness is a chat editor whose read loop PARKS on a gated read, so the
// buffer's owner is the loop and nothing on the loop side can fire the sentinel
// while the guard observes.
type valueGuardHarness struct {
	ed   *Editor
	src  *guardedOrderingSource
	done chan struct{}
	// gateOpen records that the source's one-shot gate has been opened. The gate
	// is a barrier for the FIRST read only (guardedOrderingSource's own shape): a
	// second close would panic, and once it is open the loop reads freely.
	gateOpen bool
}

// newValueGuardHarness starts the editor and waits until its loop is parked.
func newValueGuardHarness(t *testing.T) *valueGuardHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	base := &scriptedSource{width: bandGuardCols, height: bandGuardRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	src := &guardedOrderingSource{
		scriptedSource: base,
		gate:           make(chan struct{}),
		parked:         make(chan struct{}),
		raised:         make(chan struct{}, 1),
		writes:         out.count,
	}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", bandGuardRows)
	}
	h := &valueGuardHarness{ed: ed, src: src, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = ed.Run() //stylegate:ignore the harness's teardown: a real failure surfaces as a bounded wait the guard reports
	}()
	t.Cleanup(func() {
		src.allowEOF()
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(valueGuardStep * valueGuardSteps):
		}
		ed.Close()
	})
	select {
	case <-src.parked:
	case <-time.After(valueGuardStep * valueGuardSteps):
		t.Fatal("the read loop never reached its park — the guard's barrier is not in place")
	}
	return h
}

// waitText waits until a read of the model (on THIS goroutine — the foreign road)
// carries text. The wait is on the assertion's own subject.
func (h *valueGuardHarness) waitText(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(valueGuardStep * valueGuardSteps)
	for time.Now().Before(deadline) {
		if strings.Contains(h.ed.Value().Text(), text) {
			return
		}
		time.Sleep(valueGuardStep) //stylegate:allow-sleep bounded poll (valueGuardSteps) for the read loop to take a scripted line into the model
	}
	t.Fatalf("the model never carried %q — the guard's premise (the read loop owns a buffer with text in it) does not hold", text)
}

// typeOnTheOwner hands the loop a scripted line and returns once the model carries
// it: the owner has then edited the buffer and its own publication is live.
//
// The gate is a ONE-SHOT barrier (the source's own shape): it is opened the first
// time and left open, so the loop reads the script freely from then on. The
// barrier's job — "the guard's observations do not race the boot paint" — is done
// by the time this is called.
func (h *valueGuardHarness) typeOnTheOwner(t *testing.T, line string) {
	t.Helper()
	h.src.push(scriptStep{data: []byte(line)})
	if !h.gateOpen {
		close(h.src.gate)
		h.gateOpen = true
	}
	h.waitText(t, line)
}

// TestValueOwner_ForeignReaderReadsTheSnapshot is the guard the epic asks for: a
// read of the editing model from a goroutine that is not the owner must be served
// by the published snapshot, not by the live buffer the owner edits.
func TestValueOwner_ForeignReaderReadsTheSnapshot(t *testing.T) {
	h := newValueGuardHarness(t)
	h.typeOnTheOwner(t, valueGuardText)

	// ── THE FOREIGN READ ──────────────────────────────────────────────────
	// This goroutine is NOT the read loop: it is the goroutine the test runs on —
	// the same position cmd/agent's pty harness polls from.
	v := h.ed.Value()
	road, _ := v.read()
	if road == valueRoadOwner {
		t.Fatalf("a goroutine that is not the read loop reported OWNERSHIP of the editing model (road=%s): ownership is decided by goroutine identity, and a caller cannot be the loop it is not", road)
	}
	if road != valueRoadSnapshot {
		t.Fatalf("the foreign read took road %q, want %q — a read outside the owner's goroutine must be served by the published copy, never by the live buffer (measured 2026-09-28: `go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_'` reports Value.Reset on the read loop against Value.Text from cmd/agent's pty harness)", road, valueRoadSnapshot)
	}
	got := v.Text()
	if !strings.Contains(got, valueGuardText) {
		t.Fatalf("the foreign read returned %q, want the model's contents (containing %q) — a snapshot that lags the owner's last mutation is a reader reading a stale line", got, valueGuardText)
	}
	// The COPY this reader holds, taken the way a foreign reader takes one.
	frozen := v.snapshot()
	if !strings.Contains(string(frozen.runes), valueGuardText) {
		t.Fatalf("the foreign reader's snapshot is not the published copy (runes=%q): the road it reported and the storage it holds must be the same decision", string(frozen.runes))
	}

	// ── FROZEN MEANS FROZEN ───────────────────────────────────────────────
	// The OWNER edits the model (a scripted key through the read loop's own
	// dispatcher — the loop's goroutine, the only goroutine allowed to write).
	h.typeOnTheOwner(t, valueGuardLate)

	if after := string(v.snapshot().runes); after == string(frozen.runes) {
		t.Fatalf("the model's published contents did not move after the owner edited them: the foreign read below would be reading a snapshot nothing updates (frozen=%q, published now=%q)", string(frozen.runes), after)
	}
	if !strings.Contains(v.Text(), valueGuardLate) {
		t.Fatalf("a foreign read after the owner's edit does not carry %q — the publication is not tracking the model", valueGuardLate)
	}
	if !strings.HasPrefix(v.Text(), string(frozen.runes)) {
		t.Fatalf("the earlier foreign read saw %q, which is not the PREFIX of the model now (%q) — the copy a reader took must be that reader's own frozen snapshot of the buffer as it then stood, never a window onto storage the owner keeps writing", string(frozen.runes), v.Text())
	}
}

// TestValueOwner_TheOwnerReadsTheLiveBuffer pins the OTHER road, so the guard
// cannot be satisfied by routing every reader — including the owner — to the
// snapshot: the read loop's own reads must stay live (no copy per read, and the
// value the loop just edited).
func TestValueOwner_TheOwnerReadsTheLiveBuffer(t *testing.T) {
	h := newValueGuardHarness(t)

	// The hook is installed BEFORE the gate is opened (the precedent in
	// bandwriter_foreign_test.go): ReadEvent watches this field on the loop's own
	// goroutine, so writing it after a released read would be a race in the test.
	// sync.Once keeps it to one report — and it is never cleared for the same
	// reason.
	res := make(chan string, 1)
	var once sync.Once
	h.src.setAfterRead(func() {
		once.Do(func() {
			road, _ := h.ed.Value().read()
			res <- road
		})
	})
	h.typeOnTheOwner(t, valueGuardText)
	select {
	case road := <-res:
		if road != valueRoadOwner {
			t.Fatalf("a read on the READ LOOP's own goroutine reported road %q, want %q — the loop is the only writer and its repaint reads the model it just edited, not a copy it would pay for on every frame", road, valueRoadOwner)
		}
	case <-time.After(valueGuardStep * valueGuardSteps):
		t.Fatal("the read loop never ran the guard's read callback — the harness's premise does not hold")
	}
}

// TestValueOwner_TheBufferOwnerIsSticky pins the regression this task's own
// acceptance loop caught. `ReadLine` calls `Run` ONCE PER LINE, so `Run` returns
// between lines — and so any bookkeeping scoped to a single `Run` leaves the
// buffer ownerless exactly while a caller is waiting for the next line. The
// buffer's owner is a LIFETIME fact: once a read loop has taken it, a read from
// any other goroutine — including the one that BUILT the editor, which is where
// cmd/agent's harness polls from — is foreign.
//
// Measured 2026-09-28: with the identity cleared on Run's return, 3 of the first 4
// acceptance runs reported `Value.Reset` on the read loop against
// `Value.Text`/`publish` on the builder's goroutine. This test is that defect's
// deterministic repro, and it is mechanism-level on purpose — the between-lines
// window itself is not reachable from the parked-read harness above, whose loop
// stays inside one `Run`.
func TestValueOwner_TheBufferOwnerIsSticky(t *testing.T) {
	v := NewValue(0, 0)

	// No read loop has taken it: there is NO goroutine whose reads may touch the
	// live fields, so this one — even though it built the value — is served the
	// published copy.
	if road, _ := v.read(); road != valueRoadSnapshot {
		t.Fatalf("a buffer no read loop has taken reports road %q, want %q: with no owner recorded, nothing may read the live fields", road, valueRoadSnapshot)
	}

	// A read loop takes it over (Editor.Run -> noteAudience -> noteValueLoop). A
	// caller cannot be the loop it is not.
	v.noteValueLoop(valueGuardOtherLoop)
	if road, _ := v.read(); road != valueRoadSnapshot {
		t.Fatalf("a read on a goroutine that is NOT the recorded loop took road %q, want %q", road, valueRoadSnapshot)
	}

	// AND THE OWNERSHIP IS STICKY. A zero id is the shape the editor uses to
	// release a PER-RUN fact (e.loopGoid), and Run really does return between
	// lines: passing it here must not hand the buffer back to a goroutine that is
	// not the loop.
	v.noteValueLoop(0)
	if road, _ := v.read(); road != valueRoadSnapshot {
		t.Fatalf("releasing the per-Run identity handed the buffer back to a foreign goroutine (road=%q): ReadLine calls Run once per line, so the read loop that owns this buffer is a LIFETIME fact and the window between two lines is exactly when a caller polls", road)
	}
}

// TestValueOwner_GuardIsDeterministic repeats the guard's sequence: the barrier is
// a gate and the observable is a COUNT, so no run depends on host load or on
// sampling a window. A guard that flaked would be worse than none (it would be
// turned off).
func TestValueOwner_GuardIsDeterministic(t *testing.T) {
	const reps = 5
	for i := 0; i < reps; i++ {
		h := newValueGuardHarness(t)
		h.typeOnTheOwner(t, valueGuardText)
		v := h.ed.Value()
		road, _ := v.read()
		got := v.Text()
		if road != valueRoadSnapshot {
			t.Fatalf("rep %d: a read from the harness's goroutine took road %q, want %q — it is not the read loop", i, road, valueRoadSnapshot)
		}
		if !strings.Contains(got, valueGuardText) {
			t.Fatalf("rep %d: the foreign read returned %q, want the model's line", i, got)
		}
	}
}
