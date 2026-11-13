package agent

import (
	"os"
	"strings"
	"testing"
)

// TestParseTrajectoryStats_LiveSession is a regression check against the real
// competition #4 session (report numbers: 19 requests, 18,926 thinking chars,
// median ~430 / max 5,709, 19 tool calls: bash 16 / read 2 / write 1,
// completion 10,241, cache 94.1%). Skipped when the session is absent.
func TestParseTrajectoryStats_LiveSession(t *testing.T) {
	path := os.Getenv("GOPI_LIVE_SESSION")
	if path == "" {
		t.Skip("GOPI_LIVE_SESSION not set")
	}
	stats, err := ParseTrajectoryStats(path)
	if err != nil {
		t.Fatalf("ParseTrajectoryStats: %v", err)
	}
	t.Logf("\n%s", stats.Format())

	// Report cross-checks (report-triple-comparison.md §一).
	if stats.Rounds != 19 {
		t.Errorf("Rounds = %d, want 19", stats.Rounds)
	}
	if stats.ThinkingTotal != 18926 {
		t.Errorf("ThinkingTotal = %d, want 18926", stats.ThinkingTotal)
	}
	if stats.ThinkingMax != 5709 {
		t.Errorf("ThinkingMax = %d, want 5709", stats.ThinkingMax)
	}
	if stats.ToolCalls != 19 || stats.ToolBreakdown["bash"] != 16 || stats.ToolBreakdown["read"] != 2 || stats.ToolBreakdown["write"] != 1 {
		t.Errorf("tool calls = %v total=%d, want bash 16 / read 2 / write 1 = 19", stats.ToolBreakdown, stats.ToolCalls)
	}
	if stats.CompletionTotal != 10241 {
		t.Errorf("CompletionTotal = %d, want 10241", stats.CompletionTotal)
	}
	if got := stats.Format(); !strings.Contains(got, "94.1%") {
		t.Errorf("cache rate not 94.1%% in output:\n%s", got)
	}
}
