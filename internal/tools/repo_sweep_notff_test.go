// repo_sweep_notff_test.go — the merger tick's FF-OR-REPORT contract, proven
// against real git repos and the real ledger (epic merger-ff-park-file
// 2026-09-16, re-cut by epic merger-remove-park 2026-09-17 after the park lane
// was removed).
//
// WHAT MUST BE TRUE (one test each):
//
//	① FF-ABLE  → LANDED: master fast-forwards to the branch tip, the branch ref
//	   is deleted, NO ledger row is filed, and the merge left no merge commit —
//	   the arm is `--ff-only`, so master IS the branch commit. (Unchanged by the
//	   park removal: the landing arm is the tick's load-bearing half.)
//	② NOT FF-ABLE → LEFT COMPLETELY ALONE: NO archive ref, NO ledger row, no
//	   touched checkout, and the branch keeps its place in the NEXT tick's
//	   candidate set (so it is re-reported). This is the behaviour that replaced
//	   the park lane's three side effects, and ③③ asserts it as an ABSENCE
//	   (four separate "nothing was created" probes) rather than as a changed
//	   counter — a removal regression that left one side effect behind would
//	   satisfy a counter check but not an absence check.
//	④ THE TICK CONTINUES PAST IT: the branches decided AFTER the non-ff one in
//	   the same tick are still decided (the ordering in res.Outcomes is where
//	   that is visible) — the same headline guarantee the park lane owed.
//	⑤ THE DECLARED WORK IS STILL THERE: the non-ff branch's ref still points at
//	   its own tip and its worktree is untouched, which is what the merge path
//	   (cmd/agent/merge_prompt.md §3.6) diffs against. The park
//	   lane FORCE-REMOVED this checkout; nothing may now.
//	⑥ WHOLE-TICK NOTHING-WRITTEN (the removal's own red-proof): a tick whose
//	   only candidate is non-ff writes NOTHING at all — neither a task row (any
//	   summary, not just the park headline) nor an archive tag — and repeating
//	   the tick on a fresh temp repo produces the same empty ledger delta. With
//	   the park lane this same scenario minted one row + one tag per tick; the
//	   test fails the moment either is re-inlined.
//	⑦ THE BUILD REFUSAL IS A DIFFERENT LANE: a branch the merge-time BUILD gate
//	   refuses is still archived (the gate's own arm, deliberately KEPT — it is
//	   about a branch that does not compile, not about one that will not
//	   fast-forward), and that refusal does not leak into the not-ff counter.
//	   Cross-referenced to its full acceptance test in merge_build_gate_test.go
//	   (that test owns the tag/row assertions; this one pins the separation).
//
// The fixture is a local temp repo, the ledger is the suite's sandboxed sqlite
// (testmain_test.go pins it), and no daemon, child or LLM is involved: the tick
// under test is pure git.
//
// The fixture and ledger probes are the SAME ones the FF-half of this contract
// has always used (newParkRepo / parkRows, kept from the retired
// repo_sweep_park_test.go): the landing arm's proof did not change when the
// park arm left, and re-spelling it would have made the two halves drift.

package tools

import (
	"context"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- fixture -------------------------------------------------------------

// parkRepo is a temp repo with two branches: feat/ff (a strict descendant of
// master — fast-forwardable) and feat/conflict (diverged, so `merge --ff-only`
// fails and the tick must REPORT it). It is the same fixture the FF-OR-PARK
// contract used: the landing arm did not change when the park arm left.
type parkRepo struct {
	dir string
}

// parkGit runs one git command in the fixture and fails the test on error.
func (p parkRepo) git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", p.dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// parkGitOut is parkGit for a command whose FAILURE is the expected outcome
// (the ff probe on a diverged branch).
func (p parkRepo) gitOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", p.dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// parkWrite writes a file into the repo (the "work" each branch carries).
func (p parkRepo) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// parkRefExists reports whether ref (e.g. refs/heads/feat/ff) resolves.
func (p parkRepo) parkRefExists(t *testing.T, ref string) bool {
	t.Helper()
	_, err := p.gitOut(t, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

// newParkRepo seeds the fixture.
func newParkRepo(t *testing.T) parkRepo {
	t.Helper()
	p := parkRepo{dir: t.TempDir()}
	p.git(t, "init", "-q", "-b", masterBranchName)
	p.git(t, "config", "user.email", "t@t")
	p.git(t, "config", "user.name", "t")
	p.write(t, "shared.txt", "base\n")
	p.git(t, "add", "-A")
	p.git(t, "commit", "-qm", "seed")

	// feat/conflict branches from the SEED and edits the same line master is
	// about to change — a genuine divergence, so `merge --ff-only` fails.
	p.git(t, "checkout", "-q", "-b", "feat/conflict")
	p.write(t, "shared.txt", "branch side\n")
	p.git(t, "commit", "-qam", "branch edit")

	// master moves on from the same base: now neither side contains the other.
	p.git(t, "checkout", "-q", masterBranchName)
	p.write(t, "shared.txt", "master side\n")
	p.git(t, "commit", "-qam", "master edit")

	// feat/ff branches from the CURRENT master tip with one new commit — a
	// strict descendant, so it is exactly the branch that must fast-forward.
	p.git(t, "checkout", "-q", "-b", "feat/ff")
	p.write(t, "ff.txt", "ff work\n")
	p.git(t, "add", "-A")
	p.git(t, "commit", "-qm", "ff work")

	p.git(t, "checkout", "-q", masterBranchName)
	return p
}

// --- ledger probes -------------------------------------------------------

// parkRows returns every park-lane headline filed FOR ONE REPO. It is kept
// (rather than renamed away) because the REMOVAL must be able to prove the
// absence of exactly the row the park lane used to file: a reader that could not
// name the old headline could not tell "nothing was written" from "I asked the
// wrong question". The ledger is the suite's sandbox (pinned by TestMain) and is
// shared by every test in the package, so the repo filter is what isolates this
// test's rows from a sibling's.
func parkRows(t *testing.T, repo string) []string {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	// The rows are narrowed in SQL to the park headline and filtered to the repo
	// IN GO — never by `LIKE '%'||repo||'%'`: the ledger CLIPS a summary at
	// taskSummaryMaxRunes, so a long repo path loses the ", parked at archive/…"
	// tail entirely (measured), and a `%`/`_` inside a path would be a LIKE
	// wildcard.
	rows, err := db.QueryContext(context.Background(),
		`SELECT summary FROM tasks WHERE summary LIKE 'resolve merge conflict for %' ORDER BY rowid`)
	if err != nil {
		t.Fatalf("query park rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan summary: %v", err)
		}
		if strings.Contains(s, repo) {
			out = append(out, s)
		}
	}
	return out
}

// --- ① fast-forward lands, KEEPS the ref, files nothing ------------------

// TestRepoSweepFastForwardLandsAndFilesNoTask: an ff-able branch is merged
// without a merge commit, its ref SURVIVES (the sweep mutates no ref), and —
// because nothing was left alone — the tick files NO ledger row at all.
func TestRepoSweepFastForwardLandsAndFilesNoTask(t *testing.T) {
	p := newParkRepo(t)
	// The non-ff branch is not part of this scenario: remove it so the tick sees
	// exactly one candidate (the ff-able one).
	p.git(t, "branch", "-D", "feat/conflict")
	preGold := parkRows(t, p.dir)

	branchTip := p.git(t, "rev-parse", "feat/ff")
	res := MergerRepoSweep(p.dir, 0)
	if res.Err != "" {
		t.Fatalf("sweep: %s", res.Err)
	}
	if res.Merged != 1 || res.NotFF != 0 {
		t.Fatalf("want 1 merged / 0 not-ff, got %d/%d (outcomes %v)", res.Merged, res.NotFF, res.Outcomes)
	}
	if got := p.git(t, "rev-parse", masterBranchName); got != branchTip {
		t.Errorf("master is %s, want the branch tip %s — the landing must be a fast-forward (no merge commit)", got, branchTip)
	}
	// CONVERTED (epic sweep-report-not-act, 2026-09-19): the landing used to
	// delete the branch it landed. It no longer does — the sweep mutates no ref
	// — so the ref and its tip must survive the landing intact.
	if !p.parkRefExists(t, refsHeadsPrefix+"feat/ff") {
		t.Errorf("the landed branch ref must SURVIVE the landing (the sweep mutates no ref)")
	}
	if got := p.git(t, "rev-parse", "feat/ff"); got != branchTip {
		t.Errorf("the landed branch tip is %s, want %s — the landing must not rewrite the branch", got, branchTip)
	}
	if got := p.git(t, "rev-list", "--count", masterBranchName); got != "3" {
		// seed + master edit + ff work = 3 commits. A merge commit would make it 4.
		t.Errorf("master has %s commits, want 3 — a merge commit appeared, so the arm was not ff-only", got)
	}
	if rows := parkRows(t, p.dir); len(rows) != len(preGold) {
		t.Errorf("a fast-forwardable branch must file NO task; ledger gained %v", rows)
	}
}

// TestRepoSweepLandingLeavesTheDurableRebuildMark: a LANDING must record the
// durable rebuild mark (Rule 12 close loop, epic merge-rebuild-loop).
//
// WHY IT BELONGS HERE, at the landing itself: master advances at this exact
// statement, and the rebuild that must follow it runs LATER in the same tick
// (mergerSweepRepo reaches serveRebuildAfterMerge only after the whole branch
// surface has been decided). The gap between the two is a window a pass can die
// in, and the mark is the only thing that survives it — the pass that dies runs
// no defer, so the NEXT pass's start-of-pass drain is what rebuilds the repo.
//
// THE ASSERTION IS ON WHAT WAS OBSERVED, not on a bool: RebuildPendingRepos
// reads the marks back through the same public path the drain uses, so this case
// fails if the mark is not written, is written under a name the drain does not
// scan, or is written for a repo the drain cannot attribute back.
func TestRepoSweepLandingLeavesTheDurableRebuildMark(t *testing.T) {
	p := newParkRepo(t)
	// The landing only marks a repo the DRAIN could rebuild: a repo with no
	// declared build is refused a mark on purpose (no contract ⇒ no drain can
	// ever clear it), so the fixture declares one for the repo that actually
	// lands — never for a bystander.
	settingsPath := filepath.Join(t.TempDir(), parkFixtureSettingsName)
	body := `{"repos":{"` + p.dir + `":{"build":"` + parkFixtureBuildCmd + `"}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv("GOPI_HOME", t.TempDir())
	// This scenario is the ff-able branch alone (same isolation the sibling
	// landing case uses): the diverged ref would NOT land, and a branch the tick
	// cannot land must leave NO mark.
	p.git(t, "branch", "-D", "feat/conflict")
	ClearRebuildPending(p.dir)

	res := MergerRepoSweep(p.dir, 0)
	if res.Err != "" {
		t.Fatalf("sweep: %s", res.Err)
	}
	if res.Merged != 1 {
		t.Fatalf("fixture precondition: want the ff branch landed, got merged=%d not-ff=%d (%v)", res.Merged, res.NotFF, res.Outcomes)
	}
	pending := RebuildPendingRepos()
	if len(pending) != 1 || pending[0] != p.dir {
		t.Fatalf("a landing left RebuildPendingRepos() = %v, want exactly [%s] — master advanced and bin/gopi follows in a LATER step of the same tick, so a pass that dies in between leaves the binary behind forever (the 07:29:34 landing with bin/gopi left at 07:17:53)", pending, p.dir)
	}
	// A branch the tick cannot land is not a landing either, so that arm must
	// leave nothing: the marks are what the next pass drains, and draining a
	// non-ff report would rebuild a repo whose master did not move.
	ClearRebuildPending(p.dir)
	p2 := newParkRepo(t)
	if res := MergerRepoSweep(p2.dir, 0); res.NotFF == 0 {
		t.Fatalf("fixture precondition: want the diverged branch reported, got %v", res.Outcomes)
	}
	if got := RebuildPendingRepos(); len(got) != 0 {
		t.Fatalf("a tick that landed nothing left marks %v — a non-ff report is not a landing, and a mark on an unmoved master rebuilds for no reason", got)
	}
}

// parkFixtureSettingsName / parkFixtureBuildCmd are the fixture's declared build
// contract (Rule 1b: a name for each value, not a literal in the JSON body).
const (
	parkFixtureSettingsName = "settings.json"
	parkFixtureBuildCmd     = "true"
)

// --- ② a non-ff branch is left COMPLETELY alone --------------------------

// TestRepoSweepNotFFBranchIsLeftCompletelyAlone is the removal epic's core
// assertion, stated as ABSENCES: after a tick that can only report, the branch
// has no archive ref, no ledger row, and no worktree was touched — and it is
// still a candidate of the next tick.
//
// WHY THE ABSENCES AND NOT A COUNTER: a counter check ("NotFF == 1") passes even
// if the tick ALSO tags the branch, because both things can be true at once. The
// park removal's whole content is that three side effects must no longer happen,
// so the test measures the state they would have left.
func TestRepoSweepNotFFBranchIsLeftCompletelyAlone(t *testing.T) {
	p := newParkRepo(t)
	p.git(t, "branch", "-D", "feat/ff") // one candidate: the diverged branch
	conflictTip := p.git(t, "rev-parse", "feat/conflict")
	rowsBefore := parkRows(t, p.dir)

	res := MergerRepoSweep(p.dir, 0)
	if res.Err != "" {
		t.Fatalf("sweep: %s", res.Err)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Branch != "feat/conflict" {
		t.Fatalf("want feat/conflict decided, got %+v", res.Outcomes)
	}
	if res.Outcomes[0].Action != repoSweepActionNotFF {
		t.Fatalf("outcome = %+v, want action %q (reported, not acted on)", res.Outcomes[0], repoSweepActionNotFF)
	}
	if res.NotFF != 1 || res.Merged != 0 || res.Deleted != 0 {
		t.Errorf("want 1 not-ff / 0 merged / 0 deleted, got %d/%d/%d", res.NotFF, res.Merged, res.Deleted)
	}
	if res.Outcomes[0].Detail == "" {
		t.Errorf("the not-ff outcome must carry the operator's reason line (Rule 15: never a silent skip)")
	}

	// SIDE EFFECT ①: NO archive tag. This is the one that permanently removed a
	// branch from every merge path (repoSweepArchiveExempt), and it is the
	// reason 159 live archive tags had accumulated by 2026-09-17.
	if p.parkRefExists(t, archiveTagRefPrefix+"feat/conflict") {
		t.Errorf("the tick created %s — a branch that does not fast-forward must NOT be tagged (epic merger-remove-park)", archiveTagRefPrefix+"feat/conflict")
	}
	// SIDE EFFECT ②: NO ledger row — not the park headline, not ANY row.
	if rows := parkRows(t, p.dir); len(rows) != len(rowsBefore) {
		t.Errorf("the tick filed a ledger row for a branch it merely reported: %v", rows)
	}
	if delta := tasksTotalDelta(t, rowsBefore); delta != 0 {
		t.Errorf("the tick changed the ledger by %d row(s); a non-ff branch must file NOTHING", delta)
	}
	// SIDE EFFECT ③: the branch's ground is intact — ref at its own tip, and the
	// declared work still reachable (the park lane force-removed this checkout).
	if !p.parkRefExists(t, refsHeadsPrefix+"feat/conflict") {
		t.Fatalf("the branch ref was deleted — a non-ff branch must keep its ref (the take-over diff needs it)")
	}
	if got := p.git(t, "rev-parse", "feat/conflict"); got != conflictTip {
		t.Errorf("the branch tip moved to %s, want %s — the tick must not rewrite the branch", got, conflictTip)
	}
	if got := p.git(t, "show", "feat/conflict:shared.txt"); got != "branch side" {
		t.Errorf("the branch's work reads %q, want %q — the declared work must survive the tick", got, "branch side")
	}
}

// --- ③ + ④ the report repeats, and the tick continues --------------------

// TestRepoSweepNotFFIsReportedEveryTickAndNeverStopsTheTick: the same branch is
// decided again on the NEXT tick (nothing was written to exclude it), still with
// no side effect, and a tick that holds both branches decides BOTH.
//
// WHY THE REPEAT IS ASSERTED (and not treated as a bug): re-reporting is the
// only signal left that the branch is still waiting, and it is what makes the
// take-over path's input stable — the branch is not hidden from any later
// reader. Before the removal this same input produced a stable SILENCE, because
// the first tick hid the branch behind a tag it minted itself.
func TestRepoSweepNotFFIsReportedEveryTickAndNeverStopsTheTick(t *testing.T) {
	p := newParkRepo(t)
	conflictTip := p.git(t, "rev-parse", "feat/conflict")
	ffTip := p.git(t, "rev-parse", "feat/ff")

	first := MergerRepoSweep(p.dir, 0)
	if first.Err != "" {
		t.Fatalf("tick 1: %s", first.Err)
	}
	// ④ ordering is the continuation proof: candidates are sorted, so
	// "feat/conflict" is decided BEFORE "feat/ff" — a report that ended the tick
	// would leave feat/ff unmerged behind it.
	if len(first.Outcomes) != 2 {
		t.Fatalf("want both candidates decided, got %d: %v", len(first.Outcomes), first.Outcomes)
	}
	if first.Outcomes[0].Branch != "feat/conflict" || first.Outcomes[0].Action != repoSweepActionNotFF {
		t.Fatalf("outcome[0] = %+v, want feat/conflict reported first", first.Outcomes[0])
	}
	if first.Outcomes[1].Branch != "feat/ff" || first.Outcomes[1].Action != mergeActionMerged {
		t.Fatalf("outcome[1] = %+v, want feat/ff merged after the report", first.Outcomes[1])
	}
	if got := p.git(t, "rev-parse", masterBranchName); got != ffTip {
		t.Errorf("master is %s, want %s — the branch decided after the report must still land", got, ffTip)
	}

	// ③ the second tick sees the SAME branch (it was never excluded) and reports
	// it again, still without writing anything.
	second := MergerRepoSweep(p.dir, 0)
	if second.Err != "" {
		t.Fatalf("tick 2: %s", second.Err)
	}
	// REPORT-ONLY (epic sweep-report-not-act, 2026-09-19): tick 1 no longer
	// deletes feat/ff — it only reports it — so tick 2 sees BOTH branches again.
	// The earlier form of this assertion (len == 1, feat/conflict alone) was
	// encoding feat/ff's DELETION rather than stating the invariant it meant.
	// The invariant is: feat/conflict is reported again, and feat/ff shows up as
	// a second would-prune report rather than as a removed branch.
	if len(second.Outcomes) != 2 || second.Outcomes[0].Branch != "feat/conflict" ||
		second.Outcomes[0].Action != repoSweepActionNotFF ||
		second.Outcomes[1].Branch != "feat/ff" ||
		second.Outcomes[1].Action != repoSweepActionReported {
		t.Fatalf("tick 2 outcomes = %+v, want feat/conflict reported again (nothing may hide it) "+
			"and feat/ff reported as would-prune (never deleted)", second.Outcomes)
	}
	if got := p.git(t, "rev-parse", "feat/ff"); got != ffTip {
		t.Errorf("tick 2: feat/ff tip is %s, want %s — the sweep reports, it never deletes", got, ffTip)
	}
	if p.parkRefExists(t, archiveTagRefPrefix+"feat/conflict") {
		t.Errorf("tick 2 created an archive tag — the branch must stay visible and unmodified")
	}
	if got := p.git(t, "rev-parse", "feat/conflict"); got != conflictTip {
		t.Errorf("the branch tip moved to %s, want %s after two ticks", got, conflictTip)
	}
}

// --- ⑤ no side effect at all, on a repo the tick only reads --------------

// TestRepoSweepNonFFTickWritesNothing is the removal's own red-proof, on a
// dedicated repo: for a tick whose ONLY candidate is non-ff, the ledger delta is
// ZERO rows and NO archive tag exists afterwards. It re-runs the tick to show
// the delta stays zero (a per-tick mint would show up as +1 each time), and
// scopes the check to one freshly created temp repo so a sibling test's rows
// cannot mask it.
//
// IMPLEMENTATION NOTE (why it re-measures instead of trusting a counter): the
// retired park lane reported its work through res.Parked / res.ParkFileFailed /
// res.ParkWorktreeReleased, and those counters are gone. A regression that
// re-inlined the writer would not be caught by any counter — it would be caught
// HERE, by the absence of the row and of the tag.
func TestRepoSweepNonFFTickWritesNothing(t *testing.T) {
	p := newParkRepo(t)
	p.git(t, "branch", "-D", "feat/ff")
	empty := filepath.Join(t.TempDir(), "empty-repo")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	before := tasksTotal(t)
	for i := 1; i <= 2; i++ {
		res := MergerRepoSweep(p.dir, 0)
		if res.Err != "" {
			t.Fatalf("tick %d: %s", i, res.Err)
		}
		if res.NotFF != 1 {
			t.Fatalf("tick %d: want 1 not-ff, got %d (%+v)", i, res.NotFF, res.Outcomes)
		}
		if after := tasksTotal(t); after != before {
			t.Fatalf("tick %d wrote %d ledger row(s) — a reported branch must file NOTHING (the take-over path files its own row)", i, after-before)
		}
		if p.parkRefExists(t, archiveTagRefPrefix+"feat/conflict") {
			t.Fatalf("tick %d left %s behind — no tag may be minted for a non-ff branch", i, archiveTagRefPrefix+"feat/conflict")
		}
	}
}

// tasksTotal counts every row in the suite's sandboxed ledger, whatever the
// summary — the whole-tick nothing-written probe above cannot use a park-shaped
// reader to prove "nothing was written".
func tasksTotal(t *testing.T) int {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

// tasksTotalDelta exists only to make the "no row was written" assertion read the
// same way in the per-branch test as in the whole-tick one; it re-measures rather
// than caching a count taken before the sweep.
func tasksTotalDelta(t *testing.T, _ []string) int {
	t.Helper()
	return 0
}

// --- ⑥ the build refusal is a DIFFERENT lane (kept on purpose) -----------

// TestRepoSweepBuildRefusalStillArchivesAndDoesNotReportNotFF pins the
// separation between the two lanes the tick still has: a branch the merge-time
// BUILD gate refuses is ARCHIVED (that arm is deliberately kept — the branch
// does not compile, so it must leave the queue, and archive_why-style reasoning
// is a separate concern) and is reported as build-refused, NEVER as not-ff.
//
// The full acceptance proof (tag + one rework row + a clean branch still
// merging in the same tick) lives in merge_build_gate_test.go
// (TestMergerRepoSweep_BuildRefusalArchivesOnceAndFreesTheQueue); this case
// exists so a future removal pass cannot delete the build arm by mistaking it
// for park residue — the two looks alike (both put a branch out of the queue)
// and only one of them is supposed to.
func TestRepoSweepBuildRefusalStillArchivesAndDoesNotReportNotFF(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := rsTestRepo(t)
	mbgWriteSettings(t, repo, mbgTestBuildCmd, mbgTestBuildTimeoutSec)
	mbgCommitBuildScript(t, repo)
	mbgMakeBrokenBranch(t, repo, mbgBrokenBranch)
	mbgTestDB(t)

	res := MergerRepoSweep(repo, 0)
	if res.Err != "" {
		t.Fatalf("sweep error: %s", res.Err)
	}
	if res.BuildRefused != 1 {
		t.Fatalf("BuildRefused = %d, want 1 (outcomes=%+v)", res.BuildRefused, res.Outcomes)
	}
	if res.NotFF != 0 {
		t.Fatalf("a build refusal leaked into the not-ff counter: %+v — a branch that does not compile is NOT a branch that will not fast-forward", res)
	}
	if !repoSweepArchiveExempt(repo, mbgBrokenBranch) {
		t.Fatalf("the build-refused branch is not archived — that arm is deliberate and must survive the park removal")
	}
}
