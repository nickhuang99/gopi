package gopilog

import (
	"strings"
	"testing"
)

// contextTrimSession is a realistic session containing context_trim activity:
// a dead-end bash call (large result), followed by a context_trim call that
// REPLACES the dead-end pair with a one-line conclusion (the "replaced"
// outcome), and a second context_trim that DELETES another pair (the
// "removed" outcome). Tool results carry the harness decorations the agent
// loop actually logs: the tick prefix (withTick) and the
// "[tool_use id: …]" stamp header the loop prepends before the payload.
var contextTrimSession = []string{
	`{"model":"deepseek-v4-flash","sessionId":"ct-sess-1","timestamp":"2026-08-11T10:00:00.000+08:00","type":"session_start","workdir":"/tmp/w"}`,
	`{"message":{"content":"[EPIC GOAL]\ncontext trim demo","role":"user"},"promptId":"p1","timestamp":"2026-08-11T10:00:01.0+08:00","type":"user"}`,
	// dead-end bash call with a big result (the trim target)
	`{"message":{"content":[{"id":"call_dead1","input":{"command":"make build"},"name":"bash","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-11T10:00:02.0+08:00","type":"assistant"}`,
	`{"cache_hit":1,"cache_miss":2,"completion_tokens":3,"iteration":0,"prompt_tokens":4,"timestamp":"2026-08-11T10:00:02.5+08:00","type":"request"}`,
	`{"message":{"content":[{"content":"exit=1\nerror: build failed\nlots of compiler noise lines\n[tool_use id: call_dead1]\nexit=1\nerror: build failed\nlots of compiler noise lines","tool_use_id":"call_dead1","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-11T10:00:03.0+08:00","type":"tool_result"}`,
	// another dead-end read call (the second trim target)
	`{"message":{"content":[{"id":"call_dead2","input":{"path":"/tmp/big.log"},"name":"read","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-11T10:00:03.5+08:00","type":"assistant"}`,
	`{"message":{"content":[{"content":"huge log content line1\nline2\n[tool_use id: call_dead2]\nhuge log content line1\nline2","tool_use_id":"call_dead2","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-11T10:00:04.0+08:00","type":"tool_result"}`,
	// context_trim #1: REPLACE call_dead1 with a conclusion
	`{"message":{"content":[{"id":"call_ct1","input":{"call_id":"call_dead1","replace_with":"build failed because of compiler error — don't retry"},"name":"context_trim","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-11T10:00:04.5+08:00","type":"assistant"}`,
	`{"message":{"content":[{"content":"[t+4s] context_trim: replaced result of call call_dead1 with conclusion (pair kept, roles unchanged)","tool_use_id":"call_ct1","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-11T10:00:04.6+08:00","type":"tool_result"}`,
	// context_trim #2: DELETE call_dead2 (no replace_with)
	`{"message":{"content":[{"id":"call_ct2","input":{"call_id":"call_dead2"},"name":"context_trim","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-11T10:00:05.0+08:00","type":"assistant"}`,
	`{"message":{"content":[{"content":"[t+5s] context_trim: removed call call_dead2 + its result (2 message(s) dropped from context)","tool_use_id":"call_ct2","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-11T10:00:05.1+08:00","type":"tool_result"}`,
	// context_trim #3: NO-OP (unknown call)
	`{"message":{"content":[{"id":"call_ct3","input":{"call_id":"call_ghost"},"name":"context_trim","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-11T10:00:05.5+08:00","type":"assistant"}`,
	`{"message":{"content":[{"content":"[t+5s] context_trim: no-op — call call_ghost not found, incomplete pair, or already trimmed (silent)","tool_use_id":"call_ct3","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-11T10:00:05.6+08:00","type":"tool_result"}`,
	`{"elapsed_sec":1.0,"exit_code":0,"total_iterations":2,"total_tokens":10,"timestamp":"2026-08-11T10:00:06.0+08:00","type":"session_end"}`,
}

func TestCollectContextTrims(t *testing.T) {
	s, err := ParseFile(writeFixture(t, contextTrimSession))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	trims := collectContextTrims(s)
	if len(trims) != 3 {
		t.Fatalf("collectContextTrims = %d events, want 3", len(trims))
	}

	// Event 1: replaced — call_dead1 (bash) with a conclusion.
	ev := trims[0]
	if ev.CallID != "call_ct1" {
		t.Errorf("event0 CallID = %q, want call_ct1", ev.CallID)
	}
	if ev.TargetCallID != "call_dead1" {
		t.Errorf("event0 TargetCallID = %q, want call_dead1", ev.TargetCallID)
	}
	if ev.TargetName != "bash" {
		t.Errorf("event0 TargetName = %q, want bash", ev.TargetName)
	}
	if ev.Outcome != "replaced" {
		t.Errorf("event0 Outcome = %q, want replaced", ev.Outcome)
	}
	if !strings.Contains(ev.ReplaceWith, "build failed") {
		t.Errorf("event0 ReplaceWith = %q, want build-failure conclusion", ev.ReplaceWith)
	}
	// BeforeBytes measures the FULL logged result content — the exact string
	// RemoveToolPair replaces in context (tick prefix + stamped tool_use id
	// header + payload duplicated), so the before/after delta is the real
	// context freed. Fixture: "[t+?s] " is absent here; the result is
	// "exit=1\nerror: build failed\nlots of compiler noise lines\n[tool_use id: call_dead1]\nexit=1\nerror: build failed\nlots of compiler noise lines".
	wantBefore1 := len("exit=1\nerror: build failed\nlots of compiler noise lines\n[tool_use id: call_dead1]\nexit=1\nerror: build failed\nlots of compiler noise lines")
	if ev.BeforeBytes != wantBefore1 {
		t.Errorf("event0 BeforeBytes = %d, want full logged size %d", ev.BeforeBytes, wantBefore1)
	}
	if ev.AfterBytes != len(ev.ReplaceWith) {
		t.Errorf("event0 AfterBytes = %d, want %d (len ReplaceWith)", ev.AfterBytes, len(ev.ReplaceWith))
	}
	if ev.AfterBytes >= ev.BeforeBytes {
		t.Errorf("event0 after (%d) should be smaller than before (%d)", ev.AfterBytes, ev.BeforeBytes)
	}

	// Event 2: removed — pair deleted, no replace_with.
	ev = trims[1]
	if ev.CallID != "call_ct2" || ev.TargetCallID != "call_dead2" {
		t.Errorf("event1 ids = %q/%q, want call_ct2/call_dead2", ev.CallID, ev.TargetCallID)
	}
	if ev.TargetName != "read" {
		t.Errorf("event1 TargetName = %q, want read", ev.TargetName)
	}
	if ev.Outcome != "removed" {
		t.Errorf("event1 Outcome = %q, want removed", ev.Outcome)
	}
	if ev.RemovedMessages != 2 {
		t.Errorf("event1 RemovedMessages = %d, want 2", ev.RemovedMessages)
	}
	if ev.ReplaceWith != "" {
		t.Errorf("event1 ReplaceWith = %q, want empty", ev.ReplaceWith)
	}
	if ev.AfterBytes != 0 {
		t.Errorf("event1 AfterBytes = %d, want 0 (pair deleted)", ev.AfterBytes)
	}
	wantBefore2 := len("huge log content line1\nline2\n[tool_use id: call_dead2]\nhuge log content line1\nline2")
	if ev.BeforeBytes != wantBefore2 {
		t.Errorf("event1 BeforeBytes = %d, want full logged size %d", ev.BeforeBytes, wantBefore2)
	}

	// Event 3: no-op — target call not present in the log.
	ev = trims[2]
	if ev.CallID != "call_ct3" || ev.Outcome != "noop" {
		t.Errorf("event2 = %q/%q, want call_ct3/noop", ev.CallID, ev.Outcome)
	}
	if ev.BeforeBytes != 0 {
		t.Errorf("event2 BeforeBytes = %d, want 0 (target absent)", ev.BeforeBytes)
	}
}

func TestFormatContextTrim(t *testing.T) {
	s, err := ParseFile(writeFixture(t, contextTrimSession))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	out := FormatContextTrim(s)

	// header
	for _, want := range []string{"Session: ct-sess-1", "context_trim activity", "CONTEXT TRIM — 3 event(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatContextTrim missing %q\n%s", want, out)
		}
	}
	// per-event rows: context_trim call id, trimmed call (name + id), outcome
	for _, want := range []string{
		"call_ct1", "bash call_dead1", "replaced",
		"call_ct2", "read call_dead2", "removed",
		"call_ct3", "noop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatContextTrim missing %q\n%s", want, out)
		}
	}
	// size comparison (payload-only before size)
	if !strings.Contains(out, "BEFORE") || !strings.Contains(out, "AFTER") {
		t.Errorf("FormatContextTrim missing BEFORE/AFTER columns\n%s", out)
	}
	if !strings.Contains(out, "replaced with") {
		t.Errorf("FormatContextTrim missing replace detail\n%s", out)
	}
	// the replace_with conclusion survives (防遗忘)
	if !strings.Contains(out, "build failed because of compiler error") {
		t.Errorf("FormatContextTrim missing replace_with conclusion\n%s", out)
	}
}

func TestFormatContextTrimNoActivity(t *testing.T) {
	s, err := ParseFile(writeFixture(t, anthropicSession))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	out := FormatContextTrim(s)
	if !strings.Contains(out, "No context_trim activity") {
		t.Errorf("empty state missing, got:\n%s", out)
	}
}
