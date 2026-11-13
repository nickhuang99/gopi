package tools

// The producer-side release must not mistake an ORPHANED cwd-inheritor for a
// holder (epic worktree-not-released, 2026-09-24).
//
// THE MEASURED SYMPTOM this file reproduces: a `gopi worker` round's release
// pass kept its own finished ground with "held by a live process
// (cwd/fd/exe/maps)" on every pass, forever. The holder was not a run — it was a
// leaked child (a `gopi web` / server a finished round had started) that had been
// reparented to init, whose recorded launcher pid had been dead for hours, and
// whose session held no other gopi process. The LEDGER-keyed arm one file over
// already asks the orphan ladder on refusal (task_reclaim.go's guard ③); this
// arm (reclaimSelfOwnWhy's rung ②) read the raw holder scan alone, so the ONE
// pass whose job is freeing what nothing owns was the pass that could never
// release an orphan-pinned ground.
//
// The fixtures are the suite's existing ones: the orphan builder
// (task_finish_cleanup_orphan_test.go — a launcher in its own session spawns a
// holder with cwd inside the worktree and exits, so the holder is reparented to
// init carrying a dead launcher record) and daemonCwdHolder (a live,
// unreparented child of THIS test process, which is the shape a wrapper's own
// executor child wears).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// ageSelfOwnRegistration backdates the registration's creation record past
// reclaimSelfOwnMinAge, so ONLY the holder ladder can decide the case at hand
// (the age floor would otherwise keep every fresh fixture and the test would
// measure nothing).
func ageSelfOwnRegistration(t *testing.T, wt string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir:"))
	ageRegistrationRecord(t, gitdir, time.Now().Add(-2*reclaimSelfOwnMinAge))
}

// TestReclaimSelfOwnReleasesOrphanHeldRegistration is the RED proof for rung ②:
// a clean, aged, unowned registration whose ONLY occupant is an orphaned
// cwd-inheritor must be free, and the release must actually remove it (the
// orphan keeps sitting in the deleted directory — that is the leak it is, not a
// reason to withhold the directory forever).
func TestReclaimSelfOwnReleasesOrphanHeldRegistration(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db")) // empty ledger: nobody owns it
	ageSelfOwnRegistration(t, wt)

	orphan := newOrphanCwdHolder(t, wt, true)
	orphan.collect(t)
	orphan.becomesOrphan(t)

	// The primary holder scan DOES see it — the point is that seeing it is not
	// enough to call it a holder.
	if live, ok := scratch.ScanHolders().HeldByProcesses(wt); !ok || !live {
		t.Skip("orphan fixture: the holder is not visible as a cwd holder on this host")
	}
	why := reclaimSelfOwnWhy(worktreeRegistration{Path: wt, Branch: "feat/x"}, repo, scratch.ScanHolders())
	if strings.Contains(why, reclaimSelfOwnLiveHolderReason) {
		t.Fatalf("an orphaned cwd-inheritor must not keep the registration as a live holder (why=%q)", why)
	}

	// And the release really happens: the pass frees the directory.
	lines := reclaimSelfOwnRepo(repo, nil)
	freed := false
	for _, l := range lines {
		if strings.Contains(l, reclaimSelfOwnFreedMarker) && strings.Contains(l, wt) {
			freed = true
		}
	}
	if !freed {
		t.Fatalf("the orphan-pinned registration must be freed, lines=%v", lines)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Fatalf("the worktree directory must be gone after the release")
	}
	// The BRANCH is never touched — it is the deliverable.
	if got := gitRevParse(t, repo, "--verify", "--quiet", "feat/x"); got == "" {
		t.Fatalf("the branch must survive the directory's release")
	}
}

// TestReclaimSelfOwnKeepsLiveChildHeldRegistration is the POSITIVE CONTROL that
// keeps rung ②'s widening honest: a registration occupied by a LIVE CHILD OF THE
// ASKING PROCESS — the shape a `gopi task run` wrapper's own executor child wears
// (spawned with cmd.Dir = the ground) — is a genuine holder and keeps the
// registration. The ladder refuses it at its FIRST rung (not reparented to init),
// so the fall-through can never turn into "my own cwd is free".
func TestReclaimSelfOwnKeepsLiveChildHeldRegistration(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	ageSelfOwnRegistration(t, wt)

	daemonCwdHolder(t, wt) // killed by the suite's own reaper at test end
	if live, ok := scratch.ScanHolders().HeldByProcesses(wt); !ok || !live {
		t.Skip("live-holder fixture: the child's cwd is not visible as a holder on this host")
	}
	why := reclaimSelfOwnWhy(worktreeRegistration{Path: wt, Branch: "feat/x"}, repo, scratch.ScanHolders())
	if !strings.Contains(why, reclaimSelfOwnLiveHolderReason) {
		t.Fatalf("a live child of the asker standing in the registration must keep it, got %q", why)
	}
}

// TestFinishCleanupSelfTreeSparesThirdParties is the ANTI-OVER-EXCLUSION guard
// (epic worktree-not-released, measured 2026-09-25).
//
// The first version of finishCleanupSelfTree expanded its transitive closure
// from EVERY pid already in the set, ancestors included — and an ancestor is a
// shell / sshd / session leader whose other children are this host's SIBLING
// runs. Measured, that closure reached 65 pids: unrelated `gopi worker` rounds
// and a sandboxed `gopi ui` among them. The set is handed to the ladder's last
// rung (finishCleanupSessionHasLiveGopiExceptSubject), so over-excluding makes
// the rung answer "nothing is live" for a registration a REAL third party is
// standing in — the veto quietly disappears instead of narrowing.
//
// The property asserted here is stated without naming any pid: every excluded
// process must be one this asker ACCOUNTS for — itself, an ancestor of itself
// (its launcher chain is not a third party), or one of its own descendants. A
// process reparented to init in its own session, which is exactly the shape an
// unrelated round's detached child wears, is a third party and must stay OUT.
func TestFinishCleanupSelfTreeSparesThirdParties(t *testing.T) {
	self := os.Getpid()
	ancestors := finishCleanupSelfChain()
	tree := finishCleanupSelfTree()
	if tree[1] {
		t.Fatal("pid 1 must never be excluded: from init the closure reaches the whole host")
	}
	if !tree[self] {
		t.Fatal("the asker itself is not a third party and must be excluded")
	}
	// (a) the per-pid property: self, an ancestor of self, or a descendant of self.
	for pid := range tree {
		if pid == self || ancestors[pid] {
			continue
		}
		if finishCleanupIsDescendantOf(pid, self) {
			continue
		}
		t.Fatalf("pid %d is neither the asker, nor its ancestor, nor its descendant — the closure leaked outside this asker", pid)
	}
	// (b) a REAL stand-in third party, independently of the walk: a process
	// reparented to init in its own session (its launcher has exited). It must
	// not be in the set — measured: this is the shape of the leaked `gopi`
	// processes that made the over-exclusion reach the whole host.
	stray := reparentedToInitStandIn(t)
	if tree[stray] {
		t.Fatalf("pid %d (launcher exited, reparented to init, own session) is a third party and must NOT be excluded", stray)
	}
}

// finishCleanupIsDescendantOf reports whether pid's ancestry passes through
// ancestor. Unreadable ancestry is NOT a descendant (the safe direction: an
// unattributable process keeps its veto).
func finishCleanupIsDescendantOf(pid, ancestor int) bool {
	seen := map[int]bool{}
	for cur := pid; cur > 1; {
		if seen[cur] {
			return false
		}
		seen[cur] = true
		parent, ok := finishCleanupProcParent(strconv.Itoa(cur))
		if !ok {
			return false
		}
		if parent == ancestor {
			return true
		}
		cur = parent
	}
	return false
}

// reparentedToInitStandIn starts a process whose launcher exits immediately, so
// the process is handed to this host's reaper and shares no ancestry with this
// test. The pid is printed by the launcher before it exits; the stand-in is
// reaped by the test's own cleanup.
//
// A stand-in that never reaches that state is a FAILURE, not a skip: an
// unreparented process is a THIRD PARTY's child, which is the opposite of the
// shape these callers need, and skipping would let them report success without
// having built their premise (the defect this file's row fixes — see
// task_finish_cleanup_reparent_targets_test.go).
func reparentedToInitStandIn(t *testing.T) int {
	t.Helper()
	//fixture-spawn-ok: the stand-in's whole point is that its launcher EXITS immediately so it is reparented to this host's reaper; a Pdeathsig or a helper would tie it to this binary and destroy the shape. Its teardown kills the printed pid (the t.Cleanup below)
	out, err := exec.Command("sh", "-c", "sleep 60 >/dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Skipf("no sh to build the stand-in: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		t.Skipf("the stand-in printed no pid (%q)", string(out))
	}
	//stylegate:ignore signals the fixture process the test spawned
	t.Cleanup(func() { _ = exec.Command("kill", strconv.Itoa(pid)).Run() })
	waitOrphanReparentTarget(t, pid, orphanSettleWait, "session-directory stand-in")
	return pid
}
