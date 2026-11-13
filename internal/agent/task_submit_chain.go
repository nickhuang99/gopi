// task_submit_chain.go — the SERIAL-CHAIN face of the task_submit TOOL (the
// LLM path), split out of loop.go so the rule, the wording and the condition
// that decides whether anything is linked live in ONE readable place.
//
// OPERATOR RULING (2026-10-05, epic 01a10a24): "LLM 提交任务的顺序 = 逻辑依赖
// 顺序 … 不要考虑并行的损失，不用考虑！！！默认顺序就是依赖". A model cannot
// express a dependency GRAPH, so the order it submits in is the only dependency
// signal there is. A split therefore submits item 1 … item N, each hanging off
// the one before it, and the parent gate (Rule 33) releases them one at a time.
//
// THE ONE REPAIR THIS FILE MAKES (epic 01a12e34, measured 2026-10-12). The
// chain used to have no FIRST LINK of its own: with no previous submission to
// hang off, the first auto-linked submit fell back to the run's OWN row
// (autoTaskRowID) — the row the submitter is still in_progress ON. So the head
// of every session's chain was parked behind the submitter itself, and because
// item i hangs off item i-1, "the chain is parked" propagated to the WHOLE
// fan: three independent bug rows (01a12e30-e32e/f209/fc36) went out
// [queue-skip] — e32e on the triage run's own in_progress row 01a12e06, f209
// on e32e, fc36 on f209 — and NOT ONE was claimable by any dispatcher. The
// only repair was a hand-run `gopi task update <id> --clear-parent` per row.
//
// Two things were wrong, and they are fixed together:
//
//	① A SELF-LINK IS NOT A DEPENDENCY. A row parked behind its own submitter
//	   has no producer: it waits on a row that is by construction still running
//	   (that is what makes it the submitter), so the link is never "a dependency
//	   the model meant" — it is a fan that cannot move until THIS run ends, and
//	   a run that fans out bug rows is exactly the run that keeps going for a
//	   while. The first auto-linked submit now stays PARENTLESS and is reported
//	   as claimable immediately; the chain still forms from the SECOND submit
//	   onward (lastSubmitRowID), which is the ordering signal the ruling is
//	   about. Nothing links to the submitter's own row by default any more.
//	② "INDEPENDENT" HAD NO SPELLING. The chain models "this split runs in
//	   order"; it had no way to say "these items do NOT depend on one another"
//	   other than an explicit parent id — and a caller with nothing to point at
//	   had no word at all. parent_task_id now accepts an explicit INDEPENDENT
//	   sentinel (taskSubmitIndependentSentinel) that suppresses the chain for
//	   THIS submit: the row goes out parentless and the note says so. That is
//	   the only honest way to opt out — the alternative (`--clear-parent` after
//	   the fact) requires a write to every row, i.e. exactly the manual repair
//	   this epic exists to delete.
//
// THIS FILE ADDS NO MECHANISM: no new gate, no new column, no new command, no
// CLI flag. It rewrites ONE rule — "an omitted parent_task_id means the item
// submitted just before this one" — and renders its consequence back to the
// model, because a link the model cannot see is a link it will not rely on.
//
// RULE 33 IS UNTOUCHED: the release set, the predicate, the absence of any
// override/staleness release and the refusal of a dangling parent are the same
// code. What changed is WHICH row a submission points at, never when a row is
// released. Every model-facing sentence below names the release set through
// tools.TaskParentReleaseSetLabel() — the SAME reader the gate renders it with
// (Rule 14) — so the prose cannot drift from the predicate.
package agent

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// The chain note's wording (Rule 1: the strings that carry meaning are named).
const (
	// taskSubmitChainedNote explains a link the harness MADE on the model's
	// behalf — so the model knows the order it submitted in became real work
	// order and submits the next item the same way. The release set is
	// injected (tools.TaskParentReleaseSetLabel) rather than spelled: the
	// sentence a model reads and the predicate that parks the row must not be
	// two different answers.
	taskSubmitChainedNoteFormat = "(chained: this row's parent_task_id is %q — the submission you made just before it, so it is PARKED until that row reaches %s. Keep submitting the remaining items one at a time, in the order they must run: each one hangs off the previous.)"
	// taskSubmitUnchainedNote is the honest report of a FIRST auto-linked
	// submit: it carries no parent, so it is claimable the moment it lands.
	// Feeds the format below, which names the release set the chain uses from
	// then on.
	taskSubmitUnchainedNote = "(no chain link: this is the first submission of this run, so it went out with NO parent_task_id and is claimable IMMEDIATELY — there is no row to hang it off, and a row parked behind its own submitter would be parked behind work that is still running. Your NEXT submission, in this same session, will chain to this row.)"
	// taskSubmitIndependentNote reports a submit that ASKED to stay out of the
	// chain. It names the spelling that did it, so the model can rely on the
	// state instead of guessing whether the string it passed only affected
	// itself.
	taskSubmitIndependentNoteFormat = "(no chain link: parent_task_id %q is the INDEPENDENT sentinel — this row is NOT linked to the previous submission and is claimable IMMEDIATELY. Use it whenever the items do not depend on one another; every submit that follows is still chained unless it carries the same spelling.)"
	// taskSubmitNoLinkIDNote is the mirror-leg shape: the row WAS queued but
	// the submit handed back no id to chain onto (a forwarded row carries no
	// parent column), so the chain cannot be extended from here.
	taskSubmitNoLinkIDNote = "(chain link unavailable: the submit returned no task id to link the next submission onto — a forwarded row carries no parent column; submit the following item with an explicit parent_task_id.)"
	// taskSubmitResultSep separates the engine's confirmation from the note.
	taskSubmitResultSep = "\n"
)

// taskSubmitIndependentSentinel is THIS PACKAGE's name for the ONE spelling a
// caller may pass as parent_task_id to say "this item does NOT depend on my
// previous submission" (epic 01a12e34, deliverable 1). It is a SENTINEL, not a
// task id: no ledger row may carry it, so it is consumed here (chain decision)
// BEFORE the engine ever validates the parent — the engine, `gopi task update`,
// the sync legs and Rule 33's gate never see it.
//
// WHY A SENTINEL AND NOT A SECOND PARAMETER: Rule 3's tool surface is fixed,
// and the question the caller is answering ("what does this row wait for?") is
// exactly the question parent_task_id asks. The empty string is already taken
// (it means "the chain decides"), so the opt-out needs a value, and one that
// cannot be confused with an id is safer than a spelling the ledger could
// legitimately hold.
//
// WHERE THE VALUE LIVES NOW (moved 2026-11-07, task 01a1b448). The LITERAL is
// defined once in internal/toolnames.TaskSubmitIndependentSentinel, because
// this package is no longer its only consumer: `cmd/agent`'s CLI face
// (`gopi task submit --parent-task-id independent`) must give the same value
// the same meaning, and it cannot import an unexported constant. This line is
// an ALIAS, not a copy — the compiler follows a rename, so the two faces cannot
// drift, and no site in either package re-types the string (Rule 1/13/14).
const taskSubmitIndependentSentinel = toolnames.TaskSubmitIndependentSentinel

// The parent_task_id parameter's model-facing description. Rule 14: the SENTENCE
// that teaches the spelling lives here, beside the constant that decides it.
const (
	taskSubmitParentParamLeading     = "Optional: the task that must finish BEFORE this one runs — the row is parked by the parent gate until the parent reaches "
	taskSubmitParentParamTrailing    = " Omit it in a split and the harness links this row to the submission you made JUST BEFORE it, which is what makes submit ORDER the dependency order (submit item 1, then item 2, … — never all at once). The FIRST auto-linked submit of a run has no previous submission, so it goes out with NO parent and is claimable immediately (it is never parked behind the row you are running in). Pass "
	taskSubmitParentParamIndependent = " to say the items do NOT depend on one another: this row (and only this one) is left parentless and claimable, and the submits after it stay chained. Name an id explicitly only to link this row to something other than your previous submission (e.g. the task you are splitting, or a row you are NOT submitting here), and never repeat one parent across a split: identical parents make CLAIMABLE-AT-ONCE siblings, which is the dependency being thrown away."

	// taskSubmitIndependentToolClauseFormat is the same lesson at the TOP level
	// of the tool description — where the model decides HOW to submit a fan of
	// items, before it ever reads a parameter. The sentinel is injected (a
	// format, not a spelled literal) so the sentence and the constant that
	// decides the branch cannot drift (Rule 13).
	taskSubmitIndependentToolClauseFormat = "The FIRST auto-linked submit of a run is NEVER parked behind the row you are running in (it goes out parentless and claimable); the chain starts with your second submit. When the items do NOT depend on one another, say so: pass parent_task_id %q on each of them and they go out parentless and claimable immediately, instead of a chain that releases one at a time. "
)

// taskSubmitIndependentToolClause renders that clause with the sentinel
// resolved from its ONE definition site.
func taskSubmitIndependentToolClause() string {
	return fmt.Sprintf(taskSubmitIndependentToolClauseFormat, taskSubmitIndependentSentinel)
}

// taskSubmitParentParamDescription renders the parent_task_id description with
// the release set and the independent sentinel resolved from their ONE
// definition site (Rule 14 — no model-facing sentence re-types either value).
func taskSubmitParentParamDescription() string {
	return taskSubmitParentParamLeading + tools.TaskParentReleaseSetLabel() + "." +
		taskSubmitParentParamTrailing + fmt.Sprintf("%q", taskSubmitIndependentSentinel) +
		taskSubmitParentParamIndependent
}

// taskSubmitChainDecision is what the handler resolved BEFORE the engine was
// called: whether the harness made the link (chainLink), whether the caller
// asked to stay out of the chain (independent), and the parent id that was
// actually sent (parentID, "" = the row goes out parentless).
type taskSubmitChainDecision struct {
	chainLink   bool
	independent bool
	parentID    string
}

// taskSubmitResolveChain is THE rule — one function, so the condition that
// decides whether a row is parked has a single readable definition and a test
// can drive it without a ledger.
//
//	asked is the caller's parent_task_id (trimmed; the sentinel is matched
//	CASE-INSENSITIVELY because a model that answers "Independent" meant the
//	same thing — the sentinel is a word, not an id, and no ledger id may
//	carry it).
//	lastSubmitRowID is the row THIS RUN most recently submitted ("" = none).
//
// Every branch is reported, never invented: an explicit id is honoured
// VERBATIM (the parameter exists for what the chain cannot express), the
// sentinel yields a parentless row, and a first auto-linked submit yields a
// parentless row too — the repair this epic is about. Nothing here can reach
// the submitter's own row.
func taskSubmitResolveChain(asked, lastSubmitRowID string) taskSubmitChainDecision {
	asked = strings.TrimSpace(asked)
	if strings.EqualFold(asked, taskSubmitIndependentSentinel) {
		return taskSubmitChainDecision{independent: true}
	}
	if asked != "" {
		return taskSubmitChainDecision{parentID: asked}
	}
	if lastSubmitRowID != "" {
		return taskSubmitChainDecision{chainLink: true, parentID: lastSubmitRowID}
	}
	return taskSubmitChainDecision{chainLink: true}
}

// taskSubmitChainNote renders what the harness DID about the parent link for
// this submission, and is the model-facing half of the rule above.
//
// d is the resolved decision; rowID is the row that was really created (""
// = forwarded, nothing to hold on to).
func (a *Agent) taskSubmitChainNote(d taskSubmitChainDecision, rowID string) string {
	switch {
	case d.independent:
		return taskSubmitResultSep + toolnames.ToolTaskSubmit + ": " +
			fmt.Sprintf(taskSubmitIndependentNoteFormat, taskSubmitIndependentSentinel)
	case !d.chainLink:
		// The caller named its own parent: the note says nothing about a rule
		// that did not apply, and the row is linked exactly as asked.
		return ""
	}
	note := taskSubmitUnchainedNote
	if rowID == "" {
		note = taskSubmitNoLinkIDNote
	} else if d.parentID != "" {
		note = fmt.Sprintf(taskSubmitChainedNoteFormat, d.parentID, tools.TaskParentReleaseSetLabel())
	}
	return taskSubmitResultSep + toolnames.ToolTaskSubmit + ": " + note
}
