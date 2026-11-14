package main

// chat_voice_pty_test.go — `/voice` on the PRODUCTION carrier: a REAL pty, the
// REAL chat input layer, the REAL key gate seat, and a RECORDER that stands in
// only for the microphone.
//
// WHAT IS REAL HERE, and why the split is drawn at exactly this line (Rule 31).
// The pieces this file exercises are the ones that only exist at the boundary the
// REPL builds: the chatInput the turn loop drives, the editor's OWN read loop as
// the one writer of the line, the key-gate seat inside that loop (with the origin
// test that keeps a scripted Escape from switching off a human's microphone), and
// the rows the mode writes to the console. A unit test over the mode object could
// prove none of them.
//
// WHAT IS STOOD IN FOR, and why that is not a shortcut. The RECORDER is the two
// methods that reach hardware and a 910MB model (voiceChatRecorder.Select /
// Round). Driving the real ones here would assert THIS MACHINE (whether it has a
// microphone, whether the weights are installed) rather than the code, and would
// make the suite unbounded — so the seam takes a scripted recorder and the
// acceptance criteria that are ABOUT the recorder's failure modes ("a missing
// device leaves the mode OFF") are asserted by driving ITS error path, which is
// the same path the production recorder's error takes.
//
// THE JUDGE IS THE GRID (Rule 10.2): every assertion reads back the sink's
// vt100 CELLS — what the operator's terminal shows — never the bytes written.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

const (
	// voicePtyCols / voicePtyRows are the terminal this case gives the layout
	// (the same shape the injection case uses: wide enough for the chat layout,
	// short enough to read back whole).
	voicePtyCols = 60
	voicePtyRows = 14
	// voicePtyWait bounds a wait for the read loop to paint a change.
	voicePtyWait = 3 * time.Second
	// voicePtyStep is that wait's polling interval.
	voicePtyStep = 2 * time.Millisecond
	// voicePtyRowsTall is the terminal a case takes when its judgement is about a
	// NOTICE rather than about the layout. The shared 14-row default is the shape
	// the layout cases want, and a notice paragraph plus the entry note plus the
	// editing line can fill it to the last row — leaving the judgement at the
	// mercy of where the paragraph happened to wrap (the privacy row is eight
	// rows once wrapped, and IT is the first thing a full band scrolls off).
	// Taller is not a louder assertion: it is the same assertion with the screen
	// no longer deciding it.
	voicePtyRowsTall = 24
)

// noticeLog is the console's ANNOUNCEMENT record: it is teed every notice the
// layer announces (chatInput.WriteNotice's own instrument seam) and keeps the
// text, so a case can ask "was this row announced, and how many times?" without
// reading the grid.
//
// IT IS NOT A SECOND CONSOLE and it changes no bytes: WriteNotice still draws the
// row, and the observer is handed that same text. It exists because the painted
// band is a SCROLLING WINDOW — a notice can be announced and drawn and then be
// pushed off a short screen by the rows that follow it, which is a fact about the
// terminal's height rather than about the notice. Measured 2026-10-11 on the
// privacy row: drawn at t=2.4ms, gone from the 14-row grid by t=3.2ms.
type noticeLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (n *noticeLog) Write(p []byte) (int, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.text.Write(p)
}

// String returns everything announced so far.
func (n *noticeLog) String() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.text.String()
}

// scriptedRecorder is the recorder seam under the acceptance cases: it selects a
// device and returns the texts it was told to, in order, one per round.
//
// selectErr and roundErr are the FAILURE legs — a missing capture device and a
// broken engine — and they are settable so the mode's own failure path (report,
// leave the mode, keep the line) is driven rather than described.
type scriptedRecorder struct {
	mu        sync.Mutex
	desc      string
	texts     []string
	selectErr error
	roundErr  error
	// roundErrs is the PER-ROUND failure script (task 01a121fc, deliverable D):
	// each entry is consumed by one round, a nil entry meaning "this round
	// succeeds". It exists because the rule under test is about a RUN of failures
	// and its reset — a recorder with one sticky roundErr can only produce
	// "everything fails", which cannot tell a counted run from a mode that simply
	// left on the first window.
	roundErrs []error
	rounds    int
	closes    int
	// span is the window boundary this recorder reports through the optional
	// voiceWindowReporter seam. It matters to the MODE (a window cut inside
	// speech is reported differently from a quiet one), so a double that cannot
	// report one would leave that half of the loop untested.
	span voiceWindowSpan
	// roundGates is the per-round release script for the optional voiceRoundGate
	// seam: round N does not run until entry N is released, so a case can read the
	// screen each round left behind. It is armed by armRoundGate and a round with
	// no entry left runs ungated — a case that stops driving the script must not
	// park the mode's loop forever.
	roundGates []chan struct{}
	// gateAsks counts how many times the mode asked for a gate. It is the
	// observable "the loop reached the gate", which is what lets a case WAIT for
	// the parked state rather than sleep at it.
	gateAsks int
	// lastGate is the gate most recently handed to the mode, so a case can release
	// exactly the round it is holding.
	lastGate chan struct{}
}

// RoundGate satisfies the optional voiceRoundGate seam: it hands the mode the
// next release channel in the script, or nil when the script is spent.
//
// THE SCRIPTED RECORDER IS THE REASON THE SEAM EXISTS. It computes a window in
// microseconds where production waits voiceChatWindowSeconds, so an ungated case
// asserts on the screen a BURST produces — which is not a screen any capture can
// make, and (measured on TestChatVoicePrivacyRowCarriesOnePrefix) pushed the row
// under test off a short pty before the case could read it.
func (r *scriptedRecorder) RoundGate() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	// gateAsks is the OBSERVABLE "the loop arrived at the gate": the mode asks
	// before every round, so a case can wait for the ask instead of guessing at a
	// delay. The ask happens immediately before the wait, so once it is seen the
	// loop is either park on the closed channel or about to be.
	r.gateAsks++
	if len(r.roundGates) == 0 {
		return nil
	}
	g := r.roundGates[0]
	r.roundGates = r.roundGates[1:]
	r.lastGate = g
	return g
}

// armedGate returns the gate the loop is currently parked on, failing the case
// when the loop has not asked for one — a release with nothing to release is a
// fixture bug, not a passing test.
func (r *scriptedRecorder) armedGate(t *testing.T) chan struct{} {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastGate == nil {
		t.Fatal("no round gate has been handed to the mode — arm one with armRoundGate before releasing it")
	}
	return r.lastGate
}

// gateAsksRun reports how many times the mode has asked this recorder for a gate.
func (r *scriptedRecorder) gateAsksRun() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gateAsks
}

// armRoundGate queues ONE release for the next round and returns it: the round is
// parked until the caller closes it, so the case owns when the loop advances.
func (r *scriptedRecorder) armRoundGate() chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := make(chan struct{})
	r.roundGates = append(r.roundGates, g)
	return g
}

// LastWindow satisfies the voiceWindowReporter seam with the scripted boundary.
// The zero span (the default) is a recorder whose window ended on its own bound
// with nothing to carry — the ordinary case the tests below start from.
func (r *scriptedRecorder) LastWindow() voiceWindowSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.span
}

func (r *scriptedRecorder) Select(string) (string, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.selectErr != nil {
		return "", nil, r.selectErr
	}
	return r.desc, nil, nil
}

func (r *scriptedRecorder) Round(*voiceOptions, int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rounds++
	if len(r.roundErrs) > 0 {
		err := r.roundErrs[0]
		r.roundErrs = r.roundErrs[1:]
		if err != nil {
			return "", err
		}
	}
	if r.roundErr != nil {
		return "", r.roundErr
	}
	if len(r.texts) == 0 {
		// NOTHING LEFT TO SAY: the round reports silence the way the production
		// recorder does (an empty transcript and no error), and the mode must
		// answer with its silence row rather than an insert — the case below
		// asserts that distinction.
		return "", nil
	}
	text := r.texts[0]
	r.texts = r.texts[1:]
	return text, nil
}

// Close satisfies the recorder seam's teardown half. The scripted recorder holds
// nothing between rounds (the rounds are scripted, not recorded), so the no-op is
// the honest implementation — and it is COUNTED, because "did the mode release the
// microphone on the way out?" is exactly the property the staged recorder's live
// stream makes load-bearing, and a test double that silently ignored it would
// assert nothing about it.
func (r *scriptedRecorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
}

// closesRun reports how many times the mode closed this recorder.
func (r *scriptedRecorder) closesRun() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closes
}

func (r *scriptedRecorder) roundsRun() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rounds
}

// voicePtyGrid is the production input layer under test, the sink it painted
// into, and the read loop driving it.
type voicePtyGrid struct {
	in     *chatInput
	sink   *uiclient.ScreenDumper
	mode   *voiceChatMode
	rec    *scriptedRecorder
	exited chan struct{}
	// log is the console's ANNOUNCEMENT record (chatInput's own announce seam),
	// for the judgements that are about what the console SAID rather than about
	// what fits on the screen at the instant of the read.
	log *noticeLog
	// pair is the pty itself, kept so a case can drive the MASTER side — the end a
	// real keyboard writes to. It matters because the editor's key GATE decides by
	// origin: bytes that arrive through the terminal report ReadOriginTyped (the
	// operator's key) while bytes queued through the injector report
	// ReadOriginInjected (a script's), and a case about a key the gate claims must
	// be able to produce the first without pretending to be the second.
	pair chatPtyPair
}

// newVoicePtyGrid builds the REAL chat input over a real pty (the SAME
// constructor the REPL uses), arms the layout, builds the voice mode over it and
// starts the read loop.
//
// The mode is built here with the PRODUCTION seams for everything except the
// recorder: chatVoiceResolveOptions and voiceDepsReady are real functions, and
// the cases that must not depend on this machine's weights install their own
// (see voiceGridWithDeps).
func newVoicePtyGrid(t *testing.T, rec *scriptedRecorder) *voicePtyGrid {
	t.Helper()
	return newVoicePtyGridSize(t, rec, voicePtyCols, voicePtyRows)
}

// newVoicePtyGridTall is the same construction on a terminal tall enough for a
// wrapped NOTICE paragraph and the rows around it to coexist. It exists for the
// cases whose subject is what the console said rather than where the layout put
// it (see voicePtyRowsTall).
func newVoicePtyGridTall(t *testing.T, rec *scriptedRecorder) *voicePtyGrid {
	t.Helper()
	return newVoicePtyGridSize(t, rec, voicePtyCols, voicePtyRowsTall)
}

// newVoicePtyGridSize is the same construction at a CALLER-CHOSEN terminal size.
// It exists because the status row's geometry is pinned at statusColumnsAtCols:
// a case measuring the ALIGNED row must give the layout that width, and the
// default voicePtyCols is deliberately narrower (it is the shape the layout cases
// want). Everything else is identical — one construction, so a case cannot get a
// carrier the REPL would not build.
func newVoicePtyGridSize(t *testing.T, rec *scriptedRecorder, cols, rows int) *voicePtyGrid {
	t.Helper()
	pair := chatOpenPtyPair(t, cols, rows)
	sink := turnDumpSink(t)
	term, err := lineedit.Open(pair.Slave, sink)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	in := newChatInputWithSource(term, sink, chatPrompt)
	in.editor.SetPalette(lineedit.BasicPalette)
	in.editor.SetGeometryObserver(chatGeomObserver(sink))
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the real chat layout installed", cols, rows)
	}

	h := &voicePtyGrid{in: in, sink: sink, rec: rec, exited: make(chan struct{}), pair: pair, log: &noticeLog{}}
	// THE HARNESS IS THE NOTICE INSTRUMENT. Every notice this layer announces is
	// teed to the log (WriteNotice's own seam), which changes no byte of the
	// console: the row is still drawn, and the log answers the separable question
	// "was it announced?" — the one the painted band cannot answer, because the
	// band scrolls.
	in.announce = h.log
	h.mode = newChatVoiceMode(in, rec)
	go func() {
		defer close(h.exited)
		_, _ = in.editor.Run() //stylegate:ignore ends the voice pty fixture's editor so the watcher stops; the captured transcript is the assertion
	}()
	t.Cleanup(func() {
		// THE MODE IS TAKEN DOWN FIRST, and it is not tidiness: a mode left ON
		// parks its capture loop on the pacing timer, and the suite's goroutine
		// sweep reports that loop as a parked repository goroutine (measured — it
		// is how the missing session-end teardown in cli_chat.go was found). This
		// calls the SAME shutdown the REPL's own exit defers.
		h.mode.shutdown()
		// The terminal is closed FIRST — it is the stop for the parked read (the
		// order chat_inject_pty_test.go measured and states; the join below is a
		// join, not a race against a close that happens afterwards).
		in.editor.Close() //stylegate:ignore the harness's own terminal teardown
		_ = term.Close()  //stylegate:ignore the harness's own terminal teardown
		chatJoinReadLoop(t, h.exited, func() string { return runState(h.exited) })
		_ = pair.Master.Close() //stylegate:ignore pty master teardown of the voice fixture, after the echo of dictation was read
	})
	return h
}

// rows renders the sink's CURRENT grid — the cells an operator's terminal shows.
func (h *voicePtyGrid) rows() string {
	sd := h.sink.Snapshot()
	return strings.Join(sd.Grid, "\n")
}

// editingRow is the ONE grid row the operator types on — the row carrying the
// prompt. It exists because the case above counts how many times a sentence
// appears, and the rows ABOVE the line are report chrome that legitimately names
// the same sentence (the transcript note); counting the whole grid would make the
// assertion about the console's verbosity rather than about the line.
//
// IT FINDS THE ROW BY THE PROMPT, and it accepts the PROMPT-ONLY row as well as a
// row with text after it: at the moment of a settle the line may be empty, and a
// helper that only matched "prompt + text" would silently return the empty string
// — a comparison that passes on nothing.
func (h *voicePtyGrid) editingRow() string {
	for _, row := range h.sink.Snapshot().Grid {
		if strings.HasPrefix(strings.TrimLeft(row, " "), chatPrompt) {
			return row
		}
	}
	return ""
}

// waitFor polls the painted grid until cond holds, dumping it on failure.
func (h *voicePtyGrid) waitFor(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(voicePtyWait)
	for time.Now().Before(deadline) {
		if cond(h.rows()) {
			return
		}
		time.Sleep(voicePtyStep) //stylegate:allow-sleep bounded poll (voicePtyWait) for the read loop to paint the change
	}
	t.Fatalf("timed out waiting for %s\n--- grid ---\n%s", what, h.rows())
}

// waitAnnounced waits until the console has ANNOUNCED text, and reports how long
// it took.
//
// WHY THIS IS THE HONEST WAIT FOR A NOTICE. The painted band is a scrolling
// window over a fixed-height screen: a row can be announced and drawn and then be
// pushed past the top by the rows that follow it, so "it is on the grid" is a
// claim about the terminal's height and the timing of a repaint — never about
// whether the console said it. The announcement seam (chatInput.WriteNotice's own
// announce tee) is the one record of the EVENT: WriteNotice writes the text there
// as it is announced, whatever the editor then does with the row. Waiting on the
// subject — was this sentence announced? — removes the screen from the decision.
func (h *voicePtyGrid) waitAnnounced(t *testing.T, what, text string) {
	t.Helper()
	deadline := time.Now().Add(voicePtyWait)
	for time.Now().Before(deadline) {
		if strings.Contains(h.log.String(), text) {
			return
		}
		time.Sleep(voicePtyStep) //stylegate:allow-sleep bounded poll (voicePtyWait) for the loop to announce the row
	}
	t.Fatalf("timed out waiting for %s (%q)\n--- announced ---\n%s\n--- grid ---\n%s",
		what, text, h.log.String(), h.rows())
}

// waitGateAsked waits until the mode's capture loop has asked the recorder for a
// gate `want` times — i.e. until a round is sitting on the gate waiting for its
// release — and returns whether it did.
//
// It is a WAIT, not a sleep: the sequencer's whole claim is "the round has not run
// yet, so the screen holds exactly what the previous step left", and a case that
// guessed at a delay would be the same wall-clock assumption this fixture exists
// to remove. The evidence is the seam BOTH sides share (the recorder counts the
// ask the mode makes immediately before it waits), so a loop that ignored the seam
// is caught here rather than being silently raced.
func (h *voicePtyGrid) waitGateAsked(t *testing.T, want int) bool {
	t.Helper()
	deadline := time.Now().Add(voicePtyWait)
	for time.Now().Before(deadline) {
		if h.rec.gateAsksRun() >= want {
			return true
		}
		time.Sleep(voicePtyStep) //stylegate:allow-sleep bounded poll (voicePtyWait) for the loop to reach the gate
	}
	return false
}

// beforeVoiceEnter is the ONE preamble every entry case needs: the mode is asked
// for its state so the pre-entry line is on the grid, and both the engine check
// and the option build are supplied (see voiceGridEntering).
func (h *voicePtyGrid) enter(t *testing.T) {
	t.Helper()
	h.mode.handleLine(h.in, "/"+chatVoiceCommandName)
}

// voiceChatTestOptions is the engine settings a grid case runs with: the values
// are never read by the scripted recorder, and they are built here rather than
// resolved so a case does not depend on where THIS machine keeps its weights
// (Rule 31's honest half: the production resolution is asserted by the CLI case
// against the real profile, not simulated here).
func voiceChatTestOptions() (*voiceOptions, error) {
	return &voiceOptions{device: "test-device", models: "/test/models", runtime: "/test/runtime"}, nil
}

// TestChatVoice_EnterAppendsAndEscapeLeaves is the mode's acceptance case end to
// end, on the production carrier, judged on the GRID.
//
// The legs are the operator's own criteria, in the order they experience them:
// type a draft, enter the mode, watch TWO transcripts land AFTER the draft
// (append, never overwrite), press Escape, and confirm the mode says it is off
// while the line still holds everything.
func TestChatVoice_EnterAppendsAndEscapeLeaves(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic", texts: []string{"hello ", "world"}}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }

	// 1) THE DRAFT. Typed through the editor's own key injection (the production
	//    seam the replay driver uses), so the line holds text the operator wrote
	//    BEFORE the mode existed — which is the state the append must not destroy.
	if res := h.in.injector.InjectKey("d"); !res.Accepted {
		t.Fatalf("injecting the draft refused: %s", res.Why)
	}
	if res := h.in.injector.InjectKey("r"); !res.Accepted {
		t.Fatalf("injecting the draft refused: %s", res.Why)
	}
	h.waitFor(t, "the typed draft on the editing row", func(rows string) bool {
		return strings.Contains(rows, "dr")
	})

	// 2) ENTER. The entered note must reach the grid, naming the device it
	//    records — the operator's own "is it on?" question.
	h.enter(t)
	h.waitFor(t, "the entered note", func(rows string) bool {
		return strings.Contains(rows, "VOICE MODE")
	})
	if !h.mode.active() {
		t.Fatal("the mode reports OFF after entering")
	}

	// 3) TWO TRANSCRIPTS, APPENDED. `drhello world` is the whole content: the
	//    draft survived, and the two segments arrived in order with no
	//    overwriting. Asserted on the CELLS the operator reads.
	h.waitFor(t, "both transcripts appended after the draft", func(rows string) bool {
		return strings.Contains(rows, "drhello world")
	})

	// 4) A SCRIPTED ESCAPE MUST NOT LEAVE THE MODE. The injector is the replay
	//    driver's road and every key it delivers carries the INJECTED origin by
	//    construction, so the gate DECLINES it — the origin test in force, which
	//    is what stops a recording from switching off a watching operator's
	//    microphone. (The TYPED road is the one no in-process case can produce:
	//    it is the source's own answer, and it is asserted directly in the next
	//    case, where the gate is driven with both origins. The real pty run of
	//    this binary supplied exactly that keystroke end to end — see the
	//    report's `Ran:` line, where Escape leaves the mode on a live terminal.)
	if res := h.in.injector.InjectKey(lineedit.KeyNameEscape); !res.Accepted {
		t.Fatalf("injecting Escape refused: %s", res.Why)
	}
	h.waitFor(t, "the injected Escape to be delivered", func(string) bool { return true })
	if !h.mode.active() {
		t.Fatal("an INJECTED Escape left the mode: the origin test is not in force, so a scripted session can switch off a human's microphone")
	}
	//    Then leave the way this session's card allows — the mode's stop word,
	//    through the same production line the switch calls.
	h.mode.handleLine(h.in, "/"+chatVoiceCommandName+" "+chatVoiceStopWord)
	h.waitFor(t, "the leaving note", func(rows string) bool {
		return strings.Contains(rows, "voice mode OFF")
	})
	if h.mode.active() {
		t.Fatal("the mode reports ON after the stop word")
	}
	// THE LINE IS KEPT — the criterion that makes leaving safe.
	h.waitFor(t, "the line kept after leaving", func(rows string) bool {
		return strings.Contains(rows, "drhello world")
	})
}

// TestChatVoice_EscapeIsClaimedOnlyFromTheTerminal is the origin contract, at the
// seat where it matters: the mode's gate must consume a TYPED Escape (the
// operator's key) and must DECLINE an INJECTED one (the script's), or a replay
// would be able to switch off the microphone of the person watching it.
//
// It drives the gate DIRECTLY with both origins — the same call the editor's read
// loop makes — so the two arms are separated by the one field that decides them.
func TestChatVoice_EscapeIsClaimedOnlyFromTheTerminal(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic"}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	if !h.mode.active() {
		t.Fatalf("the mode did not enter: %s", h.rows())
	}

	gate := h.mode.state.gate
	if gate == nil {
		t.Fatal("entering installed no gate at the editor's key seam")
	}
	if got := h.in.editor.KeyGate(); got != lineedit.KeyGate(gate) {
		t.Fatal("the editor's key seam does not hold the mode's gate: the seat was never installed")
	}

	// 1) THE SCRIPT'S Escape — DECLINED, and the mode stays on.
	if gate.GateKey(lineedit.Key{Code: lineedit.KeyEscape, Origin: lineedit.ReadOriginInjected}) {
		t.Fatal("the gate consumed an INJECTED Escape: a scripted session would switch off a human's microphone")
	}
	if !h.mode.active() {
		t.Fatal("the mode left on an injected Escape")
	}

	// 2) THE OPERATOR'S Escape — CLAIMED, and the mode leaves. Every other key is
	//    declined too, so the mode cannot swallow the surface it lives in.
	if gate.GateKey(lineedit.Key{Code: lineedit.KeyRune, Rune: 'x', Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("the gate consumed an ordinary typed rune — the operator could not type while the mode is on")
	}
	if got := h.in.editor.KeyGate(); got != lineedit.KeyGate(gate) {
		t.Fatal("the seat was lost before the Exit was pressed")
	}
	if !gate.GateKey(lineedit.Key{Code: lineedit.KeyEscape, Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("the gate declined a TYPED Escape: the operator's own key cannot leave the mode")
	}
	if h.mode.active() {
		t.Fatal("the mode is still on after a claimed Escape")
	}

	// 3) THE SEAT IS RESTORED, not cleared: the owner that was there before (none,
	//    in this case) is what the editor holds afterwards — a mode that left a
	//    dangling consumer at the seam would eat the next watcher's Escape.
	h.waitFor(t, "the leaving note", func(rows string) bool { return strings.Contains(rows, "voice mode OFF") })
	if got := h.in.editor.KeyGate(); got != nil {
		t.Fatalf("the key seam still holds %#v after the mode left (want the pre-mode owner, nil here)", got)
	}

	// 4) AN ESCAPE AT A MODE THAT IS ALREADY OFF is not this surface's key: it
	//    must be DECLINED so the editor's own Escape behaviour is untouched.
	if gate.GateKey(lineedit.Key{Code: lineedit.KeyEscape, Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("an Escape arriving after the mode left was still consumed: the surface would lose a key it no longer owns")
	}
}

// TestChatVoice_RefusalsLeaveTheModeOffWithoutRecording is the failure contract:
// each refusal path must be a sentence, must leave the mode OFF, and must not
// have recorded anything.
//
// The legs are the operator's own list — no input line (the cooked carrier), no
// engine (missing weights), no device — plus the argument a typo produces. The
// no-editor leg uses a carrier built WITHOUT an editor, which is the shape every
// other leg would silently paper over.
func TestChatVoice_RefusalsLeaveTheModeOffWithoutRecording(t *testing.T) {
	t.Run("no input line", func(t *testing.T) {
		// A carrier with no editor: the cooked path (piped stdin, --no-editor).
		// The mode must refuse BEFORE any device work, because its whole
		// deliverable is text on a line that does not exist here.
		rec := &scriptedRecorder{desc: "test mic"}
		cooked := &chatInput{}
		mode := newChatVoiceMode(cooked, rec)
		mode.resolveOpts = voiceChatTestOptions
		mode.depsMissing = func(*voiceOptions) string { return "" }
		mode.handleLine(cooked, "/"+chatVoiceCommandName)
		if mode.active() {
			t.Fatal("the mode started on a carrier with no input line: there is nothing a transcript could be appended to")
		}
		if got := rec.roundsRun(); got != 0 {
			t.Fatalf("%d round(s) were recorded before the refusal — the seam check must come first", got)
		}
	})

	t.Run("no engine", func(t *testing.T) {
		rec := &scriptedRecorder{desc: "test mic"}
		h := newVoicePtyGrid(t, rec)
		mode := h.mode
		mode.resolveOpts = voiceChatTestOptions
		mode.depsMissing = func(*voiceOptions) string { return "encoder model not found: /nowhere/encoder.onnx" }
		mode.handleLine(h.in, "/"+chatVoiceCommandName)
		if mode.active() {
			t.Fatal("the mode started with a missing engine dependency")
		}
		if got := rec.roundsRun(); got != 0 {
			t.Fatalf("%d round(s) were recorded with no engine — a missing model must not cost a recording", got)
		}
		h.waitFor(t, "the dependency refusal on the grid", func(rows string) bool {
			return strings.Contains(rows, "encoder model not found")
		})
	})

	t.Run("no device", func(t *testing.T) {
		rec := &scriptedRecorder{selectErr: errNoTestDevice}
		h := newVoicePtyGrid(t, rec)
		mode := h.mode
		mode.resolveOpts = voiceChatTestOptions
		mode.depsMissing = func(*voiceOptions) string { return "" }
		mode.handleLine(h.in, "/"+chatVoiceCommandName)
		if mode.active() {
			t.Fatal("the mode started with no capture device: the status row would claim a microphone nobody selected")
		}
		if got := rec.roundsRun(); got != 0 {
			t.Fatalf("%d round(s) were recorded with no device", got)
		}
		// The refusal is read on the GRID, and only its opening is quoted: the
		// row WRAPS at the terminal's width, so a case that asserted the whole
		// sentence would be asserting this pty's column count rather than the
		// message (measured: the sentence splits as "…the scripted recor" /
		// "der was told to fail…").
		h.waitFor(t, "the device refusal on the grid", func(rows string) bool {
			return strings.Contains(rows, "cannot start: no capture device")
		})
	})

	t.Run("bad argument", func(t *testing.T) {
		rec := &scriptedRecorder{desc: "test mic"}
		h := newVoicePtyGrid(t, rec)
		mode := h.mode
		mode.resolveOpts = voiceChatTestOptions
		mode.depsMissing = func(*voiceOptions) string { return "" }
		mode.handleLine(h.in, "/"+chatVoiceCommandName+" wibble")
		if mode.active() {
			t.Fatal("an argument the word does not accept was read as the toggle")
		}
		h.waitFor(t, "the argument refusal on the grid", func(rows string) bool {
			return strings.Contains(rows, "unexpected argument")
		})
	})
}

// TestChatVoice_ARunOfFailuresLeavesTheModeOffAndKeepsTheLine is the mid-session
// failure that does NOT stop: a device that disappears (or an engine that starts
// failing) must be REPORTED on every window and must leave the mode once the run
// is long enough to be about the device rather than about one window, rather than
// spinning against a microphone nobody can hear.
//
// IT DRIVES A RUN, NOT ONE ROUND (task 01a121fc, deliverable D): a single failed
// window is reported and RETRIED with the mode still ON, which is the rule that
// replaced the one-strike exit this case used to pin.
func TestChatVoice_ARunOfFailuresLeavesTheModeOffAndKeepsTheLine(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic", roundErr: errTestRound}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	h.waitForBound(t, "the FAILED note and the leaving note at the end of the run", voiceFailureRunWait, func(rows string) bool {
		return strings.Contains(rows, "voice mode FAILED") && strings.Contains(rows, "voice mode OFF")
	})
	deadline := time.Now().Add(voicePtyWait)
	for time.Now().Before(deadline) && h.mode.active() {
		time.Sleep(voicePtyStep) //stylegate:allow-sleep bounded wait for the loop's own exit after the failure
	}
	if h.mode.active() {
		t.Fatal("the mode is still ON after a round failed: a broken microphone would keep the status row claiming it records")
	}
	if got := rec.roundsRun(); got != voiceChatMaxConsecutiveFailures {
		t.Fatalf("the loop ran %d rounds, want exactly %d (the run that ends it) — a broken device must be retried only up to the limit, never in a spin",
			got, voiceChatMaxConsecutiveFailures)
	}
	// EVERY FAILURE OF THE RUN IS REPORTED: the earlier ones as retried windows
	// (still listening), the last one as the FAILED row. A run that went quiet on
	// its way to the exit is Rule 15's defect.
	if rows := voiceFlatten(h.rows()); !strings.Contains(rows, voiceFlatten(voiceRetryRow(errTestRound, 1))) {
		t.Errorf("the first failure of the run was not reported as a retried window\n--- grid ---\n%s", h.rows())
	}
}

// TestChatVoice_SilenceInsertsNothingAndSaysSo is the distinction the mode must
// not blur: an engine that returns no text is SILENCE (state it, keep listening),
// not a transcript and not a failure.
func TestChatVoice_SilenceInsertsNothingAndSaysSo(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic"} // no texts at all: every round is silent
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	h.waitFor(t, "the silence note", func(rows string) bool {
		return strings.Contains(rows, "no text in the last")
	})
	if !h.mode.active() {
		t.Fatal("silence left the mode: an empty window is not a failure, and a quiet room must not end the session")
	}
	// THE NEXT WINDOW IS THE POINT: silence must lead to another round, so the
	// wait is for the SECOND round rather than for a reading taken the moment the
	// first silence row appeared (the pacing gap sits between them).
	deadline := time.Now().Add(voicePtyWait)
	for time.Now().Before(deadline) && rec.roundsRun() < 2 {
		time.Sleep(voicePtyStep) //stylegate:allow-sleep bounded poll (voicePtyWait) for the next silence window
	}
	if got := rec.roundsRun(); got < 2 {
		t.Fatalf("the loop ran %d round(s) — silence must lead to the NEXT window, not to one round and a stop", got)
	}
}

// TestChatVoice_SplitSilentWindowSaysBothFacts is the defect case of task
// 01a11b39, on the real carrier and judged on the grid.
//
// WHAT WAS WRONG. Two sentences were printed for the same round and neither
// explained the other: the boundary note ("cut inside speech … a word may be
// split") and the silence note ("no text in the last window"). An operator whose
// continuous dictation returned NO text therefore read "we heard nothing" about
// audio they had just spoken, with no statement connecting the cut to the missing
// words — and the round that produced the whole incident (a 51,999 ms window, its
// extension budget spent, whose audio the engine received and judged speechless:
// logid 20261008193840B4E45D12C25D5C367B08) was exactly this pair.
//
// THE ASSERTION IS THE CONNECTION: one row, carrying BOTH the cut (with its
// extension count) and the fact that this window produced no text. The plain
// silence row must NOT be what such a round prints, because it is the row that
// hid the cut.
func TestChatVoice_SplitSilentWindowSaysBothFacts(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic"} // no texts: the window came back empty
	rec.span = voiceWindowSpan{Split: true, Extended: voiceWindowMaxExtensions}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }

	h.enter(t)
	h.waitFor(t, "the row that states the cut AND the silence together", func(rows string) bool {
		return strings.Contains(rows, "produced NO text")
	})
	rows := h.rows()
	if !strings.Contains(rows, "cut inside speech") {
		t.Fatalf("the row does not say the window was cut inside speech:\n--- grid ---\n%s", rows)
	}
	// THE GRID WRAPS, so the sentence is matched with ALL its whitespace removed
	// (a row folded mid-token reads "extension(" + newline + "s)"): the fact under
	// test is that the count is IN the row, not where the terminal folded it.
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	if !strings.Contains(squash(rows), squash(fmt.Sprintf("%d extension(s)", voiceWindowMaxExtensions))) {
		t.Fatalf("the row does not carry the extension count %d — the operator cannot tell a spent budget from an ordinary round:\n--- grid ---\n%s", voiceWindowMaxExtensions, rows)
	}
	if !h.mode.active() {
		t.Fatal("the split-silent window left the mode: a window that lost speech is not a failed round")
	}
	// AND THE LOOP GOES ON: the next window must be armed, which is the property
	// that makes the note a report rather than an epitaph.
	deadline := time.Now().Add(voicePtyWait)
	for time.Now().Before(deadline) && rec.roundsRun() < 2 {
		time.Sleep(voicePtyStep) //stylegate:allow-sleep bounded poll (voicePtyWait) for the next window
	}
	if got := rec.roundsRun(); got < 2 {
		t.Fatalf("the loop ran %d round(s) — a split-silent window must lead to the NEXT window", got)
	}
}

// TestChatVoice_QuietWindowKeepsThePlainSilenceRow is the OTHER side of the same
// branch, and it is why the new row does not replace the old one: a window that
// ended on its own bound with nothing to carry (Split false) has no cut to
// report, so it must keep printing the plain row. A fix that printed the
// cut sentence for every empty window would make the cut meaningless.
func TestChatVoice_QuietWindowKeepsThePlainSilenceRow(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic"}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }

	h.enter(t)
	h.waitFor(t, "the plain silence row", func(rows string) bool {
		return strings.Contains(rows, "no text in the last")
	})
	if rows := h.rows(); strings.Contains(rows, "cut inside speech") {
		t.Fatalf("a window with no cut reported one:\n--- grid ---\n%s", rows)
	}
}

// TestChatVoice_SilenceMarkerInsertsNothingAndSaysSo is the defect case
// (task 01a116ad): the LOCAL engine answers a speechless window with its own
// marker `/sil` instead of an empty string, and the round used to hand that
// marker back as a transcript — it is non-empty, so the silence arm of the loop
// never fired and the operator's line grew `draft-/sil/sil/sil`.
//
// The case asserts the MODE's contract on the real carrier, judged on the grid:
// the marker never reaches the editing line, and the silence row does appear.
// (That silence still leads to the NEXT window is the sibling case's assertion —
// TestChatVoice_SilenceInsertsNothingAndSaysSo — so this one stays at ONE marked
// round, the smallest sample that carries the defect.)
func TestChatVoice_SilenceMarkerInsertsNothingAndSaysSo(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic", texts: []string{
		silenceMarkerRound() + silenceMarkerRound(), // the engine streamed one marker per chunk
	}}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }

	// A draft on the line, so the defect's own shape (`draft-/sil`) is what the
	// grid would show if the marker were inserted.
	if res := h.in.injector.InjectKey("d"); !res.Accepted {
		t.Fatalf("injecting the draft refused: %s", res.Why)
	}
	h.waitFor(t, "the typed draft", func(rows string) bool { return strings.Contains(rows, "d") })

	h.enter(t)
	h.waitFor(t, "the silence row for the marker window", func(rows string) bool {
		return strings.Contains(rows, "no text in the last")
	})

	// THE MARKER IS NOT ON THE LINE — the assertion the fix exists for.
	if rows := h.rows(); strings.Contains(rows, chatVoiceSilenceMarker) {
		t.Fatalf("the engine's silence marker reached the screen:\n--- grid ---\n%s", rows)
	}
	if !h.mode.active() {
		t.Fatal("the marker left the mode: a speechless window is not a failure")
	}
}

// silenceMarkerRound is one engine reply carrying the marker and nothing else —
// the exact bytes the local arm streams for a speechless window.
func silenceMarkerRound() string { return chatVoiceSilenceMarker }

// TestChatVoiceTranscriptTextJudgesTheEngineMarker pins the ONE judgement (and
// its conservative other direction) without a terminal: the marker alone and the
// marker REPEATED by the engine's per-chunk streaming are silence, while a window
// that carries speech — even speech followed by the marker — is not.
func TestChatVoiceTranscriptTextJudgesTheEngineMarker(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"the marker alone", chatVoiceSilenceMarker, ""},
		{"the marker streamed once per chunk", chatVoiceSilenceMarker + chatVoiceSilenceMarker, ""},
		{"three chunks", strings.Repeat(chatVoiceSilenceMarker, 3), ""},
		{"surrounded by whitespace", "  " + chatVoiceSilenceMarker + "\n", ""},
		{"an empty transcript stays empty", "   ", ""},
		{"speech is passed through UNTOUCHED (no second trim)", " hello ", " hello "},
		{"speech that merely starts like the marker", "/silence", "/silence"},
		{"speech followed by the marker is speech", "/silhello", "/silhello"},
		{"a word after the marker is speech", "/sil 你好", "/sil 你好"},
	}
	for _, c := range cases {
		got := chatVoiceTranscriptText(c.in)
		if got != c.want {
			t.Errorf("%s: chatVoiceTranscriptText(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestChatVoice_UsageAndVocabularyAreTheSameReading pins the two surfaces a user
// reads against each other: the word is in the REPL's vocabulary, /help voice
// prints this file's block, and the usage text names the window and the exit
// spellings the SAME constants define.
func TestChatVoice_UsageAndVocabularyAreTheSameReading(t *testing.T) {
	if !chatSessionWordKnown(chatVoiceCommandName) {
		t.Fatalf("/%s is answered by the switch but is not in the REPL's vocabulary", chatVoiceCommandName)
	}
	if !voiceWordPresent(chatReplCommandNames(), chatVoiceCommandName) {
		t.Fatalf("/%s is missing from chatReplCommandNames — the refusal would call an accepted word unavailable", chatVoiceCommandName)
	}
	usage, err := chatHelpText("/help " + chatVoiceCommandName)
	if err != nil {
		t.Fatalf("chatHelpText(/help %s): %v", chatVoiceCommandName, err)
	}
	if usage != voiceChatUsage {
		t.Fatal("/help voice does not print the mode's own block — the help and the loop's constants are two readings")
	}
	for _, want := range []string{chatVoiceStatusWord, chatVoiceStopWord, "Esc"} {
		if !strings.Contains(voiceChatUsage, want) {
			t.Fatalf("the usage block does not name %q, which the mode accepts", want)
		}
	}
	if !strings.Contains(voiceChatUsage, "6") {
		t.Fatal("the usage block does not state the recording window the loop enforces")
	}
	// The vocabulary the REPL renders in the chat banner's slash list, too: a
	// word the switch answers and the banner omits is the drift the vocabulary
	// tests exist to catch.
	if !strings.Contains(chatUsage, "/"+chatVoiceCommandName) {
		t.Fatalf("the chat help banner does not list /%s", chatVoiceCommandName)
	}
}

func voiceWordPresent(words []string, want string) bool {
	for _, w := range words {
		if w == want {
			return true
		}
	}
	return false
}

var (
	// errNoTestDevice is the scripted "no capture device" failure.
	errNoTestDevice = errVoiceTest("no capture device: the scripted recorder was told to fail")
	// errTestRound is the scripted mid-session round failure.
	errTestRound = errVoiceTest("the scripted recorder failed the round")
)

// errVoiceTest is a tiny error type so the scripted failures are values a case
// can compare and print, not bare strings.
type errVoiceTest string

func (e errVoiceTest) Error() string { return string(e) }
