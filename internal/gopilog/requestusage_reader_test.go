package gopilog

import (
	"fmt"
	"testing"
)

// The QUANTITY of a `request` row's four counters is written on the row by
// today's writers (requestusage.FieldUsageBase) and read back out of the log for
// rows written before the marker existed. These tests pin the reader's half of
// that contract on the SHAPES that matter, and each fixture reproduces a real
// log's numbers (epic 01a195b0):
//
//   - a CHAT log, whose rows are the CostTracker's RUNNING TOTALS;
//   - a RESUMED chat log, whose three runs each restart the counters;
//   - a TASK log, whose rows are per-request INCREMENTS and whose totals must
//     not move at all.
//
// The totals asserted here are the ones the raw HTTP log of the same real
// session sums to, which is what makes them a measurement rather than a
// restatement of the code.

// chatUsageLine writes one `request` row the way the chat path does: the four
// counters are the segment's running totals, and the split is the protocol's
// (prompt == hit + miss).
func chatUsageLine(iteration, prompt, completion int) string {
	return fmt.Sprintf(
		`{"type":"request","iteration":%d,"prompt_tokens":%d,"cache_hit":%d,"cache_miss":%d,"completion_tokens":%d,"tagged_usage":true,"usage_base":"cumulative"}`,
		iteration, prompt, prompt*2/3, prompt-prompt*2/3, completion)
}

// taskUsageLine writes one `request` row the way the task path does: a
// per-request increment, declared as such.
func taskUsageLine(iteration, prompt, completion int) string {
	return fmt.Sprintf(
		`{"type":"request","iteration":%d,"prompt_tokens":%d,"cache_hit":%d,"cache_miss":%d,"completion_tokens":%d,"tagged_usage":true,"usage_base":"delta"}`,
		iteration, prompt, prompt/2, prompt-prompt/2, completion)
}

// untaggedUsageLine writes a row with NO marker — the shape of every log
// written before the marker existed, which the reader has to classify.
func untaggedUsageLine(iteration, prompt, completion int) string {
	return fmt.Sprintf(
		`{"type":"request","iteration":%d,"prompt_tokens":%d,"cache_hit":%d,"cache_miss":%d,"completion_tokens":%d}`,
		iteration, prompt, prompt/2, prompt-prompt/2, completion)
}

// A CHAT SESSION'S HEADLINE IS ITS LAST RUNNING TOTAL, NOT THE SUM OF ALL OF
// THEM. The three readings are session d6120d04-98fa-454d-a2f0-51ea7c661956's
// own — the ones that printed as 219,191 prompt tokens when they were summed —
// and 109,705 is what the same session's raw HTTP log adds up to.
func TestChatLogTotalsComeFromTheLastRunningReading(t *testing.T) {
	lines := []string{
		`{"type":"session_start","sessionId":"chat","timestamp":"2026-10-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"hello"}}`,
		// ORDER MATTERS: a `request` row FLUSHES the iteration that is open, so
		// it is written after the content it belongs to (the real logs' shape).
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		chatUsageLine(0, 36455, 87),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"there"}]}}`,
		chatUsageLine(1, 73031, 175),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		chatUsageLine(2, 109705, 262),
		// The recorded total is the PROOF the reader's classification rests on.
		`{"type":"session_end","exit_code":0,"total_iterations":3,"total_tokens":109967,"elapsed_sec":10}`,
	}
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.TotalPrompt != 109705 || s.TotalCompletion != 262 {
		t.Fatalf("totals = prompt %d completion %d, want 109705 / 262 (the last "+
			"running reading — summing the column is the defect)", s.TotalPrompt, s.TotalCompletion)
	}
	if s.TotalTokens != 109967 {
		t.Fatalf("TotalTokens = %d, want the session's own recorded 109967", s.TotalTokens)
	}
	if s.RequestUsageSegments != 1 {
		t.Fatalf("segments = %d, want 1", s.RequestUsageSegments)
	}
	if s.RequestUsageBasis == "" {
		t.Fatal("RequestUsageBasis is empty: a derived total must carry the evidence it rests on")
	}

	// AND THE PER-ROUND NUMBERS ARE PER REQUEST, not running totals: a surface
	// that prints round 3's own usage must not print the session's cumulative
	// reading there. 109705 - 73031 = 36674.
	st := BuildStatsFromJSONL(s)
	if len(st.RoundStats) != 3 {
		t.Fatalf("round rows = %d, want 3", len(st.RoundStats))
	}
	if got, want := st.RoundStats[2].PromptTokens, 109705-73031; got != want {
		t.Fatalf("round 3 prompt = %d, want %d (that request's own usage)", got, want)
	}
	if got := st.PromptTokens; got != 109705 {
		t.Fatalf("stats prompt total = %d, want 109705", got)
	}
}

// A RESUMED SESSION SUMS EACH RUN'S LAST READING. The three runs are chat
// session 66243525-16ab-4ce7-9133-84d74c539840's measured last readings
// (44,155,810 / 40,741,990 / 310,935,011 prompt tokens); the recorded total is
// their sum plus the completion side, exactly as that log recorded it.
func TestResumedChatLogSumsEachRunsLastReading(t *testing.T) {
	prompt1, comp1 := 44155810, 133752
	prompt2, comp2 := 40741990, 157742
	prompt3, comp3 := 310935011, 1153659
	total := prompt1 + comp1 + prompt2 + comp2 + prompt3 + comp3
	lines := []string{
		`{"type":"session_start","sessionId":"resumed","timestamp":"2026-10-27T15:41:49Z"}`,
		`{"type":"user","message":{"role":"user","content":"one"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"a1"}]}}`,
		chatUsageLine(0, 142860, 79), // run 1 opens low, climbs
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"a2"}]}}`,
		chatUsageLine(1, 44155810, comp1),
		`{"type":"session_end","exit_code":1,"total_iterations":1,"total_tokens":0,"elapsed_sec":1}`,
		`{"type":"session_start","sessionId":"resumed","timestamp":"2026-10-27T22:00:23Z"}`,
		`{"type":"user","message":{"role":"user","content":"two"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"b1"}]}}`,
		chatUsageLine(0, 137568, 929), // run 2 RESTARTS the counters
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"b2"}]}}`,
		chatUsageLine(1, prompt2, comp2),
		`{"type":"session_end","exit_code":1,"total_iterations":1,"total_tokens":0,"elapsed_sec":1}`,
		`{"type":"session_start","sessionId":"resumed","timestamp":"2026-10-28T01:01:37Z"}`,
		`{"type":"user","message":{"role":"user","content":"three"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"c1"}]}}`,
		chatUsageLine(0, 137568, 929), // run 3
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"c2"}]}}`,
		chatUsageLine(1, prompt3, comp3),
		fmt.Sprintf(`{"type":"session_end","exit_code":0,"total_iterations":1,"total_tokens":%d,"elapsed_sec":1}`, total),
	}
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.TotalPrompt != prompt1+prompt2+prompt3 {
		t.Fatalf("prompt total = %d, want %d (3 runs summed; the session's LAST "+
			"reading alone would be %d)", s.TotalPrompt, prompt1+prompt2+prompt3, prompt3)
	}
	if s.RequestUsageSegments != 3 {
		t.Fatalf("segments = %d, want 3 — a resumed session's totals are a sum "+
			"over its runs and the span must be reported", s.RequestUsageSegments)
	}
	// The span reaches the RENDER, not just the reader: a reader that folds
	// three runs and prints one number leaves the reader of the box unable to
	// tell 3.9e8 tokens from a session that sent that much context once.
	if span := usageSpanText(s.RequestUsageSegments, s.RequestUsageUnknown); span == "" {
		t.Fatal("the render states no span for a 3-run session")
	}
}

// A TASK LOG'S NUMBERS DO NOT MOVE. Its rows are increments; the reader folds
// them by summing, which is what it always did, so both the totals and every
// per-round value are the ones the task path has always reported.
func TestTaskLogTotalsAreUnchangedByTheFold(t *testing.T) {
	lines := []string{
		`{"type":"session_start","sessionId":"task","timestamp":"2026-10-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"do the thing"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`,
		taskUsageLine(0, 40000, 100),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"more"}]}}`,
		taskUsageLine(1, 12000, 40),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		taskUsageLine(2, 65000, 300),
		`{"type":"session_end","exit_code":0,"total_iterations":3,"total_tokens":117440,"elapsed_sec":9}`,
	}
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.TotalPrompt != 117000 || s.TotalCompletion != 440 {
		t.Fatalf("totals = %d / %d, want the column sum 117000 / 440", s.TotalPrompt, s.TotalCompletion)
	}
	if s.RequestUsageSegments != 0 {
		t.Fatalf("segments = %d, want 0 (a delta log has no running counter)", s.RequestUsageSegments)
	}
	st := BuildStatsFromJSONL(s)
	want := []int{40000, 12000, 65000}
	for i, w := range want {
		if st.RoundStats[i].PromptTokens != w {
			t.Fatalf("round %d prompt = %d, want %d (the row as written)",
				i+1, st.RoundStats[i].PromptTokens, w)
		}
	}
}

// A LOG WRITTEN BEFORE THE MARKER IS CLASSIFIED FROM ITS OWN EVIDENCE, and the
// two directions are exercised on the same shape of rows: the same readings are
// a running total when they only rise and no recorded total contradicts it, and
// per-request increments when a fall inside a run proves it.
func TestUntaggedLogsAreClassifiedFromTheirOwnEvidence(t *testing.T) {
	cumulative := []string{
		`{"type":"session_start","sessionId":"legacy-chat","timestamp":"2026-10-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one"}]}}`,
		untaggedUsageLine(0, 30000, 50),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"two"}]}}`,
		untaggedUsageLine(1, 60000, 90),
	}
	s, err := ParseFile(writeFixture(t, cumulative))
	if err != nil {
		t.Fatalf("ParseFile (cumulative): %v", err)
	}
	if s.TotalPrompt != 60000 {
		t.Fatalf("untagged monotone log: prompt = %d, want 60000 (the last reading)", s.TotalPrompt)
	}

	increments := []string{
		`{"type":"session_start","sessionId":"legacy-task","timestamp":"2026-10-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one"}]}}`,
		untaggedUsageLine(0, 65000, 300),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"two"}]}}`,
		untaggedUsageLine(1, 12000, 40),
	}
	s, err = ParseFile(writeFixture(t, increments))
	if err != nil {
		t.Fatalf("ParseFile (increments): %v", err)
	}
	if s.TotalPrompt != 77000 {
		t.Fatalf("untagged falling log: prompt = %d, want 77000 (the column sum)", s.TotalPrompt)
	}
}
