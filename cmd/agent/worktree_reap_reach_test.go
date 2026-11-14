package main

// The worktree-reap REACHABILITY proof (epic worktree-reap-reachable).
//
// THE DEFECT THIS FILE PINS: `serveWorktreeReapSweep` — the ownership-
// INDEPENDENT reap, the only instrument that can collect a registration no
// ledger row remembers — was appended (35faf715, 2026-09-14) INSIDE
// `serveMergedCleanupSweep`. That function's only production caller had been
// deleted on 2026-09-04 (d11af9a6, "merger pass is config-driven per-repo
// sweep"), so from then on the reap was reachable from its own tests and from
// NOTHING else: on a host running the merger a dead registration sat
// uncollected forever while the wiring test kept passing.
//
// THE SAME DEFECT CAME BACK ONE LEVEL UP, and this file is what caught it. The
// first repair moved the reap onto taskLifecyclePass (the lifecycle tick).
// 187d05c then removed the WORKER round's call to that tick (operator ruling
// 2026-10-26: a worker dispatches, it does not reconcile the ledger), and the
// merger's per-tick pass the other half of the sentence named had already been
// deleted on 2026-09-19 — so from 2026-10-26 the tick had NO production caller
// either, and the reap was once again reachable from its own tests only. The
// shape is worth naming because it is what makes this test necessary: a
// reachability claim rots the moment the carrier is removed, and a test that
// drives a FUNCTION rather than the OPERATOR'S VERB cannot tell.
//
// THE CARRIER NOW (measured 2026-10-30): `gopi task merged-cleanup` —
// runTaskMergedCleanup → mergedCleanupPassOnce → serveWorktreeReapSweep. A live
// verb on every host that merges, already holding the merger's work lock (the
// reap mutates git bookkeeping), whose subject is exactly "merged branches and
// the worktrees they leave behind". No new dependency (Rule 35): no timer, no
// daemon, no config key — the reap rides a command an operator types.
//
// THE TESTS BELOW THEREFORE DRIVE THE VERB'S OWN BODY (mergedCleanupPassOnce —
// the composition runTaskMergedCleanup prints, reachable without the flock plus
// `os.Exit` face a test cannot undo) and assert the reap RAN and ACTED, in its
// PRODUCTION posture. Existence is not reachability; a call site inside an
// unreachable function is exactly the bug. The last test is the inverted guard:
// the old carrier must NOT quietly grow the reap back, because two carriers race
// over one registration and a reader could not tell which one the host uses.
//
// Hermeticity (Rule 6): a private GOPI_HOME/ledger/settings sandbox; the process
// CWD is moved INTO the sandbox (the sweep also sweeps the CWD's own repo, so
// without this it would judge this suite's real worktrees); the settings
// "repos" table names only the sandbox repo; the worktree root is pinned to a
// sandbox directory. Every repo and every registration the pass can see lives
// under t.TempDir().

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

// Reachability fixture constants (Rule 1: every literal lives here).
const (
	// wrDeadRegistrationName is the fixture registration whose directory is
	// removed — the leak shape the reap exists for.
	wrDeadRegistrationName = "wt-reach-dead"

	// wrSandboxRepoName is the sandbox repository the pass is told to sweep.
	wrSandboxRepoName = "repo"

	// wrSandboxRootName is the declared worktree root inside the sandbox.
	wrSandboxRootName = "wt-root"
)

// wrSandbox wires a private home + ledger + settings whose ONLY repo is the
// sandbox's own, pins the worktree root into the sandbox, and moves the process
// CWD there. It returns (repo, root) with a seeded repo, ready for a linked
// worktree.
func wrSandbox(t *testing.T) (repo, root, sink string) {
	t.Helper()
	// The ledger must be a FILE OF THIS SANDBOX'S PROFILE, never a sibling of it:
	// deliverTaskNotify suppresses the events of a ledger that is not its
	// profile's own, so the orphan escalation below was recorded and reported but
	// never broadcast, and this test failed on the absence of the sink file it had
	// just asked the code to write (measured 2026-11-04, task 01a1a69f).
	sandbox, _ := sandboxProfileLedger(t)
	repo = filepath.Join(sandbox, wrSandboxRepoName)
	root = filepath.Join(sandbox, wrSandboxRootName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	serveCleanupFixtureGit(t, sandbox, "init", "-q", repo)
	serveCleanupFixtureGit(t, repo, "config", "user.email", "t@t")
	serveCleanupFixtureGit(t, repo, "config", "user.name", "t")
	serveCleanupFixtureGit(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	settings := filepath.Join(sandbox, "settings.json")
	body := `{"repos":{"` + repo + `":{"worktree_root":"` + root + `"}}}`
	if werr := os.WriteFile(settings, []byte(body), 0o644); werr != nil {
		t.Fatal(werr)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	// The notify sink is a SANDBOX file (Rule 6): the live operator
	// task-events.jsonl must never be touched by a test, and the reap's orphan
	// escalation is fired through this same channel.
	sink = filepath.Join(sandbox, "notify.log")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+sink)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")
	t.Setenv("GOPI_TASK_NOTIFY_ASYNC", "0")
	t.Setenv("GOPI_TASK_NOTIFY_WEBHOOK", "")
	// The sweep also enumerates the CWD's own repo. Inside the sandbox, that
	// repo IS the fixture — so the pass cannot reach a real worktree of this
	// host, which is the property that lets these tests run in APPLY mode.
	t.Chdir(sandbox)
	return repo, root, sink
}

// wrDeadRegistrationFixture plants the leak's own shape under root: a real
// linked worktree whose directory is then removed. Only git BOOKKEEPING is
// left — precisely what survives forever (prune skips a locked entry, remove
// refuses one) and what the reap collects. It returns the admin dir that must
// be gone once the reap has acted.
func wrDeadRegistrationFixture(t *testing.T, repo, root string) string {
	t.Helper()
	wt := filepath.Join(root, wrDeadRegistrationName)
	serveCleanupFixtureGit(t, repo, "worktree", "add", wt, "-b", "feat/reach-dead")
	admin := filepath.Join(repo, ".git", "worktrees")
	entries, err := os.ReadDir(admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("fixture has %d registrations, want 1", len(entries))
	}
	// The "initializing" lock is what a SIGKILLed `worktree add` leaves behind,
	// and what makes `git worktree prune` skip the entry forever; age it past
	// the in-flight floor so it reads as abandoned, as it does in the wild.
	lock := filepath.Join(admin, entries[0].Name(), "locked")
	if werr := os.WriteFile(lock, []byte("initializing"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	old := time.Now().Add(-2 * time.Hour)
	if cerr := os.Chtimes(lock, old, old); cerr != nil {
		t.Fatal(cerr)
	}
	adminEntry := filepath.Join(admin, entries[0].Name())
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	return adminEntry
}

// reachProbeRecorder observes every git invocation the reap makes, so a test
// can assert the sweep RAN (and which verb it reached) rather than inferring it
// from a side effect alone.
type reachProbeRecorder struct{ verbs [][]string }

func (r *reachProbeRecorder) Observe(args []string) { r.verbs = append(r.verbs, args) }

func (r *reachProbeRecorder) saw(sub string) bool {
	for _, call := range r.verbs {
		if strings.Contains(strings.Join(call, " "), sub) {
			return true
		}
	}
	return false
}

// installReapProbe wraps the sweep's git calls for the current test.
func installReapProbe(t *testing.T) *reachProbeRecorder {
	t.Helper()
	rec := &reachProbeRecorder{}
	prev := tools.InstallWorktreeSweepObserver(rec)
	t.Cleanup(func() { tools.InstallWorktreeSweepObserver(prev) })
	return rec
}

func sweepCreatedRecordPathForReap(t *testing.T, worktree string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatalf("read gitdir backlink: %v", err)
	}
	line := strings.TrimSpace(string(raw))
	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("unexpected .git file content: %q", line)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	return filepath.Join(gitdir, "gitdir")
}

// wrDeadFixture is the shared body of the two reachability proofs below: seed a
// sandbox repo, plant a dead registration, and install the git-call probe.
func wrDeadFixture(t *testing.T) (repo, adminEntry string, rec *reachProbeRecorder) {
	t.Helper()
	repo, root, _ := wrSandbox(t)
	return repo, wrDeadRegistrationFixture(t, repo, root), installReapProbe(t)
}

// TestMergedCleanupPassInvokesWorktreeReap is THE reachability proof: one run of
// the production verb's own body — `gopi task merged-cleanup`, the face an
// operator types — must RUN the reap, and the reap must ACT (collect the dead
// registration). Before this change it fails on every count: the verb only
// reported, and the reap lived on a tick with no production caller.
//
// It drives mergedCleanupPassOnce rather than runTaskMergedCleanup because that
// wrapper takes the merger's work lock and calls os.Exit on a non-repo CWD; the
// composition asserted here IS the body the wrapper prints, so the assertion
// covers the production path.
func TestMergedCleanupPassInvokesWorktreeReap(t *testing.T) {
	repo, adminEntry, rec := wrDeadFixture(t)

	out := strings.Join(mergedCleanupPassOnce(repo), "\n")

	if !rec.saw("worktree list") {
		t.Fatalf("`gopi task merged-cleanup` never ran the worktree reap (no `git worktree list` from the "+
			"sweep) — serveWorktreeReapSweep is unreachable again, which is the 2026-09-16 defect and the "+
			"2026-10-26 repeat of it. observed verbs=%v output=%s", rec.verbs, out)
	}
	if !strings.Contains(out, "["+worktreeReapLegLabel+"]") {
		t.Fatalf("the verb ran the reap but reported nothing (%q absent) — Rule 15: an operator must see "+
			"the leg's verdict; output=%s", worktreeReapLegLabel, out)
	}
	if _, serr := os.Stat(adminEntry); !os.IsNotExist(serr) {
		t.Fatalf("the dead registration survived `gopi task merged-cleanup` (stat err = %v) — the reap ran "+
			"in a non-acting posture; output=%s", serr, out)
	}
}

// TestMergedCleanupPassKeepsItsOwnReportFirst pins the ORDER the verb's output
// promises: the merged-branch report is this face's own verdict and comes first,
// the registration reap's leg after it. An operator reading one command's output
// must see "what the merged branch's worktree became" before "which
// registrations nothing remembers".
func TestMergedCleanupPassKeepsItsOwnReportFirst(t *testing.T) {
	repo, _, _ := wrDeadFixture(t)
	lines := mergedCleanupPassOnce(repo)
	if len(lines) == 0 {
		t.Fatal("merged-cleanup produced no output at all — a silent verb is a Rule 15 defect")
	}
	if !strings.HasPrefix(lines[0], "merged-cleanup (") {
		t.Fatalf("the first line must be the face's own summary, got %q", lines[0])
	}
	reapAt := -1
	for i, line := range lines {
		if strings.Contains(line, "["+worktreeReapLegLabel+"]") {
			reapAt = i
			break
		}
	}
	if reapAt < 1 {
		t.Fatalf("the reap leg is missing or precedes the face's own report (index %d) — lines=%v", reapAt, lines)
	}
}

// TestLifecyclePassIsGoneAndNothingGrowsItBack is the inverted guard on the
// MOVE, inverted a second time on 2026-10-30 when the pass itself was deleted
// (task 01a18ef7).
//
// Its predecessor drove taskLifecyclePass and asserted the reap had NOT moved
// back into it. That test can no longer exist — the function is gone — and
// deleting it outright would leave the property it guarded unpinned: the reap
// must run from `gopi task merged-cleanup` and from nowhere else, because two
// carriers race over one registration (two processes judging the same
// `.git/worktrees` entry). So the guard is now on the SOURCE: no task-lifecycle
// pass function may exist, and no file may call runTaskLifecycleTick.
//
// It reads the tree the way worker_round_dispatch_only_test.go reads the round
// region — a scan, not a comment, because a comment cannot enforce itself.
func TestLifecyclePassIsGoneAndNothingGrowsItBack(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package dir: %v", err)
	}
	banned := []string{"taskLifecyclePassOnce(", "taskLifecyclePass(", "runTaskLifecycleTick("}
	var offenders []string
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		scanned++
		for _, b := range banned {
			if strings.Contains(string(data), b) {
				offenders = append(offenders, name+" contains "+b)
			}
		}
	}
	if scanned < 20 {
		t.Fatalf("the scan reached only %d non-test .go files in cmd/agent — a scan that measured nothing "+
			"must not read green (the vacuum this repository has paid for twice)", scanned)
	}
	if len(offenders) > 0 {
		t.Fatalf("a deleted task-lifecycle pass carrier is back: %v — the pass had NO production caller "+
			"(worker round: 187d05c; merger tick: 2026-09-19; `task reconcile`: 01a18799), so its legs were "+
			"given real carriers or deleted. See task_lifecycle_tick.go's header for the per-leg verdict.", offenders)
	}
}

// TestLifecyclePassEscalatesDirtyOrphanEndToEnd is the second half of the orphan
// rule, proven through a PRODUCTION path: a real DIRTY linked worktree that NO
// ledger row owns — the live shape this epic was filed for — must be ESCALATED
// on both surfaces (the pass's own log line and the operator notify channel),
// and must NOT be removed. It is the one shape every other arm on the host is
// blind to: the producer pass needs a row keyed on this host's own claim, the
// dead-holder arm needs a terminal row, and the age arm's guard ② refuses dirt.
//
// It drives the VERB's body now (the tick's leg moved; the engine's judgement is
// the same), which keeps the escalation reachable from a command an operator
// actually runs.
//
// The engine rule itself is unit-tested in internal/tools
// (TestSweepEscalatesDirtyOrphanAndKeepsTheOwnedDirtyReport); what this test adds
// is that the escalation is REACHABLE from the verb and reaches a human.
func TestLifecyclePassEscalatesDirtyOrphanEndToEnd(t *testing.T) {
	repo, root, sink := wrSandbox(t)
	wt := filepath.Join(root, "wt-orphan-dirty")
	serveCleanupFixtureGit(t, repo, "worktree", "add", wt, "-b", "fix/orphan-dirty")
	// The dirt is a TRACKED modification — the shape that means real work, not a
	// run's own untracked residue — and the tree has no ledger row at all.
	if werr := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("uncommitted delivery\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	// Age the registration past the sweep's floor through git's own creation
	// record (the only age signal a later access cannot forge).
	old := time.Now().Add(-72 * time.Hour)
	if cerr := os.Chtimes(sweepCreatedRecordPathForReap(t, wt), old, old); cerr != nil {
		t.Skipf("chtimes unavailable: %v", cerr)
	}

	out := strings.Join(mergedCleanupPassOnce(repo), "\n")

	if !strings.Contains(out, "escalated orphan=") {
		t.Fatalf("`gopi task merged-cleanup` did not escalate the dirty ownerless worktree; output=%s", out)
	}
	if !strings.Contains(out, "fix/orphan-dirty") {
		t.Errorf("the escalation must name the branch so an operator can act; output=%s", out)
	}
	if _, serr := os.Stat(wt); serr != nil {
		t.Fatalf("the dirty worktree was REMOVED — uncommitted work must never be destroyed: %v", serr)
	}
	data, rerr := os.ReadFile(sink)
	if rerr != nil {
		t.Fatalf("the escalation never reached the operator channel (Rule 15): %v", rerr)
	}
	if !strings.Contains(string(data), "worktree-orphan") || !strings.Contains(string(data), "fix/orphan-dirty") {
		t.Fatalf("the operator alarm must carry the event and the branch: %s", data)
	}
}
