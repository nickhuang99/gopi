package gopilog

// tasktext.go — the session renderers' TEXT BOUNDARY (the gopilog half of the
// binary-briefing defect, epic binary-summary-guard).
//
// THE DEFECT: `gopi session log <id> --todo` (and `--session`) exit 0 and print the
// session's accepted prompt — s.Task, the session's FIRST user row
// (reader.go, processEvents "user" case) — verbatim. When the prompt was a
// binary file read as prompt TEXT (a bare-prompt run whose positional named an
// existing non-image file: measured, the run's ./gopi ELF executable), the
// render emits `data`: 15 NULs and an ELF header straight to the terminal, and
// `gopi session log <id> --todo > f.txt` produces a file `file(1)` refuses to call
// text.
//
// WHERE THE GUARD SITS: at RENDER time, on every s.Task emit site, so a
// session file written by an older binary (or synced from another node) also
// renders as text — the same two-layer reasoning as the ledger's guard
// (internal/tools/tasksummarytext.go): the writer may already be fixed
// upstream, but a renderer cannot know what its input file holds.
//
// WHY A SHARED LEAF PACKAGE AND NOT internal/tools: this package imports
// internal/agent and internal/tools imports nothing from gopilog; a predicate
// living in tools would close a cycle. internal/textguard holds the ONE
// implementation (Rule 14) and this file is the thin, render-shaped face of it
// — a line (single-line contexts) and a lines slice (block contexts).
//
// WHAT IS DELIBERATELY NOT SANITIZED: the assistant's own text blocks, bash
// results and tool args. Those are this view's raw evidence and the operator
// may legitimately `gopi session log <id> --raw` a binary payload they captured. The
// defect is narrower and precise: the value the framework accepted as the
// task, which the renderers present as a HEADLINE. That headline must be text.

import (
	"strings"

	"github.com/nick/gopi/internal/textguard"
)

// sessionTaskText folds the session's captured task text into printable text,
// preserving its line structure. Identity for text (verified by
// TestSessionTaskGuardIsIdentityForText), so no legitimate briefing changes.
func sessionTaskText(s string) string {
	if textguard.IsText(s) {
		return s
	}
	return textguard.Sanitize(s, 0, false)
}

// sessionTaskLine is sessionTaskText for single-line contexts (summary rows,
// tables): whitespace folds to a space.
func sessionTaskLine(s string) string {
	if textguard.IsText(s) {
		return strings.Join(strings.Fields(s), " ")
	}
	return textguard.Sanitize(s, 0, true)
}

// sessionTaskLines splits the guarded task text into the lines an indented
// block (FormatSession's TASK:, FormatTodo's EPIC GOAL:) prints. Splitting
// AFTER the guard is what guarantees no emitted line can carry a NUL: a NUL
// inside the task is escaped by the guard before any line boundary is decided.
func sessionTaskLines(s string) []string {
	return strings.Split(sessionTaskText(s), "\n")
}
