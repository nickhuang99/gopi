package main

// R4 + R5 web-carrier integration tests (devdoc/web-align-p1.md):
//   - slash messages dispatch locally → one `event: command` frame, no model
//     request, no history mutation (the DSH "never sent to the model" rule);
//   - image turns validate + attach, switch to the vision model for the turn
//     and restore it after, and land the image block on the wire;
//   - oversize/unsupported images 4xx before any agent involvement;
//   - GET /api/transcript replays the server-owned context.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/tools"
)

// webServerForTestWithModel builds the standard test webServer and records
// every mock-request body + returns the server so tests can flip knobs.
func newRecordingAgent(t *testing.T, ws *webServer, record func(body []byte, model string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The Files API upload rides the SAME client as the turns (one
		// credential, one base URL), so the mock must answer it too —
		// otherwise every image turn would 502 inside the uploader and the
		// model-path assertions would never run. It is dispatched BEFORE the
		// body read: a multipart body can only be parsed once, and this
		// handler's own io.ReadAll would consume it first.
		if strings.Contains(r.URL.Path, "/files") {
			serveMockFileUpload(t, w, r)
			return
		}
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		var req struct {
			Model string `json:"model"`
		}
		json.Unmarshal(b, &req)
		record(b, req.Model)
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "the mock answer")
	}))
	t.Cleanup(srv.Close)
	ws.agent.Client = &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	ws.agent.Logger = log.New(io.Discard, "", 0)
	return srv
}

// serveMockFileUpload answers the Files API upload leg of the shared mock: it
// parses the multipart body (so a test can assert what was SENT) and replies
// with the documented Anthropic-shaped file object.
func serveMockFileUpload(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if err := r.ParseMultipartForm(mockUploadMemory); err != nil {
		http.Error(w, `{"error":"bad multipart"}`, http.StatusBadRequest)
		return
	}
	file, hdr, err := r.FormFile(deepseek.FilesAPIFileField)
	if err != nil {
		http.Error(w, `{"error":"no file part"}`, http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }() //stylegate:ignore closing a test upload part cannot fail usefully
	body, _ := io.ReadAll(file)         //stylegate:ignore bounded test read of a small fixture
	lastUploadMu.Lock()
	lastUpload = mockUpload{
		purpose:  r.FormValue(deepseek.FilesAPIPurposeKey),
		filename: hdr.Filename,
		mimeType: hdr.Header.Get("Content-Type"),
		bytes:    body,
		path:     r.URL.Path,
		beta:     r.Header.Get(deepseek.FilesAPIBetaHeader),
	}
	lastUploadMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	id := "file-api-mock"
	if hdr.Filename != "" {
		id = "file-api-mock-" + mockFileIDFor(hdr.Filename, body)
	}
	fmt.Fprintf(w, `{"id":%q,"type":"file","size_bytes":%d,"created_at":"2026-11-06T21:21:11.505657605+00:00","filename":%q,"mime_type":%q}`,
		id, len(body), hdr.Filename, hdr.Header.Get("Content-Type"))
}

// mockUpload is what the mock Files API endpoint saw on the LAST upload. A
// package-level record (guarded by a mutex) rather than a return value because
// the upload happens inside the handler, below the test's HTTP call.
type mockUpload struct {
	purpose  string
	filename string
	mimeType string
	bytes    []byte
	path     string
	beta     string
}

var (
	lastUploadMu sync.Mutex
	lastUpload   mockUpload
)

// resetMockUpload clears the recorded upload between turns.
func resetMockUpload() {
	lastUploadMu.Lock()
	lastUpload = mockUpload{}
	lastUploadMu.Unlock()
}

// mockUploadSeen returns the recorded upload.
func mockUploadSeen() mockUpload {
	lastUploadMu.Lock()
	defer lastUploadMu.Unlock()
	return lastUpload
}

// mockUploadMemory caps the mock endpoint's multipart buffer; the fixture
// pictures are bytes long, so a small cap keeps the test honest about size.
const mockUploadMemory = 1 << 20

// mockFileIDFor derives a stable per-content id so a test can prove the SAME
// picture maps to the SAME file_id across turns.
func mockFileIDFor(filename string, body []byte) string {
	sum := sha256.Sum256(append([]byte(filename), body...))
	return hex.EncodeToString(sum[:8])
}

func newPlainWebServer(t *testing.T) *webServer {
	t.Helper()
	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: "http://unused", APIKey: "k"},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0,
	}
	ag.Logger = log.New(io.Discard, "", 0)
	ag.PersistChatSession = true
	ws := &webServer{
		addr:        ":0",
		agent:       ag,
		startedAt:   time.Now(),
		visionModel: "glm-5.3-flash",
	}
	return ws
}

// TestWebChat_R4_SlashCommandNeverReachesModel is THE R4 web contract: a
// "/task list" POST returns exactly one event:command frame; the mock model
// server receives ZERO requests; s.history stays empty (command output is
// display, not conversation).
func TestWebChat_R4_SlashCommandNeverReachesModel(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	ws := newPlainWebServer(t)
	modelHits := 0
	newRecordingAgent(t, ws, func(body []byte, model string) { modelHits++ })

	body := postChat(t, ws, "/task list", nil)

	if got := strings.Count(body, "event: command"); got != 1 {
		t.Fatalf("got %d command frames, want 1:\n%s", got, body)
	}
	if strings.Contains(body, "event: delta") || strings.Contains(body, "event: done") {
		t.Errorf("slash turn leaked model-stream frames:\n%s", body)
	}
	if modelHits != 0 {
		t.Errorf("slash command hit the model %d time(s) — R4 violation", modelHits)
	}
	if len(ws.history) != 0 {
		t.Errorf("command output entered history (%d msgs) — R4 violation", len(ws.history))
	}

	// The frame payload carries the command and its output.
	var frame map[string]string
	for _, pair := range sseEvents(body) {
		if pair[0] == "command" {
			if err := json.Unmarshal([]byte(pair[1]), &frame); err != nil {
				t.Fatalf("command frame not JSON: %v", err)
			}
		}
	}
	if frame["command"] != "/task list" {
		t.Errorf("frame command = %q", frame["command"])
	}
	if frame["output"] == "" || frame["error"] != "" {
		t.Errorf("frame output/error = %q/%q, want list output, no error", frame["output"], frame["error"])
	}
}

// TestWebChat_R4_SlashUnknownCommandErrorFrame: bad commands stream a
// command frame carrying the error (uniform stream protocol), not HTTP 4xx,
// and still never touch the model.
func TestWebChat_R4_SlashUnknownCommandErrorFrame(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	ws := newPlainWebServer(t)
	newRecordingAgent(t, ws, func(body []byte, model string) {
		t.Error("unknown slash command reached the model")
	})

	body := postChat(t, ws, "/definitely-not-a-command", nil)
	if !strings.Contains(body, "event: command") {
		t.Fatalf("no command frame:\n%s", body)
	}
	if !strings.Contains(body, "unknown command") || !strings.Contains(body, "available") {
		t.Errorf("error frame lacks the diagnostics:\n%s", body)
	}
}

// TestWebChat_R4_SlashOutputNotInNextTurnContext: after a command turn, the
// next REAL turn's seed must not contain the command output (the two-turn
// continuity + isolation proof).
func TestWebChat_R4_SlashOutputNotInNextTurnContext(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	ws := newPlainWebServer(t)
	var lastBody []byte
	newRecordingAgent(t, ws, func(body []byte, model string) { lastBody = body })

	// /log is the surviving command whose output carries a fixed, unmistakable
	// header — `/memory list` was retired on 2026-11-07 and would now take the
	// unknown-command path, which is a DIFFERENT behaviour (and would make this
	// case pass for the wrong reason: no command output at all). The marker is
	// the table header, not the word "session": the latter appears in the system
	// prompt of every request and would fail for a reason that has nothing to do
	// with the isolation this case measures.
	// A fresh GOPI_HOME has no sessions, so /log last answers with this exact
	// sentence (formatRecentSessions) — a fixed marker that cannot be confused
	// with prompt text.
	const logHeaderMarker = "no sessions recorded yet"
	cmdBody := postChat(t, ws, "/log last", nil) // command turn — no model hit
	if !strings.Contains(cmdBody, logHeaderMarker) {
		t.Fatalf("the command turn printed no /log table — the case would measure nothing:\n%s", cmdBody)
	}
	postChat(t, ws, "now a real question", nil)

	if strings.Contains(string(lastBody), logHeaderMarker) {
		t.Errorf("command output leaked into the next model request:\n%s", lastBody)
	}
	if !strings.Contains(string(lastBody), "now a real question") {
		t.Errorf("real turn missing its user message:\n%s", lastBody)
	}
}

// TestWebChat_R4_TranscriptReplayAfterTextTurn locks the refresh-recovery
// contract: /api/transcript returns the messages the SERVER owns after a
// real turn (user + assistant).
func TestWebChat_R4_TranscriptReplayAfterTextTurn(t *testing.T) {
	ws := newPlainWebServer(t)
	newRecordingAgent(t, ws, func(body []byte, model string) {})

	postChat(t, ws, "hello replay", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/transcript", nil)
	rec := httptest.NewRecorder()
	ws.handleTranscript(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("transcript status %d", rec.Code)
	}
	var got struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("transcript not JSON: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "user" || got.Messages[1].Role != "assistant" {
		t.Errorf("transcript = %+v, want user+assistant", got.Messages)
	}
}

// TestWebChat_R5_ImageTurnAttachesAndSwitchesModel: an images-bearing turn
// puts the base64 payload + media_type on the wire inside the turn's user
// message, uses the vision model for THAT turn, and restores the default
// model afterwards.
func TestWebChat_R5_ImageTurnAttachesAndSwitchesModel(t *testing.T) {
	ws := newPlainWebServer(t)
	pngB64 := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	var wireModel string
	var wireHasImage bool
	newRecordingAgent(t, ws, func(body []byte, model string) {
		wireModel = model
		wireHasImage = strings.Contains(string(body), `"type": *"image"`) || strings.Contains(string(body), `"type":"image"`) || strings.Contains(string(body), pngB64)
	})

	raw := `{"message":"read the picture","images":[{"media_type":"image/png","data_b64":"` + pngB64 + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(raw))
	rec := httptest.NewRecorder()
	ws.handleChat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("image turn status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !wireHasImage {
		t.Error("image bytes never reached the model wire")
	}
	if wireModel != "glm-5.3-flash" {
		t.Errorf("image turn model = %q, want the vision model", wireModel)
	}
	if ws.agent.Model != "deepseek-v4-pro" {
		t.Errorf("model not restored after image turn: %q", ws.agent.Model)
	}

	// History capture: the image rides in the persisted transcript as a FILE
	// REFERENCE, not as pixels (marked has_image on replay). This is stricter
	// than the base64 assertion it replaces, and the strictness is the epic's
	// whole point: history is the SEED for every later turn, so a base64 blob
	// kept here would be re-inlined into every following Messages request — the
	// transport this change removes. Asserted BOTH ways: the reference is there
	// and the bytes are not.
	if len(ws.history) != 2 {
		t.Fatalf("history = %d msgs, want 2", len(ws.history))
	}
	if len(ws.history[0].Images) == 0 {
		t.Fatalf("history[0] lost the image: %+v", ws.history[0])
	}
	histImg := ws.history[0].Images[0]
	if histImg.Type != deepseek.ClaudeSourceTypeFileAPI || histImg.FileID == "" {
		t.Errorf("history[0].Images[0] = %+v, want a Files API reference (type %q + file_id)", histImg, deepseek.ClaudeSourceTypeFileAPI)
	}
	if histImg.Data != "" {
		t.Errorf("history[0].Images[0] still carries %d bytes of base64 — the seed would re-inline it next turn", len(histImg.Data))
	}

	// /api/transcript marks the image turn WITHOUT re-shipping pixels.
	req2 := httptest.NewRequest(http.MethodGet, "/api/transcript", nil)
	rec2 := httptest.NewRecorder()
	ws.handleTranscript(rec2, req2)
	tr := rec2.Body.String()
	if !strings.Contains(tr, `"has_image":true`) {
		t.Errorf("transcript lost the image marker: %s", tr)
	}
	if strings.Contains(tr, pngB64) {
		t.Error("transcript re-shipped raw pixels — replay must stay light")
	}
}

// TestWebChat_R5_ImageValidation4xx: unsupported media type, broken base64
// and >32MiB images are rejected with 400 BEFORE the agent runs.
func TestWebChat_R5_ImageValidation4xx(t *testing.T) {
	ws := newPlainWebServer(t)
	newRecordingAgent(t, ws, func(body []byte, model string) {
		t.Error("invalid image turn reached the model")
	})

	huge := base64.StdEncoding.EncodeToString(make([]byte, 33<<20))  // 33 MiB decoded
	bigB64 := base64.StdEncoding.EncodeToString(make([]byte, 3<<20)) // 3 MiB decoded — fits the 4MiB body
	cases := []struct {
		name, body string
	}{
		{"bad media type", `{"message":"x","images":[{"media_type":"image/tiff","data_b64":"aGk="}]}`},
		{"broken base64", `{"message":"x","images":[{"media_type":"image/png","data_b64":"!!!not-base64!!!"}]}`},
		// NOTE: a 33MiB DECODED image base64-encodes to ~44MiB — over the
		// request-body cap, so the decode fails at JSON level (still 4xx).
		// That cap is derived from the same 32MiB ceiling
		// (webMaxChatBodyBytes), which is why the 33MiB payload is refused by
		// the reader and not only by validateWebImages; the count-cap case
		// below exercises validateWebImages directly for the engine-level
		// rejection.
		{"oversize body", `{"message":"x","images":[{"media_type":"image/png","data_b64":"` + huge + `"}]}`},
		{"too many", `{"message":"x","images":[{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="},{"media_type":"image/png","data_b64":"aGk="}]}`},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		ws.handleChat(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body: %s)", tc.name, rec.Code, rec.Body.String())
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty 400 body", tc.name)
		}
	}
	_ = bigB64

	// Engine-level oversize (the body cap removed from the picture): a
	// 33MiB decoded image fails validateWebImages with the size message.
	_, err := validateWebImages([]webChatImage{{MediaType: "image/png", DataB64: huge}})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("validateWebImages oversize error = %v, want 'too large'", err)
	}
}

// TestWebChat_R5_NoVisionConfiguredStillTurnsWhenModelSet: a server with an
// empty visionModel must not crash a text turn (image turns degrade to the
// configured main model — the settings layer decides the vision name).
func TestWebChat_R5_TextTurnWithEmptyVisionModel(t *testing.T) {
	ws := newPlainWebServer(t)
	ws.visionModel = ""
	var gotModel string
	newRecordingAgent(t, ws, func(body []byte, model string) { gotModel = model })
	postChat(t, ws, "plain question", nil)
	if gotModel != "deepseek-v4-pro" {
		t.Errorf("text turn model = %q, want the default", gotModel)
	}
}

// TestWebChat_R4_ChatREPLSlashRouting is the chat-side integration: runChat
// executes table commands from stdin without a model turn. Driven via the
// shared table (the REPL calls execSlashCommand on every /-prefixed line
// that is not a session command) — the routing decision under test is
// isSlashCommand + the execSlashCommand wiring.
func TestWebChat_R4_ChatREPLSlashRouting(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	// M4.5 ④: /task new is REMOVED — the shared-table routing still fires
	// (proven by the pointed removal error), and enqueueing goes through
	// tools.TaskSubmit at the engine level.
	_, err := execSlashCommand("/task new \"repl routed task\"", CommandContext{})
	if err == nil || !strings.Contains(err.Error(), "removed (M4.5)") {
		t.Fatalf("/task new must be refused via the shared table, got err=%v", err)
	}
	_ = err //stylegate:ignore the error is already reported by the caller's checked call; this site only reaps or logs the leftover
	// The same input through the web dispatch yields the same engine result
	// (the two-carriers-one-table guarantee).
	ws := newPlainWebServer(t)
	newRecordingAgent(t, ws, func(body []byte, model string) {
		t.Error("chat-side command reached the model")
	})
	// M4.5 ④: enqueue at the engine level (submit), the removal refusal
	// above already proved the shared-table routing.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "repl routed task", TaskID: "t-repl-1", Source: "human",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	body := postChat(t, ws, "/task list", nil)
	if !strings.Contains(body, "repl routed task") {
		t.Errorf("web command frame lost the task created via the shared table:\n%s", body)
	}
}

// helperWebUsage renders the `gopi web` usage text exactly as runWeb prints
// it (`-h`). One place builds the argument list, so a usage-text test can
// never drift from the flag-help a user actually sees.
func helperWebUsage() string {
	return fmt.Sprintf(webUsage,
		webAddrFlag, webDefaultAddrValue, webAddrFlag, webDefaultPort, webTokenQueryKey, webAddrFlag,
		webThinkingFlag, webThinkingEnv,
		webAddrFlag, webDefaultPort, webAddrFlag, webTokenQueryKey, webDefaultPort,
		webTokenEnv, webLocalTrustEnvName, webLocalTrustOffValue,
		webMaxImagesPerRequest)
}

// TestWebUsagePrintsNewFlags: the help hard-gate — `gopi web -h` documents
// the slash commands, images and vision-model behavior.
func TestWebUsagePrintsNewFlags(t *testing.T) {
	out := helperWebUsage()
	for _, want := range []string{"Slash commands", "attach up to", "vision", "32 MiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("gopi web -h missing %q:\n%s", want, out)
		}
	}
}
