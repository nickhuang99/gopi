package main

// borrowed_fd_worker_child_test.go — the REAL-CHILD drive of the worker-lock
// borrowed-fd site.
//
// WHY A CHILD AND NOT AN IN-PROCESS CALL: the production shape is a daemon child
// that inherited the parent's lock file on the number os/exec renumbers to
// (daemonControlChildFD). A Go TEST process cannot reproduce that in-process — its
// fd 3 is already the harness's — and the fix's own explicit-fd seam
// (workerDaemonInheritLockAt) does not exist on the base commit, so an in-process
// guard could never be shown RED there. A re-exec'd child receives the descriptor
// through ExtraFiles, exactly as production does, and therefore drives the
// PRODUCTION entry point (workerDaemonInheritLock) on BOTH the base and the fixed
// tree. The child is also the honest observer: the descriptor's survival is a fact
// about the child's descriptor table.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/borrowedfd"
	"github.com/nick/gopi/internal/heavytest"
)

const (
	// bfdWorkerChildEnv marks this guard's re-exec'd child.
	bfdWorkerChildEnv = "GOPI_TEST_BORROWED_FD_WORKER_CHILD"
	// bfdWorkerChildProbe is a -test.run pattern matching no test.
	bfdWorkerChildProbe = "^$"
	// bfdWorkerChildReport is the prefix of the child's one report line.
	bfdWorkerChildReport = "BFDWORKER "
)

// RunBorrowedFDWorkerChild dispatches this guard's re-exec'd child. It belongs
// early in TestMain: the child reports and exits, never running a test.
func RunBorrowedFDWorkerChild() bool {
	if os.Getenv(bfdWorkerChildEnv) == "" {
		return false
	}
	runBorrowedFDWorkerChild()
	return true
}

// runBorrowedFDWorkerChild takes the inherited lock through the PRODUCTION entry
// point, settles finalizers, and reports whether the descriptor survived.
//
// The lock is deliberately contended by a file lock held in ANOTHER process (the
// parent starts it), so the call takes the refusal exit — the exit an ordinary
// contended start takes, and the exit whose wrapper nothing else references.
func runBorrowedFDWorkerChild() {
	lock, err := workerDaemonInheritLock()
	// SettleFinalizers is the same bounded window the in-process guards use
	// (internal/borrowedfd): it gives runtime.runfinq the chance to run the
	// finalizer a dropped wrapper would otherwise carry.
	borrowedfd.SettleFinalizers()
	// The observation is the KERNEL's: F_GETFD on the inherited number.
	alive := bfdFDAlive(daemonControlChildFD) == nil
	fmt.Printf("%slock=%v refused=%v alive=%v\n", bfdWorkerChildReport, lock != nil, err != nil, alive)
}

// TestWorkerDaemonInheritLockRefusalInARealChildLeavesTheLockFDAlive guards the
// worker-lock site's refusal exit, driven through the production entry point in a
// real child (see the file header for why it must be a child).
//
// The fixture is the production contract: the descriptor is one this process did
// NOT open (a dup of the parent's lock file — the role os/exec's ExtraFiles dup
// plays), and it is genuinely held by another PROCESS, because flock lives on the
// open file description and a dup within one process would simply re-take it. The
// refusal therefore comes from workerDaemonInheritLock's bounded-wait exit.
//
// RED on the base commit (no detach): the child's inherited descriptor is closed
// by the dropped wrapper's finalizer. GREEN with borrowedfd.Detach — production
// damage for this shape is the parent's live lock file being closed and its number
// recycled onto an unrelated handle (the measured cmd/agent EBADF flake).
func TestWorkerDaemonInheritLockRefusalInARealChildLeavesTheLockFDAlive(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate this test binary: %v", err)
	}
	lockPath := filepath.Join(t.TempDir(), workerLockFilename)
	owner, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("create the lock fixture: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown
	bfdStartFlockHolder(t, lockPath)

	child := exec.Command(self, "-test.run", bfdWorkerChildProbe)
	child.Env = append(os.Environ(), bfdWorkerChildEnv+"=1")
	// ExtraFiles[0] is renumbered to daemonControlChildFD in the child — the very
	// contract workerDaemonInheritLock documents.
	child.ExtraFiles = []*os.File{owner}
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the child failed (%v):\n%s", err, out)
	}
	line := bfdReportLine(string(out), bfdWorkerChildReport)
	if line == "" {
		t.Fatalf("the child reported nothing:\n%s", out)
	}
	if !strings.Contains(line, "refused=true") {
		t.Fatalf("the child did not take the refusal exit (%q) — this guard needs the contended path, held by the external flock holder in the parent", line)
	}
	if strings.Contains(line, "alive=false") {
		t.Fatalf("the child's inherited lock fd %d was CLOSED under its owner by the refused adoption (%q) — "+
			"the wrapper this process BORROWED (the parent's lock, dup'd in by os/exec) died with the refusal and its finalizer "+
			"closed the number; the kernel then recycles it onto an unrelated live handle, which is the measured cmd/agent EBADF flake. "+
			"The refusal exits of workerDaemonInheritLock must call borrowedfd.Detach", daemonControlChildFD, line)
	}
}

// bfdReportLine returns the child's report line carrying prefix, trimmed, or "".
func bfdReportLine(out, prefix string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			return strings.TrimSpace(l)
		}
	}
	return ""
}
