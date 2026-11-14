package lineedit

// draft_repaint_visibility_test.go — THE READ LOOP OWES A DRAFT A FRAME.
//
// THE DEFECT (measured on hosta, `gopi chat /voice`): the streaming recogniser
// states a draft from its own goroutine, the line shows NOTHING, and the text
// appears only when the operator presses a key (or Escape). draft.go's segment is
// correct — the draft is applied by the OWNER of the model (repaint → drainDraft)
// and the layout lays it out after the buffer — so what was missing was the
// DECISION to draw: the four places the read loop asks "is there anything new to
// put on the screen?" enumerated every pending thing EXCEPT the draft, so a draft
// stated at a moment with no keystroke behind it waited for the NEXT repaint —
// which on an idle line is the next keystroke. draft_test.go cannot see this: it
// drives the seam dry (SetDraft + drainDraft), which is exactly the half that was
// already right.
//
// WHAT THESE TWO CASES ARE, and what they deliberately are NOT. They run the REAL
// read loop (Run on its own goroutine) over the package's scripted source, state
// ONE draft with NO keystroke injected anywhere, and read the RENDERED ROW back
// through a Screen — never a producer counter, as the assertion's subject:
//
//	VISIBILITY — the draft reaches the painted row on its own wake;
//	NO SPIN    — and the seam it was added to does not become a repaint per idle
//	             read: the level is cleared by the draw that consumed it
//	             (drainDraft, under the data's own lock), so an idle line stays
//	             quiet.
//
// The end-to-end proof over a real pty (the operator's own terminal) lives in
// cmd/agent's chat cases; here the seam is driven at the read loop's own
// granularity, so each property fails alone.

import (
	"strings"
	"testing"
	"time"
)

const (
	// draftSeamRows / draftSeamCols is the geometry the cases open at: enough
	// rows to host the reserved layout (RowsMinUsable), and a row wide enough to
	// read the editing line back whole.
	draftSeamRows = 12
	draftSeamCols = 48
	// draftSeamText is the draft the producer states: one partial of a streaming
	// recogniser's utterance (the same shape draft_test.go's seam cases use).
	draftSeamText = "温度是多少"
	// draftSeamIdleReads is how many idle reads the quiet window covers: three
	// consecutive reads that carry no news, which is where a repaint-per-poll
	// would show itself.
	draftSeamIdleReads = 3
	// draftSeamQuiet is how long the quiet window stays open. It covers the
	// cadence at which a bounded wait is re-issued (escWaitMS is the shortest
	// one), so a timer-driven or per-poll repaint has somewhere to land.
	draftSeamQuiet = 40 * time.Millisecond
	// draftSeamWait bounds a wait for the read loop's own draw. Generous: it is a
	// synchronization bound, and a passing run never comes near it.
	draftSeamWait = 3 * time.Second
	// draftSeamStep is the poll interval of such a wait.
	draftSeamStep = time.Millisecond
	// statusLabelForDraftCase / statusValueForDraftCase are the status row's own
	// values in the no-news case: any fixed pair will do, because what the case
	// needs is a row whose value does NOT change between the two publishes.
	statusLabelForDraftCase = "voice"
	statusValueForDraftCase = "listening"
)

// TestDraftReachesTheScreenWithoutAKeystroke is the defect's direct regression
// case: ONE SetDraft, NO key injected, and the draft must be on the editing row.
//
// IT IS RED ON THE PRE-FIX READ LOOP. There, a wake finds nothing it recognises
// as drawable (the draft is pending and no other flag is raised), so the loop
// parks again and the row never carries the text — the operator's "the draft does
// not appear until I press Esc".
func TestDraftReachesTheScreenWithoutAKeystroke(t *testing.T) {
	h := newDraftSeamHarness(t)
	waitFirstFrame(t, h)

	if !h.ed.SetDraft(draftSeamText, false) {
		t.Fatal("the seam refused the draft — the case would assert nothing")
	}
	// Nothing else is injected: the producer's own wake is the only thing that
	// can carry this text to the screen.
	deadline := time.Now().Add(draftSeamWait)
	for {
		if strings.Contains(h.editingRow(t), draftSeamText) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the editing row never carried the draft %q within %s — a draft stated with no keystroke behind it never reached a repaint\n--- screen ---\n%s",
				draftSeamText, draftSeamWait, h.contents(t))
		}
		time.Sleep(draftSeamStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's draw (see draftSeamWait)
	}
	// The draw CONSUMED the level: the repaint that showed the draft is the same
	// pass that applied it (repaint → drainDraft), so nothing is left waiting.
	if h.ed.draftPending() {
		t.Fatal("the draft was drawn and is still reported pending — the level was not lowered by the pass that drew it, which is what turns a wake into a repaint per read")
	}
}

// TestDraftDoesNotSpinTheReadLoop is the OTHER HALF of the fix, and it is the
// reason the predicate was added to the seam rather than replaced by an
// unconditional draw: an idle line must not repaint per read.
//
// The property is asserted on the settled shape: the draft has been DRAWN (so the
// level has been consumed), three idle reads then arrive — the read outcome a
// bounded wait produces, i.e. exactly the branch the source publishes into — and
// the redraw count must not move. A fix that drew unconditionally at the idle
// seam fails here; so does a level that is never lowered, because then every
// subsequent read would find "news" in a draft already on the screen.
func TestDraftDoesNotSpinTheReadLoop(t *testing.T) {
	h := newDraftSeamHarness(t)
	waitFirstFrame(t, h)

	if !h.ed.SetDraft(draftSeamText, false) {
		t.Fatal("the seam refused the draft — the case would assert nothing")
	}
	// Wait for the DRAIN, not for the row: the level is the thing the seam asks,
	// so this is the state the quiet window must be measured from.
	deadline := time.Now().Add(draftSeamWait)
	for h.ed.draftPending() {
		if time.Now().After(deadline) {
			t.Fatalf("the pending draft was never applied by the read loop within %s — every read below would find it outstanding and the quiet window would measure the fix's absence, not a spin\n--- screen ---\n%s",
				draftSeamWait, h.contents(t))
		}
		time.Sleep(draftSeamStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's own drain (see draftSeamWait)
	}
	if got := h.ed.Draft(); got != draftSeamText {
		t.Fatalf("the segment carries %q after the drain, want the stated %q", got, draftSeamText)
	}

	// Three idle reads: no keystroke, no publish — the loop comes back around
	// with nothing new to draw, which is the only shape that can expose a
	// repaint-per-poll.
	before := h.src.timeoutCount()
	for i := 0; i < draftSeamIdleReads; i++ {
		h.src.push(scriptStep{timeout: true})
	}
	waitIdleReads(t, h, before+draftSeamIdleReads)

	// The window opens AFTER the idle reads have been consumed, so it measures
	// what an idle line does next rather than the reads' own cost.
	beforeRepaints := h.ed.Repaints()                  //screen-ok: MEASURES the read loop's redraw count across a quiet window (a spin bound on DECISIONS), never a wait for a screen another producer is drawing
	time.Sleep(draftSeamQuiet)                         //stylegate:allow-sleep the quiet window itself: a repaint-per-poll or a timer-driven redraw must have somewhere to land before the count is read again
	if got := h.ed.Repaints(); got != beforeRepaints { //screen-ok: the same MEASUREMENT, read after the window closed — the claim is that a draft already drawn costs the idle line nothing
		t.Fatalf("the idle line repainted %d time(s) across a %s quiet window (want 0) — the draft predicate turned the read cadence into a repaint per read\n--- screen ---\n%s",
			got-beforeRepaints, draftSeamQuiet, h.contents(t))
	}
	// AND THE DRAFT IS STILL THERE: "no spin" must not be achieved by dropping the
	// segment on the next read.
	if got := h.editingRow(t); !strings.Contains(got, draftSeamText) {
		t.Fatalf("the editing row reads %q after the quiet window, want it to still carry %q", got, draftSeamText)
	}
}

// TestDraftIsDrawnWhenTheStatusRowHasNoNews is the SAME property as the case
// above, with the ONE other flag that can stand in for it removed — and it is the
// shape the operator's `/voice` run had.
//
// WHY IT IS A SEPARATE CASE (measured while writing this file). The mode publishes
// its status when it ENTERS (voiceChatMode.markLive → publishStatus → a status flag
// plus a wake), so on the shipped path a draft stated just after the mode opened
// finds `statusPending()` already raised: the read loop draws the frame for the
// STATUS and the draft rides along in it. With the seam's own wake carrying no
// other news there is nothing to ride along in, and the draft waits — measured, not
// reasoned about: the end-to-end pty case passes on the PRE-FIX read loop, because
// the mode's entry publish supplies the flag the draft was missing. A draft stated
// at any other moment (a second utterance, a mode already painted, the publish
// already consumed) has no such flag, and that is the defect.
//
// A published value that does not change is NOT drawn again (the same discipline
// that bounds the publish path), so the flag below is consumed by the frame that
// honoured it and nothing is left outstanding — asserted, not assumed: the case
// waits for the status flag to be CONSUMED (the loop's own draw) and only then
// states the draft. Without that ordering the mode's own publish would still be
// outstanding and this case would pass on the pre-fix read loop, which is the
// false green the pty case carries.
func TestDraftIsDrawnWhenTheStatusRowHasNoNews(t *testing.T) {
	h := newDraftSeamHarness(t)
	waitFirstFrame(t, h)
	h.ed.SetStatus(func() Status { return Status{Label: statusLabelForDraftCase, Live: statusValueForDraftCase} })
	// THE MODE'S ENTRY PUBLISH: a flag plus a wake, with no keystroke behind it.
	h.ed.PublishStatus()

	// The loop draws it (that is the publish path working) and the draw CLEARS the
	// flag — the state the draft has to be drawn from on its own.
	deadline := time.Now().Add(draftSeamWait)
	for h.ed.statusPending() {
		if time.Now().After(deadline) {
			t.Fatalf("the published status was not drawn by the read loop within %s — the case cannot reach the no-news state it is about\n--- screen ---\n%s",
				draftSeamWait, h.contents(t))
		}
		time.Sleep(draftSeamStep) //stylegate:allow-sleep bounded synchronization wait on the read loop consuming the status publish (see draftSeamWait)
	}

	if !h.ed.SetDraft(draftSeamText, false) {
		t.Fatal("the seam refused the draft — the case would assert nothing")
	}
	drawBy := time.Now().Add(draftSeamWait)
	for {
		if strings.Contains(h.editingRow(t), draftSeamText) {
			break
		}
		if time.Now().After(drawBy) {
			t.Fatalf("the editing row never carried the draft %q within %s with the status row unchanged — this is the operator's defect, with the mode's entry publish taken out of the picture\n--- screen ---\n%s",
				draftSeamText, draftSeamWait, h.contents(t))
		}
		time.Sleep(draftSeamStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's draw (see draftSeamWait)
	}
}

// waitFirstFrame blocks until the read loop is past its start-up repaint and is
// parked in a read, with at least one later read DELIVERED.
//
// WHY THIS IS NOT OPTIONAL (measured while writing this case). Run() repaints
// BEFORE its first read (the line the operator sees on a fresh prompt), so a
// draft stated a hair early is carried by that frame and the case passes on
// PRE-FIX code — a false green that asserts nothing about the seam under test.
// The read-loop-relative boundary is what makes it deterministic on both
// revisions: the start-up repaint and one further read are both BEHIND the loop
// by the time this returns, so the only reads that follow are the ones the case
// issues.
func waitFirstFrame(t *testing.T, h *draftSeamHarness) {
	t.Helper()
	deadline := time.Now().Add(draftSeamWait)
	for {
		// The start-up frame has no keystroke behind it, so a read CANNOT have
		// been issued before the FIRST repaint — a repaint count above zero is
		// the loop being past that frame. `Repaints()` is a count read as the
		// WINDOW'S OWN STARTING POINT, never a wait for somebody else's draw.
		repaints := h.ed.Repaints() //screen-ok: the read-loop-relative starting point of a deterministic window (a count read twice and compared to itself), never a wait on a screen
		reads := h.src.timeoutCount()
		if repaints != 0 && reads >= 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the read loop did not reach its first read within %s (repaints=%d reads=%d) — the case cannot be ordered against the loop's own frames, and a draft stated now would assert nothing",
				draftSeamWait, repaints, reads)
		}
		time.Sleep(draftSeamStep) //stylegate:allow-sleep bounded synchronization wait on the read loop reaching its parked read (see draftSeamWait)
	}
}

// waitIdleReads blocks until the read loop has issued at least want reads. It is
// a synchronization bound on the loop reaching the idle seam, never a delay the
// assertion depends on.
func waitIdleReads(t *testing.T, h *draftSeamHarness, want int) {
	t.Helper()
	deadline := time.Now().Add(draftSeamWait)
	for h.src.timeoutCount() < want {
		if time.Now().After(deadline) {
			t.Fatalf("the read loop issued %d read(s), want at least %d — the idle reads were never picked up, so the quiet window below would assert nothing",
				h.src.timeoutCount(), want)
		}
		time.Sleep(draftSeamStep) //stylegate:allow-sleep bounded synchronization wait on the read loop consuming the pushed idle steps (see draftSeamWait)
	}
}

// draftSeamHarness is an editor whose READ LOOP IS RUNNING — the shape the defect
// lives in: the producer is never the reader, and the reader is parked in a read
// when the producer states its draft.
type draftSeamHarness struct {
	ed   *Editor
	out  *recordingWriter
	src  *scriptedSource
	done chan struct{}
}

// newDraftSeamHarness starts an editor reading, over the reserved layout, with the
// source's wake seam installed (without one the draft could not reach a parked
// read at all, and every wait below would time out for the wrong reason).
func newDraftSeamHarness(t *testing.T) *draftSeamHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(editorTestEpoch, 0)}
	src := &scriptedSource{width: draftSeamCols, height: draftSeamRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", draftSeamRows)
	}
	if !ed.NotifyEnabled() {
		t.Fatal("the harness's source supplied no wake seam — a draft could never reach the read loop, and every wait below would time out for the wrong reason")
	}
	h := &draftSeamHarness{ed: ed, out: out, src: src, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = ed.Run() //stylegate:ignore the harness's teardown: a real failure surfaces as a bounded wait below, whose dump names the row that never carried the draft
	}()
	t.Cleanup(func() {
		// Stop the SOURCE and wait for the read loop to return BEFORE tearing the
		// editor down: DisableRegion (inside Close) mutates the region the loop
		// reads, so closing first would race the harness itself.
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(draftSeamWait):
		}
		ed.Close()
	})
	return h
}

// replay renders the write log onto a FRESH screen: the read loop writes while the
// test reads, so an incremental feed would apply the already-seen chunks again.
func (h *draftSeamHarness) replay() *Screen {
	screen := NewScreen(draftSeamCols, draftSeamRows)
	for _, w := range h.out.snapshot() {
		screen.Feed(w)
	}
	return screen
}

// editingRow is the row the LAYOUT gives the editing line — read from the layout
// and never assumed, so the assertion cannot read another row's text.
func (h *draftSeamHarness) editingRow(t *testing.T) string {
	t.Helper()
	layout, ok := h.ed.Layout()
	if !ok {
		t.Fatal("the reserved layout is not installed — the editing row cannot be located")
	}
	return h.replay().Line(layout.EditorTop)
}

// contents is the failure dump.
func (h *draftSeamHarness) contents(t *testing.T) string {
	t.Helper()
	return h.replay().Contents()
}
