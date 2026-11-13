package agent

// chat_prefetch_seed_test.go — the chat path's prefetched retrieval must enter
// the request as the model's OWN memory(action=search) pair, and a vote on its
// hits must have a CONSEQUENCE.
//
// The property under test is not "the text got there" (the old shape did that)
// but "the model can act on it": the hit lines carry record ids (#N), the
// feedback discipline reaches them, and the whitelist context slim the task loop
// already runs drops the pair once the model has had its round to vote. Each
// assertion drives a REAL ChatOps turn against a recording server — the shipped
// entry point, not a re-implementation of it (Rule 31).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
)

// seedMarkerText is the needle every assertion looks for: a distinctive string
// that can only have come from the seeded section, plus a hit line carrying
// record id #7 — the id a vote in these tests refers to, and the ONLY thing the
// slim's association can key on (it parses `#N` out of the tool result).
const (
	seedMarkerText = "PREFETCHSEED-MARKER"
	seedSection    = seedMarkerText + "\n#7 memory-prefetch seeding: the hit a vote can reach"
	seedQueryKey   = "seeded retrieval query"
)

// seedTurnServer records every request body and drives a scripted round
// sequence: rounds 1..n-1 issue the tool call the caller names, the final round
// answers with text. Callers assert on the recorded bodies.
type seedTurnServer struct {
	mu     sync.Mutex
	bodies []string
	// roundToolCall, when non-nil, is issued on the FIRST round and the round
	// then ends; the following round gets a text answer.
	roundToolCall func(w http.ResponseWriter)
}

func (s *seedTurnServer) record(b []byte) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, string(b))
	return len(s.bodies)
}

func (s *seedTurnServer) body(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 1 || i > len(s.bodies) {
		return ""
	}
	return s.bodies[i-1]
}

func (s *seedTurnServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func seedTurnChatOps(t *testing.T, ag *Agent, input string) {
	t.Helper()
	if _, err := ag.ChatOps(input, nil); err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}
}

// msgText renders every piece of text a wire message carries, so an assertion
// can look for a needle wherever the wire put it: text blocks, and the content
// of a tool_result block (which is a nested interface{} — a string on this
// path, but rendered through fmt so a future block shape cannot make the
// assertion pass vacuously).
func msgText(m deepseek.ClaudeMessage) string {
	var parts []string
	for _, b := range m.Content {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
		if b.Content != nil {
			parts = append(parts, fmt.Sprint(b.Content))
		}
	}
	return strings.Join(parts, "\n")
}

// toolUseBlocks returns every tool_use block the request carries, with the
// index of the message that holds it.
func toolUseBlocks(req *deepseek.ClaudeRequest) []struct {
	idx int
	blk deepseek.ClaudeContentBlock
} {
	var out []struct {
		idx int
		blk deepseek.ClaudeContentBlock
	}
	for i := range req.Messages {
		for _, b := range req.Messages[i].Content {
			if b.Type == "tool_use" {
				out = append(out, struct {
					idx int
					blk deepseek.ClaudeContentBlock
				}{i, b})
			}
		}
	}
	return out
}

// TestChatOpsSeedsPrefetchedRetrievalAsOwnSearchPair is the shape check: the
// first request of the turn carries an assistant message holding a memory
// action=search tool_use whose argument names the retrieval's OWN query, and the
// very next message is its tool result, stamped with the same tool_use id and
// carrying the rendered section unchanged.
func TestChatOpsSeedsPrefetchedRetrievalAsOwnSearchPair(t *testing.T) {
	srv := &seedTurnServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a diagnostic test body
		n := srv.record(b)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			claudeToolUseStream(w, "read", map[string]interface{}{"path": "/tmp/does-not-matter"})
			return
		}
		claudeTextStreamFragmented(w, []string{"Answered, with enough substance to pass the length gate."}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	ag.PrefetchSeed = SeededRetrieval{Query: seedQueryKey, Section: seedSection}
	seedTurnChatOps(t, ag, "hello there")

	first := srv.body(1)
	if first == "" {
		t.Fatal("no request was made")
	}
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal([]byte(first), &req); err != nil {
		t.Fatalf("unmarshal request 1: %v", err)
	}

	// Find the seeded call: a memory action=search tool_use that names the
	// retrieval's OWN query.
	uses := toolUseBlocks(&req)
	seedMsgIdx, seedBlkIdx := -1, -1
	for i, u := range uses {
		if u.blk.Name != toolnames.ToolMemory {
			continue
		}
		if act, _ := u.blk.Input["action"].(string); act == toolnames.ToolMemoryActionSearch {
			seedMsgIdx, seedBlkIdx = u.idx, i
		}
	}
	if seedMsgIdx < 0 {
		t.Fatalf("the seeded memory search is not on the wire:\n%s", first)
	}
	seedBlk := uses[seedBlkIdx].blk
	if got, _ := seedBlk.Input["query"].(string); got != seedQueryKey {
		t.Errorf("seeded call query = %q, want the retrieval's own key %q — the model must be able to see WHICH question was prefetched", got, seedQueryKey)
	}
	if !strings.HasPrefix(seedBlk.ID, prefetchSeedCallIDPrefix) {
		t.Errorf("seeded tool_use id = %q, want the recognisable %q prefix", seedBlk.ID, prefetchSeedCallIDPrefix)
	}
	if req.Messages[seedMsgIdx].Role != "assistant" {
		t.Fatalf("the seeded call sits on a %q message, want assistant", req.Messages[seedMsgIdx].Role)
	}
	// ONE retrieval, one call: a duplicated seed would inject the same hits
	// twice and pay for them twice. Counted over the tool_use BLOCKS, not over
	// the raw text — the id legitimately appears three times on the wire (the
	// call's own id field, the matching tool_use_id field, and the result's
	// [tool_use id: …] stamp), so a text count would be a count of the stamping
	// convention rather than of the injection.
	seededBlocks := 0
	for _, u := range uses {
		if strings.HasPrefix(u.blk.ID, prefetchSeedCallIDPrefix) {
			seededBlocks++
		}
	}
	if seededBlocks != 1 {
		t.Errorf("the first request carries %d seeded tool_use block(s), want exactly 1", seededBlocks)
	}

	// The result must follow its owner IMMEDIATELY — the wire requires every
	// tool_use to be answered before anything else — and carry the rendered
	// section verbatim under the same [tool_use id: …] stamp a real result has.
	if seedMsgIdx+1 >= len(req.Messages) {
		t.Fatal("the seeded tool_use has no result after it")
	}
	res := req.Messages[seedMsgIdx+1]
	if res.Role != "user" {
		t.Fatalf("the message after the seeded call has role %q, want the tool result (user role on this wire)", res.Role)
	}
	body := msgText(res)
	if !strings.Contains(body, "[tool_use id: "+seedBlk.ID+"]") {
		t.Errorf("the seeded result carries no tool_use id stamp, so the model cannot name it:\n%s", body)
	}
	if !strings.Contains(body, seedMarkerText) {
		t.Errorf("the seeded result does not carry the rendered section:\n%s", body)
	}
	if !strings.Contains(body, "#7 ") {
		t.Errorf("the seeded result lost the hit line's record id — the slim and the vote both key on #N:\n%s", body)
	}
}

// TestSeededPrefetchPairIsDroppedWhenItsRecordsWereNotVotedUseful is the
// CONSEQUENCE half, and the point of the whole change: the model votes
// action=feedback id=7 rating=useless on the seeded hit during the tool round it
// was given, and the NEXT request no longer carries the prefetched section.
//
// This is the behaviour a text prefix could never have: with no tool_use the
// pair does not exist, the slim cannot see it, and the section rides every later
// request for the rest of the session.
func TestSeededPrefetchPairIsDroppedWhenItsRecordsWereNotVotedUseful(t *testing.T) {
	body := seedSlimRound(t, "useless")
	if strings.Contains(body, seedMarkerText) {
		t.Errorf("the prefetched section was re-sent after the model voted its hit useless:\n%s", body)
	}
	if strings.Contains(body, prefetchSeedCallIDPrefix) {
		t.Errorf("the seeded call survived the slim even though its hit was voted useless:\n%s", body)
	}
}

// TestSeededPrefetchPairSurvivesAUsefulVote is the control that keeps the
// previous test honest: the SAME turn, the SAME slim, one word changed in the
// vote — and the section must still be on the wire. Without this pair the drop
// test would pass for a slim that dropped everything unconditionally.
func TestSeededPrefetchPairSurvivesAUsefulVote(t *testing.T) {
	body := seedSlimRound(t, "useful")
	if !strings.Contains(body, seedMarkerText) {
		t.Errorf("the prefetched section was dropped although its hit was voted USEFUL:\n%s", body)
	}
}

// seedSlimRound drives one real ops turn whose tool round votes `rating` on the
// seeded hit (#7), and returns the FINAL request body — the one the slim has
// already run against.
func seedSlimRound(t *testing.T, rating string) string {
	t.Helper()
	srv := &seedTurnServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a diagnostic test body
		n := srv.record(b)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			claudeToolUseStream(w, toolnames.ToolMemory, map[string]interface{}{
				"action": toolnames.ToolMemoryActionFeedback,
				"id":     7,
				"rating": rating,
				"reason": "seeded hit judged from a real round",
			})
			return
		}
		claudeTextStreamFragmented(w, []string{"Answered, with enough substance to pass the length gate."}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	ag.PrefetchSeed = SeededRetrieval{Query: seedQueryKey, Section: seedSection}
	seedTurnChatOps(t, ag, "hello there")

	if srv.count() < 2 {
		t.Fatalf("the turn made %d request(s); the slim can only be observed on a second one", srv.count())
	}
	// The FIRST request must have carried the seed (otherwise the second one's
	// silence proves nothing but that nothing was ever injected).
	if !strings.Contains(srv.body(1), seedMarkerText) {
		t.Fatalf("the seed never reached the first request:\n%s", srv.body(1))
	}
	return srv.body(srv.count())
}

// TestPrefetchSeedIsConsumedOnce pins the consume-once contract: a turn whose
// caller published nothing must not inherit the previous turn's retrieval. A
// stored field with no clearing would re-inject the same hits on every later
// turn of a persistent chat, and pay for them each time.
func TestPrefetchSeedIsConsumedOnce(t *testing.T) {
	ag := &Agent{}
	ag.PrefetchSeed = SeededRetrieval{Query: seedQueryKey, Section: seedSection}

	if got := ag.takePrefetchSeed(); got.Section != seedSection || got.Query != seedQueryKey {
		t.Fatalf("the first take returned %+v, want the published seed", got)
	}
	if got := ag.takePrefetchSeed(); !got.empty() {
		t.Fatalf("the second take returned %+v — the seed must be consumed, not read twice", got)
	}
	// A nil agent is a real caller (the plain chat path passes a nil executor
	// everywhere); taking from it must be the zero value, never a panic.
	var nilAg *Agent
	if got := nilAg.takePrefetchSeed(); !got.empty() {
		t.Fatalf("a nil agent returned %+v, want the zero value", got)
	}
}

// TestChatOpsWithoutASeedSendsNoPrefetchPair is the OFF path: the switch off
// means no seed is published, and the request must then carry no seeded pair at
// all — the message array stays exactly what it was before this change.
func TestChatOpsWithoutASeedSendsNoPrefetchPair(t *testing.T) {
	srv := &seedTurnServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a diagnostic test body
		srv.record(b)
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"Answered, with enough substance to pass the length gate."}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	seedTurnChatOps(t, ag, "hello there")

	body := srv.body(1)
	if strings.Contains(body, prefetchSeedCallIDPrefix) {
		t.Errorf("a turn with no seed carried a prefetched pair:\n%s", body)
	}
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Errorf("the turn's message array has %d messages, want exactly the user's own one", len(req.Messages))
	}
	if fmt.Sprint(req.Messages[0].Content) == seedMarkerText {
		t.Error("the OFF path invented content")
	}
}
