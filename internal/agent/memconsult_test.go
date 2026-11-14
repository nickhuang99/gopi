package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// The review-time consult checkpoint's switch has exactly ONE spelling per
// face; the parity test below asserts the settings catalog publishes these
// very values, so a rename that misses one site fails there instead of
// silently unregistering the switch (Rule 1b/13).
const (
	memConsultEnv         = "GOPI_NO_ENFORCE_MEMORY_CONSULT"
	memConsultSettingsKey = "no_enforce_memory_consult"
	memConsultCLIFlag     = "no-enforce-memory-consult"
)

// trajectorySampleEnv names the session log the sample-file control parses
// (a single env read, so the control is reproducible from the report).
const trajectorySampleEnv = "GOPI_TRAJECTORY_SAMPLE"

// countUserMessages already exists in taskendstore_test.go (role-aware via
// Ctx.GetMessages) — reused here so the assertion helper has ONE definition.

// ---------------------------------------------------------------------------
// D1 — review-time memory-consult accountability checkpoint (candidate C)
// ---------------------------------------------------------------------------

// TestConsultCheckpoint_ArmedFiresOnceAtFirstCommit verifies the enforcement
// shape: the checkpoint is ARMED by the run's start-of-run path, fires exactly
// once (the first commit consumes it), and is never injectable again — so a
// long run cannot be spammed by it.
func TestConsultCheckpoint_ArmedFiresOnceAtFirstCommit(t *testing.T) {
	a := &Agent{Ctx: &Context{}}
	a.armConsultCheckpoint()

	if !a.maybeInjectConsultCheckpoint() {
		t.Fatal("armed checkpoint must fire on the first call (first commit)")
	}
	if !a.consultCheckpointPrompted {
		t.Fatal("consultCheckpointPrompted must latch after firing")
	}
	if got := countUserMessages(a, consultCheckpointMessage); got != 1 {
		t.Fatalf("expected exactly 1 injected consult message, got %d", got)
	}
	if a.maybeInjectConsultCheckpoint() {
		t.Fatal("checkpoint must be a one-shot: second call is a no-op")
	}
	if got := countUserMessages(a, consultCheckpointMessage); got != 1 {
		t.Fatalf("checkpoint must stay at exactly 1 injected message, got %d", got)
	}
}

// TestConsultCheckpoint_UnarmedInjectsNothing is the blast-radius guard: an
// Agent that never went through start-of-run (every hand-built literal in the
// existing test suite) must not inject this instrument, so unrelated tests
// keep their exact wire expectations.
func TestConsultCheckpoint_UnarmedInjectsNothing(t *testing.T) {
	a := &Agent{Ctx: &Context{}}

	if a.maybeInjectConsultCheckpoint() {
		t.Fatal("an unarmed agent (no start-of-run) must NOT inject the checkpoint")
	}
	if got := countUserMessages(a, consultCheckpointMessage); got != 0 {
		t.Fatalf("unarmed agent must inject nothing, got %d message(s)", got)
	}
	if a.consultCheckpointPrompted {
		t.Fatal("an unarmed agent must not latch the prompted flag")
	}
}

// TestConsultCheckpoint_DisabledByOwnSwitch verifies kill switch ①: the
// checkpoint's OWN switch turns the measurement instrument off while every
// OTHER harness memory checkpoint keeps working.
func TestConsultCheckpoint_DisabledByOwnSwitch(t *testing.T) {
	a := &Agent{Ctx: &Context{}, NoEnforceMemoryConsult: true}
	a.armConsultCheckpoint()

	if a.maybeInjectConsultCheckpoint() {
		t.Fatal("GOPI_NO_ENFORCE_MEMORY_CONSULT must disable the consult checkpoint")
	}
	if got := countUserMessages(a, consultCheckpointMessage); got != 0 {
		t.Fatalf("disabled checkpoint must inject nothing, got %d", got)
	}
	// Independence: the task-start search enforcement is untouched by it.
	if !a.maybeInjectTaskStartSearch() {
		t.Fatal("the consult switch must NOT disable the task-start search checkpoint")
	}
}

// TestConsultCheckpoint_DisabledByNoEnforceMemory verifies kill switch ②:
// NoEnforceMemory means "every harness memory checkpoint off", so an A/B
// baseline arm is not silently polluted by this third injection.
func TestConsultCheckpoint_DisabledByNoEnforceMemory(t *testing.T) {
	a := &Agent{Ctx: &Context{}, NoEnforceMemory: true}
	a.armConsultCheckpoint()

	if a.maybeInjectConsultCheckpoint() {
		t.Fatal("NoEnforceMemory (A/B baseline) must also disable the consult checkpoint")
	}
	if got := countUserMessages(a, consultCheckpointMessage); got != 0 {
		t.Fatalf("disabled checkpoint must inject nothing, got %d", got)
	}
}

// TestConsultCheckpointMessage_IsMeasurementNotWorkOrder guards the wording
// that keeps the epic's hard constraint 4 honest: the injected question must
// demand a ONE-LINE answer with a cost figure, must accept "none" as correct,
// and must forbid spawning work — otherwise the checkpoint becomes the noise
// generator the epic forbids.
func TestConsultCheckpointMessage_IsMeasurementNotWorkOrder(t *testing.T) {
	for _, want := range []string{
		consultCheckpointMarker,
		"which work could a memory search have saved",
		"ONE line",
		"what it cost",
		"MEASUREMENT, not a work order",
		"do NOT start new work",
	} {
		if !strings.Contains(consultCheckpointMessage, want) {
			t.Errorf("consultCheckpointMessage missing %q", want)
		}
	}
	// The marker must lead the message: the telemetry latches on it.
	if !strings.HasPrefix(consultCheckpointMessage, consultCheckpointMarker) {
		t.Error("consultCheckpointMarker must prefix the injected message")
	}
}

// TestConsultCheckpoint_InjectionIsLogged proves the auditability requirement:
// the injection reaches the session JSONL as a user event carrying the marker,
// which is exactly what ParseTrajectoryStats reads back into
// ConsultCheckpointInjected.
func TestConsultCheckpoint_InjectionIsLogged(t *testing.T) {
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close()

	a := &Agent{Ctx: &Context{}, SessionLog: sl}
	a.armConsultCheckpoint()
	if !a.maybeInjectConsultCheckpoint() {
		t.Fatal("armed checkpoint must fire")
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("session log sync: %v", err)
	}

	stats, err := ParseTrajectoryStats(sl.Path())
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if !stats.ConsultCheckpointInjected {
		t.Fatal("the logged injection must be visible to the telemetry (ConsultCheckpointInjected)")
	}
}

// ---------------------------------------------------------------------------
// D2 — memory-adoption telemetry ("查了不用" detector)
// ---------------------------------------------------------------------------

// memToolUse renders an assistant tool_use event for the memory tool with the
// given action/id, mirroring the JSONL the harness writes.
func memToolUse(id, action string, extra string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":%q,"input":{"action":%q%s}}]},"timestamp":%q}`,
		id, toolnames.ToolMemory, action, extra, ts(3))
}

// TestMemoryAdoption_MeasuresConsumedRows is the core "查了不用" measurement:
// a forced search returns three rows, a later feedback names ONE of them — the
// telemetry must report 3 returned / 1 consumed, i.e. it can tell a used hit
// from ignored ones.
func TestMemoryAdoption_MeasuresConsumedRows(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		memToolUse("c1", toolnames.ToolMemoryActionSearch, ""),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":42,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"#7 foo\n#9 bar\n#11 baz\n"}]},"timestamp":%q}`, ts(11)),
		// Round 1: the model votes on row #9 — evidence it actually used it.
		memToolUse("c2", toolnames.ToolMemoryActionFeedback, `,"id":9,"rating":"useful"`),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":10,"timestamp":%q}`, ts(20)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(30)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemoryRowsReturned != 3 {
		t.Errorf("MemoryRowsReturned = %d, want 3", stats.MemoryRowsReturned)
	}
	if stats.MemoryRowsConsumed != 1 {
		t.Errorf("MemoryRowsConsumed = %d, want 1 (feedback named #9)", stats.MemoryRowsConsumed)
	}
	if stats.MemorySearchRounds != 1 {
		t.Errorf("MemorySearchRounds = %d, want 1", stats.MemorySearchRounds)
	}
	if !stats.MemoryFirstToolRound {
		t.Error("the run's first tool_use WAS the search — MemoryFirstToolRound must be true")
	}
	if stats.FirstMemorySearchRound != 0 {
		t.Errorf("FirstMemorySearchRound = %d, want 0", stats.FirstMemorySearchRound)
	}
	// The search round's completion tokens are attributed to the search.
	if stats.MemorySearchCompletionTokens != 42 {
		t.Errorf("MemorySearchCompletionTokens = %d, want 42", stats.MemorySearchCompletionTokens)
	}
	out := stats.Format()
	if !strings.Contains(out, "rows_returned=3 rows_consumed=1") {
		t.Errorf("Format() must carry the adoption line, got:\n%s", out)
	}
}

// TestMemoryAdoption_CarelessSearchIsVisible is the negative half: when the
// model searches and IGNORES every hit (no feedback/update naming any row),
// the telemetry must show 0 consumed — that zero is the whole point of the
// instrument.
func TestMemoryAdoption_CarelessSearchIsVisible(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		memToolUse("c1", toolnames.ToolMemoryActionSearch, ""),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":7,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"#3 a\n#4 b\n"}]},"timestamp":%q}`, ts(11)),
		// Later work happens, but no memory feedback/update ever names a row.
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c2","name":"bash"}]},"timestamp":%q}`, ts(20)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":5,"timestamp":%q}`, ts(21)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(30)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemoryRowsReturned != 2 || stats.MemoryRowsConsumed != 0 {
		t.Fatalf("want 2 returned / 0 consumed, got %d/%d", stats.MemoryRowsReturned, stats.MemoryRowsConsumed)
	}
	if stats.MemoryFirstToolRound != true {
		t.Error("the search WAS the run's first tool_use — MemoryFirstToolRound must be true")
	}
	if stats.MemoryEmptySearches != 0 {
		t.Errorf("MemoryEmptySearches = %d, want 0 (hits were returned)", stats.MemoryEmptySearches)
	}
}

// TestMemoryAdoption_EmptySearchIsNotAConsumptionFailure pins the distinction
// the report depends on: a search that returns NOTHING cannot be judged as
// "ignored hits" — it is counted as an empty search instead, so a low
// consumption rate is never inferred from hits that never existed.
func TestMemoryAdoption_EmptySearchIsNotAConsumptionFailure(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		memToolUse("c1", toolnames.ToolMemoryActionSearch, ""),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":3,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"No distilled memory found for this query. Store it with memory(action=store)."}]},"timestamp":%q}`, ts(11)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(30)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemoryEmptySearches != 1 {
		t.Errorf("MemoryEmptySearches = %d, want 1", stats.MemoryEmptySearches)
	}
	if stats.MemoryRowsReturned != 0 || stats.MemoryRowsConsumed != 0 {
		t.Errorf("an empty search returns no rows: got %d/%d", stats.MemoryRowsReturned, stats.MemoryRowsConsumed)
	}
}

// TestMemoryAdoption_NoSearchLeavesSentinels verifies the "never searched"
// encoding: FirstToolRound / FirstMemorySearchRound read -1 so an absent
// observation is distinguishable from a round-0 observation, and MemoryFirstToolRound
// is false.
func TestMemoryAdoption_NoSearchLeavesSentinels(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"bash"}]},"timestamp":%q}`, ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":5,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(4)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.FirstToolRound != 0 {
		t.Errorf("FirstToolRound = %d, want 0 (bash was the first tool, round 0)", stats.FirstToolRound)
	}
	if stats.FirstMemorySearchRound != -1 {
		t.Errorf("FirstMemorySearchRound = %d, want -1 (no search)", stats.FirstMemorySearchRound)
	}
	if stats.MemoryFirstToolRound {
		t.Error("MemoryFirstToolRound must be false when the first tool was not a search")
	}
	if stats.MemorySearchRounds != 0 || stats.MemoryRowsReturned != 0 {
		t.Errorf("no search → zero counters, got rounds=%d rows=%d", stats.MemorySearchRounds, stats.MemoryRowsReturned)
	}
}

// TestParseHitRowIDs_IgnoresNonHitLines pins the precision of the row-id
// parser: only a line that STARTS with "#<digits>" is a hit. A '#' inside
// prose (a markdown heading, a shell comment, an issue number mid-sentence)
// must never be counted as a returned row.
func TestParseHitRowIDs_IgnoresNonHitLines(t *testing.T) {
	got := parseHitRowIDs("  #12 ok\n## heading\nsee #99 inline\n#7\nnot #5\n#\n")
	if len(got) != 2 || got[0] != 12 || got[1] != 7 {
		t.Fatalf("parseHitRowIDs = %v, want [12 7]", got)
	}
	if ids := parseHitRowIDs(""); len(ids) != 0 {
		t.Fatalf("empty text must yield no rows, got %v", ids)
	}
}

// TestMemoryAdoption_SameRowTwiceCountsOnce guards against inflating the
// consumption rate (and the returned count) when two searches hand back the
// SAME row: each distinct row counts once.
func TestMemoryAdoption_SameRowTwiceCountsOnce(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		memToolUse("c1", toolnames.ToolMemoryActionSearch, ""),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":4,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"#5 hit\n"}]},"timestamp":%q}`, ts(11)),
		memToolUse("c2", toolnames.ToolMemoryActionSearch, ""),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":4,"timestamp":%q}`, ts(20)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c2","content":"#5 hit again\n"}]},"timestamp":%q}`, ts(21)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c3","name":%q,"input":{"action":%q,"id":5,"rating":"useful"}}]},"timestamp":%q}`,
			toolnames.ToolMemory, toolnames.ToolMemoryActionFeedback, ts(22)),
		fmt.Sprintf(`{"type":"request","iteration":2,"completion_tokens":4,"timestamp":%q}`, ts(23)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(30)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemoryRowsReturned != 2 {
		t.Errorf("two searches each returned row #5 → 2 returned rows, got %d", stats.MemoryRowsReturned)
	}
	if stats.MemoryRowsConsumed != 2 {
		t.Errorf("row #5 was consumed → both occurrences count, got %d", stats.MemoryRowsConsumed)
	}
	if stats.MemorySearchRounds != 2 {
		t.Errorf("MemorySearchRounds = %d, want 2 (distinct rounds)", stats.MemorySearchRounds)
	}
}

// TestMemoryAdoption_NonMemoryHashIsIgnored is the false-positive guard that
// matters most in a live log: a '#' hit-line drawn from a BASH or READ result
// must not be attributed to a memory search.
func TestMemoryAdoption_NonMemoryHashIsIgnored(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c9","name":"bash"}]},"timestamp":%q}`, ts(2)),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":5,"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c9","content":"#1 file one\n#2 file two\n"}]},"timestamp":%q}`, ts(4)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(5)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemoryRowsReturned != 0 {
		t.Fatalf("a bash result's '#' lines are not memory rows, got %d", stats.MemoryRowsReturned)
	}
}

// ---------------------------------------------------------------------------
// Config wiring (the switch must be reachable from env + settings catalog)
// ---------------------------------------------------------------------------

// TestMemoryAdoption_TextMentionIsNotConsumption pins the LOWER-BOUND honesty
// of the detector: an assistant merely TALKING about a row id (in prose) is
// not evidence it used it. Only a memory feedback/update addressing that id
// counts — so the consumption rate can never be inflated by narration.
func TestMemoryAdoption_TextMentionIsNotConsumption(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		memToolUse("c1", toolnames.ToolMemoryActionSearch, ""),
		fmt.Sprintf(`{"type":"request","iteration":0,"completion_tokens":4,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"#3 hit\n"}]},"timestamp":%q}`, ts(11)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"memory #3 confirms the approach, proceeding"}]},"timestamp":%q}`, ts(12)),
		fmt.Sprintf(`{"type":"request","iteration":1,"completion_tokens":4,"timestamp":%q}`, ts(13)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(20)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.MemoryRowsReturned != 1 || stats.MemoryRowsConsumed != 0 {
		t.Fatalf("prose mention must NOT count as consumption: got %d returned / %d consumed",
			stats.MemoryRowsReturned, stats.MemoryRowsConsumed)
	}
}

// TestTrajectoryStats_SampleFile is the POSITIVE CONTROL harness: it parses a
// real session log named by GOPI_TRAJECTORY_SAMPLE and prints the measured
// stats, so a claim about adoption on a LIVE session can be reproduced with
// one command instead of a bespoke parser:
//
//	GOPI_TRAJECTORY_SAMPLE=~/.gopi/sessions/<dir>/<id>.jsonl \
//	  go test ./internal/agent/ -run TestTrajectoryStats_SampleFile -v
//
// Skipped (not failed) when the env var is unset — it is an operator/audit
// tool, never part of the default suite.
func TestTrajectoryStats_SampleFile(t *testing.T) {
	path := os.Getenv(trajectorySampleEnv)
	if path == "" {
		t.Skipf("%s not set — sample-file control skipped", trajectorySampleEnv)
	}
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats(%s): %v", path, err)
	}
	t.Logf("sample %s\n%s", path, stats.Format())
	if stats.Rounds == 0 {
		t.Fatal("a real session log must have at least one round — parser or fixture wrong")
	}
}

// TestMemConsultConfigParity_EnvRegistered guards the new kill switch's
// wiring so it can never become an undiscoverable no-op knob (Rule 15): the
// settings catalog must publish the SAME env name the Agent reads, the
// settings.json key must round-trip into Config, and the CLI flag spelling
// must be registered.
func TestMemConsultConfigParity_EnvRegistered(t *testing.T) {
	idx := -1
	for i := range config.EnvVars {
		if config.EnvVars[i].Env == memConsultEnv {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("settings catalog is missing %s — the switch would be undiscoverable from env/settings/help", memConsultEnv)
	}
	if got := config.EnvVars[idx].Key; got != memConsultSettingsKey {
		t.Errorf("catalog key = %q, want %s", got, memConsultSettingsKey)
	}
	if got := config.EnvVars[idx].CLIFlag; got != memConsultCLIFlag {
		t.Errorf("catalog CLI flag = %q, want %s", got, memConsultCLIFlag)
	}

	// settings.json face: the catalog key must decode into the field the
	// Agent's switch is built from.
	var cfg config.Config
	if err := json.Unmarshal([]byte(`{"`+memConsultSettingsKey+`":true}`), &cfg); err != nil {
		t.Fatalf("decode settings key %q: %v", memConsultSettingsKey, err)
	}
	if !cfg.NoEnforceMemoryConsult {
		t.Fatalf("settings.json %q:true must resolve Config.NoEnforceMemoryConsult=true", memConsultSettingsKey)
	}
}
