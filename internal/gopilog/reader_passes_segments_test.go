package gopilog

// A RESUMED SESSION'S PASS COUNT SURVIVES ITS EMPTY LAST SEGMENT.
//
// The defect (task 01a17ae2, measured on session
// 4a0957a3-bb4e-4337-a761-20606c87d1fd): the reader assigned
// `s.TotalIterations = ev.TotalIterations` on every `session_end`, so the LAST
// end in the log won — and a resume that ran no turn writes its own
// `session_end total_iterations: 0`. That log holds
//
//	line 868  {"type":"session_end", "total_iterations":29, …}
//	line 869  {"type":"session_start", …}          ← the resume opens a segment
//	line 871  {"type":"session_end", "total_iterations":0, …}   ← the resumed run, no turn
//
// so every surface printed PASSES = 0 for a session that had run 29 loop
// passes. Each run's end reports only ITS OWN passes (the REPL's counter is per
// process — cmd/agent/cli_chat.go, task 01a16fb3), therefore the session-level
// figure must SUM the segments, exactly as SessionTurns counts turns over the
// whole log rather than over the last segment.
//
// The fixture below is the smallest sample of that shape: two segments, the
// second empty. The RED assertion is the one this file exists for — with the
// pre-fix assignment the count is 0, with the fix it is the first segment's 29.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
)

// resumedSessionFixturePasses is the pass count the FIRST segment recorded; it
// is the number the second (empty) segment used to erase.
const resumedSessionFixturePasses = 29

// resumedSessionWithEmptyResume is a log of two run segments: the original run
// (two `request` rows, one operator turn, session_end 29 passes) and a resume
// that opened a second segment and ended without running a turn
// (`session_end total_iterations: 0`). Shape taken from the measured log, cut
// to its smallest reproducible form.
func resumedSessionWithEmptyResume() []string {
	return []string{
		`{"model":"deepseek-flash","sessionId":"resumed-fixture","timestamp":"2026-10-27T05:00:00.000+08:00","type":"session_start","workdir":"/home/nick/work/demo"}`,
		`{"message":{"content":"[EPIC GOAL]\nread go.mod","role":"user"},"timestamp":"2026-10-27T05:00:01.000+08:00","type":"user"}`,
		`{"message":{"content":[{"text":"on it","type":"text"}],"role":"assistant"},"timestamp":"2026-10-27T05:00:02.000+08:00","type":"assistant"}`,
		`{"cache_hit":1,"cache_miss":2,"completion_tokens":3,"iteration":0,"prompt_tokens":10,"timestamp":"2026-10-27T05:00:02.500+08:00","type":"request"}`,
		`{"cache_hit":1,"cache_miss":2,"completion_tokens":3,"iteration":1,"prompt_tokens":11,"timestamp":"2026-10-27T05:00:03.500+08:00","type":"request"}`,
		`{"elapsed_sec":3.5,"exit_code":0,"total_iterations":29,"total_tokens":1234,"timestamp":"2026-10-27T05:00:04.000+08:00","type":"session_end"}`,
		`{"model":"deepseek-flash","sessionId":"resumed-fixture","timestamp":"2026-10-27T05:10:00.000+08:00","type":"session_start","workdir":"/home/nick/work/demo"}`,
		`{"elapsed_sec":0.002867386,"exit_code":0,"total_iterations":0,"total_tokens":0,"timestamp":"2026-10-27T05:10:01.000+08:00","type":"session_end"}`,
	}
}

// TestParseResumedSessionKeepsTheFirstSegmentsPasses is THE regression guard: the
// empty last segment must not erase the recorded pass count, and the render of
// `session log --turns`' PASSES row must print the surviving number, not 0.
func TestParseResumedSessionKeepsTheFirstSegmentsPasses(t *testing.T) {
	s, err := ParseFile(writeFixture(t, resumedSessionWithEmptyResume()))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if s.TotalIterations == 0 {
		t.Fatalf("PASSES = 0 for a log whose first segment recorded %d loop passes — the empty last segment ERASED it (last-writer-wins in the reader)",
			resumedSessionFixturePasses)
	}
	if s.TotalIterations != resumedSessionFixturePasses {
		t.Errorf("PASSES = %d, want %d (the sum of the log's session_end segments)",
			s.TotalIterations, resumedSessionFixturePasses)
	}
	if s.PassSegments != 2 {
		t.Errorf("PassSegments = %d, want 2 (the log holds two run segments, one per session_end)", s.PassSegments)
	}
	if s.EndUnrecorded {
		t.Error("EndUnrecorded = true for a log that recorded two ends")
	}
	// The SAME defect shape lived on the neighbouring fields of one statement:
	// assigning elapsed/tokens made the empty segment report a 3.5s session as
	// `Elapsed: 0.0s` (measured on the real log: 43 minutes rendered as 0.0s).
	if s.ElapsedSec < 3.5 {
		t.Errorf("ElapsedSec = %v, want >= 3.5 (the sum of the log's segments, not the empty last one's 0.002s)", s.ElapsedSec)
	}
	if s.TotalTokens != 1234 {
		t.Errorf("TotalTokens = %d, want 1234 (the sum: the empty segment recorded 0)", s.TotalTokens)
	}

	// The PASSES row of the `--turns` table is the surface the report is about.
	turnTable := FormatSessionTurnTable(s)
	if want := SessionPassRowWord() + " = " + itoa(resumedSessionFixturePasses); !strings.Contains(turnTable, want) {
		t.Errorf("the `--turns` banner does not print %q:\n%s", want, turnTable)
	}
	if strings.Contains(turnTable, SessionPassRowWord()+" = 0 ") {
		t.Errorf("the `--turns` banner prints PASSES = 0 — the empty resume segment won:\n%s", turnTable)
	}
	// …and it SAYS the number spans both segments, because the log's own tail
	// reports 0 passes for the run that just ended (Rule 15: a count states its
	// span instead of leaving the reader to guess which run it covers).
	if !strings.Contains(turnTable, sessionevent.CounterSourceSessionEndAccumulated) {
		t.Errorf("the PASSES row does not declare that the count is SUMMED over the log's run segments (%q):\n%s",
			sessionevent.CounterSourceSessionEndAccumulated, turnTable)
	}
}

// TestLoadAllSummariesKeepsTheFirstSegmentsPasses covers the OTHER reader path
// (`gopi session log --last`, FormatSummaryTable): it walked the file with its
// own loop and made the same assignment, so the summary row showed the same 0.
func TestLoadAllSummariesKeepsTheFirstSegmentsPasses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	dir := filepath.Join(home, ".gopi", sessionsDir, "resumed-fixture")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fixture := filepath.Join(dir, "session.jsonl")
	body := strings.Join(resumedSessionWithEmptyResume(), "\n") + "\n"
	if err := os.WriteFile(fixture, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summaries, err := LoadAllSummaries(0)
	if err != nil {
		t.Fatalf("LoadAllSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("LoadAllSummaries = %d summaries, want 1: %+v", len(summaries), summaries)
	}
	got := summaries[0]
	if got.TotalIterations != resumedSessionFixturePasses {
		t.Fatalf("summary PASSES = %d, want %d — the resumed session's empty last segment won the assignment again",
			got.TotalIterations, resumedSessionFixturePasses)
	}
	if got.EndUnrecorded {
		t.Error("summary EndUnrecorded = true for a log that recorded two ends")
	}
	if row := FormatSummaryTable(summaries); strings.Contains(row, "                   0 ") {
		t.Errorf("the summary row prints PASSES = 0:\n%s", row)
	}
}

// TestScanSessionUsageKeepsTheFirstSegmentsPasses pins the THIRD reader with the
// same shape (`gopi session stats`): ScanSessionUsage assigned the field too, so
// the stats surface reported 0 passes for the same log.
func TestScanSessionUsageKeepsTheFirstSegmentsPasses(t *testing.T) {
	u, err := ScanSessionUsage(writeFixture(t, resumedSessionWithEmptyResume()))
	if err != nil {
		t.Fatalf("ScanSessionUsage: %v", err)
	}
	if !u.HasIterations {
		t.Fatal("HasIterations = false for a log that recorded two ends")
	}
	if u.TotalIterations != resumedSessionFixturePasses {
		t.Errorf("usage passes = %d, want %d (the sum over the log's segments)", u.TotalIterations, resumedSessionFixturePasses)
	}
	if u.PassSegments != 2 {
		t.Errorf("usage PassSegments = %d, want 2", u.PassSegments)
	}
}

// TestSingleSegmentSessionPassesAreUnchanged keeps the fix honest in the other
// direction: a log with ONE `session_end` must report exactly what that end
// recorded — a sum of one segment is the segment, and no surface may start
// printing a "summed" caption for a session that was never resumed.
func TestSingleSegmentSessionPassesAreUnchanged(t *testing.T) {
	s, err := ParseFile(writeFixture(t, anthropicSession))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.TotalIterations != 2 {
		t.Errorf("TotalIterations = %d, want 2 (the log's only session_end)", s.TotalIterations)
	}
	if s.PassSegments != 1 {
		t.Errorf("PassSegments = %d, want 1", s.PassSegments)
	}
	if src := iterationCountSource(s); src != sessionevent.CounterSourceSessionEnd {
		t.Errorf("iterationCountSource = %q, want the plain session_end word %q for a single-segment log",
			src, sessionevent.CounterSourceSessionEnd)
	}
}

// TestPassSumIsNotTheRoundCount keeps the two quantities apart on the resumed
// fixture: summing the segments must not turn the pass count into the `request`
// row count (the defect task 01a17492 removed — one number under two words).
func TestPassSumIsNotTheRoundCount(t *testing.T) {
	s, err := ParseFile(writeFixture(t, resumedSessionWithEmptyResume()))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.TotalIterations == s.RequestEvents {
		t.Fatalf("passes (%d) == request events (%d) on a fixture built so they differ — the pass count borrowed the round count",
			s.TotalIterations, s.RequestEvents)
	}
	if s.RequestEvents != 2 {
		t.Errorf("request events = %d, want 2 (the fixture's two `request` rows)", s.RequestEvents)
	}
}
