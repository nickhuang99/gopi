package testsuite

import (
	"encoding/json"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// shortReply is the short user-visible answer this test lands on: it is
// SHORTER than internal/agent's minFinalAnswerChars (40), which is the whole
// point of the repro — a chat turn whose final answer is terse used to be
// answered twice (the ChatOps degenerate-round guard re-issued the round and
// the session JSONL logged BOTH rounds' text).
const shortReply = "pty-ok"

// TestChat_TerseAnswer_OneAssistantRecordPerTurn is the Rule 5 regression
// test for the duplicated-assistant-record bug found while landing the Bubble
// Tea UI (task 01a0bedc): one user message must produce EXACTLY ONE assistant
// record in the session JSONL.
//
// The carrier is the real engine path the TUI drives — `gopi web --tools`
// serves POST /api/chat through Agent.ChatOps — and the turn's answer is
// terser than minFinalAnswerChars, the exact repro prompt ("Reply with
// exactly: pty-ok."). Before the fix this assertion read assistant=2 (two
// records, identical text) and the TUI rendered them back to back as
// "gopi> pty-okpty-ok"; after the fix it reads assistant=1.
//
// The session JSONL is the ground truth (not the SSE stream): the duplicate
// was a RECORDING artifact — the answer was appended twice for one turn — so
// the count is measured where the duplicate lived.
func TestChat_TerseAnswer_OneAssistantRecordPerTurn(t *testing.T) {
	home := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: shortReply})
	defer llm.Close()

	env := append(testutil.MockEnv(t, home, llm.URL()), "GOPI_WEB_TOOLS=1")
	proc := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"}, "", env, t.TempDir())
	defer proc.Stop()
	api := proc.WebBaseURL() + "/api/chat"

	body := postWebChat(t, api, `{"message":"Reply with exactly: `+shortReply+`."}`)
	events := testutil.ParseSSE(body)
	assertSSEAnswer(t, events, shortReply)

	// The RENDERED text: the TUI composes the reply from the SSE `delta`
	// frames, so the concatenation of those frames is what the user sees. The
	// bug showed up here as "pty-okpty-ok" — the degenerate round's text
	// streamed a second time — so this assertion is the TUI-visible half of
	// the same defect.
	if got := concatenatedDeltas(events); got != shortReply {
		t.Errorf("streamed (rendered) text = %q, want %q — the answer streamed more than once", got, shortReply)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	// The load-bearing assertion: ONE user turn ⇒ ONE assistant record.
	sess.AssertCounts(t, map[string]int{
		"user":      1,
		"assistant": 1,
	})
	// A duplicated record means the answer was logged twice; name it plainly
	// when it regresses instead of only reporting a count. (The per-round
	// `request` stat events interleave by design — they are not the turn's
	// answer and are deliberately not constrained here.)
	users := sess.UserContents()
	if len(users) != 1 || users[0] != "Reply with exactly: "+shortReply+"." {
		t.Errorf("session user records = %v, want the single repro turn", users)
	}
	texts := assistantTexts(t, sess)
	if len(texts) != 1 || texts[0] != shortReply {
		t.Errorf("session assistant records = %v, want exactly one carrying %q", texts, shortReply)
	}
}

// assistantTexts returns the text of every assistant text block in the session
// JSONL, in file order — the count is the bug's ground truth (a duplicated
// record carries the answer twice), and the text is what the TUI renders.
func assistantTexts(t *testing.T, sess *testutil.Session) []string {
	t.Helper()
	var out []string
	for _, ev := range sess.Events {
		if ev.Type != "assistant" {
			continue
		}
		m, ok := ev.Raw["message"].(map[string]interface{})
		if !ok {
			continue
		}
		blocks, ok := m["content"].([]interface{})
		if !ok {
			continue
		}
		for _, b := range blocks {
			blk, ok := b.(map[string]interface{})
			if !ok || blk["type"] != "text" {
				continue
			}
			if s, ok := blk["text"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// concatenatedDeltas joins the `delta` SSE frames' text in order — exactly
// what a streaming display (the Bubble Tea TUI, the browser page) shows the
// user for one turn.
func concatenatedDeltas(events [][2]string) string {
	var b string
	for _, ev := range events {
		if ev[0] != "delta" {
			continue
		}
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(ev[1]), &p); err != nil {
			continue
		}
		b += p.Text
	}
	return b
}
