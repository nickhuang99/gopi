package tools

// task_briefing_base_submit_test.go — the submit seam's base-staleness warning,
// in TWO ARMS against a real thrown-away git repository.
//
// THE ARM THAT MUST FIRE: a payload declaring a base revision that is an
// ancestor of the integration branch but strictly behind it yields a STATED
// measured warning — the base, the tip and the commits-ahead count, each with
// the command that produced it — appended to TaskSubmit's confirmation, while
// the row still queues and is queryable afterwards.
//
// THE ARM THAT MUST STAY SILENT (the positive control, and the reason this is a
// warning rather than a gate): a payload whose declared base IS the integration
// tip produces NO warning. A guard that fired on the honest submit would be as
// useless as one that never fired.
//
// WHY A REAL REPO AND NOT A FIXTURE OF STRINGS: "strictly behind" is a property
// of the object graph (`git merge-base --is-ancestor` + `git rev-list --count`),
// so only real git can prove it — a stubbed measurement would prove the stub.
// The repo is thrown away under the ledger scaffold's scratch root, OFF the /
// partition (Rule 22), and the ledger comes from the scaffold (Rule 32), never a
// hand-rolled path.
//
// Run this file's tests:  go test -run TestSubmitBaseStaleness ./internal/tools/

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// The git spellings this fixture drives directly (Rule 1: a meaningful literal
// is named, even in a test — a re-typed "commit" is a typo waiting to happen).
const (
	baseStaleGitName      = "git"
	baseStaleGitInit      = "init"
	baseStaleGitConfig    = "config"
	baseStaleGitAdd       = "add"
	baseStaleGitCommit    = "commit"
	baseStaleGitRevParse  = "rev-parse"
	baseStaleGitRevList   = "rev-list"
	baseStaleGitHead      = "HEAD"
	baseStaleStateFile    = "state.txt"
	baseStaleAuthorEmail  = "t@t"
	baseStaleAuthorName   = "t"
	baseStaleRootFlag     = "--max-parents=0"
	baseStaleQuietFlag    = "-q"
	baseStaleAllFlag      = "-A"
	baseStaleMessageFlag  = "-m"
	baseStaleBranchFlag   = "-b"
	baseStaleCommitText   = "state"
	baseStaleShortShaWide = 8
	baseStaleAheadCount   = 2 // three commits: the root sits 2 behind the tip
	baseStaleCommitCount  = 3
)

// baseStaleFixture builds a real repository with an integration branch
// (IntegrationBranchName) of `commits` commits, and returns the repo path and
// the sha of its ROOT commit — the base a briefing measured its evidence at. The
// tip is the LAST commit, so the expected lead is `commits-1`.
//
// The directory comes from the ledger scaffold's device scratch root (Rule 22:
// measured off `/`, and removed by the scaffold, never left in the source tree).
func baseStaleFixture(t *testing.T, commits int) (repo, root string) {
	t.Helper()
	if _, err := exec.LookPath(baseStaleGitName); err != nil {
		t.Skip("git not installed")
	}
	repo = filepath.Join(testutil.TestDeviceScratchDir(t, "base-stale"), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repo, err)
	}
	for _, args := range [][]string{
		{baseStaleGitInit, baseStaleQuietFlag, baseStaleBranchFlag, IntegrationBranchName},
		{baseStaleGitConfig, "user.email", baseStaleAuthorEmail},
		{baseStaleGitConfig, "user.name", baseStaleAuthorName},
	} {
		if out, err := groundTestGit(repo, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for i := 0; i < commits; i++ {
		if err := os.WriteFile(filepath.Join(repo, baseStaleStateFile),
			[]byte(strings.Repeat(baseStaleCommitText+"\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := groundTestGit(repo, baseStaleGitAdd, baseStaleAllFlag); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}
		if out, err := groundTestGit(repo, baseStaleGitCommit, baseStaleQuietFlag,
			baseStaleMessageFlag, baseStaleCommitText); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
	}
	out, err := groundTestGit(repo, baseStaleGitRevList, baseStaleRootFlag, baseStaleGitHead)
	if err != nil {
		t.Fatalf("git rev-list root: %v\n%s", err, out)
	}
	root = strings.TrimSpace(strings.Split(out, "\n")[0])
	if root == "" {
		t.Fatalf("fixture degenerate: the repository has no root commit")
	}
	// THE INSTRUMENT FIRST (Rule 10[4] — verify the smallest sample): the base
	// and the tip must be DIFFERENT commits, or the staleness arm would be
	// measuring nothing.
	tip := baseStaleTip(t, repo)
	if tip == root {
		t.Fatalf("fixture degenerate: the base IS the tip (%s) so the staleness arm cannot fire", root)
	}
	return repo, root
}

// baseStaleTip reports the repository's HEAD sha.
func baseStaleTip(t *testing.T, repo string) string {
	t.Helper()
	out, err := groundTestGit(repo, baseStaleGitRevParse, baseStaleGitHead)
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

// baseStalePinEnv pins the ledger and the worktree root exactly as the sibling
// submit tests do (Rule 32 — the scaffold's database, never a hand-rolled one).
func baseStalePinEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	t.Setenv(worktreeroots.WorktreeRootEnvName, t.TempDir())
}

// baseStaleRowPayload reads the row's committed payload column straight from the
// ledger — the writeback's return value is not the evidence, the ROW is.
func baseStaleRowPayload(t *testing.T, taskID string) string {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var payload string
	if err := db.QueryRowContext(context.Background(),
		`SELECT payload FROM tasks WHERE task_id = ?`, taskID).Scan(&payload); err != nil {
		t.Fatalf("read payload of %s: %v", taskID, err)
	}
	return payload
}

// TestSubmitBaseStalenessWarnsOnAStaleDeclaredBase is arm ①: the warning FIRES,
// names the measured facts, and the row still queues.
func TestSubmitBaseStalenessWarnsOnAStaleDeclaredBase(t *testing.T) {
	baseStalePinEnv(t)
	repo, root := baseStaleFixture(t, baseStaleCommitCount)
	tip := baseStaleTip(t, repo)

	const rowID = "t-submit-base-stale"
	payload := "Measured at base " + root + ": the surface is red.\nReproduce with the commands below.\n"

	// THE PREDICATE, read alone first: the warning is the subject here, and a
	// failure should name it rather than the confirmation's formatting.
	warning := TaskSubmitBaseStalenessWarning(repo, payload)
	if warning == "" {
		t.Fatalf("a payload declaring base %s (%d commits behind %s) must yield a warning — the measured defect is a "+
			"briefing filed with a base the integration branch has already moved past", root, baseStaleAheadCount, tip)
	}
	for _, want := range []string{
		TaskSubmitBaseStaleLabel,
		TaskSubmitBaseStaleInstruction,
		masterClaimShortSha(root),
		masterClaimShortSha(tip),
		"2 commit(s) behind",
		"rev-list --count",
		"base " + root,
	} {
		if !strings.Contains(warning, want) {
			t.Errorf("the warning must carry %q (a warning without its measurement is a rumour):\n%s", want, warning)
		}
	}
	if !strings.Contains(warning, "still queues") {
		t.Errorf("the warning must say the row still queues — a refusal is the one thing this seam must never be:\n%s", warning)
	}

	// THE SURFACE the submitter actually reads (Rule 31): the confirmation of a
	// real TaskSubmit, with the row's repo stamped as this repository.
	out, id, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: "briefing declares a stale base", Payload: payload,
		Repo: repo, TaskID: rowID,
	})
	if err != nil {
		t.Fatalf("TaskSubmitReturningID: %v", err)
	}
	if id != rowID {
		t.Fatalf("row id: got %q, want %q", id, rowID)
	}
	if !strings.HasPrefix(out, "Task queued: ") {
		t.Fatalf("the confirmation must still open with the queued line (the warning is ADDITIVE):\n%s", out)
	}
	if !strings.Contains(out, warning) {
		t.Errorf("the confirmation the submitter reads must carry the warning the predicate produced:\n%s", out)
	}
	// THE ROW QUEUES (never a refusal): the row is on the ledger and claimable.
	if got := taskRowStatus(t, rowID); got != taskStatusPending {
		t.Errorf("the warning must never refuse the row: status=%q, want %q", got, taskStatusPending)
	}
	if got := baseStaleRowPayload(t, rowID); !strings.Contains(got, root) {
		t.Errorf("the row's payload must keep the author's text verbatim; got %q", got)
	}
}

// TestSubmitBaseStalenessIsSilentWhenTheBaseIsTheTip is arm ② — THE POSITIVE
// CONTROL: an honest briefing that declared the tip itself gets NO warning, so
// the check cannot be passing by firing on everything.
func TestSubmitBaseStalenessIsSilentWhenTheBaseIsTheTip(t *testing.T) {
	baseStalePinEnv(t)
	repo, _ := baseStaleFixture(t, baseStaleCommitCount)
	tip := baseStaleTip(t, repo)

	const rowID = "t-submit-base-current"
	payload := "Measured at base " + tip + ": the surface is red.\n"

	// THE INSTRUMENT FIRST: the declaration must really be DETECTED (otherwise
	// the silence would be the silence of a scanner that read nothing). The
	// scanner's look-ahead window bounds how FAR the revision may stand, never
	// how much of it is returned (row 01a1373b, 2026-10-15), so the token it
	// yields IS the author's full-length revision — assert equality; a prefix
	// relation here would let a clip pass unseen.
	claims := taskreport.BaseClaimsIn(payload)
	if len(claims) != 1 {
		t.Fatalf("the fixture's declaration must be detected; got %+v", claims)
	}
	if claims[0].Commit != tip {
		t.Fatalf("the declaration must be read as the author's full revision %s; got %+v", tip, claims)
	}
	if w := TaskSubmitBaseStalenessWarning(repo, payload); w != "" {
		t.Fatalf("a payload whose declared base IS the integration tip must yield NO warning (the positive control); got:\n%s", w)
	}

	out, _, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary: "briefing declares the current base", Payload: payload,
		Repo: repo, TaskID: rowID,
	})
	if err != nil {
		t.Fatalf("TaskSubmitReturningID: %v", err)
	}
	if strings.Contains(out, TaskSubmitBaseStaleLabel) {
		t.Errorf("the confirmation must carry no base-staleness warning for a current base:\n%s", out)
	}
	if got := taskRowStatus(t, rowID); got != taskStatusPending {
		t.Errorf("status=%q, want %q", got, taskStatusPending)
	}
}

// TestSubmitBaseStalenessIgnoresProseWithoutADeclaration is the third reading,
// small but load-bearing: the ordinary payload that merely DISCUSSES a base (no
// commit-shaped token beside the word) must be untouched. This is what keeps the
// warning from degrading every submit — and it is the same narrowness the leaf
// scanner owns.
func TestSubmitBaseStalenessIgnoresProseWithoutADeclaration(t *testing.T) {
	baseStalePinEnv(t)
	repo, _ := baseStaleFixture(t, baseStaleCommitCount)
	payload := "The base case of this parser is the empty input; the codebase and the database are unaffected.\n" +
		"No revision is declared here at all.\n"
	if claims := taskreport.BaseClaimsIn(payload); len(claims) != 0 {
		t.Fatalf("prose naming no base revision must yield no claim; got %+v", claims)
	}
	if w := TaskSubmitBaseStalenessWarning(repo, payload); w != "" {
		t.Fatalf("a payload with no base DECLARATION must yield no warning; got:\n%s", w)
	}
}

// TestSubmitBaseStalenessShortShaWidth pins the abbreviation the warning renders
// against the report form master_claim.go owns — one width, one definition
// (Rules 13/14), so the test and the producer cannot drift on what "the base" is
// spelled as.
func TestSubmitBaseStalenessShortShaWidth(t *testing.T) {
	if masterClaimShortShaWidth != baseStaleShortShaWide {
		t.Errorf("the warning renders the report's abbreviation (%d); the test asserts %d",
			masterClaimShortShaWidth, baseStaleShortShaWide)
	}
}
