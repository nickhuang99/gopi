package tools

// task_payload_fileref.go — the SUBMIT ENGINE's arm of the payload file
// reference (`@/abs/path`), so the `task_submit` TOOL and `gopi task submit
// --payload @FILE` cannot disagree.
//
// WHY THIS FILE EXISTS (the measured defect, Rule 41). The CLI expanded
// `--payload @FILE` in its own ARGUMENT PARSER (cmd/agent/cli_tasks.go) and the
// engine never saw a reference at all — so the `task_submit` tool, which hands
// its `payload` argument to the SAME engine with no parser in between, stored
// the SPELLING. Three bug rows were born at exactly 88 bytes =
// len("@"+path) while the referenced files held 2677 / 3406 / 3742 bytes, and
// every one of them reported success: the submitter saw "Task queued: …" and
// the executor got a briefing that was a PATH, not the evidence. A row whose
// evidence never arrives is worse than a refused row — refusing is loud, and
// the submitter can fix it in the same turn.
//
// THE FIX IS ONE READER, AT ONE CHOKEPOINT. The reader is
// config.ExpandFileRef — the very function the CLI used (and still uses for
// `update --payload` and `--reason`), with its existence guard, its
// whitespace guard (prose that merely STARTS with '@' is not a reference), its
// `@@` escape and its `@-` stdin arm. The chokepoint is the submit ENGINE
// (TaskSubmitReturningID, internal/tools/tasks.go), which every face passes
// through: the tool, the CLI, the reframe roadmap's children, verify-rework,
// the merge backlog. So the rule "a leading @<existing path> is READ" holds
// identically on every face by construction, and a face cannot forget it.
//
// WHY THE CLI's OWN CALL STAYS REMOVED FOR SUBMIT (and not just left in place):
// two expansions of the same value is a silent rewrite waiting to happen — a
// briefing whose CONTENT happens to begin with `@…` naming an existing file
// would be expanded a second time and stored as that other file. One call site
// per value is the only way "expanded exactly once" is true by construction.
//
// WHAT IT REFUSES, AND WHY THAT IS NOT A REGRESSION. `@/missing/path` is a
// REFUSAL naming the path and the escape, exactly as the CLI has always done
// for `--payload @@missing` (an unreadable reference is an error there too).
// The alternative — storing the spelling — is the defect this file removes. The
// prose guard keeps the ordinary case untouched: a payload beginning
// "@nick mentioned…" carries whitespace after the prefix and passes through
// verbatim, which is why "@" in a briefing is not a hazard for authors.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/config"
)

// taskPayloadFileRefNoteFormat is the line a submit's confirmation carries when
// the payload ARRIVED as a reference and was read: the submitter is told the
// row's briefing is the file's CONTENT and how large it is, so the conversion
// is never silent (Rule 15) — the same contract opts.RepoNote and the payload
// spill note already meet. %s = the source description, %d = the byte count of
// what was stored.
const taskPayloadFileRefNoteFormat = "payload: read %s (%d bytes) — the row's briefing is the file's CONTENT; the @path spelling is never stored"

// taskPayloadFileRefStdinSource names the `@-` source in that line.
const taskPayloadFileRefStdinSource = "standard input"

// taskPayloadFileRefRefusalSuffix is the second half of the refusal: it names
// the ONE thing the submitter can do about it. A refusal that does not say what
// to type next leaves its reader guessing (Rule 15).
const taskPayloadFileRefRefusalSuffix = "the @path spelling is never stored as a briefing, so this submit is refused; to store text that genuinely starts with '@', escape it as '@@' (or pass the text itself as the payload)"

// taskSubmitExpandPayloadFileRef resolves a payload that is a file reference
// through config.ExpandFileRef — the CLI's own reader — and reports the
// conversion.
//
// Returns the payload to store (unchanged for every non-reference and for prose
// that only looks like one), the ONE line describing the read ("" when nothing
// was read), and an error that REFUSES the submit when the referenced text
// cannot be read.
func taskSubmitExpandPayloadFileRef(payload string) (string, string, error) {
	if !config.IsFileRef(payload) {
		return payload, "", nil
	}
	expanded, err := config.ExpandFileRef(payload)
	if err != nil {
		return "", "", fmt.Errorf("task_submit: payload: %w — %s", err, taskPayloadFileRefRefusalSuffix)
	}
	// ExpandFileRef's own guards (a bare "@", prose with whitespace after the
	// prefix) hand the value back UNCHANGED: nothing was read, so there is
	// nothing to report and the value is stored verbatim, exactly as before.
	if expanded == payload {
		return payload, "", nil
	}
	source := strings.TrimPrefix(payload, config.FileRefPrefix)
	if source == "" || strings.HasPrefix(payload, config.FileRefPrefix+config.FileRefPrefix) {
		// The `@@literal` escape: the value did not name a file at all.
		source = "the escaped '" + payload + "'"
	}
	if payload == stdinFileRefSpelling {
		source = taskPayloadFileRefStdinSource
	}
	return expanded, fmt.Sprintf(taskPayloadFileRefNoteFormat, source, len(expanded)), nil
}

// stdinFileRefSpelling is the `@-` reference (prefix + the stdin marker), the
// spelling config.ExpandFileRef reads standard input for. It is spelled here
// from the exported prefix rather than redefined: the marker itself is the
// reader's own fact, and this file only needs to NAME it in the confirmation
// line.
const stdinFileRefSpelling = config.FileRefPrefix + "-"
