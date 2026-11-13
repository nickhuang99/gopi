package main

// chat_replay_control.go — PLAYBACK CONTROL for `gopi chat --replay`: ANY key
// pressed while the recording plays PAUSES it, ANY key pressed while it is paused
// RESUMES it, and Esc leaves. THERE IS NO OTHER BINDING AND NO KEY LIST TO READ.
//
// WHY "ANY KEY" AND NOT SPACE (2026-10-20, the same day space was the only key
// that did anything). A binding table is a thing the watcher has to READ before
// playing, and this surface's job is to show a recording, not to teach a keyboard.
// "Any key toggles" is a convention a person discovers by pressing something;
// "space pauses, q leaves" is an instruction that has to be printed, and every
// printed instruction is a line of the recording the watcher does not get to see.
// The operator's own rule for this column is "要么告诉用户能干什么，要么什么都不显示" —
// and the way to satisfy BOTH halves is a control scheme simple enough that
// saying nothing loses nothing.
//
// THE ONE ASYMMETRY LEFT, stated because it is the only thing a watcher must
// learn: ESC LEAVES, and it is the ONLY key that leaves. Every other key —
// letters, digits, punctuation, the arrows, Enter, Tab, Backspace, a control byte
// — toggles the pause. Nothing else is refused and nothing else falls through
// (below).
//
// WHY THE ARROWS ARE GONE AS BINDINGS (and this is the third report of ONE
// defect). ←/→/↑/↓ were bound to "step back/forward" and "skip back/forward",
// which moved a cursor inside the recording — but the DRIVER types forward from
// where it stands and cannot un-type a beat, so the capability those keys
// advertised did not exist. Every previous repair made the LABEL longer and more
// precise instead ("(a step moves the replay's stopping point, and the run never
// goes back)", rewritten again as "the run never goes back"), and a longer
// explanation of a feature that is not there is a stronger illusion, not a fix.
// A key that has to explain what it does not do is a key that should not be
// bound: the arrows toggle, like every other key, and the surface says nothing
// about them.
//
// EVERY TYPED KEY IS CLAIMED AT THE GATE. This is a REQUIREMENT and not a
// consequence: a key this layer declines falls through to the EDITOR, and the
// editor is the performance surface — the line the recording is being typed into.
// Measured on a real pty (2026-10-20): ↑ reached the editor's history recall and
// REPLACED that line with a previous prompt (765 bytes of redraw, the performed
// text gone), and the editor's own input hold did not stop it because
// lineedit.MutatesLine classifies ↑ as motion. So "any key" means claimed at the
// seam AND acted on: the key never reaches the editor, and its effect is the
// toggle.
//
// WHAT THE SURFACE SAYS, now that it may not say help: one bare state word per
// transition, and NOTHING else — no key names, no clause, no "press … to …". See
// the OSD block below; the words a help column used to carry are asserted ABSENT
// by chat_replay_control_test.go.
//
// THE SAME PRINCIPLE REMOVED THE OPENING KEY LIST. announceStart printed the
// binding table before the first beat; printing a table on a surface whose whole
// job is to show a recording is noise, so the run opens with no key help at all
// (and `?`/`h`, which existed only to reprint that list, went with it). What a
// watcher gets is the STATE, one bare word per transition: PAUSED, RESUMED,
// LEAVING — and nothing that tells them what to press, because any key proves it.
//
// WHY THIS FILE EXISTS. A replay that cannot be paused is not a replay: the
// operator's own words are "我无法暂停，或者快进快退，或者退出…否则就失去了回放的目的".
// Until now a `gopi chat --replay` run was a one-way performance — the driver
// typed the recording at its own pace and the watcher could only watch.
//
// THE KEYS COME FROM THE EDITOR, NOT FROM A SECOND READER. This is the whole
// design and everything else follows from it. The read loop already owns the
// terminal: it is inside the reserved layout, it is the layout's only writer, and
// a second reader on os.Stdin would fight it for bytes — the two would
// interleave, a byte a human typed could be eaten by whichever read came first,
// and the recording's own typing would sometimes be read by the wrong party. So
// the control layer installs itself at the editor's OWN key seam
// (lineedit.KeyGate) and claims the keys it defines, before the editor applies
// them.
//
// THREE CONSEQUENCES, each one a requirement this file would otherwise have to
// invent a mechanism for:
//
//  1. AN OPERATOR'S KEY IS DISTINGUISHABLE FROM THE DRIVER'S, and the
//     distinction is a FACT rather than an inference: every decoded key carries
//     its read's origin (lineedit.Key.Origin), so "space" from the terminal is a
//     pause while "space" typed by the recording is a space. A control layer that
//     matched on the key alone would pause the run every time a recorded prompt
//     contained a space — and the run's own exit line, a recorded `/quit`, must
//     still be typed.
//  2. A CONTROL KEY IS NEVER CHARGED AGAINST THE INJECTION BUDGET. The keys the
//     watcher presses are not injected; they arrive from the terminal arm. The
//     budget exists to bound the MODEL's and the SCRIPT's keystrokes
//     (chat_replay_budget.go), and a watcher who presses space a hundred times has
//     spent nothing the bound was protecting — the run's exit line still injects
//     afterwards, which is the acceptance case this file is tested for.
//  3. PAUSE FREEZES THE DRIVER, AND NOTHING ELSE — that is deliberate and it is
//     what keeps the controls usable. The driver parks before its next keystroke,
//     so not one byte is typed while paused and the picture stops changing. What
//     does NOT park is the read loop: the watcher's next key has to be READ, or
//     `q` while paused would be queued behind the pause that the watcher is
//     pressing it to escape. So the surface stays live (nobody's keystroke is
//     swallowed, the terminal is never left in a special mode) and the only thing
//     that stops is the typing.
//
// THE OSD GOES THROUGH THE NOTICE CHANNEL, which is the ONE destination whose
// routing already knows whether this run is a film's producer
// (chat_notice_sink.go): a plain replay prints the controls on the operator's
// terminal, and a self-filming run keeps them OFF the photographed screen — in
// the session's own console.log — so the delivered frames carry no OSD text.

import (
	"sync"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// The control vocabulary. It is TWO ROWS now, and the second one is empty: every
// key toggles, so there is no binding table — there is a RULE and the rune the
// tests and the pty harness drive it with.
const (
	// chatControlToggleRune is the key an operator most naturally presses to toggle
	// the pause, and the rune a test or a driver sends when it wants ONE named key
	// rather than "any". IT IS NOT SPECIAL: chat_replay_control.go's controlAction
	// resolves every key to the same toggle, and this name exists so no caller types
	// a bare ' ' in a sentence of code (Rule 1b/13).
	chatControlToggleRune rune = ' '
)

// chatControlReleasePoll bounds how long a parked driver waits between checks on
// the state it is parked in. It is a POLL and not a channel wait for one reason
// worth stating: the pause must be endable by ANY key, and the goroutine parked
// here cannot see the keys itself — it only sees their effect. A short poll keeps
// the resume prompt without making the driver's own wait a busy spin (the cost
// of a poll that finds the same answer is one comparison, and no OSD line is
// written by it).
const chatControlReleasePoll = 5 * time.Millisecond

// The OSD text. ONE BARE STATE WORD PER TRANSITION, and there is nothing else on
// this surface: no key names, no "press X to Y", no clause saying what a key does
// not do.
//
// WHY THERE IS NO KEY HELP, stated here because this is where the next reader
// will be tempted to add some back. The information bar sits inside the picture
// the watcher is watching, so every word of instruction is a word of the
// recording they do not get to see — and the two things a key list would teach
// them ("something toggles the pause", "Esc gets you out") are exactly the two
// things the SCHEME teaches by being simple: any key toggles, only Esc leaves.
// The operator's rule for this column is "要么告诉用户能干什么，要么什么都不显示", and
// this surface satisfies it by saying nothing a key press cannot teach.
//
// WHAT WAS HERE BEFORE, and is now DELETED rather than shortened, because a
// shorter instruction is still an instruction: "PAUSED — space resumes, q/Esc
// leaves the replay", "resumed — space pauses, q/Esc leaves the replay", and the
// "space"/"q/Esc" names that composed them. The words are asserted ABSENT by
// chat_replay_control_test.go (Rule: a test asserts what must NOT be there, not a
// corrected sentence — a corrected sentence passes the same test that froze the
// old one).
//
// NOTE ON WHAT A STATE WORD IS NOT: it is not a coordinate and not a count. The
// line says PAUSED because the run IS paused (a pause with no visible state is a
// silent failure, Rule 15) — it says nothing about where in the recording it is,
// because a position is not something a watcher can act on and the cursor that
// would have made it meaningful is gone with the step keys.
const (
	chatControlPausedLine  = chatReplayNotice + "PAUSED\n"
	chatControlResumedLine = chatReplayNotice + "RESUMED\n"
	// chatControlQuitLine is what the watcher reads the moment they press Esc: the
	// replay is ENDING. It names no key and gives no instruction — the remaining
	// recorded turn(s) are not typed, and the prompt that comes back is an ordinary
	// `gopi chat` prompt. That sentence used to say all of that in prose; the STATE
	// is the one fact the surface owes, and the rest the operator learns by the
	// prompt appearing.
	chatControlQuitLine = chatReplayNotice + "LEAVING\n"
	// chatControlAlreadyLeftLine answers Esc pressed a second time: the replay is
	// already on its way out. It is the ONLY key this layer still answers after the
	// leave (see leaveAgain), and saying so is the difference between a key that
	// registered and a key the surface dropped — with one word, because the reason
	// it cannot act twice is not something the watcher has to read to act.
	chatControlAlreadyLeftLine = chatReplayNotice + "LEAVING\n"
)

// WHAT THIS BLOCK USED TO HOLD, in one comment instead of a dozen constants,
// because the removal is the deliverable and the next reader must not re-invent
// it. DELETED (2026-10-20): chatControlKeysLine (the opening key list),
// chatControlMovedLine, chatControlHeldLine, chatControlAtEdge,
// chatControlMoveApplied, chatControlAtEdge, chatControlHeldClause,
// chatControlPausedByStepC, chatControlStepNameFwd/Bk, chatControlStepFwd/Back,
// chatControlJumpFwd/Back, chatControlSkipNameFwd/Bk, chatControlJumpGlyph,
// chatControlStepForwardGlyph, chatControlStepBackwardGlyph,
// chatControlJumpUpGlyph, chatControlJumpDownGlyph, chatControlHelpRune,
// chatControlHelpLetter, chatControlJumpBeats, and chatControlResumedSuffix
// (that last one was retired as prose in 2026-10-05; its obituary paragraph goes
// with the keys it described).
//
// EVERY ONE OF THEM DESCRIBED A STEP OR A SKIP. The driver types the recording
// FORWARD from where it stands and cannot un-type a beat, so the cursor those
// keys moved changed nothing the watcher could see, and each repair to the
// sentence describing it made the promise more specific and more false. The
// gates below are what keep them from coming back: chat_replay_control_test.go
// asserts the words ABSENT, and internal/replayplay's key map asserts the same
// for `gopi session replay`.

// chatReplayControl is the playback control layer of ONE replay run.
//
// ITS STATE IS THREE BITS — paused, quit, closing — AND THAT IS ALL IT HAS. There
// is no position in this layer and there is no cursor: the arrows that used to
// move one are gone (this file's header says why), so the only thing the driver
// asks this layer is "may I type, and has the watcher left". A field nothing
// reads is a promise nobody keeps — and a cursor nothing can move is exactly the
// defect this epic removed (Rule 31: an unexecuted surface is not a delivered
// one).
//
// ITS STATE IS THE RUN'S, and it is touched by two goroutines: the read loop
// (which consumes the watcher's keys at the editor's key seam) and the replay
// driver (which asks, before every keystroke it types, whether it may type). The
// mutex is therefore the whole of the synchronisation, and the release channel is
// how a parked driver is woken: a driver waiting on a tick cannot be told
// anything by a bool it is not watching, and a resume that had to wait for the
// next tick would make the controls feel dead.
type chatReplayControl struct {
	// notify is where this layer's OSD text goes. It is the run's ONE notice
	// channel, whose destination is chosen by whether the run films itself
	// (chat_notice_sink.go).
	notify func(string, ...any)

	// ed is the editor this layer owns while the replay runs — the surface whose
	// line the HOLD (lineedit.Editor.SetInputLocked, taken in install) makes
	// read-only, and the handle releaseLine gives back when the replay ends. It is
	// nil when the run has no editor at all (the cooked path, where a replay is
	// refused), and every use is nil-safe.
	ed *lineedit.Editor

	mu     sync.Mutex
	paused bool
	quit   bool
	// closing is set by leftByWatcher() ONCE the driver has obeyed the quit and
	// is on its way out (chat_replay.go's run). It is the MODE BIT the watcher
	// reads: true means "the replay is over, the surface is the REPL's again",
	// and every key that arrives from then on goes to the editor untouched.
	//
	// WHY IT IS NOT `quit`. quit answers "the watcher told us to leave"; closing
	// answers "the replay has left". They are different moments, and conflating
	// them is the confusing mode this file's header names: between the two, the
	// driver may still be finishing a recorded line, so a key claimed there is
	// claimed by a layer whose run has already ended — the watcher sees the OSD
	// answering them while the REPL prints its own commands, and cannot tell
	// which of the two surfaces they are talking to.
	closing bool
	resumed chan struct{}
	// controls counts the keys this layer CONSUMED — the watcher's own
	// keystrokes, which are the ones no injection budget ever sees. It is
	// reported so an acceptance case can print how many were spent rather than
	// assert a bound nothing measured.
	controls int
}

// chatControlStartLine announced that a replay was under way, with its size and
// its pace. IT IS RETIRED (2026-10-11): it was written by nothing — no caller had
// used it since the controls landed, so the first OSD line a watcher ever read was
// chatControlKeysLine — and the two things it said are already said better elsewhere
// (the size is in the recording the run was launched with, the pace is not a fact
// about this build, and "this is a playback" is what chatControlKeysLine's own
// "playback:" prefix states). A dead constant that still names internal counters is
// exactly the kind of line this epic removed, so it is DELETED rather than left as
// a trap for the next reader who wires it up. The sentence it held, in full, was
// "replaying %d recorded turn(s) at %s — this is a playback, not a live session".

// newChatReplayControl builds the control layer for a recording of `turns`
// recorded turns.
//
// IT NO LONGER TAKES THE TURN COUNT (2026-10-20): the count existed to seed and
// clamp the CURSOR, and the cursor is gone. The parameter stayed one release
// longer than the field because the constructor's signature is called from
// production and from three test files — and a parameter kept "so the callers do
// not change" is a knob that reads as meaningful while nothing reads it, which is
// the shape of every defect this epic closed. It is deleted, and the callers with
// it.
func newChatReplayControl(notify func(string, ...any)) *chatReplayControl {
	if notify == nil {
		// A layer built with nowhere to write still CONTROLS the run — a test
		// asserting a state transition must not have to stand up an OSD, and a
		// dropped message would otherwise be a latent nil dereference on a path
		// whose whole point is that claiming a key and saying so are one act
		// (GateKey). Rule 15's concern is the opposite one — a message that looks
		// sent and reaches nobody — and here the caller has said, by passing nil,
		// that there is no destination.
		notify = func(string, ...any) {}
	}
	return &chatReplayControl{
		notify:  notify,
		resumed: make(chan struct{}),
	}
}

// install puts this layer at the editor's key seam AND takes the editor's hold
// over its own line (lineedit.Editor.SetInputLocked). The two are installed
// together on purpose, because they are two halves of ONE promise:
//
//   - the GATE claims the keys this layer defines, before the editor acts on
//     them (pause and quit — nothing else);
//   - the HOLD makes the rest of the surface read-only for a HUMAN's typing. It
//     is what defect ② needs and what the gate alone cannot give: every key the
//     gate declines falls through to the editor BY DESIGN, and while a replay
//     runs the fall-through is a watcher editing the performance surface — the
//     buffer the driver is typing the recording into. The recording itself is
//     INJECTED, and the hold refuses only keys a human typed, so the driver's
//     own keystrokes are unaffected (lineedit.MutatesLine + Key.Origin).
//
// A nil editor (the cooked, `--no-editor` path, where a replay is refused anyway)
// leaves both uninstalled rather than panicking: the run then has no controls,
// which its own refusal sentence already says.
func (c *chatReplayControl) install(ed *lineedit.Editor) {
	if c == nil || ed == nil {
		return
	}
	c.mu.Lock()
	c.ed = ed
	c.mu.Unlock()
	ed.SetInputLocked(true)
	ed.SetKeyGate(c)
	// THE WINDOW SAYS IT — THE MARKER ROWS DO NOT. lineedit's scrolling marker
	// rows advertise their own bindings ("PgUp/PgDn scroll · Alt+↑/↓ one row")
	// whenever the editing line hides text. That sentence is TRUE of the editor
	// this window belongs to and FALSE here: the gate below claims every typed
	// key before the editor sees it, so a scroll key never reaches the window,
	// and the row was redrawn on every frame (measured 2026-10-22 on a real pty:
	// 103 occurrences in a single replay, 33 of them after the first key press —
	// exactly the "help text polluting the info bar" the operator reported). The
	// COUNTS stay ("… 8 lines above" is a fact about their own text); only the
	// key names go.
	ed.SetWindowKeyRow(false)
}

// releaseLine gives the editing line back to whoever owns the surface now. It is
// called at the ONE moment the replay stops owning it (markClosing), and its
// absence would break the promise the quit line makes in so many words — "the
// prompt you get back is an ordinary `gopi chat` prompt": a hold still armed
// would drop the first characters the operator typed at that prompt, which is the
// silent defect this file exists to remove, one layer up.
func (c *chatReplayControl) releaseLine() {
	if c == nil {
		return
	}
	c.mu.Lock()
	ed := c.ed
	c.mu.Unlock()
	if ed != nil {
		ed.SetInputLocked(false)
		// THE MARKER CHROME COMES BACK WITH THE LINE. install turned the editing
		// line's key statement off because this surface claimed every typed key;
		// from here on it claims nothing, so the REPL's own window is allowed to
		// advertise its own scroll keys again. Without this the operator's ordinary
		// prompt would have lost a sentence for the rest of the process — a fix in
		// one surface silently editing another, which is the class of defect both
		// this file and lineedit/inputlock.go exist to prevent.
		ed.SetWindowKeyRow(true)
	}
}

// announceStart() IS DELETED (2026-10-20). It printed the binding table before
// the first beat, so the watcher learned the keys by being told rather than by
// pressing things — which was worth a line when there were eight of them. There
// is ONE RULE now (any key toggles, Esc leaves), the state words name no key at
// all, and an opening line on a surface whose whole job is to show a recording is
// noise that pushed the recording down a row. `?`/`h`, which existed ONLY to
// reprint that list, went with it: a help key for a one-rule surface is a key
// that answers a question nobody asked.
//
// THE CALLER WENT TOO. chat_replay.go's run() called this on the interactive
// path; a helper left unwired "in case a caller wants it" is the trap Rule 31
// names, so the call site is removed in the same change rather than left
// reachable-and-unused.

// GateKey is the lineedit.KeyGate half: it claims the keys this layer defines,
// and only those — everything else falls through to the editor, which is what
// keeps the surface's ordinary behaviour (typing, history, the completion menu)
// available during a replay.
//
// THE ORIGIN TEST COMES FIRST and is not a detail: a recorded prompt containing
// a space, an arrow, a `q` or a `?` is TYPED BY THE DRIVER (lineedit stamps every
// key with its read's origin), and a layer that claimed those by their code alone
// would swallow the recording it exists to show.
//
// EVERY OTHER TYPED KEY IS CLAIMED — that is the "any key" half of this file's
// contract, and the fall-through is why it has to be unconditional: a declined key
// reaches the editor, and the editor is the performance surface. See controlAction.
func (c *chatReplayControl) GateKey(k lineedit.Key) bool {
	if c == nil || k.Origin != lineedit.ReadOriginTyped {
		return false
	}
	// THE REPLAY IS OVER: this layer claims exactly ONE thing from here on — Esc,
	// so a watcher who presses it twice learns that the FIRST one registered
	// (chatControlAlreadyLeftLine). Every other key is RELEASED, and that release
	// is the whole of the confusing-mode fix: a pause or a toggle answered by a
	// layer whose run has ended is the two-surfaces-at-once the operator
	// photographed, and the surface the watcher is then talking to is the REPL's.
	if c.leftByWatcher() {
		return c.leaveAgain(k)
	}
	act := c.controlAction(k)
	if act == nil {
		return false
	}
	c.claim()
	act()
	return true
}

// leaveAgain claims ONLY Esc once the replay has ended. It exists so the release
// above is a RELEASE and not a blackout: "I already left" is information the
// watcher is owed, and a second Esc that said nothing would be indistinguishable
// from a key the surface dropped (Rule 15).
//
// IT IS ESC AND NOTHING ELSE (2026-10-20). While the replay runs every key toggles
// the pause; once it is ending there is no pause left to toggle, so the layer
// releases the whole keyboard except the one key that means "leave" — which is
// still answered, with the state word, because a quiet key here reads as a
// dropped one.
func (c *chatReplayControl) leaveAgain(k lineedit.Key) bool {
	if k.Code == lineedit.KeyEscape {
		c.claim()
		c.leave()
		return true
	}
	return false
}

// leftByWatcher reports whether the replay has ENDED — either the driver finished
// the recording (it is closing itself down) or the watcher quit and the driver has
// obeyed. It is the ONE predicate behind the mode, so the OSD line, the key gate
// and the driver's own stop condition cannot disagree about which surface the
// watcher is talking to (Rule 13).
//
// IT IS READ BY THE DRIVER TOO (chat_replay.go's run): a watcher who quits must
// END the replay, not merely stop its typing. The measured defect this closes is
// that the two were different things — the driver stopped typing, printed "the
// watcher left the replay", and left the REPL parked at a prompt whose answers
// still came from the recording, waiting for a line that is, in a replay, nobody's
// to type.
func (c *chatReplayControl) leftByWatcher() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quit || c.closing
}

// controlAction resolves a TYPED key to the action it performs. It returns nil
// for NO key: every key the watcher types is claimed here, which is why the
// signature's "or nil when this layer does not own the key" contract now has
// exactly one exception — a key that is not typed at all, refused by GateKey's
// origin test before this function is reached.
//
// THE WHOLE BINDING TABLE IS THREE LINES, and that is the design:
//
//	ESC        leave
//	Alt+<key>  leave (a fused ESCAPE — see below)
//	everything toggle pause/resume
//
// WHY "EVERYTHING" IS LITERAL AND NOT "everything except the arrows". Every key
// this function declines FALLS THROUGH TO THE EDITOR (lineedit's seam has no third
// destination), and the editor is the PERFORMANCE SURFACE — the line the recording
// is being typed into. Measured on a real pty before this arm was unconditional:
// ↑ reached the editor's history recall and REPLACED that line with a previous
// prompt (765 bytes of redraw, the performed text gone), and the editor's input
// hold did not stop it because lineedit.MutatesLine classifies ↑ as motion. The
// previous repair claimed the four arrows and made them INERT; the same guarantee
// with one rule instead of a special case is: no key is declined, so no key can
// reach the editor, and the key that would have been inert instead does the one
// thing this surface does.
//
// IT IS ALSO WHAT MAKES THE SCHEME LEARNABLE WITHOUT A KEY LIST. "Any key pauses,
// any key resumes, Esc leaves" is a sentence nobody has to print because pressing
// one key proves it; a table of per-key meanings is what has to be printed.
//
// A NOTE ON Ctrl+C (KeyInterrupt): it toggles, like every other key, and NOT
// because its usual meaning was overlooked. Inside a replay the run ends on Esc or
// when the recording does, and a watcher who presses Ctrl+C on this surface is a
// watcher who wants it to stop moving — the same intent as the pause. Leaving it
// unclaimed would send it to the editor, which is the fall-through above.
func (c *chatReplayControl) controlAction(k lineedit.Key) func() {
	if k.Code == lineedit.KeyEscape {
		return c.leave
	}
	// A FUSED ESCAPE LEAVES TOO (2026-10-27). An ESC that is the head of a
	// sequence is resolved by a timeout, so a watcher's Escape pressed and
	// followed by another byte INSIDE that window arrives as ONE key: Alt+k for
	// ESC+k written together, Alt+Enter for ESC+CR. The editor never binds a bare
	// Alt+<key> (handleKey's own first arm: it inserts nothing), so on this
	// surface such a key has no other meaning it could be carrying, and the only
	// way a watcher produces one is by pressing Escape and then something else
	// faster than the sequence window. Mapping it to `leave` is therefore the
	// recovery binding for the SECOND failure shape — the fused-Escape that used
	// to announce PAUSED, i.e. a key meaning one thing and saying another.
	//
	// (The FIRST shape — the fusion ACROSS PRODUCERS — is fixed where it belongs,
	// in lineedit's own resolution: a byte from another producer can no longer
	// complete a human's Escape at all. See Editor.resolveBareEscape.)
	if k.Alt {
		return c.leave
	}
	return c.toggle
}

// claim counts one key taken from the watcher. Every claimed key spends nothing
// the injection budget was protecting — the budget bounds what is TYPED into the
// editor, while these keys arrive FROM the terminal — and the count exists so that
// claim is a number rather than a sentence.
func (c *chatReplayControl) claim() {
	c.mu.Lock()
	c.controls++
	c.mu.Unlock()
}

// KeyUnclaimed and KeyRelease complete the seam's contract (lineedit's KeyGate is
// total: an owner is told about every key exactly once). This layer owns no
// opinion about keys it did not claim — "this layer did not want it" is said by
// not claiming it — so both are deliberately empty rather than absent.
func (c *chatReplayControl) KeyUnclaimed(lineedit.Key) {}

// KeyRelease reports that the editor is about to apply the key. The control layer
// has nothing to do with it; it exists so the seam's triple stays disjoint for
// every owner.
func (c *chatReplayControl) KeyRelease(lineedit.Key) {}

// toggle flips pause/resume and announces the state it landed in, so the OSD and
// the actual behaviour cannot disagree (Rule 15 — a pause with no visible state
// is the silent failure this line prevents).
//
// IT IS THE ONLY ACTION a non-Esc key has (controlAction), so every line a watcher
// can read from a key press is one of these two — and they are ONE WORD each. The
// prose they used to carry is in the OSD block's obituary: a sentence naming the
// keys was, by definition, a key list, and this surface does not print one.
func (c *chatReplayControl) toggle() {
	c.mu.Lock()
	c.paused = !c.paused
	paused := c.paused
	if !paused {
		c.resumeLocked()
	}
	c.mu.Unlock()
	if paused {
		c.say(chatControlPausedLine)
		return
	}
	c.say(chatControlResumedLine)
}

// leave records the watcher's decision to stop the replay, releases anyone
// waiting on it, and says why — so the exit is attributable rather than a run
// that died while paused.
//
// IT DOES NOT RELEASE THE EDITOR'S HOLD (releaseLine), and that is deliberate:
// `q` is a REQUEST, obeyed by the driver when it stops typing and finished by
// markClosing — the one moment the replay really is over. Releasing here would
// hand the line back while the driver may still be finishing a recorded line,
// which is the two-surfaces-at-once this file's header names.
func (c *chatReplayControl) leave() {
	c.mu.Lock()
	if c.quit {
		c.mu.Unlock()
		// ALREADY LEAVING: the key is still ANSWERED. A quit that said nothing the
		// second time would be indistinguishable from a key the surface dropped —
		// the watcher pressing Esc twice must be able to tell that the first one
		// registered (Rule 15).
		c.say(chatControlAlreadyLeftLine)
		return
	}
	c.quit = true
	c.paused = false
	c.resumeLocked()
	c.mu.Unlock()
	c.say(chatControlQuitLine)
}

// move() IS DELETED (2026-10-20), with the four bindings that called it, the
// cursor it moved, and clampLocked beside it. This is the SECOND time this arm is
// removed for the same reason, so the reason is recorded where the arm used to
// be: a move shifted the watcher's cursor inside the recording, the DRIVER types
// forward from where it stands and cannot un-type a beat, so pressing an arrow
// changed nothing the watcher could observe. The 2026-10-05 repair kept the keys
// and armed a pause instead (so the arrow at least stopped the run); the
// 2026-10-11 repairs kept the keys and rewrote the sentence; the 2026-10-20
// report was, again, "arrows do nothing but print a paragraph". The answer this
// time is not a better sentence — a key whose only effect is a line explaining
// what it did not do is a key that must not be bound.

// resumeLocked wakes everything waiting on the current pause and installs a fresh
// channel for the next one. A channel closed by an earlier resume must never be
// the one a driver waits on again, which is why the swap happens here, in the one
// place a pause ends.
func (c *chatReplayControl) resumeLocked() {
	close(c.resumed)
	c.resumed = make(chan struct{})
}

// say writes one OSD line, if this run has anywhere to write it.
func (c *chatReplayControl) say(format string, args ...any) {
	if c.notify == nil {
		return
	}
	c.notify(format, args...)
}

// waitWhilePaused is the DRIVER's side of the pause: it returns as soon as the
// run may advance, and it is called before every keystroke the driver types, so a
// paused replay types not one byte. It is poll-bounded (chatControlReleasePoll)
// so a resume is prompt without the read loop and the driver sharing a lock.
func (c *chatReplayControl) waitWhilePaused() {
	if c == nil {
		return
	}
	for {
		c.mu.Lock()
		paused, quit := c.paused, c.quit
		wake := c.resumed
		c.mu.Unlock()
		if !paused || quit {
			return
		}
		select {
		case <-wake:
		case <-time.After(chatControlReleasePoll):
		}
	}
}

// chatReplayLeftNotice is what the driver prints when the WATCHER ended the
// replay — the second half of the departure, printed by the DRIVER rather than by
// the key layer because the driver is what knows the run is over.
//
// IT IS THE STATE WORD, NOT A SENTENCE (2026-10-20, with the rest of the help
// text). It used to explain the closeout ("the remaining recorded turn(s) will not
// be typed and the session closes through the ordinary closeout") — true, and
// still an instruction on a surface that may not print one: the prompt appearing
// is how the watcher learns the prompt came back. What it says now is the same
// word the key layer says, because the two moments are ONE transition and two
// different words for it would be two states the watcher has to reconcile.
//
// IT NAMED A BEAT and stopped long before (2026-10-11): a position is not
// something a watcher can act on.
const chatReplayLeftNotice = chatControlQuitLine

// quitRequested reports that the watcher left: the driver stops typing and, from
// this call on, the replay is ENDING — see leftByWatcher, which is the ONE
// predicate the driver, the key gate and the OSD all read.
func (c *chatReplayControl) quitRequested() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quit
}

// cursorOf / setHead ARE DELETED (2026-10-20) with the keys that moved the
// cursor. The driver asked cursorOf before every beat ("may I type this one?")
// and answered setHead after each; with no key able to move the cursor, both were
// a constant dressed as a question — the driver typed the whole recording unless
// the watcher paused it, and the cursor could never be anything but "at the end".
// The driver asks the layer TWO things now, and they are the two it always really
// needed: waitWhilePaused (may I type) and quitRequested (has the watcher left).

// markClosing is the MODE FLIP, and it is the driver's to make: it is called once
// the driver knows this replay is over (it finished the recording, or it obeyed a
// watcher's quit). Every key the watcher presses after it belongs to the REPL.
//
// IT IS A METHOD rather than a bare field write because the flip must be
// observable in the SAME locked read the key gate performs — a mode the gate can
// see half-updated is the confusing mode, one round later.
func (c *chatReplayControl) markClosing() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	// THE LINE GOES BACK TO THE REPL, in the same breath as the mode flip and
	// after it: from here on the watcher's keystrokes are the REPL's input, and a
	// hold still armed would drop the first characters they typed at the prompt
	// the quit line just promised them — the same silent failure this file exists
	// to remove, one layer up.
	c.releaseLine()
}

// pausedNow reports whether the run is parked. It is the READER a test asserts a
// transition with (the writer is the keys themselves): the pause's whole contract
// is a state the driver obeys, and a claim about it needs an accessor rather than
// an inference from the OSD it wrote.
func (c *chatReplayControl) pausedNow() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

// controlled reports how many keys the watcher spent on the controls — the tally
// an acceptance case prints, so "these keys were free" is a number rather than a
// claim.
func (c *chatReplayControl) controlled() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.controls
}

// chatReplayControl.state() is DELETED (2026-10-11). It rendered "%d/%d (cursor %d)" —
// the run's head, its ceiling and the watcher's cursor — for a report line, and it had
// NO CALLER anywhere in the repository (searched: the only .state() left in the tree is
// the unrelated lineedit interface). It was the LAST thing in this file that reported a
// position, and the whole of this change is that a position is not something this surface
// says: the OSD lines stopped printing numbers, and a helper kept "for diagnostics" that
// nothing calls is a line waiting to be wired back up by the next reader (Rule 31: an
// unexecuted surface is not a delivered one — and an uncalled one is not even a surface).
// A caller that really needs the position reads it from the fields it needs, under the
// same lock, in the report that is actually being written.
