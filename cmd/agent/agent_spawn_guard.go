package main

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/config"
)

// agent_spawn_guard.go — the two CMD-AGENT call sites of the agent-spawn guard
// (internal/config.GuardAgentSpawn owns the signal research and the decision).
//
// The verbs it protects are the operator's ruling of 2026-09-12: `gopi worker`
// and `gopi task run`. Both are effectively never-returning processes; an agent
// that starts one blocks on its own tool call, stops responding, and cannot be
// detached from it (setsid leaves the process tree/cgroup intact — the 04:12
// cohort kill proved it). The operator runs the worker himself; the agent's
// only verb on the queue is `task submit`.
//
// WHERE the hook sits matters as much as what it checks:
//
//   - `gopi worker`: in isWorkerSubcommand, AFTER the -h/--help scan. A usage
//     query has no side-effect (the P0-1 rule that function already enforces)
//     and must stay answerable from anywhere, so `gopi worker -h` is never
//     refused — only the loop start is.
//   - `gopi task run`: in runTasksSubcommand, BEFORE parsing/dispatch, so a
//     refusal costs nothing and claims nothing. Same rule: the -h/--help scan
//     above it returns first, and read-only verbs (`task list`, `task show`)
//     are untouched at any point.
//
//	⑤ another process on the host that writes the worker's state files
//	   directly — gopi's queue is filesystem state, and the guard works at the
//	   VERB level, so any tool that bypasses the verb bypasses the guard.
//	   Degradation is real and visible (`--count` shows workers the operator
//	   did not launch, and §5.4's log is where their exit reason lands).
//
// The guard is in the decision file plus one call site per verb, and it
// produced no false positive on the operator's own terminal: his launcher runs
// `setsid nohup gopi worker` from a shell whose environment carries no agent
// signature, and the anchor marks `task run`'s execution child as its own —
// both are asserted in §4.1's tests.
//
// A final honest note on WHY these signals and not others: every rejected
// candidate (tty, parent name, cgroup, the task-child ids) was rejected from a
// MEASUREMENT on this host, not from taste — see devdoc §2.3. A signal that
// looks rigorous on paper but refuses the operator is worse than a narrow one
// that provably does not.
//
// The guard reads the ENVIRONMENT + procfs only — it never inspects, signals or
// waits on another process, so a refusal is instantaneous and cannot itself
// stall the caller (the failure mode the ruling exists to prevent).

// guardVerbOrExit refuses to start `verb` when this process sits inside an
// agent session, and prints a one-line warning when an explicit override (or
// the wholesale guard-off env) let it through — an override must never be
// silent.
func guardVerbOrExit(verb string) {
	d := config.GuardAgentSpawn(verb, config.AgentSpawnSignatureNow())
	if !d.Allowed {
		config.RefuseAgentSpawn(verb, d)
		return // RefuseAgentSpawn exits; kept for the reader's sake.
	}
	if d.Override {
		fmt.Fprintf(os.Stderr,
			"[gopi] %s: agent-spawn guard bypassed (%s or %s is set) — the operator ruling of 2026-09-12 forbids this from an agent\n",
			verb, config.AgentSpawnGuardOverrideEnv, config.AgentSpawnGuardOffEnv)
	}
}

// guardBarePromptOrExit is this file's THIRD guarded surface: the bare-prompt
// fallback (internal/config.GuardBarePrompt owns the decision). It is a
// different defect from the two never-returning verbs above — an agent that
// launches THIS one does not stall, it MULTIPLIES (agent → gopi <word> → a real
// paid run → agent → …), registering an `[auto] <word>` row and a clone ground
// per layer. Measured on this host (row 01a1ac14, 2026-11-05): two cascaded
// ghosts cost 440,153 + 855,807 prompt tokens.
//
// WHERE IT SITS: cmd/agent/main.go's run path, immediately after the two
// positional refusals (§1 and §1b, which answer words this binary RECOGNISES)
// and BEFORE readInput — so a refusal costs nothing: no session directory, no
// ledger row, no ground, no model call.
//
// THE OVERRIDE WARNING IS NOT PRINTED HERE, and that asymmetry is deliberate:
// the two verbs above print it because their override is a rule violation worth
// shouting about. Here the same env vars ALSO cover the ordinary human
// `gopi "summarize this"` invocation, so a warning on this face would fire on
// every interactive run under `GOPI_ALLOW_AGENT_SPAWN=1` and become noise
// nobody reads (Rule 15's "no silent failure" is about a DROPPED fact, not
// about repeating a knob the operator just typed).
func guardBarePromptOrExit(word string) {
	d := config.GuardBarePrompt(word, config.AgentSpawnSignatureNow())
	if !d.Allowed {
		config.RefuseBarePrompt(d)
		return // RefuseBarePrompt exits; kept for the reader's sake.
	}
}

// exportAgentSessionAnchor marks THIS process as a gopi agent session for the
// guard: the value is our own pid, so a child that inherits it knows it is a
// descendant and a re-exec'd agent knows the marker is its own (Rule 2: this is
// the single writer; internal/config.AgentSpawnSignatureNow is the reader).
//
// It is called from the ONE place a gopi process becomes an agent session
// (printLogPaths, next to the TaskExecutorSessionEnv export), so no other path
// can half-establish the marker.
func exportAgentSessionAnchor() {
	if err := os.Setenv(config.AgentSessionAncestorEnv, fmt.Sprint(os.Getpid())); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] agent session anchor export: %v\n", err)
	}
}
