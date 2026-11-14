package tools

// The FINDING-ROOT GATE (epic fix/worktree-sweep-finding-root-stamp).
//
// worktreeSweepJudge is a ladder: every rung returns its own verdict, and the
// plan it produces is the sweeper's deliverable. Two WIP commits left rungs that
// returned a BARE finding (skipping worktreeSweepTagFinding), so those findings
// reported neither the arm that decided them nor the root they were judged
// against — a plan an operator cannot act on, and a hole a future `return f`
// reopens silently.
//
// This gate walks EVERY arm a finding can leave the ladder by and asserts what
// each one must carry:
//
//	pre-root arms  — decided before any root is resolvable (the main checkout, a
//	                 bare or dead registration, a git lock): they answer a
//	                 question about git or the host, NOT about policy, so they
//	                 report the honest "no-root" label. Fabricating a root for
//	                 them would be a false finding (Rule 15).
//	post-root arms — every other arm: the finding must name the root it was
//	                 judged against WITH ITS PATH AND SOURCE (a bare role is not
//	                 actionable — that was the defect), and it must never claim
//	                 "no-root" when a root was in fact resolved.
//
// It also reads the judge's own source: a raw `return f` is refused, and the
// predicate that refuses it is proven red-capable on a synthetic body — so
// reintroducing the hole fails HERE, not in production.

import (
	"fmt"
	"github.com/nickhuang99/gopi/internal/scratch"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// findingRootGateCase is one rung of the ladder: the fixture that reaches it,
// the arm and disposition it must produce, and whether a root is resolvable by
// the time it is decided.
type findingRootGateCase struct {
	name string
	// arm is the policy arm the finding MUST name (never a literal inline — it
	// is the constant the ladder itself uses, Rule 13).
	arm worktreeSweepRule
	// want is the disposition the arm must produce.
	want worktreeDisposition
	// preRoot marks an arm that is decided BEFORE any root is resolved, so its
	// finding legitimately carries no root.
	preRoot bool
	// reg is the registration under judgement; the fixture callbacks below are
	// applied to it so the path can be derived from the fixture root.
	reg   worktreeRegistration
	rows  []tasksRowForSweep
	hold  scratch.HolderScan
	ho    func(now time.Time) worktreeSweepHostObservations
	roots func(root string) []worktreeroots.Root
	// reasonContains pins the positive half of Rule 15 for the arms whose
	// message is the arm's own deliverable.
	reasonContains string
	// setup runs inside the subtest BEFORE the judge is called, for the arms
	// whose fixture is not a plain struct: the session arm needs a live process
	// standing in a row's session container, which no field can express (the
	// process table is the host's, not the registration's). Nil = no setup.
	setup func(t *testing.T)
}

// TestWorktreeSweepFindingRootGate is the gate: it walks every arm of the judge
// ladder and asserts the root contract above.
func TestWorktreeSweepFindingRootGate(t *testing.T) {
	root := sweepFixtureRoot(t)
	declared := func(r string) []worktreeroots.Root {
		return []worktreeroots.Root{{Path: r, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	}
	// The out-of-policy arm needs a path OUTSIDE every root it is handed.
	outsider := filepath.Join(t.TempDir(), "gopi-verify-outside")
	// memoryBaseAvailable decides what the out-of-policy arm CAN report: on a
	// host whose scratch home is disk-backed there is no memory base to name,
	// and the honest answer is the no-root label plus a reason that says so.
	_, memoryBaseAvailable := memoryBaseRoot()

	aged := func(age time.Duration) func(time.Time) worktreeSweepHostObservations {
		return func(now time.Time) worktreeSweepHostObservations {
			return worktreeSweepHostObservations{CreatedAt: now.Add(-age)}
		}
	}
	dirty := func(age time.Duration) func(time.Time) worktreeSweepHostObservations {
		return func(now time.Time) worktreeSweepHostObservations {
			ho := worktreeSweepHostObservations{DirtyReason: "?? scratch.txt"}
			if age > 0 {
				ho.CreatedAt = now.Add(-age)
			}
			return ho
		}
	}

	cases := []findingRootGateCase{
		{
			name: "main checkout ⇒ no root (no policy question asked)",
			arm:  worktreeRuleMainCheckout, want: worktreeKeep, preRoot: true,
			reg: worktreeRegistration{Repo: "/fixture/repo", Main: true, Path: filepath.Join(root, "gopi-main")},
			ho:  aged(time72h()), roots: declared, reasonContains: "main checkout",
		},
		{
			name: "bare registration ⇒ no root",
			arm:  worktreeRuleBare, want: worktreeKeep, preRoot: true,
			reg: worktreeRegistration{Repo: "/fixture/repo", Bare: true, Path: filepath.Join(root, "gopi-bare")},
			ho:  aged(time72h()), roots: declared, reasonContains: "bare",
		},
		{
			name: "dead registration ⇒ no root",
			arm:  worktreeRuleDeadRegistration, want: worktreeReap, preRoot: true,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-dead")},
			ho: func(time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{DirGone: true}
			},
			roots: declared, reasonContains: "dead",
		},
		{
			name: "dead registration with a recyclable branch ⇒ no root",
			arm:  worktreeRuleDeadRegistration, want: worktreeReapBranch, preRoot: true,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-deadbr"), Branch: "feat/deadbr"},
			ho: func(time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{DirGone: true}
			},
			roots: declared, reasonContains: "dead",
		},
		{
			name: "dead registration held by a live process ⇒ no root",
			arm:  worktreeRuleLiveHolder, want: worktreeKeep, preRoot: true,
			reg:            worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-deadheld")},
			hold:           scratch.HolderScan{Paths: []string{filepath.Join(root, "gopi-deadheld", "keep")}},
			ho:             func(time.Time) worktreeSweepHostObservations { return worktreeSweepHostObservations{DirGone: true} },
			roots:          declared,
			reasonContains: "live process",
		},
		{
			name: "dead registration with an undatable lock ⇒ no root (cannot tell is never authorise)",
			arm:  worktreeRuleUnmeasurable, want: worktreeKeep, preRoot: true,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-deadlock"), Locked: true},
			ho: func(time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{DirGone: true}
			},
			roots: declared, reasonContains: "could not be dated",
		},
		{
			name: "locked by git ⇒ no root",
			arm:  worktreeRuleGitLock, want: worktreeKeep, preRoot: true,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-locked"), Locked: true},
			ho:  aged(time72h()), roots: declared, reasonContains: "locked by git",
		},
		{
			name: "outside every declared root ⇒ the consulted root is named",
			arm:  worktreeRuleRootScope, want: worktreeReport, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: outsider, Branch: "fix/verify-outside"},
			ho:  aged(time72h()), roots: declared, reasonContains: "consulted:",
		},
		{
			name: "held inside policy ⇒ root named",
			arm:  worktreeRuleLiveHolder, want: worktreeKeep, preRoot: false,
			reg:            worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-held"), Branch: "feat/held"},
			hold:           scratch.HolderScan{Paths: []string{filepath.Join(root, "gopi-held", "sub")}},
			ho:             aged(time72h()),
			roots:          declared,
			reasonContains: "live process",
		},
		{
			name: "owned by a live task's worktree ⇒ root named",
			arm:  worktreeRuleLedgerOwnership, want: worktreeKeep, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-owned"), Branch: "feat/owned"},
			rows: []tasksRowForSweep{{
				TaskID: "T-live", Status: taskStatusInProgress,
				Worktree: filepath.Join(root, "gopi-owned"), Branch: "feat/owned",
			}},
			ho: aged(time72h()), roots: declared, reasonContains: "T-live",
		},
		{
			name: "owned by a live task's branch ⇒ root named",
			arm:  worktreeRuleLedgerOwnership, want: worktreeKeep, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-ownedbr"), Branch: "feat/ownedbr"},
			rows: []tasksRowForSweep{{
				TaskID: "T-branch", Status: taskStatusClaimed, Branch: "feat/ownedbr",
			}},
			ho: aged(time72h()), roots: declared, reasonContains: "T-branch",
		},
		{
			name: "dirty, unowned and aged ⇒ root named (escalation, its own arm)",
			arm:  worktreeRuleOrphan, want: worktreeEscalateOrphan, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-orphan"), Branch: "fix/orphan"},
			ho:  dirty(time72h()), roots: declared, reasonContains: worktreeOrphanEscalationMarker,
		},
		{
			name: "dirty, unowned and young ⇒ root named (plain report)",
			arm:  worktreeRuleDirty, want: worktreeReport, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-dirtyyoung"), Branch: "fix/dirty-young"},
			ho:  dirty(time.Hour), roots: declared, reasonContains: "uncommitted work",
		},
		{
			name: "dirty and owned ⇒ root named (plain report)",
			arm:  worktreeRuleDirty, want: worktreeReport, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-dirtyowned"), Branch: "fix/dirty-owned"},
			rows: []tasksRowForSweep{{
				TaskID: "T-owner", Status: taskStatusCompleted,
				Worktree: filepath.Join(root, "gopi-dirtyowned"), Branch: "fix/dirty-owned",
			}},
			ho: dirty(time72h()), roots: declared, reasonContains: "uncommitted work",
		},
		{
			name: "dirty with an unmeasurable age ⇒ root named (plain report)",
			arm:  worktreeRuleDirty, want: worktreeReport, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-dirtyundated"), Branch: "fix/dirty-undated"},
			ho:  dirty(0), roots: declared, reasonContains: "uncommitted work",
		},
		{
			name: "unmeasurable registration age ⇒ root named (kept)",
			arm:  worktreeRuleUnmeasurable, want: worktreeKeep, preRoot: false,
			reg:   worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-undated"), Branch: "fix/undated"},
			ho:    func(time.Time) worktreeSweepHostObservations { return worktreeSweepHostObservations{} },
			roots: declared, reasonContains: "could not be measured",
		},
		{
			name: "younger than the threshold ⇒ root named (kept)",
			arm:  worktreeRuleTooYoung, want: worktreeKeep, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-young"), Branch: "fix/young"},
			ho:  aged(time.Hour), roots: declared, reasonContains: "younger than the threshold",
		},
		{
			name: "terminal owner ⇒ root named (reclaim)",
			arm:  worktreeRuleOwnerEnded, want: worktreeReclaim, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-ended"), Branch: "feat/ended"},
			rows: []tasksRowForSweep{{
				TaskID: "T-ended", Status: taskStatusCompleted,
				Worktree: filepath.Join(root, "gopi-ended"), Branch: "feat/ended",
			}},
			ho: aged(time72h()), roots: declared, reasonContains: "T-ended",
		},
		{
			name: "owner's run ended while its status stayed live ⇒ root named (reclaim)",
			arm:  worktreeRuleOwnerEnded, want: worktreeReclaim, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-runended"), Branch: "feat/run-ended"},
			rows: []tasksRowForSweep{{
				TaskID: "T-run", Status: taskStatusInProgress,
				Worktree: filepath.Join(root, "gopi-runended"), Branch: "feat/run-ended",
				EndedAt: float64(time.Now().Add(-2 * worktreeSweepEndedGrace).Unix()),
			}},
			ho: aged(48 * time.Hour), roots: declared, reasonContains: "T-run",
		},
		{
			// The CONCURRENT-SESSION arm (the 2026-09-17 incident's shape): no
			// row's deliverable names this registration, but a live row has a
			// run standing in its own session container — so the registration,
			// and with it the branch that run is building, is kept under this
			// arm's own rule. The fixture spawns a REAL process (a mock daemon
			// that outlives the test and is reaped by its own reaper, so the
			// liveness assertion can never pass vacuously) and chdirs the test
			// process into the row's session directory: /proc/<pid>/cwd is the
			// one foreign channel readable beside the pid triple.
			name: "a live row's standing session ⇒ root named (concurrent session)",
			arm:  worktreeRuleSessionLive, want: worktreeKeep, preRoot: false,
			reg:            worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-secondsession"), Branch: "fix/second-session"},
			rows:           []tasksRowForSweep{{TaskID: "T-two-sessions", Status: taskStatusInProgress, Worktree: filepath.Join(root, "gopi-elsewhere"), Branch: "fix/second-session"}},
			ho:             aged(time72h()),
			roots:          declared,
			reasonContains: "T-two-sessions",
			setup: func(t *testing.T) {
				t.Helper()
				// The mock daemon is the LIVE half: started (and reaped by its
				// own reaper) it is a real process the /proc scan can see, so
				// the fixture cannot drift into asserting on a dead pid.
				mock := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
				t.Cleanup(mock.Reap)
				sessionVetoChdir(t, sessionVetoSeedSessionDir(t, "T-two-sessions"))
			},
		},
		{
			name: "clean and unowned ⇒ root named (plain reclaim)",
			arm:  worktreeRuleUnclaimed, want: worktreeReclaim, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-unclaimed"), Branch: "fix/unclaimed"},
			ho:  aged(time72h()), roots: declared, reasonContains: "no task row owns it",
		},
		{
			// The SELF-CHECKOUT arm (epic 01a0c1eb, 2026-09-21): the
			// registration is a clean, unowned, aged, in-policy worktree of the
			// repository THIS PROCESS stands in. Every registration-side rung
			// passes it (the case above proves that — same fixture shape,
			// reclaim), so this one would be freed by the actor-blind ladder and
			// is kept only by the fact that the actor IS the situation it would
			// delete. preRoot is true because the rung sits where the other
			// actor/registration-identity rungs sit (main checkout, bare, dead
			// registration): before any root question, since no root verdict can
			// make standing in the target's own repo safe.
			// DELIBERATELY AFTER the root-scope rung, so preRoot is false and the
			// finding carries the root it was judged against: a registration of
			// this process's own repo that sits OUTSIDE every declared root must
			// still reach the operator as the actionable root-scope report
			// ("add the root"), not be absorbed here into a permanent keep. The
			// rung therefore protects POLICY-COMPLIANT own-repo registrations —
			// exactly the ones the actor-blind ladder would free.
			name: "the process's own checkout ⇒ root named (self-checkout refusal)",
			arm:  worktreeRuleSelfCheckout, want: worktreeKeep, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-selfcheckout"), Branch: "fix/self-checkout"},
			ho: func(now time.Time) worktreeSweepHostObservations {
				ho := aged(time72h())(now)
				ho.SelfRepoGuestCheckout = true
				return ho
			},
			roots: declared, reasonContains: worktreeSweepSelfCheckoutPrefix,
		},
		{
			// The chat axis's reclaiming arm. An INERT chat ground — no ledger
			// row owns it (a chat is not a row: chat_ground.go), its tree is
			// clean, its branch is spent and its session's index row records the
			// close — is reclaimed by its OWN rule, and the proof of endedness
			// is what carries it: the registration here is one HOUR old, far
			// below the 24 h age floor the ordinary ladder would stop at
			// (epic chat-ground-reclaim).
			name: "an inert chat ground ⇒ root named (reclaim, under the age floor)",
			arm:  worktreeRuleChatInert, want: worktreeReclaim, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c"), Branch: "chat/3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c"},
			ho: func(now time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{CreatedAt: now.Add(-time.Hour)}
			},
			setup: func(t *testing.T) { chatSweepSeedClosedRow(t, "3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c") },
			roots: declared, reasonContains: "chat ground 3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c is inert",
		},
		{
			// The chat axis's refusing half: a chat ground whose session is
			// still going is kept, and the finding names that fact rather than
			// the age floor — an operator must be able to tell a live
			// conversation's ground from residue. The index is deliberately
			// UNREADABLE here too (no setup), so the same case pins fail-closed:
			// "I could not ask" is a keep, never endedness.
			name: "an open chat session's ground ⇒ root named (kept)",
			arm:  worktreeRuleChatSessionActive, want: worktreeKeep, preRoot: false,
			reg: worktreeRegistration{Repo: "/fixture/repo", Path: filepath.Join(root, "gopi-4d3c2b1a0f9e8d7c6b5a4938271605f4"), Branch: "chat/4d3c2b1a0f9e8d7c6b5a4938271605f4"},
			ho: func(now time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{CreatedAt: now.Add(-72 * time.Hour)}
			},
			setup: func(t *testing.T) { chatSweepDenyIndex(t) },
			roots: declared, reasonContains: worktreeSweepChatUnreadableMarker,
		},
	}

	seen := map[worktreeSweepRule]int{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			opts := sweepOptionsNow(true)
			f := worktreeSweepJudge(tc.reg, tc.rows, tc.hold, tc.roots(root), opts, tc.ho(opts.Now))
			seen[tc.arm]++
			if f.Disposition != string(tc.want) {
				t.Fatalf("disposition = %s (%s), want %s — the fixture does not reach the arm under test", f.Disposition, f.Reason, tc.want)
			}
			if f.Rule != string(tc.arm) {
				t.Fatalf("rule = %q, want %q — the finding must name the arm that decided it", f.Rule, tc.arm)
			}
			if tc.reasonContains != "" && !strings.Contains(f.Reason, tc.reasonContains) {
				t.Fatalf("reason does not name %q: %s", tc.reasonContains, f.Reason)
			}
			// The pre-root exception is arm-specific, never a blanket licence:
			// swappable fixtures must not be able to claim it loosely.
			if tc.preRoot && f.Root != worktreeSweepNoRootLabel {
				t.Fatalf("root = %q, want %q for an arm decided before any root is resolved", f.Root, worktreeSweepNoRootLabel)
			}
			if !tc.preRoot {
				if f.Root == worktreeSweepNoRootLabel {
					// The ONE honest no-root case after the root test: the host
					// has no memory base to name, which the reason must state.
					if tc.arm != worktreeRuleRootScope || memoryBaseAvailable {
						t.Fatalf("root = %q for arm %s — a root WAS resolved and the finding must name it", f.Root, f.Rule)
					}
					if !strings.Contains(f.Reason, "no memory-base root is available here") {
						t.Fatalf("the no-root refusal does not say why no root can be named: %s", f.Reason)
					}
					return
				}
				if !findingRootIsActionable(f.Root) {
					t.Fatalf("root = %q for arm %s — a root named without its path and source is not actionable", f.Root, f.Rule)
				}
			}
		})
	}
	assertEveryJudgeArmIsCited(t, seen)
}

// assertEveryJudgeArmIsCited makes the table's own list of arms an ASSERTION:
// adding an arm to the ladder without citing it here (or without declaring it a
// post-judge override) fails, so the gate cannot quietly fall behind the ladder.
func assertEveryJudgeArmIsCited(t *testing.T, seen map[worktreeSweepRule]int) {
	t.Helper()
	// Judged by worktreeSweepJudge itself: every one must be cited by a case.
	judgeArms := []worktreeSweepRule{
		worktreeRuleMainCheckout, worktreeRuleBare, worktreeRuleDeadRegistration,
		worktreeRuleGitLock, worktreeRuleRootScope, worktreeRuleLiveHolder,
		worktreeRuleLedgerOwnership, worktreeRuleSessionLive, worktreeRuleOrphan,
		worktreeRuleDirty,
		worktreeRuleTooYoung, worktreeRuleUnmeasurable, worktreeRuleOwnerEnded,
		worktreeRuleUnclaimed, worktreeRuleSelfCheckout, worktreeRuleChatInert,
		worktreeRuleChatSessionActive,
	}
	for _, arm := range judgeArms {
		if seen[arm] == 0 {
			t.Errorf("arm %q is never cited by a gate case — the ladder can reach it with an unstamped finding", arm)
		}
	}
	// Decided OUTSIDE the judge, by the runner that applies the plan — the root
	// is carried through untouched, which their own tests pin
	// (TestSweepDryRunReapsNothing, TestWorktreeSweepCap*):
	//   worktreeRuleRunCap        — the run's reclamation cap was reached
	//   worktreeRuleNoLedger      — the ledger could not be read (fail closed)
	// One arm is decided AFTER the judge's ladder, by the runner that applies
	// the plan:
	//   worktreeRuleChatCarriesWork   - a chat ground whose session IS over
	//                                   but whose branch is not spent (it
	//                                   carries commits master lacks), so the
	//                                   runner keeps it before the removal
	//                                   (TestSweepKeepsAChatGroundCarryingUnpushedCommits).
	//   worktreeRuleGroundChecklist — the DETERMINISTIC CHECKLIST refused a
	//                               registration the judge had passed
	//                               (reclaim_ground_checklist.go: the ground
	//                               is this process's own checkout, or the
	//                               branch shares no ancestor with the
	//                               integration branch). Cited by
	//                               TestSweepGroundChecklistStampsItsOwnRule.
	if len(judgeArms)+4 != len(allWorktreeSweepRules(t)) {
		t.Errorf("the ladder's arm set changed (%d cited + 4 post-judge != %d defined) — cite the new arm above",
			len(judgeArms), len(allWorktreeSweepRules(t)))
	}
}

// allWorktreeSweepRules counts the arm constants defined in worktree_sweep.go by
// scanning the source, so a NEW arm constant is noticed even if nobody updates a
// list in this test (the count above is the tripwire).
func allWorktreeSweepRules(t *testing.T) []string {
	t.Helper()
	src := readJudgeSource(t)
	var out []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "worktreeRule") || !strings.Contains(trimmed, "worktreeSweepRule = ") {
			continue
		}
		name := strings.Fields(trimmed)[0]
		out = append(out, name)
	}
	if len(out) == 0 {
		t.Fatal("no arm constants found in the judge's source — the scan lost its footing")
	}
	return out
}

// findingRootIsActionable reports whether a rendered root label names the root's
// PATH and SOURCE (the bracketed provenance), not merely its role. A role alone
// was exactly the WIP defect's silent half: "memory-base" tells an operator
// nothing they can act on, while "memory-base /dev/shm [memory base measured
// from the scratch home]" tells them what was consulted and how it was found.
func findingRootIsActionable(label string) bool {
	if label == "" || label == worktreeSweepNoRootLabel {
		return false
	}
	if !strings.Contains(label, " [") || strings.HasSuffix(label, "[]") {
		return false
	}
	return true
}

// TestWorktreeSweepJudgeReturnsAreTagged is the source half of the gate: no rung
// of the ladder may return a finding that skipped worktreeSweepTagFinding. A
// future `return f` fails HERE, before it can reach production.
func TestWorktreeSweepJudgeReturnsAreTagged(t *testing.T) {
	body := judgeFunctionBody(t)
	if raw := rawFindingReturns(body); len(raw) > 0 {
		t.Fatalf("worktreeSweepJudge returns an unstamped finding (%d site(s)) — every return must go through worktreeSweepTagFinding: %v", len(raw), raw)
	}
	// A presence floor: a broken extraction would otherwise pass vacuously.
	if n := strings.Count(body, "worktreeSweepTagFinding("); n < 1 {
		t.Fatalf("the judge's body carries no stamping call at all — the extraction lost its footing (%d bytes)", len(body))
	}
}

// TestRawFindingReturnPredicateIsRedCapable proves the scan above can FAIL: the
// same predicate run over a synthetic judged body carrying a raw `return f` must
// flag it (a gate that cannot go red proves nothing).
func TestRawFindingReturnPredicateIsRedCapable(t *testing.T) {
	clean := "func worktreeSweepJudge() worktreeSweepFinding {\n\treturn worktreeSweepTagFinding(f, worktreeRuleDirty, judgedRoot)\n}"
	if got := rawFindingReturns(clean); len(got) != 0 {
		t.Fatalf("the predicate flagged a correctly stamped body: %v", got)
	}
	dirtyBody := "func worktreeSweepJudge() worktreeSweepFinding {\n\tif true {\n\t\treturn f\n\t}\n\treturn worktreeSweepTagFinding(f, worktreeRuleDirty, judgedRoot)\n}"
	got := rawFindingReturns(dirtyBody)
	if len(got) != 1 {
		t.Fatalf("the predicate must flag exactly the raw return, got %v", got)
	}
}

// rawFindingReturns lists the line numbers of bare `return f` statements in a
// worktreeSweepJudge body — the shape that skips the stamping and reopens the
// defect.
func rawFindingReturns(body string) []int {
	var out []int
	for i, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "return f" {
			out = append(out, i+1)
		}
	}
	return out
}

// judgeFunctionBody extracts worktreeSweepJudge's own body from its source file.
func judgeFunctionBody(t *testing.T) string {
	t.Helper()
	src := readJudgeSource(t)
	const sig = "\nfunc worktreeSweepJudge("
	start := strings.Index(src, sig)
	if start < 0 {
		t.Fatalf("worktreeSweepJudge is not in %s — the gate cannot read its subject", judgeSourcePath())
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatal("the judge's body has no closing brace at column 0 — the extraction lost its footing")
	}
	return rest[:end]
}

// readJudgeSource reads the file the judge lives in.
func readJudgeSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(judgeSourcePath())
	if err != nil {
		t.Fatalf("cannot read the judge's source: %v", err)
	}
	return string(raw)
}

// judgeSourcePath locates worktree_sweep.go next to THIS test file, so the scan
// reads the file in the worktree the test runs in.
func judgeSourcePath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return worktreeSweepSourceName
	}
	return filepath.Join(filepath.Dir(thisFile), worktreeSweepSourceName)
}

// worktreeSweepSourceName is the file the judge lives in — named once, so the
// gate and any future reader agree on the subject (Rule 1b: a meaning-carrying
// string is a constant, never a literal at the call site).
const worktreeSweepSourceName = "worktree_sweep.go"

// TestWorktreeSweepFindingRootGateReportsTheStyleOfTheLabel pins the RENDERING
// the two tests above rely on, so a change to worktreeSweepRootLabel cannot make
// findingRootIsActionable vacuously true.
func TestWorktreeSweepFindingRootGateReportsTheStyleOfTheLabel(t *testing.T) {
	// The path under test IS the memory base's own path — the fixture's point
	// is the label's rendering of a memory-base root, so it names the path
	// through the one definition instead of re-typing it (Rules 1b/13/14).
	memoryBasePath := worktreeroots.ShmDir
	withRole := worktreeSweepRootLabel(worktreeroots.Root{
		Path: memoryBasePath, Role: worktreeroots.RoleMemoryBase, Source: worktreeroots.SourceMemoryBaseMeasured,
	})
	wantRole := fmt.Sprintf("%s %s [%s]", worktreeroots.RoleMemoryBase, memoryBasePath, worktreeroots.SourceMemoryBaseMeasured)
	if withRole != wantRole {
		t.Fatalf("role label = %q, want %q", withRole, wantRole)
	}
	if !findingRootIsActionable(withRole) {
		t.Fatalf("a role+path+source label must count as actionable: %q", withRole)
	}
	// The roleless root an out-of-policy refusal reports: path and source, NO
	// role (a role authorises; the refusal authorises nothing).
	roleless := worktreeSweepRootLabel(worktreeroots.Root{Path: memoryBasePath, Source: worktreeroots.SourceMemoryBaseMeasured})
	if strings.Contains(roleless, string(worktreeroots.RoleMemoryBase)) || !findingRootIsActionable(roleless) {
		t.Fatalf("roleless label = %q, want path+source only and still actionable", roleless)
	}
	if findingRootIsActionable(worktreeSweepNoRootLabel) || findingRootIsActionable("") {
		t.Fatalf("%q and \"\" must never count as an actionable root", worktreeSweepNoRootLabel)
	}
}
