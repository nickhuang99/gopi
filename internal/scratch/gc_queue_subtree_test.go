package scratch

// gc_queue_subtree_test.go — the regression guard for the scratch tree's
// UNBOUNDED SUBTREE.
//
// THE BUG (measured on this host, 2026-09-14): SweepHome walked only the DIRECT
// children of the scratch home, and its ownership predicate (IsScratchEntry)
// matches MARKERS inside a name — "suite-", "gopi-suite-tmp-", "queue-". The
// queue directory is named by the bare constant `queue`, so it matched nothing:
// the whole <home>/queue subtree (5,887 entries, 97M of the home's 2.2G,
// including orphaned queue-runtime-merge-build-* run directories whose merger
// was SIGKILLed and whose gocache/gomodcache dirs are tens of MB each) was
// unreachable by the sweep that exists to bound that tree.
//
// RED-before/GREEN-after: on the pre-fix tree the queue dir survives a sweep
// with a count of 0; the fix must collect it, and must still refuse it while a
// live process holds anything inside it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
)

// queueFixture builds a scratch home holding a queue subtree with one stale
// run directory inside it, returning the home, the queue dir and the run dir.
func queueFixture(t *testing.T, age time.Duration) (home, queue, run string) {
	t.Helper()
	home = t.TempDir()
	queue = filepath.Join(home, QueueDirName)
	run = filepath.Join(queue, runtimeEntryPrefix+RuntimeClassMergeBuild+"-424242")
	if err := os.MkdirAll(run, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", run, err)
	}
	// A run's own caches: the bulk of the bytes the sweep must reclaim.
	if err := os.MkdirAll(filepath.Join(run, "gocache"), DirPerm); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	for _, p := range []string{filepath.Join(run, "gocache"), run, queue} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}
	return home, queue, run
}

// TestSweepHomeReachesTheQueueSubtree is THE regression: a stale, unheld queue
// CHILD is collected by the sweep that is supposed to bound the scratch tree.
//
// The queue DIRECTORY itself survives by design — it is the tree's structure
// (the next writer recreates it), and the unit of ownership is the RUN, not the
// container. What must not survive is the stale run inside it, which is where
// the bytes are (a run's gocache/gomodcache).
func TestSweepHomeReachesTheQueueSubtree(t *testing.T) {
	home, queue, run := queueFixture(t, staleRootAge)
	n, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d entries, want 1 (the stale queue run)", n)
	}
	if exists(t, run) {
		t.Fatalf("the stale queue run %s SURVIVED a sweep — the scratch tree is unbounded", run)
	}
	if !exists(t, queue) {
		t.Fatalf("the queue directory itself %s was removed — it is the tree's structure, not residue", queue)
	}
}

// TestSweepHomeReachesStaleQueueRunsBesideAFreshOne is the regression for the
// GRANULARITY defect (measured on this host, 2026-09-14): the first version of
// the subtree arm judged the queue ALL-OR-NOTHING, so a single fresh (or held)
// child shielded every stale sibling — of 6,064 queue children on this host,
// 5,583 were stale while the sweep reported nothing reclaimable. A container is
// not a unit of ownership; each run is.
func TestSweepHomeReachesStaleQueueRunsBesideAFreshOne(t *testing.T) {
	home, queue, staleRun := queueFixture(t, staleRootAge)
	freshRun := filepath.Join(queue, runtimeEntryPrefix+RuntimeClassMergeBuild+"-777777")
	if err := os.MkdirAll(freshRun, DirPerm); err != nil {
		t.Fatal(err)
	}
	fresh := time.Now().Add(-time.Minute)
	if err := os.Chtimes(freshRun, fresh, fresh); err != nil {
		t.Fatal(err)
	}
	n, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1 — a FRESH sibling must not shield a stale run (all-or-nothing is the defect)", n)
	}
	if exists(t, staleRun) {
		t.Fatal("a stale queue run was shielded by its fresh sibling")
	}
	if !exists(t, freshRun) {
		t.Fatal("a fresh queue run was removed — the age rule must reach inside the queue")
	}
}

// TestSweepHomeKeepsAHeldQueueSubtree: the hard rule is unchanged by the new
// reach — a live process inside the queue keeps the whole subtree.
//
// The holder must be a SEPARATE process (a child here): a directory the
// sweeping process holds ITSELF is deliberately collectable, because that can
// only be a leftover of our own earlier run (the sweep's whole contract).
func TestSweepHomeKeepsAHeldQueueSubtree(t *testing.T) {
	home, queue, run := queueFixture(t, staleRootAge)
	holder := exec.Command("sleep", "60")
	// Pdeathsig: a SIGKILLed test binary runs no t.Cleanup, and a holder left
	// standing in the queue is the exact shape this case exists to measure.
	holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	holder.Dir = run
	if err := holder.Start(); err != nil {
		t.Fatalf("start the holder: %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill() //stylegate:ignore the holder exists only to hold the directory for this test
		_ = holder.Wait()         //stylegate:ignore reaping the test's own child; its exit status is not asserted
	})
	// Starting the child refreshed the directory's mtime; re-age the entries so
	// ONLY the holder rule can explain a keep.
	old := time.Now().Add(-staleRootAge)
	for _, p := range []string{run, queue} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 0 {
		t.Fatalf("swept %d entries (err=%v), want 0 — a live cwd holder must veto the subtree", n, err)
	}
	if !exists(t, queue) || !exists(t, run) {
		t.Fatal("a held queue subtree was removed")
	}
}

// TestSweepHomeKeepsAFreshQueueSubtree: the age rule reaches inside too — a
// queue entry younger than the threshold is not collected (the pool's purpose
// is to survive; a fresh run must stay reusable).
func TestSweepHomeKeepsAFreshQueueSubtree(t *testing.T) {
	home, queue, _ := queueFixture(t, time.Minute)
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 0 {
		t.Fatalf("swept %d (err=%v), want 0 — a fresh queue entry must survive", n, err)
	}
	if !exists(t, queue) {
		t.Fatal("a fresh queue subtree was removed")
	}
}

// TestSweepHomeKeepsAnonymousBatchPoolUntilItsWiderWindow: the pooled rsync
// batch directories are bounded by BatchPoolStaleAge, NOT by the generic stale
// age — a batch a crashed rsync left behind must remain reusable for its full
// window before the sweep takes it. The prefix had NO production reader at all
// before this fix (its only readers were tests), so a leaked batch directory
// was collectable by nothing.
func TestSweepHomeKeepsAnonymousBatchPoolUntilItsWiderWindow(t *testing.T) {
	home := t.TempDir()
	pool := filepath.Join(home, BatchPoolDirPrefix+"tag-deadbeef")
	if err := os.MkdirAll(pool, DirPerm); err != nil {
		t.Fatal(err)
	}
	// Older than DefaultStaleAge (so the generic arms would take it), younger than
	// BatchPoolStaleAge: the pool's own window must protect it.
	mid := time.Now().Add(-(DefaultStaleAge + time.Hour))
	if err := os.Chtimes(pool, mid, mid); err != nil {
		t.Fatal(err)
	}
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 0 {
		t.Fatalf("swept %d (err=%v), want 0 — the pool's own age window is %v", n, err, BatchPoolStaleAge)
	}
	if !exists(t, pool) {
		t.Fatal("a pooled batch directory inside its own window was removed")
	}

	// Past the pool's window it goes — the reader the dead helper never had.
	past := time.Now().Add(-(BatchPoolStaleAge + time.Hour))
	if err := os.Chtimes(pool, past, past); err != nil {
		t.Fatal(err)
	}
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 1 {
		t.Fatalf("swept %d (err=%v), want 1 — a batch pool past %v is garbage", n, err, BatchPoolStaleAge)
	}
	if exists(t, pool) {
		t.Fatal("a stale pooled batch directory survived the sweep")
	}
}

// TestSweepHomeKeepsAnEmptyQueueDirectory: the queue DIRECTORY itself is the
// tree's own structure, never residue. An idle (or flat-artifact-only) queue
// holds no run to reclaim, so collecting it would report a removal without
// reclaiming anything — and the next writer would recreate it at once.
//
// This is the minimal-sample check (Rule 10.4) that caught the first version of
// the arm: internal/testutil's GC fixtures create an EMPTY queue dir, and
// "removed 2, want exactly 1" was how the false positive announced itself.
//
// NOTE (queue-as-namespace): the idle entry here is a FLAT FILE, not a
// directory. Since the queue is a namespace (see queueChildCollectable), every
// DIRECTORY inside it is a run by definition and ages out — there is no
// name-based way left to call a directory in the queue "someone else's", which
// is precisely the hole this epic closes. Flat entries stay: the queue is a
// shared drop point for artifacts (reports, .cleanup-* registries) that carry
// no run semantics at all.
func TestSweepHomeKeepsAnEmptyQueueDirectory(t *testing.T) {
	home := t.TempDir()
	queue := filepath.Join(home, QueueDirName)
	if err := os.MkdirAll(queue, DirPerm); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(queue, "someone-elses-loose-file")
	if err := os.WriteFile(foreign, []byte("not a run"), FilePerm); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-staleRootAge)
	for _, p := range []string{foreign, queue} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 0 {
		t.Fatalf("swept %d (err=%v), want 0 — an idle queue is the tree's structure, not residue", n, err)
	}
	if !exists(t, queue) {
		t.Fatal("the empty queue directory was removed")
	}
	if !exists(t, foreign) {
		t.Fatal("a flat artifact inside the queue was removed")
	}
}

// unmarkedQueueChild builds a scratch home whose queue holds ONE child of the
// given name and age, with the bulk directory a real run carries (gocache).
//
// THE MEASURED SHAPES this fixture reproduces (host audit, 2026-09-14): the
// queue held `verify-01a0a248-modcache-manual` (415 MB), `accept` and
// `test-budget` — queue children produced WITHOUT the "queue-" marker. The
// sweep's ownership predicate asked the NAME (IsScratchEntry: "queue-" infix),
// so each of them was invisible: never deleted, and never even listed by
// --dry-run. The bytes are the point — a run's gocache/gomodcache is what makes
// an invisible child a disk leak rather than a cosmetic one.
func unmarkedQueueChild(t *testing.T, name string, age time.Duration) (home, child string) {
	t.Helper()
	home = t.TempDir()
	queue := filepath.Join(home, QueueDirName)
	child = filepath.Join(queue, name)
	gocache := filepath.Join(child, "gocache")
	if err := os.MkdirAll(gocache, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", gocache, err)
	}
	if err := os.WriteFile(filepath.Join(gocache, "payload"), []byte("cache bytes"), FilePerm); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	for _, p := range []string{gocache, child, queue} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}
	return home, child
}

// TestSweepHomeCollectsUnmarkedQueueChildren is THE regression for the queue's
// name-marker hole: a stale, unheld queue child that carries NO marker must be
// BOTH reported by the dry-run plan and collected by the real sweep.
//
// RED-before/GREEN-after: on the pre-fix tree the plan is empty and the sweep
// removes 0 for every one of these names — the 415 MB-era shape, permanently
// invisible. The fix must not depend on the name at all: inside the queue the
// namespace IS the container, and only the age + holder vetoes decide.
func TestSweepHomeCollectsUnmarkedQueueChildren(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionWideParallel)
	for _, name := range []string{
		"verify-01a0a248-modcache-manual", // measured: 415 MB of gocache/gomodcache
		"accept",                          // measured: a bare, short, unmistakeable name
		"test-budget",                     // scripts/test-budget.sh's own directory
	} {
		t.Run(name, func(t *testing.T) {
			home, child := unmarkedQueueChild(t, name, staleRootAge)

			plan, err := SweepHomePlan(home, DefaultStaleAge)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if len(plan) != 1 || plan[0].Path != child {
				t.Fatalf("dry-run plan = %+v, want exactly [%s] — an unmarked queue child is INVISIBLE to the plan (the reported hole)", plan, child)
			}

			n, err := SweepHome(home, DefaultStaleAge)
			if err != nil {
				t.Fatalf("sweep: %v", err)
			}
			if n != 1 {
				t.Fatalf("swept %d, want 1 — %s is unreclaimable residue (the queue's name-marker hole)", n, name)
			}
			if exists(t, child) {
				t.Fatalf("%s SURVIVED the sweep — non-marker queue children leak forever", child)
			}
			if !exists(t, filepath.Join(home, QueueDirName)) {
				t.Fatal("the queue directory itself was removed — it is the tree's structure")
			}
		})
	}
}

// TestSweepHomeKeepsUnmarkedQueueArtifacts: the namespace reaches every
// DIRECTORY in the queue, so the keeps must be stated explicitly — and the
// measured keeps are flat artifacts another component drops there: a verifier's
// report (01a09fdf-verify-report.md) and the shell library's cleanup registry
// (.cleanup-<pid>, scripts/lib/tmp-ban.sh). Neither is a run: neither has a
// gocache, neither is owned by a pid that can be asked, and a report is
// evidence someone is still reading. A flat entry is therefore never a queue
// run — the one keep the namespace arm adds (Rule 15: what we cannot attribute
// to a run we do not delete).
func TestSweepHomeKeepsUnmarkedQueueArtifacts(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	home := t.TempDir()
	queue := filepath.Join(home, QueueDirName)
	if err := os.MkdirAll(queue, DirPerm); err != nil {
		t.Fatal(err)
	}
	artifacts := []string{
		filepath.Join(queue, "01a09fdf-verify-report.md"),
		filepath.Join(queue, ".cleanup-3876503"),
		filepath.Join(queue, "daemon-cwd-commit-msg.txt"),
	}
	for _, f := range artifacts {
		if err := os.WriteFile(f, []byte("in-flight artifact"), FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-staleRootAge)
	for _, p := range append([]string{queue}, artifacts...) {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if plan, err := SweepHomePlan(home, DefaultStaleAge); err != nil || len(plan) != 0 {
		t.Fatalf("plan = %+v (err=%v), want empty — a flat artifact is not a run", plan, err)
	}
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 0 {
		t.Fatalf("swept %d (err=%v), want 0 — an in-flight artifact was deleted", n, err)
	}
	for _, f := range artifacts {
		if !exists(t, f) {
			t.Fatalf("%s was removed — the queue's shared artifacts must survive the sweep", f)
		}
	}
}

// TestSweepHomeKeepsAHeldUnmarkedQueueChild: the HARD rule is untouched by the
// namespace reach. A live process whose cwd is inside an unmarked queue child
// vetoes it, exactly as it vetoes a named one — the namespace changes WHAT is
// eligible, never whether a live holder protects it.
func TestSweepHomeKeepsAHeldUnmarkedQueueChild(t *testing.T) {
	home, child := unmarkedQueueChild(t, "verify-live-holder", staleRootAge)
	holder := exec.Command("sleep", "60")
	// Pdeathsig: the holder must die with the binary, or it outlives the case
	// it was planted for and holds a queue child forever.
	holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	holder.Dir = child
	if err := holder.Start(); err != nil {
		t.Fatalf("start the holder: %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill() //stylegate:ignore the holder exists only to hold the directory for this test
		_ = holder.Wait()         //stylegate:ignore reaping the test's own child; its exit status is not asserted
	})
	// Starting the child refreshed the directories' mtime; re-age them so ONLY
	// the holder rule can explain a keep.
	old := time.Now().Add(-staleRootAge)
	for _, p := range []string{filepath.Join(child, "gocache"), child, filepath.Join(home, QueueDirName)} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 0 {
		t.Fatalf("swept %d (err=%v), want 0 — a live cwd holder must veto an unmarked queue child", n, err)
	}
	if !exists(t, child) {
		t.Fatal("a LIVE-held unmarked queue child was removed — the hard rule was weakened")
	}
}

// TestSweepHomeKeepsAFreshUnmarkedQueueChild: the age rule reaches the
// namespace too. A queue child younger than the threshold may be a run that is
// starting right now (its producer has not registered anything yet), so it is
// kept by age alone.
func TestSweepHomeKeepsAFreshUnmarkedQueueChild(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	home, child := unmarkedQueueChild(t, "test-budget", time.Minute)
	if plan, err := SweepHomePlan(home, DefaultStaleAge); err != nil || len(plan) != 0 {
		t.Fatalf("plan = %+v (err=%v), want empty — a fresh child is not stale", plan, err)
	}
	if n, err := SweepHome(home, DefaultStaleAge); err != nil || n != 0 {
		t.Fatalf("swept %d (err=%v), want 0 — a fresh unmarked queue child must survive", n, err)
	}
	if !exists(t, child) {
		t.Fatal("a fresh unmarked queue child was removed")
	}
}
