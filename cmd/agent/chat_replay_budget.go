package main

// chat_replay_budget.go — THE REPLAY'S OWN INJECTION BUDGET CHANNEL.
//
// WHAT THIS EXISTS FOR, and it is a measured deadlock rather than a precaution.
// A replay types every recorded operator turn through the SAME key injector the
// production editor installs (chat_replay.go's injectLine), and until this file
// existed it charged them against `agent.ChatSurfaceKeyCeiling()` — the ceiling
// built for the MODEL's self-driven loop (`chat_surface` action=key), whose whole
// purpose is to bound how many keystrokes a bounded party (the model) may spend
// inside ONE period. A deterministic replay is not that party: its turn count is
// the length of a file, and a recording longer than that ceiling is ordinary, not
// pathological.
//
// Measured (2026-10-04, session 0cd5d1b7-438b-4fac-98d0-f573ef0a6250, 20 turns,
// 65 injection units): the 65th unit — the closing `/quit` — was REFUSED with
//
//	[gopi replay]  injecting "/quit" was refused: this period's key ceiling is
//	reached (64 of 64 keys injected) — it does not raise itself, and only a line
//	the OPERATOR submits opens a fresh period
//
// and the inner `gopi chat` then stayed at its prompt, with the film driver, the
// Xvfb and the xterm all still alive: the driver waited forever for a completion
// marker that only that REPL's own exit writes.
//
// WHY NOT JUST RAISE THE 64. Because that number is the self-driven loop's bound
// and it protects the MODEL: raising it would hand the model a larger keystroke
// budget toward the same unbounded end. The two are different quantities — a
// deterministic script's length versus a model's allowance — so they get
// different budgets (Rule 13/14: one definition each, neither mirroring the
// other). The chat_surface ceiling is untouched by this file, and its own tests
// still pin it.
//
// WHAT THE BUDGET IS SIZED FROM. The recording itself: the exact number of
// injection units the driver will spend (each recorded turn's rune-safe chunks
// plus its Return, plus the closing quit line), derived through the SAME chunker
// the injection uses (chatReplayChunks) rather than a second estimate of it.
// Sizing from the script is what makes the budget a property of the WORK rather
// than a number somebody guessed.

import (
	"sync"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/replaydrive"
)

const (
	// chatReplayInjectSlack bounds the units the budget does not have to account
	// for a reason: the driver's own prose cannot inject, but a future seam that
	// types one extra line must not turn a legal recording into a hang. It is
	// added on top of the derived script length.
	chatReplayInjectSlack = 64
	// chatReplayInjectPerUnit is the headroom granted per injection unit of the
	// script. One is what the script needs; a session JSONL records text that the
	// driver re-types through the same chunker, so the derivation is exact for
	// the recording as it stands — this factor covers a recording whose text was
	// re-derived between the parse and the typing (never observed, cheap).
	chatReplayInjectPerUnit = 2
)

// chatReplayInjectBoundVal is this process's own injection ceiling, or ZERO when
// none has been recorded — and zero-means-unset is load-bearing, not a shorthand:
// a reader that latched `agent.ChatSurfaceKeyCeiling()` the first time anyone
// asked would FREEZE the loop's 64 against a replay whose budget is recorded a
// moment later, which is the measured defect this whole file exists to remove,
// reintroduced through the back door. (Measured while developing this file: a
// test that read the ceiling before recording its recording got 64 for the rest
// of the process, and reproduced the refusal exactly.)
var (
	chatReplayInjectBoundMu  sync.Mutex
	chatReplayInjectBoundVal int
)

// chatReplayInjectCeiling reports the injection ceiling in force for THIS
// process: the replay's own derived budget when a replay was named, and the
// self-driven loop's declared ceiling otherwise.
//
// IT IS THE ONE READER, and it has THREE consumers on purpose (Rule 14): every
// injector this binary constructs (lineedit_wire.go), the replay driver's own
// injection calls, and the period the driver opens. A run with no replay
// therefore behaves EXACTLY as before — the unrecorded default IS
// `agent.ChatSurfaceKeyCeiling()` — which is what keeps the self-driven loop's
// bound and its tests untouched. Reading it RECORDS NOTHING.
func chatReplayInjectCeiling() int {
	chatReplayInjectBoundMu.Lock()
	defer chatReplayInjectBoundMu.Unlock()
	if chatReplayInjectBoundVal <= 0 {
		return agent.ChatSurfaceKeyCeiling()
	}
	return chatReplayInjectBoundVal
}

// chatReplayInjectPlan derives the script's own injection arithmetic: how many
// units the driver will spend, over how many turns, from how many bytes of
// recorded text. It is REPORTED (the run says what it sized its budget from), and
// it is the number the budget is built on.
//
// The units are counted through `chatReplayChunks` — the chunker the injection
// itself uses — so the plan and the typing cannot disagree about what one unit
// is. Each turn contributes its chunks plus ONE Return, and the closing quit line
// contributes its own pair; that is the whole of what `run` injects, and leaving
// the exit line out is exactly how a budget would come out one unit short of the
// unit that hung the measured run.
func chatReplayInjectPlan(rec *replaydrive.Recording) (units, turns, bytes int) {
	if rec == nil {
		return 0, 0, 0
	}
	for _, turn := range rec.Turns {
		turns++
		bytes += len(turn.Text)
		units += len(chatReplayChunks(turn.Text)) + 1
	}
	// The exit line is the operator's own vocabulary, typed the same way as any
	// turn — and it is the unit the measured refusal landed on.
	quit := completionSlashPrefix + quitCommandName
	units += len(chatReplayChunks(quit)) + 1
	return units, turns, bytes
}

// chatReplayInjectBoundFor is the budget ONE recording needs, computed WITHOUT
// touching the process's latch. The arithmetic is separated from the latch
// because the latch is deliberately process-wide (a run has at most one
// `--replay`, and two latched values would be two answers to one question), while
// a test must be able to price several recordings inside one process.
func chatReplayInjectBoundFor(rec *replaydrive.Recording) int {
	need, _, _ := chatReplayInjectPlan(rec)
	bound := need * chatReplayInjectPerUnit
	// THE FLOOR, and it is what makes this channel safe to add: a recording whose
	// script is SHORTER than the model's own period must not shrink the loop's
	// bound — the only direction this number may move is up, and only for a script
	// that needs it.
	if floor := agent.ChatSurfaceKeyCeiling(); bound < floor {
		bound = floor
	}
	return bound + chatReplayInjectSlack
}

// recordChatReplayInjectBound latches this process's injection budget from the
// recording about to be re-run, and returns the ceiling in force.
//
// The latch is taken HERE rather than at the first injection for a reason that is
// ordering, not taste: the injector is CONSTRUCTED with the ceiling
// (lineedit_wire.go) before the driver's goroutine starts, so a value latched any
// later would leave the injector holding the loop's 64 while the driver charged a
// larger number — the two halves of the measured refusal disagreeing again.
func recordChatReplayInjectBound(rec *replaydrive.Recording) int {
	bound := chatReplayInjectBoundFor(rec)
	chatReplayInjectBoundMu.Lock()
	if bound > chatReplayInjectBoundVal {
		chatReplayInjectBoundVal = bound
	}
	chatReplayInjectBoundMu.Unlock()
	return chatReplayInjectCeiling()
}

// applyChatReplayInjectBudget opens the replay's own key period at the replay's
// own ceiling, and reports whether it reached the injector.
//
// ResetKeys is the ONE action that renews a period, and it is deliberately NOT a
// tool action (internal/lineedit/inject.go: "a bound the bounded party can lift
// is not a bound") — the model cannot call it. The replay process can, and must,
// because the period belongs to the SCRIPT it is executing rather than to a line
// the operator submitted. false means this run has no injection seam at all: the
// driver reports that as its own refusal, never as a quiet no-op (Rule 15).
func applyChatReplayInjectBudget(inj *lineedit.Injector) bool {
	if inj == nil {
		return false
	}
	inj.ResetKeys(chatReplayInjectCeiling())
	return true
}
