package tools

// Regression tests for the 2026-10-12 residue (task 01a130ae).
//
// MEASURED: four one-shot detached worktrees carrying uncommitted mutation
// material sat in /home/user/workspace and NO arm reached them, while the sweep
// said `keep [live-session] … — its own session's ground` about one of them — a
// registration no row names, in a sentence claiming it was the ground of an
// UNRELATED live task (that task's ground was a different directory).
//
// Two properties, each with a positive and a negative pole so the fix cannot
// degenerate into a blanket refusal, and the whole file is written against
// PRE-EXISTING constants on purpose: it compiles unchanged on the pre-fix tree,
// which is what makes the same fixture produce a verbatim RED there and a GREEN
// here (`worktreeRuleOrphan`, the escalation's own rule, is asserted by
// TestWorktreeSweepFindingRootGate instead — it does not exist before the fix, so
// a file naming it could only report a compile error, not a behaviour).
//
//	LADDER ORDER  an unrelated live session does not decide an unowned
//	              registration: it still reaches the dirty guard (which escalates
//	              an aged orphan, reports a young one) and the plain reclaim.
//	OWNERSHIP     the arm's own evidence — a standing row whose branch IS this
//	              registration's branch — still protects it, and a row whose
//	              deliverable names this directory still protects it.
//
// The fixture is the measured shape: the row's standing run is this test process
// itself, standing in the row's OWN session container (/proc/<pid>/cwd is the one
// cross-session channel readable under kernel.yama.ptrace_scope=1), and the
// registration under judgement is NOT that row's ground.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// liveSessionFixtureRowID is the live row's task id — deliberately not the id
	// of the registration under judgement (a one-shot detached arm has no id).
	liveSessionFixtureRowID = "T-standing-elsewhere"
	// liveSessionFixtureBranch is that row's own branch. The registration that
	// carries it is the concurrent-session shape (git forbids one branch in two
	// worktrees); the detached registration below carries NO branch.
	liveSessionFixtureBranch = "task/" + liveSessionFixtureRowID
	// liveSessionClaimedGroundPhrase is the FALSE sentence the pre-fix arm
	// produced for an unrelated registration (the residue's measured wording).
	// No verdict may say it about a registration no row names.
	liveSessionClaimedGroundPhrase = "its own session's ground"
	// residueFixtureHead is one of the four residue worktrees' HEAD commit
	// (measured: `git worktree list --porcelain` prints da3f952b0391… for both
	// gopi-01a12bfd-verify2 and gopi-bfd-repro). The naming pole uses a real hash
	// so the assertion reads against the spelling an operator actually meets.
	residueFixtureHead = "da3f952b039156c1ea42ab06e315a4e80e624255"
	// residueDetachedName is the phrase the escalation must NOT use: it was true
	// of every detached registration, which is why it named none of them.
	residueDetachedName = "branch " + worktreePorcelainDetachedToken
)

// liveSessionFixtureReg builds a registration inside the fixture root. head
// mirrors what `git worktree list --porcelain` prints for a detached
// registration (the package's own sweepFixtureReg predates the Head field and
// is left as it is).
func liveSessionFixtureReg(root, name, branch, head string) worktreeRegistration {
	return worktreeRegistration{
		Repo: "/fixture/repo", Path: filepath.Join(root, name), Branch: branch, Head: head,
		Detached: branch == "",
	}
}

// liveSessionFixture is the measured shape: a declared root, a live row whose run
// is STANDING (this process, in the row's own session container), and the row's
// deliverable recorded elsewhere. It returns the roots to judge against and the
// live rows (mutable by the caller for the ownership poles).
func liveSessionFixture(t *testing.T) (string, []tasksRowForSweep, []worktreeroots.Root) {
	t.Helper()
	root := sweepFixtureRoot(t)
	live := []tasksRowForSweep{{
		TaskID:   liveSessionFixtureRowID,
		Status:   taskStatusInProgress,
		Worktree: filepath.Join(root, "gopi-standing-ground"),
		Branch:   liveSessionFixtureBranch,
	}}
	sessionVetoChdir(t, sessionVetoSeedSessionDir(t, liveSessionFixtureRowID))
	return root, live, []worktreeroots.Root{{
		Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey,
	}}
}

// TestUnownedRegistrationIsNotDecidedByAnUnrelatedLiveSession is the ladder-order
// half: the SAME standing run and the SAME fixture; only the registration's own
// facts (its branch, its dirt, its age) change — and the verdict must answer
// THOSE, never the stranger's liveness.
func TestUnownedRegistrationIsNotDecidedByAnUnrelatedLiveSession(t *testing.T) {
	root, live, roots := liveSessionFixture(t)
	opts := sweepOptionsNow(true)
	aged := opts.Now.Add(-72 * time.Hour)
	young := opts.Now.Add(-1 * time.Hour)
	dirt := " M cmd/agent/merge_every_test.go"

	cases := []struct {
		name   string
		branch string // the registration's branch ("" = the measured detached arm)
		// head is the registration's HEAD commit — set for the naming pole, so
		// the escalation's sentence can be read against a real hash.
		head     string
		ho       worktreeSweepHostObservations
		wantDisp worktreeDisposition
		// wantRule is the rule the verdict MUST name; wantNotRule is the rule it
		// MUST NOT name. The escalation's own rule ("orphan-unowned") is a NEW
		// constant — naming it here would make this file fail to compile on the
		// pre-fix tree, and a compile error is not the RED this file is for. It is
		// asserted by TestWorktreeSweepFindingRootGate instead; here the escalation
		// poles pin the behaviour (an unrelated run no longer decides) by refusing
		// the two rules that decided it.
		wantRule    worktreeSweepRule
		wantNotRule worktreeSweepRule
		wantInWhy   string
	}{
		{
			// THE MEASURED RESIDUE: detached (so no row can name it), dirty, past
			// the age floor. It must be ESCALATED to a human; pre-fix it was kept
			// under live-session by an unrelated row.
			name:     "detached + dirty + unowned + aged ⇒ orphan escalation",
			ho:       worktreeSweepHostObservations{DirtyReason: dirt, CreatedAt: aged},
			wantDisp: worktreeEscalateOrphan, wantNotRule: worktreeRuleSessionLive,
			wantInWhy: worktreeOrphanEscalationMarker,
		},
		{
			// THE NAMING POLE (task 01a130ae, defect (a)). The measured sentence
			// said "(branch detached, age 21h)" — true of every detached
			// registration, so it named none of them. The escalation must name the
			// commit its HEAD points at: the one handle an operator can act on.
			// The pre-fix tree answers with the alias constant
			// (worktreeSweepDetachedLabel, since deleted) and fails here without
			// needing a new one to compile.
			name:     "detached + dirty + unowned + aged + readable HEAD ⇒ the commit is named",
			head:     residueFixtureHead,
			ho:       worktreeSweepHostObservations{DirtyReason: dirt, CreatedAt: aged},
			wantDisp: worktreeEscalateOrphan, wantNotRule: worktreeRuleSessionLive,
			wantInWhy: "detached HEAD at commit " + branchDeleteShortHash(residueFixtureHead),
		},
		{
			// Same registration inside the age floor: the dirty guard REPORTS it
			// (Rule 15 — say what you can see) instead of a keep that says nothing
			// about the registration itself.
			name:     "detached + dirty + unowned + young ⇒ dirty report",
			ho:       worktreeSweepHostObservations{DirtyReason: dirt, CreatedAt: young},
			wantDisp: worktreeReport, wantRule: worktreeRuleDirty,
			wantInWhy: "uncommitted work",
		},
		{
			// Clean, unowned and aged: the ordinary reclaim is not blocked by a
			// stranger's run either — the fix moves the arm, it adds no veto.
			name:     "detached + clean + unowned + aged ⇒ plain reclaim",
			ho:       worktreeSweepHostObservations{CreatedAt: aged},
			wantDisp: worktreeReclaim, wantRule: worktreeRuleUnclaimed,
			wantInWhy: "no task row owns it",
		},
		{
			// THE ORDERING POLE, and the one that pins the fix: the registration
			// CARRIES the live row's branch (so the concurrent-session arm would
			// have kept it, pre-fix), the row's deliverable names another
			// directory, and the registration is DIRTY and aged. The dirty guard
			// must be asked FIRST — an uncommitted tree is never destroyed by a
			// later arm, and the escalation is how the operator hears about it.
			name:     "tied + dirty + unowned + aged ⇒ the dirty guard decides",
			branch:   liveSessionFixtureBranch,
			ho:       worktreeSweepHostObservations{DirtyReason: dirt, CreatedAt: aged},
			wantDisp: worktreeEscalateOrphan, wantNotRule: worktreeRuleSessionLive,
			wantInWhy: worktreeOrphanEscalationMarker,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := liveSessionFixtureReg(root, "gopi-orphan-nilfix", tc.branch, tc.head)
			f := worktreeSweepJudge(reg, live, scratch.HolderScan{}, roots, opts, tc.ho)
			if (tc.wantRule != "" && f.Rule != string(tc.wantRule)) || f.Disposition != string(tc.wantDisp) {
				t.Fatalf("rule=%q disposition=%q (%s), want %q/%q — an unrelated live session decided this registration",
					f.Rule, f.Disposition, f.Reason, tc.wantRule, tc.wantDisp)
			}
			if tc.wantNotRule != "" && f.Rule == string(tc.wantNotRule) {
				t.Fatalf("rule=%q (%s) — the arm that answers a question about SOMEBODY ELSE decided this registration", f.Rule, f.Reason)
			}
			if !strings.Contains(f.Reason, tc.wantInWhy) {
				t.Errorf("the verdict does not say what decided it (%q missing): %s", tc.wantInWhy, f.Reason)
			}
			if strings.Contains(f.Reason, liveSessionClaimedGroundPhrase) {
				t.Errorf("the reason claims the registration is the live row's own ground: %s", f.Reason)
			}
			if strings.Contains(f.Reason, residueDetachedName) {
				t.Errorf("the reason names the registration with a phrase true of EVERY detached one (%q), so it names none of them: %s",
					residueDetachedName, f.Reason)
			}
		})
	}
}

// TestLiveSessionArmStillProtectsWhatItsOwnEvidenceNames is the ownership half:
// the same fixture, and the two shapes the arm MUST still keep. Without these
// poles, "move the arm" could silently mean "delete the protection" and reopen
// the 2026-09-17 incident (a live session's ground removed mid-run).
func TestLiveSessionArmStillProtectsWhatItsOwnEvidenceNames(t *testing.T) {
	root, live, roots := liveSessionFixture(t)
	opts := sweepOptionsNow(true)
	aged := opts.Now.Add(-72 * time.Hour)
	dirt := " M cmd/agent/merge_every_test.go"

	// POLE 1 — the deliverable tie: the row records THIS directory as its ground.
	t.Run("the row's deliverable names this directory ⇒ kept", func(t *testing.T) {
		reg := liveSessionFixtureReg(root, "gopi-owned", "", "")
		rows := []tasksRowForSweep{live[0]}
		rows[0].Worktree = reg.Path
		rows[0].Branch = ""
		f := worktreeSweepJudge(reg, rows, scratch.HolderScan{}, roots, opts,
			worktreeSweepHostObservations{DirtyReason: dirt, CreatedAt: aged})
		if f.Disposition != string(worktreeKeep) || f.Rule != string(worktreeRuleLedgerOwnership) {
			t.Fatalf("rule=%q disposition=%q (%s), want keep/%q — a row's OWN recorded ground must stay kept",
				f.Rule, f.Disposition, f.Reason, worktreeRuleLedgerOwnership)
		}
		if !strings.Contains(f.Reason, liveSessionFixtureRowID) {
			t.Errorf("the refusal does not name the row that owns it: %s", f.Reason)
		}
	})

	// POLE 2 — the arm's OWN evidence: the row's branch IS this registration's
	// branch while the row's deliverable names another directory (the incident's
	// concurrent-session shape). Clean here on purpose: a dirty one is escalated
	// by the guard that runs first (the ordering pole above), and the escalation
	// never removes anything — so what this pole pins is the protection that
	// matters, a removal being one rung away.
	t.Run("the row's branch IS this registration's branch ⇒ kept under live-session", func(t *testing.T) {
		reg := liveSessionFixtureReg(root, "gopi-second-session", liveSessionFixtureBranch, "")
		f := worktreeSweepJudge(reg, live, scratch.HolderScan{}, roots, opts,
			worktreeSweepHostObservations{CreatedAt: aged})
		if f.Disposition != string(worktreeKeep) || f.Rule != string(worktreeRuleSessionLive) {
			t.Fatalf("rule=%q disposition=%q (%s), want keep/%q — the concurrent-session guard must still hold",
				f.Rule, f.Disposition, f.Reason, worktreeRuleSessionLive)
		}
		if !strings.Contains(f.Reason, liveSessionFixtureRowID) {
			t.Errorf("the refusal does not name the standing row: %s", f.Reason)
		}
		if !strings.Contains(f.Reason, reg.Branch) {
			t.Errorf("the refusal does not name the branch that ties the run here: %s", f.Reason)
		}
	})
}
