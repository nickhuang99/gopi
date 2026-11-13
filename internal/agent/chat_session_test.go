package agent

import (
	"encoding/json"
	"github.com/nick/gopi/internal/sessionevent"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// singleSessionAgent builds an Agent with a real SessionLogger + RawLogger so
// the persistent-session (single session id / single log / append) behavior
// can be verified end-to-end. The session/raw dirs are redirected into a temp
// GOPI_HOME so the test never touches the user's real ~/.gopi.
func singleSessionAgent(t *testing.T, server *httptest.Server) *Agent {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GOPI_HOME", dir)

	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	rl, err := NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	t.Cleanup(func() {
		sl.Close() //stylegate:ignore test cleanup: SessionLogger.Close flushes and fsyncs the JSONL; a flush error at teardown has no assertion left to fail and the content is read by the test body
		rl.Close() //stylegate:ignore test cleanup: RawLogger.Close flushes and fsyncs the audit log; best-effort by design, nothing downstream depends on this error
	})

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:        &Context{},
		Model:      "deepseek-v4-pro",
		SessionLog: sl,
		RawLog:     rl,
		Logger:     log.New(io.Discard, "", 0),
	}
	return ag
}

// TestChat_PersistentSession_SingleIDAcrossTurns verifies the Epic's core
// ask: with PersistChatSession, gopi chat uses ONE session id and ONE session
// log file, appending each turn's events to the same JSONL instead of
// spawning a fresh session per turn. It verifies (1) the SAME session id is
// logged before and after two turns, (2) session_start + both turns' user/
// assistant events share one file, (3) no per-turn session_end is written,
// and (4) CloseChatSession writes exactly ONE session_end at the end.
func TestChat_PersistentSession_SingleIDAcrossTurns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"answer"}, "end_turn")
	}))
	defer server.Close()

	ag := singleSessionAgent(t, server)
	ag.PersistChatSession = true
	sessionPath := ag.SessionLog.Path()
	sessionID := ag.SessionLog.SessionID()

	if _, err := ag.Chat("turn one", nil); err != nil {
		t.Fatalf("Chat(turn one): %v", err)
	}
	if _, err := ag.Chat("turn two", ag.Ctx.Messages); err != nil {
		t.Fatalf("Chat(turn two): %v", err)
	}

	// The session must still be OPEN (no close happened after the turns) with
	// the SAME id + path.
	if ag.SessionLog == nil || ag.SessionLog.SessionID() != sessionID {
		t.Fatalf("session id changed across turns: got %v, want %s",
			map[bool]string{true: "nil", false: "not-nil"}[ag.SessionLog == nil], sessionID)
	}
	if ag.SessionLog.Path() != sessionPath {
		t.Errorf("session path changed across turns: got %s, want %s", ag.SessionLog.Path(), sessionPath)
	}

	// Read the single session file: one session_start, two user turns, two
	// assistant turns, but NO per-turn session_end (only the final one after
	// CloseChatSession).
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	lines := 0
	var starts, users, assistants, ends int
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		lines++
		var ev map[string]interface{}
		if json.Unmarshal([]byte(ln), &ev) != nil {
			continue
		}
		switch ev["type"] {
		case "session_start":
			starts++
		case "user":
			users++
		case "assistant":
			assistants++
		case "session_end":
			ends++
		}
	}
	if starts != 1 {
		t.Errorf("session_start count = %d, want 1 (ONE session, not one per turn)", starts)
	}
	if users != 2 || assistants != 2 {
		t.Errorf("user/assistant events = %d/%d, want 2/2 (both turns appended to one file)", users, assistants)
	}
	if ends != 0 {
		t.Errorf("per-turn session_end count = %d, want 0 before CloseChatSession", ends)
	}

	// CloseChatSession: ONE session_end on the SAME file.
	ag.CloseChatSession(0, 2, 0, 1.0, EndCause{Reason: sessionevent.SessionEndReasonComplete})
	data2, _ := os.ReadFile(sessionPath) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	ends2 := 0
	for _, ln := range strings.Split(string(data2), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]interface{}
		if json.Unmarshal([]byte(ln), &ev) == nil && ev["type"] == "session_end" {
			ends2++
		}
	}
	if ends2 != 1 {
		t.Errorf("session_end count after CloseChatSession = %d, want exactly 1 (single-session close)", ends2)
	}
}

// TestChat_Default_PerTurnClose verifies the OUT-OF-THE-BOX behavior stays the
// same: without PersistChatSession, each Chat() turn closes its own session
// (writes its own session_end) — the original fresh-session-per-turn flow,
// so nothing that relies on it regresses.
func TestChat_Default_PerTurnClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"ok"}, "end_turn")
	}))
	defer server.Close()

	ag := singleSessionAgent(t, server)
	if ag.PersistChatSession {
		t.Fatal("default PersistChatSession should be false")
	}
	path := ag.SessionLog.Path()
	if _, err := ag.Chat("hi", nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	data, _ := os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	ends := 0
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]interface{}
		if json.Unmarshal([]byte(ln), &ev) == nil && ev["type"] == "session_end" {
			ends++
		}
	}
	if ends != 1 {
		t.Errorf("non-persistent Chat session_end count = %d, want 1 (default closes per turn)", ends)
	}
}
