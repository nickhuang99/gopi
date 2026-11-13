// cli_tasks_child_retry_fixture_test.go — the regression guard for the
// dead-holder FIXTURE defect (2026-09-18).
//
// Four retry tests died at ONE shared precondition: `retrySeedDeadHolder`
// re-measured its own row and got verdict "running" instead of a resumable
// one, so none of them reached the ladder. The fixture's holder was built as
// "one past the highest /proc entry", which is not a RESERVED number — it is
// the very next pid the kernel will hand out — and the row's identity triple
// was written EMPTY, which pushed the classifier past the identity axes onto
// the legacy comm-name guard (`/proc/<pid>/comm` starts with "gopi"). A live,
// gopi-named process wearing that number therefore read as the row's live
// holder.
//
// This test pins the two properties that made the old shape unsound, so a
// future "simplification" back to pid arithmetic + an empty triple fails here
// rather than in four unrelated tests.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

func TestRetryDeadHolderFixtureHoldsAReservedPid(t *testing.T) {
	const taskID = "t-retry-fixture-shape"
	f := newRetryFixture(t, taskID)

	st, err := tools.TaskProcessStatusFor(f.row)
	if err != nil {
		t.Fatalf("read the seeded row: %v", err)
	}
	pid := st.HolderPid
	if pid <= 0 {
		t.Fatalf("the seeded row names no holder pid (claimed_by=%q)", st.Holder)
	}

	// (1) THE PID IS RESERVED TO A CORPSE — both halves, because either alone
	// is satisfiable by the wrong shape. Reaping is what frees a pid, so a
	// child that is killed and never waited for keeps its number: no other
	// process can be given it, and the ledger's liveness probe already reads
	// its state as dead. This is the assertion the old fixture could not
	// satisfy — it named a number the kernel was about to hand to somebody,
	// and the moment a live process wore it the verdict became "running".
	if _, err := os.Stat(filepath.Join(tools.ProcRootPath, strconv.Itoa(pid))); err != nil {
		t.Fatalf("the fixture holder pid %d has no /proc entry (%v) — a REAPED pid is free, so the number is not reserved and the kernel may hand it to a live gopi-named process (the defect)", pid, err)
	}
	if tools.PidAlive(pid) {
		t.Fatalf("the fixture holder pid %d is judged LIVE by tools.PidAlive %v — the pid must be held by an unreaped corpse", pid, tools.PidAlive(pid))
	}

	// (2) THE ROW CARRIES A REAL RECORDED IDENTITY. This half is NOT what makes
	// the verdict `dead`: taskIdentityVerdict answers the liveness axis FIRST
	// (pidAlive → dead) and never reaches the identity ladder for a corpse
	// (internal/tools/taskproc.go). What it protects is the OTHER shape the old
	// fixture had: an EMPTY triple (proc_pid=0 / proc_start_ticks=0 / proc_cmd='')
	// — a row production's own writer (tools.TaskStampHolder) REFUSES to write,
	// because it requires a readable identity. Such a row is a legacy record by
	// construction, and a legacy record whose pid is LIVE falls through to the
	// comm-name guard; that is the road that produced this bug, so the fixture
	// keeps a real triple rather than a shape only a test can produce.
	if !st.IdentityRecorded {
		t.Fatalf("the fixture row records NO identity (verdict %q) — it is the empty-triple/legacy shape production never writes, and the shape whose live pid falls through to the comm-name guard", st.Verdict)
	}
	// The START-TICK axis is the one the classifier consults (proc_start_ticks
	// > 0 makes HasStr true, which selects the identity ladder over both the
	// cmdline axis and the name guard), so that is what the fixture must
	// carry. The CMDLINE is deliberately not asserted: a corpse has no address
	// space left, so /proc/<pid>/cmdline legitimately reads empty for the
	// zombie the fixture kills — requiring it would fail on the very shape
	// that makes the pid hermetic.
	if st.RecordedPid != pid || st.RecordedStart <= 0 {
		t.Fatalf("the fixture row's identity is not a real capture: pid=%d start=%d (want pid %d and a non-zero start tick — the axis the classifier votes on)",
			st.RecordedPid, st.RecordedStart, pid)
	}

	// (3) AND THE PRECONDITION THE FOUR TESTS DEPEND ON: resumable, for the
	// reason just pinned — a dead holder the double-run gate admits.
	ok, verdict := tools.TaskRunChildResumable(f.row, os.Getpid())
	if !ok {
		t.Fatalf("the fixture row must be resumable, got verdict %q", verdict)
	}
	if verdict != tools.TaskProcessStateDead {
		t.Fatalf("the fixture holder must judge DEAD (a corpse holds the pid), got %q", verdict)
	}
}
