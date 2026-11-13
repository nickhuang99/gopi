// cli_session_help_surface_basic_test.go — G4 of the help-surface hardening
// briefing (briefings/brief-help-surface-hardening.md): the session-inspection
// + resume group — `gopi session log`, `gopi session stats`, `gopi session raw`, `gopi session checkpoint
// list|export`, `gopi session resume` (incl. `--task`).
//
// WHY THIS FILE EXISTS. The family's motivating defect (G1) was a surface that
// was implemented, compiled, unit-tested, reported complete and NEVER TYPED
// (`gopi task sync --push-only --task-id T`), found by one real invocation
// (PROJECT.md Rule 31). A suite that enters through a seam the author chose cannot
// see that class, so every case below drives the REAL BINARY over the help's
// OWN argv — the spelling an operator types — against a sandboxed GOPI_HOME and
// a REAL session fixture (`tests/fixtures/log-stats-raw`, the same corpus the
// package's log/stats/raw goldens are recorded from). No constructed session:
// the fixture is a genuine session log plus its paired raw HTTP log.
//
// THE SURFACE LIST IS DERIVED, NOT AUTHORED. `g4AdvertisedRows` re-derives it
// from the SAME raw-string usage constants the binary prints (logUsage /
// statsUsage / rawUsage / checkpointUsage / resumeUsage), and
// TestG4HelpSurfaceMatchesInventory requires the derivation, the pinned
// expectation and the claimed coverage to agree — so a synopsis added to a
// usage constant with no row and no case fails the build instead of leaving the
// inventory (devdoc/gopi-session-help-surface-inventory.md) describing last
// month's help. The doc's own table is read back from disk and compared with
// the same derivation (TestG4HelpSurfaceInventoryTableMatchesHelp).
//
// SECTIONS ARE PART OF THE CLAIM. These pages carry invocations in three
// places — `Usage:` (the canonical surface), `Flags can be combined:` (worked
// combinations) and `Examples:` (the resume examples, which is where `--task`
// is spelled) — and they are not the same promise, so the section travels in
// the row and in the table.
//
// Rule 17: the in-process halves use t.Setenv. Rule 22/32: every directory the
// probes use comes from t.TempDir() (the harness's pinned scratch root) — this
// file never spells a temp root itself. Rule 31: each row's argv is executed
// for real, with the observed stdout/exit asserted.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devdoc"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/tools"
)

// g4VerbTimeout bounds ONE probe's child. A CLI routing regression that
// dropped an argument into the agent loop would hang rather than fail, so a
// probe carries its own ceiling (Rule 1: this is the named constant).
const g4VerbTimeout = 30 // seconds

// g4FixtureSessionID / g4FixtureRawLogName name the REAL session the probes
// run against — the corpus `tests/fixtures/log-stats-raw` already uses for the
// log/stats/raw goldens (cli_logstats_fixture_test.go constants).
const (
	g4FixtureSessionID  = statsFixtureSessionID
	g4FixtureRawLogName = statsFixtureRawLogName
)

// --- the surface list, re-derived from the help -------------------------------

// g4Row is one advertised surface: one synopsis line of one of the group's help
// pages, with the section it was printed under and the invocation verbatim.
type g4Row struct {
	verb       string // the command word the invocation names (log/stats/raw/checkpoint/resume)
	section    string // "Usage", "Flags can be combined" or "Examples"
	invocation string // the invocation text, description column cut
	flags      []string
}

// g4Page is one help page of the group: its page word (which is also the argv
// that prints it) and the usage constant the binary prints from it.
type g4Page struct {
	word  string
	usage string
}

// g4Pages is the group, read from the constants (never from `-h` output, so the
// derivation runs in-process with no binary).
//
//nolint:gochecknoglobals // the group under test, read by the derivation and the gate
var g4Pages = []g4Page{
	{CliVerbLog, logUsage},
	{CliVerbStats, statsUsage},
	{CliVerbRaw, rawUsage},
	{CliVerbCheckpoint, checkpointUsage},
	{CliVerbResume, resumeUsage},
}

// g4InvocationRe matches a synopsis line: two-space indent, `gopi <verb> …`.
//
// The address is the FAMILY one (`gopi session log`), because verb-retirement
// phase 1 moved the four session verbs under their group and turned the flat
// spelling into a named refusal — every synopsis on these five pages was
// rewritten to it, so matching the old bare form found no line at all and the
// derivation went blind (measured: "only 0 checkpoint Usage line(s) were
// inspected").
var g4InvocationRe = regexp.MustCompile(`^  gopi ` + CliVerbSession + ` (log|stats|raw|checkpoint|resume) `)

// g4SectionRe matches a section header: a bare `Words:` title at column 1.
var g4SectionRe = regexp.MustCompile(`^[A-Za-z(][^ ].*:$`)

// g4PageRows derives every advertised invocation of ONE page, in help order,
// deduplicated.
//
// THE CUT IS THE DESCRIPTION COLUMN, which on all five pages opens with two or
// more spaces (an invocation never contains a double space — `--json | jq
// .prompt_tokens` is the widest test of that claim). Two refinements the real
// pages force:
//
//   - a shell PIPE ends the invocation: `gopi session stats <id> --json | jq
//     .prompt_tokens` is an EXAMPLE whose second half is not argv, so the
//     invocation is cut at ` | ` before the column cut;
//   - a page that breaks its own column rule produces a line whose argv and
//     prose run together (`cli_log.go`'s `--request --json Full first request
//     body …` had a single space where every sibling line has a column). Such a
//     line is a HELP defect, not something the derivation should paper over —
//     it is fixed on the page, and the scanner has no special case for it.
//
// The VERB comes from the line itself, not from the page: `checkpointUsage`
// prints the `gopi session resume <log> [continuation...]` synopsis (the two commands
// share one page), so attributing by page would file the resume synopsis under
// `checkpoint` — and the checkpoint page's own second synopsis line
// (`gopi session checkpoint export <log> [--dir DIR]`, indented four spaces) proves the
// page geometry is not a reliable verb source either.
func g4PageRows(p g4Page) []g4Row {
	var out []g4Row
	seen := map[string]bool{}
	section := "Usage"
	for _, line := range strings.Split(p.usage, "\n") {
		if m := g4SectionRe.FindString(line); m != "" {
			section = strings.TrimSuffix(m, ":")
			continue
		}
		if !g4InvocationRe.MatchString(line) {
			continue
		}
		inv := g4CutInvocation(line)
		if seen[inv] {
			continue
		}
		seen[inv] = true
		out = append(out, g4Row{
			verb:       g4InvocationVerb(inv),
			section:    section,
			invocation: inv,
			flags:      g4InvocationFlags(inv),
		})
	}
	return out
}

// g4CutInvocation cuts a synopsis line at its description column and trims the
// invocation. A shell pipe (the worked examples' `| jq …`) ends it first: that
// half of the line is prose about the example, not argv.
func g4CutInvocation(line string) string {
	if i := strings.Index(line, " | "); i >= 0 {
		line = line[:i]
	}
	cut := 0
	for i := 17; i < len(line)-1; i++ { // the invocation column starts at 2
		if line[i] == ' ' && line[i+1] == ' ' {
			cut = i
			break
		}
	}
	inv := line
	if cut > 0 {
		inv = line[:cut]
	}
	inv = strings.TrimSpace(inv)
	// A synopsis line's DESCRIPTION column opens at or past column 18. A longer
	// invocation reaches past it, so the first double space lands INSIDE the
	// argv — the derivation must not read prose as argv, and the prose must not
	// silently join the invocation. The invocation column stays a column: this
	// is the case the `gopi session log --worktree WT --todo` line forced (its argv ends
	// before column 18, so the cut is correct there), and it is stated rather
	// than special-cased.
	return strings.TrimSpace(strings.SplitN(inv, "  ", 2)[0])
}

// g4InvocationVerb returns the command word an invocation names.
func g4InvocationVerb(inv string) string {
	f := strings.Fields(inv)
	// The invocation opens with the PROGRAM word, then — since verb-retirement
	// phase 1 — the FAMILY word the five verbs moved under (`gopi session log`).
	// The row's verb is the word after BOTH; reading it off the page (rather than
	// assuming it is the page's own verb) is what keeps this derivation able to
	// notice a page that advertises a sibling.
	if len(f) > 0 && f[0] == gopiProgramWord {
		f = f[1:]
	}
	if len(f) > 0 && f[0] == CliVerbSession {
		f = f[1:]
	}
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// g4InvocationFlags returns every `--flag` an invocation spells.
func g4InvocationFlags(inv string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`--[a-z][a-z-]*`).FindAllString(inv, -1) {
		f := strings.TrimPrefix(m, "--")
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// g4AdvertisedRows is every row the group's help advertises, in help order.
func g4AdvertisedRows() []g4Row {
	var out []g4Row
	for _, p := range g4Pages {
		out = append(out, g4PageRows(p)...)
	}
	return out
}

// g4RowKey keys a row the way the checked-in table and the covered set do:
// "verb<TAB>section<TAB>invocation".
func g4RowKey(r g4Row) string {
	return r.verb + "\t" + r.section + "\t" + r.invocation
}

// g4DerivedKeys returns the derived surface as sorted keys.
func g4DerivedKeys() []string {
	var out []string
	for _, r := range g4AdvertisedRows() {
		out = append(out, g4RowKey(r))
	}
	sort.Strings(out)
	return out
}

// g4CoveredKeys is the inventory's COVERED set: every derived row has a case —
// either the generic `g4SurfaceCases` entry, or one of the rows in
// `g4DispatchedElsewhere`, which are proven by a DEDICATED basic test rather
// than by this file's generic runner. Both are membership in a real case set, so
// a row can never be "covered" by a test that does not exist (removing a case
// without removing its row fails TestG4CoveredSetMatchesCases).
func g4CoveredKeys() []string {
	known := map[string]bool{}
	for _, r := range g4AdvertisedRows() {
		known[g4RowKey(r)] = true
	}
	var out []string
	for k := range g4SurfaceCases {
		if !known[k] {
			continue
		}
		out = append(out, k)
	}
	for k := range g4DispatchedElsewhere {
		if !known[k] {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// g4DispatchedElsewhere names advertised rows whose proof lives in a DEDICATED
// basic test instead of this file's generic runner, with the test that proves
// them. The entry is a CLAIM: TestG4DispatchedElsewhereTestsExist checks the
// named test function is defined in this package, so the row cannot be "covered"
// by a sentence.
//
// IT IS EMPTY, and the row that used to be here is the reason: the
// `gopi session checkpoint resume <log> [continuation...]` synopsis was the one entry,
// and it is GONE FROM THE PAGE (epic verb-restructure, 2026-10-05) — the
// sub-verb was REMOVED as a duplicate of `gopi session resume`, so there is no
// advertised invocation left to cover. Its proof is now the RETIREMENT contract,
// TestCheckpointResumeLineIsRefusedByName, which drives the real binary and
// asserts the word still ARRIVES at this dispatcher and is refused BY NAME (the
// opposite of the pre-fix defect this table's old entry existed for: a page that
// printed a line the dispatcher refused).
//
// The DELETE is deliberate rather than a silently emptied map: an entry naming a
// test for a row no longer derived would fail TestG4DispatchedElsewhereTestsExist
// only if the test vanished, and would leave a claim about a surface that does
// not exist.
//
//nolint:gochecknoglobals // the coverage declaration, read by the gates
var g4DispatchedElsewhere = map[string]string{}

// g4ExpectedRows is the help surface this file was written against. It is
// pinned so a usage-constant edit is a DELIBERATE two-place change (the help
// and this list), and so a scanner regression that silently drops rows cannot
// pass as "everything still agrees".
//
//nolint:gochecknoglobals // the pinned expectation, read by the gate
var g4ExpectedRows = []string{
	"log	Usage	gopi session log <session-id>",
	"log	Usage	gopi session log <session-id> <round>",
	"log	Usage	gopi session log <session-id> <round> --request",
	"log	Usage	gopi session log <session-id> <round> --response",
	"log	Usage	gopi session log <session-id> <round> --thinking",
	"log	Usage	gopi session log <session-id> <round> --filter TOOL",
	// `gopi session log <session-id> --rounds` USED TO BE HERE and is
	// deliberately gone (58ec82b96, "对外单位统一为 turn，round 退为内部概念"):
	// the log group no longer advertises a rounds view at all — its own table is
	// `--turns` — so a row expecting the old advertisement is a stale
	// expectation, not a missing feature. The FLAG still parses, though: the same
	// commit kept `logFlagRounds` in `logFlagMember` and left the page saying its
	// flag is "a compatibility alias, no longer listed above" (measured:
	// `gopi session log --last --rounds` prints the ROUND table, exit 0). So this
	// row is deleted because the ADVERTISEMENT is gone, not because the parse is —
	// restoring the synopsis would contradict the page's own sentence.
	// `stats --rounds` and `raw --rounds` are untouched — those two groups still
	// advertise it and their cases below are correct.
	//
	// THIS ROW WAS RE-ADDED TO THE HELP ONCE, BY A MERGE HUSK (measured
	// 2026-10-25). The line had been deleted on two lines of history (58ec82b96
	// and 13ddb5c) while the parallel turn-is-the-unit branch 74ec430 was cut
	// from a base that still had it and never deleted it; merge 966cd4d resolved
	// cli_log.go to that side, so the Usage line returned and the three G4 gates
	// went red at "88 advertised, 87 expected". The deletion is the decision; a
	// merge conflict on a raw-string constant is not a re-decision.
	"log	Usage	gopi session log <session-id> --turns",
	"log	Usage	gopi session log <session-id> --toolcall N",
	"log	Usage	gopi session log <session-id> --response",
	"log	Usage	gopi session log <session-id> --thinking",
	"log	Usage	gopi session log <session-id> --request",
	"log	Usage	gopi session log <session-id> --request --json",
	"log	Usage	gopi session log <session-id> --chat",
	"log	Usage	gopi session log <session-id> --todo",
	"log	Usage	gopi session log <session-id> --memory",
	"log	Usage	gopi session log <session-id> --context-trim",
	"log	Usage	gopi session log <session-id> --search",
	"log	Usage	gopi session log <session-id> --web",
	"log	Usage	gopi session log <session-id> --job",
	"log	Usage	gopi session log <session-id> --context",
	"log	Usage	gopi session log <session-id> --vision",
	"log	Usage	gopi session log <session-id> --text2img",
	"log	Usage	gopi session log <session-id> --git",
	"log	Usage	gopi session log <session-id> --filter TOOL",
	"log	Usage	gopi session log <session-id> --grep TEXT",
	"log	Usage	gopi session log <session-id> --json",
	"log	Usage	gopi session log <session-id> --text",
	"log	Usage	gopi session log --last",
	"log	Usage	gopi session log --last N",
	"log	Usage	gopi session log --last N --memory",
	"log	Usage	gopi session log --branch BRANCH",
	"log	Usage	gopi session log --all --summary",
	"log	combines with them exactly as it does with the id	gopi session log --branch BRANCH --response",
	"log	combines with them exactly as it does with the id	gopi session log --worktree WT --todo",
	"log	Flags can be combined	gopi session log <id> --response --text",
	"log	Flags can be combined	gopi session log <id> --thinking --text",
	"log	Flags can be combined	gopi session log <id> --request --text",
	"log	Flags can be combined	gopi session log <id> --request --json",
	"log	Flags can be combined	gopi session log <id> --memory --text",
	"log	Flags can be combined	gopi session log <id> --context-trim",
	"log	Flags can be combined	gopi session log <id> --filter bash",
	"log	Flags can be combined	gopi session log <id> --filter read --filter write",
	"log	Flags can be combined	gopi session log <id> --search",
	"log	Flags can be combined	gopi session log <id> --web",
	"log	Flags can be combined	gopi session log <id> --vision",
	"log	Flags can be combined	gopi session log <id> --text2img",
	"log	Flags can be combined	gopi session log <id> --git",
	"log	Flags can be combined	gopi session log <id> --grep timeout",
	"log	Flags can be combined	gopi session log <id> --grep timeout --job",
	"log	Flags can be combined	gopi session log --last --chat",
	"stats	Usage	gopi session stats <session-id>",
	"stats	Usage	gopi session stats <session-id> --rounds",
	"stats	Usage	gopi session stats <session-id> --tools",
	"stats	Usage	gopi session stats <session-id> --bytes",
	"stats	Usage	gopi session stats <session-id> --cost",
	"stats	Usage	gopi session stats <session-id> --bill",
	"stats	Usage	gopi session stats <session-id> --json",
	"stats	Usage	gopi session stats --branch BRANCH",
	"stats	Usage	gopi session stats --last [N]",
	"stats	Usage	gopi session stats --all",
	"stats	Flags can be combined	gopi session stats <id> --rounds --json",
	"stats	Flags can be combined	gopi session stats <id> --rounds --bytes",
	"stats	Flags can be combined	gopi session stats <id> --cost --rounds",
	"stats	Flags can be combined	gopi session stats --last 5 --tools",
	"stats	Flags can be combined	gopi session stats --all --tools",
	"stats	Flags can be combined	gopi session stats <id> --json",
	"raw	Usage	gopi session raw <session-id>",
	"raw	Usage	gopi session raw <session-id> <round>",
	"raw	Usage	gopi session raw <session-id> <round> request",
	"raw	Usage	gopi session raw <session-id> <round> response",
	"raw	Usage	gopi session raw <session-id> --rounds",
	"raw	Usage	gopi session raw <session-id> --lines A-B",
	"raw	Usage	gopi session raw <session-id> --sysprompt",
	"raw	Usage	gopi session raw <session-id> --userprompt",
	"raw	Usage	gopi session raw --file PATH",
	"raw	Usage	gopi session raw --branch BRANCH",
	"raw	Usage	gopi session raw --last [N]",
	"checkpoint	Usage	gopi session checkpoint list [--dir DIR]",
	"checkpoint	Usage	gopi session checkpoint export <log> [--dir DIR]",
	"resume	Examples	gopi session resume 01a096c9-6f7d-7e43-8e46-9ecca58e9cd0",
	"resume	Examples	gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log",
	"resume	Examples	gopi session resume ~/.gopi/sessions/01a096c9-6f7d-7e43-8e46-9ecca58e9cd0/....raw.log",
	"resume	Examples	gopi session resume 4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33",
	"resume	Examples	gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log next-step.md",
	"resume	Examples	gopi session resume t-1788048877",
	"resume	Examples	gopi session resume t-1788048877 --task t-1788048877",
	"resume	Examples	gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log --task t-1788048877",
}

// TestG4HelpSurfaceMatchesInventory is the anti-staleness gate: the derivation,
// the pinned expectation and the inventory's covered set must all agree.
func TestG4HelpSurfaceMatchesInventory(t *testing.T) {
	got := g4DerivedKeys()
	if diff := g4StringDiff(g4ExpectedRows, got); diff != "" {
		t.Errorf("the session group's advertised surface changed:\n%s\n"+
			"Every advertised invocation needs a case in this file and a row in "+
			"devdoc/gopi-session-help-surface-inventory.md, and this expectation "+
			"updated in the same change.", diff)
	}
	if len(got) < 60 {
		t.Fatalf("the derivation found only %d rows — the scanner is reading "+
			"blind, so a green run would prove nothing", len(got))
	}
	// Every derived row is covered by a case (a case whose key is not derived
	// is reported by TestG4CoveredSetMatchesCases, which names it).
	covered := map[string]bool{}
	for _, k := range g4CoveredKeys() {
		covered[k] = true
	}
	var uncovered []string
	for _, k := range got {
		if !covered[k] {
			uncovered = append(uncovered, k)
		}
	}
	if len(uncovered) > 0 {
		t.Errorf("advertised but with no real-invocation case:\n  %s\n"+
			"Rule 31: an advertised surface in no execution has not been tested.",
			strings.Join(uncovered, "\n  "))
	}
}

// TestG4CoveredSetMatchesCases pins the other direction: a case that drives an
// argv the help does not advertise is a test of a surface nobody promised, and
// it must be renamed, removed, or the help updated.
func TestG4CoveredSetMatchesCases(t *testing.T) {
	derived := map[string]bool{}
	for _, k := range g4DerivedKeys() {
		derived[k] = true
	}
	var stray []string
	for k := range g4SurfaceCases {
		if !derived[k] {
			stray = append(stray, k)
		}
	}
	for k := range g4DispatchedElsewhere {
		if !derived[k] {
			stray = append(stray, k)
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Errorf("cases drive argv the help does not advertise:\n  %s\n"+
			"Either the help lost a synopsis (then the case is describing a "+
			"surface that no longer exists) or the key is misspelled.",
			strings.Join(stray, "\n  "))
	}
}

// TestG4DispatchedElsewhereTestsExist holds g4DispatchedElsewhere to its claim:
// each row it covers names a test FUNCTION that is declared in this package. A
// map entry pointing at a sentence, or at a test somebody renamed away, would
// otherwise be a coverage claim with nothing behind it — the exact class this
// file exists to catch.
func TestG4DispatchedElsewhereTestsExist(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "cmd", "agent",
		"cli_checkpoint_usage_dispatch_test.go"))
	if err != nil {
		t.Fatalf("read the dedicated-test file the coverage declaration points at: %v", err)
	}
	for row, fn := range g4DispatchedElsewhere {
		if !regexp.MustCompile(`func ` + regexp.QuoteMeta(fn) + `\(`).Match(src) {
			t.Errorf("row %q claims to be covered by %s, which is not declared in "+
				"cmd/agent/cli_checkpoint_usage_dispatch_test.go — a coverage claim "+
				"must name a test that exists", row, fn)
		}
	}
}

// --- the checked-in inventory table ------------------------------------------

// g4InventoryRowRe reads one row of the checked-in inventory table
// (devdoc/gopi-session-help-surface-inventory.md):
//
//	| 3 | `log` | Usage | `gopi session log <session-id> --response` | yes | … |
var g4InventoryRowRe = regexp.MustCompile("(?m)^\\| *[0-9]+ *\\| *`([a-z-]+)` *\\| *([A-Za-z][^|]*) *\\| *`([^`]+)` *\\|")

// g4InventoryDoc reads the checked-in inventory table from the repo root.
func g4InventoryDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "devdoc", "gopi-session-help-surface-inventory.md"))
	if err != nil {
		t.Fatalf("read the inventory table (the deliverable this gate protects): %v", err)
	}
	return string(b)
}

// TestG4HelpSurfaceInventoryTableMatchesHelp re-reads the DELIVERABLE from disk
// and compares it with the derivation: a table that lists an invocation the help
// no longer prints (or omits one it does) is a document describing last month's
// binary. The row count is asserted too, so a truncated table cannot pass by
// mentioning every surface somewhere in prose.
func TestG4HelpSurfaceInventoryTableMatchesHelp(t *testing.T) {
	rows := g4InventoryRowRe.FindAllStringSubmatch(g4InventoryDoc(t), -1)
	var got []string
	for _, m := range rows {
		got = append(got, strings.TrimSpace(m[1])+"\t"+strings.TrimSpace(m[2])+"\t"+strings.TrimSpace(m[3]))
	}
	sort.Strings(got)
	want := g4DerivedKeys()
	if diff := g4StringDiff(want, got); diff != "" {
		t.Errorf("devdoc/gopi-session-help-surface-inventory.md and the help disagree:\n%s\n"+
			"Every advertised invocation needs a table ROW — re-run "+
			"scripts/g4-session-help-surface.sh ./bin/gopi and paste the result.",
			diff)
	}
	if len(rows) != len(want) {
		t.Errorf("the inventory table has %d rows, the help advertises %d — one "+
			"row per advertised invocation", len(rows), len(want))
	}
}

// TestG4HelpSurfaceInventoryRowIsInTheTruthManifest is the classification half:
// the inventory is a NORMATIVE document (a standing claim about what the help
// advertises right now), so it must declare that class on line 1 of ITSELF.
//
// WHAT THIS USED TO READ, AND WHY IT NO LONGER DOES (measured 2026-10-24, this
// task). The witness was a row in `devdoc/DOC-INDEX.tsv`, opened off disk with
// t.Fatalf on a missing file. Commit 475001d untracked that table on the
// operator's instruction (a generated table conflicts on every branch that adds
// a document), so from that commit on the file is absent from every cut ground
// and this test was permanently red for a reason that had nothing to do with the
// inventory. Rule 36 moved the class to line 1 of the document; this reads it
// there, through the package's ONE reader (devdocClassOf), and the PROPERTY is
// unchanged: a devdoc naming no class is invisible to every class-driven reader.
// internal/standards' TestEveryCorpusDocDeclaresItsClassInItsHeader owns the
// whole-corpus version of the same rule.
func TestG4HelpSurfaceInventoryRowIsInTheTruthManifest(t *testing.T) {
	root := repoRoot(t)
	const want = devdoc.Root + "/gopi-session-help-surface-inventory.md"
	class, why := devdocClassOf(root, want)
	if why != "" {
		t.Fatalf("%s — the inventory makes a standing claim about what the help "+
			"advertises, so it must declare %q on line 1 (Rule 36)", why, devdoc.ClassNormative)
	}
	if class != devdoc.ClassNormative {
		t.Errorf("%s declares class %q, want %q — the role is load-bearing: "+
			"merge_union.go resolves a two-class conflict by reading exactly this banner, "+
			"and the preland gate attributes a conflicted document's class from it",
			want, class, devdoc.ClassNormative)
	}
}

// g4DerivationScript is the operator-facing re-derivation the inventory's "How
// to re-derive this table" section tells a reader to run. It is gated below
// because it is the ONE artifact of this group that no other arm in this file
// reads: the two derivation arms take the usage CONSTANTS (g4PageRows) and the
// checked-in table, so a script asking the binary for a name it has since
// RETIRED goes on printing nothing for that page while both in-Go arms stay
// green.
const g4DerivationScript = "scripts/g4-session-help-surface.sh"

// g4ScriptPageLine is the live address ONE page is driven through: the program
// word, the family word, the verb — `gopi session log`, built from the SAME
// constants the derivation's own pattern uses (CliVerbSession is the family word
// the binary dispatches; the verb is this file's page word), never a literal
// typed for this test.
func g4ScriptPageLine(verb string) string {
	return gopiProgramWord + " " + CliVerbSession + " " + verb
}

// g4ScriptAddressRe matches one entry of the script's own page list
// (G4_ADDRESSES): a double-quoted `gopi session <verb>` page address at the
// indent the list uses. `(?m)` is load-bearing — without it `^`/`$` anchor on the
// WHOLE file, nothing matches, and this gate would report "the script declares no
// address" for any input. It reads the SCRIPT, so the assertion is about the
// addresses the script actually drives rather than about a string that happens to
// appear somewhere in the file.
var g4ScriptAddressRe = regexp.MustCompile(`(?m)^\s*"` + gopiProgramWord + ` ` + CliVerbSession + ` (\w+)"$`)

// TestG4DerivationScriptDrivesEverySurfaceAddress is the regression guard for the
// script's own blindness.
//
// WHY IT IS NEEDED. The script drives each page and pipes the output through an
// awk matcher. Both halves can name a DEAD address and the script still exits 0:
// a retired spelling is refused at argv[0], prints a refusal message rather than
// a page, and contributes ZERO rows — a silent under-report, which is measured
// TWICE in this script's own history (the flat top-level spellings derived four
// pages of refusals; the bare `checkpoint` page word then derived nothing at
// all). No in-Go arm can see it, because none of them runs the script: they read
// the usage constants.
//
// WHAT IT REQUIRES. The script's declared page list must be EXACTLY this file's
// pages, each through its family address, and the retired BARE spelling of a page
// must appear nowhere in the script — not even in its prose, so a reader copying
// a line out of the comment header does not type a refusal. The list is read as a
// list (one address per entry), so the test cannot be satisfied by a page address
// mentioned in a comment while the loop drives something else.
func TestG4DerivationScriptDrivesEverySurfaceAddress(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(g4DerivationScript)))
	if err != nil {
		t.Fatalf("read the derivation script (the command the inventory tells a reader to re-run): %v", err)
	}
	body := string(raw)

	driven := map[string]bool{}
	for _, m := range g4ScriptAddressRe.FindAllStringSubmatch(body, -1) {
		driven[m[1]] = true
	}
	if len(driven) == 0 {
		t.Fatalf("%s declares no page address of the form %q — the derivation "+
			"runs blind, so this gate would pass on any file at all",
			g4DerivationScript, g4ScriptPageLine("<verb>"))
	}
	for _, p := range g4Pages {
		if !driven[p.word] {
			t.Errorf("%s does not drive %q — the %q page is missing from the "+
				"script, so a reader re-running it derives an inventory that is "+
				"short by that whole page", g4DerivationScript, g4ScriptPageLine(p.word), p.word)
		}
	}
	if len(driven) != len(g4Pages) {
		t.Errorf("%s drives %d page(s), this file derives %d — the two must be "+
			"the same surface", g4DerivationScript, len(driven), len(g4Pages))
	}

	// A RETIRED spelling must not come back IN A DRIVE POSITION — on a line whose
	// command word is the program word itself, or the script's own `$BIN`. The
	// bare page word is refused at argv[0] by the retirement, so such a line
	// derives nothing while the script still exits 0, which is the silent
	// under-report this test exists for. PROSE IS NOT A DRIVE POSITION ("mention
	// is not prescription"): this file's header names the retired spellings to
	// record WHY the family address is required, and the scanner reads command
	// lines, not comments.
	if lines := g4ScriptBareDriveLines(body); len(lines) > 0 {
		t.Errorf("%s drives the RETIRED spelling on %d line(s) — the binary refuses "+
			"that word by name (exit 1), so the page derives 0 rows and the script "+
			"reports the group short; drive the family address `%s` instead:\n  %s",
			g4DerivationScript, len(lines), g4ScriptPageLine("<verb>"), strings.Join(lines, "\n  "))
	}
	// The matcher must require the family word too: one that makes it OPTIONAL
	// reads a page printing a bare address as if it were live.
	if strings.Contains(body, CliVerbSession+")?") {
		t.Errorf("%s: the matcher makes the family word OPTIONAL (`%s)?`) — a "+
			"synopsis spelled with the retired bare address would then be read as "+
			"live, which is the drift this script exists to catch",
			g4DerivationScript, CliVerbSession)
	}
}

// g4ScriptBareDriveLines returns every line of the script that DRIVES a bare page
// address: a command word (the program word, or the script's own `$BIN` variable)
// followed directly by one of this file's page words, with no family word. A
// comment mentioning the retired spelling is not returned — it starts with `#`,
// so its first word is not a command word. The check is deliberately about the
// command position, because that is the half that can be dead on the wire.
func g4ScriptBareDriveLines(body string) []string {
	verbs := make([]string, 0, len(g4Pages))
	for _, p := range g4Pages {
		verbs = append(verbs, p.word)
	}
	re := regexp.MustCompile(`(?m)^\s*("?\$BIN"?|` + gopiProgramWord + `)\s+(` + strings.Join(verbs, "|") + `)\s`)
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if re.MatchString(line) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// TestG4DerivationScriptRunsTheRealBinary closes the other half: the script is
// EXECUTED (Rule 31) against the real binary, and its rows must be exactly the
// rows this file's derivation reads off the usage constants — in order, not
// merely as a set, because the two are documented as diffing line for line. The
// count is asserted too, so a script that silently derived one row would not
// pass by deriving "the right rows plus none".
func TestG4DerivationScriptRunsTheRealBinary(t *testing.T) {
	script := filepath.Join(repoRoot(t), filepath.FromSlash(g4DerivationScript))
	// The script is the SUBJECT, not the test's convenience: it is run the way
	// the inventory tells a reader to run it, against the freshly built binary
	// this package's probes also use, and its exit status is part of the result
	// (the script is `set -euo pipefail`, so a refused page is a non-zero pipe
	// member — but a page that prints a refusal MESSAGE exits 0, which is the
	// silent mode this assertion is here to catch).
	out, err := exec.Command("bash", script, gopiTestBinPath(t)).Output() //nolint:gosec // the script and the binary are this repo's own, resolved from the test's repo root
	if err != nil {
		t.Fatalf("%s <bin> failed: %v\n%s", g4DerivationScript, err, out)
	}
	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			got = append(got, line)
		}
	}
	if len(got) == 0 {
		t.Fatal("the script derived no row at all — a drive line is being refused by the binary")
	}
	want := g4ExpectedRows
	if diff := g4StringDiff(want, got); diff != "" {
		t.Errorf("the script's own output and the pinned surface disagree:\n%s\n"+
			"The script and this file read the same surface two ways — re-derive "+
			"from the run and reconcile, never by editing one of them to match.", diff)
	}
	if len(got) != len(want) {
		t.Errorf("%s derived %d row(s), the help advertises %d — the script's page "+
			"addresses and this file's derivation have drifted apart",
			g4DerivationScript, len(got), len(want))
	}
}

// g4GroupFlagMembers are the group's parser flag tables, one per verb that has
// one. This is the second half of "advertised": a flag the help prints but the
// verb's table refuses is a promise the CLI breaks ("unknown flag --x"), which
// is how G1 found `sweep-worktrees --json`.
//
//nolint:gochecknoglobals // the parser tables under test, read by the gates
var g4GroupFlagMembers = map[string]func(string) bool{
	CliVerbLog:   logFlagMember,
	CliVerbStats: statsFlagMember,
	CliVerbRaw:   rawFlagMember,
}

// TestG4AdvertisedFlagsAreAdmitted sweeps every flag the group's help prints
// through the verb's own flag table, and pins the set in BOTH directions: a
// spelling that is refused is the G1 defect class, and a member the help never
// mentions is a surface nobody can discover (the `log` page's moved
// `--http`/`--raw` are exactly that — refused by name at run time with a
// pointer, and deliberately kept IN the table so the pointer can be printed
// rather than a generic unknown-flag error; they are listed in the inventory as
// a help-declared exception).
func TestG4AdvertisedFlagsAreAdmitted(t *testing.T) {
	advertised := map[string]map[string]bool{}
	for _, r := range g4AdvertisedRows() {
		if advertised[r.verb] == nil {
			advertised[r.verb] = map[string]bool{}
		}
		for _, f := range r.flags {
			advertised[r.verb][f] = true
		}
	}
	scanned := 0
	for verb, member := range g4GroupFlagMembers {
		for f := range advertised[verb] {
			scanned++
			if !member(f) {
				t.Errorf("`gopi %s --%s` is ADVERTISED by the help and REFUSED by "+
					"%s's flag table — an advertised flag is either obeyed or "+
					"refused by NAME (Rule 15); add it to the table or take it out "+
					"of the synopsis", verb, f, verb)
			}
		}
	}
	if scanned < 30 {
		t.Fatalf("the sweep inspected only %d advertised flag sites — the "+
			"derivation is broken, so a green run proves nothing", scanned)
	}
	// The refused-by-design flags: --json/--text are advertised by `log` (and
	// by `raw` only as a REFUSAL the help states in prose) and rejected by the
	// raw table with a named error.
	for _, f := range rejectedRawFlags {
		if rawFlagMember(f) {
			t.Errorf("--%s must stay out of the raw flag table: raw prints "+
				"verbatim bytes and the refusal is a documented contract", f)
		}
	}
}

// g4LogMovedFlags are the `gopi session log` spellings the help names under "Moved to
// the raw-log commands" — kept in the parser table so the CLI can print the
// exact replacement command, refused at run time with a non-zero exit.
var g4LogMovedFlags = []string{"http", "raw"}

// TestG4LogMovedFlagsStayAdmittedAndRefused pins that contract: the flags stay
// in the table (so the pointer can be printed) and the real binary REFUSES them
// with exit 2 and the replacement command.
func TestG4LogMovedFlagsStayAdmittedAndRefused(t *testing.T) {
	for _, f := range g4LogMovedFlags {
		if !logFlagMember(f) {
			t.Errorf("--%s must stay in the log flag table: removing it turns the "+
				"documented pointer into a generic unknown-flag error", f)
		}
	}
	sb := g4NewSandbox(t)
	for _, f := range g4LogMovedFlags {
		stdout, stderr, code := sb.g4RunSplit(t, CliVerbLog, g4FixtureSessionID, "--"+f)
		if code == 0 {
			t.Errorf("`gopi session log <id> --%s` must refuse (it moved):\n%s%s", f, stdout, stderr)
			continue
		}
		if !strings.Contains(stderr, "moved out of gopi session log") || !strings.Contains(stderr, "use: gopi") {
			t.Errorf("`gopi session log <id> --%s` must refuse BY NAME with the replacement "+
				"command on stderr:\n%s%s", f, stdout, stderr)
		}
	}
}

// --- real invocations: the advertised surface, argv by argv -------------------

// g4Fixture is one probe's world: the sandbox plus the concrete substitutions
// an advertised invocation needs.
type g4Fixture struct {
	sb  g4Sandbox
	id  string // the fixture session id
	raw string // the fixture's paired raw log path
	tok string // the fixture's session directory
	// branch/worktree are the DELIVERABLE SELECTORS' real values: a live ledger
	// row in the sandbox ledger records both and its session_id is the fixture
	// session, so `--branch BRANCH` / `--worktree WT` resolve to the SAME
	// session the fixture id names (the arm the help advertises).
	branch   string
	worktree string
}

// g4Case is one advertised invocation's proof.
type g4Case struct {
	// argv is the REAL argv: the advertised invocation with the fixture's own
	// values substituted. It is built, not typed, so the case cannot drift
	// from the synopsis it claims to prove.
	argv func(fx g4Fixture) []string
	// wantExit is the expected exit code: 0 for a working surface, non-zero
	// for an invocation the help advertises and the CLI legitimately REFUSES
	// in this sandbox (an example target that cannot exist offline).
	wantExit int
	// allowEmpty drops the non-empty-stdout assertion for surfaces whose
	// correct result IS empty in this fixture (a filter with no matching tool
	// call), never as a way to excuse a failure.
	allowEmpty bool
	// wantOut is a substring STDOUT must carry — the observable effect.
	wantOut string
	// wantErr is a substring STDERR must carry (raw's stdout is pure bytes by
	// contract, so its diagnostics are asserted where they belong).
	wantErr string
	// note records WHY a row is not a plain happy path.
	note string
}

// g4Placeholder... are the tokens the group's help uses for values that vary.
const (
	g4PlaceholderID    = "<session-id>"
	g4PlaceholderIDAlt = "<id>"
	g4PlaceholderLog   = "<log>"
	// g4PlaceholderRound is the log page's round positional.
	g4PlaceholderRound = "<round>"
	// The resume page's Examples name a real-looking session id and a
	// timestamped raw path from a foreign machine. Both resolve to the
	// FIXTURE's session/log, so the invocation is the help's own argv.
	g4ExampleSessionID  = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"
	g4ExampleSessionID2 = "4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33"
	g4ExampleRawName    = "gopi-raw-20260826-120000.log"
	// g4ExampleSessionAltLog is the resume page's session-DIRECTORY raw-log
	// example (the `~/.gopi/sessions/<session-id>/<run>.raw.log` shape, run id
	// elided as `....`). It is matched WHOLE — cutting it at the raw-log
	// basename alone would leave the example session id and the `....` token in
	// argv, so the shape would not survive substitution.
	g4ExampleSessionAltLog = "~/.gopi/sessions/01a096c9-6f7d-7e43-8e46-9ecca58e9cd0/....raw.log"
	// g4RawExamplePath is the resume page's raw-log example path, spelled
	// absolutely in the help. It is matched whole for the same reason: the
	// substitution replaces the PATH, and g4ExampleRawName cuts the basename
	// token of patterns that are not this shape.
	g4RawExamplePath = "~/.gopi/raw/" + g4ExampleRawName
	// g4PlaceholderBranch / g4PlaceholderWorktree are the help pages' tokens for
	// the deliverable selectors' values. They are PLACEHOLDERS (like
	// <session-id>), substituted with the fixture's own row below — a bare `B`
	// or `feat/x` in a synopsis reads as a literal an operator might type.
	g4PlaceholderBranch   = "BRANCH"
	g4PlaceholderWorktree = "WT"
	// g4ResumeTaskID is the row the `--task` example is driven against.
	g4ResumeTaskID = "t-g4-surface"
)

// g4Subst turns an advertised invocation into real argv: the fixture's session
// id / raw log / directory / round replace the help's placeholders, and the
// prose tail of a worked example (the `| jq …` pipe) is cut, because it is not
// argv.
func g4Subst(inv string, fx g4Fixture) []string {
	inv = strings.NewReplacer(
		// The session-DIRECTORY raw-log example is spelled as one token and
		// substituted BEFORE the bare example ids below: its match starts at the
		// `~`, i.e. earlier in the line than the session id inside it, so the
		// whole path wins and the advertised shape survives. Substituting the id
		// alone would leave the `....` run-id elision in argv and the target
		// would not exist.
		g4ExampleSessionAltLog, fx.g4SessionRunLog(),
		g4PlaceholderID, fx.id,
		g4PlaceholderIDAlt, fx.id,
		g4ExampleSessionID, fx.id,
		g4ExampleSessionID2, fx.id,
		g4PlaceholderBranch, fx.branch,
		g4PlaceholderWorktree, fx.worktree,
	).Replace(inv)
	fields := strings.Fields(inv)
	// Drop the invocation's own leading `gopi` word: it is the PROGRAM, and
	// this helper returns argv (the child is started with the binary's path).
	if len(fields) > 0 && fields[0] == "gopi" {
		fields = fields[1:]
	}
	var out []string
	for _, f := range fields {
		switch f {
		case "|": // the example's shell pipe is prose, not argv
			return out
		case g4PlaceholderLog:
			out = append(out, fx.raw)
		case "<round>":
			out = append(out, gm4FirstRound)
		case "N", "[N]":
			out = append(out, "2")
		case "[continuation...]":
			// optional: the base form is `gopi session resume <target>`; the
			// continuation-file form has its own case.
		case "<target>":
			out = append(out, fx.raw)
		case "TOOL":
			out = append(out, gm4PresentTool)
		case "TEXT":
			out = append(out, gm4PresentText)
		case "A-B":
			out = append(out, gm4LineSpan)
		case "PATH":
			out = append(out, fx.raw)
		case "DIR":
			out = append(out, filepath.Dir(fx.raw))
		case "t-1788048877":
			out = append(out, g4ResumeTaskID)
		case g4ExampleSessionAltLog:
			// The example names a session-directory raw log under the OTHER
			// example session id, with `....` standing in for the run id. The
			// fixture installs exactly that shape — a raw log re-homed inside
			// its session directory — so the target is the fixture's OWN such
			// path: the advertised form (a `.raw.log` reached through a session
			// DIRECTORY) is driven for real instead of being left to refuse on a
			// literal `~`, which this host cannot expand.
			out = append(out, fx.g4SessionRunLog())
		case g4RawExamplePath:
			// The example's raw log does not exist on this host; the fixture's
			// own log beside the fixture session is the same surface (the
			// session pairing), so the invocation stays the help's own argv
			// while the target exists.
			out = append(out, filepath.Base(fx.raw))
		default:
			out = append(out, f)
		}
	}
	return out
}

// Fixture values used by the substitutions — each is a fact about the real
// fixture corpus (a tool it called, text it contains, a round and a line span
// that exist), never a value chosen to make an assertion pass.
const (
	gm4FirstRound  = "1"
	gm4LineSpan    = "1-3"
	gm4PresentTool = "memory_search"
	gm4PresentText = "tool"
)

// g4SurfaceCases is the coverage table: one entry per derived row, keyed by
// g4RowKey. Every entry builds its argv from the row's own invocation
// (g4Subst), so a help edit that changes a synopsis changes the argv the case
// drives — the two cannot drift apart.
//
//nolint:gochecknoglobals // the coverage table, read by the gates and the runner
var g4SurfaceCases = map[string]g4Case{}

// g4CaseFor registers the case for a derived row, failing loudly at
// registration time if the row is not advertised (a typo in the key would
// otherwise silently drop a row from coverage).
func g4CaseFor(row g4Row, c g4Case) {
	c.argv = func(fx g4Fixture) []string { return g4Subst(row.invocation, fx) }
	g4SurfaceCases[g4RowKey(row)] = c
}

// g4RegisterCases wires every derived row to its assertions. It is a function
// (not a var literal) because the keys are DERIVED: the table is built from the
// same rows the gate checks, so a help edit cannot leave a case pointing at a
// synopsis that no longer exists.
func init() { //nolint:gochecknoinits // the coverage table is derived from the usage constants
	for _, row := range g4AdvertisedRows() {
		if _, elsewhere := g4DispatchedElsewhere[g4RowKey(row)]; elsewhere {
			continue // proven by its own dedicated basic test, not by this table
		}
		g4CaseFor(row, g4AssertionsFor(row))
	}
}

// g4AssertionsFor builds the claims for one advertised invocation from what the
// invocation IS — the flags it names decide which surface answered, so the
// assertion is about the advertised effect, never about a decorative string.
// Every value below was read off a real run of that argv against the fixture
// (see the inventory doc's derivation section).
func g4AssertionsFor(row g4Row) g4Case {
	inv := row.invocation
	has := func(flags ...string) bool {
		for _, f := range flags {
			if strings.Contains(inv, f) {
				return true
			}
		}
		return false
	}
	switch row.verb {
	case CliVerbResume:
		// A target that CANNOT exist offline is asserted as its documented
		// refusal (the reader's own machine may hold the path; a fresh sandbox
		// does not).
		if has(g4RawExamplePath) {
			return g4Case{
				wantExit:   1,
				allowEmpty: true,
				wantErr:    "is neither a raw log path nor an existing session id",
				note: "an advertised example path that cannot exist offline refuses " +
					"by naming both target kinds it tried",
			}
		}
		if has("t-1788048877") || has(g4ExampleSessionAltLog) {
			// The task-id target and the session-DIRECTORY raw log (an example
			// from another machine, whose `....` run id stands for a run this
			// sandbox does not have) DO resolve here: the fixture installs the
			// row's run directory and the re-homed checkpoint, so both are
			// reachable addresses and the verb loads the checkpoint. Measured,
			// not assumed — the same assertion the resolvable targets get, and
			// the proof the invocation reached the verb's own code path.
			return g4Case{
				wantExit:   1,
				allowEmpty: true,
				wantErr:    "Error creating agent",
				note: "the advertised task-id / session-directory example resolves " +
					"and the CHECKPOINT LOADS; the continuation round needs a provider " +
					"credential, deliberately not mocked (Rule 31's honest limit)",
			}
		}
		return g4Case{
			wantExit:   1,
			allowEmpty: true,
			wantErr:    "Error creating agent",
			note: "the advertised target resolves and the CHECKPOINT LOADS; the " +
				"continuation round itself needs a provider credential, which this " +
				"task deliberately does not mock into existence (Rule 31's honest limit)",
		}
	case CliVerbLog:
		switch {
		case g4HasRoundPositional(row.invocation) && has("--request"):
			return g4Case{wantOut: `"model"`,
				note: "the round's request body VERBATIM — the same bytes gopi session raw prints for that round"}
		case g4HasRoundPositional(row.invocation) && has("--response"):
			return g4Case{wantOut: "← CHUNK",
				note: "the round's response side as logged (the fixture's first round streamed)"}
		case g4HasRoundPositional(row.invocation) && has("--thinking"):
			return g4Case{wantOut: "ROUND 1 of",
				note: "the round header; this fixture round logged no thinking, and a round view says which round it read"}
		case g4HasRoundPositional(row.invocation) && has("--filter"):
			return g4Case{wantOut: "ROUND 1 of",
				note: "the round header with the filter applied to that round's calls"}
		case g4HasRoundPositional(row.invocation):
			return g4Case{wantOut: "ROUND 1 of",
				note: "one round of the session, named by its own 1-based round number"}
		case has("--rounds"):
			return g4Case{wantOut: "SESSION ROUNDS",
				note: "the session's ROUND table — one JSONL `request` row = one HTTP request/response = one iteration, the unit every session surface numbers (operator's ruling, 2026-10-12)"}
		case has("--turns"):
			return g4Case{wantOut: "SESSION HUMAN TURNS",
				note: "the HUMAN-TURN table — one OPERATOR row to the next, with the rounds that turn took: the SUBSET of turns, named for itself because the bare word is the ledger's count (`gopi session show`)"}
		case has("--toolcall"):
			// The Nth TOOL CALL, in full. The assertion names the ordinal AND
			// the session's recorded call count: this view counts CALLS, which
			// the fixture has 8 of, against the session's 9 ROUNDS — the
			// distinction the flags exist to keep (the round table asserts the
			// round count; the turn table asserts the operator-row count).
			return g4Case{wantOut: "TOOL CALL 2 of 8",
				note: "the Nth tool call in full, naming the round it ran in"}
		case has("--text"):
			// Plain-text mode drops the banner and prints CONTENT ONLY, which
			// is its whole advertised point (`--response --text` = "Plain text
			// responses only"). Non-empty output is the assertion.
			return g4Case{note: "plain-text mode: content with no banner"}
		case has("--request --json"):
			return g4Case{wantOut: `"max_tokens"`, note: "the full first request body, as JSON"}
		case has("--json"):
			return g4Case{wantOut: `"sessionId"`, note: "the raw JSONL line dump starts at the session_start record"}
		case has("--grep"):
			return g4Case{wantOut: "Grep:", note: "the grep view echoes the pattern and the scope it matched"}
		case has("--context-trim"):
			return g4Case{wantOut: "Session: " + g4FixtureSessionID, note: "the context-trim view renders the session header like its siblings"}
		case has("--filter ", "--memory", "--job", "--web", "--vision", "--text2img", "--git", "--context", "--search"):
			return g4Case{wantOut: "Filter:", note: "a tool-family view announces the filter it applied"}
		case has("--response"):
			return g4Case{wantOut: "RESPONSE [iteration", note: "the assistant responses, per iteration"}
		case has("--thinking"):
			return g4Case{wantOut: "THINKING [iteration", note: "the thinking blocks, per iteration"}
		case has("--request"):
			return g4Case{wantOut: "REQUEST — system prompt", note: "the system prompt the session actually sent"}
		case has("--chat"):
			return g4Case{wantOut: "Session: " + g4FixtureSessionID, note: "the conversation view"}
		case has("--todo"):
			return g4Case{wantOut: "Session: " + g4FixtureSessionID, note: "the task-area view"}
		case has("--all"):
			return g4Case{wantOut: "SESSION", note: "the cross-session summary table"}
		case has("--branch", "--worktree"):
			// A BARE selector row: the session's own view, reached through the
			// ledger row instead of the id. Asserted by the same string the id's
			// own default case asserts — the point of the selector is that it is
			// the same session. (A selector WITH a view flag is a different row
			// and is asserted by that flag's own case above: the view decides the
			// output, exactly as it does through the id.)
			return g4Case{wantOut: "Session: " + g4FixtureSessionID,
				note: "the deliverable selector resolves the ledger row and renders its session"}
		default:
			return g4Case{wantOut: "Session: " + g4FixtureSessionID, note: "the session view names the fixture session it resolved"}
		}
	case CliVerbStats:
		switch {
		case has("--json"):
			return g4Case{wantOut: `"session_id"`, note: "the JSON digest names the session"}
		case has("--all"):
			return g4Case{wantOut: "All sessions", note: "the all-sessions roll-up"}
		default:
			return g4Case{wantOut: "Session " + g4FixtureSessionID, note: "the per-session numbers block"}
		}
	case CliVerbRaw:
		switch {
		case has("response"):
			return g4Case{wantOut: "← CHUNK", note: "the paired response side of round 1"}
		case has("request"):
			return g4Case{wantOut: `"model"`, note: "the verbatim request body itself"}
		case has("--rounds"):
			return g4Case{wantOut: "RAW ROUNDS", note: "the round index"}
		case has("--sysprompt"):
			return g4Case{wantOut: "You are GoPi", note: "the system prompt the session actually sent"}
		case has("--userprompt"):
			return g4Case{wantOut: "[EPIC GOAL]", note: "the first user prompt the session actually sent"}
		default:
			return g4Case{wantOut: "→ REQUEST", note: "the verbatim bytes, starting at the request marker"}
		}
	case CliVerbCheckpoint:
		if isExport(inv) {
			return g4Case{wantOut: `"model"`, note: "the exported checkpoint JSON body"}
		}
		return g4Case{wantOut: "reqs", note: "the raw-log listing header"}
	}
	return g4Case{}
}

// isExport reports whether the invocation is the `checkpoint export` face (the
// listing has no subcommand word after the verb).
func isExport(inv string) bool {
	return strings.Contains(inv, " export ")
}

// g4HasRoundPositional reports whether an advertised `gopi session log` invocation
// names a <round> POSITIONAL — i.e. a token right after the session-id whose
// value is a round selector. One definition, used by the assertions above, so
// a help edit that moves the round (or drops it) is visible here.
func g4HasRoundPositional(inv string) bool {
	f := strings.Fields(inv)
	for i, tok := range f {
		if tok != g4PlaceholderID && tok != g4PlaceholderIDAlt {
			continue
		}
		if i+1 >= len(f) {
			return false
		}
		// The token after the session must BE a round: the <round> placeholder,
		// an integer (0 and the negative from-the-end forms included), or the
		// bare listing-of-rounds word. "Not a pipe" is NOT the test — every
		// `gopi session log <session-id> --flag` row has a next token, so a looser test
		// would claim the round for rows that have none (measured: it did).
		next := f[i+1]
		if next == g4PlaceholderRound {
			return true
		}
		if _, err := strconv.Atoi(next); err == nil {
			return true
		}
		return next == gopilog.RoundKeywordLast
	}
	return false
}

// g4CaseDefaultWantOut is the observable effect a plain happy path must show.
func g4CaseDefaultWantOut(row g4Row) string {
	switch row.verb {
	case CliVerbLog:
		return "Session: " + g4FixtureSessionID
	case CliVerbStats:
		return "Session"
	case CliVerbRaw:
		return "REQUEST"
	case CliVerbCheckpoint:
		if strings.Contains(row.invocation, " export ") {
			return "model"
		}
		return "reqs"
	case CliVerbResume:
		return "resume"
	}
	return ""
}

// TestG4EveryAdvertisedSurfaceRuns is the Rule 31 half: every advertised
// invocation is executed against the REAL binary with the FIXTURE's own values,
// and its observable effect asserted. `-h` is never the argv.
func TestG4EveryAdvertisedSurfaceRuns(t *testing.T) {
	wantCases := len(g4AdvertisedRows()) - len(g4DispatchedElsewhere)
	if len(g4SurfaceCases) != wantCases {
		t.Fatalf("the coverage table has %d entries; the help advertises %d "+
			"invocations of which %d are proven by a dedicated basic test — every "+
			"advertised surface needs a case", len(g4SurfaceCases),
			len(g4AdvertisedRows()), len(g4DispatchedElsewhere))
	}
	fx := g4NewFixture(t)
	// The `--task` example needs a ledger row to link to (the case's own
	// happy path reaches the agent builder, so the row is only read).
	g4SeedTaskRow(t, fx)
	for _, row := range g4AdvertisedRows() {
		if _, elsewhere := g4DispatchedElsewhere[g4RowKey(row)]; elsewhere {
			continue // proven by its own dedicated basic test (g4DispatchedElsewhere)
		}
		key := g4RowKey(row)
		c, ok := g4SurfaceCases[key]
		if !ok {
			t.Errorf("no case for advertised invocation %q", key)
			continue
		}
		t.Run(g4TestName(row), func(t *testing.T) {
			argv := c.argv(fx)
			stdout, stderr, code := fx.sb.g4RunSplit(t, argv...)
			out := stdout + stderr
			if code != c.wantExit {
				t.Fatalf("`gopi %s` (from the help's %q line) exit=%d want %d\n--- stdout ---\n%s\n--- stderr ---\n%s",
					strings.Join(argv, " "), row.invocation, code, c.wantExit, stdout, stderr)
			}
			if !c.allowEmpty && strings.TrimSpace(stdout) == "" {
				t.Errorf("`gopi %s` produced NO stdout — the advertised surface ran "+
					"but showed nothing\n--- stderr ---\n%s", strings.Join(argv, " "), stderr)
			}
			if c.wantOut != "" && !strings.Contains(out, c.wantOut) {
				t.Errorf("`gopi %s` output does not carry %q (the advertised "+
					"effect)\n--- stdout ---\n%s\n--- stderr ---\n%s",
					strings.Join(argv, " "), c.wantOut, stdout, stderr)
			}
			if c.wantErr != "" && !strings.Contains(stderr, c.wantErr) {
				t.Errorf("`gopi %s` stderr does not carry %q\n--- stderr ---\n%s",
					strings.Join(argv, " "), c.wantErr, stderr)
			}
			// A refusal must never present the usage dump as its answer alone:
			// the invocation has to reach the verb's own code path.
			if code != 0 && c.wantErr == "" && c.wantOut == "" {
				t.Errorf("`gopi %s` refused without an asserted reason:\n%s",
					strings.Join(argv, " "), out)
			}
			// stdout discipline for raw: the byte view decorates nothing on
			// stdout — its diagnostics (the --last N separator, the truncation
			// note) go to stderr.
			if row.verb == CliVerbRaw && c.wantErr != "" && strings.Contains(stdout, c.wantErr) {
				t.Errorf("raw's diagnostic %q must be on STDERR (stdout stays pure "+
					"bytes)", c.wantErr)
			}
		})
	}
}

// g4TestName renders a row as a stable subtest name.
func g4TestName(r g4Row) string {
	n := r.verb + "/" + r.section + "/" + r.invocation
	n = strings.NewReplacer(" ", "_", "/", "_", "<", "", ">", "", "[", "", "]", "", "|", "", ".", "-").Replace(n)
	if len(n) > 120 {
		n = n[:120]
	}
	return n
}

// --- fixtures -----------------------------------------------------------------

// g4Sandbox is one probe's world: a GOPI_HOME the child may write anything into
// and the fixture session/raw log copied inside it. Both live under the
// harness's pinned scratch root (t.TempDir, Rules 22/32).
type g4Sandbox struct {
	bin  string
	home string
}

// g4NewSandbox builds the sandbox and installs the REAL fixture: the session
// JSONL and its paired raw log are copied into the sandbox GOPI_HOME, exactly
// as cli_logstats_fixture_test.go's statsFixtureSandbox does for the goldens —
// no constructed session, no hand-rolled log format.
func g4NewSandbox(t *testing.T) g4Sandbox {
	t.Helper()
	home := t.TempDir()
	sessions := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	raw := filepath.Join(home, config.ProfileDirName, scratch.RawLogDirName())
	for _, d := range []string{sessions, raw} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	copyFixture(t, filepath.Join("sessions", g4FixtureSessionID+".jsonl"),
		filepath.Join(sessions, g4FixtureSessionID+".jsonl"))
	copyFixture(t, filepath.Join("raw", g4FixtureRawLogName),
		filepath.Join(raw, g4FixtureRawLogName))
	// The session-DIRECTORY raw log: the re-homed shape the resume page's third
	// example advertises. It is the same bytes as the flat one — one fixture,
	// two addresses — because that is what a re-homed checkpoint IS.
	if err := os.MkdirAll(filepath.Join(sessions, g4FixtureSessionID), 0o700); err != nil {
		t.Fatalf("mkdir the session directory: %v", err)
	}
	copyFixture(t, filepath.Join("raw", g4FixtureRawLogName),
		filepath.Join(sessions, g4FixtureSessionID, g4FixtureRawLogName))
	return g4Sandbox{bin: gopiTestBinPath(t), home: home}
}

// g4NewFixture is a sandbox plus the substituted values the advertised
// invocations need — including the DELIVERABLE SELECTOR row: a live ledger row
// whose deliverable records a branch and a worktree and whose session_id IS the
// fixture session, so `--branch BRANCH` / `--worktree WT` reach exactly the
// session the fixture id names (which is what makes those synopses real: the
// selectors address a ledger row, and a ledger with no such row answers "no
// ledger row names …", not a session).
func g4NewFixture(t *testing.T) g4Fixture {
	t.Helper()
	sb := g4NewSandbox(t)
	branch := "task/g4-session-selector"
	worktree := filepath.Join(t.TempDir(), "gopi-g4-session-selector")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	g4SeedSelectorRow(t, sb.home, g4FixtureSessionID, branch, worktree)
	return g4Fixture{
		sb:       sb,
		id:       g4FixtureSessionID,
		raw:      filepath.Join(sb.home, config.ProfileDirName, scratch.RawLogDirName(), g4FixtureRawLogName),
		tok:      filepath.Join(sb.home, config.ProfileDirName, config.SessionsDirName),
		branch:   branch,
		worktree: worktree,
	}
}

// g4TaskRunLog is the path of the seeded row's OWN checkpoint: the run
// directory named by the task id (rundir.Dir) holding the co-located raw log
// rundir.RunDirRawLog resolves (scratch.SessionRawLogFileSuffix). It is the
// address `gopi session resume <task-id>` reaches from the FILESYSTEM alone, so the
// advertised task-id forms keep resolving even where the ledger row view does
// not answer.
func (fx g4Fixture) g4TaskRunLog() string {
	return filepath.Join(fx.sb.home, config.ProfileDirName, config.SessionsDirName,
		g4ResumeTaskID, g4ResumeTaskID+scratch.SessionRawLogFileSuffix)
}

// g4SessionRunLog is the fixture's session-DIRECTORY raw log — the shape the
// resume page's third example advertises (`~/.gopi/sessions/<session-id>/
// <run>.raw.log`): a checkpoint re-homed beside the session that wrote it. The
// fixture installs it for the same reason it installs the flat raw log: the
// example is a target that must EXIST for the advertised surface to run.
func (fx g4Fixture) g4SessionRunLog() string {
	return filepath.Join(fx.tok, fx.id, filepath.Base(fx.raw))
}

// g4SeedSelectorRow files the selector row into the ledger the CHILD RESOLVES:
// the default one under the sandbox GOPI_HOME (<home>/.gopi/memory.db), not a
// --db override — g4RunSplit passes the sandbox env and nothing else, so a row
// written anywhere else would be invisible to the very invocation under test.
// The row is created through the tools layer's own writer (Rule 32).
func g4SeedSelectorRow(t *testing.T, home, sessionID, branch, worktree string) {
	t.Helper()
	// The sandbox profile is the PIN: GOPI_HOME locates it, and the ledger path
	// is derived from the SAME resolution the child will use
	// (<home>/.gopi/memory.db). Both are set because the suite scaffold pins
	// GOPI_MEMORY_DB for the RUN (Rule 32), and that pin would otherwise win
	// over GOPI_HOME — the row would land in the suite's shared ledger while the
	// child (whose env drops every GOPI_* var, cliContractEnv) resolves the
	// sandbox one and finds nothing.
	t.Setenv(config.GopiDirEnv, home)
	ledger := filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)
	t.Setenv(config.MemoryDBEnv, ledger)
	const taskID = "t-g4-session-selector"
	if _, err := tools.TaskNew("g4 session selector fixture row", taskID, sessionID, ""); err != nil {
		t.Fatalf("seed selector row: %v", err)
	}
	blob := `{"branch":"` + branch + `","worktree":"` + worktree + `"}`
	db, err := sql.Open("sqlite", tools.LedgerDSN(ledger)) // db-scaffold-ok: the ledger env IS pinned two lines above (GOPI_HOME + config.MemoryDBEnv, resolved to this sandbox profile); the gate's detector matches a literal spelling of the env name, and a literal here would violate Rule 1/13
	if err != nil {
		t.Fatalf("open sandbox ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		tools.TaskStatusInProgress, blob, taskID); err != nil {
		t.Fatalf("stamp selector row: %v", err)
	}
}

// g4Run drives the real binary with the sandboxed env (no ledger override: the
// group's verbs read sessions, raw logs and the task row, never the memory
// store) and returns combined output plus the exit code.
//
// A timeout is its own failure: a correctly routed CLI verb never enters the
// agent loop, so a probe that hangs is a routing defect.
func (s g4Sandbox) g4Run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	out, _, code := s.g4RunSplit(t, args...)
	return out, code
}

// g4RunSplit is g4Run with stdout and stderr kept apart — the shape a raw-view
// assertion needs (stdout pure bytes, diagnostics on stderr).
func (s g4Sandbox) g4RunSplit(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(s.bin, familyAddressArgs(args)...)
	cmd.Env = cliContractEnv(s.home)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	err := error(nil)
	go func() { err = cmd.Run(); done <- err }()
	select {
	case <-done:
	case <-time.After(g4VerbTimeout * time.Second):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("gopi %s: TIMEOUT after %ds — argv fell through to the agent loop, or parked on a child",
			strings.Join(args, " "), g4VerbTimeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// g4SeedTaskRow writes the ledger row the `--task` example links to, through
// the tools layer (Rule 32: never a hand-rolled INSERT), AND materialises the
// row's run directory with a checkpoint — the address `gopi session resume <task-id>`
// actually resolves (rundir.DirFor + rundir.RunDirRawLog, keyed on the TARGET
// STRING, never on the ledger column).
//
// WHY THE RUN DIRECTORY, AND NOT ONLY THE ROW. The ledger pin below is a
// GO-SIDE pin: it is what makes tools.TaskSubmit land in the sandbox profile
// rather than the suite's shared ledger. The CHILD does not inherit it —
// cliContractEnv (cli_contracts_test.go) drops every GOPI_* variable and re-adds
// only GOPI_HOME — so a row written anywhere but the sandbox's own profile is
// invisible to the very invocation under test, and `gopi session resume <task-id>`
// answers "neither a raw log path nor an existing session id". The row is
// therefore pinned through BOTH channels: GOPI_HOME + config.MemoryDBEnv for the
// writer, and <home>/<profiledir>/memory.db (what the child resolves) for the
// reader — the same double pin g4SeedSelectorRow documents above.
//
// The run directory is seeded for the second reason: resolveResumeTarget tries
// the ledger row view FIRST and the run directory SECOND, and the run directory
// is the one address that survives a missing/unreadable row. With it present the
// advertised target resolves from the filesystem ALONE, which is exactly what
// the `--task` example advertises.
func g4SeedTaskRow(t *testing.T, fx g4Fixture) {
	t.Helper()
	// The sandbox profile is the pin both sides resolve (see the doc comment).
	t.Setenv(config.GopiDirEnv, fx.sb.home)
	ledger := filepath.Join(fx.sb.home, config.ProfileDirName, config.MemoryDBFileName)
	t.Setenv(config.MemoryDBEnv, ledger)
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "g4 help-surface fixture row",
		Payload: "g4 help-surface fixture payload",
		TaskID:  g4ResumeTaskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed task row: %v", err)
	}
	g4SeedTaskRunLog(t, fx)
}

// g4SeedTaskRunLog materialises the seeded row's run directory with a
// checkpoint, so `gopi session resume <the row's id>` resolves through
// internal/rundir — the filesystem address — and not only through the ledger
// row view. The copied log is the package's REAL fixture raw log
// (statsFixtureRawLogName, the same one the sandbox already installs beside the
// session), never a constructed stub.
//
// The file is named <task-id><suffix> because that is the name rundir.RunDirRawLog
// looks for in the run directory (scratch.SessionRawLogFileSuffix); RUN identity
// is the DIRECTORY name, so the basename carries the run id.
func g4SeedTaskRunLog(t *testing.T, fx g4Fixture) {
	t.Helper()
	dst := fx.g4TaskRunLog()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatalf("mkdir the row's run directory: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(statsFixtureRootRel, "raw", g4FixtureRawLogName))
	if err != nil {
		t.Fatalf("read the fixture raw log: %v", err)
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		t.Fatalf("write the row's run checkpoint %s: %v", dst, err)
	}
}

// --- shared helpers -----------------------------------------------------------

// g4StringDiff renders the two-way difference between want and got.
func g4StringDiff(want, got []string) string {
	inGot := map[string]bool{}
	for _, s := range got {
		inGot[s] = true
	}
	inWant := map[string]bool{}
	for _, s := range want {
		inWant[s] = true
	}
	var b strings.Builder
	for _, s := range want {
		if !inGot[s] {
			fmt.Fprintf(&b, "  want-only (advertised/claimed, not present): %s\n", s)
		}
	}
	for _, s := range got {
		if !inWant[s] {
			fmt.Fprintf(&b, "  got-only (present, unaccounted for): %s\n", s)
		}
	}
	return b.String()
}

// --- hardening: an invocation the help does NOT promise must refuse -------------

// TestG4UnknownFlagRefusals pins the group's refusal discipline. The help is a
// contract in both directions: what it prints must work (above), and what it
// does not print must fail LOUDLY rather than being silently ignored.
//
// This is where the group's one real defect lived: `gopi session checkpoint list
// --bogus` ran the plain listing and exited 0, because the group is dispatched
// before config resolution and the unknown flag fell into the positional
// bucket. Measured before the fix, on the pre-fix binary: exit 0 and a full
// listing. The same argv now exits 1 naming the flag.
func TestG4UnknownFlagRefusals(t *testing.T) {
	fx := g4NewFixture(t)
	cases := []struct {
		argv    []string
		wantErr string
		why     string
	}{
		{
			argv:    []string{CliVerbLog, g4FixtureSessionID, "--bogus"},
			wantErr: "unknown flag --bogus",
			why:     "log routes through its flag table",
		},
		{
			argv:    []string{CliVerbStats, g4FixtureSessionID, "--bogus"},
			wantErr: "unknown flag --bogus",
			why:     "stats routes through its flag table",
		},
		{
			argv:    []string{CliVerbRaw, g4FixtureSessionID, "--bogus"},
			wantErr: "unknown flag --bogus",
			why:     "raw routes through its flag table",
		},
		{
			argv:    []string{CliVerbCheckpoint, "list", "--bogus"},
			wantErr: "gopi session checkpoint: unknown flag --bogus",
			why:     "checkpoint runs BEFORE config resolution, so IT must refuse — this is the fixed defect",
		},
		{
			argv:    []string{CliVerbCheckpoint, "list", "--bogus", "--dir", filepath.Dir(fx.raw)},
			wantErr: "gopi session checkpoint: unknown flag --bogus",
			why:     "a known flag beside an unknown one does not launder it",
		},
		{
			argv:    []string{CliVerbCheckpoint, "export", g4FixtureRawLogName, "--bogus", "--dir", filepath.Dir(fx.raw)},
			wantErr: "gopi session checkpoint: unknown flag --bogus",
			why:     "export refuses an unknown flag the same way",
		},
		{
			argv:    []string{CliVerbCheckpoint, "bogus"},
			wantErr: "gopi session checkpoint: unknown subcommand",
			why:     "an unknown SUBCOMMAND was already loud — pinned so it stays that way",
		},
		{
			argv:    []string{CliVerbCheckpoint},
			wantErr: "Usage:",
			why:     "a bare `gopi session checkpoint` prints the page and fails (it names no subcommand)",
		},
		{
			argv:    []string{CliVerbCheckpoint, "export"},
			wantErr: "missing <log>",
			why:     "export with no target names what is missing",
		},
		{
			argv:    []string{CliVerbResume, "--bogus"},
			wantErr: "unknown option \"--bogus\"",
			why: "an unknown flag is refused BY NAME, never reported as a resume " +
				"TARGET — a -flag is not the thing to resume (the family criterion: " +
				"the flag's position must not decide which token is the operand)",
		},
		{
			argv:    []string{CliVerbResume},
			wantErr: "no resume target",
			why:     "with no operand at all the verb names what IS missing — the target",
		},
		{
			argv:    []string{CliVerbResume, fx.raw, "--bogus"},
			wantErr: "unknown option",
			why:     "resume's option slice is parsed by its own table",
		},
		{
			argv:    []string{CliVerbLog, "--bogus"},
			wantErr: "unknown flag --bogus",
			why:     "a bare log run is not an exception",
		},
		{
			argv:    []string{CliVerbStats, "--bogus"},
			wantErr: "unknown flag --bogus",
			why:     "a bare stats run is not an exception",
		},
		{
			argv:    []string{CliVerbRaw, "--bogus"},
			wantErr: "unknown flag --bogus",
			why:     "a bare raw run is not an exception",
		},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.argv, "_"), func(t *testing.T) {
			stdout, stderr, code := fx.sb.g4RunSplit(t, c.argv...)
			if code == 0 {
				t.Fatalf("`gopi %s` exited 0 — %s\n--- stdout ---\n%s\n--- stderr ---\n%s",
					strings.Join(c.argv, " "), c.why, stdout, stderr)
			}
			if !strings.Contains(stderr, c.wantErr) {
				t.Errorf("`gopi %s` refused without naming %q on stderr (%s)\n--- stderr ---\n%s",
					strings.Join(c.argv, " "), c.wantErr, c.why, stderr)
			}
		})
	}
}

// TestG4CheckpointPositionalToleranceIsDeliberate pins the one tolerance this
// group keeps, so it stays a DECISION rather than becoming folklore: a bare
// positional after the subcommand is ignored by `list` (the subcommand decides
// its own arity; `--flag` is the shape with no such reading, which is why only
// flags are refused). If that rule is ever tightened, this test is the place
// the decision is recorded — delete the case deliberately, do not let it rot.
func TestG4CheckpointPositionalToleranceIsDeliberate(t *testing.T) {
	fx := g4NewFixture(t)
	stdout, stderr, code := fx.sb.g4RunSplit(t, CliVerbCheckpoint, "list", "extra")
	if code != 0 {
		t.Fatalf("`gopi session checkpoint list extra` should still list (the subcommand "+
			"owns its positional arity): exit=%d\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "reqs") {
		t.Errorf("`gopi session checkpoint list extra` listed nothing:\n%s", stdout)
	}
}

// TestG4ProbeToTheAuthBoundaryIsHonest documents WHY the resume rows' proof
// stops where it does: the group's help can be fully proven offline (argument
// parsing, target resolution, checkpoint load, the refusal tables), but the
// CONTINUATION ROUND needs a provider credential. No credential is mocked into
// existence here (PROJECT.md Rule 31's honest limit): the case asserts the entry
// point was reached — the sandbox has the checkpoint on disk and the failure
// names the client, not the argv.
func TestG4ProbeToTheAuthBoundaryIsHonest(t *testing.T) {
	fx := g4NewFixture(t)
	_, stderr, code := fx.sb.g4RunSplit(t, CliVerbResume, fx.raw)
	if code == 0 {
		t.Skip("a live provider credential is configured in this environment: the " +
			"resume row's happy path ran for real, which is strictly more than this " +
			"test asserts")
	}
	if !strings.Contains(stderr, "Error creating agent") {
		t.Errorf("resume's offline limit must be the AGENT BOUNDARY (checkpoint "+
			"loaded, client construction failed) — not an argument or resolution "+
			"error:\n%s", stderr)
	}
	if strings.Contains(stderr, "neither a raw log path nor a session id") {
		t.Errorf("the fixture's raw log did not resolve — the probe never reached "+
			"the entry point:\n%s", stderr)
	}
}
