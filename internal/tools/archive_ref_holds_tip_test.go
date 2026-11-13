package tools

// archive_ref_holds_tip_test.go — THE RED PROOF for epic 01a12f48 (a removal
// path that leaves the removed tip reachable from NO ref).
//
// INCIDENT, as MEASURED by the filing run (task 01a12d60, host nick-HP-Laptop):
// the tip of task/01a12d06c02f71008b77c0580690cdf1 (bc8e2ea8) was found on the
// peer with `git cat-file -t` = commit while EVERY reachability enquiry was
// empty — `git for-each-ref --contains <tip>` printed nothing, no
// refs/tags/archive/* matched the task id, no refs/gopi/* either. A commit
// whose only home is the object store is one `git gc` from gone, and the WHY the
// archive namespace exists to keep would go with it.
//
// WHAT THIS FILE PINS. Three facts, each one an arm of that state:
//
//	① the ARCHIVE WRITER holds the tip: after ArchiveTagReasoned, the tip is
//	   contained by refs/tags/archive/<branch> and it SURVIVES `git gc
//	   --prune=now`;
//	② the ORPHAN GUARD refuses a delete whose only other container is the
//	   pushback lane's per-pass mirror (refs/remotes/gitPushcheck/…), because
//	   that ref is deleted by the same lane's own defer — a holder that proves
//	   nothing about durability;
//	③ the END-TO-END shape: mirror-only holder → guard → the lane prunes its
//	   mirror → the branch is deleted, and the tip is STILL reachable from a
//	   ref (the archive tag the removal path must mint first), and still there
//	   after gc.
//
// ② and ③ are the RED-first half: on the pre-fix code the guard read the mirror
// as an ordinary holder, allowed the delete, and the tip's only company was a
// ref the next pass drops — `for-each-ref --contains` then answered empty.
//
// THE CONTROLS ARE HALF THE FILE, and they are what keeps the fix from being a
// sledgehammer: a DURABLE holder (any tag, any branch, another remote's tracking
// ref) must still silence the guard exactly as before, and a delivered tip must
// still be deletable (the chain's own push is evidence the commit is kept
// elsewhere — branchDeleteOrphanRefusalAt's delivery point, pinned by
// live_row_session_veto_test.go).

import (
	"os/exec"
	"strings"
	"testing"
)

// archiveRefFixtureRepo builds a throwaway repo with one commit on master, git
// identity configured — the same shape the branch-delete guard's own fixtures
// use, so the two files cannot disagree about what a repo needs.
func archiveRefFixtureRepo(t *testing.T) string {
	t.Helper()
	return branchGuardFixtureRepo(t)
}

// archiveRefTopicBranch cuts ONE branch off master, commits one file on it,
// returns to master and reports the branch and its tip — the "unmergeable
// branch" every arm below removes.
func archiveRefTopicBranch(t *testing.T, repo, branch string) string {
	t.Helper()
	gitRun(t, repo, "checkout", "-q", "-b", branch, masterBranchName)
	branchGuardCommit(t, repo, "work.txt", "the work that must survive")
	gitRun(t, repo, "checkout", "-q", masterBranchName)
	return strings.TrimSpace(rsGit(t, repo, false, "rev-parse", archiveRefHeadsPrefix+branch))
}

// archiveRefHeadsPrefix is refs/heads/ for this file's own git calls (the
// package's ONE spelling, Rule 14).
const archiveRefHeadsPrefix = refsHeadsPrefix

// archiveRefContainingNames answers the incident's own enquiry — WHICH refs
// contain tip — as a list, so a test can assert on the ref NAME and not only on
// emptiness.
func archiveRefContainingNames(t *testing.T, repo, tip string) []string {
	t.Helper()
	out := strings.TrimSpace(rsGit(t, repo, false, "for-each-ref", "--contains", tip, "--format=%(refname)"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// archiveRefTipPresent reports whether the object store still has the commit —
// the other half of the incident's measurement (present, but attached to
// nothing).
func archiveRefTipPresent(t *testing.T, repo, tip string) bool {
	t.Helper()
	_, err := exec.Command("git", "-C", repo, "cat-file", "-e", tip).Output()
	return err == nil
}

// archiveRefGC runs the pruner the incident's grace period ends at. `--prune=now`
// is the aggressive form: an object no ref reaches is collected HERE, so a test
// that passes it has measured survival rather than luck.
func archiveRefGC(t *testing.T, repo string) {
	t.Helper()
	gitRun(t, repo, "gc", "--prune=now", "--quiet")
}

// archiveRefMirrorOnly plants the pushback lane's per-pass mirror at tip — the
// exact ref gitPushbackPruneMirrors deletes when its pass ends (Rule 27: the
// mirror is written with one `update-ref`, no peer, no load).
func archiveRefMirrorOnly(t *testing.T, repo, tip string) {
	t.Helper()
	gitRun(t, repo, "update-ref", gitPushbackCheckRef, tip)
}

// ── ① the ARCHIVE WRITER holds the tip, and gc does not take it ─────────────

// TestArchiveTagHoldsTheBranchTipAndSurvivesGC pins the durable half: the ONE
// archive writer leaves the tip reachable from the archive ref, and that ref
// keeps it across the pruner.
func TestArchiveTagHoldsTheBranchTipAndSurvivesGC(t *testing.T) {
	repo := archiveRefFixtureRepo(t)
	branch := "fix/archived"
	tip := archiveRefTopicBranch(t, repo, branch)

	if _, err := ArchiveTagReasoned(repo, branch, ArchiveReasonUnowned, "archive-ref test"); err != nil {
		t.Fatalf("ArchiveTagReasoned: %v", err)
	}

	tagRef := archiveTagRefPrefix + branch
	names := archiveRefContainingNames(t, repo, tip)
	if !archiveRefContainsName(names, tagRef) {
		t.Fatalf("the archived tip is contained by %v, want it to name %s", names, tagRef)
	}

	// The branch ref goes away — the state every archive decision produces —
	// and the tip must still be held by the tag alone.
	gitRun(t, repo, "branch", "-D", branch)
	names = archiveRefContainingNames(t, repo, tip)
	if len(names) != 1 || names[0] != tagRef {
		t.Fatalf("after the branch ref went, the tip is contained by %v, want exactly [%s]", names, tagRef)
	}

	archiveRefGC(t, repo)
	if !archiveRefTipPresent(t, repo, tip) {
		t.Fatalf("git gc --prune=now collected the archived tip %s — the archive ref did not hold it", tip)
	}
	if names = archiveRefContainingNames(t, repo, tip); len(names) != 1 || names[0] != tagRef {
		t.Fatalf("after gc the archived tip is contained by %v, want exactly [%s]", names, tagRef)
	}
}

// archiveRefContainsName reports whether names carries ref.
func archiveRefContainsName(names []string, ref string) bool {
	for _, n := range names {
		if n == ref {
			return true
		}
	}
	return false
}

// ── ② the ORPHAN GUARD must not accept a scratch holder ─────────────────────

// TestOrphanGuardRefusesATipOnlyAScratchMirrorHolds is the RED-first arm: the
// tip's only other container is the pushback lane's mirror, which that lane
// deletes when the pass ends — so the delete WOULD orphan the commit, and the
// guard must refuse and name the holder.
func TestOrphanGuardRefusesATipOnlyAScratchMirrorHolds(t *testing.T) {
	repo := archiveRefFixtureRepo(t)
	branch := "fix/mirror-only-guard"
	tip := archiveRefTopicBranch(t, repo, branch)
	archiveRefMirrorOnly(t, repo, tip)

	why := branchDeleteOrphanRefusal(repo, branch)
	if why == "" {
		t.Fatalf("the guard ALLOWED a delete whose only other container is %s (deleted by its own lane when the pass ends) — the tip %s would survive in no ref", gitPushbackCheckRef, tip)
	}
	if !strings.Contains(why, branchDeleteMirrorOnlyMarker) {
		t.Errorf("the refusal does not name the scratch-only shape (%q): %s", branchDeleteMirrorOnlyMarker, why)
	}
	if !strings.Contains(why, gitPushbackCheckRef) {
		t.Errorf("the refusal does not name the ref that holds it: %s", why)
	}
}

// TestOrphanGuardStillAllowsADurableHolder is the POSITIVE CONTROL: one durable
// ref (here a tag, the shape the archive namespace itself uses) keeps the guard
// silent, so the fix refuses the scratch-only shape only.
func TestOrphanGuardStillAllowsADurableHolder(t *testing.T) {
	repo := archiveRefFixtureRepo(t)
	branch := "fix/durable-holder"
	tip := archiveRefTopicBranch(t, repo, branch)
	archiveRefMirrorOnly(t, repo, tip)
	gitRun(t, repo, "tag", "keep/"+branch, tip)

	if why := branchDeleteOrphanRefusal(repo, branch); why != "" {
		t.Fatalf("a durable tag holder did not silence the guard: %s", why)
	}
}

// TestOrphanGuardStillRefusesATipNoRefHolds is the pre-existing refusal,
// re-measured here so the fix cannot have replaced it with the mirror-only
// sentence (the two are different facts: no holder at all vs a scratch one).
func TestOrphanGuardStillRefusesATipNoRefHolds(t *testing.T) {
	repo := archiveRefFixtureRepo(t)
	branch := "fix/no-holder"
	tip := archiveRefTopicBranch(t, repo, branch)

	why := branchDeleteOrphanRefusal(repo, branch)
	if why == "" {
		t.Fatalf("a tip no ref holds (%s) was allowed to be deleted", tip)
	}
	if !strings.Contains(why, branchDeleteNoOtherRefMarker) {
		t.Errorf("the no-holder refusal no longer carries its own marker: %s", why)
	}
}

// ── ③ the END-TO-END shape, over a real gc ─────────────────────────────────

// TestRemovalPathLeavesNoUnreachableTip drives the measured sequence to the end:
// the mirror is the tip's only company, the removal path asks the guard, the
// lane prunes its mirror (gitPushbackPruneMirrors' own delete), the branch is
// removed, and the pruner runs. Whatever the removal path does, the tip must
// still be contained by a ref afterwards — the property whose absence produced
// the incident.
//
// THE REMOVAL PATH UNDER TEST IS THE GUARD ITSELF (arm ②), and the arm below is
// the compliance half: a removal path that asks the guard and is refused leaves
// the branch in place; a path that mints the archive ref first (the sanctioned
// Rule 9 route) may delete the branch and must still leave the tip contained.
func TestRemovalPathLeavesNoUnreachableTip(t *testing.T) {
	repo := archiveRefFixtureRepo(t)
	branch := "fix/removal-path"
	tip := archiveRefTopicBranch(t, repo, branch)
	archiveRefMirrorOnly(t, repo, tip)

	// ① the guard is asked exactly as the close-out chain and the adoption arm
	// ask it.
	if why := branchDeleteOrphanRefusal(repo, branch); why == "" {
		// Pre-fix behaviour: the guard passed, so the path would delete the ref.
		// The lane then prunes its mirror and the branch goes: measure the state
		// that leaves and fail with it, rather than asserting on the refusal
		// sentence alone.
		gitRun(t, repo, "update-ref", "--no-deref", "-d", gitPushbackCheckRef)
		gitRun(t, repo, "branch", "-D", branch)
		names := archiveRefContainingNames(t, repo, tip)
		if len(names) == 0 {
			archiveRefGC(t, repo)
			gone := !archiveRefTipPresent(t, repo, tip)
			t.Fatalf("a removal path deleted %s while %s was its only other container: no ref contains %s afterwards; after `git gc --prune=now` the object is still present=%v (present=false means the commit is GONE)",
				branch, gitPushbackCheckRef, tip, !gone)
		}
	}

	// ② the guard refused, so the branch is still there — and the sanctioned
	// removal route (mint the archive ref, then delete the branch) must leave a
	// ref that contains the tip, across the pruner.
	if _, err := ArchiveTagReasoned(repo, branch, ArchiveReasonUnowned, "removal-path test"); err != nil {
		t.Fatalf("ArchiveTagReasoned: %v", err)
	}
	gitRun(t, repo, "update-ref", "--no-deref", "-d", gitPushbackCheckRef)
	gitRun(t, repo, "branch", "-D", branch)
	archiveRefGC(t, repo)

	names := archiveRefContainingNames(t, repo, tip)
	want := archiveTagRefPrefix + branch
	if !archiveRefContainsName(names, want) {
		t.Fatalf("after the removal path ran, %s is contained by %v, want it to name %s", tip, names, want)
	}
	if !archiveRefTipPresent(t, repo, tip) {
		t.Fatalf("the removal path left the tip unreachable and gc collected %s", tip)
	}
}
