package main

// cli_web_warning_test.go — the regression guard for the WEB carrier's
// `warning` frame: the operator-facing statement about the turn IN FLIGHT
// (today's only producer is context pressure, internal/agent/context_pressure.go).
//
// WHY THIS TEST EXISTS. The frame was added with the warn-only context policy
// and had no end-to-end proof: the only evidence was a constants-agreement test
// (`webEventWarning` == the page's `EV_WARNING`), which says the two sides spell
// the word the same way and nothing about whether a turn ever sends one. That is
// exactly the false green this file replaces — a wire name that agrees and a
// producer nobody exercised.
//
// WHAT IT PINS, end to end over the real handler:
//
//  1. a POSTed turn on a tiny context window produces a `warning` frame on that
//     turn's OWN SSE response (not the standing stream — this statement is about
//     the running turn, so it rides the turn's connection);
//  2. the frame's text carries the numbers the operator needs, and says that
//     NOTHING was removed — the whole point of the policy change;
//  3. the turn still ANSWERS: a warning is not a failure, and a page that
//     opened no answer bubble for it must still receive the answer normally.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
)

// webWarningTestWindow is the token window this test declares. It is far below
// any real chat prompt (the chat system prompt alone measured ~45k tokens on
// this tree), so the trigger is crossed on the FIRST turn without a fixture
// that fakes pressure — the same mechanism a real small-window run uses.
const webWarningTestWindow = 1000

// webWarningTestFill is how many words the turn's message carries. At
// ~4 chars/token this is ~2000 tokens against a 1000-token window, so the turn
// crosses the trigger by construction rather than by luck.
const webWarningTestFill = 2000

// TestWebWarningFrameCarriesContextPressure drives one real turn over the real
// handler and asserts the warning frame on the turn's own stream.
func TestWebWarningFrameCarriesContextPressure(t *testing.T) {
	mockSrv, _ := newWebNoticeRecorderServer(t)

	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore the close has no assertion riding on it

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
		// THE POLICY UNDER TEST: automatic reduction is OFF (the default), and
		// the window is small enough that this turn crosses the trigger.
		ContextWindow:     webWarningTestWindow,
		AutoReduceEnabled: false,
	}

	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}
	httpSrv := httptest.NewServer(ws.webChatHandler())
	t.Cleanup(httpSrv.Close)

	// THE PRESSURE COMES FROM THE MESSAGE ITSELF, not from a faked reading of
	// one: Context.EstimatedTokens counts the messages it holds, and a hand-built
	// agent has no system prompt in that field (the served prompt is assembled
	// per request), so a short prompt would sit under the trigger and the frame
	// would never be produced — a test that passed for the wrong reason.
	body, err := json.Marshal(webChatRequest{Message: strings.Repeat("pressure ", webWarningTestFill)})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	res, err := http.Post(httpSrv.URL+webChatPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", webChatPath, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", webChatPath, res.StatusCode)
	}

	// FACT 1: the frame arrives on the TURN's stream.
	warning := readEventFrame(t, res.Body, webEventWarning, webNoticeTestFrameTimeout)

	// FACT 2: it carries the numbers, and it says nothing was removed.
	for _, want := range []string{"context pressure", "NOTHING was removed"} {
		if !bytes.Contains([]byte(warning), []byte(want)) {
			t.Errorf("warning frame %q does not carry %q — the operator cannot act on it", warning, want)
		}
	}

	// FACT 3: this turn still reported itself as done, and the answer reached the
	// page — a warning is a statement beside the turn, never a replacement.
	done := readEventFrame(t, res.Body, webEventDone, webNoticeTestFrameTimeout)
	if !bytes.Contains([]byte(done), []byte(webNoticeTestAnswer)) {
		t.Errorf("done frame %q does not carry the answer %q", done, webNoticeTestAnswer)
	}
}
