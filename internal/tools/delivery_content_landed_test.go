package tools

// delivery_content_landed_test.go — THE REGRESSION FIXTURES for the one
// content-fingerprint seam (delivery_content_landed.go).
//
// WHAT IS PINNED, and why each fixture exists. The seam answers "does the WORK
// of this commit live at the delivery point?" and this repository lands work by
// cherry-pick / rebase, which REWRITES the sha and keeps the work. So the
// fixtures exercise exactly the two shapes that separate a content criterion
// from an identity criterion:
//
//	REPLAYED (hash changed, content identical)  → landed=true
//	RE-DERIVED (content really changed)         → landed=false
//
// and the third fixture is the RED PROOF: the same replayed pair, with the
// patch-id leg switched off (deliveryContentPatchIDOff), must stop being
// answered by that leg — proving the leg is what carries the verdict instead of
// the line-content leg quietly covering for it.
//
// THE BINARY FIXTURE IS ITS OWN REGRESSION. Measured while this seam was written
// (2026-10-28): `git show` renders a changed binary file as ONE line
// (`Binary files … differ`), so the text leg saw ZERO added lines for a commit
// whose ONLY content is a binary, and the `len(added) == 0` branch read
// "content-free, nothing owed at the destination" — i.e. LANDED for a run that
// delivered nothing. It is why the seam's content leg asks git's own `--numstat`
// binary statement and compares BLOB CONTENT IDENTITY for those paths.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The fixture's git identity: fixed so a fixture commit is byte-reproducible,
// and set per-invocation so no machine-global git config can leak in.
const (
	dcFixtureName  = "dc-fixture"
	dcFixtureEmail = "dc@fixture.test"
	// dcFixtureBranch is the branch ref the guard is asked about: a run's own
	// branch, standing at the PRE-rebase tip (exactly what `task/<id>` looks like
	// after the channel rebased that work onto master).
	dcFixtureBranch = "task/dc-fixture"
)

// dcGit runs one git command in dir and FAILS the test on error: a fixture whose
// setup silently half-succeeded measures nothing (Rule 15).
func dcGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// A LITERAL identity, per invocation: the fixture must not depend on, and
	// must not pollute, any machine-global git config. testutil.GitEnv also
	// STRIPS the inherited repository routing (GIT_DIR, GIT_WORK_TREE, …) so a
	// suite run from inside another repo cannot point this child at it.
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME="+dcFixtureName, "GIT_AUTHOR_EMAIL="+dcFixtureEmail,
		"GIT_COMMITTER_NAME="+dcFixtureName, "GIT_COMMITTER_EMAIL="+dcFixtureEmail)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// dcRepo makes a repository with one commit on `master` and returns its path.
func dcRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	dcGit(t, repo, "init", "-q", "-b", masterBranchName)
	dcWrite(t, repo, "base.txt", "base\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "base")
	return repo
}

// dcWrite writes one file inside the fixture repo.
func dcWrite(t *testing.T, repo, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dcReplayed is THE rebase fixture, built the way the clone channel builds it:
// a branch adds work, the integration branch MOVES, the branch is rebased onto
// it, and the integration branch is then advanced to the rebased tip — so the
// work is at the destination under a DIFFERENT sha, which is the only thing that
// changed.
//
// It returns (repo, originalTip, rebasedTip). Both tips exist in the repo, which
// is what lets the assertions name the rewrite rather than assume it.
func dcReplayed(t *testing.T, name, body string) (repo, originalTip, rebasedTip string) {
	t.Helper()
	repo = dcRepo(t)
	dcGit(t, repo, "checkout", "-qb", "feat")
	dcWrite(t, repo, name, body)
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "feat: add "+name)
	originalTip = dcGit(t, repo, "rev-parse", "HEAD")

	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcWrite(t, repo, "upstream-moved.txt", "the integration branch moved\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "upstream moves")
	dcGit(t, repo, "rebase", "-q", masterBranchName, "feat")
	rebasedTip = dcGit(t, repo, "rev-parse", "feat")

	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcGit(t, repo, "reset", "-q", "--hard", "feat")
	return repo, originalTip, rebasedTip
}

// TestDeliveryContentLanded_SurvivesARebaseThatChangedEveryHash is deliverable
// 2's first assertion: the sha is gone, the WORK is at the destination, the
// seam must answer LANDED.
func TestDeliveryContentLanded_SurvivesARebaseThatChangedEveryHash(t *testing.T) {
	ctx := context.Background()
	repo, originalTip, rebasedTip := dcReplayed(t, "work.txt", "line one\nline two\n")

	// The fixture must BE the shape under test: if the rebase left the sha
	// alone, the assertion below would pass for a reason that has nothing to do
	// with the criterion.
	if originalTip == rebasedTip {
		t.Fatalf("fixture is not a rewrite: the rebase kept the sha %s", originalTip)
	}
	if _, err := branchDeleteGit(ctx, repo, deliveryContentMergeBaseVerb, deliveryContentIsAncestorFlag, originalTip, masterBranchName); err == nil {
		t.Fatal("fixture is not a rewrite: the ORIGINAL tip is still an ancestor of master — ancestry alone would answer it")
	}

	landed, why, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, originalTip)
	if !judged {
		t.Fatalf("the question was not answered (judged=false, why=%q) — an unmeasured answer is not a verdict", why)
	}
	if !landed {
		t.Fatalf("a replayed commit was judged NOT landed (why=%q) — this is the false accusation the content criterion exists to prevent", why)
	}

	// AND THE LEG THAT ANSWERED IT is the patch-id leg, named: with it off, the
	// same pair must fall through to the content leg, and its sentence must say
	// so — the two legs are told apart by what they SAY, never by a boolean.
	deliveryContentPatchIDOff = true
	defer func() { deliveryContentPatchIDOff = false }()
	landedOff, whyOff, judgedOff := DeliveryContentLanded(ctx, repo, repo, masterBranchName, originalTip)
	if !judgedOff || !landedOff {
		t.Fatalf("patch leg OFF: landed=%v judged=%v why=%q — the content leg must still carry a pure replay", landedOff, judgedOff, whyOff)
	}
	if !strings.Contains(whyOff, "content-equivalent") {
		t.Errorf("patch leg OFF produced %q, want the content leg's sentence — the two legs must be distinguishable", whyOff)
	}
}

// TestDeliveryContentLanded_RefusesContentThatReallyChanged is deliverable 2's
// second assertion: the table was regenerated against a moved base, so the bytes
// the run produced are NOT at the destination. No criterion may call that
// delivered.
func TestDeliveryContentLanded_RefusesContentThatReallyChanged(t *testing.T) {
	ctx := context.Background()
	repo := dcRepo(t)

	dcGit(t, repo, "checkout", "-qb", "feat")
	dcWrite(t, repo, "table.tsv", "row\tbase=THE-RUNS-MEASUREMENT\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "feat: regenerate the census row")
	runTip := dcGit(t, repo, "rev-parse", "HEAD")

	// The destination regenerates the SAME path against ITS base: same shape,
	// different bytes — a rebase that re-derived the content.
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcWrite(t, repo, "table.tsv", "row\tbase=THE-DESTINATIONS-MEASUREMENT\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "the destination regenerates the same table against its own base")

	landed, why, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, runTip)
	if !judged {
		t.Fatalf("the question was not answered (judged=false, why=%q)", why)
	}
	if landed {
		t.Fatalf("content that really changed was judged LANDED (why=%q) — this is the silent pass that reaches `completed` with nothing delivered", why)
	}
	// The refusal must SAY what is missing: a verdict a reader cannot act on is
	// the other half of the defect (Rule 15).
	if !strings.Contains(why, "table.tsv") || !strings.Contains(why, deliveryProofClassLinesMiss) {
		t.Errorf("the refusal does not name the path and the class (%q)", why)
	}
}

// TestDeliveryContentLanded_BinaryContentIsComparedNotSkipped is the regression
// for the measured false positive described in the file header: git prints ONE
// `Binary files … differ` line instead of a text diff, so a commit whose entire
// content is binary used to reach `len(added) == 0` → "content-free" → LANDED.
//
// LANDED is the FAILING direction here, so both halves are asserted: a binary
// that IS at the destination answers landed, and one that is NOT answers not.
func TestDeliveryContentLanded_BinaryContentIsComparedNotSkipped(t *testing.T) {
	ctx := context.Background()
	const blob = "\x00\x01\x02binary payload\x00"
	repo, originalTip, rebasedTip := dcReplayed(t, "blob.dat", blob)
	if originalTip == rebasedTip {
		t.Fatalf("fixture is not a rewrite: the rebase kept the sha %s", originalTip)
	}
	if landed, why, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, originalTip); !judged || !landed {
		t.Fatalf("a replayed binary was judged landed=%v judged=%v (why=%q) — blob content must be compared, not skipped", landed, judged, why)
	}

	// The negative half: the destination carries a DIFFERENT blob for that path,
	// so the run's bytes are nowhere.
	repo2 := dcRepo(t)
	dcGit(t, repo2, "checkout", "-qb", "feat")
	dcWrite(t, repo2, "blob.dat", blob)
	dcGit(t, repo2, "add", "-A")
	dcGit(t, repo2, "commit", "-qm", "feat: add the run's binary")
	runTip := dcGit(t, repo2, "rev-parse", "HEAD")
	dcGit(t, repo2, "checkout", "-q", masterBranchName)
	dcWrite(t, repo2, "blob.dat", "\x00\x01\x02a DIFFERENT payload\x00")
	dcGit(t, repo2, "add", "-A")
	dcGit(t, repo2, "commit", "-qm", "the destination generates its own binary")
	landed, why, judged := DeliveryContentLanded(ctx, repo2, repo2, masterBranchName, runTip)
	if !judged {
		t.Fatalf("negative fixture: judged=false (why=%q)", why)
	}
	if landed {
		t.Fatalf("a binary whose bytes are NOT at the destination was judged LANDED (why=%q) — this is the false positive the blob-identity leg closes", why)
	}
}

// TestDeliveryContentLanded_UnmeasurableIsNeverARefusal pins the THREE-VALUED
// contract (memory #3342/#3339): a question that could not be asked answers
// judged=false, and never `landed=false`. The direction matters — a guard that
// refuses what it cannot see wedges the ledger.
func TestDeliveryContentLanded_UnmeasurableIsNeverARefusal(t *testing.T) {
	ctx := context.Background()
	repo := dcRepo(t)
	tip := dcGit(t, repo, "rev-parse", "HEAD")

	if _, _, judged := DeliveryContentLanded(ctx, repo, repo, "", tip); judged {
		t.Error("an empty destination ref was judged — there was nothing to measure against")
	}
	if _, _, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, ""); judged {
		t.Error("an empty tip was judged")
	}
	// A destination ref that does not resolve in a directory git cannot read.
	if _, _, judged := DeliveryContentLanded(ctx, repo, filepath.Join(repo, "does-not-exist"),
		"refs/heads/no-such-ref", tip); judged {
		t.Error("an unreadable destination was judged — Unknown must not collapse into a verdict")
	}
}

// TestDeliveryContentLanded_RefusesIdentityInstruments is the criterion stated
// as a test (deliverable 1's "explicitly does not read …"): a fixture where the
// TITLE and the WORKTREE FILE say "delivered" while the content says otherwise
// must answer NOT landed. It is the shape four faces of this machinery were
// measured getting wrong on one night.
func TestDeliveryContentLanded_RefusesIdentityInstruments(t *testing.T) {
	ctx := context.Background()
	repo := dcRepo(t)

	dcGit(t, repo, "checkout", "-qb", "feat")
	dcWrite(t, repo, "the-work.txt", "the run's actual work\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "feat: add the-work.txt")
	runTip := dcGit(t, repo, "rev-parse", "HEAD")

	// The destination carries a commit with the SAME SUBJECT on an unrelated
	// path — the "title says delivered" trap, and a working tree that has the
	// file checked out is equally irrelevant to this seam.
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcWrite(t, repo, "somewhere-else.txt", "unrelated\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "feat: add the-work.txt")
	if !strings.HasSuffix(dcGit(t, repo, "log", "-1", "--format=%s", masterBranchName), "feat: add the-work.txt") {
		t.Fatal("fixture is not the shape: the destination's subject does not match")
	}

	landed, why, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, runTip)
	if !judged {
		t.Fatalf("judged=false (why=%q)", why)
	}
	if landed {
		t.Fatalf("a matching TITLE decided the verdict (why=%q) — the criterion must read content, not identity", why)
	}
}

// TestDeliveryContentLanded_PatchIDLegIsWhatAnswersIt is THE RED PROOF
// (deliverable 2's third assertion): ONE fixture, judged with the patch-id leg
// on and then off, and the verdict flips.
//
// THE FIXTURE IS THE "LANDED, THEN SUPERSEDED" SHAPE, and it is realistic rather
// than adversarial: the run's commit IS on the integration branch (the patch-id
// leg says so, and that is true), and a LATER destination commit removed the very
// line it added, after which more than the bounded history window's worth of
// commits touched the same path. The line-content leg scans only the newest
// `deliveryProofContentClosestCandidates` commits behind the tip, so it can no
// longer see the change — exactly the bounded-walk limit that makes the patch-id
// leg load-bearing instead of decorative.
//
// With the leg ON: landed=true (the work DID land). With it OFF: landed=false.
// One boolean, one fixture, no other difference.
func TestDeliveryContentLanded_PatchIDLegIsWhatAnswersIt(t *testing.T) {
	ctx := context.Background()
	repo := dcRepo(t)

	dcGit(t, repo, "checkout", "-qb", "feat")
	dcWrite(t, repo, "work.txt", "the run's own line\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "feat: add work.txt")
	originalTip := dcGit(t, repo, "rev-parse", "HEAD")

	// The work lands on the destination (a rebase onto a moved base), so the
	// delivery is REAL — and it is real under a different sha.
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcWrite(t, repo, "upstream.txt", "upstream\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "upstream moves")
	dcGit(t, repo, "rebase", "-q", masterBranchName, "feat")
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcGit(t, repo, "reset", "-q", "--hard", "feat")
	landedTip := dcGit(t, repo, "rev-parse", "HEAD")
	if landedTip == originalTip {
		t.Fatalf("fixture is not a rewrite: the rebase kept the sha %s", originalTip)
	}

	// Then the destination moves PAST the change: the run's line is removed, and
	// more commits touch the same path than the content leg's bounded window
	// looks at.
	dcWrite(t, repo, "work.txt", "destination rewrote this file\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "the destination supersedes the run's line")
	for i := 0; i < deliveryProofContentClosestCandidates+2; i++ {
		dcWrite(t, repo, "work.txt", "destination rewrote this file\nfiller "+strings.Repeat("x", i+1)+"\n")
		dcGit(t, repo, "add", "-A")
		dcGit(t, repo, "commit", "-qm", "later destination work on the same path")
	}

	landed, why, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, originalTip)
	if !judged || !landed {
		t.Fatalf("with the patch-id leg ON: landed=%v judged=%v why=%q — the fixture is chosen so that leg is what answers", landed, judged, why)
	}
	if !strings.Contains(why, "patch-equivalent") {
		t.Fatalf("the verdict did not come from the patch-id leg: %q", why)
	}

	deliveryContentPatchIDOff = true
	defer func() { deliveryContentPatchIDOff = false }()
	landedOff, whyOff, judgedOff := DeliveryContentLanded(ctx, repo, repo, masterBranchName, originalTip)
	// THE JUDGEMENT THIS TEST EXISTS FOR (kept verbatim): with the leg off, the
	// verdict must FLIP — it must stop being "landed".
	if landedOff {
		t.Fatalf("RED PROOF DID NOT FIRE: with the patch-id leg disabled the same fixture still answered landed=true (why=%q) — the verdict is coming from somewhere else", whyOff)
	}
	// AND THE HALF THIS TEST USED TO GET WRONG. It required judged=true, i.e. it
	// required the bounded line-content leg to answer "NOT LANDED" from a window
	// it had visibly RAN OUT OF — pinning the very defect this repair exists for
	// (a window's silence reported as a finding about the work). The leg's
	// silence is a fact about the WINDOW, so the correct pair here is
	// judged=false (NOT MEASURED) + landed=false (no positive evidence), and the
	// why-sentence must say so instead of leaving a reader to guess.
	if judgedOff && !landedOff {
		t.Fatalf("the bounded leg answered a VERDICT of not-landed (why=%q) from a window that ran out — that is the "+
			"\"window truncated = deterministically absent\" defect, not a measurement", whyOff)
	}
	if !judgedOff {
		if !strings.Contains(whyOff, deliveryProofClassOutOfWindow) {
			t.Fatalf("an unmeasured answer must NAME the exhausted window (%q) so a caller can tell it from an unreadable "+
				"object; got why=%q", deliveryProofClassOutOfWindow, whyOff)
		}
		if !strings.Contains(whyOff, "NOT MEASURED") {
			t.Fatalf("an unmeasured answer must SAY it is not a finding about the work; got why=%q", whyOff)
		}
	}
}

// TestMergeBacklogPatchSuperseded_AsksTheSeam is the BYPASS READER's regression
// (deliverable 1's second half): the merge-backlog guard used to answer with its
// own `git cherry` parse, which is the identity-shaped instrument the whole epic
// is about. It now asks DeliveryContentLanded, and this test pins the two facts
// that change for a caller:
//
//	① a REBASED branch (sha gone, work present) answers superseded — the shape a
//	  bare `rev-list --count` guard refuses and a bare cherry answers only when
//	  the patch id survived;
//	② a branch whose work is genuinely ABSENT answers NOT superseded, and the
//	  three-valued answer is never turned into a refusal (an unmeasurable input
//	  answers false, which leaves the caller's safe posture standing).
func TestMergeBacklogPatchSuperseded_AsksTheSeam(t *testing.T) {
	repo, originalTip, rebasedTip := dcReplayed(t, "guard-work.txt", "the guard's work\n")
	if originalTip == rebasedTip {
		t.Fatalf("fixture is not a rewrite: the rebase kept the sha %s", originalTip)
	}
	// The guard is asked about the BRANCH REF, so give the original commit one:
	// a ref pointing at the pre-rebase tip is exactly what a run's `task/<id>`
	// ref looks like after the channel rebased its work onto master.
	dcGit(t, repo, "update-ref", "refs/heads/"+dcFixtureBranch, originalTip)

	if superseded, why := mergeBacklogPatchSuperseded(repo, "refs/heads/"+dcFixtureBranch); !superseded {
		t.Fatalf("a rebased-but-delivered branch was NOT superseded (why=%q) — the guard would refuse a legitimate close", why)
	} else if !strings.Contains(why, "the work of") {
		t.Errorf("the guard's evidence does not read like the seam's: %q", why)
	}

	// ② the negative half: work that is nowhere.
	dcGit(t, repo, "checkout", "-qb", "undelivered", masterBranchName)
	dcWrite(t, repo, "nowhere.txt", "nothing has this\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "work that never lands")
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	if superseded, why := mergeBacklogPatchSuperseded(repo, "undelivered"); superseded {
		t.Fatalf("undelivered work answered superseded (why=%q) — the guard would close a row whose branch is unmerged", why)
	}

	// ③ unmeasurable is not a refusal: a ref git cannot resolve answers false
	// WITHOUT collapsing into a verdict (the caller keeps its own posture).
	if superseded, _ := mergeBacklogPatchSuperseded(repo, "refs/heads/no-such-branch"); superseded {
		t.Error("an unresolvable ref answered superseded")
	}
}

// TestDeliveryContentLanded_WindowExhaustedIsNotAMeasurement is THE REPRO for
// the "window truncated = deterministically absent" defect: the destination's
// bounded scan runs out of history BEFORE it can reach the commit that would
// answer the question, and the seam must answer NOT MEASURED — never "absent".
//
// THE FIXTURE IS THE ONE THE PATCH-ID RED PROOF ALREADY BUILDS (work lands, a
// later destination commit supersedes the very line it added, then more commits
// touch the same path than the window looks at), used here for the question that
// proof does not ask: what does the BOUNDED reader say when nothing above it has
// answered? Before the repair it said landed=false/judged=true and classified the
// missing lines as `added-lines-missing` — a statement about the WORK derived
// from a statement about the WINDOW. An 8-commit window is a guess about how far
// the destination has moved, and a guess that expires is not evidence.
func TestDeliveryContentLanded_WindowExhaustedIsNotAMeasurement(t *testing.T) {
	ctx := context.Background()
	repo := dcRepo(t)

	dcGit(t, repo, "checkout", "-qb", "feat")
	dcWrite(t, repo, "work.txt", "the run's own line\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "feat: add work.txt")
	originalTip := dcGit(t, repo, "rev-parse", "HEAD")

	// The work DOES land (under another sha), then the destination moves past it.
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcWrite(t, repo, "upstream.txt", "upstream\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "upstream moves")
	dcGit(t, repo, "rebase", "-q", masterBranchName, "feat")
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcGit(t, repo, "reset", "-q", "--hard", "feat")
	landedTip := dcGit(t, repo, "rev-parse", "HEAD")
	if landedTip == originalTip {
		t.Fatalf("fixture is not a rewrite: the rebase kept the sha %s", originalTip)
	}
	dcWrite(t, repo, "work.txt", "destination rewrote this file\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "the destination supersedes the run's line")
	for i := 0; i < deliveryProofContentClosestCandidates+2; i++ {
		dcWrite(t, repo, "work.txt", "destination rewrote this file\nfiller "+strings.Repeat("x", i+1)+"\n")
		dcGit(t, repo, "add", "-A")
		dcGit(t, repo, "commit", "-qm", "later destination work on the same path")
	}

	// PRE-CONDITION — the fixture must BE the shape, or this proves nothing: the
	// landing commit has to sit OUTSIDE the window the bounded reader looks at,
	// and the window has to be FULL (i.e. more path history exists behind it).
	window := strings.Fields(dcGit(t, repo, "log", "--format=%H", "-n",
		fmt.Sprint(deliveryProofContentClosestCandidates), masterBranchName, "--", "work.txt"))
	if len(window) < deliveryProofContentClosestCandidates {
		t.Fatalf("fixture is wrong: the window must be FULL (%d commits touching work.txt, want %d)",
			len(window), deliveryProofContentClosestCandidates)
	}
	for _, c := range window {
		if c == landedTip {
			t.Fatalf("fixture is wrong: the landing commit %s is INSIDE the window — nothing is truncated",
				branchDeleteShortHash(landedTip))
		}
	}

	// The question is asked with the patch-id leg OFF, so nothing above the
	// bounded reader can answer it: this test measures the BOUNDED reader itself.
	deliveryContentPatchIDOff = true
	defer func() { deliveryContentPatchIDOff = false }()

	landed, why, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, originalTip)
	if judged {
		t.Fatalf("THE DEFECT: the bounded reader answered a VERDICT (landed=%v, why=%q) from a window it ran out of — "+
			"a truncated scan must report NOT MEASURED", landed, why)
	}
	if landed {
		t.Fatalf("an unmeasured answer must carry no positive claim either (why=%q)", why)
	}
	for _, want := range []string{deliveryProofClassOutOfWindow, "NOT MEASURED", "work.txt"} {
		if !strings.Contains(why, want) {
			t.Errorf("the unmeasured answer must name %q so a caller can tell an expired window from an unreadable "+
				"object and from a real gap; got: %s", want, why)
		}
	}
	// AND IT MUST NOT PASS OFF THE WINDOW'S SILENCE AS A FINDING: the classes a
	// refusal uses to say "the work is missing" describe the WORK, and no path
	// here was shown to be missing.
	for _, unwanted := range []string{deliveryProofClassLinesMiss, deliveryProofClassAbsent} {
		if strings.Contains(why, unwanted) {
			t.Errorf("an expired window must not be classified as %q — that is a claim about the work, measured "+
				"nowhere here; got: %s", unwanted, why)
		}
	}
}

// TestDeliveryContentLanded_WindowNotExhaustedStillRefuses is the CONTROL for
// the repro above: when the bounded scan DOES reach the root of the path's
// history and still does not carry the lines, the negative IS measured and the
// seam must keep answering judged=true/landed=false. Without this pair, "answer
// unverified whenever you cannot find it" would pass the repro and quietly turn
// the whole criterion into a no-op.
func TestDeliveryContentLanded_WindowNotExhaustedStillRefuses(t *testing.T) {
	ctx := context.Background()
	repo := dcRepo(t)

	dcGit(t, repo, "checkout", "-qb", "feat")
	dcWrite(t, repo, "work.txt", "the run's own line\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "feat: add work.txt")
	originalTip := dcGit(t, repo, "rev-parse", "HEAD")

	// The destination carries a commit on the SAME path with DIFFERENT content,
	// and its whole history for that path is one commit — far short of the
	// window, so nothing is truncated and the negative is measured.
	dcGit(t, repo, "checkout", "-q", masterBranchName)
	dcWrite(t, repo, "work.txt", "the destination's own line\n")
	dcGit(t, repo, "add", "-A")
	dcGit(t, repo, "commit", "-qm", "the destination does its own work there")

	deliveryContentPatchIDOff = true
	defer func() { deliveryContentPatchIDOff = false }()

	landed, why, judged := DeliveryContentLanded(ctx, repo, repo, masterBranchName, originalTip)
	if !judged {
		t.Fatalf("a window that reached the ROOT of the path's history settles the question — judged=false (why=%q) "+
			"would make every refusal unmeasurable", why)
	}
	if landed {
		t.Fatalf("the run's line is nowhere in the destination's history and the work must be refused (why=%q)", why)
	}
	if !strings.Contains(why, deliveryProofClassLinesMiss) {
		t.Fatalf("a MEASURED negative must classify the gap as %q; got: %s", deliveryProofClassLinesMiss, why)
	}
}
