package agent

// task_submit_payload_fileref_test.go — the TOOL FACE's end-to-end guard for
// the payload file reference (the defect: `task_submit` stored the "@<abs
// path>" SPELLING while `gopi task submit --payload @FILE` stored the file's
// CONTENT, so three bug rows were born at 88 bytes = len("@"+path) while their
// files held 2677/3406/3742 bytes, each reporting success).
//
// The engine-level regression test lives beside the fix
// (internal/tools/task_payload_fileref_test.go). THIS one drives the surface the
// model actually reaches — executeTool with a tool_use argument map — because
// the defect lived exactly at that seam: the tool handler forwarded the argument
// verbatim to the engine and no parser was ever in the path. It reads the row
// back through TaskNext (what a dispatcher hands the executor), so the assertion
// is about the EXECUTOR's input, not about a helper's return value.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestExecuteTaskSubmitExpandsPayloadFileRef is the tool-face acceptance test:
// payload="@/abs/file" must store the file's content, and a reference that
// cannot be read must ERROR instead of queueing a row.
func TestExecuteTaskSubmitExpandsPayloadFileRef(t *testing.T) {
	// NOT a heavy test: one isolated ledger and one small file, ~0.1s of real
	// work (measured 2.8s including this package's own setup), so it runs in
	// the default suite where a regression is actually caught.
	// Rule 32: the ledger comes from the scaffold — a hand-rolled path here
	// would resolve the operator's append-only ledger and make a fixture row
	// permanent.
	testutil.NewTestDBPath(t)
	ag := &Agent{}

	body := "EVIDENCE (the briefing the executor must receive)\n" + strings.Repeat("measured output line\n", 40)
	path := filepath.Join(t.TempDir(), "bug-evidence.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}

	out := ag.executeTool(deepseek.ToolCall{
		Name: toolnames.ToolTaskSubmit,
		Arguments: map[string]interface{}{
			"summary": "tool face payload reference",
			"payload": "@" + path,
		},
	})
	if strings.Contains(out, "task_submit error") {
		t.Fatalf("the tool must accept a @path payload whose file exists: %s", out)
	}
	taskID := taskIDFromSubmitOut(t, out)

	// Read the row the way a dispatcher does — the claimed row's payload is
	// literally what the executing instance runs with.
	res, err := tools.TaskNext("holder-payload-ref", "")
	if err != nil {
		t.Fatalf("TaskNext: %v", err)
	}
	if res.Empty || res.Task == nil {
		t.Fatalf("the submitted row must be claimable (a payload-less row is not): %s", out)
	}
	if res.Task.TaskID != taskID {
		t.Fatalf("TaskNext claimed %s, want the row just submitted (%s)", res.Task.TaskID, taskID)
	}
	if got, want := res.Task.Payload, strings.TrimSpace(body); got != want {
		t.Fatalf("the executor's briefing must be the file's CONTENT:\nwant %d bytes: %q\ngot  %d bytes: %q\nthe @path SPELLING was stored — len(\"@\"+path) = %d",
			len(want), want, len(got), got, len("@"+path))
	}
	if !strings.Contains(out, "the row's briefing is the file's CONTENT") {
		t.Errorf("the tool result must report the read to the model (Rule 15):\n%s", out)
	}

	// The loud-refusal half: a missing file must make the tool return an error
	// the model sees, never a queued row carrying the spelling.
	missing := filepath.Join(t.TempDir(), "not-there.md")
	refused := ag.executeTool(deepseek.ToolCall{
		Name: toolnames.ToolTaskSubmit,
		Arguments: map[string]interface{}{
			"summary": "tool face unreadable payload reference",
			"payload": "@" + missing,
		},
	})
	if !strings.Contains(refused, "task_submit error") {
		t.Fatalf("an unreadable @path must be REFUSED through the tool face, got:\n%s", refused)
	}
	if strings.Contains(refused, "Task queued:") {
		t.Fatalf("a refused submit must not queue a row:\n%s", refused)
	}
	if !strings.Contains(refused, missing) {
		t.Errorf("the refusal must name the path it could not read:\n%s", refused)
	}
}
