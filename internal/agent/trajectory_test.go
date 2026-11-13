package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTrajectoryFixture writes a synthetic session log with the exact event
// order the harness produces: assistant events precede their round's request
// event, tool_results follow it.
func writeTrajectoryFixture(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func ts(sec int) string {
	return time.Date(2026, 8, 22, 0, 0, sec, 0, time.UTC).Format(time.RFC3339Nano)
}

// TestParseTrajectoryStats_RealisticSession mirrors the competition #4
// session shape: 2 tool rounds + 1 final round, waits and thinking values
// chosen so every stat is hand-checkable.
func TestParseTrajectoryStats_RealisticSession(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		// Round 0: 80 chars thinking + 2 tool_use.
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"%s"}]},"timestamp":%q}`, strings.Repeat("x", 80), ts(2)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c1","name":"code_search","input":{}}]},"timestamp":%q}`, ts(3)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c2","name":"read","input":{}}]},"timestamp":%q}`, ts(4)),
		fmt.Sprintf(`{"type":"request","iteration":0,"prompt_tokens":100,"cache_hit":50,"cache_miss":50,"completion_tokens":90,"timestamp":%q}`, ts(10)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"hits"}]},"timestamp":%q}`, ts(11)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c2","content":"text"}]},"timestamp":%q}`, ts(12)),
		// Round 1: no thinking, 1 tool_use (bash).
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"c3","name":"bash","input":{}}]},"timestamp":%q}`, ts(20)),
		fmt.Sprintf(`{"type":"request","iteration":1,"prompt_tokens":200,"cache_hit":180,"cache_miss":20,"completion_tokens":200,"timestamp":%q}`, ts(25)),
		fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"c3","content":"ok"}]},"timestamp":%q}`, ts(26)),
		// Round 2: 40 chars thinking, no tools (final answer).
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"%s"}]},"timestamp":%q}`, strings.Repeat("y", 40), ts(30)),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]},"timestamp":%q}`, ts(31)),
		fmt.Sprintf(`{"type":"request","iteration":2,"prompt_tokens":300,"cache_hit":260,"cache_miss":40,"completion_tokens":50,"timestamp":%q}`, ts(35)),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"timestamp":%q}`, ts(36)),
	})

	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}

	if stats.Rounds != 3 {
		t.Errorf("Rounds = %d, want 3", stats.Rounds)
	}
	if stats.ThinkingTotal != 120 { // 80 + 40
		t.Errorf("ThinkingTotal = %d, want 120", stats.ThinkingTotal)
	}
	if stats.ThinkingRounds != 2 {
		t.Errorf("ThinkingRounds = %d, want 2", stats.ThinkingRounds)
	}
	if stats.ThinkingMedian != 60 { // median of [40, 80]
		t.Errorf("ThinkingMedian = %d, want 60", stats.ThinkingMedian)
	}
	if stats.ThinkingMax != 80 {
		t.Errorf("ThinkingMax = %d, want 80", stats.ThinkingMax)
	}
	// Waits: user(1)→req0(10) = 9s, req0(10)→req1(25) = 15s, req1(25)→req2(35) = 10s.
	if stats.WaitMedian != 10 || stats.WaitMean != 34.0/3.0 {
		t.Errorf("waits: median=%v mean=%v, want 10 / 11.333", stats.WaitMedian, stats.WaitMean)
	}
	if stats.WaitMax != 15 {
		t.Errorf("WaitMax = %v, want 15", stats.WaitMax)
	}
	if stats.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3", stats.ToolCalls)
	}
	if stats.ToolBreakdown["code_search"] != 1 || stats.ToolBreakdown["read"] != 1 || stats.ToolBreakdown["bash"] != 1 {
		t.Errorf("ToolBreakdown = %v", stats.ToolBreakdown)
	}
	if stats.CompletionTotal != 340 { // 90 + 200 + 50
		t.Errorf("CompletionTotal = %d, want 340", stats.CompletionTotal)
	}
	if stats.CacheHitTotal != 490 || stats.CacheMissTotal != 110 { // 50+180+260, 50+20+40
		t.Errorf("cache = %d hit / %d miss, want 490 / 110", stats.CacheHitTotal, stats.CacheMissTotal)
	}
	if stats.ElapsedSec != 36 {
		t.Errorf("ElapsedSec = %v, want 36", stats.ElapsedSec)
	}

	// The formatted block contains the headline numbers.
	formatted := stats.Format()
	for _, want := range []string{"[trajectory stats]", "rounds=3", "thinking=120 chars", "median 60/round",
		"tool_calls=3:", "bash 1", "completion_tokens=340", "cache: 490 hit / 600 total (81.7%)"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("Format() missing %q:\n%s", want, formatted)
		}
	}
}

// TestParseTrajectoryStats_NoRounds covers a session that failed before the
// first request: the parser must not panic and must report zero rounds.
func TestParseTrajectoryStats_NoRounds(t *testing.T) {
	path := writeTrajectoryFixture(t, []string{
		fmt.Sprintf(`{"type":"session_start","timestamp":%q}`, ts(0)),
		fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"task"},"timestamp":%q}`, ts(1)),
		fmt.Sprintf(`{"type":"session_end","exit_code":1,"timestamp":%q}`, ts(5)),
	})
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	if stats.Rounds != 0 || stats.ThinkingTotal != 0 || stats.ToolCalls != 0 {
		t.Errorf("expected empty stats, got %+v", stats)
	}
}

// TestParseTrajectoryStats_MissingFile: unreadable log surfaces an error.
func TestParseTrajectoryStats_MissingFile(t *testing.T) {
	if _, err := ParseTrajectoryStats("/nonexistent/session.jsonl"); err == nil {
		t.Error("expected error for missing file")
	}
}
