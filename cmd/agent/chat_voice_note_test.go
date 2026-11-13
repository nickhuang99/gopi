// chat_voice_note_test.go — the /voice notices' renderer, asserted at the BYTE
// level and at the COLUMN level.
//
// TWO KINDS OF CASE, and both are needed:
//
//  1. EXACT-BYTES cases over the pure renderers. They are the contract's own test:
//     every row the mode can print is produced by a function here, so a case can
//     state the expected text literally — no terminal, no mode, no clock. What
//     they PIN is the shape (which fact is on which row) and the text (the bytes
//     the operator reads), and they are the reason a reworded row cannot land
//     silently.
//
//  2. A CARRIER case on the REAL pty at the REAL narrow width the chat layout
//     may run at (voiceNoteNarrowCols), asserting the property the contract
//     PROMISES at any width: every fact BEGINS ITS OWN ROW, so a narrow terminal
//     can only wrap a VALUE and can never put two facts' labels on one row. The
//     measurement is the grid (Rule 10.2) — the cells the operator's terminal
//     shows — not the bytes handed to the writer.
package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// voiceNoteNarrowCols is the pty width the carrier case runs at: 60, the width
// the suite's own layout cases use (voicePtyCols) and narrower than the entered
// block's longest row ON PURPOSE. A case that ran at the width the block happens
// to fit would assert nothing about the promise.
const voiceNoteNarrowCols = 60

// voiceNoteNarrowRows is that pty's height: the entered block, the status row and
// the editing line all have to be on the screen at once for the labels to be read
// back.
const voiceNoteNarrowRows = 24

// voiceNoteEnteredFixture is the entered block's input as a literal, so the
// exact-bytes case below does not go through the REPL at all.
//
// LeaveWord is chatVoiceStopWord because that is the word the entered row
// ADVERTISES (task 01a1424f-4526): this fixture used chatVoiceStatusWord, which
// is how the exact-bytes case went on pinning a leave word the dispatcher does
// not act on. The renderer's argument is the caller's, so the literal here must
// be the REPL's own (chat_voice.go's voiceNoteEnteredFacts).
func voiceNoteEnteredFixture() voiceNoteEnteredFacts {
	return voiceNoteEnteredFacts{
		Device:    "test mic",
		Engine:    "recording",
		WindowSec: voiceChatWindowSeconds,
		LeaveWord: chatVoiceStopWord,
	}
}

// voiceNoteExpectedEntered is the entered block, byte for byte: the head row, then
// ONE FACT PER ROW, each indented by the named indent and separated from its value
// by the named gap.
//
// THE EXPECTED TEXT IS WRITTEN OUT HERE, not composed from the production
// constants: a case that composed its expectation from the renderer under test
// would pass no matter what the renderer did. The window number comes from the
// constant the LOOP enforces (voiceChatWindowSeconds) because that is a value, not
// a wording.
var voiceNoteExpectedEntered = voiceNotePrefixMode + "VOICE MODE ON" + "\n" +
	voiceNoteFactIndent + "device" + voiceNoteFactGap + "test mic" + "\n" +
	voiceNoteFactIndent + "engine" + voiceNoteFactGap + "recording" + "\n" +
	voiceNoteFactIndent + "window" + voiceNoteFactGap + fmt.Sprintf("%ds", voiceChatWindowSeconds) + "\n" +
	voiceNoteFactIndent + "text" + voiceNoteFactGap +
	"each transcript is APPENDED at the caret (`stop` or Esc leaves, the line is kept)."

// TestChatVoiceNote_EnteredBlockIsExactBytes pins the entered block: the four facts
// the epic requires (device / engine / where the text goes, plus the window) each
// on a row of its own, and not one of them deleted for looks.
func TestChatVoiceNote_EnteredBlockIsExactBytes(t *testing.T) {
	got := voiceNoteEntered(voiceNoteEnteredFixture())
	if got != voiceNoteExpectedEntered {
		t.Errorf("the entered block is not the shape the contract states\n--- got ---\n%q\n--- want ---\n%q", got, voiceNoteExpectedEntered)
	}
	// EVERY FACT VISIBLE SEPARATELY. This is the requirement stated as an
	// assertion rather than as a hope: the three facts the epic names each have a
	// row that starts with their label.
	for _, label := range []string{voiceNoteLabelDevice, voiceNoteLabelEngine, voiceNoteLabelText} {
		row := "\n" + voiceNoteFactIndent + label + voiceNoteFactGap
		if !strings.Contains(got, row) {
			t.Errorf("the entered block has no row that BEGINS with the %q fact (the fact is not separately visible)\n--- got ---\n%s", label, got)
		}
	}
	// AND THE VALUES ARE THE VALUES THE CALLER PASSED, not a default: a renderer
	// that dropped an argument would still satisfy the row assertions above.
	for _, value := range []string{"test mic", "recording", fmt.Sprintf("%ds", voiceChatWindowSeconds)} {
		if !strings.Contains(got, value) {
			t.Errorf("the entered block does not carry the passed value %q\n--- got ---\n%s", value, got)
		}
	}
	// NO FACT SHARES A ROW WITH ANOTHER FACT. Counted by SEGMENTATION (the
	// renderer's own promise), not by width.
	lines := strings.Split(got, "\n")
	if len(lines) != 1+4 {
		t.Errorf("the entered block is %d row(s); the contract says one head row plus one row per fact (4 facts) = 5\n--- got ---\n%s", len(lines), got)
	}
}

// TestChatVoiceNote_EnteredBlockNamesAnAbsentFactAsAbsent: a fact no surface could
// report is PRINTED, with the honest spelling — never a blank, and never a row
// that quietly disappears (Rule 15).
func TestChatVoiceNote_EnteredBlockNamesAnAbsentFactAsAbsent(t *testing.T) {
	got := voiceNoteEntered(voiceNoteEnteredFacts{Engine: "recording", WindowSec: voiceChatWindowSeconds, LeaveWord: chatVoiceStatusWord})
	if !strings.Contains(got, voiceNoteFactIndent+voiceNoteLabelDevice+voiceNoteFactGap+voiceNoteNotNamed) {
		t.Errorf("an unreported device did not print the absent-fact spelling %q (a blank row reads as a dropped fact)\n--- got ---\n%s", voiceNoteNotNamed, got)
	}
}

// TestChatVoiceNote_StatusOnIsTheSameFactsAsEntered: `/voice status` answers with
// the SAME fact set the entered row announced, so "what does it say now" does not
// depend on when the operator asked. The last fact differs by design (how to leave
// is spelled with the stop word here) and the case states that too.
func TestChatVoiceNote_StatusOnIsTheSameFactsAsEntered(t *testing.T) {
	got := voiceNoteStatusOn(voiceNoteStatusFacts{
		Device:    "test mic",
		Engine:    "recording",
		WindowSec: voiceChatWindowSeconds,
		LeaveWord: chatVoiceStopWord,
	})
	want := voiceNotePrefixMode + "voice mode is ON" + "\n" +
		voiceNoteFactIndent + "device" + voiceNoteFactGap + "test mic" + "\n" +
		voiceNoteFactIndent + "engine" + voiceNoteFactGap + "recording" + "\n" +
		voiceNoteFactIndent + "window" + voiceNoteFactGap + fmt.Sprintf("%ds", voiceChatWindowSeconds) + "\n" +
		voiceNoteFactIndent + "leave" + voiceNoteFactGap + "`" + chatVoiceStopWord + "` or Esc leaves."
	if got != want {
		t.Errorf("the status-ON block is not the shape the contract states\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
	// The two blocks agree on the three facts they share (Rule 14: one meaning per
	// fact, whoever asks).
	entered := voiceNoteEntered(voiceNoteEnteredFixture())
	for _, label := range []string{voiceNoteLabelDevice, voiceNoteLabelEngine, voiceNoteLabelWindow} {
		row := "\n" + voiceNoteFactIndent + label + voiceNoteFactGap
		if !strings.Contains(entered, row) || !strings.Contains(got, row) {
			t.Errorf("the %q fact is not stated identically by the entered block and the status block", label)
		}
	}
}

// TestChatVoiceNote_SingleClaimNotesAreOneRow: the contract's rule 2 — a note that
// is ONE claim stays ONE row. Asserted by the count of newlines, so a block that
// grew around a sentence is caught here rather than on a screen.
func TestChatVoiceNote_SingleClaimNotesAreOneRow(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"status off", voiceNoteStatusOff()},
		{"left (stopping)", voiceNoteLeft(chatVoiceLeftByEscape, chatVoiceLeftStoppingTail)},
		{"left (ran out)", voiceNoteLeft(chatVoiceLeftByCaptureEnded, chatVoiceLeftRanOutTail)},
		{"failed", voiceNoteFailed(errors.New("the device went away"))},
		{"retry", voiceNoteRetry(1, voiceChatMaxConsecutiveFailures, errors.New("refused"), voiceChatMaxConsecutiveFailures-1)},
		{"deps refusal", voiceNoteDepsRefusal("/missing/funasr")},
		{"device refusal", voiceNoteDeviceRefusal(errors.New("no capture device"))},
		{"argument refusal", voiceNoteArgRefusal("statuss")},
		{"no-editor refusal", voiceNoteNoEditorRefusal()},
		{"transcript", voiceNoteTranscript("hello world")},
		{"silence", voiceNoteSilent(voiceChatWindowSeconds)},
		{"split silence", voiceNoteSplitSilent(2, voiceChatWindowSeconds)},
		{"window split", voiceNoteWindowSplit(2, voiceChatWindowSeconds)},
		{"finish short", voiceNoteFinishShort(voiceChatFinishWait)},
		{"live finishing", voiceNoteLiveFinishing()},
		{"live finish sent", voiceNoteLiveFinishSent(voiceLiveFinishKeyLabel)},
		{"recorder line", voiceNoteRecorderLine("using a fallback device")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if n := strings.Count(tc.got, "\n"); n != 0 {
				t.Errorf("the %s note carries %d embedded newline(s); the contract says a one-claim note is ONE row\n--- got ---\n%q", tc.name, n, tc.got)
			}
			if strings.TrimSpace(tc.got) == "" {
				t.Errorf("the %s note rendered empty", tc.name)
			}
		})
	}
}

// TestChatVoiceNote_LeftCarriesBothHalves pins the leaving row's two-branch
// semantics at the RENDERER: the reason and the tail the caller paired with it are
// both on the row, and the row does not choose between them.
//
// The PAIRING itself (which reason gets which tail) is not this file's business
// and is asserted where it was fixed — chat_voice_left_note_test.go drives all
// five exits on the grid. What is asserted HERE is the renderer's half of it: it
// must print what it is given, verbatim, and it must not substitute one tail for
// the other.
func TestChatVoiceNote_LeftCarriesBothHalves(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		tail   string
	}{
		{"the operator's own exit", chatVoiceLeftByEscape, chatVoiceLeftStoppingTail},
		{"a failed round", chatVoiceLeftByFailed, chatVoiceLeftStoppingTail},
		{"the capture that ended first", chatVoiceLeftByCaptureEnded, chatVoiceLeftRanOutTail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := voiceNoteLeft(tc.reason, tc.tail)
			want := voiceNotePrefixMode + "voice mode OFF (" + tc.reason + ") — " + tc.tail
			if got != want {
				t.Errorf("the leaving row is not reason+tail verbatim\n--- got ---\n%q\n--- want ---\n%q", got, want)
			}
			other := chatVoiceLeftStoppingTail
			if tc.tail == chatVoiceLeftStoppingTail {
				other = chatVoiceLeftRanOutTail
			}
			if strings.Contains(got, other) {
				t.Errorf("the leaving row carries the OTHER direction's tail (%q) as well as its own", other)
			}
		})
	}
}

// TestChatVoiceNote_FailedRowStatesTheModeIsOff: the failure row's job is to tell
// the operator two facts — what went wrong and that the mode is off. A row that
// dropped either would leave the microphone's state to be inferred.
func TestChatVoiceNote_FailedRowStatesTheModeIsOff(t *testing.T) {
	probe := errors.New("doubao-stream engine: server: code 101")
	got := voiceNoteFailed(probe)
	if !strings.Contains(got, probe.Error()) {
		t.Errorf("the failure row does not name the error: %q", got)
	}
	if !strings.Contains(got, "The mode is OFF") {
		t.Errorf("the failure row does not say the mode is off: %q", got)
	}
	// The RETRY row is the same claim minus the exit: it must say the mode is
	// STILL listening, which is the one thing that distinguishes it from the row
	// above.
	retry := voiceNoteRetry(2, voiceChatMaxConsecutiveFailures, probe, 1)
	if !strings.Contains(retry, "Still listening") {
		t.Errorf("the retry row does not say the mode is still listening: %q", retry)
	}
	if !strings.Contains(retry, fmt.Sprintf("(%d of %d)", 2, voiceChatMaxConsecutiveFailures)) {
		t.Errorf("the retry row does not count the run: %q", retry)
	}
	if strings.Contains(retry, "The mode is OFF") {
		t.Errorf("the retry row claims the mode is off on a round that does not end it: %q", retry)
	}
}

// TestChatVoiceNote_TranscriptIsVerbatim: the transcript row carries the
// operator's OWN sentence unchanged — the renderer prefixes it and adds nothing.
func TestChatVoiceNote_TranscriptIsVerbatim(t *testing.T) {
	const text = "the quick brown fox; jumps over — 1, 2, 3 (`not a command`)"
	got := voiceNoteTranscript(text)
	if want := voiceNotePrefixVoice + text; got != want {
		t.Errorf("the transcript row is not the text verbatim under the audio prefix\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
	// A transcript containing percent verbs must not be re-interpreted: the whole
	// reason the call site passes it through `%s` and not as a format is that the
	// text is DATA.
	tricky := voiceNoteTranscript("100%s of %d")
	if tricky != voiceNotePrefixVoice+"100%s of %d" {
		t.Errorf("a transcript containing format verbs was mangled: %q", tricky)
	}
}

// TestChatVoiceNote_PrefixPolicyIsExactlyOnePrefix is the policy's own gate: every
// row this feature can print opens with EXACTLY ONE of the two prefixes, and no
// row carries two.
//
// It exists because the defect that produced it is measured: a caller wrapped a
// shared sentence (which already begins `[voice] `) in a second prefix and the
// console read `[voice] [voice] PRIVACY — …` (voice_module_defects_test.go). The
// policy — MODE rows say `[gopi] `, AUDIO rows say `[voice] `, SHARED text is not
// re-prefixed — is only real if something checks it.
func TestChatVoiceNote_PrefixPolicyIsExactlyOnePrefix(t *testing.T) {
	engineRow := voiceNoteEngineRow(voiceDoubaoPrivacyNotice(&voiceOptions{}))
	shared := []struct {
		name string
		row  string
	}{
		{"privacy (shared with the CLI)", engineRow},
		{"engine provenance (shared)", voiceNoteEngineRow(fmt.Sprintf(voiceEngineProvenanceLabel, "recording", "/m", "/r", "flag"))},
		{"recorder line (already carries the audio prefix upstream)", voiceNoteRecorderLine("fallback device")},
	}
	rows := []struct {
		name   string
		row    string
		prefix string
	}{
		{"entered", voiceNoteEntered(voiceNoteEnteredFixture()), voiceNotePrefixMode},
		{"status on", voiceNoteStatusOn(voiceNoteStatusFacts{Device: "d", Engine: "e", WindowSec: 6, LeaveWord: chatVoiceStopWord}), voiceNotePrefixMode},
		{"status off", voiceNoteStatusOff(), voiceNotePrefixMode},
		{"left", voiceNoteLeft(chatVoiceLeftByEscape, chatVoiceLeftStoppingTail), voiceNotePrefixMode},
		{"failed", voiceNoteFailed(errors.New("boom")), voiceNotePrefixMode},
		{"retry", voiceNoteRetry(1, voiceChatMaxConsecutiveFailures, errors.New("boom"), 2), voiceNotePrefixMode},
		{"deps refusal", voiceNoteDepsRefusal("/missing"), voiceNotePrefixMode},
		{"device refusal", voiceNoteDeviceRefusal(errors.New("none")), voiceNotePrefixMode},
		{"argument refusal", voiceNoteArgRefusal("nope"), voiceNotePrefixMode},
		{"no-editor refusal", voiceNoteNoEditorRefusal(), voiceNotePrefixMode},
		{"live finishing", voiceNoteLiveFinishing(), voiceNotePrefixMode},
		{"live finish sent", voiceNoteLiveFinishSent(voiceLiveFinishKeyLabel), voiceNotePrefixMode},
		{"transcript", voiceNoteTranscript("hi"), voiceNotePrefixVoice},
		{"silence", voiceNoteSilent(6), voiceNotePrefixVoice},
		{"split silence", voiceNoteSplitSilent(1, 6), voiceNotePrefixVoice},
		{"window split", voiceNoteWindowSplit(1, 6), voiceNotePrefixVoice},
		{"finish short", voiceNoteFinishShort(voiceChatFinishWait), voiceNotePrefixVoice},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.HasPrefix(tc.row, tc.prefix) {
				t.Errorf("the %s row does not open with its policy's prefix %q\n--- got ---\n%q", tc.name, tc.prefix, tc.row)
			}
			// EXACTLY ONE PREFIX IN TOTAL: the row's own policy prefix, and none
			// of the other — a row that carried both would be claiming two voices.
			total := strings.Count(tc.row, voiceNotePrefixMode) + strings.Count(tc.row, voiceNotePrefixVoice)
			if total != 1 {
				t.Errorf("the %s row carries %d prefix(es) in total; the policy is exactly one\n--- got ---\n%q", tc.name, total, tc.row)
			}
		})
	}
	for _, tc := range shared {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.HasPrefix(tc.row, voiceNotePrefixVoice) {
				t.Errorf("the %s row does not already open with %q — the owner of that sentence must prefix it\n--- got ---\n%q", tc.name, voiceNotePrefixVoice, tc.row)
			}
			if n := strings.Count(tc.row, voiceNotePrefixVoice); n != 1 {
				t.Errorf("the %s row carries %q %d time(s): a shared sentence was prefixed twice (this is the measured `[voice] [voice] PRIVACY` defect)\n--- got ---\n%q", tc.name, voiceNotePrefixVoice, n, tc.row)
			}
		})
	}
}

// TestChatVoiceNote_RenderersArePure: the same input renders the same bytes, and
// rendering twice in a row does not accumulate anything. It is a weak property on
// its own and it is stated because PURITY is what the whole file claims — a
// renderer that read the console's width would fail this, and would also be
// untestable without a terminal.
func TestChatVoiceNote_RenderersArePure(t *testing.T) {
	calls := []struct {
		name string
		fn   func() string
	}{
		{"entered", func() string { return voiceNoteEntered(voiceNoteEnteredFixture()) }},
		{"status off", func() string { return voiceNoteStatusOff() }},
		{"left", func() string { return voiceNoteLeft(chatVoiceLeftByEscape, chatVoiceLeftStoppingTail) }},
		{"transcript", func() string { return voiceNoteTranscript("x") }},
		{"silence", func() string { return voiceNoteSilent(voiceChatWindowSeconds) }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.fn()
			for i := 0; i < 3; i++ {
				if again := tc.fn(); again != first {
					t.Fatalf("call %d rendered different bytes from call 0\n--- first ---\n%q\n--- again ---\n%q", i+1, first, again)
				}
			}
		})
	}
}

// TestChatVoiceNote_BlockFactsStartTheirOwnRowAtANarrowWidth is the READABILITY
// assertion the contract promises, measured on the REAL carrier at a REAL narrow
// width.
//
// THE PROPERTY (not an impression): at voiceNoteNarrowCols the entered block's
// every fact must still BEGIN A ROW on the operator's grid. The layout is free to
// wrap a VALUE, and it does — the text fact is longer than the width — but it may
// never be able to push a label onto a row that already carries another label.
// That is exactly what one-fact-per-row buys, and it is what makes the block
// readable at 60 columns instead of only at 240.
//
// THE JUDGE IS THE GRID (Rule 10.2), and the MEASUREMENT is the number of rows the
// block occupies plus the labels found at column 0 after the indent.
func TestChatVoiceNote_BlockFactsStartTheirOwnRowAtANarrowWidth(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic"}
	h := newVoicePtyGridSize(t, rec, voiceNoteNarrowCols, voiceNoteNarrowRows)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	if !h.mode.active() {
		t.Fatalf("the mode did not enter\n--- grid ---\n%s", h.rows())
	}
	// THE ROW IS AWAITED, not assumed: the note is committed on the band by the
	// editor's own read loop, so a case that read the grid the instant enter()
	// returned would read a blank screen (measured: it did).
	h.waitFor(t, "the entered block's head on the grid", func(rows string) bool {
		return strings.Contains(rows, voiceNoteHeadEntered)
	})
	rows := h.rows()
	lines := strings.Split(rows, "\n")

	// WHAT THE BLOCK'S LONGEST ROW IS — the measurement the report states, taken
	// from the renderer rather than from an impression.
	block := voiceNoteEntered(voiceNoteEnteredFixture())
	longest := 0
	for _, l := range strings.Split(block, "\n") {
		if n := len([]rune(l)); n > longest {
			longest = n
		}
	}
	t.Logf("measured: the entered block's longest row is %d columns; this terminal is %d", longest, voiceNoteNarrowCols)
	if longest <= voiceNoteNarrowCols {
		t.Fatalf("the fixture is not exercising the narrow case: the block's longest row is %d columns and the terminal is %d, so nothing can wrap — lower voiceNoteNarrowCols or lengthen the fixture", longest, voiceNoteNarrowCols)
	}

	// EVERY FACT LABEL BEGINS A ROW ON THE GRID. The label is searched as
	// `indent+label+gap` at the START of a row (after the row's own leading
	// padding), which is the property the contract states.
	for _, label := range []string{voiceNoteLabelDevice, voiceNoteLabelEngine, voiceNoteLabelWindow, voiceNoteLabelText} {
		want := voiceNoteFactIndent + label + voiceNoteFactGap
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no row of the %d-column grid BEGINS with the %q fact (indent+label+gap %q): at this width the fact is no longer separately visible\n--- grid ---\n%s",
				voiceNoteNarrowCols, label, want, rows)
		}
	}

	// NO ROW CARRIES TWO FACTS' LABELS — the property that makes the assertion
	// above meaningful rather than accidental.
	for i, line := range lines {
		carried := 0
		for _, label := range []string{voiceNoteLabelDevice, voiceNoteLabelEngine, voiceNoteLabelWindow} {
			if strings.Contains(line, voiceNoteFactIndent+label+voiceNoteFactGap) {
				carried++
			}
		}
		if carried > 1 {
			t.Errorf("row %d of the grid carries %d fact labels: the block put two facts on one row, which the contract forbids\nrow: %q", i, carried, line)
		}
	}

	// THE HEAD IS ITS OWN ROW, so a wrapped head can never bury a fact.
	headRow := false
	for _, line := range lines {
		if strings.Contains(line, voiceNoteHeadEntered) {
			headRow = true
		}
	}
	if !headRow {
		t.Errorf("the entered block's head (%q) is not on the grid\n--- grid ---\n%s", voiceNoteHeadEntered, rows)
	}
}

// TestChatVoiceNote_NoInformationIsDeletedForLooks: the contract's last promise —
// the renderer NEVER truncates a value to make a row fit. Asserted directly: a
// very long value comes back whole, so the block's rows are as long as the facts
// need and width is the layout's business.
func TestChatVoiceNote_NoInformationIsDeletedForLooks(t *testing.T) {
	long := strings.Repeat("a-device-description ", 20) // ~420 columns, far past any terminal
	got := voiceNoteEntered(voiceNoteEnteredFacts{
		Device:    long,
		Engine:    "recording",
		WindowSec: voiceChatWindowSeconds,
		LeaveWord: chatVoiceStatusWord,
	})
	if !strings.Contains(got, long) {
		t.Errorf("the renderer truncated a long fact value — no information may be deleted for looks (got %d bytes, the value alone is %d)", len(got), len(long))
	}
	if !strings.Contains(got, voiceNoteNotNamed) && strings.Contains(got, "...") {
		t.Errorf("the block contains an ellipsis, which would mean something was elided\n--- got ---\n%s", got)
	}
}
