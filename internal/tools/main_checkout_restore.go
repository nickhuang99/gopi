package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/fullid"
	"github.com/nick/gopi/internal/worktreerelease"
)

// main_checkout_restore.go — the WORKER half of the epic "`gopi merge --fast`
// work in master".
//
// The merge-side half (repo_sweep.go's ①a and cmd/agent's mergeSweepRepo) puts the
// main checkout BACK on the integration branch before it fast-forwards. This file
// is the other end of the same defect: the run that PARKED it. A worker that does
// its work in the repo's main checkout and finishes there leaves the repo on its
// own branch, and the next landing — this host's, or another worker's — then
// fast-forwards the wrong ref. Restoring the checkout at close-out is what keeps
// the repo's default state the one every other operation assumes.
//
// WHY THE WORKER IS THE ONE THAT CAN CAUSE IT: the protocol puts a task's work in
// its OWN linked worktree (Rule 6a), and a run in a linked worktree cannot move
// the main checkout's branch at all — the two checkouts are independent. The
// parking happens when a run works in the MAIN checkout instead (a manual run, or
// a task whose deliverable repo IS the checkout), and the row still records the
// branch it worked on — which is exactly what the close-out needs to put it back.

// finishCleanupStepMainCheckout is the close-out step's report name (Rule 1b: the
// step list is matched by name, so the name has one definition).
const finishCleanupStepMainCheckout = "main-checkout"

// finishCleanupMainCheckoutPlanFmt renders the PLAN-mode prediction: the branch
// the checkout is on, and the branch the run would put back (Rule 15 — a dry run
// that omits the step would understate what the apply would do).
const finishCleanupMainCheckoutPlanFmt = "on %s, would put the main checkout back on %s"

// MainCheckoutCloneGroundMarker prefixes the ONE refusal this arm adds to its
// existing three (restored / parked / unreadable): the directory it was handed is
// this RUN'S OWN CLONE GROUND, which is not a shared main checkout at all.
//
// THE DEFECT IT CLOSES (measured 2026-10-28, this very row's ground): the clone
// channel gives a run its own workspace (`.git` is a DIRECTORY — a standalone
// clone), and an auto-registered row's deliverable names that workspace as its
// repo while recording NO worktree. The arm's two existing guards both pass in
// that shape — no branch holds a linked worktree (a clone is registered nowhere),
// and `worktree == ""` means the "the row records a ground that is not the main
// checkout" guard never fires — so the close-out ran `git checkout master` INSIDE
// the live run's ground, moving a working run off its task branch. Measured four
// times on this host before the fix (the ground's own reflog carried four
// `checkout: moving from task/… to master` entries).
//
// WHY THE CRITERION IS TWO FACTS AND NOT A PATH PREFIX: "is this a clone?" is
// `devworkspace.GroundModelOf` — the ONE discriminator (Rules 13/14) — but a
// clone alone does NOT disqualify a directory, because a repo's shared main
// checkout is an ordinary clone too (`~/work/gopi/.git` is a DIRECTORY). What
// disqualifies it is being the ground the RUNNER HANDED THIS RUN
// (`TaskGroundAnnounced`, the one reader of the announcement pair, Rule 2): that
// directory is per-run and disposable, and nobody merges into it.
const MainCheckoutCloneGroundMarker = "clone ground, not a shared main checkout"

// finishCleanupCloneGroundSkipFmt is the refusal sentence: what the directory
// is, which measurement said so, whose ground it is, and what was therefore not
// touched (Rule 15 — a refusal names its evidence and its consequence).
const finishCleanupCloneGroundSkipFmt = MainCheckoutCloneGroundMarker + ": %s holds its own repository " +
	"(.git is a DIRECTORY → ground model %q) and it is the ground announced to this run (%s) — a clone " +
	"ground is this run's own workspace, not the shared checkout a landing needs, so it is left on %s"

// finishCleanupCloneGroundSkipReason answers "must this close-out decline to
// move dir?" — "" when dir may be restored, else the refusal sentence.
//
// It separates NOTHING from a CHECKOUT: the model half is the measured kind of
// `<dir>/.git`, and the ownership half is the ground announced to this process.
// A directory that is a clone but NOT this run's announced ground (a repo's main
// checkout, a sweep's target, a fixture) is untouched by this predicate, which is
// exactly the compatibility contract: every existing restore keeps working.
func finishCleanupCloneGroundSkipReason(dir, branch string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	model := devworkspace.GroundModelOf(dir)
	if model != devworkspace.GroundModelClone {
		return "" // not a standalone clone: this arm has nothing new to say about it
	}
	announced, _ := TaskGroundAnnounced()
	if announced == "" || !finishCleanupSameWorktreePath(announced, dir) {
		return "" // a clone nobody announced as THIS run's ground — not this arm's question
	}
	return fmt.Sprintf(finishCleanupCloneGroundSkipFmt, dir, string(model), TaskGroundDirEnv, branch)
}

// finishCleanupRepoIsACloneGround reports whether repo IS the clone channel's
// per-run workspace — the shape whose "main checkout" resolves to the ground
// ITSELF, so there is no separate checkout behind it to restore.
//
// WHY THE ANNOUNCEMENT ALONE IS NOT ENOUGH (measured 2026-10-28, this row's own
// ground, one hour after the announcement arm landed): the announcement pair is
// read by the process the RUNNER spawned, so a guard that asks only "was this
// announced to ME?" declines nothing in a DIFFERENT process that moves the same
// ground. Measured, the ground's post-checkout hook:
//
//	2026-10-28T19:30:01+08:00 pid=2049361 cwd=/home/user/workspace/01a17e4b…
//	  prev=b52e00c… new=b7e575c… flag=1
//	  2049334: git checkout master
//	  2049284: …/gopi --prompt RP1-BOUNDARY-PLAIN-PROMPT-4f2a …
//	  2008494: …/agent.test -test.run=TestRunPathPlainPromptIsVerbatim
//
// — a CHILD gopi started by an unrelated `cmd/agent` test, reaching the move
// through this ONE implementation rather than through the close-out face.
//
// The discriminators are THREE, and each covers a shape the others are blind
// to (the table is in devdoc/closeout-clone-ground-not-main-checkout.md): the
// channel's own workspace-dir shape (cloneWorkspaceDirShape, against the
// CONFIGURED root), the ONE ground-model resolver (devworkspace.GroundModelOf:
// `.git` is a DIRECTORY — never a second `.git`-stat), and the directory's own
// LEAF NAME, which the channel builds as `<root>/<the row's full id>`
// (devworkspace.Dir) and no GOPI_HOME can rename. The name arm is not a
// reinvented naming rule: it reads the id shape the ledger already owns, through
// the ONE id predicate (fullid.IsUUID).
//
// A clone that none of the three recognises (a repo's shared main checkout, a
// fixture, a sweep target) is untouched by this predicate: that is the
// compatibility contract which keeps the real repair working.
func finishCleanupRepoIsACloneGround(repo string) bool {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return false
	}
	if devworkspace.GroundModelOf(repo) != devworkspace.GroundModelClone {
		return false
	}
	// THE NAME ARM, and it is the only one of the three whose fact lives on the
	// FILESYSTEM instead of in a profile: the clone channel materialises a ground
	// at `devworkspace.Dir(root, taskID)` — `<root>/<the row's full id>` — so the
	// leaf of a per-run ground IS a canonical task id, and no GOPI_HOME can rename
	// a directory. It is asked because the configured-root arm below is blind
	// whenever the moving process resolves a DIFFERENT root (a suite child with a
	// throwaway GOPI_HOME) and the ledger-address arm is blind whenever the row
	// declared its own branch — measured this run, both blind at once, evidence
	// `03-residual-hole-red.txt` and the two arms in
	// main_checkout_clone_ground_test.go.
	//
	// The reverse direction is pinned too: a shared main checkout is an ordinary
	// clone whose leaf is `gopi`, not a uuid, so the real repair still happens
	// (TestFinishCleanupMainRestoreStillRepairsACheckoutThatIsNotAGroundName).
	if fullid.IsUUID(filepath.Base(filepath.Clean(repo))) {
		return true
	}
	root, _, err := config.WorkspaceRootConfigured()
	if err != nil || strings.TrimSpace(root) == "" {
		// An unreadable root is not a licence to guess which checkouts are this
		// channel's (the boundary cloneGroundSourceRepo draws as well).
		return false
	}
	if !cloneWorkspaceDirShape(repo, root) {
		return false
	}
	return true // a clone DIRECTLY under the configured workspace root: this channel's
}

// finishCleanupCheckoutIsAGroundsLedgerAddress answers the SECOND, independent
// half of "is this a run's own ground?" — and it exists because the path test
// above is BLIND in exactly the case that produced it.
//
// A ground STANDS ON ITS LEDGER ADDRESS: `task/<id>` is the branch the clone
// channel puts every ground on (devworkspace Config.GroundBranch) and the branch
// Rule 6a says IS the row's address, so `gopi task show --branch` can read the row
// off the ref. No profile can rename that, while the CONFIGURED workspace root
// is per-process: a suite child carrying a throwaway GOPI_HOME resolves a
// temporary root, so the real ground looks like "an ordinary repo" to its path
// test. Measured 2026-10-28T19:50:56 (this ground, `go test ./cmd/agent/`):
//
//	pid=2754764 cwd=/home/user/workspace/01a17e4b… prev=0d839e4… new=b7e575c…
//	  2754729: git checkout master
//	  2754497: …/test-budget/run-…/tested-bin/gopi --prompt RP1-BOUNDARY-PLAIN-PROMPT-4f2a …
//	  2674138: go test -count=1 -timeout 45m -json ./cmd/agent
//
// — the path test said "not my workspace", and HEAD moved anyway.
func finishCleanupCheckoutIsAGroundsLedgerAddress(head string) bool {
	return strings.HasPrefix(strings.TrimSpace(head), TaskGroundBranchPrefix)
}

// FinishCleanupRestoreMainCheckout is the close-out restore: it puts the
// deliverable repo's MAIN checkout back on the branch a landing needs, and reports
// what it found.
//
// ok=false means the checkout is NOT on the integration branch after the call —
// the caller records that on the row's report (Rule 15: the refusal is never
// silent). detail is "" when there is nothing to say (the repo is already in the
// state a landing needs, or this run's ground was NOT the main checkout).
//
// WHY IT IS ITS OWN ENTRY POINT RATHER THAN A STEP INSIDE taskFinishCleanupChain:
// the chain returns EARLY on several guards (no row, a live status, no
// deliverable, no repo), and every one of those early returns is a case in which a
// run CAN still have parked the main checkout — the parking is done by the run's
// own git commands, not by the ledger. A restore bolted onto the chain's action
// steps would therefore miss the runs that need it most, so it is called from the
// same close-out boundary, BEFORE the chain's mode split.
//
// A run whose ground is a LINKED worktree is left alone: it could not have moved
// the main checkout, and moving the main checkout on the strength of an unrelated
// row would be this arm reaching outside its own delivery (Rule 29). The ground is
// resolved through the chain's OWN two readers — finishCleanupBranchWorktree for
// "is the delivered branch held by a linked worktree", and the windowed repo in
// the row's deliverable record — so the two cannot disagree about which checkout
// the run used.
func FinishCleanupRestoreMainCheckout(ctx context.Context, repo, worktree, branch string) (bool, string) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return true, "" // nothing named: no ground to restore, and nothing to say
	}
	main := finishCleanupMainRepoDir(repo)
	// A CLONE GROUND IS NOT A MAIN CHECKOUT, and this is asked FIRST — before the
	// two ownership guards below, because both of them answer the wrong question
	// about a clone ground by construction (nothing registers a clone, so no
	// branch "holds a linked worktree" there; and a clone-channel row records no
	// worktree, so the "the row's ground is not the main checkout" guard never
	// fires). See MainCheckoutCloneGroundMarker for the measured defect.
	//
	// The answer is ok=true (this arm declines because the directory is not its
	// question, exactly like the LINKED-worktree arm below) with a DETAIL: the
	// step is recorded so an operator reading the row's trail learns that a
	// parked main checkout was NOT restored because the directory named here is
	// the run's own workspace — silence would leave that question unanswerable
	// (Rule 15). Nothing is released, nothing is moved.
	if why := finishCleanupCloneGroundSkipReason(main, IntegrationBranchForRepo(repo)); why != "" {
		return true, why
	}
	if b := strings.TrimSpace(branch); b != "" && !finishCleanupBranchExempt(repo, b) {
		if ground := finishCleanupBranchWorktree(ctx, repo, b); ground != "" {
			return true, "" // the run worked in a LINKED worktree — not this arm's ground
		}
	}
	if w := strings.TrimSpace(worktree); w != "" && !finishCleanupSameWorktreePath(main, w) {
		return true, "" // the row records a ground that is not the main checkout
	}
	// RELEASE THIS PROCESS'S CWD FIRST. The close-out runs inside the run that
	// stood in the checkout (or hands it to a child), and a process whose cwd is
	// the directory about to change branch pins it: the checkout the call is about
	// to perform would be refused by this very process. The release is the ONE
	// implementation (internal/worktreerelease); it is a side effect and never
	// control flow.
	worktreerelease.ReleaseReporting(os.Stderr, finishCleanupStepMainCheckout)
	return FinishCleanupMainCheckoutRestore(ctx, repo, IntegrationBranchForRepo(repo))
}

// finishCleanupMainCheckoutPlannedStep is the PLAN-mode face of the restore: the
// same READ (the branch the checkout holds), reduced to a prediction, with nothing
// moved. It returns nil when the checkout is already where a landing needs it — a
// dry run says nothing about the no-op case either.
func finishCleanupMainCheckoutPlannedStep(ctx context.Context, repo string) *FinishCleanupStep {
	if strings.TrimSpace(repo) == "" {
		return nil
	}
	// THE APPLY PATH DECLINES A CLONE GROUND (FinishCleanupRestoreMainCheckout),
	// so the plan predicts nothing for one: a dry run that promised a move the
	// apply refuses would be the plan/apply disagreement this step exists to
	// prevent.
	if why := finishCleanupCloneGroundSkipReason(finishCleanupMainRepoDir(repo), IntegrationBranchForRepo(repo)); why != "" {
		return nil
	}
	// ... and so do BOTH implementation-level predicates the apply now carries
	// (finishCleanupRepoIsACloneGround by path, the ledger address by branch).
	// Predicted here for the same reason: the plan must not promise a move the
	// apply refuses.
	if finishCleanupRepoIsACloneGround(finishCleanupMainRepoDir(repo)) {
		return nil
	}
	want := IntegrationBranchForRepo(repo)
	head, err := finishCleanupMainCheckoutBranch(ctx, finishCleanupMainRepoDir(repo))
	if err != nil || head == want || finishCleanupCheckoutIsAGroundsLedgerAddress(head) {
		return nil // unreadable, already correct, or somebody's ground: nothing to predict
	}
	return &FinishCleanupStep{
		Step: finishCleanupStepMainCheckout, Target: repo,
		Detail: finishCleanupPlanMarker + fmt.Sprintf(finishCleanupMainCheckoutPlanFmt, head, want),
	}
}
