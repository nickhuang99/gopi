package main

// report_contract_nested_test.go — the MINIMAL REPRO of the report-clobber
// defect and its fix (epic nested-report-path-clobber, 2026-09-15; Rule 5:
// every fix ships a minimal repro test).
//
// THE DEFECT, as it was observed: GOPI_TASK_REPORT_PATH (taskreport.PathEnv) is
// the report destination of ONE session, but the environment travels down the
// whole tree. A gopi process launched from an agent session's shell inherited
// the PARENT's path — and the parent's own system prompt tells every gopi that
// "with $GOPI_TASK_REPORT_PATH set, the report is written there" — so the child
// wrote ITS report over the parent's task-report.md. Measured: session
// 01a0a4ee's deliverable was replaced by a child probe's "ls" report.
//
// THE REPRO SHAPE (nothing mocked away): a real file stands in for the parent's
// report; a REAL gopi binary built from THIS tree is launched with the inherited
// contract in its environment; its (mock) model does exactly what the real one
// does — write a report to "$GOPI_TASK_REPORT_PATH"; the test then compares the
// parent's file by CONTENT HASH.
//
//	pre-fix (master): the child's report lands on the parent's path — red.
//	post-fix:         the destination is not this session's directory, so the
//	                  guard removes the pair at session start and it is absent
//	                  from the child's shells too; the model's conditional write
//	                  finds no destination and the parent's bytes are untouched.
//
// The model's write is conditional (`[ -n "$..." ] && cat > ...`) so the test
// discriminates by BEHAVIOUR (did the destination reach the child?) rather than
// by a shell error message: with the fix there is simply nothing to write to.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// nestedReportParentBody is the parent's authored report: a complete,
// four-section deliverable. It is the artifact whose survival is the whole
// acceptance criterion.
const nestedReportParentBody = `# Task Report: the parent's deliverable

## Completed
- the parent wrote its own report here

## Files
- (parent's files)

## Verification
- (parent's evidence)

## Not Done
- (nothing)
`

// nestedReportChildBody is what the nested gopi would write if it believed the
// inherited path were its own destination.
// nestedReportParentSession is the parent session's directory name — a session
// uuid (both exporters key a session's directory by one: `task run` by the task
// id, a bare session by its uuid).
const nestedReportParentSession = "01a0a4ee-parent-session"

// nestedReportChildSession is the nested gopi's OWN session directory name. It
// is handed to the child as TaskWrapperEnv — the child's session layout key —
// so the test knows where the child's artifacts belong without guessing the
// session uuid it mints for itself.
const nestedReportChildSession = "01a0a4ee-nested-probe"

// nestedReportChildBody is what the nested gopi would write if it believed the
// inherited path were its own destination.
const nestedReportChildBody = `# Task Report: a nested probe

## Completed
- the child overwrote somebody else's report

## Files
- none

## Verification
- none

## Not Done
- everything
`

// nestedReportWriteCommand renders the mocked model's tool call: the same
// conditional write a real model performs through its bash tool when the prompt
// says a report path is set. It is built from taskreport.PathEnv (never a
// literal, Rule 1b/13) — the command, the harness and the model therefore agree
// on one spelling.
func nestedReportWriteCommand(body string) string {
	return "if [ -n \"$" + taskreport.PathEnv + "\" ]; then cat > \"$" + taskreport.PathEnv + "\" <<'NESTED_REPORT_EOF'\n" +
		body + "NESTED_REPORT_EOF\nfi\n"
}

// hashFile returns the file's sha256 (the byte-identity evidence the acceptance
// asks for) — and fails the test when the file is gone, so "the report still
// exists" is asserted rather than assumed.
func hashFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// TestNestedGopiCannotOverwriteTheParentsReport is the epic's acceptance test: a
// nested gopi inherits a report destination that belongs to ANOTHER session, and
// the parent's report must be byte-identical afterwards.
func TestNestedGopiCannotOverwriteTheParentsReport(t *testing.T) {
	reportSeamBinary(t) // the real binary, built from THIS tree (GOPI_BIN stamped)

	// The parent's session directory — <gopi home>/.gopi/sessions/<uuid>/ — with
	// the report inside it, exactly where both exporters put a session's
	// artifacts.
	parentHome := t.TempDir()
	parentDir := filepath.Join(parentHome, config.ProfileDirName, config.SessionsDirName, nestedReportParentSession)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("prepare the parent's session dir: %v", err)
	}
	parentReport := filepath.Join(parentDir, taskreport.FileName)
	if err := os.WriteFile(parentReport, []byte(nestedReportParentBody), 0o644); err != nil {
		t.Fatalf("seed the parent's report: %v", err)
	}
	before := hashFile(t, parentReport)

	work := t.TempDir()
	// The nested gopi gets its OWN home, so its session directory is somewhere
	// else entirely — the parent's report is unambiguously not its own.
	childHome := t.TempDir()
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_nested_write",
			Name: "bash",
			Args: map[string]interface{}{"command": nestedReportWriteCommand(nestedReportChildBody)},
		}},
		testutil.Resp{Text: "nested probe finished"},
	)
	defer mock.Close()

	env := testutil.MockEnv(t, childHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+filepath.Join(childHome, "memory.db"),
		// The nested gopi's OWN session dir is <childHome>/.gopi/sessions/
		// <wrapper id>/ (the layout the child selects from TaskWrapperEnv), so
		// handing it a wrapper id puts its artifacts somewhere else entirely.
		tools.TaskWrapperEnv+"="+nestedReportChildSession,
		// THE INHERITED CONTRACT — the parent session's report path. This is
		// the harness shape too: no owner marker at all, so ownership can only
		// be decided by the destination's DIRECTORY.
		taskreport.PathEnv+"="+parentReport,
		// …and the run identity this nested gopi inherited from its launcher.
		tools.TaskRunIDEnv+"="+nestedReportParentSession,
	)

	stdout, stderr, code := runGopiMapped(t, []string{"nested report probe"}, "", env, work)
	if code != 0 {
		t.Fatalf("nested gopi run failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	after := hashFile(t, parentReport)
	if after != before {
		t.Fatalf("the nested gopi OVERWROTE the parent's report %s (sha256 %s → %s) — a process must never write to a report path it does not own\nstderr:\n%s",
			parentReport, before, after, stderr)
	}
	body, err := os.ReadFile(parentReport)
	if err != nil || string(body) != nestedReportParentBody {
		t.Fatalf("the parent's report content changed although the hash matched (%v)", err)
	}
	// The refusal must be VISIBLE (Rule 15): an operator reading the child's log
	// has to see that a report contract was inherited and refused, not merely
	// that nothing happened to be written.
	if !strings.Contains(stderr, taskreport.PathEnv) {
		t.Fatalf("the nested gopi refused the inherited report destination SILENTLY — stderr carries no mention of %s:\n%s",
			taskreport.PathEnv, stderr)
	}
}

// TestNestedGopiKeepsItsOwnReportContract is the counterweight to the repro
// above: a session whose OWN directory holds the report must keep the contract —
// that is how every report gets written at all, so the guard must not break it.
//
// It drives the same real binary and the same mocked model, but the report
// destination lies inside the CHILD's own session tree (the harness shape: no
// owner marker), and asserts the child's narrative lands there.
func TestNestedGopiKeepsItsOwnReportContract(t *testing.T) {
	reportSeamBinary(t)

	childHome := t.TempDir()
	work := t.TempDir()
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_own_write",
			Name: "bash",
			Args: map[string]interface{}{"command": nestedReportWriteCommand(nestedReportChildBody)},
		}},
		testutil.Resp{Text: "owner wrote its report"},
	)
	defer mock.Close()

	// THIS session's own directory and report — the shape the harness hands a
	// session (no owner marker; the destination is inside the session's own
	// dir). The child would have created it at startup; creating it here just
	// makes the destination the test asserts on explicit.
	ownDir := filepath.Join(childHome, config.ProfileDirName, config.SessionsDirName, nestedReportChildSession)
	if err := os.MkdirAll(ownDir, 0o755); err != nil {
		t.Fatalf("prepare the child's own session dir: %v", err)
	}
	ownReport := filepath.Join(ownDir, taskreport.FileName)

	env := testutil.MockEnv(t, childHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+filepath.Join(childHome, "memory.db"),
		// The child's own session layout key — this IS its directory.
		tools.TaskWrapperEnv+"="+nestedReportChildSession,
		// …and the destination inside it: the contract it owns.
		taskreport.PathEnv+"="+ownReport,
		tools.TaskRunIDEnv+"="+nestedReportChildSession,
	)

	stdout, stderr, code := runGopiMapped(t, []string{"own report probe"}, "", env, work)
	if code != 0 {
		t.Fatalf("owner run failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	body, err := os.ReadFile(ownReport)
	if err != nil {
		t.Fatalf("the session did not write its report to the destination inside its OWN directory %s — the ownership guard broke the legitimate hand-off: %v\nstderr:\n%s",
			ownReport, err, stderr)
	}
	if !strings.Contains(string(body), "a nested probe") {
		t.Fatalf("report at %s is not the model's own narrative:\n%s", ownReport, body)
	}
}
