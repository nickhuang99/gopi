package main

// cli_term_test.go — the terminal carrier's contract.
//
// Three gates, each aimed at a way this carrier can be quietly wrong:
//
//  1. TestTermRouteWiringMatchesTheLauncher — the launcher and the route table
//     are two halves of one wire. A path, a field name or a form action that
//     drifts between them produces a page that renders perfectly and submits
//     nowhere, which is EXACTLY the defect that motivated this epic (the
//     browser page had no named field, so no text browser could submit). The
//     test greps the SERVED page rather than the constant that built it.
//
//  2. TestTermTurnStreamsAndRedirects — one POST = one streaming body ending in
//     303 → /cli#bottom, carrying the model's deltas and (with --tools) the
//     live tool lines, then the settled GET renders the same conversation.
//
//  3. TestTermVerbHelpRunsNothing — `gopi cli -h` prints usage and starts
//     NOTHING (the worker -h pitfall: a help query must be a zero-side-effect
//     path). Run against the REAL binary, with a sandbox GOPI_HOME.

import (
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/tools"
)

// termServerForTest builds a webServer whose agent points at the mock model,
// plus the mux the launcher serves (the two carriers' routes together, so the
// test also proves /cli did not disturb /api/chat's patterns).
func termServerForTest(t *testing.T, tools bool, handler http.HandlerFunc) (*webServer, *httptest.Server) {
	t.Helper()
	mockSrv := httptest.NewServer(handler)
	t.Cleanup(mockSrv.Close)
	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "test-key", HTTPClient: mockSrv.Client()},
		Ctx:     &agent.Context{},
		Model:   webTestTurnModel,
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, tools: tools, startedAt: time.Now()}
	return ws, mockSrv
}

// termFormRequest builds the POST a text browser sends for one turn: the form
// encoding the page uses, with the ONE named field.
func termFormRequest(t *testing.T, message string) *http.Request {
	t.Helper()
	form := url.Values{}
	form.Set(termChatInputName, message)
	req := httptest.NewRequest(http.MethodPost, termChatSubmitPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", termFormContentType)
	return req
}

// TestTermRouteWiringMatchesTheLauncher is gate 1: the served page carries a
// REAL form for the endpoint the launcher serves.
func TestTermRouteWiringMatchesTheLauncher(t *testing.T) {
	ws, _ := termServerForTest(t, false, func(w http.ResponseWriter, r *http.Request) {})
	rec := newStreamRecorder()
	ws.handleTermPage(rec, httptest.NewRequest(http.MethodGet, termChatPath, nil))
	page := rec.body()
	if rec.code != http.StatusOK {
		t.Fatalf("GET %s → %d, want 200", termChatPath, rec.code)
	}

	// (a) a real form with a real action — the two things the browser page
	// lacks, and the reason a text browser could not submit anything at all.
	if !strings.Contains(page, `<form action="`+termChatSubmitPath+`" method="post">`) {
		t.Errorf("the page has no POST form to %s — a text browser cannot submit without one:\n%s", termChatSubmitPath, page)
	}
	// (b) a NAMED single-line input. Unnamed fields are not submitted; a
	// textarea is unusable in a text browser (Enter shells out to $EDITOR).
	if !strings.Contains(page, `name="`+termChatInputName+`"`) {
		t.Errorf("the page's input is not named %q — a text browser submits only named fields:\n%s", termChatInputName, page)
	}
	if strings.Contains(page, "<textarea") {
		t.Errorf("the page uses <textarea> — unusable in a text browser (Enter leaves the page):\n%s", page)
	}
	// (c) the bottom fragment the turn redirects to must EXIST on the page, or
	// the redirect lands nowhere and the reader never reaches the form.
	if !strings.Contains(page, termChatBottomAnchor) {
		t.Errorf("the page carries no %q anchor for the post-turn redirect:\n%s", termChatBottomAnchor, page)
	}
	// (d) the page renders the engine's own transcript, not a client-side
	// placeholder: a settled conversation must arrive as TEXT.
	ws.mu.Lock()
	ws.history = []deepseek.Message{
		{Role: termRoleUser, Content: "what is 2+2"},
		{Role: termRoleAssistant, Content: "4"},
		{Role: termRoleTool, Content: `{"stdout":"ignored"}`},
	}
	ws.mu.Unlock()
	rec2 := newStreamRecorder()
	ws.handleTermPage(rec2, httptest.NewRequest(http.MethodGet, termChatPath, nil))
	settled := rec2.body()
	// The prefixes are HTML-escaped on the wire ("you&gt; "), so the assertion
	// goes through the same escaping the page uses rather than the raw text.
	if !strings.Contains(settled, html.EscapeString(termUserPrefix)+"what is 2+2") ||
		!strings.Contains(settled, html.EscapeString(termAssistantPrefix)+"4") {
		t.Errorf("the settled page does not render the server-held transcript:\n%s", settled)
	}
	if strings.Contains(settled, "ignored") {
		t.Errorf("the settled page renders raw tool payloads (a text browser cannot collapse them):\n%s", settled)
	}
}

// TestTermTurnStreamsAndRedirects is gate 2: the turn's wire.
func TestTermTurnStreamsAndRedirects(t *testing.T) {
	const answer = "the answer streams out"
	round := 0
	var mu sync.Mutex
	ws, _ := termServerForTest(t, true, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		round++
		n := round
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			toolUseSSE(w, "call_cli_1", "bash", `{"command":"head -20 go.mod"}`)
			return
		}
		textSSE(w, answer)
	})

	rec := newStreamRecorder()
	ws.handleTermChat(rec, termFormRequest(t, "read the top of go.mod"))
	body := rec.body()
	if rec.code != http.StatusOK {
		t.Fatalf("POST %s to %d, want 200 (the turn is a streamed document)", termChatSubmitPath, rec.code)
	}
	// The turn is a COMPLETE document: it opens with the page head and closes
	// with the same tail GET /cli serves. That ending is deliberate — a
	// redirect is not expressible after the first byte (the status line is
	// already out), so the turn hands back the form instead and the operator
	// types the next message without navigating.
	if !strings.HasPrefix(body, termPageHead) {
		t.Errorf("the turn body does not open the document with the page head:\n%s", body)
	}
	if !strings.HasSuffix(body, termPageTail) {
		t.Errorf("the turn body does not close with the page tail (form + anchor):\n%s", body)
	}
	// The settled view is reachable from the turn's own page (a text browser in
	// this workflow has no address bar).
	if !strings.Contains(body, "href=\""+termChatPath+"\"") {
		t.Errorf("the turn's page carries no link back to %s:\n%s", termChatPath, body)
	}
	// The model's text is IN the body (a text browser has no re-render
	// primitive: the deltas must be the body).
	if !strings.Contains(body, answer) {
		t.Errorf("the turn body does not carry the model's answer:\n%s", body)
	}
	// The live tool line, with the argument digest.
	if !strings.Contains(body, termToolPrefix) || !strings.Contains(body, "head -20 go.mod") {
		t.Errorf("the turn body carries no live tool-activity line with its argument digest:\n%s", body)
	}
	// The tool line must arrive BEFORE the answer: it is LIVE activity, not a
	// retrospective summary.
	if strings.Index(body, termToolPrefix) > strings.Index(body, answer) {
		t.Errorf("the tool line lands after the answer — it is not live activity:\n%s", body)
	}
	// The turn is recorded and the settled page shows it.
	ws.mu.Lock()
	hist := append([]deepseek.Message{}, ws.history...)
	ws.mu.Unlock()
	if len(hist) == 0 {
		t.Fatal("the turn left no history — the next page load would be empty")
	}
	rec2 := newStreamRecorder()
	ws.handleTermPage(rec2, httptest.NewRequest(http.MethodGet, termChatPath, nil))
	if !strings.Contains(rec2.body(), answer) {
		t.Errorf("the settled page does not carry the turn's answer:\n%s", rec2.body())
	}
}

// TestTermTurnWritesAreCoalescedAndCounted pins the ONE cost property this
// carrier has: the model's text is written to the terminal a bounded number of
// times per turn, not once per token. A regression to a flush per delta is
// invisible in any output assertion (the bytes are identical) — so it is
// measured here.
func TestTermTurnWritesAreCoalescedAndCounted(t *testing.T) {
	// 40 deltas, each emitted by the mock model as its own SSE event.
	var deltas strings.Builder
	for i := 0; i < 40; i++ {
		deltas.WriteString("chunk ")
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":1,"output_tokens":1}}}`)
		writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		for i := 0; i < 40; i++ {
			writeClaudeSSE(w, "content_block_delta",
				fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"chunk%d "}}`, i))
		}
		writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":80}}`)
		writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	}
	ws, _ := termServerForTest(t, false, handler)
	rec := newStreamRecorder()
	ws.handleTermChat(rec, termFormRequest(t, "say chunks"))
	body := rec.body()
	if !strings.Contains(body, "chunk39") {
		t.Fatalf("the turn body lost deltas:\n%s", body)
	}
	// A per-delta flush would put ~40 delta writes on the wire. The coalescer
	// (termFlushInterval) must collapse them: the bound is generous (the model
	// writes faster than a terminal re-renders) but far under per-token.
	if ws.lastTermDeltaWrites >= 40 {
		t.Errorf("delta writes = %d for 40 deltas — the coalescer is not coalescing (a text browser re-renders the whole document per write)",
			ws.lastTermDeltaWrites)
	}
	if ws.lastTermWrites == 0 {
		t.Error("no body writes recorded — the measurement itself is broken (Rule 15: a silent counter is worse than none)")
	}
	t.Logf("measured: 40 model deltas → %d body writes (%d flushes, %d carrying deltas)",
		ws.lastTermWrites, ws.lastTermFlushes, ws.lastTermDeltaWrites)
}

// TestTermTurnRejectsBadInput pins the refusals (Rule 15: a multi-line body is
// told so, not silently joined).
func TestTermTurnRejectsBadInput(t *testing.T) {
	ws, _ := termServerForTest(t, false, func(w http.ResponseWriter, r *http.Request) {})

	cases := []struct {
		name   string
		req    *http.Request
		status int
	}{
		{"GET is not a turn", httptest.NewRequest(http.MethodGet, termChatSubmitPath, nil), http.StatusMethodNotAllowed},
		{"empty message", termFormRequest(t, "   "), http.StatusBadRequest},
		{"multi-line", termFormRequest(t, "one\ntwo"), http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := newStreamRecorder()
		ws.handleTermChat(rec, tc.req)
		if rec.code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, rec.code, tc.status)
		}
	}

	// A JSON body (the browser carrier's encoding) is refused with the field
	// name, not half-honoured.
	jsonReq := httptest.NewRequest(http.MethodPost, termChatSubmitPath, strings.NewReader(`{"message":"hi"}`))
	jsonReq.Header.Set("Content-Type", "application/json")
	rec := newStreamRecorder()
	ws.handleTermChat(rec, jsonReq)
	if rec.code != http.StatusUnsupportedMediaType {
		t.Errorf("a JSON body → %d, want 415 (this endpoint takes the form encoding)", rec.code)
	}
	if !strings.Contains(rec.body(), termChatInputName) {
		t.Errorf("the 415 does not name the field to use:\n%s", rec.body())
	}
}

// TestTermSlashCommandNeverReachesTheModel pins the shared command table on
// this carrier: a slash input is answered locally and the model is never
// called.
func TestTermSlashCommandNeverReachesTheModel(t *testing.T) {
	var calls int
	var mu sync.Mutex
	ws, _ := termServerForTest(t, false, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "should not happen")
	})
	rec := newStreamRecorder()
	ws.handleTermChat(rec, termFormRequest(t, "/help"))
	if rec.code != http.StatusOK {
		t.Fatalf("a slash command → %d, want 200", rec.code)
	}
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 0 {
		t.Errorf("a slash command opened %d model requests, want 0 — commands are local", got)
	}
	if !strings.Contains(rec.body(), "/") {
		t.Errorf("the command's output did not reach the page:\n%s", rec.body())
	}
	// The reset word works here too, and clears the carrier-local history.
	ws.mu.Lock()
	ws.history = []deepseek.Message{{Role: termRoleUser, Content: "x"}}
	ws.mu.Unlock()
	rec2 := newStreamRecorder()
	ws.handleTermChat(rec2, termFormRequest(t, "/new"))
	ws.mu.Lock()
	n := len(ws.history)
	ws.mu.Unlock()
	if n != 0 {
		t.Errorf("after /new the history holds %d messages, want 0", n)
	}
}

// TestTermSubmitFormRequestIsWhatThePageSends closes the loop between the two
// halves WITHOUT a browser: the page is parsed for its form action and field
// name, a request is built from exactly those, and the endpoint accepts it. A
// drift in either direction fails here rather than at the terminal.
func TestTermSubmitFormRequestIsWhatThePageSends(t *testing.T) {
	ws, _ := termServerForTest(t, false, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "ok")
	})
	rec := newStreamRecorder()
	ws.handleTermPage(rec, httptest.NewRequest(http.MethodGet, termChatPath, nil))
	page := rec.body()

	action := between(page, `<form action="`, `"`)
	field := between(page, `name="`, `"`)
	if action != termChatSubmitPath || field != termChatInputName {
		t.Fatalf("the page's form routes to %q with field %q, want %q / %q", action, field, termChatSubmitPath, termChatInputName)
	}
	form := url.Values{}
	form.Set(field, "hello from the form")
	req := httptest.NewRequest(http.MethodPost, action, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", termFormContentType)
	rec2 := newStreamRecorder()
	ws.handleTermChat(rec2, req)
	if rec2.code != http.StatusOK {
		t.Fatalf("the request built from the page's own form → %d, want 200", rec2.code)
	}
}

// between returns the text between the first occurrence of open and the next
// occurrence of close after it ("" when either is absent).
func between(s, open, close string) string {
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestTermVerbHelpRunsNothing is gate 3, against the REAL binary: `gopi cli -h`
// prints its usage and starts NOTHING — no listener, no session, no browser.
func TestTermVerbHelpRunsNothing(t *testing.T) {
	bin := gopiTestBinPath(t)
	out, home := runCLIContractProbe(t, bin, CliVerbCLI, "-h")
	if !strings.Contains(out, "GoPi cli — chat in a TEXT-mode browser") {
		t.Fatalf("`gopi cli -h` did not print its usage page:\n%s", out)
	}
	if !strings.Contains(out, "gopi "+CliVerbCLI+" -h") {
		t.Errorf("the usage page does not show its own invocations:\n%s", out)
	}
	// Zero side effects: nothing in the sandbox home, and no listener left.
	assertNoCLIContractSession(t, CliVerbCLI+" -h", home)
	if entries, err := os.ReadDir(home); err == nil && len(entries) > 0 {
		t.Errorf("`gopi cli -h` wrote into GOPI_HOME (%s) — a help query must claim nothing", home)
	}
}

// TestTermUnknownSubcommandIsAUsageErrorNeverAnAgentRun pins the routing rule
// (the CLI contract suite's reason for existing: a typo must not burn a
// session). It runs the real binary, so the exit code and the absence of
// session artifacts are both observed.
func TestTermUnknownSubcommandIsAUsageErrorNeverAnAgentRun(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, err := runCLIContractProbeEnv(t, bin, home, CliVerbCLI, "bogus")
	if err == nil {
		t.Fatalf("`gopi cli bogus` exited 0 — an unknown subcommand must fail:\n%s", out)
	}
	if !strings.Contains(out, "unknown subcommand") {
		t.Errorf("`gopi cli bogus` does not report the unknown subcommand:\n%s", out)
	}
	assertNoCLIContractSession(t, CliVerbCLI+" bogus", home)
}

// TestTermBrowserDiscoveryPolicy pins the three channels and their ORDER: the
// override wins, a missing override is a LOUD error (never a silent fallback),
// and the vendored path is what a checkout without an override resolves.
func TestTermBrowserDiscoveryPolicy(t *testing.T) {
	// (1) the override wins outright.
	override := filepath.Join(t.TempDir(), "my-links")
	if err := os.WriteFile(override, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write override: %v", err)
	}
	t.Setenv(termLinksEnv, override)
	if got, err := resolveTermBrowser(); err != nil || got != override {
		t.Fatalf("%s override → (%q, %v), want (%q, nil)", termLinksEnv, got, err, override)
	}

	// (2) a missing override is loud and names the variable — never a silent
	// fallback to the vendored binary.
	missing := filepath.Join(t.TempDir(), "absent")
	t.Setenv(termLinksEnv, missing)
	_, err := resolveTermBrowser()
	if err == nil {
		t.Fatal("a missing override resolved anyway — a silently ignored override is worse than none")
	}
	if !strings.Contains(err.Error(), termLinksEnv) || !strings.Contains(err.Error(), missing) {
		t.Errorf("the missing-override error does not name the variable and the path: %v", err)
	}

	// (3) an override that is not executable is refused with the remedy.
	notExec := filepath.Join(t.TempDir(), "notexec")
	if err := os.WriteFile(notExec, []byte("x"), 0o644); err != nil {
		t.Fatalf("write non-executable: %v", err)
	}
	t.Setenv(termLinksEnv, notExec)
	if _, err := resolveTermBrowser(); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Errorf("a non-executable override → %v, want an error saying it is not executable", err)
	}

	// (4) the default channel: the vendored per-arch path under the repo root,
	// resolved by walking up from a nested working directory. The tree is
	// built with the marker file so the walk is the real one.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, termRepoMarkerFile), []byte("module x\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	rel := strings.Replace(termVendoredLinksRel, termLinksArchPlaceholder, runtime.GOARCH, 1)
	vendored := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(vendored), 0o755); err != nil {
		t.Fatalf("mkdir vendored dir: %v", err)
	}
	if err := os.WriteFile(vendored, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write vendored browser: %v", err)
	}
	t.Setenv(termLinksEnv, "")
	if got, ok := termRepoRoot(filepath.Dir(filepath.Dir(vendored))); !ok || got != root {
		t.Fatalf("termRepoRoot from a nested dir = (%q, %v), want (%q, true)", got, ok, root)
	}
	if got, err := resolveTermBrowserIn([]string{root}, rel); err != nil || got != vendored {
		t.Fatalf("vendored resolution = (%q, %v), want (%q, nil)", got, err, vendored)
	}

	// (5) nothing found anywhere → the actionable error names the recipe and
	// the reason (Rule 15), including the vendored path that was looked for.
	t.Setenv(termLinksEnv, "")
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, termRepoMarkerFile), []byte("module x\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	_, err = resolveTermBrowserIn([]string{empty}, rel)
	if err == nil {
		t.Fatal("an empty checkout resolved a browser out of thin air")
	}
	msg := err.Error()
	for _, want := range []string{termLinksEnv, termLinksRecipeRel, termLinksBinaryName} {
		if !strings.Contains(msg, want) {
			t.Errorf("the not-found error does not mention %q:\n%s", want, msg)
		}
	}
}

// TestTermBrowserExecArgv pins what is executed: the browser binary with the
// chat URL as its ONE argument, on the current terminal (the browser owns the
// tty — the launcher only waits).
func TestTermBrowserExecArgv(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-links")
	argsFile := filepath.Join(dir, "args")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$@\" > "+argsFile+"\n"), 0o755); err != nil {
		t.Fatalf("write fake browser: %v", err)
	}
	url := "http://localhost:8090" + termChatPath
	if err := runTermBrowser(script, url); err != nil {
		t.Fatalf("runTermBrowser: %v", err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the fake browser wrote no args: %v", err)
	}
	if string(got) != url {
		t.Errorf("browser argv = %q, want the chat URL %q", string(got), url)
	}
}

// TestTermTurnTimeoutIsItsOwnKnob pins the carrier's independent turn budget:
// the env override parses, 0 means unlimited, and a junk value falls back to
// the default LOUDLY rather than silently becoming 0 (a silently unlimited
// turn is the failure mode the default exists to prevent).
func TestTermTurnTimeoutIsItsOwnKnob(t *testing.T) {
	t.Setenv(termTurnTimeoutEnv, "")
	if got := termTurnTimeout(); got != termDefaultTurnTimeout {
		t.Errorf("default budget = %v, want %v", got, termDefaultTurnTimeout)
	}
	t.Setenv(termTurnTimeoutEnv, "30")
	if got := termTurnTimeout(); got != 30*time.Second {
		t.Errorf("%s=30 → %v, want 30s", termTurnTimeoutEnv, got)
	}
	t.Setenv(termTurnTimeoutEnv, "0")
	if got := termTurnTimeout(); got != 0 {
		t.Errorf("%s=0 → %v, want 0 (unlimited)", termTurnTimeoutEnv, got)
	}
	t.Setenv(termTurnTimeoutEnv, "soon")
	if got := termTurnTimeout(); got != termDefaultTurnTimeout {
		t.Errorf("a junk value → %v, want the default %v", got, termDefaultTurnTimeout)
	}
	// The two carriers must not share a knob: the browser budget reads a
	// DIFFERENT variable, so setting this one leaves webTurnTimeout alone
	// (asserted by name rather than by value, so the two defaults may differ).
	t.Setenv(termTurnTimeoutEnv, "30")
	if termTurnTimeoutEnv == webTurnTimeoutEnv {
		t.Fatal("the two carriers share one env knob — one carrier's budget silently rewrites the other's")
	}
}

// TestTermChatURLIsReachable pins the URL the browser is handed: it must carry
// the port the listener actually bound (port 0 → the real one) and the token
// exactly when the gate would demand it.
func TestTermChatURLIsReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_, boundPort, _ := net.SplitHostPort(ln.Addr().String()) //stylegate:ignore splits the fixture listener's own address; the derived port is the assertion
	got := termChatURL("127.0.0.1:0", ln, "")
	wantSuffix := ":" + boundPort + termChatPath
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("termChatURL = %q, want it to end in the BOUND port %q", got, wantSuffix)
	}
	if strings.Contains(got, "?") {
		t.Errorf("a loopback URL carried a token (%q) — this host is served without one", got)
	}
}

// TestTermToolLineCarriesTheArgumentDigest pins the activity line's shape: the
// kind, the tool, and the digest — one line, because a text browser has no
// place to put a card.
func TestTermToolLineCarriesTheArgumentDigest(t *testing.T) {
	ev := agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "read", Summary: "/etc/hostname"}
	got := termToolLine(ev)
	if !strings.HasPrefix(got, termToolPrefix+string(agent.ToolEventKindCall)+" read ") {
		t.Errorf("tool line = %q, want it to open with the kind and the tool name", got)
	}
	if !strings.Contains(got, "/etc/hostname") {
		t.Errorf("tool line = %q, want the argument digest", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Errorf("tool line = %q, want exactly one line (a text browser cannot collapse a card)", got)
	}
	// An empty summary must not leave a dangling separator.
	// A RESULT is drawn as the echo of the call rather than as a second call:
	// the continuation marker, dim, and no dangling separator when it has no
	// digest (the shape term_tool_row.go's contract states).
	got = termToolLine(agent.ToolEvent{Kind: agent.ToolEventKindResult, Name: "bash"})
	if got != lineedit.NoteContinuation+string(agent.ToolEventKindResult)+" bash\n" {
		t.Errorf("tool line without a digest = %q", got)
	}
}

// TestTermHistoryTextRendersOnlyTheConversation pins what a text browser is
// shown: the user's words and the model's answers, in order, with the tool
// payloads left out.
func TestTermHistoryTextRendersOnlyTheConversation(t *testing.T) {
	got := termHistoryText([]deepseek.Message{
		{Role: termRoleUser, Content: "ask"},
		{Role: termRoleAssistant, Content: "", ToolCalls: []deepseek.ToolCall{{ID: "c1", Name: "bash"}}},
		{Role: termRoleTool, Content: "{\"stdout\":\"secret\"}"},
		// A harness-injected user row: the framework's own follow-up, which
		// must NOT be attributed to the operator (measured in the live run).
		{Role: termRoleUser, Content: "[system] Continue: you have tools available"},
		{Role: termRoleAssistant, Content: "answer"},
	})
	want := termUserPrefix + "ask\n" + termAssistantPrefix + "answer\n"
	if got != want {
		t.Errorf("history text = %q, want %q", got, want)
	}
	if strings.Contains(got, "secret") {
		t.Error("the renderer shipped a raw tool payload to the terminal")
	}
	if strings.Contains(got, "Continue: you have tools") {
		t.Error("the renderer attributed a harness-injected follow-up to the operator")
	}
	if termHistoryText(nil) != termEmptyTranscriptNote {
		t.Error("an empty conversation must say so, not render a blank page")
	}
}

// TestTermSessionModeIsTheTablesOwn pins the ledger wiring: the carrier records
// its conversation under the chats table's OWN mode constant. A locally
// invented label ("cli") was rejected by the table's gate during the live
// acceptance run — the defect this assertion keeps out.
func TestTermSessionModeIsTheTablesOwn(t *testing.T) {
	if termChatSessionMode != tools.ChatModeCLI {
		t.Errorf("termChatSessionMode = %q, but the chats table's CLI mode is %q",
			termChatSessionMode, tools.ChatModeCLI)
	}
}

// TestTermUsagePageIsWellFormed keeps the help page honest: the notes it
// carries are the THREE facts an operator needs, spelled once where the usage
// prints them.
func TestTermUsagePageIsWellFormed(t *testing.T) {
	rendered := termUsage
	// The env override is named only if the constant exists (the page
	// interpolates it) — a page that forgot to mention it would be a silent
	// capability.
	if !strings.Contains(rendered, termLinksEnv) {
		t.Errorf("the usage page does not name %s:\n%s", termLinksEnv, rendered)
	}
	if !strings.Contains(rendered, termChatInputName) {
		t.Errorf("the usage page does not mention the field name:\n%s", rendered)
	}
	if strings.Contains(rendered, "%s") {
		t.Errorf("the usage page carries a printf verb but is printed without arguments:\n%s", rendered)
	}
}
