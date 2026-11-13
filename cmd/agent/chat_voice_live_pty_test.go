package main

// chat_voice_live_pty_test.go — the LIVE arm's acceptance case, on the REAL chat
// carrier: a pty, the production input layer, and the production read loop.
//
// WHAT IT PROVES THAT THE SEAM CASES CANNOT. The seam cases (chat_voice_live_test.go,
// internal/lineedit/draft_test.go) each pin one property in isolation. This one runs
// the whole path a real operator drives — the mode is entered through its own
// command line, a streaming session speaks partials from ANOTHER GOROUTINE while
// the read loop is parked, the finish key is delivered as the RAW BYTE a keyboard
// sends, and the result is read back off the PAINTED GRID — so the composition is
// checked, not just the parts.
//
// IT IS THE Rule 31 EVIDENCE for this feature at the mechanism level: the surface
// (the /voice live loop and the finish key) is EXECUTED here with real arguments
// through the real carrier. The manual PTY run recorded in the task's report is
// the same path with a microphone instead of a scripted recogniser.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/doubaostream"
)

// liveScriptedRecorder is a recorder that implements BOTH halves of the seam: the
// window half (so it is a legitimate voiceChatRecorder) and the live half, whose
// session speaks a script of partials at a scripted pace.
//
// IT IS THE SHAPE THE PRODUCTION RECORDER HAS — the live arm is an extra method on
// the same type, not a different kind of recorder — and building the double that
// way is what makes the mode's dispatch (engine + capability) the thing under test
// rather than a wiring the case installed by hand.
type liveScriptedRecorder struct {
	mu        sync.Mutex
	desc      string
	selectErr error
	// partials are the drafts the NEXT utterance produces, in order, each
	// delivered partialGap apart — the pace a streaming recogniser actually emits
	// at. They are CONSUMED (like scriptedRecorder's texts), so the script is one
	// sentence long and the utterances after it are silent: a double that
	// replayed the same sentence forever would put the same text on the line
	// repeatedly and make "the sentence appears once" untestable.
	partials []string
	// final is the text the NEXT utterance settles to (the recogniser's last
	// word), consumed with the partials.
	final string
	// utterances is how many Live calls have started, so a case can assert that
	// the loop asked for a SECOND sentence after the first settled.
	utterances int
	// liveEntered is closed by the first Live call, so a case can wait for the
	// session to be genuinely running rather than guessing a sleep.
	liveEntered chan struct{}
	// liveOnce guards liveEntered's close.
	liveOnce sync.Once
	// finished is closed when the FIRST utterance's finish channel is closed, so
	// the case can assert the utterance ended by the KEY rather than by a
	// teardown. It is closed ONCE (finishedOnce) because the mode's live loop asks
	// for utterance after utterance — the second call is the feature working, and a
	// bare close there is a panic in the double rather than a fault in the code
	// under test.
	finished     chan struct{}
	finishedOnce sync.Once
}

const (
	// livePartialGap is how far apart the scripted partials arrive. It is longer
	// than the read loop's paint budget and shorter than the case's wait, so the
	// grid genuinely shows an intermediate draft rather than a single final frame.
	livePartialGap = 120 * time.Millisecond
)

func (r *liveScriptedRecorder) Select(string) (string, []string, error) {
	if r.selectErr != nil {
		return "", nil, r.selectErr
	}
	return r.desc, nil, nil
}

func (r *liveScriptedRecorder) Round(*voiceOptions, int) (string, error) { return "", nil }

func (r *liveScriptedRecorder) Close() {}

// Live is the live half: it publishes the scripted partials through the sink, then
// settles the final text onto the line — the same sequence the production arm
// produces (chat_voice_live.go), so the mode's own handling of a partial and of a
// settle is what this case exercises.
//
// IT RESPECTS THE FINISH CHANNEL: the partials stop the moment the key arrives, and
// the settle happens then — which is what makes the case able to assert the
// operator's key is what ended the utterance.
func (r *liveScriptedRecorder) Live(_ *voiceOptions, sink voiceLiveSink, finish <-chan struct{}) (voiceLiveReport, error) {
	r.mu.Lock()
	r.utterances++
	r.mu.Unlock()
	r.liveOnce.Do(func() {
		if r.liveEntered != nil {
			close(r.liveEntered)
		}
	})
	r.mu.Lock()
	script, final := r.partials, r.final
	r.partials, r.final = nil, ""
	r.mu.Unlock()

	report := voiceLiveReport{}
	start := time.Now()
	for _, draft := range script {
		select {
		case <-finish:
			// The operator ended the sentence: the drafts stop here, exactly as
			// the production arm's feeder stops when the session is finished.
			goto settle
		case <-time.After(livePartialGap): //stylegate:allow-sleep the scripted recogniser's own pace, which is the variable under test
		}
		sink.Draft(draft)
		report.Partials++
	}
	select {
	case <-finish:
	case <-time.After(livePartialGap * 4): //stylegate:allow-sleep the scripted recogniser's own pace — an utterance with no key still ends
	}
settle:
	r.finishedOnce.Do(func() {
		if r.finished != nil {
			close(r.finished)
		}
	})
	report.Text = final
	report.Elapsed = time.Since(start)
	report.AudioMS = int(report.Elapsed.Milliseconds())
	if !sink.Settle(report.Text) {
		return report, errLiveSettleRefused
	}
	return report, nil
}

// errLiveSettleRefused is the arm's own settle refusal, mirrored here so the
// scripted recorder reports the same failure the production one does.
var errLiveSettleRefused = errVoiceLiveSettleRefused()

// livePtyGrid is the acceptance rig: the production carrier (newVoicePtyGrid) plus
// the streaming engine's settings, so the mode's OWN dispatch selects the live arm.
type livePtyGrid struct {
	*voicePtyGrid
	rec *liveScriptedRecorder
}

// newLivePtyGrid builds the rig with the streaming engine resolved, which is what
// makes the mode start the LIVE loop rather than the window loop (the dispatch is
// the mode's own, not the case's — see voiceChatMode.enter).
//
// THE ENGINE IS THE ONLY THING THE CASE INSTALLS: the credential resolution is
// stubbed (no network, no profile dependency), and everything else — the carrier,
// the mode, the read loop, the draft segment, the key gate — is production.
func newLivePtyGrid(t *testing.T) *livePtyGrid {
	t.Helper()
	rec := &liveScriptedRecorder{
		desc:        "test mic",
		partials:    []string{"温度", "温度是", "温度是多少"},
		final:       "温度是多少",
		liveEntered: make(chan struct{}),
		finished:    make(chan struct{}),
	}
	h := newVoicePtyGrid(t, nil)
	h.mode.recorder = rec
	// The engine resolution and the dependency check are the two seams enter()
	// calls BEFORE the microphone: the first is pinned to the streaming engine (the
	// dispatch under test) and the second is answered "ready" so the case does not
	// depend on this machine's credential file.
	h.mode.resolveOpts = func() (*voiceOptions, error) {
		return &voiceOptions{device: "test-device", engine: voiceStreamEngineLabel}, nil
	}
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	return &livePtyGrid{voicePtyGrid: h, rec: rec}
}

// TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey is the acceptance
// case: the whole live path, judged on the PAINTED GRID.
//
// THE LEGS ARE THE OPERATOR'S OWN CRITERIA, in the order they live them:
//
//  1. the mode is entered and starts the LIVE arm (not the window arm) — asserted
//     through the usage row, which only the live arm writes;
//  2. WHILE the session is still speaking, a DRAFT is on the editing row: the text
//     has grown in the line BEFORE the utterance ended, which is the whole feature;
//  3. the draft is REPLACED as the recogniser refines it — the grid never shows two
//     of the three statements at once (the defect this feature exists to remove);
//  4. the FINISH KEY, delivered as the raw byte a keyboard sends, ends the
//     utterance and the final text SETTLES onto the line;
//  5. the mode is STILL ON afterwards, so the operator can dictate the next
//     sentence;
//  6. Escape then leaves, and the line keeps everything that was said.
func TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey(t *testing.T) {
	h := newLivePtyGrid(t)

	// 1) ENTER. The live arm is what the mode's own dispatch selects for the
	//    streaming engine, and its usage row is its own marker.
	h.mode.handleLine(h.in, "/"+chatVoiceCommandName)
	h.waitFor(t, "the live arm's usage row", func(rows string) bool {
		return strings.Contains(rows, voiceLiveFinishKeyLabel)
	})

	// 2) THE FIRST DRAFT, painted WHILE the session is still speaking: the case
	//    waits for the session to have started before it looks, so a draft on the
	//    grid cannot be a settle that happened to arrive early.
	select {
	case <-h.rec.liveEntered:
	case <-time.After(voicePtyWait):
		t.Fatalf("the live session never started — the mode did not select the live arm for the streaming engine\n--- grid ---\n%s", h.rows())
	}
	h.waitFor(t, "a draft on the editing row while the session is still speaking", func(rows string) bool {
		return liveGridHas(rows, "温度是") || liveGridHas(rows, "温度是多少")
	})
	// THE REPLACEMENT PROPERTY, ON THE GRID: an appending implementation would show
	// all three statements glued together — "温度温度是" is what the first two look
	// like when the second does not replace the first.
	if rows := h.rows(); liveGridHas(rows, "温度温度") {
		t.Fatalf("the grid shows two drafts glued together — the segment appended instead of replacing:\n%s", rows)
	}

	// 3) THE FINISH KEY, WRITTEN TO THE PTY MASTER — the end a keyboard writes to.
	//    This is the load-bearing choice of the case: bytes that arrive through the
	//    terminal are stamped ReadOriginTyped (the operator's key), which is the
	//    origin the gate requires, while an INJECTED key is stamped
	//    ReadOriginInjected and is REFUSED by design (a replay must not end a
	//    human's sentence). So a pty write is the only way to drive this key
	//    through its real origin, and the case would otherwise be testing the
	//    refusal path.
	if _, err := h.pair.Master.Write([]byte{liveFinishKeyByte}); err != nil {
		t.Fatalf("writing the finish key to the pty master: %v", err)
	}
	select {
	case <-h.rec.finished:
	case <-time.After(voicePtyWait):
		t.Fatalf("the finish key did not end the utterance\n--- grid ---\n%s", h.rows())
	}

	// 4) THE SETTLE, on the grid, ONCE — and this is the assertion that catches
	//    both failure shapes at once: a settle that never landed (the text is
	//    missing) and one that landed twice (the sentence appears as a draft AND as
	//    the operator's text).
	// THE WAIT READS THE SAME FACE THE COUNT BELOW READS (task 01a12c12), and
	// that is the fix. The whole-grid wait was satisfied by a sentence that lives
	// on ANOTHER road as well: the [voice] transcript note carries the SAME text
	// (chatVoiceTranscriptNote), and it is a band row, so a whole-grid match
	// says nothing about the editing row the count below is about (task
	// 01a12b36's artifact: the note's sentence on the grid, the row still showing
	// the last DRAFT — `you> 温 度 是`). The line's own state is what this leg
	// measures, so it is what the wait must read. THE DRAFT SEGMENT BEING CLEARED
	// is the second half: while a draft is pending the sentence can be on the
	// line as the producer's own segment rather than as the operator's text,
	// which is exactly the state this leg must not measure (draft.go: the segment
	// is cleared in the same pass that moves the text into the buffer).
	h.waitFor(t, "the settled sentence on the editing row (draft segment cleared)", func(string) bool {
		return h.in.editor.Draft() == "" && liveGridHas(h.editingRow(), "温度是多少")
	})
	// THE SETTLE IS COUNTED ON THE EDITING ROW ONLY, because every row above it is
	// report chrome: the [voice] transcript note names the same sentence, and
	// counting it would make this assertion impossible to satisfy rather than
	// strict.
	editRow := h.editingRow()
	if got := strings.Count(liveSquash(editRow), liveSquash("温度是多少")); got != 1 {
		t.Fatalf("the settled sentence appears %d times on the editing row, want exactly once — a settle that landed twice leaves the sentence on the line as a draft AND as the operator's text:\n%s", got, editRow)
	}
	if liveGridHas(editRow, "温度温度") {
		t.Fatalf("the editing row still shows a draft glued to the settled text:\n%s", editRow)
	}

	// 5) THE MODE IS STILL ON: the finish key ended the UTTERANCE, not the mode.
	//    The proof is on the grid — the live indicator is still painted.
	if !h.mode.active() {
		t.Fatalf("the finish key left the mode — every sentence would turn the microphone off:\n%s", h.rows())
	}

	// 6) ESCAPE LEAVES, and the line keeps the sentence: the two keys' separation,
	//    end to end. It is written to the master for the same reason the finish key
	//    is — the gate claims only a TYPED Escape.
	if _, err := h.pair.Master.Write([]byte{escByteForLiveTest}); err != nil {
		t.Fatalf("writing Escape to the pty master: %v", err)
	}
	h.waitFor(t, "the mode to report it is off", func(rows string) bool {
		return !h.mode.active()
	})
	if final := h.rows(); !liveGridHas(final, "温度是多少") {
		t.Fatalf("leaving the mode lost the settled transcript:\n%s", final)
	}
}

// TestChatVoiceLive_ReportsTheUtterancesItRan pins the REPORT half: the mode keeps
// the numbers of each utterance it completed, so the acceptance evidence can quote
// measured values (partial count, audio duration) rather than an impression.
func TestChatVoiceLive_ReportsTheUtterancesItRan(t *testing.T) {
	h := newLivePtyGrid(t)
	h.mode.handleLine(h.in, "/"+chatVoiceCommandName)
	select {
	case <-h.rec.liveEntered:
	case <-time.After(voicePtyWait):
		t.Fatalf("the live session never started\n--- grid ---\n%s", h.rows())
	}
	// THE KEY IS PRESSED ONLY AFTER THE SESSION HAS SPOKEN, and this wait is the
	// difference between measuring the utterance and measuring its opening: the
	// finish key arriving before the first partial ends a sentence that had not
	// started, which is a real behaviour but not the one this case is about.
	h.waitFor(t, "the first draft, so the utterance has produced something before the key", func(rows string) bool {
		return liveGridHas(rows, "温度")
	})
	if _, err := h.pair.Master.Write([]byte{liveFinishKeyByte}); err != nil {
		t.Fatalf("writing the finish key to the pty master: %v", err)
	}
	deadline := time.Now().Add(voicePtyWait)
	for {
		if reports := h.mode.liveReportRows(); len(reports) > 0 {
			// THE REPORT CARRIES WHAT THE SESSION PRODUCED, not a placeholder: the
			// scripted session states three partials and settles one sentence, and
			// the mode must have recorded exactly that.
			if got := reports[0].Partials; got < 1 {
				t.Fatalf("the recorded utterance reports %d partials, want at least the one the session delivered before the key — the count is not reaching the mode", got)
			}
			if got := reports[0].Text; got != "温度是多少" {
				t.Fatalf("the recorded utterance reports text %q, want the settled sentence", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the mode recorded no utterance although one was run\n--- grid ---\n%s", h.rows())
		}
		time.Sleep(voicePtyStep) //stylegate:allow-sleep bounded poll (voicePtyWait) for the live loop to record the utterance
	}
}

// liveSquash removes every run of whitespace from a rendered row.
//
// IT IS HOW THIS FILE READS THE GRID, and the reason is measured: a terminal
// renders a WIDE rune (CJK) in two cells, and the sink pads the cell it does not
// use with a space, so the sentence "温度是多少" reaches the grid as
// "温 度 是 多 少" and a raw substring check for the sentence never matches. A
// folded row does the same thing with a newline. Squashing compares the TEXT the
// operator reads regardless of how the terminal laid it out — which is the fact
// under test in every case here.
func liveSquash(s string) string { return strings.Join(strings.Fields(s), "") }

// liveGridHas reports whether the rendered grid carries the given text, read with
// liveSquash's whitespace-insensitive comparison.
func liveGridHas(rows, want string) bool {
	return strings.Contains(liveSquash(rows), liveSquash(want))
}

// liveFinishKeyByte is Ctrl+G as a keyboard sends it — the byte, not a name, so the
// case drives the same decoder a real keypress does. It is read from the protocol
// package's own sentinel's spelling, kept as a local constant with its reason.
const liveFinishKeyByte = 0x07

// escByteForLiveTest is a lone ESC as a keyboard sends it — the same byte the
// decoder's own table names, spelled here because this case writes to the pty
// rather than naming a key.
const escByteForLiveTest = 0x1b

// errVoiceLiveSettleRefused reports the arm's settle refusal the way the arm does,
// so the scripted recorder cannot drift into accepting a settle the production one
// would refuse.
func errVoiceLiveSettleRefused() error {
	sink := voiceLiveSinkFor(&voiceChatRecorderStagedTest{})
	if sink.Settle("probe") {
		panic("a carrier with no draft road accepted a settle — the refusal this helper reports no longer exists")
	}
	return doubaostream.ErrSessionClosed
}
