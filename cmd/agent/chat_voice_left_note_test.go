// chat_voice_left_note_test.go — the leaving note's SECOND HALF must be the
// REASON's own.
//
// WHAT WAS WRONG (measured on a real pty against the real binary, task
// 01a11f8b: `gopi chat --voice-audio <finite file>`): the note was ONE template
// whose tail was fixed — `the recorder is stopping` — while chatVoiceLeftNote's
// reason slot was filled with every exit, including
// chatVoiceLeftByCaptureEnded. That reason means the OPPOSITE event: the capture
// had already ended ON ITS OWN (a finite source ran out, the recorder's stream
// closed) while the mode was still ON, and the mode took itself down over a
// source it no longer controlled. The grid read, verbatim:
//
//	[gopi] voice mode OFF (the capture ended) — the recorder is stopping; the
//	text already on the line is kept. No further transcript will be inserted.
//
// — a sentence stating the causality backwards: nothing was left to stop.
//
// THE FIX (chat_voice.go): the note composes the reason with the tail PAIRED to
// it (chatVoiceLeftTails / chatVoiceLeftTail), so the two halves cannot disagree
// about who ended the capture. The reason field stays exactly as attributable as
// it was — this case reads it off the same grid the operator reads.
//
// THE JUDGE IS THE GRID (Rule 10.2): every assertion below reads the cells the
// operator's terminal shows, and the expected sentence is COMPOSED from the
// production constants through the production renderer — a case that typed its
// own copy of the wording would keep passing after the note moved.
package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// voiceLeftNoteProbeErr is the failed round's error in the case below: an engine
// refusal, which is what `a failed round` names. It is a named value (Rule 1) and
// it is deliberately unlike the stream sentinel — the failed arm must not be
// reachable through the sentinel's own path.
var voiceLeftNoteProbeErr = errors.New("the engine refused the window")

// voiceLeftNoteGridCols/Rows are the geometry this case runs at: WIDE ENOUGH FOR
// THE WHOLE SENTENCE ON ONE BAND ROW. The leaving note is ~185 columns, and at
// the suite's default 60 the row wraps — a wrapped sentence whose top half has
// scrolled off the band would make the Contains assertion fail for a reason that
// has nothing to do with the wording, which is exactly the false negative this
// geometry removes.
const (
	voiceLeftNoteGridCols = 240
	voiceLeftNoteGridRows = 30
)

// voiceFlatten removes ALL whitespace from a rendered grid, so the assertion is
// about the SENTENCE and not about where a terminal happened to break it: rows
// are padded to their width, and a wrapped continuation would otherwise insert
// padding between two halves of one word.
func voiceFlatten(s string) string { return strings.Join(strings.Fields(s), "") }

// setRoundErr arms the recorder's failure leg AFTER the mode has entered: the
// exit this case drives is the ROUND's, so the mode must have been entered
// against a working recorder first (an arming at construction time would make
// the entry arm untested and the note's reason indistinguishable from an
// entrance that never happened).
func (r *scriptedRecorder) setRoundErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roundErr = err
}

// setRoundErrs arms the PER-ROUND failure script (see scriptedRecorder.roundErrs):
// one entry per round, so a case can drive a RUN of failures whose end is the
// event under test.
func (r *scriptedRecorder) setRoundErrs(errs ...error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roundErrs = errs
}

// voiceLeftNoteFailureRun is the run that ends the mode: the same engine refusal
// enough times IN A ROW to reach voiceChatMaxConsecutiveFailures. It is built from
// the constant rather than written as three literals, so the case states the rule
// and not the current number.
func voiceLeftNoteFailureRun() []error {
	run := make([]error, voiceChatMaxConsecutiveFailures)
	for i := range run {
		run[i] = voiceLeftNoteProbeErr
	}
	return run
}

// voiceLeftNoteSentence is the note as the OPERATOR sees it: the PRODUCTION
// template and the production reason constant, with the tail the CASE names.
//
// THE TAIL COMES FROM THE CASE, NOT FROM THE CODE UNDER TEST, and that is the
// difference between an assertion and a tautology: composing it through
// chatVoiceLeftTail would ask the pairing to confirm itself, and a wrong pairing
// would then be "confirmed" by the sentence it produced. The case states the
// criterion (this reason goes with this tail) and the grid has to carry it.
func voiceLeftNoteSentence(reason, tail string) string {
	return fmt.Sprintf(chatVoiceLeftNote, reason, tail)
}

// TestChatVoice_LeftNoteSecondHalfMatchesItsReason is the defect case: it drives
// all FIVE exit reasons and asserts, per reason, the causal pair —
//
//	the sentence the reason composes IS on the grid, and
//	the sentence belonging to the OTHER direction is NOT.
//
// The second half is what makes it a defect case rather than a smoke test: the
// pre-fix code satisfied `the recorder is stopping` for the four mode-driven
// exits and printed it for the capture-driven one too, which is the sentence
// that was backwards.
func TestChatVoice_LeftNoteSecondHalfMatchesItsReason(t *testing.T) {
	cases := []struct {
		name string
		// reason is the production constant the exit must attribute itself to.
		reason string
		// drive takes the mode from ON to the note. It never asserts; the grid
		// does.
		drive func(t *testing.T, h *voicePtyGrid, rec *scriptedRecorder)
		// wantTail / otherTail are the two halves the note may carry, so the case
		// states both the sentence it insists on and the one it forbids.
		wantTail string
	}{
		{
			name:   "the toggle",
			reason: chatVoiceLeftByToggle,
			drive: func(t *testing.T, h *voicePtyGrid, _ *scriptedRecorder) {
				t.Helper()
				h.mode.handleLine(h.in, completionSlashPrefix+chatVoiceCommandName)
			},
			wantTail: chatVoiceLeftStoppingTail,
		},
		{
			name:   "the stop word",
			reason: chatVoiceLeftByWord,
			drive: func(t *testing.T, h *voicePtyGrid, _ *scriptedRecorder) {
				t.Helper()
				h.mode.handleLine(h.in, completionSlashPrefix+chatVoiceCommandName+" "+chatVoiceStopWord)
			},
			wantTail: chatVoiceLeftStoppingTail,
		},
		{
			name:   "the operator's Escape",
			reason: chatVoiceLeftByEscape,
			drive: func(t *testing.T, h *voicePtyGrid, _ *scriptedRecorder) {
				t.Helper()
				gate := h.mode.state.gate
				if gate == nil {
					t.Fatal("entering installed no gate at the editor's key seam")
				}
				// The TYPED origin: the operator's own key, which is the only
				// origin the gate claims (an injected Escape is declined — the
				// rule that keeps a replay from switching off a microphone).
				if !gate.GateKey(lineedit.Key{Code: lineedit.KeyEscape, Origin: lineedit.ReadOriginTyped}) {
					t.Fatal("the gate declined the operator's typed Escape")
				}
			},
			wantTail: chatVoiceLeftStoppingTail,
		},
		{
			name:   "a failed run",
			reason: chatVoiceLeftByFailed,
			drive: func(t *testing.T, _ *voicePtyGrid, rec *scriptedRecorder) {
				t.Helper()
				// THE RUN, NOT ONE ROUND: a single failed window is RETRIED and
				// leaves the mode ON (task 01a121fc, deliverable D — see
				// chat_voice_failure_run_test.go), so the exit this case is about
				// is the one at the END of a consecutive run. Driving one failure
				// here would test the rule that replaced it.
				rec.setRoundErrs(voiceLeftNoteFailureRun()...)
			},
			wantTail: chatVoiceLeftStoppingTail,
		},
		{
			// THE DEFECT'S OWN CASE. The round answers with the SENTINEL — the
			// capture had already ended under a live mode and NOBODY asked the mode
			// to leave: no key, no word, no toggle. The mode must take itself down
			// and say that the capture ended, never that it is stopping a recorder.
			name:   "the capture ended under a live mode",
			reason: chatVoiceLeftByCaptureEnded,
			drive: func(t *testing.T, _ *voicePtyGrid, rec *scriptedRecorder) {
				t.Helper()
				rec.setRoundErr(errVoiceStreamClosed)
			},
			wantTail: chatVoiceLeftRanOutTail,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &scriptedRecorder{desc: "test mic"}
			h := newVoicePtyGridSize(t, rec, voiceLeftNoteGridCols, voiceLeftNoteGridRows)
			h.mode.resolveOpts = voiceChatTestOptions
			h.mode.depsMissing = func(*voiceOptions) string { return "" }
			h.enter(t)
			if !h.mode.active() {
				t.Fatalf("the mode did not enter\n--- grid ---\n%s", h.rows())
			}

			tc.drive(t, h, rec)

			// THE SENTENCE THE REASON COMPOSES, on the grid the operator reads.
			sentence := voiceLeftNoteSentence(tc.reason, tc.wantTail)
			h.waitFor(t, fmt.Sprintf("the leaving note for %q (%s)", tc.reason, sentence), func(rows string) bool {
				return strings.Contains(voiceFlatten(rows), voiceFlatten(sentence))
			})

			// AND THE OTHER DIRECTION'S SENTENCE IS ABSENT. This is the assertion
			// that was RED before the fix for the capture-ended reason: the grid
			// carried `the recorder is stopping` on an exit where nothing was left
			// to stop.
			other := ""
			for _, tail := range []string{chatVoiceLeftStoppingTail, chatVoiceLeftRanOutTail} {
				if tail != tc.wantTail {
					other = tail
				}
			}
			if other == "" {
				t.Fatalf("the case's own tails are indistinguishable — chatVoiceLeftStoppingTail and chatVoiceLeftRanOutTail must differ (they are the whole point of the pairing)")
			}
			rows := h.rows()
			if strings.Contains(voiceFlatten(rows), voiceFlatten(other)) {
				t.Errorf("the exit %q is explained with the OTHER direction's sentence (%q)\n--- grid ---\n%s", tc.reason, other, rows)
			}
			// The reason field stays attributable: the operator can still read WHO
			// left off the same row (Rule 15 — an exit is never anonymous).
			if !strings.Contains(voiceFlatten(rows), voiceFlatten(tc.reason)) {
				t.Errorf("the note no longer names the reason %q\n--- grid ---\n%s", tc.reason, rows)
			}
			if h.mode.active() {
				t.Errorf("the mode reports ON after the exit %q — the status row would claim a microphone nobody is reading", tc.reason)
			}
		})
	}
}
