package hippocampus

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// Test doubles: a scripted stub LLM client (no network, fully deterministic)
// ---------------------------------------------------------------------------

// stubClient returns canned replies in order; the last reply repeats. It
// records every prompt so a test can assert the request shape.
type stubClient struct {
	mu      sync.Mutex
	replies []string
	err     error
	delay   time.Duration
	calls   []string
}

func (s *stubClient) Chat(ctx context.Context, system, user string) (string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, user)
	idx := len(s.calls) - 1
	reply, err, delay := "", s.err, s.delay
	if len(s.replies) > 0 {
		if idx < len(s.replies) {
			reply = s.replies[idx]
		} else {
			reply = s.replies[len(s.replies)-1]
		}
	}
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err != nil {
		return "", err
	}
	return reply, nil
}

func (s *stubClient) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// setMemoryDB points the memory engine at a fresh temp knowledge base.
func setMemoryDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	return path
}

// newTestEngine wires the real in-process memory surface (ToolsMemory) with a
// scripted stub client — the whole cycle is network-free.
func newTestEngine(t *testing.T, replies ...string) (*Engine, *stubClient) {
	t.Helper()
	setMemoryDB(t)
	stub := &stubClient{replies: replies}
	return NewWithConfig(stub, ToolsMemory{}, testConfig()), stub
}

func testConfig() Config {
	c := DefaultConfig()
	c.Timeout = 5 * time.Second
	c.ClientTimeout = 2 * time.Second
	return c
}

// ---------------------------------------------------------------------------
// (a) retrieval returns leads from a SEEDED temp DB
// ---------------------------------------------------------------------------

// TestRunRetrievalReturnsSeededLeads: a seeded record is recalled through the
// divergent fan-out, the hits are structured (id/tier/confidence), and the
// decision verb is executed — the minimal end-to-end cycle.
func TestRunRetrievalReturnsSeededLeads(t *testing.T) {
	path := setMemoryDB(t)
	seedMemory(t, "SQLite WAL concurrency",
		"why does the memory DB block on concurrent writes?",
		"Set busy_timeout and one serialized connection; WAL lets readers proceed during a write. keywords: sqlite wal busy_timeout concurrency",
		[]string{"sqlite", "wal", "busy_timeout"})

	if _, err := tools.MemorySearchHits("sqlite wal busy_timeout", 5, RetrievalIntent); err != nil {
		t.Fatalf("seeded DB is not searchable: %v", err)
	}
	_ = path

	stub := &stubClient{replies: []string{
		`{"queries":["sqlite wal busy_timeout concurrency","memory DB concurrent write lock"]}`,
		`{"verb":"none","reason":"nothing new to store"}`,
	}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "investigating a sqlite wal busy_timeout lock on the memory DB", TaskID: "task-1"})
	if err != nil {
		t.Fatalf("Run returned a caller-side error: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatalf("no leads recalled from the seeded DB (query=%q, rounds=%d, stop=%s)", res.Query, res.Rounds, res.StopReason)
	}
	if res.Hits[0].ID <= 0 || res.Hits[0].Tier == "" {
		t.Errorf("lead is not structured: %+v", res.Hits[0])
	}
	if res.StopReason != StopNoneEmitted {
		t.Errorf("stop reason = %q, want %q", res.StopReason, StopNoneEmitted)
	}
	if len(res.Actions) != 1 || res.Actions[0].Verb != VerbNone {
		t.Fatalf("actions = %+v, want exactly one 'none'", res.Actions)
	}
	if res.Err != nil {
		t.Errorf("unexpected recorded failure: %v", res.Err)
	}
}

// ---------------------------------------------------------------------------
// (b) every decision verb performs the right DB mutation, read back
// ---------------------------------------------------------------------------

// TestVerbStoreWritesRecordReadBack: "store" distills a row; the row is read
// back from the DB (not from the return value) and carries the source tag +
// keyword breadcrumbs.
func TestVerbStoreWritesRecordReadBack(t *testing.T) {
	setMemoryDB(t)
	stub := &stubClient{replies: []string{
		`{"queries":["hippocampus core loop"]}`,
		`{"verb":"store","topic":"hippocampus bounded cycle","question":"how is the cycle bounded?","answer":"Named constants for max rounds, max act and timeout; the loop returns a partial result on expiry. keywords: hippocampus bounded cycle budget timeout","keywords":["hippocampus","bounded","cycle"]}`,
	}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "build the hippocampus bounded cycle", TaskID: "task-7"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopEmptySearch {
		t.Fatalf("stop = %q (actions=%+v), want %q", res.StopReason, res.Actions, StopEmptySearch)
	}
	act := res.Actions[0]
	if act.Verb != VerbStore || act.Err != "" {
		t.Fatalf("store action = %+v", act)
	}

	hits, err := tools.MemorySearchHits("hippocampus bounded cycle", 5, RetrievalIntent)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var found *tools.MemoryHit
	for i := range hits {
		if strings.Contains(hits[i].Topic, "hippocampus bounded cycle") {
			found = &hits[i]
		}
	}
	if found == nil {
		t.Fatalf("stored record not found in DB: %+v", hits)
	}
	if !containsStr(found.Sources, SourceTag) {
		t.Errorf("stored source = %v, want it to carry %q", found.Sources, SourceTag)
	}
	if !strings.Contains(found.Answer, "keywords:") {
		t.Errorf("keyword breadcrumbs missing from stored answer: %q", found.Answer)
	}
	if !strings.Contains(found.Answer, "task-7") && !strings.Contains(strings.Join(found.Sources, " "), "task-7") {
		t.Errorf("provenance (task-7) missing: sources=%v answer=%q", found.Sources, found.Answer)
	}
}

// TestVerbFetchReadsRecordReadBack: "fetch" returns an existing row.
func TestVerbFetchReadsRecordReadBack(t *testing.T) {
	setMemoryDB(t)
	id := seedMemory(t, "fetch target", "what does fetch do?", "It reads one row back. keywords: fetch read target", nil)

	stub := &stubClient{replies: []string{
		`{"queries":["fetch target"]}`,
		fmt.Sprintf(`{"verb":"fetch","id":%d}`, id),
	}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "check fetch target"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	act := res.Actions[0]
	if act.Verb != VerbFetch || act.Err != "" {
		t.Fatalf("fetch action = %+v", act)
	}
	if !strings.Contains(act.Detail, "fetch target") {
		t.Errorf("fetch detail = %q, want the DB render of #%d", act.Detail, id)
	}
	// Read the DB back independently.
	got, err := tools.MemoryGet(id)
	if err != nil || !strings.Contains(got, "fetch target") {
		t.Errorf("MemoryGet(%d) = %q, %v", id, got, err)
	}
}

// TestVerbUpdateMutatesRecordReadBack: "update" re-tiers a row.
func TestVerbUpdateMutatesRecordReadBack(t *testing.T) {
	setMemoryDB(t)
	id := seedMemory(t, "update target", "what does update do?", "It edits one row. keywords: update tier target", nil)

	stub := &stubClient{replies: []string{
		`{"queries":["update target"]}`,
		fmt.Sprintf(`{"verb":"update","id":%d,"tier":"cold","confidence":0.4,"reason":"superseded"}`, id),
	}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "re-tier update target"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if act := res.Actions[0]; act.Verb != VerbUpdate || act.Err != "" {
		t.Fatalf("update action = %+v", act)
	}
	hits, err := tools.MemorySearchHits("update target", 5, RetrievalIntent)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var found *tools.MemoryHit
	for i := range hits {
		if hits[i].ID == id {
			found = &hits[i]
		}
	}
	if found == nil {
		t.Fatalf("row #%d no longer recallable after update: %+v", id, hits)
	}
	if found.Tier != "cold" {
		t.Errorf("tier after update = %q, want cold", found.Tier)
	}
	if found.Confidence != 0.4 {
		t.Errorf("confidence after update = %v, want 0.4", found.Confidence)
	}
}

// TestVerbFeedbackRecordsVoteReadBack: "feedback" persists a vote and moves
// the row's decayed feedback mass.
func TestVerbFeedbackRecordsVoteReadBack(t *testing.T) {
	setMemoryDB(t)
	id := seedMemory(t, "feedback target", "was this adopted?", "Vote on it. keywords: feedback vote target", nil)

	stub := &stubClient{replies: []string{
		`{"queries":["feedback target"]}`,
		fmt.Sprintf(`{"verb":"feedback","id":%d,"rating":"useful","reason":"adopted the recipe"}`, id),
	}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "judge feedback target", TaskID: "task-9"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if act := res.Actions[0]; act.Verb != VerbFeedback || act.Err != "" {
		t.Fatalf("feedback action = %+v", act)
	}
	hits, err := tools.MemorySearchHits("feedback target", 5, RetrievalIntent)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var found *tools.MemoryHit
	for i := range hits {
		if hits[i].ID == id {
			found = &hits[i]
		}
	}
	if found == nil {
		t.Fatalf("row #%d vanished after feedback: %+v", id, hits)
	}
	if found.FbUseful <= 0 {
		t.Errorf("fb_useful after a useful vote = %v, want > 0", found.FbUseful)
	}
}

// TestVerbNoneMutationless: "none" touches nothing (the DB stays as seeded).
func TestVerbNoneMutationless(t *testing.T) {
	setMemoryDB(t)
	id := seedMemory(t, "none target", "does none mutate?", "No. keywords: none untouched target", nil)

	stub := &stubClient{replies: []string{
		`{"queries":["none untouched target"]}`,
		`{"verb":"none","reason":"no gap worth filling"}`,
	}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "nothing to do about none untouched target"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopNoneEmitted {
		t.Fatalf("stop = %q, want %q", res.StopReason, StopNoneEmitted)
	}
	if res.Actions[0].Detail != "no action" {
		t.Errorf("none action detail = %q", res.Actions[0].Detail)
	}
	before, err := tools.MemoryGet(id)
	if err != nil {
		t.Fatalf("MemoryGet: %v", err)
	}
	if !strings.Contains(before, "none target") {
		t.Errorf("row mutated by a no-op decision: %q", before)
	}
}

// TestIllegalAndRefusedVerbsAreRecorded: a bad verb, a bad rating and an
// incomplete store are RECORDED (Rule 15), never silently swallowed.
func TestIllegalAndRefusedVerbsAreRecorded(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{"illegal verb", `{"verb":"delete","id":1}`, "illegal verb"},
		{"bad rating", `{"verb":"feedback","id":1,"rating":"meh"}`, "must be useful/useless/unused"},
		{"incomplete store", `{"verb":"store","topic":"only a topic"}`, "needs topic+question+answer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setMemoryDB(t)
			id := seedMemory(t, "recorded failures", "is this recorded?", "Yes. keywords: recorded failure target", nil)
			_ = id
			reply := strings.ReplaceAll(c.reply, `"id":1`, fmt.Sprintf(`"id":%d`, id))
			stub := &stubClient{replies: []string{`{"queries":["recorded failure target"]}`, reply}}
			e := NewWithConfig(stub, ToolsMemory{}, testConfig())

			res, err := e.Run(context.Background(), Request{Context: "recorded failure target"})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Err == nil {
				t.Fatalf("failure not recorded on the result (actions=%+v)", res.Actions)
			}
			if !strings.Contains(res.Err.Error(), c.want) {
				t.Errorf("recorded failure = %q, want it to mention %q", res.Err, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// (c) network-free determinism with the stub only
// ---------------------------------------------------------------------------

// TestStubOnlyDeterminism: the engine only ever talks through the injected
// Client (no network) and two identical runs produce identical results.
func TestStubOnlyDeterminism(t *testing.T) {
	setMemoryDB(t)
	replies := []string{
		`{"queries":["determinism check"]}`,
		`{"verb":"none","reason":"stable"}`,
	}
	run := func() *Result {
		stub := &stubClient{replies: replies}
		e := NewWithConfig(stub, ToolsMemory{}, testConfig())
		res, err := e.Run(context.Background(), Request{Context: "determinism check"})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		return res
	}
	a, b := run(), run()
	if a.StopReason != b.StopReason || a.Query != b.Query || a.Rounds != b.Rounds || len(a.Actions) != len(b.Actions) {
		t.Errorf("runs diverged:\n a=%+v\n b=%+v", a, b)
	}
	if a.Err != nil || b.Err != nil {
		t.Errorf("unexpected failures: a=%v b=%v", a.Err, b.Err)
	}
}

// TestClientFailureStopsCycle: an LLM error stops the cycle (no unbounded
// retry) and is recorded.
func TestClientFailureStopsCycle(t *testing.T) {
	setMemoryDB(t)
	stub := &stubClient{err: errors.New("upstream 500")}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "a task that cannot reach the model"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopClientError {
		t.Errorf("stop = %q, want %q", res.StopReason, StopClientError)
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "upstream 500") {
		t.Errorf("client failure not recorded: %v", res.Err)
	}
	if stub.callCount() != 1 {
		t.Errorf("client called %d times, want exactly 1 (no retry loop)", stub.callCount())
	}
}

// ---------------------------------------------------------------------------
// (d) the bounded cycle stops on budget / timeout instead of looping
// ---------------------------------------------------------------------------

// TestRoundBudgetStopsCycle: with one round allowed, a cycle that keeps
// asking for work stops at the round limit.
func TestRoundBudgetStopsCycle(t *testing.T) {
	setMemoryDB(t)
	seedMemory(t, "budget target", "does the loop spin?", "No. keywords: budget round target", nil)

	stub := &stubClient{replies: []string{
		`{"queries":["budget round target"]}`,
		`{"verb":"fetch","id":1}`,
	}}
	cfg := testConfig()
	cfg.MaxRounds = 1
	cfg.MaxAct = 10
	e := NewWithConfig(stub, ToolsMemory{}, cfg)

	res, err := e.Run(context.Background(), Request{Context: "budget round target"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopRoundLimit {
		t.Errorf("stop = %q, want %q", res.StopReason, StopRoundLimit)
	}
	if len(res.Actions) != 1 {
		t.Errorf("actions = %d, want 1 (the round budget)", len(res.Actions))
	}
	if stub.callCount() != 2 {
		t.Errorf("client calls = %d, want 2 (one retrieval + one decision, no more)", stub.callCount())
	}
}

// TestActBudgetStopsCycle: the act budget caps mutations even if rounds allow.
func TestActBudgetStopsCycle(t *testing.T) {
	setMemoryDB(t)
	seedMemory(t, "hippocampus act budget", "is there a hit to continue on?", "Yes. keywords: hippocampus act budget target", nil)
	stub := &stubClient{replies: []string{
		`{"queries":["hippocampus act budget target"]}`,
		`{"verb":"store","topic":"hippocampus store budget","question":"how many stores per cycle?","answer":"At most MaxAct. keywords: hippocampus store budget","keywords":["hippocampus"]}`,
	}}
	cfg := testConfig()
	cfg.MaxRounds = 5
	cfg.MaxAct = 1
	e := NewWithConfig(stub, ToolsMemory{}, cfg)

	res, err := e.Run(context.Background(), Request{Context: "build the hippocampus store budget"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopActBudget {
		t.Fatalf("stop = %q, want %q (actions=%d)", res.StopReason, StopActBudget, len(res.Actions))
	}
	if len(res.Actions) != cfg.MaxAct {
		t.Errorf("actions = %d, want %d", len(res.Actions), cfg.MaxAct)
	}
}

// TestTimeoutReturnsPartialResult: a slow client cannot make the cycle run
// past the wall-clock ceiling — it returns the partial result, never loops.
func TestTimeoutReturnsPartialResult(t *testing.T) {
	setMemoryDB(t)
	seedMemory(t, "timeout target", "does it wait forever?", "No. keywords: timeout partial target", nil)

	stub := &stubClient{delay: 30 * time.Second, replies: []string{`{"queries":["timeout partial target"]}`}}
	cfg := testConfig()
	cfg.Timeout = 100 * time.Millisecond
	cfg.ClientTimeout = 60 * time.Second // the CYCLE deadline must be the binding one
	e := NewWithConfig(stub, ToolsMemory{}, cfg)

	start := time.Now()
	res, err := e.Run(context.Background(), Request{Context: "timeout partial target"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run must return a partial result, got caller-side error: %v", err)
	}
	if res == nil {
		t.Fatal("Run returned a nil result on timeout")
	}
	if res.StopReason != StopTimeout {
		t.Errorf("stop = %q, want %q", res.StopReason, StopTimeout)
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout not honored: cycle took %v", elapsed)
	}
	if stub.callCount() != 1 {
		t.Errorf("client calls = %d, want 1 (stopped after the first blocked call)", stub.callCount())
	}
}

// TestCancelledContextStopsCycle: a cancelled parent context is a stop, not a
// spin.
func TestCancelledContextStopsCycle(t *testing.T) {
	setMemoryDB(t)
	stub := &stubClient{delay: 10 * time.Second, replies: []string{`{"queries":["cancelled"]}`}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep recency ranking needs distinguishable timestamps
		cancel()
	}()
	res, err := e.Run(ctx, Request{Context: "cancelled task context"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopTimeout {
		t.Errorf("stop = %q, want %q", res.StopReason, StopTimeout)
	}
}

// TestEmptyContextStopsWithoutCallingTheModel: nothing to fan out from ⇒ a
// recorded no-op, and the model is never called.
func TestEmptyContextStopsWithoutCallingTheModel(t *testing.T) {
	setMemoryDB(t)
	stub := &stubClient{replies: []string{`{"queries":["x"]}`}}
	e := NewWithConfig(stub, ToolsMemory{}, testConfig())

	res, err := e.Run(context.Background(), Request{Context: "   "})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.StopReason != StopNoQuery {
		t.Errorf("stop = %q, want %q", res.StopReason, StopNoQuery)
	}
	if stub.callCount() != 0 {
		t.Errorf("client called %d times for an empty context, want 0", stub.callCount())
	}
}

// TestNilCollaboratorsRejected: a misconfigured engine fails loudly.
func TestNilCollaboratorsRejected(t *testing.T) {
	setMemoryDB(t)
	stub := &stubClient{}
	if _, err := NewWithConfig(nil, ToolsMemory{}, testConfig()).Run(context.Background(), Request{Context: "x"}); err == nil {
		t.Error("nil client accepted")
	}
	if _, err := NewWithConfig(stub, nil, testConfig()).Run(context.Background(), Request{Context: "x"}); err == nil {
		t.Error("nil memory accepted")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// seedMemory distills one record into the (temp) knowledge base and returns
// its id.
func seedMemory(t *testing.T, topic, question, answer string, keywords []string) int {
	t.Helper()
	if _, err := tools.MemoryStore(topic, question, answer, "seed", keywords); err != nil {
		t.Fatalf("seed %q: %v", topic, err)
	}
	hits, err := tools.MemorySearchHits(question, 5, RetrievalIntent)
	if err != nil {
		t.Fatalf("seed lookup: %v", err)
	}
	for _, h := range hits {
		if h.Topic == topic {
			return h.ID
		}
	}
	t.Fatalf("seeded record %q not recallable", topic)
	return 0
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
