package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// spillBriefingText builds a payload larger than the engine's inline budget.
// The tail is forced non-space: the engine TrimSpaces a submitted payload,
// so a fixture ending in '\n' would be stored one byte shorter.
func spillBriefingText(n int, marker string) string {
	var b strings.Builder
	b.WriteString("BRIEFING " + marker + "\n")
	for b.Len() < n {
		b.WriteString("detail line: the quick brown fox jumps over the lazy dog\n")
	}
	out := b.String()
	if strings.HasSuffix(out, "\n") {
		out = strings.TrimSuffix(out, "\n") + "x"
	}
	return out
}

// TestTaskRunBriefingSpilledPayloadStaysSmall — the epic's whole point,
// measured at the only place that matters: the string handed to the child.
// A submit far above the inline limit must produce a briefing whose payload
// section is a ONE-LINE pointer, not the text; the [Attachments] index must
// name the replica; and the replica must contain the submitted bytes.
func TestTaskRunBriefingSpilledPayloadStaysSmall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))

	orig := spillBriefingText(200_000, "HUGE-BRIEFING") // ~200 KB — far over the limit
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "huge briefing task",
		Payload: orig,
		TaskID:  "t-spill-brief",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	got, err := taskRunBriefing("t-spill-brief")
	if err != nil {
		t.Fatalf("taskRunBriefing: %v", err)
	}
	// (a) the 200 KB body is NOT in the briefing.
	if len(got) > 4096 {
		t.Errorf("briefing is %d bytes for a 200 KB payload — the payload reached the child inline:\n%.400s", len(got), got)
	}
	if strings.Contains(got, "detail line: the quick brown fox jumps over the lazy dog\ndetail line") {
		t.Errorf("briefing still embeds the payload body:\n%.400s", got)
	}
	// (b) the pointer is present under the payload header, and it is one line.
	if !strings.Contains(got, "[Briefing payload]\n"+taskBriefingPayloadOmittedPrefixForTest) {
		t.Errorf("briefing lacks the omitted-payload pointer under [Briefing payload]:\n%s", got)
	}
	payloadSection := got[strings.Index(got, "[Briefing payload]"):]
	if i := strings.Index(payloadSection, "\n"); i >= 0 {
		line := payloadSection[len("[Briefing payload]\n"):]
		if j := strings.Index(line, "\n"); j >= 0 {
			line = line[:j]
		}
		if !strings.HasPrefix(line, taskBriefingPayloadOmittedPrefixForTest) {
			t.Errorf("payload section is not a single pointer line: %q", line)
		}
		if len(line) > 2048 {
			t.Errorf("pointer line is %d bytes — too large to be a pointer", len(line))
		}
	}
	// (c) the [Attachments] index names a replica containing the FULL text.
	// (The index line carries the replica's ABSOLUTE path — use it as-is.)
	replica := replicaFromBriefing(t, got)
	data, err := os.ReadFile(replica)
	if err != nil {
		t.Fatalf("replica %s unreadable: %v", replica, err)
	}
	if string(data) != orig {
		t.Errorf("replica is not the submitted payload (%d vs %d bytes)", len(data), len(orig))
	}
	// (d) the pointer names that same replica (no divergence between the
	// pointer text and the index — both must describe one file).
	if !strings.Contains(got, replica) {
		t.Errorf("briefing does not name the replica %s:\n%s", replica, got)
	}
}

// taskBriefingPayloadOmittedPrefixForTest mirrors the engine's pointer
// opening. The cmd/agent package cannot reference the internal/tools
// constant (unexported), so the assertion pins the literal the child sees —
// if the wording changes, this test fails and the briefing contract is
// re-read deliberately rather than silently drifting.
const taskBriefingPayloadOmittedPrefixForTest = "[Briefing payload omitted:"

// replicaFromBriefing extracts the first [attachmentN: <path>] path.
func replicaFromBriefing(t *testing.T, briefing string) string {
	t.Helper()
	for _, line := range strings.Split(briefing, "\n") {
		if !strings.HasPrefix(line, "[attachment") {
			continue
		}
		i := strings.Index(line, ": ")
		if i < 0 || !strings.HasSuffix(line, "]") {
			t.Fatalf("malformed attachment index line: %q", line)
		}
		return strings.TrimSuffix(line[i+2:], "]")
	}
	t.Fatalf("no [attachmentN:] line in briefing:\n%s", briefing)
	return ""
}

// TestTaskRunBriefingInlinePayloadUnchanged — the regression contract: a
// payload within budget still reaches the child verbatim (no pointer, no
// attachment index).
func TestTaskRunBriefingInlinePayloadUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))

	body := "short briefing body\nwith two lines"
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "small briefing task",
		Payload: body,
		TaskID:  "t-inline-brief",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	got, err := taskRunBriefing("t-inline-brief")
	if err != nil {
		t.Fatalf("taskRunBriefing: %v", err)
	}
	want := "[Task summary]\nsmall briefing task\n[Briefing payload]\n" + body
	if got != want {
		t.Errorf("inline briefing changed:\ngot  %q\nwant %q", got, want)
	}
}
