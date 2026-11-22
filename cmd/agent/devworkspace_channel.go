package main

// devworkspace_channel.go — the CLONE CHANNEL's seam in `gopi worker`.
//
// THE WHOLE USER-VISIBLE SURFACE OF THE CHANGE IS A SETTINGS KEY. `gopi worker`
// keeps its spelling, its arguments and its exit-code meanings; which channel a
// run takes is decided by ONE value:
//
//	settings dev_repo (or GOPI_DEV_REPO) non-empty → the clone channel
//	empty / absent                                → today's worktree channel
//
// There is no new verb and no new flag (operator ruling, 2026-10-18). The env
// var is the only CLI-adjacent channel, because that is the smallest shape of
// "change one parameter without changing the command".
//
// WHAT THE CLONE CHANNEL DOES, per task, inside ONE worker invocation (no
// daemon, no cron, no tick — Rule 35):
//
//	clone dev_repo → workspace_root/<task-id>     (or reuse an existing one)
//	while attempts remain: pull --rebase → build → push
//	push accepted → the workspace is RECLAIMED (the code is at origin)
//	push refused  → back to pull --rebase
//	brake fired   → report by name, KEEP the workspace, write a record
//
// The loop itself lives in internal/devworkspace (it owns the brake and the
// classification of "refused"); this file only wires it to the spawn seam and
// to the ledger.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// cloneRunState is what the prepare half hands to the close-out half: the
// parameters it resolved (so the post-run loop cannot re-resolve them and
// disagree) and the workspace it opened.
type cloneRunState struct {
	cfg devworkspace.Config
	dir string
	// rounds is how many delivery ATTEMPTS the outer loop spent (1 when the
	// first one landed), recorded for the run's own summary line.
	rounds int
	// agentRound runs ONE agent round in the workspace, handed the briefing and
	// nothing else. It is a field so the outer loop can be proved without
	// spawning a model (Rule 31's cheap half); production leaves it nil and gets
	// spawnCloneChannelAgent.
	agentRound func(dir, briefing string) error
	// attempt is ONE delivery attempt (devworkspace.AttemptDelivery in
	// production). A field for the same reason: the loop's own arithmetic — how
	// many rounds it spends, what it does with the last blocker, whether it
	// spends the closing report round exactly once — is provable against a fake
	// without a git fixture.
	attempt func() (devworkspace.Result, *devworkspace.Blocker, error)
	// rowStatus reads the LEDGER's current status for the run's row
	// (tools.TaskRowStatusAt in production). It is the ROUND BOUNDARY's only
	// input: the loop must stop spending agent rounds the moment the operator
	// retires the row (row 01a18392), and a field keeps that provable without a
	// ledger fixture.
	rowStatus func(taskID string) (string, error)
	// groundProducedNothing reads the GROUND's own produced-nothing question
	// (tools.TaskGroundProducedNothing in production). It is the second input the
	// round boundary needs — see the produced-nothing brake in finish() — and a
	// field for the same reason as rowStatus: the loop's arithmetic must be
	// provable against a scripted answer, with no git fixture.
	groundProducedNothing func(ground, branch string) tools.GroundProducedNothingRead
}

// taskRunCloneChannelFor answers the channel question for taskID and, when the
// clone channel is ON, opens (or reuses) the task's workspace and resolves it
// into a ground. It returns cloneRun == nil when the clone channel is OFF, and
// that nil is the ONLY thing that makes the run take today's path.
//
// An ERROR means "the channel is on but the run cannot start": a settings file
// that cannot be read, a repo with no declared build command (Rule 12 — the
// command is never discovered), a clone that failed. The caller refuses the run
// loudly instead of silently falling back to the worktree channel — a silent
// channel switch would put the run in a workspace nobody declared (Rule 15).
func taskRunCloneChannelFor(taskID string, log io.Writer) (*cloneRunState, tools.TaskGroundResolution, error) {
	on, source, err := config.CloneChannelConfigured()
	if err != nil {
		return nil, tools.TaskGroundResolution{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unreadable: %w", config.DevRepoSettingsKey, err)
	}
	if !on {
		return nil, tools.TaskGroundResolution{}, nil
	}
	devRepo, _, err := config.DevRepoConfigured()
	if err != nil {
		return nil, tools.TaskGroundResolution{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unreadable: %w", config.DevRepoSettingsKey, err)
	}
	root, rootSource, err := config.WorkspaceRootConfigured()
	if err != nil {
		return nil, tools.TaskGroundResolution{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unusable: %w", config.WorkspaceRootSettingsKey, err)
	}
	// THE BUILD COMES FROM THE DECLARATION (Rule 12), never from a guess: the
	// row's own repo entry in the settings `repos` table is the only place a
	// build command may come from, and a repo that declares none refuses.
	//
	// ONE EXCEPTION, AND IT IS NOT A GUESS (bug 01a1515c, second half): a repo
	// that lies INSIDE the workspace root is a checkout this channel creates —
	// a plain clone at <root>/<task-id>, a repository of its own and therefore
	// declared nowhere. Such a row is worth one read-only question about its
	// provenance (`git remote get-url origin`) and one lookup of THAT path in the
	// same table. A hit uses the source repository's declared command; a miss
	// refuses with the SAME sentence as before. The fallback never fires for a
	// repo outside the workspace root, so a normal repo's log and git calls are
	// untouched, and it caches nothing: the refusal re-asks every time.
	repo := tools.TaskDeliverableRepoFor(taskID)
	// WHICH REPOSITORY DOES *THIS ROW* CLONE FROM? The row's own
	// deliverable.repo when it is a usable work tree of its own, else the host's
	// dev_repo (internal/devworkspace.ResolveCloneSource — ONE resolver, so this
	// leg and the delivery leg cannot disagree about the origin).
	//
	// THE DEFECT THIS CLOSES (measured 2026-11-22, row 01a20059-fc11): the
	// channel cloned `dev_repo` unconditionally, so a row whose deliverable.repo
	// was /home/user/work/sudoku got a ground holding the GOPI tree — the work it
	// was asked to do could not be written there, and the delivery loop would
	// have pushed gopi's master onto gopi's server.
	cloneSrc := devworkspace.ResolveCloneSource(repo, root, devRepo)
	if cloneSrc.Note != "" {
		fmt.Fprintf(log, "task %s: %s\n", taskID, cloneSrc.Note)
	}
	state := &cloneRunState{cfg: devworkspace.Config{
		TaskID:  taskID,
		DevRepo: cloneSrc.Repo,
		// THE ROW'S OWN DECLARATION, kept BESIDE the resolved clone source: the
		// two are the same on the ordinary shape and differ exactly when the
		// channel fell back to the host's dev_repo — and that is the only case in
		// which the empty gate's sentence needs a scope (epic measured 2026-11-22,
		// row 01a200a0-6062: gopi clone + sudoku deliverable.repo). Passing the
		// row's value through is what lets the sentence say "this count is about
		// the GROUND, not about the repository your row names" instead of leaving
		// a reader to conclude there is nothing to fetch back.
		RowRepo: repo,
		Root:    root,
		// THE GROUND STANDS ON THE INTEGRATION BRANCH — GroundBranch is left
		// EMPTY on purpose (the operator's ruling, 2026-11-05).
		//
		// The reason `task/<id>` used to be minted here was the branch→row
		// lookup, and it was answered by a ref that no merge path consumes:
		// every such ground paid a `git checkout -b task/<id>` one second
		// after its clone, and the row's commits ended up on a ref the server
		// never receives (publishGround's ONE destination is `base`). A run's
		// workspace is EXCLUSIVE — nothing else works in it, so there is no
		// collision for a private ref to prevent — and the row is addressed by
		// its WORKSPACE PATH (`deliverable.worktree`, what the claim stamps and
		// `gopi task show --worktree` resolves), never by a branch name.
		//
		// BaseBranch stays: it is the integration point this workspace rebases
		// onto before every delivery AND the one destination its push moves.
		BaseBranch: devworkspace.DefaultBranch,
		RootSource: rootSource,
		Log:        log,
	}}
	rc, err := resolveCloneChannelBuild(state.cfg, repo)
	if err != nil {
		return nil, tools.TaskGroundResolution{}, err
	}
	state.cfg.Build = rc.Build
	state.cfg.BuildTimeout = time.Duration(rc.BuildTimeoutSec) * time.Second
	// THE ANCHOR, FROM THE RECORD (`deliverable.worktree`), read ONCE here: the
	// delivery attempt's gate (devworkspace.deliveryAnchorGate) compares it with
	// the directory it is about to publish from, so a run that committed in
	// another row's ground is REFUSED by name instead of publishing an empty
	// tree (measured defect 01a17dee). The reader is the row's own accessor —
	// no second parser of the deliverable blob (Rule 14).
	if recorded, _ := tools.TaskDeliverableWorktreeFor(taskID); recorded != "" {
		state.cfg.Worktree = recorded
	}
	// THE LINE NAMES THE SOURCE THAT DECIDED, not the declaration that did not
	// (Rule 15's positive half). It used to print `dev_repo=<declared>` only, so
	// a run cloned from the row's repository still read as if dev_repo had
	// chosen — the same sentence/decision disagreement this change exists to
	// remove.
	fmt.Fprintf(log, "task %s: clone channel ON (source=%s from %s; %s=%s via %s; %s=%s via %s)\n",
		taskID, cloneSrc.Repo, cloneSrc.Arm,
		config.DevRepoSettingsKey, devRepo, source,
		config.WorkspaceRootSettingsKey, root, rootSource)
	// THE DESTINATION IS ASKED BEFORE THE WORK, not after it.
	//
	// This is the whole first half of the epic: on hp the declaration named the
	// host's OWN non-bare tree with `master` checked out and no
	// `receive.denyCurrentBranch`, so the channel cloned, pulled and BUILT — most
	// of the way through a run — and only then learned that git refuses to push a
	// checked-out branch there:
	//
	//	! [remote rejected]     HEAD -> master (branch is currently checked out)
	//
	// The answer is a property of the DESTINATION's own configuration (the same
	// declaration works on a host that sets `updateInstead`), so it is a question
	// to ASK the destination, and asking it costs one rev-parse. It is reported
	// BY NAME and the run continues: a misdeclaration is a fact the operator
	// decides about, and both fixes (declare a peer/bare destination, or set
	// `updateInstead` there) are configuration changes this run must not make for
	// them (PROJECT.md Rule 15 names the cause; Rule 35 forbids a new dependency
	// to carry the message).
	if acceptance := devworkspace.ProbePushTargetAcceptance(
		context.Background(), cloneSrc.Repo, devworkspace.DefaultBranch); acceptance.RejectsPush {
		fmt.Fprintf(log, "%s\n", devworkspace.PushTargetWarning(cloneSrc.Repo, acceptance))
	} else if acceptance.Probed {
		fmt.Fprintf(log, "task %s: the delivery point accepts a push of %s — %s\n",
			taskID, devworkspace.DefaultBranch, acceptance.Note)
	} else {
		fmt.Fprintf(log, "task %s: the delivery point was NOT asked — %s\n", taskID, acceptance.Note)
	}
	opened, err := devworkspace.Open(context.Background(), state.cfg)
	if err != nil {
		return state, tools.TaskGroundResolution{}, err
	}
	state.dir = opened.Dir
	state.cfg.Branch = opened.Branch
	ground, err := tools.TaskRunGroundForClone(taskID, opened.Dir)
	if err != nil {
		return state, tools.TaskGroundResolution{}, err
	}
	fmt.Fprintf(log, "task %s: %s\n", taskID, ground.Detail)
	return state, ground, nil
}

// finish is the close-out half: the DELIVERY LOOP, its round-by-round hand-back,
// and the reclaim.
//
// THE SHAPE (the operator's ruling, 2026-10-18): delivery is not a fixed
// pipeline whose first refusal ends the run. It is at most MaxDeliveryRounds
// ATTEMPTS. Each attempt is the deterministic part (clean → pull → build →
// push, with the push-race backoff inside it) and answers either "delivered" or
// a Blocker. A Blocker is handed to ONE agent round — the workspace path and the
// tool's own words, nothing else — and the agent decides what to do about it
// (commit the work, reconcile the conflict, fix the build). This file writes no
// step list: prescribing "first git add, then git commit" is exactly what the
// loop replaces.
//
// EXHAUSTED (every attempt blocked): the run fails, the LEDGER gets the reason
// (Rule 15 — a refusal that only reaches stderr is invisible to the next
// reader), and the agent gets ONE closing round to write the report. That round
// is NOT counted against MaxDeliveryRounds and runs at most once: a report is a
// record of what happened, not another attempt at delivering.
func (s *cloneRunState) finish(log io.Writer) (devworkspace.Result, error) {
	var last devworkspace.Result
	var blocker *devworkspace.Blocker
	for round := 1; round <= devworkspace.MaxDeliveryRounds; round++ {
		// THE ROUND BOUNDARY IS WHERE THE LEDGER IS READ (row 01a18392).
		// Before this check existed the loop was unconditional: an operator who
		// cancelled a row watched it spend all MaxDeliveryRounds attempts and
		// spawn an agent round between each pair of them, every spawn a bare
		// prompt that registered a NEW ledger row — the zombie-row source this
		// fix closes. A retired row stops HERE, before any attempt is spent.
		if err := s.stopIfRowRetired(log); err != nil {
			return last, err
		}
		s.rounds = round
		fmt.Fprintf(log, "task %s: delivery attempt %d/%d\n", s.cfg.TaskID, round, devworkspace.MaxDeliveryRounds)
		// THE SECOND FACT THE EMPTY GATE NEEDS, read fresh EACH ROUND (row
		// 01a182df): the tip this row's own delivery already recorded pushing.
		// Without it `rev-list --count HEAD --not origin/<base>` — which this
		// channel's OWN push moves to 0 — makes round 2 read "nothing here to
		// deliver" about work that round 1 landed, and the run is told it
		// produced nothing. The read is the row's durable record rather than
		// this process's memory because the pushing round can be a DIFFERENT run
		// (a resumed row, a retry leg). "" is the ordinary first-round answer and
		// leaves the gate's behaviour unchanged.
		s.cfg.DeliveredTip = tools.TaskLandedDeliveryTip(s.cfg.TaskID, s.cfg.Branch)
		res, blk, err := s.runAttempt()
		last = res
		if err != nil {
			// NOT a blocker: no declared build, an unreadable branch, a push
			// that was not a race. No agent round can change any of these.
			fmt.Fprintf(log, "task %s: %s\n", s.cfg.TaskID, err)
			return res, err
		}
		if blk == nil {
			// DELIVERED. The round that landed is the whole story.
			s.rounds = round
			last = res
			break
		}
		blocker = blk
		// THE BLOCKER, SAID OUT LOUD AND IN THE TOOL'S OWN WORDS (Rule 15): the
		// line quotes the blocker's Words verbatim, so an operator reading the
		// log sees what git/the build actually answered rather than a summary.
		fmt.Fprintf(log, "task %s: delivery attempt %d/%d blocked (%s): %s\n",
			s.cfg.TaskID, round, devworkspace.MaxDeliveryRounds, blk.Kind, blk.Report())
		if round == devworkspace.MaxDeliveryRounds {
			break
		}
		// AND A DELIVERY THAT ALREADY LANDED IS NOT WORTH RETRYING (row
		// 01a182df): the second reading is about a PAST push, not about a step
		// that refused, so nothing an agent round can do changes it — retrying
		// would spend two rounds to re-measure the same fact and would spawn
		// processes to say "nothing to do". The loop stops here and the fork
		// below names the outcome.
		if blk.Kind == devworkspace.BlockerKindAlreadyDelivered ||
			blk.Kind == devworkspace.BlockerKindInheritedDelivery {
			break
		}
		// AND NEITHER IS A GROUND THAT NEVER PRODUCED A COMMIT (bug row
		// 01a1907d-family, measured 2026-10-31 on 01a18d61-509f). The empty kind
		// does NOT break this loop, so a TOO_BIG reframe row — whose deliverable
		// is a decomposition in the LEDGER, and whose ground is still standing on
		// the commit it was cut from — spent all MaxDeliveryRounds attempts and
		// the agent rounds between them re-measuring one immutable fact. Its own
		// log is the evidence: three identical `carries no commit to deliver`
		// lines and `(after 3 delivery attempt(s) ...)`.
		//
		// THE CONDITION IS NOT "the step said empty", it is the produced-nothing
		// READING: HEAD IS the commit the branch was cut at, and the worktree is
		// clean. A round cannot turn that into a commit — there is nothing to
		// commit — so the attempt count is not a budget, it is waste. The reading
		// already exists (tools.GroundProducedNothingRead, the same witness the
		// delivery proof uses) and is asked here rather than re-derived.
		//
		// IT IS NARROW ON PURPOSE, and every clause is load bearing:
		//
		//	· Measured — an abstention does not stop anything (Rule 15);
		//	· Nothing  — a ground AHEAD of its cut point still has work to publish,
		//	             so it keeps every round it is entitled to;
		//	· !Pending — an uncommitted change is exactly what the retry round
		//	             exists to turn into a commit, so a dirty ground is never
		//	             cut short by this brake.
		if s.groundNeverCommitted(log) {
			break
		}
		// THE SECOND HALF OF THE SAME BOUNDARY, and the one that matters most:
		// the retry round is about to START A PROCESS, so the row is read again
		// here — a cancel that landed while this attempt was running must not
		// buy an agent round. Both call sites run before any spawn; neither can
		// reach exhausted()/nothingToDeliver() (row 01a18392's other half: a
		// cancelled row was being written `failed` with the loop's own reason).
		if err := s.stopIfRowRetired(log); err != nil {
			return last, err
		}
		// ONE AGENT ROUND, FACTS ONLY.
		if err := s.runAgentRound(log, deliveryRetryBriefing(blk, s.dir, round)); err != nil {
			fmt.Fprintf(log, "task %s: the delivery round's agent exited with %v — the next attempt runs anyway\n",
				s.cfg.TaskID, err)
		}
	}
	// THE FORK (row 01a18244, measured on rows 01a17e38 / 01a17e7c / 01a17e04):
	// `blocker != nil && !last.Pushed` is true for TWO OPPOSITE facts, and this
	// one condition used to answer both with exhausted():
	//
	//	NOTHING TO DELIVER (devworkspace.BlockerKindEmpty) — `rev-list --count
	//	HEAD --not origin/master` answered 0, which is a CONCLUSION about the
	//	ground, not a refusal by any tool. An items=0 counter-proof run is
	//	exactly this shape: it was asked to produce no change, it produced none,
	//	and recording that as a failed delivery is the entry 01a18269 filed as
	//	"已交付成功被判成交付没有落地". It takes the nothing-to-deliver exit
	//	BELOW: the run COMPLETES with a reason that says so.
	//
	//	A REFUSED DELIVERY (dirty / pull / build / push) — every one of those is
	//	a STEP that refused, i.e. work that exists and could not be published.
	//	Those still go to exhausted(), unchanged: this fork must never widen a
	//	real failure into a success (the red line in this task's constraints).
	if blocker != nil && !last.Pushed {
		if blocker.Kind == devworkspace.BlockerKindEmpty {
			return s.nothingToDeliver(log, last, blocker)
		}
		// THE FIFTH OUTCOME (row 01a182df): the same `count == 0`, the OPPOSITE
		// fact — this run's own push already moved the base onto its tip, so the
		// count is 0 by construction. It is a COMPLETION (the work IS on the
		// integration branch) and must not be recorded as a failed delivery, for
		// the same measured reason the empty arm is not one.
		if blocker.Kind == devworkspace.BlockerKindAlreadyDelivered {
			return s.alreadyDelivered(log, last, blocker)
		}
		// THE SIXTH OUTCOME (epic: inherited delivery): the THIRD meaning of the
		// same `count == 0` — HEAD is already an ancestor of the integration
		// branch, so the content this ground was handed is on the base and this
		// run pushed nothing. Also a COMPLETION (nothing is owed, nothing was
		// refused) and also a kind that BREAKS the loop above, for the same
		// measured reason: an agent round cannot change an immutable fact.
		if blocker.Kind == devworkspace.BlockerKindInheritedDelivery {
			return s.inheritedDelivery(log, last, blocker)
		}
		return s.exhausted(log, last, blocker)
	}
	fmt.Fprintf(log, "task %s: %s (delivery attempt %d/%d)\n", s.cfg.TaskID, last.Detail, s.rounds, devworkspace.MaxDeliveryRounds)
	// THE LANDED RECORD, WRITTEN BEFORE THE WORKSPACE GOES (2026-10-27). The
	// push moved origin and the delivery proof that follows finds the tip by
	// ancestry in the ordinary case — but a `pull --rebase` REWRITES the run's
	// commits before pushing them, and a rebase that re-derived their content (a
	// regenerated table against a moved base) leaves nothing for a patch
	// comparison to match (row 01a17a67: cb84303… vs 4f1ab5b…). What survives
	// that is the DELIVERY ITSELF, so the channel records it: the branch, the
	// tip it just pushed, and the push's own words. It is written HERE because
	// the reclaim below deletes the only place the tip can still be read.
	s.recordLandedDelivery(log, last, cloneLandedArmDelivered)
	// THE REMOVAL RECORD, WITH THE TIP, BEFORE THE DIRECTORY GOES (measured
	// 2026-10-29). This reclaim is a bare `os.RemoveAll` of a repository of its
	// own: the moment it runs, the run's HEAD is unreadable and unwritten. The
	// record is emitted FIRST, carrying the sha the directory's HEAD names
	// (tools.RecordGroundRemovalTip reads it while it still exists), so the
	// delivery proof can keep answering BY SHA — `is this commit on the
	// integration branch?` — after the ground is gone. Without it the proof's
	// three witnesses were all absent for the ordinary case and a landed commit
	// read `delivery-proof-unverified` (rows 01a17de7 / 01a17de9).
	tools.RecordGroundRemovalTip(removalaudit.ArmCloneChannelReclaim,
		tools.TaskDeliverableRepoFor(s.cfg.TaskID), s.dir, landedRecordBranchOf(s.cfg, last), s.cfg.TaskID,
		"push accepted; the channel reclaims the clone ground (the code is at origin)")
	removed, rerr := devworkspace.Reclaim(s.dir)
	switch {
	case rerr != nil:
		fmt.Fprintf(os.Stderr, "task %s: clone workspace %s could not be reclaimed (the push already landed): %v\n",
			s.cfg.TaskID, s.dir, rerr)
	case removed:
		last.Reclaimed = true
		fmt.Fprintf(log, "task %s: reclaimed the clone workspace %s (the code is at origin)\n", s.cfg.TaskID, s.dir)
	}
	return last, nil
}

// errNothingToDeliver is the SENTINEL that tells the close-out caller which of
// the two opposite facts finish() returned (row 01a18244): the reason string is
// the same in both arms (Rule 14 — composed once), so the CLASSIFICATION has to
// be carried by the error itself rather than by matching its words. The caller
// (cmd/agent/cli_tasks.go) writes the run COMPLETED with this reason on the
// sentinel arm, and FAILED on every other error.
var errNothingToDeliver = errors.New(devworkspace.CloneChannelPrefix + "nothing to deliver")

// errRowRetired is the SENTINEL for the THIRD terminal fact a delivery loop can
// meet (row 01a18392): the ledger row was retired out from under the loop, so
// the loop STOPPED rather than ran to its round limit. It is an error because
// delivery did not happen — but it is not a failure the loop produced, so the
// caller must not write `failed` over an operator's `cancelled` decision.
var errRowRetired = errors.New(devworkspace.CloneChannelPrefix + "the row was retired during delivery")

// cloneRowRetiredStatuses is the set of ledger statuses that END a delivery
// loop early. It is a NAMED set (Rule 1/13) and it is deliberately ONE member:
// `cancelled` is the measured defect (an operator retired the row and watched
// it spend every attempt and spawn a bare-prompt round between each pair).
// `superseded` is NOT here because no run has been measured doing it — widening
// a brake without evidence is how a real delivery gets stopped by a status the
// operator intended to be invisible to it.
var cloneRowRetiredStatuses = []string{tools.TaskStatusCancelled}

// cloneRowRetired reports whether status ends the loop (exact match, never a
// prefix — a status token is an identifier, not prose).
func cloneRowRetired(status string) bool {
	return slices.Contains(cloneRowRetiredStatuses, strings.TrimSpace(status))
}

// retiredReasonError carries the composed reason as its own text and answers
// errors.Is(err, errRowRetired) — the same shape as nothingToDeliverReasonError,
// for the same reason (Rule 14: one composition, one reader).
type retiredReasonError struct{ reason string }

func (e *retiredReasonError) Error() string { return e.reason }

func (e *retiredReasonError) Is(target error) bool { return target == errRowRetired }

// cloneRoundRetiredReasonFmt is the ONE sentence a retired row gets: it names
// the status, the attempts spent before the loop noticed, and the workspace that
// is KEPT (the row's commits are its owner's decision to abandon, never this
// loop's to delete).
const cloneRoundRetiredReasonFmt = "the row is %s, so the delivery loop stopped after %d attempt(s) " +
	"and started no further agent round; the workspace %s is KEPT (a retired row's work is not this loop's to delete)"

// cloneRoundStatusUnreadableFmt is the sentence a FAILED status read leaves.
// A read that could not be taken is not a permission (Rule 15): the loop keeps
// its old, unconditional behaviour instead of inventing a stop.
const cloneRoundStatusUnreadableFmt = "task %s: the ledger status could not be read (%v) — the delivery loop runs its usual rounds\n"

// stopIfRowRetired is THE ROUND BOUNDARY. It reads the ROW's own status and, on
// a retired row, returns errRowRetired so finish() stops on the spot: no further
// attempt, no agent round, and — the other half of row 01a18392 — NO
// exhausted()/nothingToDeliver() exit, because neither sentence is true of a row
// whose owner ended it.
//
// It is called at BOTH boundaries inside the loop (before each attempt, and
// before each retry round) because the two are different moments: a cancel that
// lands while an attempt is running must not buy the round that follows it.
func (s *cloneRunState) stopIfRowRetired(log io.Writer) error {
	status, err := s.rowStatusOf()
	if err != nil {
		fmt.Fprintf(log, cloneRoundStatusUnreadableFmt, s.cfg.TaskID, err)
		return nil
	}
	if !cloneRowRetired(status) {
		return nil
	}
	reason := fmt.Sprintf(cloneRoundRetiredReasonFmt, status, s.rounds, s.dir)
	fmt.Fprintf(log, "task %s: %s\n", s.cfg.TaskID, reason)
	return &retiredReasonError{reason: reason}
}

// groundNeverCommitted is the produced-nothing brake's ONE reader (Rule 14): it
// asks the ground, states the reading on the log either way it is DECIDED, and
// answers "may this loop stop spending attempts".
//
// A reading that could not be taken is printed and does NOT stop the loop: the
// rounds stay exactly as they were for every ground whose state is unknown
// (Rule 15 — an abstention never changes an outcome in either direction).
func (s *cloneRunState) groundNeverCommitted(log io.Writer) bool {
	probe := s.groundProducedNothing
	if probe == nil {
		probe = tools.TaskGroundProducedNothing
	}
	read := probe(s.dir, s.cfg.Branch)
	switch {
	case !read.Measured:
		fmt.Fprintf(log, "task %s: the produced-nothing reading could not be taken (%s) — the delivery rounds run as usual\n",
			s.cfg.TaskID, read.Evidence)
		return false
	case read.Nothing && !read.Pending:
		fmt.Fprintf(log, "task %s: "+cloneChannelProducedNothingFmt+"\n", s.cfg.TaskID, read.Evidence)
		return true
	}
	return false
}

// cloneChannelProducedNothingFmt is the brake's ONE sentence (Rule 1b). It says
// the measured fact, the fact that no round can change it, and what happens next
// — the three things a reader of the log needs, in that order.
const cloneChannelProducedNothingFmt = devworkspace.CloneChannelPrefix + "this ground NEVER produced a commit — %s. " +
	"No delivery round can change that (there is nothing to commit and nothing to push), so the remaining " +
	"attempts are not spent; the run ends with the nothing-to-deliver conclusion."

// rowStatusOf is the ONE status read the loop makes (Rule 14): the injected one
// under test, tools.TaskRowStatusAt in production.
func (s *cloneRunState) rowStatusOf() (string, error) {
	if s.rowStatus != nil {
		return s.rowStatus(s.cfg.TaskID)
	}
	return tools.TaskRowStatusAt(s.cfg.TaskID)
}

// nothingToDeliverReasonError carries the composed reason as its OWN text, so
// the ledger row and the returned error are one string (Rule 14) — while
// errors.Is still answers "was this the nothing-to-deliver exit?".
type nothingToDeliverReasonError struct{ reason string }

func (e *nothingToDeliverReasonError) Error() string { return e.reason }

// Is makes errors.Is(err, errNothingToDeliver) true for this class without
// wrapping the reason in a prefix that would read as a second failure.
func (e *nothingToDeliverReasonError) Is(target error) bool { return target == errNothingToDeliver }

// WHY IT IS NOT exhausted() (the whole point of row 01a18244): the two arms
// answer different facts. exhausted() means "there is work and it could not be
// published — the workspace is KEPT so nobody loses it". This one means "there
// is no work past the integration point", which is a CONCLUSION, and the only
// thing a run can do about a conclusion is record it and stop. Merging them
// recorded items=0 counter-proof runs as delivery failures (rows 01a17e38,
// 01a17e7c, 01a17e04 all closed that way).
//
// The reason is composed ONCE, here and nowhere else, and it is both the
// ledger's row reason and the returned error (Rule 14) — the caller
// (cmd/agent/cli_tasks.go) writes cerr.Error() into the row verbatim.
//
// The wording is deliberate and is asserted by a test: it carries "nothing to
// deliver" and it does NOT carry "delivery did not land" — an operator reading
// the row must be able to tell which of the two facts happened without opening
// the log.
func (s *cloneRunState) nothingToDeliver(log io.Writer, last devworkspace.Result, blk *devworkspace.Blocker) (devworkspace.Result, error) {
	// The workspace goes, exactly as on the delivered path: with no commit to
	// publish it holds nothing the origin does not already have, and KEEPING it
	// would leave a directory whose only contents are a clone of master — the
	// residue this channel exists to not produce. Contrast exhausted(), whose
	// whole point is that the workspace IS kept because it holds undelivered
	// work.
	// COMPOSED THROUGH THE ONE DOOR, for the same measured reason exhausted()
	// is: `nothing to deliver — clone channel: …` was this arm's shape the
	// moment its body came from (*Blocker).Report().
	reason := cloneChannelReason(fmt.Sprintf("nothing to deliver — %s (after %d delivery attempt(s), "+
		"the workspace %s is RECLAIMED: it held no commit past %s)",
		cloneChannelBody(blk.Report()), s.rounds, s.dir, devworkspace.DefaultBranch))
	fmt.Fprintf(log, "task %s: %s\n", s.cfg.TaskID, reason)
	// THE SAME CLOSING ROUND AS EVERY OTHER TERMINAL PATH: the run still owes a
	// report, and it is the round that TELLS the agent the outcome (a report
	// written as if the delivery had failed is the second half of this defect).
	if err := s.runAgentRound(log, deliveryNothingToDeliverBriefing(blk, s.dir, s.rounds)); err != nil {
		fmt.Fprintf(log, "task %s: the closing report round exited with %v\n", s.cfg.TaskID, err)
	}
	// THE SAME RECORD ON THIS EXIT, for the same reason (measured 2026-10-29):
	// this arm also deletes a clone ground, and the tip its HEAD names is the
	// only subject a later delivery judgement could have. It produced no commit
	// of its own past the base, so the sha it records is the base's — which is
	// precisely what makes the produced-nothing verdict (tipIsTheCutPoint)
	// answerable AFTER the directory is gone, instead of unanswerable.
	tools.RecordGroundRemovalTip(removalaudit.ArmCloneChannelReclaim,
		tools.TaskDeliverableRepoFor(s.cfg.TaskID), s.dir, landedRecordBranchOf(s.cfg, last), s.cfg.TaskID,
		"nothing to deliver past "+devworkspace.DefaultBranch+"; the channel reclaims the clone ground")
	if removed, rerr := devworkspace.Reclaim(s.dir); rerr != nil {
		fmt.Fprintf(os.Stderr, "task %s: clone workspace %s could not be reclaimed (nothing was pending in it): %v\n",
			s.cfg.TaskID, s.dir, rerr)
	} else if removed {
		last.Reclaimed = true
	}
	return last, &nothingToDeliverReasonError{reason: reason}
}

// errInheritedDelivery is the SENTINEL for the SIXTH terminal fact (epic:
// inherited delivery): HEAD is already an ancestor of the integration branch, so
// `count == 0` because the content this ground was handed is ON the base — not
// because the ground is empty and not because this run pushed it. It is an error
// because nothing more will be published here, but it is a COMPLETION, and the
// caller writes the row with this sentence as its reason (Rule 15: the three
// meanings of one zero never share a sentence).
var errInheritedDelivery = errors.New(devworkspace.CloneChannelPrefix + "the content this run was handed is already on the integration branch")

// inheritedDeliveryReasonError carries the composed reason as its own text and
// answers errors.Is(err, errInheritedDelivery) — the same shape as its siblings
// (Rule 14: one composition, one reader).
type inheritedDeliveryReasonError struct{ reason string }

func (e *inheritedDeliveryReasonError) Error() string { return e.reason }

func (e *inheritedDeliveryReasonError) Is(target error) bool { return target == errInheritedDelivery }

// inheritedDelivery is the INHERITED-DELIVERY exit: the base already contains
// this ground's HEAD, so there is nothing to publish and — this is what the
// empty sentence got wrong — nothing to pull either.
//
// IT SPENDS NO AGENT ROUND, and that is the second half of the defect this exit
// closes: the empty kind did not break the delivery loop, so each of up to
// MaxDeliveryRounds rounds spawned a real agent round that re-measured the same
// immutable fact and registered a new ledger row (measured: 50 sessions whose
// log carries `carries no commit to deliver`). A round spawned to say "nothing
// to do" is exactly the zombie-row source alreadyDelivered documents.
func (s *cloneRunState) inheritedDelivery(log io.Writer, last devworkspace.Result, blk *devworkspace.Blocker) (devworkspace.Result, error) {
	reason := cloneChannelReason(fmt.Sprintf("the content this run was handed is ALREADY ON THE INTEGRATION BRANCH — %s "+
		"(after %d delivery attempt(s); the workspace %s is RECLAIMED: %s)",
		cloneChannelBody(blk.Report()), s.rounds, s.dir, devworkspace.DefaultBranch))
	fmt.Fprintf(log, "task %s: %s\n", s.cfg.TaskID, reason)
	// THE REMOVAL RECORD GOES FIRST, for the same measured reason the delivered
	// arm writes it: the reclaim deletes the only place the tip can still be read.
	tools.RecordGroundRemovalTip(removalaudit.ArmCloneChannelReclaim,
		tools.TaskDeliverableRepoFor(s.cfg.TaskID), s.dir, landedRecordBranchOf(s.cfg, last), s.cfg.TaskID,
		"the content this run was handed is already on the integration branch; the channel reclaims the clone ground")
	if removed, rerr := devworkspace.Reclaim(s.dir); rerr != nil {
		fmt.Fprintf(os.Stderr, "task %s: clone workspace %s could not be reclaimed (its content is already on the base): %v\n",
			s.cfg.TaskID, s.dir, rerr)
	} else if removed {
		last.Reclaimed = true
	}
	return last, &inheritedDeliveryReasonError{reason: reason}
}

// errAlreadyDelivered is the SENTINEL for the FIFTH terminal fact (row
// 01a182df): this ground's `count == 0` is the arithmetic of a delivery THIS
// RUN already landed, not of an empty ground. It is an error because nothing
// more is going to be published here — but it is a COMPLETION, so the caller
// writes the row `completed` with this sentence as its reason, exactly as on the
// nothing-to-deliver arm and for the same reason: `failed` would tell the next
// reader that work sitting on the integration branch never arrived.
var errAlreadyDelivered = errors.New(devworkspace.CloneChannelPrefix + "the delivery already landed")

// alreadyDeliveredReasonError carries the composed reason as its own text and
// answers errors.Is(err, errAlreadyDelivered) — the same shape as the other two
// (Rule 14: one composition, one reader).
type alreadyDeliveredReasonError struct{ reason string }

func (e *alreadyDeliveredReasonError) Error() string { return e.reason }

func (e *alreadyDeliveredReasonError) Is(target error) bool { return target == errAlreadyDelivered }

// alreadyDelivered is the ALREADY-DELIVERED exit: the ground carries nothing
// past the base because this run's own push put it there.
//
// ITS SENTENCE IS NOT dead-lettered FROM nothingToDeliver's (Rule 15, and the
// measured cost this file has paid twice: one fact wearing two opposite
// sentences). Both are completions; they are opposite FACTS, and an operator's
// next action differs — "you produced nothing" means look for missing work,
// "your delivery landed" means do not look at all.
func (s *cloneRunState) alreadyDelivered(log io.Writer, last devworkspace.Result, blk *devworkspace.Blocker) (devworkspace.Result, error) {
	reason := cloneChannelReason(fmt.Sprintf("the delivery ALREADY LANDED — %s (after %d delivery attempt(s); "+
		"the workspace %s is RECLAIMED: it holds nothing this run has not already pushed)",
		cloneChannelBody(blk.Report()), s.rounds, s.dir))
	fmt.Fprintf(log, "task %s: %s\n", s.cfg.TaskID, reason)
	// THE LANDED RECORD ON THIS ARM TOO (this row's defect). This arm's whole
	// meaning is "a push of this ground already landed", so the row MUST carry
	// the tip that push produced: without it the next round's DeliveredTip read
	// answers "" and the empty gate falls back to "carries no commit to deliver"
	// — which is exactly what made this exit reachable in the first place.
	// Read (and recorded) BEFORE the reclaim below, which deletes the directory
	// the tip is read from.
	s.recordLandedDelivery(log, last, cloneLandedArmAlreadyDelivered)
	// NO CLOSING AGENT ROUND: nothing is owed. The report belongs to the round
	// that delivered, and a round spawned here would be a process minted to say
	// "nothing to do" — the zombie-row source this file has closed once already
	// (row 01a18392).
	//
	// THE REMOVAL RECORD IS WRITTEN FIRST, for exactly the reason the delivered
	// arm writes it: this reclaim deletes the only place the tip could still be
	// read, and a later judgement ("is this commit on the integration branch?")
	// must stay answerable by sha afterwards.
	tools.RecordGroundRemovalTip(removalaudit.ArmCloneChannelReclaim,
		tools.TaskDeliverableRepoFor(s.cfg.TaskID), s.dir, landedRecordBranchOf(s.cfg, last), s.cfg.TaskID,
		"the delivery already landed; the channel reclaims the clone ground")
	if removed, rerr := devworkspace.Reclaim(s.dir); rerr != nil {
		fmt.Fprintf(os.Stderr, "task %s: clone workspace %s could not be reclaimed (its delivery already landed): %v\n",
			s.cfg.TaskID, s.dir, rerr)
	} else if removed {
		last.Reclaimed = true
	}
	return last, &alreadyDeliveredReasonError{reason: reason}
}

// cloneChannelReason is THIS PACKAGE'S name for the channel's ONE composition
// door (devworkspace.CloneChannelReason) — the same door internal/agent's
// end-hook leg goes through (internal/agent/clonedelivery.go).
//
// WHY THE DOOR IS SHARED AND NOT RE-DERIVED HERE (fixed 2026-10-31). Every
// terminal arm below interpolates (*Blocker).Report(), and Report() opens EVERY
// Kind branch with the channel's prefix — so an arm that pasted the prefix into
// its own template produced `clone channel: delivery did not land in 3
// attempt(s); last blocker (dirty): clone channel: refusing to pull: …`
// (measured PROBE_PREFIX_COUNT=2 on this file's exhausted()). Deleting the
// prefix at the call sites would have traded a stutter for a line with no
// speaker; the cure is one IDEMPOTENT composition — trim, strip at most one
// prefix off the body, add exactly one back — owned by the package that owns
// the prefix (Rule 1/13), so Count("clone channel: ") == 1 is a property of the
// composition rather than of four callers' memory.
//
// The reverse proof is
// TestCloneChannelExhaustedReasonCarriesThePrefixExactlyOnce: remove this
// wrapper (or the strip inside it) and the count reads 2.
func cloneChannelReason(body string) string { return devworkspace.CloneChannelReason(body) }

// cloneChannelBody is this package's name for the shared BODY normaliser
// (devworkspace.CloneChannelBody): trimmed, with ONE leading channel prefix
// removed.
//
// IT IS THE HALF THAT FIXES THE EMBEDDED STUTTER, and the distinction is easy to
// miss: a composed line's OWN leading prefix is one thing, the prefix a
// (*Blocker).Report() carries INTO the middle of a template is another. In
// exhausted() the report is interpolated after `last blocker (dirty): `, so it
// is NOT at the start of the line and no amount of stripping the whole line
// would touch it — the body has to be normalised BEFORE it is inserted. That is
// exactly what this function is for, and why every Report() interpolation in
// this file reads `cloneChannelBody(blk.Report())`.
func cloneChannelBody(body string) string { return devworkspace.CloneChannelBody(body) }

// THE LANDED RECORD, WRITTEN BY EVERY ARM THAT KNOWS A PUSH LANDED.
//
// THE DEFECT THIS CLOSES (this row's own family, measured on 01a19bf1): the
// delivered arm wrote `clone-delivery-landed` and the ALREADY-DELIVERED arm did
// not, so a ground whose own push landed carried no record of it. The reader
// (`tools.TaskLandedDeliveryTip` → `Config.DeliveredTip`) then answered "" on the
// next round, the empty gate lost its second reading, and the loop spent all
// MaxDeliveryRounds re-measuring an immutable fact — the very line this epic was
// filed on: `carries no commit to deliver … (after 3 delivery attempt(s) …)`.
//
// WHY IT IS A METHOD AND NOT TWO COPIES. Both arms know the same fact by the
// same measurement (`tools.CloneDeliveryTip` reads the ground's HEAD) and both
// must record it in the same grammar through the same ONE writer, or the writer
// and the reader drift (Rule 14). The `arm` argument exists so the reason a
// record could NOT be written names WHICH arm could not write it.
//
// THE TIP IS READ BEFORE THE RECLAIM, always: `s.dir` is deleted a few lines
// later and it is the only place the sha can still be read. So this call is the
// last word about the tip, and (Rule 15) an unreadable tip is said OUT LOUD in
// the RUN'S OWN LOG — not only on stderr. Measured on this ground before the
// fix: `finish`'s `task %s: …` lines and this arm's stderr sentence were both
// absent from the run's output.log, because the child's streams go to the run
// log while `finish`'s `log` is the operator's stream. A run whose delivery
// landed and whose record could not be written therefore left NOTHING a later
// reader could diagnose it by, which is the half of this epic that is not about
// the ledger row.
func (s *cloneRunState) recordLandedDelivery(log io.Writer, last devworkspace.Result, arm string) {
	tip := tools.CloneDeliveryTip(s.dir)
	if tip == "" {
		said := cloneChannelReason(fmt.Sprintf(cloneLandedTipUnreadableFmt, s.cfg.TaskID, arm, s.dir))
		// THE RUN'S OWN RECORD IS `cfg.Log`, NOT THE `log` PARAMETER — and that
		// distinction is the whole requirement (measured on this ground): the
		// caller passes `os.Stdout` here (cli_tasks.go's `cloneRun.finish(os.Stdout)`),
		// which is the OPERATOR's terminal, while the child's streams — the run's
		// output.log — are what `cfg.Log` is bound to (`execRunFacts`, a
		// MultiWriter of the run log and stderr). A sentence written only to the
		// parameter would be invisible to the agent that reads output.log next,
		// which is exactly the silence this arm exists to end.
		if s.cfg.Log != nil {
			fmt.Fprintf(s.cfg.Log, "%s\n", said)
		}
		fmt.Fprintf(log, "%s\n", said)
		fmt.Fprintf(os.Stderr, "%s\n", said)
		return
	}
	tools.TaskDeliveryRecordLanded(s.cfg.TaskID, landedRecordBranchOf(s.cfg, last), s.dir, tip, landedRecordDetail(last, arm))
}

// landedRecordDetail is the record's detail column: the push's own words when
// the arm has them, and a sentence NAMING THE ARM when it does not (the
// already-delivered arm pushed nothing in this process, so `last.Detail` is
// empty there and an empty detail would be filed as "pushed to origin" by the
// writer — a claim about a push this arm did not make).
func landedRecordDetail(last devworkspace.Result, arm string) string {
	if d := strings.TrimSpace(last.Detail); d != "" {
		return d
	}
	return fmt.Sprintf(cloneLandedRecordArmFmt, arm)
}

// THE TWO ARMS THAT KNOW A PUSH LANDED (Rule 1b: the arm is an identifier, not
// prose invented at the call site).
const (
	cloneLandedArmDelivered        = "delivered"
	cloneLandedArmAlreadyDelivered = "already-delivered"
)

// cloneLandedTipUnreadableFmt is the ONE sentence for "the delivery landed but
// its record could not be written". It names the row, the ARM, the directory and
// the consequence — the four things a later reader needs to decide whether the
// missing record is a defect or a discard.
const cloneLandedTipUnreadableFmt = "the delivery landed but its `%s` record could NOT be written (arm=%s): " +
	"the pushed tip could not be read from %s, so this row carries no tip for the proof to judge — " +
	"a rebase that re-derived this run's content will make its delivery unprovable"

// cloneLandedRecordArmFmt is the detail clause for an arm that pushed nothing in
// THIS process: it says which arm made the claim, so the record is never read as
// "this process pushed it".
const cloneLandedRecordArmFmt = "the delivery already landed (recorded by the %s arm, which pushed nothing itself)"

// exhausted is the end of the road: every delivery attempt was blocked. The

// reason is composed ONCE — it is the ledger's row reason and the returned error
// verbatim, so the two cannot disagree (Rule 14) — and the agent gets its ONE
// closing round to write the report.
func (s *cloneRunState) exhausted(log io.Writer, last devworkspace.Result, blk *devworkspace.Blocker) (devworkspace.Result, error) {
	// THE ONE DOOR (fixed 2026-10-31, the stutter defect). This sentence used to
	// paste the prefix itself AND interpolate blk.Report() — which opens every
	// Kind branch with the same prefix — so the row's reason and the returned
	// error read `clone channel: delivery did not land in 3 attempt(s); last
	// blocker (dirty): clone channel: refusing to pull: …` (measured:
	// PROBE_PREFIX_COUNT=2). cloneChannelReason trims, strips AT MOST one prefix
	// off the body and adds exactly one back, so the count is decided HERE and
	// not by which of the two conventions the body happened to follow. The
	// template therefore carries NO prefix, and its %s is a BODY.
	reason := cloneChannelReason(fmt.Sprintf(
		"delivery did not land in %d attempt(s); last blocker (%s): %s — the workspace %s is KEPT",
		s.rounds, blk.Kind, cloneChannelBody(blk.Report()), s.dir))
	fmt.Fprintf(log, "task %s: %s\n", s.cfg.TaskID, reason)
	if err := s.runAgentRound(log, deliveryExhaustedBriefing(blk, s.dir, s.rounds)); err != nil {
		fmt.Fprintf(log, "task %s: the closing report round exited with %v\n", s.cfg.TaskID, err)
	}
	return last, errors.New(reason)
}

// runAttempt is ONE delivery attempt: the injected one when a test supplied it,
// devworkspace.AttemptDelivery otherwise. One place, so the loop's body cannot
// take a different path under test than in production (Rule 14).
func (s *cloneRunState) runAttempt() (devworkspace.Result, *devworkspace.Blocker, error) {
	if s.attempt != nil {
		return s.attempt()
	}
	return devworkspace.AttemptDelivery(context.Background(), s.cfg, s.dir)
}

// runAgentRound starts ONE agent in the workspace with the briefing on its
// stdin. It is the ONLY place this channel pulls a model into the delivery
// loop, and it is called at most MaxDeliveryRounds+1 times per run.
func (s *cloneRunState) runAgentRound(log io.Writer, briefing string) error {
	fmt.Fprintf(log, "task %s: starting an agent round in %s\n", s.cfg.TaskID, s.dir)
	if s.agentRound != nil {
		return s.agentRound(s.dir, briefing)
	}
	env, err := cloneRoundChildEnv(os.Environ(), s.cfg.TaskID)
	if err != nil {
		// REPORTED, never silent (Rule 15): the round would otherwise run as a
		// NEW bare-prompt run of its own — a fresh ledger row and a fresh clone
		// workspace — which is the reproduction loop this seam exists to stop.
		fmt.Fprintf(os.Stderr, cloneRoundReportUnresolvableFmt, s.cfg.TaskID, err)
		return err
	}
	return spawnCloneChannelAgent(s.dir, briefing, env)
}

// cloneRoundReportUnresolvableFmt is the ONE sentence printed when the round's
// report destination cannot be resolved (Rule 1: the spelling lives here, not
// inline at the call site).
const cloneRoundReportUnresolvableFmt = "task %s: the closing round's report destination could not be resolved, so no round is started " +
	"(a round without its destination would mint a row and a workspace of its own): %v\n"

// cloneRoundChildEnv assembles the environment for ONE closing agent round:
// this process's own environment, MINUS every execution-context pair that would
// make the child a run of its own, PLUS the report destination that keeps its
// report on the PARENT row.
//
// WHY THE SUBTRACTION IS THE WHOLE FIX (epic 01a1837e, measured 2026-10-29).
// The round used to be started with whatever environment this process happened
// to carry. As a bare prompt (no GOPI_TASK_WRAPPER_ID) it walked the start
// hook's auto-registration, minted an `[auto]` ledger row of its own, and — being
// a plain prompt — had adoptCloneGround open a SECOND workspace under that new
// row's id; its close-out then ran the delivery loop again and spawned another
// closing round. The loop is self-sustaining: 39 of the 44 in_progress rows on
// this ledger are the rounds it produced.
//
// So the child is given an EXECUTION CONTEXT that says what it is: the same
// terminal row this round reports on, its closing-round role, and NO
// auto-registration of its own. Three faces of one decision, and the ORDER
// matters — the subtraction runs on the inherited environment first, so no
// inherited copy of these names can survive beside the values written here
// (Rule 14 of the child-env contract: a duplicated name is order- and
// reader-dependent, so the child gets exactly ONE entry per name).
//
// WHAT IS DELIBERATELY KEPT, so a later reader does not "complete" the list:
// the report pair (re-exported above: the round's report belongs to the parent
// row — tools.TaskReportPath resolves the parent's own session directory, which
// is also what the child's startup guard accepts as its own); the run-id
// lineage this process already carries (GOPI_TASK_RUN_ID and its outcome/pid
// siblings — the terminal-row witness a `gopi task show` reads back);
// GOPI_TASK_WORKDIR, GOPI_HOME, the model credentials and PATH (subtraction,
// never a rebuilt allowlist — a whitelist would silently drop the next variable
// someone adds).
func cloneRoundChildEnv(inherited []string, parentTaskID string) ([]string, error) {
	parentTaskID = strings.TrimSpace(parentTaskID)
	if parentTaskID == "" {
		return nil, fmt.Errorf("the parent row id is empty")
	}
	// TaskReportDestination, not TaskReportPath: the exported path must be
	// WRITABLE before it enters a child's environment (it creates the parent
	// container and probes a real write). The read faces keep the plain path.
	reportPath, err := tools.TaskReportDestination(parentTaskID)
	if err != nil {
		return nil, err
	}
	// SUBTRACT first: the round must not inherit a report contract (the pair
	// below is the ONE writer for the round) and must not inherit somebody
	// else's wrapper identity — the parent row's is written below.
	env := stripChildEnvNames(inherited,
		agent.AutoTaskRegisterOff,
		taskreport.PathEnv,
		taskreport.OwnEnv,
		tools.TaskWrapperEnv,
		// The resume declaration is SUBTRACTED like every other name this
		// function owns, so an inherited copy cannot survive beside the value
		// written below (a duplicated name is order- and reader-dependent).
		tools.TaskRunResumeFromEnv,
	)
	// ADD the round's own execution context, ONE entry per name.
	env = append(env,
		// NO LEDGER ROW OF ITS OWN — the fix. It is the kill switch
		// internal/agent already owns (one NAME=VALUE string, never split).
		agent.AutoTaskRegisterOff,
		// THE SAME ROW THIS ROUND REPORTS ON. Two consequences, both wanted:
		// the round's session artifacts land in the row's own directory
		// (sessions/<parent id>/, the place `gopi task show` reads), and the
		// bare-prompt clone-ground adoption is skipped — the channel owns the
		// ground and the delivery, a round does neither.
		tools.TaskWrapperEnv+"="+parentTaskID,
		// THE REPORT DESTINATION, pointing at the PARENT row's session
		// directory rather than a directory minted for this round (the orphan
		// report the epic's second half is about).
		taskreport.PathEnv+"="+reportPath,
		taskreport.OwnEnv+"="+parentTaskID,
	)
	// THE RESUME WITNESS, DECLARED BY THIS DOOR (bug 01a193f5, measured
	// 2026-11-01). The round is started with the parent row's id as its wrapper
	// identity, so internal/agent's mintRunID returns that id and the child's
	// session logger APPENDS a second `session_start` to the record the row's
	// executor already wrote. Without a declaration that header said nothing
	// about why the file holds two — a continued record was indistinguishable
	// from a corrupt one, and the census that measures exactly that reported 6
	// live gaps AFTER the re-dispatch door had been fixed.
	//
	// WHY THE DECLARATION COMES FROM HERE: this is the second process that
	// decides to run a row which already has a session record, so it is the
	// second place the fact is KNOWN rather than inferred. It asks the one
	// resolver (tools.TaskRunResumeDeclare — the same call the executor seam in
	// cli_tasks.go makes), so the two doors cannot disagree about what counts as
	// a re-entry, and a row on its FIRST run declares nothing: the predicate
	// reads the row's own run DIRECTORY, which the delivery round reaches only
	// after the executor's record is already on disk.
	//
	// A round that cannot make the declaration still runs: silence here costs
	// the witness, not the delivery, and the census's LIVE-GAP bucket is what
	// makes that cost visible rather than invisible.
	if decl, ok := tools.TaskRunResumeDeclare(parentTaskID); ok {
		env = append(env, decl.Env())
	}
	return env, nil
}

// stripChildEnvNames returns env without any entry whose NAME appears in names.
// The names are given in their "NAME" or "NAME=value" spelling and only the
// name half is compared, so the same list serves the subtraction and the
// constant above cannot drift from what the addition writes (Rule 13: one
// definition, two references).
func stripChildEnvNames(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if slices.ContainsFunc(names, func(n string) bool { return envNameOf(n) == name }) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// envNameOf returns the NAME half of a "NAME=value" spelling (the value half of
// the exported OFF pair must not be compared as a name).
func envNameOf(kv string) string {
	name, _, _ := strings.Cut(kv, "=")
	return name
}

// spawnCloneChannelAgent runs the delivery round's agent: the same gopi binary
// this process is, standing in the workspace, reading the briefing from stdin —
// the shape `task run`'s own child uses (gopiExecutable + cmd.Stdin), so the
// round is an ordinary gopi run and inherits its whole tool surface.
//
// Its output goes to the run's own stream (os.Stdout/os.Stderr), which the
// wrapper has already pointed at the row's session log — so the round is
// reviewable afterwards rather than swallowed here.
func spawnCloneChannelAgent(dir, briefing string, env []string) error {
	return cloneRoundCmd(dir, briefing, env).Run()
}

// cloneRoundCmd BUILDS the round's command without starting it — the seam a
// test can read (Rule 31: a surface nobody executes is not delivered, and the
// environment the child receives is exactly what this returns).
//
// WHY IT IS SPLIT OUT, AND WHAT IT REPLACES (measured 2026-10-29, task
// 01a1837e). This body used to be spawnCloneChannelAgent's own, and it read:
//
//	cmd.Env = env                                        // ← cloneRoundChildEnv's work
//	...
//	cmd.Env = append(os.Environ(),                       // ← and then THROWN AWAY
//		tools.TaskGroundDirEnv+"="+dir,
//	)
//
// Two assignments to one field: the second discarded the first. The OFF pair,
// the wrapper row id and the report destination never reached the child, so the
// closing round stayed a bare prompt and the ledger loop kept minting rows —
// while the fix sat in the tree, green in its test, and unreachable in
// production (that test called cloneRoundChildEnv directly and never looked at
// what the spawn seam actually passed). The ground pair is therefore WRITTEN
// INTO the assembled environment now, through withEnvPair, and this function is
// what a test can assert against.
func cloneRoundCmd(dir, briefing string, env []string) *exec.Cmd {
	if len(env) == 0 {
		// No assembled environment was handed in (no production caller does
		// this): inherit this process's, never an empty child.
		env = os.Environ()
	}
	cmd := exec.Command(gopiExecutable())
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(briefing)
	// The round works in THIS run's workspace: standing in it is the whole
	// briefing's premise, and the ground env makes it readable to any tool the
	// agent spawns (the same pair task run exports, Rule 2). withEnvPair
	// REPLACES an inherited copy rather than adding a second entry — a name
	// carried twice is order- and reader-dependent (the child-env contract
	// cloneRoundChildEnv states).
	cmd.Env = withEnvPair(env, tools.TaskGroundDirEnv, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// withEnvPair returns env with exactly ONE entry for name, carrying value.
// The subtraction goes through the same stripChildEnvNames the assembly uses,
// so the two halves cannot disagree about what "the same name" means (Rule 13).
func withEnvPair(env []string, name, value string) []string {
	out := stripChildEnvNames(env, name)
	return append(out, name+"="+value)
}

// verbatimWords prepares the tool's output for a briefing WITHOUT rewording it:
// only the trailing newlines go (they would just add blank lines to the block).
// Leading spaces stay — `git status --porcelain`'s two-column spelling puts the
// worktree/staging side IN those columns, so trimming them would silently change
// what git said (measured: a first cut used strings.TrimSpace and turned `" M
// file"` into `"M file"`, i.e. "staged modification" instead of "unstaged").
func verbatimWords(w string) string {
	return strings.TrimRight(w, "\n")
}

// deliveryBriefingMarked puts the CLONE CHANNEL's harness marker in front of one
// delivery-round briefing. It is the ONE place the marker is applied, so the
// three briefings cannot disagree about whether they carry it (Rule 13/14) and a
// fourth exit added later cannot forget it — its author has to reach past this
// function to omit it.
//
// WHY THE MARKER EXISTS AT ALL (measured 2026-10-30, session
// 6a0ac1c6-4e6c-4155-8f5b-91d53a441663). The round's briefing reaches the child
// agent as its STDIN prompt — the run's FIRST and only user input — and a stdin
// prompt is recorded from the HUMAN face because there is no other provenance a
// `gopi < stdin` child can honestly claim. So the row was an ordinary operator
// line to every reader: `session log --turns` printed the framework's sentence
// about a delivery that did not land as the conversation's ONE human turn, and
// the chats index took it as `first_msg`. The marker is what makes the row
// readable as what it is — the framework talking — without changing the
// provenance field (which is a fact about HOW the child was started) or the
// text's meaning for the model (which reads the whole briefing either way).
//
// IT LEADS, DELIBERATELY, AND THE BUDGET WAS MEASURED. The ledger summary is
// `[auto] ` + the briefing folded to ONE line and cut at
// agent.AutoTaskSummaryBudget runes FROM THE START (see deliveryDecisiveBlock's
// header), so every rune spent before the decisive lines pushes them out. The
// marker is 25 runes and the measured rows carry it comfortably:
//
//	retry/empty   the measurement now begins at rune 36 of 200 (was 27)
//	pull conflict `CONFLICT (content): Merge conflict in internal/tools/…` intact
//	dirty         `" M internal/devworkspace/devworkspace.go"` intact, columns kept
//
// — asserted by the budget tests in this package rather than left to prose.
func deliveryBriefingMarked(body string) string {
	return sessionevent.HarnessCloneChannelBriefingMarker + body
}

// deliveryRetryBriefing is what a blocked delivery attempt hands the next agent
// round: THE FACTS, and only the facts.
//
//	where the workspace is  → so the agent can look
//	the tool's words         → verbatim, so it recognises the failure itself
//	which round this is      → so it can pace itself (and knows it is bounded)
//
// It deliberately contains NO next-step instruction. The choice between
// committing, discarding, reconciling a conflict or fixing a build is the
// agent's, which is the entire point of handing back a Blocker instead of
// running a fixed pipeline: a prescription written here would be the pipeline
// again, only in prose (the red line in this task's constraints).
func deliveryRetryBriefing(blk *devworkspace.Blocker, dir string, round int) string {
	// "NOTHING TO DELIVER" IS NOT A BLOCKED STEP, and the briefing must not say
	// it is: "第 N 轮交付尝试被 empty 这一步挡下了" sends the agent (and the
	// operator reading the transcript) looking for a step that refused, i.e. the
	// exact ghost-chasing this split exists to end. That kind gets its own
	// sentence, and it names the ONE fact that matters — this directory carries
	// no commit — without pretending something collided.
	//
	// AND IT NOW OPENS WITH THE DECISIVE BLOCK, like the refused branch below
	// (row 01a18801, measured on the shipped ledger). This branch used to open
	// with a fixed 149-rune prefix — the path plus "第 N/M 轮检查量到的结果是" —
	// and the row summary is `[auto] ` + the briefing folded to one line and cut
	// at agent.AutoTaskSummaryBudget (200) runes FROM THE START. So the only
	// sentence that says WHY the ground holds nothing — `… carries no commit to
	// deliver: rev-list --count … answered 0 against origin/master, so there is
	// nothing to push, but origin/master carries N commit(s) this ground does
	// not have — git pull --rebase origin master recovers them` — began at rune
	// 150 and was cut every time. 33 rows on the ledger carry the measurement and
	// not one of them carries a word of it:
	//
	//	[auto] 交付没有内容可发布。工作区仍是你的，在 /home/user/workspace/…。
	//	       第 2/3 轮检查量到的结果是「这里没有要交付的提交」——不是 pull 被拒，
	//	       也不是冲突， 不要去查网络。工具原话（逐字，未改写）： /home/user/…
	//
	// That is the refused branch's defect one kind over, and it takes the same
	// cure: the measurement leads, the path and the 「工具原话」 label move
	// behind it. The path is not dropped — it is the fourth thing the briefing
	// says (and word one of the tool's own words, handed over verbatim below) —
	// it just stops being what the ledger spends its first 200 runes on.
	// NOTE the `dir` is NOT dropped: it is word one of the tool's own words,
	// handed over verbatim below, and it is named again in the workspace
	// sentence.
	if blk.Kind == devworkspace.BlockerKindEmpty || blk.Kind == devworkspace.BlockerKindInheritedDelivery {
		// THE INHERITED KIND RIDES THIS BRANCH TOO, and it is a MEASUREMENT for
		// the same reason the empty kind is (row 01a19483): nothing refused —
		// `rev-list --count` answered 0 and the base already contains this
		// ground's HEAD. It shares the block because the two kinds share the
		// defect the block exists to fix: the composition's path prefix ate the
		// 200-rune ledger budget, so the one sentence that says WHY was cut every
		// time. What it does NOT share is the WORDING: an inherited ground has
		// nothing to say about "no commit here", so the opening and the
		// measurement clause are chosen per kind (Rule 15: the two facts never
		// share one sentence).
		opening, measurement := deliveryEmptyRetryOpening, deliveryEmptyMeasurement
		if blk.Kind == devworkspace.BlockerKindInheritedDelivery {
			opening, measurement = deliveryInheritedRetryOpening, deliveryInheritedMeasurement
		}
		return deliveryBriefingMarked(fmt.Sprintf(`%s

%s 工作区仍是你的，在 %s。

第 %d/%d 轮检查量到的结果是%s——不是 pull 被拒，也不是冲突，
不要去查网络。工具原话（逐字，未改写）：

%s

要不要在这个目录里补上提交、还是这活本来就该在别处，由你判断。交付尝试最多 %d 轮。
`,
			deliveryEmptyDecisiveBlock(dir, blk),
			opening, dir,
			round, devworkspace.MaxDeliveryRounds, measurement,
			verbatimWords(blk.Words), devworkspace.MaxDeliveryRounds))
	}
	// THE DISK KIND GETS ITS OWN SENTENCE, and it is the reason this kind
	// exists: the generic one names the build step and hands over the tool's
	// words, which is exactly the briefing that produced round after round of
	// "fix the compile error" on a full partition. The revision was never
	// linked, so there is nothing in it to fix; what the reader needs instead is
	// the filesystem reading and the reclaim command (Rule 15: a reader who is
	// not told which of the two facts they are looking at will pick the wrong
	// one).
	if blk.Kind == devworkspace.BlockerKindDiskExhaustion {
		return deliveryBriefingMarked(fmt.Sprintf(`%s

第 %d/%d 轮交付尝试被 %s 这一步挡下了 —— 但挡下它的是文件系统，不是代码。工作区仍是你的，在 %s。

%s

不要去找代码里的缺陷：这次的构建没有链接成功，所以这一轮对分支本身什么都没量到。
要不要腾出空间、还是把交付换到别处，由你判断。交付尝试最多 %d 轮。
`,
			deliveryDecisiveBlock(blk), round, devworkspace.MaxDeliveryRounds, blk.Kind, dir,
			blk.Report(), devworkspace.MaxDeliveryRounds))
	}
	return deliveryBriefingMarked(fmt.Sprintf(`%s

第 %d/%d 轮交付尝试被 %s 这一步挡下了。工作区仍是你的，在 %s。

工具原话（逐字，未改写）：

%s

该怎么处理由你判断。这是第 %d 轮之后的一轮；交付尝试最多 %d 轮。
`,
		deliveryDecisiveBlock(blk), round, devworkspace.MaxDeliveryRounds, blk.Kind, dir,
		verbatimWords(blk.Words), round, devworkspace.MaxDeliveryRounds))
}

// deliveryEmptyDecisiveBlock is the empty kind's decisive block: the measurement
// line, under the measurement label, with the composed path prefix REMOVED.
//
// WHY THE PATH GOES (row 01a18801). deliveryEmptyRefusalFmt opens with `%s
// carries no commit to deliver` and that `%s` is the workspace path — 56 runes
// on this host's `/home/user/workspace/<uuid>`, all of them the SAME prefix on
// every row and none of them a fact the reader has to act on (the briefing names
// the workspace in its own fourth sentence). Against a 200-rune ledger budget it
// is the most expensive thing in the sentence, so it is the first thing spent:
// with it, `origin/master carries N commit(s)` still falls outside; without it,
// the whole clause fits. What is removed is THIS CHANNEL'S composition, not the
// tool's words — `rev-list --count … answered 0 against origin/master` and every
// clause after it are handed over character for character, and the briefing's
// 「工具原话」 section still prints the path-carrying sentence VERBATIM.
//
// A blocker whose words do not begin with the recorded directory is left alone:
// this function recognises ONE composition (the one deliveryEmptyRefusalFmt
// makes) and never guesses at another, so an unexpected shape loses nothing
// (Rule 15 — the narrower reading is the safe one).
func deliveryEmptyDecisiveBlock(dir string, blk *devworkspace.Blocker) string {
	trimmed := *blk
	if rest, ok := strings.CutPrefix(blk.Words, dir+" "); ok {
		trimmed.Words = rest
	}
	return deliveryDecisiveBlockLabeled(deliveryMeasurementLabel, &trimmed)
}

// deliveryDecisiveLabelFmt introduces the decisive lines AND names the step that
// refused (%s = the BlockerKind). Both facts travel in this one short label on
// purpose: the ledger's summary budget is 200 runes and every rune spent here is
// a rune the deciding line does not get — while "which step" and "what it
// decided" are the two things the old summary split between them (it kept the
// step and lost the decision, row 01a1810a).
const deliveryDecisiveLabelFmt = "判定行（%s）："

// deliveryDecisiveNotFoundFmt is said OUT LOUD when the extractor recognised
// nothing (Rule 15): a briefing that silently omitted the decisive lines would
// leave a reader unable to tell "the tool said nothing decisive" from "this
// briefing has no such section at all" — and the second is a code defect.
const deliveryDecisiveNotFoundFmt = "%s没能抽出判定行（%s）。下面是完整的工具原话，请自己读。"

// deliveryDecisiveBlock renders the FIRST thing the next round's ledger summary
// will contain: the blocker's decisive lines, verbatim.
//
// WHY IT IS FIRST, AND WHY IT IS BOUNDED (row 01a18124, measured on row
// 01a1810a): the row summary is `[auto] ` + the briefing folded to ONE line and
// cut at 200 runes FROM THE START (internal/agent normalizeTaskSummary). The old
// briefing put a fixed ~180-character prefix (the workspace path and "第 N/M 轮
// 交付尝试被 pull 这一步挡下了。工具原话（逐字，未改写）：") in front of the tool's
// words, so the ONE line that distinguishes "a content conflict in
// internal/tools/task_holder_resume.go" from "the pull step refused" was always
// cut off, and the row told its reader only the second (false-by-omission) half.
//
// The cap arithmetic is the point, and it is checked by a test rather than
// asserted in prose: textguard/ledger budget (agent.AutoTaskSummaryBudget=200)
// minus the `[auto] ` marker minus this label minus the line's own cap
// (devworkspace's decisive limit) leaves the FIRST decisive line intact.
//
// It prescribes nothing: it names WHAT the tool decided, which is the agent's to
// act on (the briefing's red line).
func deliveryDecisiveBlock(blk *devworkspace.Blocker) string {
	return deliveryDecisiveBlockLabeled(fmt.Sprintf(deliveryDecisiveLabelFmt, blk.Kind), blk)
}

// deliveryMeasurementLabel is the label for a blocker that MEASURED something
// instead of refusing it (the empty kind). Naming the kind here would print
// "判定行（empty）：", i.e. it would dress a conclusion as a step that refused —
// the exact ghost the retry briefing's empty split exists to end. So the empty
// kind's closing round uses this label, and the pinned test can stay strict
// about the word "empty" never reaching a reader as if it were a step.
const deliveryMeasurementLabel = "判定行："

// deliveryDecisiveBlockLabeled renders the decisive lines under a caller-chosen
// label. ONE body, so the refused and the measured forms cannot drift apart.
func deliveryDecisiveBlockLabeled(label string, blk *devworkspace.Blocker) string {
	note := blk.DecisiveLines()
	if note.Found() {
		return label + strings.Join(note.Lines, decisiveLineBriefingSep)
	}
	// NO RECOGNISED LINE — say so, and name what was looked for. The conflicted
	// paths are the pull kind's other actionable fact, so they are used when the
	// vocabulary pass found nothing but git did name the files.
	if len(blk.Paths) > 0 {
		return fmt.Sprintf("%s没能抽出判定行（%s）；冲突路径是：%s", label, note.Why,
			strings.Join(blk.Paths, decisiveLineBriefingSep))
	}
	return fmt.Sprintf(deliveryDecisiveNotFoundFmt, label, note.Why)
}

// deliveryNothingToDeliverBriefing is the closing round for the
// nothing-to-deliver exit. It is a REPORT round like the exhausted one, and the
// one thing it must get right is the VERDICT: a run that was asked for no
// change and produced none did not fail, and a report written as if the
// delivery had failed is the second half of the defect (the ledger said
// "failed", the agent then wrote "交付失败" on top of it).
func deliveryNothingToDeliverBriefing(blk *devworkspace.Blocker, dir string, rounds int) string {
	return deliveryBriefingMarked(fmt.Sprintf(`没有内容可交付：%d 轮检查量到的都是「这里没有要交付的提交」，所以这次运行**不算失败**，
它本来就无需交付。

工作区在 %s（收尾时回收：里面没有任何超出 %s 的提交）。工具原话（逐字，未改写）：

%s

本轮只做一件事：写报告，如实记录这次运行做了什么、为什么无需交付 ——
不要写成交付失败，也不要为了「让交付看起来成功」去补一个空提交。
`,
		rounds, dir, devworkspace.DefaultBranch, verbatimWords(blk.Words)))
}

const (
	// deliveryEmptyRetryOpening is the empty retry briefing's own sentence ("there
	// is nothing here to publish"), named because a test asserts the measurement
	// sits BEFORE it in the ledger row — the pre-fix briefing opened with this
	// sentence and the measurement began at rune 150 of a 200-rune budget.
	deliveryEmptyRetryOpening = "交付没有内容可发布。"

	// deliveryEmptyMeasurement is the ONE spelling of what an empty blocker
	// MEASURED (Rule 14): `rev-list --count` answered 0. Both the retry and the
	// exhausted briefing say it, so an agent that sees the second one after the
	// first is not told two different facts.
	deliveryEmptyMeasurement = "「这里没有要交付的提交」"

	// deliveryInheritedRetryOpening / deliveryInheritedMeasurement are the
	// INHERITED kind's own two sentences (row 01a19483). They exist because the
	// empty kind's pair states a fact that is FALSE here: this ground is not
	// "nothing to publish because it holds no commit" — it holds the commit the
	// base already contains, and the base has moved past it. Sharing the empty
	// wording is the very "two facts, one sentence" defect Rule 15 forbids.
	deliveryInheritedRetryOpening = "交付的内容已经在集成分支上了。"
	deliveryInheritedMeasurement  = "「这里没有要交付的提交，因为上游已经包含了它」"

	// deliveryExhaustedKeptPrefix and the two step fragments are the SLOTS of the	// exhausted briefing, named because a caller reading the format string cannot
	// see which argument fills which slot — and the swap that produced
	// "工作区被保留，在 empty。最后挡下的是 <path> 这一步" was exactly that: the
	// arguments were (rounds, Kind, dir, words) against a template of
	// (rounds, dir, kind, words), and a `strings.Contains` test passed anyway
	// (measured, row 01a18242). The regression test asserts POSITION against
	// these three constants, so a swap can no longer be invisible.
	deliveryExhaustedKeptPrefix = "工作区被保留，在 "
	deliveryExhaustedStepPrefix = "最后挡下的是 "
	deliveryExhaustedStepSuffix = " 这一步"
)

// deliveryExhaustedBriefing is the ONE closing round, run after every delivery
// attempt was blocked: it asks for a REPORT, not for a delivery (the run is
// already failing, and pretending otherwise would spend the closing round on
// another attempt).
//
// THE KIND'S OWN SENTENCE (the exhausted half of deliveryRetryBriefing's split):
// an EMPTY blocker measured that this directory holds no commit. Nothing
// refused, so "最后挡下的是 empty 这一步" would send the reader looking for a step
// that does not exist — the same ghost the retry briefing's split exists to end,
// arriving one round later. That kind therefore gets a sentence that names the
// measurement and denies the step.
func deliveryExhaustedBriefing(blk *devworkspace.Blocker, dir string, rounds int) string {
	if blk.Kind == devworkspace.BlockerKindEmpty {
		return deliveryBriefingMarked(fmt.Sprintf(`%s

交付没有落地：%d 轮交付尝试全部被挡下，这一轮不再尝试交付。

%s%s。最后没有任何一步挡下交付：检查量到的结果是%s——不是 pull 被拒，也不是冲突，
不要去查网络，也不要把一个不存在的步骤写进报告。工具原话（逐字，未改写）：

%s

本轮只做一件事：写报告，记录发生了什么 —— 什么挡住了交付、你判断的原因、工作区里现在是什么状态。
`,
			deliveryDecisiveBlockLabeled(deliveryMeasurementLabel, blk), rounds,
			deliveryExhaustedKeptPrefix, dir,
			deliveryEmptyMeasurement, verbatimWords(blk.Words)))
	}
	return deliveryBriefingMarked(fmt.Sprintf(`%s

交付没有落地：%d 轮交付尝试全部被挡下，这一轮不再尝试交付。

%s%s。%s%s%s，工具原话（逐字，未改写）：

%s

本轮只做一件事：写报告，记录发生了什么 —— 什么挡住了交付、你判断的原因、工作区里现在是什么状态。
`,
		deliveryDecisiveBlock(blk), rounds, deliveryExhaustedKeptPrefix, dir,
		deliveryExhaustedStepPrefix, blk.Kind, deliveryExhaustedStepSuffix,
		verbatimWords(blk.Words)))
}

// decisiveLineBriefingSep joins two decisive lines in the briefing. It is a
// named constant because the summary fold COLLAPSES whitespace: a separator that
// is only spaces would glue the lines into one unreadable token in the ledger.
const decisiveLineBriefingSep = " ; "

// RepoRefusalDeadPathFmt is the ONE spelling of "the row names a path that
// cannot carry a build because it is not there" (Rule 1b: the log line and the
// refusal sentence are the same shape, rendered from the same constant, so they
// cannot tell an operator two different stories).
//
// It carries the MEASURED state (internal/devworkspace's RepoPathMissingReason /
// RepoPathNotGitReason) — never a guess — and it is deliberately DISTINCT from
// the "declares no build command" sentence, which stays true only for a path
// that really exists and is really declared nowhere.
const RepoRefusalDeadPathFmt = devworkspace.CloneChannelPrefix + "repo %q is not usable: %s"

// resolveCloneChannelBuild answers THE ONE QUESTION the channel asks before it
// may clone anything: which DECLARED build command does this row's repo run?
// (Rule 12 — the command is never discovered, and Rule 13 — one reader, so the
// refusal sentence and the fallback can never disagree.)
//
// WHAT IT MUST NOT DO, and does not:
//   - it never guesses a command;
//   - it never lets a run through when no declaration was found;
//   - it never repairs silently: the source trace prints its own line whether it
//     hits or misses;
//   - it caches nothing: every call re-reads settings and re-asks the checkout.
func resolveCloneChannelBuild(cfg devworkspace.Config, repo string) (config.RepoConfig, error) {
	refusal := func() error {
		return fmt.Errorf(
			devworkspace.CloneChannelPrefix+"repo %q declares no build command in settings %s → %s.build — "+
				"refusing to push a workspace that was never built (declare the command; it is never guessed)",
			repo, config.ReposSettingsKey, repo)
	}
	// WHICH QUESTION IS TRUE? (epic 01a15629.) The sentence above is the answer
	// to exactly ONE question — "this repository declares nothing" — and it was
	// being returned for a different fact: a row naming a directory that a
	// previous run's workspace reclamation DELETED (rows 01a154de, 01a1510e,
	// 01a1513b). A path with nothing on it declares nothing for the trivial
	// reason that it is gone; saying so sends the operator to the settings file
	// for a repair that lives on the row.
	//
	// The probe is a STAT, so a live repository costs no git process and no
	// extra read: `readable` is true for every normal run and this branch is
	// never taken.
	if readable, why := devworkspace.RepoPathReadable(repo); !readable {
		fmt.Fprintf(cfg.Log, RepoRefusalDeadPathFmt+"\n", repo, why)
		return config.RepoConfig{}, fmt.Errorf(
			RepoRefusalDeadPathFmt+" — refusing to push a workspace that cannot be built "+
				"(re-point the row at a live git work tree: %s)",
			repo, why, tools.TaskRepoRepairVerb)
	}
	rc, ok, err := config.ResolveRepoBuild(repo)
	if err != nil {
		return config.RepoConfig{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s: %w", config.ReposSettingsKey, err)
	}
	if ok {
		// DECLARED: the answer is complete. Nothing is traced, nothing is
		// printed, and no git process is spawned — a normal repo's run is
		// byte-identical to before the fallback existed.
		return rc, nil
	}
	// UNDECLARED. A repo inside the channel's own workspace root is a checkout
	// this channel made (a plain clone at <root>/<task-id>, a repository of its
	// own) — the one shape worth asking about provenance. Everything else is
	// REFUSED with the sentence it has always had.
	if repo == "" || !worktreeroots.Within(repo, cfg.Root) {
		return config.RepoConfig{}, refusal()
	}
	tracedRC, tr, hit := devworkspace.TraceToSource(context.Background(), cfg, repo)
	if !hit {
		fmt.Fprintf(cfg.Log, devworkspace.TraceOutcomeMissFmt+"\n", repo, traceErrText(tr.Err), config.ReposSettingsKey)
		return config.RepoConfig{}, refusal()
	}
	return tracedRC, nil
}

// traceErrText renders a trace failure for the miss line without a nil
// dereference (cmd/agent already has an errText for a different shape).
func traceErrText(err error) string {
	if err == nil {
		return "no origin named"
	}
	return err.Error()
}

// baseBranchOfCloneRun is the branch this run's push published — cfg.BaseBranch
// when the caller declared one (devworkspace publishes the integration branch),
// else the branch the run committed to.
func baseBranchOfCloneRun(cfg devworkspace.Config) string {
	if b := strings.TrimSpace(cfg.BaseBranch); b != "" {
		return b
	}
	return cfg.Branch
}

// landedRecordBranchOf is the branch a landed record NAMES, and it is the
// row's DECLARED branch on purpose: the proof's reader matches the record to
// `deliverable.branch` and to nothing else, so a record naming a different ref
// is evidence about a branch the row never claimed.
//
// THE NAME IS THE ADDRESS, NOT THE DESTINATION (measured 2026-10-27, `90f6a96`):
// a post-fix clone ground STANDS on `task/<id>`, and that is what the claim
// stamps as `deliverable.branch`. The push itself has ONE destination — the
// integration branch (`publishGround`) — so naming THAT here would make the
// third arm unreachable for exactly the rows it exists for: the reader would
// look for `task/<id>` and the record would say `master`. The record's BRANCH is
// therefore the address the row declares (`Result.Branch`), while its recorded
// TIP is the commit the push actually moved — which is why the address need not
// exist on the server for the record to be evidence about this row's delivery
// (measured: the address stays local, 9ab17237e). cfg.BaseBranch is only the
// fallback for a result that never got that far.
func landedRecordBranchOf(cfg devworkspace.Config, res devworkspace.Result) string {
	if b := strings.TrimSpace(res.Branch); b != "" {
		return b
	}
	return baseBranchOfCloneRun(cfg)
}
