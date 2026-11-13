package tools

import (
	"context"
	"github.com/nick/gopi/internal/scratch"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/worktreeroots"
)

// sweepFixtureRoot returns a declared root for the sweeper's tests: a real
// directory on the suite's scratch tree, pinned through the ONE sandbox hook so
// no test can reach a real workspace.
func sweepFixtureRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "declared-root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	return root
}

// sweepFixtureReg builds one linked registration under a declared root.
func sweepFixtureReg(root, name, branch string) worktreeRegistration {
	return worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, name), Branch: branch}
}

// sweepOptionsNow pins the clock so age reasoning is deterministic.
func sweepOptionsNow(dryRun bool) worktreeSweepOptions {
	return worktreeSweepOptions{DryRun: dryRun, MinAge: DefaultWorktreeSweepMinAge, Max: DefaultWorktreeSweepMax, Now: time.Now()}
}

// TestSweepReclaimsStaleUnownedRegistration is the core disposition: an
// in-root, unheld, unowned, clean, old registration is reclaimed.
func TestSweepReclaimsStaleUnownedRegistration(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := sweepFixtureReg(root, "gopi-old-task", "feat/old")
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReclaim) {
		t.Fatalf("disposition = %s (%s), want reclaim", f.Disposition, f.Reason)
	}
	if f.AgeHours < 71 || f.AgeHours > 73 {
		t.Errorf("AgeHours = %d, want ~72", f.AgeHours)
	}
}

// TestSweepKeepsRegistrationHeldByALiveProcess: the holder pass is the guard
// that makes an unattended sweep survivable — a live process's tree is never
// removed regardless of what the ledger or the clock say.
func TestSweepKeepsRegistrationHeldByALiveProcess(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := sweepFixtureReg(root, "gopi-live-suite", "feat/live")
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-1000 * time.Hour)}

	holders := scratch.HolderScan{Paths: []string{filepath.Join(reg.Path, "var", "tmp")}}
	f := worktreeSweepJudge(reg, nil, holders, roots, opts, ho)
	if f.Disposition != string(worktreeKeep) {
		t.Fatalf("disposition = %s (%s), want keep", f.Disposition, f.Reason)
	}
	if !strings.Contains(f.Reason, "held by a live process") {
		t.Errorf("reason does not name the holder guard: %s", f.Reason)
	}

	// The neighbouring spelling must NOT veto (a prefix string match would).
	sibling := []string{reg.Path + "-elsewhere"}
	if f := worktreeSweepJudge(reg, nil, scratch.HolderScan{Paths: sibling}, roots, opts, ho); f.Disposition != string(worktreeReclaim) {
		t.Fatalf("a sibling path vetoed the reclaim: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepKeepsRegistrationOwnedByLiveTask: a non-terminal row owning the
// worktree (or its branch) protects it; the SAME row once terminal stops
// protecting it — which is what makes this a stale-registration sweeper rather
// than a duplicate of the dead-holder reclaim.
func TestSweepKeepsRegistrationOwnedByLiveTask(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := sweepFixtureReg(root, "gopi-live-task", "feat/live-task")
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	live := []tasksRowForSweep{{TaskID: "T-1", Status: taskStatusInProgress, Worktree: reg.Path, Branch: reg.Branch}}
	f := worktreeSweepJudge(reg, live, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeKeep) || !strings.Contains(f.Reason, "T-1") {
		t.Fatalf("a live task's worktree was not kept: %s (%s)", f.Disposition, f.Reason)
	}

	// Ownership by BRANCH alone also protects (the deliverable may record a
	// tilde spelling of the path that does not compare equal).
	byBranch := []tasksRowForSweep{{TaskID: "T-2", Status: taskStatusClaimed, Branch: reg.Branch}}
	if f := worktreeSweepJudge(reg, byBranch, scratch.HolderScan{}, roots, opts, ho); f.Disposition != string(worktreeKeep) {
		t.Fatalf("a claimed task's branch did not protect its worktree: %s", f.Disposition)
	}

	// Terminal: protection ends, and the reclaim names the task.
	terminal := []tasksRowForSweep{{TaskID: "T-3", Status: taskStatusCompleted, Worktree: reg.Path, Branch: reg.Branch}}
	f = worktreeSweepJudge(reg, terminal, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReclaim) || !strings.Contains(f.Reason, "T-3") {
		t.Fatalf("a terminal task's stale worktree was not reclaimed: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepReportsDirtyWorktreeInsteadOfRemovingIt: uncommitted work is a
// deliverable, not garbage — it is reported and left alone.
//
// The fixture carries an OWNING row on purpose (epic worktree-reap-reachable):
// a dirty registration whose owner the ledger still remembers is somebody's
// unfinished delivery, and its verdict stays the plain report. The UNOWNED
// variant of this same shape — which this test used to build — is the orphan
// the host could never reach; it now escalates instead (see
// TestSweepEscalatesDirtyOrphanAndKeepsTheOwnedDirtyReport). Neither verdict
// removes anything.
func TestSweepReportsDirtyWorktreeInsteadOfRemovingIt(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := sweepFixtureReg(root, "gopi-dirty", "feat/dirty")
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour), DirtyReason: " M internal/x.go"}
	owner := []tasksRowForSweep{{TaskID: "T-dirty", Status: taskStatusCompleted, Worktree: reg.Path, Branch: reg.Branch}}

	f := worktreeSweepJudge(reg, owner, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReport) {
		t.Fatalf("disposition = %s (%s), want report", f.Disposition, f.Reason)
	}
	if !strings.Contains(f.Reason, "uncommitted work") {
		t.Errorf("reason does not name the dirty guard: %s", f.Reason)
	}
	// The never-removed invariant holds for the orphan shape too: the
	// escalation is a report to a human, not a removal.
	if o := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, ho).Disposition; o == string(worktreeReclaim) {
		t.Fatalf("a dirty registration was reclaimed: %s", o)
	}
}

// TestSweepEscalatesDirtyOrphanAndKeepsTheOwnedDirtyReport is the orphan rule
// (epic worktree-reap-reachable, defect 2): a DIRTY registration NO ledger row
// owns is the one shape unreachable by every other arm on the host — the
// producer pass needs a row stamped with this host's own claim, the dead-holder
// arm needs a TERMINAL row, and this sweeper's guard ② refuses dirt. It is
// therefore ESCALATED to a human once it passes the age floor, and still never
// removed. A dirty registration that DOES have an owning row keeps the plain
// report (that is somebody's unfinished delivery, not residue).
func TestSweepEscalatesDirtyOrphanAndKeepsTheOwnedDirtyReport(t *testing.T) {
	root := sweepFixtureRoot(t)
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(false)
	opts.MinAge = time.Hour

	dirty := func(name, branch string) worktreeRegistration { return sweepFixtureReg(root, name, branch) }
	agedDirty := func(reg worktreeRegistration) worktreeSweepHostObservations {
		return worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-24 * time.Hour), DirtyReason: "?? scratch.txt"}
	}

	// ① no row owns it and it is past the age floor → ESCALATE, never remove.
	reg := dirty("gopi-orphan", "fix/orphan")
	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, agedDirty(reg))
	if f.Disposition != string(worktreeEscalateOrphan) {
		t.Fatalf("a dirty registration with no ledger row was not escalated: %s (%s)", f.Disposition, f.Reason)
	}
	if !strings.Contains(f.Reason, worktreeOrphanEscalationMarker) {
		t.Errorf("the escalation must lead with the shared marker %q: %s", worktreeOrphanEscalationMarker, f.Reason)
	}
	if !strings.Contains(f.Reason, reg.Branch) {
		t.Errorf("the escalation must name the branch so an operator can act: %s", f.Reason)
	}
	if !strings.Contains(f.Reason, "--force") {
		t.Errorf("the escalation must say that no --force was used: %s", f.Reason)
	}
	// The false-positive probes: an owner (either status), and a young orphan.
	for _, tc := range []struct {
		name string
		rows []tasksRowForSweep
		age  time.Duration
		want worktreeDisposition
	}{
		{"terminal owner", []tasksRowForSweep{{TaskID: "T-1", Status: taskStatusCompleted, Worktree: reg.Path, Branch: reg.Branch}}, 24 * time.Hour, worktreeReport},
		{"live owner", []tasksRowForSweep{{TaskID: "T-2", Status: taskStatusInProgress, Worktree: reg.Path}}, 24 * time.Hour, worktreeKeep},
		{"young orphan", nil, 1 * time.Minute, worktreeReport},
	} {
		ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-tc.age), DirtyReason: " M x.go"}
		if got := worktreeSweepJudge(reg, tc.rows, scratch.HolderScan{}, roots, opts, ho).Disposition; got != string(tc.want) {
			t.Errorf("%s: disposition = %s, want %s", tc.name, got, tc.want)
		}
	}
	// Regression: the CLEAN unowned arm is untouched by this rule.
	clean := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-24 * time.Hour)}
	if got := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, clean).Disposition; got != string(worktreeReclaim) {
		t.Fatalf("the clean unowned reclaim regressed: %s", got)
	}
	// Regression: an UNMEASURABLE age keeps the plain report — "nobody owns
	// this and I cannot say how long" is not an escalation.
	unmeasurable := worktreeSweepHostObservations{DirtyReason: "?? scratch.txt"}
	if got := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, unmeasurable).Disposition; got != string(worktreeReport) {
		t.Fatalf("an undatable dirty orphan escalated: %s", got)
	}
}

// TestSweepKeepsYoungAndUnmeasurableRegistrations: the age threshold and the
// fail-closed unmeasurable case.
func TestSweepKeepsYoungAndUnmeasurableRegistrations(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := sweepFixtureReg(root, "gopi-young", "feat/young")
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)

	young := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-1 * time.Hour)}
	if f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, young); f.Disposition != string(worktreeKeep) {
		t.Fatalf("a 1h-old registration was not kept: %s", f.Disposition)
	}
	// Unmeasurable age: KEEP, with the reason naming the missing record.
	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, worktreeSweepHostObservations{})
	if f.Disposition != string(worktreeKeep) || !strings.Contains(f.Reason, "could not be measured") {
		t.Fatalf("an unmeasurable age did not keep the registration: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepReportsOutOfRootAndNeverTouchesTheMainCheckout: the two "not mine to
// delete" cases. An out-of-root registration is REPORTED (disk the operator
// should see), and the main checkout is never a target at all.
func TestSweepReportsOutOfRootAndNeverTouchesTheMainCheckout(t *testing.T) {
	root := sweepFixtureRoot(t)
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	old := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-1000 * time.Hour)}

	outside := worktreeRegistration{Repo: "/fixture/repo", Path: "/dev/shm/gopi-adhoc", Branch: "feat/adhoc"}
	f := worktreeSweepJudge(outside, nil, scratch.HolderScan{}, roots, opts, old)
	if f.Disposition != string(worktreeReport) {
		t.Fatalf("an out-of-root registration was %s (%s), want report — sweeping it would exceed the declared policy", f.Disposition, f.Reason)
	}

	main := sweepFixtureReg(root, "", "")
	main.Main = true
	main.Path = root
	if f := worktreeSweepJudge(main, nil, scratch.HolderScan{}, roots, opts, old); f.Disposition != string(worktreeKeep) || !strings.Contains(f.Reason, "main checkout") {
		t.Fatalf("the main checkout was not kept: %s (%s)", f.Disposition, f.Reason)
	}

	// git's own flags are honoured: a locked registration whose directory is
	// still THERE stays put (an explicit do-not-touch), while a prunable one —
	// git's word for "this registration's directory is gone" — is the leak this
	// sweeper exists for and is reaped. It is NOT "the repo prune collects it":
	// prune SKIPS a locked entry, so the prunable+locked shape survives forever
	// (measured on this host, twice).
	locked := sweepFixtureReg(root, "gopi-locked", "feat/locked")
	locked.Locked = true
	if f := worktreeSweepJudge(locked, nil, scratch.HolderScan{}, roots, opts, old); f.Disposition != string(worktreeKeep) {
		t.Fatalf("a locked registration was %s", f.Disposition)
	}
	prunable := sweepFixtureReg(root, "gopi-prunable", "feat/prunable")
	prunable.Prunable = true
	prunable.Branch = "" // a prunable registration carries no branch evidence
	if f := worktreeSweepJudge(prunable, nil, scratch.HolderScan{}, roots, opts, old); f.Disposition != string(worktreeReap) {
		t.Fatalf("a prunable (dir-gone) registration was %s (%s), want reap — prune cannot collect it", f.Disposition, f.Reason)
	}
}

// TestSweepRegistrationListingParsesPorcelain drives the REAL parser over a
// fixture repo with two linked worktrees: the FIRST entry is the main checkout,
// branch refs are normalized, and a detached registration is recognized.
func TestSweepRegistrationListingParsesPorcelain(t *testing.T) {
	repo := sweepInitRepo(t)
	root := sweepFixtureRoot(t)

	first := filepath.Join(root, "wt-one")
	second := filepath.Join(root, "wt-two")
	sweepGit(t, repo, "worktree", "add", first, "-b", "feat/one")
	sweepGit(t, repo, "worktree", "add", second, "-b", "feat/two")
	sweepGit(t, repo, "worktree", "add", "--detach", filepath.Join(root, "wt-detached"))

	regs, err := worktreeSweepRegistrations(repo)
	if err != nil {
		t.Fatalf("worktreeSweepRegistrations: %v", err)
	}
	if len(regs) != 4 {
		t.Fatalf("parsed %d registrations (%+v), want 4 (main + 3 linked)", len(regs), regs)
	}
	if !regs[0].Main {
		t.Fatalf("the FIRST registration (%q) is not marked as the main checkout", regs[0].Path)
	}
	for _, reg := range regs[1:] {
		if reg.Main {
			t.Errorf("linked registration %q marked as main", reg.Path)
		}
	}
	branches := map[string]string{}
	for _, reg := range regs[1:] {
		branches[reg.Path] = reg.Branch
	}
	if branches[first] != "feat/one" || branches[second] != "feat/two" {
		t.Fatalf("branch parsing wrong: %v (want the refs/heads/ prefix stripped)", branches)
	}
	// git orders `worktree list` by path, not by creation order, so the
	// detached entry is found by PATH.
	var detached *worktreeRegistration
	for i := range regs {
		if strings.HasSuffix(regs[i].Path, "wt-detached") {
			detached = &regs[i]
		}
	}
	if detached == nil {
		t.Fatalf("the detached registration is missing from %+v", regs)
	}
	if !detached.Detached || detached.Branch != "" {
		t.Errorf("detached registration = %+v, want Detached with no branch", *detached)
	}
}

// TestSweepCreatedAtUsesTheRegistrationRecord: the age signal must come from
// the REGISTRATION's own git record, because the directory mtime is refreshed by
// any access and therefore cannot answer "when was this created".
//
// SPECIFICALLY NOT the main repo's `commondir` (corrected 2026-09-16): for a
// worktree of the main repository that file holds the MAIN repo's git dir, so
// its mtime is the repository's age, not the worktree's — a registration
// created seconds ago read as months old, which is a removal licence the age
// floor exists to deny. The test therefore asserts the age is RECENT even with
// the main repo's commondir artificially aged to a year ago.
func TestSweepCreatedAtUsesTheRegistrationRecord(t *testing.T) {
	repo := sweepInitRepo(t)
	root := sweepFixtureRoot(t)
	wt := filepath.Join(root, "wt-age")
	sweepGit(t, repo, "worktree", "add", wt, "-b", "feat/age")

	// The main repo's commondir, aged to a year ago: the exact input that made
	// a fresh worktree read as ancient.
	mainCommondir := filepath.Join(repo, ".git", worktreeSweepCommondirName)
	ancient := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(mainCommondir, ancient, ancient); err != nil {
		t.Skipf("chtimes unavailable: %v", err)
	}

	created, err := worktreeSweepCreatedAt(wt)
	if err != nil {
		t.Fatalf("worktreeSweepCreatedAt: %v", err)
	}
	if time.Since(created) > time.Hour {
		t.Fatalf("creation time %v is not the registration time (should be seconds ago): the age was read from the "+
			"MAIN repo's commondir (%v), which is the repository's age — a fresh worktree must never read as old "+
			"enough for the age floor to allow its removal", created, mainCommondir)
	}

	// Touching the DIRECTORY must not move the creation record — that is the
	// whole reason the age is read from the git-internal file instead.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(wt, future, future); err != nil {
		t.Skipf("chtimes unavailable: %v", err)
	}
	again, err := worktreeSweepCreatedAt(wt)
	if err != nil {
		t.Fatalf("worktreeSweepCreatedAt after touch: %v", err)
	}
	if !again.Equal(created) {
		t.Fatalf("creation time moved with a directory touch: %v → %v (must be the commondir record)", created, again)
	}

	// A directory with no git backlink reports an error (the caller then KEEPS
	// the registration rather than assuming an age).
	if _, err := worktreeSweepCreatedAt(t.TempDir()); err == nil {
		t.Fatal("a non-worktree directory produced a creation time")
	}
}

// TestSweepRunIsPerRepoNotPerRow is the "ONE list, ONE prune per repo" rule,
// proven by instrumenting the two git verbs: with two declared roots holding N
// registrations, the run must list each repo exactly ONCE and prune only a repo
// that actually removed something. A per-row implementation would show a prune
// per row — the misattribution the header warns about.
func TestSweepRunIsPerRepoNotPerRow(t *testing.T) {
	repoA := sweepInitRepo(t)
	repoB := sweepInitRepo(t)
	root := sweepFixtureRoot(t)
	for _, spec := range []struct {
		repo, name string
	}{
		{repoA, "wt-a1"}, {repoA, "wt-a2"}, {repoB, "wt-b1"},
	} {
		if err := os.MkdirAll(filepath.Join(root, spec.name), 0o755); err != nil {
			t.Fatal(err)
		}
		sweepGit(t, spec.repo, "worktree", "add", filepath.Join(root, spec.name), "-b", "feat/"+spec.name)
	}

	// Age every registration past the threshold through the record git wrote.
	old := time.Now().Add(-72 * time.Hour)
	for _, name := range []string{"wt-a1", "wt-a2", "wt-b1"} {
		record := sweepCreatedAtRecordPath(t, filepath.Join(root, name))
		if err := os.Chtimes(record, old, old); err != nil {
			t.Skipf("chtimes unavailable: %v", err)
		}
	}

	ctx := context.Background()
	opts := sweepOptionsNow(false)
	opts.MinAge = time.Hour
	deps := worktreeSweepDeps{
		Repos:      []string{repoA, repoB},
		Rows:       []tasksRowForSweep{{TaskID: "T-old", Status: taskStatusCompleted, Worktree: filepath.Join(root, "wt-a1"), Branch: "feat/wt-a1"}},
		LedgerRead: true,
		Holders:    scratch.HolderScan{},
	}
	calls := withGitCallRecorder(t)
	res := worktreeSweepRunWith(ctx, opts, deps, nil)

	lists := calls.filter("worktree", "list")
	prunes := calls.filter("worktree", "prune")
	if len(lists) != 2 {
		t.Fatalf("worktree list ran %d times (%v), want exactly ONE per repo (2)", len(lists), lists)
	}
	if len(prunes) != 2 {
		t.Fatalf("worktree prune ran %d times (%v), want exactly ONE per repo that removed something (2)", len(prunes), prunes)
	}
	if res.Reclaimed != 3 {
		t.Fatalf("reclaimed %d (%+v), want 3 clean stale registrations", res.Reclaimed, res.Findings)
	}
	// The registrations are really gone (git's own bookkeeping agrees).
	for _, repo := range []string{repoA, repoB} {
		regs, err := worktreeSweepRegistrations(repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(regs) != 1 || !regs[0].Main {
			t.Fatalf("repo %s still has linked registrations: %+v", repo, regs)
		}
	}
}

// TestSweepDryRunChangesNothing: the default posture must be side-effect free —
// the same plan, no removal, no prune.
func TestSweepDryRunChangesNothing(t *testing.T) {
	repo := sweepInitRepo(t)
	root := sweepFixtureRoot(t)
	wt := filepath.Join(root, "wt-dry")
	sweepGit(t, repo, "worktree", "add", wt, "-b", "feat/dry")
	record := sweepCreatedAtRecordPath(t, wt)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(record, old, old); err != nil {
		t.Skipf("chtimes unavailable: %v", err)
	}

	calls := withGitCallRecorder(t)
	opts := sweepOptionsNow(true)
	opts.MinAge = time.Hour
	res := worktreeSweepRunWith(context.Background(), opts, worktreeSweepDeps{Repos: []string{repo}, LedgerRead: true}, nil)

	if res.Reclaimed != 0 || res.WouldReclai != 1 {
		t.Fatalf("dry run: reclaimed=%d would=%d (%+v), want 0/1", res.Reclaimed, res.WouldReclai, res.Findings)
	}
	if n := len(calls.filter("worktree", "remove")); n != 0 {
		t.Fatalf("a dry run invoked %d removals", n)
	}
	if n := len(calls.filter("worktree", "prune")); n != 0 {
		t.Fatalf("a dry run invoked %d prunes", n)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the dry run removed the worktree directory: %v", err)
	}
}

// TestSweepOptionsFromFlagsDryRunWins: the resolver is the ENGINE's own door,
// and it must honor EITHER spelling of the plan posture. It used to derive its
// posture from Apply alone (`DryRun: !flags.Apply`), so a caller carrying both
// flags got a destructive run out of a struct that also said "dry run".
func TestSweepOptionsFromFlagsDryRunWins(t *testing.T) {
	cases := []struct {
		name         string
		apply, dry   bool
		wantPlanOnly bool
	}{
		{"bare is a plan", false, false, true},
		{"--dry-run is a plan", false, true, true},
		{"--apply runs", true, false, false},
		{"both flags stay a plan", true, true, true},
	}
	for _, c := range cases {
		opts := worktreeSweepOptionsFromFlagsFixture(t, WorktreeSweepFlags{Apply: c.apply, DryRun: c.dry})
		if opts.DryRun != c.wantPlanOnly {
			t.Errorf("%s: DryRun = %v, want %v", c.name, opts.DryRun, c.wantPlanOnly)
		}
	}
}

// TestSweepBothFlagsReachNoRemovalPath is the end-to-end half of the same
// contract: a caller carrying --apply AND --dry-run must not touch a single
// deletion path (no `git worktree remove`, no `git worktree prune`, directory
// intact) — the same guarantee TestSweepDryRunChangesNothing pins for the
// plain dry run.
func TestSweepBothFlagsReachNoRemovalPath(t *testing.T) {
	repo := sweepInitRepo(t)
	root := sweepFixtureRoot(t)
	wt := filepath.Join(root, "wt-conflict")
	sweepGit(t, repo, "worktree", "add", wt, "-b", "feat/conflict")
	record := sweepCreatedAtRecordPath(t, wt)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(record, old, old); err != nil {
		t.Skipf("chtimes unavailable: %v", err)
	}

	// Exactly the CLI's own resolution path for `--apply --dry-run`.
	opts := worktreeSweepOptionsFromFlagsFixture(t, WorktreeSweepFlags{Apply: true, DryRun: true})
	opts.MinAge = time.Hour
	opts.Now = time.Now()

	calls := withGitCallRecorder(t)
	res := worktreeSweepRunWith(context.Background(), opts, worktreeSweepDeps{Repos: []string{repo}, LedgerRead: true}, nil)

	if !res.DryRun {
		t.Error("the result must report the plan posture the caller asked for")
	}
	if res.Reclaimed != 0 || res.Reaped != 0 || res.BranchesReaped != 0 {
		t.Fatalf("--apply --dry-run reclaimed=%d reaped=%d branches-reaped=%d, want 0/0/0",
			res.Reclaimed, res.Reaped, res.BranchesReaped)
	}
	if n := len(calls.filter("worktree", "remove")); n != 0 {
		t.Fatalf("--apply --dry-run invoked %d removals", n)
	}
	if n := len(calls.filter("worktree", "prune")); n != 0 {
		t.Fatalf("--apply --dry-run invoked %d prunes", n)
	}
	if n := len(calls.filter("branch", "-D")); n != 0 {
		t.Fatalf("--apply --dry-run reaped %d branches", n)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("--apply --dry-run removed the worktree directory: %v", err)
	}
}

// TestSweepRunFailsClosedWithoutTheLedger: with no ownership evidence, nothing
// is reclaimed — an unowned-LOOKING registration may belong to a live task.
func TestSweepRunFailsClosedWithoutTheLedger(t *testing.T) {
	repo := sweepInitRepo(t)
	root := sweepFixtureRoot(t)
	wt := filepath.Join(root, "wt-noledger")
	sweepGit(t, repo, "worktree", "add", wt, "-b", "feat/noledger")
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(sweepCreatedAtRecordPath(t, wt), old, old); err != nil {
		t.Skipf("chtimes unavailable: %v", err)
	}

	opts := sweepOptionsNow(false)
	opts.MinAge = time.Hour
	res := worktreeSweepRunWith(context.Background(), opts, worktreeSweepDeps{Repos: []string{repo}, RowsErr: "ledger unavailable"}, nil)
	if res.Reclaimed != 0 {
		t.Fatalf("reclaimed %d registrations without ledger evidence", res.Reclaimed)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the worktree was removed despite the missing ledger: %v", err)
	}
}

// TestSweepAgeAndMaxKnobsAreRead: the env knobs must be real readers (Rule 21),
// and a malformed value must fall back rather than disable the guard.
func TestSweepAgeAndMaxKnobsAreRead(t *testing.T) {
	t.Setenv(worktreeSweepMinAgeEnvName, "6")
	t.Setenv(worktreeSweepMaxEnvName, "3")
	opts := worktreeSweepOptionsFrom(false, 0, 0)
	if opts.MinAge != 6*time.Hour || opts.Max != 3 {
		t.Fatalf("knobs ignored: %+v, want 6h/3", opts)
	}
	t.Setenv(worktreeSweepMinAgeEnvName, "soon")
	t.Setenv(worktreeSweepMaxEnvName, "many")
	opts = worktreeSweepOptionsFrom(false, 0, 0)
	if opts.MinAge != DefaultWorktreeSweepMinAge || opts.Max != DefaultWorktreeSweepMax {
		t.Fatalf("malformed knobs did not fall back to the defaults: %+v", opts)
	}
	// Flags win over the environment.
	opts = worktreeSweepOptionsFromFlagsFixture(t, WorktreeSweepFlags{Apply: true, MinAgeH: "2", Max: "7"})
	if opts.DryRun || opts.MinAge != 2*time.Hour || opts.Max != 7 {
		t.Fatalf("flags ignored: %+v", opts)
	}
	// The destructive posture requires --apply: without it a run plans.
	if opts := worktreeSweepOptionsFromFlagsFixture(t, WorktreeSweepFlags{}); !opts.DryRun {
		t.Fatal("a run without --apply was not a dry run — the default must be report-only")
	}
}

// --- fixture helpers -------------------------------------------------------

// sweepInitRepo creates a committed git repo (a worktree needs a HEAD).
func sweepInitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "master"},
		{"config", "user.email", "sweep@test"},
		{"config", "user.name", "sweep"},
		{"commit", "-q", "--allow-empty", "-m", "seed"},
	} {
		sweepGit(t, dir, args...)
	}
	return dir
}

// sweepGit runs one git command in dir, failing the test on error.
func sweepGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// sweepCreatedAtRecordPath resolves the registration's OWN creation record — the
// worktree-private git dir (and the `gitdir` backlink inside it) whose newest
// mtime worktreeSweepCreatedAt reads. The fixture needs the real path to age a
// registration; it is NOT the main repo's commondir (see worktreeSweepCreatedAt).
func sweepCreatedAtRecordPath(t *testing.T, worktree string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(worktree, worktreeSweepGitdirName))
	if err != nil {
		t.Fatalf("read gitdir backlink: %v", err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), finishCleanupGitDirFilePrefix))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	return filepath.Join(gitdir, worktreeSweepGitdirFileName)
}

// gitCallRecorder captures every git invocation the sweep makes.
type gitCallRecorder struct {
	calls [][]string
}

// Observe implements worktreeSweepGitObserver (the measurement seam for the
// one-list/one-prune rule).
func (r *gitCallRecorder) Observe(args []string) {
	cp := make([]string, len(args))
	copy(cp, args)
	r.calls = append(r.calls, cp)
}

// filter returns the recorded invocations containing every given token.
func (r *gitCallRecorder) filter(tokens ...string) [][]string {
	var out [][]string
	for _, call := range r.calls {
		joined := strings.Join(call, " ")
		all := true
		for _, tok := range tokens {
			if !strings.Contains(joined, tok) {
				all = false
				break
			}
		}
		if all {
			out = append(out, call)
		}
	}
	return out
}

// withGitCallRecorder installs a recording wrapper for the git runner used by
// BOTH the sweep and the worktree-remove helper. It restores the original on
// cleanup, so sibling tests are unaffected.
func withGitCallRecorder(t *testing.T) *gitCallRecorder {
	t.Helper()
	rec := &gitCallRecorder{}
	prev := worktreeSweepGitObserverHook
	worktreeSweepGitObserverHook = rec
	t.Cleanup(func() { worktreeSweepGitObserverHook = prev })
	return rec
}

// worktreeSweepOptionsFromFlagsFixture resolves flags with the environment
// cleared, so a flag assertion cannot be satisfied by an ambient knob.
func worktreeSweepOptionsFromFlagsFixture(t *testing.T, flags WorktreeSweepFlags) worktreeSweepOptions {
	t.Helper()
	t.Setenv(worktreeSweepMinAgeEnvName, "")
	t.Setenv(worktreeSweepMaxEnvName, "")
	return WorktreeSweepOptionsFromFlags(flags)
}
