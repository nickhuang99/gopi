package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// TestToolResultCapEndToEndThroughTheWire is Rule 31's proof, not a unit test:
// a REAL bash tool call produces a genuinely huge output (`seq 1 200000`), and
// the SECOND request the loop builds — the one that carries that result to the
// API — is inspected: the tool_result block on the wire must carry the cap note
// and must not carry the raw output.
func TestToolResultCapEndToEndThroughTheWire(t *testing.T) {
	const cmd = "seq 1 200000"

	var sawMarker bool
	var wireChars int
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		switch calls {
		case 1:
			claudeToolUseStream(w, "bash", map[string]interface{}{"command": cmd})
		case 2:
			for _, m := range req.Messages {
				for _, b := range m.Content {
					if b.Type != "tool_result" {
						continue
					}
					body := fmt.Sprint(b.Content)
					wireChars = len(body)
					if strings.Contains(body, "tool result capped at") {
						sawMarker = true
						t.Logf("WIRE tool_result: %d chars on the wire, cap note PRESENT", len(body))
						t.Logf("WIRE note: %s", capFindMarker(body))
					} else {
						t.Logf("WIRE tool_result: %d chars, NO cap note", len(body))
					}
				}
			}
			claudeTextStream(w, "done", "end_turn")
		default:
			claudeTextStream(w, "all done", "end_turn")
		}
	}))
	defer srv.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    srv.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}
	ag.ContextWindow = DefaultContextWindow

	if _, err := ag.Run("print a lot of output"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls < 2 {
		t.Fatalf("the tool round never happened (calls=%d) — the test proves nothing", calls)
	}
	if !sawMarker {
		t.Fatalf("the tool_result on the wire carries no cap note (%d chars) — the cap did not reach the API", wireChars)
	}
	if wireChars > ToolResultTokenCap*4 {
		t.Fatalf("the wire carried %d chars, ~%d tokens — above the %d-token cap",
			wireChars, wireChars/3, ToolResultTokenCap)
	}
	t.Logf("wire chars=%d (~%d tokens), cap=%d", wireChars, wireChars/3, ToolResultTokenCap)

	var toolMsg string
	for _, m := range ag.Ctx.Messages {
		if m.Role == "tool" {
			toolMsg = m.Content
		}
	}
	if toolMsg == "" {
		t.Fatal("no tool message in the context")
	}
	t.Logf("context tool message: %d chars, %d tokens; CalibratedTokens()=%d",
		len(toolMsg), countTokens(toolMsg), ag.Ctx.CalibratedTokens())
	if n := countTokens(toolMsg); n > ToolResultTokenCap {
		t.Fatalf("stored tool message is %d tokens, above the cap %d", n, ToolResultTokenCap)
	}
}

// capFindMarker returns the cap note from a wire body, for the log.
func capFindMarker(body string) string {
	i := strings.Index(body, "tool result capped at")
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.IndexByte(rest, ']'); j >= 0 {
		return rest[:j+1]
	}
	return rest
}
