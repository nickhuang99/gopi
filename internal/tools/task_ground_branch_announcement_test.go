package tools

// task_ground_branch_announcement_test.go — the ONE cross-check between the two
// producers of a row's ground branch name, and the arms that make a
// DISAGREEMENT RED instead of a SKIP.
//
// THE DEFECT THIS FILE EXISTS FOR. `task/<dashless stem>` had two producers and
// nothing compared them:
//
//	DERIVED     TaskGroundBranchName(taskID) — what the claim records as
//	            `deliverable.branch` and what the clone channel asks
//	            devworkspace to check out;
//	ANNOUNCED   the value published as GOPI_RUN_GROUND_BRANCH and read back by
//	            TaskGroundAnnounced — what the publish-target guard, the
//	            delivery-proof arm and the briefing use to answer "which branch
//	            is this run's?".
//
// A reader handed the wrong one does NOT fail. `gitPublishGroundBranch` falls
// back to a measured HEAD, the guard then has nothing to refuse, and the run
// proceeds on the wrong answer — a SKIP, and a skip reads exactly like a pass.
// The only place the disagreement can be made LOUD is where the value is
// published, so that is where TaskGroundBranchPublish sits and that is what
// these arms pin:
//
//	(a) for SEVERAL row ids the derivation and the published announcement are
//	    the same string, read back through the announcement's own reader;
//	(b) stand-on = `master` (the integration branch) → AGREES, and it is
//	    published: since the operator's ruling of 2026-11-05 a clone ground
//	    stands on the integration branch and delivers by pushing it. A name
//	    that is NOT it (a descriptive ref, another row's `task/<id>`) is still
//	    a divergence, still published as the address, still reported;
//	(c) stand-on = another ROW's `task/<id>` (a dash-carrying stem — the row id
//	    verbatim) → DIVERGES too. This is the arm that separates "the fallback
//	    we accept" from "somebody else's address we must not inherit", and it is
//	    red for the lazy implementation that accepts any `task/` name;
//	(d) the ONE legitimate divergence — the taken-name fallback, a uuid-named
//	    `task/<stem>` that names NO row — is ACCEPTED (published), with its
//	    diagnostic, exactly as the epic allows;
//	(e) a ground that could not name its branch is announced from the
//	    DERIVATION and says so, rather than publishing an empty claim.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/fullid"
	"github.com/nick/gopi/internal/testutil"
)

// The row ids these arms hammer. Three of them, because the property is "the
// two producers agree for EVERY row", and a single id cannot tell a real
// derivation from a hard-coded one (Rule 1: the values are constants here
// because the test asserts strings built from them).
const (
	gbRowA = "01a18d56-dd10-70fb-942a-92f57d531856"
	gbRowB = "01a141a7-0000-7000-8000-00000000beef"
	gbRowC = "01a11554-b3b1-731e-8425-2ce4986786f9"
	// gbSeedlessStem is a stem NO row will ever carry: the taken-name
	// fallback's shape (a fresh uuid v7, minted as a name no existing ref can
	// hold). It is written dashless, which is how TaskGroundBranchName renders
	// a stem, so the refusal path and this arm agree on the spelling.
	gbSeedlessStem = "0192beef00007000800000000000c0de"
)

// gbPublish is the one call the arms make, wrapped so each arm reads as its
// assertion rather than as plumbing.
func gbPublish(t *testing.T, taskID, standingOn string) (branch, divergence string) {
	t.Helper()
	return TaskGroundBranchPublish(taskID, standingOn)
}

// TestGroundBranchDerivationIsTheAnnouncementForEveryRow is arm (a): the two
// producers are one value, and the value survives a round trip through the
// announcement ENV — the exact channel a look-alike reader uses.
func TestGroundBranchDerivationIsTheAnnouncementForEveryRow(t *testing.T) {
	testutil.NewTestDBPath(t)

	seen := map[string]string{}
	for _, id := range []string{gbRowA, gbRowB, gbRowC} {
		derived := TaskGroundBranchName(id)
		if derived == "" {
			t.Fatalf("row %s has no derivable ledger address", id)
		}
		// The ground stands on its own address: the ordinary, agreeing case.
		branch, divergence := gbPublish(t, id, derived)
		if branch != derived {
			t.Fatalf("row %s: published %q, but the derivation is %q — the two producers disagree for the ordinary case", id, branch, derived)
		}
		if divergence != "" {
			t.Fatalf("row %s agrees with itself and must carry NO divergence sentence; got %q", id, divergence)
		}
		// ROUND TRIP through the announcement pair: the published value is
		// what TaskGroundAnnounced hands back. A reader that picks the other
		// spelling is what this file exists to catch, so the reader is asked.
		t.Setenv(TaskGroundBranchEnv, branch)
		if _, announced := TaskGroundAnnounced(); announced != derived {
			t.Fatalf("row %s: the announcement reads back %q, the derivation is %q", id, announced, derived)
		}
		if prev, dup := seen[branch]; dup {
			t.Fatalf("rows %s and %s published the SAME ref %q — a ref would resolve to two rows", prev, id, branch)
		}
		seen[branch] = id
		// The stem is the row's id with its dashes dropped, and it is a full
		// id — never a truncated form (fullid's rule), which is what makes
		// `gopi task show --branch <branch>` resolvable at all.
		if !fullid.IsUUID(id) {
			t.Fatalf("fixture row id %q is not a canonical uuid", id)
		}
		if want := TaskGroundBranchPrefix + strings.ReplaceAll(id, "-", ""); derived != want {
			t.Fatalf("row %s: derived %q, want %q", id, derived, want)
		}
	}
}

// TestGroundBranchIntegrationBranchIsTheAgreement is arm (b), RE-STATED for the
// operator's ruling of 2026-11-05: a clone ground STANDS ON the integration
// branch from the moment it is cloned, and that is the fact its announcement
// must carry.
//
// THE ARM IT REPLACES read standing-on-`master` as a DIVERGENCE and demanded the
// derived `task/<id>` be published. That encoded the pre-fix shape — a private
// ref minted per ground so the branch→row lookup had an address — and the shape
// was retired: the row is addressed by its WORKSPACE PATH, no `task/<id>` ref is
// minted, and a run that announced the derivation would be announcing a ref its
// ground never stood on. The publish-target guard reads the announcement as "the
// ONE destination this run may push", so that lie refuses the run's own
// `git push origin master` — the exact motion the ruling requires.
//
// THE DISCRIMINATOR IS UNCHANGED, and the second half of this arm keeps it red:
// a name that is NOT the integration branch is still a divergence, still
// reported, and still published as the row's address.
func TestGroundBranchIntegrationBranchIsTheAgreement(t *testing.T) {
	testutil.NewTestDBPath(t)

	derived := TaskGroundBranchName(gbRowA)
	// ① THE INTEGRATION BRANCH: agreement, no diagnostic (the ordinary shape).
	branch, divergence := gbPublish(t, gbRowA, masterBranchName)
	if branch != masterBranchName {
		t.Fatalf("a ground standing on %q announced %q — the announcement must be the branch the ground really stands on, "+
			"or the publish-target guard refuses this run's own push of it", masterBranchName, branch)
	}
	if divergence != "" {
		t.Fatalf("standing on the integration branch is the CHANNEL'S OWN SHAPE since 2026-11-05 and must carry no divergence sentence; got %q", divergence)
	}
	// ② SOMEBODY ELSE'S / A DESCRIPTIVE BRANCH: still a divergence, still
	// published as the address, still NAMED (the half that catches a real
	// disagreement — it must not go quiet when the integration branch stopped
	// being special).
	const descriptive = "feat/claim-ground"
	b, d := gbPublish(t, gbRowA, descriptive)
	if b != derived {
		t.Fatalf("a ground standing on %q announced %q; the row's ledger address is %q and that is what must be published", descriptive, b, derived)
	}
	if d == "" {
		t.Fatalf("standing on %q instead of %q is a DIVERGENCE and was reported as agreement — that silence is the SKIP this epic removes", descriptive, derived)
	}
	if !strings.Contains(d, derived) || !strings.Contains(d, descriptive) {
		t.Fatalf("the divergence sentence must name BOTH values (standing %q, address %q); got %q", descriptive, derived, d)
	}
}

// TestGroundBranchDivergenceOnAnotherRowsAddressIsRed is arm (c): another row's
// `task/<id>` is that row's address, never a fallback this row may inherit.
//
// The row IS in the ledger on purpose: the discriminator between "the fallback
// we accept" and "somebody else's ref" is the LEDGER, not the shape, and a
// shape test would accept this name. `task/<dashed row id>` is written
// literally — the dash-carrying form the briefing names — because that is the
// spelling a human or a stale file would hand over.
func TestGroundBranchDivergenceOnAnotherRowsAddressIsRed(t *testing.T) {
	testutil.NewTestDBPath(t)
	seedTaskRow(t, gbRowB, "the other row whose ref this one must not inherit")

	derived := TaskGroundBranchName(gbRowA)
	foreign := TaskGroundBranchPrefix + gbRowB // dash-carrying, as a human writes it
	branch, divergence := gbPublish(t, gbRowA, foreign)
	if branch == foreign {
		t.Fatalf("row %s announced ANOTHER row's address %q — the run would publish onto a ref that resolves to row %s", gbRowA, foreign, gbRowB)
	}
	if branch != derived {
		t.Fatalf("row %s announced %q, want its own address %q", gbRowA, branch, derived)
	}
	if divergence == "" {
		t.Fatalf("inheriting row %s's address must be REPORTED, not accepted silently", gbRowB)
	}
	// The same ref written DASHLESS is still another row's address: the stem
	// sanitiser is not a licence to treat the two spellings as different rows.
	dashless := TaskGroundBranchPrefix + strings.ReplaceAll(gbRowB, "-", "")
	if b2, d2 := gbPublish(t, gbRowA, dashless); b2 != derived || d2 == "" {
		t.Fatalf("the dashless spelling of row %s's address must be refused the same way; got branch=%q divergence=%q", gbRowB, b2, d2)
	}
}

// TestGroundBranchFallbackIsTheOneAcceptedDivergence is arm (d): the taken-name
// fallback names NO row, so publishing it is correct — and it is still
// REPORTED, because "the ground is not on its row's address" is a fact an
// operator reading a delivery refusal needs.
func TestGroundBranchFallbackIsTheOneAcceptedDivergence(t *testing.T) {
	testutil.NewTestDBPath(t)
	seedTaskRow(t, gbRowB, "a row that exists, so the ledger is genuinely readable")

	fallback := TaskGroundBranchPrefix + gbSeedlessStem
	branch, divergence := gbPublish(t, gbRowA, fallback)
	if branch != fallback {
		t.Fatalf("the taken-name fallback %q is the ONE legitimate divergence, but %q was published instead", fallback, branch)
	}
	if divergence == "" {
		t.Fatalf("the fallback must still be NAMED in the diagnostic; silence would make it indistinguishable from agreement")
	}
	if !strings.Contains(divergence, fallback) {
		t.Fatalf("the diagnostic must name the fallback it accepted; got %q", divergence)
	}
}

// TestGroundBranchUnmeasuredGroundAnnouncesTheDerivation is arm (e): a ground
// that could not name its branch must still yield a REAL announcement — the
// derivation — rather than an empty value a reader would silently skip.
func TestGroundBranchUnmeasuredGroundAnnouncesTheDerivation(t *testing.T) {
	testutil.NewTestDBPath(t)

	derived := TaskGroundBranchName(gbRowC)
	branch, divergence := gbPublish(t, gbRowC, "")
	if branch != derived {
		t.Fatalf("an unmeasured ground published %q; the derivation %q is the only honest answer", branch, derived)
	}
	if divergence == "" {
		t.Fatalf("an announcement that is DERIVED rather than MEASURED must say so (Rule 15)")
	}
	// A row with no ledger address at all has no derivation to fall back on:
	// the measurement (if any) is published, and the absence is named.
	if b, d := gbPublish(t, "", ""); b != "" || d == "" {
		t.Fatalf("a row with no id and no measurement must publish nothing AND say why; got branch=%q divergence=%q", b, d)
	}
}

// TestGroundBranchAnnouncementMatchesARealCheckedOutGround runs the cross-check
// against a REAL repository: the branch is measured the way the two writers
// measure it (`git rev-parse --abbrev-ref HEAD`), and the published value is
// then compared to the ref that actually exists — the property the whole epic
// is about, on a ground that is not a string in a test.
func TestGroundBranchAnnouncementMatchesARealCheckedOutGround(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	testutil.NewTestDBPath(t)

	repo := filepath.Join(t.TempDir(), "ground")
	gitRun(t, filepath.Dir(repo), "init", "-q", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	derived := TaskGroundBranchName(gbRowA)
	gitRun(t, repo, "checkout", "-q", "-b", derived)
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "the ground's own base")

	standing, err := currentGitBranch(repo)
	if err != nil {
		t.Fatalf("measure the ground's branch: %v", err)
	}
	branch, divergence := gbPublish(t, gbRowA, standing)
	if divergence != "" {
		t.Fatalf("a ground checked out on its own address must agree with the derivation; got %q", divergence)
	}
	if branch != derived {
		t.Fatalf("measured %q off the real ground but published %q", derived, branch)
	}
	// And the ref really is there: the announcement names something git can
	// resolve, which is the property a reader downstream relies on.
	gitRun(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
}
