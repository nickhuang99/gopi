package tools

// task_nothing_to_deliver.go — what a close-out should CLAIM when the clone
// channel measures "nothing to deliver", read against what the run's own
// report says it did.
//
// THE MEASURED CASE (row 01a186b9-064c, 2026-10-29). One close-out wrote two
// facts that contradict each other:
//
//	status: completed
//	result: clone channel: nothing to deliver — /home/user/workspace/01a186b9-… carries
//	        no commit to deliver: `rev-list --count HEAD --not origin/master` answered 0 …
//	        nothing can be delivered or compared
//
// while the run's own report opened `**Verdict: BLOCKED**` and stated "本轮没有删除
// 任何代码、没有提交、没有移动 ref" — the task's goal (deleting the reopen verb)
// had not moved at all, and the row read as finished work. The evidence was in
// the report the whole time; NOTHING READ IT before the status was written.
//
// WHY `completed` WAS REACHABLE AT ALL, and why this is not a second criterion.
// The channel's empty-ground exit is a legitimate COMPLETION for a run that was
// ASKED to produce nothing (a counter-proof, a survey): row 01a18244 fixed the
// defect of recording those as `failed`, and nothing here may undo that. The
// distinguishing fact is the author's OWN verdict sentence — a counter-proof run
// says "I checked, there was nothing to change"; this run said "I did not do the
// work". So the rule reads the report and asks ONE question, through the ONE
// leaf that owns the vocabulary (taskreport.DeclaredIncomplete, Rule 14):
//
//	channel says nothing to deliver + report declares the run unfinished
//	    → the row must NOT read `completed`; it closes `failed` with a reason
//	      that quotes BOTH sentences verbatim (the author's admission and the
//	      channel's measurement), so an operator reading the row sees why.
//	channel says nothing to deliver + report declares nothing
//	    → today's behaviour, unchanged: `completed` on the channel's measurement.
//
// Rule 30 is the neighbouring mechanism, not a template: that arm REFUSES a
// claim over an unclean worktree (a write that must not happen). This one
// chooses WHICH claim is true once the measurement is in — the row still
// closes, and it closes with the status its own evidence supports.
//
// RULE 35: no new dependency. One read of the report path the report seam
// already resolves (TaskReportPath) and one `task_events` INSERT through the
// existing writer, consumed by `task show`.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/taskreport"
)

// NothingToDeliverDeclaredEvent is the attributed trail record for the case
// this file was written for: the channel measured an empty ground AND the run's
// own report said it did not finish. It is written BEFORE the terminal
// writeback (the write it explains), so a row that later moves on still carries
// why it was not `completed`.
const NothingToDeliverDeclaredEvent = "delivery-nothing-to-deliver-declared"

// NoDeliverableMarker is the GREPPABLE token an idle close-out carries, and the
// whole reason this second arm exists.
//
// THE MEASURED CASE (row 01a1b48a-8746, 2026-11-07). That run was dispatched a
// bug row, spent four rounds, made ONE memory call, wrote no report and no
// commit — and its row closed `completed` with the channel's sentence
// "carries no commit to deliver … nothing to push" as the reason. An operator
// scanning the ledger could not tell it apart from a run that WAS asked to
// produce nothing and correctly produced none (the counter-proof shape row
// 01a18244 protects), because both rows carry the same channel sentence and
// both read `completed`. Rule 5 forbids turning "no report" into a failure, so
// the difference has to be VISIBLE rather than FATAL: this token marks the row
// in its own result digest, which `task show` renders and `grep` finds.
const NoDeliverableMarker = "[no-deliverable]"

// NoDeliverableEvent is the trail record the token also lands on, so the fact
// survives a later rewrite of the row's result column (the append-only trail is
// the durable half — the same durability the report path and the Rule 31 finding
// already use).
const NoDeliverableEvent = "no-deliverable-closeout"

// noDeliverableSentence explains what the token means, in the row's own words:
// the run contributed nothing AND wrote nothing, so "completed" is the channel's
// measurement and not a claim about work.
const noDeliverableSentence = NoDeliverableMarker + " this run contributed nothing to deliver AND left no authored " +
	"report — the `completed` below is the clone channel's empty-ground measurement, not a statement that work was done; " +
	"read the session log before treating this row as finished"

// nothingToDeliverDeclaredPrefix heads the composed reason, so an operator
// scanning `task list` reads the disagreement without opening the report.
const nothingToDeliverDeclaredPrefix = "the run's own report declares it did not finish, so this close-out does not claim `completed` — the report says: "

// TaskReportDeclaresIncomplete reads the row's OWN report and answers the
// author's verdict: (sentence, true) when the report opens a `Verdict:` line
// declaring the run unfinished.
//
// Three-valued by construction, and the third answer is "not declared", never
// "finished": an absent report, an unreadable report, an empty report and a
// report with no `Verdict:` line all answer (\"\", false) — the honest reading
// is that the author made no admission, and manufacturing one would accuse
// every pre-existing report in this repository (the same lenient direction
// runcheck.go's statedLimitLine chose, and for the same measured reason).
func TaskReportDeclaresIncomplete(taskID string) (string, bool) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", false
	}
	path, err := TaskReportPath(taskID)
	if err != nil {
		taskNotifyLog("nothing-to-deliver: resolve the report path for %s: %v", taskID, err)
		return "", false
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil {
		// A run that wrote no report made no admission; the read is RECORDED and
		// the caller keeps its ordinary path (Rule 15, lenient direction).
		taskNotifyLog("nothing-to-deliver: no readable report for %s (%v)", taskID, readErr)
		return "", false
	}
	return taskreport.DeclaredIncomplete(string(body))
}

// TaskRunAuthoredReport answers "did the MODEL leave a report of its own?" —
// (authored, evidence) where the evidence sentence names the fact either way, so
// the close-out's reason never carries a bare boolean.
//
// THREE-VALUED, and the third answer is the point. An absent report and a
// harness-written skeleton are both "not authored", and they are deliberately the
// same answer here: neither contains a single sentence the model wrote about what
// it did, which is exactly the question. A report with ANY authored prose — even
// one line — answers authored=true, because the lenient direction is the only safe
// one for a marker an operator will read as "nothing happened" (Rule 5: the
// marker must never accuse a run that explained itself).
//
// THE "0 COMMITS" HALF IS ALREADY IN, and it is why this lives HERE rather than in
// a probe of its own. NothingToDeliverOutcome is reachable ONLY on the channel's
// empty-ground exit, whose own sentence is `rev-list --count HEAD --not
// origin/master` answering 0 — and the channel RECLAIMS the directory while
// composing it, so re-reading `.git/logs/HEAD` at this point would be a second
// measurement of a directory that no longer exists. This function therefore takes
// the commit count as GIVEN (the caller's precondition) and asks the ONE question
// the caller cannot answer: did the run write anything down.
//
// The read is RECORDED and never blocks: an unreadable report answers
// authored=true, i.e. it is given the benefit of the doubt rather than being
// marked idle on a filesystem error (Rule 15 / 15a).
func TaskRunAuthoredReport(taskID string) (bool, string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return true, "not judged: no task id was named"
	}
	path, err := TaskReportPath(taskID)
	if err != nil {
		taskNotifyLog("no-deliverable: resolve the report path for %s: %v — assuming authored", taskID, err)
		return true, "not judged: the report path could not be resolved"
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil {
		// No file at all is the ORDINARY idle shape (the run wrote nothing), and
		// it is not a read failure; any other error is recorded and treated
		// leniently.
		if errors.Is(readErr, fs.ErrNotExist) {
			return false, fmt.Sprintf("no report was written (expected at %s)", path)
		}
		taskNotifyLog("no-deliverable: read the report for %s: %v — assuming authored", taskID, readErr)
		return true, "not judged: the report could not be read"
	}
	if taskreport.IsSkeleton(string(body)) {
		return false, fmt.Sprintf("the report at %s is the harness skeleton — no sentence in it was written by the run", path)
	}
	return true, fmt.Sprintf("the report at %s carries authored prose", path)
}

// NothingToDeliverOutcome is the ONE decision the close-out takes when the clone
// channel measured an empty ground: which status the row closes with, and the
// sentence it carries. `channelReason` is the exit's own composed string,
// verbatim (Rule 14 — the caller must never re-word it).
//
// The returned summary is always non-empty: a row may not close with a
// fact-free reason (the same rule taskDefaultReason enforces elsewhere).
func NothingToDeliverOutcome(taskID, channelReason string) (status, summary string) {
	channelReason = strings.TrimSpace(channelReason)
	if channelReason == "" {
		channelReason = taskNothingToDeliverNoMeasurement
	}
	verdict, declared := TaskReportDeclaresIncomplete(taskID)
	if declared {
		summary = nothingToDeliverDeclaredPrefix + verdict + " | " + channelReason
		nothingToDeliverDeclaredRecord(taskID, summary)
		taskNotifyLog("nothing-to-deliver: %s is CLOSED `%s`, not `%s` — %s",
			taskID, TaskStatusFailed, TaskStatusCompleted, summary)
		return TaskStatusFailed, summary
	}
	// The author made no admission, so the channel's `completed` stands (Rule 5:
	// no report is not a failure). What the row must NOT do is wear that status
	// silently when the run also left nothing of its own to read — see
	// NoDeliverableMarker.
	if authored, evidence := TaskRunAuthoredReport(taskID); !authored {
		idleSummary := noDeliverableSentence + " (" + evidence + ")"
		summary = channelReason + " — " + idleSummary
		nothingToDeliverIdleRecord(taskID, idleSummary)
		taskNotifyLog("nothing-to-deliver: %s closes `%s` but carries %s — %s",
			taskID, TaskStatusCompleted, NoDeliverableMarker, evidence)
		return TaskStatusCompleted, summary
	}
	return TaskStatusCompleted, channelReason
}

// nothingToDeliverIdleRecord lands the idle fact on the row's append-only trail
// through the SAME writer, cap and error policy as the declared arm above.
func nothingToDeliverIdleRecord(taskID, detail string) {
	nothingToDeliverTrail(taskID, NoDeliverableEvent, detail)
}

// nothingToDeliverDeclaredRecord writes the attributed trail entry BEFORE the
// terminal write it explains, through the SAME writer and the SAME per-row cap
// the other close-out arms use (Rule 14: one spelling of the cap), so a
// re-driven close-out cannot drip identical records down the trail.
//
// A ledger that cannot be written is REPORTED and never changes the decision:
// the status and its reason still reach the row through the writeback below,
// and turning a loud decision into a crash because its own annotation failed
// would be the wrong failure (the shape taskDeliveryRefusedQueue chose).
func nothingToDeliverDeclaredRecord(taskID, detail string) {
	nothingToDeliverTrail(taskID, NothingToDeliverDeclaredEvent, detail)
}

// nothingToDeliverTrail is the ONE writer both arms above go through (Rule 14), so
// the cap, the timeout and the "a failed annotation never changes the decision"
// policy cannot drift between them.
func nothingToDeliverTrail(taskID, event, detail string) {
	if strings.TrimSpace(detail) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("%s for %s: %v", event, taskID, err)
		return
	}
	defer db.Close()
	if taskEventCountForDetailOn(ctx, db, taskID, event, detail) >= reclaimRunResidueRefusalLimit {
		return
	}
	taskEventWrite(ctx, db, taskID, event, detail)
}

// taskNothingToDeliverNoMeasurement is the reason a close-out carries when the
// channel answered "nothing to deliver" with no sentence at all — an empty
// measurement is still a measurement to record, and a blank reason is the one
// shape a terminal row must never wear.
const taskNothingToDeliverNoMeasurement = devworkspace.CloneChannelPrefix + "nothing to deliver (the exit returned no measurement)"
