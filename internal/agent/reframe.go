// Package agent — epic reframe + evaluate as ONE structured tool call.
//
// SPEC: briefings/SPEC-reframe-evaluate-merge.md (nick 2026-09-10, the
// 3-verdict zero-loophole revision).
//
// A task run starts with a raw [EPIC GOAL] briefing that may be huge (long
// prose, many constraints, attachments). Per-round injection of that raw text
// burns context on every round after the model has already read it. The old
// flow asked the model to emit a free-text [REF RAMED EPIC] BLOCK and kept
// evaluation in a SEPARATE tool (task_evaluate, run BEFORE any investigation
// on the summary alone). Two defects:
//
//  1. A free-text block is a VOID SLOT — the harness parses prose and the
//     model's judgment (doable? too big? impossible?) was never captured as a
//     decision the harness could act on.
//  2. "Too big" was a FREE answer. evaluation carried ZERO information, so
//     instance N+1 saw exactly the input instance N saw, decided the same
//     thing, and re-submitted — the bureaucracy loop (SPEC §1). Contrast the
//     splits that actually worked: they cost real work (read the code, judge
//     the boundary, write the payload).
//
// The merged flow (this file) has ONE moment and ONE shape: the model
// investigates enough to judge, then calls task(action=reframe) with
//
//	epic_summary / constraints / deliverables / acceptance — the reframe
//	verdict ∈ DOABLE | TOO_BIG | IMPOSSIBLE            — the judgment
//	roadmap / reason                                  — CONDITIONAL, enforced
//
// and the harness dispatches (handleTaskReframe): DOABLE continues; TOO_BIG
// auto-enqueues one child task per roadmap item and this run exits honorably
// (its value = due diligence + decomposition); IMPOSSIBLE marks this run's
// ledger row failed with the reason. Every exit therefore carries NEW work —
// the loop's root cause is fixed structurally (SPEC §4), not by asking nicely
// in a prompt. Validation is HARD (validateReframe): empty/unknown verdict,
// a TOO_BIG without a usable roadmap, or an IMPOSSIBLE without a reason is
// rejected outright.
//
// There is deliberately NO research verdict (SPEC §3): research IS reframing
// (read/code_search/memory_search are right there), so "I need research" is a
// job not done — a genuine unknown becomes a roadmap ITEM and the task still
// moves forward. IMPOSSIBLE stays narrow (provably unattainable in principle).
//
// After a valid call the harness swaps the raw [EPIC GOAL] message for the
// compact reframed text (applyReframe), so every later round — and every
// resume checkpoint, which snapshots the wire verbatim — carries the compact
// goal instead of the raw giant. The reframed epic stays IN MEMORY (it IS the
// [EPIC GOAL] message now); the raw text remains in the session JSONL as
// evidence.
package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/taskverdict"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// reframeVerdictList renders the verdict vocabulary once, from the constants
// (Rule 1/13/14 — the wire words live in internal/taskverdict).
var reframeVerdictList = strings.Join(taskverdict.All(), " | ")

// epicGoalHeader is the [EPIC GOAL] message prefix — the single source of
// truth for the marker every goal-recognition site scans for (Rule 1/13).
// THE VALUE IS NOT DEFINED HERE (Rule 14): it is
// sessionevent.HarnessEpicGoalFrameMarker, in the leaf package the session-log
// READERS import — so the frame this package WRITES and the frame a reader
// composes a briefing test from cannot drift apart. The trailing newline is
// this package's own formatting choice (the frame is a line, not a prefix).
const epicGoalHeader = sessionevent.HarnessEpicGoalFrameMarker + "\n"

// epicGoalFramingFormat is the ONE framing template a FIRST turn gets when the
// run IS a task (a dispatched row, the merger) — text, establishment deadline,
// reframe directive, in that order. A plain shell prompt gets no framing at
// all (Agent.RunPrompt), so this template is reached only by the task
// surfaces; declaring it here keeps the wrapper's bytes in one place instead
// of inline at the call site (Rule 1b/13).
var epicGoalFramingFormat = epicGoalHeader + "%s\n\nBased on this Epic, %s; re-emit on change.%s"

// reframeDirective is appended to the [EPIC GOAL] framing (round-1 user
// message), NOT the system prompt: it is task-framing context, applies to
// every task run, and must not grow the fixed system prompt past the density
// gate (system prompt = free text asking a question, toolcall = a formal
// action with a return value the harness can act on — SPEC §2; a decision
// request must be a toolcall).
//
// Timing: investigate just enough to judge (read the briefing + any
// [attachmentN:] files), then reframe — the verdict must be informed, but deep
// work BEFORE the swap keeps the raw giant in per-round context for nothing
// (the early-trigger evidence in
// devdoc/reframe-e2e-test-findings-20260908.md).
var reframeDirective = "\n\n[EPIC REFRAME] Before any other work: read the briefing (and any [attachmentN:] files), " +
	"then call task(action=" + toolnames.ToolTaskReframe + ") with (a) the epic restated compactly — epic_summary / constraints / " +
	"deliverables / acceptance — and (b) an honest verdict ∈ {" + reframeVerdictList + "}, investigated enough to judge, no more. " +
	taskverdict.TooBig + " requires a roadmap (one entry per child: goal + acceptance) — the harness enqueues it and this run ends there; " +
	taskverdict.Impossible + " requires a reason and means PROVABLY unattainable in principle (hard/risky/unknown is " + taskverdict.TooBig +
	", with the investigation as a roadmap item). THE BUDGET IS PART OF THE CRITERION: a roadmap whose items cannot each fit " +
	"inside ONE session budget means the epic is " + taskverdict.TooBig + ", never " + taskverdict.Doable + " — and past the " +
	"escalation mark a " + taskverdict.Doable + " verdict is refused outright while tracked items are still open. The harness acts on your verdict and REJECTS the call without the required payload. " +
	"That restatement REPLACES the raw briefing as the referenced goal for every later review (task(action=review), drift checks), " +
	"so work from it. An honest " + taskverdict.Impossible + " beats a fabricated result. " +
	"One-line Epic: skip the payload, still give the verdict."

// reframeDirectiveCharsBudget is the multiplier over the directive's own cost
// used to derive the swap threshold (Rule 1/2: the derivation lives here).
const reframeDirectiveCharsBudget = 4

// reframeMinRawLength is the harness-side swap threshold: only a briefing at
// least this long gets replaced by the reframed text. Below it the swap has no
// context-reduction value and only risks losing detail, so it is refused even
// when the model reframed. Derived from the directive's own cost (the
// directive is appended to the briefing, so the swap must save more than it
// costs to be worth doing).
var reframeMinRawLength = reframeDirectiveCharsBudget * len(reframeDirective)

// reframedBriefingNote is the tail of the swapped [EPIC GOAL] message: a
// one-line pointer that the raw briefing is archived in the session log
// (evidence). It keeps the message self-describing without re-injecting the
// raw text.
const reframedBriefingNote = "\n\n(Original raw briefing archived in the session log — the reframed epic above is the authoritative goal; the verdict and its payload were recorded by task(action=reframe).)"

// Reframe nudge backstop. The directive asks for the reframe call up front;
// the LLM-judged trigger was measured unreliable (devdoc d9d6367, E2E session
// ac786cca), so the harness reminds once when a big-briefing run has produced
// neither the toolcall nor the swap. Mirrors maybeNudgeNoList /
// pendingTaskAreaConclusionGuard — memory-aware, proven patterns.
const (
	// reframeNudgeAfterRound is the first round (1-based) a nudge may fire:
	// the first response gets its chance uncontested.
	reframeNudgeAfterRound = 2

	// reframeNudgeFreeChars is how much assistant TEXT output (outside
	// tool_use) a big-briefing run may accumulate before the nudge fires
	// regardless of round count. A reframe call is a TOOL CALL, so text
	// growth means the model is narrating instead of deciding. The gate keeps
	// the nudge from crowding out the very round that produces the call (a
	// roadmap payload can be long) — named constant, Rule 1.
	reframeNudgeFreeChars = 1500
)

// reframeNudgeMessage is the injected reminder. It names the toolcall (the
// formal action) and its conditional payload, from the same constants as the
// directive (Rule 14). Built once at init because the constants it reads are
// themselves built from package-level vars.
var reframeNudgeMessage = "\n\n" + harnessMessagePrefix + "This run's briefing is large and no task(action=" + toolnames.ToolTaskReframe + ") call has arrived yet. " +
	"Reframe NOW: read the briefing (+ any [attachmentN:] files), then call task(action=" + toolnames.ToolTaskReframe + ") with epic_summary/constraints/" +
	"deliverables/acceptance and a verdict ∈ {" + reframeVerdictList + "} (roadmap for " + taskverdict.TooBig + ", reason for " +
	taskverdict.Impossible + "). THE BUDGET IS PART OF THE CRITERION: a roadmap whose items cannot each fit inside ONE " +
	"session budget means " + taskverdict.TooBig + ", not " + taskverdict.Doable + " — " + sessionBudgetCeilingClause() + " per session. " +
	"The restatement replaces the raw briefing; work from it afterwards. " +
	"If the task is genuinely small, ignore this note."

// reframeRoadmapItem is one child task of a TOO_BIG decomposition.
type reframeRoadmapItem struct {
	Goal       string
	Acceptance string
}

// reframeInput is the parsed+validated payload of task(action=reframe).
type reframeInput struct {
	EpicSummary  string
	Constraints  string
	Deliverables string
	Acceptance   string
	Verdict      string
	Roadmap      []reframeRoadmapItem
	Reason       string
}

// payloadItems is the cardinality of the verdict's conditional payload — what
// the telemetry records as the exit's new-work quantity.
func (in reframeInput) payloadItems() int {
	if in.Verdict == taskverdict.TooBig {
		return len(in.Roadmap)
	}
	return 0
}

// parseReframeInput decodes the tool arguments. The schema-level args arrive
// as map[string]interface{} (JSON decoded), so the roadmap is a
// []interface{} of map[string]interface{}. Parsing is tolerant in SHAPE and
// strict in CONTENT: a mistyped container yields an empty payload which
// validateReframe then rejects (never a silently-accepted empty roadmap).
func parseReframeInput(args map[string]interface{}) reframeInput {
	in := reframeInput{
		EpicSummary:  strArgOf(args, "epic_summary"),
		Constraints:  strArgOf(args, "constraints"),
		Deliverables: strArgOf(args, "deliverables"),
		Acceptance:   strArgOf(args, "acceptance"),
		Reason:       strArgOf(args, "reason"),
		Verdict:      strings.ToUpper(strings.TrimSpace(strArgOf(args, "verdict"))),
	}
	arr, _ := args["roadmap"].([]interface{})
	for _, raw := range arr {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		goal, _ := item["goal"].(string)
		acc, _ := item["acceptance"].(string)
		in.Roadmap = append(in.Roadmap, reframeRoadmapItem{
			Goal:       strings.TrimSpace(goal),
			Acceptance: strings.TrimSpace(acc),
		})
	}
	return in
}

// validateReframe is the HARD harness gate (SPEC §4 — anti-bureaucracy by
// construction, not by prompt request):
//
//	verdict empty/unknown        → rejected (a decision cannot be skipped)
//	reframe fields empty         → rejected
//	TOO_BIG without a roadmap    → rejected ("too big" must cost a plan)
//	IMPOSSIBLE without a reason  → rejected (the honest exit must explain)
//
// Returns nil for a valid payload.
func validateReframe(in reframeInput) error {
	if in.Verdict == "" {
		return fmt.Errorf("task action=reframe: missing required parameter: verdict (want one of %s)", reframeVerdictList)
	}
	if !taskverdict.Valid(in.Verdict) {
		return fmt.Errorf("task action=reframe: invalid verdict %q (want one of %s)", in.Verdict, reframeVerdictList)
	}
	for _, f := range []struct{ key, val string }{
		{"epic_summary", in.EpicSummary},
		{"constraints", in.Constraints},
		{"deliverables", in.Deliverables},
		{"acceptance", in.Acceptance},
	} {
		if strings.TrimSpace(f.val) == "" {
			return fmt.Errorf("task action=reframe: missing required parameter: %s", f.key)
		}
	}
	switch in.Verdict {
	case taskverdict.TooBig:
		if len(in.Roadmap) == 0 {
			return fmt.Errorf("task action=reframe: verdict=%s requires a non-empty roadmap [{goal, acceptance}, …] — a decomposition is the price of saying \"too big\"", taskverdict.TooBig)
		}
		for i, item := range in.Roadmap {
			if item.Goal == "" || item.Acceptance == "" {
				return fmt.Errorf("task action=reframe: roadmap[%d] needs both goal and acceptance", i)
			}
		}
	case taskverdict.Impossible:
		if strings.TrimSpace(in.Reason) == "" {
			return fmt.Errorf("task action=reframe: verdict=%s requires reason (the provable blocker)", taskverdict.Impossible)
		}
	}
	return nil
}

// reframeTimeRefusal is the TIME DIMENSION of the reframe criterion (epic
// option B): a verdict is a judgment about the budget, so the budget is part of
// what it is checked against. Past the escalation mark, a DOABLE verdict is
// refused while the run still has an open tracked item — "the epic is
// executable here" cannot be true at 75% of the planning budget with the work
// unfinished, and accepting it is exactly how an over-budget epic gets waved
// through.
//
// Absent dimension, absent refusal (every one of these is "no claim"):
//   - budget 0 (unlimited) or no clock — there is no mark to be past;
//   - below the mark — DOABLE is the honest answer an unimpeded run gives;
//   - a closed tracked list / no list at all — a genuinely single-step epic is
//     answerable DOABLE at any minute of the budget (this is the "already
//     finished" exemption: the verdict costs nothing because the work is done);
//   - any verdict other than DOABLE — the other two exits already carry work
//     (a roadmap, a reason) and are never refused.
//
// A refusal latches NOTHING: the run has not reframed, so it may re-issue the
// call as TOO_BIG (or finish the work and answer DOABLE honestly).
func (a *Agent) reframeTimeRefusal(in reframeInput) string {
	if in.Verdict != taskverdict.Doable {
		return ""
	}
	st, ok := a.sessionBudgetStatus()
	if !ok || !st.PastMark {
		return ""
	}
	if !a.hasOpenTrackedItems() {
		return ""
	}
	return fmt.Sprintf(reframeTimeRefusalFmt, in.Verdict, sessionBudgetWarnPercent, st.Elapsed, st.Remaining,
		st.Budget, st.Percent, taskverdict.TooBig, taskverdict.Impossible)
}

// reframeTimeRefusalFmt renders the time-dimension refusal. It names BOTH other
// verdicts, because at that point either one may be the honest answer: the epic
// is either too big for this session (TOO_BIG + roadmap) or provably
// unattainable (IMPOSSIBLE + reason). %s = the refused verdict; the numbers come
// from the one budget measurement; the last two %s are the verdict vocabulary
// (internal/taskverdict).
const reframeTimeRefusalFmt = "task action=reframe: verdict=%s REFUSED — past %d%% of the session budget " +
	"(%v elapsed, %v remaining of %v, %d%% used) with tracked items still open: an epic still unexecuted at the " +
	"escalation mark is not executable here — it is %s with a roadmap (each item completable inside ONE session), " +
	"or %s with a reason. Nothing was recorded and nothing is blocked: re-issue this same call with the honest verdict."

// reframedEpicBody renders the compact [EPIC GOAL] body from the reframe
// payload — the text that replaces the raw briefing for every later round.
func reframedEpicBody(in reframeInput) string {
	var b strings.Builder
	b.WriteString(in.EpicSummary)
	for _, f := range []struct{ label, val string }{
		{"Constraints", in.Constraints},
		{"Deliverables", in.Deliverables},
		{"Acceptance", in.Acceptance},
	} {
		if f.val == "" {
			continue
		}
		b.WriteString("\n\n" + f.label + ": " + f.val)
	}
	b.WriteString("\n\nVerdict: " + in.Verdict)
	return b.String()
}

// reframedEpicMessage rebuilds the [EPIC GOAL] message around the reframed
// body: the header stays (mechanisms that recognize the goal by the
// "[EPIC GOAL]" prefix — wfcontract checks, trimCommittedTaskArea's keep-0 —
// keep working) and the note points at the session log.
func reframedEpicMessage(body string) string {
	return epicGoalHeader + body + reframedBriefingNote
}

// applyReframe swaps the raw [EPIC GOAL] briefing for the reframed body.
// Called from handleTaskReframe. Guards: no epic message → no-op; briefing
// below reframeMinRawLength → no-op (small task: the raw text already IS the
// reframed form, so there is no context-reduction value and only loss risk);
// already applied → no-op (latch). The scan is by prefix (index 0 is NOT
// assumed — compaction can prepend a memory block at the head) and the
// message is never dropped if absent.
//
// The swap mutates only in-memory Ctx.Messages BEFORE the next request is
// built, so the very next wire request (and any resume checkpoint of it)
// carries the compact goal — the raw giant exits per-round injection here.
func (a *Agent) applyReframe(in reframeInput) {
	if a == nil || a.Ctx == nil || a.reframeApplied {
		return
	}
	for i := range a.Ctx.Messages {
		m := &a.Ctx.Messages[i]
		if m.Role != "user" || !strings.HasPrefix(m.Content, "[EPIC GOAL]") {
			continue
		}
		if len(m.Content) < reframeMinRawLength {
			return // small briefing: refuse the swap (no positive-value reduction)
		}
		rawLen := len(m.Content)
		// The HARNESS clauses are facts about the RUN, not prose about the epic
		// (see reframe_ground_clauses.go): a summary of the epic cannot
		// reconstruct which tree this run writes. They ride along verbatim.
		kept := reframePreservedBriefingClauses(m.Content)
		body := reframedEpicBody(in)
		if kept != "" {
			body += "\n\n" + kept
		}
		m.Content = reframedEpicMessage(body)
		a.reframeApplied = true
		if a.Logger != nil {
			a.Logger.Printf("🔄 reframe applied (verdict=%s): [EPIC GOAL] %d→%d chars — raw briefing out of per-round context (session log retains the original; %d char(s) of harness clauses kept verbatim)",
				in.Verdict, rawLen, len(m.Content), len(kept))
		}
		return
	}
}

// reframeBigBriefingInContext reports whether the session's [EPIC GOAL]
// briefing is still raw AND long enough that a reframe swap would have
// positive context-reduction value (>= reframeMinRawLength). The scan is by
// prefix, the same rule applyReframe uses. Returns false once reframed (the
// message is compact) or when no epic message exists.
func (a *Agent) reframeBigBriefingInContext() bool {
	if a == nil || a.Ctx == nil {
		return false
	}
	for i := range a.Ctx.Messages {
		m := &a.Ctx.Messages[i]
		if m.Role != "user" || !strings.HasPrefix(m.Content, "[EPIC GOAL]") {
			continue
		}
		return len(m.Content) >= reframeMinRawLength
	}
	return false
}

// maybeNudgeReframe injects a one-shot reminder when a big-briefing run has
// neither called task(action=reframe) nor had a swap applied. Called at the
// round top BEFORE the request is built (so the reminder reaches the model
// this round). Guards:
//   - reframeVerdict set or reframeApplied → never nudges;
//   - briefing not raw-big → no-op (small task, or already reframed);
//   - budget gate: fires only once freeTextChars >= reframeNudgeFreeChars or
//     roundIdx >= reframeNudgeAfterRound — a reframe call may be long, so the
//     nudge must not crowd out the round that produces it;
//   - reframeNudged latch (survives resume via a context scan) → once per run.
//
// Returns true when a nudge was injected.
func (a *Agent) maybeNudgeReframe(roundIdx, freeTextChars int) bool {
	if a == nil || a.Ctx == nil || a.reframeApplied || a.reframeVerdict != "" {
		return false
	}
	if !a.reframeBigBriefingInContext() {
		return false
	}
	if freeTextChars < reframeNudgeFreeChars && roundIdx < reframeNudgeAfterRound {
		return false
	}
	if a.reframeNudged {
		return false
	}
	// Resume guard: a previously injected nudge persists in Ctx.Messages as a
	// user message (the wire snapshot restores it). The latch is run-local and
	// resets on resume, so an identical re-injection is prevented by scanning
	// for the message rather than relying on the latch alone.
	for i := range a.Ctx.Messages {
		if a.Ctx.Messages[i].Role == "user" && strings.Contains(a.Ctx.Messages[i].Content, reframeNudgeMessage) {
			a.reframeNudged = true // latch: never nudge again
			return false
		}
	}
	a.Ctx.AddUserMessage(reframeNudgeMessage)
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), reframeNudgeMessage)
	}
	a.reframeNudged = true
	if a.Logger != nil {
		a.Logger.Printf("🔔 reframe nudge: big briefing, no %s call yet (round %d, %d chars of free text)",
			toolnames.ToolTaskReframe, roundIdx, freeTextChars)
	}
	return true
}

// reframeChildPayload renders one roadmap item into the child task's briefing
// (the child runs in a FRESH gopi, so the payload must be self-contained —
// SPEC §3: each item stands alone).
func reframeChildPayload(parentSummary string, item reframeRoadmapItem) string {
	var b strings.Builder
	b.WriteString("Goal:\n" + item.Goal + "\n\nAcceptance:\n" + item.Acceptance)
	if parentSummary = strings.TrimSpace(parentSummary); parentSummary != "" {
		b.WriteString("\n\nSplit from the epic: " + parentSummary)
	}
	return b.String()
}

// reframeSubmitOptions returns the TaskSubmit options for ONE auto-enqueued
// child of a reframe verdict: parent-linked, llm-sourced, tagged so the ledger
// shows the decomposition origin, and carrying the DURABLE repo of the run that
// decomposed the epic.
//
// WHY repo IS A PARAMETER AND NOT A DEFAULT (epic 01a0d55b, measured live
// 2026-09-25): with no repo named, TaskSubmit fell back to the submitting
// process cwd — and a worker-run parent's cwd is its GROUND, the linked worktree
// the worker creates for that one run and DELETES on finish. The four children
// of row 01a0d526 were born with
// {"repo":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"} — their
// own parent's worktree — and `gopi worker <child>` then refused, correctly,
// with "the repo is not a git work tree ... refusing to start the run in the
// repo's main checkout". A child that must outlive its parent cannot inherit
// the parent's ephemeral ground.
//
// The caller supplies tools.DurableRepoForSubmit's answer (the repo's MAIN
// checkout, resolvable from inside the ground through the shared
// `--git-common-dir`); an empty repo keeps the pre-existing behaviour
// downstream (TaskSubmit's own default) and is REPORTED, never silently
// accepted — see dispatchReframeRoadmap.
func reframeSubmitOptions(summary, payload, parentID, tag, sessionID, model, repo string) tools.TaskSubmitOptions {
	return tools.TaskSubmitOptions{
		Summary:      summary,
		Payload:      payload,
		Tags:         tag,
		ParentTaskID: parentID,
		Source:       taskSourceLLM,
		SessionID:    sessionID,
		Model:        model,
		Repo:         repo,
	}
}

// reframeChildRepo answers the repository a roadmap child row must be born
// naming. ONE reader, so the resolution and its log line cannot disagree
// (Rule 13/14).
//
// THE RULE: a child inherits its PARENT ROW's deliverable.repo and nothing
// else. The parent's WORKTREE never inherits — not its declared
// deliverable.worktree, and not the directory this run happens to stand in.
//
// WHY THE PARENT ROW IS THE ONLY SOURCE (bug 01a1515c, measured live
// 2026-10-18; three of three reframe splits died on it):
//
//	parent 01a150cb deliverable.repo     = /home/user/work/gopi
//	                 deliverable.worktree = /home/user/workspace/01a150cb-…
//	children 01a150cb/01a1510e/01a1513b  repo = /home/user/workspace/01a150cb-…
//	                                             ^ the PARENT'S WORKSPACE
//
// so each child was refused at spawn by the clone channel — correctly — with
// "repo … declares no build command in settings repos", because a per-run
// workspace is a repository of its own and is declared nowhere.
//
// The previous resolution (epic 01a0d55b) asked DurableRepoForSubmit(cwd). That
// is right for a LINKED WORKTREE — git's common-dir names the main checkout
// from anywhere inside the repository — and silently wrong for the clone
// channel, whose cwd IS a standalone clone at /home/user/workspace/<parent-id>:
// the common dir of a clone is the clone, so the durable answer was the
// parent's own workspace. Reading the row removes the question entirely: the
// parent row already recorded the repository it was dispatched for, and no
// git plumbing is consulted, so no channel can reinterpret it.
//
// DEGRADES, both reported and never silent (Rule 15):
//   - a row carrying an EMPTY repo hands "" to the submit, which then applies
//     its own default (the cwd). That is the pre-existing, documented lenient
//     face — reported here so the reader knows which repo the children got;
//   - a row that names a WORKTREE as its own repo is honoured verbatim (the
//     operator's ledger may legitimately point at a worktree) but the fact is
//     printed, because the clone channel will refuse it downstream and this
//     line is what makes that refusal diagnosable.
func (a *Agent) reframeChildRepo(rowID string) string {
	repo := strings.TrimSpace(a.reframeSubmitRepo)
	if repo == "" {
		repo = strings.TrimSpace(tools.TaskDeliverableRepoFor(rowID))
	}
	if repo == "" {
		a.logReframeChildRepo("⚠ reframe TOO_BIG: the parent row %q names no deliverable.repo — children fall back to the submit default (%s)",
			rowID, reframeSubmitCwd())
		return ""
	}
	// THE DEAD-WORKSPACE REPAIR (epic 01a15629). In the clone channel the row's
	// workspace IS its repo (the clone at <root>/<task-id>), and that workspace
	// is RECLAIMED when its push lands — so a child born naming it is born
	// naming a directory that is about to disappear. When it is already gone,
	// the ledger row that recorded the path still names the repository: repair
	// to it and print the fact. A repo outside the workspace root is returned
	// unchanged with no stat and no git process, so a normal decomposition is
	// untouched; a workspace still on disk is resolved by git, which is the
	// check that decides whether "live" really means usable.
	//
	// WHAT THE REPAIR LEAVES BEHIND: only the children's value moves. The row
	// keeps naming its own workspace (that is what it really was), so the
	// lifetime's own machinery — reclamation, the workspace sweep, `task show` —
	// still sees the directory for what it is. Editing the row here would rewrite
	// a finished run's record from inside a decomposition.
	if repaired, note := tools.RepairDurableRepoForSubmit(repo); note != "" {
		a.logReframeChildRepo("%s", note)
		repo = repaired
	}
	if worktree, _ := tools.TaskDeliverableWorktreeFor(rowID); worktree != "" && tools.SameDir(repo, worktree) {
		a.logReframeChildRepo("⚠ reframe TOO_BIG: the parent row %q names its own WORKSPACE %s as its repo — inherited verbatim (the clone channel will refuse it: a workspace declares no build)",
			rowID, repo)
	}
	return repo
}

// logReframeChildRepo writes one reframe repo-resolution line — when there is a
// logger. A run without one still carries the fact in the rows it writes.
func (a *Agent) logReframeChildRepo(format string, args ...any) {
	if a == nil || a.Logger == nil {
		return
	}
	a.Logger.Printf(format, args...)
}

// reframeSubmitCwd answers the directory this process stands in — kept ONLY so
// the "children fall back to the submit default" line can name it. It is no
// longer an input to the children's repo (see reframeChildRepo: the cwd of a
// clone-channel parent IS the parent's workspace, which is exactly the defect).
func reframeSubmitCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// handleTaskReframe implements task(action=reframe): validate → record
// telemetry → swap the briefing → dispatch on the verdict.
//
// Dispatch (SPEC §3):
//
//	DOABLE     → tell the model to proceed (normal path)
//	TOO_BIG    → submit the roadmap as child tasks, one per item; this run
//	             exits honorably (its value = diligence + decomposition)
//	IMPOSSIBLE → mark THIS run's ledger row failed with the reason
//
// A second reframe in the same run is recorded (telemetry shows the repeat —
// the loop evidence) but never re-dispatched, and the FIRST verdict stands:
// duplicate children would be a real defect.
func (a *Agent) handleTaskReframe(args map[string]interface{}) string {
	in := parseReframeInput(args)
	if err := validateReframe(in); err != nil {
		return err.Error() // invalid: no side effects at all
	}
	if refusal := a.reframeTimeRefusal(in); refusal != "" {
		return refusal // refused: still no side effects, and no verdict latch — re-issue freely
	}
	if err := tools.TaskReframeRecord(a.autoTaskRowID, in.Verdict, in.payloadItems(), a.sessionIDForTasks()); err != nil {
		// Telemetry is best-effort but never silent (Rule 15): surface the
		// miss in the tool result so the model/operator sees it.
		if a.Logger != nil {
			a.Logger.Printf("⚠️ reframe telemetry not recorded: %v", err)
		}
	}
	if first := a.reframeVerdict; first != "" {
		return fmt.Sprintf("task action=reframe: already reframed this run (verdict=%s) — the repeat (verdict=%s) is recorded as telemetry; continue per the FIRST verdict.",
			first, in.Verdict)
	}
	a.reframeVerdict = in.Verdict
	// From here the run has filed its verdict: the commit-beat budget gate
	// (sessionBudgetRefusal) stands aside for the rest of the run, because the
	// remaining commits are executing this decision rather than adding to the
	// backlog. That is what keeps the gate narrow — a run that reframed before
	// the mark keeps the pre-existing commit behaviour for its whole life.
	a.applyReframe(in)

	switch in.Verdict {
	case taskverdict.Doable:
		return "VERDICT " + taskverdict.Doable + ": the epic is executable here — proceed with the work, using the reframed goal as the review anchor."
	case taskverdict.TooBig:
		return a.dispatchReframeRoadmap(in, a.autoTaskRowID)
	default: // taskverdict.Impossible
		return a.dispatchReframeImpossible(in, a.autoTaskRowID)
	}
}

// reframeSubmitFn is the submit entry point dispatchReframeRoadmap calls.
// It is a package var purely as a SEAM: the serial-chain arm has exactly one
// behaviour that no real state can produce — an item's submit FAILING in the
// middle of a roadmap — and the chain's semantics there (the next item links
// to the last SUCCESS, never to the failure) is precisely what must be pinned
// by a test (Rule 5). Everything else goes through the real function.
var reframeSubmitFn = tools.TaskSubmitReturningID

// dispatchReframeRoadmap auto-enqueues the TOO_BIG roadmap as a SERIAL CHAIN:
// one child task per item, the i-th item's parent = the (i-1)-th item's row,
// the first item's parent = this run's ledger row. The model does not need a
// separate task_submit per item — the decomposition it already produced IS the
// submission (SPEC §5: this saves an LLM toolcall).
//
// WHY A CHAIN AND NOT SIBLINGS (operator ruling, 2026-10-05, epic
// 01a10a24): "LLM 提交任务的顺序 = 逻辑依赖顺序… 不要考虑并行的损失 … 默认顺序
// 就是依赖". A model cannot express a dependency GRAPH, so the ORDER it writes
// the roadmap in is the only dependency signal there is. Siblings (the
// pre-existing shape) all became claimable at the same instant the reframing
// row reached its terminal status — the whole plan starting at once, which is
// exactly the dependency the model meant to state, thrown away. The measured
// incident: one epic split into five items that all edited the same files
// (main.go's dispatch chain, cli_verbs.go, cli_verb_guard.go); they ran in
// parallel and collided, and the chain was restored by hand with a direct
// `UPDATE tasks SET parent_task_id`.
//
// The chain is built with the id TaskSubmitReturningID hands back, NEVER by
// picking an id out of the human-readable confirmation string (that prose is
// for the model; parsing it would make the ledger's shape depend on wording).
//
// TWO DEGRADES, both reported rather than silent (Rule 15):
//
//   - an item whose submit FAILS does not break the chain: the next item stays
//     linked to the last item that DID land, so one bad roadmap entry costs one
//     child instead of orphaning every later one;
//   - an item whose submit returns NO id (the mirror leg — taskSubmitRemote
//     forwards a SyncTaskRow that carries no parent column, so there is nothing
//     to link onto) cannot extend the chain, and the run's result says so.
//
// Rule 33 is untouched: the gate, its {completed, verified} release set and the
// absence of any override are the SAME predicate that parked siblings before —
// this change only decides WHICH row each child points at.
func (a *Agent) dispatchReframeRoadmap(in reframeInput, rowID string) string {
	// THE CHILDREN'S REPO (epic 01a0d55b, corrected by bug 01a1515c): taken from
	// the PARENT ROW, never from where this run stands — see reframeChildRepo.
	repo := a.reframeChildRepo(rowID)
	var ids []string
	// parentID is the CHAIN HEAD: the row the next item hangs off. It starts as
	// this run's row (the first item's producer) and advances to each item that
	// lands, so item i's parent is always item i-1 (see the chain rationale on
	// this function).
	parentID := rowID
	orphaned := 0
	for i, item := range in.Roadmap {
		out, childID, err := reframeSubmitFn(reframeSubmitOptions(
			reframeChildSummary(item.Goal, i+1, len(in.Roadmap)),
			reframeChildPayload(a.reframeParentSummary(), item),
			parentID, tools.ReframeRoadmapTag, a.sessionIDForTasks(), a.Model, repo))
		if err != nil {
			// The chain does NOT break on a failed item: the next one links to
			// the last row that landed, so the failure costs this item only.
			ids = append(ids, fmt.Sprintf("[%d] submit FAILED: %v", i+1, err))
			continue
		}
		switch {
		case childID != "":
			parentID = childID
			ids = append(ids, fmt.Sprintf("[%d] %s", i+1, firstLine(out)))
		default:
			// No id to link onto (the mirror leg): the item IS queued, but the
			// chain cannot be extended — reported, never silently treated as a
			// link that was made.
			orphaned++
			ids = append(ids, fmt.Sprintf("[%d] %s (chain-link unavailable: the submit returned no task id — see the mirror-leg note in TaskSubmitReturningID)", i+1, firstLine(out)))
		}
	}
	chainNote := "the roadmap is a SERIAL CHAIN: item 1 hangs off this epic row and item i off item i-1, so the dispatcher offers exactly one item at a time (Rule 33's parent gate does the rest; each item releases the next when it reaches completed/verified)."
	if orphaned > 0 {
		chainNote += fmt.Sprintf(" WARNING: %d item(s) could not be linked to the chain (see the per-item lines) — an unlinked item is claimable immediately and MAY run in parallel with the chain.", orphaned)
	}
	return fmt.Sprintf("VERDICT %s: the harness enqueued %d child task(s) from your roadmap — this epic is CLOSED here (do not start the children yourself; they run in fresh sessions). %s "+
		"Child tasks:\n%s\nFinish with a one-paragraph summary of the decomposition — that diligence is this run's deliverable.",
		taskverdict.TooBig, len(in.Roadmap), chainNote, strings.Join(ids, "\n"))
}

// dispatchReframeImpossible marks this run's ledger row failed with the
// model's reason (the honest exit lands in the ledger, not in a chat reply
// nobody reads) and latches the end hook so the run's own closeout cannot
// overwrite the verdict with "completed".
func (a *Agent) dispatchReframeImpossible(in reframeInput, rowID string) string {
	if rowID == "" {
		return "VERDICT " + taskverdict.Impossible + ": " + in.Reason +
			"\n(no ledger row for this run — the reason is recorded here and in the session log; nothing to mark failed)"
	}
	if _, err := tools.TaskUpdateReason(rowID, tools.TaskStatusFailed, in.Reason, "reframe verdict="+taskverdict.Impossible); err != nil {
		return fmt.Sprintf("VERDICT %s: %s\n(task-ledger writeback failed: %v — the verdict stands; it is recorded in the session log)",
			taskverdict.Impossible, in.Reason, err)
	}
	a.reframeHonestExit = true // the end hook must not re-close this row as completed
	if a.Logger != nil {
		a.Logger.Printf("🛑 reframe verdict=%s: task %s marked failed with the reason", taskverdict.Impossible, rowID)
	}
	return "VERDICT " + taskverdict.Impossible + ": " + in.Reason +
		"\nThe task row is marked failed with this reason (honest exit — no fabricated work). Finish with a short summary; do not attempt the epic."
}

// reframeParentSummary is the epic summary used to contextualize children (the
// reframed epic when available, else the raw briefing's first line).
func (a *Agent) reframeParentSummary() string {
	if a == nil || a.Ctx == nil {
		return ""
	}
	for i := range a.Ctx.Messages {
		if m := &a.Ctx.Messages[i]; m.Role == "user" && strings.HasPrefix(m.Content, "[EPIC GOAL]") {
			return firstLine(strings.TrimPrefix(m.Content, epicGoalHeader))
		}
	}
	return ""
}

// reframeChildSummary renders a child task's ledger headline from a roadmap
// item (one line, numbered when the roadmap has several items).
// reframeChainItemLabel renders a roadmap item's position inside its chain
// ("[2/3] ") — the ledger-side trace of the ORDER the model stated, so a reader
// of `task list` sees the sequence the plan meant (Rule 1b: the format lives
// here, not inline at the call site).
const reframeChainItemLabelFormat = "[%d/%d] "

func reframeChainItemLabel(idx, total int) string {
	return fmt.Sprintf(reframeChainItemLabelFormat, idx, total)
}

// reframeChildSummary returns a child row's headline: the goal's first line,
// prefixed with its roadmap position when there is an order to state (a lone
// item is not "[1/1]" — there is no sequence to read).
func reframeChildSummary(goal string, idx, total int) string {
	line := firstLine(goal)
	if total <= 1 {
		return line
	}
	return reframeChainItemLabel(idx, total) + line
}

// firstLine returns s's first non-empty line, trimmed (ledger headlines are
// single-line by contract).
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
