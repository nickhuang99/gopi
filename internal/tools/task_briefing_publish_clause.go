package tools

// task_briefing_publish_clause.go — the PUBLISHING half of the clone-channel
// briefing: "the channel delivers your work; you do not push it".
//
// ── WHY THIS EXISTS, AND WHY IT IS PROSE ─────────────────────────────────────
//
// The clone channel's contract is that the DELIVERY LOOP owns publishing: after
// the child's round the channel cleans, pulls --rebase, runs the declared build
// and pushes the branch the ground stands on (internal/devworkspace). Nothing in
// that loop names a branch — it pushes `HEAD:<the branch the ground stands on>`.
//
// Measured 2026-10-21 on the server (rows 01a15b70-a6aa…, 01a15b70-c35f…,
// 01a15b2c-…, 01a15b11-…): four refs existed that the channel never creates —
// `refs/heads/feat/reopen-carries-evidence` and three `refs/heads/task/<id>`,
// each with exactly ONE reflog entry, `push`, on the same second the run's own
// bash tool logged `git push origin HEAD:refs/heads/task/<id>`. The run's
// reasoning is in the same log: "Better: push to a named branch" — the agent
// applied PROJECT.md Rule 6c (written for a /tmp clone that has NO channel) to a
// ground whose delivery loop had ALREADY published its commits.
//
// THE MACHINE CHECK CAME FIRST (git_publish_target_guard.go, task 01a16033): a
// `git push` whose destination is not the ground's own branch is refused before
// it runs. This file is the OTHER half of the same defect, and it is the half a
// guard cannot be: the guard acts AFTER the model has decided to push, while a
// run that knew the route from its first round never forms the decision at all.
// An instruction the model cannot see is an instruction it cannot follow, so the
// statement belongs in the briefing — the child's FIRST user message — not in a
// comment beside the guard.
//
// ── WHY IT DOES NOT WEAKEN THE GUARD ─────────────────────────────────────────
//
// It says the same thing the guard enforces, in the same words the guard's
// refusal uses ("the channel's delivery loop"), so the two cannot drift into
// disagreement: a run that reads the clause and a run that trips the guard are
// told to do the same thing. The clause is prose; the refusal is enforcement.
//
// ── THE TIMING SENTENCE: THE ONE THING THIS CLAUSE GOT WRONG ─────────────────
//
// The first spelling told the reader the loop pushes after each round — every
// time. That was FALSE about the implementation, and it produced a measured
// false defect report. The leg is latched to ONE attempt per run
// (internal/agent/loop.go's
// cloneDeliveryDone) and it is invoked from the run's END hook — the body of the
// close-out, immediately before the row's status write, with the defer beside it
// as the fallback (internal/agent/taskregister.go). Nothing walks the ground at a
// round boundary. Measured on row 01a17921-fa90: its run passed 20 rounds,
// observed `origin/master` unchanged the whole time, concluded "the delivery leg
// is broken", and filed a bug row saying so — while the same run's own
// output.log ends with `clone-channel: attempt 1/3: push accepted`, and its one
// commit (036f357) IS an ancestor of origin/master (ea2007f). The premise of the
// bug row was the clause's wording, not the code: a run cannot see a leg that has
// not been given its turn yet, and "I have not seen it" was written down as "it
// did not run".
//
// So the clause states the trigger it really has (ONCE per run, in the close-out
// hook that writes the row), says plainly that a mid-run absence is NOT evidence,
// and names the one thing that IS: the channel's own line in the run's log. The
// BAN it carries is untouched — a wrong timing sentence was never a reason to
// loosen "the channel publishes, not you".
//
// ── SCOPE: THE CLONE CHANNEL ONLY ────────────────────────────────────────────
//
// A linked worktree ground is NOT this clause's subject: its commit is already
// on the branch in the shared repo (PROJECT.md Rule 6c's first half), and the
// worktree path's briefing is left byte-identical by this change. The caller
// renders the clause only when the clone channel is ON (cmd/agent/cli_tasks.go),
// which is also exactly the condition under which the guard can fire
// (`GOPI_RUN_GROUND` + `.git` is a DIRECTORY). Briefing and enforcement are one
// condition, so a run is never told about a rule that cannot apply to it.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// TaskBriefingPublishLabel is the clause's leading marker, spelled once so a
// reader — and a test in this package and in cmd/agent alike — can find the
// clause in a briefing without re-typing the body (the
// TaskGroundAnnouncementLabel / TaskGroundEvidenceLabel precedent, Rules 1b/13).
const TaskBriefingPublishLabel = "[Publishing your work — the delivery loop does it, not you]"

// taskBriefingPublishCloneFmt is the clause's ONLY spelling: the ground's
// branch, the route that publishes it and WHEN it runs, the pushes that are
// refused, and what to do instead.
//
// THE SUBSTITUTIONS ARE VALUES, NOT TYPED LITERALS (Rule 1b/13): the branch the
// clause is about, the channel's own attempt brake
// (devworkspace.MaxPushAttempts — the number the channel's log line prints), and
// the ledger's non-delivery reason prefix (DeliveryDidNotLandReasonPref — the
// words the row carries when the leg did not land). A briefing that re-typed
// either number or phrase could drift from what the machinery actually prints,
// and a reader who is told to look for the wrong line has been told nothing.
const taskBriefingPublishCloneFmt = TaskBriefingPublishLabel + "\n" +
	"This ground is a CLONE standing on branch %s. The clone channel's own DELIVERY LOOP publishes it — clean, pull --rebase, declared build, push of that branch. It runs ONCE PER RUN, from this run's END hook (the close-out, immediately before the row's status write), NOT at a round boundary: there is no per-round push, and no push to catch mid-run. Publishing is the CHANNEL's job, not the run's.\n" +
	"So a push you have not seen YET is NOT evidence that the loop is broken — it has not had its turn, and it gets exactly one. \"No push during my rounds, therefore the delivery leg did not run\" is a false conclusion with a measured cost: row 01a17921-fa90 passed twenty rounds, reported the leg as broken, and was wrong — its own log ends `clone-channel: attempt 1/%d: push accepted`, and its commit was an ancestor of origin/master. The leg RAN and the report was noise. Evidence that it RAN is the channel's own line in this run's log (`clone-channel: attempt N/%d: …` then `push accepted`, or `clone channel: delivery delivered`); evidence that it did NOT is this row left OPEN at close-out carrying the channel's non-delivery reason (`%s`). Quote one of those, never an inference from silence.\n" +
	"So do NOT hand-roll a publish: no `git push <remote> <refspec>` to any destination other than that branch — not `HEAD:<some-name>`, not `HEAD:refs/heads/<some-name>`, not a new `feat/<name>`, not the integration branch from this checkout. A destination the RUN chose CREATES a ref on the server that nothing merges and only an operator can delete, and naming the integration branch from another checkout MOVES the server's integration branch. The bash tool refuses such a push before it runs (PROJECT.md Rule 6c's clone-channel half; internal/tools/git_publish_target_guard.go), and `GOPI_ALLOW_DANGEROUS=1` is an OPERATOR decision, never a run's.\n" +
	"What you DO instead: commit your work on this ground's branch, leave the tree clean (`git status --porcelain` prints nothing), and report what you did. If the commit is still not on the server when the RUN closes, that is the delivery loop's verdict to report — say it in your output; a refused publish is a report line, never a reason to invent a ref."

// TaskBriefingPublishClause renders the publishing clause for a CLONE-CHANNEL
// ground standing on branch.
//
// An empty branch renders "" — the clause's whole substance is "the branch you
// stand on is what the loop pushes", so a clause that cannot name it would be
// true of nothing. The caller binds nothing in that case (Rule 15: no clause is
// better than one with a hole in it, and the announcement clause beside it
// already renders the branch as "none").
func TaskBriefingPublishClause(branch string) string {
	b := strings.TrimSpace(branch)
	if b == "" {
		return ""
	}
	return fmt.Sprintf(taskBriefingPublishCloneFmt, b,
		devworkspace.MaxPushAttempts, devworkspace.MaxPushAttempts, DeliveryDidNotLandReasonPref)
}
