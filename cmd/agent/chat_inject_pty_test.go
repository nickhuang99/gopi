package main

// chat_inject_pty_test.go — KEY INJECTION on the PRODUCTION wiring: a REAL pty,
// the REAL chat layout, and the injector installed by the constructor the REPL
// itself calls.
//
// WHY THIS CASE EXISTS BESIDE THE PACKAGE'S OWN (Rule 31). internal/lineedit's
// case proves the injector drives its own editor over a scripted source. This
// one proves the thing the tool actually reaches: the chatInput the REPL's turn
// loop drives, whose editor was built over a TERMINAL — the shape a `gopi chat`
// run has. A seam that works against a test source and is unreachable from the
// shape production builds is exactly the defect Rule 31 exists for, so the two
// rungs are kept apart on purpose and this one goes through
// newChatInputWithSource (the constructor the REPL uses) on a real pty
// descriptor.
//
// The grid is the judge (Rule 10.2): the editor paints into the dump sink, the
// sink's vt100 grid is read back, and every assertion is about CELLS — never
// about bytes written.

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

const (
	// injectPtyCols / injectPtyRows are the terminal this case gives the layout.
	injectPtyCols = 60
	injectPtyRows = 14
	// injectPtyWait bounds a wait for the read loop to paint an injected key.
	injectPtyWait = 3 * time.Second
	// injectPtyStep is that wait's polling interval.
	injectPtyStep = 2 * time.Millisecond
	// injectEscapeClaimWait bounds the wait for an injected key NAME to reach the
	// turn seam. It is the same order as injectPtyWait: the claim happens in the
	// editor's read loop, which the injector's own wake wakes — but it is a
	// separate constant because the two waits are about different events (a
	// painted cell vs. a callback the REPL's own marker is built from), and one
	// bound serving two claims is how a wait stops naming its subject.
	injectEscapeClaimWait = 3 * time.Second
)

// injectPtyGrid is the input layer under test, the sink it painted into and the
// read loop driving it.
type injectPtyGrid struct {
	in     *chatInput
	sink   *uiclient.ScreenDumper
	done   chan lineedit.Result
	exited chan struct{}
}

// newInjectPtyGrid builds the REAL chat input over a real pty — through the SAME
// constructor the REPL uses — arms the layout the way a chat run does, and starts
// the read loop.
func newInjectPtyGrid(t *testing.T) *injectPtyGrid {
	t.Helper()
	pair := chatOpenPtyPair(t, injectPtyCols, injectPtyRows)
	sink := turnDumpSink(t)
	term, err := lineedit.Open(pair.Slave, sink)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}

	// THE PRODUCTION CONSTRUCTOR. It wraps the terminal in the key injector and
	// keeps the handle on the input layer, which is the whole point of this case
	// (a constructor that forgot the wrapper leaves injector nil).
	in := newChatInputWithSource(term, sink, chatPrompt)
	if in.injector == nil {
		t.Fatalf("newChatInputWithSource built an input layer with NO injection seam: chat_surface action=key would refuse on every live run")
	}
	in.editor.SetPalette(lineedit.BasicPalette)
	in.editor.SetGeometryObserver(chatGeomObserver(sink))
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the real chat layout installed", injectPtyCols, injectPtyRows)
	}
	cols, rows, ok := in.editor.LayoutGeometry()
	if !ok || cols != injectPtyCols || rows != injectPtyRows {
		t.Fatalf("the layout installed at %dx%d (ok=%v), want the pty's %dx%d", cols, rows, ok, injectPtyCols, injectPtyRows)
	}

	h := &injectPtyGrid{
		in: in, sink: sink,
		done: make(chan lineedit.Result, 1), exited: make(chan struct{}),
	}
	go func() {
		defer close(h.exited)
		res, err := in.editor.Run()
		if err != nil {
			h.done <- lineedit.Result{Line: "ERR: " + err.Error()}
			return
		}
		h.done <- res
	}()
	t.Cleanup(func() {
		// THE ORDER, and why it is stated here rather than left to luck. The read
		// loop is parked in ReadEvent on the terminal, so the ONLY thing that
		// ends it is the terminal's own stop path — editor.Close() releases the
		// editor's layout and completion worker and deliberately does NOT stop the
		// read (the same read serves the next line). Measured before this order
		// was fixed: the wait below always expired on its full bound and the loop
		// returned only when the PTY MASTER was closed, one statement later — so
		// the case paid the bound on every run and the exit it observed was the
		// descriptor's, not the terminal's.
		//
		// Closing the terminal FIRST is therefore not tidiness: it is the stop.
		// The loop then returns the terminal-is-gone event, and the join below is
		// a join rather than a race against a close that happens afterwards.
		in.editor.Close() //stylegate:ignore the harness's own terminal teardown
		_ = term.Close()  //stylegate:ignore the harness's own terminal teardown
		// THE SAME JOIN AS THE OTHER REPL FIXTURES, through the ONE helper
		// (chat_pty_fixture_test.go): bounded, and LOUD — a loop that did not
		// return is REPORTED rather than waited out in silence (Rule 15). The
		// `select` this replaces said the same thing in its own words, which is
		// how five copies of one join, in five files, drift apart; the helper is
		// also what chat_repl_teardown_order_test.go polices (the stop must sit
		// ahead of the join in the same function).
		chatJoinReadLoop(t, h.exited, func() string { return runState(h.exited) })
		_ = pair.Master.Close() //stylegate:ignore pty master teardown of the injection fixture; the injected line was asserted from the captured dump
	})
	return h
}

// rows renders the sink's CURRENT grid — the cells a user's terminal is showing.
func (h *injectPtyGrid) rows() string {
	sd := h.sink.Snapshot()
	return strings.Join(sd.Grid, "\n")
}

// waitFor polls the painted grid until cond holds, dumping it on failure.
func (h *injectPtyGrid) waitFor(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(injectPtyWait)
	for time.Now().Before(deadline) {
		if cond(h.rows()) {
			return
		}
		time.Sleep(injectPtyStep) //stylegate:allow-sleep bounded poll (injectPtyWait) for the read loop to paint the injected key
	}
	t.Fatalf("timed out waiting for %s\n--- grid ---\n%s", what, h.rows())
}

// TestChatInject_ProductionConstructorExposesTheSeam is the WIRING red-proof,
// runnable without a pty: the constructor the REPL uses must actually install the
// injection seam, and the seam it installs must accept the vocabulary the tool's
// schema advertises. A constructor without the wrapper leaves injector nil, and
// action=key would refuse on every live run while every unit test stayed green.
func TestChatInject_ProductionConstructorExposesTheSeam(t *testing.T) {
	in := newChatInputWithSource(&noInputSource{}, io.Discard, chatPrompt)
	if in.injector == nil {
		t.Fatal("newChatInputWithSource built an input layer with NO injection seam")
	}
	names := lineedit.InjectableKeyNames()
	if len(names) == 0 {
		t.Fatal("the injectable vocabulary is empty")
	}
	res := in.injector.InjectKey(names[0])
	if !res.Accepted {
		t.Fatalf("the seam refused a vocabulary key (%s): %s", res.Name, res.Why)
	}
}

// TestChatInject_KeysReachTheEditorOnARealPty drives the production input layer
// over a REAL pty and asserts the result on the PAINTED GRID.
//
// The legs are chosen to separate "bytes reached the decoder" from "bytes were
// appended to the line": a printable rune must APPEAR, and a Backspace — an
// editor POLICY, not an append — must REMOVE it. A seam that wrote bytes into the
// line instead of feeding the decoder fails the second leg by name.
func TestChatInject_KeysReachTheEditorOnARealPty(t *testing.T) {
	h := newInjectPtyGrid(t)

	// 1) A printable rune. No named key produces it, so this leg is the reason
	//    the seam takes raw bytes at all.
	if res := h.in.injector.InjectKey("z"); !res.Accepted {
		t.Fatalf("injecting the rune refused: %s", res.Why)
	}
	h.waitFor(t, "the injected rune on the editing row", func(rows string) bool {
		return strings.Contains(rows, "z")
	})

	// 2) A Backspace: an editor policy. The rune must be GONE from the line —
	//    proof the byte was decoded into a KEY, not written as content.
	if res := h.in.injector.InjectKey(lineedit.KeyNameBackspace); !res.Accepted {
		t.Fatalf("injecting backspace refused: %s", res.Why)
	}
	h.waitFor(t, "backspace to remove the rune", func(rows string) bool {
		return !strings.Contains(rows, "z")
	})

	// 3) The line is still live: a second rune lands after the edit.
	if res := h.in.injector.InjectKey("q"); !res.Accepted {
		t.Fatalf("injecting the second rune refused: %s", res.Why)
	}
	h.waitFor(t, "the second rune on the editing row", func(rows string) bool {
		return strings.Contains(rows, "q")
	})
}

// TestChatSurfaceKey_SurfaceForwardsTheBoundAndTheRenewal is the WIRING case for
// the two numbers the tool's operands are about — asserted on the REAL surface
// object (cmd/agent's chatSurface), not on a stub.
//
// WHY IT EXISTS (Rule 31 — and the live run that found it). The agent-level cases
// drive the agent's dispatcher with a STUB surface, so they prove the dispatcher
// passes a bound and a renewal on. They cannot prove the REAL surface does
// anything with either, and it did not: a live `gopi chat --tools` run — a real
// pty, a real REPL, a scripted model endpoint — showed the model ask for
// `max_keys: 4` and the run charge `keys_used=1/64`, i.e. the surface dropped the
// operand before the injector ever saw it. That is exactly the defect class Rule
// 31 describes: correct in isolation, wrong at the boundary production builds.
func TestChatSurfaceKey_SurfaceForwardsTheBoundAndTheRenewal(t *testing.T) {
	h := newInjectPtyGrid(t)
	s := &chatSurface{input: h.in}

	// 1) THE BOUND REACHES THE INJECTOR. A period ceiling far below the declared
	//    one is the observable: a surface that drops the operand reports the
	//    declared 64, and the ceiling never fires where the caller asked.
	tight := agent.ChatSurfaceKeyFloor()
	first := s.InjectKey(lineedit.KeyNameDown, tight)
	if !first.Accepted {
		t.Fatalf("the first key of a period bounded by %d was refused: %s", tight, first.Why)
	}
	if first.Bound != tight {
		t.Fatalf("the surface charged the key against bound=%d, want the operand %d — the bound is being dropped between the tool and the injector",
			first.Bound, tight)
	}
	for i := 2; i <= tight; i++ {
		if res := s.InjectKey(lineedit.KeyNameDown, tight); !res.Accepted {
			t.Fatalf("key %d of %d refused: %s", i, tight, res.Why)
		}
	}
	spent := s.InjectKey(lineedit.KeyNameDown, tight)
	if spent.Accepted {
		t.Fatalf("the key past a %d-key period was accepted: %+v", tight, spent)
	}
	if spent.Used != tight || spent.Bound != tight {
		t.Fatalf("the refusal reports %d/%d, want %d/%d", spent.Used, spent.Bound, tight, tight)
	}

	// 2) A CALLER'S LARGER BOUND CANNOT RAISE IT — the injector's own second line
	//    of defence, reached through the real surface.
	//
	//    The operand is derived from THIS RUN's injection ceiling
	//    (chatReplayInjectCeiling: the self-driven loop's declared bound, or the
	//    replay's own budget when `--replay` named a recording) rather than from
	//    chat_surface's declared constant alone — the two agree for every run
	//    that is not a replay, and the point of the case is the RATIO (a caller
	//    asking for more than its own ceiling gets nothing), which is
	//    independent of which ceiling this process latched.
	over := chatReplayInjectCeiling() * 10
	if res := s.InjectKey(lineedit.KeyNameDown, over); res.Accepted {
		t.Fatalf("a caller bound of %d raised a spent %d-key period: %+v", over, tight, res)
	}

	// 3) THE RENEWAL REACHES THE COUNTER. This is the fix itself: the carrier's
	//    boundary reaches the real surface and opens a fresh key period, so the
	//    very next key is accepted with the period count back at 1.
	s.ResetKeys(tight)
	fresh := s.InjectKey(lineedit.KeyNameDown, tight)
	if !fresh.Accepted {
		t.Fatalf("the first key after the renewal was refused: %s — the key period is not being reopened", fresh.Why)
	}
	if fresh.Used != 1 {
		t.Fatalf("the renewed period reports %d keys used, want 1 (a fresh period)", fresh.Used)
	}
	// And the surface's own accessor agrees with the answer the tool printed: one
	// number, read from the object that is charged and refuses.
	if used, _ := s.KeyUsed(); used != fresh.Used {
		t.Fatalf("the surface reports %d keys used, but the injection answered %d — the tool's report and the counter disagree",
			used, fresh.Used)
	}
}

// noInputSource is a Source with nothing to deliver — the editor built over it
// must still ACCEPT injected keys (they queue at the source, not inside a
// terminal).
type noInputSource struct{}

// ReadEvent reports no event, immediately.
func (noInputSource) ReadEvent(buf []byte, timeoutMS int) (lineedit.Event, error) {
	return lineedit.Event{Kind: lineedit.EventNone}, nil
}

// Width reports the fallback (this source has no geometry of its own).
func (noInputSource) Width(fallback int) int { return fallback }

// Height reports the fallback.
func (noInputSource) Height(fallback int) int { return fallback }

// TestChatInject_EscapeByNameReachesTheTurnSeam is the NAME-PATH rung of the
// pure interrupt (task 01a0e84b): the tool's `action=key escape` — a NAME the
// schema advertises — must reach the editor's TURN ARM, carrying the origin the
// marker and the session record are built from.
//
// WHY THIS RUNG EXISTS ON TOP OF THE pty ACCEPTANCE CASE. That case injects the
// RAW 0x1b byte on the pty master, so it covers the decoder; it does NOT cover
// the entry point a model actually uses, whose key is resolved from the
// injectable table by name (injectableKeyBytes). A table whose `escape` row
// carried the wrong bytes would leave the acceptance case green and the tool's
// own spelling broken — the exact gap between "the bytes work" and "the NAME
// which is the advertised surface works" (Rule 31).
//
// The turn seam is installed HERE, at the same two setters the REPL calls
// (chatInput.SetInterrupt → SetMinTurnActive + SetInterruptTurn). What the claim
// does with the turn is the REPL's business and is covered end to end; this case
// asserts the hand-off, which is the part the NAME can break.
func TestChatInject_EscapeByNameReachesTheTurnSeam(t *testing.T) {
	h := newInjectPtyGrid(t)

	var mu sync.Mutex
	var origins []lineedit.KeyOrigin
	turnActive := true
	h.in.editor.SetMinTurnActive(func() bool { return turnActive })
	h.in.editor.SetInterruptTurn(func(origin lineedit.KeyOrigin) bool {
		if !turnActive {
			return false
		}
		mu.Lock()
		origins = append(origins, origin)
		mu.Unlock()
		return true
	})

	// A draft on the line FIRST, so "the buffer survived" has something to be
	// about.
	if res := h.in.injector.InjectKey("d"); !res.Accepted {
		t.Fatalf("injecting the draft rune refused: %s", res.Why)
	}
	h.waitFor(t, "the draft rune on the editing row", func(rows string) bool {
		return strings.Contains(rows, "d")
	})

	// THE NAME, spelled exactly as agent.ChatSurfaceActionKey's schema advertises
	// it. It is read from the table rather than typed, so a renamed key is
	// covered by this case instead of silently skipped.
	if !injectKeyListed(lineedit.InjectableKeyNames(), lineedit.KeyNameEscape) {
		t.Fatalf("the injectable vocabulary %v does not carry %q — the tool's schema advertises a key the table cannot resolve",
			lineedit.InjectableKeyNames(), lineedit.KeyNameEscape)
	}
	res := h.in.injector.InjectKey(lineedit.KeyNameEscape)
	if !res.Accepted {
		t.Fatalf("injecting the key NAME %q refused: %s", lineedit.KeyNameEscape, res.Why)
	}

	// THE CLAIM, with the ORIGIN: waiting on the subject (the callback), never on
	// "the screen changed" — the editing row already carried the draft before the
	// Escape arrived, so a screen predicate would pass without the key.
	deadline := time.Now().Add(injectEscapeClaimWait)
	for {
		mu.Lock()
		n := len(origins)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the injected key NAME %q never reached the turn seam within %v — the tool's own spelling is broken while the raw byte works",
				lineedit.KeyNameEscape, injectEscapeClaimWait)
		}
		time.Sleep(injectPtyStep) //stylegate:allow-sleep bounded poll (injectEscapeClaimWait) for the injected key to reach the editor's read loop
	}

	mu.Lock()
	got := origins
	mu.Unlock()
	if len(got) != 1 || got[0] != lineedit.OriginEscape {
		t.Fatalf("the turn seam was told %v, want exactly [%q] — the marker and the session record are built from THIS value, so a wrong origin tells the operator about a key they never pressed",
			got, lineedit.OriginEscape)
	}
	// The origin is the ONE the record's vocabulary knows, which is what makes
	// the JSONL say reason=escape rather than ctrl-c.
	if reason := chatInterruptReason(got[0]); reason != agent.InterruptReasonEscape {
		t.Fatalf("chatInterruptReason(%q) = %q, want %q", got[0], reason, agent.InterruptReasonEscape)
	}

	// THE PURE HALF: the draft is STILL on the line. Ctrl+C's arm drops it; this
	// key must not.
	if rows := h.rows(); !strings.Contains(rows, "d") {
		t.Fatalf("the editing row lost the draft after the pure interrupt — Escape must keep the line\n--- grid ---\n%s", rows)
	}
}

// injectKeyListed reports whether want is in the injectable vocabulary — read as
// data rather than assumed. (Named for THIS list: cmd/agent already has a
// containsString, and a second one under the same name does not compile.)
func injectKeyListed(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
