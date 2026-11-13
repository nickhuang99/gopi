package config

// help_format_test.go — format contract for the rendered help (2026-09-13
// help-format epic). The epic's complaint was as much about SHAPE as about
// length: the old text was a ragged dump with rows that no longer lined up
// (the env table's %-28s broke on long variable names) and paragraphs where a
// one-line summary belonged. These tests pin the shape next to the renderer.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

// firstField returns a line's first whitespace-separated field ("" when the
// line is blank), used to recognize an env-table entry head.
func firstField(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// TestRenderRowsAlignsSummaries is the alignment oracle: every summary in a
// block starts at the same column, and wrapped continuations indent under the
// summary — never under the name.
func TestRenderRowsAlignsSummaries(t *testing.T) {
	rows := [][2]string{
		{"gopi short", "one two three four"},
		{"gopi a-much-longer-name", "alpha beta"},
	}
	col := helpColumn(rows, 10)
	got := renderRows(rows, col)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one line per row (summaries fit), got %d:\n%s", len(lines), got)
	}
	// Column of the summary = indent + name + pad.
	for i, want := range []string{"one", "alpha"} {
		if got := lines[i][helpRowIndent+col:][:len(want)]; got != want {
			t.Errorf("row %d summary starts at column %d with %q, want %q\n%s", i, helpRowIndent+col, got, want, lines[i])
		}
	}
	// A long summary wraps with the continuation aligned at the same column.
	long := [][2]string{{"gopi x", strings.Repeat("word ", 30)}}
	wrapped := renderRows(long, 20)
	for i, l := range strings.Split(strings.TrimSuffix(wrapped, "\n"), "\n") {
		if i > 0 && !strings.HasPrefix(l, strings.Repeat(" ", helpRowIndent+20)) {
			t.Errorf("continuation line %d is not aligned at the summary column: %q", i, l)
		}
	}
}

// TestHelpColumnGrowsWithContent: the column is derived, so a row wider than
// the floor pushes it out — the property whose absence misaligned the old
// fixed-width env table.
func TestHelpColumnGrowsWithContent(t *testing.T) {
	narrow := [][2]string{{"a", "x"}}
	if got := helpColumn(narrow, commandIndexMinCol); got != commandIndexMinCol {
		t.Errorf("helpColumn(narrow) = %d, want the floor %d", got, commandIndexMinCol)
	}
	wide := [][2]string{{strings.Repeat("n", commandIndexMinCol+10), "x"}}
	if got := helpColumn(wide, commandIndexMinCol); got <= commandIndexMinCol {
		t.Errorf("helpColumn(wide) = %d — an over-wide name must widen the column", got)
	}
}

// TestWrapTextRespectsBudget: wrapped prose never exceeds the wrap column and
// never splits a word (a split env var name or flag would be worse than a long
// line).
func TestWrapTextRespectsBudget(t *testing.T) {
	indent := strings.Repeat(" ", 4)
	text := strings.Repeat("alpha bravo charlie delta echo foxtrot ", 12) + "GOPI_A_VERY_LONG_VARIABLE_NAME"
	for _, l := range wrapText(text, indent) {
		if w := DisplayWidth(l); w > helpWrapCol {
			t.Errorf("wrapped line is %d display cols (> %d): %q", w, helpWrapCol, l)
		}
		if !strings.HasPrefix(l, indent) {
			t.Errorf("wrapped line lost its indent: %q", l)
		}
	}
	// The long token survives intact.
	joined := strings.Join(wrapText(text, indent), "\n")
	if !strings.Contains(joined, "GOPI_A_VERY_LONG_VARIABLE_NAME") {
		t.Errorf("wrapText split a word:\n%s", joined)
	}
}

// TestFormatEnvTableIsAlignedAndBounded: every entry's prose help sits at the
// same column, and no line runs past the wrap budget — including for the
// longest catalog entry, which is exactly what the old fixed-width renderer
// broke on.
func TestFormatEnvTableIsAlignedAndBounded(t *testing.T) {
	table := FormatEnvTable()
	if !strings.Contains(table, "Precedence: CLI flag > env var > settings.json > default.") {
		t.Errorf("--help-env must state the precedence order:\n%s", table)
	}
	byEnv := envByName()
	lines := strings.Split(table, "\n")
	helpLines := 0
	for i, l := range lines {
		// An entry head carries the catalog triple (env / settings key /
		// flag); it is DATA, budgeted by helpHeadMaxCol. Everything else is
		// prose and must wrap inside helpWrapCol.
		fields := strings.Fields(l)
		_, isHead := byEnv[firstField(fields)]
		budget := helpWrapCol
		if isHead {
			budget = helpHeadMaxCol
		}
		if w := DisplayWidth(l); w > budget {
			t.Errorf("--help-env line %d is %d display cols (> %d): %q", i+1, w, budget, l)
		}
		if !isHead && strings.HasPrefix(l, strings.Repeat(" ", envTableHelpCol)) &&
			!strings.HasPrefix(l, strings.Repeat(" ", envTableHelpCol+1)) {
			helpLines++
		}
	}
	if helpLines < len(EnvVars)/2 {
		t.Errorf("only %d prose-help lines for %d catalog entries — the renderer dropped text", helpLines, len(EnvVars))
	}
}

// TestUsageStatesThePromptPositionalRule is the help half of the RP-3 fix
// (end-user-audit slice 8): the --prompt row must state what a text-file
// positional does BESIDE --prompt, because that is the rule cmd/agent's
// readInput follows. It used to say "positionals become files/images", which
// promised the file was task input while the code silently discarded it.
func TestUsageStatesThePromptPositionalRule(t *testing.T) {
	// The help WRAPS its rows, so the rule spans lines; compare on
	// whitespace-collapsed text (the rendered page's own words, unwrapped).
	usage := strings.Join(strings.Fields(FormatUsage()), " ")
	if !strings.Contains(usage, promptPositionalRuleClaim) {
		t.Errorf("gopi -h does not state the --prompt/positional rule %q", promptPositionalRuleClaim)
	}
	old := "positionals become files/images"
	if strings.Contains(usage, old) {
		t.Errorf("gopi -h still carries the drift wording %q — it promised task input the code dropped", old)
	}
}

// TestTopLevelBlocksAreColumnAligned is the regression guard for the half of
// the epic the first pass missed: the Options/Inputs/Usage/Examples blocks were
// still hand-spaced strings, and two rows (--vision-base-url URL,
// --reasoning-effort LEVEL) silently outgrew the literal padding — a ragged
// table in the front-door help. Every block now renders from data through
// renderRows, so each summary starts at the block's own column and continuations
// indent under the summary.
func TestTopLevelBlocksAreColumnAligned(t *testing.T) {
	for _, blk := range []struct {
		name string
		rows [][2]string
		minC int
	}{
		{"Usage", usageRows, usageMinCol},
		{"Inputs", inputRows, inputMinCol},
		{"Options", optionRows, optionMinCol},
		{"Examples", exampleRows, exampleMinCol},
	} {
		col := helpColumn(blk.rows, blk.minC)
		for _, r := range blk.rows {
			nameCol := helpRowIndent + len(r[0])
			if len(r[0])+2*helpRowIndent > col {
				// An over-wide name takes its own line; its summary must
				// then start at the block's column on the following line.
				nameCol = col + helpRowIndent
			}
			rendered := renderRows([][2]string{r}, col)
			first := strings.SplitN(rendered, "\n", 2)[0]
			if len(first) <= nameCol {
				t.Errorf("%s row %q: no summary on the first line:\n%s", blk.name, r[0], rendered)
				continue
			}
			if trimmed := strings.TrimLeft(first[nameCol:], " "); trimmed == "" {
				t.Errorf("%s row %q: summary missing at column %d:\n%s", blk.name, r[0], helpRowIndent+col, first)
			}
		}
		// No row's rendered line may exceed the wrap budget.
		for _, l := range strings.Split(renderRows(blk.rows, col), "\n") {
			if w := DisplayWidth(l); w > helpWrapCol {
				t.Errorf("%s block line is %d display cols (> %d): %q", blk.name, w, helpWrapCol, l)
			}
		}
	}
}

// TestFormatUsageOmitsTheEnvTable: the top-level help is the front door; the
// ~180-line env table belongs to `gopi --help-env` only. Guarded by content
// (an env row's help prose) rather than by line count, so a re-inline fails
// here even if the text is reformatted.
func TestFormatUsageOmitsTheEnvTable(t *testing.T) {
	usage := FormatUsage()
	for _, e := range EnvVars {
		if strings.Contains(usage, e.Env) && strings.Contains(usage, e.Help) {
			t.Errorf("FormatUsage inlines the env-table entry for %s — it belongs to FormatEnvTable (--help-env)", e.Env)
		}
	}
	if !strings.Contains(usage, "--help-env") {
		t.Errorf("FormatUsage must point at `gopi --help-env`")
	}
	// Every knob the option list names must still be discoverable from the
	// table: spot-check the option-list env vars are catalog entries, so the
	// list cannot invent a knob the code does not read.
	byEnv := envByName()
	for _, env := range []string{
		"GOPI_SETTINGS", "GOPI_HOME", "GOPI_VISION_MODEL",
		"GOPI_REASONING_EFFORT", "GOPI_TIMEOUT_SEC", "GOPI_MEMORY_DB",
	} {
		if _, ok := byEnv[env]; !ok {
			t.Errorf("the option list advertises %s, which is not in the EnvVars catalog", env)
		}
	}
}

// inputRowVerbStemPattern matches an Inputs ROW NAME that presents a file-name
// exemplar whose STEM is a command word — the shape that made the page
// contradict itself (`task.txt` advertised as `gopi task.txt`). The capture is
// the stem the dispatcher matches on; the optional `./` is the qualification
// the row uses to keep the token from being read as a command.
var inputRowVerbStemPattern = regexp.MustCompile(`^\.?/?([A-Za-z_][A-Za-z_-]*)\.[A-Za-z]+$`)

// TestInputRowsNameNoCommandWord is the page-side guard for the `gopi task.txt`
// defect (measured 2026-09-26/27: the Inputs table renders its NAME cell
// verbatim as an invocation token, so the row `task.txt` was advertised as
// `gopi task.txt` — and the dispatcher reads the first argument as a command, so
// the advertised token ran the task-LEDGER verb while the row's own prose
// promised "alone it IS the prompt").
//
// TWO ARMS, because either one alone leaves the defect reachable: no Inputs row
// may name a path-less file whose stem is a command word, and the RENDERED page
// may carry no `gopi <word>.<ext>` token at all (the derivation's own shape,
// which a future block could reintroduce through another row).
func TestInputRowsNameNoCommandWord(t *testing.T) {
	for _, r := range inputRows {
		m := inputRowVerbStemPattern.FindStringSubmatch(r[0])
		if m == nil {
			continue // `"<prompt>"`, `@PATH` — not a file-name exemplar
		}
		if positionalVerbSet[m[1]] {
			t.Errorf("the Inputs row name %q is dispatched as the %q COMMAND (config.PositionalVerbs), not read as a file — "+
				"a reader who drops the path prefix gets a command instead of the prompt this row promises; "+
				"path-qualify the exemplar (./%s) or name a file that is not a command word", r[0], m[1], r[0])
		}
	}
	for _, line := range strings.Split(FormatUsage(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != procname.NameGopi {
			continue // not an advertised `gopi …` form line
		}
		// The FIRST non-flag token is the token the dispatcher reads as the
		// command (a flag before it means the run path: `gopi --timeout 600 x`).
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") {
				break
			}
			if m := inputRowVerbStemPattern.FindStringSubmatch(f); m != nil && positionalVerbSet[m[1]] {
				t.Errorf("gopi -h advertises %q: the dispatcher reads %q as the %q COMMAND while the reader reads it as a FILE — "+
					"the advertised token means two different things, and which one wins depends on whether a local file happens to exist",
					line, f, m[1])
			}
			break
		}
	}
}

// TestHelpSurfacesCarryNoInternalJargon is the content guard for the two
// RENDERED surfaces (the AST sweep in cmd/agent covers the subcommand usage
// constants): the env-section titles and the top-level index must not leak the
// project's roadmap into a user's terminal. A milestone number ("M4.7") told
// the reader nothing about the knob and made the help read like a changelog —
// the epic's "careless text" complaint.
func TestHelpSurfacesCarryNoInternalJargon(t *testing.T) {
	for _, jarg := range []*regexp.Regexp{
		regexp.MustCompile(`\bM4\.\d+`),
		regexp.MustCompile(`\bbug\d{3}\b`),
		regexp.MustCompile(`\bP\d\.\d\b`),
		regexp.MustCompile(`\bepic [\x{2460}-\x{2473}]`),
	} {
		for _, surface := range []struct{ name, text string }{
			{"FormatUsage", FormatUsage()},
			{"FormatEnvTable", FormatEnvTable()},
		} {
			if m := jarg.FindString(surface.text); m != "" {
				t.Errorf("%s carries internal roadmap jargon %q — the help must describe behaviour, not the milestone that shipped it", surface.name, m)
			}
		}
	}
}
