package main

// chat_session_budget.go — the CHAT SURFACE's default for the session budget.
//
// THE DEFECT THIS CLOSES (measured 2026-10-07, task
// 01a115c4-1ef0-7551-9960-9a5c34f3ed84). The tool-enabled chat path
// (`Agent.ChatOps`) sends the TASK system prompt — the same assembly a
// dispatched worker gets — and that prompt's [TASK MANAGEMENT RULES] rule 5
// renders the session budget:
//
//	BUDGET: session budget 60min — a task that cannot finish inside it is TOO
//	BIG by definition; running out is a planning failure, not a graceful
//	ending; past 75% split it (task(action=reframe, verdict=TOO_BIG)).
//
// On a worker that number is a PLANNING CEILING with a real referent. On a chat
// it is a FICTION: there is no epic to split, no dispatcher waiting, no clock
// the operator agreed to — the same prompt's own [CHAT SURFACE] block says a
// turn is "conversation, not a task run (no task list, no epic framing)", and
// the run is bounded instead by the ENGINE's own wall-clock cap
// (GOPI_SESSION_MAX_WALLCLOCK, default 8h — a different knob with a different
// job). The two blocks contradict each other, and the budget wins: measured on
// 2026-10-07 the PA chat reached t+5527s and used "92× over the budget" to
// impose a limit on ITSELF and propose ending early. A fabricated deadline made
// a chat quit. The operator's ruling: "你是 chat，根本不是普通的 worker 不存在
// 超时的问题".
//
// THE FIX: PUBLISH A DEFAULT, DO NOT OVERRIDE A DECISION. A chat run that finds
// the budget knob UNSET writes "0" (= unlimited) into its own environment, from
// which the workflow's one reader inherits it. 0 is already a first-class value
// end to end — internal/agent's sessionBudget() maps it to unlimited, rule 5
// renders "BUDGET: unlimited — no planning ceiling; still split anything that
// cannot finish in one sitting.", and the tick line drops its budget clause
// rather than inventing a "0min" denominator — so this file adds a DEFAULT, not
// a mechanism.
//
// WHAT IT DELIBERATELY DOES NOT DO:
//
//   - It never overwrites a value the operator (or a caller, or a test) set.
//     An explicitly exported GOPI_SESSION_BUDGET — "20", or anything else — is
//     the operator's own decision about THEIR surface and this run honours it
//     verbatim. That is the difference between a default and an enforcement.
//   - It touches nothing off the chat path. `gopi worker`, a bare prompt run
//     and `gopi chat` are separate entries into the engine, and a worker's
//     60-minute ceiling is the number the prompt was written for (Rule 28: the
//     system layer's worker semantics are not this file's to change).
//   - It adds no knob. There is no --session-budget flag and no settings key:
//     the chat default is a property of BEING a chat, and the operator who
//     wants a specific budget on their surface already has the env channel
//     this file only fills in.
//
// WHY THE ENVIRONMENT AND NOT A RENDER-TIME CHECK IN internal/agent (Rule 35).
// The alternative was to teach the prompt assembly "am I a chat?" and have it
// render 0 there. That was rejected for three measured reasons:
//
//   1. There is no such flag to read. The chat prompt is built from the SAME
//      task assembly (assembleSystemPrompt) on the ChatOps path, and at the
//      moment rule 5 is rendered the group is not distinguishable — the chat
//      blocks are APPENDED to that assembly by a different layer than the one
//      that renders it, so "is this a chat?" would have to become a new
//      process-wide signal, i.e. a knob where the fix needs a fact.
//   2. It would be a SECOND source of truth for a resolved value. Rule 2 puts
//      the budget's single reader in internal/agent (sessionBudget) and this
//      file would have to disagree with it at render time — exactly the drift
//      the one-reader rule exists to prevent.
//   3. The env channel is the operator's VISIBLE one. With the default
//      published into the environment, `gopi session stats`/a child process/a
//      `gopi chat` the agent spawns all read the SAME value the prompt was
//      rendered from, and an operator can see the chat's budget with `env`
//      rather than reasoning about which code path rendered it.
//
// The full argument (including why the two "missing channels" of the
// three-channel story are NOT built) is devdoc/chat-unlimited-session-budget.md.

import (
	"fmt"
	"os"
	"strings"
)

// chatSessionBudgetEnv is the budget knob's environment name as the CHAT path
// needs it. It is NOT a second definition of the knob (Rule 13/14): the reader
// that owns the name is internal/agent's sessionBudgetEnv, and cmd/agent is
// `package main` — it cannot import that unexported constant, which is the same
// constraint that put the chat-surface identity name in internal/config
// (ChatSurfaceIdentityEnv). The two spellings are pinned to be equal by
// TestChatBudgetPublishesTheBudgetsOwnEnvName, so a rename in internal/agent
// fails here rather than silently unsetting the default.
const chatSessionBudgetEnv = "GOPI_SESSION_BUDGET"

// chatSessionBudgetUnlimited is the value this path publishes: "0" = unlimited,
// the spelling internal/agent's sessionBudget() documents for it. It is a named
// constant (Rule 1b) because it is a value two layers must agree on — this
// writer and that reader — and a bare "0" here would be a second opinion about
// what unlimited is spelled like.
const chatSessionBudgetUnlimited = "0"

// chatSessionBudgetDefaultNotice is the ONE line that tells the operator the
// default was applied. It names the VARIABLE, the VALUE and the reason, because
// the alternative — setting it silently — leaves the single most surprising
// fact about this run ("why does my chat say unlimited?") unanswerable from the
// run's own output (Rule 15).
//
// The SURFACE is a parameter: a chat and a web console both publish the same
// default, and the one thing that must differ is WHICH surface announced it — an
// operator reading `[gopi] web:` knows it was the console they started. Nothing
// else in the sentence is per-surface, so the sentence stays ONE string (Rule 1b:
// a second, near-identical format is how two faces drift).
const chatSessionBudgetDefaultNotice = "%s%s unset — publishing %s=%s (unlimited) for this run; this surface has no planning ceiling (export %s=N to set one; the engine's own wall-clock cap is a separate knob)\n"

// chatSessionBudgetFailedFormat is the other half of the seam and it is not
// optional (Rule 15): if the publication fails, the run keeps the 60-minute
// default the defect was about, and an operator who was never told has no way
// to know why their surface still teaches a ceiling. The sentence names the
// variable, the value that was refused and the error, so the failure is
// diagnosable from the run's own output.
const chatSessionBudgetFailedFormat = "%scannot publish the default session budget (%s=%s): %v — this run keeps the %d-minute default planning ceiling, and its system prompt will teach it\n"

// sessionBudgetSurfaceChat / sessionBudgetSurfaceWeb are the two START-UP
// prefixes the sentence above opens with — the same `[gopi] <face>: ` shape the
// rest of these notices use (chat_notice_sink.go, chat_session_slots.go), so a
// reader of a mixed log can tell which surface spoke. They are surfaces, not
// hard-coded prose inside the format, because the format is ONE string for both
// (Rule 1b: a second, near-identical format is how two faces drift).
const (
	sessionBudgetSurfaceChat = "[gopi] chat: "
	sessionBudgetSurfaceWeb  = "[gopi] web: "
)

// chatSessionBudgetDefault is the task-side default the notice above contrasts
// against. It is spelled here for the SENTENCE only (the value itself belongs to
// internal/agent and is pinned by TestChatBudgetPublishesTheBudgetsOwnEnvName);
// naming it in the operator's warning is what makes "your chat will keep the 60
// minute ceiling" a statement rather than a hint.
const chatSessionBudgetDefault = 60

// publishChatSessionBudgetDefault gives THIS chat run unlimited by default.
//
// It is called at the chat run's LAUNCH POINT — `runChatWithInput`, after the
// command line is resolved and before the agent (and therefore the system
// prompt) exists — so that every carrier of the chat surface is covered by ONE
// call: the interactive REPL, `gopi chat --once`, the scripted sequence face and
// the driver all reach their turns through that function, and none of them has
// to remember this. `gopi chat -h` returns from the argv walk ABOVE the call
// site, so a usage query stays what it was: a page, no environment change.
//
// The rule is one line of policy: UNSET means "nobody decided", and the chat
// decides unlimited; SET (including a deliberately empty export? no — an empty
// value is indistinguishable from unset in the environment's own vocabulary, so
// it is treated as unset, exactly as internal/agent's sessionBudget() reads it)
// means "someone decided", and this function does not touch it.
//
// THE SECOND PARAMETER IS THE RUN'S SYSTEM VOICE, handed down from the ONE
// capability resolution in runChat (termToolStylesFor(chatCap.Colour())). This
// file NEVER resolves a capability of its own: the defect this signature exists
// to prevent is the SECOND capability decision (task 01a10e2b — three prints for
// one decision), and a writer that asked "do I have colour?" here would be
// exactly that. With a monochrome table the rows draw as their text and the
// emitted bytes are the historic Fprintf's, byte for byte.
func publishChatSessionBudgetDefault(st termToolStyles) {
	publishSessionBudgetDefault(sessionBudgetSurfaceChat, st)
}

// publishWebSessionBudgetDefault gives THIS `gopi web` console the same default.
//
// THE DEFECT THIS CLOSES (measured 2026-11-07, task 01a1b498). `gopi web` sends
// every turn through Agent.ChatOpsOnCarrier — the SAME task prompt assembly a
// dispatched worker gets — and never called the publication above, which is
// reached only from `runChatWithInput`. So a web console inherited
// internal/agent's 60-minute TASK default and taught the model a ceiling it has
// no place to spend: the operator's live PA console received "session budget:
// 55m28s elapsed of 1h0m0s (92% used)" and was told to land the work or declare
// TOO_BIG, at the 55th minute of an unbounded human conversation.
//
// The rule is the chat's, unchanged: UNSET means "nobody decided" and a
// conversational surface decides UNLIMITED; SET means "someone decided" and this
// function does not touch it. `gopi worker` and a bare prompt run keep the
// 60-minute planning ceiling they were written for (Rule 28) — they do not call
// this and nothing here reaches them.
//
// WHY IT IS A SECOND ENTRY POINT AND NOT A CALL FROM THE CHAT ONE. The two
// surfaces have different launch points and neither may reach the other's:
// `runChatWithInput` is the chat REPL, `runWeb` is a long-lived HTTP server, and
// a call from one into the other would be the second capability decision this
// mechanism exists to avoid. What they SHARE is everything that matters — the
// environment name, unlimited's spelling, the never-override rule, the failure
// line — and that half lives once, in publishSessionBudgetDefault below.
func publishWebSessionBudgetDefault(st termToolStyles) {
	publishSessionBudgetDefault(sessionBudgetSurfaceWeb, st)
}

// publishSessionBudgetDefault is the ONE implementation both surfaces call: the
// presence test, the publication, the failure report and the notice. surface is
// the start-up prefix the notice opens with ([gopi] chat: / [gopi] web:), so the
// operator can tell which face applied the default; every other byte is shared,
// because every other byte is the same policy.
func publishSessionBudgetDefault(surface string, st termToolStyles) {
	if strings.TrimSpace(os.Getenv(chatSessionBudgetEnv)) != "" {
		// A decided value — the operator's, a caller's, or a test's. It is
		// honoured verbatim and NOTHING is printed: there is no default to
		// report and no channel left to fill.
		return
	}
	if err := os.Setenv(chatSessionBudgetEnv, chatSessionBudgetUnlimited); err != nil {
		// Rule 15: the publication failed, so the run does NOT get the default.
		// Say so — the alternative is a silent 60-minute ceiling on a surface
		// that has no place to spend it. This is a FAILURE line and stays plain
		// on purpose: it is not the surface talking about itself, it is the run
		// reporting that it could not do what it promised.
		fmt.Fprintf(os.Stderr, chatSessionBudgetFailedFormat,
			surface, chatSessionBudgetEnv, chatSessionBudgetUnlimited, err, chatSessionBudgetDefault)
		return
	}
	termWriteRows(os.Stderr, termNoteRows(fmt.Sprintf(chatSessionBudgetDefaultNotice,
		surface, chatSessionBudgetEnv, chatSessionBudgetEnv, chatSessionBudgetUnlimited, chatSessionBudgetEnv), st))
}
