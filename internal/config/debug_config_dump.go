package config

// debug_config_dump.go — the ONE switch that makes a run print the configuration
// IT is in force with, into its own record.
//
// THE QUESTION IT ANSWERS, AND WHY IT NEEDS A SWITCH AT ALL. `gopi /config` (the
// chat REPL) and `gopi config show` answer "what is configured" on demand, in
// front of a human. A DISPATCHED run has no human in front of it: its evidence is
// the log file, read later, and the question that log has to answer is "which
// values did THIS run actually send" — measured 2026-11-12, a settings document
// that said `reasoning_effort: high` and a process that held no such variable,
// with every request going out thinking-disabled. The document was right and the
// run was not, and nothing in the run's own record could show it.
//
// So the dump is OPT-IN and OFF by default: it is ~25 lines per run and it spells
// out the run's environment (base URL, model, budgets) in a file that may be
// copied or attached. A knob whose whole purpose is to add evidence must not
// itself be always-on noise in every log.
//
// CHANNEL: environment only, deliberately. There is no settings.json key and no
// CLI flag, because the thing being gated is "this RUN's log", not "this
// PROJECT's behaviour": a settings key would turn every run of that profile into
// a dumping run, and the operator's question ("what did the run I just started
// send?") is per-invocation. `GOPI_DEBUG_CONFIG=1 gopi task run <id>` is the
// whole surface.
//
// THE READER IS ONE FUNCTION (Rule 2): DebugConfigDumpEnabled below, called from
// cmd/agent's start-up seam. Nothing else in the tree reads this name, so a
// second consumer cannot invent a second truthy vocabulary.

// DebugConfigDumpEnv is the env channel of the switch (GOPI_DEBUG_CONFIG).
// Rule 21: its ONE reader is the function beside it.
const DebugConfigDumpEnv = "GOPI_DEBUG_CONFIG"

// DebugConfigDumpDefaultClaim is the ONE wording of the default, for the pages
// and documents that name the switch — a knob whose purpose is to reduce
// default output must be discoverable without reading this file (the same
// discipline AudioVerboseDefaultClaim follows).
const DebugConfigDumpDefaultClaim = "default OFF"

// DebugConfigDumpEnabled reports whether this run should write the
// configuration-in-force block into its own record.
//
// TRUTHY-ONLY, through the package's ONE boolean reader: ApplyEnv force-writes
// the canonical "off" for switches a flag turned off, and a bare
// `os.Getenv(...) != ""` reads that literal as ENABLED (the regression
// EnvBool's own doc comment records, seen twice). So "off", "0" and "false" all
// mean OFF here, exactly as they do for every other switch in this package.
func DebugConfigDumpEnabled() bool {
	return EnvBool(DebugConfigDumpEnv)
}
