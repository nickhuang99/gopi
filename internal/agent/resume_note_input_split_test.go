// resume_note_input_split_test.go — the recovered history must keep each WIRE
// text block as its OWN message.
//
// THE DEFECT THIS PINS (measured on session 6f982833, request #122 — the first
// request AFTER a resume): the turn fold and the operator's next input travel as
// TWO text blocks of ONE wire user message
// (`internal/deepseek/claude_client.go:578`, "Merge ALL consecutive user
// messages"). The resume path reconstructed that message with
// `strings.Join(texts, "\n")` (internal/agent/resume.go), so the note and the
// input became ONE `deepseek.Message` whose text merely STARTS with the note
// head. Two predicates then read it wrong, in opposite directions:
//
//	isTurnFoldNoteMessage -> true  (the head is a prefix of the concatenation)
//	isTurnOpeningRow      -> false (harness prefix → not the operator's turn)
//
// ⇒ after a resume the fold treats the operator's own input as an
// already-folded note: that turn can never be folded (turnRangeFoldable's first
// exclusion), and its anchors are never collected by turnFoldNote. The input is
// still READ by the model — the text is all there — so this is not data loss in
// the request; it is the fold silently losing one turn per resume, forever.
//
// The fix is the exact inverse of the builder: one Message per wire text block.

package agent

import (
	"fmt"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/sessionevent"
)

// reseedWireBody is a request body in the shape that produces the defect: ONE
// user message carrying the fold note and the operator's voice input as two
// text blocks, then a tool round, then the assistant's answer.
//
// It is spelled as JSON rather than built in Go because the join happens in the
// PARSER — building it through BuildClaudeRequest would re-fuse the two blocks
// before the code under test ever saw them, i.e. the fixture would measure
// itself instead of the parser.
const reseedWireBodyFormat = `{
 "model": "m",
 "messages": [
  {"role": "user", "content": [
    {"type": "text", "text": %q},
    {"type": "text", "text": %q}
  ]},
  {"role": "assistant", "content": [
    {"type": "text", "text": "checking"},
    {"type": "tool_use", "id": "call_1", "name": "bash", "input": {"command": "ls"}}
  ]},
  {"role": "user", "content": [
    {"type": "tool_result", "tool_use_id": "call_1", "content": "exit=0"}
  ]},
  {"role": "assistant", "content": [
    {"type": "text", "text": "done"}
  ]}
 ]
}`

// reseedWireBody returns the fixture above with a REAL note and a REAL input, so
// the predicates are exercised against the shipped spellings rather than a
// stand-in. %q quotes both, which is what keeps the JSON valid for CJK text.
func reseedWireBody() string {
	note := turnFoldNoteHead + "\nasked: an earlier turn\nanchors: bash, read, 01a1bafd"
	input := sessionevent.VoiceInputPrefix + "\n\n重新看一下 config 的 max_tokens 是谁写的"
	return fmt.Sprintf(reseedWireBodyFormat, note, input)
}

// TestResumeKeepsTheNoteAndTheInputApart is the RED-then-GREEN arm for the
// defect: the recovered history must hold the note and the input as two
// messages, and the input must still be the turn it was.
func TestResumeKeepsTheNoteAndTheInputApart(t *testing.T) {
	req, history, _, err := checkpointFromBody([]byte(reseedWireBody()), "test-fixture")
	if err != nil {
		t.Fatalf("checkpointFromBody: %v", err)
	}

	// The WIRE shape really does carry two text blocks in one message — without
	// this the fixture would pass for the wrong reason (nothing to split).
	var wireTextBlocks int
	for _, cm := range req.Messages {
		for _, b := range cm.Content {
			if cm.Role == "user" && b.Type == "text" {
				wireTextBlocks++
			}
		}
	}
	if wireTextBlocks != 2 {
		t.Fatalf("fixture carries %d user text block(s) on the wire, want 2 — the split is not being measured", wireTextBlocks)
	}

	if len(history) < 2 {
		t.Fatalf("recovered %d message(s) from a 2-text-block wire message — want the note and the input apart", len(history))
	}

	// (1) THE NOTE IS ITS OWN MESSAGE, and it still identifies as one.
	if !isTurnFoldNoteMessage(history[0]) {
		t.Errorf("history[0] is not recognised as a fold note; head=%q", textHead(history[0].Content))
	}

	// (2) THE OPERATOR'S INPUT IS ITS OWN MESSAGE — the defect fused it into the
	// note's message, and this is the assertion that fails when that returns.
	if isTurnFoldNoteMessage(history[1]) {
		t.Errorf("history[1] (the operator's input) is still fused into the note: %q", textHead(history[1].Content))
	}
	if !isTurnOpeningRow(history[1].Content) {
		t.Errorf("history[1] does not open a turn, so the input is not an input any more: %q", textHead(history[1].Content))
	}
	if want := sessionevent.VoiceInputPrefix; len(history[1].Content) < len(want) || history[1].Content[:len(want)] != want {
		t.Errorf("history[1] does not carry the voice-input provenance at its head: %q", textHead(history[1].Content))
	}

	// (3) THE FOLD DECISION the whole epic is about: the input's turn is foldable
	// again, and the note's own range is not (folding a note nests it).
	var sawInputRange, sawNoteRange bool
	for _, r := range splitTurnRanges(history) {
		first := history[r.start]
		if first.Role != roleUserMessage {
			continue
		}
		if isTurnFoldNoteMessage(first) {
			sawNoteRange = true
			if turnRangeFoldable(history[r.start:r.end]) {
				t.Errorf("range[%d:%d] IS a note and was judged foldable", r.start, r.end)
			}
			continue
		}
		if isTurnOpeningRow(first.Content) {
			sawInputRange = true
			if !turnRangeFoldable(history[r.start:r.end]) {
				t.Errorf("range[%d:%d] is the operator's input turn and is NOT foldable — the defect", r.start, r.end)
			}
		}
	}
	// A NOTE AT INDEX 0 OPENS NO RANGE — `splitTurnRanges` cuts a note into its
	// own range only for i > 0 (turnfold.go's note branch), so `sawNoteRange`
	// is deliberately NOT required here: in this fixture the note IS at index 0.
	// That is a SEPARATE defect with its own mechanism (the fold's `bodyFrom`
	// skips everything before the first range, silently dropping a post-resume
	// head note) and its own bug row; this arm measures the fusion only.
	_ = sawNoteRange
	if !sawInputRange {
		t.Error("no input range was found in the recovered history")
	}
}

// TestResumeReconstructionRoundTripsWithoutMerging is the criterion the fix is
// justified by: parsing a body and rebuilding it must return the SAME block
// partition. The joining reconstruction loses one text block per fuse; the
// splitting one loses none.
//
// It is asserted on the SHAPE (block types and text lengths, in order), because
// that is exactly the quantity the join destroyed.
func TestResumeReconstructionRoundTripsWithoutMerging(t *testing.T) {
	req, history, _, err := checkpointFromBody([]byte(reseedWireBody()), "test-fixture")
	if err != nil {
		t.Fatalf("checkpointFromBody: %v", err)
	}
	again := deepseek.BuildClaudeRequest("m", history, "", "", nil, 100, nil)

	if len(again.Messages) != len(req.Messages) {
		t.Fatalf("round trip produced %d message(s), want %d", len(again.Messages), len(req.Messages))
	}
	for i := range req.Messages {
		got, want := wireShape(again.Messages[i]), wireShape(req.Messages[i])
		if got != want {
			t.Errorf("message %d changed shape on the round trip:\n  in  = %s\n  out = %s", i, want, got)
		}
	}
}

// TestResumeStillReplaysAssistantTextAndThinking guards the HALF THAT MUST KEEP
// MERGING: an assistant wire message carries thinking + text + tool_use and is
// reconstructed as ONE message (the API rejects tool rounds whose thinking was
// dropped, so splitting that one would be a regression, not a fix).
func TestResumeStillReplaysAssistantTextAndThinking(t *testing.T) {
	_, history, _, err := checkpointFromBody([]byte(reseedWireBody()), "test-fixture")
	if err != nil {
		t.Fatalf("checkpointFromBody: %v", err)
	}
	var found bool
	for _, m := range history {
		if m.Role != roleAssistantMessage || len(m.ToolCalls) == 0 {
			continue
		}
		found = true
		if m.Content != "checking" {
			t.Errorf("assistant text = %q, want %q", m.Content, "checking")
		}
		if len(m.ToolCalls) != 1 || m.ToolCalls[0].Name != "bash" {
			t.Errorf("assistant tool calls = %+v, want one bash call", m.ToolCalls)
		}
	}
	if !found {
		t.Fatal("no assistant message carrying a tool call was reconstructed — the fixture or the parser lost the round")
	}
}

// wireShape renders a wire message as the ordered (type, text length) sequence —
// the partition, which is what the join destroyed and the split restores.
func wireShape(m deepseek.ClaudeMessage) string {
	out := m.Role + "["
	for i, b := range m.Content {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("%s:%d", b.Type, len(b.Text))
	}
	return out + "]"
}

func textHead(s string) string {
	const n = 60
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
