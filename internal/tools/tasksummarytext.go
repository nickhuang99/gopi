package tools

// tasksummarytext.go — the ledger's TEXT BOUNDARY (epic binary-summary-guard,
// 2026-09-15). The PREDICATE and the SANITIZER live in internal/textguard
// (Rule 14: one implementation in a leaf package both internal/tools and
// internal/gopilog can import — gopilog renders the same captured-prompt bytes
// as s.Task and cannot import this package without a cycle). This file keeps
// only what is genuinely the LEDGER's business: the domain ceiling on a
// summary and the names its call sites already use.
//
// THE DEFECT THIS FILE CLOSES: `gopi task list --status failed > out.txt`
// produced a file `file(1)` calls "data" and `read` refuses ("contains NUL
// bytes"). The ledger held exactly one such row:
//
//	01a09f6d-7753-7321-ba33-8b32b09213e7  [failed]  [auto] \x7fELF\x02\x01\x01…
//
// whose 351-byte summary IS the head of a Linux ELF executable. The path was
// measured, not guessed: a bare-prompt run started in /home/user/workspace/
// peak-bin, where the just-built `bin/gopi` had been copied as ./gopi. The
// operator ran `gopi gopi help`; readInput classifies a positional that names
// an EXISTING non-image file as a TEXT INPUT FILE (cmd/agent/main.go —
// "its contents become (or contribute to) the prompt"), so the executable's
// bytes became the prompt; the start hook then stored
// `[auto] ` + the first 200 runes of that prompt as the row's summary
// (internal/agent/taskregister.go autoTaskSummaryMaxChars), and TaskList
// rendered it byte-for-byte.
//
// WHY A LEDGER-LEVEL FIX AND NOT ONLY A PROMPT-LEVEL ONE: the summary column
// is the ledger's DISPLAY TEXT — it is read by `task list`, `task show`, the
// sync/serialisation paths and now by any file a caller redirects them into.
// Whatever upstream produced the bytes (this time a binary read as a text
// input file; the same shape occurs for a log, a PDF, a core dump), the
// ledger must not persist or render control bytes as if they were a headline.
// A NUL in a text column is a data-integrity defect wherever it lands, so the
// guard sits at the WRITE path (every row) with a second, cheap guard at the
// RENDER path (rows written before this fix, and rows synced in from another
// node).
//
// CONTRACT (implemented in internal/textguard; identity for text, escaped
// non-text runs carrying their leading bytes as evidence, always valid UTF-8
// and NUL-free):
//
//   - the sanitizer is IDENTITY for ordinary single-line text (CJK, emoji,
//     punctuation, quotes) — it cannot damage a legitimate summary;
//   - a newline/tab/CR becomes ONE space: the summary is the single-line
//     ledger row (the same contract the harness's own normalizeTaskSummary
//     enforces upstream);
//   - a maximal run of non-text bytes collapses to ONE placeholder carrying
//     the run's leading bytes (`[bin \x7fELF…]`): the row stays readable AND
//     the evidence stays in the row (the operator can still see WHAT was
//     captured, which a silent strip would destroy);
//   - the result is always valid UTF-8, NUL-free, and within the ledger's
//     rune ceiling.
//
// WHY THE ESCAPE CARRIES BYTES: Rule 15's spirit (no silent failure) — a
// sanitizer that silently DROPS the payload leaves a future reader unable to
// tell "a binary was captured here" from "the summary was short". The marker
// says both what happened and which bytes it was.

import "github.com/nickhuang99/gopi/internal/textguard"

const (
	// taskSummaryMaxRunes is the ledger-side ceiling on a summary: the STORE
	// refuses to persist a longer headline. It mirrors the harness's own cap
	// (internal/agent autoTaskSummaryMaxChars = 200) so the two layers agree
	// on the length of a ledger row; a caller that hands the engine a longer
	// string gets the same truncation the auto-register path applies, instead
	// of a row that no display path is sized for. Cut mid-rune is impossible:
	// the cap counts RUNES.
	taskSummaryMaxRunes = 200

	// The rendered forms of the sanitizer are ALIASES (Rule 14): the values
	// live once, in internal/textguard, so a renderer in another package and
	// this ledger can never disagree about what a marker looks like.
	taskSummaryBinaryMarker = textguard.Marker
	taskSummaryEllipsis     = textguard.Ellipsis
)

// sanitizeTaskSummary folds any string into a valid, single-line, bounded
// ledger summary. It is the summary column's write-time and render-time guard
// (see the file comment). Identity for ordinary text.
func sanitizeTaskSummary(s string) string {
	return textguard.Sanitize(s, taskSummaryMaxRunes, true)
}

// sanitizeTaskPayloadText is the same guard for the row's PAYLOAD (the
// briefing text `task show` prints). Two deliberate differences from the
// summary: the length is NOT capped (a payload is legitimately multi-line and
// long) and line structure is PRESERVED (only non-text bytes are escaped), so
// an indented/YAML/JSON payload survives intact. Identity for text payloads.
func sanitizeTaskPayloadText(s string) string {
	return textguard.Sanitize(s, 0, false)
}
