package main

// ONE ANSWER TO "WHERE IS THIS SESSION'S LOG?" — the session verbs' shared
// resolver (Rules 13/14).
//
// WHY THIS FILE EXISTS (measured on this host's live ledger, 2026-10-29). The
// session verbs each called gopilog.FindSession directly, which answers for the
// three layouts a SESSION id names (legacy flat <id>.jsonl, task-run
// <task_id>/<id>.jsonl, bare <id>/session.jsonl). A TASK id names none of them
// as a file name — the directory is named by the task, the file by the run's
// uuid — so the same operand resolved on one verb and not on another:
//
//	$ gopi session log 01a14b50-8df4-7d92-a4f9-fc1b40a7f9a9
//	exit=1  Session not found: 01a14b50-…
//	$ gopi session resume 01a14b50-8df4-7d92-a4f9-fc1b40a7f9a9
//	exit=0  [gopi] resume: continuing session cee08beb-…
//
// Both operands name the SAME row. `resume` reached it through the row's
// `session_id` column; `log` had no row path at all. So the row path is added
// HERE, once, ahead of the id layouts — and when it resolves nothing the
// resolver falls through to gopilog.FindSession untouched, so every invocation
// that worked before resolves to the same file it did before.
//
// The row resolution itself is internal/tools' (tools.TaskOwnSessionLog — one
// implementation of its three sources and its refusals), because the merger's
// recovery arms ask the same question and a second copy would drift.
//
// A REFUSAL FROM THE ROW PATH IS NOT A FALLTHROUGH. tools.TaskOwnSessionLog
// returns an error only when the row's evidence CONTRADICTS itself (its
// session_id column names a record that records a different row as its owner,
// or its run directory holds two records written at the same instant). Walking
// on would serve a session the evidence just refused, so the error is returned
// to the caller, which exits non-zero (Rule 15).

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/tools"
)

// findSessionLogPath resolves a session-verb operand to the JSONL record it
// names: through the LEDGER ROW first when the operand names one, then through
// gopilog.FindSession exactly as before.
//
// Returns ("", nil) when neither answers — the caller's own "not found"
// diagnostic, unchanged. Returns ("", err) when the ROW evidence refused (see
// the header): the caller prints it and exits non-zero, never resolving past it.
func findSessionLogPath(dir, operand string) (string, error) {
	own, err := tools.TaskOwnSessionLog(operand)
	if err != nil {
		return "", err
	}
	if own.Note != "" {
		// Rule 15: the answer is real, but its ownership is weaker than the
		// strong path (it rests on the ledger column, not on the address it was
		// found at), so the operator is told once instead of the resolver
		// quietening the difference.
		fmt.Fprintf(os.Stderr, "[gopi] session: %s\n", own.Note)
	}
	if own.Path != "" {
		return own.Path, nil
	}
	return gopilog.FindSession(dir, operand)
}
