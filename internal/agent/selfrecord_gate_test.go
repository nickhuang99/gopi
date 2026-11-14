// selfrecord_gate_test.go — the two GATE tests of self-recording 第一期
// (selfrecord.go).
//
// GATE 1 — THE BLOCK SURVIVES COMPACTION, ON BOTH CHANNELS. A round's
// [IMPORTANT] note block must still be in the message sequence AFTER a
// compaction (re-injected verbatim by the carrier), AND its text must have
// reached the summarizer VERBATIM rather than a placeholder standing where it
// used to be. Both halves are asserted against a REAL trimContext run whose
// summarizer is an httptest server: the server's captured request body IS the
// summarizer's input, so "the note entered it" is measured rather than argued.
//
// THE PLACEHOLDER THIS GATE USED TO REQUIRE WAS A DEFECT, and the reason it
// recorded was circular: the contract said the block must never reach the
// summarizer, while the only thing the summarizer could possibly distill it from
// was that block — so the code deleted it from the input and the contract was
// then cited as the justification for the deletion. The measured net effect was
// the inverse of what a compaction should do: the memory block described the
// lossy material, and the ONE lossless copy (verbatim, turn-numbered, on disk)
// arrived through a second channel only. Passing it through is strictly more
// information for the same token budget, because the summarizer's own
// word-for-word rule (compactionSystemPromptTemplate) already requires exact
// values to survive.
//
// GATE 2 — THE LIVELOCK DOES NOT RECUR. The reported cycle (a summarizer call
// per round on a window that cannot converge: field session, 305 "anchored at …"
// rounds and 305 "summarized 1 oldest message(s)" rounds) must not come back
// through the NEW message type. The pinned window is planted with note blocks as
// its only compressible material, and the first round must pay ZERO summarizer
// calls, as must every round after it.
//
// BOTH TESTS ARE ALSO THE PHASE-1 CONTRACT'S OWN DOCUMENT: they state, in
// executable form, that the block is kept in the message (never stripped like
// hermes' strip_notes), that the record survives by being RE-INJECTED rather
// than by being exempted from deletion, and that the re-injected carrier is
// neither compressible material nor a new compaction floor.

package agent

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// selfRecordNeedle is the string every assertion in this file looks for: it
	// appears ONLY inside the note blocks, so finding it in the summarizer's
	// input is a leak, and failing to find it in the context is a lost record.
	selfRecordNeedle = "ROUND-NOTE-NEEDLE-9f3a"

	// selfRecordGateWindow is the context window both gates run at. Its
	// CompactTriggerRatio (0.75) puts the trigger at 15000 estimated tokens,
	// which the fixtures cross deliberately: a window big enough to leave the
	// fixture under the trigger would make every assertion here vacuous
	// (measured at the field window of 200000 the pass never ran and gate 2
	// passed under BOTH predicates).
	selfRecordGateWindow = 20000
)

// newSelfRecordTestAgent builds a real Agent (the production compaction
// machinery, a real HTTP summarizer stub, a real SessionLogger under an isolated
// GOPI_HOME) and returns it with the summarizer call counter and a pointer to
// the LAST request body the summarizer received.
//
// The request-body capture is the instrument gate 1 rests on: the summarizer's
// input is exactly what serializeForSummary produced, so a needle found there is
// proof that the note reached the summarizer — and its absence is proof that it
// did not, without appealing to the code under test.
func newSelfRecordTestAgent(t *testing.T, window int) (*Agent, *int, *string) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
	var lastBody string
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore the body is read for the assertion below; a read error leaves lastBody unset and the assertion fails loudly
		lastBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY-OF-FOLDED-HISTORY"))
	}, window)
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test logger teardown; the assertions read the context, not the JSONL
	ag.SessionLog = sl
	ag.CompactKeepRecent = 0
	return ag, calls, &lastBody
}

// plantSelfRecordNote records one note through the PRODUCTION path
// (recordSelfNotes — extraction + file append + the in-memory list the carrier
// reads) and returns the note text.
func plantSelfRecordNote(t *testing.T, ag *Agent, note string) string {
	t.Helper()
	content := "working on the task\n" + selfRecordOpenMarker + "\n" + note + "\n" + selfRecordCloseMarker + "\n"
	got := ag.recordSelfNotes(content, "")
	if len(got) != 1 {
		t.Fatalf("recordSelfNotes extracted %d note(s), want 1: %v", len(got), got)
	}
	return got[0]
}

// contextContains reports whether any message in the live context contains sub.
func contextContains(ctx *Context, sub string) bool {
	for _, m := range ctx.Messages {
		if strings.Contains(m.Content, sub) {
			return true
		}
	}
	return false
}

// TestSelfRecord_BlockSurvivesCompaction is GATE 1.
//
// The shape under test: a session that has recorded a note, whose history then
// grows past the compaction trigger and is compacted for real. The note's own
// message sits in the region the deletion pass walks through, so a pass that
// silently discards it (it IS discarded — the note is not compressible material,
// see isNonCompressibleMessage) must have its content re-injected by the
// survival guarantee.
func TestSelfRecord_BlockSurvivesCompaction(t *testing.T) {
	ag, calls, lastBody := newSelfRecordTestAgent(t, selfRecordGateWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)

	note := plantSelfRecordNote(t, ag, "Finished the parser rewrite "+selfRecordNeedle+". | the vendored SDK pins go1.21, bumps break it")
	c := ag.Ctx
	// The round that wrote the note: its content carries the block, and the
	// episode sits EARLY in the history so the deletion pass reaches it.
	c.AddUserMessage("EPIC TASK INPUT — " + strings.Repeat("e", 4000))
	c.AddAssistantMessage("working on the task\n"+selfRecordOpenMarker+"\n"+note+"\n"+selfRecordCloseMarker+"\n", nil)
	// A large removable middle, then the anchors and a tail above the trigger.
	fillHistory(c, 20, 2000)
	c.AddUserMessage("FINAL USER INSTRUCTION")
	c.AddAssistantMessage("final visible answer", nil)
	c.AddToolResult("call_tail", strings.Repeat("T", 40000))

	before := len(ag.Ctx.Messages)
	ag.trimContext(context.Background())

	if *calls != 1 {
		t.Fatalf("expected exactly 1 summarizer call for a history with real compressible material, got %d; log:\n%s", *calls, buf.String())
	}
	// 1) THE BLOCK IS STILL IN THE MESSAGE SEQUENCE. Nothing strips it: the
	//    note's own assistant message may be folded away with its neighbours, but
	//    the note TEXT is in front of the model either way.
	if !contextContains(ag.Ctx, selfRecordNeedle) {
		t.Fatalf("the note block did not survive compaction — the record is gone from the context (messages %d -> %d); log:\n%s",
			before, len(ag.Ctx.Messages), buf.String())
	}
	// 2) SOMETHING IN THE CONTEXT CARRIES IT — the re-injection carrier is the
	//    harness's own message, and it is what the survival guarantee promises
	//    when the note's original message is gone.
	carrier := false
	for _, m := range ag.Ctx.Messages {
		if strings.HasPrefix(strings.TrimLeft(m.Content, " \t\r\n"), selfRecordMarker) {
			carrier = true
			if !strings.Contains(m.Content, selfRecordNeedle) {
				t.Errorf("the re-injected carrier does not carry the recorded note:\n%s", m.Content)
			}
		}
	}
	if !carrier {
		t.Fatalf("no self-record carrier was re-injected after the compaction; contents:\n%s", dumpContext(ag.Ctx))
	}
	// 3) THE NOTE REACHED THE SUMMARIZER VERBATIM. The captured request body is
	//    the summarizer's actual input, so this is the note text itself, not a
	//    paraphrase of it and not a placeholder standing in its place.
	if !strings.Contains(*lastBody, selfRecordNeedle) {
		t.Fatalf("the note block did not reach the summarizer input — a placeholder in its place makes the memory block a summary of the LOSSY material alone:\n%s", *lastBody)
	}
	if strings.Contains(*lastBody, "self-record note block — omitted from the summary input") {
		t.Errorf("the summarizer input still carries the omission placeholder — the block is being dropped in place of being passed through:\n%s", *lastBody)
	}
	// The captured body must BE a summarizer request (its system prompt is the
	// memory-manager one) — otherwise "the needle is absent" could be true of the
	// wrong request entirely, and the assertion above would pass vacuously.
	if !strings.Contains(*lastBody, "memory manager of a coding agent") {
		t.Errorf("the captured body is not the summarizer request under test:\n%s", *lastBody)
	}
}

// TestSelfRecord_NoLivelockAfterCompaction is GATE 2.
//
// The pinned shape of the reported livelock, with the NEW message type inside
// it: every message the deletion pass can reach before its floors is one the
// summarizer may never be asked to distill — four note blocks plus the previous
// compaction's memory block. The window cannot converge, so the guard must
// refuse it WITHOUT paying a summarizer call, on the first round and on every
// round after.
//
// THREE PRE-COMPACTION ROUNDS, not one: the refusal is only distinguishable from
// the sliver guard (maxCompactSliverMessages = 2, which refuses ANY window of two
// messages or fewer whatever it holds) when the pinned window is LARGER than the
// sliver bound. Measured: with two pinned messages this test passed under the old
// memory-blocks-only predicate, because the sliver guard was doing the refusing;
// with four, reverting the predicate turns it red.
func TestSelfRecord_NoLivelockAfterCompaction(t *testing.T) {
	ag, calls, _ := newSelfRecordTestAgent(t, selfRecordGateWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)

	c := ag.Ctx
	for i, text := range []string{
		"Recorded the session plan " + selfRecordNeedle + ".",
		"Read the first chunk of big.txt " + selfRecordNeedle + ".",
		"Found the pinned-window shape " + selfRecordNeedle + " | a pinned window pays one LLM call per round forever if nothing guards it",
	} {
		note := plantSelfRecordNote(t, ag, text)
		if i == 0 {
			// The block the PREVIOUS compaction produced, at index 0 — the
			// original livelock case, still inside the window.
			c.AddAssistantMessage(memorySummaryMarker+"\n"+strings.Repeat("s", 300), nil)
		}
		c.AddAssistantMessage(selfRecordOpenMarker+"\n"+note+"\n"+selfRecordCloseMarker, nil)
	}
	// The anchors carry ALL the bulk: the window the deletion pass may reach
	// (everything before the first floor) is exactly the five pinned messages,
	// while the context sits far over the trigger because of the anchors
	// themselves — which no floor will surrender.
	//
	// The bulk is also UNFOLDABLE on purpose: the R1 pass collapses old TOOL
	// results first, so a fixture whose bulk is one giant tool result never
	// reaches the compaction path at all (measured: with an 80 KB tool result the
	// fold alone came back under the trigger and this test measured nothing).
	// Large user/assistant messages have no such shortcut.
	c.AddUserMessage("FINAL USER INSTRUCTION " + strings.Repeat("B", 40000))
	c.AddAssistantMessage("final visible answer "+strings.Repeat("A", 40000), nil)

	if est := ag.Ctx.EstimatedTokens(); est <= int(float64(selfRecordGateWindow)*DefaultCompactTriggerRatio) {
		t.Fatalf("fixture is under the compaction trigger (est %d) — the deletion pass would not run and every assertion below would be vacuous", est)
	}

	for round := 1; round <= 4; round++ {
		ag.trimContext(context.Background())
		if *calls != 0 {
			t.Fatalf("round %d paid %d summarizer call(s) for a window that cannot converge — the livelock guard did not refuse it (the reported cycle is one LLM call per round); log:\n%s",
				round, *calls, buf.String())
		}
	}

	// The refused compaction hands the context to the bounded deterministic trim
	// (the guard's documented fallback), so the bulk IS trimmed. What must hold
	// HERE is the malformed-request floor — a non-empty history whose head is not
	// an orphaned tool result (`"messages":null` → HTTP 422 was the defect the
	// trim fix closed, 9b9264b9) — and the record's survival on THIS path too: the
	// guarantee rides every exit from trimContext, not only the compaction one.
	if len(ag.Ctx.Messages) == 0 {
		t.Fatal("trim emptied the history — the request would serialize as messages:null (HTTP 422)")
	}
	if ag.Ctx.Messages[0].Role == "tool" {
		t.Fatalf("trim left an orphaned tool result at the head; kept %v", rolesOf(ag.Ctx))
	}
	if !contextContains(ag.Ctx, selfRecordNeedle) {
		t.Fatalf("the note block was lost on the bounded-trim path:\n%s", dumpContext(ag.Ctx))
	}
}

// dumpContext renders the live message sequence for a failure message: role,
// a marker, and the head of the content — enough to see WHAT the pass kept.
func dumpContext(ctx *Context) string {
	var b strings.Builder
	for i, m := range ctx.Messages {
		head := strings.ReplaceAll(m.Content, "\n", "⏎")
		if len(head) > 120 {
			head = head[:120] + "…"
		}
		marker := ""
		if isMemorySummaryMessage(m) {
			marker = " [memory-block]"
		}
		if isSelfRecordMessage(m) {
			marker += " [self-record]"
		}
		b.WriteString("  " + itoaSelfRecord(i) + ": " + m.Role + marker + " " + head + "\n")
	}
	return b.String()
}

// itoaSelfRecord is strconv.Itoa behind a name, so dumpContext's line shape stays
// readable without an import used once.
func itoaSelfRecord(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{digits[i%10]}, out...)
		i /= 10
	}
	return string(out)
}
