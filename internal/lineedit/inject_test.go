package lineedit

// inject_test.go — the KEY INJECTION acceptance cases: a completion menu
// opened, walked and SUBMITTED with nothing but injected bytes, asserted on the
// painted CELL GRID.
//
// WHY THE GRID IS THE JUDGE (Rule 10.2). "The injector queued 4 bytes" and "the
// injector returned accepted=true" are claims about the transport. The claim
// this feature exists to make is about the INTERACTION — the menu really opened,
// the selection really moved, the line really submitted — and the only
// instrument that can see an interaction is the screen the editor painted. So
// every leg below reads a vt100 grid fed by the bytes the editor wrote.
//
// THE DISCRIMINATING KEY. The scenario is entered with a RUNE (the "/" that
// opens the slash-command menu) and driven with Down + Enter. That "/" is the
// whole point: action=type cannot produce it as a keystroke — `type` enqueues a
// LINE, so a typed "/quit" would have submitted the command (which is exactly
// the incident this feature was filed from: "/q" + Down ran /quit instead of
// opening the menu). Here "/" arrives as one byte through the decoder, the menu
// opens, Down moves the selection ON THE GRID, and Enter takes the candidate.
//
// THE TWO LAWS AN INJECTED SEQUENCE MUST RESPECT are asserted by name, because
// they are what makes this "the same path a real keyboard uses" rather than a
// second key path:
//
//	one key per read — a single write of "/magic\r" is four contiguous
//	                   characters, i.e. ONE read carrying the whole line, and a
//	                   pasted block's Enter is suppressed by the burst law
//	                   (PasteBurstMinChars keys in one read).
//	paced keys      — an injected key must still arrive as its own read: two
//	                   units delivered back to back are decoded as one batch.
//	                   (The paste burst no longer depends on this — an injected
//	                   read is exempt BY ORIGIN — the framing does.)

import (
	"strings"
	"testing"
	"time"
)

// inject geometry / budget. Named (Rule 1); the injection ceiling is read off
// the injector rather than spelled, so the two cannot drift.
const (
	injectRows = 12
	injectCols = 40
	// injectWait bounds a wait for the read loop's effect. It is a deadline that
	// turns a hang into a screen dump, never the assertion.
	injectWait = 3 * time.Second
	// injectPollStep is that wait's polling interval.
	injectPollStep = 2 * time.Millisecond
	// injectTestCeiling is the session ceiling the acceptance cases install: big
	// enough for the whole interaction (a rune, a motion, a submit), small enough
	// that an exhaustion case can reach it in a few calls.
	injectTestCeiling = 6
	// injectRuneKey is the byte that opens the slash-command menu. It is a RUNE
	// on purpose: no named key produces it, which is why the seam must accept raw
	// bytes and not only a key table.
	injectRuneKey = "/"
	// injectQuitCommand is the command a whole-line `type` would have run by
	// accident. The acceptance case asserts it NEVER appears — the negative half
	// of "the menu opened instead of the command executing".
	injectQuitCommand = "/quit"
)

// injectHarness is the editor plus the grid it painted plus the injector wired
// in FRONT of the scripted source — the exact position the production wiring
// uses (an Injector between the editor and whatever terminal it reads).
type injectHarness struct {
	ed     *Editor
	out    *recordingWriter
	screen *Screen
	src    *scriptedSource
	inj    *Injector
	stub   *stubCompleter
	done   chan Result
	exited chan struct{}
	fed    int
}

// newInjectHarness builds the editor over an injector-wrapped scripted source,
// with the A7 completion stub installed.
//
// The scripted source HOLDS at the end (it has no steps at all: every byte this
// case delivers is INJECTED), so the editor is reading while the test injects.
func newInjectHarness(t *testing.T, stub *stubCompleter) *injectHarness {
	t.Helper()
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	inj := NewInjector(src, injectTestCeiling)
	out := &recordingWriter{}
	ed := NewChatEditor(inj, out, promptForTest, nil, nil, stub.complete)
	t.Cleanup(inj.Close)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", injectRows)
	}
	h := &injectHarness{
		ed: ed, out: out, screen: NewScreen(injectCols, injectRows),
		src: src, inj: inj, stub: stub, done: make(chan Result, 1), exited: make(chan struct{}),
	}
	go func() {
		defer close(h.exited)
		res, err := ed.Run()
		if err != nil {
			h.done <- Result{Line: "ERR: " + err.Error()}
			return
		}
		h.done <- res
	}()
	t.Cleanup(func() {
		// Teardown ORDER is the same one the A7 harness establishes: stop the
		// source (which unblocks the parked read), wait for Run to return, and
		// only then close the editor — DisableRegion writes state the running
		// loop reads.
		h.stop(t)
		ed.Close()
	})
	return h
}

// stop ends the read loop and waits for Run to return.
func (h *injectHarness) stop(t *testing.T) {
	t.Helper()
	h.src.stopSource()
	select {
	case <-h.exited:
	case <-time.After(injectWait):
		t.Fatalf("Run did not return after the source was stopped\n--- screen ---\n%s", h.rows())
	}
}

// replay feeds every recorded write the grid has not seen yet, in order.
func (h *injectHarness) replay() {
	chunks := h.out.snapshot()
	for _, w := range chunks[h.fed:] {
		h.screen.Feed(w)
	}
	h.fed = len(chunks)
}

// rows renders the grid for a failure dump.
func (h *injectHarness) rows() string {
	h.replay()
	return h.screen.Contents()
}

// editRow is the layout's editing row, asked of the LAYOUT (the band sits above
// it) — never an assumed row number.
func (h *injectHarness) editRow() int {
	h.replay()
	return h.ed.region.EditorTop()
}

// screenAtLine is the editing line as the grid currently shows it.
func (h *injectHarness) screenAtLine() string {
	h.replay()
	return h.screen.Line(h.editRow())
}

// menuTopRow is the first row below the editing line that shows one of the
// stub's candidates, or -1. Derived from the RENDERED grid, not from the
// editor's live state, so it is both race-free and the thing a user sees.
func (h *injectHarness) menuTopRow() int {
	h.replay()
	for row := h.editRow() + 1; row < injectRows; row++ {
		line := h.screen.Line(row)
		for _, c := range h.stubCompleter().offers() {
			if strings.Contains(line, c.Text) {
				return row
			}
		}
	}
	return -1
}

// selectedRow is the row carrying the menu's SELECTION marker, or -1. The
// marker's spelling comes from the renderer (menuMarkerActive), never a second
// literal.
func (h *injectHarness) selectedRow() int {
	h.replay()
	for row := h.editRow() + 1; row < injectRows; row++ {
		if strings.HasPrefix(h.screen.Line(row), menuMarkerActive) {
			return row
		}
	}
	return -1
}

// waitFor polls cond (replaying the grid on every poll) until it holds or the
// deadline passes, dumping the grid on failure.
func (h *injectHarness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(injectWait)
	for time.Now().Before(deadline) {
		h.replay()
		if cond() {
			return
		}
		time.Sleep(injectPollStep) //stylegate:allow-sleep bounded poll (injectWait) for the read loop to paint the effect of an injected key — the assertion is the grid, not the wait
	}
	t.Fatalf("timed out waiting for %s\n--- screen ---\n%s", what, h.screen.Contents())
}

// submitted returns the line Run reported, if it has submitted (non-blocking).
func (h *injectHarness) submitted() (Result, bool) {
	select {
	case r := <-h.done:
		return r, true
	default:
		return Result{}, false
	}
}

// stubCompleter returns the stub the harness was built with.
func (h *injectHarness) stubCompleter() *stubCompleter { return h.stub }

// TestInject_InjectKeyResolvesNamesAndLITERALBytes pins the resolution rule:
// a named key injects that key's wire bytes, and anything else injects its own
// bytes — the two halves a caller must be able to tell apart without guessing.
func TestInject_InjectKeyResolvesNamesAndLITERALBytes(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectTestCeiling)

	// 1) A NAME resolves to its wire bytes (the arrow, not the four letters).
	if res := inj.InjectKey(KeyNameDown); !res.Accepted {
		t.Fatalf("InjectKey(%s) refused: %s", KeyNameDown, res.Why)
	}
	// 2) A LITERAL rune is injected as itself — the case action=type can never
	//    produce as a keystroke.
	if res := inj.InjectKey(injectRuneKey); !res.Accepted {
		t.Fatalf("InjectKey(%q) refused: %s", injectRuneKey, res.Why)
	}
	if res := inj.InjectKey(""); res.Accepted {
		t.Fatalf("InjectKey(\"\") accepted = true, want a refusal")
	}

	buf := make([]byte, editReadBuf)
	var got [][]byte
	deadline := time.Now().Add(injectWait)
	for len(got) < 2 && time.Now().Before(deadline) {
		ev, err := inj.ReadEvent(buf, 0)
		if err != nil {
			t.Fatalf("ReadEvent: %v", err)
		}
		if ev.Kind != EventInput || ev.Bytes == 0 {
			time.Sleep(injectPollStep) //stylegate:allow-sleep bounded poll while the injector paces the second key
			continue
		}
		got = append(got, append([]byte(nil), buf[:ev.Bytes]...))
	}
	if len(got) != 2 {
		t.Fatalf("got %d read units, want 2: %q", len(got), got)
	}
	if string(got[0]) != escCSI+"B" {
		t.Fatalf("the named key delivered %q, want the Down sequence %q (a name must not be injected as its LETTERS)", got[0], escCSI+"B")
	}
	if string(got[1]) != injectRuneKey {
		t.Fatalf("the literal delivered %q, want %q", got[1], injectRuneKey)
	}
	if res := inj.InjectKey(strings.Repeat("x", injectReadBytes+1)); res.Accepted {
		t.Fatalf("a %d-byte literal accepted = true, want a refusal (one key is one read unit)", injectReadBytes+1)
	}
}

// TestInject_OverLongByteSequenceIsRefused keeps "one injected key is one read
// unit" honest: a caller cannot smuggle a whole line in as one injection.
func TestInject_OverLongByteSequenceIsRefused(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectTestCeiling)
	long := strings.Repeat("x", injectReadBytes+1)
	res := inj.InjectBytes(long)
	if res.Accepted {
		t.Fatalf("InjectBytes(%d bytes) accepted = true, want a refusal", len(long))
	}
	if !strings.Contains(res.Why, "one key") {
		t.Fatalf("the refusal must say one injected key is one read unit; got %q", res.Why)
	}
}

// TestInject_CeilingExhaustionIsLoudAndLast is the bound's half of the contract:
// the call that spends the last key SAYS SO, and the next call is refused with
// the counters — never a silent drop.
func TestInject_CeilingExhaustionIsLoudAndLast(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, 2)
	first := inj.InjectKey(KeyNameDown)
	if !first.Accepted || first.Last {
		t.Fatalf("first of 2 keys: accepted=%t last=%t, want accepted=true last=false", first.Accepted, first.Last)
	}
	// The first key is still queued (nothing is reading): its bytes must not be
	// re-delivered or dropped by the second injection.
	second := inj.InjectKey(KeyNameUp)
	if !second.Accepted || !second.Last {
		t.Fatalf("second of 2 keys: accepted=%t last=%t, want accepted=true last=true (the exhaustion rides the call that spent it)", second.Accepted, second.Last)
	}
	third := inj.InjectKey(KeyNameEnter)
	if third.Accepted {
		t.Fatalf("third key past the ceiling accepted = true, want a refusal")
	}
	if !strings.Contains(third.Why, "ceiling is reached") || !strings.Contains(third.Why, "2 of 2") {
		t.Fatalf("the exhaustion refusal must state the counters; got %q", third.Why)
	}
}

// TestInject_DeliversEachKeyAsItsOwnRead is the framing law in isolation: the
// injector hands one key per ReadEvent, and the keys arrive IN ORDER. A source
// that batched them would put a paste on the wire.
func TestInject_DeliversEachKeyAsItsOwnRead(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectTestCeiling)
	if res := inj.InjectKey(KeyNameDown); !res.Accepted {
		t.Fatalf("InjectKey(down) refused: %s", res.Why)
	}
	if res := inj.InjectKey(KeyNameEnter); !res.Accepted {
		t.Fatalf("InjectKey(enter) refused: %s", res.Why)
	}
	buf := make([]byte, editReadBuf)
	var got [][]byte
	deadline := time.Now().Add(injectWait)
	for len(got) < 2 && time.Now().Before(deadline) {
		ev, err := inj.ReadEvent(buf, 0)
		if err != nil {
			t.Fatalf("ReadEvent: %v", err)
		}
		if ev.Kind != EventInput || ev.Bytes == 0 {
			time.Sleep(injectPollStep) //stylegate:allow-sleep bounded poll while the injector paces the second key
			continue
		}
		got = append(got, append([]byte(nil), buf[:ev.Bytes]...))
	}
	if len(got) != 2 {
		t.Fatalf("got %d read units, want 2 (one per key): %q", len(got), got)
	}
	if string(got[0]) != escCSI+"B" {
		t.Fatalf("first read = %q, want the Down sequence %q", got[0], escCSI+"B")
	}
	if len(got[1]) != 1 || got[1][0] != crByte {
		t.Fatalf("second read = %q, want the bare CR %#x", got[1], crByte)
	}
}

// TestInject_EveryNamedKeyDecodesToItsKeyCode pins the table against the thing
// it claims to be — the editor's OWN decoder. A table entry whose bytes decode
// to something else would be a key name that lies.
//
// The lone Escape is asserted through the decoder's OWN resolution of the
// ambiguity, not through Feed: its byte is also the prefix of every arrow
// sequence, so the decoder deliberately holds it (BareEscape) and the editor
// resolves it with FlushEscape when its bounded ESC wait expires — the exact
// path a keyboard's Escape takes. Asserting it any other way would be testing a
// key path the editor does not have.
func TestInject_EveryNamedKeyDecodesToItsKeyCode(t *testing.T) {
	for _, k := range injectableKeys {
		if k.label == KeyEscape {
			dec := NewDecoder()
			if keys := dec.Feed(k.bytes); len(keys) != 0 {
				t.Fatalf("key %q (%q) fed %d keys, want 0 (a lone ESC is held as an ambiguous prefix)", k.name, k.bytes, len(keys))
			}
			if !dec.BareEscape() {
				t.Fatalf("key %q (%q): the decoder does not report a bare ESC", k.name, k.bytes)
			}
			if got := dec.FlushEscape(); got.Code != KeyEscape {
				t.Fatalf("key %q (%q) resolves to code %d, want KeyEscape — the editor resolves a lone ESC this way when its bounded wait expires", k.name, k.bytes, got.Code)
			}
			continue
		}
		keys := NewDecoder().Feed(k.bytes)
		if len(keys) != 1 {
			t.Fatalf("key %q (%q) decodes to %d keys, want exactly 1", k.name, k.bytes, len(keys))
		}
		if keys[0].Code != k.label {
			t.Fatalf("key %q (%q) decodes to code %d, want %d", k.name, k.bytes, keys[0].Code, k.label)
		}
	}
}

// TestInject_MenuOpensWalksAndSubmitsOnTheGrid is THE acceptance case: the full
// interaction — open the menu, navigate it, submit — driven with nothing but
// injected bytes, asserted on the painted cell grid.
//
// It is the scenario that previously required a pty chat.write(): without key
// injection, the only way to open this menu from inside a turn was to write "/"
// to the terminal, and `type` (which is what the tool had) submits a whole LINE
// instead — so "/q" + Down executed /quit rather than opening a menu.
func TestInject_MenuOpensWalksAndSubmitsOnTheGrid(t *testing.T) {
	stub := &stubCompleter{}
	h := newInjectHarness(t, stub)

	// 1) THE MENU OPENS: one injected RUNE byte ("/"), through the decoder. No
	//    named key produces it, which is why the byte path exists.
	if res := h.inj.InjectBytes(injectRuneKey); !res.Accepted {
		t.Fatalf("InjectBytes(%q) refused: %s", injectRuneKey, res.Why)
	}
	h.waitFor(t, "the completion menu to open below the editing line", func() bool { return h.menuTopRow() >= 0 })
	if got := h.screenAtLine(); !strings.Contains(got, injectRuneKey) {
		t.Fatalf("the editing line does not show the injected rune: %q\n--- screen ---\n%s", got, h.rows())
	}

	// 2) NAVIGATE: an injected Down must move the SELECTION on the grid — the
	//    leg that proves the arrow reached handleKey as KeyDown and not as text.
	top := h.menuTopRow()
	before := h.selectedRow()
	if res := h.inj.InjectKey(KeyNameDown); !res.Accepted {
		t.Fatalf("InjectKey(down) refused: %s", res.Why)
	}
	h.waitFor(t, "the selection marker to move DOWN one row", func() bool {
		sel := h.selectedRow()
		return sel >= 0 && sel != before
	})
	if sel := h.selectedRow(); sel != top+1 {
		t.Fatalf("the selection moved to row %d, want the second candidate row %d\n--- screen ---\n%s", sel, top+1, h.rows())
	}

	// 3) ACCEPT, THEN SUBMIT. The editor's own policy is two-stage and it is
	//    asserted here rather than worked around: with the menu open, Enter
	//    TAKES THE SELECTION (handleKey's "the open menu owns Enter") and puts
	//    the completed candidate in the input slot; only an Enter with nothing
	//    left to take submits it. A single injected Enter producing a whole
	//    submission would mean the menu had been bypassed — the defect this
	//    feature exists to remove.
	candidate := stub.offers()[1].Text
	if res := h.inj.InjectKey(KeyNameEnter); !res.Accepted {
		t.Fatalf("InjectKey(enter) refused: %s", res.Why)
	}
	h.waitFor(t, "the accepted candidate on the editing row", func() bool {
		return strings.Contains(h.screenAtLine(), candidate)
	})
	if res, ok := h.submitted(); ok {
		t.Fatalf("the first Enter SUBMITTED %q — with the menu open, Enter must take the selection, not submit\n--- screen ---\n%s", res.Line, h.rows())
	}
	if got := strings.TrimRight(h.screenAtLine(), " "); !strings.HasSuffix(got, candidate) {
		t.Fatalf("editing row after the accepting Enter = %q, want the SELECTED candidate %q at the end\n--- screen ---\n%s", got, candidate, h.rows())
	}
	if strings.Contains(h.screenAtLine(), candidate+candidate) {
		t.Fatalf("the candidate was applied twice: %q\n--- screen ---\n%s", h.screenAtLine(), h.rows())
	}

	// 4) SUBMIT: the next Enter is the real submission — and the line the editor
	//    reports is the ACCEPTED CANDIDATE, not the typed prefix. This is the leg
	//    that makes the whole interaction "menu → navigate → submit".
	if res := h.inj.InjectKey(KeyNameEnter); !res.Accepted {
		t.Fatalf("InjectKey(enter) refused: %s", res.Why)
	}
	deadline := time.Now().Add(injectWait)
	var res Result
	for {
		got, ok := h.submitted()
		if ok {
			res = got
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the line never submitted after the second injected Enter\n--- screen ---\n%s", h.rows())
		}
		time.Sleep(injectPollStep) //stylegate:allow-sleep bounded poll (injectWait) for the read loop to return the submitted line
	}
	if strings.TrimSpace(res.Line) != candidate {
		t.Fatalf("the submitted line %q is not the candidate the Down key SELECTED (%q)\n--- screen ---\n%s", res.Line, candidate, h.rows())
	}
}

// TestInject_EnterAfterABurstDoesNotSubmit is the RED-PROOF case for the framing
// law, and it is the reason the injector paces its reads.
//
// It reproduces, at the seam, exactly what a naive injector does: queue three
// contiguous characters plus an Enter, so the editor reads them as ONE burst and
// the Enter is classified as pasted CONTENT (a newline in the buffer) instead of
// a submit. The assertion is that NOTHING was submitted — which is what makes
// the paced path in TestInject_MenuOpensWalksAndSubmitsOnTheGrid meaningful
// rather than accidental.
func TestInject_ContiguousKeysInOneWriteWouldBeABurst(t *testing.T) {
	// A SOURCE that hands the editor one whole burst in ONE read — the naive
	// injector's shape, kept as a NEGATIVE CONTROL. It is not the Injector.
	burst := &burstSource{src: &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}, data: []byte(injectRuneKey + "q" + string([]byte{crByte}))}
	t.Cleanup(burst.stop)
	out := &recordingWriter{}
	ed := NewChatEditor(burst, out, promptForTest, nil, nil, (&stubCompleter{}).complete)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", injectRows)
	}
	done := make(chan Result, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		res, err := ed.Run()
		if err != nil {
			done <- Result{Line: "ERR: " + err.Error()}
			return
		}
		done <- res
	}()
	// The burst is read as one chunk (three characters = PasteBurstMinChars, so
	// the burst is armed and the CR that follows it in the same unit is
	// suppressed as content).
	select {
	case res := <-done:
		t.Fatalf("the burst SUBMITTED %q — this negative control exists to show that one-write injection does submit here, so the assertion below is wrong", res.Line)
	case <-time.After(250 * time.Millisecond):
		// Correct: the Enter was pasted content, so the line never submitted.
	}
	burst.stop()
	select {
	case <-exited:
	case <-time.After(injectWait):
		t.Fatalf("the negative-control run did not return after its source was stopped")
	}
	ed.Close()
}

// burstSource hands the editor ONE pre-built byte run in a single read and then
// holds. It is the negative control for the framing law: the shape an injector
// must NOT have.
type burstSource struct {
	src  *scriptedSource
	data []byte
	once bool
}

// ReadEvent delivers the burst on the first call, then defers to the held inner
// source.
func (b *burstSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	if !b.once {
		b.once = true
		return Event{Kind: EventInput, Bytes: copy(buf, b.data)}, nil
	}
	return b.src.ReadEvent(buf, timeoutMS)
}

// Width delegates to the inner source.
func (b *burstSource) Width(fallback int) int { return b.src.Width(fallback) }

// Height delegates to the inner source.
func (b *burstSource) Height(fallback int) int { return b.src.Height(fallback) }

// stop releases the inner source.
func (b *burstSource) stop() { b.src.stopSource() }

// timeoutRecordingSource records the timeoutMS each read was given, and answers
// exactly what the test tells it to. It exists to pin the decorator's ONE
// contract with its inner source: THE CALLER'S TIMEOUT PASSES THROUGH UNCHANGED.
type timeoutRecordingSource struct {
	lastTimeout int
	reads       int
	// eofWhenUnbounded makes this source answer EOF only for an UNBOUNDED read —
	// the shape the chat harness's script source has (a bounded wait expires, an
	// unbounded one parks until a stop arrives and then reports EOF).
	eofWhenUnbounded bool
}

func (s *timeoutRecordingSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	s.lastTimeout = timeoutMS
	s.reads++
	if s.eofWhenUnbounded && timeoutMS < 0 {
		return Event{Kind: EventInput, EOF: true}, nil
	}
	return Event{Kind: EventNone}, nil
}

func (s *timeoutRecordingSource) Width(fallback int) int  { return fallback }
func (s *timeoutRecordingSource) Height(fallback int) int { return fallback }

// parkingSource models the shape injection has to survive on the real terminal:
// a read that is BLOCKED until someone notifies it (the terminal's own wake pipe,
// raw.go) and answers EventWake when it is. It is the source of the deterministic
// guard below, so the interleaving is CONSTRUCTED rather than sampled.
type parkingSource struct {
	woke     chan struct{}
	notified chan struct{}
	reads    chan int
}

func newParkingSource() *parkingSource {
	return &parkingSource{
		woke:     make(chan struct{}, 1),
		notified: make(chan struct{}, 1),
		reads:    make(chan int, 16),
	}
}

// Notify is the wake seam (Notifier): the terminal's pipe write, stand-in form.
func (s *parkingSource) Notify() {
	select {
	case s.notified <- struct{}{}:
	default:
	}
}
func (s *parkingSource) NotifyEnabled() bool     { return true }
func (s *parkingSource) Width(fallback int) int  { return fallback }
func (s *parkingSource) Height(fallback int) int { return fallback }

// ReadEvent BLOCKS until a notification arrives — an unbounded wait nothing on
// the script side can end, which is exactly the query state the chat REPL's
// read loop is in at the prompt (promptParkWaitMS bounds it in production).
func (s *parkingSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	s.reads <- timeoutMS
	select {
	case <-s.notified:
		return Event{Kind: EventWake}, nil
	case <-time.After(promptParkWaitMS):
		return Event{Kind: EventNone}, nil
	}
}

// TestInject_KeyQueuedInTheParkWakesTheReadLoopAndIsDelivered is the regression
// guard for the ONE window this feature can lose a key in, and it constructs that
// window instead of sampling it: the read loop has PARKED (its ReadEvent is
// blocked inside the inner source, and it has decided nothing about the queue),
// and the key is queued from another goroutine with no keystroke behind it.
//
// WHY THIS IS THE SHAPE THAT MATTERS. An injected key is delivered by whichever
// read happens NEXT — so if the woken read could come back as anything other than
// the queued key, the key would sit in the queue until the next REAL keystroke
// and a caller would see "accepted but never read" (measured once: 3 bytes sat
// unread for a full second before the wake seam was used). The two halves asserted
// here are the whole guarantee:
//
//  1. the park is BROKEN by the injection alone (no keystroke, no second wake);
//  2. the very next read returns the key's OWN BYTES, and the queue is empty after.
func TestInject_KeyQueuedInTheParkWakesTheReadLoopAndIsDelivered(t *testing.T) {
	inner := newParkingSource()
	inj := NewInjector(inner, injectTestCeiling)
	defer inj.Close()

	type readOut struct {
		ev  Event
		err error
	}
	// The loop's next read, started BEFORE the injection: this goroutine is the
	// one that is parked when the key arrives.
	first := make(chan readOut, 1)
	go func() {
		buf := make([]byte, editReadBuf)
		ev, err := inj.ReadEvent(buf, -1)
		first <- readOut{ev: ev, err: err}
	}()
	// Wait until the inner source is REALLY inside its blocking read, or the
	// injection would be racing the park rather than landing in it.
	select {
	case <-inner.reads:
	case <-time.After(injectWait):
		t.Fatalf("the read loop never reached its parked read")
	}

	res := inj.InjectKey(KeyNameCtrlC)
	if !res.Accepted {
		t.Fatalf("InjectKey(%s) was refused: %s", KeyNameCtrlC, res.Why)
	}

	// 1. The park broke on the injection alone.
	var out readOut
	select {
	case out = <-first:
	case <-time.After(injectWait):
		t.Fatalf("the parked read was not woken by the injection: a queued key that does not break the park waits for the next real keystroke")
	}
	if out.err != nil {
		t.Fatalf("the woken read returned %v", out.err)
	}
	if out.ev.Kind != EventWake {
		t.Fatalf("the woken read returned kind=%v, want the wake the injection delivered", out.ev.Kind)
	}

	// 2. The VERY NEXT read returns the key's own bytes — this is the read a
	// closeout depends on, and it is the one a lost key never reaches.
	nextBuf := make([]byte, editReadBuf)
	next, err := inj.ReadEvent(nextBuf, -1)
	if err != nil {
		t.Fatalf("the read after the park returned %v", err)
	}
	if next.Kind != EventInput {
		t.Fatalf("the read after the park returned kind=%v, want the injected key as input (the loop would have to read again — the read a lost key never reaches)", next.Kind)
	}
	if next.Bytes != 1 || nextBuf[0] != ctrlC {
		t.Fatalf("the delivered key was %v (bytes=%d), want the ctrl-c byte %#x", nextBuf[:next.Bytes], next.Bytes, ctrlC)
	}
	if pending := inj.Pending(); pending != 0 {
		t.Fatalf("%d byte(s) still queued after the read: the park's read did not deliver the whole key", pending)
	}
}

// promptParkWaitMS bounds the parking source's blocking read: long enough that a
// dropped wake is a TIMEOUT rather than a pass (the guard's deadline), short
// enough that a red run reports in seconds.
const promptParkWaitMS = 2 * time.Second

// TestInject_PassesTheCallersTimeoutThroughToTheInnerSource is the regression
// guard for a defect that cost a whole suite run: the injector's first design
// SLICED the inner read into fixed polls, which turned an unbounded read
// (timeoutMS < 0) into a sequence of bounded ones and rewrote the inner source's
// contract. The chat harness's script source serves a bounded wait by expiring
// immediately and an unbounded one by parking until its stop arrives, so the
// slicing made its stop path unreachable and the editor's read loop never
// returned ("the editor's read loop did not return: the caller's writes cannot be
// attributed"). A decorator may DELAY the caller's read; it may not change what
// the caller asked for.
func TestInject_PassesTheCallersTimeoutThroughToTheInnerSource(t *testing.T) {
	inner := &timeoutRecordingSource{}
	inj := NewInjector(inner, injectTestCeiling)
	buf := make([]byte, editReadBuf)

	// Nothing queued: the inner source is read with the caller's own timeout.
	for _, want := range []int{-1, 0, 250} {
		if _, err := inj.ReadEvent(buf, want); err != nil {
			t.Fatalf("ReadEvent(%d): %v", want, err)
		}
		if inner.lastTimeout != want {
			t.Fatalf("the inner source was read with timeoutMS=%d, want the caller's %d — the decorator rewrote the read's contract (a sliced read makes a stop-aware source's stop path unreachable)",
				inner.lastTimeout, want)
		}
	}

	// An UNBOUNDED caller's read still lets the inner source end it: the shape the
	// script sources' stop path has.
	eof := &timeoutRecordingSource{eofWhenUnbounded: true}
	injEOF := NewInjector(eof, injectTestCeiling)
	ev, err := injEOF.ReadEvent(buf, -1)
	if err != nil {
		t.Fatalf("ReadEvent(-1): %v", err)
	}
	if !ev.EOF {
		t.Fatalf("an unbounded read did not reach the inner source's EOF (kind=%v eof=%v): the stop path is unreachable behind the injector", ev.Kind, ev.EOF)
	}
}

// notifierSource implements Notifier DIRECTLY — the spelling the chat harness's
// script source uses, and the one the first version of the injector failed to
// forward.
type notifierSource struct {
	notified int
	enabled  bool
}

func (s *notifierSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	return Event{Kind: EventNone}, nil
}
func (s *notifierSource) Width(fallback int) int  { return fallback }
func (s *notifierSource) Height(fallback int) int { return fallback }
func (s *notifierSource) Notify()                 { s.notified++ }
func (s *notifierSource) NotifyEnabled() bool     { return s.enabled }

// wakeAccessorSource carries its notifier behind WakeNotifier() — the OTHER
// spelling (a real terminal does this).
type wakeAccessorSource struct {
	sink *notifierSource
}

func (s *wakeAccessorSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	return Event{Kind: EventNone}, nil
}
func (s *wakeAccessorSource) Width(fallback int) int  { return fallback }
func (s *wakeAccessorSource) Height(fallback int) int { return fallback }
func (s *wakeAccessorSource) WakeNotifier() Notifier  { return s.sink }

// TestInject_ForwardsTheWakeSeamInBOTHSpellings is the second defect's guard. The
// editor resolves its wake seam through ONE function (SourceNotifier) precisely
// because a source may carry its notifier either as the Notifier interface
// itself or behind a WakeNotifier() accessor. A decorator that checked only the
// accessor silently downgraded the first spelling: the status row stopped
// refreshing and the live-hint case failed with "the harness's source supplied no
// wake seam".
func TestInject_ForwardsTheWakeSeamInBOTHSpellings(t *testing.T) {
	// Spelling 1: the source IS a Notifier.
	direct := &notifierSource{enabled: true}
	got := NewInjector(direct, injectTestCeiling).WakeNotifier()
	if got == nil || !got.NotifyEnabled() {
		t.Fatalf("the injector did not forward a source that implements Notifier directly: %T (a publish would never reach a blocked read)", got)
	}
	got.Notify()
	if direct.notified != 1 {
		t.Fatalf("Notify reached the inner source %d time(s), want 1", direct.notified)
	}

	// Spelling 2: the source carries it behind WakeNotifier().
	sink := &notifierSource{enabled: true}
	viaAccessor := NewInjector(&wakeAccessorSource{sink: sink}, injectTestCeiling).WakeNotifier()
	if viaAccessor == nil || !viaAccessor.NotifyEnabled() {
		t.Fatalf("the injector did not forward a source whose notifier is behind WakeNotifier(): %T", viaAccessor)
	}
	viaAccessor.Notify()
	if sink.notified != 1 {
		t.Fatalf("Notify reached the accessor's sink %d time(s), want 1", sink.notified)
	}

	// A source with no seam at all answers with the no-op sink, which SAYS SO
	// (Rule 15: the degradation is announced, not silent).
	bare := NewInjector(&timeoutRecordingSource{}, injectTestCeiling).WakeNotifier()
	if bare == nil {
		t.Fatal("a source with no seam produced a nil notifier")
	}
	if bare.NotifyEnabled() {
		t.Fatal("a source with no seam reported an enabled notifier")
	}
}
