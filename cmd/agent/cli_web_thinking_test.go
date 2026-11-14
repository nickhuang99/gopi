// cli_web_thinking_test.go — the live-reasoning frame at the SERVER.
//
// The epic's acceptance has two halves and this file is the first: the frame
// exists (and carries the model's raw fragments) when the switch is ON, and the
// wire is BYTE-IDENTICAL to the pre-epic one when it is OFF. Both halves are
// tested against the real handler — `handleChat`, the same function a browser's
// POST reaches — because the gate lives inside it (the switch decides whether the
// engine's callback is installed at all, so an "off" server has no code path that
// could emit the frame).
//
// The OFF case is the load-bearing one. This handler runs under `gopi ui` as
// well, and there the frames are DRAWN: a stray frame would be a repaint of a
// surface nobody asked to change, on every turn, for every operator who never
// opted in.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

// thinkingFragment is the reasoning text the mock model produces, in two
// fragments (so the stream really is chunked across frames).
var thinkingFragments = []string{"Weighing the options ", "before answering."}

// thinkingSSE streams a reasoning block (one delta per fragment) followed by the
// answer, in the Claude shape the engine parses.
func thinkingSSE(t *testing.T, w io.Writer, fragments []string, answer string) {
	t.Helper()
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}")
	for _, f := range fragments {
		payload, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("marshal fragment: %v", err)
		}
		writeClaudeSSE(w, "content_block_delta",
			fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%s}}", payload))
	}
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}")
	answerJSON, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	writeClaudeSSE(w, "content_block_delta",
		fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}", answerJSON))
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":1}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":20}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// thinkingServer builds a web server whose agent answers with a reasoning block
// then an answer, with the live-reasoning switch set as given.
func thinkingServer(t *testing.T, thinking bool, answer string) (*webServer, *httptest.Server) {
	t.Helper()
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		thinkingSSE(t, w, thinkingFragments, answer)
	}))
	t.Cleanup(mockSrv.Close)
	ag := &agent.Agent{
		Client: &deepseek.Client{
			BaseURL:    mockSrv.URL,
			APIKey:     "test-key",
			HTTPClient: mockSrv.Client(),
		},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0,
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now(), thinking: thinking}
	return ws, mockSrv
}

// TestThinkingFrameIsAbsentByDefault is the OFF half: the default server must put
// not one `thinking` frame on the wire, and the reasoning must not appear in the
// stream under any other name either (a leak into delta would be worse than a
// frame: it would be replayed as the answer).
func TestThinkingFrameIsAbsentByDefault(t *testing.T) {
	const answer = "Here is the answer."
	ws, _ := thinkingServer(t, false, answer)
	body := postChat(t, ws, "hi", nil)

	for _, pair := range sseEvents(body) {
		if pair[0] == webEventThinking {
			t.Fatalf("the default server emitted a %s frame:\n%s", webEventThinking, body)
		}
		if strings.Contains(pair[1], thinkingFragments[0]) {
			t.Fatalf("the reasoning leaked into a %s frame — a client would render it as content:\n%s", pair[0], pair[1])
		}
	}
	if !strings.Contains(body, answer) {
		t.Fatalf("the answer is missing from the default stream:\n%s", body)
	}
}

// TestThinkingFramesCarryTheRawFragments is the ON half: one frame per fragment,
// carrying the model's bytes with no marker and no SGR (the payload contract the
// renderer depends on), and the answer still arrives as delta text.
func TestThinkingFramesCarryTheRawFragments(t *testing.T) {
	const answer = "Here is the answer."
	ws, _ := thinkingServer(t, true, answer)
	body := postChat(t, ws, "hi", nil)

	var got []string
	for _, pair := range sseEvents(body) {
		if pair[0] != webEventThinking {
			continue
		}
		var v struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(pair[1]), &v); err != nil {
			t.Fatalf("the %s frame's payload does not decode (%v): %s", webEventThinking, err, pair[1])
		}
		if strings.Contains(v.Text, "\x1b[") {
			t.Fatalf("the frame carries SGR decoration: %q", v.Text)
		}
		got = append(got, v.Text)
	}
	if want := strings.Join(thinkingFragments, ""); strings.Join(got, "") != want {
		t.Fatalf("the frames carried %q, want the model's own fragments %q (frames: %q)", strings.Join(got, ""), want, got)
	}
	// Chunked, not one blob: a live display draws as the pieces arrive. And the
	// frames equal the fragments EXACTLY — the strongest available statement that
	// no marker was wrapped around them (a prefix or a suffix of any kind would
	// break the equality, whatever the marker's spelling).
	if len(got) != len(thinkingFragments) {
		t.Errorf("got %d thinking frames for %d fragments: %q", len(got), len(thinkingFragments), got)
	}
	for i, f := range thinkingFragments {
		if i < len(got) && got[i] != f {
			t.Errorf("frame %d = %q, want the model's fragment %q verbatim (no marker, no decoration)", i, got[i], f)
		}
	}
	if !strings.Contains(body, "event: "+webEventDelta) {
		t.Fatalf("the answer's own frames disappeared once reasoning was on:\n%s", body)
	}
	if !strings.Contains(body, answer) {
		t.Fatalf("the answer text is missing:\n%s", body)
	}
}

// TestThinkingStreamLeavesTheEngineRestored pins the defer that un-installs the
// callback: the persistent agent outlives the turn, so a handler that left the
// seam installed would keep framing reasoning for whatever ran next — including a
// request that asked for none.
func TestThinkingStreamLeavesTheEngineRestored(t *testing.T) {
	ws, _ := thinkingServer(t, true, "answer")
	_ = postChat(t, ws, "hi", nil)
	if ws.agent.OnThinking != nil {
		t.Error("the handler left OnThinking installed on the persistent agent")
	}
	if ws.agent.StreamThinkingDeltas {
		t.Error("the handler left StreamThinkingDeltas set on the persistent agent")
	}
}

// TestThinkingSwitchEnvChannelIsTruthyOnly pins the env channel's reading, which
// is deliberately NOT its `GOPI_WEB_TOOLS` sibling's.
//
// The sibling accepts "any non-empty value" for historical reasons (it shipped
// that way and operators' scripts depend on it); a NEW knob has no such history,
// and `GOPI_THINKING_LIVE=0` turning the feature ON is the reading nobody
// expects. Measured on the real binary: `=0` → 0 thinking frames, `=1` → 88.
func TestThinkingSwitchEnvChannelIsTruthyOnly(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false}, {"0", false}, {"false", false}, {"no", false}, {"off", false},
		{"1", true}, {"true", true}, {"yes", true},
	}
	for _, c := range cases {
		t.Setenv(webThinkingEnv, c.val)
		if got := webThinkingEnabled(); got != c.want {
			t.Errorf("%s=%q → %v, want %v", webThinkingEnv, c.val, got, c.want)
		}
	}
}
