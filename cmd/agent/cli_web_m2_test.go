// cli_web_m2_test.go — regression tests for the M2 web increments re-developed
// on feat/web-m2-rework (devdoc/web-m2-rework.md).
//
//	B5  — /api/transcript ships each tool row's name + call/result digests (the
//	      deepseek.ToolCall wire fields are json:"-", so a naive replay shipped
//	      an empty tool name), and the page rebuilds its tool cards from them.
//	B9  — the `/` + `@` input-trigger menus: one detect→menu model, candidates
//	      resolved from the EXISTING shared command table / the file tree.
//	B12 — per-turn web turn budget (GOPI_WEB_TURN_TIMEOUT_SEC, read in ONE
//	      place) plus the visible turn queue it exists to protect.
//
// Mock discipline matches cli_web_test.go / cli_web_m1_test.go: the LLM is an
// httptest fake Claude SSE server; app state is real and isolated in t.TempDir
// + GOPI_HOME/GOPI_MEMORY_DB.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

const (
	// m2PollInterval is the poll step while a turn is in flight: the tests poll
	// the REAL condition (a request arriving at the mock model), never a fixed
	// wait.
	m2PollInterval = 5 * time.Millisecond
	// m2PollBudget bounds that poll. A hang guard, not a performance assertion.
	m2PollBudget = 5 * time.Second
)

// m2Capture is a concurrency-safe sink for the model requests a test saw.
type m2Capture struct {
	mu   sync.Mutex
	seen [][]byte
}

func (c *m2Capture) add(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, b)
}

func (c *m2Capture) all() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.seen))
	copy(out, c.seen)
	return out
}

func (c *m2Capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

// m2Server builds a webServer whose single agent points at a mock streaming
// server, capturing every request body it receives.
func m2Server(t *testing.T, answer string) (*webServer, *m2Capture) {
	t.Helper()
	cap := &m2Capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body; a short read is acceptable
		cap.add(b)
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, answer)
	}))
	t.Cleanup(srv.Close)
	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   webTestTurnModel,
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}
	return ws, cap
}

// waitForCaptureCount polls the real condition (N requests recorded) instead of
// sleeping for a fixed period.
func waitForCaptureCount(t *testing.T, cap *m2Capture, want int) {
	t.Helper()
	deadline := time.Now().Add(m2PollBudget)
	for time.Now().Before(deadline) {
		if cap.count() >= want {
			return
		}
		time.Sleep(m2PollInterval) //stylegate:allow-sleep polls a real condition — a recorded model request count — and exits as soon as it is met
	}
	t.Fatalf("mock model saw %d requests, want at least %d", cap.count(), want)
}

// TestWebM2B12_PerTurnTimeoutAppliedFromOneEnvReadSite is the B12 gate: the web
// turn budget is resolved from GOPI_WEB_TURN_TIMEOUT_SEC in exactly ONE place,
// a positive override takes effect, a malformed/zero value falls back to the
// documented default, and the value is CARRIED ON THE AGENT for the turn (the
// only mechanism that actually bounds a web turn, since Agent.Chat derives its
// context from Agent.Timeout).
//
// This is also this branch's RED/GREEN pair: with the two lines that set
// ag.Timeout removed, step 3 fails (the in-flight turn runs unlimited) — see
// the report for both runs.
func TestWebM2B12_PerTurnTimeoutAppliedFromOneEnvReadSite(t *testing.T) {
	// 1. Exactly one production reader of the env name (Rule 2).
	readerSites := productionReadsOf(t, webTurnTimeoutEnv)
	if len(readerSites) != 1 {
		t.Fatalf("%s is read at %v; want exactly ONE production site (Rule 2)", webTurnTimeoutEnv, readerSites)
	}
	if !strings.HasSuffix(readerSites[0], "cli_web.go") {
		t.Errorf("the %s reader lives in %s; want cli_web.go (the one place the web turn budget is resolved)", webTurnTimeoutEnv, readerSites[0])
	}

	// 2. The resolver's contract: override wins, everything else is the default.
	t.Setenv(webTurnTimeoutEnv, "")
	if got := webTurnTimeout(); got != webDefaultTurnTimeout {
		t.Errorf("unset %s gave %v, want the default %v", webTurnTimeoutEnv, got, webDefaultTurnTimeout)
	}
	t.Setenv(webTurnTimeoutEnv, "45")
	if got, want := webTurnTimeout(), 45*time.Second; got != want {
		t.Errorf("%s=45 gave %v, want %v", webTurnTimeoutEnv, got, want)
	}
	for _, bad := range []string{"not-a-number", "0", "-3"} {
		t.Setenv(webTurnTimeoutEnv, bad)
		if got := webTurnTimeout(); got != webDefaultTurnTimeout {
			t.Errorf("%s=%q gave %v, want the default %v (a non-positive budget means unlimited, never a zero deadline)",
				webTurnTimeoutEnv, bad, got, webDefaultTurnTimeout)
		}
	}

	// 3. The budget reaches the agent WHILE the turn runs, and only for the
	// turn. The mock records the agent's Timeout at request time — the value
	// Agent.Chat actually derives its context deadline from.
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")
	t.Setenv(webTurnTimeoutEnv, "7")
	var (
		mu            sync.Mutex
		seenInFlight  time.Duration
		sawInFlightAt bool
	)
	ag := &agent.Agent{Ctx: &agent.Context{}, Model: webTestTurnModel, Timeout: 0, Logger: log.New(io.Discard, "", 0)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenInFlight = ag.Timeout
		sawInFlightAt = true
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "answered")
	}))
	defer srv.Close()
	ag.Client = &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	rec := newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, "hello"))
	if !strings.Contains(rec.body(), "event: "+webEventDone) {
		t.Fatalf("turn did not complete: %q", rec.body())
	}
	mu.Lock()
	inFlight, saw := seenInFlight, sawInFlightAt
	mu.Unlock()
	if !saw {
		t.Fatal("the mock model never saw the turn")
	}
	if want := 7 * time.Second; inFlight != want {
		t.Errorf("the in-flight turn ran with Agent.Timeout = %v, want %v (B12 regression: the per-turn budget is not applied)", inFlight, want)
	}
	if ag.Timeout != 0 {
		t.Errorf("agent timeout after the turn = %v, want the pre-turn value 0 (the per-turn budget must not leak into the process-wide knob)", ag.Timeout)
	}

	// 4. The page advertises the knob, so an operator can find it.
	if !strings.Contains(webChatPage, webTurnTimeoutEnv) && !strings.Contains(webUsage, webTurnTimeoutEnv) {
		t.Errorf("neither the page nor the usage text names %s — a documented knob must be discoverable", webTurnTimeoutEnv)
	}
}

// productionReadsOf reports every production (non-test) .go file under the
// package directory that references the given env name. Used to pin the
// "exactly one reader" half of Rule 2.
func productionReadsOf(t *testing.T, envName string) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var sites []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(b), `"`+envName+`"`) {
			sites = append(sites, filepath.Join(".", name))
		}
	}
	return sites
}

// TestWebM2B5_TranscriptCarriesToolNameAndSummaryForReplay is the B5 gate: the
// transcript must be able to rebuild a tool row after a refresh. Before B5 the
// handler marshalled deepseek.Message.tool_calls, whose fields are all json:"-"
// — so every assistant tool turn replayed with an EMPTY tool name and no call
// digest, and the page rendered "tool → <result>" placeholders.
//
// The test drives a real tool turn through the mock model (a tool_use round,
// then a final answer), then reads /api/transcript.
func TestWebM2B5_TranscriptCarriesToolNameAndSummaryForReplay(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	const (
		callID   = "call_b5_read_1"
		toolName = "read"
		toolPath = "/etc/hostname"
	)
	round := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		round++
		n := round
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			toolUseSSE(w, callID, toolName, fmt.Sprintf(`{"path":%q}`, toolPath))
			return
		}
		textSSE(w, "done")
	}))
	t.Cleanup(srv.Close)

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   webTestTurnModel,
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, tools: true, startedAt: time.Now()}

	rec := newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, "read the hostname file"))
	body := rec.body()
	if !strings.Contains(body, "event: "+webEventTool) {
		t.Fatalf("the tool turn streamed no tool frame: %q", body)
	}

	tr := httptest.NewRecorder()
	ws.handleTranscript(tr, httptest.NewRequest(http.MethodGet, "/api/transcript", nil))
	var got struct {
		Messages []struct {
			Role  string `json:"role"`
			Tools []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Call   string `json:"call"`
				Result string `json:"result"`
			} `json:"tools"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(tr.Body.Bytes(), &got); err != nil {
		t.Fatalf("transcript is not decodable JSON: %v (%s)", err, tr.Body.String())
	}
	var callRow, resultRow bool
	var resultDigest string
	for _, m := range got.Messages {
		for _, tool := range m.Tools {
			if tool.Name == "" {
				t.Errorf("a transcript tool row carries no name (B5 regression): %+v", tool)
			}
			if tool.Name != toolName {
				continue
			}
			if tool.Call != "" {
				callRow = true
				if !strings.Contains(tool.Call, toolPath) {
					t.Errorf("tool call digest = %q, want it to carry the argument %q (the same digest the live frame sent)", tool.Call, toolPath)
				}
			}
			if tool.Result != "" {
				resultRow = true
				resultDigest = tool.Result
			}
		}
	}
	if !callRow {
		t.Errorf("no transcript row carried the tool name + call digest (B5 regression: the wire ToolCall fields are json:\"-\", so tool_calls alone ships nothing); messages=%s", tr.Body.String())
	}
	if !resultRow {
		t.Errorf("no transcript row carried a tool RESULT digest; messages=%s", tr.Body.String())
	}

	// The call digest must be exactly what the live frame carried (one digest
	// definition — the loop's own summary helper), not a second rendering.
	live, ok := sseDataOf(body, webEventTool)
	if !ok {
		t.Fatal("no tool frame on the wire")
	}
	var ev struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(live), &ev); err != nil {
		t.Fatalf("tool frame payload: %v", err)
	}
	want := agent.ToolCallSummary(deepseek.ToolCall{Name: toolName, Arguments: map[string]interface{}{"path": toolPath}})
	if ev.Summary != want {
		t.Errorf("live frame summary = %q, replayed digest would be %q — the two must come from one definition", ev.Summary, want)
	}

	// The RESULT digest must say what the tool returned — never the harness's
	// "[tool_use id: …]" header line, which is what the raw transcript stores
	// first.
	if strings.Contains(resultDigest, "[tool_use id:") {
		t.Errorf("replayed result digest = %q — it renders the harness header instead of the tool's output", resultDigest)
	}

	// The live result frame goes through the SAME rule, so a replayed row and a
	// streamed card cannot disagree.
	var liveResults []string
	for _, e := range strings.Split(body, "\n\n") {
		if !strings.Contains(e, "event: "+webEventTool) {
			continue
		}
		d, ok := sseDataOf(e+"\n\n", webEventTool)
		if !ok {
			continue
		}
		var tEv agent.ToolEvent
		if err := json.Unmarshal([]byte(d), &tEv); err != nil {
			continue
		}
		if tEv.Kind == agent.ToolEventKindResult {
			liveResults = append(liveResults, tEv.Summary)
		}
	}
	if len(liveResults) == 0 {
		t.Fatal("no result tool frame on the wire")
	}
	if resultDigest != liveResults[0] {
		t.Errorf("replayed digest = %q, live result frame summary = %q — a refresh must not show different text than the live stream", resultDigest, liveResults[0])
	}

	// The digest rule itself, at its narrowest (the minimal sample): a stamped
	// result digests to the PAYLOAD line, an unstamped one to its own first
	// line, an empty result to "", and the rune cap never cuts mid-character.
	stampedSample := "[tool_use id: call_x]\nhost-b\n"
	if got := agent.ToolResultDigest(stampedSample, webTranscriptDigestMaxRunes); got != "host-b" {
		t.Errorf("ToolResultDigest(%q) = %q, want the payload line", stampedSample, got)
	}
	if got := agent.ToolResultDigest("plain\nrest", webTranscriptDigestMaxRunes); got != "plain" {
		t.Errorf("ToolResultDigest of an unstamped result = %q, want its first line", got)
	}
	if got := agent.ToolResultDigest("", webTranscriptDigestMaxRunes); got != "" {
		t.Errorf("ToolResultDigest(\"\") = %q, want empty (the caller decides the placeholder)", got)
	}
	cjk := strings.Repeat("知", webTranscriptDigestMaxRunes+50)
	digested := agent.ToolResultDigest(cjk, webTranscriptDigestMaxRunes)
	if r := []rune(digested); len(r) != webTranscriptDigestMaxRunes+1 {
		t.Errorf("rune cap gave %d runes, want %d + the ellipsis (never cut mid-character)", len(r), webTranscriptDigestMaxRunes)
	}
	if !strings.HasSuffix(digested, "…") {
		t.Errorf("a capped digest must end with the ellipsis marker")
	}

	// The page must consume those fields on replay (no second placeholder path).
	for _, needle := range []string{"m.tools", "addToolCard(t.name", "openCards"} {
		if !strings.Contains(webChatPage, needle) {
			t.Errorf("the page's replay does not use %q — a transcript with tool rows would not render as tool cards", needle)
		}
	}
}

// TestWebM2B9_MenuEndpointServesTheSharedTable is the B9 server half: the
// candidate endpoint answers from the EXISTING shared command table (never a
// second list), caps its answer at the ONE menu budget, and degrades to an
// empty list for an unknown trigger rather than erroring.
func TestWebM2B9_MenuEndpointServesTheSharedTable(t *testing.T) {
	ws, _ := m2Server(t, "ok")

	menu := func(trigger, query string) webMenuCandidates {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, webMenuPath+"?"+webMenuTriggerArg+"="+trigger+"&"+webMenuQueryArg+"="+query, nil)
		ws.handleMenu(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s (%s,%s) status = %d, want 200", webMenuPath, trigger, query, rec.Code)
		}
		var out webMenuCandidates
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("menu body %q is not JSON: %v", rec.Body.String(), err)
		}
		return out
	}

	// The slash menu offers the shared table's words, filtered by the query.
	all := menu(webTriggerSlash, "")
	if all.Source != webMenuSourceSlash {
		t.Errorf("slash menu source = %q, want %q", all.Source, webMenuSourceSlash)
	}
	if len(all.Candidates) == 0 {
		t.Fatal("the slash menu offered no candidates — the shared table is the source")
	}
	tableSet := map[string]bool{}
	for _, n := range slashCommandNames() {
		tableSet[n] = true
	}
	for _, c := range all.Candidates {
		if !tableSet[c] {
			t.Errorf("menu candidate %q is not a shared-table command — the menu must not invent words", c)
		}
	}
	// The B1 reset word is discoverable through the same table.
	if !strings.Contains(strings.Join(all.Candidates, " "), webClearCommandName) {
		t.Errorf("the slash menu does not offer /%s: %v", webClearCommandName, all.Candidates)
	}
	// A prefix filters; a prefix matching nothing falls back to the whole table
	// (a partial word is a filter, never a dead end).
	filtered := menu(webTriggerSlash, "ne")
	if len(filtered.Candidates) == 0 || !strings.Contains(filtered.Candidates[0], "ne") {
		t.Errorf("prefix query 'ne' gave %v, want matching commands", filtered.Candidates)
	}
	fallback := menu(webTriggerSlash, "zzzz-no-such-command")
	if len(fallback.Candidates) == 0 {
		t.Error("a non-matching prefix must fall back to the whole table, not offer nothing")
	}
	// The ONE budget governs the answer and the reported total.
	if len(all.Candidates) > webMenuMaxItems {
		t.Errorf("menu shipped %d candidates, above the budget %d", len(all.Candidates), webMenuMaxItems)
	}
	if all.Total < len(all.Candidates) {
		t.Errorf("total %d < shipped %d — the count must report the pre-cap match count", all.Total, len(all.Candidates))
	}

	// The @ menu resolves repo files; an unresolvable root/query is an empty
	// list, never an error.
	files := menu(webTriggerAt, "")
	if files.Source != webMenuSourceFile {
		t.Errorf("@ menu source = %q, want %q", files.Source, webMenuSourceFile)
	}
	if len(files.Candidates) > webMenuMaxItems {
		t.Errorf("@ menu shipped %d candidates, above the budget %d", len(files.Candidates), webMenuMaxItems)
	}
	none := menu(webTriggerAt, "zzz-no-such-file-zzz")
	if len(none.Candidates) != 0 {
		t.Errorf("@ menu for an unmatched fragment = %v, want empty", none.Candidates)
	}

	// An unknown trigger (or a wrong method) is an empty answer / 405, never a
	// crash and never a panic.
	unknown := menu("?", "")
	if len(unknown.Candidates) != 0 {
		t.Errorf("unknown trigger gave candidates %v, want empty", unknown.Candidates)
	}
	rec := httptest.NewRecorder()
	ws.handleMenu(rec, httptest.NewRequest(http.MethodPost, webMenuPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s status = %d, want 405", webMenuPath, rec.Code)
	}
}

// TestWebM2B12_SteeringSentinelIsCarrierMetadata proves the server strips the
// steering sentinel BEFORE the turn: the model sees the user's own words, never
// the page's framing (a sentinel that leaked would be indistinguishable from
// content the user typed).
func TestWebM2B12_SteeringSentinelIsCarrierMetadata(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	ws, cap := m2Server(t, "ok")
	rec := newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, webSteerPrefix+"narrow it down"))
	waitForCaptureCount(t, cap, 1)

	reqs := cap.all()
	if len(reqs) == 0 {
		t.Fatal("the steering turn never reached the model")
	}
	body := string(reqs[0])
	if strings.Contains(body, "!steer") {
		t.Errorf("the steering sentinel reached the model request: %s", body)
	}
	if !strings.Contains(body, "narrow it down") {
		t.Errorf("the steering message's own words were lost: %s", body)
	}
	// A sentinel-only message is an empty message (refused, never a turn).
	rec = newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, webSteerPrefix+"   "))
	if rec.code != http.StatusBadRequest {
		t.Errorf("a sentinel-only steering message → HTTP %d, want 400 (empty message)", rec.code)
	}
	if cap.count() != 1 {
		t.Errorf("a sentinel-only message opened %d model requests, want 1 (the real turn)", cap.count())
	}
}

// toolUseSSE writes one tool_use round in the Anthropic streaming shape: the
// model requests a tool and stops (the loop then executes it and asks again).
func toolUseSSE(w io.Writer, callID, name, argsJSON string) {
	fmt.Fprint(w, "event: content_block_start\n")
	fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q}}\n\n", callID, name)
	fmt.Fprint(w, "event: content_block_delta\n")
	fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%q}}\n\n", argsJSON)
	fmt.Fprint(w, "event: content_block_stop\n")
	fmt.Fprint(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
}

// TestWebM2_InvariantsSingleSessionAndSerializedTurns is the roadmap §5 gate,
// re-proved on this branch after every increment landed: the web server keeps
// ONE persistent session (one id, one JSONL, one raw log) no matter how many
// turns run, and concurrent turns are SERIALIZED by s.mu — the mock model never
// sees two requests in flight at once.
//
// It is deliberately stronger than the pre-existing concurrency test: that one
// asserts the session tally after the fact; this one also MEASURES the overlap
// (an atomic in-flight counter), which is what the per-turn mutex discipline
// actually promises.
func TestWebM2_InvariantsSingleSessionAndSerializedTurns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPI_HOME", dir)
	t.Setenv("GOPI_MEMORY_DB", "")

	const turns = 5

	var (
		inFlight    int32
		maxInFlight int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		// Hold the request briefly so an unserialized pair WOULD overlap.
		time.Sleep(5 * time.Millisecond) //stylegate:allow-sleep widens the window an unserialized pair would overlap in — the assertion is on the measured overlap, not on this delay
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "answer")
		atomic.AddInt32(&inFlight, -1)
	}))
	t.Cleanup(srv.Close)

	sl, err := agent.NewSessionLogger(webTestTurnModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	rl, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	defer func() { sl.Close(); rl.Close() }()

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      webTestTurnModel,
		Timeout:    0,
		SessionLog: sl,
		RawLog:     rl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	sessionID := ag.SessionLog.SessionID()
	sessionPath := ag.SessionLog.Path()

	var wg sync.WaitGroup
	errs := make(chan error, turns)
	for i := 0; i < turns; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rec := newStreamRecorder()
			ws.handleChat(rec, chatRequestForTest(t, fmt.Sprintf("turn %d", n)))
			if !strings.Contains(rec.body(), "event: "+webEventDone) {
				errs <- fmt.Errorf("turn %d did not complete: %q", n, rec.body())
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent turn error: %v", err)
	}

	// Invariant 1: one session, same id, same file — for every turn.
	if got := ag.SessionLog.SessionID(); got != sessionID {
		t.Errorf("session id changed under concurrency: %s → %s", sessionID, got)
	}
	if got := ag.SessionLog.Path(); got != sessionPath {
		t.Errorf("session log path changed under concurrency: %s → %s", sessionPath, got)
	}
	data, readErr := os.ReadFile(sessionPath)
	if readErr != nil {
		t.Fatalf("read session log: %v", readErr)
	}
	var users, assistants int
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]interface{}
		if json.Unmarshal([]byte(ln), &ev) != nil {
			continue
		}
		switch ev["type"] {
		case "user":
			users++
		case "assistant":
			assistants++
		}
	}
	if users != turns || assistants != turns {
		t.Errorf("one-session tally = %d user / %d assistant, want %d/%d on ONE file", users, assistants, turns, turns)
	}

	// Invariant 2: the per-turn mutex actually serialized the turns.
	if got := atomic.LoadInt32(&maxInFlight); got > 1 {
		t.Errorf("max concurrent model requests = %d, want 1 — s.mu must serialize web turns (the single-agent discipline)", got)
	}
	if got := atomic.LoadInt32(&inFlight); got != 0 {
		t.Errorf("%d requests still in flight after the turns returned — the turn domain leaks", got)
	}
}

// TestWebM2B9_MenuHonoursItsOneBudgetAndItsWalkRoot pins the two halves of the
// B9 candidate resolution that the endpoint alone cannot show: the @ menu walks
// the SAME root `gopi tool code-search` uses (GOPI_CODE_SEARCH_ROOT), skips build/VCS
// directories, caps its shipped list at the ONE budget while still reporting an
// honest total, and answers an empty menu for a query nothing matches.
func TestWebM2B9_MenuHonoursItsOneBudgetAndItsWalkRoot(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mk("alpha.go", "package alpha")
	mk("beta.go", "package beta")
	mk("sub/deep.go", "package sub")
	// A skipped directory's contents must never be offered.
	mk("node_modules/pkg/ignored.go", "package pkg")
	mk(".git/objects/sneaky.go", "package git")
	t.Setenv("GOPI_CODE_SEARCH_ROOT", root)

	got := detectFileMenu("")
	if len(got) == 0 {
		t.Fatal("the @ menu found no files under its root")
	}
	for _, cand := range got {
		if strings.Contains(cand, "node_modules") || strings.Contains(cand, ".git") {
			t.Errorf("@ menu offered %q — build/VCS directories must be skipped", cand)
		}
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"alpha.go", "beta.go", "sub/deep.go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("@ menu missing %q (it walks the code-search root); got %v", want, got)
		}
	}
	// A basename fragment matches by NAME, so a nested file is still reachable.
	if base := detectFileMenu("deep"); len(base) != 1 || base[0] != "sub/deep.go" {
		t.Errorf("@ menu for 'deep' = %v, want [sub/deep.go] (a bare fragment matches basenames)", base)
	}
	// A path fragment matches the relative path.
	if rel := detectFileMenu("sub/"); len(rel) != 1 || rel[0] != "sub/deep.go" {
		t.Errorf("@ menu for 'sub/' = %v, want [sub/deep.go]", rel)
	}

	// The ONE budget caps the shipped list, and the total stays honest.
	var all []string
	for i := 0; i < webMenuMaxItems+5; i++ {
		name := fmt.Sprintf("many_%02d.go", i)
		mk(name, "package many")
		all = append(all, name)
	}
	full := detectFileMenu("many_")
	if len(full) != len(all) {
		t.Errorf("the walk returned %d matches, want all %d (the shipped list is capped by the CALLER, not the walk — otherwise an honest total is impossible)", len(full), len(all))
	}
	capped := capMenuList(full)
	if len(capped) != webMenuMaxItems {
		t.Errorf("capMenuList returned %d candidates, want the budget %d", len(capped), webMenuMaxItems)
	}
	ws, _ := m2Server(t, "ok")
	rec := httptest.NewRecorder()
	ws.handleMenu(rec, httptest.NewRequest(http.MethodGet, webMenuPath+"?"+webMenuTriggerArg+"="+webTriggerAt+"&"+webMenuQueryArg+"=many_", nil))
	var out webMenuCandidates
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("menu body: %v", err)
	}
	if len(out.Candidates) != webMenuMaxItems {
		t.Errorf("the endpoint shipped %d candidates, want the budget %d", len(out.Candidates), webMenuMaxItems)
	}
	if out.Total <= len(out.Candidates) {
		t.Errorf("total %d must exceed the shipped %d so the page can render %q", out.Total, len(out.Candidates), webMoreFlag)
	}
}

// TestWebM2B12_PerTurnBudgetRestoresAndStopsCleanly proves the two disciplines
// the per-turn budget must not break: the agent's own timeout is RESTORED after
// the turn (the knob is per-turn, never process-wide), and a stopped turn still
// leaves the queue empty and the composer idle rather than wedging the page's
// state machine.
func TestWebM2B12_PerTurnBudgetRestoresAndStopsCleanly(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")
	t.Setenv(webTurnTimeoutEnv, "3")

	ws, cap := m2Server(t, "answered")
	// A pre-existing, non-zero agent timeout must come back unchanged.
	const preExisting = 90 * time.Second
	ws.agent.Timeout = preExisting
	rec := newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, "hello"))
	waitForCaptureCount(t, cap, 1)
	if ws.agent.Timeout != preExisting {
		t.Errorf("agent timeout after the turn = %v, want the pre-turn %v (the per-turn budget must not become the process-wide one)", ws.agent.Timeout, preExisting)
	}
	if !strings.Contains(rec.body(), "event: "+webEventDone) {
		t.Errorf("the bounded turn did not complete cleanly: %q", rec.body())
	}

	// A turn whose model is unreachable must still write a terminal frame and
	// leave nothing latched: the page's "busy" gate is driven by the stream, so
	// a silent death would wedge the composer forever.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ws.agent.Client = &deepseek.Client{BaseURL: dead.URL, APIKey: "k", HTTPClient: dead.Client()}
	dead.Close()
	rec = newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, "after the failure"))
	body := rec.body()
	if !strings.Contains(body, "event: "+webEventError) && !strings.Contains(body, "event: "+webEventDone) {
		t.Errorf("a turn against a dead upstream emitted no terminal frame — the page would stay busy forever: %q", body)
	}
}
