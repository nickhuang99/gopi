package tools

// THE PUSH CARRIES A REVISION, AND THIS SEAM MAKES IT NAME IT.
//
// The defect (epic 01a12da7, forensics of row 01a12ba1): the close-out chain
// settled a row `completed` while the commit carrying the deliverable had never
// reached the peer. The peer held 247f1859b, the run's own closing revision
// c4a869e7d was its CHILD (`git rev-list --count 247f1859b..c4a869e7d` = 1) and
// `git for-each-ref --contains c4a869e7d` was EMPTY on the client — the commit
// survived as a dangling object while the row read `completed`.
//
// WHAT THIS FILE MEASURES, and the shape it builds on (measured with a bare git
// probe, quoted in the report's ## Evidence): a push of `<branch>:<branch>`
// resolves the LOCAL REF's tip, and a repository whose ref moves while the push
// runs can end with the peer holding one revision and the ref holding its CHILD
// — the incident's arithmetic, reproduced by ordinary git (`git push` with a
// `pre-push` hook that writes a commit; the probe's reading was
// before=8944d06e… after=1c66f482… server=8944d06e…). The hook is the mover the
// test can COMPEL; the incident's own mover is not recorded anywhere (the
// briefing's causal story was INFERRED), so the honest claim is the shape, not
// the mover: a ref that differs across the push must refuse, never settle.
//
// Every assertion is PHYSICAL (a remote ref, a commit count, a directory on
// disk, a row's own event trail), never a report alone.
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// pushTipGit runs one git command with the fixture's deterministic identity.
func pushTipGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// installRefMovingPushHook arms the client repo with a `pre-push` hook that
// writes ONE commit on TOP of the delivered branch while the push runs — the
// closing commit a run writes after its sweep resolved the tip.
//
// The hook lives OUTSIDE the repository (`core.hooksPath` may be any absolute
// directory): writing it into the checkout would leave untracked residue, and
// this suite's own main-checkout hygiene gate reads that as dirt — the very
// refusal that would mask the one under test.
//
// The mover is written through `git update-ref` on a commit built by
// `commit-tree` rather than a `git commit` in a checkout, for two reasons that
// are both observable in this suite: the delivered branch is checked out in the
// LINKED worktree (so committing in the main checkout would move `master` and
// leave the ref under judgement untouched), and the chain refuses a DIRTY
// ground before it delivers — so the mover must not write the working tree at
// all.
func installRefMovingPushHook(t *testing.T, f *finishCleanupFixture) {
	t.Helper()
	hooksDir := filepath.Join(filepath.Dir(f.clientDir), "ref-moving-hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"TIP=$(git rev-parse refs/heads/" + f.featBranch + ") || exit 0\n" +
		"TREE=$(git rev-parse \"$TIP^{tree}\")\n" +
		"NEW=$(GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t " +
		"git commit-tree \"$TREE\" -p \"$TIP\" -m \"closing commit written inside the push window\")\n" +
		"git update-ref refs/heads/" + f.featBranch + " \"$NEW\"\n" +
		"echo ref-moved-inside-the-push >&2\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-push"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pushTipGit(t, f.clientDir, "config", "core.hooksPath", hooksDir)
}

// TestFinishCleanupRefusesWhenTheRefMovesAcrossThePush is the epic's acceptance
// (a) + (b): a run whose branch moves past the revision the push carried is
// REFUSED by name, the refusal rides the row's own event trail, the ground is
// kept, and the revision that would have been orphaned is still held by a ref.
func TestFinishCleanupRefusesWhenTheRefMovesAcrossThePush(t *testing.T) {
	f := newFinishCleanupFixture(t)
	dbPath := testutil.NewTestDBPath(t)
	installRefMovingPushHook(t, f)
	deliverable := f.deliverableBlob()

	ctx := context.Background()
	conn, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	preMergeSeedRow(t, conn, "t-push-tip", deliverable)

	rep, _ := runFinishCleanupChainTarget(ctx, &FinishCleanupReport{TaskID: "t-push-tip"},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, f.serverDir, false) //stylegate:ignore the report is the subject; the error is not asserted here

	// --- the push step is NOT a delivery, and it says why by name ---
	var pushStep *FinishCleanupStep
	for i := range rep.Steps {
		if rep.Steps[i].Step == finishCleanupPushVerb {
			pushStep = &rep.Steps[i]
		}
	}
	if pushStep == nil {
		t.Fatalf("the chain never reached the push: %+v", rep.Steps)
	}
	if pushStep.OK {
		t.Errorf("the push step reported a delivery although the ref moved across it: %q", pushStep.Detail)
	}
	if !strings.Contains(pushStep.Detail, finishCleanupPushTipMovedMarker) {
		t.Errorf("the refusal must be NAMED (%q), got %q", finishCleanupPushTipMovedMarker, pushStep.Detail)
	}

	// --- the incident's arithmetic, measured on the client ---
	serverTip := pushTipGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch)
	clientTip := pushTipGit(t, f.clientDir, "rev-parse", "refs/heads/"+f.featBranch)
	if serverTip == clientTip {
		t.Fatalf("the fixture did not reproduce the window: peer %s == ref %s", serverTip, clientTip)
	}
	if gap := pushTipGit(t, f.clientDir, "rev-list", "--count", serverTip+".."+clientTip); gap != "1" {
		t.Errorf("the wrapping commit must be exactly the child of what the push carried: rev-list --count = %s, want 1", gap)
	}

	// --- acceptance (b): the revision is still held by a ref, not orphaned ---
	holders := pushTipGit(t, f.clientDir, "for-each-ref", "--contains", clientTip)
	if strings.TrimSpace(holders) == "" {
		t.Errorf("the declared tip %s is contained by NO ref — the work would be unreachable", clientTip)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("the ground was collected although the delivery did not land: %v", err)
	}
	if !serverHasRefRuns(t, f.serverDir, f.featBranch) {
		t.Errorf("the branch ref was deleted although the delivery did not land")
	}

	// --- and the row cannot be classified as closed out ---
	if finishCleanupReportSettled(rep) {
		t.Errorf("a refused delivery read as SETTLED — the row would close with the wiring unreachable: %+v", rep.Steps)
	}

	// --- the refusal rides the row's OWN trail (task_event), never a report alone ---
	var detail string
	if err := conn.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT 1`,
		"t-push-tip", finishCleanupRemovalRefusedEvent).Scan(&detail); err != nil {
		t.Fatalf("the refusal must be recorded on the row's own event trail: %v", err)
	}
	if !strings.Contains(detail, finishCleanupPushTipMovedMarker) {
		t.Errorf("the recorded refusal must carry the named marker, got %q", detail)
	}
}

// TestFinishCleanupPushStepNamesTheCarriedTip is the half that makes the first
// test readable: an ORDINARY close-out records the revision it handed over, so
// "which commit did this delivery carry?" is answered by the row's own report
// instead of the hand forensics the 01a12ba1 investigation needed.
func TestFinishCleanupPushStepNamesTheCarriedTip(t *testing.T) {
	f := newFinishCleanupFixture(t)
	tip := pushTipGit(t, f.clientDir, "rev-parse", "refs/heads/"+f.featBranch)

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)

	var detail string
	for _, s := range rep.Steps {
		if s.Step == finishCleanupPushVerb {
			detail = s.Detail
		}
	}
	if !strings.Contains(detail, finishCleanupPushCarriesTipMarker+tip) {
		t.Errorf("the push step must name the revision it carried (%s): %q", tip, detail)
	}
	// Physical corroboration: the named revision is the one the peer holds.
	if got := pushTipGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch); got != tip {
		t.Errorf("the peer holds %s, the report named %s", got, tip)
	}
}
