package agent

// clonedelivery.go — THE BARE-PROMPT FACE'S DELIVERY LEG.
//
// THE GAP THIS CLOSES (deliberate, epic raw-prompt-clone-channel): cloneground.go
// moves a bare-prompt run into the clone channel's workspace, commits there, and
// KEEPS the workspace — because the channel's delivery loop
// (cmd/agent/devworkspace_channel.go) is shaped for a face that can spawn agent
// ROUNDS against a Blocker, and a bare-prompt run IS the agent. The result was a
// run whose commits existed only in /home/user/workspace/<task-id>. That is the
// state the epic left open, and this file is the leg that closes it.
//
// WHAT IT RUNS: devworkspace.AttemptDelivery, ONCE — the same function the
// channel's loop calls, not a second clone implementation (Rule 14 / the brief's
// "no second clone implementation"). One attempt, not the loop: this face has no
// rounds to hand a Blocker to, and a bare prompt that could not deliver says so
// to its operator (Rule 15) instead of spending more model calls.
//
// THREE OUTCOMES, BY SHAPE (AttemptDelivery's own contract, so nothing here
// parses text):
//
//	delivered — (res, nil, nil): the push landed. The row gains a trail note
//	            saying so, and stderr carries the branch and the attempt count.
//	blocked   — (res, blk, nil): a deterministic step refused (dirty / pull /
//	            build / push). blk.Report() — the tool's own words — reaches
//	            stderr, the row's reason column AND the task_events trail.
//	broken    — (res, nil, err): not about the work at all (no declared build, a
//	            git that cannot read the branch, a push that was not a race).
//	            Same three destinations, with the error text.
//
// WHERE IT HOOKS — AND WHY THE ORDER IS THE WHOLE POINT (fixed 2026-10-24).
//
// The leg runs from finishSessionTaskRecorded's BODY, immediately BEFORE the
// status write, with the DEFER kept only as the fallback for the hook's early
// returns (no registered row / ledger close failure). One attempt per run is
// guaranteed by cloneDeliveryDone (loop.go), not by the call sites.
//
// THE DEFECT THE ORDER CLOSES, measured on the shipped binary: with the leg in
// the defer, the row's own writeback ran FIRST and — because a success terminal
// is a claim the write path must PROVE (tools.deliveryProofRefusal) — it
// measured the tip against a destination the push had not yet moved and
// REFUSED it. Both sentences then sat on the same stderr:
//
//	[task-notify] delivery proof: REFUSED <row> — … the tip … is in NEITHER the
//	              ancestry NOR the patches of refs/remotes/origin/master …
//	clone channel: delivery delivered — pushed master to origin after 1 attempt(s)
//
// i.e. the delivery SUCCEEDED and the row stayed open forever, because the two
// duties were in the wrong order. Delivering first and claiming second is the
// only order in which each duty measures a true state: the push moves
// refs/remotes/origin/<branch> (measured: git updates the tracking ref on
// `push origin HEAD:<branch>`), so the proof that follows sees the tip AT its
// destination; a push that was BLOCKED leaves the tracking ref where it was, so
// the same proof refuses the claim and the row stays OPEN — the non-lying
// outcome — with no second verdict invented anywhere.
//
// The defer is NOT removed: a delivery duty that only covers the happy path is
// a duty that vanishes exactly when something went wrong (the reason
// worktreerelease is deferred too). The latch is what keeps the two call sites
// ONE attempt.
//
// BACKWARD COMPATIBILITY IS THE CONTRACT, one byte at a time: CloneRunGround()
// is ("", "") unless adoptCloneGround ran, which requires a non-empty dev_repo
// AND a bare prompt. An empty pair returns before reading config, before any git
// call, before any log line — the worktree channel is byte-identical to before.

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// cloneDeliveryTimeout bounds ONE delivery attempt for this face. The
	// clone channel's own loop is bounded by the wrapper process's life; a
	// bare prompt has nothing above it, so the attempt carries its own ceiling
	// or a wedged git could hold the operator's terminal open forever. It is
	// generous on purpose: the declared build of a real repository is the long
	// pole, and cutting a healthy build short would turn a working delivery
	// into a "broken" verdict.
	cloneDeliveryTimeout = 20 * time.Minute

	// THE CHANNEL'S PREFIX IS NOT DECLARED HERE (moved 2026-10-31, Rule 43).
	// It used to be a second literal in this const block; it is now the type-less
	// alias `cloneChannelLinePrefix = devworkspace.CloneChannelPrefix` declared
	// beside the combinator below, so the VALUE has one owner (Rule 1/13) and the
	// three faces that speak this channel's lines cannot drift apart.

	// The four ONE-spelling results this leg prints (Rule 14: the operator's
	// line and every test's assertion read the same text).
	//
	// THE PREFIX IS NOT IN ANY OF THEM (fixed 2026-10-30, the stutter defect):
	// each of these used to open with `clone channel: ` while the BODY it is
	// formatted with — blk.Report(), which opens every one of its Kind branches
	// with the same prefix — carried it too, so the operator's line and the row's
	// reason read `clone channel: delivery BLOCKED — clone channel: refusing to
	// pull: …`. The prefix is composed ONCE by cloneChannelSentence from the
	// channel's constant; these are the sentence that follows it, and their %s is
	// a BODY (never a prefixed line), so one composition site decides the count.
	cloneDeliveryDoneFmt    = "delivery delivered — %s (workspace %s)"
	cloneDeliveryBlockedFmt = "delivery BLOCKED — %s — the workspace %s is KEPT with its commits"
	cloneDeliveryBrokenFmt  = "delivery BROKEN — %v — the workspace %s is KEPT with its commits"
	// cloneDeliveryAlreadyDeliveredFmt is the FIFTH outcome's one sentence: the
	// same `count == 0`, the opposite fact — this run's own push already moved
	// the integration base onto its tip, so nothing is owed here (row 01a182df).
	// It is deliberately NOT the nothing-to-deliver sentence (that one says the
	// run had nothing to deliver; this one says it delivered) and it carries no
	// "did not land" prefix (the delivery DID land).
	cloneDeliveryAlreadyDeliveredFmt = "delivery ALREADY LANDED — %s (workspace %s holds nothing this run has not already pushed)"
	// cloneDeliveryTipUnreadableFmt is said OUT LOUD when the pushed tip cannot
	// be named: the delivery did land (the push was accepted), but its record
	// cannot point at a commit, and a later close-out will then have to prove
	// the delivery by content alone. Silent would be a missing proof with no
	// diagnosis (Rule 15).
	cloneDeliveryTipUnreadableFmt = "delivery delivered, but the pushed tip could not be read from %s — the landed record names no commit, so a rebase that re-derived this run's content will make its delivery proof unprovable"
	cloneDeliveryNoRowFmt         = "delivery outcome could not be written to a row (this run closed no ledger row): %s"

	// cloneNothingToDeliverToken is the ONE spelling of the conclusion, asserted
	// by the tests on BOTH sides: the operator's line and the row's reason carry
	// it, and neither carries the refusal's prefix.
	cloneNothingToDeliverToken = "nothing to deliver"

	// THE TWO MARKERS the operator stream is read back through (the tests pick
	// the BLOCKED / BROKEN line out of the buffer and count the prefix in it).
	// They are constants for the same reason the sentences are: an assertion
	// that retypes a fragment of a line drifts from the line (Rule 1/14).
	cloneDeliveryBlockedMarker = "delivery BLOCKED"
	cloneDeliveryBrokenMarker  = "delivery BROKEN"
	cloneDeliveryDoneMarker    = "delivery delivered"

	// cloneAlreadyDeliveredToken is the ONE spelling of the FIFTH outcome — the
	// same `count == 0` read as what it actually is (row 01a182df). Distinct
	// from cloneNothingToDeliverToken on purpose: the two are opposite facts and
	// the tests assert BOTH that each appears where it belongs and that neither
	// appears where the other does (Rule 15).
	cloneAlreadyDeliveredToken = "ALREADY LANDED"

	// cloneInheritedDeliveryToken is the ONE spelling of the SIXTH outcome — the
	// THIRD meaning of the same `count == 0` (epic: inherited delivery): the base
	// already CONTAINS this ground's HEAD, so there is nothing to publish and
	// nothing to pull. Distinct from BOTH siblings on purpose; the tests assert
	// each appears where it belongs and neither of the others appears with it.
	cloneInheritedDeliveryToken = "ALREADY ON THE INTEGRATION BRANCH"

	// THE SECOND EXIT, SPLIT BY KIND (bug 01a182a8, measured 2026-10-28).
	//
	// `BlockerKindEmpty` is not a failed delivery: it is the CONCLUSION that the
	// ground carries no commit past the integration branch. Its own words already
	// say "nothing to deliver" (devworkspace/delivery_anchor.go composes them),
	// and routing it through the BLOCKED exit above prefixed it with
	// tools.DeliveryDidNotLandReasonPref — so ONE fact wore two opposite
	// sentences ("nothing to deliver" AND "delivery did not land") and the end
	// hook's `undelivered` predicate wrote the run's own row back to in_progress
	// (internal/agent/taskregister.go, the LEFT OPEN arm).
	//
	// The fork is the SAME ONE the channel's first leg makes
	// (cmd/agent/devworkspace_channel.go's nothingToDeliver). The sentence is
	// composed HERE, once (Rule 14: one composition site), from the blocker's
	// own words — the classification travels as a FIELD, never as a re-parse of
	// this text.
	//
	// The workspace is KEPT, deliberately: reclaiming an empty ground is the
	// CHANNEL's call (cmd/agent's nothingToDeliver removes it), and a leg that
	// deletes a directory would be a second, unowned deleter.
	cloneDeliveryNothingToDeliverFmt = cloneNothingToDeliverToken +
		" — %s (the workspace %s is KEPT: this leg never deletes a ground)"

	// cloneDeliveryInheritedDeliveryFmt is the SIXTH outcome's one sentence (epic:
	// inherited delivery): the content this ground was handed is ALREADY on the
	// integration branch. It carries NO tools.DeliveryDidNotLandReasonPref — that
	// prefix ("delivery did not land") is false here: the content landed before
	// this run started, which is the whole distinction the kind exists to make.
	cloneDeliveryInheritedDeliveryFmt = cloneInheritedDeliveryToken +
		" — %s (workspace %s: nothing to push and nothing to pull, this is NOT a failure)"
)

// THE CHANNEL PREFIX AND ITS COMBINATOR ARE *devworkspace*'s, not this file's
// (moved 2026-10-31, Rule 43: a shared symbol is moved BEFORE the mechanism that
// housed it is narrowed). The prefix, the empty-body sentence and the idempotent
// trim→strip-once→add-once composition now live in
// internal/devworkspace/channel_prefix.go, because THREE faces speak this
// channel's lines — this end-hook leg, cmd/agent's delivery loop, and
// internal/tools' retry face — and one definition is the only way the count of
// `clone channel: ` in a line is a property of the composition instead of three
// files' memories.
//
// The three names below are ALIASES on purpose (not copies): this file's call
// sites and its tests keep reading `cloneChannelLinePrefix` /
// `cloneChannelBody` / `cloneChannelReason`, while the VALUE and the algorithm
// have exactly one owner. A second literal here is exactly the drift Rule 1/13
// forbids.
const cloneChannelLinePrefix = devworkspace.CloneChannelPrefix

// cloneChannelReasonNothing re-exports the shared empty-body sentence (Rule 15:
// the prefix over nothing is not a reason). Asserted identical to
// internal/tools' own empty-body sentence by this package's tests, so the three
// faces cannot drift into three different "no reason was reported" lines.
const cloneChannelReasonNothing = devworkspace.CloneChannelReasonNothing

// cloneChannelBody normalises a BODY for a channel line: trimmed, with the
// channel's prefix stripped AT MOST ONCE (see devworkspace.CloneChannelBody for
// the measured defect this prevents — `clone channel: delivery BLOCKED — clone
// channel: refusing to pull: …`, PROBE_PREFIX_COUNT=2).
//
// It delegates, so the algorithm has ONE implementation. Kept as a name here
// because every composition site in this file reads it.
func cloneChannelBody(body string) string { return devworkspace.CloneChannelBody(body) }

// cloneChannelReason composes ONE channel line: the prefix exactly once, then
// the body with any prefix of its own stripped — the shared door
// (devworkspace.CloneChannelReason), which is also the door cmd/agent's delivery
// loop now goes through (cmd/agent/devworkspace_channel.go). One definition, so
// a caller cannot introduce a second policy by composing elsewhere.
func cloneChannelReason(body string) string { return devworkspace.CloneChannelReason(body) }

// cloneDeliveryLine formats one of the delivery sentences (cloneDelivery*Fmt)
// and hands it to cloneChannelReason — the ONE door every operator-facing
// delivery line and every row reason in this file now goes through.
//
// Its %s/%v slot is a BODY, never a pre-prefixed line: the formatting helpers
// below strip before they insert, so a template that gains a prefixed body later
// still cannot produce a stutter.
func cloneDeliveryLine(format, body, workspace string) string {
	return cloneChannelReason(fmt.Sprintf(format, cloneChannelBody(body), workspace))
}

// cloneDeliveryNotLandedReason composes the row's NON-DELIVERY reason: the
// ledger's own sentence (tools.DeliveryDidNotLandReasonPref, whose marker text
// is taken FROM the constant, never retyped — Rule 13) over a body with any
// prefix of its own stripped. It exists because the marker itself opens with the
// channel's prefix: composing it with a prefixed body is exactly how the stutter
// was produced (`clone channel: delivery did not land — clone channel: …`).
func cloneDeliveryNotLandedReason(body string) string {
	// The marker keeps its own trailing space: it is the SEPARATOR between the
	// ledger's sentence and the body ("… did not land — " + "the build step …").
	// Trimming it produced `delivery did not land —the build step refused`, which
	// the `Contains(DeliveryDidNotLandReasonPref)` assertion in
	// TestRealDeliveryFailuresLandTerminalFailed caught (measured 2026-10-30).
	marker := strings.TrimPrefix(tools.DeliveryDidNotLandReasonPref, cloneChannelLinePrefix)
	return cloneChannelReason(marker + cloneChannelBody(body))
}

// cloneDeliveryAttempt is the ONE call this leg makes, as a FIELD on the Agent
// (loop.go) so a caller (or a test) can substitute it — the same injection shape
// the channel's own loop uses (cloneRunState.attempt). Production leaves it nil
// and reaches devworkspace.AttemptDelivery, so the production path is never a
// second implementation and a test cannot drift from it.
type cloneDeliveryAttempt func(ctx context.Context, cfg devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error)

// cloneDeliveryOutcome is what ONE delivery leg reports back to the end hook:
// whether it ran, whether the push landed, and — for the two non-landing
// shapes — the ONE sentence the row's reason column must carry.
//
// It EXISTS BECAUSE THE HOOK HAS TO ACT ON IT (fix 2026-10-24). Before the
// reorder the leg only spoke to the operator (stderr / trail / a reason edit
// made AFTER the status write), so a run whose push was blocked kept the
// `completed` the hook had already written and needed the delivery proof to
// catch it — a refusal that arrived too late to be useful and, in the delivered
// case, refused a delivery that had actually landed. The outcome is the value
// the hook decides the status with, so the status and the delivery cannot
// disagree.
type cloneDeliveryOutcome struct {
	// Attempted is true once the leg has really run for this run (it is false
	// for the compatibility arm — no clone ground — which is what keeps the
	// worktree channel byte-identical).
	Attempted bool
	// Delivered is true when the push landed (`delivered`).
	Delivered bool
	// NothingToDeliver is true when the attempt came back with the EMPTY blocker
	// for which there is nothing here to publish (`empty`) and this leg did NOT
	// fold it into a refusal — a conclusion, not a failed delivery.
	//
	// It is a FIELD of its own rather than "a Reason that happens to read
	// differently", because the end hook must be able to decide the status
	// without matching words (Rule 14: the classification travels as a value,
	// the reason travels as text, and neither is derived from the other — the
	// same contract cmd/agent's errNothingToDeliver sentinel carries on the
	// channel's first leg).
	NothingToDeliver bool
	// AlreadyDelivered is true when the attempt came back with the
	// ALREADY-DELIVERED blocker (row 01a182df): the same `count == 0`, and the
	// OPPOSITE fact — this run's own push already moved the integration base
	// onto its tip, so nothing is owed here and nothing failed.
	//
	// It is a SECOND field rather than a reuse of NothingToDeliver because the
	// two carry opposite facts (Rule 15): "the run produced nothing" and "the
	// run's work is already at the destination" send an operator to two
	// different places, and a reader of the struct must be able to tell them
	// apart without matching strings. BOTH are completions, and the end hook
	// treats them alike in exactly one respect — neither is an undelivered row.
	AlreadyDelivered bool
	// InheritedDelivery is true when the attempt came back with the
	// INHERITED-DELIVERY blocker (epic: inherited delivery): the THIRD meaning of
	// the same `count == 0` — HEAD is already an ancestor of the integration
	// branch, so the content this ground was handed is on the base.
	//
	// A THIRD field rather than a reuse, for the reason the second one is a
	// second: the three facts are different (produced nothing / pushed it itself
	// / was handed it already), and the end hook must decide the status without
	// matching strings. All three are completions, and none of them is an
	// undelivered row.
	InheritedDelivery bool
	// Reason is the row's WHY for a non-delivered outcome ("" when delivered):
	// the composed "nothing to deliver" sentence on the NothingToDeliver arm,
	// and tools.DeliveryDidNotLandReasonPref + the tool's own words on every
	// other non-delivered one.
	Reason string
}

// finishCloneDelivery runs the delivery leg for a run that took the CLONE
// ground, and reports its outcome in all three places an operator looks.
//
// rowStatus is the status the end hook decided for the row, or "" when the
// caller is about to write the row itself (the body call site): a reason write
// only happens when a status is supplied, which is the fallback/defer arm. The
// returned outcome is what the body call site uses to decide that status.
func (a *Agent) finishCloneDelivery(rowStatus string) cloneDeliveryOutcome {
	out := cloneDeliveryOutcome{}
	workspace, branch := a.CloneRunGround()
	if strings.TrimSpace(workspace) == "" {
		// THE COMPATIBILITY ARM: not a clone run (worktree channel, chat face,
		// a `task run` child). Nothing is read, nothing is called, nothing is
		// printed — the behaviour this face had before the leg existed.
		return out
	}
	if a.cloneDeliveryDone {
		// ONE attempt per run: the end hook calls this from its BODY (before the
		// status write — that order is the fix) and its DEFER (the fallback for
		// every early return). The latch is what makes those one attempt; the
		// verdict travels with it so the second caller decides with the SAME
		// knowledge the first one had.
		return a.cloneDeliveryVerdict
	}
	a.cloneDeliveryDone = true
	out.Attempted = true
	defer func() { a.cloneDeliveryVerdict = out }()
	taskID := a.autoTaskRowID
	cfg, cfgErr := a.cloneDeliveryConfig(taskID, workspace, branch)
	log := a.taskLogWriter()
	if cfgErr != nil {
		// BROKEN before a step could run: the declaration (dev_repo / workspace
		// root / the repos build table) is unreadable. Reported as the same
		// three-destination verdict a broken attempt gets — never a silent skip.
		out.Reason = cloneDeliveryNotLandedReason(fmt.Sprint(cfgErr))
		a.reportCloneDelivery(taskID, rowStatus, log, cloneDeliveryLine(cloneDeliveryBrokenFmt, fmt.Sprint(cfgErr), workspace), out.Reason)
		return out
	}
	attempt := a.cloneDeliveryAttempt
	if attempt == nil {
		attempt = devworkspace.AttemptDelivery
	}
	ctx, cancel := context.WithTimeout(context.Background(), cloneDeliveryTimeout)
	defer cancel()
	res, blk, err := attempt(ctx, cfg, workspace)
	switch {
	case blk != nil && blk.Kind == devworkspace.BlockerKindEmpty:
		// NOTHING TO DELIVER — THE CONCLUSION, NOT A FAILED DELIVERY
		// (bug 01a182a8, measured 2026-10-28). `BlockerKindEmpty` means the
		// ground carries no commit past the integration branch. Routing it
		// through the BLOCKED arm below prefixed that conclusion with
		// tools.DeliveryDidNotLandReasonPref, so ONE fact wore two opposite
		// sentences and the end hook's `undelivered` predicate wrote the run's
		// own row back to in_progress and printed LEFT OPEN
		// (internal/agent/taskregister.go — the arm this field disarms).
		//
		// The sentence is composed ONCE, here, and it is both the operator's
		// line and the row's reason (Rule 14) — no second composition, no
		// prefix, and deliberately NOT the non-delivery trail event: an empty
		// ground did not fail to deliver anything, so the row records a reason,
		// not a "not landed" event.
		out.NothingToDeliver = true
		out.Reason = cloneDeliveryLine(cloneDeliveryNothingToDeliverFmt, blk.Report(), workspace)
		a.reportCloneNothingToDeliver(taskID, rowStatus, log, out.Reason)
	case blk != nil && blk.Kind == devworkspace.BlockerKindAlreadyDelivered:
		// THE OTHER MEANING of the same `count == 0`（row 01a182df）：本 run 自己的
		// push 已经把 integration base 挪到自己的 tip 上，所以那个 0 是「交付成功」的
		// 算术，不是「这里没东西」。这一支不走 BLOCKED（那不是失败），也不走
		// NothingToDeliver 的句子（那是相反的事实：那句中会说「本来无需交付」，
		// 而这里东西已经交出去了）—— 它需要一个自己的句子（Rule 15）。
		//
		// Delivered 保持 false：它表示「这一轮 attempt 把 push 做出去了」，而这里
		// 没有任何东西被推。但本 run 的交付确实在 integration branch 上，所以 reason
		// 不携带 DeliveryDidNotLandReasonPref —— 否则「交付没落地」与「交付已落地」
		// 又是同一个字符串里两句话。
		out.AlreadyDelivered = true
		out.Reason = cloneDeliveryLine(cloneDeliveryAlreadyDeliveredFmt, blk.Report(), workspace)
		a.reportCloneNothingToDeliver(taskID, rowStatus, log, out.Reason)
		// THE LANDED RECORD, ON THIS ARM TOO (this row's defect, the same one
		// cmd/agent's loop had): "the delivery ALREADY LANDED" is a statement
		// ABOUT a push of this ground, so the row must carry the tip that push
		// produced. Without it `tools.TaskLandedDeliveryTip` answers "" on the
		// next face that judges this row, the empty gate loses its second
		// reading, and the delivery is re-measured as "carries no commit to
		// deliver" — the line this epic was filed on. Through the ONE writer and
		// the ONE grammar, so this face and cmd/agent's cannot drift.
		a.recordCloneLandedAlreadyDelivered(taskID, workspace, log)
	case blk != nil && blk.Kind == devworkspace.BlockerKindInheritedDelivery:
		// THE SIXTH OUTCOME (epic: inherited delivery): the THIRD meaning of the
		// same `count == 0` — HEAD is already an ancestor of the integration
		// branch, so the content this ground was handed is on the base and this
		// run pushed nothing. It is NOT a failed delivery (the content is there)
		// and NOT `nothing to deliver` (that sentence says the run produced no
		// change; here the change exists and is already published by someone).
		// Its OWN field, so the end hook decides the status without matching
		// words, and its own sentence, so the reader is never sent to pull what
		// is already there (Rule 15).
		out.InheritedDelivery = true
		out.Reason = cloneDeliveryLine(cloneDeliveryInheritedDeliveryFmt, blk.Report(), workspace)
		a.reportCloneNothingToDeliver(taskID, rowStatus, log, out.Reason)
	case blk != nil:
		// BLOCKED: a step refused. blk.Report() is the tool's own words. Every
		// other kind — dirty / pull / build / push — lands HERE, unchanged: the
		// fork above narrows the refused-delivery sentence to the cases that
		// really are refused deliveries, and widens nothing.
		out.Reason = cloneDeliveryNotLandedReason(blk.Report())
		a.reportCloneDelivery(taskID, rowStatus, log,
			cloneDeliveryLine(cloneDeliveryBlockedFmt, blk.Report(), workspace), out.Reason)
	case err != nil:
		// BROKEN: not about the work at all.
		out.Reason = cloneDeliveryNotLandedReason(err.Error())
		a.reportCloneDelivery(taskID, rowStatus, log,
			cloneDeliveryLine(cloneDeliveryBrokenFmt, err.Error(), workspace), out.Reason)
	default:
		// DELIVERED.
		out.Delivered = true
		detail := res.Detail
		if strings.TrimSpace(detail) == "" {
			detail = fmt.Sprintf("pushed %s to origin", res.Branch)
		}
		fmt.Fprintf(log, "%s\n", cloneDeliveryLine(cloneDeliveryDoneFmt, detail, workspace))
		// THE RECORD CARRIES THE TIP (2026-10-27). This face used to write only
		// the branch, and that is not enough for the delivery proof to judge a
		// COMMIT: a rebase that re-derived the pushed content leaves nothing a
		// patch comparison can match (row 01a17a67: cb84303... vs 4f1ab5b...),
		// so the channel's own statement that THIS tip landed is the only
		// surviving witness. Written through the ONE writer, in the ONE grammar
		// (tools.TaskDeliveryRecordLanded), so both faces and the reader cannot
		// drift.
		tip := tools.CloneDeliveryTip(workspace)
		if tip == "" {
			fmt.Fprintf(log, "%s\n", cloneChannelReason(fmt.Sprintf(cloneDeliveryTipUnreadableFmt, workspace)))
		}
		tools.TaskDeliveryRecordLanded(taskID, res.Branch, workspace, tip, detail)
	}
	return out
}

// reportCloneNothingToDeliver is reportCloneDelivery's EMPTY-ground sibling: the
// same two surfaces an operator reads (the stream and the row's reason column),
// with the one difference this fork exists for — the trail event.
//
// WHY THERE IS NO `clone-delivery-not-landed` EVENT HERE (bug 01a182a8): the
// event's name asserts a non-delivery, and an empty ground never attempted one —
// it answered a question. Recording the refusal event for a conclusion would make
// the trail lie in exactly the way the reason column used to, and the retry path
// scans that event name to decide what to re-dispatch. So the conclusion reaches
// the stream and the reason column, and the trail stays silent about a delivery
// that was never owed (Rule 15 is satisfied by the other two: neither is a
// swallow — a run that closed no row still says so out loud).
func (a *Agent) reportCloneNothingToDeliver(taskID, rowStatus string, log io.Writer, reason string) {
	if log != nil {
		fmt.Fprintf(log, "%s\n", reason)
	}
	if strings.TrimSpace(taskID) == "" {
		if log != nil {
			fmt.Fprintf(log, "%s\n", cloneChannelReason(fmt.Sprintf(cloneDeliveryNoRowFmt, cloneChannelBody(reason))))
		}
		return
	}
	if strings.TrimSpace(rowStatus) == "" {
		// The BODY call site: the end hook is about to write the row itself and
		// owns the reason column, so nothing is written here (writing it now
		// would be a second, competing verdict about the same row).
		return
	}
	if _, err := tools.TaskUpdateReason(taskID, rowStatus, reason, ""); err != nil {
		msg := cloneChannelReason(fmt.Sprintf("delivery outcome could not reach row %s: %v", taskID, err))
		if log != nil {
			fmt.Fprintf(log, "%s\n", msg)
		}
	}
}

// recordCloneLandedAlreadyDelivered writes the `clone-delivery-landed` record
// for the ALREADY-DELIVERED arm — the tip this ground's own push produced.
//
// It reads the tip from the ground while it still exists (this face does not
// delete it, but a sibling run's reclaim can, so the read is the same one-write
// order cmd/agent's loop uses). An unreadable tip is said OUT LOUD (Rule 15):
// the delivery DID land and the record cannot name it, which is a missing proof
// with a diagnosis rather than a missing proof without one.
func (a *Agent) recordCloneLandedAlreadyDelivered(taskID, workspace string, log io.Writer) {
	if strings.TrimSpace(taskID) == "" {
		// No row to file the record against — the caller's own not-a-row arm
		// already said so; there is nothing to append a trail event to.
		return
	}
	tip := tools.CloneDeliveryTip(workspace)
	if tip == "" {
		if log != nil {
			fmt.Fprintf(log, "%s\n", cloneChannelReason(fmt.Sprintf(cloneDeliveryTipUnreadableFmt, workspace)))
		}
		return
	}
	branch := tools.TaskGroundBranchName(taskID)
	tools.TaskDeliveryRecordLanded(taskID, branch, workspace, tip,
		"the delivery already landed (recorded by the already-delivered arm, which pushed nothing itself)")
}

// reportCloneDelivery writes ONE non-delivered outcome to all three
// destinations: stderr (already formatted by the caller), the row's reason
// column, and the append-only trail.
//
// The reason column is written only when the caller supplies BOTH a row and a
// status — the body call site (the primary one) supplies neither, because there
// the END HOOK itself is about to write the row and owns that sentence (a run
// whose delivery did not land is left OPEN with this reason — see
// `DELIVER FIRST, CLAIM SECOND` in taskregister.go). A run that closed no row at
// all SAYS so rather than swallowing the outcome (Rule 15). `status` is passed
// unchanged, so this is a reason edit, never a status move: a delivery leg that
// could flip a status would be a second verdict.
func (a *Agent) reportCloneDelivery(taskID, rowStatus string, log io.Writer, line, reason string) {
	if log != nil {
		fmt.Fprintf(log, "%s\n", line)
	}
	if strings.TrimSpace(taskID) == "" {
		msg := cloneChannelReason(fmt.Sprintf(cloneDeliveryNoRowFmt, cloneChannelBody(line)))
		if log != nil {
			fmt.Fprintf(log, "%s\n", msg)
		}
	}
	if strings.TrimSpace(taskID) != "" && strings.TrimSpace(rowStatus) != "" {
		if _, err := tools.TaskUpdateReason(taskID, rowStatus, reason, ""); err != nil {
			// The ledger is a channel too: a reason that did not land is a fact
			// the operator must hear, not a no-op (Rule 15).
			msg := cloneChannelReason(fmt.Sprintf("delivery outcome could not reach row %s: %v", taskID, err))
			if log != nil {
				fmt.Fprintf(log, "%s\n", msg)
			}
		}
	}
	tools.AppendTaskTrailEvent(taskID, tools.DeliveryDidNotLandEventName, reason)
}

// cloneDeliveryConfig resolves the delivery declaration for THIS run: the source
// repository the settings `dev_repo` names (the row's deliverable), the
// workspace root, the branch the run committed to, and — Rule 12 — the BUILD the
// `repos` table declares for that repository.
//
// The build is NOT discovered and NOT defaulted here: a repository that declares
// no command leaves cfg.Build empty on purpose, so AttemptDelivery refuses with
// its own sentence ("no build command is declared for this repo") and the
// outcome is a BROKEN verdict. That keeps ONE spelling of the refusal in the
// channel, and it keeps this leg from inventing a build.
func (a *Agent) cloneDeliveryConfig(taskID, workspace, branch string) (devworkspace.Config, error) {
	devRepo, _, err := config.DevRepoConfigured()
	if err != nil {
		return devworkspace.Config{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unreadable: %w", config.DevRepoSettingsKey, err)
	}
	root, rootSource, err := config.WorkspaceRootConfigured()
	if err != nil {
		return devworkspace.Config{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unusable: %w", config.WorkspaceRootSettingsKey, err)
	}
	cfg := devworkspace.Config{
		TaskID:     taskID,
		DevRepo:    devRepo,
		Root:       root,
		RootSource: rootSource,
		Branch:     branch,
		// THE PULL/REBASE POINT IS THE INTEGRATION BRANCH, NEVER THE ADDRESS.
		// This leg's workspace stands on the row's own ref `task/<id>` (the
		// clone channel put it there and `branch` — read back from the
		// workspace — is that name), while the ref it INTEGRATES is what landed
		// on the source's branch. Without this the leg would ask origin for a
		// ref the server only carries after a delivery it has not made yet:
		// `git pull --rebase origin task/<id>` fails outright with
		// `fatal: couldn't find remote ref task/<id>` (measured, git 2.43.0).
		BaseBranch: devworkspace.DefaultBranch,
		Log:        logWriterFunc(a.logAutoTaskf),
	}
	// THE ADDRESS IS NAMED ONLY WHEN THE WORKSPACE ALREADY STANDS ON IT — the
	// same measurement `internal/tools/task_delivery_retry.go` makes, so a
	// legacy ground (one opened before this wiring, standing on `master`) keeps
	// publishing `master` instead of silently gaining a `task/<id>` ref.
	if own := tools.TaskGroundBranchName(taskID); own != "" && own == strings.TrimSpace(branch) {
		cfg.GroundBranch = own
	}
	// THE ANCHOR (deliverable.worktree), read through the row's own accessor:
	// deliveryAnchorGate refuses to publish from a directory other than the one
	// this one, so a run that committed in ANOTHER row's ground is stopped by
	// name instead of publishing an empty tree (measured defect 01a17dee).
	// Empty = the row records no ground and the gate asks no question.
	if recorded, _ := tools.TaskDeliverableWorktreeFor(taskID); recorded != "" {
		cfg.Worktree = recorded
	}
	// WHICH PATH DOES THE DECLARATION HANG ON? A clone workspace is a checkout
	// OF THE SOURCE REPO, so the source is the first question — the same
	// precedence the channel's own loop uses, minus the trace: this run's
	// ground IS the workspace, so the source repo's LOCAL path is known here
	// without asking git. The row's deliverable is the second question, which
	// keeps a run whose `dev_repo` renders to no local path (an ssh spelling)
	// declared by the repo the row names.
	//
	// No hit is NOT an error: cfg.Build stays empty on purpose so
	// AttemptDelivery refuses with its own sentence (Rule 12 — the command is
	// declared, never discovered, never defaulted), and the outcome is BROKEN.
	var looked []string
	if display, derr := devworkspace.SourceRepoDisplayPath(devRepo); derr == nil && strings.TrimSpace(display) != "" {
		looked = append(looked, display)
	}
	if rowRepo := strings.TrimSpace(tools.TaskDeliverableRepoFor(taskID)); rowRepo != "" {
		looked = append(looked, rowRepo)
	}
	for _, repo := range looked {
		rc, ok, err := config.ResolveRepoBuild(repo)
		if err != nil {
			return devworkspace.Config{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s: %w", config.ReposSettingsKey, err)
		}
		if ok {
			cfg.Build = rc.Build
			cfg.BuildTimeout = time.Duration(rc.BuildTimeoutSec) * time.Second
			return cfg, nil
		}
	}
	return cfg, nil
}
