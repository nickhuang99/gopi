package tools

// worktree_sweep_undelivered_equivalent_test.go — the DATA-SOURCE half of the
// undelivered-commits rung, pinned in BOTH directions (bug row 01a17d1b).
//
// WHAT IS PINNED, in one sentence: a CLONE ground whose own never-fetched
// `refs/remotes/origin/master` does not contain a commit, while the MAIN
// checkout's integration branch provably does (under another id — a rebase — or
// under another subject — a re-land), is NOT refused; and a commit whose work
// exists nowhere is STILL refused.
//
// The second half is the one that must never be lost in the fix: releasing a
// ground whose work is nowhere deletes that work (`.git` is a DIRECTORY there and
// `devworkspace.Reclaim` removes it with `os.RemoveAll`). A "fix" that made the
// rung answer "" for everything would pass the first test alone, which is why
// every positive case here is paired with the negative one below it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// undeliveredEquivCommit writes one file in dir and commits it, returning the
// commit's object id. `subject` is the whole message, so a case can name the
// equivalence it is testing.
func undeliveredEquivCommit(t *testing.T, dir, name, content, subject string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, dir, "add", name)
	sweepGit(t, dir, "commit", "-q", "-m", subject)
	return strings.TrimSpace(sweepGit(t, dir, "rev-parse", "HEAD"))
}

// TestUndeliveredReleasesAGroundWhoseWorkWasRebasedIntoTheFreshMaster is the
// POSITIVE half for the SUBJECT leg, and it is the measured shape of the bug:
// the ground's `origin/master` snapshot predates the main checkout's master, and
// the work landed there under the SAME subject but a DIFFERENT commit id (a
// cherry-pick: no patch conflict, new parent, new hash). Pre-fix the rung read
// the stale snapshot only and kept the ground forever.
func TestUndeliveredReleasesAGroundWhoseWorkWasRebasedIntoTheFreshMaster(t *testing.T) {
	f := newUndeliveredFixture(t)
	const subject = "fix(equivalence): the ground's snapshot predates the landed commit"

	sweepGit(t, f.ground, "checkout", "-q", "-b", f.branch)
	groundTip := undeliveredEquivCommit(t, f.ground, "landed.txt", "the work\n", subject)

	// THE STALE SNAPSHOT, asserted rather than assumed: the clone's view of
	// origin/master must NOT contain the ground's commit, or this test would be
	// measuring nothing (that is precisely the state 206/208 grounds are in).
	// `--is-ancestor` answers with its EXIT STATUS (0 = ancestor), so the
	// non-failing runner is used: a stale snapshot is exit 1, not an error.
	if _, err := sweepGitErr(t, f.ground, "merge-base", "--is-ancestor", groundTip, "refs/remotes/origin/master"); err == nil {
		t.Fatal("fixture: the ground's origin/master must NOT contain the commit — otherwise there is no stale snapshot to test")
	}
	if p := worktreeUndeliveredCommits(f.ground, f.branch); p.Ahead != 1 {
		t.Fatalf("fixture: pre-condition — the un-subtracted count must be 1, got %d", p.Ahead)
	}

	// THE DELIVERY: the main checkout lands the same change (cherry-pick keeps
	// the subject, rewrites the id) — i.e. exactly what the merger did in the
	// measured case (`84cfb6f` → `3acbb60dd`).
	// The main checkout takes the commit's object into its own store first (the
	// clone channel's push lands it there), then lands it as a NEW commit — a
	// cherry-pick, which is what a rebase looks like from the ground's side.
	sweepGit(t, f.main, "fetch", "-q", f.ground, f.branch)
	sweepGit(t, f.main, finishCleanupCheckoutVerb, masterBranchName)
	// The integration branch must have MOVED since the ground branched, or a
	// cherry-pick onto the identical parent reproduces the identical commit id
	// and the case would prove nothing about hash inequality (measured here).
	undeliveredEquivCommit(t, f.main, "moved-on.txt", "master moved on\n",
		"chore(unrelated): the integration branch moved on after the ground branched")
	sweepGit(t, f.main, "cherry-pick", "FETCH_HEAD")
	landed := strings.TrimSpace(sweepGit(t, f.main, "rev-parse", "HEAD"))
	if landed == groundTip {
		t.Fatal("fixture: a cherry-pick must produce a DIFFERENT commit id — a same-id shape would prove nothing about hash equality")
	}

	why := worktreeSweepUndeliveredRefusal(f.ground, f.main, f.branch)
	if why != "" {
		t.Fatalf("a ground whose work the FRESH master carries must be collectable; got refusal %q\n"+
			"(ground tip %s, master carries it as %s)", why, groundTip, landed)
	}

	// AND THE EVIDENCE NAMES THE LEG, so a keep/release record can be re-checked
	// by hand instead of trusted (Rule 15).
	p := worktreeUndeliveredSubtractEquivalent(f.ground, f.main, f.branch,
		worktreeUndeliveredCommits(f.ground, f.branch))
	if p.Ahead != 0 {
		t.Fatalf("after the subtraction the count must be 0, got %d", p.Ahead)
	}
	if len(p.Equivalent) != 1 || !strings.Contains(p.Equivalent[0], "subject-equivalent") {
		t.Fatalf("the reduction must record the subject leg and the commit it matched; got %q", p.Equivalent)
	}
}

// TestUndeliveredReleasesAGroundWhoseWorkWasRelandedUnderANewSubject is the
// POSITIVE half for the CONTENT leg: the integration branch carries the very
// lines the ground's commit adds, under a subject that has been rewritten
// (measured on the same ground: `c529b09`'s subject does not exist on master,
// while its one added line does, as `6e3191153`). A subject-only fix keeps this
// ground forever, which is why the second leg exists.
func TestUndeliveredReleasesAGroundWhoseWorkWasRelandedUnderANewSubject(t *testing.T) {
	f := newUndeliveredFixture(t)

	sweepGit(t, f.ground, "checkout", "-q", "-b", f.branch)
	groundTip := undeliveredEquivCommit(t, f.ground, "relaid.txt",
		"// the one line that was re-landed elsewhere\n",
		"test(original): the subject nobody kept on the integration branch")

	// The main checkout re-lands the SAME line under a DIFFERENT subject — the
	// shape a rework produces when the change is re-pointed at a new carrier.
	sweepGit(t, f.main, finishCleanupCheckoutVerb, masterBranchName)
	landed := undeliveredEquivCommit(t, f.main, "relaid.txt",
		"// the one line that was re-landed elsewhere\n",
		"test(rework): re-point the same line at its new carrier")

	why := worktreeSweepUndeliveredRefusal(f.ground, f.main, f.branch)
	if why != "" {
		t.Fatalf("a ground whose work was re-landed under a new subject must be collectable; got refusal %q\n"+
			"(ground tip %s, master carries the same lines as %s)", why, groundTip, landed)
	}
	p := worktreeUndeliveredSubtractEquivalent(f.ground, f.main, f.branch,
		worktreeUndeliveredCommits(f.ground, f.branch))
	if len(p.Equivalent) != 1 || !strings.Contains(p.Equivalent[0], "content already in") {
		t.Fatalf("the reduction must record the CONTENT leg (no subject matched); got %q", p.Equivalent)
	}
}

// TestUndeliveredStillKeepsWorkTheFreshMasterHasNeverSeen is the DUAL — the half
// that makes the fix safe. Nothing about this commit appears in the main
// checkout: its subject is unique and its lines are unique. The rung must still
// refuse, with the same count and the same sentence, because removing this
// directory destroys the only copy of that work.
func TestUndeliveredStillKeepsWorkTheFreshMasterHasNeverSeen(t *testing.T) {
	f := newUndeliveredFixture(t)
	const subject = "feat(nobody-has-this): a line the integration branch has never carried"

	sweepGit(t, f.ground, "checkout", "-q", "-b", f.branch)
	groundTip := undeliveredEquivCommit(t, f.ground, "unique.txt",
		"this line exists nowhere else in the world\n", subject)

	// The main checkout DOES move on — a ground kept because master is stale
	// must not be kept by accident; here it is kept because the work is absent.
	sweepGit(t, f.main, finishCleanupCheckoutVerb, masterBranchName)
	undeliveredEquivCommit(t, f.main, "other.txt", "unrelated work\n",
		"chore(unrelated): an unrelated commit on the integration branch")

	why := worktreeSweepUndeliveredRefusal(f.ground, f.main, f.branch)
	if why == "" {
		t.Fatalf("a ground holding a commit the integration branch has never seen (tip %s) must be KEPT — releasing it deletes the only copy of that work", groundTip)
	}
	for _, want := range []string{f.branch, "1 commit", "push the branch or merge it into " + IntegrationBranchName} {
		if !strings.Contains(why, want) {
			t.Fatalf("the refusal must still name %q; got %q", want, why)
		}
	}
	if p := worktreeUndeliveredSubtractEquivalent(f.ground, f.main, f.branch,
		worktreeUndeliveredCommits(f.ground, f.branch)); p.Ahead != 1 || len(p.Equivalent) != 0 {
		t.Fatalf("nothing may be subtracted from genuinely undelivered work; got ahead=%d equivalent=%q", p.Ahead, p.Equivalent)
	}
}

// TestUndeliveredSubstitutesNothingWhenTheFreshSourceIsTheGround pins the
// no-second-source shape: the adopt arm passes the ground directory twice, and a
// ground compared against itself would answer "delivered" by construction — the
// exact self-referential zero the delivery-point builder refuses for
// `refs/heads/<branch>`.
func TestUndeliveredSubstitutesNothingWhenTheFreshSourceIsTheGround(t *testing.T) {
	f := newUndeliveredFixture(t)
	sweepGit(t, f.ground, "checkout", "-q", "-b", f.branch)
	undeliveredEquivCommit(t, f.ground, "work.txt", "work\n", "feat(x): a commit held only by this ground")

	before := worktreeUndeliveredCommits(f.ground, f.branch)
	after := worktreeUndeliveredSubtractEquivalent(f.ground, f.ground, f.branch, before)
	if after.Ahead != before.Ahead || len(after.Equivalent) != 0 {
		t.Fatalf("a ground asked against ITSELF must not be reduced (before ahead=%d, after ahead=%d, equivalent=%q)",
			before.Ahead, after.Ahead, after.Equivalent)
	}
}

// TestUndeliveredParseDiffReadsARemovedLineThatLooksLikeAHeader pins the ONE
// shape the parser's hunk-body flag exists for: a removed line whose text begins
// with "-- " arrives as `--- …`, i.e. indistinguishable from git's own file
// header by prefix alone. Reading it as a header would DROP that removal from
// the containment question, and the leg would then answer about a file it never
// read.
func TestUndeliveredParseDiffReadsARemovedLineThatLooksLikeAHeader(t *testing.T) {
	const diff = `diff --git a/doc.txt b/doc.txt
index 1111111..2222222 100644
--- a/doc.txt
+++ b/doc.txt
@@ -1,0 +1,2 @@
+-- this line is being ADDED and starts with dashes
+plain added line
@@ -5 +7,0 @@
--- this line is being REMOVED and also starts with dashes
`
	files, ok := worktreeUndeliveredParseDiff(diff)
	if !ok {
		t.Fatal("a well-formed --unified=0 diff must parse")
	}
	got := files["doc.txt"]
	if got == nil {
		t.Fatalf("doc.txt must be the file the diff names; parsed %v", files)
	}
	if len(got.added) != 2 {
		t.Fatalf("added lines = %q, want the two the hunks carry (a `++ `-looking addition must not be read as a header)", got.added)
	}
	if len(got.removed) != 1 || !strings.Contains(got.removed[0], "being REMOVED") {
		t.Fatalf("removed lines = %q, want the one `--- …` line the second hunk carries", got.removed)
	}
}
