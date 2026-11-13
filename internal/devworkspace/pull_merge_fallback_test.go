package devworkspace

// pull_merge_fallback_test.go — the delivery leg must LAND the work when a
// rebase conflict is only a HISTORY-SHAPE conflict, and must keep refusing when
// the content really is in dispute.
//
// THE MEASURED DEFECT (row 01a1a87b, 2026-11-05). The run produced a complete,
// clean commit (b77a1882, 13 files, +794/−88) and the close-out's
//
//	git pull --rebase origin master
//
// stopped on `CONFLICT (content): Merge conflict in cmd/agent/cli_tasks.go`;
// pullRebase aborted the rebase and returned a BlockerKindPull, and the
// agent-facing delivery leg has exactly ONE attempt
// (internal/agent/clonedelivery.go, finishCloneDelivery) — so the row closed
// `failed` and the commit stayed in the ground while the ledger told every later
// reader the work had not been done.
//
// THE FACT THE LOOP NEVER MEASURED, and the reason this fallback is not a
// cosmetic retry: **a rebase conflict is not evidence that the content is in
// dispute.** A rebase REPLAYS each commit onto the new base one at a time, so a
// branch whose net change to a line is nothing — an edit followed by its own
// revert — conflicts with a master that moved that line, while `git merge`
// (which integrates the two TIPS, not the intermediate commits) resolves it by
// itself. Measured on this host with a three-commit fixture:
//
//	git rebase master  → CONFLICT (content): Merge conflict in f.txt
//	git merge  master  → Merge made by the 'ort' strategy. 1 file changed
//
// So the honest fallback is the one git already offers: when the rebase cannot
// replay, integrate the same two tips with a merge. Both arms are asserted here
// — the merge that RESOLVES delivers, the merge that also CONFLICTS still
// refuses — because a genuine content dispute must never be papered over.
//
// WHAT MUST NOT CHANGE: the ordinary path. A rebase that applies cleanly still
// produces a LINEAR history, so the fallback costs nothing when nothing is
// wrong. Arm ③ pins that with the parent count of what origin received.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// shadowClones is the per-test directory that holds the "somebody else landed
// first" checkouts, so the fixture never writes inside origin or the ground.
func shadowClones(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// shadowCheckout clones origin into into/name — the peer side of a race.
func shadowCheckout(t *testing.T, into, origin, name string) string {
	t.Helper()
	dir := filepath.Join(into, name)
	gitIn(t, into, "clone", "-q", origin, dir)
	return dir
}

// commitBody writes body into file in dir and commits it.
func commitBody(t *testing.T, dir, file, body, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", file)
	gitIn(t, dir, "commit", "-q", "-m", subject)
}

// landOnOrigin commits the peer's edit in a shadow clone and pushes it, so the
// ground's own integration step has something to integrate.
func landOnOrigin(t *testing.T, shadow, file, body, subject string) {
	t.Helper()
	commitBody(t, shadow, file, body, subject)
	gitIn(t, shadow, "push", "-q", "origin", DefaultBranch)
}

// commitCount counts the commits reachable from rev.
func commitCount(t *testing.T, dir, rev string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(gitIn(t, dir, "rev-list", "--count", rev)))
	if err != nil {
		t.Fatalf("rev-list --count %s: %v", rev, err)
	}
	return n
}

// originFile reads file AS ORIGIN HOLDS IT, from the object store at its own
// branch tip (`git show <branch>:<path>`), never from origin's working tree.
// The tree is the wrong witness here and the reason is mechanical: the fixture
// origin allows a push into a checked-out branch (receive.denyCurrentBranch =
// ignore), so the REF advances while the checkout stays stale — reading the
// file from disk would report the pre-push content and call a delivered
// delivery "not landed".
func originFile(t *testing.T, origin, file string) string {
	t.Helper()
	return gitIn(t, origin, "show", DefaultBranch+":"+file)
}

// TestDeliveryFallsBackToMergeWhenTheRebaseCannotReplay is arm ①: the measured
// shape — a branch whose net change to a line is nothing, against a master that
// moved that line. The rebase conflicts, the merge resolves, the work lands.
func TestDeliveryFallsBackToMergeWhenTheRebaseCannotReplay(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	gitIn(t, dir, "checkout", "-q", "-b", "task/work")

	// The run's two commits: an edit to README.md and the revert of that edit.
	// NET CHANGE TO THAT LINE: none — which is why the rebase replayed into a
	// conflict the branch never really had.
	commitBody(t, dir, "README.md", "the run's edit\n", "work: edit the line")
	commitBody(t, dir, "README.md", "origin\n", "work: revert the edit")

	// Somebody else lands on the same line first.
	shadows := shadowClones(t)
	peer := shadowCheckout(t, shadows, origin, "shadow-race")
	landOnOrigin(t, peer, "README.md", "the peer's edit\n", "peer: edit the line")

	// PRE-MEASUREMENT: "the rebase would refuse here" is a fact of this fixture,
	// not an assumption the test is built on. The refused pull is undone (the
	// same step pullRebase performs) so AttemptDelivery starts where a close-out
	// actually starts.
	if out, err := runCommand(context.Background(), dir, gitBinary, gitPullVerb, gitPullRebaseFlag, originRemoteName, DefaultBranch); err == nil {
		t.Fatalf("the fixture must make `git pull --rebase` refuse, but it succeeded: %s", out)
	}
	gitIn(t, dir, "rebase", "--abort")
	if dirty := strings.TrimSpace(gitIn(t, dir, "status", "--porcelain")); dirty != "" {
		t.Fatalf("the fixture's ground is dirty before the delivery leg runs:\n%s", dirty)
	}
	tipBefore := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))
	before := commitCount(t, dir, "HEAD")

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery returned an error: %v", err)
	}
	if blk != nil {
		t.Fatalf("the delivery was BLOCKED (%s) — the merge fallback did not run: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("the delivery reported no push: %+v", res)
	}
	// THE FALLBACK DID SOMETHING, and the shape says what: a merge commit joined
	// the two tips, so the record the peer received has TWO parents. (`before`
	// and the parent count together refuse the opposite reading — a delivery that
	// replayed linearly would have one parent and a different commit count.)
	if after := commitCount(t, dir, "HEAD"); after != before+2 {
		t.Fatalf("HEAD moved from %d to %d commits; the fallback must add exactly a merge commit (+2: the peer's commit and the merge)", before, after)
	}
	fields := strings.Fields(strings.TrimSpace(gitIn(t, dir, "rev-list", "--parents", "-1", "HEAD")))
	if len(fields) != 3 {
		t.Fatalf("HEAD is not a merge commit (%d parent(s)); the merge fallback must record the integration: %v", len(fields)-1, fields)
	}
	// THE WORK IS THE PEER'S TREE: read from origin's own checkout.
	if got := originFile(t, origin, "README.md"); got != "the peer's edit\n" {
		t.Fatalf("origin/README.md = %q, want the peer's content to survive the merge", got)
	}
	// AND NOTHING WAS DROPPED: the run's own commit is an ancestor of what
	// origin now holds, and origin's tip is that merge.
	if !gitSucceeds(t, origin, "merge-base", "--is-ancestor", tipBefore, DefaultBranch) {
		t.Fatalf("the run's commit %s is not an ancestor of what origin received", tipBefore)
	}
	if !gitSucceeds(t, dir, "merge-base", "--is-ancestor", tipBefore, "HEAD") {
		t.Fatalf("origin's master did not advance with the delivery")
	}
}

// TestDeliveryStillRefusesWhenTheMergeAlsoConflicts is arm ②: a genuine content
// dispute. The fallback must not resolve it by picking a side — it aborts the
// merge, leaves the ground usable, and reports both tools' refusals.
func TestDeliveryStillRefusesWhenTheMergeAlsoConflicts(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	gitIn(t, dir, "checkout", "-q", "-b", "task/work")

	// A REAL dispute: the run rewrites the line, the peer rewrites it
	// differently in the same place. No integration strategy decides this.
	commitBody(t, dir, "README.md", "the run's version\n", "work: rewrite the line")
	shadows := shadowClones(t)
	peer := shadowCheckout(t, shadows, origin, "shadow-dispute")
	landOnOrigin(t, peer, "README.md", "the peer's version\n", "peer: rewrite the line")

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a refusal is a BLOCKER, not an error: %v", err)
	}
	if blk == nil {
		t.Fatalf("a genuine content dispute was reported as delivered: %+v", res)
	}
	if blk.Kind != BlockerKindPull {
		t.Fatalf("blocker kind = %q, want %q", blk.Kind, BlockerKindPull)
	}
	report := blk.Report()
	// BOTH tools' evidence must be there: the rebase's refusal AND the merge's,
	// because the reader deciding what to do needs to know the fallback ran and
	// did not resolve it (Rule 15 — a fallback that says nothing is a hidden
	// attempt, and a hidden attempt is indistinguishable from one never made).
	if !strings.Contains(report, gitPullVerb) {
		t.Fatalf("the report does not name the rebase step: %s", report)
	}
	if !strings.Contains(report, gitMergeVerb) {
		t.Fatalf("the report does not say the merge fallback was tried: %s", report)
	}
	if len(blk.Paths) == 0 {
		t.Fatalf("the refusal names no conflicted path: %s", report)
	}
	// THE GROUND IS LEFT USABLE: no rebase and no merge still in progress — the
	// same reason pullRebase aborts at all (a workspace left mid-operation
	// poisons every later attempt of the row).
	for _, state := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "MERGE_MSG"} {
		if _, statErr := os.Stat(filepath.Join(dir, ".git", state)); statErr == nil {
			t.Fatalf("the ground is left mid-operation (.git/%s exists)", state)
		}
	}
	// AND NOTHING WAS COMMITTED FOR THE READER: the run's tip is untouched.
	if res.Pushed {
		t.Fatalf("a refused delivery reported a push: %+v", res)
	}
}

// TestDeliveryKeepsALinearHistoryWhenTheRebaseApplies is arm ③ — THE CONTROL.
// The fallback must cost nothing on the ordinary path: a rebase that replays
// cleanly leaves ONE parent on the tip origin receives, and no merge commit.
func TestDeliveryKeepsALinearHistoryWhenTheRebaseApplies(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	gitIn(t, dir, "checkout", "-q", "-b", "task/work")
	// A file the peer never touches: no integration conflict of any kind.
	commitBody(t, dir, "run.txt", "the run's work\n", "work: add a file")
	shadows := shadowClones(t)
	peer := shadowCheckout(t, shadows, origin, "shadow-clean")
	landOnOrigin(t, peer, "README.md", "the peer's edit\n", "peer: edit the line")

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil || blk != nil {
		t.Fatalf("the clean path must deliver: blocker=%v err=%v", blk, err)
	}
	if !res.Pushed {
		t.Fatalf("the clean path reported no push: %+v", res)
	}
	// LINEAR: `rev-list --parents -1` prints the commit plus its parents, so a
	// linear history prints exactly two fields.
	fields := strings.Fields(strings.TrimSpace(gitIn(t, origin, "rev-list", "--parents", "-1", DefaultBranch)))
	if len(fields) != 2 {
		t.Fatalf("origin's tip has %d parent(s); the ordinary path must stay linear (one parent): %v", len(fields)-1, fields)
	}
	if got := originFile(t, origin, "run.txt"); got != "the run's work\n" {
		t.Fatalf("origin/run.txt = %q, want the run's work", got)
	}
}

// gitSucceeds asks git a question whose FALSE answer is an expected result, so
// it cannot use gitIn (which fails the test on a non-zero exit).
func gitSucceeds(t *testing.T, dir string, args ...string) bool {
	t.Helper()
	_, err := runCommand(context.Background(), dir, gitBinary, args...)
	return err == nil
}
