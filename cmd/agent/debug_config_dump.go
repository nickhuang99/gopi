package main

// debug_config_dump.go — the START-UP half of the configuration-in-force
// surface: with the switch armed, a run writes the SAME block `/config` renders
// into its own record, BEFORE it sends its first model request.
//
// WHY IT EXISTS. `gopi /config` answers "what is this process in force with?"
// in front of a human. A DISPATCHED run has no human: its evidence is
// <session-dir>/output.log, read later, and the question that log must answer is
// "which values did THIS run send". A settings document can state a knob and the
// process can still hold nothing for it — the document is then right and the run
// is not, and no line of the run's own record shows the difference. `/config`
// shows it in one line, but nobody is at that prompt.
//
// WHERE IT IS WRITTEN, AND WHY THAT IS THE ONLY HONEST PLACE. The block is
// rendered in the process that OWNS the resolution and the agent: the run
// child. The spawn seam (runTaskRunOnce) is what GIVES a run its record — it
// opens <session-dir>/output.log (tools.TaskLogPath) and points the child's
// stderr at it (taskChildStreamWriters) — so a block the child writes to stderr
// lands in exactly that file. The seam itself cannot render this block: `gopi
// task run` is dispatched from main()'s subcommand chain and returns BEFORE
// resolveRunConfig ever runs, so the wrapper holds no resolved configuration,
// carries no agent, and would print "NOT APPLIED" for every settings-fed knob —
// i.e. it would describe a process that sends no request, which is the opposite
// of the question this surface answers.
//
// ORDERING IS THE REQUIREMENT: it runs after the agent exists (so the `agent.*`
// rows are the ones the request will actually consume) and before RunPrompt (so
// the record carries the configuration of the request that follows it, not of
// one already on the wire).

import (
	"fmt"
	"io"
	"os"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
)

// debugConfigDumpSep is the ONE line printed before and after the block, so a
// reader — and a test — can tell where the table starts and ends inside a log
// that also carries the run's prose. It deliberately does NOT repeat the
// renderer's title: a second occurrence of that phrase would make
// `grep -A40 "configuration in force"` match this divider instead of the block.
const debugConfigDumpSep = "-----"

// writeDebugConfigDump writes the configuration-in-force block to w when the
// switch is armed, and writes nothing at all when it is not — the default
// path's bytes are unchanged, which is the whole point of an opt-in record.
//
// ag may be nil: the environment half still answers (that is what the web
// carrier's table does too), so a caller without an agent is not a reason to
// refuse the dump.
func writeDebugConfigDump(w io.Writer, ag *agent.Agent) {
	if !config.DebugConfigDumpEnabled() {
		return
	}
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(ag, liveRuntimeConfigDeps(runtimeConfigKnownKeys)))
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(w, debugConfigDumpSep)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w, debugConfigDumpSep)
}

// writeDebugConfigDumpToStderr is the production call site's spelling: the run's
// own record is stderr, which the spawn seam has already pointed at
// <session-dir>/output.log for every path that produces a task run.
func writeDebugConfigDumpToStderr(ag *agent.Agent) {
	writeDebugConfigDump(os.Stderr, ag)
}
