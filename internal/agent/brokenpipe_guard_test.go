package agent

// brokenpipe_guard_test.go — the regression arm for bug row 01a1950b.
//
// THE DEFECT, in one line: a run that mints its own `in_progress` ledger row
// died of SIGPIPE when its launcher walked away, so the row it had just
// promised to close was never closed — and nothing collects it afterwards
// (automatic disposal was removed by the operator's 2026-10-26 ruling,
// devdoc/zombie-sweep-removed.md).
//
// THE CHANNEL IS fd 1/2, AND THAT IS MEASURED, NOT ASSUMED. Go's runtime
// honours SIGPIPE ONLY for a write to stdout/stderr; a write to any other
// closed pipe just returns EPIPE. Measured on this host (2026-11-01), two
// separate probes, because the first one — a `syscall.Kill(os.Getpid(),
// SIGPIPE)` and a write to a self-made pipe — proved nothing: both SURVIVED on
// the pre-fix tree, so an arm built on them would have been green before the
// fix and green after it.
//
//	$ ./probe default   # child's fd 1 IS a pipe whose reader has closed
//	mode=default err=signal: broken pipe        ← the process is KILLED
//	mode=ignored err=<nil>                      ← signal.Ignore(SIGPIPE)
//
// So this arm hands the child a stdout pipe, closes the reader, and lets the
// child WRITE TO ITS OWN fd 1 — the exact shape of `gopi chats 2>&1 | head -12`,
// which is the launcher that produced the measured row.
//
//	pre-fix  : the default disposition kills the child (exit status 141,
//	           "signal: broken pipe"); no survival marker, row left open.
//	post-fix : SIGPIPE is ignored for the row owner; the child survives the
//	           EPIPE, closes its row through the ordinary end hook, and exits 0.
//
// RULE 42: the child is OURS — started here, waited for here, and its stdout
// pipe is one this test created. No pid, group or pattern is ever addressed.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/tools"
)

const (
	// brokenPipeChildEnv marks the re-exec'd child. Without it the helper test
	// skips, so an ordinary package run spawns nothing.
	brokenPipeChildEnv = "GOPI_AGENT_BROKENPIPE_CHILD"
	// brokenPipeSurvivedEnv is the file the child writes ONLY if it is still
	// alive after the closed-pipe write — the marker whose absence is the
	// pre-fix reading.
	brokenPipeSurvivedEnv = "GOPI_AGENT_BROKENPIPE_SURVIVED_FILE"
	// brokenPipeRowEnv is where the child publishes "<rowID> <status>" from
	// ITS OWN ledger, so the parent judges the row without resolving a database
	// path the child used (Rule 32).
	brokenPipeRowEnv = "GOPI_AGENT_BROKENPIPE_ROW_FILE"
	// brokenPipeChildTestName is the helper test the parent re-execs.
	brokenPipeChildTestName = "TestBrokenPipeChildRegistersARowThenWritesToAClosedPipe"
	// brokenPipeChildWait bounds the parent's wait for the child's close-out.
	brokenPipeChildWait = 60 * time.Second
	// brokenPipeFloodBytes is what the child writes to its own fd 1. One write
	// is enough — the reader is already gone — and the size makes the write go
	// straight through to the syscall instead of sitting in a buffer.
	brokenPipeFloodBytes = 1 << 20
)

// TestRowOwnerSurvivesAClosedStdoutAndClosesItsRow is the arm.
func TestRowOwnerSurvivesAClosedStdoutAndClosesItsRow(t *testing.T) {
	if os.Getenv(brokenPipeChildEnv) == "1" {
		t.Skip("helper test: driven by the parent arm only")
	}

	dir := t.TempDir()
	survivedPath := dir + "/survived.txt"
	rowPath := dir + "/row.txt"

	// The pipe whose reader this test then closes — the launcher that walked
	// away. The child's fd 1 becomes the write end.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("make the stdout pipe: %v", err)
	}

	child := exec.Command(os.Args[0], "-test.run", "^"+brokenPipeChildTestName+"$")
	child.Env = append(os.Environ(),
		brokenPipeChildEnv+"=1",
		brokenPipeSurvivedEnv+"="+survivedPath,
		brokenPipeRowEnv+"="+rowPath,
		"GOPI_HOME="+dir+"/home",
		// A wrapper child never reaches the minting arm, so an inherited value
		// would silently turn this test into a no-op.
		tools.TaskWrapperEnv+"=",
	)
	child.Stdout = pw
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("start the broken-pipe child: %v", err)
	}
	// THE LAUNCHER WALKS AWAY: the write end is closed in the parent (the child
	// holds its own dup) and then the read end goes, so the child's next write
	// to fd 1 has no reader.
	//stylegate:ignore the parent's own dup of the write end; the child inherited its own
	_ = pw.Close()
	//stylegate:ignore the read end is what this arm removes on purpose — the child's fd 1 now has no reader
	_ = pr.Close()
	t.Cleanup(func() {
		_ = child.Process.Kill() //stylegate:ignore teardown of the process THIS test started; the verdict is read from the exit status and the ledger, not from a graceful stop
		_, _ = child.Process.Wait()
	})

	waitDone := make(chan error, 1)
	go func() { waitDone <- child.Wait() }()
	var exitErr error
	select {
	case exitErr = <-waitDone:
	case <-time.After(brokenPipeChildWait):
		t.Fatalf("the child did not exit within %s", brokenPipeChildWait)
	}

	// ASSERTION 1 — the process survived the closed-pipe write. A
	// default-disposition death reports "signal: broken pipe" (status 141).
	if exitErr != nil {
		t.Fatalf("the row-owning child did not survive a vanished launcher (exit: %v) — a run that owns an in_progress row must outlive its reader, or that row is never closed", exitErr)
	}
	if _, err := os.Stat(survivedPath); err != nil {
		t.Fatalf("the child exited 0 but wrote no survival marker (%s): the arm cannot tell survival from a skipped body", survivedPath)
	}

	// ASSERTION 2 — the row reached a TERMINAL status. Surviving is only worth
	// anything if the close-out it survived for ran.
	raw, err := os.ReadFile(rowPath)
	if err != nil {
		t.Fatalf("read the verdict the child published: %v", err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		t.Fatalf("the child's published record %q is not `<rowID> <status>`", string(raw))
	}
	if fields[1] != tools.TaskStatusCompleted {
		t.Fatalf("row %s = %q after the child's ordinary end hook, want %q — surviving the signal is only half the duty; the row must reach a terminal state",
			fields[0], fields[1], tools.TaskStatusCompleted)
	}
}

// TestBrokenPipeChildRegistersARowThenWritesToAClosedPipe is the child body.
func TestBrokenPipeChildRegistersARowThenWritesToAClosedPipe(t *testing.T) {
	if os.Getenv(brokenPipeChildEnv) != "1" {
		t.Skip("helper test: re-exec'd by TestRowOwnerSurvivesAClosedStdoutAndClosesItsRow")
	}
	survivedPath := os.Getenv(brokenPipeSurvivedEnv)
	rowPath := os.Getenv(brokenPipeRowEnv)
	if survivedPath == "" || rowPath == "" {
		t.Fatalf("the child was started without %s / %s", brokenPipeSurvivedEnv, brokenPipeRowEnv)
	}

	a := newAutoTaskAgent(t)
	// THE PRODUCTION START HOOK — not a hand-built ledger insert, so the arm
	// exercises whatever that hook arms (this is the fix's own call site).
	a.maybeRegisterSessionTask("a row-owning run must survive a vanished launcher")
	if !a.autoTaskRegistered {
		t.Fatal("premise: the start hook did not mint a row, so there is no close-out obligation to protect")
	}
	rowID := a.autoTaskRowID

	// THE DEATH MODE, on the channel the runtime honours: a write to this
	// process's own fd 1, whose reader the parent has already closed. The error
	// is EPIPE post-fix (harmless, and the run outlives it); pre-fix the process
	// does not return from this call at all.
	if _, werr := os.Stdout.Write(make([]byte, brokenPipeFloodBytes)); werr != nil {
		//stylegate:ignore EPIPE from a vanished reader is the ORDINARY outcome post-fix — the point of the arm is that the process reaches the next line
		_ = werr
	}

	// Reaching this line IS the assertion. Pre-fix the process is already gone.
	if err := os.WriteFile(survivedPath, []byte("survived\n"), 0o600); err != nil {
		t.Fatalf("write the survival marker: %v", err)
	}

	// ... and the duty the survival exists for: the ordinary end hook.
	a.finishSessionTask(0, 1, 0, 1.0)

	// PUBLISH THE VERDICT from THIS process's ledger (Rule 32: the database came
	// from the shared fixture, so this side is the only one that resolves it).
	status, err := tools.TaskStatusOf(rowID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brokenpipe child: TaskStatusOf(%s): %v\n", rowID, err)
		return
	}
	if err := os.WriteFile(rowPath, []byte(rowID+" "+status), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "brokenpipe child: publish the verdict: %v\n", err)
	}
}
