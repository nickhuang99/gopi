package main

// chat_replay_control_test.go — the PLAYBACK CONTROLS, asserted on the two claims
// the file makes and nothing else can check for it.
//
// WHAT A UNIT TEST CAN DECIDE HERE, and what it cannot. It can decide which keys
// the layer CLAIMS (and, more importantly, which it refuses), what a key does to
// the state the driver obeys, and that a halted beat stays halted — those are
// values and transitions. It CANNOT decide that a real terminal's space bar
// reaches this layer: that is the read loop's behaviour and it is exercised by the
// package that owns the read loop (internal/lineedit/keysource_test.go drives the
// same seam through a real editor and a real injector). The two halves are
// deliberately split that way, so neither test claims the other's evidence.
//
// THE ORIGIN CASE IS THE ONE THAT MATTERS MOST, and it is built to fail an
// implementation that matched on key codes alone: the same glyph, twice, once
// typed by the terminal and once typed by the recording. Only the first may be a
// control — otherwise a replay pauses itself the moment a recorded prompt contains
// a space, and its own recorded `/quit` never gets typed.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// controlWait bounds a wait for a state change. It is a deadline that turns a hang
// into a dump, never the assertion.
const controlWait = 3 * time.Second

// The TWO runes this file drives the layer with — and NEITHER IS A BINDING. The
// layer's rule is "any key toggles, Esc leaves" (chat_replay_control.go's
// controlAction), so the control vocabulary has one named rune and it is a
// CONVENIENCE, not a binding table. These probes exist so the cases below can say
// WHICH keys they pressed without typing a bare literal (Rule 1b/13).
const (
	// chatControlToggleProbe is the key an operator most naturally presses to
	// toggle the pause. It is the production constant, not a second spelling: a
	// test that pressed a different rune would not be testing the surface's own
	// name for it.
	chatControlToggleProbe rune = chatControlToggleRune
	// chatControlHelpRuneProbe is a rune that used to be bound to a help overlay
	// and is NOT bound now. It is pressed as "a key that means nothing" — and the
	// assertion is that it TOGGLES, like every other key, and writes no help.
	chatControlHelpRuneProbe rune = '?'
)

// chatControlRetiredWords are the words of the capabilities this epic removed, in
// BOTH of its passes: the step/skip keys of 2026-10-20's first half, and the
// key-NAMING help text of its second half ("space resumes", "q/Esc leaves the
// replay"). They must not appear on ANY line this layer can still write.
//
// MEASURED ABSENT, NOT MERELY UNREACHABLE, and that is the whole point of an
// inversion: a sentence that survives in a constant is a sentence the next writer
// wires back up. The previous revision of this file shipped a test that REQUIRED
// "step back/forward" to be present — the defect frozen into the suite — so the
// replacement asserts the absence instead of a corrected sentence (a corrected
// sentence passes the same test that froze the old one).
var chatControlRetiredWords = []string{
	// the step/skip capability (2026-10-20, first half)
	"step back", "step forward", "skip",
	"never goes back", "step moves",
	// the key-naming help text (2026-10-20, second half)
	"space resumes", "space pauses", "q/Esc", "press ",
	"pauses,", "resumes,", "leaves the replay",
	"key", "keys", "help",
}

// chatControlStateLineMaxRunes is the ceiling on a state line's length. The
// operator's rule for this column is one short sentence; the measured failure was
// a 148-byte pause line carrying its own explanation of a feature that did not
// exist, and the state it was reduced to is ONE WORD. 24 bytes is the notice
// prefix plus a word, so the ceiling is the shape ("bare state") and not a taste:
// nothing that fits under it can also carry a clause.
const chatControlStateLineMaxRunes = 24

// controlOSD collects the lines the layer writes, standing in for the run's notice
// channel.
type controlOSD struct {
	mu    sync.Mutex
	lines []string
}

func (o *controlOSD) note(format string, args ...any) {
	o.mu.Lock()
	o.lines = append(o.lines, strings.TrimRight(fmt.Sprintf(format, args...), "\n"))
	o.mu.Unlock()
}

func (o *controlOSD) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// typed builds a key as the TERMINAL delivers it.
func typedKey(code lineedit.KeyCode, r rune) lineedit.Key {
	return lineedit.Key{Code: code, Rune: r, Origin: lineedit.ReadOriginTyped}
}

// injected builds the same key as the RECORDING delivers it.
func injectedKey(code lineedit.KeyCode, r rune) lineedit.Key {
	return lineedit.Key{Code: code, Rune: r, Origin: lineedit.ReadOriginInjected}
}

// TestChatReplayControlClaimsOnlyATypedKey is the case the whole design turns on.
// The SAME glyphs are offered twice — once from the terminal arm, once from the
// injector's — and the layer must claim exactly the first set. A layer that
// switched on the key code alone passes every other case in this file and fails
// this one.
func TestChatReplayControlClaimsOnlyATypedKey(t *testing.T) {
	osd := &controlOSD{}
	c := newChatReplayControl(osd.note)

	// EVERY KEY THE WATCHER MAY PRESS, as the RECORDING delivers it: none may be
	// claimed. This is the recorded-prompt case — a transcript containing "what do
	// you think?" or a sentence with spaces or an arrow is TYPED, and claiming any
	// of it would eat the recording.
	for _, k := range []lineedit.Key{
		injectedKey(lineedit.KeyRune, chatControlToggleProbe),
		injectedKey(lineedit.KeyRune, 'a'),
		injectedKey(lineedit.KeyLeft, 0),
		injectedKey(lineedit.KeyRight, 0),
		injectedKey(lineedit.KeyUp, 0),
		injectedKey(lineedit.KeyDown, 0),
		injectedKey(lineedit.KeyEnter, 0),
		injectedKey(lineedit.KeyEscape, 0),
	} {
		if c.GateKey(k) {
			t.Fatalf("the layer claimed a key the RECORDING delivered: %+v — a replay would swallow its own transcript", k)
		}
	}
	if got := c.controlled(); got != 0 {
		t.Fatalf("claiming nothing still counted %d control key(s)", got)
	}
	if lines := osd.all(); len(lines) != 0 {
		t.Fatalf("an uncontested keyframe wrote %d OSD line(s): %v", len(lines), lines)
	}

	// AND EVERY TYPED KEY IS CLAIMED — the "any key" rule, on keys that share no
	// code, no rune class and no modifier: a letter, a digit, punctuation, an
	// arrow, Enter, Tab, Backspace, a control byte. A layer that kept a binding
	// table fails on the first key it does not know; a layer that declines one
	// fails here, which is the point — a declined key reaches the EDITOR, the
	// performance surface (measured: ↑ replaced the performed line with a previous
	// prompt, 765 bytes of redraw).
	//
	// EACH KEY ON A FRESH LAYER, because Esc is a MODE CHANGE: a layer that has
	// answered Esc no longer claims anything but Esc (leftByWatcher), and asserting
	// "all of them are claimed" on ONE instance would be asserting the confusing
	// mode back into existence.
	for _, k := range []lineedit.Key{
		typedKey(lineedit.KeyRune, chatControlToggleProbe),
		typedKey(lineedit.KeyRune, 'a'),
		typedKey(lineedit.KeyRune, '7'),
		typedKey(lineedit.KeyRune, '/'),
		typedKey(lineedit.KeyRune, chatControlHelpRuneProbe),
		typedKey(lineedit.KeyLeft, 0),
		typedKey(lineedit.KeyRight, 0),
		typedKey(lineedit.KeyUp, 0),
		typedKey(lineedit.KeyDown, 0),
		typedKey(lineedit.KeyEnter, 0),
		typedKey(lineedit.KeyTab, 0),
		typedKey(lineedit.KeyBackspace, 0),
		typedKey(lineedit.KeyInterrupt, 0),
		typedKey(lineedit.KeyEscape, 0),
		// A FUSED ESCAPE (ESC followed by another byte inside the decoder's
		// sequence window arrives as Alt+<key>) must LEAVE, not toggle: the
		// second failure shape, where the key meant one thing and said another.
		{Code: lineedit.KeyRune, Rune: 'k', Alt: true, Origin: lineedit.ReadOriginTyped},
	} {
		fresh := &controlOSD{}
		one := newChatReplayControl(fresh.note)
		if !one.GateKey(k) {
			t.Fatalf("the layer refused a key the TERMINAL delivered: %+v — it would fall through to the editor, which is the surface the recording is being typed into", k)
		}
		if got := one.controlled(); got != 1 {
			t.Fatalf("key %+v was claimed but counted %d time(s), want 1", k, got)
		}
		// EVERY claimed key writes exactly one state word: the toggle's, or the
		// leave's. The OSD is not decorative — a control with no visible state is
		// the silent failure Rule 15 names.
		if lines := fresh.all(); len(lines) != 1 {
			t.Fatalf("key %+v wrote %d OSD line(s), want exactly one: %v", k, len(lines), lines)
		}
		// AND THE KEY DID THE ONE THING IT CAN DO: a non-Esc key toggles the pause,
		// Esc leaves. A key that is claimed and does NOTHING would satisfy the
		// fall-through requirement and fail this one — which is the 2026-10-20
		// regression this arm exists to catch.
		if k.Code == lineedit.KeyEscape || k.Alt {
			if !one.quitRequested() {
				t.Fatalf("Esc was claimed but did not leave the replay")
			}
			if one.pausedNow() {
				t.Fatalf("Esc paused the replay instead of leaving it")
			}
			continue
		}
		if !one.pausedNow() {
			t.Fatalf("key %+v was claimed but did not PAUSE the run — \"any key pauses\" is the contract", k)
		}
		if one.quitRequested() {
			t.Fatalf("key %+v was read as a QUIT — Esc is the ONLY key that leaves", k)
		}
	}

	// AFTER THE WATCHER LEAVES, THE LAYER OWNS EXACTLY ONE KEY. This is defect ①'s
	// contract (2026-10-04): the replay is over, the surface is the REPL's again,
	// and a playback layer that kept answering keys would be the second surface the
	// operator photographed answering keys the REPL is also acting on. Esc is kept
	// because "I already left" is information the watcher is owed (Rule 15) — a
	// second Esc that said nothing would be indistinguishable from a key the
	// surface dropped.
	leftOSD := &controlOSD{}
	left := newChatReplayControl(leftOSD.note)
	if !left.GateKey(typedKey(lineedit.KeyEscape, 0)) {
		t.Fatal("Esc must be claimed on a live replay")
	}
	if !left.leftByWatcher() {
		t.Fatal("a claimed Esc must put the layer in its leaving state — the mode the OSD and the gate both read")
	}
	for _, k := range []lineedit.Key{
		typedKey(lineedit.KeyRune, chatControlToggleProbe),
		typedKey(lineedit.KeyRune, 'a'),
		typedKey(lineedit.KeyLeft, 0),
		typedKey(lineedit.KeyRight, 0),
		typedKey(lineedit.KeyUp, 0),
		typedKey(lineedit.KeyDown, 0),
		typedKey(lineedit.KeyRune, chatControlHelpRuneProbe),
		typedKey(lineedit.KeyEnter, 0),
	} {
		if left.GateKey(k) {
			t.Fatalf("the layer claimed %+v AFTER the watcher left — the replay is over and that key belongs to the REPL", k)
		}
	}
	if !left.GateKey(typedKey(lineedit.KeyEscape, 0)) {
		t.Fatal("a second Esc must still be answered: the watcher has to be able to tell the first one registered")
	}
}

// TestChatReplayControlPauseFreezesTheDriverAndResumeReleasesIt is the pause's
// actual contract, asserted at the point the driver obeys: waitWhilePaused must
// not return while the run is paused, and must return promptly once it is not.
func TestChatReplayControlPauseFreezesTheDriverAndResumeReleasesIt(t *testing.T) {
	osd := &controlOSD{}
	c := newChatReplayControl(osd.note)

	c.GateKey(typedKey(lineedit.KeyRune, chatControlToggleProbe))
	released := make(chan struct{})
	go func() {
		c.waitWhilePaused()
		close(released)
	}()

	// THE DRIVER MUST STAY PARKED: a paused replay types nothing, so the call that
	// guards every keystroke must not have returned. The window is generous
	// (controlWait) because the assertion is "it has not returned", and a shorter
	// window would only make the test flakier without making it stronger.
	select {
	case <-released:
		t.Fatalf("a paused run was released within %s — the driver would have typed", controlWait)
	case <-time.After(controlWait):
	}

	c.GateKey(typedKey(lineedit.KeyRune, 'a')) // resume: ANY key resumes, so this one is a different key on purpose
	select {
	case <-released:
	case <-time.After(controlWait):
		t.Fatalf("resuming did not release the driver within %s", controlWait)
	}
	if c.quitRequested() {
		t.Fatalf("resuming reported that the watcher left")
	}
	if !strings.Contains(strings.Join(osd.all(), "\n"), "PAUSED") {
		t.Fatalf("pausing wrote no PAUSED line: %v", osd.all())
	}
}

// TestChatReplayControlQuitReleasesThePauseAndBecomesVisible pins the way OUT. A
// watcher who pauses and then presses Esc must not be stuck: the leave has to
// release the parked driver, or Esc while paused would be an unanswerable request
// (the very dead end the controls exist to remove).
func TestChatReplayControlQuitReleasesThePauseAndBecomesVisible(t *testing.T) {
	osd := &controlOSD{}
	c := newChatReplayControl(osd.note)
	c.GateKey(typedKey(lineedit.KeyRune, chatControlToggleProbe))

	released := make(chan struct{})
	go func() {
		c.waitWhilePaused()
		close(released)
	}()
	select {
	case <-released:
		t.Fatalf("the pause released the driver with no resume")
	case <-time.After(controlWait / 3):
	}

	if !c.GateKey(typedKey(lineedit.KeyEscape, 0)) {
		t.Fatalf("Esc was not claimed by the layer")
	}
	select {
	case <-released:
	case <-time.After(controlWait):
		t.Fatalf("leaving while paused did not release the driver within %s — the watcher would be stuck", controlWait)
	}
	if !c.quitRequested() {
		t.Fatalf("the leave did not reach the driver's exit check")
	}
	// THE DEPARTURE SAYS ONE WORD AND NOTHING ELSE — asserted by SHAPE here and by
	// the shared absence check in TestChatReplayControlPrintsNoHelpText: what the
	// watcher must not be told on the way out is which key they just pressed.
	if said := strings.Join(osd.all(), "\n"); !strings.Contains(said, "LEAVING") {
		t.Fatalf("the leave wrote no state word saying so: %v", osd.all())
	}
}

// TestChatReplayControlAnyKeyToggles is THE RULE THIS EPIC BINDS, on a real layer:
// while the recording plays, any key toggles the pause — the second press, of a
// DIFFERENT key, resumes — and only Esc leaves.
//
// THE KEYS ARE DELIBERATELY UNRELATED (a letter, a digit, punctuation, an arrow,
// Enter, a control byte): the old surface answered exactly one of them, so a case
// that pressed the same key twice would pass against the binding table this change
// removed. This is the inversion of the two cases that used to sit here
// (CursorMovesAndClamps and HeldLinesDistinguishTheTwoHeldStates, deleted
// 2026-10-20), which asserted that ←/→ moved a cursor by one beat, that ↑/↓ moved
// it by chatControlJumpBeats, and that a clamped move wrote chatControlAtEdge —
// every one of those claims was TRUE ABOUT A KEY THAT DID NOTHING THE WATCHER
// COULD SEE, because the driver types forward and cannot un-type a beat.
//
// THREE PROPERTIES, per key: ① CLAIMED (so it cannot reach the editor, which is
// the performance surface — measured on a real pty: ↑ replaced the performed line
// with a previous prompt, 765 bytes of redraw); ② it TOGGLES (claimed-but-inert
// was the 2026-10-20 regression this replaces); ③ it is not a quit.
func TestChatReplayControlAnyKeyToggles(t *testing.T) {
	keys := []lineedit.Key{
		typedKey(lineedit.KeyRune, chatControlToggleProbe),
		typedKey(lineedit.KeyRune, 'a'),
		typedKey(lineedit.KeyRune, '7'),
		typedKey(lineedit.KeyRune, '/'),
		typedKey(lineedit.KeyLeft, 0),
		typedKey(lineedit.KeyRight, 0),
		typedKey(lineedit.KeyUp, 0),
		typedKey(lineedit.KeyDown, 0),
		typedKey(lineedit.KeyEnter, 0),
		typedKey(lineedit.KeyBackspace, 0),
		typedKey(lineedit.KeyInterrupt, 0),
	}
	osd := &controlOSD{}
	c := newChatReplayControl(osd.note)

	// ① AND ②: EVERY key pauses, and every key after it resumes — the toggling is
	// measured on ONE layer, so the alternation itself is the assertion (a key that
	// paused twice, or one that resumed a run that was not paused, shows up as an
	// odd state after an even number of presses).
	for i, k := range keys {
		if !c.GateKey(k) {
			t.Fatalf("%+v fell through to the editor — every key must be claimed while the replay owns the line", k)
		}
		want := i%2 == 0
		if got := c.pausedNow(); got != want {
			t.Fatalf("after %d key(s) the run's paused state is %v, want %v (key %+v)", i+1, got, want, k)
		}
		if c.quitRequested() {
			t.Fatalf("%+v was read as a QUIT — Esc is the ONLY key that leaves", k)
		}
	}
	// ③: and the run is still alive after all of them.
	if !c.GateKey(typedKey(lineedit.KeyEscape, 0)) {
		t.Fatalf("Esc is no longer claimed after eleven toggles")
	}
	if !c.quitRequested() {
		t.Fatalf("Esc did not leave the replay")
	}

	// THE OSD IS SILENT ABOUT KEYS: no line it wrote names one. The state words are
	// the only thing this layer may print, and the words of the key-naming help
	// column are asserted ABSENT across every line the run produced.
	said := strings.Join(osd.all(), "\n")
	for _, banned := range chatControlRetiredWords {
		if strings.Contains(said, banned) {
			t.Fatalf("a live OSD line carries the retired word %q: %q", banned, said)
		}
	}
}

// TestChatReplayControlTreatsAnUnstampedKeyAsTyped pins the ZERO-VALUE decision
// the seam rests on (lineedit.ReadOrigin's doc): a Key built by hand — no origin at
// all, which is what every pre-existing caller and every test literal produces —
// means TYPED, so a control layer handed one treats it as a watcher's key. The
// alternative (zero = injected) would silently disarm the controls for any caller
// that constructs keys itself, which is the failure mode this asserts against.
func TestChatReplayControlTreatsAnUnstampedKeyAsTyped(t *testing.T) {
	osd := &controlOSD{}
	c := newChatReplayControl(osd.note)
	if !c.GateKey(lineedit.Key{Code: lineedit.KeyRune, Rune: 'a'}) {
		t.Fatalf("an unstamped key was treated as the recording's — a hand-built key would be unable to drive the controls")
	}
	if !c.pausedNow() {
		t.Fatalf("the unstamped key did not reach the state the driver obeys")
	}
}

// TestChatReplayControlPrintsNoHelpText is the assertion this epic INVERTS, and
// it is the one that used to freeze the defect into the test suite.
//
// WHAT IT SAID BEFORE (chat_replay_control_test.go:437, deleted 2026-10-20): the
// opening line had to CONTAIN the strings "pause/resume", "step back/forward",
// "the run never goes back", "skip", "help" and "quit the replay" — i.e. the test
// required the surface to advertise four arrow keys that did nothing observable,
// and any change that removed them failed the suite. A test that mandates the
// promise is a test that keeps the lie alive, which is why the fix is an
// INVERSION rather than a rewrite of the sentence. The SECOND pass (this one)
// inverts the same way for the key-naming help column: a corrected sentence
// passes the very test that froze the old one, so what is asserted is ABSENCE.
//
// FOUR PARTS:
//
//	① the run opens with NO line at all (announceStart is gone);
//	② `?` — a key that used to reprint a key list — is an ordinary toggle key
//	   now, and prints no help (it is claimed, and the state word it prints names
//	   no key);
//	③ the words of BOTH removed capabilities appear in NO line this layer can
//	   write: not in the constants, and not in what driving the keys produces;
//	④ the state lines are a bare state word: short by measurement, with no key
//	   name and no clause.
func TestChatReplayControlPrintsNoHelpText(t *testing.T) {
	osd := &controlOSD{}
	c := newChatReplayControl(osd.note)

	// ①: no opening line. Nothing has been pressed, so nothing may be printed.
	if lines := osd.all(); len(lines) != 0 {
		t.Fatalf("the layer wrote %d line(s) before any key was pressed: %v", len(lines), lines)
	}

	// ②: the retired help rune is claimed like every other key and says nothing
	// about itself.
	if !c.GateKey(typedKey(lineedit.KeyRune, chatControlHelpRuneProbe)) {
		t.Fatalf("%q fell through to the editor — under the any-key rule it is a toggle, not a help key", string(chatControlHelpRuneProbe))
	}
	if !c.pausedNow() {
		t.Fatalf("%q did not toggle the pause", string(chatControlHelpRuneProbe))
	}

	// ③: every retired word, on the constants that exist...
	for _, banned := range chatControlRetiredWords {
		for _, line := range []string{
			chatControlPausedLine, chatControlResumedLine,
			chatControlQuitLine, chatControlAlreadyLeftLine,
			chatReplayLeftNotice,
		} {
			if strings.Contains(line, banned) {
				t.Fatalf("a state line still carries the retired word %q: %q", banned, line)
			}
		}
	}
	// ...and on every line a key can still produce.
	c.GateKey(typedKey(lineedit.KeyRune, 'a')) // resume
	c.GateKey(typedKey(lineedit.KeyEscape, 0)) // leave
	c.GateKey(typedKey(lineedit.KeyEscape, 0)) // the repeat leave
	said := strings.Join(osd.all(), "\n")
	if said == "" {
		t.Fatalf("no line at all was written — the case's premise fails")
	}
	if banned := chatControlHelpViolation(said); banned != "" {
		t.Fatalf("a live line carries the retired word %q: %q", banned, said)
	}

	// ④: A BARE STATE WORD. The ceiling is the shape, not a taste: nothing that
	// fits under it can also carry a clause or a key name, and the check below
	// states the two properties a word has and a sentence does not.
	for _, line := range []string{chatControlPausedLine, chatControlResumedLine, chatControlQuitLine} {
		if n := len(line); n > chatControlStateLineMaxRunes {
			t.Fatalf("a state line is %d bytes, over the %d-byte ceiling: %q", n, chatControlStateLineMaxRunes, line)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(line, chatReplayNotice), "\n")
		if strings.ContainsAny(body, "—;,") {
			t.Fatalf("a state line is a sentence, not a state word (it carries punctuation): %q", line)
		}
		if strings.ContainsAny(body, " ") {
			t.Fatalf("a state line carries more than one word: %q", line)
		}
		if body != strings.ToUpper(body) {
			t.Fatalf("a state word is not a state word (it is not upper case, so it reads as prose): %q", line)
		}
	}
}

// TestChatReplayControlAwaitsTheCursorForAHeldBeat IS DELETED (2026-10-20) with
// the hold it asserted. It drove the driver's beatAllowed/waitForCursor pair, which
// existed so a watcher's cursor could be "behind" the beat about to be typed — and
// with no key able to move the cursor the pair could never answer anything but
// "type it". The pause's own contract is unaffected and is asserted twice above
// (the driver parks on space, the release is prompt, the quit releases a parked
// driver).

// chatControlHelpViolation reports the FIRST retired help word found in s, or ""
// when the text names nothing it may not name. It is the ONE reading of
// chatControlRetiredWords shared by the unit cases here and the pty cases beside
// them, so "no help text" cannot mean two different things in two files (Rule 13).
func chatControlHelpViolation(s string) string {
	for _, banned := range chatControlRetiredWords {
		if strings.Contains(s, banned) {
			return banned
		}
	}
	return ""
}

// lastLineContaining returns the last OSD line containing want, or "".
func lastLineContaining(lines []string, want string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], want) {
			return lines[i]
		}
	}
	return ""
}
