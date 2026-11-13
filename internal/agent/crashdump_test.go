package agent

import (
	"context"
	"encoding/json"
	"github.com/nick/gopi/internal/sessionevent"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/signalrec"
)

// newTestSession builds a SessionLogger in an isolated GOPI_HOME.
func newTestSession(t *testing.T) *SessionLogger {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	//stylegate:ignore test cleanup: SessionLogger.Close flushes and fsyncs the JSONL at teardown; the test reads the file inside its own body, so a teardown flush error has no assertion left to fail
	t.Cleanup(func() { sl.Close() })
	return sl
}

// TestLogSessionEnd_NormalExitHasNoErrorBlock (epic ⑤ at_exit semantics): a
// healthy session_end carries NO error block — its presence is the marker.
func TestLogSessionEnd_NormalExitHasNoErrorBlock(t *testing.T) {
	sl := newTestSession(t)
	if err := sl.LogSessionEnd(0, 3, 100, 1.5, nil, EndCause{Reason: sessionevent.SessionEndReasonComplete}); err != nil {
		t.Fatalf("LogSessionEnd: %v", err)
	}
	sl.Close() //stylegate:ignore test forces the flush by closing the logger before reading the JSONL; a flush error would surface as absent content in the assertions on the next lines
	data, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var end map[string]interface{}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &end); err != nil {
		t.Fatalf("unmarshal session_end: %v", err)
	}
	if end["type"] != "session_end" {
		t.Fatalf("last event type = %v, want session_end", end["type"])
	}
	if _, has := end["error"]; has {
		t.Fatalf("normal exit must not carry an error block: %v", end)
	}
}

// TestLogSessionEnd_ErrorBlockPresent: a failed session_end carries the
// error block with the epic-mandated fields.
func TestLogSessionEnd_ErrorBlockPresent(t *testing.T) {
	sl := newTestSession(t)
	er := &ErrorRecord{
		Category:          "4xx",
		HTTPStatus:        400,
		ErrorBody:         `{"error":{"code":"1214"}}`,
		LastRequestDigest: "model=m messages=5 last_role=user bytes=1234",
		FailedIteration:   2,
		RecoverableHint:   false,
	}
	sl.WriteCrashDump(er)
	if er.DumpPath == "" {
		t.Fatalf("WriteCrashDump did not record DumpPath")
	}
	if err := sl.LogSessionEnd(1, 2, 90, 8.0, er, EndCause{Reason: sessionevent.SessionEndReasonRunFailed}); err != nil {
		t.Fatalf("LogSessionEnd: %v", err)
	}
	sl.Close() //stylegate:ignore test forces the flush before reading, and the next lines assert the dump was persisted to a durable file, which subsumes the flush error

	data, _ := os.ReadFile(sl.Path()) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var end struct {
		Type  string `json:"type"`
		Error struct {
			Category          string `json:"category"`
			HTTPStatus        int    `json:"http_status"`
			LastRequestDigest string `json:"last_request_digest"`
			FailedIteration   int    `json:"failed_iteration"`
			RecoverableHint   bool   `json:"recoverable_hint"`
			DumpPath          string `json:"dump_path"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &end); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if end.Type != "session_end" || end.Error.Category != "4xx" || end.Error.HTTPStatus != 400 {
		t.Fatalf("bad session_end: %+v", end)
	}
	if end.Error.FailedIteration != 2 || end.Error.RecoverableHint {
		t.Fatalf("bad error block: %+v", end.Error)
	}
	if end.Error.LastRequestDigest == "" || end.Error.DumpPath == "" {
		t.Fatalf("digest/dump_path missing: %+v", end.Error)
	}
}

// TestWriteCrashDump_NamingFamily: the dump lives NEXT TO the JSONL with the
// same base name — <session-id>.jsonl → <session-id>.dump.json.
func TestWriteCrashDump_NamingFamily(t *testing.T) {
	sl := newTestSession(t)
	er := &ErrorRecord{Category: "network", TransportErr: "connection refused", RecoverableHint: true}
	sl.WriteCrashDump(er)
	want := strings.TrimSuffix(sl.Path(), ".jsonl") + ".dump.json"
	if er.DumpPath != want {
		t.Fatalf("DumpPath = %s, want %s", er.DumpPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("dump missing: %v", err)
	}
	raw, _ := os.ReadFile(want) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("dump not valid JSON: %v", err)
	}
	if doc["type"] != "crash_dump" || doc["session_id"] != sl.SessionID() {
		t.Fatalf("bad dump doc: %v", doc)
	}
	if doc["category"] != "network" {
		t.Fatalf("category = %v", doc["category"])
	}
}

// TestWriteCrashDump_ContainsRequestAndContext (epic ② dump content): the
// full last request body + run context land in the dump.
func TestWriteCrashDump_ContainsRequestAndContext(t *testing.T) {
	sl := newTestSession(t)
	req := map[string]interface{}{
		"model":      "test-model",
		"max_tokens": 100,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
	}
	b, _ := json.Marshal(req) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	er := &ErrorRecord{
		Category:          "5xx",
		HTTPStatus:        500,
		ErrorBody:         "server exploded",
		LastRequestDigest: "model=test-model messages=1 last_role=user bytes=100",
		LastRequest:       b,
		Iterations:        7,
		Tokens:            4200,
		ElapsedSec:        33.3,
		RecentToolCalls:   []string{"bash", "read"},
	}
	sl.WriteCrashDump(er)
	raw, _ := os.ReadFile(er.DumpPath) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	var doc struct {
		Request     json.RawMessage `json:"request"`
		Iterations  int             `json:"iterations"`
		TotalTokens int             `json:"total_tokens"`
		ElapsedSec  float64         `json:"elapsed_sec"`
		RecentTools []string        `json:"recent_tool_calls"`
		ErrorBody   string          `json:"error_body"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal dump: %v", err)
	}
	if len(doc.Request) == 0 || !strings.Contains(string(doc.Request), "test-model") {
		t.Fatalf("request missing from dump: %s", string(doc.Request))
	}
	if doc.Iterations != 7 || doc.TotalTokens != 4200 || doc.ElapsedSec != 33.3 {
		t.Fatalf("context missing: %+v", doc)
	}
	if len(doc.RecentTools) != 2 || doc.ErrorBody != "server exploded" {
		t.Fatalf("tools/body missing: %+v", doc)
	}
}

// TestHasSessionEnd / orphan detection (epic ② kill -9 case).
func TestDetectOrphanSessions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Complete session: HAS session_end → not an orphan. The pid stamp names
	// a provably-dead process, because classification requires BOTH clauses:
	// a session whose writer is still alive is not a crash (it is simply
	// still being written), and a session with no liveness stamp cannot be
	// classified at all — see internal/agent/orphanclassify.go.
	write("complete.jsonl",
		deadStampStartLine("complete")+"\n"+`{"type":"session_end","exit_code":0}`+"\n")
	// Orphan: writer gone, NO session_end (killed mid-run) → orphan — but only
	// once it has been QUIET past the live window (a JSONL still being written
	// is a live session, not a crash), so its mtime is backdated.
	write("orphan.jsonl", deadStampStartLine("orphan")+"\n")
	backdate(t, filepath.Join(sessionsDir, "orphan.jsonl"), 2*time.Hour)
	// Not a session file (dump family) → never scanned.
	write("x.dump.json", `{}`)

	fresh := writeSessionJSONL(t, sessionsDir, "live.jsonl", `{"type":"session_start"}`+"\n", 0)

	// A JSONL written just now has no session_end either — it is ALIVE. The
	// mtime window is what tells it apart from a crash.
	if orphans := DetectOrphanSessions(); len(orphans) != 1 || filepath.Base(orphans[0]) != "orphan.jsonl" {
		t.Fatalf("orphans = %v, want exactly [orphan.jsonl] (live.jsonl is still being written)", orphans)
	}

	orphans := DetectOrphanSessions()
	if filepath.Base(orphans[0]) != "orphan.jsonl" {
		t.Fatalf("orphan = %s", orphans[0])
	}

	written, retracted := BackfillOrphanCrashDumps()
	if written != 1 || retracted != 0 {
		t.Fatalf("BackfillOrphanCrashDumps = (%d, %d), want (1, 0)", written, retracted)
	}
	if _, err := os.Stat(fresh + ".dump.json"); err == nil {
		t.Fatalf("live session was dumped: %s", fresh)
	}
	dump := strings.TrimSuffix(orphans[0], ".jsonl") + ".dump.json"
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("backfill dump missing: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bad backfill dump: %v", err)
	}
	if doc["orphan"] != true || doc["category"] != orphanDumpCategory {
		t.Fatalf("bad backfill doc: %v", doc)
	}
	// Second run is idempotent (dump exists → no rewrite).
	if w, r := BackfillOrphanCrashDumps(); w != 0 || r != 0 {
		t.Fatalf("second backfill = (%d, %d), want (0, 0) (idempotent)", w, r)
	}
}

// TestCollectErrorRecord_HTTPAndPanic: the single collection path.
func TestCollectErrorRecord_HTTPAndPanic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)

	// HTTP failure recorded on a client. NewClaudeClient needs settings+
	// key env — absent in a clean CI checkout, so guard: a nil client must
	// not panic the collector (the collector treats it as transport-only).
	client, cerr := deepseek.NewClaudeClient()
	if cerr == nil && client != nil {
		client.LastErrDetail.RecordForTest(429, []byte(`{"error":"rate limited"}`), true)
	}
	a := &Agent{Client: client}
	er := a.collectErrorRecord(nil, 3, 100, 5.5)
	if cerr == nil && client != nil {
		if er.Category != "4xx" || er.HTTPStatus != 429 || !er.RecoverableHint {
			t.Fatalf("429 record: %+v", er)
		}
	} else if er.Category == "4xx" {
		t.Fatalf("nil client must not yield 4xx category: %+v", er)
	}

	// Panic context overrides.
	panicCollectorStore("nil map write", "goroutine 1 [running]:...")
	er2 := a.collectErrorRecord(nil, 4, 200, 6.0)
	if er2.Category != "panic" || er2.ErrorBody != "nil map write" {
		t.Fatalf("panic record: %+v", er2)
	}
	if er2.RecoverableHint || er2.PanicStack == "" {
		t.Fatalf("panic hint/stack: %+v", er2)
	}
}

// TestAccountLevelEvidence_EndToEnd — the epic's acceptance path: an
// account-level failure (GLM 1113) recorded through the REAL client must come
// out as category "account" with recoverable_hint false and the operator
// remedy text on ALL THREE evidence surfaces (session_end error block, crash
// dump file, task-ledger digest).
func TestAccountLevelEvidence_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","code":"1113","message":"[1113][余额不足或无可用资源包,请充值。]"}}`))
	}))
	defer srv.Close()

	sl := newTestSession(t)
	c := &deepseek.Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: deepseek.RetryConfig{MaxRetries: 0},
	}
	if _, err := c.ClaudeChat(context.Background(), &deepseek.ClaudeRequest{Model: "m", MaxTokens: 10, Messages: []deepseek.ClaudeMessage{{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "hi"}}}}}); err == nil {
		t.Fatal("expected the account-level send to fail")
	}

	a := &Agent{Client: c, SessionLog: sl}
	er := a.collectErrorRecord(nil, 3, 100, 5.5)
	if er.Category != string(deepseek.CategoryAccount) {
		t.Fatalf("category = %q, want %q", er.Category, deepseek.CategoryAccount)
	}
	if er.RecoverableHint {
		t.Fatal("account-level failure marked recoverable — a resumer would retry a dead account")
	}
	if !strings.Contains(er.Remedy, "1113") || !strings.Contains(er.Remedy, "top up") {
		t.Fatalf("remedy must name condition + remedy: %q", er.Remedy)
	}

	// Surface 3: the ledger digest (notification pipe → PA).
	if d := er.SummaryDigest(); !strings.Contains(d, "category=account") || !strings.Contains(d, "recoverable=false") || !strings.Contains(d, "remedy=") {
		t.Fatalf("digest missing account evidence: %s", d)
	}

	// Surface 2: the crash dump file.
	sl.WriteCrashDump(er)
	if er.DumpPath == "" {
		t.Fatal("dump path not recorded")
	}
	raw, err := os.ReadFile(er.DumpPath)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bad dump JSON: %v", err)
	}
	if doc["category"] != "account" || doc["recoverable_hint"] != false {
		t.Fatalf("dump category/hint = %v/%v, want account/false", doc["category"], doc["recoverable_hint"])
	}
	remedy, _ := doc["remedy"].(string)
	if !strings.Contains(remedy, "1113") || !strings.Contains(remedy, "top up") {
		t.Fatalf("dump remedy = %q", remedy)
	}

	// Surface 1: the session_end error block.
	if err := sl.LogSessionEnd(1, 3, 100, 5.5, er, EndCause{Reason: sessionevent.SessionEndReasonRunFailed}); err != nil {
		t.Fatalf("LogSessionEnd: %v", err)
	}
	sl.Close() //stylegate:ignore test forces the flush before reading the JSONL; the assertions below verify both the error block and the crash dump, so a lost flush fails them
	data, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var end struct {
		Error map[string]interface{} `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &end); err != nil {
		t.Fatalf("unmarshal session_end: %v", err)
	}
	if end.Error["category"] != "account" || end.Error["recoverable_hint"] != false {
		t.Fatalf("session_end error block = %v", end.Error)
	}
	endRemedy, _ := end.Error["remedy"].(string)
	if !strings.Contains(endRemedy, "top up") {
		t.Fatalf("session_end remedy = %q", endRemedy)
	}
}

// TestGenuine429Evidence_NoRemedy — the regression guard on the EVIDENCE
// surface: a genuine rate limit keeps category 4xx + recoverable true and
// must NOT grow a remedy field (the dump/session_end contract stays stable
// for every non-account failure).
func TestGenuine429Evidence_NoRemedy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited, retry later"}`))
	}))
	defer srv.Close()

	sl := newTestSession(t)
	c := &deepseek.Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: deepseek.RetryConfig{MaxRetries: 0},
	}
	_, _ = c.ClaudeChat(context.Background(), &deepseek.ClaudeRequest{Model: "m", MaxTokens: 10, Messages: []deepseek.ClaudeMessage{{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "hi"}}}}}) //stylegate:ignore chat round in a stub-backed fixture; the recorded request is the assertion

	a := &Agent{Client: c, SessionLog: sl}
	er := a.collectErrorRecord(nil, 1, 10, 1.0)
	if er.Category != string(deepseek.CategoryHTTP4xx) || !er.RecoverableHint || er.Remedy != "" {
		t.Fatalf("genuine 429 record = category %q recoverable %t remedy %q", er.Category, er.RecoverableHint, er.Remedy)
	}
	sl.WriteCrashDump(er)
	raw, err := os.ReadFile(er.DumpPath)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bad dump JSON: %v", err)
	}
	if _, has := doc["remedy"]; has {
		t.Fatalf("non-account dump must not carry a remedy field: %v", doc)
	}
	if doc["category"] != "4xx" || doc["recoverable_hint"] != true {
		t.Fatalf("dump = %v/%v, want 4xx/true", doc["category"], doc["recoverable_hint"])
	}
}

// TestSummaryDigest: the task-ledger failed tail (notification pipe).
func TestSummaryDigest(t *testing.T) {
	er := &ErrorRecord{
		Category:        "4xx",
		HTTPStatus:      400,
		FailedIteration: 5,
		ErrorBody:       `deepseek: API error (status 400): input is invalid`,
		DumpPath:        "/tmp/x.dump.json",
	}
	d := er.SummaryDigest()
	for _, want := range []string{"category=4xx", "http=400", "iter=5", "recoverable=false", "dump=/tmp/x.dump.json", "input is invalid"} {
		if !strings.Contains(d, want) {
			t.Fatalf("digest %q missing %q", d, want)
		}
	}
}

// TestRecoverRunPanic_WritesDumpAndRepanics (epic ④): recover writes the
// breadcrumbs then re-panics — the exit code survives.
func TestRecoverRunPanic_WritesDumpAndRepanics(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sl, err := NewSessionLogger("m", "")
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{SessionLog: sl}
	finishCalls := 0
	finish := func(exitCode, iter, tokens int, elapsed float64) {
		finishCalls++
		if exitCode != 1 {
			t.Errorf("finish exitCode = %d, want 1", exitCode)
		}
	}

	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("re-panic expected — recover must not swallow the crash")
			}
			if r != "boom" {
				t.Fatalf("re-panic value = %v, want boom", r)
			}
		}()
		a.recoverRunPanic("boom", finish)
	}()

	if finishCalls != 1 {
		t.Fatalf("finish called %d times, want 1", finishCalls)
	}
	dump := strings.TrimSuffix(sl.Path(), ".jsonl") + ".dump.json"
	raw, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("panic dump missing: %v", err)
	}
	if !strings.Contains(string(raw), "boom") || !strings.Contains(string(raw), "panic_stack") {
		t.Fatalf("panic dump content: %s", raw)
	}
	// session_end(error) was written before close.
	data, _ := os.ReadFile(sl.Path()) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if !strings.Contains(string(data), `"panic"`) {
		t.Fatalf("session_end missing panic error block: %s", data)
	}
}

// TestErrorCategoryMatrix (bug036 contract): the classification table.
func TestErrorCategoryMatrix(t *testing.T) {
	cases := []struct {
		snap     deepseek.FailureSnapshot
		wantCat  string
		wantHint bool
	}{
		{deepseek.FailureSnapshot{TransportErr: "dial tcp: connection refused"}, "network", true},
		{deepseek.FailureSnapshot{StatusCode: 500, Body: "oops"}, "5xx", true},
		{deepseek.FailureSnapshot{StatusCode: 503, Body: "overloaded"}, "5xx", true},
		{deepseek.FailureSnapshot{StatusCode: 400, Body: "bad shape"}, "4xx", false},
		{deepseek.FailureSnapshot{StatusCode: 400, Body: "1214 invalid"}, "4xx", true},
		{deepseek.FailureSnapshot{StatusCode: 429, Body: "rate limited"}, "4xx", true},
		// Account-level: the SAME 429 status as the row above, but carrying
		// GLM's insufficient-balance code — a dead account, not a rate limit
		// (this row is the taxonomy's whole reason to exist; the 23:00 HP
		// worker death was recorded recoverable_hint:true before it).
		{deepseek.FailureSnapshot{StatusCode: 429, Body: `{"type":"error","error":{"type":"rate_limit_error","code":"1113","message":"余额不足"}}`}, "account", false},
		// The same verdict on the OpenAI-wire flat shape ({"code":"1113"} at
		// the top level): the account code is what decides, not the envelope.
		{deepseek.FailureSnapshot{StatusCode: 429, Body: `{"code":"1113"}`}, "account", false},
		{deepseek.FailureSnapshot{StatusCode: 402, Body: `{"error":"payment required"}`}, "account", false},
		{deepseek.FailureSnapshot{StatusCode: 401, Body: "bad key"}, "4xx", false},
		{deepseek.FailureSnapshot{Body: "stream error: overloaded"}, "vendor", true},
		{deepseek.FailureSnapshot{}, "unknown", false},
	}
	for i, c := range cases {
		if got := string(c.snap.Category()); got != c.wantCat {
			t.Errorf("case %d: category = %s, want %s (%+v)", i, got, c.wantCat, c.snap)
		}
		if got := c.snap.RecoverableHint(); got != c.wantHint {
			t.Errorf("case %d: recoverable = %t, want %t", i, got, c.wantHint)
		}
	}
}

// TestDetectOrphanSessions_SideFileIsNeverACandidate is the session-tree
// accounting regression for the orphan sweep: a session dir holding
// session.jsonl + signal-provenance.jsonl must offer exactly ONE candidate —
// the record. Before the layout-keyed scan (internal/sessionlayout), a bare
// "*.jsonl" glob offered the provenance record too, and a provenance line
// carries no session_start (it is a signal record), so it would classify as
// OrphanUnknown — silent by luck, and one JSON-schema change away from being
// reported as a crashed session whose "session log" is not a session at all.
//
// The assertion is on the CANDIDATE SET, not on the orphan count, so it holds
// whether or not the fixture crashes: the side file must never be examined.
func TestDetectOrphanSessions_SideFileIsNeverACandidate(t *testing.T) {
	dir := orphanTestSessionsDir(t)
	sessionID := "55551111-2222-4333-8444-555555555555"
	record := writeSessionJSONL(t, dir, sessionID+sessionlayout.JSONLSuffix, deadStampStartLine(sessionID)+"\n", testOrphanAge)
	// The side file the SIGTERM path writes beside the record — backdated the
	// same way, so a glob would have swept it as a quiet "session" too.
	side := filepath.Join(dir, signalrec.FileName)
	if err := os.WriteFile(side, []byte(`{"kind":"signal","signal":"SIGTERM"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, side, testOrphanAge)

	records, err := sessionlayout.Records(dir)
	if err != nil {
		t.Fatalf("sessionlayout.Records: %v", err)
	}
	if len(records) != 1 || records[0].Path != record {
		t.Fatalf("candidate records = %+v, want exactly the session record %s (the provenance side file is not a session)", records, record)
	}

	// The sweep itself: the genuine crash is reported, and nothing in the
	// result is the side file.
	orphans := DetectOrphanSessions()
	if len(orphans) != 1 || orphans[0] != record {
		t.Fatalf("orphans = %v, want exactly [%s]", orphans, record)
	}
	for _, o := range orphans {
		if filepath.Base(o) == signalrec.FileName {
			t.Errorf("the provenance side file was reported as a crashed session: %s", o)
		}
	}

	// And no dump is ever written for the side file (the dump family is
	// derived from record paths only).
	if _, err := os.Stat(strings.TrimSuffix(side, sessionlayout.JSONLSuffix) + dumpFileSuffix); err == nil {
		t.Fatalf("a crash dump was written for the provenance side file: %s", side)
	}
}
