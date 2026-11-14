package main

// chat_config.go — the `/config` command: print the configuration THIS PROCESS
// is actually running with.
//
// WHY IT IS A COMMAND AND NOT ANOTHER CLI VERB (operator, 2026-11-12). The
// question "what config is in force?" is a question about a RUNNING process, and
// a shell command cannot answer it about somebody else's process without
// guessing. `gopi config show` re-reads settings.json and describes the FILE;
// what an operator needs to see is what the request builder in THIS process will
// actually send — measured the same day: the document said
// reasoning_effort="high", the process held no such variable, and every request
// went out thinking-disabled. Only code inside the process can see that gap, so
// it lives beside the other console words, and it reaches the same renderer as
// every other door (runtime_config_snapshot.go).
//
// IT PRINTS, AND IT COSTS NO MODEL TURN. The block is ~25 short lines, composed
// locally from values this process already holds: nothing is sent, and nothing
// is injected into the next turn. It is written through the notice stream
// (WriteNoticeRuns), the channel the reserved layout owns — a raw stdout write
// would tear the answer band.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
)

// The command's vocabulary (Rules 1/13/14): every word the switch, the usage
// page and the shared table compare against or print is declared once.
const (
	// chatConfigCommandName is the slash word that owns this file.
	chatConfigCommandName = "config"
	// chatConfigHelpWord answers `/config help` from chatConfigUsage — the same
	// word `commandHelpRequested` treats as help on every table command.
	chatConfigHelpWord = "help"
)

// chatConfigUsage is `/help config` and `/config help` — the ONE definition, so
// the page and the command cannot describe two different reports.
//
// IT IS DELIBERATELY SHORT. `gopi chat -h` is held to a line budget
// (TestPublishedHelpPagesStayWithinTheirUsageBudget) and this block is a
// per-command page, not part of that one — but the same discipline applies: the
// four channel names it documents ARE the constants the renderer compares
// against, so this block cannot promise a report shape the code does not
// produce.
var chatConfigUsage = `Usage: /config

Print the configuration THIS RUNNING PROCESS is in force with: one
` + "`key  value  [channel]`" + ` row per knob, then the couplings the values alone do
not state.

NOT ` + "`gopi config show`" + `: that verb re-reads settings.json and describes the
FILE. This reads what the process HOLDS — the environment as the OS delivered
it plus this run's own resolved agent fields — so it answers "what will the next
request actually carry?". The two answers can disagree, and on 2026-11-12 they
did: the document said reasoning_effort="high", the process carried no such
variable, and every request went out thinking-disabled.

CHANNELS — what decided each value:

  flag             a CLI flag on this run's own command line
  env              the process carries the variable and this run's settings
                   load did not put it there — an operator export, a parent
                   process, or a path knob this run resolved itself
  settings.json    this run loaded it from the document and published it (the
                   resolution is DIFFED before/after, so this label is measured)
  built-in default nothing named it; the code's constant runs — and the VALUE
                   cell then NAMES that constant: ` + "`(unset), 16384 runs`" + `.
                   A constant that spells "no ceiling" prints as the code
                   spells it (` + "`timeout (unset), 0 runs`" + ` — 0 = unlimited)
  agent            resolved by the agent itself (what the request will carry)
  NOT APPLIED      the document states it and this process never received it —
                   a DEAD value on a knob whose only reader is os.Getenv
  (unset)          nothing specified it anywhere, and the code has no default
  decided by KEY   another ROW of this table decides this one's value, so the
                   compiled-in default does NOT run and neither is printed.
                   ` + "`thinking`" + ` is the one such knob: an explicit, valid
                   effort that is not ` + "`off`" + ` auto-enables thinking, and the
                   ` + "`!`" + ` line names the effort that did it

A ` + "`!`" + ` line below the table states a coupling: e.g. the request builder CLEARS
reasoning_effort whenever thinking is disabled, so an effort can be present,
visible, and still absent from the wire.

Credentials print as ` + "`<set>`" + `: this report goes to a terminal and into logs.
Local and immediate — no model turn.`

// chatConfigHandleLine answers `/config` on the REPL: build the snapshot from
// THIS process and the live agent, render it, and publish it on the notice
// stream.
func chatConfigHandleLine(input *chatInput, ag *agent.Agent) {
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(ag, liveRuntimeConfigDeps(runtimeConfigKnownKeys)))
	input.WriteNoticeRuns(termNoteRows(joinRuntimeConfigLines(lines), input.tool), input.flowWriter(), true)
}

// chatConfigArg returns everything the line carried after the word itself —
// `/config help` → "help", a bare `/config` → "". The word is stripped ONCE
// here rather than at the call site, so the switch and the usage answer cannot
// disagree about where the argument starts.
func chatConfigArg(line string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), chatSlashTrigger+chatConfigCommandName))
}

// chatConfigWantsHelp reports whether the argument list asks for the usage
// block rather than for the report — `/config`, `/config help`, `/config -h`.
func chatConfigWantsHelp(arg string) bool {
	switch arg {
	case chatConfigHelpWord, chatConfigHelpFlagShort, chatConfigHelpFlagLong:
		return true
	}
	return false
}

// chatConfigHelpFlagShort / chatConfigHelpFlagLong are the flag spellings of the
// same request — the HARD help standard's two spellings, declared here so the
// switch, the usage page and the table compare against one definition.
const (
	chatConfigHelpFlagShort = "-h"
	chatConfigHelpFlagLong  = "--help"
)

// joinRuntimeConfigLines is the ONE join: the REPL arm and the shared table's
// answer assemble the same bytes from the same slice.
func joinRuntimeConfigLines(lines []string) string {
	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}
