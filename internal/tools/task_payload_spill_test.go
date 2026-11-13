package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// ---------------------------------------------------------------------------
// Payload cap (epic "task payload length must be caped"): an oversized
// payload/summary is preserved VERBATIM in the task session dir and the
// ledger column becomes a one-line pointer at the attachment.
// ---------------------------------------------------------------------------

// spillTestText builds a payload of exactly n bytes with recognisable
// content (a line-oriented body — payloads are text) plus a unique marker
// line so a test can assert the ORIGINAL reached the replica byte for byte.
// The tail is forced non-space: TaskSubmit TrimSpaces the payload, so a
// fixture ending in '\n' would be stored one byte shorter than it was built
// (and the byte-exactness assertions would compare apples to oranges).
func spillTestText(n int, marker string) string {
	head := "TASK PAYLOAD " + marker + "\n"
	if n <= len(head) {
		return head[:n]
	}
	var b strings.Builder
	b.WriteString(head)
	line := 0
	for b.Len() <= n {
		fmt.Fprintf(&b, "line %04d: the quick brown fox jumps over the lazy dog\n", line)
		line++
	}
	out := b.String()[:n]
	if last := out[len(out)-1]; last == '\n' || last == ' ' || last == '\t' || last == '\r' {
		out = out[:len(out)-1] + "x"
	}
	if len(out) != n {
		panic("spillTestText: length drift")
	}
	return out
}

// assertPointerEnvelope checks the ONE thing the pointer must guarantee
// regardless of where the sandbox lives: everything it says AROUND the
// replica path (size clause + digest + read instruction + preview) fits in
// taskPointerEnvelopeMaxBytes. The path itself is exempt — it is the file's
// identity and the child needs it verbatim.
func assertPointerEnvelope(t *testing.T, pointer, replica string) {
	t.Helper()
	envelope := strings.ReplaceAll(pointer, replica, "")
	if len(envelope) > taskPointerEnvelopeMaxBytes {
		t.Errorf("pointer envelope is %d bytes, over the %d-byte budget:\n%s",
			len(envelope), taskPointerEnvelopeMaxBytes, pointer)
	}
}

// spillRowPayload reads the stored payload/summary columns straight from the
// ledger (raw SQL, not the composer under test — a fix must be observable in
// the persisted row).
func spillRowPayload(t *testing.T, taskID string) (summary, payload, deliverable string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	err = db.QueryRowContext(ctx,
		`SELECT summary, payload, deliverable FROM tasks WHERE task_id = ?`, taskID).
		Scan(&summary, &payload, &deliverable)
	if err != nil {
		t.Fatalf("read row %s: %v", taskID, err)
	}
	return summary, payload, deliverable
}

// spillReplicaFor reads the manifest carried in the row's deliverable and
// returns the replica ABSOLUTE path of manifest entry idx (0-based) after
// asserting it exists on disk, plus the entry itself.
func spillReplicaFor(t *testing.T, gopiHome, taskID, deliverable string, idx int) (TaskAttachment, string) {
	t.Helper()
	atts, idxs, err := taskDeliverableAttachmentsRaw(taskID, deliverable)
	if err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	if len(atts) <= idx {
		t.Fatalf("manifest has %d entries, want > %d (%+v)", len(atts), idx, atts)
	}
	if len(idxs) != len(atts) {
		t.Fatalf("index length %d != manifest length %d", len(idxs), len(atts))
	}
	abs := idxs[idx].Path
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("replica %s does not exist: %v", abs, err)
	}
	if !strings.HasPrefix(abs, filepath.Join(gopiHome, ".gopi")) {
		t.Errorf("replica %s is not under the sandboxed gopi home %s", abs, gopiHome)
	}
	return atts[idx], abs
}

// TestPayloadSpillOverLimitConvertsToAttachment — the epic's core demand:
// a payload above the inline limit is NOT truncated and NOT dispatched
// inline. The ledger payload column becomes a one-line pointer, the FULL
// original text is stored byte-for-byte under the task session dir, and the
// row's manifest carries it as attachment1 so the child's [attachmentN:]
// index resolves it.
func TestPayloadSpillOverLimitConvertsToAttachment(t *testing.T) {
	home := phase1AttachEnv(t)
	orig := spillTestText(taskPayloadInlineMaxBytes*4, "SPILL-MARKER-OVER")

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "oversized payload task",
		Payload: orig,
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	summary, payload, deliverable := spillRowPayload(t, taskID)

	// (a) the column no longer carries the text.
	if len(payload) >= len(orig) {
		t.Fatalf("stored payload is %d bytes, want far below the submitted %d", len(payload), len(orig))
	}
	if strings.Contains(payload, "SPILL-MARKER-OVER\nline 0001") {
		t.Errorf("stored payload still embeds the body:\n%s", payload)
	}
	// (b) the column is a POINTER naming the limit and the replica.
	if !strings.HasPrefix(payload, taskPayloadSpillPointerPrefix) {
		t.Fatalf("payload pointer prefix missing:\n%s", payload)
	}
	if !strings.Contains(payload, fmt.Sprintf("%d bytes exceed the %d-byte inline limit", len(orig), taskPayloadInlineMaxBytes)) {
		t.Errorf("pointer does not name the sizes:\n%s", payload)
	}
	if strings.Contains(payload, "\n") {
		t.Errorf("pointer must stay ONE line (the briefing block it replaces is the whole point):\n%s", payload)
	}
	// (c) the summary still reads as the task's intent (untouched — only the
	// payload was oversized).
	if summary != "oversized payload task" {
		t.Errorf("summary = %q, want it untouched", summary)
	}
	// (d) attachment1 is the spilled payload, byte-for-byte the original.
	att, abs := spillReplicaFor(t, home, taskID, deliverable, 0)
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read replica: %v", err)
	}
	if string(got) != orig {
		t.Errorf("replica is not the submitted payload: got %d bytes, want %d (sha mismatch? manifest sha=%s)",
			len(got), len(orig), att.SHA256)
	}
	if !strings.Contains(payload, abs) {
		t.Errorf("pointer does not name the replica path %s:\n%s", abs, payload)
	}
	if !strings.Contains(payload, att.SHA256) {
		t.Errorf("pointer does not carry the digest %s (the child cannot verify what it reads):\n%s", att.SHA256, payload)
	}
	// (e) the submit confirmation reported the conversion.
	if !strings.Contains(out, "over the") || !strings.Contains(out, abs) {
		t.Errorf("submit confirmation does not report the spill:\n%s", out)
	}
}

// TestPayloadSpillUnderLimitStaysInline — the regression contract: a payload
// within budget is stored and dispatched exactly as before (no attachment,
// no pointer, the v1 repo-only deliverable shape).
func TestPayloadSpillUnderLimitStaysInline(t *testing.T) {
	phase1AttachEnv(t)
	orig := spillTestText(taskPayloadInlineMaxBytes, "STAYS-INLINE")

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "small payload task",
		Payload: orig,
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	_, payload, deliverable := spillRowPayload(t, taskID)
	if payload != orig {
		t.Errorf("payload = %d bytes, want the submitted %d stored verbatim", len(payload), len(orig))
	}
	if strings.Contains(payload, taskPayloadSpillPointerPrefix) {
		t.Errorf("a payload AT the limit must never spill:\n%s", payload)
	}
	if !strings.Contains(out, "Task queued:") || strings.Contains(out, "over the") {
		t.Errorf("an inline submit must not report a conversion:\n%s", out)
	}
	if atts, _, err := taskDeliverableAttachmentsRaw(taskID, deliverable); err != nil || len(atts) != 0 {
		t.Errorf("inline submit must carry no attachments manifest (atts=%d err=%v)", len(atts), err)
	}
}

// TestPayloadSpillBoundaryIsExact — off-by-one guard on the ONE threshold:
// limit bytes inline, limit+1 spilled. (Rule 10 minimal-sample discipline:
// the boundary is where a cap breaks.)
func TestPayloadSpillBoundaryIsExact(t *testing.T) {
	phase1AttachEnv(t)
	atLimit := spillTestText(taskPayloadInlineMaxBytes, "BOUNDARY-AT")
	overLimit := spillTestText(taskPayloadInlineMaxBytes+1, "BOUNDARY-OVER")

	for _, tc := range []struct {
		name  string
		text  string
		spill bool
	}{
		{"at-limit", atLimit, false},
		{"one-over", overLimit, true},
	} {
		out, err := TaskSubmit(TaskSubmitOptions{Summary: "boundary " + tc.name, Payload: tc.text})
		if err != nil {
			t.Fatalf("%s: TaskSubmit: %v", tc.name, err)
		}
		taskID := extractTaskID(t, out)
		_, payload, _ := spillRowPayload(t, taskID)
		gotSpill := strings.HasPrefix(payload, taskPayloadSpillPointerPrefix)
		if gotSpill != tc.spill {
			t.Errorf("%s (%d bytes): spilled=%v, want %v", tc.name, len(tc.text), gotSpill, tc.spill)
		}
		if !tc.spill && payload != tc.text {
			t.Errorf("%s: inline payload altered", tc.name)
		}
	}
}

// TestPayloadSpillKeepsSubmitterAttachmentsAndIndex — a spilled payload must
// NOT renumber or displace the submitter's own --attachment files: the spill
// takes manifest slot 1, the user's files keep their order from slot 2, and
// every replica (including the user's untouched originals) is intact.
func TestPayloadSpillKeepsSubmitterAttachmentsAndIndex(t *testing.T) {
	home := phase1AttachEnv(t)
	dir := filepath.Join(home, "userfiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fileA := filepath.Join(dir, "a.log")
	fileB := filepath.Join(dir, "b.log")
	for p, body := range map[string]string{fileA: "log-A", fileB: "log-B"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orig := spillTestText(taskPayloadInlineMaxBytes*3, "SPILL-WITH-ATTACH")

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:     "spill plus attachments",
		Payload:     orig,
		Attachments: []string{fileA, fileB},
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	_, payload, deliverable := spillRowPayload(t, taskID)

	// manifest order: spill, then the user's files in submission order.
	_, spillRep := spillReplicaFor(t, home, taskID, deliverable, 0)
	if got, err := os.ReadFile(spillRep); err != nil || string(got) != orig {
		t.Fatalf("attachment1 must be the spilled payload (err=%v)", err)
	}
	for i, tc := range []struct {
		src  string
		body string
	}{{fileA, "log-A"}, {fileB, "log-B"}} {
		att, replica := spillReplicaFor(t, home, taskID, deliverable, i+1)
		if att.OriginalAbsPath != tc.src {
			t.Errorf("manifest[%d].original_abs_path = %q, want %q", i+1, att.OriginalAbsPath, tc.src)
		}
		body, err := os.ReadFile(replica)
		if err != nil {
			t.Fatalf("read replica %s: %v", replica, err)
		}
		if string(body) != tc.body {
			t.Errorf("replica %s = %q, want %q", replica, body, tc.body)
		}
	}
	// The pointer points at slot 1 — the spilled payload, never a user file.
	_, spillAgain := spillReplicaFor(t, home, taskID, deliverable, 0)
	if !strings.Contains(payload, spillAgain) {
		t.Errorf("pointer must name the spill replica %s:\n%s", spillAgain, payload)
	}
	if strings.Contains(payload, fileA) {
		t.Errorf("pointer names a user attachment path — wrong slot:\n%s", payload)
	}
}

// TestPayloadSpillRepoLessSubmit — the fingerprint-scanning auto-submit paths
// (branch-scan merge-backlog, push notify, conflict dispatch) can run from a
// non-work-tree cwd and therefore carry NO deliverable blob. The spill must
// still preserve the text AND the pointer must stay self-sufficient (it
// names the absolute replica path, so a child without a manifest still finds
// its briefing).
func TestPayloadSpillRepoLessSubmit(t *testing.T) {
	home := phase1AttachEnv(t)
	t.Chdir(t.TempDir()) // not a git work tree → no deliverable.repo
	orig := spillTestText(taskPayloadInlineMaxBytes*2, "SPILL-NO-REPO")

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "repo-less oversized payload",
		Payload: orig,
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	_, payload, deliverable := spillRowPayload(t, taskID)
	if deliverable != "" {
		t.Fatalf("fixture assumption broken: a non-repo cwd produced deliverable %q", deliverable)
	}
	if !strings.HasPrefix(payload, taskPayloadSpillPointerPrefix) {
		t.Fatalf("payload was not converted:\n%s", payload)
	}
	// The pointer names the REPLICA (the file the manifest would have
	// indexed), and that file must exist with the full text — this is the
	// self-sufficiency guarantee for a row whose deliverable was dropped.
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("GopiDir: %v", err)
	}
	sess := filepath.Join(gd, config.SessionsDirName, taskID)
	src := filepath.Join(sess, taskPayloadSpillSourceDirName, taskPayloadSpillFileName)
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("spill source missing: %v", err)
	}
	if string(got) != orig {
		t.Errorf("spill source is not the submitted text (%d vs %d bytes)", len(got), len(orig))
	}
	replica := filepath.Join(sess, filepath.FromSlash(taskPayloadSpillReplicaRel))
	if !strings.Contains(payload, replica) {
		t.Errorf("pointer must name the replica even without a manifest:\n%s\n(want path %s)", payload, replica)
	}
	if data, err := os.ReadFile(replica); err != nil || string(data) != orig {
		t.Errorf("pointer's replica %s unreadable or not the payload (err=%v)", replica, err)
	}
	if strings.Contains(payload, "\n") {
		t.Errorf("pointer must stay one line:\n%s", payload)
	}
	if !strings.Contains(src, home) {
		t.Errorf("spill source %s escaped the sandboxed home %s", src, home)
	}
}

// TestPayloadSpillOversizedSummaryPositionalShape — the positional submit
// form (`gopi task submit "<huge text>"`) puts the whole request in the
// SUMMARY column and mirrors it into the payload. Both columns must leave
// the inline budget: the text survives in the attachment, the payload column
// keeps a one-line pointer (a payload-less row would never be dispatched),
// and the summary collapses to a clipped headline.
func TestPayloadSpillOversizedSummaryPositionalShape(t *testing.T) {
	home := phase1AttachEnv(t)
	huge := spillTestText(taskPayloadInlineMaxBytes*3, "POSITIONAL-HUGE")

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: huge,
		Payload: huge, // the CLI's positional mirror
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	summary, payload, deliverable := spillRowPayload(t, taskID)

	if len(summary) > taskSummaryHeadlineMaxBytes+len("…") {
		t.Errorf("summary is %d bytes — the headline column must collapse", len(summary))
	}
	if !strings.HasPrefix(summary, "TASK PAYLOAD POSITIONAL-HUGE") {
		t.Errorf("clipped headline lost the task's first words: %q", summary)
	}
	if !strings.HasPrefix(payload, taskPayloadSpillPointerPrefix) {
		t.Fatalf("payload column must carry the pointer (payload-less rows are never dispatched):\n%s", payload)
	}
	att, abs := spillReplicaFor(t, home, taskID, deliverable, 0)
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read replica: %v", err)
	}
	if string(got) != huge {
		t.Errorf("replica != submitted text (%d vs %d bytes, sha %s)", len(got), len(huge), att.SHA256)
	}
}

// TestPayloadSpillSubstringPreservedForDedup — the ledger relies on payload
// SUBSTRING scans (branch-scan merge-backlog dedup, agency fence counting,
// `task list --grep`). A spilled payload must keep its opening text in the
// pointer so an existing needle still matches.
func TestPayloadSpillSubstringPreservedForDedup(t *testing.T) {
	home := phase1AttachEnv(t)
	needle := "branch feat/merge-backlog-dedupe-needle"
	body := needle + "\n" + spillTestText(taskPayloadInlineMaxBytes*2, "DEDUP")

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "dedup needle task", Payload: body})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	_, payload, deliverable := spillRowPayload(t, taskID)
	if !strings.Contains(payload, needle) {
		t.Errorf("pointer dropped the payload's opening text — payload-substring dedup would miss this row:\n%s", payload)
	}
	_, replica := spillReplicaFor(t, home, taskID, deliverable, 0)
	assertPointerEnvelope(t, payload, replica)
}

// TestPayloadSpillIsByteExactForBinaryishText — the spill must not
// re-encode: CRLF, tabs and non-ASCII survive the round trip (the child
// reads a briefing, not a normalized document).
func TestPayloadSpillIsByteExactForBinaryishText(t *testing.T) {
	home := phase1AttachEnv(t)
	var b strings.Builder
	for i := 0; i < taskPayloadInlineMaxBytes/8; i++ {
		b.WriteString("行\tline\r\n\x01")
	}
	orig := b.String()
	if len(orig) <= taskPayloadInlineMaxBytes {
		t.Fatalf("fixture too small: %d", len(orig))
	}
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "byte-exact spill", Payload: orig})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)
	_, _, deliverable := spillRowPayload(t, taskID)
	_, abs := spillReplicaFor(t, home, taskID, deliverable, 0)
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read replica: %v", err)
	}
	if string(got) != orig {
		t.Errorf("replica bytes differ from the submitted payload (%d vs %d)", len(got), len(orig))
	}
}
