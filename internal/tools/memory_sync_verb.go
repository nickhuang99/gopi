package tools

// The WORDS of the `gopi memory sync` command line (Rule 1/14): this is the
// ONE definition of the verb and of its peer-side mode flag, referenced by the
// remote argv the asset leg composes AND by cmd/agent's dispatcher, help page
// and flag keys.
//
// WHY THEY LIVE HERE, and not in `internal/cliverbs` where the family's other
// words sit: `gopi memory sync --serve-assets` is simultaneously the command an
// OPERATOR types and the REMOTE COMMAND this binary sends over ssh (the same
// shape `gopi memory backup --serve` has, ledger_backup.go:81-95). A value the
// transport composes and the CLI dispatches must be defined beside the
// transport, or the two halves drift and the leg dials a spelling no binary
// dispatches (measured, 2026-11-05: it dialled the DELETED verb and every
// session ended in a removal notice as if it were a protocol error).
//
// HISTORY, so a reader can tell why they were absent rather than forgotten:
// `cliverbs.MemorySyncVerb` / `MemorySyncPeerFlag` / `MemorySyncPushFlag` /
// `MemorySyncPullFlag` were deleted in 5677cc6e together with the CLI file that
// was their only reader. The tools layer kept working the whole time — the
// export/import legs, the whitelist and the direction modes were never removed —
// so restoring the manual door restores the words at their one remaining
// consumer: here.
const (
	// MemorySyncVerb is the sub-verb word dispatched by `gopi memory <sub>`
	// (cmd/agent/cli_memory.go) and composed into the peer-side remote argv.
	MemorySyncVerb = "sync"
	// MemorySyncServeAssetsFlag is the peer-side mode flag (dashes omitted, the
	// shape every flag constant in this repo uses): an initiator ssh's
	// `gopi memory sync --serve-assets`, which reads its export from stdin and
	// answers with this node's reply export on stdout.
	MemorySyncServeAssetsFlag = "serve-assets"
)

// MemorySyncProgram is the human-readable spelling of the verb, used by every
// sentence this layer PRINTS about it (a refusal that tells an operator how to
// raise a bound must name a command they can type, not a constant).
const MemorySyncProgram = "gopi " + CliVerbMemoryGroup + " " + MemorySyncVerb
