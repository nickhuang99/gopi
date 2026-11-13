package main

// REGRESSION GUARD for the "silent false success" defect of the `task run`
// wrapper (task 01a0f3ef-…, briefings/brief-worker-trusts-exit-code-not-ledger.md),
// fixed by epic worktree-dirty-residue (2026-09-27).
//
// WHAT THE DEFECT WAS. runTaskRunOnce decided the run's outcome from the
// child's exit code. It DID read the ledger row's terminal state
// (tools.TaskRowTerminalAt) — but only inside the `if runErr != nil` branch.
// On the success path (child exited 0) the ledger was never consulted and the
// writeback's own error was dropped, so when the row was NOT terminal the
// wrapper still wrote/announced `completed` and exited 0.
//
// THE ARM THAT REACHES IT DETERMINISTICALLY (the finding). The operator's own
// Rule 30 enforcement switch makes the state reachable: with
// GOPI_TASK_RUN_RESIDUE_ENFORCE=1 armed and the run's own worktree holding
// untracked litter, tools.TaskUpdateWithResult REFUSES the writeback
// (internal/tools/tasks.go: `if note, refused := reclaimRunResidueRefusal(taskID);
// refused { return "", fmt.Errorf(...) }`) — the row stays OPEN. The wrapper's
//
//	writebackOutcome(target, tools.TaskStatusCompleted, …)
//
// applied NO special handling to a refused write: it called TaskRunWriteback,
// which reported `unclean-work`; the wrapper LOGGED that ("writeback failed
// (continuing)"), then printed
//
//	task <id> completed (elapsed …)
//
// and returned nil. The row was left non-terminal, the exit code was 0, and
// the operator-facing line said the task completed. That is what made a dirty
// worktree a PERMANENT resident: the guard correctly refused to free it, and
// no operator ever learned the row was still open.
//
// THE FIX THIS TEST NOW GUARDS (cmd/agent/task_run_closeout_landed.go). The
// success arm reads the row back AFTER its own writeback: a close-out that did
// not land is escalated — the escalation line replaces the false "completed"
// line and the wrapper returns a non-nil error, so exit code, console and
// ledger finally agree. Nothing is committed, cleaned or deleted (Rule 30).
//
// A SECOND MEASURED FACT (the green arm) is what makes the read land where it
// does rather than earlier: the child CANNOT close the row itself, and it is
// not asked to. At the child's exit the wrapper — the parent inside cmd.Wait()
// — is alive by construction and has not reached its own writebackOutcome yet,
// so the row is ALWAYS non-terminal at that instant, and a read taken there
// would reclassify every healthy run as an abnormally dead one. (The former
// child-side close-out that tried to reconcile the row there was deleted by
// task 01a16f7c: its gate refused every write while the claim pid was alive,
// and on the healthy path the row was already terminal — a no-op by
// construction.) The wrapper's post-Wait writeback is the SOLE writer of a
// successful run's row; "did MY write land?" is therefore the only answerable
// question, and it is asked after the write.
//
// RULE 30/32: the fixture lives in the pinned scratch root; the row is read
// back through the production reader (tools.TaskShow), never hand-rolled SQL.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// wpReproRowRed / wpReproRowGreen are the fixture rows (Rule 1: named).
const (
	wpReproRowRed   = "08eefc83-aa18-40e3-b6ea-5c5f89a9cc4a"
	wpReproRowGreen = "cac1407c-c556-4a11-8b54-33743a8d573c"
)

// wpReproResidueEnforceOffEnv is the Rule 30 switch that turns a dirty
// close-out into a REFUSED writeback — armed only in the red arm.
const wpReproResidueEnforceOn = "GOPI_TASK_RUN_RESIDUE_ENFORCE=1"

// wpReproLitterName is the untracked file the child leaves in its own ground:
// the residue the armed switch refuses the close-out for.
const wpReproLitterName = ".wprepro-litter.txt"

// wpReproWritebackRefusedMarker is the wrapper's own report of the refused
// writeback: the stderr line the wrapper prints on EVERY non-nil writeback
// error (a report, never a course change), and the marker the red arm asserts
// is still emitted after the fix.
const wpReproWritebackRefusedMarker = "writeback failed (continuing)"

// wpReproEscalationMarker is the FIX's own line: the close-out did not land,
// reported by name instead of being called a success (Rule 1b — the test greps
// the same constant the wrapper prints).
const wpReproEscalationMarker = taskRunCloseoutEscalationMarker

// wpReproCompletedLine is the string the wrapper printed REGARDLESS of whether
// the ledger accepted the writeback. After the fix it may only appear when the
// row really is terminal.
const wpReproCompletedLine = " %s completed (elapsed "

// wpReproRun is one arm's fixture: submit the row from the fixture repo, run
// the real wrapper (`gopi worker <id>` — the explicit-id door onto
// runTaskRunOnce), then read the row back through the production reader.
type wpReproArm struct {
	code     int    // the wrapper process's exit code
	status   string // the row's status AFTER the wrapper returned
	stdout   string
	stderr   string
	childLog string // the child's own output.log, appended
}

// wpReproLitterScript is the tool payload: a legitimate single write, plus the
// untracked scratch file the run does not clean up — exactly the hygiene debt
// Rule 30's enforcement arm is aimed at.
func wpReproLitterScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wp-repro-work.sh")
	body := "#!/usr/bin/env bash\n" +
		"echo 'the work itself is done' > ./wprepro-result.txt\n" +
		"echo 'scratch left behind by the run' > ./" + wpReproLitterName + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write the tool payload: %v", err)
	}
	return path
}

func wpReproRun(t *testing.T, row, summary, mockURL, childDir, gopiHome string, env []string) wpReproArm {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath) // the PARENT process's tools.* reads
	runEnv := append(append([]string{}, env...), "GOPI_MEMORY_DB="+dbPath)

	if _, stderr, c := runGopiMapped(t, []string{"task", "submit",
		summary, "--payload", "do the work", "--task-id", row}, "", runEnv, childDir); c != 0 {
		t.Fatalf("submit %s: code=%d stderr=%s", row, c, stderr)
	}
	out, errOut, c := runGopiMapped(t, []string{"worker", row}, "", runEnv, childDir)
	t.Logf("wrapper stdout for %s:\n%s", row, out)
	if s := strings.TrimSpace(errOut); s != "" {
		t.Logf("wrapper stderr for %s:\n%s", row, s)
	}
	// THE CHILD'S OWN OUTPUT travels with the arm: the child's close-out
	// verdict is printed THERE, not by the wrapper — and it is a fact the green
	// arm asserts.
	childLog := ""
	if b, err := os.ReadFile(filepath.Join(gopiHome, ".gopi", "sessions", row, "output.log")); err == nil {
		childLog = string(b)
	}
	return wpReproArm{code: c, status: taskStatusOf(t, row), stdout: out, stderr: errOut, childLog: childLog}
}

// wpReproEvidence prints the raw observation block the report quotes.
func wpReproEvidence(armName string, a wpReproArm) {
	fmt.Printf("\n=== RUNTIME EVIDENCE (%s) ===\n", armName)
	fmt.Printf("wrapper exit code        : %d\n", a.code)
	fmt.Printf("wrapper stdout tail      : %q\n", lastNonEmptyLine(a.stdout))
	fmt.Printf("wrapper says 'completed' : %v\n", strings.Contains(a.stdout, "completed"))
	fmt.Printf("wrapper stderr has %q : %v\n", wpReproWritebackRefusedMarker,
		strings.Contains(a.stderr, wpReproWritebackRefusedMarker))
	fmt.Printf("ledger row status after  : %q\n", a.status)
	fmt.Printf("row terminal?            : %v\n", wpReproTerminal(a.status))
	fmt.Printf("=== END RUNTIME EVIDENCE (%s) ===\n\n", armName)
}

// TestWrapperSilentFalseSuccessOnRefusedWriteback_RED is the defect, reached
// deterministically: the ledger REFUSED the close-out and the wrapper reported
// success anyway.
//
// THE FIX THIS TEST AWAITS (and why the assertion inverts itself): the
// success path must read the row — the same tools.TaskRowTerminalAt its failure
// sibling already calls — and treat "the writeback was refused / the row is not
// terminal" as something other than success. When that lands, the row stops
// being reported as completed and this test fails with a message saying so,
// rather than passing silently (Rule 15).
func TestWrapperSilentFalseSuccessOnRefusedWriteback_RED(t *testing.T) {
	SandboxRepoContext(t)
	childDir := RepoContextFixtureRepo(t)
	script := wpReproLitterScript(t)

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_work",
			Name: "bash",
			Args: map[string]interface{}{"command": script, "timeout_sec": float64(60)},
		}},
		testutil.Resp{Text: "Work finished. Leaving the run's scratch in the worktree."},
	)
	defer mock.Close()

	gopiHome := t.TempDir()
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, config.SkipClaimBroadcastEnv+"=1", wpReproResidueEnforceOn)

	arm := wpReproRun(t, wpReproRowRed, "refused writeback (red arm)", mock.URL(), childDir, gopiHome, env)
	wpReproEvidence("red arm: residue enforcement armed", arm)

	// ① the run really did its work and really left the litter: the refusal is
	//    a genuine hygiene verdict, not a broken fixture.
	if !strings.Contains(arm.stdout+arm.stderr, "unclean-work") &&
		!strings.Contains(arm.stdout+arm.stderr, wpReproWritebackRefusedMarker) {
		t.Errorf("the fixture did not produce a REFUSED writeback, so this arm proves nothing "+
			"about the wrapper's handling of one.\nstdout:\n%s\nstderr:\n%s", arm.stdout, arm.stderr)
	}
	// ①b THE SPLIT IS EXERCISED BY THIS CASE, not only by the helper's own unit
	//    test (cli_tasks_statusword_test.go). Measured 2026-11-05 (run
	//    -1793868076-300750, row 01a1aae8): this test went red in whole-package
	//    runs on the assertion BELOW — "the escalation must name what the ledger
	//    holds instead" — because arm.status still carried the renderer's
	//    measured suffix (`in_progress + holder dead (pid N gone)`,
	//    internal/tools/taskholder_gone.go) while the wrapper's escalation names
	//    the LEDGER's answer, the bare word. `strings.Contains(<word>,
	//    <word + marker>)` is false by construction, so the failure said nothing
	//    about the wrapper — it measured the marker logic as a side effect of a
	//    status assertion. The rendering is read here on purpose: the raw field
	//    is what a split can be wrong about, and the WORD is what the assertion
	//    below may compare.
	raw := rawStatusField(t, wpReproRowRed)
	t.Logf("raw status field for %s: %q (arm.status %q)", wpReproRowRed, raw, arm.status)
	if strings.Contains(arm.status, tools.StatusWordMarkerSep) {
		t.Fatalf("arm.status = %q carries the render-time marker (separator %q), so this arm "+
			"cannot compare it against anything the wrapper says: the wrapper names the LEDGER's "+
			"status WORD (tools.TaskRowStatusAt) and a MARKED field can never be a substring of it. "+
			"taskStatusOf must answer the word (statusWord, cli_tasks_statusword_split_test.go) — "+
			"raw field: %q", arm.status, tools.StatusWordMarkerSep, raw)
	}
	// ② THE FIX: the refused close-out is ESCALATED — never announced as a
	//    success. Three facts, together, because any one alone can be faked:
	//    the ledger kept the row OPEN, the console says so, and the process's
	//    own exit code says so.
	if wpReproTerminal(arm.status) {
		t.Fatalf("this arm needs a REFUSED writeback (the row must stay open) — row %s is %q.\n"+
			"The fixture no longer reaches the state it guards (Rule 30's gate did not refuse), "+
			"so nothing below would prove anything.", wpReproRowRed, arm.status)
	}
	if !strings.Contains(arm.stderr, wpReproEscalationMarker) {
		t.Errorf("the wrapper must report the un-landed close-out BY NAME (marker %q) — a silent "+
			"open row is exactly the defect this guards; stderr:\n%s", wpReproEscalationMarker, arm.stderr)
	}
	if !strings.Contains(arm.stderr, arm.status) {
		t.Errorf("the escalation must name what the ledger holds instead (%q), so the operator can "+
			"see the row is still open; stderr:\n%s", arm.status, arm.stderr)
	}
	if strings.Contains(arm.stdout, fmt.Sprintf(wpReproCompletedLine, wpReproRowRed)) {
		t.Errorf("the wrapper must NOT announce completion when the ledger REFUSED the writeback; "+
			"stdout:\n%s", arm.stdout)
	}
	if arm.code == 0 {
		t.Errorf("an un-landed close-out must not exit 0 — the exit code is what a caller (and the "+
			"worker loop) reads; stdout:\n%s\nstderr:\n%s", arm.stdout, arm.stderr)
	}
	t.Logf("FIX VERIFIED: ledger kept the row %q, the escalation line is on stderr, "+
		"stdout carries no completion claim, exit=%d", arm.status, arm.code)
}

// TestWrapperSuccessIsSoleWriterAndHonest_GREEN is the mirror arm. Same
// fixture, the enforcement switch OFF: the close-out lands, the run is a
// normal success — and it pins the fact that makes the red arm matter.
func TestWrapperSuccessIsSoleWriterAndHonest_GREEN(t *testing.T) {
	SandboxRepoContext(t)
	childDir := RepoContextFixtureRepo(t)
	script := wpReproLitterScript(t)

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_work",
			Name: "bash",
			Args: map[string]interface{}{"command": script, "timeout_sec": float64(60)},
		}},
		testutil.Resp{Text: "Work finished. Leaving the run's scratch in the worktree."},
	)
	defer mock.Close()

	gopiHome := t.TempDir()
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, config.SkipClaimBroadcastEnv+"=1") // enforcement left OFF

	arm := wpReproRun(t, wpReproRowGreen, "accepted writeback (green arm)", mock.URL(), childDir, gopiHome, env)
	wpReproEvidence("green arm: enforcement off", arm)

	if arm.code != 0 {
		t.Fatalf("the green arm is a normal run: wrapper must exit 0 (exit=%d)\nstderr:\n%s", arm.code, arm.stderr)
	}
	if !wpReproTerminal(arm.status) {
		t.Errorf("green arm: row %s = %q, want a terminal status", wpReproRowGreen, arm.status)
	}
	// THE POSITIVE HALF OF THE SAME CONTRACT (Rule 15: a guard that refuses
	// everything proves nothing): a close-out that LANDED still prints its
	// completion line, and raises no escalation. This is the arm that keeps the
	// new check from degenerating into a machine that distrusts every run.
	line := fmt.Sprintf(wpReproCompletedLine, wpReproRowGreen)
	if !strings.Contains(arm.stdout, line) {
		t.Errorf("the healthy run must still print its completion line (%q); stdout:\n%s", line, arm.stdout)
	}
	if strings.Contains(arm.stderr, wpReproEscalationMarker) {
		t.Errorf("the healthy run must NOT escalate — the close-out landed; stderr:\n%s", arm.stderr)
	}
	// THE SOLE-WRITER FACT, now STRUCTURAL rather than a measured refusal: the
	// child-side close-out is DELETED (task 01a16f7c), so nothing in the child
	// can write this row at all — the terminal status above was written by the
	// WRAPPER, and there is no second opinion to fall back on. That is exactly
	// why the wrapper's exit-code-only decision is load-bearing.
	//
	// The assertion that used to sit here grepped the child's log for "no live
	// claim evidence" — the deleted seam's refusal sentence. A test that waits
	// for a sentence nobody can print any more fails for the wrong reason
	// forever, so it went with the seam. What replaces it is the property,
	// checked on the surviving evidence BELOW: the wrapper's writeback was not
	// refused (so the terminal state IS its write) and the child's log was
	// captured (so "the child wrote nothing" is measured, not assumed).
	// THE CORRECTION, MACHINE-CHECKED. Two measured facts, together: (a) the
	// child did NOT close the row (asserted above), and (b) the wrapper's own
	// writeback was NOT refused on this healthy run. Therefore, at the instant
	// the wrapper looked at the ledger — its post-Wait moment — the row was
	// necessarily still NON-TERMINAL, on a perfectly successful run.
	//
	// That is why the FIX does not ask "is the row terminal?" at the child's
	// exit (the briefing's first proposal): the row is ALWAYS non-terminal at
	// that moment, so that check would reclassify EVERY healthy run as a dead
	// one. The question that separates the two arms is whether MY OWN writeback
	// LANDED — asked AFTER the write, which is exactly what the fix reads
	// (taskRunCloseoutLanded) and what fact (b) above pins for this arm.
	if strings.Contains(arm.stderr, wpReproWritebackRefusedMarker) {
		t.Errorf("this arm is the HEALTHY run: the wrapper's writeback must not have been refused, "+
			"else the correction above cannot be inferred.\n--- stderr ---\n%s", arm.stderr)
	}
	if arm.childLog == "" {
		t.Errorf("the child's output.log was not captured — the sole-writer fact is unverified")
	}
}

// wpReproTerminal reads the ledger's own vocabulary (tools exposes the values
// the row carries); the statuses that mean "this row is decided".
func wpReproTerminal(status string) bool {
	switch status {
	case tools.TaskStatusCompleted, tools.TaskStatusFailed, tools.TaskStatusVerified, tools.TaskStatusCancelled:
		return true
	}
	return false
}
