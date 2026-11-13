package testsuite

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestWeb_HappyPath_TwoTurnsSingleSession starts the REAL `gopi web` server
// (runWeb with an ephemeral --addr=127.0.0.1:0, so the bound port comes from
// the listening banner on stderr), then drives two /api/chat turns over real
// HTTP exactly like the browser does (turn 2 re-ships the accumulated
// history). Asserts per-turn SSE streaming (delta events + a done event whose
// result equals the mock's answer) and the single-session contract: both
// turns append to ONE session JSONL (one session_start, 2 user, 2 assistant,
// no per-turn session_end — the end event is written once at server
// shutdown, asserted in-process by cmd/agent's TestWebChat_SinglePersistentSession).
func TestWeb_HappyPath_TwoTurnsSingleSession(t *testing.T) {
	home := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: "web answer one"},
		testutil.Resp{Text: "web answer two"},
	)
	defer llm.Close()

	proc := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"},
		"", testutil.MockEnv(t, home, llm.URL()), t.TempDir())
	defer proc.Stop()
	base := proc.WebBaseURL()
	api := base + "/api/chat"

	// Turn 1: no history yet.
	body1 := postWebChat(t, api, `{"message":"first web turn","history":[]}`)
	events1 := testutil.ParseSSE(body1)
	assertSSEAnswer(t, events1, "web answer one")

	// Turn 2: re-ship the accumulated conversation, like the browser JS does.
	history := `[{"role":"user","content":"first web turn"},{"role":"assistant","content":"web answer one"}]`
	body2 := postWebChat(t, api, `{"message":"second web turn","history":`+history+`}`)
	events2 := testutil.ParseSSE(body2)
	assertSSEAnswer(t, events2, "web answer two")

	// Single-session contract: ONE file, ONE session id, both turns appended.
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertEventOrder(t, "session_start", "user", "assistant", "user", "assistant")
	sess.AssertCounts(t, map[string]int{
		"session_start": 1,
		"user":          2,
		"assistant":     2,
		"session_end":   0, // written only at server shutdown (finalizeChatSession)
	})
	if got := sess.UserContents(); len(got) != 2 || got[0] != "first web turn" || got[1] != "second web turn" {
		t.Errorf("web session user messages = %v, want the two turns", got)
	}
}

// TestWeb_ErrorScenario_BadRequests: the /api/chat endpoint must reject the
// browser's error cases with proper status codes over real HTTP — empty
// message 400, malformed JSON 400, non-POST 405 — and the root page must
// still serve (200).
func TestWeb_ErrorScenario_BadRequests(t *testing.T) {
	home := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "unused"})
	defer llm.Close()

	proc := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"},
		"", testutil.MockEnv(t, home, llm.URL()), t.TempDir())
	defer proc.Stop()
	base := proc.WebBaseURL()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"empty message", http.MethodPost, "/api/chat", `{"message":"   ","history":[]}`, http.StatusBadRequest},
		{"missing message", http.MethodPost, "/api/chat", `{"history":[]}`, http.StatusBadRequest},
		{"malformed json", http.MethodPost, "/api/chat", `{"message":`, http.StatusBadRequest},
		{"non-post", http.MethodGet, "/api/chat", "", http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest(c.method, base+c.path, strings.NewReader(c.body)) //stylegate:ignore builds a request against the test's own server; the response is the assertion
			if c.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", c.method, c.path, err)
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != c.want {
				t.Errorf("%s %s status = %d, want %d", c.method, c.path, resp.StatusCode, c.want)
			}
		})
	}

	// The chat page itself must still be served.
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "gopi web") {
		t.Errorf("GET / = %d, want 200 with the chat page", resp.StatusCode)
	}
}

// postWebChat issues one /api/chat turn over real HTTP and returns the raw
// SSE body, failing the test on any transport/status problem.
func postWebChat(t *testing.T, api, payload string) string {
	t.Helper()
	resp, err := http.Post(api, "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("POST /api/chat: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/chat status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	return string(body)
}

// assertSSEAnswer asserts the turn's SSE stream carried >=1 delta event and a
// terminal done event whose result equals the mock's answer.
func assertSSEAnswer(t *testing.T, events [][2]string, wantAnswer string) {
	t.Helper()
	if n := testutil.CountSSEEvents(events, "delta"); n < 1 {
		t.Errorf("SSE stream has %d delta events, want >= 1 (events: %v)", n, events)
	}
	var done map[string]interface{}
	gotDone := false
	for _, ev := range events {
		if ev[0] == "error" {
			t.Errorf("unexpected SSE error event: %s", ev[1])
		}
		if ev[0] == "done" {
			gotDone = true
			if err := json.Unmarshal([]byte(ev[1]), &done); err != nil {
				t.Fatalf("bad done payload %q: %v", ev[1], err)
			}
		}
	}
	if !gotDone {
		t.Fatalf("SSE stream never emitted done (events: %v)", events)
	}
	if res, _ := done["result"].(string); res != wantAnswer {
		t.Errorf("done.result = %q, want %q", res, wantAnswer)
	}
}
