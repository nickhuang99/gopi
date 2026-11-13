package checkpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/scratch"
)

// buildTrimFixture writes a raw log big enough for the REAL trim arm to rewrite
// it, and returns its path plus the cap that gets it over the line.
//
// The entries are shaped exactly as internal/agent's raw logger writes them
// (marker line, single-line JSON request payload, terminator newline), and the
// file holds MANY rounds so the trim's head/tail windows are separated by a
// real dropped middle. Model names encode the round number, which is what lets
// a test say WHICH round a reader answered from — the entire defect is that the
// answer silently came from round 1.
// trimFixtureRoundBytes sizes one round's request body ABOVE the trim tail
// budget, which is what makes the fixture reproduce the measured defect.
const trimFixtureRoundBytes = 32 << 10

func buildTrimFixture(t *testing.T, rounds int) (path string, capBytes int64) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "fixture.raw.log")
	var b strings.Builder
	b.WriteString("# gopi raw log — created 2026-10-31T00:00:00Z pid=1 session=fixture\n")
	for i := 1; i <= rounds; i++ {
		b.WriteString("→ REQUEST\n")
		fmt.Fprintf(&b, `{"model":"round-%03d","max_tokens":%d,"messages":[{"role":"user","content":[{"type":"text","text":"%s"}]}]}`,
			i, 100, strings.Repeat("x", trimFixtureRoundBytes))
		b.WriteString("\n")
		b.WriteString("← CHUNK\n")
		b.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// THE DANGEROUS SHAPE, reproduced on purpose: the cap sits just over the
	// trim arm's floor while every round is FAR bigger than the tail budget, so
	// the rewrite's tail begins in the MIDDLE of a round. The last `→ REQUEST`
	// marker in the rewritten file is therefore the HEAD's — the exact shape in
	// which its payload runs past the marker line and swallows it
	// (`invalid character '#'`, the measured symptom).
	capBytes = scratch.SessionsRawLogTrimMinCapBytes * 2
	return path, capBytes
}

// TestLastRequest_TrimmedLogIsRefusedByName is the regression this change
// exists for.
//
// PRE-FIX BEHAVIOUR (measured, and the reason this test is written against the
// REAL trim arm rather than a hand-made marker line): `TrimRawLog` wrote head +
// `# gopi … TRIMMED …` + tail; the parser knew only the three round directions,
// so the head's `→ REQUEST` ran to the next round marker on the far side of the
// marker line and swallowed it. `LastRequest` then failed with
//
//	parse last → REQUEST: invalid character '#' after top-level value
//
// — a JSON syntax error naming neither the tool nor the fact that the file had
// been rewritten. After the fix the same file answers with the NAMED refusal, so
// a caller can tell "this log was bounded" from "this log is corrupt".
func TestLastRequest_TrimmedLogIsRefusedByName(t *testing.T) {
	path, capBytes := buildTrimFixture(t, 40)

	// BEFORE the trim: the fixture answers, and the answer is the LAST round —
	// the property the trim destroys.
	before, beforeReq, err := LastRequest(path)
	if err != nil {
		t.Fatalf("pre-trim LastRequest must succeed: %v", err)
	}
	if beforeReq.Model != "round-040" {
		t.Fatalf("pre-trim checkpoint is %q, want round-040 (the fixture's last round)", beforeReq.Model)
	}
	if before.Offset <= 0 {
		t.Fatalf("pre-trim request offset = %d, want the marker's real position", before.Offset)
	}

	// The REAL rewrite — the same verb the operator runs.
	out, err := scratch.TrimRawLog(path, capBytes)
	if err != nil {
		t.Fatalf("TrimRawLog: %v", err)
	}
	if !out.Trimmed {
		t.Fatalf("TrimRawLog refused to rewrite the fixture (before=%d cap=%d after=%d) — the fixture is not over the cap", out.Before, capBytes, out.After)
	}
	if out.After > capBytes {
		t.Fatalf("trimmed file is %d bytes, over the %d cap", out.After, capBytes)
	}

	// The marker is on disk and is the writer's own spelling (Rules 1/13/14).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trimmed log: %v", err)
	}
	if !strings.Contains(string(raw), scratch.SessionsRawLogTrimMarkerPrefix) {
		t.Fatalf("trimmed log carries no %q marker", scratch.SessionsRawLogTrimMarkerPrefix)
	}

	// AFTER the trim: the NAMED refusal, not a JSON syntax error.
	entry, req, err := LastRequest(path)
	if err == nil {
		t.Fatalf("post-trim LastRequest returned a checkpoint (model=%q, %d bytes) from a log whose middle is gone — that answer is the head's first round presented as the last state", req.Model, len(entry.Payload))
	}
	if !errors.Is(err, ErrTrimmedRawLog) {
		t.Fatalf("post-trim error is not ErrTrimmedRawLog: %v", err)
	}
	// Rule 15: the refusal names the file and the reason, in its own sentence.
	if !strings.Contains(err.Error(), path) {
		t.Errorf("refusal does not name the file: %v", err)
	}
	if !strings.Contains(err.Error(), scratch.TrimmedLogResumeRefusal) {
		t.Errorf("refusal does not carry the shared reason sentence %q: %v", scratch.TrimmedLogResumeRefusal, err)
	}
}

// TestTrimMarkerEndsTheRecord pins the PARSER half on its own: a marker line at
// a line boundary terminates the payload before it, so the `#` text can never
// reach json.Unmarshal. This arm fails pre-fix with exactly the measured
// `invalid character '#' after top-level value`, independent of the refusal.
func TestTrimMarkerEndsTheRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gopi-raw-test.log")
	body := `{"model":"m1","max_tokens":10,"messages":[{"role":"user","content":[{"type":"text","text":"first"}]}]}`
	content := "→ REQUEST\n" + body + "\n" +
		scratch.SessionsRawLogTrimMarkerPrefix + "at 2026-10-31T00:00:00Z — dropped 10 bytes\n" +
		"→ REQUEST\n" + strings.Replace(body, "m1", "m2", 1) + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	// findMarker must SEE the header — it is a record boundary like the others.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	marker, off := findMarker(data, len("→ REQUEST\n"+body+"\n"))
	if marker != dirMarker {
		t.Fatalf("findMarker saw %q at the head, want the trim header %q — a marker the parser cannot see is a marker that swallows a payload", marker, dirMarker)
	}
	if off <= 0 {
		t.Fatalf("trim header offset = %d, want its real position", off)
	}

	// And the ENTRY-level consequence, exercised on the parser itself: the
	// payload span stops at the header, so what reaches json.Unmarshal carries
	// no `#`. (LastRequest is not the instrument here — it refuses a trimmed log
	// before parsing, by design; that refusal has its own test above.)
	head := []byte("→ REQUEST\n" + body + "\n")
	entry, ok := lastRequestInWindow(append(head, []byte(scratch.SessionsRawLogTrimMarkerPrefix+"at 2026-10-31T00:00:00Z — dropped 10 bytes\n")...), 0)
	if !ok {
		t.Fatalf("lastRequestInWindow found no request before the header")
	}
	if strings.Contains(string(entry.Payload), "#") {
		t.Fatalf("the request payload swallowed the trim header: %q — pre-fix this is what json.Unmarshal rejects with `invalid character '#' after top-level value`", tail(string(entry.Payload), 120))
	}
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal(entry.Payload, &req); err != nil {
		t.Fatalf("payload ending at the trim header does not parse: %v (payload tail: %q)", err, tail(string(entry.Payload), 120))
	}
	if req.Model != "m1" {
		t.Fatalf("parsed model = %q, want m1", req.Model)
	}
}

// tail is a test-only helper for a readable failure message: the last n bytes
// of s, or s itself.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// TestTrimmedMarkerRecordIsScanVisible keeps the RECORD TYPE honest: an Entry
// built from the header has its own direction, so a caller that walks entries
// can tell a header from a round instead of learning it from a JSON error.
func TestTrimmedMarkerRecordIsScanVisible(t *testing.T) {
	if got := DirectionMarker; got != scratch.SessionsRawLogTrimMarkerPrefix {
		t.Fatalf("DirectionMarker = %q, want the writer's prefix %q (a mirrored literal would drift)", got, scratch.SessionsRawLogTrimMarkerPrefix)
	}
	if dirMarker != scratch.SessionsRawLogTrimMarkerPrefix {
		t.Fatalf("dirMarker = %q, want %q", dirMarker, scratch.SessionsRawLogTrimMarkerPrefix)
	}
}
