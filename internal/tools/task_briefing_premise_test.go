package tools

// task_briefing_premise_test.go — the dispatch-time premise refresh, driven
// through REAL git in a REAL repository (the shape master_claim_test.go uses,
// because the verdict IS an exit status and a text match would not be one).
//
// THREE LEGS, and the middle one is the reason this file exists:
//
//	① RED — a briefing asserting a LANDED revision is "not on master" gets the
//	   stamp, naming the revision, the measured exit status and what to do.
//	② POSITIVE CONTROL — the SAME briefing shape naming a genuinely UNLANDED
//	   revision gets NO stamp. A guard that stamps everything is as useless as
//	   one that stamps nothing: without this leg, leg ① would pass on a function
//	   that ignores git entirely.
//	③ THE UNMEASURABLE ARM — an unresolvable repo must be STATED (Rule 15),
//	   never read as a pass and never as a refusal of the run.

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// premiseGitTimeout bounds every git child this file spawns (Rule 27: a test
// that can park the package's budget carries its own ceiling).
const premiseGitTimeout = 60 * time.Second

// premiseLandedExitPhrase is the exit-status spelling the stamp must carry
// (master_claim.go's ancestor status is 0 = an ancestor; the value is not
// re-typed here — the phrase is asserted and the status is checked against the
// evidence below).
const premiseLandedExitPhrase = "exit="

const (
	premiseFixtureSeedMsg    = "seed"
	premiseFixtureFeatureMsg = "feature"
	premiseFixtureSideBranch = "feat/premise-fixture"
)

// premiseFixture is a repo whose master tip is LANDED (an ancestor of master)
// and whose side-branch tip is NOT: the two inputs the two legs need, built
// once each.
type premiseFixture struct {
	Repo string
	// Landed is the master tip — "not on master" about it is a STALE premise.
	Landed string
	// Unlanded is the side branch's tip, reachable but not an ancestor.
	Unlanded string
}

// premiseBuildRepo builds the fixture with REAL git commands.
func premiseBuildRepo(t *testing.T) premiseFixture {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), premiseGitTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", IntegrationBranchName)
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("commit", "--allow-empty", "-q", "-m", premiseFixtureSeedMsg)
	landed := git("rev-parse", "HEAD")
	git("checkout", "-q", "-b", premiseFixtureSideBranch)
	git("commit", "--allow-empty", "-q", "-m", premiseFixtureFeatureMsg)
	unlanded := git("rev-parse", "HEAD")
	git("checkout", "-q", IntegrationBranchName)
	return premiseFixture{Repo: repo, Landed: landed, Unlanded: unlanded}
}

// premiseBriefing renders the briefing shape the motivating row carried: a
// premise sentence naming a revision and asserting it never reached the
// integration branch (row 01a12c7b, base 305ae03d0 / fix 9867ed461).
func premiseBriefing(rev string) string {
	return "# Epic: fix the read path\n\n" +
		"The row 01a12c4a is completed but its fix (" + rev + ") never landed on " +
		IntegrationBranchName + " — re-implement it in this run's worktree.\n"
}

// TestBriefingPremise_ALandedRevisionIsStamped is leg ① — the RED PROOF of the
// fix: the premise was true when the briefing was written and is FALSE at
// dispatch, so the child must be told, with the measurement behind it.
func TestBriefingPremise_ALandedRevisionIsStamped(t *testing.T) {
	f := premiseBuildRepo(t)
	clause, record := TaskBriefingPremiseRefresh(f.Repo, premiseBriefing(f.Landed))
	if clause == "" {
		t.Fatalf("a briefing naming the LANDED revision %s got no stamp — this is the defect: "+
			"the child re-implements work already on %s; record: %v", f.Landed, IntegrationBranchName, record)
	}
	ev := MasterClaimEvidenceAt(f.Repo, f.Landed)
	for _, want := range []string{
		TaskBriefingPremiseLabel,      // the clause's own marker
		ev.ShortCommit,                // the revision, abbreviated
		IntegrationBranchName,         // the branch it is on
		TaskBriefingPremiseVerifyOnly, // what to do
		"do NOT re-implement",         // the instruction, spelled
		premiseLandedExitPhrase,       // the measurement, not an opinion
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the stamped clause must carry %q; clause:\n%s", want, clause)
		}
	}
	// THE RECORD IS THE STAMP'S EVIDENCE (Rule 15): one line per claim examined,
	// and the landed one must say so where the run's facts are read.
	if len(record) != 1 || !strings.Contains(record[0], "STAMPED") {
		t.Fatalf("the seam's record must carry the landed verdict; got %v", record)
	}
	if !strings.Contains(record[0], ev.ShortCommit) || !strings.Contains(record[0], "exit=0") {
		t.Errorf("the record line must name the revision and its exit status; got %q", record[0])
	}
}

// TestBriefingPremise_AnUnlandedRevisionIsNotStamped is leg ② — the POSITIVE
// CONTROL. The same detector, the same sentence shape, a revision that is
// genuinely open: the briefing stands, nothing is stamped, and the measurement
// still reaches the record.
func TestBriefingPremise_AnUnlandedRevisionIsNotStamped(t *testing.T) {
	f := premiseBuildRepo(t)
	clause, record := TaskBriefingPremiseRefresh(f.Repo, premiseBriefing(f.Unlanded))
	if clause != "" {
		t.Fatalf("a briefing naming the genuinely UNLANDED revision %s was stamped — a guard that "+
			"stamps everything tells the child to skip real work; clause:\n%s", f.Unlanded, clause)
	}
	if len(record) != 1 {
		t.Fatalf("the measurement must still be recorded for an open premise; got %v", record)
	}
	if !strings.Contains(record[0], "is NOT an ancestor") || !strings.Contains(record[0], "exit=1") {
		t.Errorf("the open-premise record must state the measured negative verdict; got %q", record[0])
	}
}

// TestBriefingPremise_AnUnmeasurableRepoIsStatedNotPassed is leg ③. With no
// repo to measure against, every claim is UNMEASURED — the one outcome that
// must never be rendered as a pass, and never as a refusal either (the run
// proceeds).
func TestBriefingPremise_AnUnmeasurableRepoIsStatedNotPassed(t *testing.T) {
	f := premiseBuildRepo(t)
	clause, record := TaskBriefingPremiseRefresh("", premiseBriefing(f.Landed))
	if clause != "" {
		t.Fatalf("an unmeasurable repo produced a stamp — nothing was measured, so nothing may be "+
			"asserted; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Fatalf("an unmeasurable repo must be STATED per claim (never a silent pass); got %v", record)
	}
	if !strings.Contains(record[0], "not a pass") {
		t.Errorf("the unmeasured record must say what it is NOT; got %q", record[0])
	}
	// A missing directory is a different unmeasurable arm than an empty one: both
	// must be stated, neither may be a pass.
	_, missing := TaskBriefingPremiseRefresh(f.Repo+"/does-not-exist", premiseBriefing(f.Landed))
	if len(missing) != 1 || !strings.Contains(missing[0], "UNMEASURED") {
		t.Errorf("a nonexistent repo must be stated, not read as a pass; got %v", missing)
	}
}

// TestBriefingPremise_NoClaimIsANoOp is the blast-radius guard: the ordinary
// briefing (no such sentence) must be left entirely alone — no clause, no
// record — or every dispatched row would grow a paragraph about a premise it
// never made.
func TestBriefingPremise_NoClaimIsANoOp(t *testing.T) {
	f := premiseBuildRepo(t)
	for name, body := range map[string]string{
		"no premise at all": "# Epic\nDo the thing " + f.Landed + " describes.\n",
		// The MIRROR's truthful shape: this sentence is the one implclaim.go
		// exists for ("master ALREADY has X"), it is TRUE, and a stale-premise
		// detector that fired on it would tell the child to skip work that is
		// really landed. It is excluded by the landed marker, and that is the
		// ONLY new thing separating the two classes — stated here rather than
		// hidden, because a reader must know the detector's real boundary.
		"the mirror's truthful shape": "# Report\n\n- the work (" + f.Landed + ") has already landed on " + IntegrationBranchName + ", so this run verified it instead\n",
		"a premise with no rev":       "# Epic\nsomething is missing from " + IntegrationBranchName + " — find it.\n",
	} {
		clause, record := TaskBriefingPremiseRefresh(f.Repo, body)
		if clause != "" {
			t.Errorf("%s: nothing measurable was claimed, so nothing may be stamped; got:\n%s", name, clause)
		}
		if name == "a premise with no rev" && len(record) != 1 {
			t.Errorf("%s: a claim naming no revision must still be STATED; got %v", name, record)
		}
	}
}

// TestBriefingPremise_ASecondPassAppendsNothing is the idempotence leg: the
// clause QUOTES the sentence it contradicts, and that quote carries the very
// tokens the detector looks for — so a naive re-read of a stamped briefing
// re-fires on its own output (measured before this leg existed). A briefing
// handed over twice (a resume, a re-dispatch) must not grow a stamp about a
// stamp.
func TestBriefingPremise_ASecondPassAppendsNothing(t *testing.T) {
	f := premiseBuildRepo(t)
	body := premiseBriefing(f.Landed)
	clause, _ := TaskBriefingPremiseRefresh(f.Repo, body)
	if clause == "" {
		t.Fatalf("fixture precondition: the first pass must stamp %s", f.Landed)
	}
	again, record := TaskBriefingPremiseRefresh(f.Repo, body+"\n\n"+clause)
	if again != "" {
		t.Fatalf("a second pass APPENDED to an already-stamped briefing — the stamp re-fires on its own quote:\n%s", again)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Errorf("the second pass must be STATED (Rule 15), not silent; got %v", record)
	}
}

// TestBriefingPremise_MirrorsTheImplClaimDetector pins the relationship the epic
// asked for: the two detectors are MIRRORS over the same sentence population —
// implclaim.go finds "master ALREADY has X", this one finds "X is NOT on
// master" — and each one's truthful shape is the other's subject, so neither
// may fire on it.
func TestBriefingPremise_MirrorsTheImplClaimDetector(t *testing.T) {
	f := premiseBuildRepo(t)
	stale := premiseBriefing(f.Landed)
	truthful := "# Report\n\n- the work (" + f.Landed + ") has already landed on " + IntegrationBranchName + "\n"
	implClaim := "# Report\n\n- " + IntegrationBranchName + " already implements " + f.Landed + "\n"

	if got := taskreport.UnlandedClaimsIn(stale, IntegrationBranchName).Claims; len(got) != 1 {
		t.Fatalf("the stale premise must be detected as a claim; got %v", got)
	}
	if got := taskreport.ImplClaimsIn(stale, IntegrationBranchName).Claims; len(got) != 0 {
		t.Errorf("a 'never landed' premise is not an implementation claim — the mirror readers must "+
			"agree about which class a sentence belongs to; got %v", got)
	}
	if got := taskreport.ImplClaimsIn(implClaim, IntegrationBranchName).Claims; len(got) != 1 {
		t.Fatalf("'master already implements X' must be the OTHER detector's claim; got %v", got)
	}
	if got := taskreport.UnlandedClaimsIn(implClaim, IntegrationBranchName).Claims; len(got) != 0 {
		t.Errorf("and it must not be read as a stale premise; got %v", got)
	}
	if got := taskreport.UnlandedClaimsIn(truthful, IntegrationBranchName).Claims; len(got) != 0 {
		t.Errorf("a sentence carrying a landed marker must NOT be read as a stale premise; got %v", got)
	}
}
