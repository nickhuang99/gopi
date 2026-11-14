// reclaim_own_ground_test.go — the CLOSE-OUT GROUND MARK's own guards
// (reclaim_own_ground.go), and the red proof for the two rungs it exempts.
//
// THE DEFECT these tests pin (measured on master f4451b9d, reproduced by this
// run from a pristine checkout: `go test -run TestManualRunRecyclesItsWorktree
// ./cmd/agent/` → FAIL, EXIT=1). A run that closes out the row it executed
// cannot release that row's ground, because the arm walks it while the process
// is still the thing the guards describe:
//
//	own registered worktree kept: … — younger than the 5m0s floor (3s) — a
//	  concurrent round may be creating it            (rung ①, the age floor)
//	own worktree release refused: … guest-row-reconcile refusal (arm
//	  guestRowOwnReleaseRefusal): this process stands in the linked worktree …
//	                                                 (the receiver arm)
//
// Both sentences describe a state that IS the close-out's contract rather than
// its hazard. The mark (reclaim_own_ground.go) is the ONE publication of the
// fact that separates the two, so these tests are written at the two directions
// that make it safe:
//
//	① ARMED: a registration that belongs to THIS process's own row — young, and
//	   standing on the asker's own ground — is released by the ownership-
//	   independent pass. That is the acceptance's unit-level shape, and it is RED
//	   on master (measured: the age floor's sentence above).
//	② UNARMED: the SAME registration, same age, same cwd, with no close-out
//	   pass — still KEPT by the age floor, and ANOTHER row's ground is kept even
//	   INSIDE a close-out pass. This is the direction that would break if the
//	   exemption were widened from "the marks" to "a close-out is running": the
//	   whole point of the mark is that membership is exact.
//
// The red proof is a MUTATION of the shipped code, in the style
// task_reclaim_self_cwd_authority_test.go already uses (this package ships no
// test-only knob): replace the `!SelfGroundReleasePendingFor(reg.Path)` test in
// reclaimSelfOwnWhy's rung ① with the unconditional body it had (i.e. drop the
// exemption), and ① turns RED with the age-floor sentence while ② stays GREEN —
// which is also the exact pre-fix behaviour measured on the E2E.
package tools

import (
	"github.com/nickhuang99/gopi/internal/scratch"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// ownGroundCloseoutRepo holds the claim stamp the close-out pass reads: a row
// whose claimed_by is THIS process is the row a pass closes out.
const ownGroundCloseoutRepoBranch = "feat/x"

// fixtureRowSeq numbers the ledger rows these fixtures mint.
//
// WHY A COUNTER AND NOT t.Name() ALONE (measured 2026-10-30, `go test
// ./internal/tools/ -count=3 -run 'TestReclaimGroundChecklist…'`, exit 1 on
// iterations 2 and 3):
//
//	--- FAIL: TestReclaimGroundChecklistReleasesTheArmedOwnGround (0.02s)
//	    reclaim_own_ground_test.go:184: submit
//	    t-closeout-testreclaimgroundchecklistreleasesthearmedownground:
//	    task: task_id "…" already exists
//
// The ledger key is UNIQUE and the ledger OUTLIVES one execution of a test
// body: `-count=N` runs that body N times IN ONE PROCESS against one database.
// A name-derived id is therefore a PROCESS CONSTANT while the ledger is
// PROCESS PERSISTENT, so the second submit is refused before the behaviour
// under test is reached — the failure was the FIXTURE's non-re-entrancy, never
// the checklist rung. The suffix keeps each iteration's id readable AND
// distinct; it never touches an assertion.
var fixtureRowSeq atomic.Int64

// uniqueFixtureTaskID mints a ledger row id that names its test AND its
// iteration: `t-<prefix>-<test-name>-<n>`. Exactly one execution of a body gets
// one id, so the same body may run any number of times in one process.
func uniqueFixtureTaskID(t *testing.T, prefix string) string {
	t.Helper()
	slug := strings.ToLower(strings.NewReplacer("/", "-", "_", "-", " ", "-").Replace(t.Name()))
	return "t-" + prefix + "-" + slug + "-" + strconv.Itoa(int(fixtureRowSeq.Add(1)))
}

// ownGroundCloseoutFixture builds the acceptance's unit shape: a repository with
// ONE linked worktree registration, a declared root that permits it, and a row
// that THIS process claims and whose deliverable names that registration — the
// own ground of the pass. The registration is left FRESH (seconds old): the
// close-out's ground is young by construction, which is exactly what rung ①
// vetoed on master.
//
// The asker additionally STANDS in the registration (t.Chdir), because that is
// the manual-run contract's designed state — the production pass runs before the
// cwd release (cmd/agent taskReclaimOwnDirectories) — and it is what makes the
// guest/own-ground rungs take part.
func ownGroundCloseoutFixture(t *testing.T) (repo, wt, taskID string) {
	t.Helper()
	// The launch candidates are PROCESS-GLOBAL (the mark publishes one fact for
	// the whole process), so a test that reads them must pin them: without this
	// the ambient value of whichever process is running the suite would decide
	// whether a launch ground exists — measured, it was the launching session's
	// own worktree in a foreign repo. Pin it empty by default, and let the
	// assertions that need one set it themselves.
	SetSelfGroundLaunchDir("")
	t.Cleanup(func() { SetSelfGroundLaunchDir("") })
	root := t.TempDir()
	repo, wt = worktreeRefusalFixture(t, root)
	// The fixture tree doubles as the declared worktree root, so the
	// registration is INSIDE the pass's containment rung and cannot be kept for
	// that reason instead (the pattern cwdAuthorityFixture uses).
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	taskID = uniqueFixtureTaskID(t, "closeout")
	seedLiveStatusRow(t, taskID, repo, ownGroundCloseoutRepoBranch, wt, reclaimSelfStamp())
	return repo, wt, taskID
}

// TestSelfGroundReleaseReleasesTheOwnGroundOfTheClosingRow is ①: with the
// close-out pass running, its own row's young registration standing on the
// asker's own ground is RELEASED, and the branch survives (the merger's input).
//
// RED on master: the registration is refused by rung ①'s age floor ("younger
// than the 5m0s floor") and never reaches a removal.
func TestSelfGroundReleaseReleasesTheOwnGroundOfTheClosingRow(t *testing.T) {
	repo, wt, taskID := ownGroundCloseoutFixture(t)
	t.Chdir(wt)
	// The LAUNCH ground is the one this mark exists for: the directory the run
	// was launched in, which no ledger row records (the worker creates its own
	// ground for the executor child). Published here the way a producer's
	// close-out publishes it, before the cwd moves.
	SetSelfGroundLaunchDir(wt)

	lines := ReclaimOwnWorktreesAtCloseout()
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, ReclaimSelfReleasedMarker) {
		t.Fatalf("the close-out must release the ground of its OWN row %s; lines=%v", taskID, lines)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the ground %s must be gone after the release (stat err=%v); lines=%v", wt, err, lines)
	}
	if worktreeListed(t, repo, wt) {
		t.Fatalf("the registration %s must be dropped from git as well; lines=%v", wt, lines)
	}
	// The BRANCH is never touched — it is the deliverable.
	if out := gitOut(t, repo, "rev-parse", "--verify", "--quiet", ownGroundCloseoutRepoBranch); out == "" {
		t.Fatalf("the branch %s must survive the directory's release", ownGroundCloseoutRepoBranch)
	}
}

// TestSelfGroundReleaseIsDisarmedWhenThePassEnds is the boundary of ①: the mark
// is armed BY the pass and disarmed when it ends, so nothing outside it inherits
// the exemption. Asserted on the observable arming, not on a comment: an empty
// mark is what every other caller of the shared guards sees.
func TestSelfGroundReleaseIsDisarmedWhenThePassEnds(t *testing.T) {
	repo, wt, _ := ownGroundCloseoutFixture(t)
	t.Chdir(wt)
	ReclaimOwnFinishedWorktrees()
	if armed := SelfGroundReleaseArmedGrounds(); len(armed) != 0 {
		t.Fatalf("the mark must be disarmed after the pass (armed=%v)", armed)
	}
	if SelfGroundReleasePendingFor(wt) {
		t.Fatalf("%s must not read as the close-out's ground outside the pass", wt)
	}
	_ = repo
}

// TestSelfGroundReleaseKeepsEveryForeignRegistration is ② — the reversed
// direction, and the one that must stay GREEN whatever the exemption does:
//
//	(a) UNARMED, the own row's registration is KEPT by the age floor: a
//	    registration this process merely has a row for is not yet a close-out's
//	    ground, so rung ①'s anti-race protection stands untouched.
//	(b) ARMED, ANOTHER row's ground is still KEPT: the exemption is membership,
//	    not "a close-out is running".
func TestSelfGroundReleaseKeepsEveryForeignRegistration(t *testing.T) {
	t.Run("unarmed, the age floor still keeps a fresh registration", func(t *testing.T) {
		repo, wt, _ := ownGroundCloseoutFixture(t)
		t.Chdir(wt)
		// No close-out pass: the row is this process's own (the claim stamp
		// matches) and the directory is the asker's own ground — exactly the
		// state ① releases — and the floor must still keep it.
		why := reclaimSelfOwnWhy(worktreeRegistration{Path: wt, Branch: ownGroundCloseoutRepoBranch}, repo, scratch.HolderScan{})
		if !strings.Contains(why, "younger than the") {
			t.Fatalf("without the close-out mark the anti-race floor must keep a fresh registration, got %q", why)
		}
	})

	t.Run("armed, another row's ground is not in the marks", func(t *testing.T) {
		repo, wt, _ := ownGroundCloseoutFixture(t)
		_ = repo
		other := filepath.Join(t.TempDir(), "someone-elses-ground")
		disarm := SelfGroundReleaseArm([]string{other})
		defer disarm()
		if SelfGroundReleasePendingFor(wt) {
			t.Fatalf("%s is NOT this close-out's ground — the mark must not cover it", wt)
		}
		// A look-alike spelling is not a membership either: the marks compare
		// canonical paths exactly, so a parent directory never matches.
		if SelfGroundReleasePendingFor(filepath.Dir(other)) {
			t.Fatalf("%s is the PARENT of an armed ground — a partial match would free a directory nobody armed", filepath.Dir(other))
		}
	})
}

// TestReclaimGroundChecklistReleasesTheArmedOwnGround pins the same fact at the
// SHARED CHECKLIST rung (reclaim_ground_checklist.go), which is where the
// guest/own-ground refusals are asked: the ordinary entry point
// (reclaimGroundChecklistVerdict — what the sweep and the unowned-registration
// pass call) must NOT refuse a directory the current close-out pass has armed,
// while a directory it did NOT arm is refused exactly as before.
//
// Rung ② (the lineage check) is not under test here: the fixture's branch is
// cut from master, so it shares history and the rung passes — the verdict
// therefore isolates rung ①, which is the rung armed by the mark.
func TestReclaimGroundChecklistReleasesTheArmedOwnGround(t *testing.T) {
	repo, wt, _ := ownGroundCloseoutFixture(t)
	t.Chdir(wt)

	// Unarmed: the asker stands in the ground, so rung ① refuses it —
	// guestRowOwnCheckoutRefusal's own sentence (a process never frees the
	// checkout it stands in). This is the direction the breach of 2026-09-21
	// came through.
	if why := reclaimGroundChecklistVerdict(repo, wt, ownGroundCloseoutRepoBranch); !strings.Contains(why, "own-ground refusal") {
		t.Fatalf("outside a close-out pass the checklist must refuse the ground the asker stands in, got %q", why)
	}

	// Armed: the same directory, as the ground the pass is closing out.
	disarm := SelfGroundReleaseArm([]string{wt})
	defer disarm()
	if why := reclaimGroundChecklistVerdict(repo, wt, ownGroundCloseoutRepoBranch); why != "" {
		t.Fatalf("the checklist must let the close-out's own armed ground through, got %q", why)
	}
}

// TestSelfGroundReleaseArmIgnoresAnEmptyGroundSet: arming with nothing must
// leave nothing armed. An empty set rendering as "everything is mine" is the
// failure direction that costs a live directory, so it is pinned.
func TestSelfGroundReleaseArmIgnoresAnEmptyGroundSet(t *testing.T) {
	disarm := SelfGroundReleaseArm([]string{"", "   "})
	defer disarm()
	if armed := SelfGroundReleaseArmedGrounds(); len(armed) != 0 {
		t.Fatalf("an empty ground set must arm nothing, got %v", armed)
	}
	if SelfGroundReleasePendingFor("/") {
		t.Fatalf("an empty ground set must not match any path")
	}
	// A disarm with nothing armed is a no-op, never a panic — the producers
	// spell the pair in one breath (defer done()) on every path.
	disarm()
}

// TestSelfGroundReleaseArmedGroundIsCanonical pins the comparison the marks rely
// on: a spelling that resolves to the same directory matches, one that merely
// shares a prefix does not. Without this, a caller passing the fixture's
// un-evaluated path (a symlinked scratch root is the ordinary case) would miss
// its own ground and the defect would survive in a different disguise.
func TestSelfGroundReleaseArmedGroundIsCanonical(t *testing.T) {
	root := t.TempDir()
	ground := filepath.Join(root, "ground")
	if err := os.MkdirAll(ground, 0o755); err != nil {
		t.Fatal(err)
	}
	disarm := SelfGroundReleaseArm([]string{ground + string(filepath.Separator)})
	defer disarm()
	if !SelfGroundReleasePendingFor(ground) {
		t.Fatalf("a trailing-separator spelling must name the same ground")
	}
	if SelfGroundReleasePendingFor(filepath.Join(root, "ground-sibling")) {
		t.Fatalf("a sibling whose name shares a prefix must never match")
	}
}

// TestSelfGroundReleaseArmsOnlyTheLaunchGround pins the WIDTH of the mark, and
// it is the direction that keeps the 2026-09-21 breach shut: a ground some row
// NAMES is never in the marks, so the ledger-keyed arms keep refusing the
// checkout a run stands in (the pre-existing criterion
// TestReclaimSelfRefusesItsOwnCheckout). Only the directory the run was LAUNCHED
// in — named by no row — is ever armed.
func TestSelfGroundReleaseArmsOnlyTheLaunchGround(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	recorded := filepath.Join(root, "recorded-ground")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "task/recorded", recorded)
	gitRun(t, repo, "worktree", "add", "-q", "-b", "task/launch", wt)
	t.Chdir(wt)
	SetSelfGroundLaunchDir(wt)
	t.Cleanup(func() { SetSelfGroundLaunchDir("") })

	rows := []reclaimDeadHolderRow{{TaskID: "t-a", Repo: repo, Worktree: recorded}}
	disarm := SelfGroundReleaseArmCloseout(rows)
	armed := SelfGroundReleaseArmedGrounds()
	if len(armed) != 1 {
		t.Fatalf("exactly the launch ground must be armed, got %v", armed)
	}
	if !SelfGroundReleasePendingFor(wt) {
		t.Fatalf("the launch ground %s must be armed", wt)
	}
	if SelfGroundReleasePendingFor(recorded) {
		t.Fatalf("the ROW'S RECORDED ground %s must never be armed — a named ground is refused by the ledger-keyed arms, and that is the breach of 2026-09-21", recorded)
	}
	// No launch candidate → nothing armed, even with rows in hand. The first arm
	// is released FIRST, so this asserts the second arm's own content rather than
	// the outer mark the save/restore semantics deliberately keep.
	disarm()
	SetSelfGroundLaunchDir("")
	disarm2 := SelfGroundReleaseArmCloseout(rows)
	defer disarm2()
	if got := SelfGroundReleaseArmedGrounds(); len(got) != 0 {
		t.Fatalf("with no launch candidate and only recorded grounds, nothing may be armed, got %v", got)
	}
}

// TestSelfGroundReleaseMarkIsNotStaleAcrossPasses: two close-outs in one process
// (the worker round serves many) must not leave the first pass's grounds armed
// for the second — a stale mark is a directory nobody is closing out.
func TestSelfGroundReleaseMarkIsNotStaleAcrossPasses(t *testing.T) {
	disarmFirst := SelfGroundReleaseArm([]string{"/tmp/first-pass"})
	disarmFirst()
	disarmSecond := SelfGroundReleaseArm([]string{"/tmp/second-pass"})
	defer disarmSecond()
	if SelfGroundReleasePendingFor("/tmp/first-pass") {
		t.Fatalf("the first pass's ground must not stay armed into the second")
	}
	if !SelfGroundReleasePendingFor("/tmp/second-pass") {
		t.Fatalf("the current pass's ground must be armed")
	}
}

// TestSelfGroundLaunchDirAdmissionConditions pins the three conditions the
// close-out applies before it may call a directory its own ground: it is a
// LINKED worktree, it belongs to a closing row's repo, and it is only ever
// reached through the armed mark. Diagnostic value is deliberate — this test
// asserts the intermediate facts, so a future failure says WHICH condition
// changed rather than "the ground was not released".
func TestSelfGroundLaunchDirAdmissionConditions(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	gitRun(t, repo, "worktree", "add", "-b", "task/launch-ground", wt)
	t.Chdir(wt)

	if !inLinkedWorktreeRoot(wt) {
		t.Fatalf("condition ①: %s must read as a linked worktree", wt)
	}
	kind := scratchCanonicalGround(reclaimSelfOwnRepoOf(wt))
	if kind == "" {
		t.Fatalf("condition ②: git must name the repo %s belongs to", wt)
	}
	if kind != scratchCanonicalGround(repo) {
		t.Fatalf("condition ②: %s resolves to %q, want %q", wt, kind, scratchCanonicalGround(repo))
	}
	if !selfGroundLaunchDirArmable(wt, []string{repo}) {
		t.Fatalf("the launch ground %s must be armable for a closing row of %s", wt, repo)
	}
	// The three refusals, one per condition — each asserted so a widening of the
	// admission shows up as a named failure.
	if selfGroundLaunchDirArmable("", []string{repo}) {
		t.Fatalf("an unrecorded launch directory must never be armable")
	}
	if selfGroundLaunchDirArmable(repo, []string{repo}) {
		t.Fatalf("condition ①: the MAIN checkout must never be armable as a launch ground")
	}
	if selfGroundLaunchDirArmable(wt, []string{filepath.Join(root, "other-repo")}) {
		t.Fatalf("condition ②: a directory of another repo must never be armable")
	}
	if selfGroundLaunchDirArmable(wt, nil) {
		t.Fatalf("condition ②: with no closing row there is no repo to belong to")
	}
}

// TestSelfGroundReleaseMarkComposesAcrossNestedPasses pins the ONE property that
// cost this change two measured failures: an arm taken at one depth must survive
// the disarm of a NESTED arm taken inside it.
//
// MEASURED (master, this change's own iterations): the close-out seam arms both
// halves, then the LEDGER-KEYED half arms its own mark around itself and disarmed
// it on return — and because that disarm CLEARED rather than RESTORED, the
// ownership-independent half then ran unmarked. The symptom was the age floor's
// sentence a second time, for the very ground the seam had just armed
// (`own registered worktree kept: …/001/wt — younger than the 5m0s floor`), i.e. a
// mark that silently evaporates when two passes nest. Composing (save/restore) is
// what makes the seam's mark mean what it says; this test is its guard.
func TestSelfGroundReleaseMarkComposesAcrossNestedPasses(t *testing.T) {
	outerDir := t.TempDir()
	innerDir := t.TempDir()
	otherDir := t.TempDir()

	disarmOuter := SelfGroundReleaseArm([]string{outerDir})
	defer disarmOuter()
	if !SelfGroundReleasePendingFor(outerDir) {
		t.Fatalf("the outer mark must be armed")
	}

	// A nested pass arms its own set and disarms it on return — the shape both
	// producer passes have.
	disarmInner := SelfGroundReleaseArm([]string{innerDir})
	if !SelfGroundReleasePendingFor(innerDir) {
		t.Fatalf("a nested arm's own set must be in force while it is armed")
	}
	if SelfGroundReleasePendingFor(outerDir) {
		t.Fatalf("a nested arm carries what ITS pass knows — the inner set is the one in force (the disarm restores the outer one)")
	}
	disarmInner()
	if !SelfGroundReleasePendingFor(outerDir) {
		t.Fatalf("the nested disarm must RESTORE the outer mark, not clear it (measured failure: the ground stayed kept)")
	}
	if SelfGroundReleasePendingFor(innerDir) {
		t.Fatalf("the nested arm's own set must be gone after its disarm")
	}

	// An arm taken inside a nested arm restores to the NESTED set, not to empty:
	// the stack unwinds one level at a time.
	disarmInner2 := SelfGroundReleaseArm([]string{innerDir})
	disarmOther := SelfGroundReleaseArm([]string{otherDir})
	disarmOther()
	if !SelfGroundReleasePendingFor(innerDir) || SelfGroundReleasePendingFor(otherDir) {
		t.Fatalf("two levels of nesting must unwind one level at a time")
	}
	disarmInner2()
	if SelfGroundReleasePendingFor(innerDir) || !SelfGroundReleasePendingFor(outerDir) {
		t.Fatalf("after the full unwind only the outer mark may remain")
	}
}

// TestSelfGroundReleaseArmCloseoutPassArmsFromTheLedger pins the seam's own
// arming: with rows in the ledger whose claim stamp is this process's, the pass
// arms the launch candidates — and with an unreadable/empty ledger it arms
// nothing (a permission nothing can grant).
func TestSelfGroundReleaseArmCloseoutPassArmsFromTheLedger(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	recorded := filepath.Join(root, "recorded")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "task/rec2", recorded)
	gitRun(t, repo, "worktree", "add", "-q", "-b", "task/launch2", wt)
	t.Chdir(wt)
	SetSelfGroundLaunchDir(wt)
	t.Cleanup(func() { SetSelfGroundLaunchDir("") })
	seedLiveStatusRow(t, "t-arm-closeout-pass", repo, "feat/x", recorded, reclaimSelfStamp())

	done := selfGroundReleaseArmCloseoutPass()
	defer done()
	if !SelfGroundReleasePendingFor(wt) {
		t.Fatalf("the pass's own arming must cover the launch ground of its live row")
	}
	if SelfGroundReleasePendingFor(recorded) {
		t.Fatalf("the pass's arming must NOT cover a ground a row names")
	}
}
