package lineedit

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

// readwait_level_test.go — THE INVARIANT THE READ WAIT IS CONDITIONAL ON.
//
// The defect these cases exist for (measured on a real session, task 01a18990):
// a draw LEVEL was pending — a band frame, a band note, an answer op — and its
// notification was lost or already spent, so the read loop sat in
// `poll(2)` with `pollTimeoutBlock` (-1) and nothing could ever wake it. The
// only key that can reach a console in that state is the operator's Ctrl+C, and
// it waited for ever: `chat-exit.marker` recorded `reason:"interrupt"`,
// `exit_code:0`, `signal:""` — the 0x03 DID arrive, arbitrarily late.
//
// WHY A LEVEL CANNOT TRUST A WAKE. A notification is an EDGE. `Terminal.ReadEvent`
// DRAINS the notification pipe when it reports EventWake, and the band seam
// queues its frame BEFORE it notifies, so an edge that lands between the loop's
// decision to block and its poll(2) is spent on a decision it cannot inform. The
// fix does not repair that race — it removes the need for it: a pending level
// bounds the wait (drawLevelPollMS), so the loop re-derives the level on its own
// and draws it, and the operator's next keystroke is collected at the next
// bounded wake instead of waiting for a wake that will not come.
//
// THE FIXTURE IS THE HOSTILE CASE, and it is exact rather than racy:
//
//   - the source NEVER reports EventWake (`scriptedSource.Notify` is left
//     unwired — the notification is LOST by construction, not by scheduling);
//   - a level is pending (a queued band frame, a queued answer op);
//   - the 0x03 is at the END of the script, so it can only be read once the loop
//     comes back around on its own.
//
// Before the fix the loop blocks for ever at the read taken BEFORE the queued
// step (`holdAtEnd` keeps a bounded read from reporting EOF, and an unbounded
// one never returns at all), so the interrupt never arrives. After it, the wait
// is bounded by drawLevelPollMS and the loop reaches the 0x03 in milliseconds.

const (
	// levelReadWaitBound is how long the case allows the injected 0x03 to reach
	// Result.Interrupted. It is a SHAPE bound, not a measured duration: the fix
	// makes the wait at most drawLevelPollMS (50ms) plus scheduling, while the
	// defect is UNBOUNDED (the read never returns). Two orders of magnitude
	// separate the two verdicts, so a slow host cannot turn this red — the same
	// reasoning bandwait_owner_test.go states for its own bound.
	levelReadWaitBound = 2 * time.Second
	// levelBandRow is the queued band frame's text; the content is irrelevant to
	// the wait and is here only so a failure reads as a row rather than a bool.
	levelBandRow = "level-marker"
)

// levelCtrlC is the input step carrying Ctrl+C. ctrlCByte (interrupt_turn_test.go)
// is the raw byte a raw-mode terminal delivers (ISIG is cleared, so there is no
// SIGINT): the editor's decoder classifies it as KeyInterrupt, and editor.go's
// case KeyInterrupt is what answers Result.Interrupted.
var levelCtrlC = []byte{ctrlCByte}

// levelHarness builds an editor whose layout is installed, with `script` as the
// ONLY input it will ever see and NO notification channel at all. It returns the
// source too, because the loop's own CADENCE is what two of these cases assert.
func levelHarness(t *testing.T, script ...scriptStep) (*Editor, *scriptedSource) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{steps: script, width: noloopCols, height: noloopRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	errOut := &bytes.Buffer{}
	ed := NewChatEditor(src, out, promptForTest, nil, errOut, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%dx%d) = false, want true (the layout's floor is %d rows)",
			noloopCols, noloopRows, RowsMinUsable())
	}
	// NO WAKE SEAM AT ALL — the fixture's whole point. EnableRegion installs the
	// source's own notifier; it is replaced here by the no-op sink, which is the
	// state a run has when its source can supply none (BandNotifyMisses counts
	// it) and, more to the point, the state in which a level's notification is
	// LOST BY CONSTRUCTION rather than by scheduling. Nothing in this fixture can
	// wake the loop except the loop's own bounded wait.
	ed.SetNotification(nil)
	if ed.notify == nil || ed.notify.NotifyEnabled() {
		t.Fatalf("the harness has a live wake seam: a level's notification would not be lost, so the case is not "+
			"exercising the stall (notify=%T enabled=%v)", ed.notify, ed.notify != nil && ed.notify.NotifyEnabled())
	}
	t.Cleanup(func() {
		src.stopSource()
		ed.Close()
	})
	return ed, src
}

// liveWakeHarness is levelHarness' counterpart for the arm that must show the
// UNBOUNDED wait is still allowed: the source keeps the notifier EnableRegion
// installed (a working wake seam), so a level raised later is announced to the
// parked read and the loop may block for ever without starving anything. A
// fixture that removed the seam would make these two arms the same case.
func liveWakeHarness(t *testing.T) *Editor {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: noloopCols, height: noloopRows, clock: clock, holdAtEnd: true}
	ed := NewChatEditor(src, &recordingWriter{}, promptForTest, nil, &bytes.Buffer{}, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%dx%d) = false, want true", noloopCols, noloopRows)
	}
	if !ed.wakeReachesParkedRead() {
		t.Fatal("the harness has no live wake seam: the unbounded wait is only legal when a notification can " +
			"interrupt a parked read, and this arm exists to state exactly that")
	}
	t.Cleanup(func() {
		src.stopSource()
		ed.Close()
	})
	return ed
}

// TestReadWait_APendingBandFrameCannotBlockCtrlC is the gate's primary arm, and
// it reproduces the PRODUCTION ORDER of events rather than a convenient one.
//
// THE ORDER IS THE WHOLE CASE. The operator is sitting at a prompt, the read loop
// is PARKED in an unbounded wait, a level arrives (a band frame — the flow
// seam's, the status row's), and THEN the operator presses Ctrl+C. That is how
// the measured session went: the stall had been standing for minutes when the
// operator reached for the keyboard, and the `chat-exit.marker` it left records
// `reason:"interrupt"`, `exit_code:0`, `signal:""` — the 0x03 arrived, and only
// much later.
//
// So the ^C is delivered AFTER the park, and nothing wakes the loop except its
// own cadence. A fixture that queued the ^C up front would be testing a loop that
// had never parked, which is why this case waits for the loop's own first read
// before it raises the level and only then pushes the key.
func TestReadWait_APendingBandFrameCannotBlockCtrlC(t *testing.T) {
	ed, src := levelHarness(t) // no script yet: the loop parks on an empty source
	runDone := make(chan Result, 1)
	go func() {
		res, err := ed.Run()
		if err != nil {
			runDone <- Result{EOF: true, EOFKind: err.Error()}
			return
		}
		runDone <- res
	}()
	deadline := time.Now().Add(levelReadWaitBound)
	for src.timeoutCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond) //stylegate:allow-sleep synchronization aid: waits for the loop's first (parked) read, bounded by levelReadWaitBound
	}
	if src.timeoutCount() == 0 {
		t.Fatal("the read loop never took a read: the case cannot observe a parked wait")
	}
	// THE LEVEL ARRIVES WHILE THE LOOP IS PARKED — and its wake is lost by
	// construction (no seam), which is the state the fix must survive.
	if ed.queueBand(bandFrame{text: levelBandRow, oneRow: true}) {
		t.Fatal("queueBand() = true: this fixture has no wake seam, so it cannot have delivered a wake")
	}
	if !ed.bandPending() {
		t.Fatal("the band level is not raised: the fixture did not set up the state under test")
	}
	// THE DISCRIMINATING OBSERVABLE IS THE CADENCE, NOT THE KEY, and saying so is
	// the honest part of this case: a byte typed on a terminal makes poll(2)
	// report POLLIN, so the ^C below would be read even by the broken loop —
	// which is precisely why "Ctrl+C eventually works" was never evidence of
	// health. What the defect actually costs is that the LEVEL never gets drawn
	// and the loop never comes back on its own; the loop's own read cadence is
	// the witness for that, and it is what turns this case red without the fix.
	before := src.timeoutCount()
	deadline2 := time.Now().Add(levelReadWaitBound)
	for src.timeoutCount() <= before && time.Now().Before(deadline2) {
		time.Sleep(time.Millisecond) //stylegate:allow-sleep synchronization aid: waits for the loop's own bounded cadence, bounded by levelReadWaitBound
	}
	if src.timeoutCount() <= before {
		t.Fatalf("the parked loop never came back around after a level was raised against it: with no wake seam "+
			"nothing can interrupt its read, so the wait must be bounded (drawLevelPollMS=%d / noWakePollMS=%d) — "+
			"otherwise the queued row waits for a wake that cannot arrive", drawLevelPollMS, noWakePollMS)
	}
	start := time.Now()
	// NOW the operator presses Ctrl+C.
	src.push(scriptStep{data: levelCtrlC})

	select {
	case res := <-runDone:
		elapsed := time.Since(start)
		if !res.Interrupted {
			t.Fatalf("Run() = %+v after %s, want Result.Interrupted: the 0x03 was read but answered as an end of input", res, elapsed)
		}
		if elapsed > levelReadWaitBound {
			t.Errorf("the 0x03 took %s, want under %s", elapsed, levelReadWaitBound)
		}
	case <-time.After(levelReadWaitBound):
		t.Fatalf("the ^C pressed after the park did not reach Result.Interrupted within %s: the read wait is "+
			"UNBOUNDED while a level is queued and no wake can reach the loop — the stall that makes Ctrl+C look "+
			"like an infinite delay (drawLevelPollMS / noWakePollMS)", levelReadWaitBound)
	}
}

// TestReadWait_APendingAnswerOpCannotBlockCtrlC is the sibling arm: the answer
// queue is a DIFFERENT level with a different producer (the turn's goroutine),
// and the bound must not be spelled for the band alone. It asserts the read
// wait's DECISION directly — an answer op cannot be queued without a renderer
// whose region is active on a foreign goroutine, and the point here is the
// decision, not the delivery.
func TestReadWait_APendingAnswerOpCannotBlockCtrlC(t *testing.T) {
	ed, _ := levelHarness(t, scriptStep{data: levelCtrlC})
	// Raise the answer level exactly as a foreign producer does (queueAnswerOp
	// appends and sets `active` under the queue's own lock).
	ed.answers.mu.Lock()
	ed.answers.ops = append(ed.answers.ops, answerOp{kind: answerOpDelta, text: "level"})
	ed.answers.active = true
	ed.answers.mu.Unlock()
	if !ed.answerPending() {
		t.Fatal("the answer level is not raised: the fixture did not set up the state under test")
	}
	timeout, _, levelPolled := ed.readWait()
	if !levelPolled {
		t.Fatalf("readWait() = (%d, _, false) with an answer op pending, want a BOUNDED level poll: an unbounded "+
			"wait here is the stall (drawLevelPollMS)", timeout)
	}
	if timeout != drawLevelPollMS {
		t.Errorf("readWait() timeout = %d, want %d (drawLevelPollMS)", timeout, drawLevelPollMS)
	}
}

// TestReadWait_IsUnboundedWhenNothingIsOwed is the case's own CONTROL ARM, and
// it is what keeps the fix from being "bound every read": an idle loop with no
// level pending must still block for ever, or the bound is a poll-per-iteration
// spin (the repaint storm this editor's cadences are all shaped to avoid) and
// the invariant inverts — the loop would never sleep at all.
//
// Without this arm, "always bounded" is a defensible reading of the fix and it
// would pass every other case here.
func TestReadWait_IsUnboundedWhenNothingIsOwed(t *testing.T) {
	ed := liveWakeHarness(t) // a working wake seam, and no script: nothing will ever arrive
	timeout, polledCompletion, levelPolled := ed.readWait()
	if levelPolled {
		t.Error("readWait() reported a level poll with NO level pending: an idle loop must block, not spin")
	}
	if polledCompletion {
		t.Error("readWait() reported a completion poll with no completion outstanding")
	}
	if timeout != pollTimeoutBlock {
		t.Errorf("readWait() timeout = %d, want %d (pollTimeoutBlock): an idle read must be unbounded",
			timeout, pollTimeoutBlock)
	}
}

// TestDrawLevelPending_CoversEveryQueueTheLoopDrains pins the PREDICATE itself,
// level by level, because the defect class is exactly "this queue was left out
// of the predicate": a level that the EventWake arm draws but the wait decision
// does not see is a level whose lost wake parks the loop for ever. Each subtest
// raises ONE level and asserts the predicate answered true with the others
// lowered.
func TestDrawLevelPending_CoversEveryQueueTheLoopDrains(t *testing.T) {
	t.Run("band frame", func(t *testing.T) {
		ed, _ := levelHarness(t)
		if ed.drawLevelPending() {
			t.Fatal("drawLevelPending() = true with every queue empty")
		}
		ed.queueBand(bandFrame{text: levelBandRow, oneRow: true})
		if !ed.drawLevelPending() {
			t.Error("drawLevelPending() = false with a band frame queued: its drainer is the read loop")
		}
	})
	t.Run("band note", func(t *testing.T) {
		ed, _ := levelHarness(t)
		ed.answer.Note("level note", Style{})
		if !ed.drawLevelPending() {
			t.Error("drawLevelPending() = false with a band note queued: its drainer is the read loop")
		}
	})
	t.Run("answer op", func(t *testing.T) {
		ed, _ := levelHarness(t)
		ed.answers.mu.Lock()
		ed.answers.ops = append(ed.answers.ops, answerOp{kind: answerOpDelta, text: "level"})
		ed.answers.active = true
		ed.answers.mu.Unlock()
		if !ed.drawLevelPending() {
			t.Error("drawLevelPending() = false with an answer op queued: its drainer is the read loop")
		}
	})
	t.Run("line insert", func(t *testing.T) {
		ed, _ := levelHarness(t)
		ed.lines.mu.Lock()
		ed.lines.active = true
		ed.lines.mu.Unlock()
		if !ed.drawLevelPending() {
			t.Error("drawLevelPending() = false with a line insert queued: its drainer is the read loop")
		}
	})
	t.Run("draft", func(t *testing.T) {
		ed, _ := levelHarness(t)
		ed.drafts.mu.Lock()
		ed.drafts.active = true
		ed.drafts.mu.Unlock()
		if !ed.drawLevelPending() {
			t.Error("drawLevelPending() = false with a draft stated: its drainer is the read loop")
		}
	})
	t.Run("status", func(t *testing.T) {
		ed, _ := levelHarness(t)
		ed.answer.PublishStatus()
		if !ed.drawLevelPending() {
			t.Error("drawLevelPending() = false with a status publish undrawn: its drainer is the read loop")
		}
	})
}

// TestReadWait_IsConcurrencySafe asserts the predicate is called from the loop
// while foreign producers raise levels: it takes each queue's own lock, and a
// case that raised a level WHILE it ran must not have raced the read of it.
// It is a smoke arm for the -race suite rather than a behavioural assertion.
func TestReadWait_IsConcurrencySafe(t *testing.T) {
	ed, _ := levelHarness(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				ed.answer.Note("level note", Style{})
				_ = ed.queueBand(bandFrame{text: levelBandRow, oneRow: true})
			}
		}
	}()
	for i := 0; i < 50; i++ {
		_ = ed.drawLevelPending()
		_, _, _ = ed.readWait()
	}
	close(stop)
	wg.Wait()
}

// TestReadWait_ALevelRaisedAfterTheWaitCannotBeStarved is the SECOND half of the
// invariant, and it is the half the pending-level bound cannot cover.
//
// THE TWO HALVES ARE DIFFERENT DEFECTS:
//
//   - "a level was pending when the wait was decided" — the loop can SEE it, and
//     drawLevelPollMS makes it refuse to block (the cases above).
//   - "a level is raised AFTER the wait was decided, by a producer whose
//     notification cannot reach a parked read" — the loop cannot see a future
//     level, and when the source has no wake pipe there is no edge that could
//     tell it. This is the measured production shape: the band seam queues the
//     frame at T and notifies at T+ε, and a run whose transport cannot deliver
//     that notification leaves the level queued behind a read that will never
//     return.
//
// The observable is the loop's own CADENCE — the wait it took, recorded by the
// source — because "the read never returned" has no other witness that is not a
// wall-clock guess. With a live wake seam the wait is allowed to be unbounded
// (that is what the seam is FOR); without one it must not be.
func TestReadWait_ALevelRaisedAfterTheWaitCannotBeStarved(t *testing.T) {
	ed, src := levelHarness(t) // nothing to read: the loop parks
	runErr := make(chan error, 1)
	go func() {
		_, err := ed.Run()
		runErr <- err
	}()
	// Wait until the loop is PARKED — it has taken at least one read. The
	// fixture holds at end, so an unbounded read never returns and a bounded one
	// comes back as EventNone.
	deadline := time.Now().Add(levelReadWaitBound)
	for src.timeoutCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond) //stylegate:allow-sleep synchronization aid: waits for the loop to take its first read, bounded by levelReadWaitBound
	}
	if src.timeoutCount() == 0 {
		t.Fatal("the read loop never took a read: the case cannot observe a parked wait")
	}
	// THE LEVEL ARRIVES WHILE THE LOOP IS ALREADY PARKED — the production shape.
	ed.queueBand(bandFrame{text: levelBandRow, oneRow: true})

	// The loop must come back around ON ITS OWN: with no wake seam, a wait that
	// is still unbounded here is the stall, and the bound is the only thing that
	// can end it.
	before := src.timeoutCount()
	for src.timeoutCount() <= before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond) //stylegate:allow-sleep synchronization aid: waits for the loop's own bounded cadence, bounded by levelReadWaitBound
	}
	if src.timeoutCount() <= before {
		t.Fatalf("the parked loop never came back around after a level was raised against it: with no wake seam "+
			"nothing can interrupt its read, so the wait must be bounded (noWakePollMS=%d) — otherwise the queued "+
			"row and the operator's next keystroke wait for a wake that cannot arrive", noWakePollMS)
	}
	// AND THE CADENCE IT TOOK IS THE NAMED ONE, not an accident of a shorter
	// bound elsewhere: the last wait must be noWakePollMS (or shorter).
	src.mu.Lock()
	last := src.timeouts[len(src.timeouts)-1]
	src.mu.Unlock()
	if last < 0 {
		t.Errorf("the last read wait was unbounded (%d) with no wake seam and a level pending", last)
	}
	if last > noWakePollMS {
		t.Errorf("the last read wait was %dms, want at most noWakePollMS (%d)", last, noWakePollMS)
	}
	// The loop is released HERE (stopSource is idempotent, so the cleanup's own
	// call is a no-op): waiting for Run to return without asking the fixture to
	// end would be waiting for input that never comes.
	src.stopSource()
	select {
	case <-runErr:
	case <-time.After(levelReadWaitBound):
		t.Fatal("Run() did not return after the fixture was asked to stop")
	}
}
