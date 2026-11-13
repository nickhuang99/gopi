package tools

// branch_delete_nondurable_tip_test.go — THE RED PROOF for the `-d` gap (epic
// 01a12f68): the arms that remove a ref through `git branch -d` instead of
// asking the orphan guard.
//
// The claim this file settles was READ, not measured, by the report that fixed
// the guard (devdoc/archive-ref-holds-tip.md §5b-2): "`-d` refuses unless the
// branch is merged into its UPSTREAM, so a `-d` arm cannot orphan a tip". The
// first test below MEASURES that premise in a fixture repo and pins the hole:
//
//	$ git branch -d topic                                  (upstream = the lane's scratch mirror)
//	warning: deleting branch 'topic' that has been merged to
//	         'refs/remotes/gitPushcheck/topic', but not yet merged to HEAD.
//	Deleted branch topic (was 32a2803).                    RC=0
//	$ git merge-base --is-ancestor 32a2803 master          rc=1  (NOT merged)
//	$ git for-each-ref --contains 32a2803                  → then the pass-end prune → NOTHING
//
// The second test pins the LOCK the `-d` arms now hold, and the third is the
// census that keeps the population wired (a new `branch -d` site that skips the
// lock fails the build, Rule 13's anti-regression shape).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// nonDurableTipFixture builds the measured shape: a branch whose tip is on NO
// durable ref and is NOT in the integration branch, with a `remote` named as
// the pushback lane (git requires the remote to EXIST for its scratch ref to
// resolve as an upstream) and the branch's UPSTREAM pointing at the lane's
// mirror. Returns repo, branch, tip.
func nonDurableTipFixture(t *testing.T) (repo, branch, tip string) {
	t.Helper()
	repo = branchGuardFixtureRepo(t)
	branch = "fix/non-durable-tip"
	gitRun(t, repo, "checkout", "-b", branch, "master")
	branchGuardCommit(t, repo, "non-durable.txt", "work held by no durable ref")
	tip = strings.TrimSpace(rsGit(t, repo, false, "rev-parse", "refs/heads/"+branch))
	gitRun(t, repo, "checkout", "master")
	branchGuardCommit(t, repo, "master-moves.txt", "the integration branch moves on")
	// The lane's mirror ref the pushback fetch leaves behind, written with the
	// `+`-refspec's own primitive (no peer, no load — Rule 27). Its name is the
	// branch's own, exactly as gitPushbackPeerRefMirrorFmt spells it for a
	// per-branch check.
	mirror := gitPushbackCheckRootRef + branch
	gitRun(t, repo, "update-ref", mirror, tip)
	gitRun(t, repo, "remote", "add", gitPushbackCheckNamespace, filepath.Join(repo, "no-such-peer.git"))
	return repo, branch, tip
}

// TestBranchDConsultsTheUpstreamIsTheMeasuredGap drives the REAL git in the
// fixture and asserts the hole exists — this is the verdict of the epic,
// reproduced where the suite can see it. It asserts on `for-each-ref
// --contains <tip>` (the reachability enquiry), NOT on `fsck --dangling`.
func TestBranchDConsultsTheUpstreamIsTheMeasuredGap(t *testing.T) {
	repo, branch, tip := nonDurableTipFixture(t)
	ctx := context.Background()

	// Premise first: the tip is an ancestor of NOTHING in the integration
	// branch — so the "merged" reading `-d` accepts cannot be about master.
	if _, err := branchDeleteGit(ctx, repo, branchDeleteMergeBaseVerb, branchDeleteIsAncestorFlag, tip, branchDeleteIntegrationRef); err == nil {
		t.Fatalf("fixture is wrong: %s IS an ancestor of %s", tip, branchDeleteIntegrationRef)
	}
	// The upstream IS the lane's mirror, and it resolves as one only because a
	// remote of that name exists — the pre-condition of the whole case.
	mirror := gitPushbackCheckRootRef + branch
	gitRun(t, repo, "branch", "--set-upstream-to="+mirror, branch)
	up := strings.TrimSpace(rsGit(t, repo, true, "for-each-ref", "--format=%(upstream:short)", "refs/heads/"+branch))
	if up == "" {
		t.Fatalf("fixture is wrong: the branch has no upstream, so git's own lock never binds")
	}

	// THE MEASUREMENT: git deletes it. Exit status is the answer, so the raw
	// command result is read rather than asserted through a helper.
	out, err := exec.Command("git", "-C", repo, "branch", "-d", branch).CombinedOutput()
	if err != nil {
		t.Fatalf("git branch -d refused (%v) — the premise of this file no longer holds: %s", err, out)
	}
	if !strings.Contains(string(out), up) {
		t.Errorf("git's own warning did not name the upstream it deleted against: %q", strings.TrimSpace(string(out)))
	}
	// The pass-end prune (gitPushbackPruneMirrors' primitive): the holder goes.
	gitRun(t, repo, "update-ref", "--no-deref", "-d", mirror)
	// THE VERDICT, on the reachability enquiry the epic names: no ref contains
	// the tip, while the OBJECT is still there.
	if refs := strings.TrimSpace(rsGit(t, repo, false, "for-each-ref", "--contains", tip)); refs != "" {
		t.Fatalf("the tip is still contained by a ref: %s", refs)
	}
	if got := strings.TrimSpace(rsGit(t, repo, false, "cat-file", "-t", tip)); got != "commit" {
		t.Fatalf("the object is gone (%q) — this fixture is measuring gc, not reachability", got)
	}
	if rsGit(t, repo, false, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch) != "" {
		t.Fatalf("the branch survived a delete git reported as done")
	}
}

// branchDeleteIntegrationRef is this package's spelling of the integration
// branch for the fixtures above — the constant, never a literal (Rule 14).
const branchDeleteIntegrationRef = IntegrationBranchName

// TestNonDurableTipRefusalKeepsTheRef is the locked behaviour, both poles plus
// the two controls that must NOT refuse: a durable holder and a merged tip.
func TestNonDurableTipRefusalKeepsTheRef(t *testing.T) {
	repo, branch, tip := nonDurableTipFixture(t)

	why := branchDeleteNonDurableTipRefusal(repo, branch, branchDeleteIntegrationRef)
	if !strings.Contains(why, branchDeleteScratchTipMarker) {
		t.Fatalf("no refusal on a tip held only by a non-durable ref: %q", why)
	}
	// The refusal must be actionable: the marker, the short tip and the ONE
	// recovery command, all spellable by the operator who reads it.
	for _, want := range []string{branchDeleteShortHash(tip), "git branch", branch + "-recovered"} {
		if !strings.Contains(why, want) {
			t.Errorf("the refusal does not carry %q: %s", want, why)
		}
	}
	if _, err := exec.Command("git", "-C", repo, "branch", "-d", branch).CombinedOutput(); err != nil {
		t.Logf("(informational) git itself also refuses this fixture: %v", err)
	}
	if got := strings.TrimSpace(rsGit(t, repo, false, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)); got != tip {
		t.Fatalf("the branch was not kept: %q", got)
	}

	// CONTROL ①: a DURABLE holder silences the refusal — a tag is a ref a
	// writer is responsible for keeping (the same premise the guard uses).
	gitRun(t, repo, "tag", "keep/non-durable", tip)
	if why := branchDeleteNonDurableTipRefusal(repo, branch, branchDeleteIntegrationRef); why != "" {
		t.Fatalf("a tag holding the tip did not silence the refusal: %s", why)
	}
	gitRun(t, repo, "tag", "-d", "keep/non-durable")

	// CONTROL ②: a tip that IS in the integration branch is the ordinary case
	// — the arms' own ancestry read, re-asked at the ref, and no refusal.
	merged := branch + "-merged"
	gitRun(t, repo, "branch", merged, branchDeleteIntegrationRef)
	if why := branchDeleteNonDurableTipRefusal(repo, merged, branchDeleteIntegrationRef); why != "" {
		t.Fatalf("a tip already in the integration branch was refused: %s", why)
	}

	// An absent branch is not a refusal: there is no tip to orphan.
	if why := branchDeleteNonDurableTipRefusal(repo, "fix/never-existed", branchDeleteIntegrationRef); why != "" {
		t.Fatalf("a missing branch was refused: %s", why)
	}
}

// TestEveryBranchDeleteSiteAsksTheNonDurableTipLock is the CENSUS that keeps
// the population honest (Rule 13's shape: a value that exists but is bypassed
// is the defect). Every ref-removing `branch -d` in production source must be
// preceded by one of the lock's spellings; a new site that skips it fails here
// rather than orphaning a tip in the field.
func TestEveryBranchDeleteSiteAsksTheNonDurableTipLock(t *testing.T) {
	const lockWindow = 2000 // bytes of source before the delete that must carry the ask
	// THE LOCK LIST WAS INCOMPLETE, and the two entries added on 2026-10-18 were
	// never scanned because their sites are spelled with the shared constants and
	// this census used to see literals only. Both ask the SAME QUESTION the lock
	// asks — "is this ref's tip already contained in the integration branch, so
	// that removing the ref can orphan nothing?" — and neither may be dropped in
	// favour of the other:
	//
	//	worktreeSweepBranchIsSpent      — `merge-base --is-ancestor <b> <integration>`
	//	                                  for the sweeper's dead-registration and
	//	                                  inert-chat-ground rungs.
	//	mergedCleanupStillMergedReason  — the orphan/pair lanes' race-free recheck
	//	                                  that the branch is STILL master's ancestor.
	//
	// Adding them is not a weakening: it brings two real delete sites under the
	// census for the first time (Rule 41 — a scan that cannot see a spelling
	// reports a count about a different set than the one it claims).
	locks := []string{
		"branchDeleteNonDurableTipRefusal",
		"BranchDeleteNonDurableTipRefusal",
		"mergeSpentRefusal",
		"branchDeleteOrphanRefusal",
		"worktreeSweepBranchIsSpent",
		"mergedCleanupStillMergedReason",
	}
	roots := []string{".", filepath.Join("..", "..", "cmd", "agent")}
	// THE POPULATION HAS TWO SPELLINGS, AND THE CENSUS MUST SEE BOTH (2026-10-18,
	// task 01a14b8e). It used to count only the INLINE argv spelling
	// (`"branch", "-d"`), which made it blind in exactly one direction: a site
	// that correctly REFERENCES the shared constants (finishCleanupBranchVerb,
	// finishCleanupBranchDeleteFlag — the spelling Rule 13 requires) walked out of
	// the population entirely. Measured while fixing this: the count dropped 8→7
	// and this gate failed with "at least 8" while the site it lost was still
	// there, still deleting, still asking the lock. A census that cannot see a
	// spelling reports a count about a different set than the one it claims.
	//
	// A constant-spelling occurrence is a DELETE SITE unless it is the OBSERVER
	// call: worktreeSweepObserve takes the same two arguments to RECORD a deletion
	// it is about to perform, and counting the record as a second site would
	// inflate the population with a line that cannot delete anything.
	literalNeedle := `"branch", "-d"`
	// The needle is SOURCE TEXT, not an evaluated value: the census reads the
	// spelling a writer typed, so this names the two identifiers as they appear.
	constantNeedle := "finishCleanupBranchVerb, finishCleanupBranchDeleteFlag"
	const observerCallee = "worktreeSweepObserve"
	// branchDeleteCensusEscape is the reviewed inline exemption, the shape this
	// repo already uses for every other scan gate (stylegate, else-ok, panic-ok):
	// a site that relies on a DIFFERENT, NAMED guard says so on its own line. It
	// is not a way past the census — the reason is read by the next person, and
	// an unexplained use is the defect the escape exists to make visible.
	const branchDeleteCensusEscape = "branch-delete-ok:"
	sites, locked := 0, 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("ReadFile %s: %v", path, rerr)
			}
			src := string(b)
			for i := 0; i < len(src); {
				at, span := -1, 0
				for _, needle := range []string{literalNeedle, constantNeedle} {
					if idx := strings.Index(src[i:], needle); idx >= 0 && (at < 0 || i+idx < at) {
						at, span = i+idx, len(needle)
					}
				}
				if at < 0 {
					break
				}
				i = at + span
				// The OBSERVER records a deletion; it never performs one.
				lineStart := strings.LastIndexByte(src[:at], '\n') + 1
				lineEnd := len(src)
				if rel := strings.IndexByte(src[at:], '\n'); rel >= 0 {
					lineEnd = at + rel
				}
				// The OBSERVER records The deletion it is about to perform; it is
				// not a site. Its sibling call a few lines below is, and that is
				// where the lock is asked.
				if strings.Contains(src[lineStart:lineEnd], observerCallee) {
					continue
				}
				sites++
				from := at - lockWindow
				if from < 0 {
					from = 0
				}
				// The escape is read on the WHOLE matched line (a reviewer writes it
				// at the end of the statement it explains), the lock only BEFORE it.
				escapeTo := len(src)
				if rel := strings.IndexByte(src[at:], '\n'); rel >= 0 {
					escapeTo = at + rel
				}
				lockedHere := strings.Contains(src[from:escapeTo], branchDeleteCensusEscape)
				for _, lock := range locks {
					if lockedHere {
						break
					}
					lockedHere = strings.Contains(src[from:at], lock)
				}
				if lockedHere {
					locked++
					continue
				}
				t.Errorf("%s: a `branch -d` removes a ref without asking the non-durable-tip lock — "+
					"git's own `-d` lock is the branch's UPSTREAM and a scratch mirror can satisfy it "+
					"(measured, epic 01a12f68)", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	// A census that found nothing must not pass (Rule 15: a scan that never ran
	// is not a scan that found nothing). THE FLOOR IS THE MEASURED POPULATION,
	// and it went 8 → 7 because the POPULATION shrank — not because the census
	// lost sight of a spelling (the 2026-10-18 defect above, which this floor
	// caught and which is the reason it is a COUNT rather than a bare > 0).
	//
	// Measured 2026-10-31 (task 01a18f6e), non-test files under the scanned roots:
	//
	//	58b5618b^   matches=10  sites=9   (4 literal + 4 constant + 1 that predates)
	//	8cf39fb3    matches= 8  sites=7   ← the deletion below
	//	HEAD        matches= 8  sites=7
	//
	// `8cf39fb3 refactor(merge): delete the one-shot gopi merge implementation`
	// deleted two ref-removing sites with the mechanism that owned them
	// (`mergeGit(repo, "branch", "-d", branch)` twice). Those sites are GONE
	// rather than newly invisible: the seven that remain are the two inline
	// `task_merge*.go` pairs, the two `task_merge_executor.go` sites, the two
	// `task_merged_cleanup.go` constant-spelled sites, and the sweeper's one
	// constant-spelled site — each one still asked for the lock, so `locked ==
	// sites` below is unchanged.
	//
	// The floor stays a FLOOR: a mechanism deleted WITH its sites moves it down,
	// and a site that walks out while still deleting is still the red this test
	// exists for (the site count drops but `locked` would too — the second check
	// is what catches that).
	if sites < 7 {
		t.Fatalf("the census found %d `branch -d` sites — the population it is supposed to cover is at least 7 "+
			"(the measured population at 5199b73: see the history in the comment above — a DROP means a site walked "+
			"out of the census's sight, and the spelling list is what has to be widened, never this floor)", sites)
	}
	if locked != sites {
		t.Fatalf("%d of %d delete sites ask the lock", locked, sites)
	}
	t.Logf("census: %d ref-removing `branch -d` sites, all of them preceded by a lock ask", sites)
}
