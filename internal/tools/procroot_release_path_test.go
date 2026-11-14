// The regression guard for the aliasing defect of 2026-09-15.
//
// `tools.ProcessRootPath` spelled TWO different facts at once: the procfs mount
// ("/proc", what every procfs reader in this package and cmd/agent resolves
// under) and the cwd-release destination ("/", where a producer re-parents
// itself after its worktree is removed). When the release work consolidated the
// constant into internal/worktreerelease and aliased this name to the RELEASE
// value, every procfs read silently started resolving under "/" — where no
// "<pid>/comm" exists. The symptom was not a crash but a fabricated reading:
// `gopi worker --status` reported a perfectly live daemon as
// "pid N: cannot attest (no readable comm entry)" and demoted it to residue.
//
// Measured: TestWorkerDaemon_StatusReportsTheLiveDaemon and
// TestWorkerStatusRecycledPidIsResidueNotAWorker both FAILED on the branch
// while passing on the untouched master baseline (10f96bc0) — a grep of
// cmd/agent at that moment showed both facts served by the one name.
//
// The fix is two names, each meaning one thing (Rules 13/14 — but an alias is
// only correct between names that mean the SAME thing). This test pins that:
// the procfs root really is a procfs root, the release destination really is
// the leaf package's single value, and the two are NOT the same path.
package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/nickhuang99/gopi/internal/worktreerelease"
)

func TestProcRootAndCwdReleasePathAreDistinctFacts(t *testing.T) {
	// ① The procfs root names the procfs mount — asserted by USE, not by
	// comparing two spellings of the same literal: a real <pid>/comm must be
	// readable under it for THIS process.
	if ProcRootPath != procRootName {
		t.Fatalf("ProcRootPath = %q, want the procfs mount %q", ProcRootPath, procRootName)
	}
	comm := filepath.Join(ProcRootPath, strconv.Itoa(os.Getpid()), procCommName)
	if _, err := os.Stat(comm); err != nil {
		t.Fatalf("ProcRootPath %q carries no %s — every procfs read resolves to a false negative: %v",
			ProcRootPath, comm, err)
	}

	// ② The release destination is the leaf package's ONE value (Rules 13/14).
	if CwdReleasePath != worktreerelease.ProcessRootPath {
		t.Fatalf("CwdReleasePath = %q, want the one release destination %q",
			CwdReleasePath, worktreerelease.ProcessRootPath)
	}

	// ③ The defect itself: one name serving both facts. These are different
	// facts, so a future consolidation that re-merges them fails here.
	if ProcRootPath == CwdReleasePath {
		t.Fatalf("the procfs root and the cwd-release destination resolved to the same path %q — "+
			"one name cannot mean both; the release destination is %q",
			ProcRootPath, worktreerelease.ProcessRootPath)
	}
}
