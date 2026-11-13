// task_merged_cleanup_stale_registration_test.go — a REGISTRATION whose
// directory is GONE must not protect its branch.
//
// Re-derived (epic: re-measure archive/fix/park-worktree-live-row-guard) from
// the archived tag's own registration parser, which skipped git's `prunable`
// entries ("their registration is `git worktree prune`'s business, not a
// removal's"). The park lane that parser served is retired, but the marker it
// honoured is not: mergedCleanupWorktrees — the ONE branch→worktree reader
// behind repoSweepProtectedBranches (the merger tick's candidate filter) and
// the reclaim arm's occupancy lookups — ignores it, so a registration whose
// directory is gone still withholds its branch and still claims an occupancy.
//
// MEASURED (git 2.x on this host; the probe is inlined in
// tasks/01a0b51d-park-worktree-live-row-guard-remeasure-report.md): after the
// directory is deleted git keeps listing the entry and annotates it
// `prunable gitdir file points to non-existent location`; `git worktree prune`
// then drops it. So the registration is a bookkeeping leftover, never a
// checkout — nothing is working in a directory that does not exist.
//
// The fixture is repo_sweep_notff_test.go's temp repo (real git, no daemon, no
// child, no LLM); the ledger is not consulted by this arm.
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergedCleanupProtectedExcludesAStaleRegistration(t *testing.T) {
	p := newParkRepo(t)
	live := filepath.Join(t.TempDir(), "live-wt")
	p.git(t, "worktree", "add", "-q", live, "feat/conflict")

	// CONTROL — a registration whose directory EXISTS protects its branch. The
	// fix must leave this half alone: a real checkout is ACTIVE BOND WORK.
	protected, err := repoSweepProtectedBranches(p.dir)
	if err != nil {
		t.Fatalf("protected branches: %v", err)
	}
	if !protected["feat/conflict"] {
		t.Fatalf("fixture/control: a LIVE linked worktree does not protect feat/conflict: %+v", protected)
	}

	// The directory goes away. git's own answer is what the fixture asserts, so
	// the test cannot pass on a git that stopped marking the entry: the exact
	// porcelain token the reader must key on is the assertion.
	if err := os.RemoveAll(live); err != nil {
		t.Fatal(err)
	}
	if out := p.git(t, "worktree", "list", "--porcelain"); !strings.Contains(out, worktreePorcelainPrunable) {
		t.Fatalf("fixture: git no longer marks the deleted checkout %q (git version dependent) — the reader's key is this token:\n%s",
			worktreePorcelainPrunable, out)
	}

	protected, err = repoSweepProtectedBranches(p.dir)
	if err != nil {
		t.Fatalf("protected branches: %v", err)
	}
	if protected["feat/conflict"] {
		t.Errorf("feat/conflict is reported PROTECTED by a registration whose directory is GONE "+
			"(git marks it %q) — the merger therefore withholds a branch no process can be working in: %+v",
			worktreePorcelainPrunable, protected)
	}

	// The consequence, in the SAME call's vocabulary: the branch is back in the
	// candidate set the tick decides.
	cands, _, err := repoSweepUnprotectedBranches(p.dir, 0, nil)
	if err != nil {
		t.Fatalf("unprotected branches: %v", err)
	}
	found := false
	for _, c := range cands {
		if c == "feat/conflict" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("feat/conflict is missing from the candidate set although its registration's directory is gone: %v", cands)
	}
}
