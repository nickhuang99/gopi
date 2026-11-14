// Code generated from 58b5618^ (the revision before the finish-cleanup
// retirement, slice 3/4) by a one-off extraction — see the task report.
//
// WHY THESE LIVE HERE (Rule 43, the reference-distribution criterion): every
// declaration below used to sit inside the retired finish-cleanup mechanism, but
// LIVE tests in this package still call it by name. That reference distribution
// is what keeps it — a shared tool moves out BEFORE its mechanism is deleted.
// They are carried here verbatim, from the parent revision.
//
// THERE IS NO PRODUCTION CALLER OF ANYTHING IN THIS FILE AND THERE CANNOT BE ONE
// (measured 2026-10-31, task 01a18f76): the mechanism was retired whole in
// 58b5618b, so every production name below the type block is a COMMENT recording
// that retirement. `grep -rnw <symbol> --include=*.go . | grep -v _test.go`
// filtered for non-comment lines returns nothing for every one of them.
//
// SO NOTHING HERE IS EXPORTED FOR A CONSUMER (Rule 43's export half). Four
// symbols — the report renderer, the removed-worktree predicate, the settled
// predicate and the chain entry point — carried an EXPORTED spelling because the
// command package called them (cmd/agent/cli_tasks.go, cli_resume.go,
// cli_worker_resume.go, internal/agent/taskregister.go). Those call sites are
// GONE with the mechanism, so the spellings are unexported: a `_test.go`-defined
// exported symbol claims a consumer that does not exist, which is the exact
// signal that hid this file's origin (0e9fdbca "restored the fixtures" by giving
// 43 undefined symbols a same-named test definition, so "the production function
// is gone" stopped being visible in `go build`). An exported spelling inside a
// `_test.go` file must have a reader OUTSIDE that file's package, or it is a lie;
// internal/standards' exported-test-symbol gate enforces that now.
//
// A symbol that no longer has a live reader does NOT belong here: this file is a
// restore of what the live tier still names, never a home for dead code.

package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// finishCleanupReportLine renders a chain report as one sweep log line.
//
// UNEXPORTED 2026-10-31 (Rule 43, task 01a18f76): it was exported "so the CLI
// sweep shares the agent hook's renderer shape", and BOTH of those callers died
// with the mechanism in 58b5618b (cmd/agent/cli_tasks.go, internal/agent/
// taskregister.go — measured: 0 non-comment production references). Its readers
// are this package's own tests and the shared chain harness below.
func finishCleanupReportLine(rep *FinishCleanupReport) string {
	if rep.Skipped {
		return rep.TaskID + finishCleanupSkipSpacer + FinishCleanupSkipVerdict + rep.SkipReason + FinishCleanupSkipStepTail(rep)
	}
	ok, failed := []string{}, []string{}
	for _, s := range rep.Steps {
		if s.OK {
			ok = append(ok, s.Step)
		} else {
			failed = append(failed, s.Step+": "+s.Detail)
		}
	}
	line := rep.TaskID + " branch=" + rep.Branch + " done=[" + strings.Join(ok, ",") + "]"
	if len(failed) > 0 {
		line += " failed=[" + strings.Join(failed, "; ") + "]"
	}
	// WHICH DIRECTORY the worktree step covered (Rule 15). `done=[…,worktree,…]`
	// alone is a claim about A directory, and the close-out owns two of them: the
	// row's recorded deliverable ground, and — the measured case (2026-09-26,
	// TestManualRunRecyclesItsWorktree) — the worktree the run was LAUNCHED in,
	// which no ledger row names and which the release pass frees in a SEPARATE
	// line afterwards. Printed without the path, the step reads as "the worktree
	// is handled" while the directory an operator will still find is a different
	// one entirely; printed WITH it, the done step and the residue can be told
	// apart on one line each. Only when the step SUCCEEDED: a refused or absent
	// step already names its own target in `failed=[…]` (its Detail), and adding
	// a "freed" path beside a refusal would claim exactly what was refused.
	if finishCleanupStepSucceeded(rep, finishCleanupStepWorktree) && strings.TrimSpace(rep.Worktree) != "" {
		line += " " + finishCleanupFreedWorktreeLabel + strings.TrimSpace(rep.Worktree)
	}
	return line
}

// finishCleanupReportRemovedWorktree reports whether this (EXECUTED) report
// actually removed a worktree directory: the worktree step succeeded on disk
// rather than being predicted (finishCleanupPlanMarker) or refused (a step
// carrying a git refusal has OK=false and a non-empty detail). Callers count
// reclamations with it so "the chain ran" is never confused with "the chain
// freed the directory" — the distinction the operator's stale-worktree
// pile-up makes load-bearing.
//
// UNEXPORTED 2026-10-31 (Rule 43, task 01a18f76): its production reader was
// cmd/agent/cli_worker_resume.go, whose tail no longer asks the chain at all
// (58b5618b retired it; the remaining mentions there are comments recording the
// retirement). Only this package's tests read it now.
func finishCleanupReportRemovedWorktree(rep *FinishCleanupReport) bool {
	for _, s := range rep.Steps {
		if s.Step != finishCleanupStepWorktree || !s.OK {
			continue
		}
		return !strings.HasPrefix(strings.TrimSpace(s.Detail), finishCleanupPlanMarker)
	}
	return false
}

// finishCleanupReportSettled reports whether the run ended with NOTHING LEFT
// TO DO — the row's close-out is already complete. Two shapes, one verdict:
//
//	· the chain ran but found no worktree registered for the branch (the
//	  directory was recycled by an earlier pass, so the only remaining steps
//	  are the branch ones); and
//	· the row recorded a worktree whose directory is GONE **and whose
//	  delivery the report itself evidences** (the push step) — git's chdir
//	  refusal is the evidence of ABSENCE, never of delivery, and the
//	  removal reason's own "^delivery already attempted" wording is derived
//	  from that absence. A gone directory whose branch is still here is a row
//	  whose delivery half never ran: it is NOT settled (measured 2026-10-12:
//	  `settled=1 would-run=0` on a row absent from every peer ref).
//
// Why a separate disposition: the one-shot verb counted every skip as a
// "refusal", so a host whose directories were long gone printed
// `refused=10 would-run=0` and read as a STUCK sweep — the false alarm that
// hid the real refusals (a live holder, a dirty bond) among harmless ones.
// A settled row is not a refusal; it is the pass having nothing to do.
//
// UNEXPORTED 2026-10-31 (Rule 43, task 01a18f76): it had NO production caller
// even at the parent revision — only the retired mechanism's own bodies and this
// package's tests read it (measured: `git grep -c -w` at 58b5618^ lists
// task_finish_cleanup.go and *_test.go, nothing else).
func finishCleanupReportSettled(rep *FinishCleanupReport) bool {
	if rep == nil {
		return false
	}
	if rep.Skipped {
		// A GROUND A RECORDED ARM ALREADY FREED IS SETTLED BY CONSTRUCTION (task
		// 01a18a04-04e6-7ecb-9b49-f53236a477c9): the clone channel removed the
		// directory because its push LANDED, and the removal-audit record says so
		// — there is nothing left for this chain to do with it, on any node role
		// and with or without a peer declared. Asked FIRST because every arm below
		// reasons from the ABSENCE of the directory, and this reason is the one
		// case where the absence is the answer rather than a symptom. The
		// predicate is the reason's own marker (Rules 1b/13), so the classification
		// and the prose an operator reads cannot drift apart.
		if strings.Contains(rep.SkipReason, finishCleanupGroundReclaimedMarker) &&
			finishCleanupStepSucceeded(rep, finishCleanupGroundReclaimedStep) {
			return true
		}
		dir := strings.TrimSpace(rep.Worktree)
		if dir == "" {
			// Nothing registered for the branch: the skip IS the chain having
			// nothing to do, and the reason says so.
			return strings.Contains(rep.SkipReason, finishCleanupNoLinkedWorktreeMarker)
		}
		// A recorded worktree git cannot enter because the directory is gone:
		// the reason names it, and the chdir refusal is the EVIDENCE of absence
		// (a reason naming some OTHER directory — a foreign/broken binding —
		// must never read as settled).
		//
		// THE DELIVERY IS ASKED ABOUT, NOT INFERRED. "The directory is gone" is
		// evidence about a DIRECTORY — it says nothing about whether this row's
		// branch ever reached the peer, and the removal reason's own wording
		// ("delivery already attempted") is derived from that absent directory
		// rather than measured. Measured 2026-10-12 (this epic): a row whose
		// worktree directory was deleted by hand, whose branch was in NO peer
		// ref, printed `settled=1 would-run=0` and was therefore never
		// delivered — the settled reading cancelled the ONE act this chain
		// exists for. So the gone-directory arm is answered by the same
		// evidence the rest of the chain derives standing from: a delivered
		// row's report CARRIES the push step
		// (finishCleanupBranchAbsentDeliveryNote / the delivered push), while a
		// row whose branch is still here carries the owed-wording composed by
		// finishCleanupRemovalReasonWithDelivery. No delivery evidence →
		// NOT settled, and the chain's own disposition (refused / would-run)
		// carries the pending delivery instead.
		if strings.Contains(rep.SkipReason, dir) && finishCleanupMissingDirReason(rep.SkipReason) {
			// NO DELIVERY CHANNEL AT ALL is its own settled shape: with no peer
			// declared (or an alias git may not be handed) the hand-off does not
			// exist on this host — the removal reason says so in the composer's
			// own words — so there is nothing left to do here and the row keeps
			// its settled reading (the 2026-09-16 single-node measurement).
			if finishCleanupNoDeliveryChannelReason(rep.SkipReason) {
				return true
			}
			// THE SAME SHAPE ONE STEP EARLIER (measured 2026-10-12): a row
			// whose recorded directory is gone reaches the chain's TERMINAL
			// -STATE PROBE (`git rev-parse --verify HEAD` inside that
			// directory) BEFORE any delivery reasoning, and the probe's own
			// refusal — `worktree unreadable: git rev-parse --verify HEAD:
			// chdir <dir>: no such file or directory` — names the vanished
			// directory but neither the delivery state nor the peer
			// declaration, so it matched NEITHER arm above: the row read
			// `Settled=False WouldRun=False`, i.e. not finished and no act to
			// run — the two fields disagreeing about one row. The peer
			// question is asked (finishCleanupNoDeclaredPeer, the ONE reader
			// of that fact in this package): on a host with NO delivery
			// channel the hand-off does not exist, so there is nothing to owe
			// — settled, exactly as the gone-directory arm above reads it. A
			// host WITH a peer keeps the unset verdict: that row's delivery is
			// owed and the control arm (`Settled=False WouldRun=True`) must
			// keep reading so.
			if finishCleanupNoDeclaredPeer() && finishCleanupUnreadableGoneWorktreeReason(rep.SkipReason, dir) {
				return true
			}
			// THE SAME QUESTION, ASKED BY ROLE (measured 2026-10-13, real binary
			// over a fixture ledger). A SERVER node declares peers — so the arm
			// above does not fire — but its own body has no hand-off to make:
			// finishCleanupServerRecycle PRESERVES the branch as the shared
			// repo's merge input and pushes nothing. Its probe refusal therefore
			// leaves the branch exactly as it was, and the row read
			// `Settled=False WouldRun=False` — the two fields disagreeing about
			// one row, counted as `refused` by every sweep for ever (measured:
			// `candidates=1 would-run=0 settled=0 refused=1` on the server role,
			// peer empty before AND after `--apply`). There is no delivery
			// channel for THIS row on this node either, which is the identical
			// reason the no-peer arm above states — asked through the ONE
			// predicate that decides which arm a row takes
			// (finishCleanupClientArmApplies), so a client host that declares a
			// peer keeps the unset verdict: its branch is still owed.
			if !finishCleanupClientArmApplies() && finishCleanupUnreadableGoneWorktreeReason(rep.SkipReason, dir) {
				return true
			}
			return !strings.Contains(rep.SkipReason, finishCleanupDeliveryOwedSkipMarker) &&
				finishCleanupDeliveryEvidenced(rep)
		}
		// A skip that recorded a delivery is the removal-side refusal this epic
		// introduced (dirt / a gone directory reached AFTER the push): the row
		// is settled as far as the chain can take it — the branch is on the
		// peer, and the only thing left is a directory a human must clean.
		// Reading it as "still to do" would make a delivered row look stuck,
		// which is what this classifier exists to prevent. A skip whose
		// delivery was never attempted (the delivery premise, the no-peer case)
		// stays UNSETTLED: that branch was not handed over.
		//
		// The delivery is read from the report's OWN steps — the same evidence
		// the rest of the chain derives standing from
		// (finishCleanupStepSucceeded) — never from a flag beside them, so a
		// report cannot claim a delivery its steps do not show.
		return finishCleanupStepSucceeded(rep, finishCleanupPushVerb)
	}
	// A REFUSED PUSH IS THE ONE DISPOSITION THAT CAN NEVER BE SETTLED
	// (measured 2026-10-17, this run: `candidates=10 removed=0 settled=10`
	// while local refs were still on the host).
	//
	// WHY IT WAS MISSED: the two arms above both reason from a SKIP, and this
	// one from the step list — but "no worktree step" was read as "nothing left
	// to do", which is false whenever the chain stopped at a step it RAN AND
	// FAILED. The measured shape: `d.Repo` pointed at a worktree directory that
	// was gone, so the push exited at chdir, the chain reported a refused push
	// and returned before any worktree step could be appended — and the row read
	// `settled`, i.e. "its close-out is complete", for a row whose branch had
	// not crossed and whose local ref was still there. Step ④ never ran, which
	// is exactly what an operator reads `settled` to mean has happened.
	//
	// The evidence is the report's OWN step (finishCleanupPushStepRefused — the
	// same reader the delivery sentences use, so no second judgement is
	// introduced): a push this run attempted and did not land is work left to
	// do, and it says so.
	if finishCleanupPushStepRefused(rep) {
		return false
	}
	// The chain ran: settled exactly when it had NO worktree step to perform
	// — nothing was registered for the branch, so only the branch steps
	// remain. A chain that rendered any worktree step (removed it, failed to
	// remove it, or predicted a removal in plan mode) is not settled: there
	// WAS a worktree and something happened to it.
	return !finishCleanupReportHasWorktreeStep(rep)
}

// taskFinishCleanupSessionID runs the 4-step close-out chain for ONE
// session task row (looked up by its t-<unix> task_id — consolidation
// decoupled task_id from the session UUID). The whole chain is fail-soft:
// every guard mismatch returns a Skipped report, every step failure lands
// in Steps with OK=false — close-out problems are REPORTED, never fatal to
// the caller.
//
// UNEXPORTED 2026-10-31 (Rule 43, task 01a18f76). At the parent revision
// (58b5618^) this was the chain's PRODUCTION entry point and eight production
// call sites reached it: cmd/agent/cli_tasks.go (4), cli_resume.go (2),
// cli_worker_resume.go (2), internal/agent/taskregister.go, task_delivery_retry.go,
// task_reclaim_self_delivery.go (itself deleted 2026-10-31, task 01a19052),
// task_sync_git_pushback.go, taskstate.go. ALL
// EIGHT DIED WITH THE MECHANISM in 58b5618b — every remaining production mention
// is a comment recording the retirement (cli_resume.go:1227, cli_worker_resume.go:269,
// taskregister.go:794, task_sync_git_pushback.go:34, taskstate.go:71/101) and
// the standards census explicitly counts it as an arm
// nothing derives (cwd_release_gate_test.go:303-320). The export therefore had
// zero consumers, and an exported spelling with no production caller is what kept
// "the production function is gone" invisible.
//
// Its remaining readers are this package's OWN tests: the shared harness below
// (line ~1830) and the env-gated LIVE diagnostic in zz_wtrecycle_diag_test.go —
// which is the whole reason the chain body is still carried here rather than
// deleted (it judges a stranded worktree through the real ownership gate instead
// of a drifting re-implementation).
func taskFinishCleanupSessionID(taskID string) (*FinishCleanupReport, error) {
	return taskFinishCleanup(taskID, false)
}

// allowTestPushTarget arms the harness's in-process-marker opt-out so an arm
// may push a deliverable to a target inside ITS OWN temp root (the bare repo
// the fixture stands up) instead of a real node's checkout.
//
// It exists because the fence's allow-list is a PATH property (is the target
// inside the suite's temp root?), and a run that carries no temp-root pin — a
// single-package invocation, a hand-run binary — would otherwise refuse a
// legitimate local arm. Arms that inject `localTestPeerTarget` should call
// this; the assertion in the regression test below does not need it.
func allowTestPushTarget(t *testing.T) {
	t.Helper()
	t.Setenv(finishCleanupInProcessTestMarker, "1")
}

// assertNoStepRan fails when a step with the given name succeeded (OK=true) — the
// assertion that a refused close-out performed no delivery.
func assertNoStepRan(t *testing.T, rep *FinishCleanupReport, step string) {
	t.Helper()
	for _, s := range rep.Steps {
		if s.Step == step && s.OK {
			t.Fatalf("a refused close-out must not perform %s (step detail: %s)", step, s.Detail)
		}
	}
}

// assertStep asserts the chain's step states by name.
func assertStep(t *testing.T, rep *FinishCleanupReport, step string, wantOK bool) {
	t.Helper()
	for _, s := range rep.Steps {
		if s.Step == step {
			if s.OK != wantOK {
				t.Fatalf("step %s ok=%v want %v (detail: %s)", step, s.OK, wantOK, s.Detail)
			}
			return
		}
	}
	t.Fatalf("step %s missing from report %+v", step, rep.Steps)
}

// auditAgeRegistration backdates a registration's own creation record so the
// fixture is past the sweep's age floor (chtimes is the fixture's "this
// happened long ago"; unreachable → skip, never a silent pass).
func auditAgeRegistration(t *testing.T, worktree string) {
	t.Helper()
	record := sweepCreatedAtRecordPath(t, worktree)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(record, old, old); err != nil {
		t.Skipf("chtimes unavailable: %v", err)
	}
}

// auditEmitOff disables the emit for the rest of the test, restoring it on
// cleanup — the RED half's switch.
func auditEmitOff(t *testing.T) {
	t.Helper()
	prev := removalaudit.SetEmitDisabledForTest(true)
	t.Cleanup(func() { removalaudit.SetEmitDisabledForTest(prev) })
}

// auditProbe is the NAMED assertion: "" when the sink holds a record for
// (arm, target) carrying at least one attributing detail, else the failure text.
// The text is RETURNED rather than fatal'd so the RED half can assert on the
// same sentence the GREEN half passes.
func auditProbe(t *testing.T, sink string, arm removalaudit.Arm, target string) string {
	t.Helper()
	recs, err := removalaudit.Records(sink)
	if err != nil {
		if os.IsNotExist(err) {
			return "no audit record for arm " + string(arm) + " target " + target + " (records present: 0)"
		}
		return "read the audit sink: " + err.Error()
	}
	for _, r := range recs {
		if r.Arm != string(arm) || r.Target != target {
			continue
		}
		if r.Branch == "" && r.TaskID == "" && r.Evidence == "" {
			return "the record for " + target + " carries no branch/row/evidence — unattributable"
		}
		return ""
	}
	return "no audit record for arm " + string(arm) + " target " + target +
		" (records present: " + strconv.Itoa(len(recs)) + ")"
}

// auditSink pins the audit sink for one test and returns its path. ONE place
// these tests point the emitter at a file of their own.
func auditSink(t *testing.T) string {
	t.Helper()
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)
	return sink
}

// chainBranchDeleteRecords reads the sink back and returns the branch-delete
// records for ONE arm and branch, in file order. A sink that does not exist yet
// reads as "nothing recorded" — the honest reading, and the one the D1 count
// depends on (an absent file is zero records, not a failure to read).
func chainBranchDeleteRecords(t *testing.T, sink string, arm removalaudit.Arm, branch string) []removalaudit.Record {
	t.Helper()
	recs, err := removalaudit.Records(sink)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the audit sink: %v", err)
	}
	var out []removalaudit.Record
	for _, r := range recs {
		if r.Kind == removalaudit.KindBranchDelete && r.Arm == string(arm) && r.Target == branch {
			out = append(out, r)
		}
	}
	return out
}

// covLifeGit runs one git command in dir, failing the test on a non-zero exit.
func covLifeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v (in %s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// covLifeGitBinary is the git executable every fixture here invokes (Rule 1b:
// the tool name has one home).
const covLifeGitBinary = "git"

// covLifeGitEnv isolates the process for a git-touching test: GOPI_HOME (data +
// settings resolution), the ledger, and PATH/HOME (a fixture `git` and no real
// user config), all in t.TempDir() space.
func covLifeGitEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(memoryDBEnv, filepath.Join(home, memoryDefaultDBFile))
	t.Setenv("HOME", home)
	t.Setenv("PATH", os.Getenv("PATH"))
}

// covLifeGitRepo creates a throwaway git repo in a t.TempDir() with one commit,
// returning its path. No user config is required (the commit is made with
// explicit -c identity flags).
func covLifeGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	covLifeGit(t, repo, covLifeGitBinary, "init", "-q")
	covLifeWriteFile(t, filepath.Join(repo, "README.md"), "fixture\n")
	covLifeGit(t, repo, covLifeGitBinary, "add", "README.md")
	covLifeGit(t, repo, covLifeGitBinary, "-c", "user.email=cov@example.invalid",
		"-c", "user.name=cov", "commit", "-q", "-m", "fixture commit")
	return repo
}

// covLifeRepoWithWorktree creates a fixture repo carrying one linked
// worktree on a non-exempt branch, and returns (repo, worktree, branch).
func covLifeRepoWithWorktree(t *testing.T, branch string) (string, string, string) {
	t.Helper()
	repo := covLifeGitRepo(t)
	covLifeGit(t, repo, covLifeGitBinary, "branch", branch)
	wt := filepath.Join(t.TempDir(), "wt-"+strings.ReplaceAll(branch, "/", "-"))
	covLifeGit(t, repo, covLifeGitBinary, "worktree", "add", "-q", wt, branch)
	return repo, wt, branch
}

// covLifeSetRole pins this process's node role through the env override and
// clears the process-wide cached identity (ResolveNodeRole caches once).
func covLifeSetRole(t *testing.T, role string) {
	t.Helper()
	config.ResetNodeIdentityForTest()
	t.Setenv(config.RoleEnv, role)
	t.Cleanup(config.ResetNodeIdentityForTest)
}

// daemonCwdHolder starts a child process that sits INSIDE dir (CWD = dir) while
// advertising a DAEMON argv[0] — the exact holder shape guard ③ sees when a
// detached merger was spawned from a task worktree. It returns the mock process
// (whose Reap() both kills the group and REAPS the child, so a following
// predicate never waits on a zombie's procfs entry) and the argv[0] it carries.
func daemonCwdHolder(t *testing.T, dir string) (*testutil.MockProcess, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], daemonHolderChildArgRun)
	cmd.Dir = dir
	cmd.Args[0] = daemonHolderArgv0
	cmd.Env = append(os.Environ(), daemonHolderEnvMarker+"=1",
		testutil.SuiteTempRootEnvName+"="+t.TempDir(),
		testutil.TempDirEnvName+"="+t.TempDir())
	mp := testutil.StartMockProcessCmd(t, cmd)
	waitProcCwd(t, mp.Pid(), dir)
	return mp, daemonHolderArgv0
}

// live-CWD holder (Rule 7 zero-dep: the holder IS this test binary re-exec'd
// as a child — no shell, no external sleep binary, no timing guesswork).
const (
	// holderEnvMarker identifies the child process as the CWD holder (its
	// only job is to sit in the CWD its parent gave it).
	holderEnvMarker = "GOPI_FINISH_CLEANUP_CWD_HOLDER"
	// holderChildArgRun re-execs exactly the holder test in the child binary
	// (any other test is skipped — the marker gates the body).
	holderChildArgRun = "-test.run=TestHelperWorktreeCwdHolder"
	// daemonHolderChildArgRun is the DAEMON holder's selector (its own constant,
	// not an inline literal: the holder-bodied sibling of holderChildArgRun, and
	// the name the selector guard resolves against the child body).
	daemonHolderChildArgRun = "-test.run=TestHelperDaemonWorktreeHolder"
	// daemonHolderEnvMarker identifies the child process as a DETACHED DAEMON
	// holder (epic daemon-cwd-escape): the child is re-exec'd with an OVERRIDDEN
	// argv[0], which is the identity channel the reclaim report recognises. The
	// marker is what makes the helper skip unless it is the intended child.
	daemonHolderEnvMarker = "GOPI_FINISH_CLEANUP_DAEMON_HOLDER"
	// daemonHolderArgv0 is the argv[0] the helper advertises: the real detached
	// merger's name (daemonArgv0Prefix + "merger"), never a copied literal —
	// the recovery check and the spawn seam must agree at compile time.
	daemonHolderArgv0 = "gopi-" + taskverbs.Merger
)

// deadOwnerPid is a pid that CANNOT name a process: it is above the kernel's
// pid_max, so kill(pid, 0) answers ESRCH on any host.
//
// It used to be 7, with the comment "a pid that cannot be alive in this
// container (a kernel thread — kill 0 returns ESRCH)". THAT PREMISE WAS NEVER
// MEASURED AND IS FALSE ON A REAL HOST: /proc/7/comm reads kworker/R-netns, it
// is owned by root, and kill(7, 0) from an unprivileged test answers **EPERM**
// — the process EXISTS. The fixture only looked correct because the old probe
// folded EPERM into "dead", i.e. it was resting on the very defect epic
// 01a17e87 removes: once the probe told the truth, these two tests failed with
// "claimed by host-a pid 7 (alive)".
//
// The replacement cannot rot that way: pid_max is a hard bound on every pid the
// kernel can hand out, so a larger number is provably unassigned. It also does
// not depend on what happens to be running, on the container's pid namespace,
// or on whether this process is root.
const deadOwnerPid = 4194304 + 1

// deadPidOnThisHost returns a pid this test has STARTED, REAPED and CONFIRMED
// dead — the only honest source for the fixture's launcher record. It is
// zero-dep: the probe is this very test binary, selected so that it skips and
// exits at once.
func deadPidOnThisHost(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], orphanHolderProbeRunArg)
	if err := cmd.Start(); err != nil {
		t.Skipf("orphan fixture: cannot start the dead-pid probe (%v)", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait() //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion// reaped — the pid is now genuinely dead
	if pidAlive(pid) {
		t.Skipf("probe pid %d is still alive — a reaped pid is this fixture's premise", pid)
	}
	return pid
}

// deliverableBlob is the executor-backfill shape the chain reads.
func (f *finishCleanupFixture) deliverableBlob() string {
	return `{"repo":"` + f.clientDir + `","branch":"` + f.featBranch + `","worktree":"` + f.wtDir + `"}`
}

// finishCleanupDeleteDeliveredBranch is ④ (delete the local branch) as its own
// step, so a chain with NO worktree can reach it. It is the SAME decision the
// wt-bearing chain makes two hundred lines above — one implementation, called
// from the two places a delivered branch may need reaping (Rule 14): the tail
// of the ordinary chain, where ③ collected the directory first, and the wt == ""
// path, where there is no directory at all and therefore no ③ to be paired
// with.
//
// WHY A wt-LESS CHAIN MAY REACH IT (the 2026-09-26 HP measurement): the "pair
// stays together" rule exists so a worktree the sweep did not collect does not
// become a deleted ref — it binds ④ to a ③ THAT HAPPENED. With no worktree in
// the row and none resolvable, there is no such ③; returning instead skipped ④
// for the whole shape, and two completed rows kept their task/<id> refs after
// their branches had reached the peer. The protection the pairing serves is
// enforced by ④'s OWN two axes and does not depend on ③: axis 2 requires THIS
// sweep's push to have succeeded (finishCleanupBranchForceDeleteReason) and the
// orphan guard consults the delivery point (branchDeleteOrphanRefusalAt). A
// chain that pushed nothing fails both and keeps the ref.
func finishCleanupDeleteDeliveredBranch(ctx context.Context, rep *FinishCleanupReport, branchDir, branch, target string, planOnly bool) (*FinishCleanupReport, error) {
	// Branch-deletion guard (2026-09-12 epic), axis 1 — liveness: the ref is
	// deleted only when no LIVE ledger row still owes work through it. The step
	// ③ worktree gate spares a live owner's DIRECTORY; this spares its BRANCH,
	// which is reachable even after the directory was legitimately recycled (a
	// live row re-pointed at the branch). Reported, never silent.
	if why := finishCleanupBranchLiveOwnerReason(ctx, rep.TaskID, branch); why != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch, Detail: why})
		return rep, nil
	}
	// THE RUN-IDENTITY GUARD on the BRANCH step too (SL3-2's second observed
	// effect: the ref was moved after the directory was gone). The branch half
	// rests on the ground the branch is checked out in, when one still stands:
	// a live foreign run's ground is not a ref this close-out may reap.
	if wt := finishCleanupBranchWorktree(ctx, branchDir, branch); wt != "" {
		if why := finishCleanupRunIdentityRefusal(wt, branch); why != "" {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch, Detail: why})
			return rep, nil
		}
	}
	// ④ IS NEVER RUN AGAINST A REF THAT IS NOT THERE (defect D1, measured
	// 2026-10-11 and filed as task 01a12b80).
	//
	// WHY THIS GUARD EXISTS, in the chain's own words: `git branch -d` and
	// `git branch -D` BOTH fail on an absent ref (exit 1, "branch 'x' not
	// found") — so the two states that matter here are INDISTINGUISHABLE at
	// git's error check, and the force-delete standing gate reads THIS
	// chain's own push report rather than the world. A row whose ref an earlier
	// pass already deleted therefore kept a perfectly good standing: the push
	// step records its idempotent no-op as OK (finishCleanupBranchAbsentDeliveryNote),
	// the force-delete guard asks finishCleanupStepSucceeded(rep, push), is told
	// yes, and the chain writes a `finish-cleanup-branch-force` RECORD for a
	// deletion it could not perform. Measured on one branch: three identical
	// records, from two pids —
	// one of them the 10-minute serve tick (serveFinishCleanupSweep), which
	// re-selects terminal rows for ever because nothing records that this row's
	// close-out already ran.
	//
	// THE HARM IS NOT THE REPEATED WORK, it is that the audit stream stopped
	// distinguishing a real deletion from an empty gesture: both read
	// `finish-cleanup-branch-force` with the identical sentence, and an operator
	// reconstructing "was this delivered?" reads a record as evidence of a
	// deletion that happened — or, the way it actually went wrong, reads a
	// healthy delivery as one that never landed.
	//
	// SO: no ref, no step, NO RECORD. The three fields a reader needs are
	// recorded as a step instead, so the disposition is reported and never
	// silent (Rule 15) — and the record itself stays absent, because a record
	// for a deletion that did not happen is a false record.
	//
	// The tip is captured HERE, before the ref goes away, and it is the same
	// reading that both proves the ref exists and fills the audit record's sha:
	// after the delete nothing can ask what this ref pointed at (epic 01a12af7
	// case ④ — the tip is the one fact a recovery command needs, and asking a
	// moment later is asking a question whose subject is gone).
	deletedTip := branchDeleteTipOf(ctx, branchDir, branch)
	if strings.TrimSpace(deletedTip) == "" {
		// A MEASURED ABSENCE AND A PROBE THAT COULD NOT RUN ARE NOT THE SAME
		// ANSWER (Rule 15; measured 2026-10-17 on the operator's own host).
		// The deletion above cannot have happened when the reading is empty
		// because `branchDir` is GONE — the live shape, where the row's `repo`
		// IS the worktree and that directory was removed. Reporting "the ref is
		// already gone" there was a claim about a BRANCH drawn from a failure
		// about a DIRECTORY: `git branch` still listed it, and the close-out
		// reported the row settled. So the refusal names the unreadable
		// directory and the surviving ref it could not measure, and it is the
		// main repo that the caller is pointed at.
		if finishCleanupProbeCouldNotRun(ctx, branchDir) {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch,
				Detail: finishCleanupBranchTipUnreadableReason(branchDir)})
			// The surviving ref is the operator's to see: asked of the MAIN
			// repo (finishCleanupMainRepoDir), never of the unreadable one.
			// A tip that RESOLVES here is the measurement the old wording
			// asserted away, and it is recorded with the deletion that is owed.
			if tip := finishCleanupBranchTip(ctx, finishCleanupMainRepoDir(branchDir), branch); tip != "" {
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch,
					Detail: finishCleanupBranchStillHereNote + tip})
				// NOT SETTLED: the ref is measurably here and this pass did not
				// remove it. Read by finishCleanupReportSettled through the
				// owed marker, so `settled=N` can never count a surviving ref.
				rep.Skipped = true
				rep.SkipReason = finishCleanupDeliveryOwedPrefix + finishCleanupBranchStillHereNote + tip
			}
			return rep, nil
		}
		detail := finishCleanupBranchAbsentNote
		if planOnly {
			detail = finishCleanupPlanMarker + detail
		}
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch, Detail: detail})
		return rep, nil
	}
	planDetail := ""
	if planOnly {
		// The plan counterpart of ③b, in the position the act gives it (before
		// the delete). It predicts the STEP, not its reading: whether git holds
		// anything to prune is a fact about this host's registry, and a dry run
		// that claimed to know it would be predicting the answer it exists to
		// discover.
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStaleRegistrationStep,
			Target: branchDir, OK: true, Detail: finishCleanupPlanStaleRegistrationDetail})
		planDetail = finishCleanupPlanMarker + "run: git " + finishCleanupBranchVerb + " " +
			finishCleanupPlanDeleteFlag + " " + branch
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch, OK: true, Detail: planDetail})
		return rep, nil
	}
	// Audit record BEFORE the ref deletion (package removalaudit): the deciding
	// evidence is the chain's own branch-liveness gate, which did not refuse —
	// no live ledger row owes this branch. The record carries the TIP captured
	// above and the DELIVERY POINT this chain pushed to (②), so "did this row's
	// branch reach the server?" is answerable from the ledger alone — the
	// question the pre-2026-10-11 record left to a hand investigation
	// (removalaudit's FieldSha / FieldDeliveryPoint).
	//
	// THIS IS THE PLAIN ARM'S RECORD, and it is written on the delete path even
	// though the delete below is the FORCE half (Rule 13: an arm constant that no
	// emit passes is a DEAD constant — `TestRemovalAuditGateEveryRemovalSiteEmits`
	// fails the build on one, and this record is what keeps `finish-cleanup-branch`
	// live). What it states is the gate that was actually asked and actually
	// passed — the branch-liveness/run-identity pair above, whose verdict governs
	// the delete regardless of which git flag performs it — so a reader of the
	// sink can STILL tell the two shapes apart: a plain record stands alone (the
	// path that reaches the delete without a force standing), and the force record
	// (written a few lines below) is the shape whose delete the chain's own push
	// had to authorise first.
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, branchDir, branch, rep.TaskID,
		deletedTip, target,
		"delivery chain's branch-liveness gate did not refuse (no live row owes this branch)")
	// ③b THE STALE REGISTRATION, pruned BEFORE the delete is attempted.
	//
	// MEASURED 2026-10-13 (this run, in a two-node fixture and on this host's own
	// ledger). A worktree whose DIRECTORY is gone leaves a registration behind —
	// `git worktree list --porcelain` keeps naming the vanished path and `git
	// branch -d`/`-D` answer, for ever:
	//
	//	error: cannot delete branch 'task/<id>' used by worktree at '<gone path>'
	//
	// Nothing in the chain drops that entry. The removal arm above reads the
	// directory as gone and correctly declines to `worktree remove` it (git
	// refuses such an entry anyway: exit 128, "is not a working tree"), so the
	// registration survived every pass and step ④ — which this function exists to
	// perform — could NEVER succeed. Net effect: a row reading `completed` and
	// `[closeout-refused]`, with a reason saying "delivery already attempted",
	// while the local ref it names is still on the host.
	//
	// THE FIX IS GIT'S OWN VERB. `git worktree prune` drops exactly the
	// registrations whose directory is MISSING — it takes no path, never touches
	// a directory and cannot affect a live worktree — so the three removal
	// guards (liveness, foreign owner, ledger liveness) are not bypassed: there
	// is nothing of anyone's to remove. The reclaim arm repairs the same defect
	// this way already (task_reclaim.go's reclaimRepairRegistration, whose
	// comment records the same consequence: "a gone-but-registered worktree
	// keeps appearing there until it is pruned"); this is that repair, reached
	// from the close-out standing on the row.
	//
	// It runs on the APPLY path only (a plan predicts; it does not tidy) and
	// only here, AFTER the plan arm has returned — so the plan's forecast of
	// the branch delete is unchanged and the step records what the act actually
	// met.
	if out, perr := finishCleanupPruneStaleWorktrees(ctx, branchDir); perr != nil {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStaleRegistrationStep,
			Target: branchDir, Detail: perr.Error()})
	} else if strings.TrimSpace(out) == "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStaleRegistrationStep,
			Target: branchDir, Detail: finishCleanupStaleRegistrationNoneNote})
	} else {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStaleRegistrationStep,
			Target: branchDir, OK: true,
			Detail: finishCleanupStaleRegistrationNote + ": " + strings.TrimSpace(out)})
	}
	// ④ THE DELETE. ONE VERB, AND IT IS THE FORCE HALF (measured 2026-10-14,
	// task 01a131bb). The chain used to run `git branch -d` first and fall back
	// to `-D` on its refusal. The safe half's question is git's own — "is this
	// branch merged into its upstream / HEAD?" — and ④'s position makes that
	// answer NO BY CONSTRUCTION: ② has just pushed the ref to the DELIVERY POINT
	// (the peer's own checkout), so the tip is on the far side while THIS repo's
	// HEAD still does not contain it (`gitMergeBaseAncestor` false: master has
	// not merged this branch). The refusal was therefore not a discovery but a
	// fixed cost paid by every close-out, and it is the shape the audit stream
	// shows: `finish-cleanup-branch` and `finish-cleanup-branch-force` march in
	// lock-step (measured on HP: 393/393 — no `-d` ever stood alone).
	//
	// WHAT REPLACES IT IS NOT "NOTHING": the force half is unlocked by the SAME
	// two questions the fallback asked, both asked BEFORE the delete, both
	// UNCHANGED by this change —
	//   axis 2: ② must have landed THIS ref (finishCleanupBranchForceDeleteReason);
	//   orphan: the tip must survive in another ref, the delivery point included
	//           (branchDeleteOrphanRefusalAt — the guard, untouched, not
	//           bypassed and not weakened).
	// A chain that pushed nothing, or whose tip no other ref holds, still keeps
	// the branch — fail-soft, report AND keep. The ONLY thing that changed is
	// which flag is handed to a delete the guards had already authorised.
	if why := finishCleanupBranchForceDeleteReason(finishCleanupStepSucceeded(rep, finishCleanupPushVerb)); why != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch, Detail: why})
		return rep, nil
	}
	// ORPHAN guard (2026-09-17 incident): a FORCE delete is the one path that
	// can make a commit reachable from no ref at all. The delivery point is part
	// of the refs consulted (2026-09-18): this sweep pushed the branch to
	// `target` in ②, so a local-only scan would answer "no other ref" for a tip
	// the delivery just kept.
	if why := branchDeleteOrphanRefusalAt(branchDir, branch, target); why != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch, Detail: why})
		return rep, nil
	}
	// Audit record BEFORE the delete. The evidence is the standing the guard
	// above just granted: this chain's OWN push landed the ref, so this is not a
	// force delete of an undelivered branch. Same two facts as the plain record
	// above — the tip (still resolvable: this ref is being deleted, not already
	// gone) and the point the push landed it on.
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranchForce, branchDir, branch, rep.TaskID,
		deletedTip, target,
		"this chain's own push landed the ref and the orphan guard did not refuse (force-delete standing)")
	delOut, delErr := finishCleanupGit(ctx, branchDir, finishCleanupBranchVerb, finishCleanupBranchForceDeleteFlag, branch)
	if delErr != nil {
		// REPORTED, NEVER SWALLOWED (Rule 15/15b). Both guards granted this
		// delete, so a failure here is a real failure and the branch is still
		// on the host: the step is recorded NOT OK with git's own sentence and
		// the error. There is deliberately no second attempt — the fallback the
		// chain used to make is gone, and retrying the SAME verb would only
		// change what the audit stream says about a delete that did not happen.
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch,
			Detail: finishCleanupBranchVerb + " " + finishCleanupBranchForceDeleteFlag + " failed after the guards granted it: " +
				strings.TrimSpace(delOut) + ": " + delErr.Error()})
		return rep, nil
	}
	rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch, OK: true,
		Detail: planDetail + finishCleanupBranchScratchOnlyNote(ctx, branchDir, deletedTip, branch)})
	return rep, nil
}

// finishCleanupFixture is a minimal two-node git world: server (bare) +
// client (working repo) + one linked worktree on a feature branch.
type finishCleanupFixture struct {
	t          *testing.T
	serverDir  string // bare "server" repo
	clientDir  string // client working repo (main checkout, on master)
	wtDir      string // linked worktree on the feature branch
	featBranch string
}

// finishCleanupFixtureReapChild kills pid and waits for terminal death, so a
// fixture's cleanup is EVIDENCE rather than an assertion about one sample. On
// timeout it reports the measured state, and distinguishes a recycled pid (a
// fixture-environment failure, not a leak) by comparing start ticks — a pid
// number is not an identity.
func finishCleanupFixtureReapChild(t *testing.T, pid int, label string) {
	t.Helper()
	if pid <= 1 {
		return
	}
	startTicks, hadStart := fixtureProcStartTicks(pid)
	deadline := time.Now().Add(finishCleanupFixtureReapWait)
	detail := ""
	for {
		gone, why := finishCleanupFixtureTerminalGone(pid)
		detail = why
		if gone {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first terminal observation
		time.Sleep(finishCleanupFixtureReapPoll)
	}
	if hadStart {
		if now, ok := fixtureProcStartTicks(pid); ok && now != startTicks {
			t.Errorf("%s: pid %d was RECYCLED while the fixture reaped it (start ticks %s -> %s) — "+
				"a fixture-environment failure, not a leak: the shape must be rebuilt", label, pid, startTicks, now)
			return
		}
	}
	t.Errorf("%s: pid %d did not reach terminal death within %s — measured: %s; signal 0 %s, pidAlive=%t",
		label, pid, finishCleanupFixtureReapWait, detail, fixtureSignalZeroVerdict(pid), pidAlive(pid))
}

const (
	// finishCleanupFixtureReapWait bounds the wait for a killed fixture process
	// to reach terminal death. Generous on purpose: the loop waits for EVIDENCE,
	// and a loaded host only makes the evidence later, never wrong.
	finishCleanupFixtureReapWait = 5 * time.Second
	// finishCleanupFixtureReapPoll is that wait's interval — an order of
	// magnitude inside the measured dying window it must not miss.
	finishCleanupFixtureReapPoll = 2 * time.Millisecond
	// procStatStarttimeIndex is /proc/<pid>/stat's starttime field number
	// (1-based, counting from the pid itself) — the kernel's incarnation stamp,
	// the only sound way to tell "same process" from "a recycled pid number".
	procStatStarttimeIndex = 22

	// fixtureReapProbeProgram is the probe child: a real process the kernel
	// schedules, standing still until it is killed. Not a load generator
	// (Rule 27) — it exists so the predicate is judged against a process the
	// kernel owns.
	fixtureReapProbeProgram = "/bin/sleep"
	// fixtureReapProbeHoldArg is how long the probe stands still when it must
	// stay alive for the arm to observe it.
	fixtureReapProbeHoldArg = "10"
)

// finishCleanupFixtureTerminalGone reports whether pid's current incarnation
// can no longer be observed running, and — when it cannot be judged gone — the
// MEASURED evidence for the refusal.
//
// Gone means exactly one of:
//   - /proc/<pid>/stat is gone (ENOENT): the kernel released the pid. Signal 0
//     is only trustworthy here (measured: the two channels disagree for ~150us
//     during exit — the defect above).
//   - state 'Z': terminated, only the reap outstanding. A zombie is not
//     schedulable, so it is terminal — and judging it HERE, before the kernel
//     walks the pid through the transient 'X', is what removes the race.
//   - state 'X': measured as the transient death state between 'Z' and ENOENT.
//   - any other stat error: the incarnation cannot be observed at all, which is
//     not evidence that it is running (Rule 15: the reader gets the reason).
//
// Anything else — a schedulable state, or a state letter the probe did not
// measure — is NOT terminal, and the caller keeps waiting.
func finishCleanupFixtureTerminalGone(pid int) (bool, string) {
	gone, _, why := fixtureTerminalEvidence(pid)
	return gone, why
}

// finishCleanupFreedWorktreeLabel names, on the one-line close-out report, WHICH
// directory the `worktree` step covered — one spelling (Rules 1b/13), so a
// reader greps one token to find every close-out that freed a ground.
const finishCleanupFreedWorktreeLabel = "worktree-freed="

// finishCleanupNoDeliveryChannelReason reports whether a skip reason states that
// this host has NO delivery channel at all — no peer declared, or a declared
// alias git may not be handed (the two causes noServerTargetSkipReason reads
// apart). On that host the hand-off does not merely fail, it does not EXIST
// (the composer's own words), so a row in the gone-directory state has nothing
// left to do and must keep reading SETTLED: the measurement this protects is a
// single-node host whose directoryless rows all read as refusals
// (2026-09-16, `refused=10 would-run=0` mis-read as a stuck sweep).
func finishCleanupNoDeliveryChannelReason(reason string) bool {
	for _, marker := range []string{finishCleanupNoPeerDeclaredNote, finishCleanupMalformedPeerNote} {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

// finishCleanupReportHasWorktreeStep reports whether the chain touched a
// worktree at all.
func finishCleanupReportHasWorktreeStep(rep *FinishCleanupReport) bool {
	for _, s := range rep.Steps {
		if s.Step == finishCleanupStepWorktree {
			return true
		}
	}
	return false
}

// finishCleanupServerRecycle is the server/single-node arm of the chain:
// a terminal run whose deliverable points at a worktree must release that
// worktree (resource-responsibility — the merger's trigger is "no worktree
// protection"), but the branch stays: it is the shared repo's merge input,
// and deleting it here would remove work the merger has not yet consumed.
// Step set: worktree remove only (no push — nothing to push to; no branch
// -d — the branch is the deliverable). Same clean/dirty guard as the client
// arm. Fail-soft per step.
func finishCleanupServerRecycle(ctx context.Context, rep *FinishCleanupReport, d taskFinishDeliverable, planOnly bool) (*FinishCleanupReport, error) {
	// The report carries its own mode, so a caller that enters the chain
	// through THIS seam (the tests, and any future second entry point) gets
	// the same honest report as the ledger-resolving one — the printer
	// decides what to say from the flag alone.
	rep.PlanOnly = planOnly
	// THE MAIN REPO'S TWO LINES — the server/single-node arm owes the same
	// question as the client arm (epic closeout-reads-the-main-repo). See
	// runFinishCleanupChainTarget for why the call is repeated at each body's
	// head and why the shared report makes it a single reading per close-out.
	if skipped, ok := finishCleanupMainRepoGate(ctx, rep, d); !ok {
		return skipped, nil
	}
	// Branch resolution: backfilled deliverable wins; fallback = the
	// worktree's checked-out branch. An exempt branch is never touched.
	branch, brErr := finishCleanupResolveBranch(ctx, rep, d)
	if brErr != nil {
		rep.Skipped = true
		rep.SkipReason = brErr.Error()
		return rep, nil
	}
	if branch == "" {
		return rep, nil // the resolver already named the refusal on the report
	}
	rep.Branch = branch

	// Worktree resolution — the row's own record (deliverable.worktree) wins;
	// git's answer (the branch's LINKED worktree in the deliverable repo) is
	// resolved EITHER WAY, because the two can disagree: `git worktree add
	// <existing-path> <branch>` re-attaches a directory, moving the branch's
	// checkout to a path the row never recorded. The manual-run shape is the
	// normal one — the row carries the child's cwd (main repo) and the
	// Rule-6 worktree lives keyed by branch. NO linked worktree on the branch
	// = the run worked on the main checkout — nothing to recycle (the branch
	// stays as the merger's input either way).
	declared := strings.TrimSpace(d.Worktree)
	linked := finishCleanupBranchWorktree(ctx, d.Repo, branch)
	// Ownership gate ① (2026-09-10 epic): the row's record and git's answer
	// must name the SAME directory. A disagreement means the branch was
	// re-pointed at a directory this row does not claim — refusing HERE (not
	// only at the removal below) keeps the arm from probing the foreign
	// directory at all. SKIP with BOTH paths named, never a removal.
	if why := finishCleanupOwnerDeclaredReason(declared, linked); why != "" {
		rep.Skipped = true
		rep.SkipReason = why
		return rep, nil
	}
	wt := declared
	if wt == "" {
		wt = linked
	}
	rep.Worktree = wt
	if wt == "" {
		rep.Skipped = true
		rep.SkipReason = "no linked worktree on branch " + branch + finishCleanupNoLinkedWorktreeMarker
		return rep, nil
	}

	// THE DIRTY PROBE IS NOT TAKEN HERE — see the alignment precondition
	// below. It used to sit at this point and RETURN `Skipped`, which meant a
	// finished run's own untracked residue (or its uncommitted edit) turned the
	// delivery into a silent no-op: the alignment was never measured and the
	// close-out read as routine hygiene (measured, 2026-09-21). The probe asks
	// a REMOVAL question, and it is now asked where that question is decided —
	// after the delivery has been aligned. Its tolerance is unchanged.

	// THE ABSENT GROUND IS ASKED ABOUT BEFORE IT IS DESCRIBED (task
	// 01a18a04-04e6-7ecb-9b49-f53236a477c9). A clone-channel run's ground is
	// removed BY DESIGN once its push lands (cmd/agent/devworkspace_channel.go ->
	// devworkspace.Reclaim), and that arm records the removal in the removal-audit
	// sink BEFORE it deletes (ArmCloneChannelReclaim). This close-out then
	// re-enters for the same row and finds no directory — and every arm below
	// reasons from that ABSENCE: the probe composed "worktree unreadable", the
	// removal-state reason composes "nothing to remove", and both are sentences
	// about a LOSS for the one state in which an arm deliberately finished.
	//
	// It sits at the HEAD of the arm (before the probe) because the probe RETURNS:
	// an arm placed after it would be unreachable for exactly the case it
	// explains. It is ONE call for both entry orders — see
	// finishCleanupReclaimedGone — and only the ABSENCE PLUS A RECORD concludes a
	// reclaim; no record takes the loud path unchanged.
	if reclaimedRep, reclaimed := finishCleanupReclaimedGone(ctx, rep, wt); reclaimed {
		return reclaimedRep, nil
	}
	// Terminal-state guard: the row must be completed/failed (delivery
	// done) — the CALLER enforces this for the wrapper row; the worktree
	// check here is the chain's own second lock (idempotent re-entry after
	// a crash mid-run must never recycle a live worktree).
	if _, err := finishCleanupGit(ctx, wt, "rev-parse", "--verify", "HEAD"); err != nil {
		rep.Skipped = true
		// THE RECORD WAS ALREADY ASKED, ONE STEP UP (finishCleanupReclaimedGone):
		// reaching here means the directory is not there AND no arm recorded
		// freeing it — or the sink could not be read, which was reported. So this
		// remains the LOUD path, by measurement rather than by assumption, and the
		// word "unreadable" now means what it says.
		rep.SkipReason = finishCleanupUnreadableWorktreeMarker + ": " + err.Error()
		// THE READING THIS REFUSAL LEAVES BEHIND IS ANSWERED BY THE CLASSIFIER,
		// NOT HERE (measured 2026-10-13, real binary over a fixture ledger — see
		// the red proof in the report). The FIRST shape of this fix delegated the
		// refusal into the CLIENT arm, on the belief that a peer-declaring host
		// could reach this body. It cannot: the dispatcher (taskFinishCleanup)
		// enters the client chain whenever finishCleanupClientArmApplies() is
		// true, so every call arriving HERE arrives on a host whose row has NO
		// delivery channel. The delegation was unreachable and the red proof
		// said so (this arm stayed red with the delegation reverted). What the
		// row actually needed is its VERDICT: finishCleanupReportSettled now asks
		// the same predicate about this same refusal, so `settled` stops
		// disagreeing with `would-run`.
		return rep, nil
	}
	// Liveness gate (the 2026-09-10 incident): a worktree some LIVE process
	// still works inside is not recyclable — `git worktree remove` would tear
	// the directory out from under a running session (bogus package-load
	// failures, work loss on the --force path). Reported, never silent.
	if _, why := finishCleanupWorktreeInUse(wt); why != "" {
		// The refusal is checked against its INPUT before it is trusted (epic
		// worktree-reclaim-two-gaps, Gap A): a cwd-holder that is merely an
		// ORPHANED cwd-inheritor — reparented to init, its launcher record
		// dead, no live gopi process left in its session — vetoed this
		// directory forever. When EVERY holder is that, the directory is
		// recyclable and the release is REPORTED (never silent).
		if _, orphanWhy := finishCleanupOrphanCwdInheritorReason(wt); orphanWhy != "" {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: orphanWhy})
		} else {
			rep.Skipped = true
			rep.SkipReason = why
			return rep, nil
		}
	}
	// Ownership gate (2026-09-10 epic): the resolved worktree must be the
	// one THIS row's deliverable claims — and no OTHER row's claim. The
	// resolution above may fall back to "the branch's linked worktree",
	// which a re-attached directory can make another row's ground; a
	// mismatch is a SKIP naming both paths, never a removal.
	if why := finishCleanupOwnerRowGate(rep.TaskID, d.Worktree, wt); why != "" {
		rep.Skipped = true
		rep.SkipReason = why
		return rep, nil
	}
	// Ledger-liveness gate (2026-09-12 epic): this row is terminal, but the
	// LEDGER may have moved on — a later task legitimately builds its ground
	// at the path this row parked at. While such a row is LIVE
	// (pending/claimed/in_progress) the directory is an active session's
	// working tree, and this close-out is the wrong arm to collect it: the
	// removal here is the same `worktree remove` that tears a live run's
	// ground out from under it. An unreadable ledger refuses too — this gate
	// exists to protect a live session, so an unknown ledger SKIPS, never
	// proceeds (fail-soft = report and keep).
	if why := finishCleanupLiveOwnerGate(ctx, wt, rep.TaskID); why != "" {
		rep.Skipped = true
		rep.SkipReason = why
		return rep, nil
	}

	if planOnly {
		// PLAN MODE: the removal is the chain's outcome when nothing refuses it
		// — recorded as the action it WOULD take (marker-prefixed), with no git
		// process run and no registration dropped.
		//
		// ①a THE BRANCH'S OWN CHECK is predicted in the same breath, because
		// it sits AHEAD of the removal and a plan that omitted it would report a
		// sequence the applied arm does not perform. A dry run runs nothing: the
		// row is the PREDICTION, and its caveat says which part of it is not
		// knowable from here.
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchCheckStep, Target: branch, OK: true,
			Detail: finishCleanupPlanMarker + "run: the branch's own test run at the revision being handed over " +
				"(packages from " + branch + "'s diff against the integration branch, through the repo's declared scoped entry point) — " +
				"recorded, never blocking"})
		// THE COMPLETION FACT IS ASKED IN PLAN MODE TOO, and this is the wiring
		// the FIRST Rule 31 run of this fix exposed: the dry run printed
		// "would run: git worktree remove" for a row whose branch was NOT in the
		// integration branch. The plan path reached the removal through
		// finishCleanupRemovalStateReason (dirt + ownership) and never asked the
		// question the applied path now owes — so the prediction promised an
		// action the execution would refuse, which is worse than no prediction:
		// an operator reads "would run" as "this is what happens".
		if why := finishCleanupWorkCompletionReason(ctx, d.Repo, wt, branch, rep.TaskID, IntegrationBranchName); why != "" {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: finishCleanupPlanMarker + why})
			rep.Skipped = true
			rep.SkipReason = why
			return rep, nil
		}
		// The REMOVAL-side state then decides the removal's own row — measured
		// against a THROWAWAY report, so the prediction does not print the same
		// step twice (the reason reader records the untracked-residue note as a
		// step; the plan renders its own predicted rows below). The throwaway
		// carries THIS row's id: every live-row lookup excludes the row being
		// closed out, and an empty id would exclude nobody — the plan would
		// refuse on the closing row's own ledger entry, which the applied arm
		// does not.
		probe := &FinishCleanupReport{TaskID: rep.TaskID}
		if reason := finishCleanupRemovalStateReason(ctx, probe, wt); reason != "" {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: finishCleanupPlanMarker + reason})
			rep.Skipped = true
			rep.SkipReason = reason
			return rep, nil
		}
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, OK: true,
			Detail: finishCleanupPlanRemoveDetail + " " + wt})
		return rep, nil
	}

	// ① THE BRANCH'S OWN CHECK, ASKED BEFORE THE DIRTY GATE BELOW, WHICH IS THE
	// MEASURED BREAKER (task_finish_premerge.go,
	// finishCleanupBranchCheckVerify). The dirty gate returns `rep.Skipped` on a
	// condition that has nothing to do with the branch's own tests — one
	// untracked file is enough — so a check that sat after it was never reached
	// and became a silent no-op while the close-out read as routine hygiene.
	// What stands here is the ordinary human flow: run your own tests at the
	// revision you are handing over, then hand it over.
	//
	// IT MARKS, IT DOES NOT BLOCK. This position used to hold an ALIGNMENT
	// PRECONDITION (the client merging the peer's master into the delivered
	// branch, rebuilding the merged tree, vetting it) and a PUBLISHED-ANCHOR
	// GATE, both of which could stop the arm here and release nothing. Both are
	// gone: the client no longer merges, and whether a branch merges is the
	// merger's decision on the integration branch. So the verdict is recorded on
	// the row's own trail and the arm proceeds — the gate that matters for the
	// delivery is the branch's own tests, and it lives here so this face (the
	// SUMMARY / plan-only path that stops before the push gate) reports it too.
	//
	// The position is still the SAFE one (Rule 29): everything above proved
	// this directory is this row's own ground and no other live row claims it.
	checkGround := wt
	if checkGround == "" && finishCleanupOwningWorktree(ctx, d.Repo) != "" {
		checkGround = d.Repo
	}
	verify := finishCleanupBranchCheckVerify(ctx, d.Repo, checkGround, branch)
	rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchCheckStep, Target: branch,
		OK: verify.Status != finishCleanupPreMergeStatusRefused, Detail: verify.Detail})
	finishCleanupBranchCheckReason(rep.TaskID, verify)

	// GONE-DIRECTORY / DIRT — the REMOVAL's own question ("may this directory
	// be removed?"), asked through ONE helper so the plan path and the applied
	// path cannot drift. It sits AFTER the alignment on purpose: it used to sit
	// before everything and RETURN, which is the measured breaker (one untracked
	// file turned the whole close-out into a silent no-op). It is now asked
	// where the removal is decided, and its answer keeps the directory — it
	// never releases it.
	removeStateWhy := finishCleanupRemovalStateReason(ctx, rep, wt)
	// THE RESCUE (epic 01a11548, the operator's directive). A dirty ground was
	// refused FOR EVER: the directory stayed, no sweep collected it, and the
	// only exit was a human deleting a file (measured 2026-10-07: three live
	// rows, refused=1). So before a DIRT reason is allowed to end the arm, the
	// residue is RESCUED — its index-visible content into a ref this repo can
	// address for ever (refs/gopi/residue/…), its git-ignored content into a tar
	// bundle inside the run's own session directory, the whole act recorded on
	// the row's trail (path + size + sha256) — and only a CLEARED rescue drops
	// the reason. A rescue that could not run leaves it untouched and the
	// refusal exactly as it was (task_finish_cleanup_salvage.go). Asked HERE,
	// with the removal's own question, so the plan path and the applied path
	// cannot disagree (Rule 14).
	if removeStateWhy != "" && finishCleanupDirtyReasonIsResidue(removeStateWhy) {
		note, cleared, serr := finishCleanupSalvageGround(ctx, wt, rep.TaskID, branch)
		switch {
		case serr != nil:
			// Reported, never swallowed; the refusal stands and its reason now
			// carries WHY the rescue could not run (Rule 15).
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupSalvageStep, Target: wt, Detail: serr.Error()})
			removeStateWhy += finishCleanupSalvageFailedNote + serr.Error()
		case cleared:
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupSalvageStep, Target: wt, OK: true, Detail: note})
			removeStateWhy = ""
		default:
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupSalvageStep, Target: wt, Detail: note})
		}
	}
	if removeStateWhy != "" {
		rep.Skipped = true
		rep.SkipReason = removeStateWhy
		return rep, nil
	}

	// ①a THE BRANCH'S OWN CHECK and ② THE BRANCH'S OWN CORPUS are asked by the
	// DELIVERY arm, at the delivery point (see the `!branchPresent` sibling
	// below): this face reaches the removal through the summary path and stops
	// before the push gate, so it reports what it knows and removes.

	// THE RUN-IDENTITY GUARD (task_finish_cleanup_run_identity.go, SL3-2): the
	// checklist above answers "is this ground MINE?" from the ASKER's position;
	// this asks the question that incident turned on — "is a RUN still standing on
	// it?" — over the ground's OWN anchor record, which the terminal transition
	// cannot erase. The measured incident: the closing row was terminal with
	// proc_pid = 0 while the real run's process still stood in the directory.
	if why := finishCleanupRunIdentityRefusal(wt, branch); why != "" {
		rep.Skipped = true
		rep.SkipReason = why
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
		finishCleanupPreMergeRefusedReason(rep.TaskID, why+finishCleanupDeliveryBlockedMarker)
		return rep, nil
	}
	// THE COMPLETION FACT (finishCleanupWorkCompletionReason) — the gate this
	// arm owed the row all along, and the one whose absence removed the ground
	// of branch fix/stash-guard-compound-chain at 22:52:20 on 2026-09-21 while
	// it was five commits ahead and two live rows still named it. Everything
	// above this line is a statement about a MERGE (the sync landed, the branch
	// shares history with the integration branch); a removal needs a statement
	// about the WORK, and this is where it is asked — after the alignment, so a
	// refusal still leaves the branch DELIVERED (the sync's side effects are
	// kept, exactly as the ownership gates above are placed for), and before the
	// audit record, so no removal is ever recorded on an unfinished row.
	if why := finishCleanupWorkCompletionReason(ctx, d.Repo, wt, branch, rep.TaskID, IntegrationBranchName); why != "" {
		rep.Skipped = true
		rep.SkipReason = why
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
		finishCleanupPreMergeRefusedReason(rep.TaskID, why+finishCleanupDeliveryBlockedMarker)
		return rep, nil
	}
	// THE DETERMINISTIC CHECKLIST (reclaim_ground_checklist.go) is asked HERE,
	// last — immediately before the audit record and the removal it authorises
	// (WorktreeRemove records the act; false makes it plan-only) — and it did not
	// always sit here. It used to run between the delivery-point sync and the
	// RUN-IDENTITY GUARD above, and both of those asked their own questions in
	// between, so the checklist's call ended up 41 lines above the removal it
	// guards. At the gate's 40-line window that made a GUARDED arm read as
	// unguarded — and a gate that reads a guarded arm as unguarded is not a
	// stricter gate, it is one whose red no longer means "nobody asked". The two
	// arrangements are behaviourally IDENTICAL, which is the point: every refusal
	// between them keeps the worktree and returns unchanged, so no rung's answer
	// changes and none is skipped — only the distance to the removal does. This
	// IS the removal's own question ("is this ground mine?"), so it belongs
	// against the removal; the completion fact above is what runs FIRST, because
	// it is the one whose absence caused the 2026-09-21 removal.
	if why := reclaimGroundChecklistVerdictForCloseout(d.Repo, wt, branch); why != "" {
		rep.Skipped = true
		rep.SkipReason = why
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
		finishCleanupPreMergeRefusedReason(rep.TaskID, why+finishCleanupDeliveryBlockedMarker)
		return rep, nil
	}
	// Audit record BEFORE the removal (package removalaudit): this arm's
	// deciding evidence is the delivery chain's own local-sync step having
	// landed (step ② is the standing this removal derives from) AND the
	// deterministic checklist having passed.
	removalaudit.WorktreeRemove(removalaudit.ArmFinishCleanupServer, d.Repo, wt, branch, rep.TaskID,
		finishCleanupRemovalAuditStanding+"checklist "+reclaimGoalChecklistSummary)
	// THE FORCE ARM AT LAST (epic 01a11548). Everything this removal needed has
	// been asked and ANSWERED by the gates above (ownership, ledger liveness,
	// run identity, the completion fact, the checklist), and the one thing that
	// was missing is the flag git's own safety asks for: a bare `worktree remove`
	// REFUSES a directory holding untracked files — which is exactly the state
	// the rescue above just made RETRIEVABLE. Without --force the rescue would
	// clear the reason and git would refuse anyway, so the close-out would still
	// end with the directory alive and no reader could tell why. The flag is
	// taken ONLY on the path that got here through every gate AND through a
	// cleared rescue: nothing is deleted that is not in the rescue ref or the
	// bundle (a --force over an unrescued tree is the data-loss path this whole
	// file is about).
	line, err := finishCleanupGit(ctx, d.Repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, wt, finishCleanupPlanForceFlag)
	if err != nil {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: line})
		return rep, nil
	}
	rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, OK: true, Detail: line})
	return rep, nil
}

// finishCleanupUnreadableGoneWorktreeReason reports whether a skip reason is
// the probe refusal above, naming THIS row's directory, for a directory git
// cannot enter (the chdir marker pair). The path is checked for the same reason
// finishCleanupMissingDirReason's caller checks it: a reason mentioning an
// unrelated directory — a foreign/broken binding — must never read as this
// shape. Derived from the ONE "directory is not there" reading rather than a
// second `stat` of the path (Rule 14), so the two can never disagree.
func finishCleanupUnreadableGoneWorktreeReason(reason, dir string) bool {
	return strings.Contains(reason, finishCleanupUnreadableWorktreeMarker) &&
		strings.Contains(reason, dir) &&
		finishCleanupMissingDirReason(reason)
}

const (
	// fixtureEvidenceNone is the zero value: NOT gone — the pid is a process
	// the kernel still schedules.
	fixtureEvidenceNone fixtureGoneEvidence = iota
	// fixtureEvidenceReleased: the kernel released the pid — its /proc entry is
	// gone, or the read found the incarnation gone mid-read.
	fixtureEvidenceReleased
	// fixtureEvidenceZombie: terminated, awaiting reap (state Z).
	fixtureEvidenceZombie
	// fixtureEvidenceDeadState: the measured transient death state (X).
	fixtureEvidenceDeadState
)

// fixtureGoneEvidence names WHY a pid was judged terminally gone, as a value
// rather than as prose. The guards assert on this value, so a verdict can never
// be accepted (or rejected) by matching a message — measured 2026-10-03: a
// guard that string-matched the detail reddened on a real, correct verdict
// whose wording differed, which is a broken instrument, not a finding.
type fixtureGoneEvidence int

// fixtureProcDeadState is the transient /proc state between 'Z' and a released
// pid, measured during exit. It is named rather than typed (Rule 1) because it
// is the character the non-monotone window turns on.
const fixtureProcDeadState = "X"

// fixtureProcStartTicks reads /proc/<pid>/stat's starttime field.
func fixtureProcStartTicks(pid int) (string, bool) {
	b, err := os.ReadFile(fmt.Sprintf("%s/%d/%s", procRootName, pid, procStatName))
	if err != nil {
		return "", false
	}
	fields := fixtureProcStatFields(b)
	if len(fields) < procStatStarttimeIndex-2 {
		return "", false
	}
	return fields[procStatStarttimeIndex-3], true
}

// fixtureProcStatFields splits the whitespace fields that FOLLOW the
// "pid (comm)" prefix, whose comm may itself contain spaces and parentheses —
// the last-')' discipline pidIsZombie uses, shared so both readers break the
// line the same way.
func fixtureProcStatFields(b []byte) []string {
	start := -1
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == ')' {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	var fields []string
	for i := start; i < len(b); {
		for i < len(b) && (b[i] == ' ' || b[i] == '\n') {
			i++
		}
		j := i
		for j < len(b) && b[j] != ' ' && b[j] != '\n' {
			j++
		}
		if j > i {
			fields = append(fields, string(b[i:j]))
		}
		i = j
	}
	return fields
}

// fixtureProcStateLetter returns /proc/<pid>/stat's state character — the first
// field after the last ')' — and whether it was parsable.
func fixtureProcStateLetter(b []byte) (string, bool) {
	fields := fixtureProcStatFields(b)
	if len(fields) == 0 || len(fields[0]) == 0 {
		return "", false
	}
	return fields[0][:1], true
}

// fixtureSignalZeroVerdict spells signal 0's outcome for a refusal message.
func fixtureSignalZeroVerdict(pid int) string {
	if syscall.Kill(pid, 0) == nil {
		return "still reaches the pid"
	}
	return "no longer reaches the pid"
}

// fixtureStateEvidence is the table itself — one switch, consulted by both the
// boolean contract above and the evidence-carrying reader, so the two cannot
// disagree (Rule 14).
func fixtureStateEvidence(state string) (fixtureGoneEvidence, string) {
	switch state {
	case procZombieState:
		return fixtureEvidenceZombie, "terminated, awaiting reap (state Z — not schedulable)"
	case fixtureProcDeadState:
		return fixtureEvidenceDeadState, "transient death state (X) — the kernel is releasing the pid"
	}
	return fixtureEvidenceNone, fmt.Sprintf("state %s is still a process the kernel schedules", state)
}

// fixtureTerminalEvidence is the ONE reader both the reap and its guards use:
// it reports whether the pid is terminally gone, WHAT the evidence was, and the
// measured detail for a refusal (Rule 15: a verdict carries its reason).
func fixtureTerminalEvidence(pid int) (bool, fixtureGoneEvidence, string) {
	if pid <= 1 {
		return true, fixtureEvidenceReleased, "no fixture process to reap"
	}
	b, err := os.ReadFile(fmt.Sprintf("%s/%d/%s", procRootName, pid, procStatName))
	if err != nil {
		if isReleasedProcError(err) {
			return true, fixtureEvidenceReleased, fmt.Sprintf("the kernel released the pid (/proc entry gone: %v)", err)
		}
		// A read that failed for any OTHER reason is not evidence that the pid
		// is running — and it is not evidence it is gone either. The honest
		// verdict is "not judged": the caller keeps waiting and the refusal
		// names the measured error (Rule 15). Declaring a pid gone on no
		// evidence is exactly the leap this file exists to remove.
		return false, fixtureEvidenceNone, fmt.Sprintf("the pid's incarnation cannot be observed (%v)", err)
	}
	state, ok := fixtureProcStateLetter(b)
	if !ok {
		// Same discipline as an unobservable stat: an unparsable state field is
		// a failure to observe, never a licence to call the pid gone.
		return false, fixtureEvidenceNone, "stat carried no parsable state field"
	}
	evidence, why := fixtureStateEvidence(state)
	return evidence != fixtureEvidenceNone, evidence, why
}

// gitRevParse is a one-line porcelain reader for the assertions above.
func gitRevParse(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out := preMergeGit(t, dir, append([]string{"rev-parse"}, args...)...)
	return strings.TrimSpace(out)
}

const (
	// leakShimAlias is the ssh-looking peer name the shim is installed under.
	// It must NOT be resolved by the real ssh (which would ask DNS/known_hosts
	// and fail for an unknown host in a way that depends on the host's
	// configuration) — the shim on PATH answers it first.
	leakShimAlias = "gopi-leak-shim"

	// leakShimBranch is the fixture branch whose landing in the real repo was
	// the measured leak. Named here so this file compiles and reads the same
	// against the unfixed code (the fix's own constant, leakFixtureBranch, is
	// introduced by the very commit this probe must run without).
	leakShimBranch = "feat/sync-conflict-completed"

	// leakShimPeerAlias is the peer the leaking arm declares: the server's own
	// alias, which resolves to this host and therefore to this checkout.
	leakShimPeerAlias = "peer1"

	// leakShimScript is the checked-in shim body, run through the `sh` on the
	// system so the test never depends on the file's own exec bit.
	leakShimScript = "testdata/ssh-sentinel-shim.sh"

	// leakShimLogEnv / leakShimRepoEnv are the shim's inputs: where to append
	// the ssh argv it was handed, and the bare repo it must route the receiver
	// to.
	leakShimLogEnv  = "GOPI_SSH_SENTINEL_LOG"
	leakShimRepoEnv = "GOPI_SSH_SENTINEL_REPO"

	// gitSSHCommandEnv is git's own ssh-command override; belt and braces with
	// the PATH shim, so the route is taken whichever way git picks its ssh.
	gitSSHCommandEnv = "GIT_SSH_COMMAND"
)

// installLeakSSHShim writes the wrapper, prepares the sentinel bare repo, and
// routes every ssh git spawns through the sentinel for the duration of the
// test (t.Setenv, Rule 17).
func installLeakSSHShim(t *testing.T) *leakSSHShim {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not installed")
	}
	script, err := filepath.Abs(leakShimScript)
	if err != nil {
		t.Fatalf("resolve shim path: %v", err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("shim %s missing: %v", script, err)
	}

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir shim dir: %v", err)
	}
	sshPath := filepath.Join(binDir, "ssh")
	// A wrapper rather than the script itself: the committed file then needs no
	// exec bit and no shebang handling by git.
	wrapper := "#!/bin/sh\nexec sh " + shellQuotePath(script) + " \"$@\"\n"
	if err := os.WriteFile(sshPath, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write shim ssh: %v", err)
	}

	logPath := filepath.Join(root, "ssh.log")
	repo := filepath.Join(root, "sentinel.git")
	pushContractGit(t, root, "init", "--bare", "--quiet", repo)

	t.Setenv(leakShimLogEnv, logPath)
	t.Setenv(leakShimRepoEnv, repo)
	t.Setenv(gitSSHCommandEnv, sshPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return &leakSSHShim{alias: leakShimAlias, log: logPath, repo: repo}
}

// isReleasedProcError reports whether a /proc/<pid>/stat read failed because the
// incarnation is gone. BOTH errors occur, and the second is the one a
// fs.ErrNotExist-only check misses — measured 2026-10-03 inside this package's
// own suite: `read /proc/<pid>/stat: no such process` (ESRCH), i.e. the open()
// resolved the path and the read() then found the pid released. The kernel's
// message for the two cases differs; the meaning — this incarnation cannot be
// observed again — does not.
func isReleasedProcError(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// lastStepNamed returns the LAST report step with the given name (steps are
// appended in order; a later append supersedes an earlier one).
func lastStepNamed(t *testing.T, rep *FinishCleanupReport, name string) FinishCleanupStep {
	t.Helper()
	for i := len(rep.Steps) - 1; i >= 0; i-- {
		if rep.Steps[i].Step == name {
			return rep.Steps[i]
		}
	}
	t.Fatalf("no %q step in the report: %+v", name, rep.Steps)
	return FinishCleanupStep{}
}

// leakSSHShim is an installed sentinel: the alias the chain is pointed at, the
// log of every ssh argv, and the bare repo the receiver writes to.
type leakSSHShim struct {
	alias string
	log   string
	repo  string
}

// invocations returns the ssh argv log, or nil when the shim was never called.
func (s *leakSSHShim) invocations(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(s.log)
	if err != nil {
		return nil // no file: no ssh ran at all
	}
	body := strings.TrimSpace(string(b))
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

// leakShimRefState reads refs/heads/<branch> in repo: its object name, or ""
// when the ref is absent. A real git failure is reported rather than flattened
// into "absent" — an unreadable sentinel would otherwise read as a pass.
func leakShimRefState(t *testing.T, repo, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "" // absent: --verify --quiet exits 1 with no output
		}
		t.Fatalf("read %s refs/heads/%s: %v", repo, branch, err)
	}
	return strings.TrimSpace(string(out))
}

// liveCwdHolder starts a child process sitting INSIDE dir (CWD = dir, the
// minimal shape of the live session / go test the guard protects) and
// returns its pid; the process is killed at test cleanup.
func liveCwdHolder(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], holderChildArgRun)
	cmd.Dir = dir
	// The child is a SUITE binary: its TestMain pins a scratch root of its
	// own. Pin that root INSIDE this test's fixture, so the holder's scratch
	// is the test's own — the creator cleans up what it created. Without the
	// pin the holder pins a root in the SHARED scratch home and the reaper
	// kills it before its own TestMain can remove that root: one orphan
	// suite-tools-run-* directory per spawned holder (measured: 50 such
	// entries after a handful of runs), collected only by the 6h age sweep.
	cmd.Env = append(os.Environ(), holderEnvMarker+"=1",
		testutil.SuiteTempRootEnvName+"="+t.TempDir(),
		testutil.TempDirEnvName+"="+t.TempDir())
	mp := testutil.StartMockProcessCmd(t, cmd)
	waitProcCwd(t, mp.Pid(), dir)
	return mp.Pid()
}

// mainRepoFixture is the shape the ticket measures: a MAIN checkout (on the
// integration branch) that holds a task branch, plus a SEPARATE linked worktree
// where the run's own work happens. The run's ground therefore has nothing to do
// with the main checkout — which is exactly why the existing dirty arms cannot
// see the main repo's dirt.
type mainRepoFixture struct {
	*finishCleanupFixture
}

// newFinishCleanupFixture builds the fixture with real git commands (the
// chain is git-semantics-dependent — mocks would prove nothing).
func newFinishCleanupFixture(t *testing.T) *finishCleanupFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	f := &finishCleanupFixture{
		t:          t,
		serverDir:  filepath.Join(root, "server.git"),
		clientDir:  filepath.Join(root, "client"),
		wtDir:      filepath.Join(root, "wt"),
		featBranch: "feat/sync-conflict-completed",
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git(root, "init", "--bare", f.serverDir)
	git(root, "init", f.clientDir)
	git(f.clientDir, "config", "user.email", "t@t")
	git(f.clientDir, "config", "user.name", "t")
	git(f.clientDir, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(f.clientDir, "a.txt"), []byte("hello"), 0o644)
	git(f.clientDir, "add", "a.txt")
	git(f.clientDir, "commit", "-m", "init")
	git(f.clientDir, "remote", "add", "server", f.serverDir)
	git(f.clientDir, "push", "server", "master")
	// feature branch with one more commit, checked out in a linked worktree
	git(f.clientDir, "checkout", "-b", f.featBranch)
	os.WriteFile(filepath.Join(f.clientDir, "b.txt"), []byte("deliverable"), 0o644)
	git(f.clientDir, "add", "b.txt")
	git(f.clientDir, "commit", "-m", "deliverable work")
	git(f.clientDir, "checkout", "master")
	// main checkout back on master BEFORE the worktree add (a branch can
	// only be checked out in one place at a time)
	git(f.clientDir, "worktree", "add", f.wtDir, f.featBranch)
	return f
}

// newMainRepoFixture reuses the finish-cleanup fixture wholesale (real git, a
// bare "server", a linked worktree on the feature branch) — the main checkout is
// its clientDir, and it is on master with a clean tree. Only the assertion
// surface is new, so no second fixture vocabulary is invented.
func newMainRepoFixture(t *testing.T) *mainRepoFixture {
	t.Helper()
	return &mainRepoFixture{finishCleanupFixture: newFinishCleanupFixture(t)}
}

// newOrphanCwdHolder builds the shape. record=true gives the holder the gopi
// launcher record (the gopi-run orphan); record=false gives it none (the
// operator-shell shape, which must keep the veto).
func newOrphanCwdHolder(t *testing.T, dir string, record bool) *orphanFixture {
	t.Helper()
	dead := deadPidOnThisHost(t)
	env := []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + os.Getenv("HOME"),
		orphanHolderEnvMarker + "=1",
	}
	if record {
		// The launcher record: a gopi executor/agent pid that is ALREADY DEAD
		// when the holder is born. That is not a contrivance — it is exactly
		// what a real orphan carries (measured 2026-09-23: three live orphans
		// whose recorded wrapper/anchor pids had been dead for hours to days).
		env = append(env,
			finishCleanupHolderEnvWrapperPID+"="+strconv.Itoa(dead),
			finishCleanupHolderEnvAnchor+"="+strconv.Itoa(dead))
	}
	launcher := exec.Command("/bin/sh", "-c", orphanLauncherScript, "sh", dir, os.Args[0])
	launcher.Env = env
	// A NEW SESSION for launcher + holder (setsid): the fixture then judges a
	// session of its own, and killing the launcher leaves that session with a
	// dead leader — the state the classifier reads as "no live gopi here".
	launcher.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := launcher.Output()
	if err != nil {
		t.Skipf("orphan fixture: cannot run the launcher here (%v)", err)
	}
	holder, convErr := strconv.Atoi(strings.TrimSpace(string(out)))
	if convErr != nil || holder <= 0 {
		t.Fatalf("orphan fixture: the launcher reported %q, want a pid", strings.TrimSpace(string(out)))
	}
	f := &orphanFixture{holderPid: holder, launcherPid: launcher.Process.Pid, deadPid: dead}
	// The holder's cwd must be observable before ancestry is judged — the same
	// precondition the suite's other holder fixtures wait on.
	waitProcCwd(t, holder, dir)
	t.Logf("orphan fixture: holder=%d launcher=%d recorded-launcher-record=%d (dead) cwd=%s",
		f.holderPid, f.launcherPid, f.deadPid, dir)
	return f
}

// orphanFixture is one built shape: a holder sitting in a worktree, its
// launcher killed, with a launcher record the test controls.
type orphanFixture struct {
	holderPid   int
	launcherPid int
	deadPid     int // the pid the holder's launcher RECORD names (measured dead)
}

// becomesOrphan waits until the kernel has handed the holder to one of THIS
// host's reparent targets — the state the classifier is allowed to consult. A
// test that skipped this wait would judge a still-parented child.
//
// It replaces a wait on the literal pid 1, which never succeeded on a systemd
// user session and turned the premise into a green SKIP (see
// task_finish_cleanup_reparent_targets_test.go for the measurement).
//
// No kill is needed: the launcher's script backgrounds the holder and exits, so
// the launcher is GONE (and reaped by the caller) before this is called, and the
// only thing left to observe is the reparenting itself. `launcherPid` is kept
// for the diagnostic line only.
func (f *orphanFixture) becomesOrphan(t *testing.T) {
	t.Helper()
	waitOrphanReparentTarget(t, f.holderPid, orphanSettleWait, "holder")
	// The fixture's premise: the recorded launcher pid is (still) dead. A
	// recycled pid would make the record read as a LIVE launcher and the test
	// would measure the wrong thing — refuse the measurement rather than
	// report a false verdict.
	if pidAlive(f.deadPid) {
		t.Skipf("orphan fixture: the recorded pid %d was recycled — the record cannot be staged", f.deadPid)
	}
}

// collect registers the holder's removal, so no state survives the test and the
// directory is free for whatever the test does next.
func (f *orphanFixture) collect(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		// Killed, then REAPED to terminal death by the shared predicate
		// (task_finish_cleanup_fixture_reap_test.go) — never judged by a single
		// pidAlive() snapshot, whose exit window is non-monotone.
		syscall.Kill(f.holderPid, syscall.SIGKILL) // stylegate:ignore best-effort: the reap below measures whether it died
		finishCleanupFixtureReapChild(t, f.holderPid, "orphan fixture")
	})
}

const (
	// orphanHolderEnvMarker identifies the child process as the orphan-candidate
	// holder. The helper's ONLY job is to sit in the cwd its parent gave it.
	orphanHolderEnvMarker = "GOPI_FINISH_CLEANUP_ORPHAN_HOLDER"
	// orphanHolderRunArg re-execs exactly the holder test in the child binary.
	orphanHolderRunArg = "-test.run=TestHelperOrphanCwdHolder"
	// orphanLauncherScript is the launcher body. It chdir's into the worktree,
	// starts the holder there (so the child's cwd is the worktree), prints the
	// child's pid, and exits — leaving the holder to the kernel. $1 = worktree,
	// $2 = this test binary.
	//
	// The holder's own stdin/stdout/stderr go to /dev/null on purpose: it
	// INHERITS its launcher's descriptors, so without this the launcher's
	// stdout pipe would stay open for the holder's whole lifetime and the
	// caller's Output() would block until the fixture timed out (measured).
	orphanLauncherScript = `cd "$1" || exit 1; "$2" ` + orphanHolderRunArg + ` >/dev/null 2>&1 </dev/null & echo $!`
	// orphanHolderProbeRunArg is how the fixture mints a pid it has MEASURED to
	// be dead (the launcher record's value).
	orphanHolderProbeRunArg = "-test.run=TestHelperNeverRuns"
	// orphanSettleWait bounds the wait for the kernel to reparent the holder to
	// init once its launcher is killed.
	orphanSettleWait = 10 * time.Second
	// orphanSettlePoll is the poll interval of the waits below.
	orphanSettlePoll = 20 * time.Millisecond
	// orphanStartWait bounds the wait for the launcher to report a child pid.
	orphanStartWait = 30 * time.Second
)

// orphanReparentPids renders this host's reparent targets in ascending order,
// for the failure messages — a reader must be able to see WHICH pids the wait
// rejected rather than only the one it expected.
func orphanReparentPids(t *testing.T) ([]int, string) {
	t.Helper()
	targets, err := finishCleanupOrphanReparentTargets()
	if err != nil {
		t.Fatalf("orphan fixture: this host's reparent targets cannot be measured (%v) — "+
			"an unmeasurable premise is never a pass, and it is never a skip either (Rule 15)", err)
	}
	if len(targets) == 0 {
		t.Fatalf("orphan fixture: this host reports NO reparent target at all — " +
			"the kernel always has a namespace init to hand orphans to, so an empty set means the reading is broken")
	}
	pids := make([]int, 0, len(targets))
	for pid := range targets {
		pids = append(pids, pid)
	}
	for i := range pids {
		for j := i + 1; j < len(pids); j++ {
			if pids[j] < pids[i] {
				pids[i], pids[j] = pids[j], pids[i]
			}
		}
	}
	text := ""
	for i, pid := range pids {
		if i > 0 {
			text += ", "
		}
		text += strconv.Itoa(pid)
	}
	return pids, text
}

// orphanReparentPoll is the poll interval of the shared wait above — the cadence
// at which the kernel's reparenting is observed. It is short because the event it
// waits for is a kernel action that completes in microseconds; the BUDGET is what
// bounds the wait, and it belongs to each caller (the fixtures' existing settle
// budgets: orphanSettleWait, orphanTreeSettleWait, sharedSessionSettleWait).
const orphanReparentPoll = 20 * time.Millisecond

// ownershipSeedRow inserts a task row carrying deliverable.worktree (the
// ownership record the gate scans).
func ownershipSeedRow(t *testing.T, db, taskID, status, claimedBy, worktree string) {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	deliverable := `{"repo":"/tmp/x","worktree":"` + worktree + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, claimed_by, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'auto', ?, ?)`,
		taskID, "owns "+worktree, status, claimedBy, deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed row %s: %v", taskID, err)
	}
}

// live-CWD scan wait tunables (Rule 1): a freshly started holder's procfs
// CWD appears asynchronously, so the test polls a bounded number of times.
const (
	procScanWaitAttempts = 50
	procScanWaitInterval = 20 * time.Millisecond
	// procHolderHoldTime is how long the re-exec'd holder blocks in its CWD
	// (far beyond any single test's runtime; cleanup kills it regardless). It
	// is also the BOUND of the post-kill exit wait (stopHolder): a killed
	// holder's procfs entry disappears at once, so the bound is never reached
	// on a healthy host.
	procHolderHoldTime = 60 * time.Second
	// procHolderExitPollPeriod is the poll interval of the walks this file makes while a holder settles.
	procHolderExitPollPeriod = 10 * time.Millisecond
)

// reclaimSelfDeliveryLine LIVED HERE (Rule 43, deleted 2026-10-31). It rendered
// the close-out chain's ②③④ verdict for one freed row as a single line, and its
// ONLY caller was `task_reclaim_self_delivery_test.go`. When that test file and
// its subject (`task_reclaim_self_delivery.go`, which declared nothing but the
// two marker strings this helper renders) were deleted — the alone-deletion
// probe found them DEAD-ALONE with zero production references — the helper lost
// its last caller and went in the same commit rather than sitting here unused.
//
// Reinstate it together with a caller, and re-derive it from the object store:
//   git show c49dc227:internal/tools/finish_cleanup_moved_helpers_test.go

// reset turns the fixture's state into a KNOWN baseline: the chain's own delivery
// steps (push → worktree remove → branch delete) are not the subject here, so
// each case only asserts what the MAIN-repo gate did.
func (f *mainRepoFixture) reset(t *testing.T) {
	t.Helper()
	// The main checkout must be on master for the "clean main" case to be the
	// happy path; the shared fixture leaves it there.
	gitRun(t, f.clientDir, "checkout", "master")
	gitRun(t, f.clientDir, "status", "--porcelain")
}

// runChain executes the chain with the sync-peer gate defused: the fixture
// world has no real ssh peers, so the target override points the push at
// the bare "server" (target injection = the same seam gitPushback uses).
func (f *finishCleanupFixture) runChain() *FinishCleanupReport {
	f.t.Helper()
	f.t.Setenv(finishCleanupExemptEnv, "")
	rep, _ := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: "test", Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, f.serverDir, false) //stylegate:ignore test helper: the report is the subject, the error is not asserted here
	return rep
}

// runFinishCleanupChainTarget runs the chain body with an explicit push
// target (the injection seam shared by tests and the production resolver).
func runFinishCleanupChainTarget(ctx context.Context, rep *FinishCleanupReport, d taskFinishDeliverable, target string, planOnly bool) (*FinishCleanupReport, error) { // The report carries its own mode from HERE, so every entry into the
	// chain body (the ledger-resolving face and this injected-target seam)
	// produces an honest report — a caller reads the flag, never the call site.
	rep.PlanOnly = planOnly
	// THE MAIN REPO'S TWO LINES (epic closeout-reads-the-main-repo) — the same
	// gate the chain's ENTRY asks (taskFinishCleanup, next to the ①a restore),
	// repeated here because THIS is the seam the tests inject a deliverable into:
	// a guard only at the entry would leave every injected-fixture arm
	// unguarded. Both calls share the report's own `mainRepoChecked`, so the two
	// git readings happen once per close-out (Rule 14).
	if skipped, ok := finishCleanupMainRepoGate(ctx, rep, d); !ok {
		return skipped, nil
	}
	branch, brErr := finishCleanupResolveBranch(ctx, rep, d)
	if brErr != nil {
		rep.Skipped = true
		rep.SkipReason = brErr.Error()
		return rep, nil
	}
	if branch == "" {
		return rep, nil // the resolver already named the refusal on the report
	}
	rep.Branch = branch

	// The row's recorded worktree is the removal target; git's own answer
	// (the worktree containing the deliverable's repo, or — when the row
	// recorded a worktree while the repo it names is the main checkout (the
	// manual-run shape) — the branch's LINKED worktree) is resolved to CHECK
	// that record. The branch probe is skipped for a declared-less row: the
	// chain's existing behaviour for it is unchanged (nothing to compare,
	// nothing probed).
	wt := strings.TrimSpace(d.Worktree)
	linked := finishCleanupOwningWorktree(ctx, d.Repo)
	if linked == "" && wt != "" {
		linked = finishCleanupBranchWorktree(ctx, d.Repo, branch)
	}
	if wt == "" {
		wt = linked
	}
	rep.Worktree = wt

	// Ownership gate ① (2026-09-10 epic): the row's record and git's answer
	// must name the SAME directory. A disagreement means the branch (or the
	// row's repo) is bound to a directory this row does not claim — a
	// re-attached worktree is another row's ground.
	//
	// WHAT IT REFUSES (changed by the client-push-rebase epic, 2026-09-19):
	// the DIRECTORY — never entered, never probed, never removed. It has no
	// business stopping the DELIVERY: the push moves a ref on the peer and
	// nothing from the working tree, so the foreign-directory condition is
	// recorded as the REMOVAL's reason and the chain delivers (Rule 15: the
	// reason is kept verbatim and reported — only its reach changed). Before
	// this epic the gate returned at the head of the chain (`rep.Skipped`),
	// so a re-attached directory cost the branch its delivery as well.
	//
	// The probe runs ONLY on ground this row owns: reading a foreign
	// directory's porcelain and reporting it as this row's dirt is exactly what
	// gate ① exists to refuse (finishCleanupRemovalReason sequences the two).
	//
	// The PLAN computes the same reason against a THROWAWAY report: the reason
	// reader records the untracked-residue note as a step (its durable form),
	// and a plan renders its own predicted rows below — recording into the plan
	// would print the same step twice, once bare and once marked as a
	// prediction.
	reasonRep := rep
	if planOnly {
		reasonRep = &FinishCleanupReport{}
	}
	removeWhy := finishCleanupRemovalReason(ctx, reasonRep, d, wt, linked)

	// THE ABSENT GROUND IS ASKED ABOUT BEFORE IT IS ACTED ON — the SAME call the
	// server arm makes (see finishCleanupReclaimedGone and that arm's comment for
	// the measured defect, task 01a18a04-04e6-7ecb-9b49-f53236a477c9).
	//
	// WHY IT SITS HERE AND NOT EARLIER: the ownership gate above must speak FIRST.
	// Its question — "is this row's binding and git's answer the same directory?"
	// — is what tells a FOREIGN binding (a hard refusal, with both paths named)
	// from a ground a recorded arm freed; a directory that is gone has no second
	// side, so pre-empting the gate with the record would report a recycled path
	// as a reclaim for a row whose binding points elsewhere entirely.
	//
	// WHY NOT LATER: every arm below consumes `removeWhy`, and for a gone
	// directory that sentence is "the recorded worktree directory is not there —
	// nothing to remove (delivery not established …)" — a LOSS sentence for a
	// ground an arm deliberately finished, which is exactly the false alarm this
	// file exists to end. It is asked before the no-push-target branch so a
	// single-node host reads the same INFO, and before the delivery premise so the
	// row is not counted as owing anything.
	if reclaimedRep, reclaimed := finishCleanupReclaimedGone(ctx, rep, wt); reclaimed {
		return reclaimedRep, nil
	}

	// NO PUSH TARGET — the single-node deployment, or a declared-but-unusable
	// alias. Nothing can be handed over, so the chain stops; the REMOVAL-side
	// state found above is composed into the reason (the gone-directory
	// diagnostic and its "not handed off to any server" note are one sentence
	// for an operator, and the dead-end words are what a settled reader keys
	// on — see finishCleanupGoneWorktreeNoPeersNote). The check lives here, in
	// the body, rather than in the resolver, because only here is the removal
	// state known.
	if strings.TrimSpace(target) == "" {
		rep.Skipped = true
		rep.SkipReason = noServerTargetSkipReason(finishCleanupNoServerTargetBase)
		if removeWhy != "" {
			rep.SkipReason += finishCleanupReasonSeparator + removeWhy
		}
		return rep, nil
	}

	// IN-PROCESS TEST FENCE — a test must never be able to push a deliverable
	// into a REAL node's repo (Rule 30; epic fix/fixture-leak-temp-root).
	//
	// MEASURED 2026-09-22 on the server itself (peer1): a plain
	// `go test ./internal/tools/ -count=1` drove this body with the target the
	// PRODUCTION resolver renders — `peer1:/home/user/work/GoPi` — from
	// TestFinishCleanupGoneWorktreeNamesTheUndeclaredPeer's "a peer IS
	// declared" arm (t.Setenv(config.SyncPeersEnv, "peer1")). On the server
	// that alias resolves to 127.0.0.1 and `~` IS this checkout, so the
	// fixture's throwaway branch landed in the REAL repository as
	// refs/heads/feat/sync-conflict-completed. GIT_TRACE caught the exact
	// command:
	//
	//	git push 'peer1:/home/user/work/GoPi' feat/sync-conflict-completed:feat/sync-conflict-completed
	//
	// WHY THE RESOLVED DIRECTORY IS THE REAL REPO: neither the peer catalog nor
	// the fixture is at fault — a host may legitimately declare its own alias
	// (this one does), and pushing to a local bare repo is the right fixture
	// shape (the push-contract tests do exactly that). The defect is that the
	// chain ran IN THE TEST PROCESS while its target pointed OUTSIDE the space
	// the run was confined to: the fixture's every artefact — the client repo
	// and the "server" — lives under t.TempDir(), and the resolution of
	// `peer1:/home/user/work/GoPi` does not. The two sides disagree, and the test pushes
	// to the side it does not own.
	//
	// THE CRITERION IS THEREFORE THAT SAME BOUNDARY, and it needs no knowledge
	// of what a "test target" looks like: is the LOCAL side of the target inside
	// a temp/scratch root this process may write? On the loopback host the
	// remote and the local side are the same filesystem, so the fixture's bare
	// repo resolves inside TMPDIR while `/home/user/work/GoPi` resolves outside it — the
	// asymmetry is the whole check. The roots are the ones already in force
	// (Rules 22/30/32) rather than a bespoke "test mode": a shipped binary,
	// whose tmpdir is the operator's workspace, simply never has a test process
	// asking, and the marker opt-out covers RE-EXEC'D test children (real
	// subprocesses, where testing.Testing() is false).
	//
	// This gate never fires for a production run (testing.Testing() is false)
	// and never fires for a test whose target is inside its own temp root, so
	// it costs the push-contract surface nothing (Rule 31).
	if !planOnly {
		if why := finishCleanupTestSpaceRefusal(target); why != "" {
			rep.Skipped = true
			rep.SkipReason = why
			return rep, nil
		}
	}

	// DELIVERY PREMISE (see finishCleanupDeliveryPremise): the one condition
	// under which the push is withheld. It holds exactly when ownership
	// disagrees AND no ground is left for the pre-merge gate — because then the
	// chain would push a branch it never compared against the peer's master.
	// Bounded on purpose: a broader gate (wt == "" for example) would suppress
	// deliveries that happen today, including the single-node shape where the
	// pre-merge gate abstains by design.
	if wt != "" {
		if why := finishCleanupDeliveryPremise(ctx, d, branch, wt, linked); why != "" {
			rep.Skipped = true
			rep.SkipReason = why
			return rep, nil
		}
	}

	// The branch op runs against the MAIN repo dir, never d.Repo itself:
	// when the deliverable's repo field points at the worktree (the live
	// t-1788157112 shape — the executor backfilled repo=worktree dir),
	// step ③ has just removed that directory and `git -C <gone dir>` exits
	// 128 with no output (the "(no output) / -D: (no output)" report) —
	// the branch survived a fully-successful push. Resolve the MAIN repo
	// dir BEFORE ③ (while the worktree still exists) via
	// `rev-parse --git-common-dir`: the shared .git dir outlives the
	// worktree, and its parent is the main checkout `branch` needs.
	branchDir := d.Repo
	if wt != "" && branchDir == wt {
		// PARSED path: stdout only.
		if out, err := finishCleanupGitParsedStdout(ctx, branchDir, finishCleanupGitTimeout,
			gitRevParseVerb, gitParsedPathFormatAbsoluteFlag, gitParsedCommonDirFlag); err == nil {
			common := strings.TrimSpace(out)
			if common != "" && strings.Contains(common, ".git") {
				branchDir = filepath.Dir(common) // <main-repo>/.git → <main-repo>
			}
		}
	}

	// IS THERE ANYTHING TO HAND OVER? A row whose branch ref is not here has
	// nothing to deliver — the ordinary shape of a SECOND close-out pass on a
	// row whose first pass pushed the branch and deleted the local ref. The
	// delivery is then a recorded no-op and the chain goes straight to the
	// removal decision; attempting the push instead would report
	// "src refspec … does not match any" on a row that was delivered fine, and
	// an operator reads a settled row as a failed one.
	branchPresent := finishCleanupBranchExists(ctx, branchDir, branch)

	if planOnly {
		// PLAN MODE: the push is the first ACT — predicted, never run (its
		// effect is a remote ref, which is not this process's to move during
		// an inspection). The branch renderer is the executed argv. The
		// pre-merge gate that precedes it is predicted the same way: a dry run
		// asks no peer and runs no build (no fetch, no merge, no ledger write).
		if !branchPresent {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, Detail: finishCleanupBranchAbsentDeliveryNote})
		} else {
			// THE DELIVERY PENDING — rendered for a row whose branch is STILL
			// HERE, whichever shape the removal half is in. It was rendered only
			// for the `removeWhy != ""` branch before, which is why a row whose
			// recorded worktree was GONE (the state this epic is about) printed
			// `would-run=0 settled=1` while the branch had never reached the
			// peer: nothing in the plan said the delivery was owed, and the
			// settled classifier had no step to discount the absence with.
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchCheckStep, Target: branch, OK: true,
				Detail: finishCleanupPlanMarker + "run: the branch's own test run at the revision being handed over " +
					"(the packages its diff against the integration branch touches, through the repo's declared scoped entry point) — " +
					"recorded on the row's trail, never blocking the push" + finishCleanupPlanGoneGroundCaveat})
			// The PREDICTION carries the same tip the apply path would carry
			// (Rule 13/14: one code path, two modes) — and it is read HERE, in
			// plan mode, by the same resolver the push uses. A plan that cannot
			// resolve the ref predicts the refusal the apply run would take,
			// never a push it cannot make.
			tip := branchDeleteTipOf(ctx, d.Repo, branch)
			if why := finishCleanupPushCarriesTheRefTip(target, branch, tip, tip); why != "" {
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch,
					Detail: finishCleanupPlanMarker + "refused — " + why})
			} else {
				// THE PREDICTION DESCRIBES THE CHAIN --apply RUNS (the
				// 01a13017 defect: a plan rendering a shorter chain than the
				// executed one). The apply path no longer makes a single push —
				// a refusal escalates to the bounded delivery loop — so the
				// prediction names the loop and its limit, and a reader is not
				// told a hand-over is one call when it is up to
				// pushbackDeliveryRounds of align-and-push.
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, OK: true,
					Detail: finishCleanupPlanMarker + "run: git " + finishCleanupPushVerb + " " + target + " " + branch + ":" + branch +
						"; on a refusal the bounded delivery loop (up to " + strconv.Itoa(pushbackDeliveryRounds) +
						" rounds: fetch, classify, align the branch onto the peer, push again)" +
						finishCleanupPushCarriesTipMarker + tip})
			}
		}
		if wt == "" {
			// NO WORKTREE IN THIS PLAN: there is no removal prediction to make
			// (the plan renders one only when it has a directory), so the ④
			// prediction is what is owed — and it must be the SAME prediction
			// the apply path acts on, never silence. Recorded for the same
			// reason the apply path records it: a wt-less row's branch is the
			// only thing this chain can hand back.
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: rep.Worktree,
				Detail: finishCleanupNoGroundStepDetail})
			return finishCleanupDeleteDeliveredBranch(ctx, rep, branchDir, branch, target, planOnly)
		}
		if removeWhy != "" {
			// The REMOVAL is refused (dirt / a foreign binding / a directory
			// that is not there), so the plan renders it as the refused
			// prediction it is — never as a removal that would happen. The
			// delivery prediction above stands: the plan is neither MORE
			// permissive nor more pessimistic than --apply (both deliver, both
			// keep the directory).
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: finishCleanupPlanMarker + removeWhy})
			rep.Skipped = true
			// THE DELIVERY IS ASKED ABOUT, NOT INFERRED (Rule 41). The reason's
			// own head is a statement about the DIRECTORY, and the settled
			// classifier used to read a delivery out of it — so a row whose
			// branch never reached the peer printed `settled=1 would-run=0`.
			// The composer below states the delivery half as the report's OWN
			// push step measured it (absent / refused / succeeded), in the one
			// place both arms read, and `finishCleanupReportSettled` refuses the
			// settled reading for a row that still owes the act.
			rep.SkipReason = finishCleanupRemovalReasonWithDelivery(rep, removeWhy,
				finishCleanupGoneWorktreeDeliveryOwed(rep, removeWhy, branchPresent))
			// THE DELIVERY ROUTE'S OWN STEPS ARE PREDICTED TOO — the defect this
			// paragraph exists for (task 01a13017, measured after 01a12fe6): the
			// plan rendered the push/removal/branch predictions and said NOTHING
			// about the step the executed chain runs on exactly this shape, so
			// the operator's decision surface described a shorter chain than the
			// one --apply performs. The step is predicted with the per-step
			// marker every prediction carries (a plan claims no run), and its
			// own limits are stated in the same breath rather than left for the
			// reader to infer from silence (Rule 15).
			// The prediction follows the EXECUTION's own precondition: an
			// absent branch is not delivered, so no check is asked of it (the
			// executed chain's `!branchPresent` arm records the delivery as a
			// no-op and asks nothing) — a plan that predicted the step here
			// would promise an act the run does not perform.
			if finishCleanupWorktreeDirAbsent(removeWhy) && branchPresent {
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchCheckStep, Target: branch,
					Detail: finishCleanupPlanGoneGroundStep(branch) + finishCleanupPlanGoneGroundCaveat})
			}
			// ④ IS PREDICTED FOR THE GONE-DIRECTORY STATE TOO, exactly as the
			// apply path runs it (the 2026-09-26 leg): the removal has nothing
			// left to protect, so the branch prediction is owed. It stays a
			// PREDICTION — a plan runs nothing — and it is the same call the
			// apply arm makes, so the two faces cannot describe different
			// chains.
			if finishCleanupWorktreeDirAbsent(removeWhy) {
				return finishCleanupDeleteDeliveredBranch(ctx, rep, branchDir, branch, target, planOnly)
			}
			return rep, nil
		}
	} else {
		if !branchPresent {
			// No local ref: nothing to hand over. Recorded as a successful
			// no-op (the delivery half is DONE — either an earlier pass pushed
			// it and step ④ removed the ref, or the work never had a ref to
			// begin with), never as a failed push.
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, OK: true,
				Detail: finishCleanupBranchAbsentDeliveryNote})
			if wt == "" {
				return rep, nil
			}
		} else {
			// ①0 GROUND OWNERSHIP — THE REF-MOVING DECISION POINT (SL3-2's
			// pushback scope, measured 2026-09-26).
			//
			// The ③ removal guard runs at the END of this chain, and until now
			// the delivery half ran BEFORE it: a `gopi task finish-cleanup
			// --apply` on a completed row whose ground was taken by a LIVE
			// FOREIGN run still reached ①a and moved the delivered branch —
			// measured on the real CLI with the pre-fix binary: the branch ref
			// cf49acc → a0fe8fd inside the linked worktree that live run was
			// standing on, while ③ refused afterwards with
			// "nothing was removed and no ref was moved". The refusal line was
			// a false statement, and the live run's own checkout was moved out
			// from under it.
			//
			// So the SAME guard is asked HERE, before the first act that can
			// move a ref or merge into a ground: nothing about this row's
			// delivery is owed to a directory another live run owns — its own
			// close-out is what aligns, pushes and frees it. No second pid
			// ladder: `finishCleanupRunIdentityRefusal` is the one verdict, and
			// asking it earlier only makes the verdict TRUE, because the acts
			// it excuses have not yet happened.
			//
			// The ground is resolved exactly as the REMOVAL phase resolves it
			// (finishCleanupBranchWorktree — the same authoritative record, the
			// same reconciliation), and the two shapes that matter are BOTH
			// covered: a row that records its worktree (`wt`), and a row whose
			// ref is nevertheless checked out in a live run's ground, which the
			// branch registry names even when the row does not. The second shape
			// is the one the real CLI reaches with a `--repo <main>` close-out:
			// measured 2026-09-26, the alignment then moved the branch ref IN THE
			// SHARED MAIN REPO while the live run stood on the linked worktree
			// whose HEAD is that same ref — the run's checkout changed under it
			// while ③ refused afterwards. A guard that only read `wt` missed it,
			// which is why the branch registry is asked too.
			deliveryGround := wt
			if deliveryGround == "" {
				deliveryGround = finishCleanupBranchWorktree(ctx, finishCleanupMainRepoDir(d.Repo), branch)
			}
			if deliveryGround != "" {
				if why := finishCleanupRunIdentityRefusal(deliveryGround, branch); why != "" {
					rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: deliveryGround, Detail: why})
					rep.Skipped = true
					rep.SkipReason = why
					return rep, nil
				}
			}

			// ①a THE BRANCH'S OWN CHECK (task_finish_premerge.go,
			// finishCleanupBranchCheckVerify) — asked at the delivery point,
			// ahead of every removal-side refusal. It works in THIS run's own
			// ground, at the branch's own revision: the packages its diff
			// against the integration branch touches, run through the repo's
			// DECLARED scoped entry point. It is the one thing a build cannot
			// do — `go build` compiles no `_test.go`, so a red this branch
			// introduces is invisible to every compiler.
			//
			// IT MARKS, IT DOES NOT BLOCK. What stood here was an ALIGNMENT
			// PRECONDITION (the client merging the peer's master into the
			// delivered branch, rebuilding the merged tree, vetting it) followed
			// by a published-anchor gate and a trial-merge verification — three
			// acts of the SERVER's merge, any of which could withhold the push.
			// Measured 2026-10-12: three finished branches were refused by that
			// lane in ONE batch (one by an anchor outside its own diff, two
			// after 834-838 s with no FAIL line anywhere), while the same three
			// hand-pushed in about a minute each and merged cleanly. So the
			// verdict is RECORDED — as this step and on the row's own trail —
			// and the delivery proceeds: whether a branch merges, and whether
			// the COMPOSED tree is sound, is decided once by the merger on the
			// integration branch.
			// GROUND: the recorded worktree, else d.Repo when THAT is the linked
			// worktree of the delivered branch (the executor's repo=worktree
			// backfill). A repo that is the main checkout yields no ground — the
			// lane then ABSTAINS instead of testing the shared checkout, and
			// says so (Rule 29).
			checkGround := wt
			if checkGround == "" && finishCleanupOwningWorktree(ctx, d.Repo) != "" {
				checkGround = d.Repo
			}
			verify := finishCleanupBranchCheckVerify(ctx, d.Repo, checkGround, branch)
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchCheckStep, Target: branch,
				OK: verify.Status != finishCleanupPreMergeStatusRefused, Detail: verify.Detail})
			// THE TRAIL WRITE THE COMMENT BELOW ALREADY PROMISED (measured
			// 2026-10-13, epic 01a13303). The paragraph under this line has
			// claimed since the vocabulary was added that "EVERY arm is
			// recorded, under the name its verdict earns" — and the CLIENT arm,
			// the one that actually delivers, never called the recorder: the
			// whole `finishCleanupBranchCheckReason` call sat only on the
			// server arm (finishCleanupServerRecycle). Measured on a real
			// `gopi task finish-cleanup --apply --task <id>` over a gone-ground
			// row: the report carried `steps that DID run=[branch-checks,push,…]`
			// while `task_events` held NO `branch-checks-*` row at all — so the
			// durable half of "was this branch's own revision measured?" was
			// absent precisely where a delivery happens. That is the ONE
			// existing recorder, called in the ONE place it was missing
			// (Rule 14: no second writer, no new mechanism).
			finishCleanupBranchCheckReason(rep.TaskID, verify)
			// EVERY arm is recorded, under the name its verdict earns: REFUSED
			// (the lane proved the revision undeliverable), ABSTAIN (nothing
			// could be measured) and VERIFIED (the lane RAN and passed).
			// Recording only the abstention was the other half of the defect —
			// the vocabulary then had no positive name in USE, so "ran and
			// passed" and "never ran" were one observation (Rule 15).
			//
			// A REFUSAL NO LONGER STOPS THE CHAIN (2026-10-12). It used to: the
			// lane merged the peer's master into this branch, rebuilt the merged
			// tree and withheld the push when any of that failed — three
			// finished branches were withheld in one batch (834-838 s each, no
			// FAIL line anywhere) and hand-pushed in about a minute each. That
			// decision is the SERVER's, made once on the integration branch, so
			// the verdict is recorded here and the delivery proceeds; a finished
			// branch that reaches nobody is the worse failure.
			// ② THE PUSH — and the TIP it actually carries (epic
			// 01a12da7, the delivery seam's own observation).
			//
			// WHAT WENT WRONG, measured on row 01a12ba1 (forensics reproduced
			// in the report's ## Evidence): the peer ended up holding
			// 247f1859b while the run's own closing revision c4a869e7d was its
			// CHILD — `git rev-list --count 247f1859b..c4a869e7d` = 1 — and
			// `git for-each-ref --contains c4a869e7d` was EMPTY on both hosts,
			// so the commit carrying the deliverable survived only as a
			// dangling object while the row read `completed`.
			//
			// WHAT THIS SEAM CAN SEE, and what it enforces: the tip is resolved
			// HERE, from the local ref, immediately before the push, and the
			// report records it — so "which revision did this sweep hand over?"
			// became an answerable question on the row's own record (the 01a12ba1
			// forensics had to reconstruct it by hand from a dangling object).
			// The guard then asks the strongest question a LOCAL observer can:
			// is the ref still at that revision once the push returned? A push
			// of `<branch>:<branch>` carries the LOCAL REF's tip (git resolves
			// the src ref inside the push, which is why no pre-transfer read
			// alone can declare what left), so a ref that MOVED across the
			// transfer refuses BY NAME with both hashes rather than settle on
			// whichever revision it happens to be looking at.
			// THE DELIVERY TARGET'S OWN RESOLUTION, recorded where the push
			// happens (2026-10-12). The target is not a composed
			// `<alias>:<path>` string any more: it is the ANSWER to "which git
			// target does the declared peer mean in THIS repo"
			// (peerRemoteTargetFor), and that answer has two arms with
			// different knowledge — the alias is a name this repo's git really
			// knows (so a misconfiguration here is visible, and the diagnosis
			// can say so) versus a name this repo's git does not configure at
			// all. A report that printed only the target could not tell a
			// reader which arm they were in, and after a refusal that is the
			// first question asked (Rule 15). Both arms hand git the SAME wire
			// form — the explicit ssh target — because this push resolves it in
			// the ROW's repo, where a name configured here resolves to nothing
			// (peerRemoteTarget.SSHTarget carries the measurement).
			//
			// A caller-injected target (the fixture seam) is STATED as such:
			// the step then carries the topology's own answer beside it rather
			// than pretending the injected string came from the resolution.
			targetRes := finishCleanupRowTargetResolution(finishCleanupMainRepoDir(d.Repo))
			targetDetail := targetRes.Reason
			// The comparison is against the form the LEGS carry (SSHTarget), not
			// the repo-level resolution: on the remote arm the two differ by
			// design, and comparing against Rendered would report an ordinary
			// production push as caller-injected (a false claim, Rule 15).
			if targetRes.SSHTarget != target {
				targetDetail += finishCleanupReasonSeparator +
					fmt.Sprintf(finishCleanupTargetInjectedNoteFmt, targetRes.SSHTarget)
			}
			rep.Steps = append(rep.Steps, FinishCleanupStep{
				Step: finishCleanupPushTargetStep, Target: target, OK: true, Detail: targetDetail})

			// THE PUSH RUNS IN THE MAIN REPO, NEVER IN `d.Repo` WHEN THAT IS
			// THE (GONE) WORKTREE DIRECTORY (measured 2026-10-17, this run).
			// `d.Repo` legitimately points AT the worktree — the executor's
			// backfill records `deliverable.repo` as the directory the run
			// worked in — so once that directory is removed every git call in
			// it fails at chdir. `branchDir` is exactly the directory this
			// chain already resolved for its branch operations (the shared
			// .git's parent, computed BEFORE step ③ removed anything, above),
			// and a push moves a REF: it transfers nothing from the working
			// tree, so the main checkout is the correct and sufficient place to
			// run it. Before this change the chdir failure was reported as a
			// REFUSED PUSH, which withheld step ④ for a reason that was never
			// about the delivery — the branch stayed on the host while the
			// report said the push had been refused.
			tip := branchDeleteTipOf(ctx, branchDir, branch)
			line, err := finishCleanupGit(ctx, branchDir, finishCleanupPushVerb, target, branch+":"+branch)
			if err != nil {
				// ONE REFUSED PUSH IS NOT A VERDICT (epic 01a17033: the bounded
				// delivery loop). A refusal has four different causes — a stale
				// base, a name collision, a content conflict, a dead peer — and
				// only the first is answered by looking again. The loop fetches
				// the peer's master afresh each round, ALIGNS the branch onto it
				// without touching a working tree (merge-tree + commit-tree +
				// update-ref; this directory is the main checkout, whose HEAD is
				// not the branch being delivered), and pushes again — at most
				// pushbackDeliveryRounds times.
				//
				// The loop is the SAME implementation the periodic flush uses
				// (deliverBranchToPeer, Rule 14), so the two delivery paths
				// cannot describe one refusal two ways. Its Reason is what this
				// step reports on exhaustion: the last round's own git output,
				// never a generic "push failed".
				if delivery := deliverBranchToPeer(ctx, branchDir, target, branch, nil); delivery.Delivered {
					line = delivery.String()
					err = nil
					// THE TIP IS RE-READ, and that is not a formality: a
					// delivered loop may have ALIGNED the branch, which moves
					// the ref to a new merge commit. The `tip` above was taken
					// before the push, so carrying it into the check below would
					// refuse a delivery that SUCCEEDED — "the peer does not hold
					// the tip we verified" — and withhold step ④ from exactly
					// the run the loop just rescued. The check that follows asks
					// whether the peer holds the branch's CURRENT tip; the
					// deliverable is whatever the branch is now.
					tip = branchDeleteTipOf(ctx, branchDir, branch)
				} else {
					line = peerRemoteTargetFailureDetail(ctx, d.Repo, target,
						gitPushbackRefusal(ctx, branchDir, target, branch, delivery.Reason))
					rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, Detail: line})
					finishCleanupNoteRemovalNotReached(rep, removeWhy, branchPresent)
					return rep, nil
				}
			}
			if why := finishCleanupPushCarriesTheRefTip(target, branch, tip, branchDeleteTipOf(ctx, branchDir, branch)); why != "" {
				// REFUSED BY NAME, and the refusal rides the row's trail
				// (finishCleanupRemovalRefusedEvent — the SAME recorder every
				// other delivery refusal uses, Rule 35: no new state file, no
				// new sweep, no new mechanism). The directory survives un-touched
				// (the removal phase is below this return) and ④ is never
				// reached, so a run that cannot hand its work over cannot
				// collect the ground its work stands on either.
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, Detail: line + " — " + why})
				// THE REPORT'S OWN DISPOSITION, not only a step: a refused
				// delivery must not classify as SETTLED. `Settled` reads the
				// step list, and a chain that returns HERE renders no worktree
				// step — so without this pair the row reads "nothing left to
				// do" while its wiring is unreachable, which is the very
				// settle this epic exists to stop. With Skipped set, the
				// classifier falls through to the push step it already
				// consults and finds it NOT OK.
				rep.Skipped = true
				rep.SkipReason = why
				finishCleanupPushCarriesTipReason(rep.TaskID, why)
				finishCleanupNoteRemovalNotReached(rep, removeWhy, branchPresent)
				return rep, nil
			}
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, OK: true,
				Detail: line + finishCleanupPushCarriesTipMarker + tip})
			// NO DIRECTORY WAS EVER IN THIS CHAIN (wt == ""): skip the whole
			// REMOVAL phase below — every guard in it (in-use, ownership ②,
			// ledger liveness, the checklist, completion) asks whether a
			// DIRECTORY may be deleted, and with wt == "" each one would abort
			// the chain with a reason about a directory nobody asked it to
			// remove. That is not a refusal of the delivery; it is a refusal
			// of a removal that does not exist — and it cost the branch its ④
			// (measured 2026-09-26: two completed HP rows kept their
			// task/<id> refs after their branches had reached the peer).
			// ④'s own standing is sufficient here and is asserted by its two
			// axes (this push landed; the orphan guard consults the delivery
			// point): see the branch block below for the full argument.
			if wt == "" {
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: rep.Worktree,
					Detail: finishCleanupNoGroundStepDetail})
				return finishCleanupDeleteDeliveredBranch(ctx, rep, branchDir, branch, target, planOnly)
			}
		}
	}
	// THE REMOVAL RECORD (epic client-push-rebase, 2026-09-19). Everything
	// below is a REMOVAL-side act, and every reason it may refuse on is
	// recorded from HERE — after the delivery, never before it. The delivery
	// above is what a refusal must never cost: the push moved a ref on the
	// peer, it read nothing from the working tree, and it is the ONE half of
	// the close-out with no irreversible adverse effect (a branch that reached
	// the peer is recoverable; a branch whose single-shot close-out skipped is
	// not).
	//
	// So the guard's REACH is what this block changes, never its tolerance:
	// dirt, a foreign binding and a gone directory each keep the directory
	// alive and the ref untouched, exactly as before — they simply stop gating
	// the delivery, and each one SETS rep.Skipped with its own reason (so a
	// reader who only looks at the verb's disposition still learns the removal
	// did not happen).
	// A PLAN never reaches here with a removal reason: the plan block above
	// returns the refused prediction already (and a plan executes nothing, so
	// it claims no removal).
	if !planOnly {
		if removeWhy != "" && finishCleanupDirtyReasonIsResidue(removeWhy) { // THE RESCUE (epic 01a11548, the operator's directive). A dirty
			// ground was refused FOR EVER: the directory stayed, no sweep
			// collected it, and the only exit was a human deleting a file
			// (measured 2026-10-07: three live rows, refused=1). The residue is
			// first RESCUED — its index-visible content into a ref this repo can
			// address for ever (refs/gopi/residue/…), its git-ignored content
			// into a tar bundle inside the run's own session directory, the
			// whole act recorded on the row's trail with path/size/sha256 — and
			// only a CLEARED rescue drops the refusal. A rescue that could not
			// run leaves removeWhy untouched and the refusal exactly as it was
			// (task_finish_cleanup_salvage.go).
			note, cleared, serr := finishCleanupSalvageGround(ctx, wt, rep.TaskID, branch)
			switch {
			case serr != nil:
				// Reported, never swallowed; the refusal stands and its reason
				// now carries WHY the rescue could not run (Rule 15).
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupSalvageStep, Target: wt, Detail: serr.Error()})
				removeWhy += finishCleanupSalvageFailedNote + serr.Error()
			case cleared:
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupSalvageStep, Target: wt, OK: true, Detail: note})
				removeWhy = "" // the removal arm below may now proceed
			default:
				rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupSalvageStep, Target: wt, Detail: note})
			}
		}
		if removeWhy != "" {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: removeWhy})
			rep.Skipped = true
			// SAME COMPOSITION AS THE PLAN ARM (Rule 14): one call, so the
			// apply run and the dry run cannot disagree about whether this
			// row's delivery is owed — and, when it was RAN and refused, about
			// the sentence that names that shape.
			rep.SkipReason = finishCleanupRemovalReasonWithDelivery(rep, removeWhy,
				finishCleanupGoneWorktreeDeliveryOwed(rep, removeWhy, branchPresent))
			// Recorded on the ROW's trail, not only a stderr line: the
			// directory is still there and the branch's delivery is what the
			// reason above states, which is the state a later sweep or an
			// operator has to act on
			// (devdoc/requirements/merge-and-branches.md B2). Best-effort.
			finishCleanupRemovalRefusedReason(rep.TaskID, removeWhy)
			// ④ IS NOT A REMOVAL-SIDE ACT — and the 2026-09-26 HP measurement
			// is what forces this sentence to be written down: when the
			// recorded worktree directory is ALREADY GONE (`wt != ""` but
			// nothing on disk), the removal has nothing to keep alive, and
			// returning here left the branch ref on the host forever. All ten
			// rows this sweep could see were in exactly that state
			// (`settled=7 refused=3`, every one "the recorded worktree
			// directory is not there"), which is why the host's task/<id> refs
			// outlived the work they name: the delivery is a single shot per
			// row and nothing re-runs it (Rule 6d).
			//
			// The rule this does NOT break is Step ③ standing's: ④ may not
			// outrun ③. Where ③ HAD a directory to collect and did not, the
			// pair holds and the return above stands. Where there is no
			// directory in the chain at all, the relevant question is the one
			// ④ asks itself: did THIS sweep deliver, and is the tip held
			// elsewhere? (finishCleanupDeleteDeliveredBranch asserts both —
			// axis 2 refuses a force-delete without this sweep's own push, and
			// the orphan guard consults the delivery point.)
			if wt == "" || finishCleanupWorktreeDirAbsent(removeWhy) {
				return finishCleanupDeleteDeliveredBranch(ctx, rep, branchDir, branch, target, planOnly)
			}
			return rep, nil
		}
	}
	// ③ Liveness gate BEFORE the --force removal (the data-loss path: --force
	// on a dirty tree deletes uncommitted work). A live CWD inside the
	// worktree skips the step with an explicit Detail — the pushed branch is
	// already safe on the server, the directory stays until the holder exits.
	if _, why := finishCleanupWorktreeInUse(wt); why != "" {
		// Same input check as the entry gate (Gap A): an orphaned
		// cwd-inheritor is not a holder, and the removal may proceed — with
		// the release recorded as this step's Detail.
		_, orphanWhy := finishCleanupOrphanCwdInheritorReason(wt)
		if orphanWhy == "" {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
			return rep, nil
		}
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: orphanWhy})
	}
	// Ownership gate ② (2026-09-10 epic): step ③ is the --force removal — the
	// one path that deletes uncommitted work. The directory must not be
	// another row's recorded deliverable home (a re-attached / shared
	// directory). The check sits AFTER the push on purpose: a refused removal
	// must still leave the branch delivered (step ④ is not reached, so the
	// local branch survives — nothing is destroyed either way).
	if owners, oerr := finishCleanupWorktreeOwnerRows(rep.TaskID); oerr == nil {
		if why, _ := finishCleanupForeignOwnerReason(owners, wt); why != "" {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
			return rep, nil
		}
	}
	// Ledger-liveness gate (2026-09-12 epic): the terminal row's record is
	// not the only claim that matters — a LIVE row may now own this
	// directory (a later task's worktree at the same path), and this arm's
	// removal is the same `worktree remove` that destroys a running
	// session's ground. Placed AFTER the push on purpose, exactly like the
	// ownership gate above: a refused removal must still leave the branch
	// delivered (step ④ is not reached, so the local branch survives too).
	if why := finishCleanupLiveOwnerGate(ctx, wt, rep.TaskID); why != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
		return rep, nil
	}
	// THE DETERMINISTIC CHECKLIST (reclaim_ground_checklist.go) — the --force arm
	// destroys uncommitted work, so it owes the same two rungs BEFORE the plan is
	// rendered (a plan that proposes a removal the checklist refuses would be a
	// plan for a removal that cannot happen). Close-out form: this is the same
	// run's own arm (see the rung's doc).
	if why := reclaimGroundChecklistVerdictForCloseout(d.Repo, wt, branch); why != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
		return rep, nil
	}
	// THE RUN-IDENTITY GUARD on the --force arm too (SL3-2): this is the arm the
	// incident's audit record names (`finish-cleanup-worktree-force`), and the
	// question it owes is the ground's OWN record — see
	// task_finish_cleanup_run_identity.go. A live run standing on the ground
	// refuses the removal BY NAME, with nothing removed.
	if why := finishCleanupRunIdentityRefusal(wt, branch); why != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
		return rep, nil
	}
	// THE COMPLETION FACT, on the --force arm too. This path deletes uncommitted
	// work outright, so it owes the same question the ordinary arm owes — and it
	// owed it most: the first Rule 31 run of this fix showed the plan rendering
	// "would run: git worktree remove … --force" for a row whose branch was not
	// in the integration branch at all. A --force removal is exactly where a
	// MERGE statement ("the push delivered the ref") is furthest from a
	// COMPLETION statement about the work.
	if why := finishCleanupWorkCompletionReason(ctx, d.Repo, wt, branch, rep.TaskID, IntegrationBranchName); why != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: why})
		return rep, nil
	}
	if planOnly {
		// PLAN MODE: this is the --force removal — the one step that deletes
		// uncommitted work — so the plan renders it in full (flag included)
		// and runs nothing.
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, OK: true,
			Detail: finishCleanupPlanRemoveDetail + " " + wt + " " + finishCleanupPlanForceFlag})
	} else {
		var line string
		var err error
		// Audit record BEFORE the removal (package removalaudit). This is the
		// --force step — the one that deletes uncommitted work — so the record
		// matters most here: the chain reached it only past its own live-owner
		// gate, and that gate's empty verdict is the deciding evidence.
		removalaudit.WorktreeRemove(removalaudit.ArmFinishCleanupForce, d.Repo, wt, branch, rep.TaskID,
			"delivery chain's live-owner gate did not refuse (no live owner of this worktree); checklist "+reclaimGoalChecklistSummary)
		line, err = finishCleanupGit(ctx, d.Repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, wt, finishCleanupPlanForceFlag)
		if err != nil {
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, Detail: line})
			return rep, nil
		}
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupStepWorktree, Target: wt, OK: true})
	}

	// Step ③ standing (2026-09-12 epic): the branch step runs only when this
	// row's worktree step ALSO succeeded. A worktree the sweep did not
	// deliver is still the run's ground, so the branch is not "delivered by
	// this sweep" either — the pair stays together, as ONE decision, with
	// the earlier step's reason already reported. (A call site that ever
	// appends a non-OK worktree step without returning is then still safe
	// rather than silently falling through to a ref deletion.)
	if !finishCleanupStepSucceeded(rep, finishCleanupStepWorktree) {
		rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch,
			Detail: finishCleanupBranchNotDeliveredMarker + ": no worktree was collected by this sweep — the pair survives"})
		return rep, nil
	}

	// ④ Delete the local branch. The decision itself lives in
	// finishCleanupDeleteDeliveredBranch — the SAME implementation the wt-less
	// path calls (Rule 14), so the two ways a delivered branch may be reaped can
	// never drift. The refusal semantics stay: a branch the push did NOT deliver
	// is "not fully merged" and SURVIVES, and `-D` (the force half) is refused
	// unless THIS sweep delivered the ref.
	//
	// branchDir was resolved BEFORE ③ (the worktree is gone by now — see the
	// branchDir block above the push).
	return finishCleanupDeleteDeliveredBranch(ctx, rep, branchDir, branch, target, planOnly)
}

// shellQuotePath single-quotes a path for sh, so a path with a space still
// reaches the shim intact.
func shellQuotePath(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// staleOwnerRow seeds a row that recorded worktree as its deliverable home
// while its life is OVER (terminal status) and its claim stamp resolves to a
// pid that is GONE on this host — the shape that pinned gopi-cov-agent
// forever (completed row 01a08cfd blocked by cancelled sibling 01a08cd5).
func staleOwnerRow(t *testing.T, db, taskID, status, worktree string) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skipf("hostname unavailable: %v", err)
	}
	ownershipSeedRow(t, db, taskID, status, host+":"+strconv.Itoa(deadOwnerPid), worktree)
}

// taskFinishCleanup is the ONE chain implementation; planOnly selects whether
// its action steps act or are predicted.
func taskFinishCleanup(taskID string, planOnly bool) (*FinishCleanupReport, error) {
	rep := &FinishCleanupReport{TaskID: taskIDNormalize(taskID), PlanOnly: planOnly}
	if rep.TaskID == "" {
		return nil, fmt.Errorf("finish-cleanup: task_id must not be empty")
	}
	if !taskFinishCleanupOn() {
		rep.Skipped = true
		rep.SkipReason = "disabled (" + finishCleanupEnv + ")"
		return rep, nil
	}

	// ① The row must exist and be terminal.
	//
	// The LEDGER context is deliberately its own scope (finishCleanupDBTimeout):
	// it must expire with the read it was created for, never while the chain
	// downstream is driving a merge, a compiler or a build — that expiry was
	// measured refusing an already-verified delivery (see
	// finishCleanupChainTimeout).
	dbCtx, dbCancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer dbCancel()
	db, err := openTasksDB(dbCtx, memoryDBPath())
	if err != nil {
		return nil, fmt.Errorf("finish-cleanup: open ledger: %w", err)
	}
	defer db.Close()
	t, err := taskByID(dbCtx, db, rep.TaskID)
	if err != nil {
		return nil, fmt.Errorf("finish-cleanup: %w", err)
	}
	if t == nil {
		rep.Skipped = true
		rep.SkipReason = "no such task row"
		return rep, nil
	}
	rep.Status = t.Status
	if !taskCleanupEligibleStatus(t.Status) {
		rep.Skipped = true
		rep.SkipReason = "status=" + t.Status + " not cleanable (terminal or cancelled required)"
		return rep, nil
	}
	// A failed row without a deliverable has nothing to push — but a
	// deliverable-bearing failed row still cleans (the work EXISTS; the
	// server decides its fate, and -d protects it from deletion anyway).
	if t.Deliverable == "" {
		rep.Skipped = true
		rep.SkipReason = "no deliverable"
		return rep, nil
	}
	d := finishCleanupParseDeliverable(t.Deliverable)
	if d.Repo == "" {
		rep.Skipped = true
		rep.SkipReason = "deliverable has no repo"
		return rep, nil
	}
	rep.Repo = d.Repo

	// Mode split by node role (the finish-cleanup resource-responsibility
	// principle, serve-worker-split epic): a TERMINAL deliverable-bearing
	// row must release its worktree on EVERY node — a finished run's
	// parked worktree starves the merger (its merge trigger = "no worktree
	// protection"). What differs is the branch step:
	//
	//	client (peer declared):  push → worktree remove → branch -d (the
	//	                         hand-off recycle — the branch must reach the
	//	                         server before its local checkout disappears)
	//	server / single-node:    worktree remove only, branch PRESERVED —
	//	                         the branch lives in the shared repo (or is
	//	                         this node's own merge input); deleting it
	//	                         here would remove work the merger has not
	//	                         yet consumed.
	// The chain's own budget starts HERE, after the ledger read: the action
	// steps (merge, declared build, vet, worktree release, push) must not
	// inherit a deadline that was sized for one database round-trip.
	ctx, cancel := taskFinishCleanupActionContext()
	defer cancel()

	// ①a THE MAIN CHECKOUT, restored BEFORE either arm runs.
	//
	// This is the WORKER half of the epic "`gopi merge --fast` work in master",
	// and it is placed HERE — after the ledger read, before the mode split —
	// because every guard ABOVE returns early (no row, a live status, no
	// deliverable, no repo) and each of those returns is a run that can still have
	// parked the repo: the parking is done by the run's own git commands, not by
	// the ledger, so a restore attached to the action steps below would miss the
	// runs that most need it.
	//
	// The step is recorded only when there is something to report — a checkout
	// already where a landing needs it adds no line, because a permanent "restored"
	// line trains the reader to ignore the real one. A REFUSAL is recorded too
	// (OK=false), so an operator sees a checkout this close-out could not put back
	// instead of a silent no-op (Rule 15).
	if planOnly {
		if step := finishCleanupMainCheckoutPlannedStep(ctx, d.Repo); step != nil {
			rep.Steps = append(rep.Steps, *step)
		}
	} else if ok, detail := FinishCleanupRestoreMainCheckout(ctx, d.Repo, d.Worktree, d.Branch); !ok || detail != "" {
		rep.Steps = append(rep.Steps, FinishCleanupStep{
			Step: finishCleanupStepMainCheckout, Target: d.Repo, OK: ok, Detail: detail,
		})
	}

	// ①b THE MAIN REPO ITSELF — clean, and holding the INTEGRATION branch (epic
	// closeout-reads-the-main-repo; the two lines the close-out's checklist was
	// missing). It sits HERE for the SAME reason ①a does, one paragraph up:
	// every guard above returns early, and each of those returns is a run that
	// can still have left its by-products in the checkout it was launched from —
	// the parking and the dirt are done by the run's own git commands, not by
	// the ledger. It also has to sit before the mode split: there are TWO chain
	// bodies (server recycle and client delivery), so a guard wired into one of
	// them would be silently absent on the other.
	//
	// The refusal is NAMED (repo path + git's own porcelain lines or both branch
	// names + the operator repair) and it STOPS the chain: a run whose own
	// ground is clean has still not finished while the checkout it ran from
	// carries its residue or a moved HEAD. Nothing is repaired here — no `git
	// clean`, no `checkout --`, no stash, no commit (see
	// finishCleanupMainRepoReason).
	if checked, ok := finishCleanupMainRepoGate(ctx, rep, d); !ok {
		return checked, nil
	}

	if !finishCleanupClientArmApplies() {
		return finishCleanupServerRecycle(ctx, rep, d, planOnly)
	}
	return taskFinishCleanupChain(ctx, rep, d, planOnly)
}

// taskFinishCleanupChain executes steps ①–④ against the parsed deliverable.
// Split from the lookup above so tests exercise the chain directly (injected
// repo/branch/worktree without ledger rows).
func taskFinishCleanupChain(ctx context.Context, rep *FinishCleanupReport, d taskFinishDeliverable, planOnly bool) (*FinishCleanupReport, error) {
	// THE MAIN REPO'S OWN TWO LINES, before any step of this arm runs (see
	// finishCleanupMainRepoReason): the checkout the run was launched from must
	// be CLEAN and hold the INTEGRATION branch. Every arm of the close-out is
	// downstream of a checkout whose dirt or parked HEAD it cannot see, and a
	// run that left either behind has not finished — so this gate refuses the
	// chain with a named reason rather than delivering on top of it.
	if rep, ok := finishCleanupMainRepoGate(ctx, rep, d); !ok {
		return rep, nil
	}
	// Branch resolution: backfilled deliverable wins; fallback = the repo's
	// checked-out branch (HEAD). An exempt branch aborts before any step.
	branch, brErr := finishCleanupResolveBranch(ctx, rep, d)
	if brErr != nil {
		rep.Skipped = true
		rep.SkipReason = brErr.Error()
		return rep, nil
	}
	if branch == "" {
		return rep, nil // the resolver already named the refusal on the report
	}
	rep.Branch = branch

	// Worktree resolution: backfilled deliverable wins; fallback = the
	// worktree containing the repo dir (a repo checked out on a branch
	// inside a linked worktree). Empty fallback = the main checkout.
	wt := strings.TrimSpace(d.Worktree)
	if wt == "" {
		wt = finishCleanupOwningWorktree(ctx, d.Repo)
	}
	rep.Worktree = wt

	// Dirty gate: a dirty worktree keeps the DIRECTORY — and by itself it no
	// longer stops the chain. The probe and its refusal live INSIDE the chain
	// body (runFinishCleanupChainTarget), because only there is the order
	// knowable: the refusal is a REMOVAL-side fact, and the delivery (①b
	// pre-merge + ② push) must happen before it (epic client-push-rebase,
	// 2026-09-19). A guard that returns HERE would make the whole chain skip
	// again — the exact defect (devdoc/hp-push-diagnosis-20260916.md A/A′).

	// Push target: the first declared sync peer is the server, asked of THE
	// ROW's repo (finishCleanupRowTargetResolution) and never of this process's
	// cwd — a remote is a per-repository setting, and a CLI close-out is invoked
	// from a different checkout than the one it delivers from. An empty
	// target is NOT one condition but two — NO peer declared (the single-node
	// deployment) versus a declared-but-unusable alias (a settings slip) —
	// and noServerTargetSkipReason names WHICH, plus what it means for this
	// row's deliverable branch (fail-soft skip either way: nothing removed).
	//
	// The empty case is resolved INSIDE the chain body, not here: the reason
	// must carry the removal-side state the body has just probed (a gone
	// directory's diagnostic + the "not handed off to any server" note are one
	// sentence an operator reads, and the dead-end wording is what
	// finishCleanupReportSettled keys on).
	return runFinishCleanupChainTarget(ctx, rep, d, finishCleanupServerTargetForRepo(d.Repo).SSHTarget, planOnly)
}

// waitOrphanReparentTarget waits until pid has been handed to one of this host's
// reparent targets and returns the measured ppid. It is the shared premise of
// every orphan fixture in this package, and it FAILS LOUDLY in both directions
// it cannot satisfy:
//
//	the set cannot be measured → Fatalf (an unmeasurable premise is not a pass);
//	the budget expires          → Fatalf, naming the pid, the ppid it actually
//	                              has, and every target that was rejected.
//
// It never skips. That is the whole point of the helper: the defect it replaces
// was invisible precisely because six fixtures turned this same unmet premise
// into a green SKIP line.
func waitOrphanReparentTarget(t *testing.T, pid int, budget time.Duration, what string) int {
	t.Helper()
	pids, text := orphanReparentPids(t)
	start := time.Now()
	deadline := start.Add(budget)
	for {
		ppid, ok := finishCleanupProcParent(strconv.Itoa(pid))
		if ok {
			for _, target := range pids {
				if ppid == target {
					t.Logf("orphan fixture: %s pid %d handed to this host's reaper %d after %s (budget %s)",
						what, pid, ppid, time.Since(start).Round(time.Millisecond), budget)
					return ppid
				}
			}
		}
		if time.Now().After(deadline) {
			ppid, ok := finishCleanupProcParent(strconv.Itoa(pid))
			t.Fatalf("orphan fixture: %s pid %d was not reparented to any of this host's reparent targets {%s} within %s "+
				"(ppid=%d ok=%t) — the fixture's premise does not hold, so its assertions would never have run; "+
				"that is a failure here, never a skip (Rule 15)",
				what, pid, text, budget, ppid, ok)
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(orphanReparentPoll)
	}
}

// waitProcCwd blocks until pid's procfs CWD resolves under want — a CWD
// that is read one instant too early would make the guard test flaky. An
// unreadable procfs (non-Linux / restricted) SKIPS: the guard degrades to
// fail-soft there and the test cannot exercise it (zero-dep, Rule 7).
func waitProcCwd(t *testing.T, pid int, want string) {
	t.Helper()
	resolved := want
	if r, err := filepath.EvalSymlinks(want); err == nil {
		resolved = r
	}
	for i := 0; i < procScanWaitAttempts; i++ {
		if cwd, ok := finishCleanupProcCWD(strconv.Itoa(pid)); ok &&
			(finishCleanupPathUnder(cwd, want) || finishCleanupPathUnder(cwd, resolved)) {
			return
		}
		time.Sleep(procScanWaitInterval) //stylegate:allow-sleep input to the liveness observation itself — the holder must stay alive and the log stay quiet
	}
	t.Skipf("procfs CWD of pid %d unreadable — liveness guard untestable on this host", pid)
}
