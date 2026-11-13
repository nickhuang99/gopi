// turnfold_head_note_test.go — the regression for the SILENT HEAD DROP.
//
// A resumed chat's history begins with the note the PREVIOUS run folded its last
// completed turn into (the note is the substitute that outlived that session, so
// it is the first message of the restored history). splitTurnRanges does not cut
// a range at index 0, so a head note and the assistant reply riding after it sat
// BEFORE every turn range — and foldCompletedTurns wrote
// `bodyFrom := complete[0].start`, i.e. the START OF THE FIRST RANGE, so
// `Messages[0:bodyFrom]` never entered `kept`: the head note (the previous run's
// knowledge AND its anchors) was deleted with no error and no log line
// (`folded=N` counts turns folded, never messages lost).
//
// THE ASSERTION IS THE ARITHMETIC, not a shape: a fold may only exchange
// `Removed` raw messages for `Folded` notes, so `len(out)` must be exactly
// `len(in) - Removed + Folded`, and the head note must still be message 0.
package agent

import (
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/toolnames"
)

// headNoteHistory builds the shape a post-resume chat carries: the previous
// run's folded note at index 0, the assistant reply that followed it, one
// COMPLETE raw turn carrying tool traffic (so it is foldable), and the in-flight
// turn.
func headNoteHistory(t *testing.T) (msgs []deepseek.Message, headNote string) {
	t.Helper()
	headNote = (&Agent{}).turnFoldNote([]deepseek.Message{
		{Role: roleUserMessage, Content: "asked before the resume"},
		{Role: roleAssistantMessage, Content: "answered before the resume"},
	}, nil, true, 1)
	if headNote == "" {
		t.Fatal("the fixture's head note is empty — the arm would measure nothing")
	}
	return []deepseek.Message{
		// ---- the head: what the PREVIOUS run left behind
		{Role: roleUserMessage, Content: headNote},
		{Role: roleAssistantMessage, Content: "carrying on after the resume"},
		// ---- turn 2: a raw completed turn with a tool round
		{Role: roleUserMessage, Content: sessionevent.HarnessMessageBoundaryPrefix + "input two"},
		{Role: roleAssistantMessage, Content: "running it", ToolCalls: []deepseek.ToolCall{{
			ID:   "call_head_drop_bash",
			Name: toolnames.ToolBash,
			Arguments: map[string]interface{}{
				"command": "go test ./internal/agent/ -run TurnFold",
			},
		}}},
		{Role: roleToolMessage, ToolCallID: "call_head_drop_bash", Content: "exit=0\nok  internal/agent  1.2s"},
		{Role: roleAssistantMessage, Content: "it passes"},
		// ---- in flight: must survive verbatim
		{Role: roleUserMessage, Content: sessionevent.HarnessMessageBoundaryPrefix + "in flight"},
		{Role: roleAssistantMessage, Content: "still working"},
	}, headNote
}

// TestTurnFoldKeepsTheHeadNote is the minimal reproduction of the defect: with a
// note at index 0 the fold used to drop index 0 up to the first turn range
// (measured before the fix: IN=8, OUT=3, removed=4 — five messages gone while the
// fold reported four).
func TestTurnFoldKeepsTheHeadNote(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chatMode bool
	}{
		// The chat face: index 0 is NOT the premise and the first range is
		// foldable, so `complete[0].start` is 2 — the head is what vanished.
		{name: "chat", chatMode: true},
		// The task face: index 0 is exempt as the premise, but the exemption is
		// applied to a range that starts at 2, so the head dropped all the same.
		{name: "task", chatMode: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs, headNote := headNoteHistory(t)
			a := turnFoldAgent(msgs)
			a.ChatMode = tc.chatMode

			in := len(a.Ctx.Messages)
			stats := a.foldCompletedTurns()
			out := len(a.Ctx.Messages)

			// NO MESSAGE VANISHES UNACCOUNTED: a fold swaps `Removed` raw
			// messages for `Folded` notes and must not move anything else.
			if want := in - stats.Removed + stats.Folded; out != want {
				t.Fatalf("IN=%d OUT=%d, want %d (in %d − removed %d + folded %d): the fold moved messages it never reported — %+v",
					in, out, want, in, stats.Removed, stats.Folded, stats)
			}
			// The head note IS the previous run's knowledge and anchors: it must
			// still be the first message, byte for byte.
			if len(a.Ctx.Messages) == 0 || a.Ctx.Messages[0].Content != headNote {
				t.Fatalf("the head note did not survive the fold: first message is %.80q", firstContent(a.Ctx.Messages))
			}
			// …and so must the assistant reply that rode after it.
			if a.Ctx.Messages[1].Content != "carrying on after the resume" {
				t.Fatalf("the reply after the head note was dropped: %.80q", a.Ctx.Messages[1].Content)
			}
		})
	}
}

// firstContent renders the first message for a failure line without panicking on
// an empty slice.
func firstContent(msgs []deepseek.Message) string {
	if len(msgs) == 0 {
		return "<empty conversation>"
	}
	return msgs[0].Content
}
