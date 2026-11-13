package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestForeignHolderIsUnknownOnBothFaces pins the HOST DIMENSION of the holder
// verdict, and it pins it on BOTH faces that answer the question: the status
// view (`task status`, taskHolderVerdictFor) and the row-wise liveness ladder
// (`task show`'s live word, taskRowProcessLiveness).
//
// WHY A HOST IS NEEDED AT ALL. A claim stamp is "<host>:<pid>", and a pid is a
// number in ONE kernel's namespace. Before this arm, a row claimed on another
// machine was judged by THIS machine's /proc: if pid N happened to be alive
// here the row read `running` (a false double-run refusal, an operator told to
// wait for a process that is not theirs), and if N was free the row read `dead`
// — the verdict that AUTHORISES A RESUME. Resuming a row another host is
// executing is the cross-machine double run: two agents, two workspaces, one
// task id, and no convergence afterwards.
//
// THE ANSWERS THIS PINS:
//   - verdict is UNKNOWN on both faces (never "running", never "dead");
//   - resumable is NO (unknown is not permission);
//   - the evidence line NAMES the foreign host and the command that answers it.
func TestForeignHolderIsUnknownOnBothFaces(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-ident-crosshost"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	now := float64(1234567890)
	// A pid that is certainly alive HERE, deliberately: the defect was that this
	// host's liveness decided a foreign row. Using our own pid makes the test
	// fail on the OLD code in the loudest possible way ("running" for a process
	// that has nothing to do with the row).
	const stamp = "some-other-host.example:4242"
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ? WHERE task_id = ?`,
		taskStatusInProgress, stamp, now, now, taskID); err != nil {
		t.Fatalf("forge: %v", err)
	}

	// FACE 1 — the status view.
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Verdict != TaskProcessStateUnknown {
		t.Errorf("status view verdict = %q, want %q — a foreign pid must not be read through this host's /proc", st.Verdict, TaskProcessStateUnknown)
	}
	if !st.Foreign {
		t.Errorf("holder_foreign = false on a stamp minted elsewhere — the JSON consumer cannot see the reason")
	}
	if st.Resumable {
		t.Errorf("resumable = true on a foreign holder — resuming here would run the same task on two machines")
	}
	for _, want := range []string{"some-other-host.example", "gopi task diff --peer"} {
		if !containsFold(st.Note, want) {
			t.Errorf("evidence line does not name %q:\\n%s", want, st.Note)
		}
	}

	// FACE 2 — the row-wise ladder that `task show` renders. It must give the
	// SAME verdict, or the two faces disagree about one row.
	t2, err := taskByID(ctx, db, taskID)
	if err != nil || t2 == nil {
		t.Fatalf("re-read %s: %v", taskID, err)
	}
	if got := taskRowProcessLiveness(t2).State; got != TaskProcessStateUnknown {
		t.Errorf("liveness ladder = %q, want %q — one holder, one verdict (Rule 14)", got, TaskProcessStateUnknown)
	}
}

// TestLocalStampStillJudgedNormally is the counter-arm: the host condition must
// not swallow the ordinary case. A stamp minted HERE, naming a pid that is dead,
// still reads dead — and dead is what admits the resume of a crashed run.
func TestLocalStampStillJudgedNormally(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-ident-local-dead"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	now := float64(1234567890)
	// A pid that cannot be alive: above pid_max on Linux.
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ? WHERE task_id = ?`,
		taskStatusInProgress, claimHolderOfPid(4194000), now, now, taskID); err != nil {
		t.Fatalf("forge: %v", err)
	}
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Verdict == TaskProcessStateUnknown {
		t.Fatalf("a LOCAL stamp whose pid is gone read %q — the host arm is swallowing the ordinary case", st.Verdict)
	}
	if st.Foreign {
		t.Fatalf("holder_foreign = true on a stamp this host minted")
	}
	_ = os.Getpid()
}

// containsFold is a tiny case-insensitive substring check (the note's own
// capitalisation is the renderer's business, not this assertion's).
func containsFold(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	ls, lsub := lowerASCII(s), lowerASCII(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return i
		}
	}
	return -1
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
