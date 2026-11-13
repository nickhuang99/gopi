package agent

// brokenpipe_guard.go — the ONE place a run that OWNS A LEDGER ROW arms its
// survival against a vanished launcher.
//
// WHY IT EXISTS (bug row 01a1950b, measured 2026-11-01 on the real binary).
// A write to fd 1/2 whose reader has closed the pipe raises SIGPIPE, and
// SIGPIPE's DEFAULT disposition is to kill the process. That is the right
// answer for a one-shot unix filter and the WRONG one for a run that has just
// inserted an `in_progress` row: the row is a promise to reach the end hook and
// write a terminal status, and a death by SIGPIPE breaks the promise with no
// session_end and no way back. Nothing collects it — automatic disposal was
// removed by the operator's 2026-10-26 ruling (devdoc/zombie-sweep-removed.md,
// "gopi reports a stuck row, it never rewrites one") — so the row sits at
// `in_progress` with `ended_at` empty forever.
//
// MEASURED, from this tree:
//
//	$ GOPI_MEMORY_DB=<private copy> ./bin/gopi chats 2>&1 | head -12
//	… 12 banner lines …
//	pipefail exit=141                       ← 128 + SIGPIPE(13)
//	$ ./bin/gopi task list --status in_progress
//	#01a1954e-…  [in_progress]  [auto] chats  …  [died 1.93s since 12:12:07]
//
// and 90 seconds later the same row still read `status: in_progress`,
// `ended_at: (not ended)`, `process: … (live: gone)`.
//
// WHAT WAS ALREADY THERE, AND WHY THE KEY WAS WRONG. cmd/agent/main.go ignores
// SIGPIPE for a `task run` CHILD (`GOPI_TASK_WRAPPER_ID` is set pre-spawn), and
// its own comment states the boundary it drew: "Top-level `gopi | head`
// semantics are untouched — a plain run keeps the default SIGPIPE disposition."
// That was true when a top-level run owned nothing. Since run-identity-unify it
// MINTS ITS OWN ROW (maybeRegisterSessionTask → TaskNewOID + the self-claim
// stamp), so it carries exactly the close-out obligation the wrapper child's
// guard was written to protect. The guard was keyed on the process's LINEAGE
// when the fact that matters is its DEBT.
//
// SCOPE, stated so this is not read as "gopi now ignores SIGPIPE everywhere".
// The only caller is maybeRegisterSessionTask, immediately after the row INSERT
// succeeds: a `gopi task list | head`, a `gopi config show`, every subcommand
// that owns no row, and every wrapper child (which never reaches that arm — it
// latches the wrapper row and returns before minting) keep the default SIGPIPE
// disposition and die like the ordinary unix tools they are.
//
// THE EFFECT ON THE WRITE ITSELF is the ordinary one for an ignored SIGPIPE: a
// write to a closed pipe returns EPIPE instead of killing the process. That is
// strictly better here — the close-out path's writes are best-effort diagnostics
// whose errors are already non-fatal by contract (logAutoTaskf, the ledger
// writes' own error handling), so the run reaches its end hook and the row
// lands in a terminal status even when nobody is reading its output.

import (
	"os/signal"
	"syscall"
)

// armBrokenPipeSurvivalForRowOwner makes SIGPIPE non-fatal for THIS process,
// because this process has just promised a ledger row a terminal status.
//
// Idempotent and cheap: signal.Ignore on an already-ignored signal is a no-op
// re-registration, so a run that registers a row more than once pays nothing.
// It is deliberately NOT behind a config knob — a switch that can re-arm the
// "run dies silently and the row never closes" behaviour would be a switch that
// can recreate the defect, and Rule 35's test for a new knob is what it breaks.
func armBrokenPipeSurvivalForRowOwner() {
	signal.Ignore(syscall.SIGPIPE)
}
