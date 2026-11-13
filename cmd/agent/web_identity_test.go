// web_identity_test.go — the identity surface: the engine answers "which
// session/model/tools am I driving?" over the GATED wire, so a remote browser
// (or `gopi ui --server`) can identify what it reached instead of guessing from
// the URL.
//
// The load-bearing properties pinned here:
//   - the route is behind the SAME access gate as every other route (a LAN bind
//     refuses it unauthenticated — it is not an information leak);
//   - the payload names the run's session id and the model actually serving
//     turns, plus the tools posture and the bind;
//   - the TUI's own consumer (webFetchIdentity) reads what the route writes,
//     over a real HTTP server — the two sides cannot drift;
//   - the page carries the route constant (the page cannot import Go constants).
package main

import (
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

// identityTestServer builds a webServer with a REAL session logger (so the
// session id is a real id, not a placeholder) and a live httptest server, and
// returns both.
func identityTestServer(t *testing.T) (*webServer, *httptest.Server) {
	t.Helper()
	sl, err := agent.NewSessionLogger(webIdentityTestModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	//stylegate:ignore closes the fixture session log after its flush
	t.Cleanup(func() { sl.Close() })
	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: "http://127.0.0.1:1", APIKey: "k", HTTPClient: http.DefaultClient},
		Ctx:        &agent.Context{},
		Model:      webIdentityTestModel,
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	srv := &webServer{addr: "127.0.0.1:8090", agent: ag, startedAt: time.Now()}
	return srv, httptest.NewServer(srv.webChatHandler())
}

// webIdentityTestModel is the model name these tests build agents with. It is
// deliberately NOT the production default: the assertion below must prove the
// payload reports the agent's OWN model, not a re-resolved default.
const webIdentityTestModel = "identity-test-model"

// TestWebIdentity_RouteIsGated is the red proof that the identity route adds no
// hole: the SAME request is 200 through the bare mux and 401 through the gated
// one, and 200 again with the token. On a LAN bind the identity of the agent
// (who is driving it, with which model, from where) is exactly what must not
// leak to an unauthenticated peer.
func TestWebIdentity_RouteIsGated(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	srv := &webServer{}

	// Control half: without the gate the route answers — so the 401 below is
	// caused by the gate, not by the handler refusing.
	rec := httptest.NewRecorder()
	srv.webChatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("control failed: ungated GET %s = %d, want 200 (the gate must be what refuses)", webIdentityPath, rec.Code)
	}

	gated := webTokenGate(token, srv.webChatHandler(), false)
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("RED-PROOF FAILED: unauthenticated GET %s = %d, want 401 — a LAN bind would hand a stranger this engine's identity", webIdentityPath, rec.Code)
	}

	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath+"?"+webTokenPair(token), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("token-bearing GET %s = %d, want 200", webIdentityPath, rec.Code)
	}

	// The loopback posture is unchanged: no token configured, no gate.
	rec = httptest.NewRecorder()
	webTokenGate("", srv.webChatHandler(), false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback (empty token) GET %s = %d, want 200", webIdentityPath, rec.Code)
	}
}

// TestWebIdentity_NamesTheSessionAndModel: the payload carries THIS run's
// session id (the id `gopi session list` shows and finalizeChatSession closes)
// and the model that serves turns — the two facts the briefing asks for.
func TestWebIdentity_NamesTheSessionAndModel(t *testing.T) {
	srv, ts := identityTestServer(t)
	defer ts.Close()
	wantSession := srv.agent.SessionLog.SessionID()

	rec := httptest.NewRecorder()
	srv.webChatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", webIdentityPath, rec.Code)
	}
	var got webIdentityPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("identity body is not the payload: %v (%s)", err, rec.Body.String())
	}
	if got.SessionID != wantSession {
		t.Errorf("session_id = %q, want the run's own session id %q", got.SessionID, wantSession)
	}
	if got.Model != webIdentityTestModel {
		t.Errorf("model = %q, want the agent's own model %q (a re-resolved default would be the bug)", got.Model, webIdentityTestModel)
	}
	if got.Addr != srv.addr {
		t.Errorf("addr = %q, want %q", got.Addr, srv.addr)
	}
	if got.Started == "" {
		t.Error("started is empty — a client cannot tell a restarted engine from the one it was told about")
	}

	// An image turn swaps the agent's model for the turn's duration: the route
	// must report what is SERVING, not a startup-time snapshot.
	srv.visionModel = "identity-vision-model"
	srv.agent.Model = srv.visionModel
	var during webIdentityPayload
	rec = httptest.NewRecorder()
	srv.webChatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath, nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &during); err != nil {
		t.Fatalf("identity body: %v", err)
	}
	if during.Model != srv.visionModel || during.VisionModel != srv.visionModel {
		t.Errorf("during an image turn model=%q vision_model=%q, want both %q", during.Model, during.VisionModel, srv.visionModel)
	}
}

// TestWebIdentity_ReportsTheSessionCounters: an EMPTY session and a live
// conversation must be distinguishable — the single-session design means a
// remote browser cannot otherwise tell whether the transcript it sees is the
// one it is contributing to.
func TestWebIdentity_ReportsTheSessionCounters(t *testing.T) {
	srv, ts := identityTestServer(t)
	defer ts.Close()

	var empty webIdentityPayload
	rec := httptest.NewRecorder()
	srv.webChatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath, nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("identity body: %v", err)
	}
	if empty.Messages != 0 || empty.Turns != 0 {
		t.Fatalf("fresh engine reports messages=%d turns=%d, want 0/0", empty.Messages, empty.Turns)
	}

	srv.mu.Lock()
	srv.history = []deepseek.Message{{Role: "user", Content: "hi"}}
	srv.iterCount, srv.totalTokens = 2, 42
	srv.mu.Unlock()

	var live webIdentityPayload
	rec = httptest.NewRecorder()
	srv.webChatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, webIdentityPath, nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
		t.Fatalf("identity body: %v", err)
	}
	if live.Messages != 1 || live.Turns != 2 || live.Tokens != 42 {
		t.Errorf("live engine reports messages=%d turns=%d tokens=%d, want 1/2/42", live.Messages, live.Turns, live.Tokens)
	}
}

// TestWebIdentity_MethodIsGet: the route is read-only. A POST must be refused
// rather than silently behaving like a GET (Rule 15 — no silent success).
func TestWebIdentity_MethodIsGet(t *testing.T) {
	srv := &webServer{}
	rec := httptest.NewRecorder()
	srv.webChatHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, webIdentityPath, strings.NewReader("{}")))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s = %d, want 405", webIdentityPath, rec.Code)
	}
}

// TestWebIdentity_FetchCrossesTheRealWire: the CONSUMER (`gopi ui --server`)
// reads what the ROUTE writes, over a real HTTP server with a real token —
// the two sides of the wire are tested against each other, not against a
// hand-written fixture.
func TestWebIdentity_FetchCrossesTheRealWire(t *testing.T) {
	const token = "feedfacefeedfacefeedfacefeedface"
	srv, ts := identityTestServer(t)
	defer ts.Close()
	gated := httptest.NewServer(webTokenGate(token, srv.webChatHandler(), false))
	defer gated.Close()

	got, err := webFetchIdentity(gated.URL, token)
	if err != nil {
		t.Fatalf("webFetchIdentity with the token: %v", err)
	}
	if got.SessionID != srv.agent.SessionLog.SessionID() || got.Model != webIdentityTestModel {
		t.Fatalf("fetched %+v, want session %q / model %q", got, srv.agent.SessionLog.SessionID(), webIdentityTestModel)
	}

	// Without the token the fetch reports the refusal instead of returning an
	// empty identity a caller might print as fact (Rule 15).
	if _, err := webFetchIdentity(gated.URL, ""); err == nil {
		t.Fatal("webFetchIdentity without a token succeeded against a gated bind — the refusal must be reported")
	}

	// A peer that is not serving this API is reported, never guessed.
	if _, err := webFetchIdentity("http://127.0.0.1:1", ""); err == nil {
		t.Fatal("webFetchIdentity against a dead port succeeded — a connection failure must be reported")
	}
}

// TestWebIdentityNoticeRendersEveryField: the terminal carriers print ONE line,
// and an unreported field reads "unknown" rather than vanishing (an empty value
// reads like a normal one — Rule 15).
func TestWebIdentityNoticeRendersEveryField(t *testing.T) {
	line := webIdentityNotice(webIdentityPayload{SessionID: "s-1", Model: "m-1", Tools: true, Addr: "127.0.0.1:8090"})
	for _, want := range []string{"session=s-1", "model=m-1", "tools=true", "addr=127.0.0.1:8090"} {
		if !strings.Contains(line, want) {
			t.Errorf("notice %q missing %q", line, want)
		}
	}
	line = webIdentityNotice(webIdentityPayload{})
	for _, want := range []string{"session=" + webIdentityUnknown, "model=" + webIdentityUnknown, "addr=" + webIdentityUnknown} {
		if !strings.Contains(line, want) {
			t.Errorf("notice %q missing the explicit unknown %q", line, want)
		}
	}
	if got := webStartedOrUnknown(webIdentityPayload{}); got != webIdentityUnknown {
		t.Errorf("webStartedOrUnknown(empty) = %q, want %q", got, webIdentityUnknown)
	}
	if got := webStartedOrUnknown(webIdentityPayload{Started: "2026-01-02T03:04:05Z"}); got != "2026-01-02T03:04:05Z" {
		t.Errorf("webStartedOrUnknown = %q, want the instant back", got)
	}
}

// TestWebPageCarriesTheIdentityRoute: the page's fetch target and the route the
// mux registers are ONE value (the page cannot import Go constants — this test
// is the only thing standing between a rename and a silent desync).
func TestWebPageCarriesTheIdentityRoute(t *testing.T) {
	script := pageScript(t)
	i := strings.Index(script, "const API_IDENTITY=")
	if i < 0 {
		t.Fatalf("the page no longer declares API_IDENTITY — the browser cannot ask the engine who it is")
	}
	line := script[i:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	if !strings.Contains(line, "'"+webIdentityPath+"'") {
		t.Fatalf("the page declares %q but the server route is %q", line, webIdentityPath)
	}
	if !strings.Contains(script, "identifyEngine()") {
		t.Fatal("the page never calls identifyEngine() — the route exists but nothing renders it")
	}
}
