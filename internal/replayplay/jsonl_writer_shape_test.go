package replayplay

// jsonl_writer_shape_test.go — the timeline reader against the SHAPE THE WRITER
// ACTUALLY PRODUCES, and the ONE reader that reads it.
//
// WHY THIS FILE EXISTS (this port's own defect, found by running the verb against
// a real session on 2026-10-06 rather than by any unit test). The reader's first
// version assumed `message` was a bare string or a bare block list — the
// ANTHROPIC WIRE shape — and the ported fixture was written to that assumption,
// so the suite was green while `gopi session replay <a real task run>` refused
// with "carries no user/assistant turns with text". A real row carries an
// ENVELOPE:
//
//	{"role":"assistant","content":[{"type":"text","text":"…"},{"type":"tool_use",…}]}
//
// measured over this run's own log: 103 assistant rows with a block list and 5
// user rows with a plain string, every one an envelope carrying `role`+`content`.
//
// THAT READER IS NOW GONE — this package no longer parses JSONL at all (the
// grouping is internal/gopilog's, Rule 14) — so the regression this file guards
// has MOVED: it is no longer "does our private reader understand the envelope"
// but "does the SHARED reader, as this package drives it, produce a round with
// the text in it". The fixtures below are unchanged in shape on purpose: they are
// the measured row shapes, and a change here would be a change to the evidence.
//
// The row shapes that must contribute NO round content are also pinned: a
// tool_result is a RESULT (it belongs to a tool call, and this reader puts it
// there), and a thinking-only block is REASONING (it belongs to the round's
// thinking, never to its prose).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheSharedReaderHandlesTheWritersRowShapes is the regression guard: every
// row shape a session log really holds is read through the shared reader, and
// each one lands where it belongs.
func TestTheSharedReaderHandlesTheWritersRowShapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SessionJSONLName)
	log := `{"type":"session_start","sessionId":"x"}
{"type":"user","message":{"role":"user","content":"the operator question"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"the answer"},{"type":"thinking","thinking":"reflection"},{"type":"tool_use","id":"t1","name":"bash","input":{"command":"echo rawbytes"}}]}}
{"type":"request","iteration":0}
{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"rawbytes"}]}}
`
	if err := os.WriteFile(path, []byte(log), 0o600); err != nil {
		t.Fatalf("write the fixture log: %v", err)
	}
	// The words are one token each, so a wrap can never split them and the
	// assertion is about the TEXT surviving rather than about where the wrap fell.
	mat, err := LoadJSONLAt(path, FallbackCols, FallbackRows)
	if err != nil {
		t.Fatalf("LoadJSONLAt on the writer's own row shape: %v", err)
	}
	if len(mat.Rounds) != 1 {
		t.Fatalf("the writer's own row shape produced %d rounds, want 1: %+v", len(mat.Rounds), mat.Rounds)
	}
	r := mat.Rounds[0]
	joined := strings.Join(roundCellRows(r), "\n")
	for _, want := range []string{"question", "answer"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the replayed round does not carry %q — a row shape is being dropped:\n%s", want, joined)
		}
	}
	// THE TWO THAT MOVED RATHER THAN VANISHED: the reasoning is the round's
	// THINKING (it used to be dropped outright), and the tool result is the
	// CALL's result (it used to be dropped too).
	if len(r.Thinking) != 1 || !strings.Contains(r.Thinking[0], "reflection") {
		t.Errorf("the thinking block did not land on the round's reasoning: %#v", r.Thinking)
	}
	if len(r.Calls) != 1 || strings.TrimSpace(r.Calls[0].Result) != "rawbytes" {
		t.Errorf("the tool result did not land on the tool call: %+v", r.Calls)
	}
	// And it is NOT mistaken for prose: the two fields stay apart, so a viewer
	// reading the round's TEXT does not read the model's private reasoning.
	for _, txt := range r.Text {
		if strings.Contains(txt, "reflection") {
			t.Errorf("a thinking block became the round's prose: %q", txt)
		}
	}
}
