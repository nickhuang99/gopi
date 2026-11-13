package config

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/taskverbs"
)

// envSection groups EnvVars entries under a section heading for the help.
type envSection struct {
	title string
	envs  []string // env var names, in EnvVars order
}

// Sections is the display grouping order. Any env var not listed here lands
// in the trailing "Other" section.
var Sections = []envSection{
	{"Endpoint (DeepSeek / Anthropic-compatible)", []string{
		"ANTHROPIC_AUTH_TOKEN", "DEEPSEEK_API_KEY",
		"ANTHROPIC_BASE_URL", "DEEPSEEK_BASE_URL",
		"ANTHROPIC_MODEL", "DEEPSEEK_MODEL",
	}},
	{"Vision (image understanding)", []string{
		"GOPI_VISION_MODEL", "GOPI_VISION_BASE_URL",
	}},
	{"Thinking & reasoning", []string{
		"GOPI_THINKING", "GOPI_REASONING_EFFORT", "GOPI_EFFORT_VALUES", "GOPI_THINKING_BUDGET",
		"GOPI_HIDE_THINKING", "GOPI_KEEP_THINKING", "GOPI_MAX_TOKENS", "GOPI_CONTEXT_WINDOW",
	}},
	{"Agent runtime", []string{
		"GOPI_TIMEOUT_SEC",
		"GOPI_COMPACT", "GOPI_DEBUG",
	}},
	{"Memory compaction", []string{
		"GOPI_COMPACT_TRIGGER", "GOPI_COMPACT_TARGET", "GOPI_COMPACT_KEEP_RECENT",
		"GOPI_COMPACT_MAX_SUMMARY_TOKENS", "GOPI_COMPACT_COOLDOWN",
		"GOPI_COMPACT_REFERENCE_ONLY",
	}},
	{"Logging", []string{
		"GOPI_RAW_LOG", "GOPI_RAW_LOG_DIR",
	}},
	{"Tools (bash / file / websearch / memory)", []string{
		"GOPI_TOOL_TIMEOUT_SEC", "GOPI_BASH_TIMEOUT_SEC", "GOPI_BASH_IDLE_SEC",
		"GOPI_ALLOW_DANGEROUS", WebSearchScriptsDirEnvName, "GOPI_MEMORY_DB",
		"GOPI_BASH_LOCALE", "GOPI_SHOT_CHROME", "GOPI_CODE_SEARCH_ROOT",
		"GOPI_WEB_ADDR", "GOPI_WEB_TOOLS", "GOPI_WEB_TOKEN",
		"GOPI_CLI_TURN_TIMEOUT_SEC", "GOPI_LINKS_BIN",
		ChatSurfaceIdentityEnv,
	}},
	{"Distributed task ledger (sync / worker / role)", []string{
		"GOPI_NODE_ROLE", "GOPI_SYNC_TO", "GOPI_SYNC_PEERS", "GOPI_SYNC_NODE",
		"GOPI_ADOPTION_INTERVAL", "GOPI_WORKER_MAX_ROUNDS",
		"GOPI_WORKER_IDLE_EXITS", WorkerIdleIntervalEnv,
		TaskSyncEnv,
		"GOPI_TASK_NOTIFY_WEBHOOK", "GOPI_TASK_NOTIFY_CMD", "GOPI_TASK_NOTIFY_EVENTS",
		"GOPI_TASK_NOTIFY_PATTERN", "GOPI_TASK_NOTIFY_ASYNC", SkipClaimBroadcastEnv,
		WorktreeSweepMinAgeEnvName, WorktreeSweepMaxEnvName,
	}},
	{"Memory-asset sync (distilled_qa knowledge)", []string{
		"GOPI_MEMORY_PEERS",
	}},
	{"Harness tuning & diagnostics", []string{
		"GOPI_NUDGE_ROUNDS", "GOPI_NUDGE_TOOLS",
		"GOPI_ERROR_DUMP_DIR", "GOPI_1214_DUMP_DIR", "GOPI_ANCHOR_GUARD", "GOPI_HIPPO_DB",
	}},
	{"Settings path", []string{
		"GOPI_HOME", "GOPI_SETTINGS",
	}},
	{"Temp workspace & agent sandbox", []string{
		TmpDirEnvName, scratch.TmpDirHeadroomFloorEnvName,
		SandboxEnvName, SandboxLedgerSeedEnvName,
	}},
	// (the `Remote dispatch` group lived here; it carried one variable and a
	// "NO CONSUMER" marker — the knob was deleted 2026-10-22, see the report)
}

// envByName maps env var → its catalog entry for fast lookup.
func envByName() map[string]envKey {
	m := make(map[string]envKey, len(EnvVars))
	for _, e := range EnvVars {
		m[e.Env] = e
	}
	return m
}

// Layout budgets for the rendered help (Rule 1a: naming the numbers keeps the
// wrapping policy in one place instead of scattered inline literals).
const (
	// helpWrapCol is the right margin for wrapped prose: every block wraps
	// below this column. It ALIASES the exported HelpWrapCol (Rule 14: one
	// definition for every consumer — the subcommand usage constants live in
	// cmd/agent and their guard measures against the same budget).
	helpWrapCol = HelpWrapCol
	// envTableHelpCol is the indent of an env entry's prose help.
	envTableHelpCol = 4
	// helpHeadMaxCol bounds an env entry's HEAD line — the catalog triple
	// (env name + settings key + flag). It is data, not prose: the columns are
	// sized from the catalog so the triple cannot be wrapped, and this budget
	// only fails if the catalog grows a name wide enough to push the row past
	// what a terminal shows.
	helpHeadMaxCol = 96
	// commandIndexMinCol keeps a short command block’s summaries from
	// crowding the names; a wider name pushes the column out automatically.
	commandIndexMinCol = 28
	// usageMinCol / inputMinCol / optionMinCol / exampleMinCol floor the
	// summary column of each top-level block. They are floors only: the
	// column is computed from the block's own widest name, so the two
	// long--vision-base-url/--reasoning-effort rows that used to break the
	// Options block's hand-spaced alignment simply widen the column.
	usageMinCol   = 34
	inputMinCol   = 16
	optionMinCol  = 24
	exampleMinCol = 44
	// helpRowIndent is the leading indent of one table row.
	helpRowIndent = 2
	// emDashFlagLabel is the flag column's "this knob has no CLI flag" mark,
	// U+2014 EM DASH. One definition: envFlagLabel prints it, the `gopi -h`
	// reachability sentence and the --help-env preamble quote it as the mark to
	// look for, and the claim↔reality gate asserts the column and the refusal
	// set agree on it (Rules 1/13/14).
	emDashFlagLabel = "—"
	// emDashFlagCaption is how the help NAMES what that mark means, shared by the
	// --help-env preamble and the gate asserting the mark is explained, so the
	// mark and its explanation cannot drift apart (Rule 13).
	emDashFlagCaption = "no flag of its own"
)

// The two spellings of the reachability claim the help text may carry. The
// universal form is what shipped before 2026-09-27 and is FALSE for every
// catalog row with no flag of its own (44 of 80 measured rows); the subset form
// is the true one. Both live here so the prose and the gate on it cannot drift
// (Rule 13) — the test asserts the subset form is present and the universal one
// is gone.
const (
	universalReachabilityClaim = "Any other knob is reachable with its full env var name as a flag"
	reachabilityClaimSubset    = "A knob that has a CLI flag is also reachable by its full env var name"
)

// promptPositionalRuleClaim is the ONE sentence the help uses to state what a
// positional does when --prompt is given. It is a constant because the
// implementation must follow the same rule it advertises: until 2026-09-27 the
// --prompt row said "positionals become files/images" while readInput read a
// non-image file positional and then DISCARDED it (end-user-audit slice 8,
// RP-3) — the row and the code had drifted. The --prompt row renders this
// constant and internal/config's help gate asserts it survives, so a
// re-worded row cannot silently drop the rule again (Rule 13).
const promptPositionalRuleClaim = "a text file's contents are appended to the instruction"

// FrameNoteText is the ONE sentence with which the top-level help states WHERE
// the agent-harness framing applies (RP-1, end-user-audit slice 8, 2026-09-26).
//
// WHY IT EXISTS: the `"<prompt>"` Inputs row promises the shell text is what the
// model is asked. Before this fix the run path also wrapped it as `[EPIC GOAL]`
// plus the task-list deadline and the reframe protocol (~1000 chars), and the
// page said nothing about it. The frame is now confined to the surfaces that
// ARE tasks, and this sentence makes the difference predictable from the page.
//
// IT CLAIMS EXACTLY WHAT WAS MEASURED, AND NO MORE. Measured at the request
// boundary: a plain prompt's own text is the model's first user message, as
// typed — and a TASK-START memory checkpoint is still appended to that turn by
// the harness (GOPI_NO_ENFORCE_MEMORY=1 removes it). The sentence therefore
// promises "no epic goal, task-list directive or reframe protocol added" and
// keeps the checkpoint in its own clause, rather than claiming a bare turn the
// run does not produce. A page that overstated this would be the same defect in
// the other direction.
//
// EXPORTED (Rule 14) so the gate that drives the REAL `gopi -h` (cmd/agent)
// asserts the shipped page carries this claim, rather than re-typing its words
// — the sentence and the assertion cannot drift.
const FrameNoteText = "No epic goal, task-list directive or reframe protocol is added to it: the text is " +
	"the model's first user message as typed. The run is still governed by the agent's system prompt and its own " +
	"checkpoints. A DISPATCHED run (" + TaskVerbRun + " / " + TaskVerbWorker +
	") is framed instead — its briefing is wrapped in the " + FrameMarkerEpic + " / " + FrameMarkerTaskList + " / " +
	FrameMarkerReframe + " frame."

// The three markers the harness frame is built from, spelled as they appear in a
// session's own transcript (so a reader who meets one in `gopi session log`/`gopi session raw`
// can find what produced it). They live here because FrameNoteText names them —
// the page and the gate read the same constants (Rule 14).
const (
	FrameMarkerEpic     = sessionevent.HarnessEpicGoalFrameMarker
	FrameMarkerTaskList = "[TASK_LIST_BEGIN]"
	FrameMarkerReframe  = "[EPIC REFRAME]"
)

// The verbs FrameNoteText names — the surfaces whose run IS a task. They are
// consts of THIS package because internal/config cannot import cmd/agent (its
// spelling source is package main): the family verb comes from the leaf package
// that exists for exactly that (taskverbs), and the top-level verbs from the
// leaf packages that already hold them (procname.VerbWorker) — never typed
// inline at the use site (Rule 1b/13/14).
//
// `gopi merge` WAS the third member of this list and is GONE (epic
// merge-removal, item 2): the verb no longer dispatches, so it is not a
// dispatched run and naming it here would promise a framing that no run will
// produce (Rule 15 — a page that names a surface nothing dispatches is the
// dead-end shape `gopi merger`'s missing index row was removed for). The
// merge IMPLEMENTATION (merge_run.go and the rest) is untouched by this
// removal; it is simply no longer reachable from argv.
const (
	// TaskVerbRun is the FAMILY spelling (`gopi task run`) — the group verb is
	// part of the invocation, so naming only the sub-verb would print a command
	// that does not exist.
	TaskVerbRun    = "gopi " + cliverbs.Task + " " + taskverbs.Run
	TaskVerbWorker = "gopi " + procname.VerbWorker
)

// helpColumn computes the alignment column for a block of (name, summary)
// rows: two spaces past the widest name, floored at minColumn. Sizing the
// column from the block's OWN content is what keeps every rendered table
// aligned — an inline width literal would silently break the moment a row
// outgrows it (Rule 1a/1b).
func helpColumn(rows [][2]string, minColumn int) int {
	widest := 0
	for _, r := range rows {
		if w := DisplayWidth(r[0]); w > widest {
			widest = w
		}
	}
	if widest+2 < minColumn {
		return minColumn
	}
	return widest + 2
}

// usageRows are the invocation forms shown under "Usage:".
var usageRows = [][2]string{
	{`gopi [options] "<prompt>"`, "Run one task from a prompt argument"},
	{"gopi [options] input.txt", "Run one task read from a text file"},
	{"gopi [options] < input.txt", "Run one task read from stdin"},
	{"gopi [options] --prompt TEXT a.png", "Run one task with an attached image"},
	{"gopi -h | --help", "This help"},
	{"gopi --help-env", "The complete environment-variable table"},
	{"gopi <command> -h", "Full help for one command"},
	{"gopi version", "Build version (also --version / -v)"},
}

// inputRows describe what gopi accepts on the command line.
//
// THE "<prompt>" ROW IS THE CONTRACT THE MODEL'S FIRST REQUEST IS MEASURED
// AGAINST (RP-1, end-user-audit slice 8): the shell prompt is sent verbatim, so
// this row states that — and, explicitly, that a TAILORED RUN (a dispatched
// task row, `gopi merge`) is framed instead, whose own pages document the
// framing. A page that promised "the task text" while the run path also
// appended a task-list deadline and a reframe protocol made the two
// un-predictable from the page alone (help.go's frameNoteMarker is what a gate
// asserts is present here).
var inputRows = [][2]string{
	{`"<prompt>"`, "The task text. A bare word or sentence is prose. A token that LOOKS like a path — a \"/\" or a file extension (.txt/.md/.png/.gif/.jpeg/.webp) — is a FILE: one naming nothing is refused by name (exit 1), not sent as prompt; pass such text with --prompt TEXT or \\@TEXT. With no prompt and no stdin, gopi prints this help and exits 1. " + FrameNoteText},
	{"@PATH", "A positional or --prompt value starting with '@' reads the task payload from PATH verbatim; '@-' reads stdin, and \"\\@text\" passes a literal prompt that starts with '@'."},
	{"./image.png", "A positional naming an existing .png/.jpg/.gif/.webp file is attached NATIVELY (pixels, no OCR round-trip) and auto-switches the model to the vision one; --prompt TEXT carries the instruction."},
	{"./notes.txt", "A positional naming an existing non-image file contributes its text to the task; alone it IS the prompt, and with --prompt TEXT its text is appended to the instruction."},
	{"./PROJECT.md", "Project rules, auto-loaded as a system message when present (`man gopi` for the GOPI.md fallback)."},
}

// optionRows are the common options: flag, its env var (and settings key when
// the shorthand differs), and a one-line summary. This is a curated subset —
// the complete catalog is `gopi --help-env`.
var optionRows = [][2]string{
	{"--settings PATH", "settings.json path (GOPI_SETTINGS)"},
	{"--home PATH", "gopi data root: sessions, raw logs, memory.db, settings under $GOPI_HOME"},
	{"--prompt TEXT", "task instruction; positionals still carry task input — " + promptPositionalRuleClaim},
	{"--model NAME", "model (DEEPSEEK_MODEL / ANTHROPIC_MODEL)"},
	{"--api-key KEY", "API key (DEEPSEEK_API_KEY / ANTHROPIC_AUTH_TOKEN)"},
	{"--base-url URL", "API base URL (DEEPSEEK_BASE_URL / ANTHROPIC_BASE_URL)"},
	{"--vision-model NAME", "vision model (GOPI_VISION_MODEL)"},
	{"--vision-base-url URL", "vision base URL (GOPI_VISION_BASE_URL)"},
	{"--reasoning-effort LEVEL", "reasoning depth (GOPI_REASONING_EFFORT)"},
	{"--thinking ON|OFF", "thinking mode (GOPI_THINKING)"},
	{"--hide-thinking ON|OFF", "hide the live reasoning display"},
	{"--keep-thinking ON|OFF", "replay reasoning in assistant history"},
	{"--max-tokens N", "per-response output cap (GOPI_MAX_TOKENS)"},
	{"--context-window N", "context window budget (GOPI_CONTEXT_WINDOW)"},
	{"--timeout SECONDS", "overall agent timeout, 0 = unlimited (GOPI_TIMEOUT_SEC)"},
	{"--compact ON|OFF", "memory compaction (GOPI_COMPACT)"},
	{"--tool-timeout SEC", "read/write/edit timeout (GOPI_TOOL_TIMEOUT_SEC)"},
	{"--bash-timeout SEC", "bash absolute timeout (GOPI_BASH_TIMEOUT_SEC)"},
	{"--bash-idle SEC", "bash no-output timeout (GOPI_BASH_IDLE_SEC)"},
	{"--raw-log ON|OFF", "raw HTTP logging (GOPI_RAW_LOG)"},
	{"--db PATH", "knowledge base for memory search/store (GOPI_MEMORY_DB)"},
	{"--tmpdir PATH", "temp workspace for gopi and its children (GOPI_TMPDIR)"},
	// The reply-language row. Its summary is a CONSTANT (output_language.go)
	// for the reason stated there: the knob shipped documented in
	// `gopi --help-env` alone, so this page — the one a user reads first —
	// never mentioned a language preference at all.
	{"--" + OutputLanguageFlagName + " TAG", OutputLanguageRowSummary},
	// The chat-size row: the geometry the chat surface's chrome is drawn for. It
	// is on this page for the same reason the language row is — the knob is
	// user-facing (a narrower terminal can SEE the difference), and the page a
	// user reads first is where it becomes discoverable.
	{"--" + ChatIdealGeometryFlagName + " COLS" + ChatIdealGeometryDimSep + "ROWS", ChatIdealGeometryRowSummary},
	// The memory-prefetch row. It is on this page for a third time the same
	// reason: the knob shipped complete (three channels, an executor, tests,
	// a CONFIG.md row) and THIS page — the front door — did not mention it, so
	// nothing a user reads first could tell them it existed. The flag is
	// hoisted (it must precede the verb), which the summary states, because a
	// reader who tries `gopi chat --memory-prefetch` gets a refusal.
	{"--" + MemoryPrefetchFlagName, MemoryPrefetchRowSummary},
	// The self-driven turn budget. It is on this page because the number is the
	// operator's guarantee, not the model's: a page that hid it would leave the
	// console notice ("self-driven turn 1/20") as the only place it is visible.
	{"--" + ChatSurfaceTurnsFlagName + " N", ChatSurfaceTurnsRowSummary},
}

// exampleRows are the copy-paste starting points closing the help.
var exampleRows = [][2]string{
	{`gopi "write a README for this repo"`, "run a task"},
	{"gopi --timeout 600 ./task.txt", "run with a 10-minute cap"},
	{"gopi ./screenshot.png", "understand an image"},
	{`gopi --prompt "chart data?" chart.png`, "attach an image + instruction"},
	{`gopi --prompt "compare these" a.txt b.txt`, "instruction + the text of both files"},
	{`gopi task submit "Fix the parser" @brief.md`, "enqueue a task for a worker"},
	{`gopi --output-language zh-CN "reply in Chinese"`, "answer in Chinese"},
}

// renderSection renders a titled block of (name, summary) rows using the
// block's own widest name for the column, so a long name can never break the
// alignment (the defect the 2026-09-13 help-format epic fixed in the env
// table — the Options/Inputs/Examples blocks carried the same hand-spacing).
func renderSection(title string, rows [][2]string, minColumn int) string {
	return title + "\n" + renderRows(rows, helpColumn(rows, minColumn))
}

// renderRows renders (name, summary) rows into one aligned block: the name
// starts the row, the summary follows at the block's column, and the summary
// wraps under ITSELF (never under the name). A name wider than the column gets
// its own line — the column is a floor for a narrow block, and an over-wide
// name is never cut.
func renderRows(rows [][2]string, column int) string {
	var b strings.Builder
	summaryIndent := strings.Repeat(" ", helpRowIndent+column)
	for _, r := range rows {
		// Every measurement is DISPLAY width: a CJK name is twice as wide as
		// its rune count, and padding by runes would shift the summary column
		// under a wide row (HelpWrapCol/DisplayWidth in width.go).
		nameOnOwnLine := DisplayWidth(r[0])+helpRowIndent > column
		indent := summaryIndent
		if nameOnOwnLine {
			indent = strings.Repeat(" ", 2*helpRowIndent)
		}
		lines := wrapText(r[1], indent)
		pad := strings.Repeat(" ", column-DisplayWidth(r[0]))
		switch {
		case len(lines) == 0:
			b.WriteString(strings.Repeat(" ", helpRowIndent) + r[0] + "\n")
		case nameOnOwnLine:
			b.WriteString(strings.Repeat(" ", helpRowIndent) + r[0] + "\n")
		default:
			// The first wrapped line shares the row with the name; the rest
			// flow underneath it at the summary's column.
			b.WriteString(strings.Repeat(" ", helpRowIndent) + r[0] + pad +
				strings.TrimPrefix(lines[0], indent) + "\n")
			lines = lines[1:]
		}
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// wrapText wraps s to helpWrapCol and prefixes every produced line with
// indent. A word longer than the available width is left intact — splitting
// (or truncating) an env var name or a flag would be worse than a long line.
// Note the continuation contract: EVERY line — the first and each wrapped
// one — carries the same `indent`. A caller consuming the block as a whole
// must de-indent, exactly like the rows renderer does; that is what keeps a
// wrapped TITLE and a wrapped ROW the same shape instead of two.
//
// THE BUDGET IS THE INVARIANT, THE FLOOR IS NOT. The available width is
// `helpWrapCol - len(indent)`; a floor that can exceed it would render PAST
// the wrap column — a block whose summary column is deep (a long command name
// widens the whole block via helpColumn) would emit lines wider than the
// terminal, which is the one thing this function exists to prevent. Measured:
// the renderer's floor of helpWrapCol/2 with a 48-column block produced
// 50+44 = 94-column lines, reddening
// cmd/agent's TestTopLevelUsageIsAlignedAndTerse. So the floor is applied
// only where it CANNOT violate the budget — as a guard against a degenerate
// (non-positive) indent, never as a licence to overflow.
func wrapText(s, indent string) []string {
	width := helpWrapCol - len(indent)
	if width < 1 {
		// Degenerate indent (at or past the wrap column): there is no room to
		// wrap into, and a zero/negative budget would drop every word on its
		// own line. One column is the honest minimum; the caller's own budget
		// test is what catches a block this deep.
		width = 1
	}
	var out []string
	cur := ""
	curWidth := 0
	for _, w := range strings.Fields(s) {
		ww := DisplayWidth(w)
		switch {
		case cur == "":
			cur, curWidth = w, ww
		case curWidth+1+ww <= width:
			cur += " " + w
			curWidth += 1 + ww
		default:
			out = append(out, indent+cur)
			cur, curWidth = w, ww
		}
	}
	if cur != "" {
		out = append(out, indent+cur)
	}
	return out
}

// FormatEnvTable exports the grouped env-var table (used by `gopi --help-env`).
func FormatEnvTable() string {
	return "GoPi environment variables\n" +
		"Every knob below is reachable as an environment variable and as a\n" +
		"settings.json key. The third column is the CLI flag:\n" +
		"  --flag  the knob also has this CLI flag, and its full env var name is\n" +
		"          accepted as a flag too (--gopi-timeout-sec ≡ --timeout)\n" +
		"  —       the knob has " + emDashFlagCaption + " — env var + settings.json key\n" +
		"          only; typing its env name as a flag is refused, naming this\n" +
		"          reason (there is no flag column entry to reach it through)\n" +
		"Precedence: CLI flag > env var > settings.json > default.\n" +
		formatEnvTable() + "\n"
}

// formatEnvTable renders the grouped env-var table. The table is long by
// nature, which is exactly why it lives behind its own `--help-env` flag
// instead of being inlined into the everyday `gopi -h`.
//
// Columns are measured from the catalog rather than from a fixed width, so a
// long variable name (GOPI_COMPACT_MAX_SUMMARY_TOKENS) can never push a row's
// columns out of alignment.
func formatEnvTable() string {
	byName := envByName()

	envCol, keyCol, flagCol := 0, 0, 0
	for _, e := range EnvVars {
		if len(e.Env) > envCol {
			envCol = len(e.Env)
		}
		if n := len(envKeyLabel(e)); n > keyCol {
			keyCol = n
		}
		if n := len(envFlagLabel(e)); n > flagCol {
			flagCol = n
		}
	}

	// One entry: ENV  settings-key  --flag, then the prose help wrapped
	// underneath it. The head is a triple of NAMES (env / settings key /
	// flag), budgeted by helpHeadMaxCol — it is data, not prose, so it is the
	// one line class that does not wrap; every prose line below it does.
	line := func(e envKey) string {
		head := fmt.Sprintf("  %-*s  %-*s  %s", envCol, e.Env, keyCol, envKeyLabel(e), envFlagLabel(e))
		return head + "\n" +
			strings.Join(wrapText(e.Help, strings.Repeat(" ", envTableHelpCol)), "\n") + "\n"
	}

	claimed := map[string]bool{}
	var b strings.Builder
	for _, sec := range Sections {
		b.WriteString("\n" + sec.title + "\n")
		any := false
		for _, name := range sec.envs {
			e, ok := byName[name]
			if !ok {
				continue
			}
			b.WriteString(line(e))
			claimed[name] = true
			any = true
		}
		if !any {
			b.WriteString("  (none)\n")
		}
	}
	// Anything left unclaimed.
	var rest []string
	for _, e := range EnvVars {
		if !claimed[e.Env] {
			rest = append(rest, e.Env)
		}
	}
	if len(rest) > 0 {
		b.WriteString("\nOther\n")
		for _, name := range rest {
			if e, ok := byName[name]; ok {
				b.WriteString(line(e))
			}
		}
	}
	return b.String()
}

// envKeyLabel is the settings.json key shown for one catalog entry; the
// settings path key renders as a pointer (there is no key to edit).
func envKeyLabel(e envKey) string {
	if e.Key == settingsPathKey {
		return "(see --settings)"
	}
	return e.Key
}

// envFlagLabel is the CLI flag shown for one catalog entry, or an em dash when
// the knob has no flag of its own.
func envFlagLabel(e envKey) string {
	if e.CLIFlag == "" {
		return emDashFlagLabel
	}
	return flagPrefix + e.CLIFlag
}

// FormatCommandIndex renders the command index ALONE — every section
// `commandIndex` declares, in its own order, each title as a heading and each
// row as an aligned name+summary line — with NO preamble, NO Options/Inputs
// block, NO Examples and NO env-var closing paragraph.
//
// It exists because the index has a SECOND consumer with a different frame:
// the agent's system prompt renders the same commands so the model knows what
// `gopi` dispatches without probing `-h` verb by verb (internal/agent's
// `commands` section). The alternative shapes were both rejected: a copy of the
// table in internal/agent would be a mirror of the one value Rule 14 says may
// not be mirrored (and would go stale the moment a verb retires — the exact
// defect this renderer exists to remove), while making the prompt carry
// `FormatUsage()` would hand it Options, Examples and an env-var paragraph that
// belong to a shell user, not to the model.
//
// FormatUsage's command region IS this function's output: one writer for the
// rows, so `gopi -h` and the system prompt cannot disagree about what a
// command line says. Each block begins with a newline (the renderer's own row
// spacing), so a caller joining blocks keeps them apart without adding a
// separator of its own.
func FormatCommandIndex() string {
	var b strings.Builder
	for _, sec := range commandIndex {
		b.WriteString("\n")
		// The section TITLE is wrapped like any other prose line, so the
		// 88-col budget holds for it too (TestTopLevelUsageIsAlignedAndTerse).
		// A title that a consumer must read as ONE phrase (see
		// TestCLINamingExceptionsDocumentedInTopLevelHelp) therefore has to be
		// written short enough to fit a single line — the renderer wraps by
		// width and does not re-join across lines.
		for _, l := range wrapText(sec.title, "") {
			b.WriteString(l + "\n")
		}
		b.WriteString(renderRows(sec.rows, helpColumn(sec.rows, commandIndexMinCol)))
	}
	return b.String()
}

// FormatUsage renders the top-level `gopi -h` — the front door to the CLI:
// how to invoke it, what it accepts, the common options, and an index of every
// command, each pointing at its own `-h` for the full contract.
//
// The complete env-var table deliberately does NOT appear here (it made the
// everyday help ~470 lines and buried the command index; 2026-09-13
// help-format epic). The option list still maps each common flag to its env
// var, and the closing paragraph names `gopi --help-env` for the full table.
//
// Every command dispatched in main.go appears in the index — enforced by
// TestTopLevelUsageAdvertisesEveryDispatchedVerb, which enumerates both the
// cliVerbs constants and the dispatch chain, so a command can never become
// undiscoverable again.
func FormatUsage() string {
	var b strings.Builder

	b.WriteString(`GoPi — a tool-using AI coding agent (DeepSeek / Anthropic-compatible endpoints).

One run is one task: the model reads and edits files, runs shell commands, searches
code and the web, and stores distilled lessons.

`)
	b.WriteString(renderSection("Usage:", usageRows, usageMinCol))
	b.WriteString("\n")
	b.WriteString(renderSection("Inputs:", inputRows, inputMinCol))
	b.WriteString("\n")
	b.WriteString(renderSection("Options (flag > env var > settings.json > default):",
		optionRows, optionMinCol))
	b.WriteString(`
  A knob that has a CLI flag is also reachable by its full env var name as a
  flag; a knob with none is env + settings only (` + emDashFlagLabel + ` in ` + "`gopi --help-env`" + `).
  No single-dash options: "-z" is refused (` + "`man gopi` OPTIONS" + `).

Commands (each accepts -h for its full help; a tool-named
command keeps the tool's own underscore spelling; a tool name that is
NOT a command is refused by name — devdoc/cli-naming-convention.md).
`)

	b.WriteString(FormatCommandIndex())

	b.WriteString("\n")
	b.WriteString(renderSection("Examples:", exampleRows, exampleMinCol))
	b.WriteString(`
Environment variables:
  Every option above also exists as an env var, a settings.json key and a CLI
  flag. Precedence: flag > environment variable > settings.json > default.
  Run ` + "`gopi --help-env`" + ` for the complete table, grouped by area.
`)
	return b.String()
}

// commandSection is one titled block of the command index.
type commandSection struct {
	title string
	rows  [][2]string // name, one-line summary ("\n" continues the row)
}

// commandIndex is the top-level command index: every command gopi dispatches,
// grouped by what the user is trying to do. The one-line summaries are the
// index's whole point — the full contract lives in each command's own -h
// (cli_*.go usage constants), which is where detail belongs.
//
// ONE VERB, ONE LINE, ONE SENTENCE (tightened 2026-10-05, task 01a10bb7): a row
// is the verb plus a single short sentence; enumerations, flag lists and
// historical asides belong on the verb's own page. Two properties make that
// shape load-bearing rather than cosmetic. (1) The census gate
// (cmd/agent/cli_verb_census_test.go) reads a verb at the START of a help line —
// `^ {2}gopi <verb>` — so a row that merely CONTAINS a word advertises nothing,
// and a summary long enough to wrap costs a line for no advertised verb.
// (2) topLevelUsageMaxLines caps the page (cmd/agent/usage_test.go), so every
// wrapped continuation line is spent from a budget the index exists to keep.
//
// Coverage is machine-checked (TestTopLevelUsageAdvertisesEveryDispatchedVerb):
// the test enumerates the cliVerbs constants and main.go's dispatch chain and
// requires each verb to appear here.
var commandIndex = []commandSection{
	{"Task ledger & execution — the cross-session audit trail of every task:", [][2]string{
		// THE ROW ENUMERATES taskverbs.IndexRowVerbs, WHICH IS NOW FamilyVerbs
		// AND NOTHING ELSE (2026-11-05). It was widened on 2026-10-10
		// (task 01a11f70) to carry the cross-node legs too — `send` / `receive`
		// / `pull`, the spellings that moved a ROW to another machine — because
		// a reader who did not already suspect they existed could not reach
		// them from the first page he opens. That SURFACE IS DELETED: no verb in
		// this binary opens a session against another node's ledger, so the row
		// must not offer one. IndexRowVerbs still derives from the union
		// (FamilyVerbs ∪ SyncVerbs) and SyncVerbs is simply empty now, so this
		// row needs no edit when the family changes — the derivation is the ONE
		// place that decides what a reader is offered.
		//
		// WHAT STAYED, and it is deliberate: the sync family is not erased from
		// the vocabulary — its eight spellings are in taskverbs.RemovedVerbs and
		// each still dispatches to a removal sentence, and `gopi task -h`
		// carries the note that replaces them (naming where a row that must be
		// worked on another host is FILED). This row names LIVE verbs only; the
		// explanation lives where a reader who greps for the intent lands.
		{"gopi task <subcommand>", "The task-as-memory ledger: " + strings.Join(taskverbs.IndexRowVerbs, ", ") + " — append-only, never deleted."},
		{"gopi worker [<task-id>]", "Runs ONE task row and exits."},
		// NO ROW FOR `gopi merger`, AND THE ABSENCE IS A DECISION (task
		// 01a113a8…, 2026-10-06). The verb is RETIRED (2026-09-19,
		// devdoc/merger-retired.md) and REFUSES at every spelling — `-h`
		// included — so its index row was the one row on this page that could
		// not honour the page's own contract: the section preamble promises
		// that every command listed "accepts -h for its full help", and this
		// one answers a help query with a refusal (measured: `gopi merger -h`
		// → exit 1, no page). The operator's report is exactly that reading:
		// 「merger 既然都不存在来为什么还要提一遍？这不是混淆吗？」 — a
		// front-door row that teaches a dead end.
		//
		// HIDDEN IS NOT GONE, and the split is the point (Rule 15): the WORD
		// still routes, so an old script or service unit gets the retirement
		// sentence naming the surviving face instead of a bare "unknown
		// command" — that half is pinned by cmd/agent/merger_retired_face_test.go,
		// and this half (no index row, no completion candidate) by
		// cmd/agent/cli_help_retired_verb_test.go. A reader who goes looking
		// for the merge surface still learns the retirement from `gopi task -h`,
		// which states it in its own prose.
		//
		// The MACHINE half of this decision is cmd/agent/cli_completion.go's
		// completionSuppressedVerbs — the ONE list that withholds a dispatching
		// verb from the published surfaces, read by the forward guards
		// (cmd/agent/usage_test.go) so re-adding this row is red rather than
		// silently green.
		//
		// NO ROW FOR `gopi merge` EITHER, AND THAT ABSENCE IS THE POINT OF THE
		// merge-removal epic's item 2: the verb no longer dispatches at all, so
		// `gopi merge -h` is not a page a reader can be sent to. The row that
		// used to sit here promised "fast-forwardable branches land, the rest is
		// handed over" — a capability an operator could reach by typing the
		// word. Removal order matters and this is the operator-visible half:
		// THIS page stops asking, then the standalone dispatch goes. The ENGINE
		// (merge_run.go and the merge internals) is deliberately still in the
		// tree and is NOT unreachable: the git atoms run it under the family
		// spelling (`gopi task merge`), which is why this page's `gopi task` row
		// still lists them. Later epic items own the engine's own deletion.
		// `gopi task archive <branch>` is DELIBERATELY ABSENT as its own row
		// (operator ruling, 2026-10-28: "archive 应该在 task 的 subcommand，根本
		// 不应该出现在顶级 help 菜单里"). It is a SUBCOMMAND, and the `gopi task`
		// row immediately above already lists `archive` among the family's
		// subcommands — a second row spelled the same word twice on one page and
		// taught a reader that `archive` competes with `task` rather than living
		// under it. Nothing was withdrawn: the verb still dispatches, and its own
		// page (`gopi task archive -h`, cli_archive.go) is unchanged and states
		// the full flag set with a reason.
		// The negative direction is guarded by
		// cmd/agent/usage_test.go's TestTopLevelHelpDoesNotRepeatTaskSubcommands,
		// so re-adding the row is red rather than silently green.
	}},
	{"Coding & session inspection:", [][2]string{
		{"gopi session <subcommand>", "The session family — list/show plus the log reader (`gopi session -h`)."},
		{"gopi screen <subcommand>", "The BYTES of a recorded screen (`gopi screen replay <id>`; `gopi screen -h`)."},
		// The TOP-LEVEL address of the same reader (cli_screen_replay_verb.go):
		// one command, an operand that may be a session id or a stream file,
		// and `--verify A B` for a frame-by-frame comparison. It is a row and
		// not a footnote on the group's, because the operator's complaint was
		// the HOP (`screen` → `replay`), and a reader who has to learn a family
		// tree to get the bytes has not been given the address.
		// ONE typeable spelling, and the other operand forms described in the
		// description column: an `A|B` shape in the invocation column is the
		// alternative-branch shorthand the help gate refuses, because it is not
		// something an operator can type.
		{"gopi screen-replay <SESSION-ID>", "Recorded render bytes to stdout; the operand may also be a `.screen.stream` path; `--verify A B` diffs two recordings (`gopi screen-replay -h`)."},
		// The NO-VERB resume address (epic resume-surface convergence, step 1):
		// a FLAG row, never a verb — it is the same implementation `gopi session
		// resume` reaches, offered on the bare-prompt invocation the operator was
		// already typing.
		// The summary stays inside TWO wrapped lines on purpose: the top-level
		// page has a hard line budget (usage_test.go,
		// topLevelUsageMaxLines), and a third line here would buy the whole row
		// at another row's expense. The prompt argument and the checkpoint
		// detail are the flag's own page's job (`gopi --session -h`).
		{"gopi --session <id> [\"<prompt>\"]", "Continue an interrupted session from its checkpoint (`gopi --session -h`)."},
		{"gopi tool code-search <symbol>", "Where a symbol is DEFINED (ctags)."},
		{"gopi tool keyword_search <query>", "Full-text search over the repo."},
	}},
	{"Chat & web:", [][2]string{
		{"gopi chat [--tools]", "Interactive multi-turn REPL."},
		{"gopi web [--tools]", "Local browser chat UI (SSE)."},
		// TWO ROWS ARE DELIBERATELY ABSENT HERE: `gopi ui` and `gopi cli`
		// (operator ruling, 2026-10-06, task 01a10dba…). Both verbs still
		// dispatch (isUISubcommand / isTermSubcommand in cmd/agent/main.go) and
		// still answer their own `-h`; they are simply not PUBLISHED on the
		// front door, and tab completion follows this index (cmd/agent/
		// cli_completion.go's completionSuppressedVerbs). The absence is a
		// DECISION, never an omission, so it is guarded in the other direction —
		// re-adding a row fails cmd/agent/cli_help_hidden_verbs_test.go's
		// TestTopLevelHelpHidesUnadvertisedVerbs instead of looking like
		// harmless tidying.
		//
		// UNION RESOLUTION (rework 01a10e11…, 2026-10-06): the branch this
		// index was merged from had taken the other route — the two words
		// retired outright, with no successor — but the ruling above is LATER
		// and it is the one the tree now implements: a hidden verb is still a
		// live verb, so removing it from the dispatch chain would withdraw a
		// command the operator kept, and would delete its `-h` page with it.
		{"gopi ui_state [--pid N] [--json]", "Read a live chat screen as a cell grid."},
		// TWO ROWS DELIBERATELY ABSENT: `gopi web search "<query>"` and
		// `gopi web fetch <url>` (operator ruling, 2026-10-28). Both spellings
		// still dispatch — `gopi web` is the browser chat UI's own carrier AND
		// the group's door to the web TOOLS, both reach the same predicate
		// (isWebSubcommand, cmd/agent/cli_tool.go) — so nothing was withdrawn.
		// What goes away is the FRONT-DOOR ROW: on a page whose neighbouring
		// line is `gopi web [--tools]   Local browser chat UI (SSE)`, a reader
		// cannot tell which `web` they are looking at, and the tools have their
		// own home in the Tools group below (`gopi tool web`, whose description
		// names the two tool words `web_search` / `web_fetch`).
		// Same shape as the `gopi merger` absence documented above: unpublishing
		// an index row is a DECISION, and both directions stay guarded in
		// cmd/agent/usage_test.go.
	}},
	// The underscore spellings vanished from this index with the
	// verb-retirement epic (2026-10-05): every local tool is now reached
	// ONLY as `gopi tool <command>`, so the underscored TOOL names are the
	// commands' own words one level down and the top-level page no longer
	// spells any of them. The rule and its exceptions are unchanged and still
	// live in devdoc/cli-naming-convention.md: a command whose word is an LLM
	// TOOL name keeps that name byte-for-byte (`gopi tool vision_analyze` and
	// the vision_analyze tool call are one string, so a session log's tool_use
	// stays greppable as a command); gopi's own verbs are dash-cased
	// (`code-search`, `scratch-gc`).
	{"Tools (the agent's own tools, from the shell):", [][2]string{
		// ONE line, on purpose: the top-level page is an INDEX, and the budget
		// (`topLevelUsageMaxLines`) says detail belongs on `gopi <cmd> -h`.
		// Every row below is ONE line too, and every tool row now starts with
		// the GROUP word — which is exactly what the census wants: the group is
		// the dispatched verb, and the leaf commands it carries are enumerated
		// by `gopi tool -h`, not by this index.
		{"gopi tool <command>", "Every local tool under one noun (`gopi tool -h`)."},
		{"gopi tool capture", "Capture the LIVE X11 desktop (screen / window / rect)."},
		{"gopi tool screen_capture", "The tool's own spelling: whole screen, `--out` path."},
		{"gopi tool act <action>", "Drive the LIVE X11 desktop: move, click, type."},
		{"gopi tool annotate", "Draw marks onto an EXISTING PNG (measure, IoU)."},
		{"gopi tool gif", "PNG frames → ONE animated GIF."},
		{"gopi tool shot [--url URL|--self]", "Screenshot a URL or the local chat UI, for `vision_analyze`; `--wait-selector` waits for an SPA element."},
		{"gopi tool vision_analyze <image>", "Image understanding / OCR via the vision model."},
		{"gopi tool voice_input <audio>", "Speech to text. BARE records a mic and transcribes it with the `doubao` cloud engine (DEFAULT, uploaded) or `--engine local` (Fun-ASR-Nano); `--server` serves HTTP, `--client` records a mic — `gopi tool voice_input -h`."},
		{"gopi tool text2img <input.txt>", "Render a text file to PNG (pure Go, CJK)."},
		{"gopi tool text2img-verify", "Render the `知` sample at N px (IoU check)."},
		{"gopi tool proxy", "Anthropic/OpenAI protocol proxy."},
		{"gopi tool sandbox <subcommand>", "The ONE standard agent sandbox; `gopi tool sandbox -h`."},
		// The two web TOOLS keep the tool's own spelling here, and the row is
		// ONE line: `web_search`/`web_fetch` are the words the agent's tool
		// calls use, so a session log's tool_use stays greppable as a command
		// and the exception list has an index entry to point at
		// (TestCLINamingExceptionsDocumentedInTopLevelHelp). No `<a|b>`
		// shorthand: the family page (`gopi tool -h`) is where the group's
		// alternation is printed, and this index spells no shape an operator
		// cannot read as one row.
		{"gopi tool web", "`web_search` / `web_fetch` — the web TOOLS."},
		{"gopi tool scratch-gc", "One-shot sweep of the suite scratch home."},
		{"gopi tool ledger-window", "Wall-clock window → epoch bounds."},
	}},
	{"Memory & knowledge:", [][2]string{
		{"gopi memory <subcommand>", "`gopi memory backup` snapshots and curates the ledger."},
		{"gopi memory sync", "One MANUAL session against a declared `memory_peers` peer: unique-only merge, both ways unless `--push`/`--pull` names one."},
		{"gopi memory brew", "Distill lessons from sessions."},
		// `gopi memory sync` HAS A ROW AGAIN (below), and the row's absence
		// between 2026-11-05 and 2026-11-06 is worth keeping in the margin: it
		// went with the cross-node sync family and was restored the next day as a
		// MANUAL door. Its `memsync` sibling is STILL DELETED (it moved rows as a
		// JSON patch file, and no manual door took its place) — that word still
		// routes to a refusal (verbAliases), which is where a reader who types it
		// is caught, because this page lists what RUNS.
		//
		// What remains, and why the subgroup is not shrunk further:
		// `gopi memory backup` still SHIPS a snapshot to a declared peer, and
		// `--serve` is its peer side. That is a file transfer to a host the
		// operator already configured, not a ledger-to-ledger session, so it is
		// not part of the removed family — it is the surviving answer to "get a
		// copy of this ledger onto the other machine".
	}},
	{"Configuration & maintenance:", [][2]string{
		{"gopi config show", "Inspect the effective configuration and its source."},
		{"gopi config setup", "Provision a fresh host."},
		{"gopi config completion <shell>", "Emit a shell tab-completion script (bash, zsh, fish)."},
		{"gopi notify <subcommand>", "Read the task-event notify stream."},
	}},
}

// DebugSections exposes the commandIndex titles for a one-off diagnostic.
func DebugSections() []string {
	var out []string
	for _, s := range commandIndex {
		out = append(out, s.title)
	}
	return out
}

// DebugWrap exposes wrapText for a one-off diagnostic.
func DebugWrap(s string) []string { return wrapText(s, "") }
