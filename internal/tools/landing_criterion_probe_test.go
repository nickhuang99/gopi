package tools

// landing_criterion_probe_test.go — HOW TO JUDGE "did this work land?", pinned
// as an executable criterion instead of prose.
//
// THE MEASURED MISJUDGEMENT THIS FILE EXISTS FOR (row 01a17dd1, 2026-10-27/28).
// Its briefing asserted master lacked four functions and a 235-line red proof:
//
//	git log --oneline --grep="a rebase that re-derived the content is still a delivery" --all
//	# （空）
//	comm -13 <(git show origin/master:internal/tools/task_delivery_proof.go | grep "^func ")
//	         <(git -C <ground> show 3df047b:internal/tools/task_delivery_proof.go | grep "^func ")
//	+func CloneDeliveryTip(dir string) string   (and three more)
//
// Both halves of that verdict were FALSE at the revision this ground stands on
// (origin/master = 84beb309ce3c90875e85142eb12769564778ea45):
//
//	$ for f in CloneDeliveryTip TaskDeliveryRecordLanded deliveryProofLandedRecorded deliveryProofParseLandedRecord; do git grep -c "func $f" origin/master -- '*.go'; done
//	origin/master:internal/tools/task_delivery_proof.go:1   (x4)
//	$ git show origin/master:internal/tools/zz_redproof_delivery_proof_rebase_test.go | wc -l
//	235
//
// WHY. Three different errors, each with its own probe here:
//
//	(1) `--grep` together with `--all` is NOT a landed check — it searches every ref this
//	    clone happens to carry, so a commit that exists only on an ARCHIVE TAG
//	    answers "present" while the integration branch has never seen it, and a
//	    commit the channel REBASED carries a different sha whose subject still
//	    matches (so it answers "present" for the wrong reason). Arm 3 pins the
//	    false positive; arms 1/2 pin the false NEGATIVE a hash probe gives.
//	(2) A COMMIT SUBJECT is not an identity. This repo carries two commits with
//	    the byte-identical subject "a rebase that re-derived the content is
//	    still a delivery" (1ae64e138, 700ce92) whose patches are DIFFERENT
//	    (patch-ids 686d800e435cf8238691e52d8a8a2ae36c9a59d4 and
//	    d454427c4edd42a23ecc679859448b9386beeaf4), and 15 subject strings appear
//	    more than once among its 668 commits (measured: `git log --format=%s
//	    origin/master | sort | uniq -d | wc -l` → 15). Arm 2 pins it.
//	(3) A MEASUREMENT HAS A TIME AND A REF. The briefing's numbers were taken
//	    at 2026-10-27 22:46 against master a802b473e196; the fix reached master
//	    at 23:33 (1ae64e138). Re-running the same commands later contradicts the
//	    same briefing — so a verdict must NAME the ref and the sha it was
//	    measured against, which is why every helper below takes `ref` and the
//	    tests pass `origin/master` with its resolved sha printed on failure.
//
// THE CRITERION, and this file is its executable form — three arms, cheapest
// first, and a NEGATIVE from one arm is not "not landed" until all three fail:
//
//	① ANCESTRY  `git merge-base --is-ancestor <tip> <ref>` — a hit is a proof.
//	② PATCH-ID  the tip's `git patch-id --stable` equals one of <ref>'s — a hit
//	            is a proof, and it survives a rebase or a cherry-pick (which is
//	            how this repo lands work).
//	③ CONTENT   every line the commit ADDED is present, normalised — the only
//	            arm that survives a rebase that RE-DERIVED the patch, where ②
//	            is guaranteed to answer false (task_delivery_proof_content.go).
//
// and NEVER: a commit hash (a rebase rewrites it — arm 1), a commit subject
// (two different changes share one — arm 2), or a probe over `--all` (a tag is
// not a delivery point — arm 3).
//
// WHY THIS FILE CARRIES ITS OWN PROBES instead of calling the production arms:
// the three arms already exist and are exercised where they belong
// (`TaskDeliveryProof` / `deliveryProofContentArm`, judged against a LEDGER ROW
// and its recorded deliverable). This file judges a RAW GIT PAIR — tip against
// integration ref — so it can be re-run by the next reader against any repo,
// with no ledger, no session and no row. It adds no production mechanism
// (Rule 35): every call below is `rev-parse`, `show`, `rev-list`,
// `merge-base` or `patch-id` — no fetch, no push, no ref move, no write.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// lcIntegrationRef is the ONE ref a landed verdict is asked of. A clone
	// channel refreshes it every round; a client host's refs/heads/master is a
	// manual ref that goes stale by design (devdoc/integration-claim-ref-staleness.md).
	lcIntegrationRef = "refs/remotes/" + deliveryProofOriginRemote + "/" + masterBranchName
	// lcWorkPath is the file the "run" writes its work into.
	lcWorkPath = "work.go"
	// lcBasePath is the file the fixture's base commit carries.
	lcBasePath = "base.txt"
	// lcTipSubject is the subject the run's commit carries, and the subject arm 2
	// copies onto an UNRELATED change to show the arm cannot tell them apart.
	lcTipSubject = "fix(delivery-proof): a rebase that re-derived the content is still a delivery"
	// lcOtherSubject is a subject no arm may match on.
	lcOtherSubject = "chore: an unrelated change that nobody delivered"
	// lcGroundRefPrefix carries the ground's commits into the destination clone
	// WITHOUT giving them a branch or a tag: a verdict must be askable about a
	// commit the destination has the OBJECT for and no REF points at, which is
	// exactly the shape `git branch --contains` / `git tag --contains` cannot see.
	lcGroundRefPrefix = "refs/lc/ground/"
	// lcShaHexLen is the length of a hex sha (Rule 1: a literal with meaning gets
	// a name — a "verdict" whose ref resolves to anything else is not a verdict).
	lcShaHexLen = 40
)

// lcPatchID answers the CONTENT FINGERPRINT of one commit's patch. `--stable`
// is the spelling that ignores whitespace-only line moves; a plain `patch-id`
// would answer differently for the same change (git's own documentation).
func lcPatchID(t *testing.T, dir, rev string) string {
	t.Helper()
	diff := rebuildTestGit(t, dir, "show", "--no-color", "--format=", rev)
	cmd := exec.Command("git", "patch-id", "--stable")
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git patch-id --stable on %s in %s: %v\n%s", rev, dir, err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// lcLandedByHash is arm ① — and the hash IS a proof when it hits. It is the
// probe that answers FALSE for every commit the clone channel rebased.
func lcLandedByHash(t *testing.T, dir, ref, tip string) bool {
	t.Helper()
	_, err := rebuildTestGitE(t, dir, "merge-base", "--is-ancestor", tip, ref)
	return err == nil
}

// lcLandedByPatchID is arm ② — the tip's content fingerprint is already at the
// destination under SOME commit, whatever sha it arrived with.
func lcLandedByPatchID(t *testing.T, dir, ref, tip string) bool {
	t.Helper()
	want := lcPatchID(t, dir, tip)
	if want == "" {
		t.Fatalf("the tip %s has no patch-id in %s", tip, dir)
	}
	for _, c := range strings.Fields(rebuildTestGit(t, dir, "rev-list", ref)) {
		if lcPatchID(t, dir, c) == want {
			return true
		}
	}
	return false
}

// lcLandedBySubject is the BANNED probe, kept here on purpose: it is what the
// briefing's `--grep` half actually measured. It answers TRUE for an unrelated
// change that merely borrowed the title.
func lcLandedBySubject(dir, ref, subject string) bool {
	cmd := exec.Command("git", "-C", dir, "log", "--format=%s", "--fixed-strings",
		"--grep", subject, ref)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// lcLandedAnywhere is the OTHER banned probe — `--all` widened to every ref this
// clone carries, so an ARCHIVE TAG counts as a delivery point.
func lcLandedAnywhere(dir, tip string) bool {
	cmd := exec.Command("git", "-C", dir, "branch", "-a", "--contains", tip)
	out, err := cmd.CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return true
	}
	cmd = exec.Command("git", "-C", dir, "tag", "--contains", tip)
	out, err = cmd.CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// lcAddedLinesAllPresent is arm ③, the coarse form the criterion needs: every
// line the tip ADDS is present in the destination's version of that file. It is
// deliberately line-based and normalised (trailing whitespace dropped) — the
// production arm additionally drops comment-only and blank lines and
// self-referential `measured-at:` rows; a test that re-derived that normaliser
// would be a second implementation of it (Rule 35), so it asks the narrowest
// question that still separates "re-derived" from "absent".
func lcAddedLinesAllPresent(t *testing.T, dir, ref, tip string) bool {
	t.Helper()
	diff := rebuildTestGit(t, dir, "show", "--no-color", "--format=", "--unified=0", tip)
	var path string
	destinations := map[string]string{}
	added := map[string][]string{}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			path = strings.TrimPrefix(line, "+++ b/")
			if _, seen := destinations[path]; !seen {
				blob, err := rebuildTestGitE(t, dir, "show", ref+":"+path)
				if err != nil {
					blob = ""
				}
				destinations[path] = blob
			}
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if body := strings.TrimRight(strings.TrimPrefix(line, "+"), " \t"); strings.TrimSpace(body) != "" {
				added[path] = append(added[path], body)
			}
		}
	}
	if len(added) == 0 {
		return false
	}
	for path, lines := range added {
		have := map[string]bool{}
		for _, l := range strings.Split(destinations[path], "\n") {
			have[strings.TrimRight(l, " \t")] = true
		}
		for _, l := range lines {
			if !have[l] {
				return false
			}
		}
	}
	return true
}

// lcChannel builds the clone-channel shape and returns (server, ground, repo):
//
//	server  the bare delivery point
//	ground  a CLONE of it — it holds the run's commits, and its own
//	        origin/master is the CUT POINT (it never fetched after that)
//	repo    a SECOND clone that fetched the adoption — its origin/master is the
//	        fresh destination and it does NOT hold the run's commits.
//
// Two directories on purpose: the destination's objects need not exist in the
// ground (a clone that never fetched has the commits and not the adoption).
func lcChannel(t *testing.T) (server, ground, repo string) {
	t.Helper()
	root := t.TempDir()
	server = filepath.Join(root, "server")
	if err := os.MkdirAll(server, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, server, "init", "-q", "--bare", "-b", masterBranchName)

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "init", "-q", "-b", masterBranchName)
	lcIdentity(t, seed)
	if err := os.WriteFile(filepath.Join(seed, lcBasePath), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", lcBasePath)
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", server, masterBranchName)

	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", server, ground)
	lcIdentity(t, ground)
	repo = filepath.Join(root, "repo")
	rebuildTestGit(t, root, "clone", "-q", server, repo)
	lcIdentity(t, repo)
	return server, ground, repo
}

// lcIdentity configures a committer so a fixture commit needs no host config.
func lcIdentity(t *testing.T, dir string) {
	t.Helper()
	rebuildTestGit(t, dir, "config", "user.email", "lc@test")
	rebuildTestGit(t, dir, "config", "user.name", "lc")
}

// lcExposeGround copies the ground's branches into the destination clone under
// lcGroundRefPrefix. Objects only: an unfetched tip cannot be `git show`n, and
// the point of the whole exercise is that the destination never had the data.
func lcExposeGround(t *testing.T, repo, ground string) {
	t.Helper()
	rebuildTestGit(t, repo, "fetch", "-q", ground,
		"+refs/heads/*:"+lcGroundRefPrefix+"*")
}

// lcRunCommit writes the run's work in the ground and returns its tip.
func lcRunCommit(t *testing.T, ground, subject, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ground, lcWorkPath), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", lcWorkPath)
	rebuildTestGit(t, ground, "commit", "-q", "-m", subject)
	return rebuildTestGit(t, ground, "rev-parse", "HEAD")
}

// TestLandingCriterion_AReplayedCommitIsLandedThoughItsHashIsNot THERE — arm ①
// and arm ② disagree on the ordinary clone-channel delivery, and ② is right.
func TestLandingCriterion_AReplayedCommitIsLandedThoughItsHashIsNot(t *testing.T) {
	_, ground, repo := lcChannel(t)
	tip := lcRunCommit(t, ground, lcTipSubject, "func Work() int { return 1 }\n")
	lcExposeGround(t, repo, ground)

	// The channel replays it onto a destination that MOVED: a sibling commit
	// lands first, so the replay cannot keep the sha.
	if err := os.WriteFile(filepath.Join(repo, "sibling.txt"), []byte("sibling\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "sibling.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "chore: a sibling that moved the destination")
	rebuildTestGit(t, repo, "cherry-pick", tip)
	rebuildTestGit(t, repo, "push", "-q", "origin", masterBranchName)
	rebuildTestGit(t, repo, "fetch", "-q", "origin")

	landed := rebuildTestGit(t, repo, "rev-parse", lcIntegrationRef)
	if landed == tip {
		t.Fatalf("fixture is wrong: the replay must change the sha, both are %s", tip)
	}

	if lcLandedByHash(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("fixture is wrong: a replayed commit must NOT be an ancestor")
	}
	if !lcLandedByPatchID(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("arm ② must answer LANDED: the tip %s is patch-identical to a commit in %s (%s)",
			tip, lcIntegrationRef, landed)
	}
	t.Logf("hash probe: NOT landed (false negative) — patch-id probe: landed; %s is at %s as %s",
		tip, lcIntegrationRef, landed)
}

// TestLandingCriterion_ASharedSubjectIsNotAnIdentity — arm ③ of the headline,
// i.e. the subject probe, answers TRUE for an unrelated change that borrowed the
// title. This is the `--grep` half of the misjudgement, pinned.
func TestLandingCriterion_ASharedSubjectIsNotAnIdentity(t *testing.T) {
	_, _, repo := lcChannel(t)

	// The destination gets a DIFFERENT change that reuses the run's subject.
	if err := os.WriteFile(filepath.Join(repo, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "unrelated.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", lcTipSubject)
	rebuildTestGit(t, repo, "push", "-q", "origin", masterBranchName)
	rebuildTestGit(t, repo, "fetch", "-q", "origin")

	if !lcLandedBySubject(repo, lcIntegrationRef, lcTipSubject) {
		t.Fatalf("fixture is wrong: the subject probe must fire on a shared subject")
	}
	// The run's ACTUAL work is nowhere: prove it with a tip that carries it.
	groundTip := lcRunCommit(t, repo, lcOtherSubject, "func Work() int { return 2 }\n")
	if lcLandedByHash(t, repo, lcIntegrationRef, groundTip) {
		t.Fatalf("fixture is wrong: the probe tip must not be an ancestor")
	}
	if lcLandedByPatchID(t, repo, lcIntegrationRef, groundTip) {
		t.Fatalf("a shared SUBJECT must never make the patch-id arm fire — %s is not at %s",
			groundTip, lcIntegrationRef)
	}
	t.Logf("subject probe: LANDED (false positive) — patch-id probe: NOT landed; the subject %q names two different changes",
		lcTipSubject)
}

// TestLandingCriterion_ACommitOnAnArchiveTagIsNotDelivered — the `--all` half,
// pinned: a commit reachable from SOME ref answers "present anywhere" while the
// integration branch has never seen it.
func TestLandingCriterion_ACommitOnAnArchiveTagIsNotDelivered(t *testing.T) {
	_, ground, repo := lcChannel(t)
	tip := lcRunCommit(t, ground, lcOtherSubject, "func Work() int { return 3 }\n")

	// The run's commits are archived as a tag (Rule 9's archive shape) and never
	// pushed to the delivery point.
	rebuildTestGit(t, ground, "tag", "-a", "archive/task/lc", "-m", "loser", tip)
	rebuildTestGit(t, repo, "fetch", "-q", ground, "refs/tags/archive/task/lc:refs/tags/archive/task/lc")

	if !lcLandedAnywhere(repo, tip) {
		t.Fatalf("fixture is wrong: the tip is on an archive tag, so the --all probe must fire")
	}
	if lcLandedByHash(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("a tag is not a delivery point — %s is not an ancestor of %s", tip, lcIntegrationRef)
	}
	if lcLandedByPatchID(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("a tag is not a delivery point — no commit in %s carries %s's patch", lcIntegrationRef, tip)
	}
	t.Logf("--all probe: PRESENT (false positive, found on archive/task/lc) — the integration ref %s does not carry it",
		lcIntegrationRef)
}

// TestLandingCriterion_ARederivedRebaseNeedsTheContentArm — arm ②'s NEGATIVE is
// not a verdict. A rebase that re-derived a hunk changes the patch-id itself, so
// only the content arm can see that the work arrived.
func TestLandingCriterion_ARederivedRebaseNeedsTheContentArm(t *testing.T) {
	_, ground, repo := lcChannel(t)
	body := "// the run's work, in four lines\nfunc Work() int {\n\treturn 4\n}\n"
	tip := lcRunCommit(t, ground, lcTipSubject, body)
	lcExposeGround(t, repo, ground)

	// The destination carries the SAME added lines, inside a file the rebase
	// reflowed (a comment line the ground did not have). The patch-id therefore
	// differs, while the work is present.
	reflowed := "// reflowed by the rebase, same work\n" + body
	if err := os.WriteFile(filepath.Join(repo, lcWorkPath), []byte(reflowed), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", lcWorkPath)
	rebuildTestGit(t, repo, "commit", "-q", "-m", lcOtherSubject)
	rebuildTestGit(t, repo, "push", "-q", "origin", masterBranchName)
	rebuildTestGit(t, repo, "fetch", "-q", "origin")

	if lcLandedByHash(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("fixture is wrong: a re-derived rebase is never an ancestor")
	}
	if lcLandedByPatchID(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("fixture is wrong: a re-derived patch must change the patch-id, or this arm proves nothing")
	}
	if !lcAddedLinesAllPresent(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("arm ③ must answer LANDED: every line %s added is present at %s", tip, lcIntegrationRef)
	}
	t.Logf("patch-id probe: NOT landed (false negative) — content arm: landed; %s's lines are at %s",
		tip, lcIntegrationRef)
}

// TestLandingCriterion_TrulyUndeliveredWorkFailsAllThreeArms — the CONTROL, so a
// criterion that answers "landed" to everything cannot pass this file. The work
// exists only in the ground: no ancestry, no patch-id, no content at the point.
func TestLandingCriterion_TrulyUndeliveredWorkFailsAllThreeArms(t *testing.T) {
	_, ground, repo := lcChannel(t)
	tip := lcRunCommit(t, ground, lcTipSubject, "func Work() int { return 5 }\n")
	lcExposeGround(t, repo, ground)

	if lcLandedByHash(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("control is wrong: the tip was never pushed")
	}
	if lcLandedByPatchID(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("control is wrong: no commit at %s carries the tip's patch", lcIntegrationRef)
	}
	if lcAddedLinesAllPresent(t, repo, lcIntegrationRef, tip) {
		t.Fatalf("control is wrong: the work file does not exist at %s", lcIntegrationRef)
	}
	if lcLandedAnywhere(repo, tip) {
		t.Fatalf("control is wrong: the tip is on no ref in the destination clone")
	}
	t.Logf("all three arms answer NOT landed for %s — which is what a genuinely undelivered run looks like", tip)
}

// TestLandingCriterion_EveryVerdictNamesItsRefAndSha — the meta-arm: a verdict
// is a reading of a REVISION, so the ref must resolve and the sha must be
// carried with it. Run against the real tree (skipped where it is not a repo).
func TestLandingCriterion_EveryVerdictNamesItsRefAndSha(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("not in the repo tree: %v", err)
	}
	out, err := rebuildTestGitE(t, root, "rev-parse", "--verify", "--quiet", lcIntegrationRef)
	if err != nil || strings.TrimSpace(out) == "" {
		t.Skipf("no %s in this checkout — the ref is a clone channel's, and it must be NAMED to be read", lcIntegrationRef)
	}
	if len(strings.TrimSpace(out)) != lcShaHexLen {
		t.Fatalf("%s resolved to %q, which is not a sha — a verdict that cannot name its revision is not a verdict",
			lcIntegrationRef, out)
	}
	t.Logf("this checkout measures against %s = %s", lcIntegrationRef, out)
}
