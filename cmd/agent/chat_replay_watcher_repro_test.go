package main

// chat_replay_watcher_repro_test.go — THE TWO MINIMAL REPRODUCTIONS, written so
// they run against an UNFIXED tree as well as this one.
//
// WHY A SECOND HARNESS EXISTS (not duplication for its own sake): the fuller case
// in chat_replay_readonly_test.go asserts through the API the FIX introduces
// (Editor.InputLocked / InputLockDrops / chatReplayControl.pausedNow), so on the
// pre-fix revision it does not COMPILE — and a case that cannot be run against the
// old code cannot show the defect it guards against. These two use only what
// existed before the fix (the pty, the production input constructor, the injector,
// the control layer's own readers), so `git stash` is enough to watch them go RED
// and back GREEN. That red-then-green pair is the evidence the report carries; the
// richer case beside them is what stays honest afterwards.
//
// DEFECT ②: a watcher's key while a replay runs must not edit the line the
// recording is being typed into.
// DEFECT ①: an arrow must NOT resume the recording, and must say what it did.

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

const (
	// reproCols / reproRows are the terminal this case gives the layout.
	reproCols = 80
	reproRows = 24
	// reproSettle is how long the line is watched after a watcher's keystroke
	// before it is read back. It is a SETTLE, not a synchronisation: the assertion
	// is "the text is unchanged", and the only way a wrong implementation could
	// hide from that is by changing the text more slowly than this.
	reproSettle = 400 * time.Millisecond
	// reproWait bounds a wait for a state the read loop sets.
	reproWait = 3 * time.Second
	// reproStep is that wait's polling interval.
	reproStep = 2 * time.Millisecond
	// reproSeed is the PERFORMANCE SURFACE: what the recording has already typed
	// into the line before the watcher presses anything.
	reproSeed = "the recording typed this"
)

// reproOSD collects the lines the control layer writes: the sentence the watcher
// reads is part of the contract (a key whose effect is invisible is defect ①).
type reproOSD struct {
	mu    sync.Mutex
	lines []string
}

func (o *reproOSD) note(format string, args ...any) {
	o.mu.Lock()
	o.lines = append(o.lines, fmt.Sprintf(format, args...))
	o.mu.Unlock()
}

func (o *reproOSD) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// reproSurface is the replay line: real pty, real chat input layer, real control
// layer at the editor's key seam.
type reproSurface struct {
	in     *chatInput
	ctrl   *chatReplayControl
	osd    *reproOSD
	master *os.File
	exited chan struct{}
}

func newReproSurface(t *testing.T, turns int) *reproSurface {
	t.Helper()
	pair := chatOpenPtyPair(t, reproCols, reproRows)
	sink := turnDumpSink(t)
	term, err := lineedit.Open(pair.Slave, sink)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	in := newChatInputWithSource(term, sink, chatPrompt)
	if in.injector == nil {
		t.Fatalf("the production input constructor built no injection seam")
	}
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: this case needs the real chat layout", reproCols, reproRows)
	}
	osd := &reproOSD{}
	ctrl := newChatReplayControl(osd.note)
	ctrl.install(in.editor)
	h := &reproSurface{in: in, ctrl: ctrl, osd: osd, master: pair.Master, exited: make(chan struct{})}
	go func() {
		defer close(h.exited)
		_, _ = in.editor.Run() //stylegate:ignore ends the repro surface's editor so the watcher stops; the rendered line is the assertion
	}()
	t.Cleanup(func() {
		in.editor.Close() //stylegate:ignore the harness's own terminal teardown
		_ = term.Close()  //stylegate:ignore the harness's own terminal teardown
		chatJoinReadLoop(t, h.exited, func() string { return runState(h.exited) })
		_ = pair.Master.Close() //stylegate:ignore pty master teardown of the watcher repro fixture, after the line was compared
	})
	return h
}

// key delivers bytes the way a TERMINAL does — through the pty master — which is
// what makes the resulting key TYPED rather than injected.
func (h *reproSurface) key(t *testing.T, s string) {
	t.Helper()
	if _, err := h.master.WriteString(s); err != nil {
		t.Fatalf("the watcher's keystroke could not be delivered to the pty: %v", err)
	}
}

// line reads the editing buffer.
func (h *reproSurface) line() string { return h.in.editor.Value().Text() }

// waitLine waits for the line to hold want.
func (h *reproSurface) waitLine(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(reproWait)
	for time.Now().Before(deadline) {
		if h.line() == want {
			return
		}
		time.Sleep(reproStep) //stylegate:allow-sleep bounded poll of the buffer the read loop owns
	}
	t.Fatalf("the line never held %q: %q", want, h.line())
}

// seed types the recording's own text — the INJECTED arm — and waits for it.
func (h *reproSurface) seed(t *testing.T) {
	t.Helper()
	if res := h.in.injector.InjectBytes(reproSeed); !res.Accepted {
		t.Fatalf("injecting the recording's own text was refused: %s", res.Why)
	}
	h.waitLine(t, reproSeed)
}

// TestReproWatcherBackspaceDoesNotEditTheLine is DEFECT ②'s minimal reproduction:
// the recording's text is in the buffer, the watcher presses Backspace, and the
// line must be UNCHANGED. RED before the fix (the character is deleted, and the
// driver's next recorded chunk is appended to the edited buffer, so the damage is
// permanent); GREEN after it.
func TestReproWatcherBackspaceDoesNotEditTheLine(t *testing.T) {
	h := newReproSurface(t, 3)
	h.seed(t)

	h.key(t, "\x7f")        // Backspace, as a terminal sends it
	time.Sleep(reproSettle) //stylegate:allow-sleep settles the pty after a Backspace write before the rendered line is read — the slave may not have redrawn yet

	if got := h.line(); got != reproSeed {
		t.Fatalf("a watcher's Backspace edited the line the recording is being typed into:\n got  %q\n want %q", got, reproSeed)
	}
}

// TestReproArrowsDoNothingAtAll is DEFECT ①'s minimal reproduction, inverted twice:
// once for the 2026-10-20 key removal and once for the any-key rule. The case used to assert that an arrow moved the cursor
// and did not resume the recording — true, and useless: the watcher could not see
// a cursor, and the key's only visible effect was a paragraph of OSD explaining
// what it had not done. The operator reported it three times.
//
// WHAT IS ASSERTED NOW, on the same real surface (a real pty, the real chat input
// layer, the real editor, the real control layer at its key seam), with the run
// PAUSED first so the driver is provably parked:
//
//	① all four arrows are CLAIMED — none of them reaches the editor;
//	② each one TOGGLES (the any-key rule; a claimed-but-inert arrow was the
//	   2026-10-20 regression this second pass replaces);
//	③ the recording's own text in the line is untouched;
//	④ and the OSD names no key: the retired help words are ABSENT from every line
//	   the whole sequence wrote.
func TestReproArrowsDoNothingAtAll(t *testing.T) {
	h := newReproSurface(t, 6)
	h.seed(t)

	// ANY KEY PAUSES (2026-10-20, second pass): the key is deliberately NOT the
	// space the old binding table named — a case that pressed the bound key would
	// pass against the binding this change removed.
	h.key(t, "a")
	deadline := time.Now().Add(reproWait)
	for time.Now().Before(deadline) && !h.ctrl.pausedNow() {
		time.Sleep(reproStep) //stylegate:allow-sleep bounded poll of the state the read loop sets
	}
	if !h.ctrl.pausedNow() {
		t.Fatalf("an arbitrary key did not reach the control layer's pause state: %v", h.osd.all())
	}
	released := make(chan struct{})
	go func() {
		h.ctrl.waitWhilePaused()
		close(released)
	}()
	select {
	case <-released:
		t.Fatalf("the pause did not park the driver at all — the case's premise fails")
	case <-time.After(80 * time.Millisecond):
	}

	for i, seq := range []string{"\x1b[D", "\x1b[C", "\x1b[A", "\x1b[B"} { // Left, Right, Up, Down
		h.key(t, seq)
		want := i%2 == 1 // the first arrow resumes the paused run, the second pauses it again
		deadline := time.Now().Add(reproWait)
		for time.Now().Before(deadline) && h.ctrl.pausedNow() != want {
			time.Sleep(reproStep) //stylegate:allow-sleep bounded poll of the state the read loop sets after an arrow
		}
		if got := h.ctrl.pausedNow(); got != want {
			t.Fatalf("arrow %q left the run's paused state at %v, want %v — every key toggles", seq, got, want)
		}
		if h.ctrl.quitRequested() {
			t.Fatalf("arrow %q was read as a QUIT — Esc is the only key that leaves", seq)
		}
	}
	// ④ NO KEY IS NAMED ON THE SURFACE: the words of the retired help column are
	// absent from every line the run wrote, arrows and pause included.
	for _, banned := range chatControlRetiredWords {
		if strings.Contains(strings.Join(h.osd.all(), "\n"), banned) {
			t.Fatalf("the OSD carries the retired help word %q: %v", banned, h.osd.all())
		}
	}
	if got := h.line(); got != reproSeed {
		t.Fatalf("an arrow changed the line the recording is being typed into:\n got  %q\n want %q", got, reproSeed)
	}
	// RELEASE THE PARKED DRIVER before the case ends (a goroutine left on this
	// layer's barrier is this case's litter, and the suite's sweep says so).
	h.ctrl.leave()
}
