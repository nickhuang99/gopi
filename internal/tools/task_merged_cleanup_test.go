package tools

// Merged-branch cleanup tests (epic 2026-09-02, nick's finding — serve
// merges branches but never removes the worktree). Real-git fixture (merge
// + worktree + branch -d semantics are git-truth — mocks cannot prove the
// -d refusal for worktree-checked-out branches, porcelain dirt, or the
// ancestor recheck). Each epic acceptance case maps to one test:
//
//	(a) merged branch + clean worktree + terminal/no task → removed
//	    (worktree gone AND branch gone)
//	(b) merged branch + dirty worktree → skipped untouched
//	(c) merged branch + live task referencing the branch → skipped
//	(d) bond recommitted after the merge (branch no longer in master)
//	    → skipped untouched

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// mergedCleanupFixture is a real git world: a main checkout (on master)
// with a feature branch MERGED into master AND still checked out in a
// linked worktree (the exact frozen shape the bug leaves behind: merge
// landed, worktree never removed, branch -d refused).
type mergedCleanupFixture struct {
	t          *testing.T
	root       string
	clientDir  string // main checkout (master)
	wtDir      string // linked worktree on the merged feature branch
	featBranch string
}

// mergedCleanupGit runs git in dir with the sandbox identity.
func mergedCleanupFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// newMergedCleanupFixture builds the frozen shape:
//   - clientDir: main checkout, master with a base commit
//   - featBranch: one feature commit, MERGED into master (merge commit),
//     then checked out in a linked worktree — branch -d would refuse
//     ("checked out at") and the worktree is what the sweep must remove.
func newMergedCleanupFixture(t *testing.T) *mergedCleanupFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	f := &mergedCleanupFixture{
		t:          t,
		root:       root,
		clientDir:  filepath.Join(root, "client"),
		wtDir:      filepath.Join(root, "wt"),
		featBranch: "feat/merged-work",
	}
	git := func(dir string, args ...string) string {
		return mergedCleanupFixtureGit(t, dir, args...)
	}
	git(root, "init", f.clientDir)
	git(f.clientDir, "config", "user.email", "t@t")
	git(f.clientDir, "config", "user.name", "t")
	git(f.clientDir, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(f.clientDir, "base.txt"), []byte("base\n"), 0o644)
	git(f.clientDir, "add", "base.txt")
	git(f.clientDir, "commit", "-m", "base")
	// feature branch: one commit
	git(f.clientDir, "checkout", "-b", f.featBranch)
	os.WriteFile(filepath.Join(f.clientDir, "feat.txt"), []byte("feat\n"), 0o644)
	git(f.clientDir, "add", "feat.txt")
	git(f.clientDir, "commit", "-m", "feat work")
	// merge INTO master (the serve merge path's outcome) — master now
	// contains the branch tip.
	git(f.clientDir, "checkout", "master")
	git(f.clientDir, "merge", "--no-ff", "-m", "merge "+f.featBranch, f.featBranch)
	// THEN the branch gets checked out in a linked worktree (the frozen
	// shape: the merge path could not branch -d it afterwards).
	git(f.clientDir, "worktree", "add", f.wtDir, f.featBranch)
	// master is still the main checkout.
	git(f.clientDir, "checkout", "master")
	return f
}

// mergedCleanupFixtureSeedLedger seeds a terminal task row referencing the
// branch via deliverable.branch (the executor-backfill shape).
func mergedCleanupSeedTerminalRow(t *testing.T, db string, branch string) {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	deliverable := `{"repo":"/tmp/x","branch":"` + branch + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'completed', ?, 'auto', ?, ?)`,
		"t-clean-terminal", "deliverable done", deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed terminal row: %v", err)
	}
}

// mergedCleanupSeedLiveRow seeds a LIVE (in_progress) task row referencing
// the branch via deliverable.branch.
func mergedCleanupSeedLiveRow(t *testing.T, db string, branch string) {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	deliverable := `{"repo":"/tmp/x","branch":"` + branch + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, 'auto', ?, ?)`,
		"t-clean-live", "live bond working", deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed live row: %v", err)
	}
}

// mergedCleanupRun runs the sweep against the fixture repo in the PRODUCTION
// disposition (sandbox ledger isolated via GOPI_MEMORY_DB): ReportOnly, because
// the reaper's mutating arms are retired (epic sweep-report-not-act,
// 2026-09-19). The guard ladder is identical — only the disposal is gone.
func mergedCleanupRun(t *testing.T, f *mergedCleanupFixture) *MergedCleanupReport {
	t.Helper()
	return MergedCleanupSweep(MergedCleanupOptions{Repo: f.clientDir, ReportOnly: true})
}

// assertBranchPresent asserts the branch ref SURVIVES the pass. It replaces the
// old assertBranchGone: the reaper mutates no ref, so every fixture branch that
// used to be deleted must now still be there, at its own tip (converted, not
// deleted — epic sweep-report-not-act, 2026-09-19).
func assertBranchPresent(t *testing.T, dir, branch string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", branch).CombinedOutput(); err != nil {
		t.Errorf("branch %s must SURVIVE the pass (the reaper mutates no ref): %v: %s", branch, err, out)
	}
}

// TestMergedCleanupReportsFrozenPair is epic case (a), CONVERTED (epic
// sweep-report-not-act, 2026-09-19): merged branch + clean worktree + terminal
// task → the pair is REPORTED (with its owner evidence) and NOTHING is taken
// away. It used to assert "worktree removed AND branch deleted"; the guard
// ladder is unchanged, the disposal is retired.
func TestMergedCleanupReportsFrozenPair(t *testing.T) {
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	mergedCleanupSeedTerminalRow(t, db, f.featBranch)

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("the reaper mutates nothing: removed=%+v", rep.Removed)
	}
	if len(rep.Reported) != 1 {
		t.Fatalf("want the pair REPORTED, got reported=%+v skipped=%+v failed=%+v", rep.Reported, rep.Skipped, rep.Failed)
	}
	if rep.Reported[0].Branch != f.featBranch || rep.Reported[0].Worktree != f.wtDir {
		t.Errorf("report entry = %+v, want branch %q worktree %q", rep.Reported[0], f.featBranch, f.wtDir)
	}
	if !strings.Contains(rep.Reported[0].Detail, repoSweepOwnerEvidenceLabel) {
		t.Errorf("the report must carry owner evidence: %s", rep.Reported[0].Detail)
	}
	// Physical truth: worktree AND branch SURVIVE (the disposal is retired).
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("the worktree must survive the pass: %v", err)
	}
	assertBranchPresent(t, f.clientDir, f.featBranch)
}

// TestMergedCleanupNoTaskRowStillRemoves: no ledger row at all (the pure
// serve-merge shape — a branch merged by the audit/executor with no
// deliverable backfill) is still safe to clean: the branch is IN master by
// construction, so no further work can be owed through it.
func TestMergedCleanupNoTaskRowStillReported(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("the reaper mutates nothing: removed=%+v", rep.Removed)
	}
	if len(rep.Reported) != 1 {
		t.Fatalf("want the pair reported with no task row, got reported=%+v skipped=%+v failed=%+v", rep.Reported, rep.Skipped, rep.Failed)
	}
	if !strings.Contains(rep.Reported[0].Detail, repoSweepOwnerNoneText) {
		t.Errorf("with no row naming the branch the evidence must say so: %s", rep.Reported[0].Detail)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("the worktree must survive the pass: %v", err)
	}
	assertBranchPresent(t, f.clientDir, f.featBranch)
}

// TestMergedCleanupDirtyWorktreeSkipped is epic case (b): merged branch +
// DIRTY worktree → skipped untouched (a live bond is writing there).
func TestMergedCleanupDirtyWorktreeSkipped(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	os.WriteFile(filepath.Join(f.wtDir, "wip.txt"), []byte("live bond writing"), 0o644)

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("dirty worktree must not be removed: %+v", rep.Removed)
	}
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Detail, "dirty") {
		t.Fatalf("want one dirty skip, got skipped=%+v", rep.Skipped)
	}
	// Physical truth: worktree AND branch survive.
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("worktree must survive a dirty skip: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive a dirty skip")
	}
}

// TestMergedCleanupLiveCWDWorktreeSkipped is the third-guarded-site case
// (2026-09-10 liveness incident): the ledger says the pair is free and the
// branch is merged, but a LIVE PROCESS still has its CWD inside the
// worktree — the sweep must skip it (reported, never silent) instead of
// tearing the directory out from under that process.
func TestMergedCleanupLiveCWDWorktreeSkipped(t *testing.T) {
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	// No ledger row at all: the pair is otherwise fully removable — only the
	// live CWD holds it back.
	pid := liveCwdHolder(t, f.wtDir)

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("worktree in use must not be removed: %+v", rep.Removed)
	}
	if len(rep.Skipped) != 1 {
		t.Fatalf("want one liveness skip, got skipped=%+v failed=%+v", rep.Skipped, rep.Failed)
	}
	if !strings.Contains(rep.Skipped[0].Detail, finishCleanupInUseMarker) ||
		!strings.Contains(rep.Skipped[0].Detail, strconv.Itoa(pid)) {
		t.Errorf("skip must name the marker and holder pid, got %q", rep.Skipped[0].Detail)
	}
	// Physical truth: the directory the live process works in SURVIVES.
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("worktree in use must survive the sweep: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive a liveness skip")
	}
}

// TestMergedCleanupLiveTaskSkipped is epic case (c): merged branch + LIVE
// ledger task (in_progress) referencing the branch → skipped untouched.
func TestMergedCleanupLiveTaskSkipped(t *testing.T) {
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	mergedCleanupSeedLiveRow(t, db, f.featBranch)

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("live task must protect the branch: %+v", rep.Removed)
	}
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Detail, "live task") {
		t.Fatalf("want one live-task skip, got skipped=%+v", rep.Skipped)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("worktree must survive a live-task skip: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive a live-task skip")
	}
}

// TestMergedCleanupRecommitAfterMergeSkipped is epic case (d): a bond
// commits AGAIN to an already-merged branch (continuing work after its
// task closed) — the branch ref moves off master, so the race-free
// ancestor recheck skips it untouched.
func TestMergedCleanupRecommitAfterMergeSkipped(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))

	// The bond recommits INSIDE the worktree — the branch ref and the
	// worktree HEAD move in lockstep off master.
	os.WriteFile(filepath.Join(f.wtDir, "more.txt"), []byte("continuing work\n"), 0o644)
	git := func(dir string, args ...string) string {
		return mergedCleanupFixtureGit(t, dir, args...)
	}
	git(f.wtDir, "add", "more.txt")
	git(f.wtDir, "commit", "-m", "continuing work after merge")

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("recommitted branch must not be removed: %+v", rep.Removed)
	}
	// The recommit moved the branch off master — coarse discovery
	// (`branch --merged master`) no longer lists it, so the sweep never
	// even reaches the guards: an empty pass is the correct outcome.
	if len(rep.Skipped) != 0 || len(rep.Failed) != 0 {
		t.Fatalf("recommitted branch should not appear at all: skipped=%+v failed=%+v", rep.Skipped, rep.Failed)
	}
	// Physical truth: the new commit and the branch survive untouched.
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("worktree must survive a recommit skip: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive a recommit skip")
	}
	if out := git(f.clientDir, "log", "--oneline", "-1", f.featBranch); !strings.Contains(out, "continuing work after merge") {
		t.Errorf("recommit must be untouched:\n%s", out)
	}
}

// TestMergedCleanupStillMergedRecheck: the race-free ④ recheck — the
// guard that fires when a bond recommits BETWEEN the coarse discovery
// snapshot and the removal decision (the branch was merged at discovery,
// then moved off master). A merged branch passes (""), a moved branch is
// refused with the protecting reason.
func TestMergedCleanupStillMergedRecheck(t *testing.T) {
	f := newMergedCleanupFixture(t)
	if reason := mergedCleanupStillMergedReason(f.clientDir, f.featBranch); reason != "" {
		t.Fatalf("merged branch must pass the recheck, got %q", reason)
	}
	// Move the branch off master (recommit in the worktree).
	os.WriteFile(filepath.Join(f.wtDir, "more.txt"), []byte("continuing\n"), 0o644)
	git := func(dir string, args ...string) string {
		return mergedCleanupFixtureGit(t, dir, args...)
	}
	git(f.wtDir, "add", "more.txt")
	git(f.wtDir, "commit", "-m", "continuing after merge")
	if reason := mergedCleanupStillMergedReason(f.clientDir, f.featBranch); reason == "" {
		t.Fatalf("moved branch must be refused by the recheck")
	}
}

// TestMergedCleanupExemptBranches: master/main/archive are never cleaned —
// the finish-cleanup exemption set is shared.
func TestMergedCleanupExemptBranches(t *testing.T) {
	if !mergedCleanupBranchExempt("", "master") || !mergedCleanupBranchExempt("", "archive/loser-20260902") {
		t.Fatalf("exempt branches must be exempt")
	}
	if mergedCleanupBranchExempt("", "feat/real-work") {
		t.Fatalf("feature branch must NOT be exempt")
	}
}

// TestArchiveNamespace_SingleSource (hardcoding regression, feat/
// archive-namespace-single-source): the loser-archive namespace constants
// archiveBranchPrefix + archiveTagRefPrefix live ONLY in tasks.go, and every
// consumer face derives from them:
//   - the exported aliases cmd/agent consumes (branch-scan reachability)
//   - the finish-cleanup exemption (finishCleanupBranchExempt)
//   - the safe-merge guard blacklist (mergeExecBlacklistPrefixes)
//
// A mirror literal reintroduced in any consumer makes one of these asserts
// fail (safe-merge vs finish-cleanup drift would merge or clean an archived
// loser — Rule 9 / safe-merge.sh semantics).
func TestArchiveNamespace_SingleSource(t *testing.T) {
	if archiveBranchPrefix != "archive/" {
		t.Fatalf("archiveBranchPrefix = %q, want archive/ (the namespace contract)", archiveBranchPrefix)
	}
	if archiveTagRefPrefix != "refs/tags/archive/" {
		t.Fatalf("archiveTagRefPrefix = %q, want refs/tags/archive/", archiveTagRefPrefix)
	}
	// Exported aliases the cmd/agent branch-scan face consumes.
	if ArchiveBranchPrefix != archiveBranchPrefix || ArchiveTagRefPrefix != archiveTagRefPrefix {
		t.Fatalf("exported aliases must alias the private constants")
	}
	// finish-cleanup exemption derives from the single source.
	if !finishCleanupBranchExempt("", "archive/loser-20260902") {
		t.Fatalf("finish-cleanup must exempt archive/ branches (single-source derive)")
	}
	// safe-merge guard blacklist derives its archive member from the same
	// source — guard and exemption cannot drift apart.
	var blacklisted bool
	for _, p := range strings.Split(mergeExecBlacklistPrefixes, ",") {
		if strings.HasPrefix("archive/loser", p) {
			blacklisted = true
		}
	}
	if !blacklisted {
		t.Fatalf("safe-merge blacklist %q must contain an archive/ member deriving from archiveBranchPrefix", mergeExecBlacklistPrefixes)
	}
}

// TestArchiveNamespace_MirrorLiteralGate greps the production Go sources of
// internal/tools + cmd/agent for a reintroduced "refs/tags/archive/" or
// "archive/" namespace LITERAL outside internal/tools/tasks.go — the
// grep-gate analogue of aa517b1's single-literal gate. A future namespace
// rename (archive → loser-archive) is a one-line change in tasks.go instead
// of an N-site grep-and-replace.
func TestArchiveNamespace_MirrorLiteralGate(t *testing.T) {
	// package under test = internal/tools; `go test` runs with cwd =
	// internal/tools. Scan internal/tools (cwd) + cmd/agent (../../cmd/agent).
	toolsDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	agentDir, err := filepath.Abs(filepath.Join("..", "..", "cmd", "agent"))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	scan := []struct{ dir, pkg string }{
		{toolsDir, "internal/tools"},
		{agentDir, "cmd/agent"},
	}
	for _, s := range scan {
		entries, err := os.ReadDir(s.dir)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", s.dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			// The ONE allowed holder is internal/tools/tasks.go — every other
			// production file (incl. every cmd/agent file) must import the
			// exported constants, never carry a namespace literal.
			if s.pkg == "internal/tools" && e.Name() == "tasks.go" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
			if err != nil {
				t.Fatalf("ReadFile %s: %v", e.Name(), err)
			}
			for _, lit := range []string{`"refs/tags/archive/"`, `"archive/"`} {
				if bytes.Contains(b, []byte(lit)) {
					t.Errorf("mirror literal %s in %s — archive namespace must live only in internal/tools/tasks.go", lit, filepath.Join(s.pkg, e.Name()))
				}
			}
		}
	}
}

// TestMergedCleanupSweepClockFree: the sweep itself is pure (no throttle
// clock inside — the serve face owns cadence). Running it twice on an
// already-clean repo is a no-op, never an error.
func TestMergedCleanupIdempotentSecondRun(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	rep1 := mergedCleanupRun(t, f)
	if len(rep1.Reported) != 1 {
		t.Fatalf("first run must report the pair: %+v", rep1)
	}
	rep2 := mergedCleanupRun(t, f)
	// A reported pair is NOT consumed: nothing was taken away, so the second run
	// reports the same pair again (the honest post-retirement behaviour — a
	// report repeats until a human or the owner chain resolves it).
	if len(rep2.Reported) != 1 || len(rep2.Failed) != 0 {
		t.Fatalf("second run must report the same pair: reported=%+v failed=%+v", rep2.Reported, rep2.Failed)
	}
	if len(rep2.Removed) != 0 {
		t.Fatalf("the reaper mutates nothing: removed=%+v", rep2.Removed)
	}
}

// TestMergedCleanupForeignOwnedWorktreeSkipped (2026-09-10 epic, gate ⑤c):
// the sweep's removal target comes from git's branch→worktree map, which
// carries no ownership — a directory ANOTHER row recorded as its
// deliverable home (here a TERMINAL row in a different repo, so neither the
// live-ledger guard ⑤ nor the dirty/merged guards protect it) must be
// refused with the owner named, directory and branch intact.
func TestMergedCleanupForeignOwnedWorktreeSkipped(t *testing.T) {
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	// Terminal row (guard ⑤ only protects LIVE rows) in ANOTHER repo (the
	// re-attached-directory shape: the directory is this row's deliverable
	// home, the branch is this repo's).
	deliverable := `{"repo":"/somewhere/else","worktree":"` + f.wtDir + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'completed', ?, 'auto', ?, ?)`,
		"t-wt-owner", "owns the worktree", deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed owner row: %v", err)
	}
	conn.Close() //stylegate:ignore transport teardown on a connection already reported as failed

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("another row's worktree must never be removed: %+v", rep.Removed)
	}
	// Removal-not-preferred (2026-09-12): a row-owned worktree is RELEASED BY
	// ITS OWNER'S DELIVERY CHAIN, not by this sweep's blind branch -d. The
	// guarantee becomes stronger than the old flat skip: the sweep names the
	// row that owns the directory, reports what happened, and never removes
	// (or branch-deletes) it behind that row's back.
	if len(rep.Skipped) != 0 {
		t.Fatalf("a row-owned worktree is handed over, not skipped: %+v", rep.Skipped)
	}
	if len(rep.Notes) != 1 {
		t.Fatalf("want one handover note, got notes=%+v skipped=%+v failed=%+v", rep.Notes, rep.Skipped, rep.Failed)
	}
	if !strings.Contains(rep.Notes[0].Detail, finishCleanupSweepNotPreferred) ||
		!strings.Contains(rep.Notes[0].Detail, "t-wt-owner") {
		t.Errorf("note must mark the handover and name the owner row: %q", rep.Notes[0].Detail)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("foreign-owned worktree must survive the sweep: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive the ownership skip")
	}
}

// TestMergedCleanupLiveMergeSummaryMatch: a live row whose SUMMARY parses
// as "merge <branch> into master" (the audit's shape — no deliverable
// backfill yet) also protects the branch.
func TestMergedCleanupLiveMergeSummaryMatch(t *testing.T) {
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, source, created_at, updated_at)
		 VALUES (?, ?, 'pending', 'auto', ?, ?)`,
		"t-clean-summary", "merge "+f.featBranch+" into master", nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed summary row: %v", err)
	}

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("live merge-summary row must protect the branch: %+v", rep.Removed)
	}
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Detail, "live task") {
		t.Fatalf("want one live-task skip, got skipped=%+v", rep.Skipped)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("worktree must survive: %v", err)
	}
}

// --- 2026-09-12 stranded-worktree epic: the sweep's half. ---

// TestMergedCleanupHandsRowOwnedWorktreeToItsOwner asserts the
// removal-not-preferred rule: a directory some row recorded is NOT removed
// by this sweep's blind branch -d — it is handed to the owner's chain (the
// wired remover), which keeps the branch (the merger's input) and reports
// what happened.
func TestMergedCleanupHandsRowOwnedWorktreeToItsOwner(t *testing.T) {
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	ownershipSeedRow(t, db, "t-owner", TaskStatusCompleted, "", f.wtDir)

	var asked []string
	SetFinishCleanupSweepOwnerRemover(func(wt string) SweepOwnerRemoval {
		asked = append(asked, wt)
		if err := os.RemoveAll(wt); err != nil {
			return SweepOwnerRemoval{Detail: err.Error()}
		}
		return SweepOwnerRemoval{Removed: true, Detail: "owner chain release"}
	})
	t.Cleanup(func() { SetFinishCleanupSweepOwnerRemover(nil) })

	rep := mergedCleanupRun(t, f)
	if len(asked) != 1 || asked[0] != f.wtDir {
		t.Fatalf("the sweep must hand the row-owned worktree to its owner chain, asked=%v", asked)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("the sweep itself must not remove a row-owned worktree: %+v", rep.Removed)
	}
	if len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0].Detail, finishCleanupSweepNotPreferred) ||
		!strings.Contains(rep.Notes[0].Detail, "t-owner") {
		t.Fatalf("the handover must be reported with the owner row: %+v", rep.Notes)
	}
	// The owner chain removed the directory but kept the branch (the server
	// arm's contract) — the sweep must not then delete that branch.
	if out, err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).CombinedOutput(); err != nil {
		t.Errorf("the branch must survive the handover (merger input): %s", out)
	}
}

// TestMergedCleanupStaleOwnerRowIsReleased is the sweep-side regression of
// the stranded shape: a merged branch whose worktree is bound to a row whose
// holder is GONE. Before the fix the sweep refused outright ("owned by row
// … (cancelled, claimed-by pid … gone)") and nothing ever released the
// directory — the sweep scan (completed/failed only) never even reached the
// cancelled owner. Now the binding is classified stale: reported as a note,
// and the directory released via the owner chain, with the branch preserved.
func TestMergedCleanupStaleOwnerRowIsReleased(t *testing.T) {
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	staleOwnerRow(t, db, "t-stale-owner", TaskStatusCancelled, f.wtDir)

	block, note := finishCleanupForeignOwnerReason([]TaskWorktreeRow{
		{TaskID: "t-stale-owner", Status: TaskStatusCancelled,
			ClaimedBy: "zbook:" + strconv.Itoa(deadOwnerPid), Deliverable: f.wtDir, HolderLive: false},
	}, f.wtDir)
	if block != "" {
		t.Fatalf("a provably-dead holder must not block the sweep: %q", block)
	}
	if !strings.Contains(note, finishCleanupOwnerStaleMarker) {
		t.Fatalf("the stale binding must be reported: %q", note)
	}

	SetFinishCleanupSweepOwnerRemover(func(wt string) SweepOwnerRemoval {
		if err := os.RemoveAll(wt); err != nil {
			return SweepOwnerRemoval{Detail: err.Error()}
		}
		return SweepOwnerRemoval{Removed: true, Detail: "owner chain release"}
	})
	t.Cleanup(func() { SetFinishCleanupSweepOwnerRemover(nil) })

	rep := mergedCleanupRun(t, f)
	if len(rep.Skipped) != 0 {
		t.Fatalf("a stale binding must not produce an ownership skip: %+v", rep.Skipped)
	}
	if _, err := os.Stat(f.wtDir); !os.IsNotExist(err) {
		t.Errorf("the stale-owned worktree must be released: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive (merger input): %v", err)
	}
}

// TestMergedCleanupOrphanWorktreeStillRemoved guards the other side: a
// worktree NO row records (no owner to hand it to) is REPORTED — the sweep
// takes nothing away (converted from "…StillRemoved", epic sweep-report-not-act,
// 2026-09-19).
func TestMergedCleanupOrphanWorktreeStillReported(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	t.Cleanup(func() { SetFinishCleanupSweepOwnerRemover(nil) })
	SetFinishCleanupSweepOwnerRemover(func(string) SweepOwnerRemoval {
		t.Errorf("an orphan worktree must not be handed to an owner chain")
		return SweepOwnerRemoval{}
	})

	rep := mergedCleanupRun(t, f)
	if len(rep.Reported) != 1 {
		t.Fatalf("an orphan worktree is this sweep's to REPORT: %+v (notes=%+v)", rep.Reported, rep.Notes)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("the reaper mutates nothing: removed=%+v", rep.Removed)
	}
	assertBranchPresent(t, f.clientDir, f.featBranch)
}

// TestMergedCleanupLiveCwdHolderKeepsRowOwnedWorktree is the ACTIVE-WORKTREE
// SAFETY case of the epic: a dir that a live process sits inside is never
// touched — not by the sweep, and not by the owner handover either, even
// though the row's record is stale. Without the live-CWD guard the new path
// would remove a directory under a running session (data loss).
func TestMergedCleanupLiveCwdHolderKeepsRowOwnedWorktree(t *testing.T) {
	f := newMergedCleanupFixture(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	liveCwdHolder(t, f.wtDir)
	SetFinishCleanupSweepOwnerRemover(func(string) SweepOwnerRemoval {
		t.Errorf("the owner chain must never be asked to release a worktree in use")
		return SweepOwnerRemoval{}
	})
	t.Cleanup(func() { SetFinishCleanupSweepOwnerRemover(nil) })

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("a worktree with a live CWD holder must never be removed: %+v", rep.Removed)
	}
	found := false
	for _, s := range rep.Skipped {
		if strings.Contains(s.Detail, finishCleanupInUseMarker) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the live-CWD refusal must be reported: skipped=%+v failed=%+v", rep.Skipped, rep.Failed)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("the live worktree must survive: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive: %v", err)
	}
}
