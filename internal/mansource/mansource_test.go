package mansource

// mansource_test.go — the parser's own regression guards.
//
// These cases are the shapes that actually went wrong while the gate was being
// built (2026-10-11, task 01a12b36…), not invented examples — each is a
// measured defect in the reader, kept so it cannot come back silently:
//
//  1. `rowHead` took the row's INDENT for the head/description column gap, so
//     every indented row yielded an EMPTY head and the reader found no flag on
//     the help page at all. The reader had to be wrong about the page's own
//     output before it could be right about anything else.
//  2. a row carrying TWO flags draws the second one past the column gap
//     (`  --cols N      / --rows N`), so a head-only vocabulary under-counts
//     the page — the width that reads it is the formatter's, not a preference.
//  3. a MAN source is markdown, so its rows spell a flag inside a backtick
//     span; reading it with the help page's reader finds nothing, and a check
//     that finds nothing is indistinguishable from a check that passes.

import (
	"io"
	"os"
	"strings"
	"testing"
)

// topHelp is a verbatim slice of `gopi -h` — the top-level form: a bare
// `Usage:` header with the rows beneath it.
const topHelp = `GoPi — a tool-using AI coding agent.

Usage:
  gopi [options] "<prompt>"           Run one task from a prompt argument
  gopi [options] input.txt            Run one task read from a text file
  gopi -h | --help                    This help

Commands:
  gopi chat <subcommand>      Talk to the model in a loop.
  gopi config show            Inspect the effective configuration.
  gopi ui_state [--target T]  Read a live screen as a cell grid.
  gopi version                Build version.

Options (flag > env var > settings.json > default):
  --settings PATH                  settings.json path (GOPI_SETTINGS)
  --home PATH                      gopi data root: sessions, raw logs, memory.db,
                                   settings under $GOPI_HOME
  --cols N      / --rows N         the surface geometry
`

// verbHelp is the verb form: the first command shape sits ON the `Usage:` line.
const verbHelp = `Usage: gopi chat [--tools] [--no-editor] [--no-color] [--dump-screen[=PATH]] [chat-ideal-geometry COLSxROWS]
       gopi chat --once [-p|--prompt PROMPT] [--tools] [--turns N]
                        [--max-seconds S]

Start an interactive, multi-turn chat session.

--once, -p [PROMPT]  run ONE turn and exit.
--no-color           no colour or emphasis anywhere.
--turns N, --max-seconds S, --stop-on TEXT
--dump-screen[=PATH]
--no-dump-screen     turn the screen dump OFF for this run.
--chat-ideal-geometry COLSxROWS
                     the chat surface's ideal geometry.
`

// headerlessHelp is the THIRD help form, measured on two real pages of this
// CLI (`gopi merge -h`, `gopi worker -h`): no `Usage:` header at all — the page
// opens on a one-line title and goes straight into an indented block of
// invocations, which is a usage block with nothing naming it as one.
//
// RETIRED SURFACE IN THE FIXTURE, ON PURPOSE (merge verb removal, 2026-11-01):
// `gopi merge -h` no longer resolves to a page — the top-level verb was removed
// and the word refuses with exit 1 — so this literal is now the RECORD of the
// shape it was measured on, kept verbatim because a parser fixture must not
// drift with the surface it was taken from (the second measured page,
// `gopi worker -h`, still supplies the shape live). Nothing here advertises the
// removed verb to a reader: it is an input string for TestHelpParsing, and the
// surviving merge spelling is `gopi task merge` (man/gopi-merge.md).
const headerlessHelp = `GoPi merge — one foreground merge run.

  gopi merge                          one run over every repo in the
                                      settings.json "repos" table
  gopi merge --fast                   git-only run: land and prune.

Notes:
  The ceilings are named constants in merge_run.go.

Why the page is this shape: devdoc/merge-page-background.md.
`

// proseAboutGopiHelp is the line that must NOT be mistaken for a usage row: an
// UNINDENTED sentence that begins with the program's own name. The reader is
// the only thing standing between this page and a block that starts wherever
// prose happens to mention gopi.
const proseAboutGopiHelp = `GoPi version — print the version.

gopi version is the only spelling that prints the artifact's identity, and
nothing here is read from the working directory.

Usage:
  gopi version
`

// tailProseHelp is the shape that walked into the fallback AFTER it shipped:
// a page that opens on its invocations (capability-first, the shape the
// `merge`/`worker` epic requires), keeps its block ABOVE the section headings,
// and then carries prose in the Notes tail which happens to begin — indented —
// with the program's own name. Measured on a probe page (2026-10-11): the
// unbounded fallback accepted that tail line as "the first usage row" and the
// page's synopsis became a sentence.
const tailProseHelp = `GoPi thing — do the thing.

  gopi thing                  do the thing, then exit
  gopi thing --fast           skip the probe

Options:
  --fast             skip the probe

Notes:
  gopi thing --fast is the only spelling that skips the probe.
`

// TestUsageRowsDoesNotReadATailProseRow is guard 5: the headerless scan is
// bounded by the page's FIRST SECTION HEADING, so prose below the sections
// cannot be mistaken for the block. Without the bound this test fails with the
// tail sentence — which is exactly what makes it evidence rather than
// illustration.
func TestUsageRowsDoesNotReadATailProseRow(t *testing.T) {
	got := UsageRows(tailProseHelp)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (the block above `Options:`): %#v", len(got), got)
	}
	for _, row := range got {
		if strings.Contains(row, "only spelling") {
			t.Errorf("the Notes tail's prose row leaked into the usage block: %#v", row)
		}
	}
	if !strings.HasPrefix(got[0], "  gopi thing ") {
		t.Errorf("row 0 = %q — the block's own first line is row 0", got[0])
	}
}

// TestUsageRowsFindsNothingBelowTheSections is the same bound stated as the
// negative: a page whose only `gopi …` row is a tail sentence yields NO rows,
// and the caller reports that loudly rather than quoting a paragraph as a
// synopsis. The bound is what makes "no usage block" a true claim about the
// page instead of a blind spot of the reader.
func TestUsageRowsFindsNothingBelowTheSections(t *testing.T) {
	proseOnly := `GoPi thing — do the thing.

Some prose.

Notes:
  gopi thing --fast is the only spelling that skips the probe.
`
	if got := UsageRows(proseOnly); len(got) != 0 {
		t.Fatalf("a page with no invocation block must yield no rows: got %#v", got)
	}
}

// TestUsageRowsReadsTheHeaderlessForm is guard 4, and it exists because step 2
// of the man epic walked into it: `merge` and `worker` print NO `Usage:` header,
// so a reader that only knew the other two forms returned NO rows for them and
// the gate reported "the synopsis fact source is missing" — a claim about a
// reader, dressed up as a claim about a page. The fallback is asserted here so
// it cannot be removed by someone who only ever tested the other two forms.
func TestUsageRowsReadsTheHeaderlessForm(t *testing.T) {
	got := UsageRows(headerlessHelp)
	if len(got) != 3 {
		t.Fatalf("headerless usage block: got %d rows, want 3: %#v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "  gopi merge ") {
		t.Errorf("row 0 = %q — the block's own first line is row 0, indentation kept", got[0])
	}
	if !strings.Contains(got[2], "git-only run") {
		t.Errorf("row 2 = %q — the block runs to the first blank line", got[2])
	}
}

// TestUsageRowsDoesNotOpenOnProse is guard 4's other half, and it is the
// reason the fallback tests the INDENT and a WORD after the program's name
// rather than a bare prefix: `gopi version is the only spelling …` is a
// sentence, and a block that started there would copy a paragraph into every
// man page's SYNOPSIS. The `Usage:` header wins when the page prints one, so
// the prose above it is never a candidate.
func TestUsageRowsDoesNotOpenOnProse(t *testing.T) {
	got := UsageRows(proseAboutGopiHelp)
	if len(got) != 1 || strings.TrimSpace(got[0]) != "gopi version" {
		t.Fatalf("the `Usage:` header must win over prose mentioning gopi: got %#v", got)
	}
	for _, row := range got {
		if strings.Contains(row, "only spelling") {
			t.Errorf("prose leaked into the usage rows: %#v", row)
		}
	}
}

// TestUsageRowsReadsBothHelpForms is guard 3's sibling for the usage block: the
// two help forms are read the same way, and the `Usage:` token is claimed by
// neither.
func TestUsageRowsReadsBothHelpForms(t *testing.T) {
	top := UsageRows(topHelp)
	if len(top) != 3 {
		t.Fatalf("top-level usage block: got %d rows, want 3: %#v", len(top), top)
	}
	if strings.HasPrefix(top[0], "Usage:") {
		t.Errorf("the `Usage:` token leaked into the rows: %q", top[0])
	}
	if !strings.HasPrefix(top[0], "  gopi [options]") {
		t.Errorf("row 0 = %q — the rows must be the page's own, indentation included", top[0])
	}

	verb := UsageRows(verbHelp)
	if len(verb) != 3 {
		t.Fatalf("verb usage block: got %d rows, want 3 (the `Usage:` line counts as row 0): %#v", len(verb), verb)
	}
	if !strings.HasPrefix(verb[0], "gopi chat [--tools]") {
		t.Errorf("row 0 = %q — the shape on the `Usage:` line itself must be row 0, with the token stripped", verb[0])
	}
	if strings.HasPrefix(verb[1], "Usage:") {
		t.Errorf("row 1 = %q — a continuation line keeps its own indentation", verb[1])
	}
}

// TestHelpFlagTokensCountsFlagsPastTheColumnGap is guard 2: a row's SECOND flag
// is part of the page's vocabulary even though it is drawn past the gap.
func TestHelpFlagTokensCountsFlagsPastTheColumnGap(t *testing.T) {
	got := HelpFlagTokens(topHelp)
	for _, want := range []string{"--settings", "--home", "--cols", "--rows"} {
		if !contains(got, want) {
			t.Errorf("HelpFlagTokens missed %s — the page prints it in a flag row; got %v", want, got)
		}
	}
	if contains(got, "GOPI_SETTINGS") {
		t.Errorf("an env spelling is not a flag: %v", got)
	}
}

// TestFlagTokensReadsOnlyTheRowHead is the head-reader's half of guard 2: it is
// the reader that measures a MAN source's prose rows, where a mention of a
// sibling flag must not count as this page's own.
func TestFlagTokensReadsOnlyTheRowHead(t *testing.T) {
	got := FlagTokens(topHelp)
	if !contains(got, "--settings") {
		t.Errorf("head reader missed --settings: %v", got)
	}
	if contains(got, "--rows") {
		t.Errorf("head reader claimed --rows, which sits past the column gap — that is HelpFlagTokens' scope, not this one's: %v", got)
	}
}

// TestManFlagTokensReadsMarkdownRows is guard 1 + 3: a man source's rows spell
// the flag inside a backtick span, and the row is still a row.
func TestManFlagTokensReadsMarkdownRows(t *testing.T) {
	const md = "# OPTIONS\n\n" +
		"`--once`, `-p [PROMPT]`, `--prompt TEXT`\n" +
		": run ONE turn and exit.\n" +
		"\n" +
		"`--dump-screen[=PATH]`\n" +
		": write a screen dump.\n" +
		"\n" +
		"The synopsis is GENERATED by `make man`; see `--memory-prefetch` for a top-level flag.\n"
	got := ManFlagTokens(md)
	for _, want := range []string{"--once", "--prompt", "--dump-screen"} {
		if !contains(got, want) {
			t.Errorf("ManFlagTokens missed %s — an indented/backticked markdown row is still a row: %v", want, got)
		}
	}
	if contains(got, "--memory-prefetch") {
		t.Errorf("ManFlagTokens claimed a flag only MENTIONED in prose — a mention is not a row head: %v", got)
	}
}

// TestManFlagTokensMeasuresOnlyDefinitionRows pins the NARROWING in both
// directions at once, because each direction alone can be satisfied by a broken
// reader: a reader that measures every flag-smelling line passes the "a row is
// measured" arms, and a reader that measures nothing passes the "a mention is
// not a row" arms. The four shapes below are the shapes the shipped pages
// actually contain, and each is here because it caused a measured false
// positive (man/gopi-session.md / man/gopi-version.md, 2026-10-25).
func TestManFlagTokensMeasuresOnlyDefinitionRows(t *testing.T) {
	const doc = "# OPTIONS\n" +
		// (1) a definition-list row: measured, multi-flag head included.
		"`--once`, `-p [PROMPT]`, `--prompt TEXT`\n" +
		": run ONE turn and exit; the prompt comes from `--prompt`.\n" +
		"\n" +
		// (2) a wrapped CONTINUATION line of a code block / synopsis: it begins
		// with a flag and is prose, not a definition — the `--turn` shape.
		"```\n" +
		"  gopi session raw <id> [round] [part]   → the verbatim raw log\n" +
		"                                            --turns N lists the turns\n" +
		"```\n" +
		"\n" +
		// (3) a PROSE PARAGRAPH that begins with a backticked foreign flag, its
		// sentence running on to the next line — the `--replay` shape.
		"`--replay` **re-runs** the recorded session on the live surface, while\n" +
		"the other verb plays it back.\n" +
		"\n" +
		// (4) a run of TERM lines sharing ONE description, which then names each
		// flag in the helper form — the `gopi session -h` TURNS/REQUEST run.
		"      --turns     one per completed answer, printed by `session log`\n" +
		"      --requests  one JSONL request row\n" +
		": the counts under repl_turns and operator_turns:\n" +
		"        --turns N     one per completed answer\n" +
		"        --requests N  one per request\n" +
		"\n" +
		// (5) a term line that FOLLOWS a description block rather than preceding
		// one — the `gopi worker` detached-faces shape.
		"`--once`\n" +
		": run a single round.\n" +
		"`--daemon`, `--status`\n" +
		": the detached faces.\n"
	got := ManFlagTokens(doc)

	for _, want := range []string{"--once", "--prompt", "--turns", "--requests", "--daemon", "--status"} {
		if !contains(got, want) {
			t.Errorf("ManFlagTokens missed %s — it IS a documented flag spelling (a definition term, or a helper of one): %v", want, got)
		}
	}
	if contains(got, "--replay") {
		t.Errorf("ManFlagTokens claimed --replay — it is only NAMED in prose, which is not a claim to list it: %v", got)
	}
	// The continuation line carries no flag marker line of its own, so NO token
	// read off it may appear: `--turns` reaches the result only through (4),
	// never through the wrapped synopsis line — measured by removing (4).
	const withoutRun = "`--once`\n: run ONE turn.\n" +
		"```\n  gopi session raw <id>   → the raw log\n                            --turns N lists the turns\n```\n" +
		"`--replay` **re-runs** the recorded session on the live surface.\n"
	if got := ManFlagTokens(withoutRun); len(got) != 1 || got[0] != "--once" {
		t.Errorf("a document whose ONLY definition row is --once reported %v — a wrapped continuation or a prose paragraph opening with a flag must not be measured", got)
	}
}

// TestManFlagTokensReportsWhatItDeclinesToMeasure is the Rule 15 half of the
// narrowing: a line that BEGINS a flag row and is yet measured for no reason is
// an ambiguous document, and a reader that drops it silently makes the next
// false negative look exactly like a clean page. The notice names the line.
func TestManFlagTokensReportsWhatItDeclinesToMeasure(t *testing.T) {
	const doc = "`--orphan` says something about a flag with no definition under it.\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	got := ManFlagTokens(doc)
	os.Stderr = prev
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	msg, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("an unpaired flag-smelling line was measured as a listed flag: %v", got)
	}
	if !strings.Contains(string(msg), "--orphan") || !strings.Contains(string(msg), "not measured") {
		t.Errorf("the reader declined to measure a flag-smelling line WITHOUT reporting it — a silent drop is indistinguishable from a clean page (Rule 15); stderr was %q", string(msg))
	}
}

// TestExtractBlockRefusesAMalformedRegion is the Rule 15 half: a region whose
// markers are missing or out of order is an ERROR, never an empty body. An
// empty body compares equal to nothing and would read as "no drift found".
func TestExtractBlockRefusesAMalformedRegion(t *testing.T) {
	const ok = "intro\n" + "BEGIN\nbody\n" + "END\noutro\n"
	body, err := ExtractBlock(ok, "BEGIN", "END")
	if err != nil {
		t.Fatalf("a well-formed region: %v", err)
	}
	if strings.TrimSpace(body) != "body" {
		t.Errorf("body = %q, want %q", body, "body")
	}
	for name, doc := range map[string]string{
		"no markers":     "intro\nbody\n",
		"begin only":     "BEGIN\nbody\n",
		"end only":       "body\nEND\n",
		"end before beg": "END\nbody\nBEGIN\n",
	} {
		if _, err := ExtractBlock(doc, "BEGIN", "END"); err == nil {
			t.Errorf("%s: ExtractBlock returned no error — a vanished region must not read as an empty one", name)
		}
	}
	// A region with its markers ADJACENT parses to an empty body and NO error,
	// and that division of labour is deliberate: the parser cannot know what a
	// region is supposed to contain, so "empty" is the CONSUMER's refusal. Both
	// consumers here make it (manSourceDisagreements refuses an empty synopsis
	// as "the fact source is missing", and the verb index as "an empty region
	// is not a passing comparison") — pinned by TestManSourcesMatchTheirHelpPage.
	if body, err := ExtractBlock("BEGIN\nEND\n", "BEGIN", "END"); err != nil {
		t.Errorf("markers adjacent: %v (the parser must not decide what a region contains)", err)
	} else if strings.TrimSpace(body) != "" {
		t.Errorf("markers adjacent: body = %q, want empty", body)
	}
}

// TestReplaceBlockRoundTrips pins the writer/reader pair: what ReplaceBlock
// writes is what ExtractBlock reads, so the generator and the gate cannot
// disagree about the region.
func TestReplaceBlockRoundTrips(t *testing.T) {
	const doc = "intro\nBEGIN\nold\nEND\noutro\n"
	out, err := ReplaceBlock(doc, "BEGIN", "END", "new\n")
	if err != nil {
		t.Fatalf("ReplaceBlock: %v", err)
	}
	if !strings.Contains(out, "intro\n") || !strings.Contains(out, "outro\n") {
		t.Errorf("ReplaceBlock touched text outside the region:\n%s", out)
	}
	body, err := ExtractBlock(out, "BEGIN", "END")
	if err != nil {
		t.Fatalf("ExtractBlock of the writer's output: %v", err)
	}
	if strings.TrimSpace(body) != "new" {
		t.Errorf("round trip body = %q, want %q", body, "new")
	}
	// Idempotence is what makes `make man` safe to run twice.
	again, err := ReplaceBlock(out, "BEGIN", "END", "new\n")
	if err != nil {
		t.Fatalf("second ReplaceBlock: %v", err)
	}
	if again != out {
		t.Errorf("ReplaceBlock is not idempotent — a second run would rewrite the document:\n%s", again)
	}
}

// TestHelpVerbsReadsIndexRows pins the verb reader against the shape the help
// page actually prints, including a verb whose row carries a subcommand.
func TestHelpVerbsReadsIndexRows(t *testing.T) {
	got := HelpVerbs(topHelp)
	want := []string{"chat", "config", "ui_state", "version"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("HelpVerbs = %v, want %v (sorted, deduplicated, subcommand rows included)", got, want)
	}
}

// subcommandHelpFixture is a verb page whose SUBCOMMAND rows are the shape that
// produced a false "invented flag" (2026-10-30, man/gopi-session.md:155): the
// verb's page names its subcommands and one sentence about each, and the flag
// ROWS those subcommands own are printed one page further down. It also carries
// a row of ANOTHER verb, so the test can prove the verb argument is re-checked
// and not assumed from the page's identity, plus a metavariable row
// (`<branch>`) that must not be read as a subcommand at all.
const subcommandHelpFixture = `GoPi session — inspect sessions.

Usage:
  gopi session gc [--apply] [--older-than DAYS]
  gopi session list [--kind KIND]
  gopi merge <branch> [<branch>...]

Notes:
    gopi memory search "<query>"      another verb's row, quoted in prose
`

// TestHelpSubcommandsReadsOnlyThisVerbsSubcommandRows is the regression guard
// for the reach defect the man gate measured on 2026-10-30.
//
// THE DEFECT. A flag vocabulary built from the top-level page plus each verb's
// own page cannot see a SUBCOMMAND's flags, because a verb page prints its
// subcommands' invocations and not their definition rows — `--trim-raw-log` is
// a definition row on `gopi session gc -h` and is never drawn as one on
// `gopi session -h`. The gate then reported a TRUE man sentence as an invented
// flag, and its message ("no `-h` page of this CLI prints it") was false about
// the binary while being true about the reader. The refusal was correct; the
// vocabulary's REACH was not.
func TestHelpSubcommandsReadsOnlyThisVerbsSubcommandRows(t *testing.T) {
	got := HelpSubcommands(subcommandHelpFixture, "session")
	want := []string{"gc", "list"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("HelpSubcommands(session) = %v, want %v — sorted, deduplicated, and the metavariable row (`<branch>`) is not a command word", got, want)
	}

	// The verb is re-checked, not assumed from the page: a page that quotes a
	// SIBLING verb's row contributes nothing to this verb's subcommand set.
	if other := HelpSubcommands(subcommandHelpFixture, "memory"); len(other) != 0 {
		t.Errorf("HelpSubcommands(memory) = %v on a page that only QUOTES a memory row — a mention is not a subcommand of the memory verb", other)
	}

	// An unknown verb reads as empty rather than as everything.
	if unknown := HelpSubcommands(subcommandHelpFixture, "not-a-verb"); len(unknown) != 0 {
		t.Errorf("HelpSubcommands(not-a-verb) = %v, want empty", unknown)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// reportFixture is a small man source exercising all three shapes the report
// distinguishes: a real definition row (measured), a PROSE line that begins
// with a flag (declined, no generated block), and a wrapped continuation line
// INSIDE a generated region — the two shapes the reader was narrowed for
// (man/gopi-session.md, 2026-10-25) plus the one it measures.
const reportFixture = "# NAME\n" +
	"\n" +
	"gopi-demo - a fixture\n" +
	"\n" +
	"# SYNOPSIS\n" +
	"\n" +
	"<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->\n" +
	"\n" +
	"```\n" +
	"  gopi demo [--once] --mode M\n" +
	"      --turn N prints one in full\n" +
	"```\n" +
	"\n" +
	"<!-- END SYNOPSIS -->\n" +
	"\n" +
	"# OPTIONS\n" +
	"\n" +
	"`--once`\n" +
	": run a single turn and exit\n" +
	"\n" +
	"`--peer` ships the same request to another host.\n" +
	"\n"

// TestManFlagReportCarriesTheDeclinedLines is the REPORT half of the Rule 15
// trace: the same two lines the reader refuses to measure are returned as data,
// with their LINE NUMBERS and with whether a generated block already accounts
// for them.
//
// WHY THE LINE NUMBER IS PART OF THE CONTRACT. A gate that has to name the line
// it refuses cannot do it from the text alone — the same paragraph can occur
// twice in a page, and a reader told "somewhere in this document" has been
// handed a search problem instead of a location. The number is the difference
// between a report and a rumour.
func TestManFlagReportCarriesTheDeclinedLines(t *testing.T) {
	report := ManFlagReport(reportFixture)

	if !contains(report.Tokens, "--once") {
		t.Errorf("the definition row was not measured: %v", report.Tokens)
	}
	lines := make(map[int]UnmeasuredLine, len(report.Unmeasured))
	for _, u := range report.Unmeasured {
		lines[u.Line] = u
	}
	if len(lines) != 2 {
		t.Fatalf("want exactly the two declined lines, got %d: %+v", len(lines), report.Unmeasured)
	}

	// Line 11 is the wrapped continuation of the generated synopsis: a real
	// declined line, and STRUCTURALLY accounted for by the block it sits in.
	gen, ok := lines[11]
	if !ok {
		t.Fatalf("the wrapped synopsis continuation (line 11) is missing from the report: %+v", report.Unmeasured)
	}
	if !gen.InGeneratedBlock || gen.Block != "SYNOPSIS" {
		t.Errorf("line 11 is inside the generated SYNOPSIS block, reported as block=%q inBlock=%v — a caller cannot tell a structurally-checked line from prose without this", gen.Block, gen.InGeneratedBlock)
	}
	if !strings.Contains(gen.Text, "--turn N prints one in full") {
		t.Errorf("line 11's text is %q — the report must carry the line a reader would look at", gen.Text)
	}

	// Line 21 is prose: no block covers it, so a caller must judge it.
	prose, ok := lines[21]
	if !ok {
		t.Fatalf("the prose flag row (line 21) is missing from the report: %+v", report.Unmeasured)
	}
	if prose.InGeneratedBlock || prose.Block != "" {
		t.Errorf("line 21 is PROSE, reported as block=%q inBlock=%v — mislabelling prose as generated is how an ungraded line hides", prose.Block, prose.InGeneratedBlock)
	}
	if !strings.Contains(prose.Text, "--peer ships the same request") {
		t.Errorf("line 21's text is %q", prose.Text)
	}
}

// TestManFlagReportKeepsTheStderrNotice is the NON-REGRESSION half: the report
// is an addition to the Rule 15 trace, never a replacement. A maintainer
// running the gate by hand (or `go test -v`) still sees the notice, and a caller
// that collects the report does not have to trust a channel it cannot read.
func TestManFlagReportKeepsTheStderrNotice(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	report := ManFlagReport(reportFixture)
	os.Stderr = prev
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	msg, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := strings.Count(string(msg), "not measured")
	if got != len(report.Unmeasured) {
		t.Errorf("stderr carried %d notices for %d reported lines — the trace and the report must describe the same set, or one of them is lying:\n%s",
			got, len(report.Unmeasured), string(msg))
	}
}

// TestManFlagReportAgreesWithManFlagTokens pins that the report did not fork
// the reader: ManFlagTokens is the report's Tokens, so a caller cannot measure
// one answer and grade another (Rule 13).
func TestManFlagReportAgreesWithManFlagTokens(t *testing.T) {
	report := ManFlagReport(reportFixture)
	plain := ManFlagTokens(reportFixture)
	if strings.Join(report.Tokens, " ") != strings.Join(plain, " ") {
		t.Errorf("ManFlagTokens = %v but the report measured %v — two readings of one document", plain, report.Tokens)
	}
}

// TestUnmeasuredLineCarriesTheFlagsItNames is what makes a REASON pay for
// itself: a declined line is still a sentence naming a flag, so the report must
// carry WHICH flags — otherwise a caller that excuses the line has excused an
// arbitrary spelling, including one no page of the CLI prints (the
// invented-flag defect, escaping through the classification door).
func TestManFlagReportCarriesTheFlagsEachLineNames(t *testing.T) {
	report := ManFlagReport(reportFixture)
	var prose *UnmeasuredLine
	for i, u := range report.Unmeasured {
		if u.Line == 21 {
			prose = &report.Unmeasured[i]
			break
		}
	}
	if prose == nil {
		t.Fatalf("the prose flag row (line 21) is missing from the report: %+v", report.Unmeasured)
	}
	if !contains(prose.Flags, "--peer") {
		t.Errorf("line 21 names --peer, reported as Flags=%v — a caller cannot check what it is excusing", prose.Flags)
	}
	if contains(prose.Flags, "--once") {
		t.Errorf("the prose line's Flags=%v carries the DEFINITION row's flag — the report mixed two lines", prose.Flags)
	}
	for _, u := range report.Unmeasured {
		if len(u.Flags) == 0 {
			t.Errorf("line %d is in the report with no flags — it was declined for no reason, which is the opposite of what the report means: %+v", u.Line, u)
		}
	}
}

// TestGeneratedBlockAtClassifiesEveryRegionShape pins the block classifier
// against the marker forms the man sources really carry: this package's own
// SYNOPSIS/VERB INDEX constants, and a region ANOTHER package writes
// (IMAGE SELF-CHECK, cmd/agent) whose name carries a qualifier and a path.
func TestManFlagReportClassifiesEveryGeneratedRegionShape(t *testing.T) {
	lines := strings.Split(reportFixture+
		"\n<!-- BEGIN IMAGE SELF-CHECK (verbatim mergeHelpImageSelfCheckNote, cmd/agent/cli_tasks_merger_help.go) -->\n"+
		"body\n"+
		"<!-- END IMAGE SELF-CHECK -->\n"+
		"tail prose\n", "\n")

	cases := []struct {
		line  int
		block string
		in    bool
	}{
		{line: 8, block: "SYNOPSIS", in: true},          // inside the synopsis fence
		{line: 19, block: "", in: false},                // between regions: prose
		{line: 24, block: "IMAGE SELF-CHECK", in: true}, // a foreign region, by pattern
		{line: 26, block: "", in: false},                // after its END: prose again
	}
	for _, c := range cases {
		block, in := generatedBlockAt(lines, c.line)
		if block != c.block || in != c.in {
			t.Errorf("line %d: generatedBlockAt = (%q,%v), want (%q,%v)", c.line, block, in, c.block, c.in)
		}
	}

	// A BEGIN with no END reports the region as OPEN rather than as prose: the
	// document is broken, and calling its lines "prose nobody generated" would
	// blame the author for the generator's damage (Rule 15).
	open := strings.Split("<!-- BEGIN SYNOPSIS -->\nbody\n", "\n")
	if block, in := generatedBlockAt(open, 2); !in || block != "SYNOPSIS" {
		t.Errorf("an unterminated region read as (%q,%v) — want it reported as an open SYNOPSIS region", block, in)
	}
}
