package tools

// task_payload_fileref_test.go — the regression guard for the defect this run
// repairs: the `task_submit` TOOL stored a literal "@<abs path>" string as the
// briefing while `gopi task submit --payload @FILE` stored the file's CONTENT.
//
// The tests drive the REAL engine (TaskSubmitReturningID — what both faces
// call) against an isolated ledger and read the PAYLOAD COLUMN back, because
// that column is the executor's only input: a row whose payload is a path
// carries no evidence, and one whose payload is the file's bytes does. The
// measured pre-fix values are quoted in the failure messages (88 bytes =
// len("@"+the path) while the file held 2677/3406/3742).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// payloadFileRefLedger prepares an isolated ledger for one submit and returns
// the path (the Rule 32 scaffold, never a hand-rolled database path).
func payloadFileRefLedger(t *testing.T) string {
	t.Helper()
	return testutil.NewTestDBPath(t)
}

// payloadFileRefRowPayload reads the stored payload column back through the
// ledger reader, so the assertion is about what a dispatcher will actually
// hand the executor.
func payloadFileRefRowPayload(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db := openTasksDBForTest(t)
	row, err := taskByID(ctx, db, taskID)
	if err != nil {
		t.Fatalf("read row %s: %v", taskID, err)
	}
	if row == nil {
		t.Fatalf("row %s is missing from the ledger", taskID)
	}
	return row.Payload
}

// writePayloadFileRefFixture writes a briefing whose bytes deliberately include
// a multi-byte character, a trailing newline and a line beginning with '@' —
// the three shapes an argv round-trip or a second expansion pass would damage.
func writePayloadFileRefFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "briefing.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture briefing: %v", err)
	}
	return path
}

// submitStoredPayload runs one submit through the REAL engine and returns the
// payload column the ledger then holds — the value a dispatcher will hand the
// executor, which is the thing this whole file is about.
func submitStoredPayload(t *testing.T, summary, payload string) (string, string) {
	t.Helper()
	out, taskID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Source:  "human",
	})
	if err != nil {
		t.Fatalf("submit %q: %v", summary, err)
	}
	if taskID == "" {
		t.Fatalf("submit %q returned no row id: %s", summary, out)
	}
	return payloadFileRefRowPayload(t, taskID), out
}

// TestTaskSubmitReadsPayloadFileRef is the RED-proof test: before the fix the
// stored payload was the SPELLING (len("@/…") bytes) and the file's content
// never reached the row.
//
// THE COMPARISON IS FACE-TO-FACE, not reference-to-raw-bytes. The engine has
// always normalised the payload column with strings.TrimSpace (a pre-existing
// rule shared by every face: a briefing's LEADING/trailing blank lines are not
// its text), so "byte-identical to the CLI" means identical to what the CLI
// FACE produces end to end. The second submit below is that face's value: the
// CLI expanded the reference in its own parser and handed the engine the
// CONTENT — so if the tool face and that value agree, the two faces agree, and
// the defect (the tool face storing the spelling) is gone.
func TestTaskSubmitReadsPayloadFileRef(t *testing.T) {
	payloadFileRefLedger(t)
	// 3 KB of evidence, the size the measured bug rows lost.
	body := "@mention at line start — 中文 evidence\n" + strings.Repeat("evidence line: the briefing is the file's CONTENT\n", 60)
	path := writePayloadFileRefFixture(t, body)
	if len(body) < 1024 {
		t.Fatalf("fixture too small to be a meaningful probe: %d bytes", len(body))
	}

	got, out := submitStoredPayload(t, "payload file reference through the tool face", "@"+path)
	cliExpanded, err := config.ExpandFileRef("@" + path)
	if err != nil {
		t.Fatalf("the CLI reader: %v", err)
	}
	cliFace, _ := submitStoredPayload(t, "the CLI face's end-to-end value", cliExpanded)
	if got != cliFace {
		t.Fatalf("the tool face and the CLI face must store the SAME payload:\ntool face (%d bytes): %q\nCLI face  (%d bytes): %q",
			len(got), got, len(cliFace), cliFace)
	}
	if want := strings.TrimSpace(body); got != want {
		t.Fatalf("the row's payload must be the file's CONTENT (modulo the engine's pre-existing TrimSpace):\nwant %d bytes: %q\ngot  %d bytes: %q\nthe @path SPELLING was stored — that is the defect (len(\"@\"+path) = %d)",
			len(want), want, len(got), got, len("@"+path))
	}
	// The conversion is REPORTED where the submitter looks (Rule 15), never
	// silent: a 3 KB briefing must not look like an ordinary inline submit.
	if !strings.Contains(out, "the row's briefing is the file's CONTENT") {
		t.Errorf("the confirmation must report the read (Rule 15 — no silent rewrite):\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("the confirmation must name the source it read:\n%s", out)
	}
}

// TestTaskSubmitPayloadFileRefMatchesTheCLIReader pins the "one implementation"
// half: the engine stores EXACTLY what config.ExpandFileRef (the CLI's own
// reader) produces for the same reference — the two faces cannot drift.
func TestTaskSubmitPayloadFileRefMatchesTheCLIReader(t *testing.T) {
	payloadFileRefLedger(t)
	body := "line one\nline \"two\" — with 中文 and $VARS\n"
	path := writePayloadFileRefFixture(t, body)
	want, err := config.ExpandFileRef("@" + path)
	if err != nil {
		t.Fatalf("the CLI reader: %v", err)
	}
	got, _ := submitStoredPayload(t, "tool face and CLI face agree", "@"+path)
	if got != strings.TrimSpace(want) {
		t.Fatalf("the engine's stored payload must be the CLI reader's expansion (through the engine's own TrimSpace):\nwant %q\ngot  %q", strings.TrimSpace(want), got)
	}
}

// TestTaskSubmitRefusesUnreadablePayloadFileRef is the loud-refusal half: a
// reference naming a missing file must FAIL the submit with a message naming
// the path and the escape — never queue a row carrying the spelling.
func TestTaskSubmitRefusesUnreadablePayloadFileRef(t *testing.T) {
	payloadFileRefLedger(t)
	missing := filepath.Join(t.TempDir(), "not-there.md")

	out, taskID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: "unreadable payload reference",
		Payload: "@" + missing,
		Source:  "human",
	})
	if err == nil {
		t.Fatalf("an unreadable @path payload must REFUSE the submit, not store the spelling; got row %q: %s", taskID, out)
	}
	if taskID != "" {
		t.Errorf("a refused submit must not report a row id: %q", taskID)
	}
	for _, want := range []string{missing, "@@", "payload"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q:\n%v", want, err)
		}
	}
}

// TestTaskSubmitPayloadProseAndEscapePassThrough pins the guards that keep the
// ordinary case untouched — this is a regression test on the CLI's own
// documented contract (config/fileref.go), run through the ENGINE:
//
//   - prose that merely STARTS with '@' ("@the operator mentioned…") is not a
//     reference: it is stored verbatim;
//   - the '@@' escape stores the literal text;
//   - a command line that merely CONTAINS '@' is untouched.
func TestTaskSubmitPayloadProseAndEscapePassThrough(t *testing.T) {
	payloadFileRefLedger(t)
	cases := []struct{ name, payload, want string }{
		{"prose with whitespace after the prefix", "@the operator mentioned this in passing", "@the operator mentioned this in passing"},
		{"bare prefix", "@", "@"},
		{"escape hatch", "@@literal-text", "@literal-text"},
		{"at sign inside the text", "ping me at the operator@example.com", "ping me at the operator@example.com"},
		{"ordinary text", "no reference here", "no reference here"},
	}
	for _, c := range cases {
		_, taskID, err := TaskSubmitReturningID(TaskSubmitOptions{
			Summary: "payload pass-through: " + c.name,
			Payload: c.payload,
			Source:  "human",
		})
		if err != nil {
			t.Fatalf("%s: submit must succeed with the value stored verbatim: %v", c.name, err)
		}
		if got := payloadFileRefRowPayload(t, taskID); got != c.want {
			t.Errorf("%s: payload = %q, want %q", c.name, got, c.want)
		}
	}
}
