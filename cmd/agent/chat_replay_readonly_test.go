package main

// chat_replay_readonly_test.go — the two defects the operator reported on
// 2026-10-05, asserted on the REAL path: a real pty, the real chat input layer
// (newChatInputWithSource, the constructor the REPL itself calls), the real
// editor, and the REAL control layer at its key seam.
//
// WHY THIS IS NOT A UNIT CASE (Rule 31). The claim is "while a replay runs, a key
// the WATCHER types cannot change the line the recording is being typed into" —
// and every part of that sentence is a property of the assembled surface: the pty
// is what makes a key TYPED (ReadOriginTyped comes from the terminal arm), the
// editor is what would apply it, and the control layer is what must stop it.
// chat_replay_control_test.go decides the layer's own decisions (which keys it
// claims, what state each one sets); this file decides that the decisions reach
// the buffer a watcher is looking at.
//
// THE TWO REPRODUCTIONS ARE MINIMAL ON PURPOSE: one typed backspace for defect ②
// (the smallest key that changes text), and one arrow for defect ① (the key the
// operator said did nothing).

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// controlBannedCoordinates are the internal words an OSD line on this surface may
// NOT carry (2026-10-11, the operator's rule for this control column: "要么告诉用户
// 能干什么，要么什么都不显示"). "beat" and "cursor" name the replay's own machinery —
// positions in a recording — and the watcher has neither a model of them nor any
// action that addresses them; "was" is how the removed position report spelled the
// comparison. They are asserted ABSENT on the real path, beside the real keys,
// because a claim about what the screen says is a claim about this surface.
var controlBannedCoordinates = []string{"beat", "cursor", "was", " %d of %d"}

const (
	// replayLineCols / replayLineRows are the terminal this case gives the layout
	// — a size the reserved chat layout installs at.
	replayLineCols = 80
	replayLineRows = 24
	// replayTestWait bounds a wait for the read loop to apply (or refuse) a key.
	// It is a deadline that turns a hang into a dump, never the assertion.
	replayTestWait = 3 * time.Second
	// replayTestStep is that wait's polling interval.
	replayTestStep = 2 * time.Millisecond
	// replayTestSeed is what the DRIVER types into the line before the watcher's
	// key arrives: it is the performance surface, and the assertion is that it
	// survives the watcher's keyboard untouched.
	replayTestSeed = "the recording typed this"
	// replaySettle is how long a case waits before deciding that a key which should
	// do NOTHING did nothing: the read loop has no signal to give for an unbound
	// key, so the wait is a settle bounded well inside replayTestWait.
	replaySettle = 250 * time.Millisecond
)

// replayLine is the surface under test: the real input layer on a real pty, the
// editorial gate/ hold installed exactly as chatReplay.run installs them, and the
// read loop running.
type replayLine struct {
	in     *chatInput
	ctrl   *chatReplayControl
	osd    *controlOSD
	pair   chatPtyPair
	exited chan struct{}
}

// newReplayLine builds the replay surface a live run has, minus the driver: the
// production input constructor over a pty, the layout armed, the control layer
// installed, and the read loop going.
func newReplayLine(t *testing.T, turns int) *replayLine {
	t.Helper()
	pair := chatOpenPtyPair(t, replayLineCols, replayLineRows)
	sink := turnDumpSink(t)
	term, err := lineedit.Open(pair.Slave, sink)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	in := newChatInputWithSource(term, sink, chatPrompt)
	if in.injector == nil {
		t.Fatalf("the production input constructor built no injection seam: the recording could not be typed")
	}
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: this case needs the real chat layout", replayLineCols, replayLineRows)
	}
	osd := &controlOSD{}
	ctrl := newChatReplayControl(osd.note)
	ctrl.install(in.editor)
	h := &replayLine{in: in, ctrl: ctrl, osd: osd, pair: pair, exited: make(chan struct{})}
	go func() {
		defer close(h.exited)
		_, _ = in.editor.Run() //stylegate:ignore ends the read-only replay editor's loop; the dump was already read and is the assertion
	}()
	t.Cleanup(func() {
		// The teardown ORDER the sibling pty cases establish: the terminal is
		// what unparks the read loop, so it goes first and the join is a join.
		in.editor.Close() //stylegate:ignore the harness's own terminal teardown
		_ = term.Close()  //stylegate:ignore the harness's own terminal teardown
		chatJoinReadLoop(t, h.exited, func() string { return runState(h.exited) })
		_ = pair.Master.Close() //stylegate:ignore pty master teardown of the read-only replay fixture, after the dump comparison
	})
	return h
}

// line reads the editing buffer — the artifact the recording is being typed into.
func (h *replayLine) line() string { return h.in.editor.Value().Text() }

// typeBytes delivers bytes the way a TERMINAL does: through the pty master. This
// is what makes the key TYPED rather than injected, and it is the only honest way
// to reproduce a watcher's keyboard.
func (h *replayLine) typeBytes(t *testing.T, s string) {
	t.Helper()
	if _, err := h.pair.Master.WriteString(s); err != nil {
		t.Fatalf("the watcher's keystroke could not be delivered to the pty: %v", err)
	}
}

// inject asks the DRIVER's own seam to type text — the recording's arm.
func (h *replayLine) inject(t *testing.T, s string) {
	t.Helper()
	res := h.in.injector.InjectBytes(s)
	if !res.Accepted {
		t.Fatalf("injecting %q (the recording's own typing) was refused: %s", s, res.Why)
	}
}

// waitForLine polls until the buffer holds want.
func (h *replayLine) waitForLine(t *testing.T, want, why string) {
	t.Helper()
	deadline := time.Now().Add(replayTestWait)
	for time.Now().Before(deadline) {
		if h.line() == want {
			return
		}
		time.Sleep(replayTestStep) //stylegate:allow-sleep bounded poll of the editor buffer the read loop owns; nothing tells the test when its keystroke landed
	}
	t.Fatalf("%s: the line holds %q after %s, want %q", why, h.line(), replayTestWait, want)
}

// waitForClaimed polls until the CONTROL LAYER has claimed at least n of the
// watcher's keys, and returns the count.
//
// IT USED TO POLL THE EDITOR'S HOLD (Editor.InputLockDrops) AND THAT STOPPED BEING
// THE RIGHT INSTRUMENT on 2026-10-20, when every typed key became a control: a key
// the gate claims NEVER REACHES the hold (keyDropped asks the gate first), so the
// drop count now stays at 0 for a surface that is protecting the line CORRECTLY.
// The property the cases around this helper assert is unchanged — a watcher's key
// does not edit the performance surface — and the layer's own tally is the thing
// that proves the key landed on that path instead of in the buffer.
func (h *replayLine) waitForClaimed(t *testing.T, n int) int {
	t.Helper()
	deadline := time.Now().Add(replayTestWait)
	for time.Now().Before(deadline) {
		if got := h.ctrl.controlled(); got >= n {
			return got
		}
		time.Sleep(replayTestStep) //stylegate:allow-sleep bounded poll of the layer's own count; the seam exposes no completion signal
	}
	t.Fatalf("the control layer claimed %d key(s) within %s, want at least %d — a watcher's key reached the line instead of the controls",
		h.ctrl.controlled(), replayTestWait, n)
	return h.ctrl.controlled()
}

// TestChatReplayWatcherCannotEditTheLine is DEFECT ②, reproduced minimally and
// then widened: while a replay is running, a key a HUMAN typed must not change the
// line — not a backspace, not a letter, not the Return that would submit it.
//
// THE MINIMAL REPRODUCTION IS THE BACKSPACE: the recording's text is in the
// buffer, the watcher presses `\x7f` (what a terminal sends for Backspace), and
// the line must be UNCHANGED. Before the fix the character was gone — and gone
// irreversibly, because the driver's next recorded chunk is appended to whatever
// the buffer then holds, so the pollution is baked into the rest of the
// performance.
func TestChatReplayWatcherCannotEditTheLine(t *testing.T) {
	h := newReplayLine(t, 3)
	// THE RECORDING'S OWN TYPING (injected): the performance surface is populated.
	h.inject(t, replayTestSeed)
	h.waitForLine(t, replayTestSeed, "the recording's own text was not typed into the line")

	// THE WATCHER'S KEYBOARD (typed, through the pty):
	//   \x7f  = Backspace  — the minimal reproducer: it deletes.
	//   'X'   = a rune     — it inserts.
	//   \r    = Return     — it would SUBMIT the line and end the editor's turn.
	h.typeBytes(t, "\x7f")
	if got := h.waitForClaimed(t, 1); got < 1 {
		t.Fatalf("the backspace was not held (drops=%d)", got)
	}
	h.waitForLine(t, replayTestSeed, "a watcher's backspace changed the line the recording is being typed into")

	h.typeBytes(t, "X")
	h.waitForClaimed(t, 2)
	h.waitForLine(t, replayTestSeed, "a watcher's letter changed the line the recording is being typed into")

	h.typeBytes(t, "\r")
	h.waitForClaimed(t, 3)
	h.waitForLine(t, replayTestSeed, "a watcher's Return changed the line the recording is being typed into")
	select {
	case <-h.exited:
		t.Fatalf("the watcher's Return SUBMITTED the line — the read loop returned and the replay's surface is gone")
	default:
	}

	// AND THE HOLD DID NOT EAT THE RECORDING: the driver's own arm still types.
	// This is the half that a fix by "claim every key at the gate" would fail,
	// which is why it is asserted here rather than left to the gate's own case.
	h.inject(t, " and more")
	h.waitForLine(t, replayTestSeed+" and more", "the hold refused the RECORDING's own typing")
}

// TestChatReplayArrowsLeaveTheSurfaceSilent is DEFECT ① INVERTED, on the same real
// path. The case that stood here (TestChatReplayStepKeepsThePauseAndNamesTheRun)
// asserted that a watcher's arrow MOVED A CURSOR and reported what it had done in
// a paragraph; the operator reported that behaviour three times, because the
// cursor is not on the screen and the paragraph replaced the information bar with
// an explanation of a capability the driver does not have.
//
// THE ARROW IS STILL DELIVERED THE WAY THE TERMINAL DELIVERS IT (`\x1b[D` on the
// pty master, decoded TYPED) — what changed in the 2026-10-20 SECOND pass is the
// assertion: the arrows are not inert any more, they TOGGLE like every other key
// under the any-key rule. What did not change is the two properties that matter —
// they never reach the editor (the line the recording is being typed into is
// untouched), and no line the surface writes names a key.
func TestChatReplayArrowsLeaveTheSurfaceSilent(t *testing.T) {
	h := newReplayLine(t, 6)
	h.inject(t, replayTestSeed)
	h.waitForLine(t, replayTestSeed, "the recording's own text was not typed into the line")

	// A NON-SPACE key, on purpose (2026-10-20, second pass): under the any-key rule
	// every key toggles, and pressing the one key the deleted binding table named
	// would pass against the table this change removed.
	h.typeBytes(t, "a")
	waitForCondition(t, "the pause to be recorded", func() bool { return h.ctrl.pausedNow() })
	before := strings.Join(h.ctrlOSDLines(), "\n")
	if !strings.Contains(before, "PAUSED") {
		t.Fatalf("the pause wrote no PAUSED line, so this case's premise fails: %v", h.ctrlOSDLines())
	}
	// THE LINE IS A BARE STATE WORD: the pause says PAUSED and names no key, which
	// is the whole of this epic's second half (a state, never an instruction).
	if msg := chatControlHelpViolation(before); msg != "" {
		t.Fatalf("the pause line carries help text: %s", msg)
	}

	// EVERY KEY TOGGLES (2026-10-20, second pass), so the arrows are not silent any
	// more: the assertion is the ALTERNATION — the first arrow resumes the paused
	// run, the second pauses it again — plus the two things that did NOT change:
	// the keys never reach the editor, and none of them is a quit.
	for i, seq := range []string{"\x1b[D", "\x1b[C", "\x1b[A", "\x1b[B"} { // Left, Right, Up, Down
		want := i%2 == 1
		h.typeBytes(t, seq)
		waitForCondition(t, "an arrow to toggle the pause", func() bool { return h.ctrl.pausedNow() == want })
		if h.ctrl.quitRequested() {
			t.Fatalf("arrow %q was read as a QUIT — Esc is the only key that leaves", seq)
		}
	}
	if after := strings.Join(h.ctrlOSDLines(), "\n"); len(after) == 0 {
		t.Fatalf("nothing was written at all — the case's premise fails")
	}
	for _, banned := range chatControlRetiredWords {
		if strings.Contains(strings.Join(h.ctrlOSDLines(), "\n"), banned) {
			t.Fatalf("a live OSD line carries the retired word %q: %v", banned, h.ctrlOSDLines())
		}
	}
	h.waitForLine(t, replayTestSeed, "an arrow key changed the line the recording is being typed into")
}

// ctrlOSDLines exposes the OSD the case's layer wrote.
func (h *replayLine) ctrlOSDLines() []string { return h.osd.all() }

// waitForConditionDump is waitForCondition with a state dump on failure.
func waitForConditionDump(t *testing.T, what string, cond func() bool, dump func() string) {
	t.Helper()
	deadline := time.Now().Add(replayTestWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(replayTestStep) //stylegate:allow-sleep bounded poll of a state the read loop owns; nothing signals the test when the key landed
	}
	t.Fatalf("waited %s for %s — %s", replayTestWait, what, dump())
}

// waitForCondition polls a predicate with the same bound and pacing as the other
// waits in this file.
func waitForCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(replayTestWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(replayTestStep) //stylegate:allow-sleep bounded poll of a state the read loop owns; nothing signals the test when the key landed
	}
	t.Fatalf("waited %s for %s", replayTestWait, what)
}

// TestChatReplayLineReturnsToTheReplWhenTheReplayEnds pins the other half of the
// hold's contract: it is a LOAN. The moment the replay is over (markClosing — the
// ONE mode flip the driver makes when the recording is done or the watcher's quit
// has been obeyed), the line is the REPL's again and the watcher's typing lands.
// A hold that outlived its owner would drop the first characters typed at the
// prompt the quit line promised — the same silent failure, one layer up.
func TestChatReplayLineReturnsToTheReplWhenTheReplayEnds(t *testing.T) {
	h := newReplayLine(t, 2)
	h.inject(t, replayTestSeed)
	h.waitForLine(t, replayTestSeed, "the recording's own text was not typed into the line")

	h.typeBytes(t, "\x7f")
	h.waitForClaimed(t, 1)
	h.waitForLine(t, replayTestSeed, "a watcher's backspace changed the line while the replay ran")

	if !h.in.editor.InputLocked() {
		t.Fatalf("the case's premise is wrong: the hold should be armed while the replay runs")
	}
	// (the accessor above is the READER; the premise is asserted through it rather
	// than assumed, so a constructor that stopped arming the hold fails HERE and
	// not silently in the assertion below)

	h.ctrl.markClosing()
	if h.in.editor.InputLocked() {
		t.Fatalf("the replay ended and the editing line is STILL held: the operator's own typing at the prompt would be dropped")
	}
	h.typeBytes(t, "Z")
	h.waitForLine(t, replayTestSeed+"Z", "the line was still held after the replay ended")
}
