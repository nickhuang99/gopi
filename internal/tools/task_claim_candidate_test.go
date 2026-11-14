package tools

// The claim-time CANDIDATE half of the ground stamp (gap A of the
// TaskWorktreeStampClaimerFor follow-up, epic 01a0bb4d).
//
// THE GAP THESE TESTS PIN. TaskWorktreeStampClaimerFor resolves the ground of
// the directory the executor child is ABOUT to start in. When that directory is
// the row's repo — the dominant wrapper shape — the main-checkout arm reads the
// repo's own HEAD, which at claim time is master: EXEMPT, so nothing is stamped
// and the row keeps its submit-time {"repo":…} blob. That is right for a run
// that will create its ground mid-run and wrong for the run whose ground was
// ALREADY created (a provisioning step, a resume, a sibling process) — the
// creation seam may never fire, so the row stays repo-only for its whole life.
//
// Measured on the live ledger (2026-09-20): row 01a0bb71-ec9d sat in_progress
// with {"repo":"/home/user/work/GoPi"} while /home/user/workspace/gopi-chat-wrap
// existed and was registered as that repo's linked worktree on a non-exempt
// branch. Measured too: that row's session LAUNCHED in the main checkout
// (workdir=/home/user/work/GoPi) — byte-identical to its already-grounded
// sibling 01a0bb71-eae2's — and the ONLY thing separating the two runs is the
// `git worktree add … -b feat/chat-stream-tty-wrap` the gap-A run itself
// executed. Hence the seam's central rule, pinned below:
//
//	THE SESSION READING IS REQUIRED EVIDENCE, NOT A TIE-BREAKER.
//
// The registry alone may never bind. In exactly this shape the run standing in
// the worktree records NOTHING on its own row, so taskWorktreeRowOwner /
// taskBranchRowOwner — which scan for rows that NAME the worktree/branch — are
// blind to it BY CONSTRUCTION; a registry-only bind would hand this row a LIVE
// foreign run's directory (the 2026-09-12 foreign-owner hijack, one level up).
// A row with no provable session reading therefore stays repo-only, exactly as
// before this seam existed.
//
// The tests below are RED before the change by construction: the candidate
// resolver does not exist yet, so the row keeps its repo-only blob and nothing
// is logged. Each acceptance case maps to one test:
//
//	(a) the session names a registered worktree, unowned ⇒ bound (both axes)
//	(b) the same, but ANOTHER row already names that worktree ⇒ no bind + line
//	(c) the same, but another row claims the BRANCH ⇒ no bind + line
//	(d) two registered, the session names ONE ⇒ bound (evidence disambiguates)
//	(d2) the session names a worktree the repo does NOT register ⇒ no bind + line
//	(e) the session's ground was unregistered (removed) ⇒ no bind + line
//	(e2) NO session reading at all ⇒ no bind + line (the registry never decides)
//	(f) an unprovable ownership scan ⇒ FAIL-CLOSED: no bind (never a proceed)
//	(g) a row that already names a ground is NEVER re-pointed
//
// Fixtures are real-git: worktree/porcelain semantics are git-truth, the claim
// path's whole question is "what does this repo have registered?", and the
// evidence rule's subject is a real session JSONL.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// claimCandFixture is a real repo whose MAIN checkout is on master (the
// exempt head the claim path reads) with ONE linked worktree on a non-exempt
// branch — the measured gap-A shape.
type claimCandFixture struct {
	t      *testing.T
	root   string
	repo   string
	wt     string
	branch string
}

func newClaimCandFixture(t *testing.T, branch string) *claimCandFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	f := &claimCandFixture{
		t:      t,
		root:   root,
		repo:   filepath.Join(root, "repo"),
		wt:     filepath.Join(root, "workspace", "gopi-claimed-ground"),
		branch: branch,
	}
	deliverableWTGit(t, root, "init", f.repo)
	deliverableWTGit(t, f.repo, "config", "user.email", "t@t")
	deliverableWTGit(t, f.repo, "config", "user.name", "t")
	deliverableWTGit(t, f.repo, "checkout", "-b", masterBranchName)
	if err := os.WriteFile(filepath.Join(f.repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, f.repo, "add", "a.txt")
	deliverableWTGit(t, f.repo, "commit", "-m", "base")
	deliverableWTGit(t, f.repo, "worktree", "add", "-b", branch, f.wt, masterBranchName)
	return f
}

// claimCandAddWorktree registers a SECOND linked worktree of the same repo on
// its own non-exempt branch and returns its path — the ambiguity fixture.
func (f *claimCandFixture) claimCandAddWorktree(name, branch string) string {
	f.t.Helper()
	second := filepath.Join(f.root, "workspace", name)
	deliverableWTGit(f.t, f.repo, "worktree", "add", "-b", branch, second, masterBranchName)
	return second
}

// claimCandNoGroundCommand is a command that creates no ground: it is what a
// session carries when the run has not created its ground yet. Named (Rule 1b)
// because several tests distinguish "no reading" from "a reading".
const claimCandNoGroundCommand = "git status --short"

// claimCandSeedSession writes the row's OWN run session — the EVIDENCE this
// seam requires — and points the row at it.
//
// workdir is where the run LAUNCHED. Every fixture here launches in the repo's
// main checkout, because that is the measured gap-A shape and the reason the
// launch directory alone can never decide this question. command is what the
// run executed: a `git worktree add … -b …` names the ground the run chose;
// claimCandNoGroundCommand carries no ground at all.
func claimCandSeedSession(t *testing.T, db, taskID, command, workdir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	sessDir := filepath.Join(home, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessID := "claim-cand-" + taskID
	start := fmt.Sprintf(`{"type":"session_start","sessionId":%q,"workdir":%q}`, sessID, workdir)
	use := fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":%q,"input":{"command":%q}}]}}`,
		toolnames.ToolBash, command)
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"), []byte(start+"\n"+use+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `UPDATE tasks SET session_id = ? WHERE task_id = ?`, sessID, taskID); err != nil {
		t.Fatalf("point the row at its session: %v", err)
	}
}

// claimCandSeedGroundedRow seeds a repo-only row AND the session that proves it
// stands on the given worktree — the arrangement every adoption case starts
// from. Returns the raw deliverable blob's producer via inPlaceRowBlob's fn.
func claimCandSeedGroundedRow(t *testing.T, f *claimCandFixture, taskID, wt, branch string) {
	t.Helper()
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), taskID, f.repo)
	claimCandSeedSession(t, os.Getenv(memoryDBEnv), taskID,
		fmt.Sprintf("git worktree add %s -b %s %s", wt, branch, masterBranchName), f.repo)
}

// claimCandRowBlob runs fn against an ALREADY-SEEDED row and returns the row's
// raw deliverable blob afterwards. Unlike inPlaceRowBlob it seeds nothing: the
// candidate fixtures seed the row and its session together (claimCandSeed…),
// and seeding twice would violate the ledger's task_id uniqueness — a fixture
// arranging its own subject, not a contract.
func claimCandRowBlob(t *testing.T, taskID string, fn func()) string {
	t.Helper()
	fn()
	return deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), taskID)
}

// claimCandLog capture: the INFO line a considered-and-refused candidate
// produces (Rule 15). The seam is the package's log sink, rebound for the
// duration of the test — production rebinds nothing (precedent:
// taskWorktreeOwnerScan).
func claimCandCaptureLog(t *testing.T) *[]string {
	t.Helper()
	var lines []string
	prev := taskClaimGroundLog
	taskClaimGroundLog = func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { taskClaimGroundLog = prev })
	return &lines
}

// setSessionEnv points the session lookup at a per-test GOPI_HOME (the lookup
// resolves the row's log through config.GopiDir, which GOPI_HOME pins).
func setSessionEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
}

// TestClaimCandidateBindsTheRegisteredWorktree is case (a): a row claimed with
// its cwd at the repo's MAIN checkout, whose OWN session executed the
// worktree-add for a worktree this repo registers on a non-exempt branch,
// records that worktree + branch while still in flight.
func TestClaimCandidateBindsTheRegisteredWorktree(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-one")
	lines := claimCandCaptureLog(t)
	claimCandSeedGroundedRow(t, f, "t-claim-cand", f.wt, f.branch)

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		_, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo)
		if !written {
			t.Fatalf("claim-time stamp must bind the session's own ground (repo %s, worktree %s, branch %s); log:\n%s",
				f.repo, f.wt, f.branch, strings.Join(*lines, "\n"))
		}
	})

	var d struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		t.Fatalf("blob is not valid JSON (%s): %v", blob, err)
	}
	if d.Worktree != f.wt {
		t.Errorf("row must name the session's worktree: got %q, want %q (blob %s)", d.Worktree, f.wt, blob)
	}
	if d.Branch != f.branch {
		t.Errorf("row must name the session's branch: got %q, want %q (blob %s)", d.Branch, f.branch, blob)
	}
	if d.Repo != f.repo {
		t.Errorf("repo must be preserved: got %q, want %q", d.Repo, f.repo)
	}
}

// TestClaimCandidateRefusedWhenAnotherRowNamesTheWorktree is case (b): the ONE
// WORKTREE, ONE OWNING ROW guard (the 2026-09-12 foreign-owner hijack). The
// session may prove the ground, but a worktree another row already NAMES is
// that row's — so THIS claim records nothing, and says so in one line.
func TestClaimCandidateRefusedWhenAnotherRowNamesTheWorktree(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-owned")
	lines := claimCandCaptureLog(t)
	// Another row already owns this worktree.
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), "t-claim-cand-other", f.repo)
	if err := taskBackfillDeliverableWorktree("t-claim-cand-other", f.wt, f.branch); err != nil {
		t.Fatalf("seed owner row: %v", err)
	}
	claimCandSeedGroundedRow(t, f, "t-claim-cand", f.wt, f.branch)

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		if _, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo); written {
			t.Errorf("a worktree already named by another row must NEVER be bound to this one")
		}
	})
	if strings.Contains(blob, DeliverableWorktreeKey) {
		t.Errorf("refused claim must leave the row repo-only: %s", blob)
	}
	assertClaimCandLineNames(t, lines, f.wt)
}

// TestClaimCandidateRefusedWhenAnotherRowClaimsTheBranch is case (c): the
// BRANCH axis of the same one-owner rule. A ground whose branch another row
// already claims is refused even when the worktree directory is unowned.
func TestClaimCandidateRefusedWhenAnotherRowClaimsTheBranch(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-branchowned")
	lines := claimCandCaptureLog(t)
	// Another row names the BRANCH (no worktree in its blob).
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), "t-claim-cand-branchowner", f.repo)
	if _, err := taskBackfillDeliverableWorktreeAt("t-claim-cand-branchowner", "", f.branch, &f.repo, taskWorktreeStampMomentClaim); err != nil {
		t.Fatalf("seed branch owner: %v", err)
	}
	claimCandSeedGroundedRow(t, f, "t-claim-cand", f.wt, f.branch)

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		if _, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo); written {
			t.Errorf("a branch already named by another row must NEVER be bound to this one")
		}
	})
	if strings.Contains(blob, DeliverableBranchKey) || strings.Contains(blob, DeliverableWorktreeKey) {
		t.Errorf("refused claim must leave the row repo-only: %s", blob)
	}
	assertClaimCandLineNames(t, lines, f.branch)
}

// TestClaimCandidateTwoRegisteredTheSessionDecides is case (d): two live
// candidates are registered and the claim dir names NEITHER — the registry
// alone cannot answer. The session CAN: it names the worktree the run itself
// created, so the row binds to THAT one and the second registration is
// irrelevant. (Guessing is what this seam refuses; resolving is what it does.)
func TestClaimCandidateTwoRegisteredTheSessionDecides(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-a")
	second := f.claimCandAddWorktree("gopi-second", "feat/claim-cand-b")
	claimCandSeedGroundedRow(t, f, "t-claim-cand", second, "feat/claim-cand-b")

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		if _, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo); !written {
			t.Errorf("the session names ONE of the two registered worktrees — that one must bind")
		}
	})
	if !strings.Contains(blob, second) {
		t.Errorf("the row must name the worktree the session created (%s): %s", second, blob)
	}
	if strings.Contains(blob, f.wt) {
		t.Errorf("the OTHER registration must not bind — nothing selected it: %s", blob)
	}
}

// TestClaimCandidateRefusedWhenSessionReadingIsStale is case (d2): the row's
// session names a branch that NO LONGER EXISTS in the repo — a resume whose
// branch was deleted, or a session read across a rewrite. A stale reading
// proves nothing about where the run stands, so it binds nothing and says why
// (the same verification the terminal writeback applies before adopting).
func TestClaimCandidateRefusedWhenSessionReadingIsStale(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-live")
	ghost := filepath.Join(f.root, "workspace", "gopi-gone")
	lines := claimCandCaptureLog(t)
	// The session names a ground whose BRANCH was never created in this repo.
	claimCandSeedGroundedRow(t, f, "t-claim-cand", ghost, "feat/claim-cand-ghost")

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		if _, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo); written {
			t.Errorf("a STALE session reading (its branch is gone) must NEVER bind")
		}
	})
	if strings.Contains(blob, DeliverableWorktreeKey) || strings.Contains(blob, DeliverableBranchKey) {
		t.Errorf("stale-reading claim must leave the row repo-only: %s", blob)
	}
	assertClaimCandLineNames(t, lines, "feat/claim-cand-ghost")
}

// TestClaimCandidateNoneRegisteredNamesTheRepo is case (e): the session names a
// ground whose registration is GONE (the worktree was removed). The candidate
// path runs, resolves nothing, and leaves ONE line that says so — the honest
// outcome.
func TestClaimCandidateNoneRegisteredNamesTheRepo(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-removed")
	// Remove the only candidate registration (the ground is gone) — the session
	// still names it, which is precisely the case the reader must not guess past.
	deliverableWTGit(t, f.repo, "worktree", "remove", f.wt)
	lines := claimCandCaptureLog(t)
	claimCandSeedGroundedRow(t, f, "t-claim-cand", f.wt, f.branch)

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		if _, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo); written {
			t.Errorf("no candidate is registered — nothing may be bound")
		}
	})
	if strings.Contains(blob, DeliverableWorktreeKey) || strings.Contains(blob, DeliverableBranchKey) {
		t.Errorf("candidate-less claim must leave the row repo-only: %s", blob)
	}
	assertClaimCandLineNames(t, lines, f.repo)
}

// TestClaimCandidateNoReadingNeverBindsTheRegistry is case (e2) and the CORE
// correctness pin of this seam: a row with NO provable session reading must not
// bind, no matter how clean the registry looks. This is exactly the shape
// TestClaimTimeGroundStampLeavesMainCheckoutRowRepoOnly builds (a claimed row
// whose run started in the main checkout, no session seeded), and the reason is
// not caution: the run standing in a registered worktree records nothing on its
// own row, so the ONE-OWNER guards cannot see it. Binding here would hand this
// row a live foreign run's directory.
func TestClaimCandidateNoReadingNeverBindsTheRegistry(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	setSessionEnv(t)
	f := newClaimCandFixture(t, "feat/claim-cand-reading")
	lines := claimCandCaptureLog(t)
	// The row exists; its session carries NO ground-bearing command.
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), "t-claim-cand", f.repo)
	claimCandSeedSession(t, os.Getenv(memoryDBEnv), "t-claim-cand", claimCandNoGroundCommand, f.repo)

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		if _, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo); written {
			t.Errorf("a registry-only reading must NEVER bind — the guards cannot see the run standing there")
		}
	})
	if strings.Contains(blob, DeliverableWorktreeKey) || strings.Contains(blob, DeliverableBranchKey) {
		t.Errorf("a no-reading claim must leave the row repo-only: %s", blob)
	}
	assertClaimCandLineNames(t, lines, f.repo)
}

// TestClaimCandidateFailClosedOnUnprovableOwnerScan is case (f): the ownership
// guard's ERROR is a REFUSAL to bind, never a proceed. An unreadable ledger
// cannot prove the candidate is unowned, and binding a directory another row
// may name is the hijack the guard exists to prevent (Rule 15, fail-closed).
//
// The scan is driven through the package's OWN seam (taskWorktreeOwnerScan) —
// a ledger-level corruption cannot separate "the guard refused" from "the write
// would have refused anyway", so the seam is the only way this branch is
// observable at all.
func TestClaimCandidateFailClosedOnUnprovableOwnerScan(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-unprovable")
	lines := claimCandCaptureLog(t)
	prev := taskWorktreeOwnerScan
	taskWorktreeOwnerScan = func(string, string) (string, error) {
		return "", errClaimCandUnprovable
	}
	t.Cleanup(func() { taskWorktreeOwnerScan = prev })
	claimCandSeedGroundedRow(t, f, "t-claim-cand", f.wt, f.branch)

	blob := claimCandRowBlob(t, "t-claim-cand", func() {
		if _, _, written := taskWorktreeStampClaimer("t-claim-cand", f.repo, f.repo); written {
			t.Errorf("an UNPROVABLE ownership scan must refuse the bind (fail-closed), never proceed")
		}
	})
	if strings.Contains(blob, DeliverableWorktreeKey) || strings.Contains(blob, DeliverableBranchKey) {
		t.Errorf("fail-closed claim must leave the row repo-only: %s", blob)
	}
	assertClaimCandLineNames(t, lines, f.wt)
}

// claimCandLineMarker is the stable, human-readable prefix of the candidate
// INFO line — the substring a reader (here and an operator) greps for. The
// full spelling stays in the production constant; this marker is only what the
// test asserts ON, so a reworded suffix does not silently un-pin the test.
const claimCandLineMarker = "no candidate bound at the main checkout"

// errClaimCandUnprovable is the synthetic guard failure case (f) injects.
var errClaimCandUnprovable = errClaimCandUnprovableType("ledger unreadable")

type errClaimCandUnprovableType string

func (e errClaimCandUnprovableType) Error() string { return string(e) }

// TestClaimCandidateNeverRepointsAGroundedRow is case (g): the candidate path
// closes an EMPTY ground and nothing else. A row already naming a branch (or a
// worktree) keeps it — a re-point is exactly the hijack.
func TestClaimCandidateNeverRepointsAGroundedRow(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-repoint")
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), "t-claim-cand-grounded", f.repo)
	if _, err := taskBackfillDeliverableWorktreeAt("t-claim-cand-grounded", "", "feat/claim-cand-own", &f.repo, taskWorktreeStampMomentClaim); err != nil {
		t.Fatalf("seed grounded row: %v", err)
	}

	before := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-claim-cand-grounded")
	if _, _, written := taskWorktreeStampClaimer("t-claim-cand-grounded", f.repo, f.repo); written {
		t.Errorf("a row that already names a ground must never be re-pointed")
	}
	after := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-claim-cand-grounded")
	if before != after {
		t.Errorf("grounded row's blob changed: %s → %s", before, after)
	}
	if strings.Contains(after, f.wt) {
		t.Errorf("the candidate worktree must not appear on a grounded row: %s", after)
	}
}

// TestClaimCandidateLineIsAbsentForAGroundedRow pins the OTHER half of the
// logging contract: "already grounded" is NOT a refusal, so it must not emit
// the candidate INFO line (a signal on every ordinary claim is noise, and noise
// is how a real miss gets ignored).
func TestClaimCandidateLineIsAbsentForAGroundedRow(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newClaimCandFixture(t, "feat/claim-cand-quiet")
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), "t-claim-cand-quiet", f.repo)
	if _, err := taskBackfillDeliverableWorktreeAt("t-claim-cand-quiet", f.wt, f.branch, &f.repo, taskWorktreeStampMomentClaim); err != nil {
		t.Fatalf("seed grounded row: %v", err)
	}
	lines := claimCandCaptureLog(t)

	taskWorktreeStampClaimer("t-claim-cand-quiet", f.repo, f.repo)
	for _, l := range *lines {
		// The CANDIDATE line is the one this contract governs: "already
		// grounded" is not a refusal, so it must not be reported as one. (The
		// pre-existing "no non-exempt branch at the claim dir" INFO line is a
		// different fact — the claim dir's own HEAD — and keeps firing, exactly
		// as it did before this change.)
		if strings.Contains(l, claimCandLineMarker) {
			t.Errorf("an already-grounded row must not report a refused candidate, got %q", l)
		}
	}
}

// assertClaimCandLineNames fails when no captured line carries the INFO
// spelling and the given needle — the ONE honest line a refused/ambiguous/
// absent candidate must leave behind.
func assertClaimCandLineNames(t *testing.T, lines *[]string, needles ...string) {
	t.Helper()
	joined := strings.Join(*lines, "\n")
	if !strings.Contains(joined, "no candidate bound") {
		t.Fatalf("a candidate path that bound nothing must leave ONE INFO line, got:\n%s", joined)
	}
	for _, n := range needles {
		if !strings.Contains(joined, n) {
			t.Fatalf("the INFO line must name %q, got:\n%s", n, joined)
		}
	}
}

// TestClaimGroundCandidatesSkipsDetachedAndExempt pins the candidate READER
// itself: only a linked worktree on a NON-exempt branch is an adoptable
// ground — a detached worktree names no branch, and master/main/archive-* is
// not a deliverable (taskBranchExempt).
func TestClaimGroundCandidatesSkipsDetachedAndExempt(t *testing.T) {
	f := newClaimCandFixture(t, "feat/claim-cand-adoptable")
	detached := filepath.Join(f.root, "workspace", "gopi-detached")
	deliverableWTGit(t, f.repo, "worktree", "add", "--detach", detached, masterBranchName)

	cands, err := taskClaimGroundCandidates(f.repo)
	if err != nil {
		t.Fatalf("taskClaimGroundCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("only the non-exempt, branch-carrying worktree is a candidate: got %+v", cands)
	}
	if cands[0].worktree != f.wt || cands[0].branch != f.branch {
		t.Errorf("candidate mismatch: got %+v, want {%s %s}", cands[0], f.wt, f.branch)
	}
	// A MISSING directory is a registration, not a place: the candidate reader
	// must drop it so a claim never binds a row to a directory that is gone.
	if err := os.RemoveAll(f.wt); err != nil {
		t.Fatal(err)
	}
	got, err := taskClaimGroundCandidates(f.repo)
	if err != nil {
		t.Fatalf("taskClaimGroundCandidates after removal: %v", err)
	}
	for _, c := range got {
		if c.worktree == f.wt {
			t.Errorf("a worktree whose directory is GONE must not be a candidate: %+v", got)
		}
	}
}
