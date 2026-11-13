package agent

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// All no-list-yet nudge tests drive maybeNudgeNoList directly (it is
// self-contained: no running loop needed). A helper arms a minimal Agent with
// TaskState + Ctx so every guard is exercised.
func newNudgeAgent(nudgeRounds, nudgeToolRounds int) *Agent {
	a := &Agent{
		TaskState:       newTodoState(),
		Ctx:             &Context{},
		NudgeRounds:     nudgeRounds,
		NudgeToolRounds: nudgeToolRounds,
	}
	a.nudgeNextAt = nudgeRounds // arm on first round (mirrors Run loop init)
	return a
}

func TestMaybeNudgeNoList_TriggersAfterThreshold(t *testing.T) {
	// N=5 rounds, 3 tool rounds, no list → fires exactly once.
	a := newNudgeAgent(5, 3)
	for r := 1; r <= 5; r++ {
		fired := a.maybeNudgeNoList(r, 3, 3)
		if r == 5 && !fired {
			t.Fatalf("nudge should fire at round 5, did not")
		}
		if r < 5 && fired {
			t.Fatalf("nudge fired early at round %d, want only at 5", r)
		}
	}
	if a.nudgeCount != 1 {
		t.Errorf("nudgeCount = %d, want 1", a.nudgeCount)
	}
	if len(a.Ctx.Messages) != 1 {
		t.Errorf("Messages = %d, want 1 (one nudge injected)", len(a.Ctx.Messages))
	}
}

func TestMaybeNudgeNoList_NoTriggerBelowToolRounds(t *testing.T) {
	// Same round count but only 2 tool rounds (below min 3) → never fires.
	a := newNudgeAgent(5, 3)
	for r := 1; r <= 10; r++ {
		if a.maybeNudgeNoList(r, 2, 3) {
			t.Fatalf("nudge fired with only 2 tool rounds at round %d, want none", r)
		}
	}
	if a.nudgeCount != 0 {
		t.Errorf("nudgeCount = %d, want 0", a.nudgeCount)
	}
}

func TestMaybeNudgeNoList_NoTriggerBelowRounds(t *testing.T) {
	// nudgeNextAt=5 armed, but we only reach round 4 → never fires
	// (regardless of tool rounds).
	a := newNudgeAgent(5, 3)
	a.maybeNudgeNoList(4, 4, 3)
	if a.nudgeCount != 0 {
		t.Errorf("nudgeCount = %d, want 0 (round threshold 5 not reached)", a.nudgeCount)
	}
}

func TestMaybeNudgeNoList_NoTriggerWithoutListToolRounds(t *testing.T) {
	// Round floor 5 reached with 3 tool rounds, but nudgeNextAt wasn't reached
	// because roundIdx still below it — covered by NoTriggerBelowRounds. Here
	// instead verify: tool round threshold met but roundIdx < nudgeNextAt.
	a := newNudgeAgent(8, 3)
	fired := a.maybeNudgeNoList(5, 3, 3) // need 8 rounds but only 5 passed
	if fired {
		t.Fatalf("nudge fired at round 5 when nudgeNextAt=8")
	}
}

func TestMaybeNudgeNoList_StopsAfterListEstablished(t *testing.T) {
	// Once hasList==true, never nudges even past thresholds.
	a := newNudgeAgent(1, 1)
	a.TaskState.hasList = true
	for r := 1; r <= 20; r++ {
		if a.maybeNudgeNoList(r, 5, 1) {
			t.Fatalf("nudge fired after list established (round %d)", r)
		}
	}
	if a.nudgeCount != 0 {
		t.Errorf("nudgeCount = %d, want 0 (list established)", a.nudgeCount)
	}
}

func TestMaybeNudgeNoList_CappedAtMax(t *testing.T) {
	// Anti-harassment: even with endless rounds and tool rounds and no list,
	// nudge fires at most maxNudgeCount (2) times.
	a := newNudgeAgent(1, 1)
	fired := 0
	for r := 1; r <= 100; r++ {
		if a.maybeNudgeNoList(r, 1, 1) {
			fired++
		}
	}
	if fired != maxNudgeCount {
		t.Errorf("fired = %d, want maxNudgeCount=%d", fired, maxNudgeCount)
	}
	if a.nudgeCount != maxNudgeCount {
		t.Errorf("nudgeCount = %d, want %d", a.nudgeCount, maxNudgeCount)
	}
}

func TestMaybeNudgeNoList_NilTaskStateNeverFires(t *testing.T) {
	// No todoState (tests/legacy Agent{} literals) → no nudge, no panic.
	a := &Agent{Ctx: &Context{}}
	if a.maybeNudgeNoList(10, 10, 3) {
		t.Fatal("nudge fired with nil TaskState")
	}
}

func TestMaybeNudgeNoList_NilCtxNeverFires(t *testing.T) {
	a := newNudgeAgent(1, 1)
	a.Ctx = nil
	if a.maybeNudgeNoList(5, 5, 1) {
		t.Fatal("nudge fired with nil Ctx")
	}
	if a.nudgeCount != 0 {
		t.Errorf("nudgeCount = %d, want 0 (nil Ctx)", a.nudgeCount)
	}
}

// --- bookkeeping-decay nag (review 2026-09-07 6.5 / C2) -------------------
//
// The mirror of the no-list nudge: a list EXISTS but no [TASK_LIST] block has
// been re-emitted for maxRoundsWithoutTaskList rounds. Same driver style —
// maybeNudgeStaleTaskList is self-contained, no running loop needed.

// TestMaybeNudgeStaleTaskList_FiresAtCadence guards the primary contract: with
// a tracked list holding unfinished work, the nag fires exactly when the
// stale-round count reaches maxRoundsWithoutTaskList — the same constant the
// prompt interpolates, so prompt cadence and nag cadence cannot drift.
func TestMaybeNudgeStaleTaskList_FiresAtCadence(t *testing.T) {
	a := &Agent{TaskState: newTodoState(), Ctx: &Context{}}
	a.TaskState.update([]TaskItem{
		{TaskID: "1", Summary: "fix bug", Status: TaskStatusProgressing},
	})
	for r := 1; r < maxRoundsWithoutTaskList; r++ {
		if a.maybeNudgeStaleTaskList(r) {
			t.Fatalf("nag fired at %d stale rounds, want only at %d", r, maxRoundsWithoutTaskList)
		}
	}
	if !a.maybeNudgeStaleTaskList(maxRoundsWithoutTaskList) {
		t.Fatalf("nag did not fire at %d stale rounds", maxRoundsWithoutTaskList)
	}
	if a.staleListNudgeCount != 1 {
		t.Errorf("staleListNudgeCount = %d, want 1", a.staleListNudgeCount)
	}
	if len(a.Ctx.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1", len(a.Ctx.Messages))
	}
	// The injected text names the round count and both outs.
	want := "[system] No TASK_LIST block in 3 rounds. Re-emit it, or mark tasks completed."
	if got := a.Ctx.Messages[0].Content; got != want {
		t.Errorf("nag text = %q, want %q", got, want)
	}
}

// TestMaybeNudgeStaleTaskList_SilentWhenAllCompleted guards the P1.5
// omit-as-receipt case: a model whose tracked list is all completed has
// legitimately stopped emitting, and must not be nagged.
func TestMaybeNudgeStaleTaskList_SilentWhenAllCompleted(t *testing.T) {
	a := &Agent{TaskState: newTodoState(), Ctx: &Context{}}
	a.TaskState.update([]TaskItem{
		{TaskID: "1", Summary: "fix bug", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "write test", Status: TaskStatusCompleted},
	})
	for r := 1; r <= 20; r++ {
		if a.maybeNudgeStaleTaskList(r) {
			t.Fatalf("nag fired on an all-completed list at %d stale rounds", r)
		}
	}
}

// TestMaybeNudgeStaleTaskList_PendingBlocksExitToo guards the three-state
// interaction: a list all-pending is NOT completed, so the nag still fires —
// a plan-only list is exactly the state that decays silently.
func TestMaybeNudgeStaleTaskList_PendingBlocksExitToo(t *testing.T) {
	a := &Agent{TaskState: newTodoState(), Ctx: &Context{}}
	a.TaskState.update([]TaskItem{
		{TaskID: "1", Summary: "fix bug", Status: TaskStatusPending},
		{TaskID: "2", Summary: "write test", Status: TaskStatusPending},
	})
	if !a.maybeNudgeStaleTaskList(maxRoundsWithoutTaskList) {
		t.Fatal("nag did not fire on an all-pending (never started) list")
	}
}

// TestMaybeNudgeStaleTaskList_NeverFiresWithoutList guards the complement: no
// established list at all is the no-list nudge's job, not this one.
func TestMaybeNudgeStaleTaskList_NeverFiresWithoutList(t *testing.T) {
	a := &Agent{TaskState: newTodoState(), Ctx: &Context{}}
	for r := 1; r <= 20; r++ {
		if a.maybeNudgeStaleTaskList(r) {
			t.Fatalf("nag fired with no list established (round %d)", r)
		}
	}
}

// TestMaybeNudgeStaleTaskList_CappedAtMax guards the anti-harassment cap.
func TestMaybeNudgeStaleTaskList_CappedAtMax(t *testing.T) {
	a := &Agent{TaskState: newTodoState(), Ctx: &Context{}}
	a.TaskState.update([]TaskItem{{TaskID: "1", Status: TaskStatusProgressing}})
	fired := 0
	for r := 1; r <= 200; r++ {
		if a.maybeNudgeStaleTaskList(r) {
			fired++
		}
	}
	if fired != maxNudgeCount {
		t.Errorf("fired = %d, want maxNudgeCount=%d", fired, maxNudgeCount)
	}
}

// TestMaybeNudgeStaleTaskList_NilStateAndCtxNeverPanics guards the nil paths
// tests and legacy Agent{} literals take.
func TestMaybeNudgeStaleTaskList_NilStateAndCtxNeverPanics(t *testing.T) {
	noState := &Agent{Ctx: &Context{}}
	if noState.maybeNudgeStaleTaskList(10) {
		t.Fatal("nag fired with nil TaskState")
	}
	noCtx := &Agent{TaskState: newTodoState()}
	noCtx.TaskState.update([]TaskItem{{TaskID: "1", Status: TaskStatusProgressing}})
	if noCtx.maybeNudgeStaleTaskList(10) {
		t.Fatal("nag fired with nil Ctx")
	}
}

// TestAgent_Run_NudgesStaleTaskList is the end-to-end contract for the 6.5
// bookkeeping-decay nag: the model establishes a list on round 1, then stops
// re-emitting it while still calling tools. After maxRoundsWithoutTaskList
// block-less rounds the harness injects the reminder into the NEXT request —
// the exit-time warning's signal, delivered early enough to matter.
func TestAgent_Run_NudgesStaleTaskList(t *testing.T) {
	dir := t.TempDir()
	files := make([]string, 3)
	for k := range files {
		files[k] = filepath.Join(dir, fmt.Sprintf("note-%d.txt", k))
		if err := os.WriteFile(files[k], []byte(fmt.Sprintf("work log %d\n", k)), 0o644); err != nil {
			t.Fatalf("temp file: %v", err)
		}
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case callCount == 1:
			// Establish the list, then keep working.
			claudeTextPlusToolStream(w, "Planning.\n"+
				"[TASK_LIST_BEGIN]\n"+
				`[{"task_id":"1","summary":"Fix bug","status":"progressing"}]`+"\n"+
				"[TASK_LIST_END]", "read", map[string]interface{}{"path": files[0]})
		case callCount == 5:
			// The nag landed: the model re-emits its list and keeps working.
			claudeTextPlusToolStream(w, "Updating.\n"+
				"[TASK_LIST_BEGIN]\n"+
				`[{"task_id":"1","summary":"Fix bug","status":"progressing"}]`+"\n"+
				"[TASK_LIST_END]", "read", map[string]interface{}{"path": files[0]})
		case callCount <= 7:
			// Back on cadence, then two more block-less tool rounds.
			claudeToolUseStream(w, "read", map[string]interface{}{"path": files[callCount%len(files)]})
		default:
			claudeTextStream(w, "Confirmed.", "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 10,
		Logger:        log.New(&strings.Builder{}, "", 0),
		// A literal Agent gets no TaskState from NewAgent; seed the tracked
		// state the harness would have built from round 1's block.
		TaskState: newTodoState(),
	}
	if _, err := ag.Run("Fix the bug"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	// Rounds 2-4 are block-less tool rounds, so the nag is armed for round 5.
	// The tracked list still has a progressing task, so the omit-as-receipt
	// exemption does not apply. After the reminder the model re-emits, and the
	// remaining rounds stay under the cadence — proving the nag both fired and
	// did its job, without re-firing through the exit confirmation rounds.
	if ag.staleListNudgeCount != 1 {
		t.Fatalf("stale-list nag fired %d times, want exactly 1", ag.staleListNudgeCount)
	}
	want := "[system] No TASK_LIST block in 3 rounds. Re-emit it, or mark tasks completed."
	if n := countUserMessages(ag, want); n != 1 {
		t.Fatalf("expected the stale-list nag injected exactly once, got %d", n)
	}
}

// TestAgent_Run_NoStaleNagWhenListKeptCurrent is the negative control: the
// model re-emits its list every round, so the nag must stay silent.
func TestAgent_Run_NoStaleNagWhenListKeptCurrent(t *testing.T) {
	dir := t.TempDir()
	files := make([]string, 3)
	for k := range files {
		files[k] = filepath.Join(dir, fmt.Sprintf("note-%d.txt", k))
		if err := os.WriteFile(files[k], []byte(fmt.Sprintf("work log %d\n", k)), 0o644); err != nil {
			t.Fatalf("temp file: %v", err)
		}
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		if callCount <= 3 {
			claudeTextPlusToolStream(w, "Working.\n"+
				"[TASK_LIST_BEGIN]\n"+
				`[{"task_id":"1","summary":"Fix bug","status":"progressing"}]`+"\n"+
				"[TASK_LIST_END]", "read", map[string]interface{}{"path": files[callCount-1]})
			return
		}
		claudeTextStream(w, "Confirmed.", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		Logger:        log.New(&strings.Builder{}, "", 0),
		TaskState:     newTodoState(),
	}
	if _, err := ag.Run("Fix the bug"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if ag.staleListNudgeCount != 0 {
		t.Fatalf("stale-list nag fired %d times with a current list, want 0", ag.staleListNudgeCount)
	}
}
