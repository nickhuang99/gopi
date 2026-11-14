package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// compact_verbatim_values_test.go — regression guard for the compaction
// prompt's CONTRADICTION (task 01a198cd-9ae8-7ca9-ad04-2bd87f72a917).
//
// WHAT BROKE: the prompt said both
//   - "Do NOT echo, quote, or reproduce any part of the excerpt — no code
//     blocks ..." (HARD RULES), and
//   - "Preserve ... Commands run and their outcomes, test results".
//
// A value that IS a command outcome — the total an awk run printed, a token
// count, a duration — sits exactly on the seam: the first rule deletes it, the
// second asks for it. The summarizer resolved the conflict by deleting it, so
// after a MID-TURN compaction the agent no longer had "TOTAL = 180.3075 CNY"
// and re-derived it with a second awk run in the same turn, paying twice.
//
// WHAT THESE TESTS PIN: the carve-out exists, it is stated as WINNING, it names
// the classes of value it protects, and — the part that matters — it actually
// reaches the summarizer request on the wire. They do NOT pin the LLM's
// behaviour: that needs a live endpoint, and the measured sample lives in the
// task's session directory (see the task report), not in the suite.

// verbatimCarveOutMarker is the sentence that must survive any future edit of
// the summarizer prompt: it is the rule that resolves the contradiction.
const verbatimCarveOutMarker = "VERBATIM VALUES ARE DELIVERABLES, NOT ECHO"

// bannedEchoWording is the pre-fix phrasing. "any part of the excerpt" is what
// made a command's output unquotable; the fix narrows the ban to the
// CONVERSATION. Its return would re-open the defect, so its presence is a
// failure, not a style note.
const bannedEchoWording = "Do NOT echo, quote, or reproduce any part of the excerpt"

func TestCompactionPromptRequiresVerbatimValues(t *testing.T) {
	prompt := compactionSystemPrompt()

	if !strings.Contains(prompt, verbatimCarveOutMarker) {
		t.Fatalf("compaction prompt lost the verbatim-value carve-out (%q) — the summarizer will drop command outputs again", verbatimCarveOutMarker)
	}
	// The carve-out must say WHICH rule wins, otherwise the two rules are merely
	// adjacent again and the summarizer is free to pick the destructive one.
	for _, want := range []string{
		"EXCEPTION TO THE RULE ABOVE, AND IT WINS",
		"CHARACTER-FOR-CHARACTER",
		"NOT \"reproducing the excerpt\"",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("carve-out does not state %q — an exception that does not name itself as the winner is not an exception", want)
		}
	}
	// The classes of value an agent cannot cheaply recreate must be enumerated;
	// a generic "keep important things" is what got paraphrased away.
	for _, class := range []string{"a total", "a percentage", "a token count", "a duration", "a file or directory path", "an error string", "a commit hash"} {
		if !strings.Contains(prompt, class) {
			t.Errorf("carve-out does not name value class %q", class)
		}
	}
	// The word budget was a second, independent way to lose the number: a
	// summarizer trimming to 300-500 words drops digits first. The carve-out has
	// to be exempt from that budget explicitly.
	if !strings.Contains(prompt, "do NOT count against that budget") {
		t.Error("verbatim values are not exempted from the word budget — a 300-500 word cap will delete them anyway")
	}
	// The delivered example must be a labelled VALUE, not a placeholder.
	if !strings.Contains(prompt, "TOTAL = 180.3075 CNY") {
		t.Error("carve-out lost its worked example (a labelled value), leaving the rule abstract")
	}
}

func TestCompactionPromptNoLongerBansQuotingCommandOutput(t *testing.T) {
	prompt := compactionSystemPrompt()
	if strings.Contains(prompt, bannedEchoWording) {
		t.Fatalf("the pre-fix ban is back (%q): it forbids reproducing ANY part of the excerpt, which is exactly what forbids preserving a command's output — the two rules contradict again", bannedEchoWording)
	}
	// The ban still has to exist for its real target (transcribing the chat), or
	// the fix traded one failure for the other: raw transcripts in the summary.
	if !strings.Contains(prompt, "Do NOT echo the conversation back") {
		t.Error("the anti-transcription ban was dropped entirely instead of narrowed")
	}
}

// TestSummarizerRequestCarriesTheCarveOut proves the rule reaches the WIRE —
// a constant nobody passes to the model would satisfy the tests above while
// changing nothing. The fake server records the request body and the test
// asserts the system prompt inside it carries the carve-out.
func TestSummarizerRequestCarriesTheCarveOut(t *testing.T) {
	var body string
	ag, _ := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read summarizer request body: %v", err)
		}
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("MEMORY: summary"))
	}, 2000)
	ag.CompactMaxSummaryTokens = 4000

	msgs := []deepseek.Message{
		{Role: "user", Content: "sum the ledger rows"},
		{Role: "tool", ToolCallID: "c1", Content: "TOTAL = 180.3075 CNY"},
	}
	if _, err := ag.summarizeMessages(context.Background(), msgs); err != nil {
		t.Fatalf("summarizeMessages: %v", err)
	}
	if body == "" {
		t.Fatal("no summarizer request was captured — the assertion below would pass on an empty string")
	}

	var req struct {
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode summarizer request: %v", err)
	}
	if !strings.Contains(string(req.System), verbatimCarveOutMarker) {
		t.Fatalf("the summarizer's SYSTEM prompt on the wire does not carry the carve-out; system=%s", req.System)
	}
	// And the excerpt the model is asked to compress still contains the value, so
	// the rule has something to apply to (a prompt rule cannot preserve a number
	// that never entered the excerpt — truncation is a separate concern).
	if !strings.Contains(strings.Join(rawContents(req.Messages), ""), "180.3075") {
		t.Fatalf("the value never reached the summarizer input: %s", body)
	}
}

// rawContents flattens the message contents of a captured request body. The
// Claude Messages shape allows a content STRING or a block ARRAY (the
// summarizer sends a string today; a later caller may send blocks), so the
// decode accepts both — an assertion that only handled one shape would pass or
// fail on a wire detail instead of on the rule being tested.
func rawContents(msgs []struct {
	Content json.RawMessage `json:"content"`
}) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, string(m.Content))
	}
	return out
}

// TestAnchorIndexKeepsComputedTotal is the DETERMINISTIC half of "the number
// survives": the mechanical Anchor Index appends exact identifiers after the
// LLM summary, so even a summary that drops a figure leaves it recoverable in
// the same memory block. The prompt fix and this bedrock fail independently,
// which is why both are pinned.
func TestAnchorIndexKeepsComputedTotal(t *testing.T) {
	anchors := buildAnchorIndex([]deepseek.Message{
		{Role: "assistant", Content: "The ledger totals TOTAL = 180.3075 CNY across 48213 rows."},
	})
	if !strings.Contains(anchors, "180.3075") {
		t.Fatalf("anchor index dropped the computed total %q — nothing mechanical backs the prompt rule", anchors)
	}
}
