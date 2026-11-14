// reasoning_row_test.go — the live-reasoning row of `gopi ui` (epic: the model
// is thinking, made visible).
//
// WHAT IS PINNED HERE, and why each of the three is a real way to get this
// wrong:
//
//  1. THE WIRE: a `thinking` frame decodes into a reasoning row and NOT into the
//     answer. The frame exists as its own event precisely so the two cannot be
//     confused, and the client's decoder is where that is enforced (an unknown
//     event would be a refusal; a wrong one would be silent corruption of the
//     transcript).
//  2. THE WINDOW: the row is a ROLLING window over the newest lines. A row that
//     kept the whole stream would push the ANSWER off the screen — the reasoning
//     is an order of magnitude longer than the answer, so this is the difference
//     between a display that shows the model thinking and one that shows
//     nothing else.
//  3. THE EMPHASIS: the row is drawn LOW-BRIGHTNESS under the `· ` gutter, and
//     the row's emphasis is the row's OWN — never the answer's. The visible
//     properties are what a reader uses to tell the aside from the answer, so
//     a silent loss of either (a field that never reaches the paint, a palette
//     entry that is the answer's colour) has to fail here.

package uiclient

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestThinkingFrameBecomesAReasoningRowNotAnAnswer drives the frame through the
// client's own decode + apply path — the same path a live SSE stream takes.
func TestThinkingFrameBecomesAReasoningRowNotAnAnswer(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	m.entries = append(m.entries, entry{kind: entryAssistant, text: ""})
	m.assistantIdx = 0

	m.applyFrame(Frame{Event: EventThinking, Data: `{"text":"weighing the options "}`})
	m.applyFrame(Frame{Event: EventThinking, Data: `{"text":"before answering"}`})

	if m.errText != "" {
		t.Fatalf("a thinking frame was refused by the decoder: %s", m.errText)
	}
	if got := m.entries[m.assistantIdx].text; got != "" {
		t.Fatalf("the reasoning landed in the ANSWER row: %q", got)
	}
	if m.reasoningIdx < 0 {
		t.Fatal("no reasoning row was opened")
	}
	if got := m.entries[m.reasoningIdx].text; got != "weighing the options before answering" {
		t.Fatalf("reasoning row = %q, want the concatenated fragments", got)
	}
	if m.entries[m.reasoningIdx].kind != entryReasoning {
		t.Fatalf("the row's kind = %v, want entryReasoning", m.entries[m.reasoningIdx].kind)
	}
}

// TestReasoningRowIsDrawnAboveTheAnswer pins the placement: the aside belongs to
// the turn being produced, so a reader sees it BEFORE the answer it precedes.
func TestReasoningRowIsDrawnAboveTheAnswer(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	m.entries = append(m.entries, entry{kind: entryUser, text: "hi"})
	m.entries = append(m.entries, entry{kind: entryAssistant, text: ""})
	m.assistantIdx = 1

	m.applyFrame(Frame{Event: EventThinking, Data: `{"text":"thinking"}`})
	m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"the answer"}`})

	if m.reasoningIdx >= m.assistantIdx {
		t.Fatalf("reasoning row %d is not above the answer row %d", m.reasoningIdx, m.assistantIdx)
	}
	view := m.View()
	ti := strings.Index(view, "thinking")
	ai := strings.Index(view, "the answer")
	if ti < 0 || ai < 0 || ti > ai {
		t.Fatalf("the view does not show the reasoning above the answer (reasoning@%d answer@%d):\n%s", ti, ai, view)
	}
}

// TestReasoningRowKeepsOnlyTheNewestLines is the rolling window: the row's drawn
// height stays at the window, and the newest text is what survives.
func TestReasoningRowKeepsOnlyTheNewestLines(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	m.assistantIdx = -1

	const total = reasoningWindowLines * 3
	var last string
	for i := 0; i < total; i++ {
		last = "reasoning line " + string(rune('A'+i))
		m.applyFrame(Frame{Event: EventThinking, Data: `{"text":"` + last + `\n"}`})
	}

	rows := m.wrapEntry(m.entries[m.reasoningIdx], 80)
	if len(rows) > reasoningWindowLines {
		t.Fatalf("the reasoning row drew %d lines, want at most the window's %d:\n%s",
			len(rows), reasoningWindowLines, strings.Join(rows, "\n"))
	}
	view := m.View()
	if !strings.Contains(view, last) {
		t.Errorf("the NEWEST reasoning line is not visible — the window kept the wrong end:\n%s", view)
	}
	if strings.Contains(view, "reasoning line A") {
		t.Errorf("the OLDEST reasoning line is still visible — the window is not rolling:\n%s", view)
	}
}

// TestReasoningRowIsDrawnLowBrightnessUnderItsOwnGutter pins the two signals a
// reader uses to tell the aside from the answer.
func TestReasoningRowIsDrawnLowBrightnessUnderItsOwnGutter(t *testing.T) {
	m := newTestModel(t)
	m.pal = lineedit.HermesDarkPalette
	m.entries = nil
	m.assistantIdx = -1
	m.applyFrame(Frame{Event: EventThinking, Data: `{"text":"the model's aside"}`})

	rows := m.wrapEntry(m.entries[m.reasoningIdx], 80)
	if len(rows) == 0 {
		t.Fatal("the reasoning row rendered nothing")
	}
	if !strings.HasPrefix(rows[0], reasoningPrefix) {
		t.Fatalf("the reasoning row does not carry its gutter: %q", rows[0])
	}
	// The emphasis is the row's own field, and it must actually reach the paint:
	// a palette entry nobody reads is the quiet way this feature regresses.
	want := m.pal.Reasoning.Wrap("the model's aside")
	if !strings.Contains(rows[0], want) {
		t.Fatalf("the row is not painted with Palette.Reasoning:\n  row  = %q\n  want %q", rows[0], want)
	}
	// And the emphasis must be DISTINCT from the answer's, or the two rows are
	// the same colour and the reader has only the gutter left.
	if lineedit.HermesDarkPalette.Reasoning == lineedit.HermesDarkPalette.Answer {
		t.Error("the reasoning role and the answer role are the same style in the Hermes table")
	}
	if lineedit.HermesDarkPalette.Reasoning == (lineedit.Style{}) {
		t.Error("the reasoning role is empty in the Hermes table — the emphasis would silently vanish")
	}
}

// TestReasoningFrameIsRefusedWhenMalformed keeps the client's strict-decode
// contract: garbage on the thinking frame is a FAULT, never prose (defect 3).
func TestReasoningFrameIsRefusedWhenMalformed(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	m.assistantIdx = -1
	m.applyFrame(Frame{Event: EventThinking, Data: `raw bytes {"text":`})
	if m.errText == "" {
		t.Fatal("a malformed thinking frame produced no error state")
	}
	if m.reasoningIdx >= 0 {
		t.Error("a malformed thinking frame still opened a reasoning row")
	}
}

// TestReasoningPrefixIsListedInTheReaderGrammar keeps the dump reader's prefix
// strip in step with what the renderer draws (the drift the grammar test exists
// for, applied to the row this epic adds).
func TestReasoningPrefixIsListedInTheReaderGrammar(t *testing.T) {
	known := false
	for _, p := range uiRowPrefixes() {
		if p == reasoningPrefix {
			known = true
		}
	}
	if !known {
		t.Fatalf("uiRowPrefixes does not carry the reasoning gutter %q: the reader would leave it in the row", reasoningPrefix)
	}
	if reasoningPrefix != lineedit.ThinkingGutter {
		t.Fatalf("the reasoning gutter is %q here and %q in lineedit — one idea, two spellings", reasoningPrefix, lineedit.ThinkingGutter)
	}
}
