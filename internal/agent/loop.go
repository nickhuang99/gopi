package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nick/gopi/internal/tasksource"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/runend"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/sgr"
	"github.com/nick/gopi/internal/smokegate"
	"github.com/nick/gopi/internal/taskreport"
	"github.com/nick/gopi/internal/taskverdict"
	"github.com/nick/gopi/internal/text2img"
	"github.com/nick/gopi/internal/thinkmode"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// Thinking-output display markers.
// Reasoning content is streamed to stderr live so the user can see the model
// working, but it must be visually separable from the final answer.
const (
	// thinkingPrefix is the stable textual marker prepended to reasoning
	// output. It is used as-is when stderr is not a TTY (piped/redirected)
	// so the output stays parseable, and wrapped in ANSI dim on TTYs.
	thinkingPrefix = "[thinking] "
	// The reasoning block's open/close SGR sequences are NOT declared here:
	// they are read from internal/sgr below, which owns the one escape
	// vocabulary this project emits (Rule 14). A per-package copy of the
	// literal is the duplication this task retires.

	// thinkingCharsPerToken estimates DeepSeek thinking tokens from streamed
	// characters (measured live: 23,876 chars ≈ 5,970 tokens). The harness
	// uses it to enforce the thinking budget the API ignores.
	thinkingCharsPerToken = 4
)

// ansiDim dims reasoning text on interactive terminals; ansiReset restores
// normal attributes after the block. Both are the sgr package's own sequences
// (DimSeq/ResetSeq), so a change to the escape vocabulary lands in exactly one
// place and cannot drift from what the cell grid interprets. They are VARS
// rather than consts only because the sequence is composed by a call — they are
// never written after init.
var (
	ansiDim   = sgr.DimSeq()
	ansiReset = sgr.ResetSeq()
)

// interruptErr is the run-engine error returned when the agent is interrupted
// by an external signal (SIGINT/SIGTERM → Agent.Interrupt(), the
// chat-once/bare-prompt zombie fix). It unwinds the run through the normal
// end-of-run defer — session_end (exit_code=1) + auto-ledger row failed — and
// its text names the signal so the session_end error block and the row digest
// carry the interrupt reason.
var interruptErr = errors.New("run interrupted by signal")

// errTurnInterrupted is the sentinel a run returns when the OPERATOR abandoned
// the turn in flight (a run-scoped cancel — Ctrl+C at an interactive carrier's
// incoming prompt) while the session itself stays OPEN.
//
// It is a distinct value on purpose, because the two interruptions have opposite
// carriers' obligations: interruptErr ends the SESSION (the carrier unwinds to
// its close-out and writes session_end with a non-zero exit), while this one ends
// only the TURN (the carrier prints the interrupt marker and keeps reading the
// next prompt). Errors.Is is how a carrier tells them apart; the sentence is what
// a forwarded failure would print if one ever escaped without being handled.
var errTurnInterrupted = errors.New("turn interrupted by the operator")

// IsTurnInterrupted reports whether err is the per-turn interrupt sentinel (see
// errTurnInterrupted) — the ONE predicate a carrier uses to decide "abandon this
// turn and keep the session" instead of matching the error's text (Rule 1b: the
// comparison lives beside the value).
func IsTurnInterrupted(err error) bool {
	return errors.Is(err, errTurnInterrupted)
}

// Task-list integrity guardrails (bug010). The heartbeat's accumulated task
// state is the authoritative source for exit decisions; the current round's
// parse may legitimately be nil (DeepSeek's learned token-saving behavior of
// omitting the task list in intermediate rounds). These constants back the
// corrective-message machinery for malformed / rule-violating lists.
const (
	// maxTaskListViolations is the number of consecutive rounds in which the
	// model emits a malformed or rule-violating task list before the run
	// aborts with an error. A single slip is corrected, not fatal.
	maxTaskListViolations = 3

	// malformedTaskListCorrection is injected when [TASK_LIST_BEGIN]...
	// [TASK_LIST_END] markers are present but the JSON between them is
	// malformed. nil,nil (no markers at all) is legal and never corrected.
	malformedTaskListCorrection = harnessMessagePrefix + "Your previous response contained a " +
		"[TASK_LIST_BEGIN]...[TASK_LIST_END] block with malformed JSON. " +
		"Please re-emit a valid task list JSON block with the fields " +
		"task_id, summary and status."

	// deletedTaskCorrection is injected when the model drops a previously
	// established task_id from its list (task deletion is forbidden).
	deletedTaskCorrection = harnessMessagePrefix + "You deleted task_ids from your task list. " +
		"All previous task_ids must be preserved — never delete or rename them. " +
		"Restore the missing task_ids and mark their statuses honestly."

	// maxConsecutiveEndTurnRounds is the number of consecutive rounds with no
	// tool calls required before the agent exits (bug022 fix). DeepSeek V4
	// sometimes ends a turn mid-task (e.g. right after emitting a task list);
	// the follow-up round gives the model a chance to resume tool calls.
	// N=2 means: the first end_turn is NOT trusted as an exit signal, only
	// the second consecutive one is. R3 exception (perf-vs-claude-fix-plan.md):
	// a first end_turn whose task list is ALL completed and whose answer is
	// substantive exits immediately — the bug022 guard exists for mid-task
	// stops, not for declared completion.
	maxConsecutiveEndTurnRounds = 2

	// Task-list establishment nudge (P1.4/P2, audit v2 RC5): the heartbeat's
	// ShouldRemind only fires when CountProgressing(tasks) > 0 — a list that
	// was never established never triggers it, so 25-60-turn list-less runs
	// slip through. These constants back a harness nudge that reminds the
	// model to issue a list when N rounds have elapsed with hasList==false.
	// Default N=5 rounds with at least MIN_TOOL_ROUNDS=3 tool rounds having
	// occurred. Overridable via GOPI_NUDGE_ROUNDS / GOPI_NUDGE_TOOLS for
	// tests (Rule 2: constant = default, env overrides in exactly one place).
	// maxNudgeCount caps the total nudges per session (anti-harassment): the
	// nudge fires once at the first crossing, then again only after nudgeEscapeRounds
	// more rounds if the model still has not established a list. Orthogonal to
	// R2 (R2 governs update cadence; this governs establishment).
	defaultNudgeRounds     = 5
	defaultNudgeToolRounds = 3
	maxNudgeCount          = 2
	nudgeEscapeRounds      = 3 // rounds between the 1st and 2nd (final) nudge

	// staleListBlockRounds is the round-N threshold for the stale-list nag
	// (Report A §6.5): after the list is established, N consecutive rounds
	// with no fresh [TASK_LIST] block trigger the reminder (re-emit it or
	// mark tasks completed).
	staleListBlockRounds = 4

	// noListYetNudge is injected when the above conditions are met. It both
	// issues the reminder and re-asserts the emission contract that R1 context
	// folding may have dropped (audit v2 RC4). One-shot text, <=50 tokens.
	noListYetNudge = harnessMessagePrefix + "No task list established yet — issue one from the Epic goal."

	// staleTaskListNudge is the bookkeeping-decay nag (review 2026-09-07 6.5,
	// C2): the mirror of noListYetNudge. Where that one fires while NO list has
	// been established, this one fires when a list EXISTS but the model has
	// stopped re-emitting it — silent bookkeeping decay, the failure the
	// exit-time check (warnExitingWithProgressing) catches far too late to
	// matter. Fired at the round boundary so the reminder reaches the model
	// that same round. Text mirrors the report's proposed wording.
	staleTaskListNudge = harnessMessagePrefix + "No TASK_LIST block in %d rounds. Re-emit it, or mark tasks completed."

	// minFinalAnswerChars is the minimum length (after stripping the
	// [TASK_LIST] block) for a first end_turn to be trusted as a normal
	// completion (R3). Shorter content means the model stopped without
	// giving a real answer — keep the bug022 confirmation round then.
	minFinalAnswerChars = 40

	// endTurnContinuationMessage is injected as a user message after a round
	// with no tool calls. The model may resume tool use next round; a second
	// consecutive end_turn confirms it truly wants to stop.
	endTurnContinuationMessage = harnessMessagePrefix + "You ended your turn without calling any tools. " +
		"If the task is complete, confirm completion. Otherwise, continue working " +
		"by calling tools (read/edit/bash/code_search/keyword_search/web_search). Do not stop " +
		"while any task in your task list is still 'progressing'."

	// resumeContinuationMessage is appended as the continuation instruction
	// when `gopi --resume <path>` is run with no explicit user input: the
	// recovered context is resumed and the model is asked to continue from
	// where it left off, without repeating completed steps. It is placed as a
	// standalone const after endTurnContinuationMessage to keep the sibling
	// continuation prompts together.
	resumeContinuationMessage = harnessMessagePrefix + "Continue from where you left off. The previous session was interrupted; " +
		"resume the task in this exact context. Do not repeat completed steps. " +
		"If the task is complete, confirm completion. Otherwise, continue working " +
		"by calling tools (read/edit/bash/code_search/keyword_search/web_search). Do not stop " +
		"while any task in your task list is still 'progressing'."

	// completionCheckpointMessage is the one-shot harness enforcement of the
	// task-end memory_store (对称强制 to the task-start memory_search). The
	// framework requires a distilled store before the run's final response, so
	// experience accumulates. This is injected at most once, only at the
	// all-completed completion point, when the run has not yet stored — it is
	// NOT a per-round reminder, so it cannot spam (task-end only).
	completionCheckpointMessage = harnessMessagePrefix + "COMPLETION CHECKPOINT: your task list is all completed and you are about to give " +
		"the final response. Before answering, call memory(action=store) exactly once to persist this run's knowledge gain, " +
		"skill gain, facts discovered, lessons learned, pitfalls avoided, success stories, setbacks, and a summary — " +
		"whatever is worth noting (store distilled experience, not raw logs). This is how task-end memory(action=store) and " +
		"task-start memory(action=search) stay symmetric. Then continue to your final answer."

	// taskStartSearchMessage is the one-shot harness enforcement of the
	// task-start memory(action=search) (对称强制 with the task-end completion
	// checkpoint). Injected once before the first round when the run has not
	// searched — the closed-loop ENTRY point: past experience may already
	// contain the recipe, so the search happens even without LLM volition.
	// Not a per-round reminder (one-shot latch), so it cannot spam.
	taskStartSearchMessage = harnessMessagePrefix + "TASK-START CHECKPOINT: this task has just begun. " +
		"Before planning or reading anything, call memory(action=search) once with the task's core topic " +
		"— past experience may already contain the recipe (取不到→memory(action=store)). Then, while the search result is still " +
		"in front of you, vote memory(action=feedback) IMMEDIATELY per hit (used→useful, contradicted→useless; id=#N) — " +
		"never defer the vote to task end. A hit that your later results CONTRADICT — memory(action=update) it (id=#N + corrected field): feedback says WHAT is wrong, update does the FIX. Then proceed. " +
		"During the task: the moment a tool result is a dead-end (errored/no-value/bulky error output), call context_trim " +
		"with that result's tool_use id immediately — leave a one-line conclusion and keep the clean worktable. " + "Store is EVENT-DRIVEN, not end-of-task: " + eventDrivenStoreTriggers + ". Then proceed."

	// consultCheckpointMarker is the stable, machine-findable marker of the
	// review-time memory-consult checkpoint (candidate C). It is deliberately a
	// constant shared with the telemetry reader (trajectory.go), which latches
	// ConsultCheckpointInjected by finding this marker in the session log — the
	// injection writes a trace the measurement can actually see (a mechanism
	// that leaves no trace is unmeasurable).
	//
	// THE VALUE IS NOT DEFINED HERE (Rule 14): it is
	// sessionevent.HarnessConsultCheckpointMarker, in the leaf package the
	// session-log READERS also import. While the value lived here, a reader
	// could not see it and IsHarnessInjected knew only the `[system] ` prefix;
	// measured on session 80c07a7d, that made the `[SELF-RECORD …]` carrier a
	// "turn the operator typed" on three surfaces. This name stays as the alias
	// the injection site and its tests read.
	consultCheckpointMarker = sessionevent.HarnessConsultCheckpointMarker

	// consultCheckpointMessage is the one-shot review-time accountability
	// question, injected when the FIRST item of the run commits — the point
	// where the run has real evidence about what it would have needed. It is a
	// MEASUREMENT INSTRUMENT, not a work order: it asks the run to name the
	// work a memory search would have saved (with concrete numbers: what was
	// recomputed/re-explored and what it cost), and it says explicitly that the
	// answer costs one line and must NOT spawn work. That framing is what keeps
	// the epic's hard constraint 4 honest ("never ship a noise generator"): the
	// value harvested is a case list, and a run with nothing to report says so.
	consultCheckpointMessage = consultCheckpointMarker + " This round: which work could a memory search have saved? " +
		"Answer in ONE line (or \"none — prior knowledge was already sufficient\") naming (a) what you recomputed, " +
		"re-explored or re-derived that distilled experience already contained, and (b) roughly what it cost " +
		"(tokens, rounds, or repeated reads). This is a MEASUREMENT, not a work order: do NOT start new work, do NOT " +
		"re-plan the task, and do NOT search again for the sake of the answer. If a past search already covered it, say " +
		"so and stop. If nothing was wasted, \"none\" is the correct and expected answer."

	// deadEndTrimMessage is a reactive prompt nudge injected only when a
	// tool result just arrived that looks like a dead-end (an errored /
	// failed / no-value result — the exact case context_trim exists for). The
	// task-start priming is read once and forgotten by the time a mid-task
	// dead-end appears, so this short message fires AT the dead-end moment,
	// pointing the model at the specific failed call (its tool_use id) and
	// telling it to trim it now. It does NOT perform the trim — the model
	// still must call context_trim (verified in the session tool_use log);
	// this is pure prompt wiring, a sibling of taskStartSearchMessage. The
	// deadEndNudgePending latch (one per round) keeps it from ever spamming.
	deadEndTrimMessage = harnessMessagePrefix + "The tool call above just returned a dead end (error/failure/no-value output). " +
		"Trim it now with context_trim: call_id is the '[tool_use id: …]' stamped on that result, " +
		"replace_with = a one-line conclusion (e.g. 'X failed because Y — don't retry'). " +
		"A dead-end result bloats every later round — trimming stops that. Then continue with the task."

	// maxThinkingAbortStreak is the number of consecutive rounds whose
	// thinking exceeds the harness budget before the run PAUSES thinking
	// (gopi-race R5-R6 post-mortem, session 952b7b73): 4/8 rounds there
	// aborted and re-issued, each paying two full prefills (160-600s vs
	// 6-10s for normal rounds). Two aborts in a row means this run's
	// thinking tends to run away — the following rounds run thinking-disabled
	// at one prefill each. A round that completes within budget resets the
	// streak. The pause is NOT permanent: see thinkingProbeSpacing.
	maxThinkingAbortStreak = 2

	// thinkingProbeSpacing is how many rounds thinking stays paused before a
	// single thinking-ENABLED probe round is allowed back on the wire (2026-09-15:
	// the latch used to be permanent for the whole run, so ONE runaway round —
	// near the start of a long task — silently cost every later round its
	// reasoning; the operator saw "disabling thinking for the rest of the
	// session" with no way back). The probe costs at most one extra aborted
	// round (no new double-prefill loop): a probe that finishes within budget
	// CLEARS the pause and thinking stays on; a probe that blows the budget
	// re-engages it immediately for another thinkingProbeSpacing rounds.
	thinkingProbeSpacing = 8

	// maxStuckRounds is the agent-loop deadlock net (incident t-1788173679:
	// 4276 iterations, every 20s one "已完成：HEAD" plain-text round for
	// 12h+ — the loop had no bottom guard; Rule 7's stop relied purely on
	// LLM volition). When this many CONSECUTIVE rounds produce no tool_use
	// AND text whose first stuckTextPrefixChars runes match the previous
	// such round's text, the run is declared stuck and terminated
	// (terminateStuckRun → GOPI_TASK_NOTIFY event=loop-stuck + error
	// return). The bug022 end-turn machinery (maxConsecutiveEndTurnRounds=2)
	// would already have exited, so a run reaching N=5 stuck rounds is one
	// whose end-turn signal never fires (malformed stop reasons, polite
	// waiting loops, external-state polling). Reset conditions: any
	// tool_use round (real work) or a text that diverges beyond the prefix
	// window (still writing different things). N=5 sits above the
	// bug022 confirmation pair and below any plausible productive cadence.
	maxStuckRounds = 5

	// stuckTextPrefixChars is the similarity window for "same or近似 text":
	// the first N runes of each no-tool round's text. Incident
	// t-1788173679's rounds were byte-identical ("已完成：HEAD"); a prefix
	// window also catches the near-duplicate variants (page numbers,
	// reflowed whitespace) that poll-and-report loops produce, while
	// ordinary final-message rounds ("Here is the summary: …" vs "I also
	// verified …") diverge within a few runes and stay below the threshold.
	stuckTextPrefixChars = 50

	// notifyEventLoopStuck is the wire name of the synthetic GOPI_TASK_NOTIFY
	// event fired when the stuck detector terminates a run (sibling of
	// "stale", "terminal-push-failed", "legacy-conflict").
	//
	// The name is DECLARED in internal/tools (tasknotify.go) and aliased
	// here: the "all" filter enumerates every alarm from that package's
	// taskNotifyAlarmEvents registry, so a literal mirrored into this one
	// would silently fall out of the enumeration (Rule 14 — measured: all
	// three agent-side alarms were invisible to GOPI_TASK_NOTIFY_EVENTS=all
	// because of exactly that mirror).
	notifyEventLoopStuck = tools.NotifyEventLoopStuck

	// notifyEventLoopStuckToolful is the synthetic GOPI_TASK_NOTIFY event
	// fired when the toolStreak detector terminates a run: N consecutive
	// rounds of IDENTICAL tool_use (same tool + same normalized parameter
	// hash + same text) — the blind spot of the first counter. Incident
	// Bond 718319 (session 2b38bfa2, 2026-09-02): the process was adopted
	// after delivery but never exited — it polled `bash sleep 115; ssh hp
	// 'sqlite3 … SELECT task_id,status…'` 272 times (~120s each, 10h15m).
	// Every round HAD a tool_use, so the no-tool stuck counter never fired.
	// Same shape as t-1788173679 (12h, 4276 iter) — the two incidents share
	// the "monitor loop after the real work is done" DNA.
	notifyEventLoopStuckToolful = tools.NotifyEventLoopStuckToolful

	// maxStuckToolfulRounds is the toolStreak detector's threshold: N
	// consecutive rounds of identical tool_use terminate the run. A round
	// qualifies when it carries >=1 tool_use whose (tool name, normalized
	// parameter hash, first stuckTextPrefixChars runes of the round text)
	// all equal the previous qualifying round's. Reset: any round whose
	// tool_use differs (different tool, different params — real work
	// changes its parameters every round, e.g. sleep 115 + fixed ssh =
	// byte-identical, a genuine progress poll varies), or a no-tool round.
	// Incident-shaped: sleep 115 → 10 rounds ≈ 20 minutes — a sane "waiting
	// ceiling" (the loop was 272 rounds / 10h; N=10 would have cut it at
	// ~20min). Default 10; override GOPI_STUCK_TOOLFUL_ROUNDS (Rule 2:
	// single reader — stuckToolfulRounds()).
	maxStuckToolfulRounds = 10

	// stuckToolfulRoundsEnv is the toolStreak threshold override.
	// GOPI_STUCK_TOOLFUL_ROUNDS: unset → default (10), "0" → disabled,
	// positive → override, invalid → default + stderr warning.
	stuckToolfulRoundsEnv = "GOPI_STUCK_TOOLFUL_ROUNDS"

	// notifyEventStuckObservation is the wire name of the THIRD loop alarm:
	// an observation-only run (a look-without-fixing loop) terminated by the
	// observation bound. Declared in internal/tools (tasknotify.go) and
	// aliased here for the same reason as its two siblings — the
	// GOPI_TASK_NOTIFY_EVENTS=all expansion enumerates that package's
	// taskNotifyAlarmEvents registry, so a mirrored literal would be invisible
	// to the subscriber who asked for everything (Rule 14).
	notifyEventStuckObservation = tools.NotifyEventStuckObservation

	// maxObservationRounds is the observation bound's threshold: N
	// consecutive rounds that ONLY looked terminate the run. It is the
	// third net (§6.3 of devdoc/cli-as-eyes-and-hands-feasibility.md), and
	// it exists because the other two are structurally blind to this shape:
	// stuckState needs a round with NO tool_use (an observing round always
	// has one), and toolStreakState needs a byte-identical fingerprint
	// while ALSO exempting a poll whose RESULT changed — a live screen
	// changes every round, which that exemption reads as "alive". The bound
	// is therefore on the missing quantity: consecutive looks with no act.
	//
	// RESET is deliberately generous (the bug055 lesson): a MUTATOR tool
	// (write/edit/task/task_submit/memory/text2img) — the action class the
	// epic names — resets immediately AND re-arms a high-water exemption
	// (maxObservationRounds×2) for the burst of verification looks that
	// follows real work; the polling tool `job` is an ACT here (detector 2
	// owns polling semantics, so a poll is never billed twice) and a no-tool
	// round is the wall-clock path's to own, not a look. A run therefore
	// survives a >48-look verification burst after a mutation, and a looker
	// that never mutates trips at N.
	//
	// Threshold justification (MEASURED against the real corpus, not tuned):
	// the scan in devdoc/look-without-fixing-bound.md reports, over 2317
	// sessions with at least one round, the longest unbroken
	// observation-without-act streak per session — p99 = 6, p99.9 = 24,
	// MAX = 32 (session 42cfe7ab: 32 consecutive diagnostic reads, a
	// genuinely productive deep investigation, which then acted). A threshold
	// must not cut off that session: N=24 would have terminated 6 real
	// sessions, one of them named above. N=40 sits above the corpus maximum,
	// so at the shipped default the bound fires on ZERO of the 2317 measured
	// sessions — an honest measured NEGATIVE (the loop does not occur in the
	// corpus) and a deliberate prospective ceiling for the observation
	// surfaces the spec is about, which the corpus predates.
	maxObservationRounds = 40

	// observationRunEnv is the observation bound's threshold override, same
	// convention as its siblings: unset → default (maxObservationRounds),
	// "0" → DISABLED (the bound is off), positive → that many rounds,
	// invalid → default + stderr warning. Exactly one reader
	// (observationRounds(), Rule 2).
	observationRunEnv = "GOPI_STUCK_OBSERVE_ROUNDS"

	// notifyEventWallclockExceeded is the ② synthetic GOPI_TASK_NOTIFY
	// event: the session wallclock cap finished the run gracefully
	// (sibling of loop-stuck — the operator-facing signal that the run
	// stopped at the environment boundary, not by declared completion).
	notifyEventWallclockExceeded = tools.NotifyEventWallclockExceeded

	// defaultSessionWallclock is the wall-clock cap for one session
	// (② of the loop-deadlock epic): a session running past it is finished
	// gracefully — the loop stops issuing new LLM rounds, returns the last
	// substantive assistant text, and the normal end-of-run bookkeeping
	// (session_end + task-ledger close + notify) closes it out. Derived
	// from the incident: the 12h+ loops ran unattended overnight.
	defaultSessionWallclock = 8 * time.Hour

	// sessionWallclockEnv is the wall-clock cap override (seconds).
	// GOPI_SESSION_MAX_WALLCLOCK=0 means UNLIMITED (the cap is disabled);
	// unset means the 8h default; a positive value overrides it.
	// Rule 2: exactly one reader — here.
	sessionWallclockEnv = "GOPI_SESSION_MAX_WALLCLOCK"

	// ── the session time budget the PROMPT teaches (P1-P3) ──
	// sessionBudgetMinutes is the planning budget the system prompt asks the
	// model to hold itself to: a task that cannot finish inside it is TOO BIG
	// by definition, and running out is a planning failure rather than a
	// graceful ending. ONE definition feeding all three faces — the tick-clock
	// line, the task-management rule, and the commit return's elapsed/budget
	// line (Rules 1b/2/13: one source, no inline numbers, no drift).
	//
	// It is NOT defaultSessionWallclock (8h) and must not be folded into it:
	// the 8h figure is the boundary the ENGINE enforces on a run, this one is
	// a number the PROMPT teaches the model to plan against. Different
	// audiences, different jobs — see devdoc/session-budget-prompt.md.
	sessionBudgetMinutes = 60

	// sessionBudgetEnv is the budget override (MINUTES).
	// GOPI_SESSION_BUDGET=0 means UNLIMITED (no budget line, no escalation);
	// unset means sessionBudgetMinutes; a positive value overrides it.
	// Rule 2: exactly one reader — sessionBudget().
	sessionBudgetEnv = "GOPI_SESSION_BUDGET"

	// sessionBudgetWarnPercent is the escalation mark: past this share of the
	// budget the commit return stops merely reporting and names the concrete
	// available action (split via reframe/TOO_BIG), and the commit beat
	// REFUSES a further subtask until the run lands or splits (see
	// sessionBudgetRefusal).
	sessionBudgetWarnPercent = 75

	// sessionBudgetRefusalEnv is the KILL SWITCH for the commit-beat gate (the
	// epic's option A). Unset = the gate is ON; "0"/"false"/"off" = OFF.
	//
	// It exists because the operator's constraint is that enforcement must
	// never be the thing standing between a run and its work ("强制不好"): an
	// operator who is blocked by a refusal turns it off without rebuilding.
	// Rule 2/21: one declared name, one reader — sessionBudgetCommitRefusal().
	sessionBudgetRefusalEnv = "GOPI_SESSION_BUDGET_REFUSAL"

	// ── memory-aware T2: framework hard-takeover of the task list ──
	// maxTaskAreaNudges caps how many completion-nudge user messages the
	// framework injects when the model declares itself done ("all done" /
	// "task complete" / a no-tool exit round) while the session's pending
	// task(action=commit) set is not yet fully committed. Nick's design: the
	// LLM has no authority to declare overall completion via free text —
	// each item must be committed through task(action=commit,
	// status=completed) and the framework holds the final word. The cap (3)
	// bounds the loop: when the model chooses not to comply, the harness
	// stops insisting and lets the run exit (anti-deadlock — the framework
	// never hard-blocks a run that refuses to participate).
	maxTaskAreaNudges = 3

	// taskAreaCompletionNudge is the nudge user message injected on a no-tool
	// exit round when the framework's pending set is non-empty (see
	// pendingTaskAreaConclusionGuard). It names the exact un-committed
	// task_ids and the two allowed resolutions (commit each via task
	// action=commit, or commit a conclusion marked skipped with the
	// reason) — the model cannot satisfy the guard with prose alone.
	taskAreaCompletionNudge = harnessMessagePrefix + "FRAMEWORK COMPLETION CHECK: your task list still has items without a committed " +
		"conclusion. Per the working-area contract, an item is finished only when its conclusion is committed via " +
		"task(action=commit) — free-text completion announcements do not close items. Commit each remaining item now " +
		"(task(action=commit, task_id=..., status=completed, conclusion=...)); for any item you are intentionally " +
		"skipping, commit it with status=completed and state 'skipped: <reason>' in the conclusion. Pending items: %s"
)

// pollingTools are the tools whose semantic contract is "poll/wait/check
// progress on an EXTERNAL state": the merged job tool (its output action —
// return the job's current status + capped output — is safe to call any
// number of times; its start action spawns a detached job whose result
// carries the unique job id; its kill action waits for the reaper and its
// result carries the final state). A Bond legitimately repeats these with
// IDENTICAL arguments while WAITING — polling the same job action=output
// job_id=… every round is by design, not evidence of a stuck loop (bug055
// follow-up, t-1788306421/…: three legitimate job action=output waits killed as
// "loop-stuck-toolful" on 2026-09-02). The stuck detector is name-level
// (isPollingRound checks pollingTools[tc.Name]): one job tool name exempts
// ALL its actions — the same semantics as the pre-merge exemption, where
// every job_* tool was already exempt. For these tools identical tool+args
// alone is NOT a stuck signal: the POLLED TARGET's state is the real signal
// (a changing result — elapsed ticking, a new job id, a status flip —
// proves the Bond is waiting on a live, finite external change; a
// byte-identical result N times proves the target is frozen = genuinely
// stuck). Tools NOT in this set keep the pure fingerprint signal (the
// 718319 bare `bash sleep 115; ssh …` loop must STILL trip — bash has no
// external wait semantics).
var pollingTools = map[string]bool{
	toolnames.ToolJob: true,
}

// deadEndMarkers are the substrings that mark a tool result as a dead end: a
// failed command (non-zero exit / error / not-found), a read/write/edit on a
// missing file, a crash traceback, or an explicit "No such" failure. Chosen
// conservatively so a legitimately useful result does not get nudged; the nudge
// is only a prompt (the model still decides whether to actually trim).
var deadEndMarkers = []string{
	"Bash error",
	"Error reading",
	"Error writing",
	"Error editing",
	"exit=", // bash exit=N line — re-targeted below only when non-zero
	"no such file",
	"No such file",
	"not a git repository",
	"Traceback",
	"ModuleNotFoundError",
	"Exception",
	"not found",
	"failed",
	"[stderr]",
	"Missing required parameter:",
}

// toolErrorMarkers are the per-tool error-contract prefixes a tool emits
// itself when its EXECUTION FAILS (dispatcher returns "... error: ..."). These
// always signal a real tool failure regardless of which tool produced them, so
// they stay on for every tool — including the content-returning ones whose
// retrieved body is not a status signal.
var toolErrorMarkers = []string{
	"code_search error",
	"keyword_search error",
	"web_search error",
	"web_fetch error",
	"memory error",
	"vision_analyze error",
	"text2img error", screenCaptureErrorPrefix,
	"job error",
}

// contentReturningTools are tools whose result body is RETRIEVED CONTENT
// (memory records, search snippets, web pages, code) rather than an
// execution-status report. The generic deadEndMarkers ("failed", "not found",
// "Exception", "[stderr]", ...) are bare-substring matches that naturally
// recur inside that retrieved prose — a memory QA answer like "X failed because
// Y", a code snippet carrying "[stderr]", a search snippet with "not found" —
// WITHOUT the tool itself having failed. Applying the generic markers to these
// results produces FALSE-POSITIVE dead-end nudges ("trim your useful
// memory_search result") and confuses the model (observed live 2026-08-29:
// memory_search returning 5 relevant recipes was "trimmed as a dead end" when
// its answer prose contained "failed"). For these tools only the real
// toolErrorMarkers (the tool's own "... error:" contract) count as a dead end.
var contentReturningTools = map[string]bool{
	toolnames.ToolMemory:        true, // memory(action=search) returns content
	toolnames.ToolWebSearch:     true,
	toolnames.ToolWebFetch:      true,
	toolnames.ToolCodeSearch:    true,
	toolnames.ToolKeywordSearch: true,
}

// resultsHaveDeadEnd reports whether any tool result in the batch looks like a
// dead end. The "exit=" marker is only a dead end when it is followed by a
// non-zero code (exit=0 is a success); all other markers are treated literally.
// toolCalls and results are index-aligned (results[j] is the result of
// toolCalls[j]). Content-returning tools (memory_search, web_search, ...) only
// match their own tool-error contract markers, so retrieved prose ("failed",
// "not found", "Exception" inside a record/search body) never triggers a false
// dead-end nudge against a productive result.
func resultsHaveDeadEnd(toolCalls []deepseek.ToolCall, results []string) bool {
	for j, r := range results {
		// For content-returning tools, only the tool's own error contract
		// signals a failed execution — never the generic prose markers.
		contentReturning := contentReturningTools[toolCalls[j].Name]
		if toolCalls[j].Name == toolnames.ToolMemory {
			// Action-aware (tool-family merge, memory family): only
			// action=search returns retrieved prose; store/update/feedback
			// return short status text, so the generic markers may apply.
			if action, _ := toolCalls[j].Arguments["action"].(string); action != toolnames.ToolMemoryActionSearch {
				contentReturning = false
			}
		}
		if contentReturning {
			for _, m := range toolErrorMarkers {
				if strings.Contains(r, m) {
					return true
				}
			}
			continue
		}
		for _, m := range deadEndMarkers {
			if strings.Contains(r, m) {
				// "exit=0" is a success, not a dead end — only non-zero exits
				// count, and only when it is a bash-style exit line.
				if m == "exit=" {
					if isNonZeroExit(r) {
						return true
					}
					continue
				}
				return true
			}
		}
	}
	return false
}

// isNonZeroExit reports whether the result carries a non-zero bash exit code
// (e.g. "exit=1", "exit=128"). Pure success results end in "exit=0".
//
// The code is PARSED, not enumerated: a hard-coded list {1,2,128,127,126,255,
// 11,137} silently missed 3/42/134/143 (git exit 3, curl 42, SIGABRT/SIGTERM
// 128+N family) — any non-zero code must count (t-1788053264-2). The match is
// anchored to a real bash exit line ("exit=N" at line start or after a
// leading "[t+…s] " tick), so embedded text like "the pexit=1 prefix" or an
// "exit=foo" non-numeric line is NOT a dead end.
func isNonZeroExit(r string) bool {
	for _, line := range strings.Split(r, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "] "); i >= 0 && strings.HasPrefix(line, "[t+") {
			line = strings.TrimSpace(line[i+2:]) // strip the tick prefix
		}
		if strings.HasPrefix(line, "exit=") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "exit=")))
			return err == nil && n != 0
		}
	}
	return false
}

// warnExitingWithProgressing logs the exit-observability warning when the
// run is about to return while its task list still has progressing tasks
// (pure observability — we do NOT interrupt; the model decided to finish and
// we trust its judgment). Fired from the REAL exit paths only (normal
// end_turn returns and the max-iterations bail): an exit-check that fires on
// every round is stderr spam whose "exit with" wording misleads (the
// self-review's t-1788053264-1 — the unguarded check sat on the every-round
// path and logged the same warning 3+ times in a normal 4-round run).
//
// Completion view: THIS round's list when it re-emits one (the receipt case),
// else the harness-tracked state (P1.5 — a final message may legitimately
// omit the list; the tracked list is the run's contract state).
func (a *Agent) warnExitingWithProgressing(tasks []TaskItem) {
	if a.Logger == nil {
		return
	}
	view := tasks
	if view == nil && a.TaskState != nil {
		view = a.TaskState.view()
	}
	if view == nil || AllCompleted(view) {
		return
	}
	a.Logger.Printf("[gopi] run exiting with %d progressing / %d pending task(s): %v",
		CountProgressing(view), CountPending(view),
		func() []string {
			ids := make([]string, len(view))
			for i, t := range view {
				ids[i] = string(t.TaskID)
			}
			return ids
		}())
}

// lastSubstantiveText returns the best available text for a graceful
// wallclock-cap finish: the most recent non-trivial assistant text still in
// context (the work's last word), or a short cap note when nothing
// substantive was ever produced. Scanned newest-first over the in-memory
// context; tool_result and system-injected user messages are skipped.
func lastSubstantiveText(a *Agent) string {
	if a.Ctx != nil {
		// ONE BACKWARD WALK OVER BOTH SHAPES, not two walks. Since the fold
		// (turnfold.go) a completed turn's own last word is no longer an
		// assistant message — it is the conclusion line of the note that
		// replaced it — so a run whose newest turn is folded has TWO kinds of
		// candidate in the array that mean the same thing. Walking newest-first
		// and taking whichever shape appears first keeps "the most recent
		// non-trivial text" true; scanning for assistant text first and falling
		// back to the notes only afterwards would answer with the OLDEST
		// unfolded turn's word (measured: a 4-turn fixture answered "understood"
		// — turn 1's — while turn 3's conclusion was the real last word).
		for k := len(a.Ctx.Messages) - 1; k >= 0; k-- {
			m := a.Ctx.Messages[k]
			if m.Role == roleAssistantMessage {
				if t := strings.TrimSpace(m.Content); t != "" {
					return t
				}
				continue
			}
			if t := turnFoldConclusionClaim(m); t != "" {
				return t
			}
		}
	}
	return fmt.Sprintf("[gopi] session wallclock cap reached (%v) — finished gracefully before the model produced a final answer", sessionWallclockCap())
}

// toolRoutingRules teaches the LLM which tool to reach for in which situation,
// in the dense counter-example style measured in the dsh study (competition #4,
// 2026-08-21: dsh's 4.1K-character system prompt, one sentence + one
// "not shell X" counter-example per tool, produced a fraction of gopi's
// per-round thinking). The model's trained default is "grep the code"; these
// rules override it: code_search returns every definition of a symbol in one
// structured call (file:line, kind, signature), while bash grep returns raw
// noisy lines the model then has to read and filter. This is framework-level,
// not user-configurable.
const toolRoutingRules = `
[TOOL ROUTING — not a shell workaround]
- MEMORY (if-then) — 记忆=竞争力:
  a. start each task with memory(action=search) FIRST.
  b. at completion call memory(action=store) once; mid-task notable find/pitfall → memory(action=store) NOW — ` + storeOnTheSpotClause + `
  c. IMMEDIATELY after each memory(action=search) vote memory(action=feedback, rating=useful/useless/unused) id=#N; useless → memory(action=feedback, rating=useless)+memory(action=update).
- read: Use read — not shell cat/head/sed; sed/head/tail = byte-precise extraction INSIDE a compound command.
- edit: one unique string or all — not write.
- bash: build/test — not for reading files or searching code.
- code_search: symbol DEFINITIONS — Call code_search FIRST with the bare identifier, not shell grep; returns every definition in ONE structured call.
- context_trim: dead-end/errored/no-value result → trim NOW + leave one-line conclusion; copy header tool_use id.
- git_blame: git file line-history — "谁写的这行 / 哪次 commit 引入 bug" — git_blame(file[,start,end]), not bash git blame. Author meaningless here (everyone is gopi); read task_summary + task_status for INTENT first.
- task(action=reframe): DOABLE → do it; never dodge; TOO_BIG → roadmap; IMPOSSIBLE → reason. task_submit: an independent work item.
`

// efficiencyRules is an immutable block appended to the system prompt. It
// teaches the LLM to minimize round-trips: batch independent tool calls into
// one response, act once enough information exists, and keep output terse.
// The trained default (one tool call per round, narration between steps) is
// what produced the 68-round / 19-minute runs — the execution layer can run
// any all-read-only batch concurrently, but never gets the chance when the
// model emits one call at a time. This is framework-level, not
// user-configurable.
const efficiencyRules = `
[EFFICIENCY RULES]
1. PARALLEL: all independent tool calls in ONE response.
2. ACT WHEN READY.
3. BE CONCISE.
4. Report outcomes faithfully.
`

// maxRoundsWithoutTaskList is the cadence at which the model must emit a
// [TASK_LIST] block even when nothing changed (R2 — devdoc/perf-vs-claude-fix-plan.md):
// re-emitting the full JSON list every round costs 150-500 output tokens/round
// of pure bookkeeping (the model also invents create_at timestamps). The
// harness already tolerates omitted lists (ParseTaskList returns nil,nil and
// the loop never wipes tracked state on nil), so the prompt only relaxes the
// emission cadence — no harness change needed.
const maxRoundsWithoutTaskList = 3

// taskReportKB is the bytes-per-kilobyte divisor used to render
// taskreport.MaxBytes in the prompt's human-readable cap (the byte VALUE
// itself lives only in taskreport — this is a unit, not a policy knob).
const taskReportKB = 1024

// emissionContract is the single source of truth for WHEN a task list must be
// established and re-emitted (P1 — tasklist-audit compare-report §9): rule 1
// (first substantive round), rule 2's re-emission cadence, and the Epic
// framing's "first substantive round" all previously restated this
// independently, and the drift pushed deferred emissions (the RC2 21%
// plan-tailing class). Every statement of the establishment deadline and the
// re-emission cadence now interpolates this constant — change it here only.
// emissionContract is the task-list establishment deadline the model reads in
// rule 1 (declared here, not inline, so a doc that quotes the contract and
// this rule cannot drift apart — Rule 1b/13). A TASK rule counts it against
// the density gate, so it is folded into rule 1's SCOPE sentence rather than
// given a rule of its own.
const emissionContract = "[TASK_LIST_BEGIN] by your second tool round"

// taskManagementRules is an immutable block appended to the system prompt.
// It teaches the LLM how to produce structured task lists for progress tracking.
// A var (not const) so the emission-cadence constant is single-sourced.
//
// Rewritten 2026-09-07 against devdoc/system-prompt-review-2026-09-07.md
// (T1-T8): three-state vocabulary with a cardinality rule (T2), the DONE gate
// that includes the project's own build+test+report contract (T8 — GOPI.md
// Rule 5 finally reaches the agent prompt), "tool rounds" instead of the
// undefined "responses" (T4), a sanctioned way to retire a task (T5), the
// trivial-task exemption (T7), and an inverted decomposition rule — the old
// rule 4 marked the parent "completed" while its children were still
// progressing, which contradicted the all-completed DONE rule (T3). The
// undefined "Epic ≠ task" jargon is gone (T6): rule 1 states the same intent
// positively. The JSON shape lives in the todo tool description (the task-list
// tool), which is sent every round anyway — only the field names stay here as
// a first-emission hint.
//
// This is framework-level, not user-configurable.
//
// Rule 5's report requirement names the report path's SOURCE as an env var
// (taskreport.PathEnv) and the byte cap (taskreport.MaxBytes) from the
// package that OWNS both — a hardcoded path or cap here would drift from the
// writer/seal that enforce them (Rule 1b/13/14).
//
// It is a FUNCTION because rule 5 renders the session budget, whose value the
// env override may change (sessionBudget(); Rule 1b/2): a package-level var
// would freeze the budget at process init and a GOPI_SESSION_BUDGET set later
// would decorate no prompt. The two faces of the rule (this block and the
// tick-clock line) therefore render from the SAME source at the same moment —
// they cannot disagree.
func taskManagementRules() string {
	return fmt.Sprintf(`
[TASK MANAGEMENT RULES]
1. SCOPE: >=3 real steps gets a list; a one-shot lookup skips it. PLAN (not receipt): %s.
2. STATUS: pending -> progressing (exactly ONE) -> completed; a parent waits on its children; never narrate progress in thinking.
3. Re-emit on change; idle <= every %d tool rounds.
4. Done item: task(action=commit) → task(action=review, verdict=A..E).
5. %s
6. DONE = ids completed AND verification green AND, with %s set, the report written there (## Completed/## Files/## Verification/## Not Done, <=%dKB).
`, emissionContract, maxRoundsWithoutTaskList, sessionBudgetRule(), taskreport.PathEnv, taskreport.MaxBytes/taskReportKB)
}

// bashEfficiencyRules is an immutable block appended to the system prompt.
// It encodes the measured efficiency levers from the gopi-vs-claude race
// (2026-08-06): same model and endpoint, but claude finished the task in
// 7 min vs gopi's 27 min. The gap came from per-round information density
// (one dense command instead of several tiny ones), early git-history
// awareness, and cheap verification — not from model speed.
const bashEfficiencyRules = `
[BASH EFFICIENCY RULES]
1. Chain steps into ONE command (the project's own build+test gate); parallel batches are separate and fine.
2. Many facts, cap output. 3. On error: adjust, don't re-run.
4. Early: git log --all. 5. Verify cheap.
`

// todoState is the harness's view of the model's task list, fed by every
// response that carries a [TASK_LIST] block and read by the todo tool (A3,
// competition #4: gopi made 19 tool calls with zero todo calls and burned
// thinking narrating progress). Querying the tracked state costs the model
// nothing to compose — it replaces "self-justifying progress" narration in
// thinking with a one-call lookup.
type todoState struct {
	mu      sync.Mutex
	tasks   []TaskItem
	hasList bool
}

func newTodoState() *todoState { return &todoState{} }

// update records the latest parsed task list. A nil list (no markers in the
// response) is legal and must NOT wipe previously tracked tasks.
//
// M4.5 ⑦ 存活追踪: the moment a tasklist block ARRIVES is a raw fact — the
// ledger's tasklist_issued_at column is touched here (best-effort, never
// blocks the loop) so `task list`/stale detection can see the harness is
// receiving the model's bookkeeping.
func (ts *todoState) update(tasks []TaskItem) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tasks = tasks
	ts.hasList = true
	if len(tasks) > 0 {
		touchTasklistIssuedAt()
	}
}

// view returns a snapshot of the tracked task list (nil when no list was
// ever established). Used by the exit path to warn on the run's contract
// state even when the final message omits the task list (P1.5).
func (ts *todoState) view() []TaskItem {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.hasList {
		return nil
	}
	return ts.tasks
}

// format renders the tracked state for the todo tool result.
func (ts *todoState) format() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.hasList || len(ts.tasks) == 0 {
		return "No task list yet — emit a [TASK_LIST_BEGIN]...[TASK_LIST_END] block in your next response to establish one."
	}
	pending, progressing, completed := CountPending(ts.tasks), CountProgressing(ts.tasks), 0
	for _, t := range ts.tasks {
		if t.IsCompleted() {
			completed++
		}
	}
	return fmt.Sprintf("%d pending, %d progressing, %d completed — current task list:\n%s",
		pending, progressing, completed, FormatTaskList(ts.tasks))
}

// jobRules teaches the job-management discipline measured in dsh's study
// (competition #4): long-running work is detached via job(action=start),
// never run through bash with a giant timeout — bash kills its WHOLE
// process group on timeout (a `nohup ... &` child dies with it; gopi's #4
// build was killed this way), while job(action=start) runs the command in
// its own session where no bash timeout can reach it. And background jobs
// are never busy-polled: start → continue other work → collect once with
// job(action=output). Framework-level, not user-configurable.
const jobRules = `
[JOB RULES]
1. Outlives a bash call? job(action=start).
2. job(action=output) ONCE; 3. kill when done.
`

// engineeringRules = engineeringRulesBase (the 7 text2img habits) + rule 8 —
// ONE definition for both prompt paths (Rule 1/13/14; industry_practice.go).
const engineeringRules = engineeringRulesBase + industryPracticeRule8 + "\n" + recordDisciplineRule9 + "\n"

// engineeringRulesBase is [ENGINEERING RULES] minus rule 8.
const engineeringRulesBase = `
[ENGINEERING RULES]
1. 约束反应: CREATE within the constraint FIRST.
2. 渲染真值: render check = PIXEL-level truth, not OCR.
3. 根因优先: 5-why root cause first.
4. 最小样本: verify the SMALLEST sample.
5. 回归测试: every bug fix ships a minimal repro test.
6. 配方入记忆: proven recipes → memory(action=store) (复用不重做).
7. 依赖预算: ZERO-DEP default.
`

// memLayoutHint tells the model which context regions are read-only anchors
// and which are its own working area. No softness label: the only other
// sections carry none, and the write_task_area conclusion line here is NOT
// advisory — the framework intercepts a no-tool exit round whose tracked items
// lack committed conclusions (pendingTaskAreaConclusionGuard), so labelling
// this "soft" understated a hard contract (review 2026-09-07 A5).
const memLayoutHint = `[WORKING MEMORY LAYOUT]
SYSTEM+briefing read-only; analysis/rounds = working area, COMPACT.`

// conciseThinkingRules steers reasoning toward claude's concise-output-style
// pattern (competition #3 root cause: DeepSeek ignores budget_tokens on the
// Anthropic-compatible endpoint, so gopi's 2048-token budget was fiction and
// thinking ran to ~7.5k tokens/round — see devdoc/thinking-budget.md).
// The harness hard-caps thinking (chatOnce downgrades over-budget rounds to
// thinking=disabled), so this prompt block is what keeps TYPICAL rounds light.
// Competition #4 measured the gap: gopi's thinking median ~430 chars vs
// claude's ~280 on the same model+endpoint; the counter-example tool rules
// (toolRoutingRules) plus the explicit character target here pull gopi toward
// the claude median (150-280 chars). Framework-level, not user-configurable.
const conciseThinkingRules = `
[THINKING RULES]
1. Think briefly: 1-2 sentences (under ~250 characters).
2. No recaps; lead with the call.
`

// clockRule is the CLOCK-TICK system-prompt line: a ticking clock runs from
// session start and every tool result carries its elapsed tick. The tick is
// the LLM's primary time signal — timestamps are useless to a model that
// cannot do arithmetic on them ("bad at math"), while a natural-number counter
// the tick makes relative speed directly perceptible.
//
// It also carries the SESSION BUDGET (P1): the sentence that explains the tick
// is where the planning ceiling belongs, because the tick is what makes the
// ceiling legible — the model already reads the tick every round, and now it
// knows what it is counting toward. The number comes from sessionBudget()
// (one source, Rule 1b/2), never a literal here.
func clockRule() string {
	if minutes := sessionBudgetMinutesResolved(); minutes > 0 {
		return fmt.Sprintf(clockRuleBase, minutes)
	}
	// 0 = unlimited: the tick and its decision survive, the budget clause is
	// DROPPED rather than rendered as a "0min" ceiling nobody has (0 = unlimited
	// keeps meaning unlimited — in the prompt too, Rule 1b/2).
	return clockRuleUnlimited
}

// clockRuleUnlimited is the tick line for a disabled budget: the same clock and
// the same t+300s decision, with no planning ceiling claimed.
// VAR, not const, and deliberately: the two clock lines embed
// tickDecisionLabel, which clock.go derives from tickDecisionAfter through the
// same tier function the live clock uses. A const expression cannot call a
// function, so the choice was this var or a second hand-written spelling of the
// threshold in the prompt — the exact drift Rule 1/13 forbids (measured: the
// pre-change text said "t+300s" beside a tick that now renders "[t+~5m]").
var clockRuleUnlimited = "A ticking clock runs from session start — " + tickShapeLabel + ". " +
	tickDecisionLabel + " = wrong tool: job(action=start) or cut scope."

// sessionBudgetCeilingClause is the SHORT form of the time dimension for use
// inside another sentence (the roadmap schema): the minutes come from the
// budget source, and 0 = unlimited says so instead of inventing a number.
func sessionBudgetCeilingClause() string {
	if minutes := sessionBudgetMinutesResolved(); minutes > 0 {
		return fmt.Sprintf("%d minutes", minutes)
	}
	return "unlimited (no ceiling on this run)"
}

// sessionBudgetTimeDimension states the budget's TIME dimension for the reframe
// tool description — the criterion that tool's reader applies to pick a verdict.
// Built from the budget source, never typed, so a GOPI_SESSION_BUDGET override
// changes what the tool description claims (Rule 1b/2/13).
func sessionBudgetTimeDimension() string {
	if minutes := sessionBudgetMinutesResolved(); minutes > 0 {
		return fmt.Sprintf("the session budget is a planning ceiling of %d minutes, so an epic whose roadmap implies "+
			"MORE THAN ONE session of work is %s BY DEFINITION, not %s — each roadmap item must be completable "+
			"inside ONE %d-minute session. Past the %d%% escalation mark a %s verdict is refused while tracked items "+
			"are still open.",
			minutes, taskverdict.TooBig, taskverdict.Doable, minutes, sessionBudgetWarnPercent, taskverdict.Doable)
	}
	// The UNLIMITED branch carries the SAME criterion as the limited one, in the
	// same words, because it is the same decision: a roadmap item is scoped to
	// ONE session either way, so an epic implying MORE THAN ONE session is
	// TOO_BIG BY DEFINITION rather than "harness-gated". Teaching only "more
	// than one sitting" here left the tool description silent on the criterion
	// whenever the ambient budget was 0 (the shape `gopi chat` publishes at arm
	// time) — measured red in TestReframeTimeDimensionTeachesTheCeiling.
	//
	// It deliberately does NOT carry the escalation-percentage sentence: a
	// percent is a share of a KNOWN budget, and sessionBudgetStatus() reports
	// ok=false when budget <= 0, so under 0 = unlimited no escalation mark
	// exists to teach (it also refuses DOABLE only from that status). Naming a
	// mark here would teach a gate this run does not have.
	return fmt.Sprintf("there is no planning ceiling on this run (budget 0 = unlimited), so judge the split on intrinsic "+
		"size alone — an epic whose roadmap implies MORE THAN ONE session of work is %s BY DEFINITION, not %s; each "+
		"roadmap item must still be completable inside ONE session budget and stand alone.",
		taskverdict.TooBig, taskverdict.Doable)
}

// sessionBudgetRule renders task rule 5. Past the escalation mark the rule is
// not merely informational — it names the one move left (split via
// reframe/TOO_BIG). Unlimited (0) states that plainly instead of inventing a
// ceiling. Format args and the mark come from the budget source (Rule 1b/2).
// This is the budget's RULE half. Its FACT half is clockRuleBase below: the
// tick line the model re-reads every round carries the clock and what the
// ceiling MEANS, this one carries what to DO past the mark. Stating the whole
// rule in both places was 126 characters of pure duplication — exactly the room
// engineering rule 8 needed — so the two now split one rule rather than
// restating it.
func sessionBudgetRule() string {
	minutes := sessionBudgetMinutesResolved()
	if minutes == 0 {
		return "BUDGET: unlimited — no planning ceiling; still split anything that cannot finish in one sitting."
	}
	return fmt.Sprintf("BUDGET: session budget %dmin; running out is a planning failure, not a graceful ending; past %d%% split it (task(action=reframe, verdict=TOO_BIG)).", minutes, sessionBudgetWarnPercent)
}

// clockRuleBase renders the tick line + the budget's meaning. %d = budget
// minutes; 0 = unlimited, which reads as "0min" — no invented denominator.
// It is the budget's FACT half; sessionBudgetRule above is its RULE half.
var clockRuleBase = "A ticking clock runs from session start — " + tickShapeLabel + ". " +
	"Session budget %dmin; cannot finish inside it = TOO BIG by definition. " +
	tickDecisionLabel + " = wrong tool: job(action=start) or cut scope."

// selfKnowledgeSectionName is the registry Name of the self-knowledge block
// (Rule 1: the name is a value a test and the registry must agree on, so it is
// declared once instead of typed at both sites).
const selfKnowledgeSectionName = "selfKnowledge"

// selfKnowledgeMarker is the section's bracketed heading — the one string that
// proves the block reached the ASSEMBLED prompt rather than merely existing as
// an unreferenced constant (the exact failure mode this section repairs: a
// capability that was built and invisible).
const selfKnowledgeMarker = "[SELF-KNOWLEDGE"

// selfKnowledgeRules is the SELF-KNOWLEDGE block: it tells the model that the
// repository it is reading IS gopi's own source, that the agent therefore IS
// gopi, and that invoking gopi's own CLI through the EXISTING bash tool is how
// it senses itself and its machine.
//
// WHY IT IS A PROMPT SECTION AND NOT A NEW TOOL (Rule 3: tools are minimal —
// 17, and claudeToolDefs is their SSOT): the senses already exist as built,
// dispatched top-level CLI verbs (`gopi ui_state` reads a live `gopi ui` screen
// as a cell grid with no pixels and no vision model; `gopi session log`/`raw`/`stats`
// read this agent's own session surface; `gopi task show <id>` reads its ledger
// row; `gopi tool capture`/`act` see and drive the live X11 desktop; `gopi tool sandbox`
// hands back the ONE configured private profile). What was MISSING was not
// perception but SELF-KNOWLEDGE: nothing in the agent's prompt said it was
// running inside gopi's own repo or that its own CLI was the way to observe
// itself, so the model reached for `ps`/`top`/`ls /tmp` instead — or did not
// look at all. Teaching the capability through the tool it already has is the
// whole fix; a new tool would have been a second way to do what bash does.
//
// RULE 1/13/14 — every verb is a NAMED CONSTANT, never a typed-in literal:
// the mesh is a Go CONSTANT EXPRESSION concatenating internal/cliverbs leaf
// constants (Go folds this at compile time), so the wiring is static, the
// rendered prompt is byte-identical on every turn, and the caching prefix
// cannot be disturbed by a runtime lookup. internal/cliverbs exists because
// cmd/agent/cli_verbs.go is `package main` and unimportable — the same reason
// cmd/agent's CliVerbMemory aliases tools.CliVerbMemoryGroup — and its own test
// pins each leaf constant to the cmd/agent declaration that dispatches it, so
// a verb rename cannot leave the prompt teaching a word the CLI rejects.
//
// Framework-level, not user-configurable.
//
// IT STAYS A CONST (measured 2026-10-06, task 01a10e03-9bc6-7427-b7e6-90a47f27f96f).
// The verb-retirement epic made its addresses two-word (`tool capture`,
// `session log`) and the first landing of that spelled them as JOINER FUNCTIONS
// (cliverbs.ToolAddress / SessionAddress) — which cannot appear in a constant
// expression, so this declaration was downgraded to a VAR to call them, silently
// giving up the property it is named for: the section's text is a COMPILE-TIME
// string, so the rendered prompt is byte-identical on every turn and the
// prompt-cache prefix cannot be disturbed by a lookup. The addresses are FIXED
// strings built from two constants each, so cliverbs exports them as constant
// expressions (ToolCaptureAddress, SessionAddresses, …) and this stays a const.
// TestSessionAddressesIsAConstantExpression pins both halves of that shape.
const selfKnowledgeRules = "\n[SELF-KNOWLEDGE — you ARE gopi] " +
	"You ARE gopi, in gopi's own source repo: bash calling gopi's own CLI is how you sense this machine. " +
	// HOW TO FIND WHAT A COMMAND DOES (2026-11-10, this epic). The section used
	// to open with cliverbs.UsageHint — "`gopi <verb> -h` prints usage" — and
	// close with a hand-listed "Yours: …" tail of five addresses. Both were the
	// same mistake in two places: an INCOMPLETE list plus a probing instruction,
	// so the model learned those five lines and discovered everything else by
	// asking `-h` one level at a time (and, for a verb retired since, by hitting
	// a refusal). The route taught now is the SOURCE, which is complete by
	// construction and cannot go stale: the dispatch is cmd/agent/cli_*.go, each
	// verb's contract is its own usage constant there, and the whole table is
	// internal/config/help.go's commandIndex — the same value `gopi -h` prints
	// and the same one the COMMAND INDEX block below renders.
	"To learn what a command does, READ THE SOURCE: cmd/agent/cli_*.go (dispatch + usage) and " +
	"internal/config/help.go's commandIndex (the ONE table `gopi -h` prints, below). `-h` per verb is the slow path. " +
	// THE ONE ADDRESS KEPT FROM "Yours:". The retired tail was a hand list of
	// five addresses; four of them (`task`, `tool capture`, `tool act`, `tool
	// sandbox`) are rows the derived COMMAND INDEX below now prints, so a copy
	// here would be a second place they exist. This one stays because it is not
	// a command to look up but the agent's OWN record of the run it is in — the
	// thing the section exists to teach it to consult — and no index row says
	// "this is your transcript".
	"Yours: " + cliverbs.SessionAddresses + " = this run's transcript. " +
	cliverbs.UIState + " reads a live screen — " + cliverbs.Chat + " (the console you answer in) or " + cliverbs.UI +
	" — as a CELL GRID carrying colour+format per cell-range (" + lineedit.DumpAttrRunLabel + "). " +
	"It is an EVIDENCE channel: it can DISAGREE with the transcript; " +
	cliverbs.UIState + " " + cliverbs.Compare + " \"<line>\" says which side lost it (" + cliverbs.SessionFlag + " defaults to this dump's)."

// PromptSection is one named block of the system prompt. Sections are
// assembled in ascending Order and joined with blank lines; empty Text drops
// the section (P0-2, dsh renderPrompt semantics —
// packages/core/system-prompt/src/index.ts). Ordering is stable for equal
// orders (sort.SliceStable).
type PromptSection struct {
	Name  string
	Order int
	Text  string
	// Render, when set, supplies the section's text AT ASSEMBLY TIME and takes
	// precedence over Text. It exists for the sections whose text renders a
	// runtime value — the session budget (P1/P2 of the session-budget rule) —
	// where a package-level Text would be evaluated once at process init and
	// would freeze whatever the env said then. Text stays the ordinary form:
	// most sections carry their literal, and one sentence + one counter-example
	// is what the density gate is written against.
	Render func() string
}

// promptSections is the ordered registry of system-prompt blocks. The header
// (identity + tool list) is order 0; the discipline blocks follow in the
// order they were previously concatenated. Framework-level, not
// user-configurable.
var promptSections = []PromptSection{
	{Name: headerSectionName, Order: 0, Text: headerRules},
	{Name: selfKnowledgeSectionName, Order: 5, Text: selfKnowledgeRules},
	{Name: "toolRouting", Order: 10, Text: toolRoutingRules},
	{Name: "efficiency", Order: 20, Text: efficiencyRules},
	{Name: "taskManagement", Order: 30, Render: taskManagementRules},
	{Name: "bashEfficiency", Order: 40, Text: bashEfficiencyRules},
	{Name: "job", Order: 50, Text: jobRules},
	{Name: "engineering", Order: 55, Text: engineeringRules},
	{Name: "conciseThinking", Order: 60, Text: conciseThinkingRules},
	{Name: "memLayout", Order: 62, Text: memLayoutHint},
	{Name: "tickClock", Order: 65, Render: clockRule},
	{Name: commandsSectionName, Order: commandsSectionOrder, Render: commandsSection},
}

// assembleSystemPrompt renders the system prompt from promptSections:
// sections sorted by Order (stable), empty sections dropped, joined with
// blank lines. Density target: <4.5K characters (dsh measured 4.1K).
//
// The operator's reply-language directive, when one is configured, is prepended
// to the whole assembly — see systemPromptWithOutputLanguage. It is deliberately
// NOT a registry entry: the registry shape is framework, and this knob is a
// per-machine preference the operator sets through three ordinary config
// channels (the ruled shape of briefings/brief-output-language-knob.md).
func assembleSystemPrompt() string {
	sorted := make([]PromptSection, len(promptSections))
	copy(sorted, promptSections)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	var parts []string
	for _, s := range sorted {
		text := renderPromptSection(s)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return systemPromptWithOutputLanguage(strings.Join(parts, promptSectionSeparator))
}

// systemPromptWithOutputLanguage puts the operator's reply-language directive,
// when one is configured, at the HEAD of a system prompt — and returns the
// prompt UNCHANGED when none is.
//
// The head placement is the operator's own instruction, and it is also where
// the directive does its work: the body that follows is English, and a rule
// that must survive an all-English turn has to be stated before that inertia,
// not after it.
//
// Silent-when-unset is the compatibility contract, and it is ONE function
// rather than an `if` at each call site precisely so the two prompt paths
// (task + chat) cannot disagree about it: a path that prepended nothing
// differently — a stray separator, a defaulted "reply in English" line — would
// change every prompt on every machine that configured nothing. The directive
// text comes from internal/config, which owns the resolution and the Rule 15
// reporting (Rule 2); the separator is the same two-newline join the registry
// uses, so a configured prompt differs from an unconfigured one ONLY by the
// directive text itself.
func systemPromptWithOutputLanguage(prompt string) string {
	directive := config.OutputLanguageDirective()
	if directive == "" {
		return prompt
	}
	return directive + promptSectionSeparator + prompt
}

// promptSectionSeparator is the blank line the prompt registry joins
// its
// sections with (assembleSystemPrompt). It is named here (Rule 1) because the
// output-language directive is prepended with the SAME separator: a second
// literal would be a second answer to "where does one block end".
const promptSectionSeparator = "\n\n"

// chatSystemPrompt is the minimal system prompt for the simplified chat path
// (Agent.Chat). Chat mode is Q&A, not a task run: the model needs no GoPi
// tool routing, no [EPIC GOAL] task-list framing, no efficiency/job/engineering
// rules.
const chatSystemPrompt = "You are GoPi in chat mode, a helpful conversational assistant. " +
	"Reply directly to the user's message in a natural, human-readable way. " +
	"Honesty over fluency: never claim to have seen, run or checked something you have not."

// chatSystemPromptServed is the system prompt the plain chat path actually
// SENDS: the chat composition (persona + chat block, see
// assembleChatSystemPrompt), with the output-language directive at its head
// when the operator configured one. It is a FUNCTION rather than a const so the
// runtime half (host · node · repo · this run's own transcript) is re-read per
// turn and so both prompt paths keep sharing systemPromptWithOutputLanguage.
//
// The AGENT is the argument because half the block is per-run state — THIS
// process's own transcript path — so both paths share one Render for the static
// half and one chatSessionBlock for it, not two that could disagree.
func chatSystemPromptServed(a *Agent) string {
	return assembleChatSystemPrompt(a)
}

// claudeToolDefs are the tools in Claude Messages API format
// (input_schema instead of parameters). The Claude interface is the only
// API path.
// bashJobTestEntryPointNote is the ONE sentence that carries this repo's
// DECLARED test entry point into the two tool descriptions that actually launch
// a test suite (Rule 14: a value spoken by several parties gets one spelling;
// Rule 13: the command itself is referenced from the package that defines it,
// never retyped here).
//
// WHY THE DESCRIPTIONS AND NOT A BOUND IN THE ENTRY POINT (measured
// 2026-10-01, devdoc/overlapping-test-runs-diagnosis.md, task
// 01a0f3f72a0173e4a44548a3a8accee2): a run issued two overlapping `go test`
// sets (~2 min of true concurrency, one package in both lists) and BOTH were
// BARE `go test`, so neither ever entered the declared entry point inside the
// Makefile) — the budget, the heavy/smoke skip ledger and the per-run
// stream/scope check were bypassed, and no concurrency bound existed anywhere to
// have been bypassed. A bound placed inside that script therefore cannot see
// this failure mode at all: the incident's commands never entered it. The
// descriptions are the tool's ONLY persuasion mechanism (PROJECT.md Rule 11), so
// that is where the entry point has to be named.
//
// It also answers the incident's second half: a bare `go test` buffers
// per package, so a foreground call prints nothing for minutes and is killed by
// the idle-output net — the entry point writes a per-run stream from its start.
var bashJobTestEntryPointNote = " Run the repo's declared test entry point — " +
	smokegate.Command + " (whole tree) or " + smokegate.ScopedCommand +
	" (the packages your diff touched), both of which route through the declared per-package budget, " +
	"skip the heavy set and write a per-run stream — rather than a bare `go test`: a bare run bypasses " +
	"all three and BUFFERS PER PACKAGE, so a foreground call prints nothing for minutes and is killed " +
	"by the idle-output net. Two `go test` sets in one run is the observed failure: overlap the scope, " +
	"not the run (" + smokegate.Doc + ")."

// bashJobToolDef returns the registered tool definition whose name matches,
// with the shared test-entry-point note folded into its description — one
// mechanism, so the description and the gate can never disagree about which
// tools carry it (the constant above is their single source).
func bashJobToolDef(name string) (deepseek.ClaudeToolDef, bool) {
	for _, td := range claudeToolDefs {
		if td.Name == name {
			td.Description += bashJobTestEntryPointNote
			return td, true
		}
	}
	return deepseek.ClaudeToolDef{}, false
}

var claudeToolDefs = []deepseek.ClaudeToolDef{
	{
		Name:        toolnames.ToolRead,
		Description: "Read a file's contents as text. Use read — not shell cat/head/sed — to see any file before analyzing or editing it. Every page is capped at ~50KB; a truncated page ends with a footer naming the total line count and the next offset. Large file? Page with offset/limit — a full read burns tokens. NOTE offset semantics: offset=N skips the first N lines (continue after line K with offset=K) — NOT the 'start at line N' convention. If the path is a symlink, the read result notes the link target.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to read. Supports ~/ expansion.",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of lines to skip from the beginning (1-indexed: offset=N skips the first N lines — continue after line K with offset=K). Default 0 reads from the start.",
					"default":     0,
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of lines to read (page still capped at ~50KB). Default 0 reads the entire file.",
					"default":     0,
				},
			},
			"required": []string{"path"},
		},
	},
	{
		Name:        toolnames.ToolGitBlame,
		Description: "Call git_blame for ANY git line-history question — who wrote line N, which commit introduced a bug, WHY the code is this way, and whether your change should DEFER to it. ALWAYS use git_blame over `bash git blame`: it returns structured JSON (no raw-git token waste), guards wrong line numbers / oversized windows, and attributes each line to the gopi TASK that produced it. NEVER run `git blame` via bash when git_blame answers the question. Returns {file_path, task_enrichment (none|partial|full), task_enrichment_note, ledger_access, line_items:[{line_no, commit_hash, author, timestamp, commit_message, code_line, task_id, task_summary, task_status, task_host}]}. In a gopi repo the author field is USELESS (every run commits as gopi <gopi@gopi>) — the information is task_summary/task_status: MEASURED example, git_blame(internal/tools/clone_ground_branch_create_guard_test.go, 1, 6) returns commit 11d5513d with task_id 01a195bb-4287-7ad5-a764-b6df563895dc, task_status completed and task_summary 「run 01a1957a 在自己的 clone 之外还进入了共享主目录 /home/user/work/gopi 并建 task/<id> 分支写入半成品，导致所有人 pull 被挡、make build 报 undefined: scratchGCConsentGiven（/usr/local/bin/gopi 停在旧版）」 — i.e. that work exists to close a real escape path, so a change that re-opens it should DEFER, while a change that serves the same intent may proceed. The tool only REPORTS; the judgement is yours. task_enrichment=none with a note saying no task trailer means the repo was never touched by a gopi run — real information, not a tool failure; it also cannot happen at all outside a gopi repo. file_path is repo-root-relative (e.g. internal/memory/store.go). Omit start_line/end_line for a small default window (50 lines); ranges wider than 200 lines are refused. Only works on files tracked by git. Optional tool: no git in the environment → structured 'unavailable' error, everything else keeps working.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Repo-root-relative path, e.g. internal/memory/store.go",
				},
				"start_line": map[string]interface{}{
					"type":        "integer",
					"description": "First line number (1-based). Omitted = a small default window starting at line 1.",
				},
				"end_line": map[string]interface{}{
					"type":        "integer",
					"description": "Last line number; must be >= start_line. Clipped to the file's line count.",
				},
			},
			"required": []string{"file_path"},
		},
	},
	{
		Name:        toolnames.ToolWrite,
		Description: "Create or fully REPLACE a file's content — existing content is overwritten entirely (no merge). Use for new files and whole-file rewrites. For a targeted change use edit: it sends only the changed lines, so it is cheaper and leaves the rest untouched. Paths support ~/ expansion. If the file exists, its old content is gone after this call. Paths that are symlinks are refused by default to protect the link; pass follow_symlink=true to write through to the target.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to write.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write to the file.",
				},
				"follow_symlink": map[string]interface{}{
					"type":        "boolean",
					"description": "When path is a symlink: false (default) refuses the write (renaming onto a symlink replaces the link node and leaves the target unchanged); true resolves the link and writes the target file in place, keeping the symlink intact.",
					"default":     false,
				},
			},
			"required": []string{"path", "content"},
		},
	},
	{
		Name:        toolnames.ToolEdit,
		Description: "Surgically replace one unique string in a file: finds oldStr and swaps it for newStr. Use edit — NOT write — for targeted changes: only the affected lines are sent (cheaper) and everything else stays intact (safer). oldStr must be unique unless replace_all=true; a non-unique or missing match is refused with the mismatch shown — re-read the file and copy the exact text. Paths that are symlinks are refused by default to protect the link; pass follow_symlink=true to edit through to the target.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to edit.",
				},
				"oldStr": map[string]interface{}{
					"type":        "string",
					"description": "Exact string to find and replace. Must be unique in the file unless replace_all=true.",
				},
				"newStr": map[string]interface{}{
					"type":        "string",
					"description": "Replacement string.",
				},
				"replace_all": map[string]interface{}{
					"type":        "boolean",
					"description": "Replace ALL occurrences of oldStr. Default false: oldStr must be unique and a non-unique match is refused.",
					"default":     false,
				},
				"follow_symlink": map[string]interface{}{
					"type":        "boolean",
					"description": "When path is a symlink: false (default) refuses the edit (renaming onto a symlink replaces the link node and leaves the target unchanged); true resolves the link and edits the target file in place, keeping the symlink intact.",
					"default":     false,
				},
			},
			"required": []string{"path", "oldStr", "newStr"},
		},
	},
	{
		Name: toolnames.ToolBash,
		Description: "Execute a shell command via bash -c for build, test, run, git and file operations — NOT for searching code (definitions → code_search, full-text → keyword_search) and NOT for reading files (use read, not cat/head/sed). NEVER invoke grep/rg for a symbol definition — ALWAYS use code_search (one structured call vs a grep round-trip). Reading a file (cat/head/sed -n FILE) when read exists = a logged violation. Returns 'exit=N\\n' + stdout, stderr prefixed '[stderr] '. 120s timeout by default; set timeout_sec only for slightly-long commands. Command may outlive a call? Use job(action=start) — bash kills its WHOLE process group on timeout. Dangerous cmds (rm -rf etc.) blocked unless GOPI_ALLOW_DANGEROUS=1." +
			bashJobTestEntryPointNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute.",
				},
				// timeout_sec default (120) mirrors bashDefaultTimeoutSec in
				// internal/tools/bash.go — informational for the LLM; the real
				// default is enforced in code (BashWithTimeout).
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"description": "Max seconds to wait. Default 120. 0 = unlimited absolute time (still subject to the 120s no-output safety net).",
					"default":     120,
				},
				// workdir mirrors bash.go's BashInDir: validated (must be an
				// existing directory) before the command starts.
				"workdir": map[string]interface{}{
					"type":        "string",
					"description": "Working directory for the command (must exist). Omitted = current directory.",
				},
			},
			"required": []string{"command"},
		},
	},
	{
		Name:        toolnames.ToolCodeSearch,
		Description: "Locate where a symbol (function, struct, interface, var, const, type, class, macro) is DEFINED. ALWAYS use this for a definition lookup — NEVER run `bash grep <symbol>` for it. ONE call returns every definition as structured hits (file:line, kind, signature) from a pre-indexed ctags database (Go, Python, shell, C/C++ incl. .h headers, Rust, JavaScript, TypeScript), auto-falling back to ripgrep. When reviewing or reading unfamiliar code, resolve EVERY unknown identifier here FIRST — before grep, before guessing from includes. Faster and cleaner than bash grep: one structured call vs a 2-call grep round-trip of noisy lines you must re-read+filter. Takes a symbol NAME (an identifier like 'NewClient') — never a sentence, description or tool name. 'No symbol found' means the identifier exists nowhere in the codebase: do not grep for it; use keyword_search for related text or files outside the indexed languages.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"symbol": map[string]interface{}{
					"type":        "string",
					"description": "Symbol name to look up (e.g. 'NewClient', 'Thing').",
				},
			},
			"required": []string{"symbol"},
		},
	},
	{
		Name: toolnames.ToolKeywordSearch,
		Description: "Full-text search with TWO SCOPES, and the SCOPE decides what you are searching: (1) scope=\"tree\" (the default) = the project tree on disk; (2) scope=\"session\" = THIS CONVERSATION'S OWN on-disk record — the full-length log of every row this run wrote, including the rounds that COMPACTION REMOVED. " +
			"CHOOSE scope=\"session\" THE MOMENT THE THING YOU WANT CAME OUT OF THIS CONVERSATION RATHER THAN OUT OF THE REPO — the recall triggers are literal: the user asks 「你还记得…」「我们之前讨论过…」「上次那个…」「之前不是说好的…」 / \"do you remember …\", \"we discussed this before\"; or YOU need a detail from earlier in this run that is no longer in your context — a command's complete output, an exact error string, a task id, a file path you saw 500 rounds ago, the number a tool printed before a compaction. Searching the TREE for a sentence that only ever existed in the conversation is the wrong scope and returns nothing: the conversation is NOT in the repo. " +
			"Session hits come back STRUCTURED — round number, row type, timestamp, a bounded snippet and the `gopi session log <id> <round>` command that opens the whole round — never a raw JSON line. Do NOT search [IMPORTANT] notes with it: those are already in your context. `raw.log` (the same history re-sent, ~58x the bytes) is EXCLUDED unless include_raw_log=true, and its hits are labelled with their source. " +
			"scope=\"tree\" is the ordinary meaning: code, docs, comments, config, scripts; Query may be a plain string or a regex; hits are (file:line, snippet). Use it for USAGES/references of an identifier (where is X called?), error messages, comments, docs, config — and as the follow-up when code_search reports a symbol is not found. For locating a symbol DEFINITION use code_search, not grep. Too-broad results? Narrow the query — noisy output wastes a round.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Text or regex to search for.",
				},
				"scope": map[string]interface{}{
					"type":        "string",
					"description": "Where to search — THIS ARGUMENT DECIDES WHAT YOU GET, so read it as a question: did the thing you want come from THIS CONVERSATION or from the REPO? \"session\" = this conversation's own on-disk record (session.jsonl), rendered as round/type/timestamp/snippet/locator hits — use it for 「你还记得…」「之前不是说好的…」, \"we discussed this before\", and for any detail from earlier in this run that compaction removed (a command's full output, an exact error string, a task id, a path). \"tree\" (or omit / \"\") = the project tree on disk — the ordinary meaning, and the WRONG scope for a sentence that only ever existed in the conversation, because the conversation is not in the repo.",
					"enum":        []string{"tree", tools.SessionSearchScopeSession},
				},
				"session": map[string]interface{}{
					"type":        "string",
					"description": "scope=\"session\" only: which session. Empty = the CURRENT session (the normal case). A session id or an absolute path searches another/older session.",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "scope=\"session\" only: how many hits to return. Default 5, max 50; anything dropped is reported, never silent.",
				},
				"include_raw_log": map[string]interface{}{
					"type":        "boolean",
					"description": "scope=\"session\" only: also search the raw HTTP log (the same history re-sent on every turn, ~58x the bytes). Off by default; hits from it are labelled source=raw.log and carry a line number instead of a round.",
				},
			},
			"required": []string{"query"},
		},
	},
	{
		Name:        toolnames.ToolWebSearch,
		Description: "Search the web or ask an AI; returns unified JSON ({status,count,results}). Methods: bing (CN direct, fastest), google (overseas/EN), arxiv (papers), deepseek (AI answer), github (repos/issues; code needs a token), wiki (Wikipedia). No method arg = bing+google+arxiv merged (dedup + relevance sort + source attribution). count caps results (default 5, max 20); ignored for the AI-ask methods. Snippets only — follow up with web_fetch for the full page. Use web_search — NOT bash curl of a search engine page or shelling out to the plugin scripts.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search phrase or question.",
				},
				"method": map[string]interface{}{
					"type":        "string",
					"description": "bing|google|arxiv|deepseek|github|wiki. Empty = merged multi-method default.",
					"enum":        []string{"bing", "google", "arxiv", "deepseek", "github", "wiki"},
				},
				"count": map[string]interface{}{
					"type":        "integer",
					"description": "Max results to return (default 5, max 20).",
					"default":     5,
				},
			},
			"required": []string{"query"},
		},
	},
	{
		Name:        toolnames.ToolWebFetch,
		Description: "Fetch a URL and return its cleaned full text as JSON ({status,url,chars,truncated,content}). Search ≠ Read: web_search returns snippets — use web_fetch to actually read a page (deep research, verifying a source, reading a README/abstract in full). Takes ONE http(s) URL (e.g. a web_search result URL); boilerplate HTML (nav/header/footer/scripts) is stripped and the text is tail-truncated to max_chars (default 8000) to keep tokens low. Use web_fetch — NOT bash curl of a webpage. Workflow: web_search to discover URLs → web_fetch to read the promising ones.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "http(s) URL to fetch (e.g. a web_search result URL, GitHub README, arxiv abstract page).",
				},
				"max_chars": map[string]interface{}{
					"type":        "integer",
					"description": "Max chars of cleaned text to return (default 8000).",
					"default":     8000,
				},
			},
			"required": []string{"url"},
		},
	},
	{
		Name: toolnames.ToolMemory,
		Description: "One tool for gopi's knowledge base (hippocampus) — the memory family merged memory search + store + update + feedback (the former memory_search/memory_store/memory_update/memory_feedback tools) into ONE tool with an action parameter. Pick exactly ONE action (the only required arg):\n" +
			"- action=" + toolnames.ToolMemoryActionSearch + " — " + memorySearchBullet + "\n" +
			"- action=" + toolnames.ToolMemoryActionStore + " — may run serial only. Save a DISTILLED lesson into the knowledge base so future tasks can find it. USE after a task with a reusable lesson: a success path that worked, a pitfall, a pattern worth repeating. USE MID-TASK too — store is EVENT-DRIVEN, not end-of-task-driven: " + eventDrivenStoreTriggers + ". Store only DISTILLED experience — repeatable pattern / reproducible loop / measurable value. NEVER store raw conversation transcripts or step-by-step logs (流水账). Structure: topic (the subject), question (what a future agent might search for), answer (how to succeed / what NOT to do). YOUR JOB = leave retrieval breadcrumbs (检索面包屑): 2-3 mixed 中英 keywords per 5W1H dimension (WHAT 主题 / WHY 问题 / WHEN 场景 / WHERE 环境 / WHO 实体 / HOW 方法), with synonyms / translations / variant phrasings / Unicode normalization (agent‑loop → agent-loop) — these ARE the breadcrumbs action=search will match; think one step further when storing. Returns the stored record id; deduped means it merged into an existing entry.\n" +
			"- action=" + toolnames.ToolMemoryActionFeedback + " — may run serial only. Vote on a search hit RIGHT NOW — 投票=质量裁判, judge each result the moment it arrives (搜索结果的当下就投), never defer to task end. Record an explicit useful/useless vote on a search hit so the knowledge base evolves (LLM 是唯一用户 = 唯一质量裁判 — you are already judging each hit while reading it; this tool just lands that judgment). useful = you actually used it. useless ONLY when the hit CONTRADICTS your operation results / is provably stale or wrong (bug-report culture: a merely-meh hit still gets useful or no vote). Then memory(action=update) to correct the wrong memory — feedback says WHAT is wrong, update does the FIX (右转: call memory(action=update) with id + the corrected field RIGHT NOW, not later). One vote per session per hit moves the hit's stored confidence 0.05 (a useless vote -0.10) and re-ranks search; NO tier is ever auto-demoted — the machine only SUGGESTS (3 decayed useless votes and zero useful ⇒ --suggest-cold lists a candidate), and setting tier=cold is yours via action=update. When rating, id=#N is REQUIRED (the hit line's record number — not a call id; the engine reads this one alone). A search pair survives slimming iff one of its records was voted useful by #N; every other pair is dropped.\n" +
			"- action=" + toolnames.ToolMemoryActionUpdate + " — may run serial only. Directly correct or re-tier an existing memory record (hard edit — the soft signal is feedback). USE at the CONTRADICTION moment: a search hit contradicted by your VERIFIED evidence (operation output), or a record you know is stale (API / behavior changed) — fix it NOW, don't let the wrong record sit (误导=给未来的自己挖坑). Prefer action=feedback when unsure — feedback is reversible and cheap; update is for certainty. A corrected record stays hot/warm; a wrong record left warm poisons every later task. Do NOT use update to delete history — the knowledge base keeps versions for audit. Input: id (the #N) + the CORRECTED field(s): rewrite the wrong field's value (topic/question/answer/tier/keywords/confidence), not a patch-note stuffed into keywords.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{toolnames.ToolMemoryActionSearch, toolnames.ToolMemoryActionStore, toolnames.ToolMemoryActionUpdate, toolnames.ToolMemoryActionFeedback},
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "action=search only. What you want to know: a task, problem or situation you suspect happened before. Searched against BOTH the distilled knowledge base and the round-note index of past runs (see the [distilled] / [session] sections in the result).",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "action=search only. Max records to return (default 5).",
					"default":     5,
				},
				"intent": map[string]interface{}{
					"type":        "string",
					"description": "action=search only. Optional routing hint for the backend (e.g. 'debug', 'build', 'search').",
				},
				"topic": map[string]interface{}{
					"type":        "string",
					"description": "action=store/update: subject of the lesson / new subject.",
				},
				"question": map[string]interface{}{
					"type":        "string",
					"description": "action=store/update: a question a future agent might ask when it needs this lesson / new question.",
				},
				"answer": map[string]interface{}{
					"type":        "string",
					"description": "action=store/update: the distilled experience (how to succeed / what NOT to do) / new distilled body.",
				},
				"source": map[string]interface{}{
					"type":        "string",
					"description": "action=store/update: optional provenance (repo, file, task) / new provenance.",
				},
				"keywords": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "5W1H 检索面包屑：中英混合关键词（主题/问题/场景/环境/实体/方法六维度），含同义/翻译/变体问法/Unicode 归一化. action=store: set them; action=update: replace (rewrites search_kw AND the answer's keywords tail).",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"declarative", "procedural"},
					"description": "action=store only. Row type: declarative (default) = distilled lesson QA; procedural = reusable workflow stored in the procedural table. With type=procedural, pass name/trigger/steps (and optionally preconditions/tools_used) — topic/question/answer are ignored except as fallbacks (topic→name, question→trigger, answer→single step).",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "action=store, type=procedural only: workflow name (unique identity, e.g. '读错误日志→定位→修复→验证').",
				},
				"trigger": map[string]interface{}{
					"type":        "string",
					"description": "action=store, type=procedural only: 何时启用 — the condition/situation that should activate this workflow.",
				},
				"preconditions": map[string]interface{}{
					"type":        "string",
					"description": "action=store, type=procedural only: preconditions that must hold before running the workflow.",
				},
				"steps": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "action=store, type=procedural only: ordered step list (each a command or instruction). Structured JSON is persisted; prefer 3-8 actionable steps.",
				},
				"tools_used": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "action=store, type=procedural only: tools the workflow exercises (e.g. bash, code_search).",
				},
				"id": map[string]interface{}{
					"type":        "integer",
					"description": "action=feedback: REQUIRED — the record number #N printed on the hit line of a search result you retrieved. action=update: REQUIRED — same id. This is the only id the engine reads (never a call id / tool_use id).",
				},
				"rating": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"useful", "useless", "unused"},
					"description": "action=feedback: REQUIRED. useful = adopted/helped; useless = contradicted/failed/misleading; unused = seen but not used (neutral).",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "action=feedback only. Optional short reason (recommended for useless votes).",
				},
				// NOTE: the retired call-id parameter is deliberately absent here.
				// It named the originating search pair for a vote, but the feedback
				// path never read it and that mechanism was retired with WHITELIST
				// slimming — see the sync gate in memory_feedback_contract_test.go.
				"tier": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"hot", "warm", "cold"},
					"description": "action=update only. cold = demote (no longer 1st-tier to return). Prefer demotion over content edits for low-quality records.",
				},
				"confidence": map[string]interface{}{
					"type":        "number",
					"description": "action=update only. New confidence in [0,1].",
				},
			},
			"required": []string{"action"},
		},
	},
	{
		Name:        toolnames.ToolContextTrim,
		Description: "TRIM DEAD-ENDS NOW — 干净仪表盘: the moment a tool result is a dead end, trim it immediately. A dead end is an errored/failed/no-value output — bash command on a missing file or wrong args, a read of a nonexistent path, a tool result with no useful signal. Every untrimmed dead end is re-sent to the model every round — trimming is how you stop paying for it. ALWAYS trim before moving on; NEVER leave an untrimmed dead-end result in context. Call with the dead end call's id; optionally pass replace_with to leave a one-line conclusion (e.g. 'X failed because Y — don't retry'): with replace_with the pair is KEPT, the bulky result swapped for your conclusion; without it the pair is removed entirely. The id is the call_… string stamped in that tool result's header ('tool_use id: …') — copy it verbatim, NOT the record number #N. Each id is trimmed at most once; unknown ids no-op silently.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"call_id": map[string]interface{}{
					"type":        "string",
					"description": "The tool_use id of the dead-end call to trim (the call_… string stamped in that tool result's header — copy it verbatim).",
				},
				"replace_with": map[string]interface{}{
					"type":        "string",
					"description": "Optional one-line conclusion that REPLACES the original tool result (keeps the pair, drops the bulky output — 'X failed because Y — don't retry'). Omit to delete the pair entirely.",
				},
			},
			"required": []string{"call_id"},
		},
	},
	{
		Name: toolnames.ToolTaskSubmit,
		Description: "Submit a work item to the persistent task queue (status=pending) instead of doing it in this session. USE WHEN: 1) you discover a bug you cannot fix without derailing the current task — submit it with tags=['bug'] and payload=reproduction steps + log paths, then CONTINUE your current work (the queue/another instance picks it up); " +
			"2) the current task is too large — split it: submit the subtasks ONE AFTER ANOTHER, IN THE ORDER THEY MUST RUN. The harness links each submission to the one submitted just before it (a SERIAL CHAIN: item i's parent = item i-1), so the dispatcher releases them one at a time and the ORDER YOU SUBMIT IN IS THE DEPENDENCY — a model cannot express a dependency graph, so its order is the only signal there is, and the harness makes it real. Do not count on parallelism, and do not send several submissions expecting them to run at once. " +
			taskSubmitIndependentToolClause() +
			"Each subtask needs a self-contained payload briefing. " +
			"3) work that only makes sense asynchronously/for others. " +
			"Repo closure: work targeting ANOTHER repo (a foreign project) must carry that repo — the repo parameter stamps deliverable.repo and a runner picking the task up without one only gets a cross-project warning at claim time (verify-before-execute is on you). " +
			"Every bug-tagged submit prints a running [bug-flow] tally (bug tasks total / verified / reopened) — the adoption loop's measurable heartbeat. " +
			"ROUTE FIRST: trivially-doable work should just be done — task_submit is the 'separate work item' exit (bug report / well-scoped subtask), never a way to dodge work you can do. " +
			"Anti-self-reference: this session never executes its own submissions (claims exclude the submitting session) — expect the task_id in the result and reference it via bash `gopi task show <task_id>`.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "One-line ledger headline (what a human scanning `gopi task list` should read).",
				},
				"payload": map[string]interface{}{
					"type":        "string",
					"description": "FULL request text — the briefing the executing gopi will run with as its only input. Write it self-contained: context, goal, constraints, acceptance. Omit only when the summary says it all. A value beginning with '@' followed by an EXISTING file path (`@/abs/briefing.md`) is READ, exactly as `gopi task submit --payload @FILE` reads it — the engine expands the reference through the same reader, stores the file's CONTENT, and reports the byte count; a reference naming an unreadable file is REFUSED (never stored as the '@path' spelling). Escape a payload that genuinely starts with '@' as '@@'. An oversized payload (>16 KiB) is NOT truncated: it is preserved verbatim as a task attachment and the row keeps a one-line pointer to it, so keep the payload concise and ship bulk evidence (logs, diffs, file dumps) as attachments instead.",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional classification tags; 'bug' is the reserved bug-flow tag (payload = repro steps + log paths).",
				},
				"parent_task_id": map[string]interface{}{
					"type":        "string",
					"description": taskSubmitParentParamDescription(),
				},
				"repo": map[string]interface{}{
					"type":        "string",
					"description": "Optional git repo path/URL the work happens in. DEFAULT: your current working directory — the normal case, do NOT invent a repo path. The path is verified (git rev-parse) BEFORE the task row is written: a non-git directory aborts the whole submit with an error. Verified repo lands in the task's deliverable block (deliverable.repo) so verify/merge run with zero memory.",
				},
				"priority": map[string]interface{}{
					"type":        "integer",
					"description": "Optional queue weight, lower runs first (M4.11). Omit to derive from tags: bug→1 (highest — LLM-reported bugs are bugs), docs/doc-only→8, else 5. An explicit value ALWAYS overrides the tags derivation.",
				},
				"attachments": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional ABSOLUTE paths of files to archive for the task (epic-task-reframe Phase 1): each is copied at submit time into the task's session-dir attachments area under its original path structure (/tmp/log.txt → .../attachments/tmp/log.txt — same-name files in different dirs never collide). The executing instance sees each replica's path as an [attachmentN:] entry in its EPIC briefing and reads it with the normal read/file tools. Files only — directories and missing paths abort the submit. Use for evidence the task needs (screenshots, logs, docs) that is too big to inline in the payload.",
				},
			},
			"required": []string{"summary"},
		},
	},
	{
		Name: toolnames.ToolJob,
		Description: "Manage a background shell job — the action parameter selects the operation (start | output | kill). action=start runs a command in the BACKGROUND, detached in its own session — immune to bash timeouts (bash kills its whole process group on timeout, which would take a shared background process down with it). Returns a job id + stdout/stderr file paths. Use for long-running commands (builds, tests, servers, scripts). Do NOT poll: continue other work — the harness injects a notice when the job finishes; collect the output once with action=output. action=output fetches a background job's status and output: status=running | exited code=N | killed, plus the captured output (stderr included). Call ONCE — never busy-poll or sleep-wait; work on other things while the job runs (you are notified on completion). Every fetch re-sends the full output: read once, don't re-fetch. action=kill kills a background job's WHOLE process group, child processes included — use when the job is no longer needed or must be stopped; prefer killing over leaving zombie jobs holding resources." +
			bashJobTestEntryPointNote,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{toolnames.ToolJobActionStart, toolnames.ToolJobActionOutput, toolnames.ToolJobActionKill},
					"description": "Which job operation to run: start (run a command in the background), output (fetch a job's status/output), kill (terminate a job).",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to run in the background (required for action=start).",
				},
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "Job id returned by action=start (required for action=output and action=kill).",
				},
			},
			"required": []string{"action"},
		},
	},
	{
		Name: toolnames.ToolTask,
		Description: "One tool for the harness-tracked task list and the in-session working area (task family merged todo + write_task_area). Pick exactly ONE action (the only required arg):\n" +
			"- action=" + toolnames.ToolTaskView + " — READ-ONLY task-list view, parallel-safe. Show the current task-list state as tracked by the harness. You update tasks by emitting a [TASK_LIST_BEGIN]...[TASK_LIST_END] block in your response (full array each time, never a diff; an unclosed block auto-recovers with a warning). Shape: [{\"task_id\":\"1\",\"summary\":\"Read source\",\"status\":\"pending\"}]. Statuses: pending (not started) → progressing (exactly ONE at a time) → completed (verified: project's own checks green); a decomposed parent stays progressing until every child completes; never drop a task_id (obsolete → completed with summary \"(dropped: why)\"). Check before reporting progress, choosing the next step, and at status checkpoints.\n" +
			"- action=" + toolnames.ToolTaskCommit + " — SESSION-internal, may run serial only. Commit the CURRENT subtask/item's stable conclusion into the session working area: task_id (which item), status=\"completed\" only, conclusion REQUIRED (compact final result/decision/key facts — what a later step must know), compressed_analysis OPTIONAL (only when a later item must reuse the reasoning). CALL ONCE PER ITEM at convergence — never batch, never free-text 'all done'. Afterwards the item's working region is COMMITTED (append-only, queryable; may be trimmed next round; conclusion carried forward as authoritative context). Does NOT touch the cross-session ledger (task_submit is separate).\n" +
			"- action=" + toolnames.ToolTaskReframe + " — evaluate the epic AFTER investigating it (read the briefing + any [attachmentN:] files FIRST, never before) and return BOTH the reframe (epic_summary/constraints/deliverables/acceptance) and a verdict ∈ {" + toolnamesVerdictEnum() + "}. THE BUDGET IS PART OF THE CRITERION: " + sessionBudgetTimeDimension() + " The conditional payload is HARNESS-ENFORCED, so every exit carries NEW work: " + taskverdict.TooBig + " requires roadmap[{goal,acceptance}] (the harness auto-enqueues ONE child task per item and this run ends there — your deliverable is the diligence + the decomposition), " + taskverdict.Impossible + " requires reason. " + taskverdict.Impossible + " means PROVABLY unattainable in principle — merely hard, risky, or unknown is " + taskverdict.TooBig + " with the investigation as a roadmap item (there is NO research verdict: research is part of reframing). \"'Too big' without a concrete roadmap is REJECTED — that is what stops the queue bouncing the same summary. An honest " + taskverdict.Impossible + " beats a fabricated result.\n" +
			"- action=" + toolnames.ToolTaskReview + " — SESSION-internal, may run serial only. RUN IMMEDIATELY AFTER action=commit of each subtask (the post-commit checkpoint; when the committed item is the LAST one this doubles as the whole-epic closing review). Review the WHOLE problem against the ORIGINAL goal (the epic/spec at the top of context) and the committed-conclusion chain. Arguments: verdict = ONE code (A = ON-TRACK, continue; B = RE-ALIGN — goal re-scoped, re-emit the roadmap; C = AMEND-ITEM <task_id> — commit a CORRECTION; D = REDO-ITEM <task_id> — supersede + redo; E = PATH-DEAD — record finding, re-plan), plus optional task_id (C/D) and reason. Every exit verdict is honorable and no-punishment (misdirection happens; recording beats patching).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{toolnames.ToolTaskView, toolnames.ToolTaskCommit, toolnames.ToolTaskReview, toolnames.ToolTaskReframe},
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Which subtask/item id (commit + review C/D). E.g. \"1\", \"2\" … matching your task list.",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Fixed value \"completed\" (only status accepted; commit).",
				},
				"verdict": map[string]interface{}{
					"type":        "string",
					"description": "action=review: the review code A|B|C|D|E. action=reframe: ONE of " + toolnamesVerdictEnum() + " — never empty.",
				},
				"epic_summary": map[string]interface{}{
					"type":        "string",
					"description": "REQUIRED (reframe): the epic restated in one compact paragraph — the authoritative goal every later review anchors on.",
				},
				"constraints": map[string]interface{}{
					"type":        "string",
					"description": "REQUIRED (reframe): the binding constraints (project rules, environment limits, acceptance gates) you must work within.",
				},
				"deliverables": map[string]interface{}{
					"type":        "string",
					"description": "REQUIRED (reframe): the concrete artifacts the epic must produce.",
				},
				"acceptance": map[string]interface{}{
					"type":        "string",
					"description": "REQUIRED (reframe): how completion is verified (the checks you will actually run).",
				},
				"roadmap": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"goal":       map[string]interface{}{"type": "string", "description": "What this child task accomplishes."},
							"acceptance": map[string]interface{}{"type": "string", "description": "How that child is verified — each item must stand alone as a self-contained briefing."},
						},
					},
					"description": "REQUIRED (reframe) when verdict=" + taskverdict.TooBig + ": the decomposition, one entry per child task. Non-empty, each with goal+acceptance; the harness enqueues each entry as its own child task. Each item must be completable inside ONE session budget (" + sessionBudgetCeilingClause() + "). An empty or goal-less roadmap is rejected. A genuine unknown belongs here as a research ITEM (read/compare/write the conclusion doc) — not as a verdict.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Short reason for a review verdict (any non-A verdict). REQUIRED for action=reframe with verdict=" + taskverdict.Impossible + " — the provable blocker, recorded as the task row's failure reason.",
				},
				"conclusion": map[string]interface{}{
					"type":        "string",
					"description": "REQUIRED (commit only): compact final result / decision / key facts — what a later step must know without re-reading this item's analysis.",
				},
				"compressed_analysis": map[string]interface{}{
					"type":        "string",
					"description": "Optional (commit): omit = the analysis is DISCARDED after commit; include only when a later item must reuse the reasoning.",
				},
			},
			"required": []string{"action"},
		},
	},

	{
		Name:        toolnames.ToolVisionAnalyze,
		Description: "Understand an image via the vision model (deepseek-v4-flash-vision-exp — 图片理解/OCR/图表分析). When to use: 图片理解 / 截图文字识别 / 图表分析 — 截图文字识别直接调 vision，不需要 OCR 脚本 (replaces external OCR). For RENDER verification (font/glyph quality) use pixel-level comparison (IoU), not vision — a vision model can't quantify quality loss. Input: image_path (local file, required) + prompt (optional, default 描述这张图片并识别文字). The image is read, base64-encoded and sent to the vision model in one lightweight standalone request — 图片不进主上下文，只有分析结果文本返回 (context protection). Supports jpg/jpeg/png/gif/webp, max 32 MiB. Images are only valid in user messages — this tool always sends them as a user message.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the local image file to analyze (required). Supported formats: jpg/jpeg, png, gif, webp; max 32 MiB.",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "Optional instruction for the vision model (e.g. '识别图片中的所有中文文字', '描述图表数据'). Default: 描述这张图片，并识别其中的文字（如有）.",
				},
			},
			"required": []string{"image_path"},
		},
	},
	{
		Name:        toolnames.ToolText2img,
		Description: "Render a text file to PNG images (纯 GO 零依赖 TrueType 渲染 — text→PNG for screenshots / reports / OCR-friendly images; CJK via autodetected system font). When to use: 把文本变成图片 / 生成截图素材 / 渲染含中文的图文 — internal/text2img 自研 SFNT/TrueType 解析 + 4x 超采样栅格化, 无 CGO 无 x/image. Verify render quality with PIXEL-level comparison (IoU), not OCR — OCR can't see quality loss. Input: src (text file path, required) + out (PNG path, optional — default lands in the gopi-text2img scratch class tree, NEVER beside src) + optional size/cols/lh/lines/margin/font overrides (defaults: 20px 字号, 50 列折行, 32 行高, 90 行/页, 12 边距). Text longer than one page auto-paginates: an explicit out becomes out-1.png, out-2.png …; the default names pages <stem>-1.dump.png, <stem>-2.dump.png … Returns the page/chars/lines summary and output paths.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"src": map[string]interface{}{
					"type":        "string",
					"description": "Path to the input text file to render (required).",
				},
				"out": map[string]interface{}{
					"type":        "string",
					"description": "Output PNG path (optional). Default: an absolute path in the gopi-text2img scratch class tree (NOT beside src — a default render must never leave a file in your working tree); multi-page output appends -1, -2, … before the class suffix.",
				},
				"size": map[string]interface{}{
					"type":        "integer",
					"description": "Font size in px (default 20).",
				},
				"cols": map[string]interface{}{
					"type":        "integer",
					"description": "Wrap width in characters for over-long lines (default 50).",
				},
				"lh": map[string]interface{}{
					"type":        "integer",
					"description": "Line height in px, ~font-size×1.6 (default 32).",
				},
				"lines": map[string]interface{}{
					"type":        "integer",
					"description": "Max lines per page (~2800 chars/page; default 90).",
				},
				"margin": map[string]interface{}{
					"type":        "integer",
					"description": "Page margin in px (default 12).",
				},
				"font": map[string]interface{}{
					"type":        "string",
					"description": "Font path override (default: autodetect system CJK TrueType font).",
				},
			},
			"required": []string{"src"},
		},
	},

	{
		Name:        toolnames.ToolScreenCapture,
		Description: "Capture the LIVE X11 desktop (the real screen) into a PNG file, and return machine-readable ground truth (saved path, pixel width×height, position x/y, depth, display, PNG byte count, content sha256_16). Four shapes — pick the narrowest that answers the question: the WHOLE SCREEN (default; every window on the display), the window that HOSTS this process (`self: true` — the terminal gopi is running in, resolved by pid), the ACTIVE WINDOW (`window: true` — whatever the user last clicked, a fraction of the pixels and therefore a fraction of the context), or an ARBITRARY RECTANGLE (`area: \"WxH+X+Y\"`, e.g. \"800x600+100+50\"; offsets optional). When to use: 看屏幕 / 截图当前桌面 / 截图当前窗口 / 电脑操作 (computer use) 的第一步 — 'what is on screen right now', before and after driving the GUI. For 'where am I running' prefer `self: true` — `window: true` answers a different question and can return another application. This is the EYE of the capture→act→verify loop: call it, pass the returned path to vision_analyze to READ the screen, then act (`bash`: `gopi tool act …`), then call it again and compare sha256_16 to prove the screen changed. NOT for a web page you want rendered headlessly (use `gopi tool shot --url` via bash — that drives headless Chromium and never touches the real screen), and NOT an image-file reader (vision_analyze). Pure Go, zero-CGO, zero-dependency, no external binary: a hand-rolled X11 protocol client issues a GetImage request, so any X11 display works, local or forwarded (Xvfb included). X11 ONLY — on a Wayland session this fails loudly by design (no portal implementation). Returns JSON.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"display": map[string]interface{}{
					"type":        "string",
					"description": "X11 display to capture, e.g. ':99' or 'localhost:10.0' (optional). Default: $DISPLAY, i.e. the display of the desktop the agent runs on.",
				},
				"out": map[string]interface{}{
					"type":        "string",
					"description": "Output PNG path (optional). Default: an ABSOLUTE path inside gopi's capture class directory (<scratch home>/dumps/gopi-captures/screen-<unix>.dump.png) — NEVER the current directory, because a run's cwd is its own git ground and an untracked PNG there breaks Rule 30. Always use the `path` field the result returns; it is absolute and it is the only correct argument for vision_analyze.",
				},
				"window": map[string]interface{}{
					"type":        "boolean",
					"description": "Capture the ACTIVE WINDOW instead of the whole screen (optional; the CLI spelling is `gopi tool capture --window`). It means whatever window the user last clicked — for 'the window I am running in' use `self` instead, which resolves by pid and cannot be a different application. The result's x/y/window fields say WHICH window was grabbed (`window_route` names how it was resolved: _NET_ACTIVE_WINDOW, GetInputFocus, or QueryPointer). Fails loudly when the display has no active window to resolve.",
				},
				"self": map[string]interface{}{
					"type":        "boolean",
					"description": "Capture the window that HOSTS this gopi process — the terminal you are running in (optional; the CLI spelling is `gopi tool capture --self`). Use it when the question is about WHERE YOU ARE RUNNING rather than what the user is looking at: it reads /proc/self/fd/0 for the controlling tty, finds the terminal emulator that owns that pty's master side, and matches that pid against each window's _NET_WM_PID. It is an identity, not a guess, so it cannot return a different application. Fails loudly (naming the ancestry walked and the pids the display reports) when this process and the display's clients do not share one pid namespace — e.g. an X-forwarded display.",
				},
				"decorations": map[string]interface{}{
					"type":        "boolean",
					"description": "With `self`: include the window-manager frame (title bar and borders) by expanding the window's rectangle by _NET_FRAME_EXTENTS (optional). Without it the client area — the pixels the application drew — is captured, which for a client-side-decorated window (GNOME Terminal) already contains the title bar; the result's `window_route` says which rectangle was taken.",
				},
				"area": map[string]interface{}{
					"type":        "string",
					"description": "Capture an arbitrary rectangle of the screen, spelled WxH+X+Y in pixels (optional; the CLI spelling is `gopi tool capture --area WxH+X+Y`). The offsets are optional, so \"800x600\" means the top-left 800x600. The rectangle must fit the display — one that runs off the screen is refused, never clipped. Takes precedence over `window` and `self`.",
				},
			},
		},
	},
}

// Agent holds the state for the ReAct loop.
type Agent struct {
	Client        *deepseek.Client
	Ctx           *Context
	Model         string
	MaxIterations int
	SystemPrompt  string // first message acts as system prompt when Context has none
	Logger        *log.Logger
	Debug         bool
	Cost          *deepseek.CostTracker
	RawLog        *RawLogger     // raw HTTP data log (session-dir-unify: co-located with the session JSONL; ~/.gopi/raw/ legacy fallback)
	SessionLog    *SessionLogger // JSONL session log (~/.gopi/sessions/<task_id>/<run-uuid>.jsonl or <uuid>/session.jsonl)

	// Hippocampus is the active-memory organ's host (Lv-0..Lv-3 delivery +
	// budget), nil unless GOPI_HIPPOCAMPUS enabled it — nil means NO code path
	// at all, which is what keeps the default runtime byte-identical
	// (devdoc/hippocampus.md §4.2 口径 1). Built in NewAgent, never mutated
	// afterwards except through SetHippocampusRecall (the organ binding).
	Hippocampus *hippoScheduler

	// hippocampusRound is the 1-based round the round-boundary delivery runs
	// for (0 = run start). The audit record and the retrieval are stamped with
	// it so a session log can be read round by round.
	hippocampusRound int

	// autoTaskSessionID is the task-ledger provenance session id used by
	// task_submit's anti-self-reference stamp when tests build Agent{}
	// literals without a SessionLogger. Empty in production wiring
	// (sessionIDForTasks reads SessionLog directly).
	autoTaskSessionID string

	// TurnBaseline is how many TURN-OPENING rows the session's record already
	// held when this run started — the anchor every turn number this run stamps
	// starts from (noteTurnNumber, selfrecord_noteindex.go).
	//
	// WHY IT LIVES ON THE AGENT. The file line and the index row are written by
	// an Agent that may be re-created per turn (the chat surface), so the agent
	// cannot count the session; SessionLogger.TurnsSoFar counts this run's own
	// contribution, and this is the other half. A resumed chat sets it from the
	// log's own count (`chatResumeTurnBaseline` → `chatTurnOrdinalAt` →
	// `gopilog.SessionTurns`, the producer the display surfaces print), so the
	// number a note carries and the number `session log --turns` gives the same
	// turn are the same number (task 01a16fb3's offset, closed for the record
	// the same way it was closed for the screen).
	//
	// A FRESH SESSION LEAVES IT ZERO, which is not an absence here but the
	// ordinary case: nothing was recorded before this run, so this run's own
	// count IS the session's. Nothing infers a turn from it.
	TurnBaseline int

	// chatSurface is the CHAT-SCOPED tool's live state (internal/agent/
	// chat_surface.go): the surface half installed by the process that owns the
	// chat REPL (SetChatSurface), the pending self-driven line, and the counter
	// the loop's bound is enforced against. The zero value is the ordinary case
	// — no surface installed, every action answered with a sentence
	// (chatSurfaceUnavailableNotice) rather than a silent empty success.
	chatSurface chatSurfaceLoop

	// chatSurfaceFact is this run's own ARM STATE as published by the process
	// that owns the chat REPL (SetChatSurfaceFact, chat_surface_fact.go): whether
	// its screen dump is armed, and where the artifact is. It travels to the
	// REQUEST (a system note rendered by both chat assemblies) instead of to the
	// operator's screen, because the fact is the MODEL's business — the screen
	// keeps only the anomalies that a human has to act on. The zero value is
	// "nothing was published", which renders as nothing.
	chatSurfaceFact chatSurfaceFact

	// TaskState is the harness's view of the model's task list (fed by
	// [TASK_LIST] blocks, read by the todo tool). Set in NewAgent; tests
	// building Agent{} literals may leave it nil (todo reports "no list").
	TaskState *todoState

	// taskAreaActive latches that the session has committed at least one
	// write_task_area conclusion (the in-session working area exists). Read
	// by the carry-forward hook (injectTaskAreaConclusions) to decide whether
	// committed conclusions must be re-surfaced in the next round's context;
	// written only by the write_task_area dispatch. Single-threaded loop
	// access (mirrors the deadEndNudged latch pattern).
	taskAreaActive bool

	// taskAreaDirty latches that a write_task_area commit happened THIS round
	// and its conclusions have not yet been re-surfaced as authoritative
	// context (POC H3: carry conclusions embedded in the next round's
	// user/system context — never implied-but-missing, which H4 showed turns
	// into fabricated facts). Cleared by injectTaskAreaConclusions.
	taskAreaDirty bool

	// taskAreaNudges counts the framework completion nudges injected so far
	// (memory-aware T2 hard-takeover). Capped by maxTaskAreaNudges: the
	// framework insists the model commit every pending item through
	// write_task_area before letting a no-tool round exit — but only up to
	// the cap, after which the run is allowed to finish (anti-deadlock: a
	// model that refuses to comply is never hard-blocked). Single-threaded
	// loop access (mirrors the taskAreaActive latch pattern).
	taskAreaNudges int

	// lastTaskReviewVerdict records the most recent task(action=review)
	// verdict the model submitted (tool-family merge — task family:
	// review-as-toolcall). Empty until the first review. The harness does
	// NOT auto-mutate the model-emitted task list from a verdict (the list
	// is model-owned; the model re-emits after acting on the returned
	// verdict action); it validates + latches the verdict so the completion
	// path and audits can read the structured decision. Single-threaded loop
	// access.
	lastTaskReviewVerdict string

	// CarryForwardSessionID is the ORIGINAL session whose committed
	// write_task_area conclusions must be re-injected into this run's
	// context (memory-aware T2 resume carry-forward, cross-RUN handoff). Set
	// by the resume CLI when it can recover the source session id (the --task
	// row's session_id column); empty means no carry-forward. Read once in
	// the runEngine resume branch, BEFORE the continuation message, so the
	// first resumed request already carries the committed-conclusions block.
	CarryForwardSessionID string

	// Nudge config + per-run latch for the no-list-yet reminder (P1.4/P2).
	// NudgeRounds is how many rounds with no list must elapse (default 5),
	// NudgeToolRounds how many of those rounds must have carried tool calls
	// (default 3) before the harness nudges. Both are env-overridable
	// (GOPI_NUDGE_ROUNDS / GOPI_NUDGE_TOOLS); constants are the defaults
	// (Rule 2). nudgeCount is the cumulative number of nudges actually sent;
	// nudgeNextAt is the 1-based round index the next nudge is armed for
	// (escalates by nudgeEscapeRounds between fires, capped by maxNudgeCount).
	// All read/written only from the Run loop (single-threaded) — mirrors the
	// deadEndNudged / taskStartSearchPrompted latch pattern.
	NudgeRounds     int
	NudgeToolRounds int
	nudgeCount      int
	nudgeNextAt     int
	roundsNoList    int // consecutive rounds with a tracked list but no fresh [TASK_LIST] block (Report A §6.5)

	// staleListNudgeCount caps the bookkeeping-decay nag (staleTaskListNudge)
	// at maxNudgeCount, mirroring the no-list nudge's anti-harassment cap.
	staleListNudgeCount int

	// ReasoningEffort controls DeepSeek reasoning depth (minimal/low/medium/high).
	// Set via GOPI_REASONING_EFFORT env var. Empty = model default.
	ReasoningEffort string

	// Thinking controls DeepSeek thinking mode (enabled/disabled).
	// Default: ENABLED at reasoning_effort=minimal (set in NewAgent — DeepSeek's
	// model default is thinking OFF). Data-backed sweet spot (thinking-sweep):
	// light thinking cuts round-trips ~35% (23→15) and wall time (289s→216s)
	// versus disabled at 1.4x per-round cost; medium/max multiply latency with
	// no quality gain. GOPI_THINKING=disabled opts out.
	Thinking *deepseek.Thinking

	// HideThinking suppresses the live stderr display of reasoning content.
	// Thinking mode itself stays enabled — only the live print is hidden.
	// Set via GOPI_HIDE_THINKING env var (any non-empty value).
	HideThinking bool

	// Task-ledger auto-registration latches (internal/agent/taskregister.go):
	// autoTaskRegistered latches that the start hook inserted the session's
	// own AUTO in_progress row (the end hook closes exactly that row, once).
	// Set by maybeRegisterSessionTask (runEngine start) — read/written only
	// on the single-threaded run path. autoTaskRowID is the ledger task_id
	// the liveness heartbeat + end hook act on: the auto row's t-<unix> for
	// a plain session, OR the HUMAN wrapper row's task_id for a `task run`
	// child (GOPI_TASK_WRAPPER_ID — task-single-row-exec: the child mints no
	// auto row and keeps the single execution row alive). The end hook,
	// terminal push, finish-cleanup and the heartbeat all key on it instead
	// of assuming task_id == session_id.
	autoTaskRegistered bool
	autoTaskRowID      string

	// The CLONE CHANNEL ground a bare-prompt run took
	// (internal/agent/cloneground.go): the workspace directory and the branch
	// this run committed to. Both EMPTY when the channel is off — which is the
	// ordinary shape and the backward-compatibility contract (an empty pair
	// means "this run is in the worktree channel", exactly as before). Written
	// only by adoptCloneGround on the single-threaded run path; the only reader
	// is CloneRunGround, the CLI's close-out narration.
	cloneGroundDir    string
	cloneGroundBranch string

	// lastClosedRowStatus is the status the END hook just wrote for the row
	// this run registered ("" when it wrote none). The clone ground's delivery
	// leg (internal/agent/clonedelivery.go) writes its outcome as the row's
	// REASON, and it must pass the SAME status the close chose: re-reading the
	// ledger would race the close, and inventing one would be a second verdict
	// on the row. Single-threaded run path, like the latches above.
	lastClosedRowStatus string

	// cloneDeliveryAttempt is the delivery leg's ONE call
	// (internal/agent/clonedelivery.go). nil in production → the leg reaches
	// devworkspace.AttemptDelivery, the SAME function the clone channel's own
	// loop calls; a test substitutes it to prove the three outcomes (delivered /
	// blocked / broken) reach stderr and the row without a git fixture.
	cloneDeliveryAttempt cloneDeliveryAttempt

	// cloneDeliveryDone latches the delivery leg to ONE attempt per run. The
	// ORDER the end hook needs is "deliver, THEN claim" — the delivery proof at
	// the write choke point (tools.deliveryProofRefusal) measures the row's
	// claim against the destination the push has to move, so a leg that runs
	// after the claim refuses its own success (measured: `delivery proof:
	// REFUSED` on the same stderr line as `delivery delivered`, 2026-10-24).
	// The leg is therefore invoked from the hook BODY, immediately before the
	// status write, and the defer is kept as the fallback for every early
	// return (no registered row / ledger close failure) — this latch is what
	// makes those two call sites ONE attempt rather than two.
	cloneDeliveryDone bool

	// cloneDeliveryVerdict is that ONE attempt's outcome, kept for the second
	// call site. A latch that only remembered "an attempt happened" would let
	// the fallback's hook re-decide a status with NO knowledge that the push
	// did not land — writing the `completed` the primary call site just
	// refused. So the verdict travels with the latch, and every later call
	// sees the same answer (single-threaded run path, like the latches above).
	cloneDeliveryVerdict cloneDeliveryOutcome

	// lastSubmitRowID is the ledger id of the row THIS RUN most recently
	// enqueued through the task_submit TOOL (LLM path), so the next
	// task_submit in the same run can chain onto it: with no explicit
	// parent_task_id the child's parent becomes the previous submission.
	// The operator's ruling is the reason (2026-10-05): "LLM 提交任务的顺序 =
	// 逻辑依赖顺序 … 默认顺序就是依赖" — a model cannot express a dependency
	// graph, so the order it submits in IS the dependency. It starts EMPTY:
	// an explicit parent_task_id always wins, and the FIRST auto-linked submit
	// of a run hangs off the run's OWN row (autoTaskRowID, the id the operator
	// is splitting), never off nothing.
	// Single-threaded run path, like the latches above.
	lastSubmitRowID string

	// taskChildRowID is the WRAPPER row id a `task run` child was spawned
	// with (tools.TaskWrapperEnv, read ONCE in NewAgent). It is the
	// discriminator between the two meanings of "a fresh run from stdin": a
	// TASK CHILD carries it (its stdin IS a briefing and the Epic framing is
	// its contract), while the operator's own `gopi "<prompt>"` does not —
	// RunPrompt sends that text verbatim. Empty for every other run.
	taskChildRowID string

	// Crash-dump epic ① collection state (single-threaded run path):
	// lastClaudeRequest remembers the request built for the CURRENT round —
	// when chatOnce fails terminally the collector digests/dumps it;
	// failedIteration is that round's index; recentToolCallNames is a
	// bounded ring of tool names the session executed (dump context).
	lastClaudeRequest   *deepseek.ClaudeRequest
	failedIteration     int
	recentToolCallNames []string

	// recentToolCallNamesMax bounds the tool-name ring (Rule 1).
	// (constant lives in crashdump.go)

	// Chat-table registration latch (internal/agent/chatregister.go — M4.6
	// 钩子分流): chat processes register into the chats table instead of the
	// tasks ledger. Set by RegisterChatSession (first successful turn),
	// cleared by CloseChatRow (chat exit).
	chatRegistered bool

	// OnText, when set, receives every text fragment the model streams during
	// a run — the same fragments that would otherwise only be printed to
	// stderr. Chat mode (gopi chat) uses it to surface the model's answer on
	// stdout per-delta (real-time streaming) as well as keeping the final
	// result. Called on the single-threaded run path; must be safe to invoke
	// from within chatOnceAttempt (no locking needed — single goroutine).
	OnText func(string)

	// OnThinking, when set, receives the live reasoning stream — the SAME bytes
	// the stderr path renders, markers included: the markers' open delimiter,
	// then each reasoning delta, then the close delimiter. It mirrors OnText
	// exactly (optional callback, single-threaded run path, nil by default).
	//
	// WHY it exists: a carrier that OWNS the screen (gopi chat's reserved layout
	// paints the answer band, the editing row and the status row at absolute
	// rows) cannot be written over by a flow stream — the reasoning text landed
	// on the row the layout owns and tore it. The carrier re-points the stream.
	// A nil value keeps the historic os.Stderr rendering BYTE-FOR-BYTE, so
	// cooked/flow, a piped run and the testsuite's greps are unchanged.
	//
	// It is the SINGLE declaration of this seam: the carrier re-points the flow
	// stream through it (Agent.flowWriterFor adapts it to an io.Writer so the
	// markers, the deltas and the logger's own notices all share one
	// destination), and gopi chat installs it via installChatFlowSeam.
	OnThinking func(string)

	// StreamThinkingDeltas, when true, makes the LIVE reasoning stream reach
	// OnThinking as the RAW deltas instead of the stderr-shaped stream: no
	// open marker, no "[thinking] " prefix, no closing reset, no newline.
	//
	// The DESTINATION rules are the flag's companion, and each is a real fork
	// this epic had to decide rather than inherit:
	//
	//   - OnThinking set  → the seam receives the raw fragments, and NOTHING is
	//     written to os.Stderr (a copy there would be written over the display
	//     the carrier owns — the very smearing silenceEngineConsole exists for);
	//   - OnThinking nil  → the stream is DROPPED, not routed to os.Stderr. That
	//     is the difference the flag buys: "raw" is a claim that the decoration
	//     is unwanted, and decoration-free bytes on a terminal are reasoning a
	//     reader cannot tell from the answer. A raw carrier that forgot its
	//     callback shows nothing, which is a visible, debuggable state rather
	//     than a screen nobody can read.
	//
	// WHY a flag and not a second callback: the seam is one destination by
	// construction (flowWriterFor), and a carrier that RENDERS the reasoning
	// itself must not receive the marker bytes — they are display decoration
	// for the terminal path (SGR dim, a textual prefix) and a renderer that
	// counts cells would have to strip them again. The flag moves the
	// DECORATION decision, not the destination, so there is still exactly one
	// writer to address.
	//
	// Default FALSE, so every existing carrier keeps the historic byte stream
	// (internal/agent/thinking_seam_test.go pins it and `gopi chat`'s banded
	// layout strips nothing it was not already stripping).
	//
	// It is read ONLY on the streaming path's live display; the non-streaming
	// fallback keeps whole-block marking (it has no deltas to stream).
	StreamThinkingDeltas bool

	// OnToolEvents, when set, receives one ToolEvent per tool call and per
	// tool result issued during ChatOps turns (internal/agent/chat_ops.go).
	// It mirrors the OnText pattern exactly (optional callback on the
	// single-threaded run path; loop machinery itself untouched) so carriers
	// outside the terminal — the `gopi web` SSE stream — can surface tool
	// activity live. `gopi chat` leaves it nil (its tool rounds print to
	// stderr already).
	OnToolEvents func(ToolEvent)

	// OnUserNotice, when set, receives a statement addressed to the OPERATOR —
	// not to the model, not to a log file. It is the channel a carrier uses for
	// something the human must act on, and it exists because Logger is not that
	// channel: on `gopi chat` stderr is off-screen under the reserved layout or
	// erased by the editor's next repaint (cmd/agent/lineedit_wire.go's
	// WriteNotice records the measurement). The first (and currently only) user
	// is context pressure (context_pressure.go): a filled window is announced
	// here and nowhere else, and the carrier decides where the operator sees it.
	//
	// Optional and nil by default; a run with no carrier installed keeps the
	// statement on the Logger and SAYS SO (Rule 15), never silently dropping it.
	OnUserNotice func(string)

	// OnMessageEnd, when set, is called when ONE ASSISTANT MESSAGE of the turn
	// has ended — at the end of each round's stream (chatOnceAttempt), before the
	// round's tool calls run. A turn with tools carries SEVERAL assistant
	// messages; a carrier that classifies the answer's format (the banded
	// editor's fence classifier, lineedit.AnswerRenderer.AnswerBoundary) needs
	// this boundary, because a message that ends inside a fenced block it never
	// closed must not leave the classifier inside that block for the NEXT
	// message's text.
	//
	// It is optional and nil by default: a carrier that does not classify the
	// answer sees no byte and no call, and the historic stderr/flow path is
	// unchanged. It carries no text — it is a delimiter, and a delimiter a
	// consumer could mistake for content is not one.
	OnMessageEnd func()

	// PersistChatSession keeps ONE session id + ONE session/raw log across
	// ALL turns of a gopi chat run (the Epic's ask: chat is one conversation
	// → one session, content appended to the same log, not a fresh session
	// id + log file per turn). When true, Agent.Chat does NOT close the
	// session log / raw log and does NOT write a per-turn session_end —
	// the session stays open so each turn's user/assistant/request events
	// append to the same JSONL, and the caller closes it once at chat exit
	// via CloseChatSession. False (default) preserves the original per-turn
	// behavior (mirrors the single-shot gopi prompt each turn).
	PersistChatSession bool

	// ChatMode marks the simplified chat path (Agent.Chat). When true, the
	// answer's text is NOT printed to stderr by chatOnce (it would double-
	// print alongside the stdout OnText→chatRenderer path) — only thinking
	// stays on stderr. False for the normal task ReAct loop.
	ChatMode bool

	// ChatCarrier names the FACE answering this turn (internal/tools'
	// ChatPolicyCarrier* values: "terminal", "web", "ui", "cli"). It is set by the
	// chat entry points for the length of one turn (same publish/restore shape as
	// chatTurnInput) and read ONLY by the chat write boundary, which uses it to
	// decide what `prompt` means on a face that has no approval channel at all:
	// `gopi web --tools` declares "no approval gate" on its own page, so asking the
	// browser operator to approve a line they typed is a question with no reader
	// (internal/tools/chat_policy_carrier.go states the measurement). Empty means
	// UNKNOWN, which degrades to nothing: the frozen operator posture answers, the
	// console's behaviour verbatim.
	ChatCarrier string

	// chatTurnInput is THIS chat turn's own user message, held for the length of
	// the turn and overwritten by the next one. It is the chat write boundary's
	// authorization channel (internal/tools/chat_cleanup_authorization.go): the
	// destructive half of the cleanup faces is allowed only when the OPERATOR
	// asked for cleanup in these words, so the gate reads a function of one
	// turn's input rather than a stored switch — there is nothing here a later
	// turn could inherit, and nothing to leave enabled (Rule 35: a setting or an
	// env var would be a persistent artifact created to avoid one sentence).
	chatTurnInput string

	// PrefetchSeed is the retrieval the CALLER ran for the model before this
	// turn's messages were seeded — a background memory lookup whose hits must
	// reach the model in the shape of its OWN memory(action=search), so the hit
	// lines carry record ids (#N) and the existing feedback discipline plus the
	// whitelist context slim apply to them unchanged. internal/agent/
	// chat_prefetch_seed.go states the design and owns the seeding.
	//
	// It is PUBLISHED BY THE TURN'S OWNER AND CONSUMED ONCE by the runner
	// (takePrefetchSeed clears it) — the same shape chatTurnInput uses: a value
	// a caller sets for one turn, which no later turn can inherit. The zero
	// value is the OFF path: nothing is seeded, and the message array is
	// byte-identical to the pre-change tree.
	PrefetchSeed SeededRetrieval

	// TurnSource is the FACE this turn's user line came from
	// (internal/inputsource: Human, LLM, Queue) — published by the turn's owner
	// beside PrefetchSeed and consumed once by the runner, so the JSONL's
	// `user` row can record which face produced the line the model is answering.
	//
	// WHY IT COULD NOT LIVE IN THE MESSAGE: `chat_surface action=type` lets the
	// model enqueue its own next turn, and such a row is indistinguishable from
	// an operator's from the outside (no harness prefix — that marks the
	// FRAMEWORK's injections, not the model's self-typed lines). The operator's
	// ruling is that it still COUNTS as a turn; recording the face is what keeps
	// the count honest AND the transcript traceable.
	//
	// The empty value means "the caller did not say", which the logger records
	// as absent — the same shape a pre-2026-10-25 row has, and the reader's
	// default for those is Human.
	TurnSource string
	// TurnOpenedAt is the moment THIS turn's `user` row was written — taken
	// once, at the write, and published back to the caller so a banner paints
	// the SAME timestamp the record holds.
	//
	// THE DIRECTION IS THE WHOLE POINT. A display that read a clock would print
	// a different second than the row (and, on a replay, the wrong DAY); a
	// display that re-parsed the file would race the writer's own buffer. The
	// row's timestamp is minted in exactly one place — `SessionLogger.log`'s
	// default — so the honest shape is: the writer stamps it, keeps the value,
	// and the banner reads it back. Consumed once, like TurnSource above.
	TurnOpenedAt time.Time

	// Unattended declares that NO HUMAN is on the other end of this run: there
	// is no reader to answer a question, so a question is where the run ends.
	// It is set by the ONE face whose contract that is (`gopi chat --once`,
	// cmd/agent/chat_once.go) and is OFF by default, so an interactive console
	// — where a human IS reading — renders exactly the prompt it rendered
	// before (chat_unattended.go states the design and the measured A/B behind
	// it: the block is the difference between asking a human and choosing a
	// convention and continuing).
	//
	// It is read ONLY by the two chat assemblies (assembleChatSystemPrompt,
	// chatOpsSystemPrompt) through chatUnattendedBlockOpen, and by nothing that
	// enforces behaviour: the block is context, never a mechanism (Rule 3 — no
	// new tool, and no bound is relaxed by it).
	Unattended bool

	// CloseSignal names the signal (signalrec's spelling: "SIGHUP", "SIGTERM")
	// that ENDED this session through the carrier's own close-out, or "" when
	// the session ended any other way. The carrier sets it at the moment its
	// watcher decodes the signal, BEFORE it unwinds, so the close-out that runs
	// afterwards can state the cause instead of having to guess it.
	//
	// WHY THE AGENT HAS TO BE TOLD (measured, session ab9e0023, 2026-10-21).
	// CloseChatSession collects a failure record whenever the exit code is
	// non-zero, and collectErrorRecord's arms all key on a RUN ERROR — a signal
	// death has none, so the record fell through to the unknown default and the
	// session's own dump read `category: unknown` beside a deliberate, orderly
	// close-out. An operator reading that afterwards cannot tell a terminal
	// teardown from a provider failure: that is the "vanished with no
	// explanation" report this field exists to answer. The field is a STRING
	// because the value's spelling is signalrec's (one definition, Rule 14) and
	// the carrier renders it with signalrec.SignalName — internal/agent never
	// re-spells a signal name.
	CloseSignal string

	// continuationShot latches that ChatOps already injected its one-shot
	// degenerate-round continuation (a no-tool round with no substantive
	// answer). A second consecutive degenerate round is accepted as-is
	// rather than looping. Reset whenever a tool round runs.
	continuationShot bool

	// KeepThinking is the P2 thinking-trim switch (GOPI_KEEP_THINKING=1).
	// Default false: assistant reasoning (thinking text) is STRIPPED from
	// the request wire on every round — the text is already preserved in
	// the session JSONL (LogAssistantThinking), and LLM reasoning is
	// per-round (it does not carry over between rounds), so replaying it
	// only burns 1-4K tokens per round for zero benefit. =1 replays it in
	// assistant history (A/B control arm). In-memory Context.Messages keep
	// ReasoningContent either way — only the wire is trimmed.
	KeepThinking bool

	// thinkingAbortStreak counts consecutive rounds whose thinking blew the
	// harness budget and had to be aborted + re-issued thinking-disabled.
	// thinkingForceDisabled engages when the streak reaches
	// maxThinkingAbortStreak: a run whose thinking keeps running away gets its
	// thinking PAUSED, so it stops paying the double-prefill cost of abort
	// rounds (see chatOnce). The pause is a cycle, not a one-way latch —
	// thinkingPauseRounds counts the rounds served paused and every
	// thinkingProbeSpacing-th round goes out thinking-enabled again as a
	// recovery probe (see thinkingPausedForRound).
	//
	// Internal state, set/read only from the Run loop (single-threaded).
	thinkingAbortStreak   int
	thinkingForceDisabled bool
	thinkingPauseRounds   int

	// thinkingProbeRound marks the NEXT chatOnce call as a recovery probe
	// (thinking was re-enabled for this one round while the rest of the run
	// stays paused). Set where the request is built, consumed by chatOnce.
	thinkingProbeRound bool

	// thinkingBudgetReported latches the one-time effective-thinking-budget
	// report (Rule 15: a configured budget above max_tokens/2 is silently
	// clamped by the request builder; without the line an operator sees the
	// abort but never the number behind it).
	thinkingBudgetReported bool

	// memoryStored is set true on the first successful memory_store tool call
	// in this run. completionStorePrompted is a one-shot latch: at the
	// all-completed completion point, if the run still has not stored, a single
	// checkpoint user message is injected once to force the task-end store
	// (mirror of the task-start memory_search enforcement). Both are
	// observability/enforcement state, read/written only from the Run loop.
	memoryStored            bool
	completionStorePrompted bool

	// memorySearched is set true on the first successful memory_search tool
	// call in this run (对称 with memoryStored). taskStartSearchPrompted is a
	// one-shot latch: before the first round, if the run has not searched, a
	// single taskStartSearchMessage user message is injected once to force the
	// task-start search (mirror of the task-end completion store). Both are
	// observability/enforcement state, read/written only from the Run loop.
	memorySearched          bool
	taskStartSearchPrompted bool

	// consultCheckpointPrompted latches the review-time memory-consult
	// checkpoint (candidate C) so it fires exactly once per run, at the first
	// committed item. consultArmed carries the arm state set by the
	// start-of-run path (disarmed for a hand-built Agent). Set only from the
	// Run loop.
	consultCheckpointPrompted bool
	consultArmed              bool

	// selfRecord holds the self-recording state (selfrecord.go): where this
	// run's record file is, the notes recorded so far, whether the
	// post-compaction carrier has been re-injected for the current
	// compaction, and whether the model-facing contract was injected.
	// selfRecordRulePrompted is its one-shot injection latch. Read/written
	// only from the Run loop.
	selfRecord             selfRecordState
	selfRecordRulePrompted bool

	// reframeApplied latches that the reframed epic was swapped into the
	// [EPIC GOAL] message (reframe+evaluate merge). Set by applyReframe —
	// read/written only from the Run loop. A run applies at most one swap;
	// later rounds skip the scan (the latch keeps the per-round prefix check
	// off the hot path).
	reframeApplied bool

	// reframeVerdict is the verdict of this run's task(action=reframe) call
	// ("" = none yet). Read/written only from the Run loop; it gates the
	// nudge backstop (a reframed run is never nudged) and latches the
	// repeat-call detector (a second reframe is recorded as telemetry but
	// never re-dispatched — duplicate children would be a real defect).
	reframeVerdict string

	// reframeSubmitRepo is the DURABLE repository the TOO_BIG roadmap's child
	// rows inherit (epic 01a0d55b). A worker-run parent stands in its GROUND —
	// a linked worktree created for that run and deleted when it finishes — so
	// defaulting a child's repo to the cwd bound the child to a directory its
	// own parent destroys. The field is the ONE seam the resolution and its
	// test enter through; empty means "resolve it at dispatch time".
	reframeSubmitRepo string

	// reframeHonestExit latches an IMPOSSIBLE verdict: the dispatch already
	// marked this run's ledger row failed with the model's reason, so the end
	// hook must NOT re-close the same row as completed (that would erase the
	// verdict from the ledger).
	reframeHonestExit bool

	// reframeNudged latches the reframe nudge (early-trigger backstop: a
	// big-briefing run that has neither reframed nor been swapped gets one
	// reminder). Read/written only from the Run loop.
	reframeNudged bool

	// deadEndNudged is a per-run latch: once a dead-end tool result has been
	// followed by the deadEndTrimMessage nudge, further nudges are suppressed
	// for the rest of the run. A single clear nudge (right after the first
	// dead-end) is enough to teach the pattern; repeating it every error
	// would spam the context with the very noise context_trim exists to
	// remove. Internally read/written from the Run loop (single-threaded).
	deadEndNudged bool

	// NoEnforceMemory disables BOTH harness memory checkpoints (task-start
	// search + task-end store) for A/B isolation of prompt-vs-harness effects
	// (MEMORY-PROMPT-PERSUASION.md §4.1). Set via GOPI_NO_ENFORCE_MEMORY /
	// --no-enforce-memory. Default false = enforcement on.
	NoEnforceMemory bool

	// plainPrompt marks a run whose first user message is the caller's text
	// VERBATIM — the plain `gopi [options] "<prompt>"` shell invocation, whose
	// page promises `"<prompt>"  The task text` and nothing else. Set by
	// RunPrompt (the shell path and only it); false for every TASK surface —
	// a dispatched row's briefing, the merger's prompt — which keep the
	// [EPIC GOAL]/task-list/reframe framing that IS their contract. Read once,
	// in runEngine's first-turn branch.
	plainPrompt bool

	// NoEnforceMemoryConsult disables ONLY the review-time memory-consult
	// accountability checkpoint (memory-consult epic, candidate C) — the
	// one-shot "which work could a memory search have saved?" question asked
	// when the first item commits. A separate switch from NoEnforceMemory so
	// the A/B baseline can drop this measurement instrument while the
	// task-start search enforcement stays exactly as it is. Set via
	// GOPI_NO_ENFORCE_MEMORY_CONSULT / --no-enforce-memory-consult.
	// Default false = the checkpoint is injected.
	NoEnforceMemoryConsult bool

	// Clock is the session clock (CLOCK-TICK): t0 recorded at NewAgent, and
	// every tool result is prefixed with its elapsed tick before it
	// reaches the context and session log (see withTick). Agents built as
	// literals in tests leave it nil — results pass through unticked.
	Clock *SessionClock

	// ContextWindow is the token budget for conversation history. Before each
	// API call the estimated token count is compared against the compact
	// trigger ratio of this window; when exceeded, the oldest messages are
	// trimmed automatically (system prompt + recent history are kept).
	// 0 disables trimming. Default: DefaultContextWindow (200000 — the
	// deployment window; prefill cost, not model context, is the constraint).
	ContextWindow int

	// MaxTokens is the per-response output cap sent as max_tokens in every
	// Claude request. It bounds thinking blocks AND tool-call arguments
	// (a write tool call carries the whole file draft as escaped JSON, so a
	// small cap truncates long drafts with stop_reason=max_tokens — see
	// devdoc/maxtokens-truncation.md). 0 falls back to
	// deepseek.DefaultClaudeMaxTokens. Override via GOPI_MAX_TOKENS.
	MaxTokens int

	// CompactionEnabled enables LLM-assisted memory compaction (GOPI_COMPACT=off
	// to disable). When the estimated context exceeds the trigger ratio of
	// the window, the oldest messages are summarized into a memory block
	// instead of dropped.
	//
	// IT ONLY DECIDES WHICH REDUCTION RUNS, NEVER WHETHER ONE RUNS: the automatic
	// path as a whole is gated by AutoReduceEnabled (context_pressure.go). With
	// that switch off — the default — this field is read by nobody, because
	// trimContext reduces nothing.
	CompactionEnabled bool

	// AutoReduceEnabled re-enables the automatic context reduction path
	// (fold → compact → trim) that used to be unconditional
	// (GOPI_CONTEXT_AUTOREDUCE=1, truthy-only; see AutoReduceEnv). Default FALSE:
	// a filled window is WARNED about, never emptied — the operator is told the
	// numbers and decides whether to start a new session.
	AutoReduceEnabled bool

	// contextPressureBucket is the last percentage bucket announced to the
	// operator (0 = nothing announced). It makes the warning ONCE PER LEVEL
	// instead of once per round — see context_pressure.go.
	contextPressureBucket int

	// CompactCount is how many context compactions this agent has PERFORMED —
	// the sessions's compression count, the read-out Hermes shows only when it is
	// non-zero. It counts successful compactions (a compaction attempt that a
	// guard refused, or whose window was structurally empty, is not a
	// compression), and it is incremented at the one success return of
	// compactContext. Single-threaded loop access, like every other loop field.
	CompactCount int

	// CompactTriggerRatio is the fraction of the context window at which
	// trimming kicks in (GOPI_COMPACT_TRIGGER, default
	// DefaultCompactTriggerRatio). fold/compact/trim all share this single
	// limit. <= 0 falls back to the default (Agent{} literals in tests).
	CompactTriggerRatio float64

	// TurnFoldRecentTurns is the size of the turn fold's RAW WINDOW: how many of
	// the newest COMPLETE turns stay in the request verbatim. It is an operator
	// ESCALATION and its default is ZERO — no window, so every completed turn is
	// replaced by its note (the substitute contract, turnfold.go's
	// foldCompletedTurns). A NEGATIVE field falls back to that default; zero is a
	// real value and means the same thing it means in the shipped configuration.
	TurnFoldRecentTurns int

	// turnFoldRangeAnnounced latches the once-per-session line for the fold's
	// FIRST early exit: the conversation cut into fewer than two turn ranges, so
	// there is no completed turn to fold at all (turnfold.go's
	// foldCompletedTurns). The state is sticky for the run, so it is reported
	// once — the boundary the fold runs from is per ROUND, and a line repeated
	// every round is noise an operator learns to skip (task 01a1d723: the same
	// argument compactPinnedAnchorWarned carries).
	turnFoldRangeAnnounced bool

	// turnFoldWindowAnnounced is the same latch for the SECOND early exit: every
	// complete turn sits inside the operator's raw window
	// (TurnFoldRecentTurns), so the fold has nothing old enough to replace. It
	// is a SEPARATE flag rather than a shared one because a session can reach
	// both, in either order, and each reason is the operator's own fact to act
	// on.
	turnFoldWindowAnnounced bool

	// CompactTargetRatio is the fraction of the window the history is
	// compacted down to (GOPI_COMPACT_TARGET, default
	// DefaultCompactTargetRatio).
	CompactTargetRatio float64

	// CompactKeepRecent is how many of the most recent messages are never
	// removed by compaction (GOPI_COMPACT_KEEP_RECENT, default 8).
	CompactKeepRecent int

	// CompactMaxSummaryTokens caps the summarizer's output budget
	// (GOPI_COMPACT_MAX_SUMMARY_TOKENS, default min(5% of the window, 12000)).
	CompactMaxSummaryTokens int

	// CompactCooldown pauses summarizer attempts for this long after a
	// summarizer failure (GOPI_COMPACT_COOLDOWN, default 600s).
	CompactCooldown time.Duration

	// CompactReferenceOnly wraps the memory block in REFERENCE ONLY semantics
	// (GOPI_COMPACT_REFERENCE_ONLY, default on).
	CompactReferenceOnly bool

	// compactFailureCooldownUntil is when summarizer attempts resume after a
	// failure (zero = not in cooldown). compactIneffectiveCount counts
	// consecutive compressions that saved <10% (only genuinely-attempted
	// compressions — a compression with no window is a structural no-op, see
	// compactStructuralNoOpUntil, and never strikes the breaker); at 2 the
	// loop skips compaction and trims directly. lastCompactSavingsPct
	// is the most recent compression's savings (100.0 before any compaction).
	compactFailureCooldownUntil time.Time
	compactIneffectiveCount     int
	lastCompactSavingsPct       float64
	// compactRecoveryProbeAt is when the anti-thrash recovery probe is armed
	// after the ineffective breaker trips (count≥2). Once it fires, one probe
	// compaction is allowed (counter drops to 1) so a session that has since
	// grown real compressible material recovers; a fresh probe that is again
	// ineffective re-trips immediately (bounded, not thrash). Zero = not armed.
	compactRecoveryProbeAt time.Time
	// compactStructuralNoOpUntil backs off summarizer attempts after a
	// structural no-op (no window to compress, too few messages) WITHOUT
	// striking the anti-thrash breaker — a short session must not be
	// permanently disarmed when it later grows compressible material
	// (Hermes #93022). Zero = not in backoff.
	compactStructuralNoOpUntil time.Time
	// compactPinnedAnchorWarned latches the once-per-session pinned-anchor
	// warning (an anchor floor that keeps the context above the compaction
	// target). The pinned state is sticky, so it is reported once — an
	// operator cannot act on the same line every round.
	compactPinnedAnchorWarned bool
	// Timeout is the global deadline for the entire agent loop (Run).

	// Set via GOPI_TIMEOUT_SEC env var. Default: 300 (5 minutes).
	// Longer design/analysis tasks can set e.g. GOPI_TIMEOUT_SEC=1800 (30 min).
	Timeout time.Duration

	// interruptBase is the process-wide cancel root the run-engine contexts
	// (runEngine / Chat / ChatOps) derive from. Interrupt() cancels it so a
	// signal handler can gracefully unwind an in-flight run: the round-
	// boundary ctx.Done() check fires (or the in-flight LLM stream aborts),
	// the loop returns an error, and the normal end-of-run defer writes
	// session_end + closes the auto ledger row — the chat-once/bare-prompt
	// SIGTERM zombie fix (cli_web bug003 pattern). Created in NewAgent; nil
	// in hand-built test agents (run-ctx sites fall back to
	// context.Background()). NEVER cancelled by Close()/defer cancel() — it
	// outlives any single run so a multi-turn chat can be interrupted
	// repeatedly.
	interruptBase   context.Context
	interruptCancel context.CancelFunc
	// interrupted latches that Interrupt() fired (read by the run loops to
	// disambiguate a ctx cancellation from a wallclock/timeout hit; the ctx
	// channel's happens-after makes the plain bool race-free — written by
	// the signal goroutine before cancel, read by the run goroutine only
	// after ctx.Done()).
	interrupted bool

	// runCtx / runCancel are the RUN-SCOPED cancellation handle published by
	// the run-ctx sites (see runScopedRoot): the current run's context and its
	// cancel, held under runMu so InterruptRun can abandon exactly this run
	// without touching the process-wide interrupt root — which, being
	// cancel-once and never re-armed, would poison every later turn of a
	// long-lived server. A run's own `defer cancel()` is what clears them.
	runMu     sync.Mutex
	runCtx    context.Context
	runCancel context.CancelFunc
	// runInterrupted records that InterruptRun() — not Interrupt() — abandoned
	// the run in flight: a TURN, while the session stays open. Read through
	// TurnInterrupted (which requires ctx.Err() != nil) and reset when the next
	// run's scope opens.
	runInterrupted bool
	// runInterruptReason is WHICH input asked for that (InterruptReasonCtrlC /
	// InterruptReasonEscape / InterruptReasonSignal), so the witness this run
	// writes names the key the operator actually pressed. It is set with the
	// latch, under the same lock, and cleared with it — a reason surviving into
	// the next run would label an interruption that key never caused.
	runInterruptReason string
	// partialAnswerRunes counts the ANSWER characters this run has streamed to
	// its caller (OnText), which is the size of the partial answer an operator
	// sees when a turn is abandoned mid-stream. It exists because that size is
	// claimed in the session JSONL (turn_interrupted's partial_chars) and shown
	// to the user, and the run is the only party that saw every delta — a
	// carrier that reached for the agent's final string would be reading a
	// value the cancellation never assigns.
	//
	// It is monotonic WITHIN one run and reset when a run's scope opens
	// (runScopedRoot); the counter is written only by the run's own goroutine
	// (answerTextDelta) and read by the same goroutine after the cancel — which
	// is why it needs no lock of its own (runMu guards the RESET, so no
	// goroutine reads a half-reset counter).
	partialAnswerRunes int

	// executeToolFn, when non-nil, replaces the per-call tool executor.
	// Test-only hook: lets tests observe concurrency, control per-call
	// duration, and force batch timeouts without touching real files or
	// processes. Nil = real dispatch (executeToolDefault).
	executeToolFn func(tc deepseek.ToolCall) string

	// parallelTimeout bounds a whole parallel batch of read-only tool calls.
	// 0 = tools.FileToolTimeout(). Unexported — tests set it to keep
	// timeout cases fast.
	parallelTimeout time.Duration
}

// boolEnvTrue reports whether an enabling "on" env flag is set. It accepts the
// documented truthy values ("1", "on", "yes", "true", "enabled") and treats
// everything else — including "0", "off", "false", "no", and the empty string
// — as DISABLED. The spellings are NOT spelled here: this is a delegate of
// config.EnvBool, the env channel of the ONE vocabulary definition
// (internal/boolvocab, PROJECT.md Rule 14) which config's raw-string readers and
// this package's default-ON switches read the same way (Rule 13: a constant
// exists to be referenced, never re-inlined).
//
// The previous `os.Getenv(...) != ""` here was a real regression: config's
// ApplyEnv(fromFlags=true) force-writes GOPI_DEBUG=off whenever --debug is not
// passed, and `!= ""` treated that literal "off" as ENABLED — turning on the
// full raw request/stream dump on every ordinary run ("gopi outputs the whole
// log"). Truthy-only parsing makes the env channel safe regardless of how the
// value got there.
func boolEnvTrue(name string) bool {
	return config.EnvBool(name)
}

// sessionWallclockCap resolves the wall-clock cap (loop-deadlock epic ②):
// unset env = defaultSessionWallclock (8h), "0" = unlimited (cap disabled),
// a positive integer = that many seconds. Exactly one env reader (Rule 2).
// Invalid values warn and fall back to the default — a typo'd cap must not
// silently disable the net.
func sessionWallclockCap() time.Duration {
	v := strings.TrimSpace(os.Getenv(sessionWallclockEnv))
	if v == "" {
		return defaultSessionWallclock
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "[gopi] warning: invalid %s=%q, using default %v\n",
			sessionWallclockEnv, v, defaultSessionWallclock)
		return defaultSessionWallclock
	}
	if n == 0 {
		return 0 // 0 = unlimited
	}
	return time.Duration(n) * time.Second
}

// sessionBudget resolves the model-facing PLANNING budget: unset env =
// sessionBudgetMinutes, "0" = unlimited (0), a positive integer = that many
// MINUTES. Exactly one env reader (Rule 2). Invalid values warn and fall back
// to the default — a typo'd budget must not silently read as the one value
// (0) that switches the whole mechanism off.
//
// It is deliberately NOT defaultSessionWallclock: that is the 8h boundary the
// ENGINE enforces, this is a number the prompt asks the MODEL to plan
// against. Keeping them separate is the point — one is enforced, the other is
// taught (see the budget constants below).
func sessionBudgetMinutesResolved() int {
	if b := sessionBudget(); b > 0 {
		return int(b.Minutes())
	}
	return 0 // 0 = unlimited — the prompt does not invent a denominator for it
}

func sessionBudget() time.Duration {
	v := strings.TrimSpace(os.Getenv(sessionBudgetEnv))
	if v == "" {
		return sessionBudgetMinutes * time.Minute
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "[gopi] warning: invalid %s=%q, using default %dm\n",
			sessionBudgetEnv, v, sessionBudgetMinutes)
		return sessionBudgetMinutes * time.Minute
	}
	if n == 0 {
		return 0 // 0 = unlimited
	}
	return time.Duration(n) * time.Minute
}

// sessionBudgetPrefix opens every rendered budget face. It is the one string a
// reader (and the request-path tests) uses to ask "does this text carry the
// budget at all" — declared once so the three format strings below and the
// tests that look for the clause cannot drift (Rules 1/13/14).
const sessionBudgetPrefix = "session budget: "

// sessionBudgetReportFmt / sessionBudgetEscalationFmt / sessionBudgetRefusalFmt
// render the three faces from ONE measurement (Rule 1b/2/13): the plain report,
// the report past the mark with its expected action, and the refusal. Named
// constants so a rewording happens in one place and the acceptance text can be
// quoted from a single source.
const (
	sessionBudgetReportFmt = sessionBudgetPrefix + "%v elapsed of %v (%d%% used)."

	sessionBudgetEscalationFmt = sessionBudgetPrefix + "%v elapsed, %v remaining of %v (%d%% used, past the %d%% mark) — " +
		"a task that cannot finish inside the budget is TOO BIG by definition, and running out is " +
		"a planning failure, not a graceful ending. Expected action: land the work now (commit the remaining items), " +
		"or declare the split: task(action=%s, verdict=%s) with a roadmap."

	sessionBudgetRefusalFmt = "REFUSED — past %d%% of the session budget with tracked items still open: " +
		"%v elapsed, %v remaining of %v (%d%% used). This commit would add a further subtask, and a task that " +
		"cannot finish inside the budget is TOO BIG by definition. Two exits, both available now: " +
		"(1) LAND THE WORK — finish and commit the items still open, then commit with the list fully completed; or " +
		"(2) SPLIT — file task(action=%s, verdict=%s) with a roadmap, and the harness enqueues the children. " +
		"The commit was NOT recorded. Nothing else is required: no session is being killed."
)

// sessionBudgetStatus is the ONE measurement of where the run stands against its
// planning budget. A named struct because three readers consume the same
// numbers — the commit return's report line, the commit-beat gate and the
// in-loop nudge — and a second inline computation is how a percentage drifts
// away from the mark it is compared against (Rules 1b/2/13).
//
// ok=false means "no claim": the budget is unlimited (0) or there is no clock.
// Every caller then degrades to the pre-existing behaviour, which is what keeps
// the unlimited/nil-clock return byte-identical.
type sessionBudgetStatus struct {
	Elapsed   time.Duration // rounded to the second, as reported
	Remaining time.Duration // budget - elapsed, clamped at zero
	Budget    time.Duration
	Percent   int
	PastMark  bool // Percent >= sessionBudgetWarnPercent
}

func (a *Agent) sessionBudgetStatus() (sessionBudgetStatus, bool) {
	budget := sessionBudget()
	if budget <= 0 || a == nil || a.Clock == nil {
		return sessionBudgetStatus{}, false
	}
	elapsed := a.Clock.Elapsed()
	st := sessionBudgetStatus{
		Elapsed:   elapsed.Round(time.Second),
		Remaining: budgetRemaining(elapsed, budget),
		Budget:    budget,
		Percent:   int(float64(elapsed) / float64(budget) * 100),
	}
	st.PastMark = st.Percent >= sessionBudgetWarnPercent
	return st, true
}

// sessionBudgetCommitRefusal resolves the gate's kill switch (Rule 2: exactly
// one reader of sessionBudgetRefusalEnv). Anything that is not an explicit
// off-spelling leaves the gate ON — a typo must not silently disarm the
// behaviour the run is being measured on.
//
// The vocabulary is NOT re-spelled here: internal/boolvocab is the one
// definition (Rule 13/14), and `!IsFalse` is the default-ON shape every former
// falsey list behaved as — an unrecognized value leaves the gate armed.
func sessionBudgetCommitRefusal() bool {
	return !boolvocab.IsFalse(os.Getenv(sessionBudgetRefusalEnv))
}

// budgetRemaining is the time the run has LEFT against the budget — the
// measurement the escalation sentence must state (the line previously carried
// only the percentage). Clamped at zero: a past-budget run reports "0s
// remaining", never a negative duration that would read as slack.
func budgetRemaining(elapsed, budget time.Duration) time.Duration {
	left := budget - elapsed
	if left < 0 {
		return 0
	}
	return left.Round(time.Second)
}

// sessionBudgetLine renders the session-budget line appended to every
// task(action=commit) return (P3). Below the escalation mark it is a plain
// report; at or past it, the report carries elapsed, REMAINING, the mark and
// the expected action — the model has one move left (split via reframe/TOO_BIG)
// and the return value names it rather than leaving it to inference.
//
// Budget 0 = unlimited returns "" — the line is omitted entirely rather than
// rendered against an arbitrary denominator (0 = unlimited keeps meaning
// unlimited, in the prompt and in this return value alike). A nil clock
// (Agent literals in tests) reports nothing: there is no elapsed to report.
func (a *Agent) sessionBudgetLine() string {
	st, ok := a.sessionBudgetStatus()
	if !ok {
		return ""
	}
	if !st.PastMark {
		return fmt.Sprintf(sessionBudgetReportFmt, st.Elapsed, st.Budget, st.Percent)
	}
	return fmt.Sprintf(sessionBudgetEscalationFmt, st.Elapsed, st.Remaining, st.Budget, st.Percent,
		sessionBudgetWarnPercent, toolnames.ToolTaskReframe, taskverdict.TooBig)
}

// sessionBudgetRefusal decides whether the commit beat REFUSES this call. The
// gate is deliberately NARROW (the operator's constraint: 强制不好 — do not
// over-enforce): it fires only when ALL of
//
//	the budget is finite and the clock exists (the same precondition as the report);
//	elapsed is at or past sessionBudgetWarnPercent of it;
//	the model's tracked task list still has at least one item that is not
//	  completed — i.e. this commit would ADD a further subtask;
//	the run has NOT already filed its verdict (a reframed run is executing the
//	  split and must be allowed to keep committing, or the gate would deadlock
//	  the very exit it names).
//
// Everything else keeps the pre-existing behaviour: budget 0 (unlimited), a nil
// clock, a below-the-mark session, a commit that CLOSES the last open item (the
// "land the work" exit) and a no-list single-step session all pass through with
// only the plain report line. Returns the refusal text and true when the call
// must be refused.
func (a *Agent) sessionBudgetRefusal() (string, bool) {
	if a == nil || !sessionBudgetCommitRefusal() {
		return "", false
	}
	st, ok := a.sessionBudgetStatus()
	if !ok || !st.PastMark {
		return "", false
	}
	if a.reframeVerdict != "" {
		return "", false // the split is already filed — let the run execute it
	}
	if !a.hasOpenTrackedItems() {
		return "", false // no tracked list, or every item is completed: nothing further to add
	}
	return fmt.Sprintf(sessionBudgetRefusalFmt, sessionBudgetWarnPercent, st.Elapsed, st.Remaining,
		st.Budget, st.Percent, toolnames.ToolTaskReframe, taskverdict.TooBig), true
}

// hasOpenTrackedItems reports whether the model's harness-tracked task list
// exists and still carries an item that is not completed — the "would ADD a
// further subtask" test. A session with no tracked list (a single-step run, or
// a run that never emitted [TASK_LIST]) answers false: there is no subtask
// bookkeeping to refuse, and the pre-existing behaviour must survive untouched.
func (a *Agent) hasOpenTrackedItems() bool {
	if a == nil || a.TaskState == nil {
		return false
	}
	for _, item := range a.TaskState.view() {
		item.Resolve()
		if !item.IsCompleted() {
			return true
		}
	}
	return false
}

// stuckState tracks the consecutive no-tool-use same-text streak (loop
// deadlock epic ①). Single-threaded: read/written only from the Run loop,
// mirroring the nudge/dead-end latch pattern.
type stuckState struct {
	streak int    // consecutive qualifying rounds so far
	prefix string // first stuckTextPrefixChars runes of the last qualifying round's text
}

// observe feeds one round into the detector. A round qualifies when it has
// NO tool_use and non-empty text; it matches the streak when its
// stuckTextPrefixChars-rune prefix equals the previous qualifying round's
// prefix. tool_use or divergent text RESETS the streak (real work is never
// penalized). Empty-text no-tool rounds (pure thinking / whitespace rounds)
// neither count nor reset — they carry no signal.
func (s *stuckState) observe(text string, hasToolUse bool) {
	if hasToolUse {
		s.streak, s.prefix = 0, ""
		return
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return // no signal either way
	}
	r := []rune(trimmed)
	p := stuckTextPrefixChars
	if len(r) < p {
		p = len(r)
	}
	prefix := string(r[:p])
	if s.prefix != "" && prefix == s.prefix {
		s.streak++
	} else {
		s.streak = 1
	}
	s.prefix = prefix
}

// tripped reports whether the streak has reached maxStuckRounds.
func (s *stuckState) tripped() bool { return s.streak >= maxStuckRounds }

// summary renders the streak state for the notify payload / stderr log:
// the last N qualifying prefixes so an operator sees WHAT the loop was
// repeating. Best-effort observability, never an error.
func (s *stuckState) summary() string {
	return fmt.Sprintf("streak=%d prefix=%q", s.streak, s.prefix)
}

// notifyLoopStuck fires the GOPI_TASK_NOTIFY pipeline with the synthetic
// loop-stuck event (sibling of "stale"): same delivery machinery, operator
// subscribes via GOPI_TASK_NOTIFY_EVENTS=loop-stuck (or "all"). Delivered
// through tools.NotifySyntheticEvent so the agent package needs no task-DB
// plumbing; failures are logged, never fatal (side-channel contract).
func (a *Agent) notifyLoopStuck(reason string) {
	if a.SessionLog != nil {
		tools.NotifySyntheticEvent(notifyEventLoopStuck, a.SessionLog.SessionID(),
			a.Model, fmt.Sprintf("agent loop stuck: %s", reason))
	} else {
		tools.NotifySyntheticEvent(notifyEventLoopStuck, "", a.Model,
			fmt.Sprintf("agent loop stuck: %s", reason))
	}
}

// stuckToolfulRounds resolves the toolStreak threshold from the env var —
// the ONLY reader of GOPI_STUCK_TOOLFUL_ROUNDS (Rule 2). Unset → default
// (10), "0" → disabled (0 rounds can never be reached), positive → override,
// invalid → default + stderr warning (mirrors sessionWallclockCap).
func stuckToolfulRounds() int {
	v := strings.TrimSpace(os.Getenv(stuckToolfulRoundsEnv))
	if v == "" {
		return maxStuckToolfulRounds
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "[gopi] warning: invalid %s=%q, using default %d\n",
			stuckToolfulRoundsEnv, v, maxStuckToolfulRounds)
		return maxStuckToolfulRounds
	}
	return n // 0 = disabled
}

// toolStreakState is the SECOND stuck detector (bug055 / Bond 718319): the
// monitor-loop blind spot of the first counter. Where stuckState catches
// consecutive no-tool rounds with identical text, toolStreakState catches
// consecutive rounds whose TOOL_USE is identical — same tool name, same
// normalized parameter hash, same text prefix. The incident loop (`bash
// sleep 115; ssh hp 'sqlite3 …'` 272×) had a tool_use every round, so the
// first counter's tool_use-reset arm silently zeroed it forever.
//
// Round fingerprint: (tool name, parameter-hash, text-prefix). "Same
// parameters" is deliberately NORMALIZED — the incident's ssh command was
// byte-identical; a polling loop that varies a counter/timestamp in the
// params still reads as the same fingerprint only if the TEXT prefix also
// matches (the loop's surrounding chatter is usually identical too).
// Parameter change resets (real work varies params every round — that is
// the point: sleep 115 + fixed ssh = identical hash every time).
type toolStreakState struct {
	streak    int    // consecutive identical-tool_use rounds so far
	toolName  string // tool name of the last qualifying round
	paramHash string // normalized parameter hash of the last qualifying round
	textPref  string // first stuckTextPrefixChars runes of the last qualifying round's text
	enabled   bool   // false when the threshold is 0 (env-disabled)
	threshold int    // rounds needed to trip (maxStuckToolfulRounds default)

	// Polling-tool exemption (bug055 follow-up, t-1788306421/…): a Bond
	// waiting on a background job polls job(action=output, job_id=…) with
	// the SAME arguments every round — byte-identical tool_use is BY DESIGN
	// for polling tools, so the pure fingerprint signal falsely kills
	// legitimate waits. For polling rounds the stuck signal is instead the
	// POLLED TARGET's state: identical RESULT text N times = the target is
	// frozen (genuinely stuck: a completed-job poll, the 718319 sqlite
	// SELECT shape); a CHANGING result (elapsed ticking, status flip, new
	// job id) = the Bond waits on a live, finite external change = alive.
	pollChain      int    // consecutive polling rounds so far (0 = last round was not a poll)
	lastPollResult string // sha256 of the last executed polling round's results
	hasPollResult  bool   // a polling round has executed at least once
}

// newToolStreakState builds a toolStreakState with the env-resolved
// threshold; threshold 0 disables the detector (GOPI_STUCK_TOOLFUL_ROUNDS=0).
func newToolStreakState() *toolStreakState {
	return &toolStreakState{enabled: stuckToolfulRounds() > 0, threshold: stuckToolfulRounds()}
}

// toolFingerprint hashes one round's tool_use into (name, params, textPref):
// the identity a repeated monitor loop must keep byte-identical. Multiple
// tool_use blocks in a round are joined (an ordered concatenation) so a
// round is ONE fingerprint — a round is "the same" only if its whole
// tool_use sequence is the same. Parameter normalization: a single
// deterministic hash of the JSON-serialized arguments map (json.Marshal on
// a map sorts keys — devdoc/resume-feasibility.md:152 — so argument order
// cannot mask identity; only VALUE differences change the hash). The text
// prefix uses the same 50-rune window as stuckState.
func toolFingerprint(toolCalls []deepseek.ToolCall, text string) (name string, params string, textPref string) {
	var names []string
	h := sha256.New()
	for _, tc := range toolCalls {
		names = append(names, tc.Name)
		if b, err := json.Marshal(tc.Arguments); err == nil {
			h.Write(b)
		} else {
			h.Write([]byte(tc.Name)) // best-effort: name alone on marshal failure
		}
		h.Write([]byte{0})
	}
	r := []rune(strings.TrimSpace(text))
	p := stuckTextPrefixChars
	if len(r) < p {
		p = len(r)
	}
	return strings.Join(names, "+"), fmt.Sprintf("%x", h.Sum(nil)), string(r[:p])
}

// isPollingRound reports whether the round's tool_use includes a polling
// tool (pollingTools) — a tool whose contract is "same args, check external
// state, return current state". Only such rounds qualify for the
// result-state exemption; everything else keeps the pure fingerprint signal.
func isPollingRound(toolCalls []deepseek.ToolCall) bool {
	for _, tc := range toolCalls {
		if pollingTools[tc.Name] {
			return true
		}
	}
	return false
}

// observe feeds one round into the detector. A round qualifies when it has
// >=1 tool_use and its fingerprint equals the previous qualifying round's
// (identical tool name + parameter hash + text prefix). A differing
// fingerprint (real work — parameters changed) or a no-tool round RESETS
// the streak. No-tool rounds carry no tool signal: they neither count nor
// reset (sibling semantics of stuckState's empty-text rounds) — a mixed
// monitor loop (tool round, text round, tool round…) still accumulates.
//
// Polling rounds (job action=output / job action=start) are DEFERRED here:
// the fingerprint is recorded, but the streak is decided by observeResults
// once the round's results exist — a poll's identical tool+args is
// legitimate waiting, only the polled state's identity is a stuck signal. A
// non-polling round between two polling rounds resets pollChain, so a
// frozen-poll streak never survives a real-work round.
func (s *toolStreakState) observe(toolCalls []deepseek.ToolCall, text string) {
	if !s.enabled || len(toolCalls) == 0 {
		return // disabled, or no tool signal this round
	}
	name, hash, pref := toolFingerprint(toolCalls, text)
	if isPollingRound(toolCalls) {
		s.toolName, s.paramHash, s.textPref = name, hash, pref
		s.pollChain++
		return // state change signal decided in observeResults
	}
	// Non-polling round: pure fingerprint accumulation (718319 shape).
	s.pollChain = 0
	if name == s.toolName && hash == s.paramHash && pref == s.textPref {
		s.streak++
	} else {
		s.streak = 1
	}
	s.toolName, s.paramHash, s.textPref = name, hash, pref
}

// observeResults feeds the round's EXECUTED results into the detector —
// the polling-tool exemption's state signal. Called once per round after
// tool execution, only meaningful for polling rounds: the polled target's
// RESULT identity is the stuck signal. A byte-identical result to the
// previous poll → the external state did not change → accumulate (frozen
// poll = genuinely stuck: completed-job poll, unchanged output). A changed
// result (elapsed ticking on a still-running job, a new job id, a status
// flip) → the Bond is waiting on a live change → reset to 1. Non-polling
// rounds never reach here (their signal is the fingerprint in observe).
func (s *toolStreakState) observeResults(toolCalls []deepseek.ToolCall, results []string) {
	if !s.enabled || len(toolCalls) == 0 || !isPollingRound(toolCalls) {
		return
	}
	h := sha256.New()
	for _, r := range results {
		h.Write([]byte(r))
		h.Write([]byte{0})
	}
	cur := fmt.Sprintf("%x", h.Sum(nil))
	// Chain continuity: only consecutive polling rounds share one streak —
	// a real-work round between polls resets pollChain in observe(), so an
	// identical result after intervening work starts a fresh count.
	if s.hasPollResult && s.pollChain >= 2 && cur == s.lastPollResult {
		s.streak++
	} else {
		s.streak = 1
	}
	s.lastPollResult, s.hasPollResult = cur, true
}

// tripped reports whether the streak has reached the threshold.
func (s *toolStreakState) tripped() bool {
	return s.enabled && s.streak >= s.threshold
}

// summary renders the streak state for the notify payload / stderr log.
// Best-effort observability, never an error.
func (s *toolStreakState) summary() string {
	return fmt.Sprintf("streak=%d tool=%q paramhash=%s text=%q", s.streak, s.toolName, s.paramHash, s.textPref)
}

// toolNames extracts the tool names of one round's tool_use blocks — the input
// the observation bound's class predicate works on (a round is classified by
// the SET of names it carries, not by its first call).
func toolNames(toolCalls []deepseek.ToolCall) []string {
	if len(toolCalls) == 0 {
		return nil
	}
	names := make([]string, 0, len(toolCalls))
	for _, tc := range toolCalls {
		names = append(names, tc.Name)
	}
	return names
}

// observationRounds resolves the observation bound's threshold from the env
// var — the ONLY reader of GOPI_STUCK_OBSERVE_ROUNDS (Rule 2). Unset →
// default (maxObservationRounds), "0" → DISABLED (0 rounds can never be
// reached), positive → override, invalid → default + stderr warning
// (mirrors sessionWallclockCap / stuckToolfulRounds, including the
// "a typo'd net must not silently disarm itself" rule).
func observationRounds() int {
	v := strings.TrimSpace(os.Getenv(observationRunEnv))
	if v == "" {
		return maxObservationRounds
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "[gopi] warning: invalid %s=%q, using default %d\n",
			observationRunEnv, v, maxObservationRounds)
		return maxObservationRounds
	}
	return n // 0 = disabled
}

// observationState is the THIRD stuck detector — the LOOK-WITHOUT-FIXING
// bound (§6.3 of devdoc/cli-as-eyes-and-hands-feasibility.md). It is the
// net for the one loop shape the other two are structurally blind to:
//
//   - detector 1 (stuckState) counts consecutive rounds with NO tool_use — a
//     round that observes always carries a tool_use, so the counter is reset
//     by the very act of looking;
//   - detector 2 (toolStreakState) counts byte-identical tool_use
//     fingerprints and EXEMPTS polling rounds whose RESULT changes — a live
//     screen/file/job output changes every round, which that exemption reads
//     as "the model is waiting on live state" (correct for bug055's job
//     polls, blind for an observation loop).
//
// So the missing quantity is not sameness of TOOL, it is REPETITION of
// round — and the repeat judgment (added below, in the JUDGED QUANTITY
// block) is what the streak now counts. The reset rules it adds:
//
//   - DIFFERENT FINGERPRINT (another file, another keyword, another
//     command) → streak 0: a non-repeated observation is not billed.
//   - CHANGED RESULT (same call, live state) → streak 0: the bug055
//     exemption, INHERITED from detector ②, not re-derived here.
//   - observationToolClass still supplies the class test, and its `false`
//     arm (mutator / polling / unknown) remains an ACT; its `true` arm is
//     now only the ENTRY CONDITION for the repeat judgment.
//
// N consecutive rounds
// that only looked, with nothing mutated between them. Five years of
// "look again, learn something, fix nothing" is invisible to (1) and (2) and
// is bounded here.
//
// Reset semantics (each one is a regression guard, not a courtesy):
//
//   - MUTATOR tool in the round (write/edit/task/task_submit/memory/text2img)
//     → streak 0 AND the post-work exemption (s.exempt) is re-armed for
//     maxObservationRounds×2 LOOK rounds. The action class the epic names
//     resets the bound; the exemption is what keeps the VERIFICATION burst
//     that follows real work (edit → build → test → read → read → …) from
//     being billed as a look-without-fixing loop. It is measured in look
//     ROUNDS, so the total a run survives after a mutation is 3N — linear,
//     not quadratic in the threshold.
//   - `job` (polling) round → ACT, not a look: detector 2 owns polling
//     semantics (its changing-result exemption is load-bearing, bug055), so
//     this bound must not double-bill a legitimate wait. A poll resets the
//     streak exactly like a mutation, and the real work that job performs
//     still re-arms the exemption (the `job action=start` round is an ACT
//     here too — hence job's absence from observerTools).
//   - no-tool round → ENDS the streak (measured; see observe). A round spent
//     talking is not a round spent looking, and the alternative (passthrough)
//     was measured to bill long productive research sessions.
//   - UNKNOWN tool name → treated as an act (its class has not been
//     reasoned about; see toolclass.go) — the safe direction: the bound
//     never widens itself over a tool the tree grows later.
//
// THE JUDGED QUANTITY IS REPEAT, NOT TOOL CLASS (fix for task 01a0faac,
// field task 01a0f8c9). The first shipped version of this detector billed
// every round whose tool_use was all-observer — a criterion with no content
// in it: reading /a.go, then /b.go, then /c.go is forty DIFFERENT acts of
// investigation and was billed as forty identical ones, so a legitimate
// evidence-gathering run was TRUNCATED at 40 with zero output (01a0f8c9 died
// that way four times). What the bound is actually for is REPETITION, so the
// round is now judged by the SAME question detector ② asks — "is this the
// same round again?" — plus the SAME exemption bug055 established: an
// observation may accumulate ONLY when
//
//	(1) its fingerprint repeats (tool name + normalized param hash + text
//	    prefix, from the ONE existing comparator toolFingerprint — Rule 14,
//	    no second implementation), AND
//	(2) its EXECUTED result is byte-identical to the previous observation
//	    round's result.
//
// Either one differing is a fresh observation and starts the count over:
// a different file/keyword/argument (real investigation) or a CHANGING
// result (a poll, a log being tailed, a screen that moves) is evidence the
// run is doing something, never evidence of a loop. Detector ②'s
// changing-result exemption is the semantics this detector INHERITS
// (bug055 is load-bearing; the sibling is not re-derived here).
//
// The threshold is UNCHANGED (maxObservationRounds=40) and no new escape
// hatch is added: the fix is in the criterion, not in the number.
//
// OFFSET, stated so nobody reads it as an off-by-one: the count is a count of
// REPEATS, and the first observation of a run has nothing to compare against,
// so the Nth repeat arrives on the (N+1)th identical round — the bound fires
// one round LATER than the class criterion did. That direction is deliberate
// (constraint: a false negative beats a false positive) and it is pinned by
// TestAgent_ObservationLoop_RedProofB_SameCallSameResultTrips, which asserts
// the trip at maxObservationRounds+1 rather than at the bare threshold.
type observationState struct {
	streak    int  // consecutive REPEATED observations so far
	looks     int  // looks observed since the last ACT (summary only)
	exempt    int  // look rounds still exempt (post-work verification burst)
	enabled   bool // false when the threshold is 0 (env-disabled)
	threshold int  // rounds needed to trip (maxObservationRounds default)

	// Repeat judgment. The pre-switch feed (observe) records an
	// observation round's identity; the post-execution feed (observeResults)
	// compares it against the previous observation round and decides the
	// streak. `pending` marks a recorded-but-not-yet-decided round, so a
	// round whose tools never execute (a continue path) cannot be billed.
	pending    bool   // an observer round is recorded, awaiting its results
	pendName   string // its toolFingerprint: tool name(s)
	pendParams string // its toolFingerprint: normalized parameter hash
	pendPref   string // its toolFingerprint: 50-rune text prefix
	lastName   string // the previous DECIDED observation round's fingerprint
	lastParams string
	lastPref   string
	lastResult string // sha256 of the previous observation round's results
	hasLast    bool   // a previous observation round exists to compare with
}

// newObservationState builds an observationState with the env-resolved
// threshold; threshold 0 disables the detector (GOPI_STUCK_OBSERVE_ROUNDS=0).
func newObservationState() *observationState {
	n := observationRounds()
	return &observationState{enabled: n > 0, threshold: n}
}

// observe feeds one round of the loop into the bound. It is called from the
// SAME single feed point as detectors 1 and 2 (immediately after response
// extraction, before the finish_reason switch), so no continue path — the
// truncated round, the end-turn nudge, the completion checkpoint — can
// bypass it.
//
// It decides the ACT arms (a mutator, the polling tool, an unknown name, or
// no tool_use at all — all of them end or never start the streak) and, for
// an observation-only round, it only RECORDS the round's identity
// (toolFingerprint, the ONE existing comparator — Rule 14). The streak
// itself is decided in observeResults, once the round's results exist: a
// repeat is "same call AND same result", and until the result is known there
// is no evidence the round repeated anything. A round that is recorded and
// never receives results therefore cannot be billed at all — the safe
// direction (constraint 6: a false negative beats a false positive).
func (s *observationState) observe(toolCalls []deepseek.ToolCall, text string, hasToolUse bool) {
	if !s.enabled {
		return
	}
	s.pending = false // a round that does not qualify below leaves nothing awaited
	if !hasToolUse {
		// A no-tool round ENDS the streak (measured decision, not taste): a
		// round the model spent talking is not a round it spent looking, and
		// letting such rounds pass through unbilled glues two separate
		// research windows into one streak. Measured over the corpus, that
		// "passthrough" variant is what makes the bound bill long productive
		// sessions (150 of 2317 would trip at N=24; the same variant's
		// longest glued streak is 139 rounds). The cost of resetting is the
		// residual gap named in the devdoc: a model that narrates between
		// looks restarts the count, so a NARRATING look-without-fixing loop
		// is caught only when the narration is itself identical (detector 1).
		s.reset()
		return
	}
	observeOnly, act := observationToolClass(toolNames(toolCalls))
	if act {
		s.reset()
		s.exempt = 2 * s.threshold // post-work verification burst
		return
	}
	if !observeOnly {
		return // no signal either way
	}
	name, params, pref := toolFingerprint(toolCalls, text)
	s.pending, s.pendName, s.pendParams, s.pendPref = true, name, params, pref
	s.looks++
	// NOTE: the streak is NOT touched here. It is decided in observeResults
	// once the round's result exists — recording a candidate must not erase
	// the run of repeats it may extend (the defect caught by
	// TestObservationBound_StateUnit: resetting here made the bound
	// untrippable, i.e. the fix's own red proof failed).
}

// observeResults decides a recorded observation round ONCE its results exist
// — the second half of the repeat judgment (the call half is the fingerprint
// in observe). Accumulate only when BOTH halves repeat:
//
//	same fingerprint as the previous observation round, AND
//	byte-identical result to that round.
//
// Anything else is a fresh observation: a different argument (another file,
// another keyword) or a result that MOVED (a tailed log, a re-read screen,
// live state). Both are work, and neither is billed — this is detector ②'s
// bug055 semantics applied to the observation class, which is what the epic
// requires (detector 3 inherits the exemption instead of re-deriving it).
//
// Exemption accounting lives here for the same reason: the 2N post-work
// burst must be spent on observations that were actually performed and whose
// results arrived, not on rounds that were merely proposed.
func (s *observationState) observeResults(results []string) {
	if !s.enabled || !s.pending {
		return
	}
	s.pending = false
	name, params, pref := s.pendName, s.pendParams, s.pendPref
	cur := observationResultHash(results)
	// The repeat test is the CALL (tool name + normalized arguments) plus the
	// RESULT — the round's PROSE is deliberately not part of it. Reason: the
	// fingerprint's text prefix is detector ②'s own discriminator, and a
	// comparison that required it too would make detector ③ fire only after
	// detector ② had already fired at its lower threshold (a strictly smaller
	// subset — dead code, not a second net). Excluding it gives ③ the shape
	// ② cannot see: the SAME look repeated with the SAME unchanged result
	// while the model narrates differently each round. The epic's wording is
	// this criterion exactly ("same command + same arguments + result no
	// longer changing").
	sameCall := s.hasLast && name == s.lastName && params == s.lastParams
	sameResult := s.hasLast && cur == s.lastResult
	if s.exempt > 0 {
		s.exempt--
		s.streak = 0
	} else if sameCall && sameResult {
		s.streak++
	} else {
		// A different call (real investigation) or a moving result (live
		// state, polling, a tail) — the run is doing something. Streak 0 AND
		// the previous-round reference is dropped, so the next repeat is
		// judged against THIS round and needs its own two matching halves
		// (mirrors toolStreakState: a changed parameter resets to 1).
		s.streak = 0
	}
	s.lastName, s.lastParams, s.lastPref = name, params, pref
	s.lastResult, s.hasLast = cur, true
}

// observationResultHash is the RESULT half of the repeat judgment: one hash
// over the round's executed results in tool-call order (the same shape
// toolStreakState.observeResults uses for its polling rounds — an ordered
// hash, so a reordered batch is a different round).
func observationResultHash(results []string) string {
	h := sha256.New()
	for _, r := range results {
		h.Write([]byte(r))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// reset clears the streak AND the repeat reference: nothing is awaiting
// results, and the next observation round starts a fresh comparison.
func (s *observationState) reset() {
	s.streak = 0
	s.pending = false
	s.looks = 0
	s.hasLast = false
	s.lastName, s.lastParams, s.lastPref, s.lastResult = "", "", "", ""
}

// tripped reports whether the run has looked without acting long enough.
// Pure (no side effects) so a test can ask the question twice.
func (s *observationState) tripped() bool {
	return s.enabled && s.streak >= s.threshold
}

// summary renders the reason line for the notify payload and the error.
func (s *observationState) summary() string {
	return fmt.Sprintf("streak=%d looks=%d threshold=%d repeated=%s", s.streak, s.looks, s.threshold, s.lastName)
}

// notifyLoopStuckObservation fires the GOPI_TASK_NOTIFY pipeline with the
// loop-stuck-observation event (third sibling of loop-stuck /
// loop-stuck-toolful): same machinery — an operator subscribes via
// GOPI_TASK_NOTIFY_EVENTS=loop-stuck-observation (or "all", which enumerates
// the registry this name is declared in).
func (a *Agent) notifyLoopStuckObservation(reason string) {
	if a.SessionLog != nil {
		tools.NotifySyntheticEvent(notifyEventStuckObservation, a.SessionLog.SessionID(),
			a.Model, fmt.Sprintf("agent loop stuck (observation without act): %s", reason))
	} else {
		tools.NotifySyntheticEvent(notifyEventStuckObservation, "", a.Model,
			fmt.Sprintf("agent loop stuck (observation without act): %s", reason))
	}
}

// notifyLoopStuckToolful fires the GOPI_TASK_NOTIFY pipeline with the
// loop-stuck-toolful event (sibling of loop-stuck): same machinery, operator
// subscribes via GOPI_TASK_NOTIFY_EVENTS=loop-stuck-toolful (or "all").
func (a *Agent) notifyLoopStuckToolful(reason string) {
	if a.SessionLog != nil {
		tools.NotifySyntheticEvent(notifyEventLoopStuckToolful, a.SessionLog.SessionID(),
			a.Model, fmt.Sprintf("agent loop stuck (identical tool_use): %s", reason))
	} else {
		tools.NotifySyntheticEvent(notifyEventLoopStuckToolful, "", a.Model,
			fmt.Sprintf("agent loop stuck (identical tool_use): %s", reason))
	}
}

// NewAgent creates a new agent with a Claude Messages API client.
// splitEffortValuesEnv parses the GOPI_EFFORT_VALUES env var (a
// comma-separated list injected by config.ApplyEnv from the settings.json
// "effort_values" array). Empty/whitespace entries are dropped; nil means
// "no list" → the resolver falls back to the DeepSeek baseline.
func splitEffortValuesEnv(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, v := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func NewAgent(model string) (*Agent, error) {
	return NewAgentResuming(model, RunIdentity{})
}

// NewAgentResuming is NewAgent for a run that CONTINUES an existing session:
// id.SessionID is reused instead of minted, and id.RawLogPath (when set) is
// appended to instead of created.
//
// THE MINT IS THE WHOLE DEFECT (epic: `gopi chat --resume <id>` minted a second
// session). Everything else NewAgent does — client, model, window, prompt,
// tools — is a function of the model, not of the identity, so the identity is the
// ONLY parameter this variant threads; a second constructor that restated any of
// it would be the drift this repo's rules call "one question, two answers".
//
// The fresh-run path is byte-for-byte what it was: RunIdentity{} means "mint a
// uuid, create a new raw log", which is the zero-value semantics RunIdentity's
// doc states, so every other face keeps its behaviour without being able to
// observe this seam.
func NewAgentResuming(model string, id RunIdentity) (*Agent, error) {
	client, err := deepseek.NewClaudeClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create DeepSeek Claude client: %w", err)
	}

	// Session-dir-unify S1 layout decision: a `task run` CHILD (the wrapper
	// exported GOPI_TASK_WRAPPER_ID pre-spawn — see taskregister.go) logs
	// into sessions/<task_id>/, a bare prompt/chat session into
	// sessions/<session-uuid>/. The run uuid (session id) is minted ONCE and
	// shared by the raw logger and the session logger so both files land in
	// the same run directory with matching names.
	//
	// A RESUMED RUN MINTS NOTHING: the id is the continued session's, so both
	// files land in the directory that session already owns and grow in place.
	taskID := strings.TrimSpace(os.Getenv(tools.TaskWrapperEnv))
	runUUID := id.SessionID
	if !id.resuming() && runUUID == "" {
		runUUID = mintRunID(taskID)
	}
	// THE WITNESS THE CALLER DID NOT THREAD (epic: land the resume witness on
	// the RE-DISPATCH door). A re-dispatched row's executor child is spawned by
	// the wrapper, so the door that KNOWS "this row already ran" and the process
	// that writes the record are two different processes — the only channel
	// between them is the child's environment, and tools.TaskRunResumeFrom is
	// its ONE reader (Rule 21).
	//
	// IT IS A FALLBACK, NEVER AN OVERRIDE: an identity the caller threaded
	// explicitly (`session resume`, the chat --resume faces) always wins, so a
	// child that inherited a stale variable from its parent cannot rewrite a
	// resume that was declared in code. The value is only consulted for a run
	// that is NOT already resuming — i.e. exactly the shape this defect was.
	resumedFrom := id.ResumedFrom
	if resumedFrom == "" && taskID != "" {
		resumedFrom = tools.TaskRunResumeFrom()
	}

	// THE CONTINUED RECORD IS INSPECTED BEFORE ANYTHING IS TOUCHED (epic
	// 01a19434 — the refusal that promised more than it delivered).
	//
	// refuseUnterminatedRecordTail refuses to append onto a record that ends
	// mid-line, and its own text says why: "The session cannot be continued
	// until the record is examined". That promise was FALSE in the resumed
	// path, because this constructor went on to open the RAW log (appending a
	// resume header to it) and the caller went on to spend a full model round —
	// measured 2026-11-01 on a 405,504-byte torn record: exit 0, `ok`, 1,139,114
	// bytes of raw HTTP log, and ZERO bytes written into any session.jsonl. The
	// run's whole transcript existed in the wire log and in no record at all,
	// which is the second defect the guard exists to prevent.
	//
	// SO THE REFUSAL IS TAKEN HERE, at the LAST moment before this run has any
	// side effect: the raw logger below APPENDS to the continued run's wire log
	// (writeRawLogResumeHeader goes straight to the file), so a check placed
	// only at the record's own open would leave the refusal costing a header
	// line in a file the operator did not ask to grow. A refused resume leaves
	// the run's assets exactly as it found them — the same promise
	// `gopi session resume` already makes about the LEDGER ROW
	// (TestResumeRefusedLeavesTheRowAlone).
	//
	// IT IS THE SAME PREDICATE, NOT A SECOND OPINION (Rules 13/14): the
	// writer-side guard inside newSessionLoggerOnFile still runs unchanged, and
	// this call cannot disagree with it — one function answers "is this record
	// continuable". What the second call buys is ORDER: the answer arrives
	// before the first byte is appended anywhere.
	//
	// FRESH RUNS CANNOT REACH IT: the condition is id.SessionLogPath != "", the
	// one door to "continue the record that EXISTS" (RunIdentity's doc, row
	// 01a17dd8). RunIdentity{} — every other face — skips it entirely.
	if id.SessionLogPath != "" {
		if err := refuseUnterminatedRecordTail(id.SessionLogPath); err != nil {
			return nil, err
		}
	}

	rawLog, rawLogErr := NewRawLoggerForResumedRun(taskID, RunIdentity{
		SessionID:   runUUID,
		ResumedFrom: resumedFrom,
		RawLogPath:  id.RawLogPath,
	})
	if rawLogErr != nil {
		fmt.Fprintf(os.Stderr, "[gopi] raw log: %v\n", rawLogErr)
	}

	// Wire raw HTTP logging into the DeepSeek client.
	if rawLog != nil {
		client.RawLog = rawLog.LogFunc()
	}

	var rawLogPath string
	if rawLog != nil {
		rawLogPath = rawLog.Path()
	}
	var sessionLog *SessionLogger
	var sessionErr error
	switch {
	case id.SessionLogPath != "":
		// THE CONTINUED RECORD, BY PATH (row 01a17dd8). A resumed run must
		// append to the JSONL that EXISTS. Deriving it from the id is only
		// correct in the BARE layout, because the task-run layout's directory
		// comes from GOPI_TASK_WRAPPER_ID — an env var a resume child does not
		// carry — so an id-derived path appends to sessions/<uuid>/session.jsonl
		// and leaves the row's own sessions/<task-id>/<uuid>.jsonl untouched
		// while a NEW directory appears: a resume that mints a second session.
		sessionLog, sessionErr = openResumeSessionRecord(id.SessionLogPath, model, runUUID, taskID, rawLogPath, resumedFrom)
	case taskID != "":
		// THE TASK-RUN BRANCH CARRIES THE WITNESS TOO (epic: land the resume
		// witness on the RE-DISPATCH door). It used to call the plain
		// NewSessionLoggerForRun, which hardcodes resumedFrom="" — so a row run
		// a second time appended a second session_start that said nothing about
		// why the file holds two, while every other resumed face recorded it.
		// The value comes from the DOOR through RunIdentity (never measured off
		// the file here: the raw log's own resume header is deleted by
		// `gopi session gc --trim-raw-log`, so inferring from it loses the
		// witness exactly when the record is trimmed).
		sessionLog, sessionErr = NewSessionLoggerForResumedRun(model, rawLogPath, runUUID, []string{taskID}, resumedFrom)
	default:
		// The bare branch MUST pass the SAME runUUID as the raw logger. Going
		// through NewSessionLogger here mints a SECOND uuid, so one bare
		// session scattered its assets across two directories: the raw log in
		// sessions/<uuid-A>/ and the JSONL in sessions/<uuid-B>/ (measured
		// 2026-09-12 on the live profile — 119/300 sampled bare sessions, 0
		// co-located). Bare sessions pass no taskID, which is what selects the
		// sessions/<session-uuid>/ layout.
		sessionLog, sessionErr = NewSessionLoggerForResumedRun(model, rawLogPath, runUUID, nil, resumedFrom)
	}
	if sessionErr != nil {
		// RULE 15 ON THE CONTINUE-THE-RECORD DOOR (epic 01a19434).
		//
		// A run that STARTS a session may go on without a record: session
		// logging is documented best-effort ("nil is returned with an error that
		// the caller can log and continue without"), and that contract is
		// untouched here — every fresh face still prints and continues.
		//
		// A run that CONTINUES one may NOT. Its record IS the session: the
		// caller asked to append to that file, and continuing with no record
		// produces exactly the measured shape this epic closes — a full model
		// round spent, its transcript present only in the raw HTTP log, and a
		// session.jsonl that says nothing happened. The refusal's own text
		// promises "the session cannot be continued until the record is
		// examined"; this line is that promise, and it is spent BEFORE the
		// caller reaches the first round.
		//
		// (The TORN-TAIL case never gets here: it is answered even earlier, by
		// the pre-open check above, so not one byte of the run's raw log has
		// grown. What lands here is the rest of the family — the directory gone,
		// the open refused — which used to be swallowed the same way.)
		if id.SessionLogPath != "" {
			if rawLog != nil {
				_ = rawLog.Close() //stylegate:ignore close on the refusal path; the error being returned is the one that matters
			}
			return nil, sessionErr
		}
		fmt.Fprintf(os.Stderr, "[gopi] session: %v\n", sessionErr)
	}

	// Context window: default to DeepSeek 1M context with headroom,
	// overridable via GOPI_CONTEXT_WINDOW.
	window := DefaultContextWindow
	if v := os.Getenv("GOPI_CONTEXT_WINDOW"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			window = n
		} else {
			fmt.Fprintf(os.Stderr, "[gopi] warning: invalid GOPI_CONTEXT_WINDOW=%q, using default %d\n", v, window)
		}
	}

	// MaxTokens: per-response output cap sent as max_tokens in every request.
	// Default: deepseek.DefaultClaudeMaxTokens (16384 — A4 convergence, see
	// the constant's doc comment; measured sufficient for ~600-line single
	// write drafts with 2x headroom over the worst observed round). Override
	// via GOPI_MAX_TOKENS. The thinking budget scales with this cap (half),
	// so raising it never starves output.
	maxTokens := deepseek.DefaultClaudeMaxTokens
	if v := os.Getenv("GOPI_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		} else {
			fmt.Fprintf(os.Stderr, "[gopi] warning: invalid GOPI_MAX_TOKENS=%q, using default %d\n", v, maxTokens)
		}
	}

	// System prompt: hardcoded, immutable. Lists the tools plus the routing
	// rules that decide when each one is the right choice. The model plans
	// freely but is steered toward the cheapest correct tool — code_search
	// first for symbol definition lookups, never bash grep.
	// Assembled from ordered sections (P0-2): see assembleSystemPrompt.
	systemPrompt := assembleSystemPrompt()

	// Project context file (GOPI.md): OPTIONAL, resolved ONCE here at session
	// start by walking up from the process cwd — see projectctx.go for the
	// two-boundary model (the enclosing project, then the filesystem root)
	// and why the walk never re-reads. NOT an env var. NOT a config knob.
	// A run started from a SUBDIRECTORY of the project gets the project's
	// rules (the measured defect this closes); a run with no file anywhere
	// above it loads nothing, prints the directories it searched, and
	// proceeds.
	// File NEVER changes during task execution (prompt-cache byte stability).
	projectCtx := withEnvironmentBlock(LoadProjectContext(""))

	// Reasoning effort: DeepSeek V4 reasoning depth control (P0-1).
	// Default: "minimal" — light thinking budget, the measured sweet spot
	// (thinking-sweep 2026-08-21: W3 benchmark, deepseek-v4-flash). Minimal/low
	// thinking beats disabled on wall time (216s/239s vs 289s) because it cuts
	// round-trips (15/17 vs 23) despite slightly slower rounds (12.0s/10.0s vs
	// 8.6s median); medium/max are 2x+ slower with identical task quality.
	//
	// The value goes out as a REAL wire parameter (dead-config fix): enum
	// off|minimal|low|medium|high|max is sent as reasoning_effort on the
	// Anthropic-compatible endpoint. The endpoint ACCEPTS it and does NOT
	// honor it — neither reasoning_effort nor budget_tokens bounds thinking
	// there (measured 2026-08-21, re-measured 2026-09-15 and again
	// 2026-11-14: minimal produced MORE thinking chars than max, and a 64x
	// budget difference moved the output by 23%; the probe command and both
	// tables are in devdoc/thinking-budget.md, and the same conclusion sits
	// on the field itself in internal/deepseek/claude_client.go). So this
	// value selects whether thinking is ON — not how deep it goes; the only
	// bound is the harness's own abort threshold. "off" resolves to thinking
	// disabled and sends nothing (dsh resolveThinking semantics). Override
	// via GOPI_REASONING_EFFORT.
	//
	// PA ruling 2026-08-27 — effort and thinking are two knobs, one
	// resolution: an explicit thinking setting wins; an explicit VALID
	// non-off effort with no explicit thinking auto-enables thinking (effort
	// is the depth knob — asking for a depth implies wanting thinking); an
	// invalid effort falls back to minimal WITHOUT auto-enabling; nothing
	// set stays disabled. effortExplicit tracks the RAW env presence BEFORE
	// the fallback — the default is "minimal", so the env value must be the
	// discriminator, not the resolved value.
	reasoningEffort := "minimal"
	effortExplicit := false
	effortOff := false
	if v := os.Getenv(ReasoningEffortEnvName); v != "" {
		effortExplicit = true
		reasoningEffort = v
	}
	// effort_values (config-ization, no vendor mapping in code): the
	// requested effort is validated against the settings.json
	// "effort_values" list — the provider's own legal value range declared
	// as data (config.ResolveReasoningEffort: nil list → DeepSeek baseline;
	// listed values pass through VERBATIM — what you send is what goes; no
	// cross-protocol translation). A vendor that still rejects a listed
	// effort is handled at runtime by the minimal-core degradation ladder
	// (error_1214 rung 1 strips reasoning_effort and re-sends) — the error
	// path IS the mapping's runtime implementation. An unlisted effort
	// falls back to minimal WITHOUT auto-enabling thinking (same semantics
	// as the old hardcoded switch — the PA ruling's "invalid value does not
	// imply intent").
	effortValues := splitEffortValuesEnv(os.Getenv(EffortValuesEnvName))
	if err := config.ValidateReasoningEffortSettings(effortValues); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] config: %v (list ignored — baseline applies)\n", err)
		effortValues = nil
	}
	resolvedEffort, effortValid := config.ResolveReasoningEffort(reasoningEffort, effortValues)
	if !effortValid && effortExplicit {
		fmt.Fprintf(os.Stderr, "[gopi] warning: GOPI_REASONING_EFFORT=%q not in the provider's effort_values, using %q\n", reasoningEffort, resolvedEffort)
		effortExplicit = false
	}
	reasoningEffort = resolvedEffort
	effortOff = reasoningEffort == config.EffortOff

	// Thinking mode: DeepSeek V4 thinking toggle (enabled/disabled).
	// Default: DISABLED. The DeepSeek /anthropic endpoint enables thinking
	// by default when the field is omitted, and long tasks measured against
	// deepseek-v4-flash showed thinking eating the ENTIRE max_tokens budget,
	// returning empty content (qwen test) — so GoPi now sends an explicit
	// thinking=disabled unless the user opts in. The escape hatch:
	// settings.json "thinking": "enabled" / GOPI_THINKING=enabled /
	// --thinking enabled re-enables it (paired with GOPI_REASONING_EFFORT
	// for the depth knob). Invalid values warn and fall back to disabled.
	// GOPI_REASONING_EFFORT=off forces thinking disabled (effort owns the switch).
	//
	// Budget: GOPI_THINKING_BUDGET (tokens per round, default
	// deepseek.DefaultClaudeThinkingBudget). The API accepts budget_tokens but
	// does NOT enforce it (verified live 2026-08-21 — see
	// devdoc/thinking-budget.md), so the harness enforces it: chatOnce
	// aborts any round whose streamed thinking exceeds the budget and re-issues
	// it with thinking disabled.
	// Resolution (PA ruling 2026-08-27):
	//   1. Explicit GOPI_THINKING (settings "thinking" via ApplyEnv) wins —
	//      enabled/disabled as stated. GOPI_REASONING_EFFORT=off overrides
	//      even an explicit enabled (off wins, warning below).
	//   2. No explicit thinking + explicit valid non-off effort →
	//      thinking auto-enabled (effort implies thinking).
	//   3. Neither set → disabled (default).
	thinkingEnv, thinkingSet := os.LookupEnv(ThinkingEnvName)
	thinkingExplicit := thinkingSet && thinkingEnv != ""
	var thinking *deepseek.Thinking
	switch {
	case thinkingExplicit && thinkingEnv == thinkmode.Enabled:
		if effortOff {
			fmt.Fprintf(os.Stderr, "[gopi] warning: GOPI_REASONING_EFFORT=off overrides GOPI_THINKING=enabled — thinking disabled\n")
			thinking = &deepseek.Thinking{Type: thinkmode.Disabled}
		} else {
			thinking = &deepseek.Thinking{Type: thinkmode.Enabled}
		}
	case thinkingExplicit && thinkingEnv == thinkmode.Disabled:
		thinking = &deepseek.Thinking{Type: thinkmode.Disabled}
	case thinkingExplicit:
		fmt.Fprintf(os.Stderr, "[gopi] warning: invalid GOPI_THINKING=%q, using default (disabled)\n", thinkingEnv)
		thinking = &deepseek.Thinking{Type: thinkmode.Disabled}
	case EffortImpliesThinking(effortExplicit, effortOff):
		thinking = &deepseek.Thinking{Type: thinkmode.Enabled}
	default:
		thinking = &deepseek.Thinking{Type: thinkmode.Disabled}
	}
	// The internal field keeps the resolved effort (observability: the user
	// asked for minimal/high/... even when thinking is off); the wire only
	// carries it while thinking is enabled. "off" never reaches either (it
	// was resolved to thinking disabled above).
	internalEffort := reasoningEffort
	if effortOff {
		internalEffort = ""
	}
	if v := os.Getenv("GOPI_THINKING_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			thinking.BudgetTokens = n
		} else {
			fmt.Fprintf(os.Stderr, "[gopi] warning: invalid GOPI_THINKING_BUDGET=%q, using default %d\n", v, deepseek.DefaultClaudeThinkingBudget)
		}
	}

	// HideThinking: suppress live reasoning display (GOPI_HIDE_THINKING=1).
	// Thinking mode itself remains enabled — only the live print is hidden.
	// EnvBool truthy-only: GOPI_HIDE_THINKING=0/off/false must NOT hide
	// (the old `!= ""` inverted these — P1-4).
	hideThinking := config.EnvBool("GOPI_HIDE_THINKING")

	// KeepThinking: P2 thinking-trim switch (GOPI_KEEP_THINKING=1, A/B
	// control arm). Default OFF — assistant reasoning is stripped from the
	// request wire each round (see Agent.KeepThinking doc). Truthy-only
	// parse (P1-4), same convention as HideThinking.
	keepThinking := config.EnvBool("GOPI_KEEP_THINKING")

	// Timeout: global deadline for the agent loop.
	// Default 0 = unlimited; e.g. GOPI_TIMEOUT_SEC=300 for old 5min behavior.
	timeout := 0 * time.Second
	if v := os.Getenv("GOPI_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		} else {
			fmt.Fprintf(os.Stderr, "[gopi] warning: invalid GOPI_TIMEOUT_SEC=%q, using default %v\n", v, timeout)
		}
	}

	// No-list-yet nudge (P1.4/P2): N=NudgeRounds rounds with no list and
	// >=NudgeToolRounds tool rounds before the harness nudges. Env-overridable
	// for tests (defaults 5/3 per Rule 2; single read point here).
	nudgeRounds := defaultNudgeRounds
	if v := os.Getenv("GOPI_NUDGE_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			nudgeRounds = n
		}
	}
	nudgeToolRounds := defaultNudgeToolRounds
	if v := os.Getenv("GOPI_NUDGE_TOOLS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			nudgeToolRounds = n
		}
	}

	// Memory compaction (GOPI_COMPACT=off disables; default on): LLM-assisted
	// summary of the oldest messages when the context approaches the window.
	compactionEnabled := true
	if v := os.Getenv("GOPI_COMPACT"); v != "" {
		if strings.EqualFold(v, "off") || v == "0" || strings.EqualFold(v, "false") {
			compactionEnabled = false
		}
	}

	// THE AUTOMATIC REDUCTION PATH IS OPT-IN (context_pressure.go): off unless
	// GOPI_CONTEXT_AUTOREDUCE says otherwise, in which case a filled window is
	// WARNED about and nothing is removed. `compactionEnabled` above survives as
	// "which reduction to run", never as "whether".
	autoReduceEnabled := boolEnvTrue(AutoReduceEnv)

	// Compaction parameters (P0): trigger/target/keep-recent/budget/cooldown/
	// reference-only — all GOPI_COMPACT_* overridable, invalid values warn
	// and fall back to defaults (compactConfigFromEnv).
	cc := compactConfigFromEnv(window)

	// Interrupt root (chat-once/bare-prompt SIGTERM zombie fix): a
	// cancellable base the run-engine contexts derive from, so an external
	// Interrupt() call gracefully unwinds the in-flight run (session_end +
	// auto-row close via the normal defer) instead of the process dying on
	// SIGTERM with defers unrun. Production agents (this constructor) always
	// get one; hand-built test Agent{} literals fall back to Background() at
	// the run-ctx sites.
	interruptBase, interruptCancel := context.WithCancel(context.Background())

	ag := &Agent{
		Client:                  client,
		Ctx:                     &Context{ProjectContext: projectCtx},
		Model:                   model,
		MaxIterations:           0, // 0 = unlimited, like Pi-mono — model decides when to stop
		SystemPrompt:            systemPrompt,
		ReasoningEffort:         internalEffort,
		Thinking:                thinking,
		HideThinking:            hideThinking,
		KeepThinking:            keepThinking,
		Logger:                  log.New(os.Stderr, "[gopi] ", 0),
		Debug:                   boolEnvTrue("GOPI_DEBUG"),
		NoEnforceMemory:         boolEnvTrue("GOPI_NO_ENFORCE_MEMORY"),
		NoEnforceMemoryConsult:  boolEnvTrue("GOPI_NO_ENFORCE_MEMORY_CONSULT"),
		Clock:                   NewSessionClock(),
		Cost:                    &deepseek.CostTracker{},
		RawLog:                  rawLog,
		SessionLog:              sessionLog,
		TaskState:               newTodoState(),
		ContextWindow:           window,
		CompactionEnabled:       compactionEnabled,
		AutoReduceEnabled:       autoReduceEnabled,
		CompactTriggerRatio:     cc.TriggerRatio,
		TurnFoldRecentTurns:     turnFoldRecentTurnsFromEnv(),
		CompactTargetRatio:      cc.TargetRatio,
		CompactKeepRecent:       cc.KeepRecent,
		CompactMaxSummaryTokens: cc.MaxSummaryTokens,
		CompactCooldown:         cc.Cooldown,
		CompactReferenceOnly:    cc.ReferenceOnly,
		lastCompactSavingsPct:   100.0,
		MaxTokens:               maxTokens,
		Timeout:                 timeout,
		NudgeRounds:             nudgeRounds,
		NudgeToolRounds:         nudgeToolRounds,
		interruptBase:           interruptBase,
		interruptCancel:         interruptCancel,
		Hippocampus:             newHippocampusHost(loadHippocampusConfig(), nil),
		// The SAME read that selected the session-dir layout above: a
		// non-empty value means this process IS a `task run` child (its stdin
		// is a briefing), which RunPrompt consults to keep the Epic framing.
		taskChildRowID: taskID,
	}
	// Hippocampus production binding (epic child 4/4): the host above is only
	// the SCHEDULER — without the organ bound to it, an enabled host would
	// archive a "retrieval failed" marker every run and the feature could
	// never activate. bindHippocampusRecall is a no-op when the gate is off
	// (nil host ⇒ byte-identical default behaviour, devdoc/hippocampus.md
	// §4.2 口径 1) and reports any failure through the agent's own logger
	// instead of swallowing it (Rule 15).
	bindHippocampusRecall(ag, ag.Logger.Printf)
	// The tool-result cap (toolresult_cap.go) names THIS path in the note it
	// splices into an oversized result, so a model reading the excerpt is told
	// where the whole text was recorded. LogToolResult writes the FULL result
	// before AddToolResult applies the cap, so the file really does hold what
	// the note claims. Set here, in the ONE production constructor, so the task
	// loop and the chat ops loop both carry it.
	if sessionLog != nil {
		ag.Ctx.ToolResultSessionPath = sessionLog.Path()
	}
	return ag, nil
}

// Interrupt gracefully stops the in-flight run (runEngine / Chat / ChatOps):
// it cancels the interrupt root every run context derives from, so the run
// loop's round-boundary ctx.Done() check fires (or an in-flight LLM stream
// aborts) and the loop unwinds through its normal end-of-run defer — writing
// session_end (exit_code=1) and closing the auto task-ledger row. Safe to
// call from any goroutine (a signal handler, typically); idempotent.
// chat-once/bare-prompt SIGTERM zombie fix: signal handlers MUST call this
// instead of letting the process die, so the defer that writes session_end
// actually runs (cli_web bug003 pattern, extended from the web carrier to
// the run/chat paths).
func (a *Agent) Interrupt() {
	if a == nil {
		return
	}
	// Latched BEFORE cancel so the run loop (which reads the flag only after
	// ctx.Done() — the ctx channel's happens-after) always sees it.
	a.interrupted = true
	if a.interruptCancel != nil {
		a.interruptCancel()
	}
}

// interruptedBySignal reports whether Interrupt() fired (as opposed to the
// run ctx ending by wallclock cap or GOPI_TIMEOUT_SEC). Only meaningful AFTER
// the run ctx reports Done — the ctx channel's happens-after makes the plain
// bool race-free. Used by the run loops to return the signal-interrupt error
// text (and by tests to assert the interrupt path ran).
func (a *Agent) interruptedBySignal() bool {
	return a != nil && a.interrupted
}

// interruptRoot returns the base context every run-engine ctx derives from:
// the agent's interrupt root (cancelled by Interrupt()) when present, else
// context.Background() (hand-built test Agent{} literals have no root). The
// run-ctx sites (runEngine / Chat / ChatOps) layer their wallclock-cap /
// GOPI_TIMEOUT_SEC deadlines on this root — NEVER on Background directly —
// so an external Interrupt() reaches the in-flight run.
func (a *Agent) interruptRoot() context.Context {
	if a == nil || a.interruptBase == nil {
		return context.Background()
	}
	return a.interruptBase
}

// runScopedRoot returns the base context a run-engine ctx derives from: the
// process-wide interrupt root, with a cancel scoped to THIS run published on
// the agent for the duration of that run.
//
// WHY THE EXTRA LAYER (measured): interruptBase is cancelled by Interrupt()
// once and NEVER re-armed — NewAgent builds it and nothing recreates it. That is
// right for a signal handler about to end the process (the SIGTERM zombie fix
// above), and wrong for a long-lived server that wants to abandon ONE turn: once
// Interrupt() has fired, every later turn's context is born cancelled, so the
// surface would report an instant failure for the rest of its life. Abandoning a
// turn while the server lives on therefore needs a per-run cancel, which is what
// this publishes; the caller's existing `defer cancel()` releases it.
func (a *Agent) runScopedRoot() context.Context {
	base, cancel := context.WithCancel(a.interruptRoot())
	a.runMu.Lock()
	a.runCtx, a.runCancel = base, cancel
	// THE PER-TURN LATCH IS CLEARED WITH THE SCOPE IT DESCRIBES. runInterrupted
	// says "THIS run was abandoned by InterruptRun (the operator's per-turn
	// Ctrl+C)"; it is the flag TurnInterrupted reads, and a session with several
	// turns must not carry turn 1's interrupt into turn 2 — otherwise the second
	// turn's own failure would be reported, and RECORDED, as an operator interrupt
	// that never happened.
	//
	// It is deliberately NOT the interrupted latch Interrupt() sets: that one
	// means "leave the session" and its lifetime is the process (see
	// interruptedBySignal). Clearing the session latch here would silently change
	// what a SIGINT means for every later turn, which is not this feature's to
	// change.
	a.runInterrupted = false
	// …and the REASON it would have carried, for the same reason: a key that
	// abandoned turn 1 must not be named in turn 2's witness. Cleared to the
	// honest default rather than to the empty string, so a run interrupted by a
	// path that never states an origin still records a reason the reader can
	// group by (InterruptRun's own default).
	a.runInterruptReason = InterruptReasonCtrlC
	// The abandoned-turn witness counts THIS run's answer deltas, so it starts
	// with them (see answerTextDelta / recordTurnInterruption): a count carried
	// over from the previous turn would overstate how much of THIS answer the
	// operator saw.
	a.partialAnswerRunes = 0
	a.runMu.Unlock()
	return base
}

// endRunScope RETIRES the run-scoped handle runScopedRoot published — the other
// half of the pair, and the half that did not exist until task 01a1b4a2.
//
// WHY IT IS NOT OPTIONAL. runCtx is published at the start of a run and, before
// this, nothing cleared it when the run RETURNED: the caller's `defer cancel()`
// cancels the DERIVED context, so the published one kept Err() == nil forever.
// Measured consequences on a long-lived server (gopi web) between two turns:
// `RunInFlight()` answered true with no run in existence, and `InterruptRun()`
// — the read half being honest is what the interrupt route asks — returned true
// and latched `runInterrupted` on a context that had already finished, so
// `POST /api/chat/interrupt` acked an interruption of a turn nobody could be
// running.
//
// IT TAKES THE SCOPE, NOT A FLAG (identity, not first-come). The value passed is
// the one runScopedRoot returned to THIS run, and only that exact value may be
// retired: a run that is still unwinding must not clear the handle a LATER run
// published in the meantime, or the interrupt would stop reaching the live turn
// — reintroducing the very freeze (the `gopi ui` one) the run scope exists to
// end. Two runs overlapping is not hypothetical: a carrier that has returned
// from one turn and immediately POSTs another overlaps the first turn's defers.
func (a *Agent) endRunScope(scope context.Context) {
	if a == nil || scope == nil {
		return
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.runCtx != scope {
		// A later run owns the handle now; retiring it would be stealing its
		// interrupt. Nothing to do, and nothing to report (Rule 15: this is the
		// expected overlap, not a silent failure).
		return
	}
	a.runCtx = nil
	a.runCancel = nil
}

// runScopedContext is the ONE place the simple run-ctx sites (Chat, ChatOps)
// publish a run scope and derive their run context from it: the process-wide
// interrupt root with a cancel scoped to THIS run, capped by timeout when one is
// set (0 = uncapped, Rule 2).
//
// It returns THREE things, and they are not interchangeable: `cancel` ends the
// derived context THIS run uses, `endRun` withdraws the handle the agent
// publishes to InterruptRun / RunInFlight, and `ctx` is the run's context. A
// caller that defers only `cancel` leaves the published handle alive after its
// turn returned — the defect task 01a1b4a2 fixes — so the pairing is spelled
// once, here, rather than at each site where it could be forgotten again
// (Rules 13/14).
func (a *Agent) runScopedContext(timeout time.Duration) (ctx context.Context, cancel context.CancelFunc, endRun func()) {
	scope := a.runScopedRoot()
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(scope, timeout)
	} else {
		ctx, cancel = context.WithCancel(scope)
	}
	return ctx, cancel, func() { a.endRunScope(scope) }
}

// InterruptReasonCtrlC is the single spelling of the operator's per-turn
// interrupt by Ctrl+C: Ctrl+C at the incoming prompt of an interactive carrier,
// meaning "abandon this turn", never "end the session". It is a constant so the
// two halves of the feature — the marker the carrier prints and the reason the
// session JSONL records — cannot drift (Rule 1b/14).
const InterruptReasonCtrlC = "ctrl-c"

// InterruptReasonEscape is the same interruption delivered by a LONE ESCAPE: the
// pure interrupt, which cancels the turn in flight, keeps whatever the operator
// had typed, and finishes the session with it still open (task 01a0e84b).
//
// It is a SEPARATE reason from InterruptReasonCtrlC on purpose. The record has
// to answer "which key did the operator press" for a reader who was not at the
// terminal — that is the whole point of writing a reason at all — and the two
// keys differ in the one way a reader would act on: Escape closes nothing, while
// Ctrl+C at the prompt ends the session (see the carrier's key arms).
const InterruptReasonEscape = "escape"

// InterruptReasonEOF is the same abandonment asked for by Ctrl+D — end of input
// — pressed while a turn was still in flight (task 01a1650f).
//
// WHY IT HAS TO CANCEL AT ALL, rather than just queue the end-of-input marker.
// The REPL's main loop is blocked INSIDE ag.Chat while a turn runs, so a queued
// marker cannot be read until the turn returns by itself; against a peer that
// never answers that is never, and the measured outcome was a console that
// stopped reading the terminal altogether (pty probe, 2026-10-23: Ctrl+D at
// t+5.4s, Ctrl+C at t+35.4s, still alive and unresponsive at t+65s).
//
// ITS OWN REASON, not ctrl-c: the operator who pressed this key asked for the
// SESSION to end (the marker is already queued and the loop leaves at the turn
// boundary), while a Ctrl+C mid-turn asks only for the turn to stop. A reader of
// the JSONL who saw both recorded as "ctrl-c" could not tell which happened.
const InterruptReasonEOF = "ctrl-d"

// InterruptReasonShutdown is the same abandonment asked for by the PROCESS going
// away — a SIGINT/SIGTERM that a long-lived carrier (the `gopi web` server)
// converts into "cancel the turn in flight, then close the session".
//
// IT IS NOT ctrl-c, for the reason every other member is its own spelling: the
// record answers "what asked for this turn to stop", and no operator pressed a
// key on a shutdown. Measured (epic 01a16dab): a SIGTERMed server whose turn was
// never abandoned did not exit at all — the finalizer waited on the turn's lock
// while the turn waited on a peer round — so a reader of those sessions found no
// session_end to explain, and the ones that did close recorded an interruption
// nobody typed.
const InterruptReasonShutdown = "shutdown"

// InterruptReasonClientGone is the same abandonment asked for by the REQUEST
// channel closing — the browser tab that POSTed a turn went away mid-stream.
//
// IT DOES NOT MEAN THE SERVER IS STOPPING, which is why it is not "shutdown":
// the process keeps serving, and only the turn nobody can read any more is
// abandoned. Measured (epic 01a16dab): without this the server burned the WHOLE
// model round for a closed socket and held the session's turn lock for it, so
// the next request queued behind a response no one would ever receive.
const InterruptReasonClientGone = "client-gone"

// InterruptReasonSignal is the reason recorded when a SIGNAL ended a turn and
// this process never learned which one — the honest FLOOR of the signal path,
// never a substitute for the signal's name.
//
// ITS USUAL VALUE IS NOT THIS CONSTANT. A signal-borne interruption normally
// records signalrec's own spelling ("SIGINT", "SIGTERM") carried by
// Agent.CloseSignal, because a reader asking "what ended this turn" is better
// served by the signal than by the word "signal" — and that spelling has ONE
// source (internal/signalrec), which this package never re-derives (see
// SignalReason). This constant covers the residue: Interrupt() reached the run
// by a path that published no name, and inventing one would be the same lie the
// source field exists to prevent.
const InterruptReasonSignal = "signal"

// InterruptReasonSet is every reason constant above, in the order a reader scans
// them. It exists so a test can assert the CLOSED set rather than a membership
// list that drifts when a member is added (Rule 13: a constant is referenced,
// and the only guard against a bypassed one is an exhaustive check).
//
// TWO MEMBERS, and the missing third is a decision: Ctrl+C reaches a carrier by
// two transports (the raw 0x03 byte, and a real SIGINT on a cooked input) and
// both are THE SAME KEY. The record answers "which key did the operator press",
// and a reader who could not group the two transports together would miss half
// of every ctrl-c interruption — so the reason stays ctrl-c while the
// TRANSPORT is honest in internal/lineedit's own KeyOrigin vocabulary
// (OriginSignal), mapped at the ONE place the two vocabularies meet
// (cmd/agent's chatInterruptReason).
var InterruptReasonSet = []string{InterruptReasonCtrlC, InterruptReasonEscape, InterruptReasonEOF, InterruptReasonShutdown, InterruptReasonClientGone, InterruptReasonSignal}

// InterruptReasonKnown reports whether reason is one of the defined values. A
// carrier that recorded an invented reason would poison the session JSONL's
// grouping (the file is read by scripts), so the vocabulary is closeable and
// this is the predicate that closes it.
func InterruptReasonKnown(reason string) bool {
	for _, r := range InterruptReasonSet {
		if reason == r {
			return true
		}
	}
	return false
}

// InterruptSourceOperator names WHO ended a turn, in the vocabulary of the
// record: the person at the terminal, as opposed to the engine's own reasons
// (runend's wallclock-cap / max-iterations) or a process-level signal. Named
// because the session JSONL is read by scripts, and a free-text provenance field
// is one no reader can group by.
const InterruptSourceOperator = "operator"

// InterruptSourceSignal is InterruptSourceOperator's counterpart: the turn was
// ended by a SIGNAL this process handled (a SIGINT/SIGTERM the carrier decoded
// and turned into Interrupt()), not by a key.
//
// WHY IT IS A SEPARATE MEMBER AND NOT A REASON. The two fields answer two
// different questions — WHO ended the turn, and WHAT asked for it — and a signal
// breaks the operator pairing they had: the reason vocabulary is
// InterruptReasonSet, whose members name KEYS (ctrl-c, escape, ctrl-d), while the
// honest reason for a signal is the signal's own name. Recording a signal under
// InterruptSourceOperator would make every script that groups by source
// attribute a supervisor's SIGTERM to the person at the keyboard (the measured
// cost this epic exists for: session 07d98407's screen said "interrupt
// received" while the JSONL carried no witness at all, and the naive fix — one
// call to recordTurnInterruption — would have replaced silence with a lie).
const InterruptSourceSignal = "signal"

// InterruptSourceSet is every source constant above, so a test can assert the
// CLOSED set instead of a membership list that drifts (Rule 13).
var InterruptSourceSet = []string{InterruptSourceOperator, InterruptSourceSignal}

// InterruptSourceKnown reports whether source is one of the defined values — the
// same closure InterruptReasonKnown gives the reason vocabulary, for the field a
// reader groups by just as hard. A writer that invented a source would make
// "who ended this turn" unanswerable.
func InterruptSourceKnown(source string) bool {
	for _, s := range InterruptSourceSet {
		if source == s {
			return true
		}
	}
	return false
}

// TurnInterrupted reports whether the run ctx in flight was cancelled by the
// OPERATOR (Interrupt or InterruptRun) rather than by a deadline, the round cap
// or an engine stop — i.e. whether this turn's end must be recorded as an
// interruption instead of as a failure.
//
// It requires ctx.Err() != nil for the reason interruptedBySignal's callers do:
// the flag is set BEFORE the cancel, so a run that is already unwinding for its
// own reason (a wallclock cap that expired in the same instant) must not be
// re-labelled by a latch that has not been acted on yet.
func (a *Agent) TurnInterrupted(ctx context.Context) bool {
	if a == nil || ctx == nil || ctx.Err() == nil {
		return false
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	return a.runInterrupted
}

// TurnInterruptReason is the input that abandoned the run in flight, as recorded
// (InterruptReasonSet). It is read by the carrier that prints the marker — the
// marker must name the key the operator pressed, and the carrier cannot know it
// (a lone Escape and a Ctrl+C reach the same cancel) — and by this package's own
// witness writer, which records it in the session JSONL.
//
// A run that was NOT abandoned reports the honest default: InterruptRun's own
// spelling, ctrl-c. Callers pair this with TurnInterrupted, which is the
// predicate that says whether an abandonment happened at all.
func (a *Agent) TurnInterruptReason() string {
	if a == nil {
		return InterruptReasonCtrlC
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.runInterruptReason == "" {
		return InterruptReasonCtrlC
	}
	return a.runInterruptReason
}

// SignalReason is the signal that ended this session in flight, as recorded
// (Agent.CloseSignal), or InterruptReasonSignal when no name was published.
//
// THE SPELLING IS NEVER THIS PACKAGE'S. A carrier sets CloseSignal at the moment
// its watcher DECODES the signal, using signalrec.SignalName — the one
// definition of how a signal is spelled for a reader (Rule 14), shared by the
// process that catches a signal and the process that sends one. internal/agent
// therefore CONSUMES the name and never derives it; a second spelling here would
// make the turn witness and the session's own cause disagree about the same
// event.
//
// WHY IT IS NOT TurnInterruptReason. That function answers "which key did the
// operator press", and its default (ctrl-c) is honest only when a key really
// was pressed. Pairing this with InterruptSourceSignal is what lets the record
// say "source: signal, reason: SIGINT" instead of the false "operator pressed
// Ctrl-C".
func (a *Agent) SignalReason() string {
	if a == nil {
		return InterruptReasonSignal
	}
	if name := strings.TrimSpace(a.CloseSignal); name != "" {
		return name
	}
	return InterruptReasonSignal
}

// answerTextDelta is the ONE funnel every streamed answer character passes
// through on its way to the carrier: it counts the answer's own size (the
// abandoned-turn witness) and then forwards the delta to OnText.
//
// WHY THE CARRIER IS TOLD WHICH KEY ENDED THE TURN (task 01a0e84b). The witness
// this funnel's count feeds is written by recordTurnInterruption, and it carries
// the reason the cancel was made with (TurnInterruptReason) — so the marker the
// operator reads and the record a script groups by name the SAME input. The
// alternative (each carrier deriving the reason from the key it saw) is what
// would drift: a lone Escape and a Ctrl+C reach the same cancel, and the carrier
// that did not see the key (the signal path, the web route) has nothing to derive
// from.
//
// WHY A FUNNEL AND NOT TWO CALLS AT EACH SITE (task 01a0e4390). The partial
// answer's size is claimed in the session JSONL and shown to the operator when a
// turn is interrupted, and the two streaming sites (the SSE text_delta branch and
// the non-streaming fallback) both deliver the answer. Counting at either site
// alone would under-report one of the two paths — and the fallback is exactly the
// path a provider that refuses streaming lands on. One funnel, both paths, and
// the count cannot disagree with what OnText was handed.
func (a *Agent) answerTextDelta(delta string) {
	if a == nil || delta == "" {
		return
	}
	a.partialAnswerRunes += len([]rune(delta))
	if a.OnText != nil {
		a.OnText(delta)
	}
}

// recordTurnInterruption writes the turn-interrupted witness into the session
// JSONL and reports the partial answer's own size. It is called by the run that
// was abandoned, at the moment it understands WHY — so the record is written by
// the same goroutine that stops the stream, never by a signal handler racing it.
//
// source is that "why", in the InterruptSourceSet vocabulary, and it is a
// PARAMETER because the two arms that reach this writer end a turn for different
// reasons: a run the OPERATOR gave up on (InterruptSourceOperator, whose reason
// is the key TurnInterruptReason names) and one a handled SIGNAL ended
// (InterruptSourceSignal, whose reason is the signal itself — SignalReason).
// With the source hard-coded to the operator, the signal arm could only ever
// have been recorded as a keypress nobody made.
//
// It returns the number of answer characters that had reached the caller's stream
// (the partial answer the operator can still read on screen — agent.PartialAnswerRunes).
// That value is what the carrier reports to the user, and it is read HERE rather
// than by the carrier for the same reason the write is: the run is the only party
// that saw every delta.
func (a *Agent) recordTurnInterruption(source string) int {
	if a == nil {
		return 0
	}
	n := a.partialAnswerRunes
	if a.SessionLog == nil {
		return n
	}
	// A source the vocabulary does not know is SAID, then written as the
	// operator's: that is the only party that reaches this writer without a
	// signal, and an invented provenance string is a row no reader can group by
	// (Rule 15 — the alternative is a silent hole in a field scripts filter on).
	if !InterruptSourceKnown(source) {
		fmt.Fprintf(os.Stderr, "[gopi] turn-interrupt record names an unknown source %q — recording it as %q\n",
			source, InterruptSourceOperator)
		source = InterruptSourceOperator
	}
	reason := a.TurnInterruptReason()
	if source == InterruptSourceSignal {
		// A signal is not a key: TurnInterruptReason answers ctrl-c for a run that
		// was never abandoned through InterruptRun, which is exactly every run a
		// signal ends. Reading the signal's own name here is what keeps the record
		// from claiming a keypress nobody made.
		reason = a.SignalReason()
	}
	if err := a.SessionLog.LogTurnInterrupted(reason, source, n); err != nil {
		// Rule 15: a record that could not be written is SAID, never swallowed.
		// The session is still open and the operator is still at the prompt, so
		// their terminal is the channel that reaches the one party who can act.
		fmt.Fprintf(os.Stderr, "[gopi] turn-interrupt record not written: %v\n", err)
		return n
	}
	// Flush NOW: the next event this run writes may be many turns away (the
	// session stays open), and a witness buffered until then would be missing
	// from exactly the crash dump an operator opens after a hard kill.
	if err := a.SessionLog.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] turn-interrupt record not flushed: %v\n", err)
	}
	return n
}

// PartialAnswerRunes is how many answer characters the run in flight (or the run
// that just ended) streamed to its carrier — the size of the partial answer a
// per-turn interrupt leaves on screen. It is what the carrier prints in its
// interrupt marker.
func (a *Agent) PartialAnswerRunes() int {
	if a == nil {
		return 0
	}
	return a.partialAnswerRunes
}

// InterruptRun abandons the run in flight and reports whether there was one to
// abandon: it cancels the CURRENT run's context, leaving the process-wide
// interrupt root (and therefore every later turn) untouched.
//
// It is the run-scoped counterpart of Interrupt(): Interrupt stays the signal
// path that ends a process, while an interactive surface's "stop this turn"
// needs a cancel that cannot outlive the turn — without it the server keeps
// running the abandoned turn to completion, holding its single-turn lock and
// leaving the operator's next message waiting with nothing on screen accounting
// for the wait (the `gopi ui` freeze).
//
// The unwind is unchanged from any other ctx cancellation: the loop's
// round-boundary ctx.Done() check fires (or the in-flight LLM stream aborts) and
// the normal end-of-run defer writes session_end and closes the ledger row.
func (a *Agent) InterruptRun() bool { return a.InterruptRunBy(InterruptReasonCtrlC) }

// InterruptRunBy is InterruptRun with the reason RECORDED: which input asked for
// the turn to be abandoned (agent.InterruptReasonCtrlC / InterruptReasonEscape /
// InterruptReasonSignal — see InterruptReasonSet). The origin is set under the
// same lock that latches the interruption, so the witness the abandoned run
// writes can never name a key other than the one that really claimed it, however
// many transports fire for one keystroke.
//
// The reason is carried ONLY as far as the record: InterruptRun keeps its
// reasonless spelling because a caller that does not know the origin (the web
// interrupt route, the ui's Ctrl+X hook, a signal-driven term-once turn) must not
// invent one, and ctrl-c is the honest default on every one of those paths — they
// are all "the operator pressed Ctrl+C and a server turned it into a route".
// RunInFlight reports whether a run is live RIGHT NOW — i.e. whether
// InterruptRun would have something to cancel. It is the READ half of the same
// state InterruptRunBy mutates, taken under the same lock, and it READS ONLY:
// a status surface (the web page's "can this turn be stopped?" badge) must be
// able to ask without the question itself cancelling a turn.
//
// It exists so that predicate has one definition (Rule 14): the web carrier's
// state machine and its /api/chat/interrupt route ask this, rather than each
// keeping a second opinion about whether a turn is running.
func (a *Agent) RunInFlight() bool {
	if a == nil {
		return false
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	return a.runCtx != nil && a.runCtx.Err() == nil
}

func (a *Agent) InterruptRunBy(reason string) bool {
	if a == nil {
		return false
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	// An already-cancelled (or absent) run context means no turn is in flight:
	// reporting true there would claim an interruption that did not happen.
	if a.runCtx == nil || a.runCtx.Err() != nil {
		return false
	}
	// The latch is set BEFORE the cancel, and under the SAME lock that publishes
	// the run's scope: the run loop reads it after ctx.Done(), and this lock is
	// what orders the two (TurnInterrupted takes it to read).
	a.runInterrupted = true
	a.runInterruptReason = reason
	a.runCancel()
	return true
}

// Run executes the ReAct loop using the Claude Messages API: ask LLM →
// execute tools → repeat. Text content is printed to stderr in real-time as
// chunks arrive. Tool calls arrive as native tool_use content blocks.
// Run executes a fresh ReAct loop: the user input is wrapped as an Epic goal
// with task-list framing, and the one-shot task-start memory_search checkpoint
// is injected. It is the entry point for a brand-new session.
//
// images (optional) attach image content blocks to the first user message —
// native multimodal input: the vision model sees the pixels directly (image
// tokens processed like text tokens), no OCR round-trip. Each image must
// carry a base64 source.
func (a *Agent) Run(userInput string, images ...*deepseek.ClaudeImageSource) (result string, err error) {
	return a.runEngine(userInput, false, nil, images)
}

// RunPrompt executes a fresh ReAct loop for a PLAIN PROMPT: the caller's text
// is the whole first user message, sent to the model verbatim.
//
// WHY THIS IS NOT Run (RP-1, end-user-audit slice 8, 2026-09-26): Run's
// contract is a TASK — it frames the input as an Epic goal and appends the
// task-list deadline + reframe directive (~1000 chars of harness operating
// instructions). That is correct for a dispatched row and for the merger, and
// it is what `gopi -h` must therefore NOT promise to a shell user. The page's
// first Usage line — `gopi [options] "<prompt>"   Run one task from a prompt
// argument` — and its Inputs row — `"<prompt>"  The task text` — describe a
// run whose payload IS the text, so the shell path calls THIS entry point and
// the model receives the prompt.
//
// THE TASK-RUN CHILD REACHES THIS SAME MAIN (cmd/agent/cli_tasks.go spawns
// `<gopi>` with the briefing on stdin), so the entry point alone cannot decide:
// the decision is the wrapper identity the spawn seam exports
// (tools.TaskWrapperEnv). A child carrying that id HAS a ledger row and a
// briefing and keeps the framing — which is why the flag is derived from
// a.taskChildRowID (NewAgent's single read of that env) and not set
// unconditionally. The merger and every in-process task caller still call Run,
// so their framing is untouched.
func (a *Agent) RunPrompt(userInput string, images ...*deepseek.ClaudeImageSource) (result string, err error) {
	a.plainPrompt = a.taskChildRowID == ""
	return a.runEngine(userInput, false, nil, images)
}

// Resume continues an interrupted session from a previously captured full
// context. history is the exact message sequence (user/assistant/tool) that
// the interrupted session's last request carried on the wire — it is seeded
// into Ctx so the resumed loop continues in the SAME context without
// re-framing the continuation message as a new Epic goal or re-injecting the
// task-start memory_search checkpoint (both would corrupt the recovered
// context). userInput is appended verbatim as the model's next instruction.
func (a *Agent) Resume(userInput string, history []deepseek.Message) (result string, err error) {
	return a.runEngine(userInput, true, history, nil)
}

// runEngine is the shared ReAct-loop entry. When resume is false it behaves
// exactly like the historical Run(): [EPIC GOAL] framing + taskStartSearch.
// When resume is true it seeds the recovered history and appends the
// continuation message verbatim — no re-framing, no taskStartSearch.
// images are attached natively to the first user message (vision input).
func (a *Agent) runEngine(userInput string, resume bool, history []deepseek.Message, images []*deepseek.ClaudeImageSource) (result string, err error) {
	// Hippocampus wiring root: the scheduler's recalls derive from the SAME
	// cancel root as every other run work, so Agent.Interrupt() (the user's
	// hard stop) ends an in-flight hippocampal cycle too. Nil host ⇒ no-op.
	if a.Hippocampus != nil {
		a.Hippocampus.setBase(a.interruptRoot())
	}
	// Empty-input guard (root cause: the SessionLogger is created in
	// NewAgent, BEFORE any input is known — so "empty input" must be
	// rejected at the loop entry or an empty session file is already on
	// disk). The CLI prints usage for empty stdin; this library-level guard
	// makes the same promise for every other caller: an empty prompt with
	// no images never starts a session. Resume is exempt — an empty
	// continuation there is legitimate (resumeContinuationMessage).
	if !resume && strings.TrimSpace(userInput) == "" && len(images) == 0 {
		return "", fmt.Errorf("empty task input: give gopi a prompt (argument, --prompt, or piped stdin); refusing to start a session with no task")
	}

	a.Ctx.SystemPrompt = a.SystemPrompt
	// The tool-result cap (toolresult_cap.go) names THIS path in the note it
	// splices into an oversized result, so a model reading the excerpt is told
	// where the whole text was recorded. LogToolResult writes the FULL result
	// before AddToolResult applies the cap (a few lines below in the tool-call
	// assembly), so the file really does hold what the note claims.
	if a.SessionLog != nil {
		a.Ctx.ToolResultSessionPath = a.SessionLog.Path()
	}

	if resume {
		// Seed the recovered conversation verbatim, then append the user's
		// continuation instruction as an ordinary user message (no [EPIC GOAL]
		// framing — the model already knows the task).
		a.Ctx.Messages = append([]deepseek.Message{}, history...)
		// Cross-RUN working-area carry-forward (memory-aware T2): when the
		// resume path recovered the ORIGINAL session's id (CarryForwardSessionID
		// — set from the --task row's session_id column), re-inject that
		// session's committed write_task_area conclusions into the fresh
		// context BEFORE the continuation, so the first resumed request
		// already carries the authoritative committed-conclusions block (the
		// POC's "next round" across process boundaries — same marker and
		// render as in-loop injectTaskAreaConclusions so both paths agree).
		if cf := taskAreaCarryForward(a.CarryForwardSessionID); cf != "" {
			a.Ctx.AddUserMessage(cf)
			if a.SessionLog != nil {
				a.SessionLog.LogUser(newUUIDv4(), cf)
			}
			if a.Logger != nil {
				a.Logger.Printf("🧾 resume: carried forward committed conclusions from session %s", a.CarryForwardSessionID)
			}
		}
		var continuation string
		if userInput != "" {
			continuation = userInput
		} else {
			continuation = resumeContinuationMessage
		}
		a.Ctx.AddUserMessage(continuation)
		if a.SessionLog != nil {
			a.SessionLog.LogUser(newUUIDv4(), continuation)
		}
		// Pairing invariant (epic 2026-09-22): the recovered checkpoint is the
		// ONLY input here that the harness did not build itself — it is whatever
		// the interrupted process put on the wire, and a damaged checkpoint is
		// precisely how the 400-death became per-resume instead of one-off.
		// Repair it against the recovered array and PROVE it before the run
		// starts, so a resume either continues on a sound history or fails with
		// the reason, instead of dying on the provider's HTTP 400 one iteration
		// later. The same invariant is gated again at the wire choke point
		// (internal/deepseek), which is the net under every other builder.
		if repaired, orphanIDs := repairToolUsePairs(a.Ctx.Messages); len(orphanIDs) > 0 {
			a.Ctx.Messages = repaired
			if a.Logger != nil {
				a.Logger.Printf("🔗 resume: repaired %d unanswered tool_use(s) in the recovered checkpoint (ids: %s) — the interrupted request carried them without their tool_result", len(orphanIDs), strings.Join(orphanIDs, ", "))
			}
		}
		if err := verifyToolUsePairs(a.Ctx.Messages); err != nil {
			return "", fmt.Errorf("resume refused: the recovered checkpoint violates the Claude Messages tool_use/tool_result pairing invariant and could not be repaired — %w", err)
		}
		// THE CLONE CHANNEL'S START-HOOK HALF, FOR A CONTINUATION (bug measured
		// 2026-10-31, rows 01a190bb @05:57 and 01a190b4 @06:02). A resume mints
		// no row, so the `if !resume` arm below — which registers one AND adopts
		// the ground — never ran for it, and the ground was therefore never
		// adopted on a continuation either. `CloneRunGround()` answered ("", ""),
		// which is the WORKTREE channel's answer, and the close-out's delivery
		// leg took its compatibility arm: no attempt, no `push accepted`, no
		// `delivery did not land` — and the row closed `completed` with its
		// commits still in the clone (the three-layer chain the defect was).
		//
		// The row here is the ADOPTED one (AdoptLedgerRow ran in cmd/agent before
		// the engine started), so it is the row the ground belongs to. The
		// adoption re-enters a directory rather than minting one: a continuation
		// works where its first run worked.
		a.adoptContinuationCloneGround(a.autoTaskRowID)
	} else {
		// First turn: wrap user input as an Epic goal with task list framing.
		// The establishment deadline interpolates emissionContract (single
		// source of truth) — the framing no longer says "first substantive
		// round", which competed with rule 2's omit cadence and pushed the
		// deferred/plan-tailing emissions (compare-report RC2/P1).
		//
		// A PLAIN PROMPT IS NOT AN EPIC (Agent.RunPrompt): `gopi [options]
		// "<prompt>"` sends the shell text as the WHOLE first user message —
		// no wrapper, no task-list deadline, no reframe directive. Confining
		// the frame to the surfaces that ARE tasks is what makes `gopi -h`'s
		// `"<prompt>"  The task text` true at the request boundary.
		epicMessage := userInput
		if !a.plainPrompt {
			epicMessage = fmt.Sprintf(epicGoalFramingFormat, userInput, emissionContract, reframeDirective)
		}
		// Native image input: attach the turn's image content blocks to the
		// ONE user message. The vision model sees the pixels directly (image
		// tokens processed like text tokens) — NOT an OCR round-trip. The
		// epic framing text accompanies the images as the instruction. All N
		// images ride in this single message: the endpoint accepts 8/9/12
		// image blocks in one user message (measured 2026-11-07, all 200).
		if len(images) > 0 {
			a.Ctx.AddMessage(deepseek.Message{
				Role:    "user",
				Content: epicMessage,
				Images:  derefImages(images),
			})
		} else {
			a.Ctx.AddUserMessage(epicMessage)
		}

		// Log user message to session JSONL.
		if a.SessionLog != nil {
			a.SessionLog.LogUser(newUUIDv4(), epicMessage)
		}

		// Task ledger auto-registration (start hook, best-effort): insert the
		// session's in_progress row (auto t-<unix> task_id, session UUID in
		// the session_id column — consolidation; summary = [auto] + briefing)
		// right after the epic user message is logged. Kill switch:
		// GOPI_AUTO_TASK_REGISTER=0. Resume runs skip this (the resumed
		// session already has its row).
		if !resume {
			a.maybeRegisterSessionTask(userInput)
			// THE CLONE CHANNEL'S START-HOOK HALF (epic
			// raw-prompt-clone-channel): when settings `dev_repo` is set, the
			// bare-prompt face works in a clone workspace like `gopi worker`
			// does, instead of minting a Rule-6a linked worktree in the main
			// checkout. Placed HERE because the workspace is named by the
			// row's id, which exists only after the registration above; a
			// no-op for every other face (a chat, a task child — see
			// adoptCloneGround's own gate).
			a.adoptCloneGround(a.autoTaskRowID)
		}

		// M4.5 ⑦ liveness: latch the session whose ledger row the heartbeat
		// touches target (the run's in_progress row — normally the
		// auto-registered t-<unix> row; for a `task run` CHILD
		// (GOPI_TASK_WRAPPER_ID set) it is the HUMAN wrapper row's task_id,
		// so the single execution row stays alive — task-single-row-exec).
		// SessionLog can be nil in minimal test Agents — guard (heartbeat is
		// best-effort end to end).
		if a.SessionLog != nil {
			setTaskHeartbeatSession(a.SessionLog.SessionID(), a.autoTaskRowID)
		}

		// Task-start memory_search enforcement (对称强制 with the task-end
		// completion checkpoint): before the first round, if the run has not
		// searched, inject the one-shot taskStartSearchMessage so the loop begins
		// with a search even without LLM volition — the closed-loop ENTRY point
		// (start-search / end-store symmetry). One-shot latch, never per-round.
		// Disabled by GOPI_NO_ENFORCE_MEMORY / --no-enforce-memory (A/B isolation).
		//
		// With the hippocampus gate ON this hook is SUPERSEDED, not duplicated:
		// the organ's recall runs instead and the forced search message is not
		// injected (devdoc/hippocampus.md §4.2 明确禁止双注入). Off ⇒ unchanged.
		a.startOfRunFor(a.interruptRoot())

		// Arm the review-time memory-consult checkpoint so the run's FIRST
		// commit consumes it (maybeInjectConsultCheckpoint). Arming here — on
		// every start-of-run path — is what makes the checkpoint
		// un-skippable: it rides the first commit, whichever path reaches it.
		// It is independent of the hippocampus gate above: failing to consult
		// memory BEFORE acting is measurable regardless of which organ
		// performed the recall.
		a.armConsultCheckpoint()
	}

	startTime := time.Now()
	iterCount := 0

	// Wallclock cap (loop-deadlock epic ②): one hard wall-clock ceiling for
	// the whole session, independent of MaxIterations (which is 0=unlimited
	// in production). Reaching the cap finishes the run GRACEFULLY: the
	// loop stops issuing new LLM rounds and returns the last substantive
	// assistant text with a nil error; the normal end-of-run defer writes
	// session_end, closes the ledger row (completed — the work done so far
	// stands; the cap is an environment boundary, not a failure) and the
	// loop-stuck-style notify (event=session-wallclock-exceeded) tells the
	// operator why the run stopped. GOPI_SESSION_MAX_WALLCLOCK=0 disables.
	wallclockCap := sessionWallclockCap()
	wallclockExceeded := false
	deadlineTime := startTime.Add(wallclockCap)
	// End reason (runend): WHY the loop stopped, carried into the close-out
	// path. The close-out hook used to decide the ledger status from the exit
	// code ALONE, and every engine-forced stop here exits ZERO — so a run the
	// cap cut short was recorded `completed`, a false success that sync/verify/
	// merge then built upon. The reason rides the DEFERRED stats closure
	// (registered below), so it is read after the loop has returned; every
	// engine-forced exit site assigns it before returning.
	endReason := runend.Finished

	// Crash-dump epic ④: the panic recover runs FIRST (defers run LIFO —
	// placing this defer AFTER the stats defer below would let the stats
	// defer's SessionLog access happen after Close; instead the recover
	// defer is registered FIRST so it runs LAST, after the normal end-of-run
	// defer had its chance on the happy path). On panic: reset the stats
	// snapshot from the closure values, write dump + session_end(error) +
	// task-ledger failed, then re-panic (exit code stays non-zero).
	defer func() {
		if r := recover(); r != nil {
			panicElapsed.Store(time.Since(startTime).Seconds())
			panicIterations.Store(int64(iterCount))
			totalTokens := 0
			if a.Cost != nil {
				totalTokens = a.Cost.TotalPromptTokens + a.Cost.TotalCompletionTokens
			}
			panicTokens.Store(int64(totalTokens))
			a.recoverRunPanic(r, a.finishSessionTask)
		}
	}()

	// GOPI_TIMEOUT_SEC: max runtime in seconds (set in NewAgent). 0 = unlimited.
	// The wallclock cap (②) is folded into the SAME context: a cap hit trips
	// ctx.Done() at the round boundary, and wallclockExceeded disambiguates
	// it from a GOPI_TIMEOUT_SEC timeout for the graceful-finish path.
	var ctx context.Context
	var cancel context.CancelFunc
	// The run scope is PUBLISHED here and RETIRED by the defer below on every
	// return path (normal, error, panic-ish unwind): a long-lived server that
	// kept the handle after its turn returned would answer RunInFlight() true —
	// and InterruptRun() would claim an interruption of a finished turn.
	runScope := a.runScopedRoot()
	defer a.endRunScope(runScope)
	switch {
	case a.Timeout > 0 && wallclockCap > 0 && deadlineTime.Sub(time.Now()) < a.Timeout:
		ctx, cancel = context.WithDeadline(runScope, deadlineTime)
	case a.Timeout > 0:
		ctx, cancel = context.WithTimeout(runScope, a.Timeout)
	case wallclockCap > 0:
		ctx, cancel = context.WithDeadline(runScope, deadlineTime)
	default:
		ctx, cancel = context.WithCancel(runScope)
	}
	defer cancel()

	// task-liveness-coverage: the in-process keepalive. It ticks while THIS
	// run's context is alive — the coverage for the interval no caller-side
	// touch can reach: a single tool call (a 10-minute bash) or a model
	// inference that outlives runKeepaliveInterval, during which no loop
	// iteration returns and no batch starts. Bound to ctx by construction, so
	// it dies with the run and can never stamp a row after the run is gone.
	startRunKeepalive(ctx)

	defer func() {
		elapsed := time.Since(startTime)
		stats := "tokens: (not tracked)"
		if a.Cost != nil {
			stats = a.Cost.Summary()
		}
		status := "OK"
		if err != nil {
			status = "FAIL"
		}
		// Wallclock cap observability (②): a cap-finished run closes like a
		// normal completion but must be visible — stderr note + notify event
		// so the operator knows the run stopped at the environment boundary
		// and WHY (not by the model's own declared completion).
		if wallclockExceeded && err == nil {
			fmt.Fprintf(os.Stderr, "[gopi] session wallclock cap reached (%v) — finishing gracefully\n", wallclockCap)
			sid := ""
			if a.SessionLog != nil {
				sid = a.SessionLog.SessionID()
			}
			tools.NotifySyntheticEvent(notifyEventWallclockExceeded, sid,
				a.Model, fmt.Sprintf("session wallclock cap exceeded (%v) — finished gracefully after %d iterations",
					wallclockCap, iterCount))
		}
		fmt.Fprintf(os.Stderr, "\n⏱ %.1fs | %d iterations | %s | %s\n",
			elapsed.Seconds(), iterCount, status, stats)

		if a.RawLog != nil {
			a.RawLog.Close()
		}

		// Write session_end event and close the session log.
		if a.SessionLog != nil {
			exitCode := 0
			if status == "FAIL" {
				exitCode = 1
			}
			totalTokens := 0
			if a.Cost != nil {
				totalTokens = a.Cost.TotalPromptTokens + a.Cost.TotalCompletionTokens
			}
			// Crash-dump epic ①: on a failed run, collect the error record
			// (LLM error / stream error — same path as the panic recover),
			// write the crash dump FIRST (so the session_end error block can
			// point at the dump path), then log session_end with the error
			// block. Normal (exit_code=0) sessions carry no error block.
			var failure *ErrorRecord
			if exitCode != 0 {
				failure = a.collectErrorRecord(err, iterCount, totalTokens, elapsed.Seconds())
				a.SessionLog.WriteCrashDump(failure)
			}
			a.SessionLog.LogSessionEnd(exitCode, iterCount, totalTokens, elapsed.Seconds(), failure, a.endCauseFor(exitCode))

			// Task ledger auto-registration (end hook, best-effort): close the
			// start hook's row — completed/failed by exit code, summary += the
			// same stats the session_end event carries. The already-collected
			// failure record (with its dump path) is passed through so the
			// failed summary tail carries the SAME digest as session_end.
			a.finishSessionTaskRecorded(exitCode, iterCount, totalTokens, elapsed.Seconds(), failure, endReason)

			// Hippocampus end-of-run (epic child 3/4): wait (bounded), drain and
			// archive the organ's tail BEFORE the session log closes — a brief
			// produced after the last LLM round has no round left to be
			// delivered into, so it degrades to Lv-0 (devdoc/hippocampus.md §4).
			// Nil host ⇒ no-op.
			a.endOfRunHippocampus(ctx)

			a.SessionLog.Close()

			// A6: trajectory stats generated from the session log — measured
			// numbers only, so reports never rely on memory (competition #4:
			// gopi under-reported thinking by ~12x).
			a.printTrajectoryStats()
		}
	}()

	// Standard ReAct loop: ask LLM → execute tools → repeat.
	// GoPi does NOT dictate when the model should exit or continue.
	// Exit signal (bug022): the model stops when it produces
	// maxConsecutiveEndTurnRounds consecutive end_turn rounds with no tool
	// calls. A single end_turn is NOT trusted — DeepSeek V4 sometimes ends a
	// turn mid-task and resumes tool calls on the next round. R3 exception:
	// an end_turn whose task list is all-completed with a substantive answer
	// exits on the FIRST round (declared completion needs no confirmation).
	// Task list parsing is for observability only (logged on exit, never enforced).
	// consecutiveEndTurn counts consecutive rounds with no tool calls (bug022 fix);
	// pendingFinalContent preserves the first end_turn's substantive answer.
	consecutiveEndTurn := 0
	pendingFinalContent := "" // substantive text of the first end_turn round
	// freeTextChars accumulates this run's assistant TEXT output (outside
	// tool_use blocks). It is the reframe nudge's budget signal: a reframe
	// call is a tool call, so text growth means the model is narrating
	// instead of deciding.
	freeTextChars := 0

	// No-list-yet nudge (P1.4/P2): tracker for elapsed tool rounds and the
	// round index of the next armed nudge. a.nudgeCount / a.nudgeNextAt
	// (struct fields) hold the persistent nudge state; maybeNudgeNoList
	// manages both. Single-threaded, read/written only in this loop.
	elapsedToolRounds := 0
	// roundsSinceTaskList counts consecutive rounds whose response carried no
	// [TASK_LIST] block (bookkeeping-decay nag 6.5). Reset on every emission.
	roundsSinceTaskList := 0
	nudgeToolRoundsCfg := a.NudgeToolRounds
	if a.NudgeToolRounds <= 0 {
		nudgeToolRoundsCfg = defaultNudgeToolRounds
	}
	if a.nudgeNextAt == 0 {
		nd := a.NudgeRounds
		if nd <= 0 {
			nd = defaultNudgeRounds
		}
		a.nudgeNextAt = nd
	}

	// Stuck detector (loop-deadlock epic ①): consecutive no-tool-use
	// same-text rounds net. fed per round below (observe resets on
	// tool_use / divergent text); tripped() terminates the run.
	stuck := &stuckState{}
	// toolStreak detector (bug055): consecutive identical-tool_use rounds
	// net — the monitor-loop blind spot (Bond 718319: 272× identical bash
	// sleep+ssh polls, 10h15m). fed per round below; tripped() terminates.
	toolStreak := newToolStreakState()
	// observation bound (§6.3): consecutive observation-only rounds net —
	// the LOOK-WITHOUT-FIXING blind spot of both counters above (an
	// observing round always has a tool_use, so ① resets; its tool_use and
	// its live result both CHANGE, so ②'s polling exemption reads it as
	// alive). fed at the same single point below; tripped() terminates.
	observation := newObservationState()

	for i := 0; a.MaxIterations == 0 || i < a.MaxIterations; i++ {
		iterCount = i + 1
		// task-liveness-coverage: the round boundary is a liveness instant.
		// Interval (b) — between two tool batches the model may think for
		// minutes, and pre-fix that window wrote nothing to the ledger, so a
		// live run read exactly like a dead one. Best-effort, throttled,
		// never blocks (see taskheartbeat.go).
		touchRunIterationStart()
		// Hippocampus round stamp: every host decision of this round (the
		// retrieval made at the boundary, the level, the Lv-0 audit record) is
		// labeled with the round it belongs to. Zero cost when the host is nil.
		a.hippocampusRound = i + 1
		// Round-boundary deadline check (② wallclock cap + GOPI_TIMEOUT_SEC):
		// a cap hit at the boundary finishes the run gracefully — the loop
		// returns the last substantive assistant text (or a cap note when
		// none exists) with a nil error, so the end-of-run defer closes the
		// session + ledger as a normal completion. A GOPI_TIMEOUT_SEC hit
		// keeps the historical error return (a hard timeout IS a failure).
		select {
		case <-ctx.Done():
			if wallclockCap > 0 && !time.Now().Before(deadlineTime) {
				wallclockExceeded = true
				endReason = runend.WallclockCap
				fmt.Fprintln(os.Stderr) // newline after streamed text
				return lastSubstantiveText(a), nil
			}
			// Signal-interrupt semantics (chat-once/bare-prompt SIGTERM
			// zombie fix): Interrupt() cancelled the run ctx — return the
			// interrupt error so the end-of-run defer writes session_end
			// (exit_code=1) + closes the auto ledger row (failed). The
			// distinction from GOPI_TIMEOUT_SEC matters only for the error
			// text — both unwind through the same defer.
			if a.interruptedBySignal() {
				fmt.Fprintln(os.Stderr) // newline after streamed text
				return "", interruptErr
			}
			// The context ended for a reason the loop did not choose. Say
			// WHICH one, and never blame a budget: the loop's own counter is
			// not a limit, and in production there is no limit at all
			// (a.MaxIterations == 0). This exit used to answer every one of
			// these with a timeout claim measured against an iteration budget
			// — a cause that does not exist in production — and print the loop
			// counter as its number, so one defect reported 43, 48, 49, 52 …
			// and every reader looked for a budget instead of for the caller
			// that cancelled (measured on 81 merge hand-over failures; the old
			// sentence and the full measurement are in
			// devdoc/ctx-ended-run-reason.md).
			//
			// ctx.Err() is the ONLY thing that distinguishes the two, and it
			// does so exactly (both are errors.Is-comparable): DeadlineExceeded
			// is our own deadline expiring (GOPI_TIMEOUT_SEC / the wallclock
			// cap, folded into this same context), Canceled is the caller
			// ending the run. Both are engine-forced, so both carry a
			// truncation reason into the row's `reason:` column.
			//
			// The ITERATION budget keeps its own exit — the for-loop's own
			// exhaustion below (`max iterations (%d) reached`), which is
			// reachable only when a.MaxIterations > 0. This site is reachable
			// WITH MaxIterations == 0 (the production setting), which is
			// exactly why it must not borrow that budget's name.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				endReason = runend.DeadlineExceeded
			} else {
				endReason = runend.RunContextCancelled
			}
			fmt.Fprintln(os.Stderr) // newline after streamed text
			return "", fmt.Errorf("agent run ended before the task was finished: %w (%s)", ctx.Err(), endReason.Token())
		default:
		}

		// R5: round boundary — persist last round's buffered session/raw log
		// events before the next LLM round-trip (one fsync per round instead
		// of per event / per SSE chunk). Sync is a cheap no-op when nothing
		// is buffered (LogToolResult already synced the tool results).
		if a.SessionLog != nil {
			a.SessionLog.Sync()
		}
		if a.RawLog != nil {
			a.RawLog.Sync()
		}

		// THE NOTE AS A SUBSTITUTE (turnfold.go) — the task loop's round
		// boundary. Every COMPLETED turn's raw messages leave the request here and
		// ONE note takes their place; there is no size threshold (the one commit
		// 4c8c44df added was measured inert) and no raw window unless an operator
		// sets GOPI_TURN_FOLD_KEEP_TURNS. What still stops a fold is only what
		// would lose information (foldableTurnRanges). The turn in flight is never
		// touched, and the fold is idempotent.
		//
		// It runs BEFORE the injections on purpose: an injected notice belongs to
		// the turn that is about to run, and folding after would either move it
		// into a note or leave it stranded outside one.
		a.foldCompletedTurns()

		// P0-3: inject job-completion notices as user messages BEFORE the
		// request is built — the model never polls (dsh onJobDone: notice
		// lands in the inbox, zero polling). Each notice is one user message;
		// jobs settling together cost one round.
		//
		// The external half first (session_inbox.go): a message an outside
		// process delivered with `gopi notify --session <id> --message <text>`
		// is merged into this SAME queue, so the take below drains both kinds
		// through the one take point.
		a.loadSessionInbox()
		for _, n := range a.noticesToInject(tools.TakeJobNotices()) {
			// Creation-time ground detection for the DETACHED half of a run's
			// work (the job tool's start action): the bash-round seam below
			// cannot see a worktree/branch a background job created, so a row
			// whose ground comes from a job stayed repo-only. Same seam, same
			// writer, same ownership guards — only the caller differs (Rules
			// 13/14). Notices are emitted for naturally-exited jobs only (the
			// kill action is synchronous), and the probe is directory/git
			// truth, so a failed job simply resolves nothing. Best-effort,
			// exactly like the bash caller.
			// A notice produced by a JOB; an external message (session_inbox.go)
			// has no command and no worktree to probe, so the ground-detection
			// seam is skipped for it explicitly rather than relying on the
			// command pre-filter inside.
			if n.External == nil {
				tools.TaskDeliverableWorktreeJobForRow(a.autoTaskRowID, n.Command)
			}
			a.injectJobNotice(n)
		}

		// Hippocampus round boundary (Lv-1/Lv-2/Lv-3): the organ's finished
		// briefs become user messages HERE and nowhere else — the same physical
		// channel as the job notices above. Lv-1 appends a post-round memo,
		// Lv-2 appends an in-context meta event, Lv-3 the scheduler-adjudicated
		// soft interrupt; all three are ordinary user messages (NEVER a new
		// promptSection — devdoc/hippocampus.md §3), logged 1:1 to the session
		// JSONL. The host also launches the next retrieval here, so the organ
		// works while this round's LLM call runs. Nil host ⇒ no-op.
		a.roundBoundaryInjections(ctx)

		// No-list-yet nudge (P1.4/P2, audit v2 RC5): if the model has run
		// >=NudgeRounds rounds with no task list AND >=NudgeToolRounds of them
		// carried tool calls AND a list still has not been established, inject
		// a one-shot reminder. The check runs BEFORE the request is built so
		// the nudge reaches the model this round (i is 0-based; a.nudgeNextAt
		// is 1-based). Re-arms with interval escalation (nudgeEscapeRounds)
		// until maxNudgeCount or until a list is established.
		a.maybeNudgeNoList(i+1, elapsedToolRounds, nudgeToolRoundsCfg)

		// Bookkeeping-decay nag (6.5, C2): a list EXISTS but no block has been
		// re-emitted for maxRoundsWithoutTaskList rounds. Runs before the
		// request is built so the reminder reaches the model this round. The
		// counter is reset below, on the round that actually emits a block.
		a.maybeNudgeStaleTaskList(roundsSinceTaskList)

		// Reframe early-trigger backstop: a big-briefing run that has neither
		// called task(action=reframe) nor been swapped is nudged once, before
		// the request is built so the reminder reaches the model this round.
		// The directive asks for the call up front; this catches the
		// LLM-judged-trigger misses the review measured (devdoc d9d6367).
		// Budget-aware: it waits for free-text growth or a round boundary so it
		// never crowds out the round producing the call. One-shot — a small
		// task (below reframeMinRawLength) never fires it.
		a.maybeNudgeReframe(i+1, freeTextChars)

		// Snapshot cost before request so we can compute per-request delta.
		var prevPrompt, prevHit, prevMiss, prevCompletion int
		if a.Cost != nil {
			prevPrompt = a.Cost.TotalPromptTokens
			prevHit = a.Cost.TotalCacheHitTokens
			prevMiss = a.Cost.TotalCacheMissTokens
			prevCompletion = a.Cost.TotalCompletionTokens
		}

		// Context window awareness: estimate tokens and trim the oldest
		// messages before the API call when the budget is nearly exhausted.
		a.trimContext(ctx)

		// Per-response output cap: explicit Agent.MaxTokens wins, otherwise
		// the package default. 0/negative means "unset" (existing tests build
		// Agent{} literals without the field).
		maxTokens := a.MaxTokens
		if maxTokens <= 0 {
			maxTokens = deepseek.DefaultClaudeMaxTokens
		}

		// Thinking pause (maxThinkingAbortStreak): a run whose thinking keeps
		// blowing the budget has its thinking held off — one prefill per round
		// instead of the abort round's two. Every thinkingProbeSpacing-th round
		// is sent thinking-ENABLED as a recovery probe so reasoning returns once
		// the runaway has passed. Effort goes off the wire with thinking.
		thinking := a.Thinking
		wireEffort := a.ReasoningEffort
		paused, probe := a.thinkingPausedForRound()
		a.reportEffectiveThinkingBudget(thinkingBudgetOf(thinking), maxTokens, effectiveThinkingBudgetOf(thinking, maxTokens))
		// Effort goes off the wire whenever thinking is off — not only while
		// paused (PA ruling 2026-08-27: wireEffort is empty when thinking is
		// disabled). BuildClaudeRequest also guards this, but the intent is
		// explicit here.
		if (paused && !probe) || thinking == nil || thinking.Type == thinkmode.Disabled {
			thinking = &deepseek.Thinking{Type: thinkmode.Disabled}
			wireEffort = ""
		}
		a.thinkingProbeRound = probe

		// The task wire carries claudeToolDefs; record them so the context-size
		// read-out counts what the server bills (context_tools.go).
		a.Ctx.SetTools(claudeToolDefs)

		buildRoundRequest := func() *deepseek.ClaudeRequest {
			return deepseek.BuildClaudeRequest(
				a.Model,
				// requestMessages, not Ctx.WireMessages: it returns the same wire
				// list PLUS this request's turn/budget stamp appended as the last
				// user message (clock.go). The task loop is the surface the whole
				// epic is about — every round of a worker run, commit beat or not,
				// carries the elapsed tick and the budget progress tag, so the
				// 60-minute ceiling is visible on EVERY request instead of only in
				// the string task(action=commit) returns (P3). A nil Clock makes
				// the two calls identical, which is what keeps Agent{} literals in
				// the suite sending the bytes they always sent.
				a.requestMessages(a.KeepThinking),
				a.SystemPrompt,
				a.Ctx.ProjectContext,
				claudeToolDefs,
				maxTokens,
				thinking,
				wireEffort, // P0-1: real wire parameter (empty when thinking off)
			)
		}
		req := buildRoundRequest()

		// Pairing invariant PROOF (epic 2026-09-22). The conversion above is
		// where a damaged source array turns into an HTTP 400, so the built
		// request is checked before it leaves this function: fail closed here,
		// loudly, with the message index and the tool_use id — never send it
		// and read the provider's rejection as a mystery ("the run died in 1
		// iteration / 0.4s" was this, undiagnosed, twice). internal/deepseek
		// gates the same invariant at the wire choke point for every other
		// builder; this one keeps the failure at the layer that owns the
		// context, where the index is actionable.
		if err := deepseek.ValidateToolUsePairs(req); err != nil {
			return "", fmt.Errorf("tool_use/tool_result pairing invariant violated by the outbound request (iteration %d): %w", i, err)
		}

		if a.Debug {
			b, _ := json.MarshalIndent(req, "", "  ") //stylegate:ignore pretty-printing an in-process value for a log line; a failure is non-fatal
			a.Logger.Printf("→ REQUEST [%d] (%d msgs, %d tools)\n%s\n", i, len(req.Messages), len(req.Tools), b)
		}

		// Crash-dump epic ①: remember the last request + recent tool names
		// so a terminal failure can produce the digest/dump without the
		// collector having to rebuild history. The GATE records it again at
		// the point of send (request_tokens_cap.go), which is what makes a
		// request re-rendered after a shrink the one on record.
		a.failedIteration = i

		// ONE ASSEMBLY PER ROUND, and the request validated/debugged above IS
		// the one that goes on the wire. Assembling a SECOND request here (the
		// shape before this change: `req := buildRoundRequest()` for the
		// invariant proof and the debug print, then the gate's builder calling
		// it again) is not a harmless duplicate: requestMessages MINTS the turn
		// stamp (clock.go), and a mint advances SessionClock.lastRequest — so
		// the pre-check build turned the request the gate then sent into the
		// session's SECOND request, which is stamped. On the plain-prompt
		// surface that is the RP-1 defect measured end to end: the operator's
		// prompt reached the model as "<prompt>[system] [t+0s] @ … — +0s since
		// your previous request. session budget: …" because the FIRST request's
		// exemption (turnStampForRequest) had already been spent by a build
		// nobody sent.
		//
		// The re-render the gate NEEDS is preserved: it is handed a builder that
		// yields THIS request on its first call and re-assembles only after it
		// has actually shrunk the context (a request built before compaction
		// describes a context that no longer exists — request_tokens_cap.go). So
		// the normal path assembles once, and the mint happens once per request
		// the run really makes.
		builtForSend := false
		buildOnceThenRebuild := func() *deepseek.ClaudeRequest {
			if !builtForSend {
				builtForSend = true
				return req
			}
			return buildRoundRequest()
		}
		res, err := a.chatOnceGated(ctx, buildOnceThenRebuild, i)
		if err != nil {
			// Wallclock cap (②): the cap deadline can expire DURING an LLM
			// round-trip — chatOnce then fails with a context error. That is
			// not a run failure: finish gracefully (last substantive text,
			// nil error) exactly like the round-boundary cap hit. A
			// GOPI_TIMEOUT_SEC hit (its deadline earlier than the cap) keeps
			// the historical error path — a hard timeout IS a failure.
			if wallclockCap > 0 && !time.Now().Before(deadlineTime) && ctx.Err() != nil {
				wallclockExceeded = true
				endReason = runend.WallclockCap
				fmt.Fprintln(os.Stderr) // newline after streamed text
				return lastSubstantiveText(a), nil
			}
			// Signal-interrupt semantics: Interrupt() cancelled the ctx
			// mid-round-trip — the in-flight LLM stream aborts with a ctx
			// error. Map to the interrupt error so the defer closes the
			// session + ledger as a signal-interrupted failure.
			if a.interruptedBySignal() && ctx.Err() != nil {
				fmt.Fprintln(os.Stderr) // newline after streamed text
				return "", interruptErr
			}
			return "", err
		}
		content := res.content
		toolCalls := res.toolCalls

		// Stuck detector (①): ONE feed point immediately after response
		// extraction — BEFORE the finish_reason switch — so no continue
		// path can bypass it. The incident (t-1788173679) survived bug022's
		// end-turn exit precisely because truncated (finish_reason=length)
		// rounds reset consecutiveEndTurn and continue: 4276 rounds of
		// identical no-tool text. Here: tool rounds reset the streak,
		// identical no-tool text (50-rune prefix window) accumulates, and
		// maxStuckRounds in a row terminates the run + fires loop-stuck.
		stuck.observe(content, len(toolCalls) > 0)
		if stuck.tripped() {
			fmt.Fprintln(os.Stderr) // newline after streamed text
			if a.Logger != nil {
				a.Logger.Printf("⛔ stuck loop detected: %d consecutive no-tool rounds with same text (%q) — terminating (incident t-1788173679 net)",
					stuck.streak, stuck.prefix)
			}
			a.notifyLoopStuck(fmt.Sprintf("%d consecutive no-tool rounds repeating text %q — run terminated", stuck.streak, stuck.prefix))
			endReason = runend.StuckLoop
			return "", fmt.Errorf("agent loop stuck: %d consecutive rounds with no tool calls and identical text — terminated (see GOPI_TASK_NOTIFY loop-stuck)", stuck.streak)
		}

		// toolStreak detector (② bug055 — the toolful blind spot): fed at
		// the SAME single point as the stuck counter, immediately after
		// response extraction, so no continue path can bypass it. A round
		// with identical tool_use (name + normalized param hash + text
		// prefix) accumulates; parameter change (real work) or no tool_use
		// resets. Tripped → terminate + event=loop-stuck-toolful.
		toolStreak.observe(toolCalls, content)
		if toolStreak.tripped() {
			fmt.Fprintln(os.Stderr) // newline after streamed text
			if a.Logger != nil {
				a.Logger.Printf("⛔ stuck loop detected: %d consecutive identical tool_use rounds (%s %s) — terminating (incident Bond 718319 net)",
					toolStreak.streak, toolStreak.toolName, toolStreak.paramHash)
			}
			a.notifyLoopStuckToolful(fmt.Sprintf("%d consecutive identical tool_use rounds %s — run terminated",
				toolStreak.streak, toolStreak.summary()))
			endReason = runend.StuckToolLoop
			return "", fmt.Errorf("agent loop stuck: %d consecutive rounds with identical tool calls (tool=%s) — terminated (see GOPI_TASK_NOTIFY loop-stuck-toolful)",
				toolStreak.streak, toolStreak.toolName)
		}

		// Observation bound (③ §6.3 — the look-without-fixing loop): fed at
		// the SAME single point as ①/②, immediately after response
		// extraction, so no continue path can bypass it. A round that only
		// LOOKED (every tool_use is an observer: read/code_search/bash/…)
		// RECORDS its identity here; a MUTATOR round (write/edit/task/
		// memory/…), the polling tool `job` (detector ② owns polling,
		// bug055), or an unknown tool name is an ACT and resets. The streak
		// is decided in the post-execution feed below, where the round's
		// RESULT is known — repeat = same call AND same result.
		observation.observe(toolCalls, content, len(toolCalls) > 0)

		// Log assistant messages to session JSONL (text + tool_use blocks).
		if a.SessionLog != nil {
			if content != "" {
				a.SessionLog.LogAssistantText(content)
			}
			for _, tc := range toolCalls {
				a.SessionLog.LogAssistantToolUse(tc)
			}
		}

		// Crash-dump context: remember executed tool names (bounded ring).
		for _, tc := range toolCalls {
			a.recentToolCallNames = append(a.recentToolCallNames, tc.Name)
		}
		if len(a.recentToolCallNames) > recentToolCallNamesMax {
			a.recentToolCallNames = a.recentToolCallNames[len(a.recentToolCallNames)-recentToolCallNamesMax:]
		}

		// Parse the task list ONCE per round, surfacing any recovery/drop
		// warning so a BEGIN-without-END block is never silently discarded
		// (P0 — devdoc/tasklist-p0-evaluation.md). The helper returns the
		// tasks (nil when nothing parseable) plus a warning string ("" when
		// none); the warning is logged here and the tasks flow through every
		// normal downstream path (exit-check, TaskState.update, R3 fast-exit)
		// untouched — a recovered list is NOT second-class.
		tasks, warn := a.parseTaskList(content)
		if warn != "" {
			a.Logger.Printf("[gopi] %s", warn)
		}
		// A recovered/dropped block still counts as an emission for the decay
		// counter — the model did try to emit, and the recovery warning above
		// already covers the malformed case. Only a TOOL round with no markers
		// advances the counter: the prompt's cadence is "every 3 tool rounds"
		// (T4), and a no-tool round that omits the list is the contract's
		// blessed omit-as-receipt case (P1.5), not decay.
		switch {
		case tasks != nil:
			roundsSinceTaskList = 0
		case len(toolCalls) > 0:
			roundsSinceTaskList++
		}

		// Track the harness's view of the task list for the todo tool (A3).
		// nil (no markers this round) must not wipe previously tracked tasks.
		// Also update the stale-list counter (Report A §6.5): a round with a
		// fresh block resets it; a round with none (list already established)
		// increments it so the round-N nag can re-arm emission.
		if a.TaskState != nil {
			if tasks != nil {
				a.TaskState.update(tasks)
				a.roundsNoList = 0
			} else if a.TaskState.hasList {
				a.roundsNoList++
			}
		}

		// Reframe+evaluate merge: the reframe is now a TOOL CALL —
		// task(action=reframe), dispatched in the `case toolnames.ToolTask`
		// switch above via a.handleTaskReframe, which validates the verdict,
		// records telemetry, dispatches on it (roadmap auto-enqueue / failed
		// row) and swaps the [EPIC GOAL] message for the reframed text. The
		// former free-text [REF RAMED EPIC] block parse is GONE — a void slot
		// the harness had to guess at, with no decision attached.

		// (No stuck.observe here — the detector is fed ONCE at the top of
		// the round, before this switch, so truncated rounds count too.)

		// SELF-RECORDING (第一期): one extraction per ROUND, before the
		// finish_reason switch — the record is this round's own response, and a
		// truncated round has still recorded whatever it managed to write. Both
		// search spaces are passed (the answer body and the model's reasoning);
		// never touched; the shaped notes then become memory candidates.
		a.captureSelfNotes(a.recordSelfNotes(content, res.thinking))

		// AUTO-MEMORY (response): the SAME round, the OTHER object. The note
		// above is capped at the contract length, so a response whose value IS a
		// table or a diagram cannot carry it; this classifies the body the
		// console displayed and stores its shaped content. The two gates never
		// see the same text: captureResponseText strips the note block first,
		// and a body that was ONLY a note is refused by name
		// (selfnotecaptureresponse.go).
		a.captureResponseText(content)

		// Handle finish_reason — mapped from the Claude stop_reason.
		switch res.finishReason {
		case "length":
			if a.Logger != nil {
				a.Logger.Printf("⚠️ output truncated (finish_reason=length) — re-issuing; tool calls may be incomplete")
			}
			// Preserve the truncated text in context so the model sees what
			// it already produced, then inject a reminder. Truncated tool
			// calls must NOT enter history: the Claude API rejects tool_use
			// blocks without immediately-following tool_result blocks
			// (verified live). The model re-issues any missing actions.
			a.Ctx.AddAssistantMessageWithThinking(content, nil, res.thinking, res.thinkingSig)
			a.Ctx.AddUserMessage(
				harnessMessagePrefix + "Your previous response was truncated (token limit reached). " +
					"If you were calling tools, those tool calls may be incomplete. " +
					"Please re-issue any missing actions or continue your response.",
			)
			consecutiveEndTurn = 0
			continue
		case "content_filter":
			return "", fmt.Errorf("LLM response blocked by content filter")
		case "tool_calls":
			// Tool round — reset the end_turn counter.
			consecutiveEndTurn = 0
		case "stop", "":
			// Normal round end — the exit logic below decides whether the
			// model is truly done.
		default:
			if a.Logger != nil {
				a.Logger.Printf("⚠️ unknown finish_reason: %q", res.finishReason)
			}
		}

		// Log request metadata to session JSONL.
		if a.SessionLog != nil {
			p, h, m, comp := 0, 0, 0, 0
			if a.Cost != nil {
				p = a.Cost.TotalPromptTokens - prevPrompt
				h = a.Cost.TotalCacheHitTokens - prevHit
				m = a.Cost.TotalCacheMissTokens - prevMiss
				comp = a.Cost.TotalCompletionTokens - prevCompletion
			}
			a.SessionLog.LogRequest(i, p, h, m, comp)
		}

		if a.Debug {
			a.Logger.Printf("← STREAM [%d] content (%d chars):\n%s\n", i, len(content), content)
			if len(toolCalls) > 0 {
				a.Logger.Printf("  tool calls (%d):", len(toolCalls))
				for j, tc := range toolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
					a.Logger.Printf("    [%d] %s(%s)", j, tc.Name, string(argsJSON))
				}
			}
		}

		// Exit logic (bug022 fix): NEVER exit on toolCalls == 0 alone.
		// A round without tools may still be mid-task (e.g. right after the
		// model emits a task list). Require maxConsecutiveEndTurnRounds
		// consecutive no-tool rounds before exiting; the intermediate round
		// nudges the model to resume tool use.
		// R3 exception: when the round's task list is all-completed AND the
		// answer is substantive, the first end_turn IS the normal completion
		// — trust it and skip the guaranteed +1 confirmation round (bug022
		// guards mid-task stops, not declared completion).
		if len(toolCalls) == 0 {
			consecutiveEndTurn++
			if consecutiveEndTurn == 1 {
				// First end_turn carries the substantive answer — return it
				// on exit, not the (usually terse) confirmation.
				pendingFinalContent = content
			}
			a.Ctx.AddAssistantMessageWithThinking(content, nil, res.thinking, res.thinkingSig)
			// Framework hard-takeover of completion (memory-aware T2): the
			// LLM has no authority to declare the whole run done via free
			// text while tracked items lack committed write_task_area
			// conclusions. When the session has joined the working-area
			// contract, a no-tool exit round is intercepted here — BEFORE
			// both the R3 trusted-completion fast-exit and the bug022
			// maxConsecutiveEndTurnRounds exit — and a completion nudge is
			// injected instead (see pendingTaskAreaConclusionGuard; capped
			// at maxTaskAreaNudges so a non-compliant model is never
			// hard-blocked). The continue skips both exit paths this round.
			if a.pendingTaskAreaConclusionGuard() {
				continue
			}
			// P1.5 (compare-report RC2): completion is trusted either from
			// THIS round's all-completed list (legacy path) or from the
			// harness-tracked state (list established earlier, all completed,
			// possibly via todo — the contract explicitly blesses omitting
			// the end-of-run receipt list). Decoupling the fast-exit from the
			// end-of-run emission removes the incentive to hold the only list
			// for the final message (~21% plan-tailing sessions).
			if (tasks != nil && AllCompleted(tasks)) || a.trustedCompletion() {
				// Task-end memory_store enforcement (对称强制): the run is
				// complete but never stored — force the store once before the
				// final response so experience actually accumulates. Only here
				// (all-completed) and only once.
				if len(contentWithoutTaskList(content)) >= minFinalAnswerChars {
					// R3 trusted completion: substantive answer, skip the
					// bug022 confirmation round and exit on the first end_turn.
					if a.maybeEnforceCompletionStore() {
						continue
					}
					fmt.Fprintln(os.Stderr) // newline after streamed text
					a.warnExitingWithProgressing(tasks)
					if pendingFinalContent != "" {
						return pendingFinalContent, nil
					}
					return content, nil
				}
				// Terse completed answer (e.g. smoke "SMOKE-REMOVE-OK", a byte
				// count, or a one-line confirmation): not substantive enough
				// for the R3 first-end_turn trust, so the loop still needs the
				// bug022 confirmation round — but the task-end store must NOT
				// be bypassed by the terse-answer exit gap. Fire the
				// completion checkpoint now (it replaces the end-turn nudge:
				// the task IS complete); the injected checkpoint message
				// forces the model to call memory_store before the final
				// response, then the confirmation round exits normally.
				if a.maybeEnforceCompletionStore() {
					continue
				}
			}
			if consecutiveEndTurn >= maxConsecutiveEndTurnRounds {
				fmt.Fprintln(os.Stderr) // newline after streamed text
				a.warnExitingWithProgressing(tasks)
				if pendingFinalContent != "" {
					return pendingFinalContent, nil
				}
				return content, nil
			}
			// The model may resume tool calls next round — nudge it.
			a.Ctx.AddUserMessage(endTurnContinuationMessage)
			continue
		}
		consecutiveEndTurn = 0
		freeTextChars += len(content)
		a.Ctx.AddAssistantMessageWithThinking(content, toolCalls, res.thinking, res.thinkingSig)

		if a.Debug {
			a.Logger.Printf("🔧 TOOL CALLS [%d]: %d calls", i, len(toolCalls))
			for j, tc := range toolCalls {
				a.Logger.Printf("  [%d/%d] %s(%v)", j+1, len(toolCalls), tc.Name, tc.Arguments)
			}
		}

		// Execute this round's tool calls. The read-only subset runs
		// concurrently even inside mixed batches; side-effect tools run
		// serially (see executeToolBatch). Results come back in tool-call
		// order.
		elapsedToolRounds++ // a tool round elapsed (feeds the no-list nudge)
		results := a.executeToolBatchCtx(ctx, toolCalls)

		// toolStreak polling exemption: feed the round's RESULTS to the
		// detector — a polling round (job action=output) with identical
		// tool+args is legitimate waiting; only a byte-identical
		// RESULT (frozen polled state) accumulates. A changed result resets.
		// Tripped → same terminate path as the fingerprint signal below.
		toolStreak.observeResults(toolCalls, results)
		if toolStreak.tripped() {
			fmt.Fprintln(os.Stderr) // newline after streamed text
			if a.Logger != nil {
				a.Logger.Printf("⛔ stuck loop detected: %d consecutive identical poll results (%s %s) — terminating (frozen polled state)",
					toolStreak.streak, toolStreak.toolName, toolStreak.paramHash)
			}
			a.notifyLoopStuckToolful(fmt.Sprintf("%d consecutive identical poll results %s — run terminated",
				toolStreak.streak, toolStreak.summary()))
			endReason = runend.StuckToolLoop
			return "", fmt.Errorf("agent loop stuck: %d consecutive rounds with identical poll results (tool=%s) — terminated (see GOPI_TASK_NOTIFY loop-stuck-toolful)",
				toolStreak.streak, toolStreak.toolName)
		}

		// Observation bound (③) — the RESULT half of the repeat judgment,
		// fed once the round's tools have executed (mirrors
		// toolStreak.observeResults: same call AND same result is the only
		// repeat; a different argument or a result that MOVED is a fresh
		// observation and is never billed — bug055's changing-result
		// exemption, inherited by detector ③, per the epic for 01a0faac).
		// Tripped → same terminate path + event=loop-stuck-observation.
		observation.observeResults(results)
		if observation.tripped() {
			fmt.Fprintln(os.Stderr) // newline after streamed text
			if a.Logger != nil {
				a.Logger.Printf("⛔ stuck loop detected: %d consecutive repeated observation rounds (same call AND same result, %s) — terminating (look-without-fixing net, §6.3)",
					observation.streak, observation.summary())
			}
			a.notifyLoopStuckObservation(fmt.Sprintf("%d consecutive repeated observation rounds — same tool call and byte-identical result (%s) — run terminated",
				observation.streak, observation.summary()))
			endReason = runend.StuckObservation
			return "", fmt.Errorf("agent loop stuck: %d consecutive rounds repeating the same observation with an unchanged result — terminated (see GOPI_TASK_NOTIFY loop-stuck-observation)",
				observation.streak)
		}

		for j, tc := range toolCalls {
			// Prefix the elapsed tick (clock.go TickLabel) — every communication
			// carries it, so the LLM always has a live sense of elapsed time.
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", j)
			}
			// Stamp every tool result with its own tool_use_id: the LLM must
			// copy this id verbatim into context_trim's call_id when trimming
			// a dead end — without it the model hallucinates a call_… string
			// and the trim safely no-ops (verified live: models fabricate
			// plausible-looking call ids). Feedback/update address a memory
			// row by its record id (#N), never by this call id. The header sits in front of the
			// payload, so the original content stays intact.
			//
			// Assembly order matters: the tick wraps the STAMPED result
			// ("<tick> [tool_use id: X]\n<payload>") so the payload appears
			// exactly once — an earlier order appended the raw payload twice
			// ("<tick> <payload>[tool_use id: X]\n<payload>"), doubling
			// every tool result on the wire and in the session log (found
			// during the P2 A/B exercise).
			result := a.withTick(fmt.Sprintf("[tool_use id: %s]\n%s", id, results[j]))
			if a.SessionLog != nil {
				a.SessionLog.LogToolResult(id, result)
			}
			a.Ctx.AddToolResult(id, result)
			if a.Debug {
				a.Logger.Printf("  [%d/%d] result: %d bytes", j+1, len(toolCalls), len(result))
			}
		}

		// Context slimming — WHITELIST semantics (nick design): unlike the
		// former blacklist (only rating=useless + explicit tool_use_id got
		// dropped, which the LLM almost never issued → slimming never ran),
		// the default is now DROP EVERY memory_search pair. Only a pair whose
		// returned records were explicitly voted useful (memory_feedback
		// rating=useful) survives. "No mention is also useless for this
		// session" — a search's hits that were never (or contradictorily)
		// voted useful are noise and stop being re-sent every round (saves
		// more context than deferred removal).
		//
		// Association is done by RECORD ID, parsed from each search pair's
		// own tool_result (the render embeds "#N tier=…" per hit) — NOT by
		// trusting an LLM-supplied tool_use_id (models hallucinate call ids,
		// seen in the search-remove smoke; record ids come straight from the
		// engine). A pair is kept iff at least one of the records it returned
		// was voted useful anywhere in this task's context. Conservative
		// boundary: a record shown in several searches keeps them all.
		//
		// DB-side feedback recording is untouched — the useful/useless/or
		// unused vote already landed in the memory_feedback table when the
		// tool dispatched; this is purely a context operation. Dedup is
		// enforced inside RemoveToolPair (a dropped pair is removed at most
		// once). Only this task's context is scanned (a.Ctx.Messages is the
		// whole current session = one task), so no historical search is ever
		// touched.
		a.whitelistContextSlim(currentMemorySearchIDs(toolCalls))

		// Dead-end nudge (prompt wiring, not auto-trim): after a round whose
		// results include a dead-end (errored/failed/no-value) tool call, and
		// unless we already nudged this run, inject the short
		// deadEndTrimMessage so the model trims it with context_trim. The
		// trim is still a voluntary model call (proven in the session log),
		// but deepseek-v4-flash forgets the task-start priming by the time a
		// mid-task dead-end appears — this fires exactly then.
		if !a.deadEndNudged && resultsHaveDeadEnd(toolCalls, results) {
			a.deadEndNudged = true
			a.Ctx.AddUserMessage(deadEndTrimMessage)
			if a.Debug {
				a.Logger.Printf("  dead-end nudge injected (after %d result(s))", len(results))
			}
		}

		// write_task_area commit signal (POC H3/H4): a successful commit this
		// round marks the completed working region committed. The recorded
		// conclusions are carried forward as AUTHORITATIVE context (a user
		// message the model treats as fact), and the region between this
		// commit and the previous one is trimmed — the item's analysis is
		// gone, only its conclusion survives. Never implied-but-missing: the
		// carry-forward block is injected BEFORE any trim so a referenced
		// conclusion can never be fabricated (H4 safety red line).
		if a.taskAreaDirty {
			a.taskAreaDirty = false
			a.injectTaskAreaConclusions()
			a.trimCommittedTaskArea()
			// Review-time memory-consult accountability checkpoint (candidate
			// C): the first commit is the run's first REAL evidence about what
			// it would have needed — ask the one-shot question there. It is
			// armed before the loop so the first commit always carries it.
			a.maybeInjectConsultCheckpoint()
			// ===== post-commit review moved to a structured TOOL CALL
			// (tool-family merge — task family, nick 2026-09-10) =====
			// The old injectPostCommitReview text prompt (which asked the
			// model to free-text a REVIEW-VERDICT: reply that NOTHING parsed
			// — a void slot) is gone. Post-commit review is now the model
			// CALLING task(action=review, verdict=A..E): the dispatch runs
			// a.handleTaskReview, which validates the verdict, latches and
			// receipt-processes it, detects a closing (whole-epic) review,
			// and returns the verdict-driven action. The carry-forward +
			// trim above still run on every real commit; the review tool
			// call (prompted by the system TASK MANAGEMENT RULES) closes the
			// checkpoint.
		}

	}

	// Max-iterations bail is a REAL exit path too — the harness-tracked state
	// is the only completion view here (this round's `tasks` is out of scope).
	// It is engine-forced (the budget, not the model, ended the run), so it
	// carries the truncation reason like the cap and the stuck nets.
	a.warnExitingWithProgressing(nil)
	endReason = runend.MaxIterations
	return "", fmt.Errorf("max iterations (%d) reached", a.MaxIterations)
}

// Chat sends a single free-form user message to the model and returns its
// text answer WITHOUT running the full ReAct task loop. It is the simplified
// chat-mode path the Epic asks for — a clean Q&A turn that deliberately skips
// every piece of task scaffolding:
//
//   - no tools (nil tool defs — chat is conversation, not a tool run),
//   - a minimal chat system prompt (chatSystemPrompt) instead of the ~4K
//     GoPi tool-routing/system prompt,
//   - no [EPIC GOAL] framing, no task-list nudges, no task-ledger
//     registration, no task-start search / completion-store enforcement,
//   - a single round-trip (one chatOnce) — no ReAct iteration, no timeout/
//     iteration/finish-reason bookkeeping.
//
// history carries the accumulated conversation (seeded verbatim, mirroring
// Resume) and userInput is appended verbatim as the next turn. Afterwards
// a.Ctx.Messages holds the full conversation so the caller can capture it as
// the next turn's seed. Text streams to OnText (if set) for real-time display.
// ChatOnCarrier is Chat with the CARRIER named — the same one-line publication
// ChatOpsOnCarrier performs, for the tool-free path (a carrier can enable tools on
// one turn and not another; the boundary must know WHICH face every time).
func (a *Agent) ChatOnCarrier(carrier, userInput string, history []deepseek.Message, images ...*deepseek.ClaudeImageSource) (result string, err error) {
	defer a.publishChatCarrier(carrier)()
	return a.Chat(userInput, history, images...)
}

func (a *Agent) Chat(userInput string, history []deepseek.Message, images ...*deepseek.ClaudeImageSource) (result string, err error) {
	startTime := time.Now()

	// Cost is a chat-path invariant: a hand-built Agent (tests, embedded
	// carriers) may have left it nil — the turn's token accounting and the
	// chats-table registration both read it.
	if a.Cost == nil {
		a.Cost = &deepseek.CostTracker{}
	}

	// Mark this run as the chat (no-tools, no-task-scaffolding) path so
	// chatOnce suppresses the raw stderr text print (the answer surfaces
	// once, via OnText on stdout). Reset on exit so a reused agent still
	// runs the normal task loop. The turn's OWN message is published beside it
	// — and CLEARED on exit — because it is the chat write boundary's
	// authorization channel: a reused agent must not carry one turn's
	// authorization into the next.
	a.ChatMode = true
	defer func() { a.ChatMode = false }()
	defer a.publishChatTurn(userInput)()

	// Seed the accumulated history verbatim, then append the user's message
	// as an ordinary user turn (no EPIC framing). R5: the turn's images ride
	// in ONE user message — image blocks first, text after (the wire's user
	// case builds exactly that order) — the same shape ChatOps and the task
	// path's runEngine use.
	a.Ctx.Messages = append([]deepseek.Message{}, history...)
	if len(images) > 0 {
		a.Ctx.AddMessage(deepseek.Message{Role: "user", Content: userInput, Images: derefImages(images)})
	} else {
		a.Ctx.AddUserMessage(userInput)
	}
	if a.SessionLog != nil {
		// CONSUME-ONCE, the same contract takePrefetchSeed states: the face is
		// published for THIS turn and cleared here, so a caller that publishes
		// nothing cannot have the previous turn's face inherited and stamp a
		// human line as self-typed (or the reverse).
		source := a.TurnSource
		a.TurnSource = ""
		// THE MOMENT IS CONSUMED ONCE, exactly like the face above: a caller
		// that published one (the banner painted it) gets that value in the
		// row, and a caller that published nothing gets the row minted at the
		// write — never the previous turn's moment, which would put a banner's
		// clock on a row it does not belong to.
		at := a.TurnOpenedAt
		a.TurnOpenedAt = time.Time{}
		a.SessionLog.LogUserFromAt(newUUIDv4(), userInput, source, at)
	}

	// Max tokens: explicit Agent.MaxTokens wins, else the package default.
	maxTokens := a.MaxTokens
	if maxTokens <= 0 {
		maxTokens = deepseek.DefaultClaudeMaxTokens
	}

	// Thinking/effort derivation mirrors the ReAct loop: effort goes off the
	// wire whenever thinking is off, and the pause/probe cycle applies here too
	// (a bare prompt is one round, so a probe is the common case).
	thinking := a.Thinking
	wireEffort := a.ReasoningEffort
	paused, probe := a.thinkingPausedForRound()
	a.reportEffectiveThinkingBudget(thinkingBudgetOf(thinking), maxTokens, effectiveThinkingBudgetOf(thinking, maxTokens))
	if (paused && !probe) || thinking == nil || thinking.Type == thinkmode.Disabled {
		thinking = &deepseek.Thinking{Type: thinkmode.Disabled}
		wireEffort = ""
	}
	a.thinkingProbeRound = probe

	// THE ROUND BOUNDARY of a NO-TOOLS chat turn — the same one place, asked at
	// the same moment: an inbound message (`gopi notify --session <id>
	// --message <text>`) is merged into the ONE notice queue and taken through
	// the ONE take point BEFORE the request is built, so the model reads it in
	// THIS round rather than discovering it at the next turn. Without this arm a
	// default `gopi chat` (no --tools, therefore `Chat` rather than `ChatOps`)
	// would never see a delivered message at all — the feature would exist and
	// be unreachable, which is the failure Rule 31 exists for. This turn has
	// exactly one boundary; this is it.
	a.loadSessionInbox()
	for _, n := range a.noticesToInject(tools.TakeJobNotices()) {
		a.injectJobNotice(n)
	}

	// THE NOTE AS A SUBSTITUTE (turnfold.go) — the no-tools chat turn's ONE
	// boundary. The history this turn was seeded with has had its previous
	// turns folded already (the fold's shape is what the carrier captured as the
	// next turn's seed), so this call is the cheap idempotent pass that keeps a
	// conversation whose carrier did NOT fold in step.
	a.foldCompletedTurns()

	// THE WARNING THIS PATH USED TO LACK (context_pressure.go). Agent.Chat never
	// called trimContext, so a no-tools chat session could fill its window and
	// reach the send-time cap without telling the operator anything — measured
	// 2026-11-08: `gopi chat --tools` warned, `gopi chat` was silent, on the same
	// conversation. It is placed at the same boundary the tools path uses (just
	// before the request is built), and it only WARNS: this path has never
	// reduced anything, and it still does not.
	a.warnContextPressure()

	// Build a chat request: the chat system prompt, the project's GOPI.md when
	// one was loaded (its own system block — chat_ops.go carries the shared
	// rationale), NO tools. With empty tools no tool_use is ever emitted.
	buildChatRequest := func() *deepseek.ClaudeRequest {
		return deepseek.BuildClaudeRequest(
			a.Model,
			a.requestMessages(a.KeepThinking),
			chatSystemPromptServed(a),
			a.Ctx.ProjectContext,
			nil, // no tools
			maxTokens,
			thinking,
			wireEffort,
		)
	}

	ctx, cancel, endRun := a.runScopedContext(a.Timeout)
	defer endRun()
	defer cancel()

	// Single round-trip through the SEND-TIME GATE (chatOnceGated counts the
	// request, refuses one over the cap, and hands a fitting one to chatOnce —
	// which owns streaming + fallback + the thinking budget).
	res, err := a.chatOnceGated(ctx, buildChatRequest, 0)
	// PER-TURN INTERRUPT (task 01a0e4390): the OPERATOR abandoned this turn with
	// a run-scoped cancel (Ctrl+C at the incoming prompt), and the session stays
	// open. Report it as its own outcome — not as a failure and NOT as a
	// completed answer — so the carrier prints the interrupt marker and the JSONL
	// records the turn as interrupted, with the partial answer's size.
	//
	// THE CHECK IS UNCONDITIONAL, and that is not defensive coding — it is the
	// measured shape of an aborted stream (found by the interrupt test itself).
	// A cancelled SSE read ENDS the stream loop normally: whatever text had
	// arrived is returned as the result with a NIL error, so an interrupt checked
	// only on the error path is invisible in exactly the case it exists for — the
	// operator stops the answer, and the run reports a triumphant completed turn
	// whose text stops mid-sentence. A truncated answer is not an answer: it must
	// not be logged as the assistant's message, not counted as a turn, and not
	// seed the next turn's context.
	if a.TurnInterrupted(ctx) {
		// The witness is written by the RUN that was abandoned (it is the only
		// party that saw every delta) and the carrier reads the size back off it
		// for its marker.
		a.recordTurnInterruption(InterruptSourceOperator)
		return "", errTurnInterrupted
	}
	if err != nil {
		// Signal-interrupt semantics (chat-once/bare-prompt zombie fix):
		// Interrupt() cancelled the ctx mid-turn — surface the interrupt
		// error so the caller (runChat's turn loop) unwinds to finishChat
		// and writes session_end + closes the chat row instead of leaving
		// them open.
		//
		// AND RECORD IT. This arm is the no-tools sibling of chat_ops.go's
		// signal branch, and it carried the same hole: the turn ended, the
		// screen said so, and session.jsonl said nothing (measured, session
		// 07d98407). The source is the SIGNAL's and the reason its name — see
		// chat_ops.go's comment for why neither may be the operator's.
		if a.interruptedBySignal() && ctx.Err() != nil {
			a.recordTurnInterruption(InterruptSourceSignal)
			return "", interruptErr
		}
		return "", err
	}

	// Record the assistant answer in context (feeds the next turn's seed) and
	// log it to the session JSONL.
	// SELF-RECORDING (第一期 + 4/5): the chat surface's own record. The chat
	// path never walks the task loop, so this is the ONE place a chat round's
	// [IMPORTANT] block becomes a durable note — the same extractor and the same
	// side-channel file the task path writes, reached from the carrier that
	// actually produced the text (res.content / res.thinking). Placement is
	// BEFORE the assistant message enters the context and before the JSONL
	// record, so a recording failure is reported without the round being lost.
	a.captureSelfNotes(a.recordSelfNotes(res.content, res.thinking))
	// AUTO-MEMORY for the chat answer: the same one-per-round response capture
	// the tool-enabled path and the task loop do (loop.go) — a table or a
	// diagram in the answer this console displayed becomes a memory row, which
	// is the object the note gate could never carry (maxSelfRecordNoteChars).
	a.captureResponseText(res.content)
	a.Ctx.AddAssistantMessageWithThinking(res.content, nil, res.thinking, res.thinkingSig)
	if a.SessionLog != nil {
		if res.content != "" {
			a.SessionLog.LogAssistantText(res.content)
		}
		// RUNNING TOTALS, and the row SAYS SO (LogRequestCumulative). The chat
		// turn's CostTracker is a segment-cumulative reading, so a reader must
		// fold the column as a running total — measured 2026-11-01: reading it
		// as an increment inflated session d6120d04 by 2x (219,191 against a
		// true 109,705) and 34bb9adc by 48.6x.
		p, h, m, comp := 0, 0, 0, 0
		if a.Cost != nil {
			p = a.Cost.TotalPromptTokens
			h = a.Cost.TotalCacheHitTokens
			m = a.Cost.TotalCacheMissTokens
			comp = a.Cost.TotalCompletionTokens
		}
		a.SessionLog.LogRequestCumulative(0, p, h, m, comp)
	}

	// Close out this turn's session log + raw log. In persistent-chat mode
	// (PersistChatSession, gopi chat reusing one agent) the session is NOT
	// closed per turn — it stays open so every turn appends to the SAME
	// session id + session JSONL + raw log (the Epic's single-session ask);
	// the caller closes it once at chat exit via CloseChatSession. We still
	// Sync() so each turn's events are durable on disk (a crash mid-chat
	// loses at most the current turn, never a whole multi-turn conversation).
	// Otherwise each turn is a clean self-contained session (mirroring the
	// fresh-agent-per-turn flow). Either way no ⏱ task-stats line and no
	// task-ledger row from here — chat handles its own registration.
	if a.PersistChatSession {
		if a.SessionLog != nil {
			a.SessionLog.Sync()
		}
		if a.RawLog != nil {
			a.RawLog.Sync()
		}
	} else {
		elapsed := time.Since(startTime)
		if a.RawLog != nil {
			a.RawLog.Close() //stylegate:ignore raw-log flush at exit; same best-effort audit channel as the session log
		}
		if a.SessionLog != nil {
			exitCode := 0
			if err != nil {
				exitCode = 1
			}
			totalTokens := 0
			if a.Cost != nil {
				totalTokens = a.Cost.TotalPromptTokens + a.Cost.TotalCompletionTokens
			}
			a.SessionLog.LogSessionEnd(exitCode, 1, totalTokens, elapsed.Seconds(), nil, a.endCauseFor(exitCode))
			a.SessionLog.Close() //stylegate:ignore session log flush at exit; the JSONL is append-only and best-effort
		}
	}

	return res.content, nil
}

// CloseChatSession finalizes a persistent chat session (PersistChatSession):
// it writes the single session_end event and closes the session + raw logs.
// Only called ONCE at chat exit (the /quit or Ctrl+D path in runChat) so the
// one chat session gets exactly one session_end, carrying the accumulated
// cost across all turns. No-op when the session was already closed per-turn
// (PersistChatSession false) or nothing is open.
//
// cause is HOW this run ended, and it is a parameter because the carrier is the
// only party that knows: a SIGTERM and a closed stdin both reach this function,
// and until task 01a163f8 they produced identical records. The caller states
// what it observed; a Signal already recorded on the agent (the watcher sets
// CloseSignal at the moment it DECODED the signal, which is the only moment it
// is known) WINS over the caller's word, since a signal that arrived is the
// more specific fact — see agentEndCause.
func (a *Agent) CloseChatSession(exitCode, iterations, tokens int, elapsedSec float64, cause EndCause) {
	if a.RawLog != nil {
		a.RawLog.Close() //stylegate:ignore raw-log flush at exit; same best-effort audit channel as the session log
	}
	if a.SessionLog != nil {
		cause = a.agentEndCause(exitCode, cause)
		var failure *ErrorRecord
		if exitCode != 0 {
			failure = a.collectErrorRecord(nil, iterations, tokens, elapsedSec)
			a.SessionLog.WriteCrashDump(failure)
		}
		a.SessionLog.LogSessionEnd(exitCode, iterations, tokens, elapsedSec, failure, cause)
		a.SessionLog.Close() //stylegate:ignore session log flush at exit; the JSONL is append-only and best-effort
	}
}

// agentEndCause resolves the ending cause from the two facts this package owns,
// with ONE precedence order (Rule 2: one reader, one rule).
//
// A RECORDED SIGNAL IS THE STRONGEST FACT. The watcher in each carrier sets
// CloseSignal on the goroutine that decoded the signal, so its presence means
// "a terminating signal reached this process and we unwound through the normal
// close-out" — more specific than any reason the close-out caller can state,
// and the one fact an operator reading a kill afterwards needs. Everything else
// defers to the caller, and a caller that states nothing gets a reason derived
// from the exit code rather than a silent zero.
func (a *Agent) agentEndCause(exitCode int, stated EndCause) EndCause {
	if a.CloseSignal != "" {
		return EndCause{Reason: sessionevent.SessionEndReasonSignalled, Signal: a.CloseSignal}
	}
	if stated.Reason != sessionevent.SessionEndReasonNone {
		return stated
	}
	return a.endCauseFor(exitCode)
}

// endCauseFor is the cause derived from the exit code ALONE — the fallback for
// the close-out paths that observe no input event and no signal (the one-shot
// face, the non-persistent per-turn path, the web and term carriers). It is
// deliberately coarse: those paths know the run finished or failed, and saying
// exactly that is honest, whereas reusing a chat leg's word would be a lie
// (Rule 15).
func (a *Agent) endCauseFor(exitCode int) EndCause {
	if a.CloseSignal != "" {
		return EndCause{Reason: sessionevent.SessionEndReasonSignalled, Signal: a.CloseSignal}
	}
	if exitCode != 0 {
		return EndCause{Reason: sessionevent.SessionEndReasonRunFailed}
	}
	return EndCause{Reason: sessionevent.SessionEndReasonComplete}
}

// maybeEnforceCompletionStore is the harness-side half of the task-end
// memory_store enforcement (对称强制 with the task-start memory_search). It is
// called at the all-completed completion point, just before the run would
// return. If the run already stored (memoryStored) or the checkpoint was
// already injected (completionStorePrompted), it leaves the loop to exit.
// Otherwise it injects the one-shot completionCheckpointMessage user message
// and returns true so the caller `continue`s — forcing the model to call
// memory_store before the final response WITHOUT needing LLM volition or a
// user prompt. It never fires more than once per run (task-end only, not
// per-round), so it cannot spam.
// maybeInjectTaskStartSearch is the harness-side half of the task-start
// memory_search enforcement (对称强制 with the task-end completion store). It
// is called once before the first round: if the run already searched
// (memorySearched), was already prompted (taskStartSearchPrompted), or
// enforcement is disabled (NoEnforceMemory), it is a no-op. Otherwise it
// injects the one-shot taskStartSearchMessage user message and returns true —
// forcing the loop to begin with a search WITHOUT needing LLM volition or a
// user prompt. It never fires more than once per run, so it cannot spam.
func (a *Agent) maybeInjectTaskStartSearch() bool {
	if a.NoEnforceMemory || a.memorySearched || a.taskStartSearchPrompted {
		return false
	}
	a.taskStartSearchPrompted = true
	a.Ctx.AddUserMessage(taskStartSearchMessage)
	// M2 hot-resident injection (hot = 常驻注入候选): the hot-tier digest is
	// appended to the same user message — existence hints the model sees
	// without searching. Best-effort: MemoryHotResident returns "" on any
	// error (DB missing, schema legacy, timeout), in which case nothing is
	// added and the checkpoint message stands alone. Never blocks the start.
	digest := tools.MemoryHotResident()
	if digest != "" {
		a.Ctx.AddUserMessage(hotResidentMessagePrefix + digest)
	}
	// Auditability: the injected messages go to the session log too — a Rule
	// 11 check (or a future debug) reads the JSONL, and an unlogged wire
	// message is indistinguishable from a missing one.
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), taskStartSearchMessage)
		if digest != "" {
			a.SessionLog.LogUser(newUUIDv4(), hotResidentMessagePrefix+digest)
		}
	}
	if a.Logger != nil {
		if digest != "" {
			a.Logger.Printf("🧠 hot-resident digest injected (%d bytes)", len(digest))
		}
		a.Logger.Printf("🧠 task-start checkpoint: forcing memory_search before first round")
	}
	return true
}

// hotResidentMessagePrefix frames the hot-tier digest: a labeled hint block,
// not a search replacement (the task-start memory(action=search) requirement
// above stays mandatory).
const hotResidentMessagePrefix = harnessMessagePrefix + "Hot-tier resident candidates (hot = 常驻候选, updated per task start): " +
	"these distilled lessons are currently tier=hot — consult them directly when relevant; " +
	"they do NOT replace the memory(action=search) below.\n"

func (a *Agent) maybeEnforceCompletionStore() bool {
	if a.NoEnforceMemory || a.memoryStored || a.completionStorePrompted {
		return false
	}
	a.completionStorePrompted = true
	a.Ctx.AddUserMessage(completionCheckpointMessage)
	if a.Logger != nil {
		a.Logger.Printf("🧠 completion checkpoint: forcing task-end memory_store before final response")
	}
	return true
}

// armConsultCheckpoint arms the review-time memory-consult checkpoint. Called
// once per run from the start-of-run path; the checkpoint itself fires at the
// first commit (maybeInjectConsultCheckpoint). The armed flag is the ZERO
// value (false) for any Agent that never went through start-of-run — so this
// instrument stays out of every unrelated hand-built unit test.
func (a *Agent) armConsultCheckpoint() {
	a.consultArmed = true
}

// maybeInjectConsultCheckpoint is the review-time memory-consult
// accountability checkpoint (memory-consult epic, candidate C — the data
// instrument). It is called when the run's FIRST item commits: the moment the
// run holds evidence about what a memory search would have saved.
//
// Shape follows the proven one-shot checkpoint siblings above
// (maybeInjectTaskStartSearch / maybeEnforceCompletionStore):
//   - ARMED before the loop (armConsultCheckpoint) and released on the first
//     call, so it cannot be skipped by the model — no LLM volition is
//     consulted, exactly like the task-start search;
//   - one-shot (the consultCheckpointPrompted latch) and only at the first
//     commit, so it can never spam a long run;
//   - injected as a user message appended at the round boundary (the
//     deadEndTrimMessage precedent), NOT a promptSections edit — the static
//     system prefix stays cache-friendly;
//   - DISABLED BY ITS OWN SWITCH (NoEnforceMemoryConsult), so the measurement
//     instrument drops out without touching the task-start enforcement — and
//     by NoEnforceMemory as well, which already means "every harness memory
//     checkpoint off" (an A/B baseline must stay untouched by a third one);
//   - written to the session log with the marker constant, so the telemetry in
//     trajectory.go sees it (an unlogged injection is indistinguishable from a
//     missing one).
func (a *Agent) maybeInjectConsultCheckpoint() bool {
	if !a.consultArmed || a.NoEnforceMemory || a.NoEnforceMemoryConsult || a.consultCheckpointPrompted {
		return false
	}
	a.consultCheckpointPrompted = true
	a.Ctx.AddUserMessage(consultCheckpointMessage)
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), consultCheckpointMessage)
	}
	if a.Logger != nil {
		a.Logger.Printf("🧠 review-time memory-consult checkpoint: asking which work a search would have saved")
	}
	return true
}

// parseTaskList parses a task list from the round's content and returns the
// tasks plus a warning string ("" when none). It wraps ParseTaskList so the
// P0 recovery/drop warnings (ErrTaskListRecovered / ErrTaskListUnparseable)
// are surfaced with session-identifiable context and logged exactly once per
// round by the caller, eliminating the old `tasks, _ :=` silent-drop. A
// recovered list returns non-nil tasks (flows through every normal path);
// an unparseable BEGIN-without-END returns nil tasks (nothing fabricated).
func (a *Agent) parseTaskList(content string) ([]TaskItem, string) {
	tasks, err := ParseTaskList(content)
	switch {
	case errors.Is(err, ErrTaskListRecovered):
		return tasks, fmt.Sprintf("⚠️ recovered task list from BEGIN-without-END (session %s): %d task(s); LLM likely forgot [TASK_LIST_END]",
			a.sessionLabel(), len(tasks))
	case errors.Is(err, ErrTaskListUnparseable):
		return nil, fmt.Sprintf("⚠️ task list dropped — BEGIN-without-END with no parseable JSON (session %s); no garbage list produced", a.sessionLabel())
	default:
		return tasks, ""
	}
}

// sessionLabel returns a short human-identifiable session reference for
// warnings, or "?" when no session log is attached.
func (a *Agent) sessionLabel() string {
	if a.SessionLog != nil {
		return a.SessionLog.SessionID()
	}
	return "?"
}

// chatResult is the outcome of a single LLM round-trip via the Claude API.
type chatResult struct {
	content      string
	toolCalls    []deepseek.ToolCall
	finishReason string
	thinking     string // thinking blocks concatenated (replayed in assistant history)
	thinkingSig  string // last non-empty thinking signature
}

// thinkingPausedForRound reports whether the round about to be sent runs with
// thinking paused, and whether THIS round is a recovery probe (thinking
// enabled again for one round to test whether the runaway has passed).
//
// The pause is not permanent (see thinkingProbeSpacing): after a streak of
// maxThinkingAbortStreak aborted rounds the following rounds go out
// thinking-disabled, but every thinkingProbeSpacing-th one is sent with
// thinking enabled so a run whose thinking has settled down REGAINS reasoning
// instead of silently losing it for hours.
func (a *Agent) thinkingPausedForRound() (paused, probe bool) {
	if !a.thinkingForceDisabled {
		return false, false
	}
	if a.thinkingPauseRounds >= thinkingProbeSpacing {
		a.thinkingPauseRounds = 0
		return false, true
	}
	a.thinkingPauseRounds++
	return true, false
}

// thinkingBudgetOf returns the budget the operator configured (0 = "unset —
// the engine default applies", which needs no clamp report because the builder
// already derives it from max_tokens).
func thinkingBudgetOf(thinking *deepseek.Thinking) int {
	if thinking == nil || thinking.Type == thinkmode.Disabled {
		return 0
	}
	return thinking.BudgetTokens
}

// effectiveThinkingBudgetOf returns the budget the request will actually carry
// after the builder's max_tokens/2 clamp (deepseek.EffectiveThinkingBudget) —
// the number the harness aborts a round at.
func effectiveThinkingBudgetOf(thinking *deepseek.Thinking, maxTokens int) int {
	if thinking == nil || thinking.Type == thinkmode.Disabled {
		return 0
	}
	return deepseek.EffectiveThinkingBudget(maxTokens, thinking.BudgetTokens)
}

// reportEffectiveThinkingBudget logs the thinking budget actually in force for
// this run, once, and only when it differs from what the operator configured.
//
// Rule 15 (no silent failure): the request builder clamps the configured
// budget to max_tokens/2 (the API requires budget_tokens < max_tokens), so
// setting GOPI_THINKING_BUDGET=16384 with the default max_tokens=16384 yields
// an EFFECTIVE budget of 8192 — which is also the harness's abort point. Two
// aborted rounds then pause thinking, and nothing on the wire explained why.
// The line names both numbers and the knob to move.
func (a *Agent) reportEffectiveThinkingBudget(configured, maxTokens, effective int) {
	if a.thinkingBudgetReported || a.Logger == nil {
		return
	}
	a.thinkingBudgetReported = true
	if configured <= 0 || configured == effective {
		return
	}
	a.Logger.Printf("ℹ️ thinking budget %d clamped to %d (max_tokens/2 = %d/2): the endpoint ignores budget_tokens, so the harness aborts a round whose streamed thinking passes %d tokens (~%d chars) and re-issues it thinking-disabled. Raise GOPI_MAX_TOKENS to keep a larger thinking budget.",
		configured, effective, maxTokens, effective, effective*thinkingCharsPerToken)
}

// chatOnce performs a single Claude Messages round-trip and returns the
// accumulated text content, tool calls, finish reason and thinking blocks.
//
// It prefers streaming via ClaudeChatStream() so text appears on stderr in
// real-time. If the stream cannot be established, it falls back to the
// non-streaming ClaudeChat() — the full content is then printed to stderr
// at once.
//
// Thinking budget enforcement: DeepSeek's Anthropic-compatible endpoint
// accepts thinking.budget_tokens but does NOT enforce it (verified live
// 2026-08-21: budgets of 128/512/2048 all produced ~15k tokens of thinking,
// max_tokens exhausted, zero text — see devdoc/thinking-budget.md). The
// harness enforces the budget instead: when a round's streamed thinking
// exceeds the budget, the stream is aborted and the round re-issued once
// with thinking disabled (verified honored: zero thinking, full answer).
// Rounds that stay under the budget are untouched — light thinking stays on.
func (a *Agent) chatOnce(ctx context.Context, req *deepseek.ClaudeRequest, iter int) (*chatResult, error) {
	// A recovery-probe round is consumed exactly once: the re-issued
	// (thinking-disabled) attempt of the SAME round must not be read as a
	// probe, or a probe that blows the budget would look like a fresh streak.
	probe := a.thinkingProbeRound
	a.thinkingProbeRound = false
	for attempt := 0; ; attempt++ {
		res, overBudget, err := a.chatOnceAttempt(ctx, req, iter, attempt)
		if err != nil || !overBudget || attempt > 0 {
			// A thinking-enabled round that completed within budget resets the
			// abort streak; re-issued (disabled) rounds leave it untouched so
			// two aborts in a row still pause thinking. A successful round also
			// ENDS a pause — the runaway has demonstrably passed, so a probe
			// that comes back under budget restores thinking for good.
			if err == nil && attempt == 0 {
				a.thinkingAbortStreak = 0
				if a.thinkingForceDisabled && probe {
					a.thinkingForceDisabled = false
					a.thinkingPauseRounds = 0
					if a.Logger != nil {
						a.Logger.Printf("✅ thinking recovery probe finished within budget — thinking is back on for this run")
					}
				}
			}
			return res, err
		}
		// Over-budget thinking: abort the stream and re-issue with thinking
		// disabled. When aborts are consecutive enough, PAUSE thinking for the
		// next rounds — the double prefill of every abort round dominates wall
		// time (gopi-race post-mortem: 160-600s vs 6-10s).
		//
		// A probe round that blew the budget re-engages the pause IMMEDIATELY
		// rather than after another streak: the run already proved twice that
		// its thinking runs away, and a second paid double-prefill buys nothing.
		a.thinkingAbortStreak++
		switch {
		case a.thinkingAbortStreak >= maxThinkingAbortStreak:
			a.thinkingForceDisabled = true
			a.thinkingPauseRounds = 0
			if a.Logger != nil {
				a.Logger.Printf("⚠️ thinking exceeded the budget %d rounds in a row — pausing thinking for this run (retried once every %d rounds; this is per-run state, not a session setting)",
					maxThinkingAbortStreak, thinkingProbeSpacing)
			}
		case probe:
			a.thinkingPauseRounds = 0
			if a.Logger != nil {
				a.Logger.Printf("⚠️ the thinking recovery probe blew the %d-token budget — keeping thinking paused for another %d rounds",
					req.Thinking.BudgetTokens, thinkingProbeSpacing)
			}
		default:
			if a.Logger != nil {
				a.Logger.Printf("⚠️ thinking exceeded %d-token budget — re-issuing round with thinking disabled",
					req.Thinking.BudgetTokens)
			}
		}
		req.Thinking = &deepseek.ClaudeThinking{Type: thinkmode.Disabled}
	}
}

// chatOnceAttempt is a single attempt of a Claude round-trip. overBudget
// reports that the round's thinking blew through the request's budget and
// must be re-issued (chatOnce retries with thinking disabled); such rounds
// return no result — the stream was aborted mid-thinking.
func (a *Agent) chatOnceAttempt(ctx context.Context, req *deepseek.ClaudeRequest, iter, attempt int) (*chatResult, bool, error) {
	// thinkingBudget is the harness-enforced per-round thinking cap in tokens.
	// 0 = no cap (thinking disabled for this request, or no thinking at all).
	thinkingBudget := 0
	if req.Thinking != nil && req.Thinking.Type == thinkmode.Enabled {
		thinkingBudget = req.Thinking.BudgetTokens
	}

	// Per-attempt context: aborting an over-budget stream must not kill the
	// run-level context — the re-issued attempt needs it alive.
	attemptCtx, attemptCancel := context.WithCancel(ctx)
	defer attemptCancel()

	// THE LOCAL PREDICTION IS READ BEFORE THE RESPONSE (the epic's gap B). This is
	// the number the status row is showing right now — CalibratedTokens() on the
	// context as it stands before this request's answer is folded back in — so the
	// reconciliation records the pair a READER compares, not an internal
	// intermediate. Read here, at the top of the attempt, because every later
	// point in this function may already have absorbed part of the answer.
	predictedPromptTokens := a.Ctx.CalibratedTokens()

	events, streamErr := a.Client.ClaudeChatStream(attemptCtx, req)
	if streamErr != nil {
		// ACCOUNT-LEVEL failure (402 billing / vendor account code — e.g. GLM
		// 1113 "余额不足" arriving as a 429): the provider account or the
		// credential is unusable. Re-sending the SAME request to the SAME
		// endpoint with the SAME key cannot succeed, so the non-streaming
		// "fallback" is skipped — it would only double the wasted wall time
		// on a dead account. Evidence: the 23:00 HP worker death log
		// (devdoc/night-report-20260910.md) shows "after 3 retries,
		// fallback: ... after 3 retries" against one dead GLM credential.
		if deepseek.IsAccountLevelError(streamErr) {
			msg := deepseek.AccountLevelMessageOf(streamErr)
			if a.Logger != nil {
				a.Logger.Printf("%s", msg)
			}
			return nil, false, errors.New(msg)
		}
		// Fall back to non-streaming ClaudeChat() — e.g. the API rejects
		// the stream parameter or the SSE connection cannot be established.
		if a.Debug {
			a.Logger.Printf("⚠ ClaudeChatStream failed: %v (attempt %d) — falling back to ClaudeChat()", streamErr, attempt)
		}
		resp, chatErr := a.Client.ClaudeChat(attemptCtx, req)
		if chatErr != nil {
			if deepseek.IsAccountLevelError(chatErr) {
				msg := deepseek.AccountLevelMessageOf(chatErr)
				if a.Logger != nil {
					a.Logger.Printf("%s", msg)
				}
				return nil, false, errors.New(msg)
			}
			return nil, false, fmt.Errorf("LLM call failed (stream: %w, fallback: %w)", streamErr, chatErr)
		}
		if a.Cost != nil {
			a.Cost.AddClaudeUsage(resp.Usage)
		}
		if a.Ctx != nil && resp.Usage.HasPromptMeasurement() {
			a.Ctx.SetPromptTokens(resp.Usage.PromptSideTokens())
		}
		thinking, sig := resp.Thinking()

		// The non-streaming path cannot abort mid-thinking, so the budget
		// check happens after the fact: an over-budget response is discarded
		// (it spent everything on thinking — nothing useful in it) and the
		// round is re-issued with thinking disabled.
		if thinkingBudget > 0 && len(thinking) >= thinkingBudget*thinkingCharsPerToken {
			return nil, true, nil
		}

		// Show reasoning on the fallback path too, so the user still sees
		// thinking even when the stream could not be established. It goes
		// through the flow seam, so a carrier that owns the screen receives it
		// instead of having its layout written over.
		if thinking != "" && !a.HideThinking {
			// ONE destination, decided in ONE place (flowWriterFor): the
			// carrier's own seam when the chat layout owns the screen (the seam
			// receives the reasoning so a carrier that owns the screen is not
			// written over), os.Stderr byte-for-byte otherwise.
			//
			// The carrier branch keeps the marker bytes on that destination,
			// because internal/agent/thinking_seam_test.go pins the seam's stream
			// to the stderr path's exact bytes; the band's own SGR-aware renderer
			// is what keeps those bytes out of its cell arithmetic. The markers are
			// therefore built HERE, beside the writer they go to — but only when
			// the carrier RENDERS the reasoning itself (StreamThinkingDeltas
			// empties them; see that field).
			m := a.thinkingMarkersFor()
			flow := a.flowWriterFor()
			fmt.Fprint(flow, m.open)
			fmt.Fprint(flow, thinking)
			fmt.Fprint(flow, m.close)
		}
		if thinking != "" && a.SessionLog != nil {
			a.SessionLog.LogAssistantThinking(thinking)
		}

		// Print the full content at once to mimic streaming progress.
		content := resp.Text()
		if content != "" {
			if !a.ChatMode {
				fmt.Fprint(os.Stderr, content)
			}
			a.answerTextDelta(content)
		}
		if a.Debug {
			b, _ := json.MarshalIndent(resp, "", "  ") //stylegate:ignore pretty-printing an in-process value for a log line; a failure is non-fatal
			a.Logger.Printf("← FALLBACK RESPONSE [%d] stop=%s\n%s\n", iter, resp.StopReason, b)
		}
		return &chatResult{
			content:      content,
			toolCalls:    resp.ToolCalls(),
			finishReason: deepseek.MapClaudeStopReason(resp.StopReason),
			thinking:     thinking,
			thinkingSig:  sig,
		}, false, nil
	}

	// Accumulate the streaming response: content blocks are keyed by index.
	// Text and thinking deltas stream to stderr live; tool_use arguments
	// arrive as input_json_delta fragments parsed at block stop.
	blockMap := make(map[int]*deepseek.ClaudeContentBlock) // index → block under construction
	var blocks []deepseek.ClaudeContentBlock               // finalized blocks in wire order
	var stopReason string

	// Live thinking display: stream thinking deltas to stderr, visually
	// separated from the final answer. ANSI dim on TTYs; plain "[thinking] "
	// prefix otherwise (parseable when piped/redirected). The defer closes
	// the block on early return (stream error, etc.).
	markers := a.thinkingMarkersFor()
	// thinkingOut is the destination of the live reasoning stream. With a
	// carrier seam installed the reasoning goes to the carrier (which owns the
	// screen and renders it in its own rows); otherwise it is os.Stderr,
	// byte-for-byte as before.
	//
	// The markers are emitted HERE, on whatever writer that is, and NOT
	// suppressed for the carrier: the seam is a writer, not a markerless
	// display, so the band receives the same open/delta/close shape as stderr.
	// (The pre-merge branch suppressed them for a carrier because its own
	// display path took over the labelling; the merged seam does not, so a
	// suppression here would drop the block's delimiters from BOTH paths.)
	//
	// The block is closed through the ONE close helper below, which writes on the
	// SAME writer the open went to, so the seam's byte stream stays the seam's
	// (internal/agent/thinking_seam_test.go pins it) — the writer is not captured
	// anywhere else, so open and close cannot address two destinations.
	//
	// The branch's single close helper is kept on top of that destination: it
	// closes through the SAME writer the open went to, so the seam's byte
	// stream stays the seam's (internal/agent/thinking_seam_test.go pins it).
	// The flow destination (master): the carrier's own seam when one is
	// installed, os.Stderr otherwise — the carrier branch keeps the
	// marker bytes, because internal/agent/thinking_seam_test.go pins
	// the seam's stream to the stderr path's exact bytes; the band's own
	// SGR-aware renderer is what keeps those bytes out of its cell
	// arithmetic.
	thinkingOut := a.flowWriterFor()
	thinkingStarted := false
	closeThinking := func() {
		if thinkingStarted {
			fmt.Fprint(thinkingOut, markers.close)
		}
		thinkingStarted = false
	}
	defer closeThinking()

	// Budget enforcement counters: streamed thinking characters vs the
	// request's token budget (estimated at thinkingCharsPerToken chars/token).
	// overBudget stays false unless the round must be discarded and re-issued.
	var thinkingChars int
	overBudget := false

	// Usage is only added from message_delta (complete counts: input +
	// output + cache). message_start carries input-only usage and would
	// double-count if both were added.
	//
	// MEASURED SHAPE (2026-10-06, this repo's own raw logs). A frame the model
	// really sends carries the FULL prompt-side count, not a partial one:
	//
	//	message_start … "usage":{"input_tokens":29456,"cache_creation_input_tokens":0,
	//	                         "cache_read_input_tokens":2048,"output_tokens":0}
	//	message_delta … "usage":{"input_tokens":29456,"cache_creation_input_tokens":0,
	//	                         "cache_read_input_tokens":2048,"output_tokens":113}
	//
	// (session 8981132f, its raw log lines 6 and 310 — the two frames agree on
	// all three prompt-side counters and differ only in output_tokens, and the
	// same holds for all 144 rounds of that session: NOT ONE message_delta
	// arrived with input_tokens==0). So the prompt-side total is known at
	// message_start already, and waiting for message_delta to learn it is a
	// gratuitous delay: a round that is interrupted, that blows the thinking
	// budget and is re-issued, or that dies before its delta never measures at
	// all — and the context then keeps guessing (chars/4) for a count the API
	// already told us.
	//
	// So BOTH frames feed the measurement, and the double-count the old note
	// warned about is prevented by assignment rather than by exclusion:
	// SetPromptTokens REPLACES the recorded count, so a later frame overrides
	// its predecessor with a strictly more complete value (the delta differs
	// from the start only by output_tokens, which is not part of the
	// prompt-side total). The COST tracker is the one that ACCUMULATES and
	// therefore still takes exactly ONE frame per round — the delta, which is
	// the complete one — with the start as its no-delta fallback.
	var startUsage deepseek.ClaudeUsage
	sawDelta := false

	// reconciled makes the local-vs-server write happen ONCE per attempt, on the
	// FIRST frame that carries a prompt measurement. Both frames do carry one and
	// they agree on every prompt-side counter (the measured shape quoted above,
	// and all 144 rounds of session 8981132f), so a second row would not record a
	// second fact — it would double the population a reader counts, and "how often
	// does the drift exceed 2%" would then be answered against a doubled sample.
	reconciled := false

	for ev := range events {
		if ev.Error != "" && !overBudget {
			return nil, false, fmt.Errorf("stream error: %s", ev.Error)
		}

		switch ev.Type {
		case "message_start":
			startUsage = ev.Usage
			// The measurement lands HERE, not only on the delta (see the
			// measured shape above): a real frame carries the full prompt-side
			// count at start, and a round that never reaches its delta
			// (interrupted / over-budget re-issue / a dying stream) otherwise
			// leaves the context guessing. Assignment, so no double-count.
			if a.Ctx != nil && ev.Usage.HasPromptMeasurement() {
				if !reconciled {
					reconciled = true
					a.reconcilePromptTokens(ev.Usage, predictedPromptTokens, reconFrameStart)
				}
				a.Ctx.SetPromptTokens(ev.Usage.PromptSideTokens())
			}

		case "content_block_start":
			if ev.Block == nil {
				continue
			}
			blockMap[ev.Index] = ev.Block

		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			b := blockMap[ev.Index]
			if b == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				b.Text += ev.Delta.Text
				// Close any open thinking block first so only the final
				// answer reads as the model's reply — through the ONE close
				// helper, so the close lands on the same writer the open did.
				closeThinking()
				if !a.ChatMode {
					fmt.Fprint(os.Stderr, ev.Delta.Text)
				}
				a.answerTextDelta(ev.Delta.Text)

			case "thinking_delta":
				b.Thinking += ev.Delta.Thinking
				thinkingChars += len(ev.Delta.Thinking)
				if thinkingBudget > 0 && thinkingChars >= thinkingBudget*thinkingCharsPerToken && !overBudget {
					// Harness-enforced budget: the API accepted budget_tokens
					// but kept generating thinking — abort the stream;
					// chatOnce re-issues the round with thinking disabled.
					overBudget = true
					attemptCancel()
					// The budget aborts the stream on the turn's own
					// goroutine; the block is closed on the SAME writer the
					// deltas went to, so the seam never holds a half-open
					// block (it flushes at the turn boundary either way) —
					// through the same close helper as the text-delta path.
					closeThinking()
				}
				if !overBudget && !a.HideThinking {
					if !thinkingStarted {
						fmt.Fprint(thinkingOut, markers.open)
						thinkingStarted = true
					}
					a.emitThinking(ev.Delta.Thinking)
				}

			case "input_json_delta":
				b.InputJSON += ev.Delta.PartialJSON

			case "signature_delta":
				b.Signature += ev.Delta.Signature
			}

		case "content_block_stop":
			b := blockMap[ev.Index]
			if b == nil {
				continue
			}
			// Parse the accumulated input_json fragments into the tool_use
			// input. Fragments may be individually unparseable; only the
			// combined string is valid JSON.
			if b.Type == "tool_use" && b.InputJSON != "" {
				var input map[string]interface{}
				if err := json.Unmarshal([]byte(b.InputJSON), &input); err == nil && input != nil {
					b.Input = input
				}
			}
			blocks = append(blocks, *b)
			delete(blockMap, ev.Index)

		case "message_delta":
			stopReason = ev.StopReason
			sawDelta = true
			if a.Cost != nil && ev.Usage.TotalTokens() > 0 {
				a.Cost.AddClaudeUsage(ev.Usage)
			}
			if a.Ctx != nil && ev.Usage.HasPromptMeasurement() {
				if !reconciled {
					reconciled = true
					a.reconcilePromptTokens(ev.Usage, predictedPromptTokens, reconFrameDelta)
				}
				a.Ctx.SetPromptTokens(ev.Usage.PromptSideTokens())
			}
		}
	}

	// Stream ended without message_delta (e.g. interrupted): fall back to
	// the input-only usage from message_start.
	if !sawDelta && a.Cost != nil && startUsage.TotalTokens() > 0 {
		a.Cost.AddClaudeUsage(startUsage)
	}

	if overBudget {
		// The aborted stream yielded no usable content — the round must be
		// re-issued with thinking disabled (chatOnce does that).
		return nil, true, nil
	}

	resp := &deepseek.ClaudeResponse{Content: blocks, StopReason: stopReason}
	thinking, sig := resp.Thinking()

	// ONE assistant message of this turn has ended. It is announced BEFORE the
	// round's answer reaches OnText, so a carrier that classifies the answer's
	// format (the banded editor's fence classifier) closes the message that is
	// ending rather than the one that is about to start — a message that stops
	// inside a fenced block it never closed leaves that block open, and the text
	// of THIS round must not inherit it. The whole message is announced at once
	// (below), so the boundary here cannot fall between two parts of one message.
	if a.OnMessageEnd != nil {
		a.OnMessageEnd()
	}

	// Log reasoning/thinking content from the streaming response.
	if thinking != "" && a.SessionLog != nil {
		a.SessionLog.LogAssistantThinking(thinking)
	}

	return &chatResult{
		content:      resp.Text(),
		toolCalls:    resp.ToolCalls(),
		finishReason: deepseek.MapClaudeStopReason(stopReason),
		thinking:     thinking,
		thinkingSig:  sig,
	}, false, nil
}

// trimContext is the ONE place that looks at how full the context is before a
// request goes out — and it asks TWO questions, in this order:
//
//  1. "is the window nearly full?" (EstimatedTokens() > CompactTriggerRatio ×
//     ContextWindow). If not, it is a no-op.
//  2. "is this run allowed to SHRINK the conversation?" That is
//     Agent.AutoReduceEnabled, and it is FALSE by default (context_pressure.go):
//     the operator is WARNED with the numbers and nothing is removed, because
//     the choice between finishing here and starting a fresh session belongs to
//     the human, not to the harness.
//
// Only when a run has explicitly opted in (GOPI_CONTEXT_AUTOREDUCE=1) does it
// reduce: first folding old tool results into one-line stubs (R1, zero LLM
// cost), then LLM-assisted memory compaction, then dropping the oldest messages.
// The system prompt and the most recent messages are kept either way. No-op when
// ContextWindow <= 0 (the window is unknown, so nothing can be judged).
func (a *Agent) trimContext(ctx context.Context) {
	if a.ContextWindow <= 0 {
		return
	}
	triggerRatio := a.CompactTriggerRatio
	if triggerRatio <= 0 {
		triggerRatio = DefaultCompactTriggerRatio
	}
	limit := int(float64(a.ContextWindow) * triggerRatio)
	before := a.Ctx.EstimatedTokens()
	if before <= limit {
		return
	}
	// THE DEFAULT PATH REMOVES NOTHING — it TELLS THE OPERATOR (context_pressure.go).
	//
	// This is the whole policy change: crossing the trigger no longer authorises
	// a mutation of the conversation. Nothing is folded, nothing is summarized,
	// nothing is dropped, and the message sequence is identical when this
	// function returns. The operator gets the numbers and the two options
	// (finish here, or start a new session and pull detail back with
	// session_search) and the choice is theirs — which is only meaningful if the
	// run cannot make it for them behind their back.
	//
	// The reduction path below survives for a run that opts in with
	// GOPI_CONTEXT_AUTOREDUCE=1 (AutoReduceEnv). It is kept whole, and kept
	// reachable, so a long unattended run still has the old behaviour available —
	// but as a stated choice at start-up, never as a surprise mid-conversation.
	if !a.AutoReduceEnabled {
		a.notifyContextPressure(before, limit)
		return
	}
	// SELF-RECORDING (第一期): the record's SURVIVAL GUARANTEE, on EVERY exit
	// from this function. The compaction path is not the only one that can remove
	// the message a note block lived in — the deterministic trim
	// (trimToFitStickyHead, no anchor floors) reaches it too, and the structural
	// no-op guard can restore the context and hand over to that trim. Putting the
	// guarantee here makes it a property of "the context was bounded" rather than
	// of one of the three ways that happens.
	//
	// It is NOT a TakeOldest exemption and NOT an anchor target: it re-injects the
	// notes this run recorded (a harness-injected user message, which the anchor
	// policy already refuses to treat as the operator's last word) and is a no-op
	// when they are all still there. See selfrecord.go.
	defer a.ensureSelfRecordPresent()
	// THE MEASURED COUNT AT TRIGGER TIME (Rule 15 — a number that is read for
	// a log line is read where it still exists). `measuredAtTrigger` is what
	// the trigger actually judged; reading a.Ctx.PromptTokens further down
	// would read it AFTER the compaction branch below RESET it (compact.go:
	// "the API measurement ... must be invalidated or the next round
	// re-triggers on the stale count"), so the compaction line printed
	// "measured 0" for a trigger that had fired ON the measurement — measured
	// on this repo's own sessions: every one of the 95 compacted lines in
	// ~/.gopi/sessions/*/console.log reads "(measured 0)", including
	// 8981132f's, whose raw log carries 144 message_start frames and not one
	// of them has input_tokens==0 (its own last measurement reads 151407).
	// The value was never missing; it was read one branch too late.
	measuredAtTrigger := a.Ctx.PromptTokens
	// R1: collapse old tool results into one-line stubs before resorting to
	// a destructive trim or an LLM compaction. Tool results are the
	// context-growth driver and the model rarely re-reads old ones; folding
	// may bring the estimate back under the limit at zero cost.
	// THE FOLD LOG'S PAIR IS MEASURED ON ONE BASIS — the chars/4 heuristic —
	// and this is a correction, not a style choice. The log used to print
	// EstimatedTokens() on both sides of the fold, and that pair is CONSTANT by
	// construction: EstimatedTokens() uses the API-measured count as its base for
	// every message up to PromptTokensIndex, and every result folding touches is
	// OLDER than that index (folding reaches only results past
	// maxVerbatimToolResults), so rewriting their content cannot move the
	// measured base at all. The field log therefore read
	// "folded 12 old tool result(s) into stubs (est 150491 → 150491 tokens)" on
	// every fold — a before/after comparison that says nothing, printed by the
	// one path whose whole purpose is to shrink the context. Same defect class as
	// compact.go's savings verdict (never compare numbers from two bases; the
	// heuristic is the only basis available on BOTH sides of a mutation, which is
	// what Context.heuristicTokens is for).
	//
	// WHAT IS NOT CHANGED HERE: the DECISION. `limit` and the early return below
	// stay on EstimatedTokens() — the trigger must fire on the measured size
	// (context.go's EstimatedTokens note), and a fold verdict that skipped
	// compaction on the heuristic alone would re-open the measured-size hole the
	// calibration closed. Only the pair the reader sees is on one basis.
	beforeFoldHeuristic := a.Ctx.heuristicTokens()
	// The compaction log below pairs its own before/after on this basis; with a
	// fold already applied it is re-based to the post-fold state, so the
	// compaction is never credited with the fold's own saving.
	beforeHeuristic := beforeFoldHeuristic
	if folded := a.Ctx.summarizeOldToolResults(); folded > 0 {
		afterFold := a.Ctx.EstimatedTokens()
		afterFoldHeuristic := a.Ctx.heuristicTokens()
		if a.Logger != nil {
			a.Logger.Printf("🧠 context window: folded %d old tool result(s) into stubs (est %d → %d tokens, limit %d)",
				folded, beforeFoldHeuristic, afterFoldHeuristic, limit)
		}
		if afterFold <= limit {
			return
		}
		before = afterFold
		beforeHeuristic = afterFoldHeuristic
	}
	// Prefer LLM-assisted memory compaction: summarize the oldest messages
	// into a memory block so the agent keeps its goal, decisions and
	// findings. Falls back to dropping the oldest messages when compaction
	// is disabled, paused (failure cooldown / anti-thrash debounce) or the
	// summarizer fails (compactContext already removed the oldest messages
	// on failure — the result is a plain trim).
	if a.CompactionEnabled && !a.compactionPaused() && a.compactContext(ctx) {
		after := a.Ctx.EstimatedTokens()
		targetRatio := a.CompactTargetRatio
		if targetRatio <= 0 {
			targetRatio = DefaultCompactTargetRatio
		}
		target := int(float64(a.ContextWindow) * targetRatio)
		// Assertion (P0): after a compaction the context must fit within the
		// target waterline plus the summary block itself. A violation means
		// the keep-recent/anchor/protected floor deliberately kept the
		// estimate above target — warn loudly with the numbers.
		summaryEst := 0
		if len(a.Ctx.Messages) > 0 && isMemorySummaryMessage(a.Ctx.Messages[0]) {
			summaryEst = EstimateTokens(a.Ctx.Messages[0])
		}
		assertOK := after <= target+summaryEst
		// THE CHANGE PAIR IS ON ONE BASIS (defect A's class, second instance in
		// this function). `before` is the MEASURED estimate that tripped the
		// trigger, while `after` is necessarily heuristic — compactContext resets
		// PromptTokens (the measurement covered the pre-compaction context), so
		// EstimatedTokens() falls back to chars/4 on the post-reset side. Printing
		// them as one pair is exactly the phantom-savings shape compact.go's own
		// savings verdict was corrected for (its field log: "before=119145
		// measured → after=118285 heuristic — saved 0.7%" for a context that GREW).
		// So the trigger STATEMENT stays measured (`est %d (measured %d)` — a
		// real-usage judgement, the basis the trim must fire on) and the CHANGE is
		// printed on the heuristic, the only basis defined on both sides.
		line := fmt.Sprintf("🧠 context window: est %d tokens (measured %d) > %d (%.1f%% of %d) — compacted: heuristic %d → %d tokens (%.1f%% → %.1f%%, target %.1f%%) — assertion: after ≤ target+summary = %v",
			before, measuredAtTrigger, limit, float64(before)/float64(a.ContextWindow)*100, a.ContextWindow,
			beforeHeuristic, a.Ctx.heuristicTokens(),
			float64(beforeHeuristic)/float64(a.ContextWindow)*100,
			float64(a.Ctx.heuristicTokens())/float64(a.ContextWindow)*100, targetRatio*100, assertOK)
		if a.Logger != nil {
			if assertOK {
				a.Logger.Print(line)
			} else {
				a.Logger.Printf("⚠️ %s (measured-before=%d heuristic-after=%d limit=%d)", line, before, after, limit)
			}
		}
		return
	}
	// The eviction target is the compaction waterline when a summarizer can
	// follow the eviction (it is what refills the freed region cheaply), and
	// the trigger line itself when compaction is OFF: there the window is the
	// only regulator, so trimming past the line would simply be discarded
	// budget — the context may sit at the line, not below it.
	target := limit
	if a.CompactionEnabled {
		targetRatio := a.CompactTargetRatio
		if targetRatio <= 0 {
			targetRatio = DefaultCompactTargetRatio
		}
		if amt := int(float64(a.ContextWindow) * targetRatio); amt < limit {
			target = amt
		}
	}
	removed := a.Ctx.trimToFitStickyHead(limit, target)
	a.Logger.Printf("⚠️ context window: est %d tokens > %d (%.1f%% of %d) — trimmed %d oldest message(s)",
		before, limit, float64(before)/float64(a.ContextWindow)*100, a.ContextWindow, removed)
}

// compactionPaused reports whether summary attempts are currently paused:
// after a summarizer failure (cooldown, CompactCooldown), during a structural
// no-op backoff (no window to compress — transient, does not strike), or after
// consecutive ineffective compressions (debounce: last compactIneffectiveCount
// saved under compactEffectiveSavingsPct each). While the breaker is tripped
// the anti-thrash recovery probe lets ONE attempt through once the recovery
// window elapses, so a session that has since grown real compressible material
// recovers; an
// ineffective probe re-trips immediately (Hermes _ANTI_THRASH_RECOVERY at
// :3659-3698, _STRUCTURAL_NO_OP at :2601-2625). While paused the loop falls
// back to TrimToFit so the context stays bounded (Hermes :1173-1209).
func (a *Agent) compactionPaused() bool {
	if time.Now().Before(a.compactFailureCooldownUntil) {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ memory compaction in cooldown until %v — trimming instead",
				a.compactFailureCooldownUntil.Format("15:04:05"))
		}
		return true
	}
	if time.Now().Before(a.compactStructuralNoOpUntil) {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ compaction skipped — no compressible window, retrying in %v — trimming instead",
				time.Until(a.compactStructuralNoOpUntil).Round(time.Second))
		}
		return true
	}
	if a.compactIneffectiveCount >= compactIneffectiveStrikes {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ compaction skipped — last %d compressions saved <%.0f%% each — consider a fresh session",
				a.compactIneffectiveCount, compactEffectiveSavingsPct)
		}
		// Anti-thrash recovery probe: once the recovery window elapses, drop
		// the counter to one strike and allow a single probation attempt. If
		// it is ineffective again the next verdict re-trips the guard — the
		// worst case in a truly incompressible session is one attempt per
		// recovery window, bounded, not thrash (Hermes #14694, #54923).
		if !a.compactRecoveryProbeAt.IsZero() && time.Now().After(a.compactRecoveryProbeAt) {
			a.compactRecoveryProbeAt = time.Time{}
			a.compactIneffectiveCount = 1
			if a.Logger != nil {
				a.Logger.Printf("⚠️ anti-thrash recovery: %v elapsed since the guard tripped — allowing one compaction probe (ineffective=%d)",
					compactRecoveryProbeDelay.Round(time.Second), a.compactIneffectiveCount)
			}
			return false
		}
		return true
	}
	return false
}

// maxSearchResults caps one search tool result at 50. The old cap of 20
// silently truncated real targets (R2 A/B A14: the 21st+ match was the
// symbol the model needed); the formatter now renders "N more matches" when
// the cap bites, so no truncation is silent. 50 keeps a noisy query from
// flooding the context while leaving headroom over typical hit counts.
const maxSearchResults = 50

// codeSearchScope is the search scope for the code_search tool: the project
// root (cwd) and the indexed source files (Go, Python, shell, C++, Rust,
// JavaScript, TypeScript — see tools.CodeSearchGlobs), with a result cap so
// one noisy query cannot flood the context window.
func codeSearchScope() tools.SearchScope {
	return tools.SearchScope{
		RootPath:    ".",
		IncludeGlob: tools.CodeSearchGlobs,
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   maxSearchResults,
	}
}

// keywordSearchScope is the search scope for the keyword_search tool: anything
// except VCS/vendor noise, capped like code_search.
func keywordSearchScope() tools.SearchScope {
	return tools.SearchScope{
		RootPath:    ".",
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   maxSearchResults,
	}
}

// parallelSafeTools lists the tools safe to run concurrently. They are
// read-only — no file writes, no state changes — so calls cannot interfere
// with each other regardless of execution order. git_blame spawns git, but
// the invocation is strictly read-only (blame/ls-files), so it is safe too.
var parallelSafeTools = map[string]bool{
	toolnames.ToolRead:          true,
	toolnames.ToolCodeSearch:    true,
	toolnames.ToolKeywordSearch: true,
	toolnames.ToolGitBlame:      true, // read-only git history query (spawns git, no writes)
}

// parallelSafeCall decides whether ONE tool call may run concurrently with the
// other calls of a round. Action-aware (tool-family merge — task family): the
// merged `task` tool is parallel-safe ONLY for its READ-ONLY action
// (ToolTaskView: returns the harness-tracked task list, no state change); the
// side-effect actions (ToolTaskCommit writes the working-area row + latches
// the commit round; ToolTaskReview records the review verdict) must run
// serial — their ordering relative to the other commits/reviews of the round
// matters (the carry-forward hook fires once per real commit round). Both
// therefore fall to the serial Phase-2 pass, like every other side-effect tool.
func parallelSafeCall(tc deepseek.ToolCall) bool {
	if parallelSafeTools[tc.Name] {
		return true
	}
	if tc.Name == toolnames.ToolTask {
		if action, _ := tc.Arguments["action"].(string); action == toolnames.ToolTaskView {
			return true
		}
	}
	if tc.Name == toolnames.ToolMemory {
		// Action-aware (tool-family merge, memory family): the merged
		// `memory` tool is parallel-safe ONLY for its READ-ONLY action
		// (ToolMemoryActionSearch — reads the knowledge base, no state
		// change); the side-effect actions (store writes a record,
		// feedback mutates trust scores + may trim context, update rewrites
		// a record) must run serial — their effects and ordering matter.
		if action, _ := tc.Arguments["action"].(string); action == toolnames.ToolMemoryActionSearch {
			return true
		}
	}
	return false
}

// executeToolBatch executes a round's tool calls and returns their results in
// the SAME order as the input calls (tool_result order must match the
// assistant's tool_use order — see devdoc/bug026-parallel-tools-evaluation.md).
//
// The read-only subset (parallelSafeTools: read, code_search, keyword_search,
// todo) runs CONCURRENTLY under a single batch timeout; side-effect tools
// (write/edit/bash/job_*) run serially afterwards, so effects never overlap
// (R4 — a mixed [read, read, bash] round used to run fully serially even
// though the reads are side-effect-free). Results are written into an
// index-keyed slice, so ordering is preserved regardless of completion order.
// maybeNudgeNoList implements the P1.4/P2 no-list-yet nudge: inject a
// one-shot reminder when the session has run roundIdx rounds (1-based) with
// no established task list, elapsedToolRounds of them carried tool calls, and
// a.TaskState still has hasList==false. Guards:
//   - list established (hasList==true) → never nudges again (and re-arms off);
//   - nudgeCount capped at maxNudgeCount (anti-harassment);
//   - between fires, re-arms nudgeEscapeRounds later (interval escalation).
//
// roundIdx is the 1-based iteration index; a.nudgeNextAt stores the 1-based
// round the next nudge is armed for (set in NewAgent/the Run loop to
// NudgeRounds; tests must arm it explicitly). Self-contained and
// unit-testable without a running loop. Returns true if a nudge was injected.
// trustedCompletion reports whether the harness-tracked task state already
// shows an established, all-completed list (P1.5). This is the decoupled
// completion signal: an end-of-run round whose answer text omits the task
// list (per the emission contract's "do not re-emit as a receipt") can still
// be trusted for the R3 fast-exit, because the list lives in harness state —
// tracked at the round it was established, not re-claimed in the final message.
// With no list ever established it returns false (a no-list session keeps the
// normal exit path / nudge protection).
func (a *Agent) trustedCompletion() bool {
	return a.TaskState != nil && a.TaskState.hasList && AllCompleted(a.TaskState.tasks)
}

// pendingTaskAreaConclusionGuard is the memory-aware T2 framework
// hard-takeover of task-list completion: it decides whether a no-tool exit
// round must be INTERCEPTED (nudge injected, run continues) because the
// session's tracked items still lack committed write_task_area conclusions.
//
// Nick's design intent: free text has no power to declare the whole run
// complete — completion advances item-by-item through write_task_area
// (status=completed) and the framework holds the final word. The pending set
// is the harness-tracked task list's item ids; the completed set is the
// session's committed conclusions (tools.SessionConclusions, keyed by the
// same session id write_task_area commits to).
//
// Guard gates (all must hold to intercept — legacy sessions untouched):
//   - taskAreaActive: the session has JOINED the working-area contract (it
//     actually called write_task_area). A session that never joined keeps the
//     legacy free-text completion contract — every existing R3 / bug022 exit
//     test (and every pre-T2 production session) is exempt by construction.
//   - a tracked task list exists (TaskState.hasList). A single-step session
//     without a [TASK_LIST] is never intercepted (E6).
//   - taskAreaNudges < maxTaskAreaNudges: the cap bounds the insistence — a
//     model that refuses to comply is allowed to exit after the cap (the
//     framework never hard-blocks a run, anti-deadlock).
//
// All-completed tracked lists are ALSO subject to the guard when a conclusion
// is missing: an item marked "completed" in the free-text list but never
// committed through write_task_area has not satisfied the framework contract
// (the completed set — not the free-text status — is the machine-trusted
// progress view). DB read failures fail OPEN (no interception): the guard
// must never wedge a run on a query error.
//
// Returns true when a nudge was injected (the caller must continue the loop,
// skipping both exit paths this round).
func (a *Agent) pendingTaskAreaConclusionGuard() bool {
	if !a.taskAreaActive || a.taskAreaNudges >= maxTaskAreaNudges || a.Ctx == nil {
		return false
	}
	if a.TaskState == nil || !a.TaskState.hasList {
		return false // no tracked list (single-step session) — exempt
	}
	view := a.TaskState.view()
	if len(view) == 0 {
		return false
	}
	// The completed set: this session's committed conclusions.
	concs, err := tools.SessionConclusions(a.sessionIDForTasks())
	if err != nil {
		return false // DB failure — fail open, never wedge the exit
	}
	concluded := make(map[string]bool, len(concs))
	for _, c := range concs {
		concluded[c.TaskID] = true
	}
	// Pending = tracked items whose task_id has no committed conclusion.
	var pending []string
	for _, t := range view {
		if !concluded[string(t.TaskID)] {
			pending = append(pending, string(t.TaskID))
		}
	}
	if len(pending) == 0 {
		return false // every tracked item has a committed conclusion
	}
	a.Ctx.AddUserMessage(fmt.Sprintf(taskAreaCompletionNudge, strings.Join(pending, ", ")))
	a.taskAreaNudges++
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), fmt.Sprintf(taskAreaCompletionNudge, strings.Join(pending, ", ")))
	}
	if a.Logger != nil {
		a.Logger.Printf("🔒 framework completion guard: %d pending item(s) without committed conclusion (%v) — nudged %d/%d",
			len(pending), pending, a.taskAreaNudges, maxTaskAreaNudges)
	}
	return true
}

func (a *Agent) maybeNudgeNoList(roundIdx, elapsedToolRounds, toolRoundsCfg int) bool {
	// Never nudge once a list has been established.
	if a.TaskState == nil || a.TaskState.hasList {
		return false
	}
	// Not enough total rounds, not enough tool rounds, or over the cap.
	if a.nudgeNextAt == 0 || roundIdx < a.nudgeNextAt || elapsedToolRounds < toolRoundsCfg || a.nudgeCount >= maxNudgeCount {
		return false
	}
	if a.Ctx == nil {
		return false
	}

	a.Ctx.AddUserMessage(noListYetNudge)
	a.nudgeCount++
	a.nudgeNextAt = roundIdx + nudgeEscapeRounds
	if a.Logger != nil {
		a.Logger.Printf("nudge #%d: no task list established after %d rounds / %d tool rounds",
			a.nudgeCount, roundIdx, elapsedToolRounds)
	}
	return true
}

// maybeNudgeStaleTaskList is the bookkeeping-decay nag (review 2026-09-07 6.5,
// C2): the mirror of maybeNudgeNoList. The prompt's re-emission cadence (rule
// 3, "at least every %d tool rounds") relied entirely on LLM volition — the
// harness only *logged* a warning at EXIT (warnExitingWithProgressing), which
// is the same signal delivered too late to matter. This fires at the round
// boundary instead.
//
// staleRounds is how many consecutive rounds have passed without a
// [TASK_LIST] block (maxRoundsWithoutTaskList — the same constant the prompt
// interpolates, so prompt and nag can never drift apart). Fires only while the
// tracked list still has unfinished work: a model that legitimately stopped
// emitting because everything is completed is not nagged (that is the
// contract's blessed omit-as-receipt case, P1.5). Capped at maxNudgeCount so a
// model that has decided to finish is not harassed. Returns true if injected.
func (a *Agent) maybeNudgeStaleTaskList(staleRounds int) bool {
	if a.Ctx == nil || a.TaskState == nil || !a.TaskState.hasList {
		return false
	}
	if staleRounds < maxRoundsWithoutTaskList || a.staleListNudgeCount >= maxNudgeCount {
		return false
	}
	view := a.TaskState.view()
	if AllCompleted(view) {
		return false
	}
	a.Ctx.AddUserMessage(fmt.Sprintf(staleTaskListNudge, staleRounds))
	a.staleListNudgeCount++
	if a.Logger != nil {
		a.Logger.Printf("nudge #%d: no task list block for %d rounds (%d unfinished task(s) tracked)",
			a.staleListNudgeCount, staleRounds, len(view)-CountStatus(view, TaskStatusCompleted))
	}
	return true
}

func (a *Agent) executeToolBatch(toolCalls []deepseek.ToolCall) []string {
	return a.executeToolBatchCtx(a.runScope(), toolCalls)
}

// executeToolBatchCtx is executeToolBatch bound to the caller's run scope: the
// context travels to every tool call of the round, so a tool that can act on a
// cancellation (bash) dies the moment the operator's key reaches the run. The
// run loops pass their own ctx; the thin wrapper above lets the legacy callers
// (tests, carriers that cannot thread one) inherit the run's scope instead of
// building a root of their own.
func (a *Agent) executeToolBatchCtx(ctx context.Context, toolCalls []deepseek.ToolCall) []string {
	// M4.5 ⑦ heartbeat: a tool batch start = the agent is ALIVE. The touch
	// is best-effort ledger I/O (never blocks, never errors the round).
	touchLastToolcallAt()
	results := make([]string, len(toolCalls))

	// Phase 1: run the read-only subset concurrently. A single read-only
	// call has nothing to overlap with — run it inline like any serial call.
	var readIdx []int
	var readCalls []deepseek.ToolCall
	for j, tc := range toolCalls {
		if parallelSafeCall(tc) {
			readIdx = append(readIdx, j)
			readCalls = append(readCalls, tc)
		}
	}
	if len(readCalls) > 1 {
		parallelResults := a.executeToolBatchParallel(ctx, readCalls)
		for k, idx := range readIdx {
			results[idx] = parallelResults[k]
		}
	} else if len(readCalls) == 1 {
		results[readIdx[0]] = a.executeToolCtx(ctx, readCalls[0])
	}

	// Phase 2: side-effect tools run serially — their effects must never
	// overlap (file conflicts, ordering).
	for j, tc := range toolCalls {
		if parallelSafeCall(tc) {
			continue
		}
		results[j] = a.executeToolCtx(ctx, tc)
	}
	return results
}

// executeToolBatchParallel runs every call in the round concurrently, bounded
// by ONE timeout for the whole batch (tools.FileToolTimeout, or
// a.parallelTimeout when set — test override). Each call runs in its own
// goroutine; results are written into an index-keyed slice so they come back
// in tool-call order regardless of completion order.
//
// A call that outlives the batch deadline gets a timeout error result while
// the others still return normally. The underlying tool goroutine may keep
// running after the deadline (same pattern as the file-tool timeouts in
// internal/tools) — the caller is simply unblocked.
func (a *Agent) executeToolBatchParallel(ctx context.Context, toolCalls []deepseek.ToolCall) []string {
	// M4.5 ⑦ heartbeat (parallel path): same touch as the serial entry.
	touchLastToolcallAt()
	timeout := a.parallelTimeout
	if timeout <= 0 {
		timeout = tools.FileToolTimeout()
	}
	// The batch budget is layered ON the caller's run scope, never on
	// Background: the operator's cancellation must reach the calls running
	// inside the batch, which is the same property the serial path has.
	batchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = batchCtx

	results := make([]string, len(toolCalls))
	batchStart := time.Now()

	var wg sync.WaitGroup
	for j, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, call deepseek.ToolCall) {
			defer wg.Done()
			callStart := time.Now()

			done := make(chan string, 1)
			go func() {
				done <- a.executeToolCtx(ctx, call)
			}()

			select {
			case res := <-done:
				results[idx] = res
			case <-ctx.Done():
				results[idx] = fmt.Sprintf("Error: tool %s timed out after %s (parallel batch)", call.Name, timeout)
			}

			if a.Logger != nil {
				a.Logger.Printf("parallel tool [%d/%d] %s: %s", idx+1, len(toolCalls), call.Name, time.Since(callStart))
			}
		}(j, tc)
	}
	wg.Wait()

	if a.Logger != nil {
		a.Logger.Printf("parallel batch: %d concurrent calls, %s total (timeout %s)",
			len(toolCalls), time.Since(batchStart), timeout)
	}
	return results
}

// SetToolExecutor installs a replacement for the per-call tool executor for the
// lifetime of this agent (nil restores the real dispatch, executeToolDefault).
//
// It is the EXPORTED half of the seam the `executeToolFn` field has always
// been, and it exists because the field's own comment ("test-only") was the
// only thing keeping a caller outside this package from reaching the ONE point
// where a tool call stops being static text and becomes a real read/write/exec.
// The replay driver (internal/replaydrive, `gopi chat --replay`) is that caller:
// it drives the REAL chat surface — the real REPL, the real editor, the real
// renderer — against a recording, and hands each recorded tool call back its
// recorded result, so the replay exercises every layer except the one that
// would touch the machine.
//
// The field itself is NOT renamed and NOT retyped: the in-package tests that
// build `Agent{executeToolFn: …}` literals keep working byte-for-byte, which is
// the constraint this method exists to satisfy (a rename would have been a
// second, silently different seam for the same behaviour).
func (a *Agent) SetToolExecutor(fn func(tc deepseek.ToolCall) string) {
	if a == nil {
		return
	}
	a.executeToolFn = fn
}

// ToolExecutorInstalled reports whether a replacement executor is in force. It
// is the read side of SetToolExecutor, for a caller that must state in its own
// record WHICH dispatch produced the results it is quoting (Rule 15: a degraded
// or overridden path is announced, never inferred).
func (a *Agent) ToolExecutorInstalled() bool { return a != nil && a.executeToolFn != nil }

// executeTool executes a single tool call and returns the result string.
//
// It uses the agent's RUN SCOPE as the tool's context (runScope): a tool that
// can be interrupted must be given the context the operator's key cancels, and
// this wrapper is the shape callers outside the run loop (tests, the web/UI
// carriers) reach. The run loops pass their own ctx explicitly through
// executeToolCtx, which is what makes the chain end to end rather than
// approximate.
func (a *Agent) executeTool(tc deepseek.ToolCall) string {
	return a.executeToolCtx(a.runScope(), tc)
}

// executeToolCtx executes a single tool call bound to ctx — the run-scoped
// context that carries the operator's cancellation (InterruptRun) and the run's
// own deadline. The context reaches the tools that can act on it: `bash` kills
// its command when ctx is cancelled (BashInDirCtx), so Escape/Ctrl+C stops a
// running command instead of leaving it behind for the rest of its budget.
func (a *Agent) executeToolCtx(ctx context.Context, tc deepseek.ToolCall) string {
	if a.executeToolFn != nil {
		return a.executeToolFn(tc)
	}
	return a.executeToolDefaultCtx(ctx, tc)
}

// runScope returns the context the agent's in-flight RUN published (runScopedRoot),
// or context.Background() when none is in flight. It is the fallback the legacy
// signatures use so that a caller which cannot pass a ctx still inherits the
// run's cancellation instead of a root of its own.
func (a *Agent) runScope() context.Context {
	if a == nil {
		return context.Background()
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.runCtx == nil {
		return context.Background()
	}
	return a.runCtx
}

// executeToolDefault is the real per-tool dispatch (executeTool without the
// test hook), bound to the agent's run scope.
func (a *Agent) executeToolDefault(tc deepseek.ToolCall) string {
	return a.executeToolDefaultCtx(a.runScope(), tc)
}

// executeToolDefaultCtx is the real per-tool dispatch with an explicit
// run-scoped context.
func (a *Agent) executeToolDefaultCtx(ctx context.Context, tc deepseek.ToolCall) string {
	args := tc.Arguments
	if args == nil {
		args = make(map[string]interface{})
	}

	// Reject missing/empty required parameters before execution so the
	// model receives a clear, standardized error it can self-correct from.
	if msg := validateToolArgs(tc.Name, args); msg != "" {
		return msg
	}

	switch tc.Name {
	case toolnames.ToolRead:
		path, _ := args["path"].(string)
		offset, _ := args["offset"].(float64)
		limit, _ := args["limit"].(float64)
		result, err := tools.Read(path, int(offset), int(limit))
		if err != nil {
			return fmt.Sprintf("Error reading %s: %v", path, err)
		}
		return result

	case toolnames.ToolGitBlame:
		filePath, _ := args["file_path"].(string)
		// start_line/end_line arrive as float64 (JSON numbers); zero when
		// omitted — tools.GitBlame applies its own defaults/clamps.
		var startLine, endLine int
		if v, ok := args["start_line"].(float64); ok {
			startLine = int(v)
		}
		if v, ok := args["end_line"].(float64); ok {
			endLine = int(v)
		}
		return tools.GitBlame(filePath, startLine, endLine)

	case toolnames.ToolWrite:
		if msg := a.chatModeWriteRefusal(tc.Name, args); msg != "" {
			return msg
		}
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		// follow_symlink arrives as a JSON boolean; false when omitted —
		// symlink paths are refused by default (protect the link node).
		followSymlink := false
		if v, ok := args["follow_symlink"].(bool); ok {
			followSymlink = v
		}
		err := error(nil)
		if followSymlink {
			err = tools.WriteThroughSymlink(path, content)
		} else {
			err = tools.Write(path, content)
		}
		if err != nil {
			return fmt.Sprintf("Error writing %s: %v", path, err)
		}
		return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)

	case toolnames.ToolEdit:
		if msg := a.chatModeWriteRefusal(tc.Name, args); msg != "" {
			return msg
		}
		path, _ := args[toolnames.ToolArgPath].(string)
		oldStr, _ := args[toolnames.ToolArgOldStr].(string)
		newStr, _ := args[toolnames.ToolArgNewStr].(string)
		// AN EMPTY REPLACEMENT IS NOT AN EDIT. Reading a key the caller did
		// not send yields "", and "" is a perfectly legal value for
		// strings.Replace: it DELETES the matched text. So a misnamed
		// argument (newText for newStr) used to delete the matched line and
		// return "Successfully edited <path>" — silent content loss, the
		// worst shape Rule 15 has (measured 2026-10-04: 6 of 17 edit calls in
		// one session's raw log carried their replacement under "newText").
		// The refusal names the key the caller did send, so the next attempt
		// is informed rather than guessed.
		if newStr == "" {
			return editEmptyReplacementRefusal(path, args)
		}
		// replace_all arrives as a JSON boolean (bool after json.Unmarshal);
		// false when omitted — strict-unique default behavior unchanged.
		replaceAll := false
		if v, ok := args["replace_all"].(bool); ok {
			replaceAll = v
		}
		// follow_symlink: same default-refuse policy as write.
		followSymlink := false
		if v, ok := args["follow_symlink"].(bool); ok {
			followSymlink = v
		}
		var err error
		if followSymlink {
			err = tools.EditThroughSymlink(path, oldStr, newStr, replaceAll)
		} else {
			err = tools.Edit(path, oldStr, newStr, replaceAll)
		}
		if err != nil {
			return fmt.Sprintf("Error editing %s: %v", path, err)
		}
		return fmt.Sprintf("Successfully edited %s", path)

	case toolnames.ToolBash:
		if msg := a.chatModeWriteRefusal(tc.Name, args); msg != "" {
			return msg
		}
		command, _ := args["command"].(string)
		// workdir is optional (string); empty = current directory (BashInDir
		// validates it as an existing directory before running the command).
		workdir, _ := args["workdir"].(string)
		// timeout_sec arrives as float64 (JSON integers decode as float64 via
		// plain json.Unmarshal). Semantics (enforced in BashWithTimeout):
		//   omitted        → default (BashTimeout(), 120s, env-overridable)
		//   timeout_sec>0  → absolute budget of timeout_sec seconds
		//   timeout_sec==0 → unlimited absolute time (idle safety net only)
		//   timeout_sec<0  → invalid; falls back to the default
		var output string
		var err error
		if arg, ok := args["timeout_sec"]; ok {
			if v, ok := arg.(float64); ok {
				output, err = tools.BashWithTimeoutInDirCtx(ctx, command, int(v), workdir)
			} else {
				output, err = tools.BashInDirCtx(ctx, command, workdir)
			}
		} else {
			output, err = tools.BashInDirCtx(ctx, command, workdir)
		}
		if err != nil {
			return fmt.Sprintf("Bash error: %v\n%s", err, output)
		}
		// Deliverable.worktree information-closure (bugfix epic 2026-09-03):
		// the executor creates its Rule-6 worktree by RUNNING git (there is
		// no Go-side `git worktree add` site), so the creation-time backfill
		// is a DETECTION seam here — after every successful bash round,
		// resolve where the executor just worked and, when it is inside a
		// linked worktree of the wrapper row's repo, stamp
		// deliverable.worktree + deliverable.branch into the row.
		// Information closure WHILE THE TASK RUNS — resume/recycle/merge
		// can then see the worktree. Candidates: the bash workdir (falling
		// back to the process cwd) plus any `git -C <path>` target in the
		// command (executors commonly run git from the main repo cwd with
		// an explicit -C worktree path). Best-effort and cheap: a session
		// with no ledger row returns immediately; the git probe runs only
		// when a row exists. The repo is resolved from the row
		// (deliverable.repo) — never guessed from cwd. Gated on a
		// successful command: a FAILED git worktree add created nothing.
		//
		// THE ROW IS THE ONE THIS RUN OWNS (epic raw-prompt-worktree-release,
		// 2026-09-14): autoTaskRowID is the row the harness registered for a
		// bare-prompt/chat run — the face with NO wrapper env — so passing it
		// explicitly is what lets the seam reach that face at all. A wrapper
		// child has already latched autoTaskRowID to the wrapper row
		// (taskregister.go), so both faces hand the seam the same thing they
		// hand the heartbeat, and the ambient GOPI_TASK_WRAPPER_ID stays the
		// fallback inside the seam (TaskWorktreeBackfillRow).
		tools.TaskDeliverableWorktreeBashForRow(a.autoTaskRowID, workdir, command)
		return output

	case toolnames.ToolCodeSearch:
		symbol, _ := args["symbol"].(string)
		hits, dropped, searchCap, err := tools.CodeSearch(codeSearchScope(), symbol)
		if err != nil {
			return fmt.Sprintf("code_search error: %v", err)
		}
		return tools.FormatCodeSymbolHitsWithCap(symbol, hits, dropped, searchCap)

	case toolnames.ToolKeywordSearch:
		query, _ := args["query"].(string)
		// SCOPE=session: search THIS conversation's own on-disk record instead of
		// the tree (internal/tools/session_search.go). It is a SCOPE on the
		// existing tool, not a new top-level tool (PROJECT.md Rule 3), and the
		// result is a STRUCTURED page — round, row type, timestamp, snippet,
		// locator — never the raw JSON line. The current session id comes from
		// the logger this run already holds, so the ordinary call passes no
		// session argument at all.
		if strArgOf(args, "scope") == tools.SessionSearchScopeSession {
			sessionID := strArgOf(args, "session")
			if sessionID == "" {
				if a.SessionLog == nil {
					return "keyword_search error: scope=session needs a session — this run has no session logger, so name one with `session`"
				}
				sessionID = a.SessionLog.SessionID()
			}
			limit := intArgOf(args, "limit")
			includeRaw, _ := args["include_raw_log"].(bool)
			res, err := tools.SessionSearch(tools.SessionSearchOptions{
				Query:         query,
				SessionID:     sessionID,
				Limit:         limit,
				IncludeRawLog: includeRaw,
			})
			if err != nil {
				return fmt.Sprintf("keyword_search error: %v", err)
			}
			return tools.FormatSessionSearchHits(res)
		}
		hits, dropped, searchCap, err := tools.KeywordSearch(keywordSearchScope(), query)
		if err != nil {
			return fmt.Sprintf("keyword_search error: %v", err)
		}
		return tools.FormatTextHitsWithCap(hits, dropped, searchCap)

	case toolnames.ToolJob:
		// Merged job tool (tool-family merge, job family): the action
		// argument selects which operation runs. The original per-tool
		// handler logic (tools.JobStart/JobOutput/JobKill) is preserved
		// unchanged as the action-internal functions.
		switch strArgOf(args, "action") {
		case toolnames.ToolJobActionStart:
			command, _ := args["command"].(string)
			result, err := tools.JobStart(command)
			if err != nil {
				return fmt.Sprintf("job error (start): %v", err)
			}
			return result
		case toolnames.ToolJobActionOutput:
			jobID, _ := args["job_id"].(string)
			result, err := tools.JobOutput(jobID)
			if err != nil {
				return fmt.Sprintf("job error (output): %v", err)
			}
			return result
		case toolnames.ToolJobActionKill:
			jobID, _ := args["job_id"].(string)
			result, err := tools.JobKill(jobID)
			if err != nil {
				return fmt.Sprintf("job error (kill): %v", err)
			}
			return result
		default:
			return fmt.Sprintf("Missing required parameter: action (start|output|kill), got %q", strArgOf(args, "action"))
		}

	case toolnames.ToolTask:
		// Tool-family-merge action dispatch (task family): one `task` tool
		// carries view (task list) | commit (working-area conclusion) |
		// review (post-commit checkpoint). See the schema + the action
		// doc-comments below; dispatch is action-aware.
		action, _ := args["action"].(string)
		switch action {
		case toolnames.ToolTaskView, "":
			if a.TaskState == nil {
				return "No task list yet — emit a [TASK_LIST_BEGIN]...[TASK_LIST_END] block in your next response to establish one."
			}
			return a.TaskState.format()
		case toolnames.ToolTaskCommit:
			return a.commitTaskItem(args)
		case toolnames.ToolTaskReview:
			return a.handleTaskReview(args)
		case toolnames.ToolTaskReframe:
			return a.handleTaskReframe(args)
		default:
			return fmt.Sprintf("task: unknown action %q (want %s)", action, toolnamesTaskActionList())
		}

	case toolnames.ToolWebSearch:
		query, _ := args["query"].(string)
		method, _ := args["method"].(string)
		// count arrives as float64 (JSON numbers decode via json.Unmarshal);
		// zero when omitted — tools.WebSearch applies its own default/clamp.
		var count int
		if v, ok := args["count"].(float64); ok {
			count = int(v)
		}
		return tools.WebSearch(query, method, count)

	case toolnames.ToolWebFetch:
		url, _ := args["url"].(string)
		// max_chars arrives as float64 (JSON numbers decode via
		// json.Unmarshal); zero when omitted — tools.WebFetch applies its own
		// default/clamp.
		var maxChars int
		if v, ok := args["max_chars"].(float64); ok {
			maxChars = int(v)
		}
		return tools.WebFetch(url, maxChars)

	case toolnames.ToolMemory:
		// Tool-family-merge action dispatch (memory family): one `memory` tool
		// carries search (retrieve past experience) | store (distill a lesson)
		// | feedback (vote on a hit) | update (correct a record). Dispatch is
		// action-aware; the bodies are the former separate memory_* tools.
		action, _ := args["action"].(string)
		switch action {
		case toolnames.ToolMemoryActionSearch:
			query, _ := args["query"].(string)
			// limit arrives as float64 (JSON numbers decode via json.Unmarshal);
			// zero when omitted — tools.MemorySearch applies its own default.
			var limit int
			if v, ok := args["limit"].(float64); ok {
				limit = int(v)
			}
			intent, _ := args["intent"].(string)
			result, err := tools.MemorySearch(query, limit, intent)
			if err != nil {
				return fmt.Sprintf("memory error: %v", err)
			}
			// (Procedural rows merge inside tools.MemorySearch — the engine-level
			// merge serves the LLM tool path and the CLI path alike.)
			// A real, successful search (not an error) satisfies the run's
			// task-start memory requirement — the harness task-start checkpoint
			// will not force a redundant search (对称 with memoryStored). An
			// empty result still counts: 取不到 itself is a signal the run
			// searched.
			a.memorySearched = true
			return result

		case toolnames.ToolMemoryActionStore:
			topic, _ := args["topic"].(string)
			question, _ := args["question"].(string)
			answer, _ := args["answer"].(string)
			source, _ := args["source"].(string)
			keywords := stringArgs(args["keywords"])
			// type=procedural routes the call to the workflow table (GLM
			// five-layer model, procedural layer): name/trigger/steps fields
			// carry the workflow, topic/question/answer stay the declarative
			// path. Everything else behaves identically (store result satisfies
			// the task-end requirement either way).
			if storeType, _ := args["type"].(string); storeType == "procedural" {
				name, _ := args["name"].(string)
				trigger, _ := args["trigger"].(string)
				precond, _ := args["preconditions"].(string)
				steps := stringArgs(args["steps"])
				toolsUsed := stringArgs(args["tools_used"])
				w := tools.ProceduralWorkflow{
					Name: name, Trigger: trigger, Preconditions: precond,
					ToolsUsed: toolsUsed,
				}
				if topic != "" && name == "" {
					w.Name = topic
				}
				if trigger == "" {
					w.Trigger = question // degrade gracefully: question ≈ when to use
				}
				for i, s := range steps {
					w.Steps = append(w.Steps, tools.ProceduralStep{Action: fmt.Sprintf("%d", i+1), Detail: s})
				}
				if answer != "" && len(w.Steps) == 0 {
					// Steps omitted → the freeform answer becomes one step, so a
					// lazy call still lands a searchable workflow, not an empty one.
					w.Steps = []tools.ProceduralStep{{Action: "do", Detail: answer}}
				}
				result, err := tools.ProceduralStore(&w, source)
				if err != nil {
					return fmt.Sprintf("memory error: %v", err)
				}
				a.memoryStored = true
				return result
			}
			// The provenance pointer (epic 自动记忆 item 2/5): the session this store
			// call happens in and the round it happens at, so a captured row can be
			// walked back to the turn that produced it. The model passes nothing —
			// session and round are properties of the RUN (memory_origin_stamp.go); a
			// run with no session log stamps no pointer rather than a made-up one.
			// A REPLAY STORES NOTHING HERE EITHER (internal/replaymode, epic defect
			// A). This is the MODEL-INITIATED shape of the same write — the two
			// automatic capture paths are gated in selfnotecapture*.go — and a
			// replay's rounds still run the real dispatcher, so a `memory store`
			// call in a recording would reach the operator's ledger unless this
			// site asks the flag too. The turn gets an answer that NAMES the
			// refusal rather than an empty success: the tool result is the run's
			// own transcript, and a silent "" would read as a store that worked.
			// The decision itself lives in memory_replay_guard.go, so this file
			// carries no new import and no anchor below it moves (Rule 4).
			if refusal := a.memoryWriteReplayRefusal("run the memory tool's store in the operator's ledger"); refusal != "" {
				return refusal
			}
			origin := a.memoryStoreOrigin()
			result, err := tools.MemoryStoreWithOrigin(topic, question, answer, source, keywords, origin)
			if err != nil {
				return fmt.Sprintf("memory error: %v", err)
			}
			// A real, successful store (not a validation/garbage rejection) means
			// the run's task-end memory requirement is satisfied — the harness
			// completion checkpoint will not force a redundant store. Only the
			// completed store is authoritative; errors leave the need pending so
			// the checkpoint can still fire.
			if !strings.HasPrefix(result, "memory error:") {
				a.memoryStored = true
				// NO ASSET SYNC RUNS HERE (epic memory-store-sync-decouple,
				// 2026-10-27). A store's success criterion is the LOCAL write, and
				// that is all this return states: cross-node propagation is the
				// operator's own action and a ONE-WAY COPY since 2026-11-05 —
				// `gopi memory backup` ships this ledger to a declared peer and
				// imports nothing back (the cross-node sync family was deleted by
				// 5677cc6e; config.LedgerCopyForwardReplacement states it) — never
				// a leg this process spawns.
			}
			return result

		case toolnames.ToolMemoryActionFeedback:
			// id arrives as float64 (JSON numbers decode via json.Unmarshal).
			var id int
			if v, ok := args["id"].(float64); ok {
				id = int(v)
			}
			rating, _ := args["rating"].(string)
			reason, _ := args["reason"].(string)
			// Session dedup key: one vote per record per task. CLI maintenance
			// votes (empty task) bypass dedup — that channel is always recorded.
			taskID := ""
			if a.SessionLog != nil {
				taskID = a.SessionLog.SessionID()
			}
			if refusal := a.memoryWriteReplayRefusal("record a memory feedback vote in the operator's ledger"); refusal != "" {
				return refusal
			}
			result, err := tools.MemoryFeedback(id, rating, reason, taskID)
			if err != nil {
				return fmt.Sprintf("memory error: %v", err)
			}
			return result

		case toolnames.ToolMemoryActionUpdate:
			var id int
			if v, ok := args["id"].(float64); ok {
				id = int(v)
			}
			topic, _ := args["topic"].(string)
			question, _ := args["question"].(string)
			answer, _ := args["answer"].(string)
			source, _ := args["source"].(string)
			tier, _ := args["tier"].(string)
			f := tools.MemoryUpdateFields{
				Topic:    topic,
				Question: question,
				Answer:   answer,
				Source:   source,
				Tier:     tier,
				Keywords: stringArgs(args["keywords"]),
			}
			if v, ok := args["confidence"].(float64); ok {
				c := v
				f.Confidence = &c
			}
			if refusal := a.memoryWriteReplayRefusal("rewrite a memory record in the operator's ledger"); refusal != "" {
				return refusal
			}
			// Same engine function as the CLI (gopi memory update <id> ...) —
			// tool and CLI behavior are identical by construction.
			result, err := tools.MemoryUpdate(id, f)
			if err != nil {
				return fmt.Sprintf("memory error: %v", err)
			}
			return result

		default:
			return fmt.Sprintf("memory: unknown action %q (want search|store|update|feedback)", action)
		}

	case toolnames.ToolTaskSubmit:
		summary, _ := args["summary"].(string)
		payload, _ := args["payload"].(string)
		parentID, _ := args["parent_task_id"].(string)
		tags := stringArgs(args["tags"])
		// M4.8 deliverable closure: the repo defaults to the submitter's
		// cwd (server mode = the workspace cwd) and is verified with
		// `git rev-parse --is-inside-work-tree` inside TaskSubmit — a
		// non-repo dir aborts the whole submit (t-1788089584). The llm
		// tool keeps this strict pre-resolution (an llm submitter's
		// non-repo cwd is an error, surfaced to the model); TaskSubmit
		// also defaults an empty repo to cwd but leniently for internal
		// auto paths (epic task-submit-repo-default, t-1788441090).
		repo, _ := args["repo"].(string)
		// repoNote carries the dead-workspace repair's own line, if it ran: the
		// model is told WHICH repository the row ended up naming and why.
		var repoNote string
		if strings.TrimSpace(repo) == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Sprintf("task_submit error: resolve default repo (cwd): %v", err)
			}
			// DURABLE, not the cwd itself: an auto-submit filed from inside a
			// run's GROUND (the linked worktree a worker creates for the run)
			// must name the main checkout that outlives the run — otherwise
			// the row is born naming a directory its own parent deletes (epic
			// 01a0d55b: the TOO_BIG reframe's children, refused at spawn with
			// "the repo is not a git work tree").
			durable, durableNote, derr := tools.DurableRepoForSubmitWithNote(cwd)
			if derr != nil {
				// THE DEAD-WORKSPACE REPAIR (epic 01a15629): a submit filed
				// from a workspace path that was already reclaimed has nothing
				// for git to resolve. The ledger row that recorded the path
				// still names the repository, so the value is repaired and the
				// fact is reported in the tool's own result (never silently,
				// Rule 15). Reached ONLY when the resolution failed, so a
				// normal submit's behaviour is untouched.
				repaired, note := tools.RepairDurableRepoForSubmit(cwd)
				if repaired == cwd {
					return fmt.Sprintf("task_submit error: resolve default repo: %v", derr)
				}
				repoNote = note
				durable = repaired
			} else if durableNote != "" {
				// THE CLONE-GROUND ARM (epic 01a160a2): the cwd is this run's
				// clone-channel workspace, so the row names the repository the
				// workspace was cloned from. Reported in the result the model
				// reads, so the substitution is never silent.
				repoNote = durableNote
			}
			repo = durable
		}
		// SERIAL CHAIN (operator ruling, 2026-10-05): an omitted parent_task_id
		// does NOT mean "no dependency" — it means "the item I submitted just
		// before this one". The model submits a split in the order it must run,
		// so the previous submission is the producer.
		//
		// WHAT THE CHAIN DOES *NOT* DO (epic 01a12e34, measured 2026-10-12):
		// it no longer falls back to the run's OWN row when the run has not
		// submitted yet. That fallback made the head of every session's chain
		// park behind the submitter itself (a row still in_progress BY
		// CONSTRUCTION), and item i hanging off item i-1 propagated the park to
		// the whole fan — three independent bug rows went out [queue-skip] with
		// no dispatcher able to take one. The first auto-linked submit is now
		// PARENTLESS and claimable immediately. The rule itself, its condition
		// and every sentence the model reads live in task_submit_chain.go
		// (taskSubmitResolveChain / taskSubmitChainNote), so there is ONE
		// definition of "is this row parked" and a test can drive it without a
		// ledger. An explicit parent_task_id is honoured VERBATIM, and the
		// INDEPENDENT sentinel opts this submit out of the chain entirely.
		decision := taskSubmitResolveChain(parentID, a.lastSubmitRowID)
		parentID = decision.parentID
		// Anti-self-reference (ADDENDUM #5): the row is stamped with THIS
		// session's id (source=llm) so `task next`/`task serve` can exclude
		// this session's own submissions from its claim selection.
		// M4.11: explicit priority (rule ④) overrides the tags derivation.
		priority := 0
		if v, ok := args["priority"].(float64); ok && v > 0 {
			priority = int(v)
		}
		// Phase-1 attachments: the repeatable "attachments" array (absolute
		// paths — archived into the task session dir at submit). Absent =
		// no attachment archiving.
		var attachments []string
		if arr, ok := args["attachments"].([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					attachments = append(attachments, s)
				}
			}
		}
		result, rowID, err := tools.TaskSubmitReturningID(tools.TaskSubmitOptions{
			Summary:      summary,
			Payload:      payload,
			Tags:         strings.Join(tags, ","),
			ParentTaskID: parentID,
			Source:       taskSourceLLM,
			SessionID:    a.sessionIDForTasks(),
			Model:        a.Model,
			Repo:         repo,
			Priority:     priority,
			Attachments:  attachments,
		})
		if err != nil {
			// The chain does not advance on a failure: the NEXT submission
			// still links to the last row that really landed (same semantics
			// as dispatchReframeRoadmap).
			return fmt.Sprintf("task_submit error: %v", err)
		}
		if rowID != "" {
			a.lastSubmitRowID = rowID
		}
		if repoNote != "" {
			result += "\n" + repoNote
		}
		return result + a.taskSubmitChainNote(decision, rowID)

	case toolnames.ToolVisionAnalyze:
		imagePath, _ := args["image_path"].(string)
		prompt, _ := args["prompt"].(string)
		result, err := tools.VisionAnalyze(imagePath, prompt)
		if err != nil {
			return fmt.Sprintf("vision_analyze error: %v", err)
		}
		return result

	case toolnames.ToolText2img:
		src, _ := args["src"].(string)
		out, _ := args["out"].(string)
		result, err := tools.Text2Img(src, out, text2imgOptionsFromArgs(args))
		if err != nil {
			return fmt.Sprintf("text2img error: %v", err)
		}
		return result

	case toolnames.ToolScreenCapture:
		// The "eye" of the capture→act→verify loop: grab the LIVE X11 root
		// window (or a window / rectangle of it) and return machine-readable
		// ground truth. Every argument is optional (display defaults to
		// $DISPLAY inside the engine, out to an absolute path in the capture
		// class of gopi's scratch tree — never cwd, Rule 30) —
		// validateToolArgs deliberately requires nothing here. The engine is
		// reached through the screenCaptureEngine seam so the model path and
		// the CLI path (`gopi tool screen_capture`) are the same call; when the
		// model asks for a RECT mode, the mode-aware seam is used instead, so
		// the plain whole-screen call stays exactly the call it always was.
		display, _ := args["display"].(string)
		out, _ := args["out"].(string)
		window, _ := args["window"].(bool)
		self, _ := args["self"].(bool)
		decorations, _ := args["decorations"].(bool)
		area, _ := args["area"].(string)
		if !window && !self && strings.TrimSpace(area) == "" {
			result, err := screenCaptureEngine(display, out)
			if err != nil {
				return fmt.Sprintf("%s: %v", screenCaptureErrorPrefix, err)
			}
			return result
		}
		opts := tools.ScreenCaptureOptions{Display: display, Out: out, Mode: tools.ScreenCaptureModeWindow}
		if self {
			// The window that HOSTS this gopi process — resolved by
			// _NET_WM_PID through the process' own tty chain, never by focus
			// (internal/shot/pidwindow.go). It is the answer to "where am I
			// running", the question `window` does NOT answer.
			opts.Mode = tools.ScreenCaptureModeSelf
			opts.Decorations = decorations
		}
		if strings.TrimSpace(area) != "" {
			rect, err := tools.ParseScreenCaptureArea(area)
			if err != nil {
				return fmt.Sprintf("%s: %v", screenCaptureErrorPrefix, err)
			}
			opts.Mode = tools.ScreenCaptureModeArea
			opts.Area = rect
		}
		result, err := screenCaptureModeEngine(opts)
		if err != nil {
			return fmt.Sprintf("%s: %v", screenCaptureErrorPrefix, err)
		}
		return result

	case toolnames.ToolContextTrim:
		// Purely a context operation: mutate the message array (remove or
		// replace the dead-end pair) and report what happened. No-op cases
		// (unknown / incomplete / already-processed id) return a truthful
		// message — the tool itself never errors.
		callID, _ := args["call_id"].(string)
		replaceWith, _ := args["replace_with"].(string)
		removed, replaced := a.Ctx.RemoveToolPair(callID, replaceWith)
		switch {
		case replaced:
			return fmt.Sprintf("context_trim: replaced result of call %s with conclusion (pair kept, roles unchanged)", callID)
		case removed > 0:
			return fmt.Sprintf("context_trim: removed call %s + its result (%d message(s) dropped from context)", callID, removed)
		default:
			return fmt.Sprintf("context_trim: no-op — call %s not found, incomplete pair, or already trimmed (silent)", callID)
		}

	case toolnames.ToolChatSurface:
		// The CHAT-SCOPED tool (chat_surface.go): a chat turn's own eye and hand
		// on the surface it is answering in. It is reachable only because it was
		// advertised, and it is advertised only on the chat wire
		// (chatOpsToolDefs) — the task path's tool list never carries it.
		return a.runChatSurfaceTool(args)

	default:
		b, _ := json.Marshal(args) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		return fmt.Sprintf("Unknown tool: %s with args %s", tc.Name, string(b))
	}
}

// text2imgOptionsFromArgs converts tool_use arguments (floats — JSON numbers)
// into text2img render options. Missing/zero values fall back to the package
// defaults inside internal/text2img, so the agent tool and the CLI subcommand
// share one parameter surface.
func text2imgOptionsFromArgs(args map[string]interface{}) text2img.Options {
	intArg := func(key string) int {
		if f, ok := args[key].(float64); ok {
			return int(f)
		}
		return 0
	}
	return text2img.Options{
		Size:   intArg("size"),
		Cols:   intArg("cols"),
		Lh:     intArg("lh"),
		Lines:  intArg("lines"),
		Margin: intArg("margin"),
		Font:   strArgOf(args, "font"),
	}
}

// sessionIDForTasks returns the current session id for task-ledger
// provenance (task_submit's anti-self-reference stamp): the SessionLog's id
// when a session is live. autoTaskSessionID is a test seam (tests build
// Agent{} literals with no SessionLogger). Empty when neither exists — rows
// without a session are never excluded from claims (an agent-less submitter
// imposes no queue blind spot).
func (a *Agent) sessionIDForTasks() string {
	if a.autoTaskSessionID != "" {
		return a.autoTaskSessionID
	}
	if a.SessionLog != nil {
		return a.SessionLog.SessionID()
	}
	return ""
}

// toolnamesVerdictEnum renders the reframe verdict vocabulary for the tool
// schema (the single reader of internal/taskverdict — Rule 14).
func toolnamesVerdictEnum() string { return strings.Join(taskverdict.All(), "|") }

// toolnamesTaskActionList renders the task tool's action vocabulary once, for
// its schema and every error message (Rule 1: never type the action words
// inline at a second site).
func toolnamesTaskActionList() string {
	return strings.Join([]string{
		toolnames.ToolTaskView, toolnames.ToolTaskCommit,
		toolnames.ToolTaskReview, toolnames.ToolTaskReframe,
	}, "|")
}

// taskSourceLLM is the task-ledger source value stamped by LLM-originated
// submits (the task_submit tool and the reframe auto-enqueue path). The VALUE
// lives in internal/tasksource (Rule 14): the old spelling "llm" is the
// historical alias of the current `agent`.
//
// NOTE FOR THE WORKER LOOP: this constant names the SESSION's provenance, which
// is true for the tool call but says nothing about the row's ROLE. A follow-up
// row filed by the worker loop itself belongs to the `worker` role
// (tasksource.Worker) — naming that one is a per-site decision, not a flag flip.
const taskSourceLLM = tasksource.Agent

// strArgOf reads a string argument from tool_use args ("" when missing).
func strArgOf(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}

// intArgOf reads an integer argument from tool_use args (0 when missing). JSON
// numbers arrive as float64; a caller that says nothing gets 0, which every
// consumer here reads as "take the default" — never as an explicit zero.
func intArgOf(args map[string]interface{}, key string) int {
	if f, ok := args[key].(float64); ok {
		return int(f)
	}
	return 0
}

// stringArgs converts a JSON array of strings (e.g. the memory_store
// keywords breadcrumbs) into a []string. Tolerant: absent or mistyped
// arguments yield nil — keywords stays optional, backward compatible.
func stringArgs(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// validateToolArgs checks that required parameters are present and non-empty
// before a tool executes. It returns a standardized error message the model
// can act on, or "" when the arguments are valid.
//
// Tool arguments are extracted with zero-value defaults — a missing or
// mistyped key silently yields "" — so without this guard tools would run
// against empty paths / commands and fail with confusing, per-tool errors.
// Returning "Missing required parameter: <name>" lets the model self-correct
// by re-issuing the call with the parameter filled in.
func validateToolArgs(name string, args map[string]interface{}) string {
	strArg := func(key string) string {
		s, _ := args[key].(string)
		return strings.TrimSpace(s)
	}
	switch name {
	case toolnames.ToolRead, toolnames.ToolWrite, toolnames.ToolEdit:
		if strArg("path") == "" {
			return "Missing required parameter: path"
		}
	case toolnames.ToolGitBlame:
		if strArg("file_path") == "" {
			return "Missing required parameter: file_path"
		}
	case toolnames.ToolBash:
		if strArg("command") == "" {
			return "Missing required parameter: command"
		}
	case toolnames.ToolCodeSearch:
		if strArg("symbol") == "" {
			return "Missing required parameter: symbol"
		}
	case toolnames.ToolKeywordSearch:
		if strArg("query") == "" {
			return "Missing required parameter: query"
		}
	case toolnames.ToolJob:
		// The merged job tool's required parameters are action-conditional:
		// action=start needs command; action=output/kill need job_id. The
		// action itself is always required (schema "required": ["action"],
		// and a bad/missing action is rejected below so the model sees the
		// valid enum rather than an empty-command error).
		switch strArg("action") {
		case toolnames.ToolJobActionStart:
			if strArg("command") == "" {
				return "Missing required parameter: command"
			}
		case toolnames.ToolJobActionOutput, toolnames.ToolJobActionKill:
			if strArg("job_id") == "" {
				return "Missing required parameter: job_id"
			}
		default:
			return fmt.Sprintf("Missing required parameter: action (start|output|kill), got %q", strArg("action"))
		}
	case toolnames.ToolWebSearch:
		if strArg("query") == "" {
			return "Missing required parameter: query"
		}
	case toolnames.ToolTaskSubmit:
		if strArg("summary") == "" {
			return "Missing required parameter: summary"
		}
	case toolnames.ToolTask:
		// Action-aware (tool-family merge — task family): each action has its
		// own required params.
		action := strArg("action")
		switch action {
		case toolnames.ToolTaskCommit:
			if strArg("task_id") == "" {
				return "Missing required parameter: task_id (task action=commit)"
			}
			if strArg("status") == "" {
				return "Missing required parameter: status (task action=commit)"
			}
			if strArg("conclusion") == "" {
				return "Missing required parameter: conclusion (task action=commit)"
			}
		case toolnames.ToolTaskReview:
			if strArg("verdict") == "" {
				return "Missing required parameter: verdict (task action=review, A-E)"
			}
		case toolnames.ToolTaskReframe:
			// The verdict's conditional requirements ARE the anti-bureaucracy
			// gate; checking here (as well as in handleTaskReframe) keeps an
			// obviously incomplete call from ever reaching dispatch.
			if err := validateReframe(parseReframeInput(args)); err != nil {
				return err.Error()
			}
		case toolnames.ToolTaskView, "":
			// no required params; view is read-only.
		default:
			return "Invalid task action (want " + toolnamesTaskActionList() + "): " + action
		}
	case toolnames.ToolChatSurface:
		// The chat-scoped tool is action-aware for the same reason the merged
		// families are: each verb has its own required operand, and a missing one
		// must come back as the sentence that names it (the model self-corrects
		// from a message, not from a silent empty run).
		if msg := validateChatSurfaceArgs(args); msg != "" {
			return msg
		}
	case toolnames.ToolWebFetch:
		if strArg("url") == "" {
			return "Missing required parameter: url"
		}
	case toolnames.ToolMemory:
		// Action-aware (tool-family merge — memory family): each action has
		// its own required params (search → query; store → topic/question/
		// answer unless procedural; feedback → id+rating; update → id + one
		// field).
		action := strArg("action")
		switch action {
		case toolnames.ToolMemoryActionSearch:
			if strArg("query") == "" {
				return "Missing required parameter: query (memory action=search)"
			}
		case toolnames.ToolMemoryActionStore:
			for _, key := range []string{"topic", "question", "answer"} {
				if strArg(key) == "" {
					// type=procedural has its own identity fields (name/trigger);
					// the declarative triple is only required there.
					if !(strArg("type") == "procedural" && strArg("name") != "" && strArg("trigger") != "") {
						return "Missing required parameter: " + key + " (memory action=store)"
					}
				}
			}
		case toolnames.ToolMemoryActionFeedback:
			id, ok := args["id"].(float64)
			if !ok || id <= 0 {
				return "Missing required parameter: id (memory action=feedback)"
			}
			if strArg("rating") == "" {
				return "Missing required parameter: rating (memory action=feedback)"
			}
		case toolnames.ToolMemoryActionUpdate:
			id, ok := args["id"].(float64)
			if !ok || id <= 0 {
				return "Missing required parameter: id (memory action=update)"
			}
			if strArg("tier") == "" && strArg("topic") == "" && strArg("question") == "" &&
				strArg("answer") == "" && strArg("source") == "" &&
				args["confidence"] == nil && args["keywords"] == nil {
				return "memory(action=update): nothing to update (set at least one field)"
			}
		default:
			return "Invalid memory action (want search|store|update|feedback): " + action
		}
	case toolnames.ToolContextTrim:
		if strArg("call_id") == "" {
			return "Missing required parameter: call_id"
		}
	case toolnames.ToolVisionAnalyze:
		if strArg("image_path") == "" {
			return "Missing required parameter: image_path"
		}
	case toolnames.ToolText2img:
		if strArg("src") == "" {
			return "Missing required parameter: src"
		}
	}
	return ""
}

// stderrIsTTY reports whether stderr is attached to an interactive terminal.
// Pipes and regular files are not character devices; TTYs are.
func stderrIsTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// flowWriterFor returns the writer this agent's CHAT-VISIBLE flow output must
// go to: the carrier's own seam when one is installed (OnThinking != nil), and
// os.Stderr otherwise.
//
// WHY a carrier re-points it: the flow stream and a carrier that OWNS the
// screen cannot share one destination. `gopi chat`'s reserved layout paints the
// answer band, the editing row and the status row at ABSOLUTE rows inside a
// scrolling region, so a flowed reasoning delta lands on a row the layout owns —
// measured on a 100x30 terminal, the thinking block tore the band's rows and the
// `[gopi]` notices overwrote the status row mid-line.
//
// The fallback is not a convenience: cooked/flow output, a piped run and the
// testsuite's greps read those exact bytes, so with no seam installed every
// caller below writes to os.Stderr precisely as it always did.
func (a *Agent) flowWriterFor() io.Writer {
	if a.OnThinking != nil {
		return flowSeamWriter{a.OnThinking}
	}
	if a.StreamThinkingDeltas {
		// A RAW carrier with no callback: the stream is DROPPED, not sent to
		// os.Stderr. See Agent.StreamThinkingDeltas — raw bytes on a terminal
		// are reasoning a reader cannot tell from the answer, so the honest
		// behaviour for a half-configured raw carrier is to show nothing.
		return io.Discard
	}
	return os.Stderr
}

// flowSeamWriter adapts the OnThinking callback to an io.Writer, so the flow
// sites can hand their bytes over verbatim (markers included) instead of each
// one branching on whether a carrier is installed.
type flowSeamWriter struct{ fn func(string) }

// Write forwards the chunk to the seam and reports it fully consumed. The seam
// takes a string, so the byte count is the chunk's — exactly what a caller of
// io.Writer expects; there is no partial write to report.
func (w flowSeamWriter) Write(p []byte) (int, error) {
	w.fn(string(p))
	return len(p), nil
}

// thinkingMarkers holds the open/close delimiters used to visually separate
// live reasoning output from the final answer.
type thinkingMarkers struct {
	open  string
	close string
}

// emitThinking routes one piece of the live reasoning display through the flow
// destination (see flowWriterFor): the carrier's own seam when one is installed
// — so a carrier that OWNS the screen takes the fragment instead of having its
// rows written over — and os.Stderr otherwise, byte-for-byte as before. It is
// the ONE write point for a reasoning fragment, so the marker bytes and the
// text they wrap cannot be addressed to two different writers.
func (a *Agent) emitThinking(s string) {
	if s == "" {
		return
	}
	fmt.Fprint(a.flowWriterFor(), s)
}

// newThinkingMarkers returns markers for the given terminal state:
//   - TTY: reasoning is dimmed with ANSI SGR codes.
//   - non-TTY: a stable textual prefix is used so piped output stays parseable.
//
// The close marker always ends with a newline so the final answer starts on
// a fresh line, visually separate from the thinking block.
func newThinkingMarkers(tty bool) thinkingMarkers {
	if tty {
		return thinkingMarkers{open: ansiDim + thinkingPrefix, close: ansiReset + "\n"}
	}
	return thinkingMarkers{open: thinkingPrefix, close: "\n"}
}

// thinkingMarkersFor returns the markers for THIS agent's live reasoning
// stream: the stderr-shaped open/close pair for a carrier that receives the
// stream as BYTES (the historic shape, unchanged), and the EMPTY pair for a
// carrier that RENDERS the reasoning itself (Agent.StreamThinkingDeltas).
//
// WHY ONE SITE FOR THE DECISION: the markers decide what the receiving side
// must UNDO before it can draw the text — strip an SGR dim, drop a `[thinking] `
// prefix, absorb a trailing reset+newline. A carrier that draws cells must
// receive no decoration at all, and the choice has to be the same one on the
// streaming path and on the non-streaming fallback: two sites each building
// their own markers is exactly how one of them keeps a prefix the renderer then
// prints as content.
func (a *Agent) thinkingMarkersFor() thinkingMarkers {
	if a.StreamThinkingDeltas {
		return thinkingMarkers{}
	}
	return newThinkingMarkers(stderrIsTTY())
}

// recordIDRe extracts memory record ids ("#N" where N is a positive integer)
// from a memory_search tool_result body — the engine's render embeds one
// "#N tier=…" meta line per returned hit, so the record ids of a search are
// recovered from its own result without trusting any LLM-supplied string.
var recordIDRe = regexp.MustCompile(`#(\d+)(?:[^#]|$)`)

// currentMemorySearchIDs returns the tool_use ids of the memory action=search
// calls executed in the CURRENT round. These are on a one-round grace period:
// their results were just produced and the LLM has NOT yet had a round to
// vote on them (a model decides its tool calls before seeing results, so a
// search in this round cannot be useful-voted until next round). Exempting
// them this round guarantees a search is never dropped before its hits could
// be recognized useful; a vote lands next round and the slim, running after
// that round, then keeps or drops it.
func currentMemorySearchIDs(toolCalls []deepseek.ToolCall) map[string]bool {
	ids := make(map[string]bool)
	for _, tc := range toolCalls {
		if isMemorySearchCall(&tc) && tc.ID != "" {
			ids[tc.ID] = true
		}
	}
	return ids
}

// isMemorySearchCall reports whether a tool call is a memory action=search —
// the merged `memory` tool with action "search" (formerly the standalone
// memory_search tool). Action-aware classifier (tool-family merge, memory
// family): the same helper serves the context-slim and task-start-check logic.
func isMemorySearchCall(tc *deepseek.ToolCall) bool {
	if tc.Name != toolnames.ToolMemory {
		return false
	}
	action, _ := tc.Arguments["action"].(string)
	return action == toolnames.ToolMemoryActionSearch
}

// isMemoryFeedbackCall reports whether a tool call is a memory action=feedback
// — the merged `memory` tool with action "feedback" (formerly the standalone
// memory_feedback tool). Action-aware classifier serving the context-slim
// useful-vote scan.
func isMemoryFeedbackCall(tc *deepseek.ToolCall) bool {
	if tc.Name != toolnames.ToolMemory {
		return false
	}
	action, _ := tc.Arguments["action"].(string)
	return action == toolnames.ToolMemoryActionFeedback
}

// whitelistContextSlim implements the memory_search WHITELIST context
// slimming (memory action=search pairs). Default = drop every search pair
// from the message array; only a pair whose returned records were explicitly
// voted useful survives. Association is by record id parsed from each
// search's own tool_result, not by an LLM tool_use_id (models hallucinate
// call ids). A pair that showed no useful-voted record (including never-voted
// and useless-voted) is dropped — "no mention is also useless for this
// session". Searches executed THIS round (graceSet) are exempted so the LLM
// always has one full round to vote before a pair can be dropped. The DB-side
// vote already landed when the memory action=feedback dispatched; this is
// purely a context operation, task-local (a.Ctx.Messages is the whole current
// session).
func (a *Agent) whitelistContextSlim(graceSet map[string]bool) {
	useful := a.usefulVotedRecordIDs()
	var dropIDs []string
	for i := 0; i < len(a.Ctx.Messages); i++ {
		asst := &a.Ctx.Messages[i]
		if asst.Role != "assistant" {
			continue
		}
		for k := range asst.ToolCalls {
			tc := &asst.ToolCalls[k]
			if !isMemorySearchCall(tc) {
				continue
			}
			if graceSet[tc.ID] {
				continue // just searched this round — give the model a round to vote
			}
			if a.searchReturnedUseful(i, tc.ID, useful) {
				continue // at least one of its records was voted useful → keep
			}
			dropIDs = append(dropIDs, tc.ID)
		}
	}
	for _, tid := range dropIDs {
		if removed, _ := a.Ctx.RemoveToolPair(tid, ""); removed > 0 && a.Logger != nil {
			a.Logger.Printf("🧹 context slim (whitelist): dropped unrewarded memory_search pair %s (%d msg(s))", tid, removed)
		}
	}
}

// usefulVotedRecordIDs collects the set of memory record ids that were voted
// rating=useful by any memory action=feedback tool call anywhere in the
// current context (this task). Votes may be immediate or deferred; scanning
// the whole context keeps a search pair alive as long as any record it
// returned was recognized useful at some point.
func (a *Agent) usefulVotedRecordIDs() map[int]bool {
	useful := make(map[int]bool)
	for i := range a.Ctx.Messages {
		asst := &a.Ctx.Messages[i]
		if asst.Role != "assistant" {
			continue
		}
		for k := range asst.ToolCalls {
			tc := &asst.ToolCalls[k]
			if !isMemoryFeedbackCall(tc) {
				continue
			}
			rating, _ := tc.Arguments["rating"].(string)
			if rating != "useful" {
				continue
			}
			switch id := tc.Arguments["id"].(type) {
			case float64:
				useful[int(id)] = true
			case int:
				useful[id] = true
			}
		}
	}
	return useful
}

// searchReturnedUseful reports whether the memory_search pair beginning at
// assistant message index i (tool_use id tid) returned at least one record
// appearing in the useful set. The matching tool result is located after the
// assistant message (tool results follow their owner in tool-call order).
func (a *Agent) searchReturnedUseful(i int, tid string, useful map[int]bool) bool {
	for j := i + 1; j < len(a.Ctx.Messages); j++ {
		if a.Ctx.Messages[j].Role == "assistant" {
			break
		}
		if a.Ctx.Messages[j].Role != "tool" || a.Ctx.Messages[j].ToolCallID != tid {
			continue
		}
		for _, m := range recordIDRe.FindAllStringSubmatch(a.Ctx.Messages[j].Content, -1) {
			if len(m) < 2 {
				continue
			}
			if n, err := strconv.Atoi(m[1]); err == nil && useful[n] {
				return true
			}
		}
		return false
	}
	return false
}

// taskAreaConclusionsMarker is the leading marker of the authoritative
// carry-forward block injected after a write_task_area commit (POC H3:
// conclusions embedded in the next round's context are treated as fact by the
// model — cheaper and more robust than an isolated assistant "CONCLUSION:"
// line the model may doubt). The marker makes the block machine-recognizable
// (tests, logging, and the anchor policy in context.go) and tells the model
// these are STABLE recorded facts, not working notes.
// THE VALUE IS NOT DEFINED HERE (Rule 14): it is
// sessionevent.HarnessTaskAreaConclusionsMarker, in the leaf package the
// session-log READERS also import (see consultCheckpointMarker's note for the
// measured cost of the value having lived here).
const taskAreaConclusionsMarker = sessionevent.HarnessTaskAreaConclusionsMarker

// taskAreaConclusionsMessageMarker is the historical name of
// taskAreaConclusionsMarker, kept so the injection/trim call sites and their
// tests read unchanged.
const taskAreaConclusionsMessageMarker = taskAreaConclusionsMarker

// injectTaskAreaConclusions carries the session's recorded write_task_area
// conclusions into context as ONE authoritative user message, placed BEFORE
// any committed-region trim. The POC H4 red line: a conclusion referenced but
// absent invites fabrication — the framework therefore NEVER lets a
// committed conclusion be implied-but-missing; after any commit it re-states
// the full recorded set. In-loop this is the cross-"round" handoff (the next
// LLM round sees the block); across separate runs the same block is served
// by SessionConclusions (resume/reload paths can re-inject it).
func (a *Agent) injectTaskAreaConclusions() {
	sessionID := a.sessionIDForTasks()
	if sessionID == "" || a.Ctx == nil {
		return
	}
	concs, err := tools.SessionConclusions(sessionID)
	if err != nil || len(concs) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString(taskAreaConclusionsMessageMarker)
	for _, c := range concs {
		b.WriteString("\n- ")
		b.WriteString(c.TaskID)
		b.WriteString(": ")
		b.WriteString(c.Conclusion)
	}
	a.Ctx.AddUserMessage(b.String())
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), b.String())
	}
	if a.Logger != nil {
		a.Logger.Printf("🧾 task commit: carried %d committed conclusion(s) forward as authoritative context", len(concs))
	}
}

// trimCommittedTaskArea drops the completed working region — the messages
// between the epic briefing (index 0, never trimmed) and the current round's
// write_task_area commit — now that the conclusion is recorded and carried
// forward. Physical mechanism (POC §3): in-round message-array compaction
// keeps tool_use/tool_result pairing, so the drop walks whole
// assistant+tool-result groups (the orphan-safety discipline TrimToFit /
// RemoveToolPair use). What goes: earlier items' analysis AND this item's own
// analysis region — every committed item's conclusion survives in the
// carry-forward block, so dropping the analysis cannot lose information
// (H3/H4: recorded conclusions, never implied-but-missing). The walk stops at
// the commit round itself: its tool results and the freshly injected
// carry-forward block stay in the live tail untouched.
func (a *Agent) trimCommittedTaskArea() {
	if a.Ctx == nil || len(a.Ctx.Messages) < 3 {
		return
	}
	// The commit round: the NEWEST assistant message carrying a task commit
	// (tool `task`, action=commit — tool-family merge, was write_task_area)
	// tool call (this round's — the loop hooks immediately after its results
	// landed). Earlier commit rounds (already recorded and re-carried) live
	// strictly before it and are part of the dropped region.
	ci := -1
	for i := len(a.Ctx.Messages) - 1; i >= 1; i-- {
		m := &a.Ctx.Messages[i]
		if m.Role != "assistant" {
			continue
		}
		found := false
		for k := range m.ToolCalls {
			tc := &m.ToolCalls[k]
			if tc.Name == toolnames.ToolTask {
				if action, _ := tc.Arguments["action"].(string); action == toolnames.ToolTaskCommit {
					found = true
					break
				}
			}
		}
		if found {
			ci = i
			break
		}
	}
	if ci <= 1 {
		return // commit round is already adjacent to the epic — nothing between
	}
	// Slice out everything in [1, ci): the epic briefing (index 0) is never
	// dropped; the kept tail starts at the commit round (an assistant message).
	//
	// The head is NOT orphan-safe "by design" — that assertion was WRONG and
	// it cost a live run (epic 2026-09-22). Keeping index 0 unconditionally is
	// only safe when messages[0] carries no tool_use whose result lived in
	// [1, ci): an assistant at index 0 with an UNPAIRED tool_use survives the
	// slice as an unbalanced head, the wire then violates the Claude Messages
	// pairing invariant, and the API answers HTTP 400 "messages.1: `tool_use`
	// ids were found without `tool_result` blocks immediately after" — a
	// deterministic death that was also PERSISTED (the raw log recorded the
	// damaged request, so every resume died on its first request). Measured:
	// 346 messages -> 3, orphan call_00_ET_TBCTnLz8k12TGe2ZEVxl2105 kept while
	// its result was cut; dump /home/user/.gopi/dumps/20260922-040822.dump.json.
	//
	// So the boundary is DERIVED, not presumed — and the derivation is the
	// criterion AFTER the slice, not a scan of the window being deleted:
	//
	//	the head may be kept only if, in the KEPT array, the message directly
	//	following it answers every tool_use it carries.
	//
	// The kept array is {0} ∪ [ci, ...), so the head's successor is msgs[ci] —
	// the commit round. That is why keeping a head which carries ANY tool_use
	// is unsound, whatever the answer elsewhere. THE FIRST FIX GOT THIS WRONG
	// and the mistake is worth stating because it is the exact mechanism of the
	// second death (2026-10-31, row 01a18f4a, killed at iteration 317 with the
	// same message at the same index): it scanned [1, ci) for the answer and
	// read "answered inside the window" as SAFE TO KEEP, when [1, ci) is
	// precisely the region the trim deletes. A head whose tool_result sits at
	// index 1 was therefore kept AND its result removed — the answer being in
	// the dropped window is the reason the head must go, and the scan treated
	// it as the reason it could stay. The live run's own log shows the
	// consequence from the other side: it printed "epic briefing at index 0
	// dropped" while the request carried the orphan at messages.0, because the
	// log condition read the KEPT count instead of the dropped one (fixed
	// below).
	head := trimCommittedTaskAreaHead(a.Ctx.Messages, ci)
	if head == committedAreaHeadDropped && a.Logger != nil {
		a.Logger.Printf("🧾 task commit: epic briefing at index 0 dropped — it carries a tool_use that nothing in the kept region can answer (the commit round is an assistant message, so it cannot carry the result; keeping it would send an unbalanced tool_use and the provider rejects that with HTTP 400)")
	}
	removed := ci - head
	if removed > 0 {
		kept := make([]deepseek.Message, 0, len(a.Ctx.Messages)-removed)
		kept = append(kept, a.Ctx.Messages[:head]...)
		kept = append(kept, a.Ctx.Messages[ci:]...)
		a.Ctx.Messages = kept
		if a.Logger != nil {
			a.Logger.Printf("🧾 task commit: trimmed %d message(s) of the committed working region (kept epic + commit round + live tail)", removed)
		}
	}
}

// The two answers trimCommittedTaskAreaHead may give. Named because the
// caller's LOG CONDITION keys on them and an inverted condition is exactly the
// defect this pair was extracted from: the 2026-10-31 run printed "epic
// briefing at index 0 dropped" while keeping it (the condition tested
// `head > 0`, i.e. KEPT), so the run's own log pointed away from the bug.
const (
	committedAreaHeadDropped = 0
	committedAreaHeadKept    = 1
)

// trimCommittedTaskAreaHead decides how much of the head the committed-region
// trim may keep: the number of leading messages to retain (0 or 1).
//
// It used to be a constant 1 ("index 0 is the epic briefing, never trimmed"),
// with orphan-safety justified in prose. The prose was wrong. Keeping index 0
// is safe only when the KEPT array still satisfies the Claude Messages pairing
// invariant:
//
//	for every tool_use the head carries, the message directly after it must
//	carry the matching tool_result.
//
// The kept array is {0} ∪ [ci, ...), so the head's successor is msgs[ci]. Two
// consecutive versions of this function each failed to state that:
//
//   - version 1 kept index 0 unconditionally, so a head tool_use whose result
//     was anywhere in the dropped window survived as an orphan — HTTP 400,
//     measured 2026-09-22 (346 messages trimmed to 3, orphan
//     call_00_ET_TBCTnLz8k12TGe2ZEVxl2105).
//   - version 2 scanned [1, ci) for the answer and KEPT the head when it found
//     one — but [1, ci) is the region the trim deletes, so "answered in there"
//     is the precondition for having to DROP it. Measured 2026-10-31 (row
//     01a18f4a, iteration 317): orphan call_00_xAMlopO2DVTawtVAbfYU7604 at
//     messages.0, its result one message later — inside the deleted window.
//     Dump 01a18f4a-…dump.json.
//
// The criterion is therefore stated once, generally, and it does not depend on
// WHERE an answer happens to live: the head is kept only when the kept array
// can answer it. A head with no tool_use carries nothing to answer and is
// always kept (the epic-briefing path). A head with a tool_use is dropped,
// because its successor after the slice is the commit round — an assistant
// message, which cannot carry a tool_result. Its content is not lost: the
// committed-conclusions block injected before this call is what carries the
// briefing forward (H4), and a malformed request is not context the model can
// read.
func trimCommittedTaskAreaHead(msgs []deepseek.Message, ci int) int {
	if len(msgs) == 0 || ci <= 0 || ci > len(msgs) {
		return len(msgs)
	}
	// The message that will directly follow the head once the window is gone.
	successor := ci
	if successor >= len(msgs) {
		return committedAreaHeadDropped
	}
	headOK := true
	for _, tc := range msgs[0].ToolCalls {
		answered := msgs[successor].Role == "tool" && msgs[successor].ToolCallID == tc.ID
		if !answered {
			headOK = false
			break
		}
	}
	if headOK {
		return committedAreaHeadKept
	}
	return committedAreaHeadDropped
}

// nextIncompletePointerFuncName names the helper the no-retrieval guard
// resolves in this package's production files (the guard fails if it cannot
// find it, so a rename cannot leave the check blind).
const nextIncompletePointerFuncName = "nextIncompletePointer"

// Subtask-beat pointer (brief: brief-memory-prefetch-at-subtask-beats.md).
// Per-commit-return length discipline: the line rides EVERY commit return, so
// it is one line, bounded, and derived from state the harness already tracks.
const (
	// subtaskPointerTitleMaxRunes bounds the next item's title in the pointer.
	// A title is model-authored and can be a paragraph; the pointer only has to
	// be enough to recognize the topic, not to restate it.
	subtaskPointerTitleMaxRunes = 80
	// subtaskPointerEllipsis marks a truncated title (never silent clipping).
	subtaskPointerEllipsis = "…"
	// subtaskPointerFmt is the pointer line: the beat, the item, and the ONE
	// call it points at. "memory(action=search)" is spelled through the action
	// const (Rule 1b) rather than typed twice.
	subtaskPointerFmt = "Next item %s %q is starting — if it looks like something you have done before, " +
		"memory(action=" + toolnames.ToolMemoryActionSearch + ") is free and one call away."
	// subtaskPointerNoTitleFmt is the titleless fallback: the beat is still
	// named (item id) without fabricating a topic for the query.
	subtaskPointerNoTitleFmt = "Next item %s is starting — if it looks like something you have done before, " +
		"memory(action=" + toolnames.ToolMemoryActionSearch + ") is free and one call away."
)

// nextIncompletePointer implements the subtask-beat POINTER (brief:
// briefings/brief-memory-prefetch-at-subtask-beats.md): name the next move at
// the moment the model decides it, instead of injecting search results it did
// not ask for. It is PROSE over state the harness already tracks — no query
// runs, no hits are injected, nothing is prefetched (the tool schema's own
// rule stands: value-gated retrieval, not dumb RAG; unconditional retrieval
// would pay result tokens every commit for hits the model never wanted).
//
// Silent degrade, never a fabricated topic: no tracked list, an all-completed
// list, or the committed item itself as "next" all render "" and the commit
// return is left byte-identical to what it was. Order is list order (the
// model's own plan), not a re-derived one.
func (a *Agent) nextIncompletePointer(committedTaskID string) string {
	if a == nil || a.TaskState == nil {
		return ""
	}
	committed := strings.TrimSpace(committedTaskID)
	tasks := a.TaskState.view()
	for i := range tasks {
		item := tasks[i]
		item.Resolve()
		id := strings.TrimSpace(item.TaskID.String())
		if id == "" || id == committed || item.IsCompleted() {
			continue
		}
		// The title is model-authored prose: collapse every whitespace run so
		// the line stays ONE line in the tool result.
		title := strings.Join(strings.Fields(item.Summary), " ")
		if runes := []rune(title); len(runes) > subtaskPointerTitleMaxRunes {
			title = string(runes[:subtaskPointerTitleMaxRunes]) + subtaskPointerEllipsis
		}
		if title == "" {
			return fmt.Sprintf(subtaskPointerNoTitleFmt, id)
		}
		return fmt.Sprintf(subtaskPointerFmt, id, title)
	}
	return ""
}

// commitTaskItem implements task(action=commit): records the item's stable
// conclusion into the append-only, session-scoped working area (was the
// write_task_area tool; now a `task` action — tool-family merge, task family)
// and signals that the item's working frame is committed (may be trimmed next
// round; the conclusion is carried forward as authoritative context). The tool
// schema requires task_id + status="completed" + conclusion (validateToolArgs
// enforces before dispatch).
func (a *Agent) commitTaskItem(args map[string]interface{}) string {
	taskID, _ := args["task_id"].(string)
	status, _ := args["status"].(string)
	conclusion, _ := args["conclusion"].(string)
	analysis, _ := args["compressed_analysis"].(string)
	// The budget GATE, first (option A). Past the escalation mark a commit that
	// would ADD a further subtask is refused BEFORE anything is written: the
	// refusal states the two exits and the call is NOT recorded. Placed ahead of
	// the write so a refused call leaves no half-committed state, and after the
	// args are read so the (unused-on-this-path) values cost nothing.
	if refusal, refused := a.sessionBudgetRefusal(); refused {
		return refusal
	}
	result, err := tools.WriteTaskArea(tools.WriteTaskAreaOptions{
		SessionID:          a.sessionIDForTasks(),
		TaskID:             taskID,
		Status:             status,
		Conclusion:         conclusion,
		CompressedAnalysis: analysis,
	})
	if err != nil {
		return fmt.Sprintf("task(action=commit) error: %v", err)
	}
	// Framework commit signal: latch that this session has an active
	// working area (used by the carry-forward + trim hook on the next
	// round; the commit is then recorded as authoritative context and the
	// committed working region is dropped).
	a.taskAreaActive = true
	a.taskAreaDirty = true
	// P3: every commit return reports elapsed/budget (and, past the escalation
	// mark, names the one remaining move). The measurement is here because the
	// commit is the beat where the model decides whether to keep going — the
	// budget is useless as a planning signal if it only reaches the model when
	// the run is already over. Budget 0 (unlimited) and a nil clock both yield
	// "" and leave the return value exactly as it was.
	if line := a.sessionBudgetLine(); line != "" {
		result += " " + line
	}
	// Subtask-beat pointer (brief: brief-memory-prefetch-at-subtask-beats). The
	// commit is the beat where the model picks the NEXT item; if that item may
	// be something it already solved, saying so here is free and a query is one
	// call away. Appended AFTER the existing budget line so the two coexist
	// without reordering, and omitted entirely (no trailing space, no empty
	// clause) when there is no next incomplete item.
	if pointer := a.nextIncompletePointer(taskID); pointer != "" {
		result += " " + pointer
	}
	return result
}

// taskReviewMenuFor maps a task(action=review, verdict=A..E) to the
// deterministic, verdict-driven ACTION the harness tells the model to execute,
// plus the verdict semantics. The verdict DRIVES the task list not by the
// harness silently rewriting it (the list is model-owned, emitted via
// [TASK_LIST] blocks; the harness never mutates TaskState from a verdict —
// the model re-emits) but by returning the concrete action the model must
// take, and by the harness RECORDING the verdict (validate → latch → routing)
// so it is no longer the void slot the old text injection was.
func taskReviewMenuFor(verdict, taskID, reason string) string {
	note := ""
	if reason != "" {
		note = " Reason: " + reason
	}
	if taskID != "" {
		taskID = " (task " + taskID + ")"
	}
	switch verdict {
	case "A":
		return "VERDICT A:ON-TRACK — aligned; continue, no task-list change." + note
	case "B":
		return "VERDICT B:RE-ALIGN — the goal was mis-read. Re-read the epic, then re-emit an updated [TASK_LIST_BEGIN] roadmap for the remaining work, and continue." + note
	case "C":
		return "VERDICT C:AMEND-ITEM" + taskID + " — commit a CORRECTION: task(action=commit, task_id, status=completed, conclusion=\"CORRECTION: ...\") then task(action=review)." + note
	case "D":
		return "VERDICT D:REDO-ITEM" + taskID + " — that item's direction was wrong. Append a SUPERSEDES conclusion on the SAME task_id (task(action=commit, task_id, conclusion=\"SUPERSEDES: ...\")), re-emit it as progressing, and redo it." + note
	case "E":
		return "VERDICT E:PATH-DEAD — this whole path is a dead end. Record the negative finding as a conclusion (task(action=commit)), then plan an alternative path, re-emit the roadmap, and continue from the CURRENT point (completed work stays as trial evidence — never erased)." + note
	}
	return "VERDICT " + verdict + " (unknown)"
}

// handleTaskReview implements task(action=review): the POST-COMMIT CHECKPOINT as
// a structured TOOL CALL — the tool-family-merge successor to the removed
// injectPostCommitReview text injection (nick 2026-09-10 design). The model
// calls it right after each task(action=commit) (drilled by the system TASK
// MANAGEMENT RULES); when the committed item is the LAST one, this review
// doubles as the whole-epic closing review.
//
// Why structured instead of text: the old injected text asked the model to
// free-text a REVIEW-VERDICT: reply that the harness NEVER parsed — a void
// slot that neither drove the task list nor left a machine-visible trace.
// Here the verdict arrives as a tool ARGUMENT, is validated (A-E), recorded
// (a.lastTaskReviewVerdict latch + the closure flags), and has the loop fire
// its verdict-driven action and whole-epic closing verdict handling — the
// harness actually consumes the decision it was handed.
func (a *Agent) handleTaskReview(args map[string]interface{}) string {
	verdict, _ := args["verdict"].(string)
	verdict = strings.ToUpper(strings.TrimSpace(verdict))
	taskID, _ := args["task_id"].(string)
	reason, _ := args["reason"].(string)
	switch verdict {
	case "A", "B", "C", "D", "E":
	default:
		return fmt.Sprintf("task action=review: invalid verdict %q (want A-E)", verdict)
	}
	// Record the verdict + drive the review receipt into the working area so
	// it is machine-visible: latched on the Agent (queries/audit + the loop
	// completion path can read it) and stored as a REVIEW-VERDICT row.
	a.lastTaskReviewVerdict = verdict
	if a.Debug && a.Logger != nil {
		a.Logger.Printf("  task(action=review): verdict %s recorded", verdict)
	}

	// CLOSING detection: if every tracked item is completed/absent, this
	// review is the WHOLE-EPIC closing review — tell the model it may finish.
	closing := ""
	if a.TaskState != nil {
		if view := a.TaskState.view(); len(view) == 0 || (CountPending(view)+CountProgressing(view) == 0) {
			closing = "\n[CLOSING REVIEW] every tracked item is committed — this is the WHOLE-EPIC final review. If aligned+complete, close the task (emit the completed [TASK_LIST_BEGIN] and the final summary)."
		}
	}
	return taskReviewMenuFor(verdict, taskID, reason) + closing
}

// taskAreaCarryForward returns the session's committed conclusions rendered as
// one authoritative string (empty when none). Public read side used by resume
// / reload paths that want to re-inject recorded conclusions into a fresh
// context (cross-RUN carry-forward — the POC's "next round" across process
// boundaries). Same render as injectTaskAreaConclusions so both paths agree.
func taskAreaCarryForward(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	concs, err := tools.SessionConclusions(sessionID)
	if err != nil || len(concs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(taskAreaConclusionsMessageMarker)
	for _, c := range concs {
		b.WriteString("\n- ")
		b.WriteString(c.TaskID)
		b.WriteString(": ")
		b.WriteString(c.Conclusion)
	}
	return b.String()
}

// screenCaptureErrorPrefix is the screen_capture arm's own error contract
// prefix ("screen_capture error"). It is a named constant, not an inline
// literal, because TWO sites need it and they must agree: the dispatcher emits
// it, and toolErrorMarkers lists it (Rule 1b/13).
//
// It sits at the END of the file — after every line-anchored symbol — on
// purpose: devdoc/hippocampus.md and devdoc/agent-five-organs-ontology.md cite
// this file by line (`loop.go:NNN`, 870 across 166 documents), so appending
// keeps those citations where they were. HISTORY: the anchor gate that once
// asserted them went with internal/docclass in 5a35f61 — churn cost now, not a
// red check; the live anchor check is the config census over the CONFIG.md rows.
const screenCaptureErrorPrefix = toolnames.ToolScreenCapture + " error"

// screenCaptureEngine is the capture engine the screen_capture dispatch arm
// calls. Production binds the one engine BOTH faces share — tools.ScreenCapture,
// the same call the `gopi tool screen_capture` CLI verb makes (cmd/agent/
// cli_screencapture.go) — so the model path and the human path can never drift.
// It is a seam var (the toolname→engine wiring is what this arm adds): a test
// can substitute a stand-in to pin the argument plumbing and the error contract
// without an X display (Rule 11's real-turn check covers the live engine).
var screenCaptureEngine = tools.ScreenCapture

// screenCaptureModeEngine is the seam for the WINDOW/AREA capture modes — the
// sibling of screenCaptureEngine, which stays bound to the plain whole-screen
// call so its binding contract (TestScreenCaptureDispatch_EngineIsTheCLISharedOne)
// keeps asserting exactly what it always did: the model's default capture is
// the same call the CLI makes. This one carries the mode-aware options, and
// production binds it to tools.ScreenCaptureWith — again the very function the
// `gopi tool capture` verb calls, so the model path and the human path are one
// engine in every mode.
var screenCaptureModeEngine = tools.ScreenCaptureWith

// ---------------------------------------------------------------------------
// The prompt's identity block, declared as NAMED CONSTANTS (Rule 1/13/14)
// ---------------------------------------------------------------------------
//
// These live at the END of this file ON PURPOSE, and not beside the registry
// that references them. Several landed devdocs cite `internal/agent/loop.go` by
// BARE LINE NUMBER for symbols in the middle of this file. Inserting the
// declarations where they read best — next to promptSections, their only
// reader — moved six of those citations off their symbols. Declarations added
// AFTER the file's last cited line move none of them (the gate that once
// asserted them, the devdoc line-anchor gate, went with internal/docclass
// in 5a35f61 — churn now, not a red check; see the end of this file). The price
// is that a reader meets the value here rather than at its use, which the
// rule-1 comments below make explicit. Rule 13 is satisfied either way: the
// registry references these by NAME, not by a typed-in literal.

// headerSectionName is the registry Name of the prompt's FIRST block — the one
// the model reads before anything else, and the block whose ordering the
// self-knowledge section must follow (promptSections, Order 0).
//
// It is a constant because a registry Name is a VALUE two places must agree on:
// the entry in promptSections and any test that locates the header by name.
// A bare "header" literal at either site is the mirror Rule 13 forbids.
const headerSectionName = "header"

// headerRules is the prompt's first block, byte for byte as it has been: the
// identity sentence, then the two contracts the model starts from.
//
// IT CARRIES NO REPLY-LANGUAGE SENTENCE OF ITS OWN. The operator's
// output-language directive is prepended to the WHOLE assembly by
// systemPromptWithOutputLanguage, which is what puts the language rule above
// every English block that follows it; and this prompt has a hard density
// budget (TestSystemPromptDensity, a ~4.5K-character target that the assembly
// sits within a few characters of), so stating a rule twice is not available
// even if it were desirable.
//
// Keeping the header STATIC is also what makes the PREPEND contract assertable
// byte for byte: the body a configured prompt carries after the directive is
// the same bytes the unconfigured prompt carries alone, so the placement can be
// proven by suffix instead of by re-typing a sentence that lives outside the
// contract being protected (the failure this declaration repairs — see
// devdoc/output-language-knob.md and the task report).
const headerRules = "You are GoPi. Use tool_use blocks; a tool's description says when to use it."

// TaskSystemPromptHead returns the opening of the TASK assembly's header section
// (headerRules above) — the marker that tells the two prompts apart. It is
// exported so a caller OUTSIDE this package can assert the task prompt did NOT
// arrive without re-typing its wording (Rule 13/14: one definition, referenced).
// The chat assemblies never carry it: they compose from chatSystemPrompt and the
// chat-context block, and the registry that owns headerRules is the task path's.
func TaskSystemPromptHead() string { return headerRules }

// doc-anchor history, recorded HERE because this is the only place in loop.go
// where a comment cannot move a line it talks about.
//
// The identity block above says its declarations sit at the end of the file
// because several landed devdocs cite symbols in the middle of loop.go by BARE
// LINE NUMBER. That drift used to be ENFORCED: the deleted doc-anchor gate
// resolved each `loop.go:NNN` anchor and failed when one landed off its symbol
// or on a blank line, and it was deleted — whole, with its baseline — together
// with internal/docclass in 5a35f61. Nothing resolves a `loop.go:NNN` anchor now,
// so the argument above is a churn cost (re-keying 870 citations across 166
// documents by hand), not a red gate.
//
// What used to be the check to satisfy instead was the config-anchor census over
// CONFIG.md's rows (`config-anchor-census.go --check`, TestMergeConfigAnchorGate*
// arms in internal/tools) — and it never looked at loop.go either. It was
// deleted with the documentation-citation family in 78bdf05a (2026-11-05), so
// the only surviving generator check in that family is
// `go run internal/standards/testdata/commands-census.go . COMMANDS.md --check`,
// which pins the generated command index to its source.
