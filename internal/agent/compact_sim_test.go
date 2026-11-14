package agent

// compact_sim_test.go — Method A (task §6.3): replays a real session's
// message stream through the in-memory Context and the real trimContext
// chain (fake summarizer server — no LLM), measuring the per-round est-token
// curve.
//
// Session: ~/.gopi/sessions/da75915d-4990-4017-9dbe-27a58babe723.jsonl
// (93 rounds / 85 memory_store rounds / final API-measured prompt 130,687).
//
// Honest-note on the task's §6.3A premise: the session's measured prompt
// tokens peak at 130,687 — BELOW the 0.75×200000=150000 trigger line (the
// task text's "130K 终局 > 150K 触发线" is arithmetically wrong), and the
// pure heuristic estimate peaks at ~96K. So at the deployment window this
// session correctly triggers ZERO times — it fits comfortably (the contrast
// point vs the old 540K line that never triggered on anything). To
// demonstrate the mechanism engaging, the same stream is replayed at a
// scaled window where the trigger actually crosses, and a store-sparse
// synthetic session shows full target compliance at 200K.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

const simSessionPath = "/home/user/.gopi/sessions/da75915d-4990-4017-9dbe-27a58babe723.jsonl"

// simSession parses a gopi session JSONL into flat deepseek messages (order
// preserved) plus per-round landmarks (message count at each request, the
// API-measured prompt tokens for that round).
func simSession(t *testing.T) (msgs []deepseek.Message, landmarks [][2]int) {
	t.Helper()
	f, err := os.Open(simSessionPath)
	if err != nil {
		t.Skipf("session file not available: %v", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Role        string      `json:"role"`
				Content     interface{} `json:"content"`
				ToolUseID   string      `json:"tool_use_id"`
				ToolResults []json.RawMessage
			}
			PromptTokens int `json:"prompt_tokens"`
		}
		if err := dec.Decode(&rec); err != nil {
			break
		}
		switch rec.Type {
		case "user":
			msgs = append(msgs, deepseek.Message{Role: "user", Content: rec.Message.Content.(string)})
		case "assistant":
			var content string
			var tcs []deepseek.ToolCall
			for _, blk := range rec.Message.Content.([]interface{}) {
				b := blk.(map[string]interface{})
				switch b["type"] {
				case "text":
					content += b["text"].(string)
				case "tool_use":
					input := b["input"].(map[string]interface{})
					raw, _ := json.Marshal(input) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
					tcs = append(tcs, deepseek.ToolCall{
						ID:        b["id"].(string),
						Name:      b["name"].(string),
						Arguments: input,
						RawArgs:   string(raw),
					})
				}
			}
			msgs = append(msgs, deepseek.Message{Role: "assistant", Content: content, ToolCalls: tcs})
		case "tool_result":
			var result string
			var toolUseID string
			for _, blk := range rec.Message.Content.([]interface{}) {
				b := blk.(map[string]interface{})
				if c, ok := b["content"]; ok {
					switch v := c.(type) {
					case string:
						result += v
					case []interface{}:
						for _, part := range v {
							if s, ok := part.(string); ok {
								result += s
							}
						}
					}
				}
				if id, ok := b["tool_use_id"].(string); ok {
					toolUseID = id
				}
			}
			msgs = append(msgs, deepseek.Message{Role: "tool", Content: result, ToolCallID: toolUseID})
		case "request":
			landmarks = append(landmarks, [2]int{len(msgs), rec.PromptTokens})
		}
	}
	if len(landmarks) == 0 {
		t.Fatal("no request landmarks in session")
	}
	return msgs, landmarks
}

// simAgent builds the replay agent (fake summarizer server) with the given
// window and a fresh context.
func simAgent(t *testing.T, window int) (*Agent, *int) {
	return newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SIMULATED SUMMARY of the oldest excerpt"))
	}, window)
}

// replaySession replays the session stream through the agent's real
// trimContext chain. measuredPinning: pin the API-measured per-round count
// (SetPromptTokens) only until the first compaction — after that the
// session's recorded measurements (taken on the never-compacted stream) no
// longer correspond to the replayed context, and the heuristic estimate is
// the faithful stand-in for what the next real response would report.
// Returns the per-round post-trim est curve and the summarizer call count.
func replaySession(t *testing.T, ag *Agent, msgs []deepseek.Message, landmarks [][2]int, measuredPinning bool, calls *int) (curve []int) {
	t.Helper()
	next := 0
	pin := measuredPinning
	for _, lm := range landmarks {
		for _, m := range msgs[next:lm[0]] {
			ag.Ctx.AddMessage(m)
		}
		next = lm[0]
		if pin {
			ag.Ctx.SetPromptTokens(lm[1])
		}
		ag.trimContext(context.Background())
		curve = append(curve, ag.Ctx.EstimatedTokens())
		if pin && ag.Ctx.PromptTokens == 0 {
			// A compaction just reset the measurement — stop pinning.
			pin = false
		}
	}
	return curve
}

// --- deployment window: the real session fits (§6.3A contrast point) -------

func TestCompactSim_DeploymentWindowFitsRealSession(t *testing.T) {
	msgs, landmarks := simSession(t)
	ag, calls := simAgent(t, 200000)
	curve := replaySession(t, ag, msgs, landmarks, true, calls)

	if *calls != 0 {
		t.Fatalf("expected 0 compactions at the 200K deployment window (session peaks at 130,687 measured / ~96K heuristic, both < 150K trigger), got %d", *calls)
	}
	for r, est := range curve {
		if est > 150000 {
			t.Fatalf("curve exceeds trigger at round %d: est %d", r, est)
		}
	}
	if got := len(ag.Ctx.Messages); got != len(msgs) {
		t.Fatalf("expected all %d session messages retained (no trimming), got %d", len(msgs), got)
	}
	t.Logf("200K window: 0 compactions — curve max %d (measured max 130687, trigger 150000)", maxInt(curve))
}

// --- scaled window: the mechanism engages (1-2 compactions) -----------------

func TestCompactSim_RealSessionScaledWindow(t *testing.T) {
	msgs, landmarks := simSession(t)
	const window = 170000
	limit := int(float64(window) * DefaultCompactTriggerRatio)
	ag, calls := simAgent(t, window)
	ag.Logger = log.New(os.Stderr, "[sim] ", 0)
	curve := replaySession(t, ag, msgs, landmarks, true, calls)
	compactions := *calls

	if compactions < 1 || compactions > 2 {
		t.Fatalf("expected 1-2 compactions at window %d (trigger %d), got %d — adjust window", window, limit, compactions)
	}
	// The recorded (post-trim) curve never exceeds the trigger line.
	for r, est := range curve {
		if est > limit {
			t.Fatalf("curve exceeds trigger %d at round %d: est %d", limit, r, est)
		}
	}
	// All 85 memory_store rounds survive the compaction(s).
	stores := 0
	for _, m := range ag.Ctx.Messages {
		for _, tc := range m.ToolCalls {
			if tc.Name == "memory_store" {
				stores++
			}
		}
	}
	if stores != 85 {
		t.Fatalf("expected all 85 store rounds preserved, got %d", stores)
	}
	// The last visible assistant reply (the session's final answer) survives.
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || last.Content == "" {
		t.Fatalf("precondition: session must end with a visible assistant reply, got %q", last.Content)
	}
	found := false
	for _, m := range ag.Ctx.Messages {
		if m.Content == last.Content {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("last visible assistant reply must survive compaction")
	}
	t.Logf("scaled window %d (trigger %d): %d compaction(s), curve max %d, store rounds kept %d",
		window, limit, compactions, maxInt(curve), stores)
	// Round→est table (compressed) for the implementation report.
	var b strings.Builder
	for r, est := range curve {
		if r%5 == 0 || r == len(curve)-1 {
			fmt.Fprintf(&b, " r%d=%d", r, est)
		}
	}
	t.Logf("curve:%s", b.String())
}

// --- store-sparse synthetic session: full target compliance at 200K ---------

func TestCompactSim_StoreSparseSyntheticTargetCompliance(t *testing.T) {
	// 40 rounds, each ~5.1K tokens (a 20K-char tool result per round, like a
	// bash output): crosses the 150K trigger at round ~30. memory_store
	// rounds at rounds 20/25/30 (first protected at round 20 — everything
	// before it is compactable), plus one at round 40 (after the trigger).
	ag, calls := simAgent(t, 200000)
	var buf strings.Builder
	ag.Logger = log.New(&buf, "", 0)
	limit := int(float64(ag.ContextWindow) * DefaultCompactTriggerRatio)
	target := int(float64(ag.ContextWindow) * DefaultCompactTargetRatio)
	big := strings.Repeat("L", 20000)
	var curve []int
	compactions := 0
	for round := 0; round < 40; round++ {
		ag.Ctx.AddUserMessage(fmt.Sprintf("task round %d: analyze file f%02d", round, round))
		ag.Ctx.AddAssistantMessage(fmt.Sprintf("reading f%02d now", round), nil)
		ag.Ctx.AddAssistantMessage("", []deepseek.ToolCall{{
			ID:        fmt.Sprintf("call_r%d", round),
			Name:      "read_file",
			Arguments: map[string]interface{}{"path": fmt.Sprintf("f%02d.go", round)},
		}})
		ag.Ctx.AddToolResult(fmt.Sprintf("call_r%d", round), big)
		if round == 20 || round == 25 || round == 30 || round == 40-1 {
			ag.Ctx.AddAssistantMessage("", []deepseek.ToolCall{{
				ID:        fmt.Sprintf("call_s%d", round),
				Name:      "memory_store",
				Arguments: map[string]interface{}{"topic": fmt.Sprintf("t%d", round), "question": "q", "answer": "a"},
			}})
			ag.Ctx.AddToolResult(fmt.Sprintf("call_s%d", round), fmt.Sprintf("Memory stored (id=%d).", round))
		}
		ag.trimContext(context.Background())
		curve = append(curve, ag.Ctx.EstimatedTokens())
	}
	compactions = *calls
	if compactions != 1 {
		t.Fatalf("expected exactly 1 compaction, got %d", compactions)
	}
	for r, est := range curve {
		if est > limit {
			t.Fatalf("curve exceeds trigger %d at round %d: est %d", limit, r, est)
		}
	}
	// Post-compaction estimate falls to ≤ target + summary block (the
	// assertion log fired true).
	summaryEst := 0
	if len(ag.Ctx.Messages) > 0 && isMemorySummaryMessage(ag.Ctx.Messages[0]) {
		summaryEst = EstimateTokens(ag.Ctx.Messages[0])
	}
	if !strings.Contains(buf.String(), "assertion: after ≤ target+summary = true") {
		t.Fatalf("expected target-compliant assertion in log, got:\n%s", buf.String())
	}
	// Store rounds 20/25/30/40 all preserved; last user + last visible
	// assistant preserved.
	stores := 0
	for _, m := range ag.Ctx.Messages {
		for _, tc := range m.ToolCalls {
			if tc.Name == "memory_store" {
				stores++
			}
		}
	}
	if stores != 4 {
		t.Fatalf("expected 4 store rounds preserved, got %d", stores)
	}
	var joined string
	for _, m := range ag.Ctx.Messages {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "task round 39") {
		t.Fatal("last user message must survive compaction")
	}
	if !strings.Contains(joined, "reading f39 now") {
		t.Fatal("last visible assistant reply must survive compaction")
	}
	t.Logf("synthetic 200K: 1 compaction at round 30 (est %d → %d ≤ target+summary %d+%d), curve max %d",
		curve[29], curve[30], target, summaryEst, maxInt(curve))
}

func maxInt(xs []int) int {
	m := 0
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}
