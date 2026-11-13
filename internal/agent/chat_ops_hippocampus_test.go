package agent

// chat_ops_hippocampus_test.go — the two chat-side hippocampus deliverables,
// pinned on the wire and in the session JSONL:
//
//	(A) the ROUND STAMP: every ChatOps round sets a.hippocampusRound, so a
//	    chat session's archived brief carries the round it belongs to instead
//	    of the zero value (which is what the field held before this change —
//	    only the task loop, runEngine, ever wrote it).
//	(B) the per-TURN one-shot memory(action=store) CHECKPOINT: fires exactly
//	    once for a turn that did real tool work, never on a turn that only
//	    talked, never when a store already succeeded, and never at all with the
//	    gate off.
//
// Both are asserted against a REAL httptest SSE server driving the real
// ChatOps loop (Rule 31: the changed surface is executed, not mocked).

import (
	"context"
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

	"github.com/nick/gopi/internal/toolnames"
)

// chatOpsStoreProbePath is the file the fake tool round reads. It EXISTS (the
// checkpoint's "the turn did real work" predicate must be honest), and it is
// created by the test rather than read from the real tree.
const chatOpsStoreProbeBody = "CHAT-CHECKPOINT-PROBE"

// chatOpsHippoAgent is chatOpsAgent plus a REAL session log (so the archive
// and the injected checkpoint are readable from disk) and a bound host whose
// recall answers with one fixed brief.
func chatOpsHippoAgent(t *testing.T, server *httptest.Server, recalled *hippocampusBrief) *Agent {
	t.Helper()
	newTestAgentEnv(t)
	ag := chatOpsAgent(t, server)
	ag.Logger = log.New(io.Discard, "", 0)
	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() {
		//stylegate:ignore test cleanup: SessionLogger.Close flushes the JSONL the test already read
		sl.Close()
	})
	ag.SessionLog = sl
	// A hand-built agent has NO host (only NewAgent builds one from the config
	// when the gate is on), so bind the enabled host directly — this is the
	// gate-ON shape the chat path must behave correctly under.
	ag.Hippocampus = newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)
	if ag.Hippocampus == nil {
		t.Fatal("newHippocampusHost returned nil for an enabled config")
	}
	if recalled != nil {
		if !ag.SetHippocampusRecall(stubRecall(t, new(int32), recalled, nil)) {
			t.Fatal("SetHippocampusRecall refused a host that was just built")
		}
	}
	return ag
}

// chatOpsProbeFile writes the file the tool round reads and returns its path.
func chatOpsProbeFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "probe.txt")
	if err := os.WriteFile(p, []byte(chatOpsStoreProbeBody), 0o644); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	return p
}

// storeCheckpointUserEvents counts the session-log `user` events carrying the
// checkpoint text — the audit half of the injection (an injected wire message
// that never reaches the JSONL is indistinguishable from a missing one).
func storeCheckpointUserEvents(t *testing.T, path string) int {
	t.Helper()
	n := 0
	for _, ev := range readSessionJSONL(t, path) {
		if ev["type"] != "user" {
			continue
		}
		msg, ok := ev["message"].(map[string]interface{})
		if !ok {
			continue
		}
		if s, _ := msg["content"].(string); strings.Contains(s, chatOpsTurnStoreMarker) {
			n++
		}
	}
	return n
}

// requestCarriesCheckpoint reports whether any request body the fake model
// received carries the checkpoint — the WIRE half (the model must actually be
// asked, not merely logged about).
func requestCarriesCheckpoint(bodies []string) bool {
	for _, b := range bodies {
		if strings.Contains(b, chatOpsTurnStoreMarker) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// (A) The round stamp.
// ---------------------------------------------------------------------------

// TestHippocampus_ChatOpsRoundStamp proves the round a chat round archives
// under: with a bound host the round boundary archives one brief per round, and
// each archived brief's "round" is the round it was retrieved on — never 0.
//
// Before this change a.hippocampusRound was written ONLY by runEngine
// (loop.go, the task loop), so every chat-path archive landed under round 0
// and a chat session's audit trail could not be ordered by round at all.
func TestHippocampus_ChatOpsRoundStamp(t *testing.T) {
	probe := chatOpsProbeFile(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n <= 2 {
			claudeToolUseStream(w, toolnames.ToolRead, map[string]interface{}{"path": probe})
			return
		}
		claudeTextStream(w, "read it twice — nothing else to do.", "end_turn")
	}))
	t.Cleanup(srv.Close)

	// A different brief per cycle, so the convergence gate cannot suppress the
	// second round's delivery (this test is about the stamp, not the noise gate).
	brief := func(n int) *hippocampusBrief {
		return &hippocampusBrief{Confidence: 0.9, MemoryWeight: 0.8, Memories: 1,
			Summary: fmt.Sprintf("round-stamp probe %d", n)}
	}
	// The stub records the ROUND each launched retrieval carries — the host's
	// own decision, labeled with the round it belongs to (the same value the
	// archive then lands under).
	ag := chatOpsHippoAgent(t, srv, nil)
	var stubMu sync.Mutex
	var launched []int
	var n int32
	if !ag.SetHippocampusRecall(func(_ context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error) {
		stubMu.Lock()
		launched = append(launched, req.Round)
		stubMu.Unlock()
		return brief(int(atomic.AddInt32(&n, 1))), nil
	}) {
		t.Fatal("SetHippocampusRecall refused the recording stub")
	}

	if _, err := ag.ChatOps("read the probe twice", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if err := ag.SessionLog.Sync(); err != nil {
		t.Fatalf("session sync: %v", err)
	}

	var rounds []int
	for _, ev := range readSessionJSONL(t, ag.SessionLog.Path()) {
		if ev["type"] != hippocampusBriefEvent {
			continue
		}
		r, ok := ev[hippocampusLogKeyRound].(float64)
		if !ok {
			t.Fatalf("%s event has no numeric round: %v", hippocampusBriefEvent, ev)
		}
		rounds = append(rounds, int(r))
	}
	if len(rounds) == 0 {
		t.Fatalf("no %s event archived — the chat round boundary never delivered (session event types: %v)",
			hippocampusBriefEvent, sessionEventTypes(t, ag.SessionLog.Path()))
	}
	for i, r := range rounds {
		if r <= 0 {
			t.Fatalf("archive %d carries round=%d — the chat path never stamped a.hippocampusRound (want 1..N)", i, r)
		}
		if i > 0 && r < rounds[i-1] {
			t.Fatalf("archived rounds are not monotonic: %v", rounds)
		}
	}
	// The launched retrievals are the sharpest form of the claim: the boundary
	// of round k launches its retrieval WITH round k, so the stamped rounds are
	// exactly the rounds the turn ran (nothing is stamped 0, and no two rounds
	// share a stamp).
	stubMu.Lock()
	got := append([]int{}, launched...)
	stubMu.Unlock()
	if len(got) == 0 {
		t.Fatal("no retrieval was launched — the round boundary never ran")
	}
	// The schedule is exact: the start-of-run launch carries round 0, and the
	// boundary of round k launches round k — so the launched rounds are
	// 0,1,2,…,N with N the number of rounds the turn ran. A missing stamp
	// leaves the zero value everywhere (the defect), which shows up here as a
	// repeated 0 and a short tail.
	for i, r := range got {
		if r != i {
			t.Fatalf("launched rounds = %v, want 0..%d — entry %d was stamped %d", got, len(got)-1, i, r)
		}
	}
	ran := int(atomic.LoadInt32(&calls))
	if len(got) != ran+1 {
		t.Fatalf("round boundaries launched %d retrievals for a turn that ran %d rounds, want %d", len(got), ran, ran+1)
	}
	if want := int32(0); calls != want && got[len(got)-1] != ran {
		t.Fatalf("last launched round = %d, want %d", got[len(got)-1], ran)
	}
}

// TestHippocampus_ChatOpsRoundStampIsSessionCumulative pins the property that
// makes the stamp USEFUL rather than merely non-zero: one session log spans
// several turns (PersistChatSession), so the counter must be monotonic ACROSS
// turns. A per-turn counter restarts at 1 and repeats round numbers inside the
// same log — measured on the real binary before this test existed, as the
// archive trail [1,2,3,1,1,2] for a three-turn session.
func TestHippocampus_ChatOpsRoundStampIsSessionCumulative(t *testing.T) {
	probe := chatOpsProbeFile(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		// Two tool rounds per turn, then the answer: a bounded turn (well under
		// the round cap and under every host budget), so both turns run their
		// boundaries normally.
		if n%3 == 1 || n%3 == 2 {
			claudeToolUseStream(w, toolnames.ToolRead, map[string]interface{}{"path": probe})
			return
		}
		claudeTextStream(w, "read the probe.", "end_turn")
	}))
	t.Cleanup(srv.Close)

	// A DIFFERENT brief per retrieval: the round boundary suppresses a repeat of
	// the same delivery (the noise gate), and this test is about numbering, not
	// about the gate.
	ag := chatOpsHippoAgent(t, srv, nil)
	// Multi-turn is the SHAPE this test is about: `gopi chat` keeps one agent
	// and one session log across turns (PersistChatSession), which is precisely
	// why the round counter must not restart. Without it ChatOps closes the log
	// at the end of turn 1.
	ag.PersistChatSession = true
	var n int32
	if !ag.SetHippocampusRecall(func(_ context.Context, _ hippocampusRecallRequest) (*hippocampusBrief, error) {
		k := atomic.AddInt32(&n, 1)
		return &hippocampusBrief{Confidence: 0.9, MemoryWeight: 0.8, Memories: 1,
			Summary: fmt.Sprintf("cumulative-round probe %d", k)}, nil
	}) {
		t.Fatal("SetHippocampusRecall refused the varying stub")
	}

	// Read the ARCHIVED trail (the store of record), per turn: the numbered
	// launch is the host's decision, but the archive is what a later reader of
	// the session sees, and it is the archive whose round numbers must be
	// orderable across turns.
	archived := func() []int {
		if err := ag.SessionLog.Sync(); err != nil {
			t.Fatalf("session sync: %v", err)
		}
		var out []int
		for _, ev := range readSessionJSONL(t, ag.SessionLog.Path()) {
			if ev["type"] != hippocampusBriefEvent {
				continue
			}
			r, _ := ev[hippocampusLogKeyRound].(float64)
			if r <= 0 {
				t.Fatalf("an archive carries round=%d — the stamp is missing (the chat path never wrote a.hippocampusRound)", int(r))
			}
			out = append(out, int(r))
		}
		return out
	}
	runTurn := func(turn int) {
		if _, err := ag.ChatOps("read the probe", nil); err != nil {
			t.Fatalf("turn %d: ChatOps: %v", turn, err)
		}
	}
	runTurn(0)
	first := archived()
	if len(first) == 0 {
		t.Fatal("turn 1 archived nothing — the chat path is not accumulating at all")
	}
	runTurn(1)
	all := archived()
	if len(all) <= len(first) {
		t.Fatalf("turn 2 archived nothing (trail = %v): a later turn must keep accumulating", all)
	}
	// The session's numbering must be strictly increasing across the turn
	// boundary. A per-turn counter restarts at 1 inside the SAME log, so the
	// first archive of turn 2 sorts BELOW turn 1's last — exactly the defect the
	// live binary showed as the trail [1,2,3,1,1,2].
	if all[len(first)] <= all[len(first)-1] {
		t.Fatalf("the trail does not increase across the turn boundary: turn 1 ended at round %d, turn 2 started at %d (trail=%v)",
			all[len(first)-1], all[len(first)], all)
	}
	for i := 1; i < len(all); i++ {
		if all[i] <= all[i-1] {
			t.Fatalf("the archived trail is not strictly increasing: %v", all)
		}
	}
}

// ---------------------------------------------------------------------------
// (B) The per-turn one-shot store checkpoint.
// ---------------------------------------------------------------------------

// TestChatOps_HippocampusStoreCheckpointFiresOnceWithWork pins the positive
// case and its COUNT: a turn that really used tools is asked to store exactly
// once, however many tool rounds it ran, and the ask is visible both on the
// wire and in the session JSONL.
func TestChatOps_HippocampusStoreCheckpointFiresOnceWithWork(t *testing.T) {
	probe := chatOpsProbeFile(t)
	var calls int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		// THREE tool rounds, then the answer: a per-ROUND implementation would
		// inject the checkpoint up to three times.
		if n <= 3 {
			claudeToolUseStream(w, toolnames.ToolRead, map[string]interface{}{"path": probe})
			return
		}
		claudeTextStream(w, "read the probe three times; nothing new to report.", "end_turn")
	}))
	t.Cleanup(srv.Close)

	ag := chatOpsHippoAgent(t, srv, &hippocampusBrief{Confidence: 0.1, Summary: ""})
	if _, err := ag.ChatOps("read the probe", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if err := ag.SessionLog.Sync(); err != nil {
		t.Fatalf("session sync: %v", err)
	}

	if got := countUserMessages(ag, chatOpsTurnStoreCheckpoint); got != 1 {
		t.Fatalf("checkpoint injected %d times into the context, want exactly 1 (one shot per TURN, never per round)", got)
	}
	if routed := int(atomic.LoadInt32(&calls)); routed != 4 {
		t.Fatalf("the turn used %d rounds, want 4 (3 tool rounds + the answer)", routed)
	}
	if !requestCarriesCheckpoint(bodies) {
		t.Fatal("no request body carried the checkpoint — the model was never actually asked")
	}
	// The injected message STAYS in the window (that is honest — it is one
	// harness line), so later requests keep seeing it. The count that proves
	// one-shot-ness is therefore the number of checkpoint COPIES in the last
	// request: three tool rounds would leave three copies.
	if got := strings.Count(bodies[len(bodies)-1], chatOpsTurnStoreMarker); got != 1 {
		t.Fatalf("the last request carries %d copies of the checkpoint, want exactly 1 (one shot per turn)", got)
	}
	if strings.Contains(bodies[0], chatOpsTurnStoreMarker) {
		t.Fatal("the FIRST request already carried the checkpoint — nothing had been done yet")
	}
	if logged := storeCheckpointUserEvents(t, ag.SessionLog.Path()); logged != 1 {
		t.Fatalf("session JSONL carries %d checkpoint user events, want 1 (the injection must be auditable)", logged)
	}
}

// TestChatOps_HippocampusStoreCheckpointSkippedWithoutWork: a turn that never
// used a tool has no knowledge gain to persist, so the checkpoint must not fire
// — asking would invite a fabricated store.
func TestChatOps_HippocampusStoreCheckpointSkippedWithoutWork(t *testing.T) {
	var calls int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, "just talking — no tools needed for this one.", "end_turn")
	}))
	t.Cleanup(srv.Close)

	ag := chatOpsHippoAgent(t, srv, &hippocampusBrief{Confidence: 0.1, Summary: ""})
	if _, err := ag.ChatOps("say something", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if err := ag.SessionLog.Sync(); err != nil {
		t.Fatalf("session sync: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("the turn routed %d rounds, want the single answer round", n)
	}
	if got := countUserMessages(ag, chatOpsTurnStoreCheckpoint); got != 0 {
		t.Fatalf("checkpoint injected %d times on a zero-tool round, want 0 — the turn had nothing to store", got)
	}
	if requestCarriesCheckpoint(bodies) {
		t.Fatal("a zero-tool turn carried the checkpoint on the wire")
	}
	if logged := storeCheckpointUserEvents(t, ag.SessionLog.Path()); logged != 0 {
		t.Fatalf("session JSONL carries %d checkpoint events for a zero-tool turn, want 0", logged)
	}
}

// TestChatOps_HippocampusStoreCheckpointSkippedWhenAlreadyStored: a turn that
// stored for real is not asked again — the same "no redundant store" rule the
// task loop applies through memoryStored.
func TestChatOps_HippocampusStoreCheckpointSkippedWhenAlreadyStored(t *testing.T) {
	probe := chatOpsProbeFile(t)
	var calls int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			claudeToolUseStream(w, toolnames.ToolMemory, map[string]interface{}{
				"action":   toolnames.ToolMemoryActionStore,
				"topic":    "chat checkpoint redundancy guard",
				"question": "does a real store suppress the per-turn chat checkpoint?",
				"answer":   "Yes: the checkpoint reads memoryStored, exactly like the task-end completion checkpoint.",
			})
			return
		}
		if n == 2 {
			claudeToolUseStream(w, toolnames.ToolRead, map[string]interface{}{"path": probe})
			return
		}
		claudeTextStream(w, "stored the lesson, then verified the probe.", "end_turn")
	}))
	t.Cleanup(srv.Close)

	ag := chatOpsHippoAgent(t, srv, &hippocampusBrief{Confidence: 0.1, Summary: ""})
	if _, err := ag.ChatOps("store then read", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if !ag.memoryStored {
		t.Fatal("the successful memory(action=store) tool call did not latch memoryStored")
	}
	if err := ag.SessionLog.Sync(); err != nil {
		t.Fatalf("session sync: %v", err)
	}
	// The stored round and the tool round BOTH ran; the checkpoint had its
	// chance after the second and must have stayed silent.
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("the turn routed %d rounds, want 3", n)
	}
	if got := countUserMessages(ag, chatOpsTurnStoreCheckpoint); got != 0 {
		t.Fatalf("checkpoint injected %d times after a real store, want 0", got)
	}
	if requestCarriesCheckpoint(bodies) {
		t.Fatal("a turn that already stored carried the checkpoint on the wire")
	}
}

// TestChatOps_StoreCheckpointRefusedStoreStillReportsTheToolUse: the session
// JSONL must show the memory(action=store) tool_use REALLY issued — and, when
// the tool refuses it, the refusal must be a recorded result rather than a
// silent nothing (Rule 15 / the report's `memory_store: refused` assertion).
func TestChatOps_StoreCheckpointRefusedStoreStillReportsTheToolUse(t *testing.T) {
	probe := chatOpsProbeFile(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			claudeToolUseStreamID(w, "call_store_empty", toolnames.ToolMemory, map[string]interface{}{
				"action": toolnames.ToolMemoryActionStore,
			})
			return
		}
		if n == 2 {
			claudeToolUseStream(w, toolnames.ToolRead, map[string]interface{}{"path": probe})
			return
		}
		claudeTextStream(w, "store was refused; the probe read fine.", "end_turn")
	}))
	t.Cleanup(srv.Close)

	ag := chatOpsHippoAgent(t, srv, &hippocampusBrief{Confidence: 0.1, Summary: ""})
	if _, err := ag.ChatOps("store nothing, then read", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if err := ag.SessionLog.Sync(); err != nil {
		t.Fatalf("session sync: %v", err)
	}
	if ag.memoryStored {
		t.Fatal("a refused store must NOT latch memoryStored — the knowledge was never persisted")
	}
	events := readSessionJSONL(t, ag.SessionLog.Path())
	issued, refused := 0, 0
	for _, ev := range events {
		switch ev["type"] {
		case "assistant":
			if !sessionEventHasToolUse(ev, toolnames.ToolMemory) {
				continue
			}
			issued++
		case "tool_result":
			if strings.Contains(sessionEventText(ev), "memory_store:") || strings.Contains(sessionEventText(ev), "Missing required parameter") {
				refused++
			}
		}
	}
	if issued != 1 {
		t.Fatalf("session JSONL carries %d memory tool_use records, want 1 (the checkpoint's ask must lead to a real call)", issued)
	}
	if refused != 1 {
		t.Fatalf("session JSONL carries %d recorded store refusals, want 1 — a refused store must be visible, never silent", refused)
	}
	// A refused store does NOT re-open the checkpoint: one shot per turn means
	// one shot, and the latch is set before the injection precisely so a
	// rejection cannot turn the prompt into a loop.
	if got := countUserMessages(ag, chatOpsTurnStoreCheckpoint); got != 1 {
		t.Fatalf("checkpoint injected %d times, want exactly 1 even though the store was refused", got)
	}
}

// TestChatOps_HippocampusStoreCheckpointAbsentWhenGateOff pins the gate-off
// contract on the chat path: with NO host (what loadHippocampusConfig produces
// when GOPI_HIPPOCAMPUS is unset) a tool-using turn is byte-identical in shape
// to the same turn without the feature — no checkpoint anywhere, on the wire or
// in the session log.
func TestChatOps_HippocampusStoreCheckpointAbsentWhenGateOff(t *testing.T) {
	probe := chatOpsProbeFile(t)
	var calls int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		if n <= 2 {
			claudeToolUseStream(w, toolnames.ToolRead, map[string]interface{}{"path": probe})
			return
		}
		claudeTextStream(w, "gate off: identical to yesterday.", "end_turn")
	}))
	t.Cleanup(srv.Close)

	newTestAgentEnv(t)
	if cfg := loadHippocampusConfig(); cfg.Enabled {
		t.Fatal("the gate is on in this environment — the off-contract cannot be asserted")
	}
	ag := chatOpsAgent(t, srv) // hand-built agent: no host, exactly like the gate being off
	ag.Logger = log.New(io.Discard, "", 0)
	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ag.SessionLog = sl

	if _, err := ag.ChatOps("read the probe twice", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("session sync: %v", err)
	}
	records := 0
	for _, ev := range readSessionJSONL(t, sl.Path()) {
		if b, err := json.Marshal(ev); err == nil && strings.Contains(string(b), chatOpsTurnStoreMarker) {
			records++
		}
	}
	if records != 0 {
		t.Fatalf("the session log carries %d checkpoint records with the gate off, want 0", records)
	}
	if requestCarriesCheckpoint(bodies) {
		t.Fatal("the checkpoint rode the wire with the gate off")
	}
	if got := countUserMessages(ag, chatOpsTurnStoreCheckpoint); got != 0 {
		t.Fatalf("checkpoint injected %d times with the gate off, want 0", got)
	}
	//stylegate:ignore test cleanup: SessionLogger.Close flushes the JSONL the test already read
	sl.Close()
}

// sessionEventHasToolUse reports whether a JSONL event is an assistant record
// carrying a tool_use block for the named tool.
// sessionEventTypes lists the JSONL event types, for a failure message that
// names what the log actually holds.
func sessionEventTypes(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, ev := range readSessionJSONL(t, path) {
		s, _ := ev["type"].(string)
		out = append(out, s)
	}
	return out
}

func sessionEventHasToolUse(ev map[string]interface{}, tool string) bool {
	msg, ok := ev["message"].(map[string]interface{})
	if !ok {
		return false
	}
	blocks, ok := msg["content"].([]interface{})
	if !ok {
		return false
	}
	for _, b := range blocks {
		blk, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		if blk["type"] == "tool_use" && blk["name"] == tool {
			return true
		}
	}
	return false
}

// sessionEventText renders a JSONL event's message content as text (a
// tool_result's content is a LIST of blocks, so it is walked rather than
// type-asserted to a string).
func sessionEventText(ev map[string]interface{}) string {
	msg, ok := ev["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	switch c := msg["content"].(type) {
	case string:
		return c
	case []interface{}:
		var sb strings.Builder
		for _, b := range c {
			if blk, ok := b.(map[string]interface{}); ok {
				if s, ok := blk["content"].(string); ok {
					sb.WriteString(s)
				}
			}
		}
		return sb.String()
	}
	return ""
}
