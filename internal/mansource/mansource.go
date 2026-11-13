// Package mansource is the ONE reader and writer of the generated blocks that
// keep a man source under man/ and the `-h` page the binary prints the SAME
// source of truth (operator decision, 2026-10-11, epic "man page 方案 A").
//
// WHY A SHARED PACKAGE. Two consumers need to agree about what a man source
// carries and what `gopi <verb> -h` says: the generator that REWRITES the
// blocks (internal/standards/mancensus, a main package) and the gate that
// REFUSES drift (internal/standards). A parser living in either one would force
// the other to carry a copy, and two copies of "how a flag row is read" is the
// drift this package exists to prevent (Rule 13/14: one definition, referenced
// everywhere the value is needed).
//
// WHAT IS SHARED AND WHAT IS NOT. The PARSERS below read the help page's own
// shapes — the usage rows, the verb-index rows, the flag-headed option rows —
// and the MARKERS/block helpers address the generated regions of a man source.
// The prose of a man page is NOT generated: it is written by a human in man/,
// reviewed like any other document, while the spellings it quotes (the verb
// list, the synopsis, the flags) come from the parsers here.
//
// The parsers are pure string functions: no filesystem, no process, no network.
// The caller owns "where the help text came from" (the built binary in the
// generator, the contract suite's own binary in the gate), so the two consumers
// can disagree about nothing except their inputs — which is exactly the
// disagreement the gate measures.
package mansource

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// The generated-block markers. A man source carries each generated region
// between its own pair; the generator replaces the body, the gate compares it.
// They are named here once (Rule 1: a meaningful string literal is a constant).
const (
	// IndexBegin/IndexEnd delimit the TOP-LEVEL verb index of `man gopi`.
	IndexBegin = "<!-- BEGIN VERB INDEX (generated — internal/standards/mancensus; do not edit by hand) -->"
	IndexEnd   = "<!-- END VERB INDEX -->"
	// SynopsisBegin/SynopsisEnd delimit the usage block copied verbatim from
	// the verb's own `-h` page. It is the same text a user sees at the top of
	// `gopi <verb> -h`, which is what makes "same source" checkable rather
	// than asserted.
	SynopsisBegin = "<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->"
	SynopsisEnd   = "<!-- END SYNOPSIS -->"
)

// generatedBlockAt reports the GENERATED block a 1-based line number falls
// inside, and its name — `SYNOPSIS`, `VERB INDEX`, `IMAGE SELF-CHECK`, …, the
// word between the marker's `BEGIN ` and the first ` (`. Both answers are empty
// for a line outside every region, which is the PROSE case a caller must judge.
//
// A BEGIN marker with no END after it is reported as an OPEN REGION rather than
// as prose. That direction is the conservative one: the region is broken, and
// telling the caller "this line is prose nobody generated" would blame the
// author for a document the generator mangled (Rule 15).
func generatedBlockAt(lines []string, lineNo int) (string, bool) {
	block := ""
	for i, line := range lines {
		if name, opens := blockMarkerName(line, blockBeginMarker); opens {
			block = name
		} else if _, closes := blockMarkerName(line, blockEndMarker); closes && i+1 <= lineNo {
			block = ""
		}
		if i+1 == lineNo {
			return block, block != ""
		}
	}
	return "", false
}

// blockBeginMarker/blockEndMarker are the generated region's delimiters. They
// are matched by PATTERN rather than by the three spelled constants above,
// because the man sources carry regions this package does not own (`IMAGE
// SELF-CHECK` is written by cmd/agent) and a reader that only knew its own
// three names would classify a real generated region as prose (Rule 15).
const (
	blockBeginMarker = "<!-- BEGIN "
	blockEndMarker   = "<!-- END "
	// blockMarkerClose ends a marker, and blockNameQualifier separates a
	// block's NAME from the parenthetical that says who writes it — the two
	// pieces of a marker's shape this reader has to parse.
	blockMarkerClose   = "-->"
	blockNameQualifier = " ("
)

// blockMarkerName reports whether a line is a generated-region marker, and the
// block's NAME when it is: the word between `BEGIN ` (or `END `) and the first
// ` (`, with the trailing `-->` removed for the names that carry no qualifier.
func blockMarkerName(line, prefix string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimSpace(trimmed[len(prefix):]), blockMarkerClose)
	if i := strings.Index(name, blockNameQualifier); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name), true
}

// Rows are the shapes a help page is read through. Each is defined once, here.
var (
	// verbRow matches a top-level index row head: `  gopi chat <subcommand>  …`.
	// Only rows at a two-space indent are read, so a command mentioned inside a
	// wrapped description cannot be mistaken for a listed verb.
	verbRow = regexp.MustCompile(`(?m)^  gopi ([a-z][a-z0-9_-]*)(?:\s|$)`)
	// subcommandRow matches a SUBCOMMAND row of a verb's OWN help page:
	// `  gopi session gc [--apply] …`. Both words are captured, so a caller
	// reading `gopi session -h` cannot mistake a row it saw on another verb's
	// page for one of this verb's — the verb is re-checked against the argument
	// rather than assumed from the page it came from. A usage row whose second
	// word is not a command word (`gopi merge <branch>`, `gopi tool <command>`)
	// does not match at all, which is what keeps a metavariable out of the
	// subcommand set.
	subcommandRow = regexp.MustCompile(`(?m)^  gopi ([a-z][a-z0-9_-]*) ([a-z][a-z0-9_-]*)(?:\s|$)`)
	// UsageHeader opens the usage block on the pages that print a standalone
	// one, and it is EXPORTED because the two consumers that must agree about
	// "a page has a usage block" are in different packages: this reader and the
	// form census in cmd/agent. A second spelling of `Usage:` there is how the
	// two would silently disagree (Rule 13/14).
	UsageHeader = "Usage:"
	// gopiPrefix is the token every usage row addresses the program by.
	gopiPrefix = "gopi "
	// blankLine splits a help page into its blocks.
	blankLine = regexp.MustCompile(`(?m)^\s*$`)
)

// HelpFlagTokens returns every long flag spelling a help page prints AS A ROW —
// the whole line of any line whose head begins a flag row, read across the
// column gap.
//
// It is deliberately wider than FlagTokens, and the width is the help
// formatter's doing, not a loosening for convenience: a row carrying two flags
// draws the second one PAST the column gap (`  --cols N      / --rows N`), so a
// head-only reader would under-count the page's own vocabulary and the caller
// would refuse a man page for naming a flag the page plainly prints. The scope
// that keeps this honest is the ROW: a flag merely mentioned inside a
// description is on the same line as its row's head and is therefore not
// claimed by this reader, which is what stops a cross-reference from being
// mistaken for a spelling the page owns.
func HelpFlagTokens(help string) []string {
	return rowFlags(help, func(line string) string { return line })
}

// FlagTokens returns every long flag spelling a help page prints in a row HEAD,
// in the order it first prints them. See HelpFlagTokens for the row-wide reader
// and for why the two exist.
func FlagTokens(help string) []string {
	return rowFlags(help, rowHead)
}

// rowFlags collects the long flags of every flag row, taking each row's
// candidate text from pick (the whole line, or just the head).
func rowFlags(help string, pick func(string) string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, line := range strings.Split(help, "\n") {
		if !startsFlagRow(line) {
			continue
		}
		for _, tok := range longFlag.FindAllString(pick(line), -1) {
			if seen[tok] {
				continue
			}
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out
}

// longFlag matches a long flag spelling, `--voice-audio` included.
var longFlag = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// startsFlagRow reports whether a line's head begins a flag row: a trimmed
// leading `--flag`, or a short/long pair (`-v, --verbose`).
func startsFlagRow(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if trimmed == "" || !strings.HasPrefix(trimmed, "-") {
		return false
	}
	return strings.HasPrefix(trimmed, "--") || shortFlagRow.MatchString(trimmed)
}

// shortFlagRow matches `-v, --verbose …` and `-p, --prompt …` row heads.
var shortFlagRow = regexp.MustCompile(`^-[a-zA-Z], --[a-z]`)

// rowHead returns the flag column of a help row: everything before the
// two-space column gap that separates a row's head from its description. The
// row's own INDENT is skipped first — the indent is itself a run of spaces, so a
// head read from the raw line would be empty for every indented row, which is
// every row in this formatter's output. A row whose head itself contains a
// two-space run (none in this formatter's output) would simply end its head
// earlier, which is the conservative direction.
func rowHead(line string) string {
	body := strings.TrimLeft(line, " \t")
	if i := columnGap.FindStringIndex(body); i != nil {
		return body[:i[0]]
	}
	return body
}

// columnGap is the help formatter's head/description separator.
var columnGap = regexp.MustCompile(`  +`)

// ManFlagTokens returns every long flag spelling a MARKDOWN man source names,
// in the order it first names them.
//
// It is a separate reader from FlagTokens because the two documents have two
// different shapes, and reading one with the other's parser is how a check
// silently measures nothing: a markdown row head is written as a backtick span
// (“--dump-screen[=PATH]“), so the help page's "the line begins with a dash"
// test would find NO flag in a man source at all. The markdown reader therefore
// strips backticks first and then applies the help page's own row test (`the
// head is the text up to the column gap`), which keeps "a row head" ONE
// definition even though the two documents spell it differently.
//
// A flag merely MENTIONED in a paragraph is not a row head, and is correctly
// not measured here: prose about a flag is not a claim to list it.
//
// A FLAG ROW IS A DEFINITION-LIST PAIR, AND THAT TEST IS THE WHOLE OF IT. The
// man sources document flags as markdown definition lists: a head line that is
// nothing but flag spellings (`--once`, `-p [PROMPT]`) followed IMMEDIATELY by
// a line whose first character is the list's `:` marker. Only such a head is a
// claim to list a flag, and only its own line is read. Two shapes that begin
// with a flag are therefore NOT measured, and each was measured as a real
// false positive before this rule existed (man/gopi-session.md, 2026-10-25):
//
//  1. a WRAPPED CONTINUATION LINE of the generated SYNOPSIS — the block quotes
//     `gopi session -h` verbatim, wrapping included, so a description line
//     indented under `  gopi session replay <id> [--terminal N]` begins with
//     `--turn N prints one in full`. That line is the HELP PAGE's own prose
//     quoted back, not a statement this man page makes;
//  2. a PROSE PARAGRAPH that begins with a backticked foreign flag — "the chat
//     verb's `--replay` **re-runs** the recorded session" is a sentence about
//     ANOTHER verb's flag. Reading it as "gopi session lists --replay" refuses
//     a page for saying the opposite of what it said.
//
// Neither shape has a `:` marker line under it, so the pair test drops both
// without looking at their words; an invented flag written as a real definition
// row keeps the description line and still fails the caller's comparison. The
// price is that a flag listed in a run of several rows before ONE shared
// description (`gopi session -h`'s TURNS/REQUEST/LOGGED/PASSES run) leaves the
// measurement — the helper form below is those flags' door back in.
//
// IT IS AN ANSWER, NOT A REPORT. A caller that must JUDGE what this reader
// declined to measure needs ManFlagReport, which returns the same Tokens beside
// the declined lines; this function is that report's Tokens and stays for the
// callers (and the red proofs) that only want the flags.
func ManFlagTokens(md string) []string {
	return ManFlagReport(md).Tokens
}

// FlagReport is what the markdown flag reader MEASURED and what it DECLINED to
// measure, in one value.
//
// WHY IT EXISTS. The reader's Rule 15 trace used to leave the process only as a
// line on stderr, and stderr is not a channel a test conclusion carries: a
// plain `go test` (no `-v`) swallows the writing test binary's output whole, so
// the notice was written and nobody read it — the comment's promise ("never
// silently dropped") held only under `-v`. That is the shape Rule 15 exists to
// forbid, one order of magnitude down: the trace existed, and the thing that
// was supposed to judge it could not see it. A caller that must GRADE the
// notice therefore needs it as DATA — the line's number, its text, and whether
// it sits inside a generated block — and that is this type.
type FlagReport struct {
	// Tokens are the flags the reader measured, in first-mention order — the
	// same answer ManFlagTokens returns.
	Tokens []string
	// Unmeasured are the flag-smelling lines it declined to measure, one entry
	// per line, in document order.
	Unmeasured []UnmeasuredLine
}

// UnmeasuredLine is one flag-smelling line the reader did not measure.
type UnmeasuredLine struct {
	// Line is the 1-based line number in the source, which is what a failure
	// message must name: a reader who has to grep for the text is being handed
	// a search problem instead of a location.
	Line int
	// Text is the line with its backticks stripped and its outer space trimmed
	// — the word string the flag reader itself scanned, so the report and the
	// scan can never describe different bytes.
	Text string
	// Block is the name of the GENERATED block the line falls inside
	// (`SYNOPSIS`, `VERB INDEX`, …), empty when the line is in prose.
	Block string
	// Flags are the long flag spellings the line carries and the reader did
	// NOT measure. They are the reason the line is in this list at all, and a
	// caller must judge THEM: a declined line is still a sentence that names a
	// flag, and a sentence naming a flag no page of the CLI prints is the
	// invented-flag defect — it would otherwise escape the vocabulary check
	// precisely because the reader declined to measure the line.
	Flags []string
	// InGeneratedBlock says whether the line falls inside a `<!-- BEGIN … -->`
	// region. It is a separate field from Block so a caller can branch on the
	// property without matching a name: generated regions are checked
	// STRUCTURALLY by their own gate (the SYNOPSIS is compared verbatim
	// against `gopi <verb> -h`), so a line inside one is already accounted
	// for; a line in prose is not, and must carry a reason.
	InGeneratedBlock bool
}

// ManFlagReport measures a man source's flag rows and reports the lines the
// measurement declined.
//
// It keeps the Rule 15 stderr notice ManFlagTokens has always written — the
// report is an ADDITION to that trace, never a replacement: a reader running
// `go test -v`, or a maintainer running the gate by hand, still sees the line,
// and a caller that collects the report does not have to trust a channel it
// cannot read. One notice is written per LINE (the report's own granularity)
// rather than one per flag spelling on it.
func ManFlagReport(md string) FlagReport {
	tokens, unmeasured := scanFlagRows(md)
	report := FlagReport{Tokens: tokens, Unmeasured: unmeasured}
	for _, u := range report.Unmeasured {
		fmt.Fprintf(os.Stderr, unmeasuredNoticeFmt, u.Text)
	}
	return report
}

// containsString reports list membership.
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// unmeasuredNoticeFmt is the Rule 15 trace, one definition (Rule 1/13).
const unmeasuredNoticeFmt = "mansource: %q begins a flag row but is neither a definition row nor a documented flag spelling — not measured\n"

// scanFlagRows is the ONE scanner behind ManFlagTokens and ManFlagReport: it
// records the flag rows it measures and the flag-smelling lines it declines.
func scanFlagRows(md string) ([]string, []UnmeasuredLine) {
	seen := make(map[string]bool)
	var tokens []string
	var unmeasured []UnmeasuredLine
	record := func(tok string) {
		if !seen[tok] {
			seen[tok] = true
			tokens = append(tokens, tok)
		}
	}
	lines := strings.Split(md, "\n")
	for i, line := range lines {
		lineNo := i + 1
		bare := strings.ReplaceAll(line, backtick, "")
		if !startsFlagRow(bare) {
			continue
		}
		if i+1 < len(lines) && startsDescription(lines[i+1]) {
			for _, tok := range longFlag.FindAllString(rowHead(bare), -1) {
				record(tok)
			}
			continue
		}
		var declined []string
		for _, tok := range longFlag.FindAllString(bare, -1) {
			if flagDefinitioned(lines, tok) {
				record(tok)
				continue
			}
			if !containsString(declined, tok) {
				declined = append(declined, tok)
			}
		}
		if len(declined) == 0 {
			continue
		}
		// Rule 15: a flag-smelling line that is measured for NO reason is
		// reported, never silently dropped — a narrowing this wide must leave
		// a trace a reader can see, or the next false negative looks exactly
		// like a clean document.
		block, inBlock := generatedBlockAt(lines, lineNo)
		unmeasured = append(unmeasured, UnmeasuredLine{
			Line:             lineNo,
			Text:             strings.TrimSpace(bare),
			Flags:            declined,
			Block:            block,
			InGeneratedBlock: inBlock,
		})
	}
	return tokens, unmeasured
}

// startsDescription reports whether a man-source line opens a definition-list
// description: markdown's `: ` marker at the start of the line, which is how
// every flag's description is written (indented or not — `gopi ui_state`'s
// rows are flush left, `gopi chat`'s are not, and the marker is the same).
func startsDescription(line string) bool {
	return strings.HasPrefix(line, descriptionMarker)
}

// descriptionMarker begins a markdown definition-list description.
const descriptionMarker = ":"

// flagDefinitioned reports whether the document writes tok as a DOCUMENTED flag
// spelling: either it is the operand of a definition-list term — the same
// row of a multi-flag head, or the head line right after a description block,
// which is the shape of `gopi session -h`'s TURNS/REQUEST/LOGGED/PASSES run —
// or the shared description under that run names it in the helper form
// (`--turns N     one per completed answer`).
//
// It is deliberately not "the flag appears anywhere in the document": that is
// the false positive this reader exists to stop. It asks the narrower question
// a prose mention cannot answer YES to — is this spelling a TERM or a helper of
// a term — and it keeps the caller's refusal arm reachable for every flag a man
// page really claims.
func flagDefinitioned(lines []string, tok string) bool {
	for i, line := range lines {
		bare := strings.ReplaceAll(line, backtick, "")
		if !strings.Contains(bare, tok) {
			continue
		}
		head := strings.TrimLeft(bare, " \t")
		// The term itself: the flag sits in a row head whose next line is the
		// `:` description, or in a term line that follows a description block.
		if startsFlagRow(bare) && (i+1 < len(lines) && startsDescription(lines[i+1]) || i > 0 && startsDescription(lines[i-1])) {
			if strings.Contains(rowHead(bare), tok) {
				return true
			}
		}
		// The helper form: a flag spelled inside the shared description under a
		// run of terms, introduced by its own indentation and its own value.
		if flagSpellingStart.MatchString(head) && strings.HasPrefix(head, tok) {
			return true
		}
	}
	return false
}

// flagSpellingStart matches the helper form's opening: a flag spelling at the
// start of a description line, followed by a value slot and the column gap the
// help formatter uses (`--turns N     one per completed answer`). The gap is
// what separates a defined spelling from a sentence that merely opens with a
// backticked flag — the false positive this reader must not re-admit.
var flagSpellingStart = regexp.MustCompile(`^--[a-z][a-z0-9-]*(\s+[A-Z][A-Z0-9]*)?\s{2,}\S`)

// backtick is markdown's inline-code delimiter, the span a man source writes a
// flag spelling in.
const backtick = "`"

// isWordByte reports whether b may begin or continue a command word — the
// letters, digits, dash and underscore the CLI's own verb names are made of.
func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_'
}

// HelpVerbs returns the top-level verbs a help page lists, sorted and
// deduplicated. Sorted because the caller (a generated index) must be stable
// across rewrites: an unstable order would rewrite the document on every run.
func HelpVerbs(help string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range verbRow.FindAllStringSubmatch(help, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// HelpSubcommands returns the SUBCOMMAND words a verb's own help page lists,
// sorted and deduplicated. Sorted for the same reason HelpVerbs is: a caller
// that generates or compares a list must be stable across rewrites.
//
// WHY IT EXISTS, MEASURED. A verb's page is not the whole of that verb's
// surface: `gopi session -h` prints its subcommands' invocations and one
// sentence about each, while the FLAG ROWS those subcommands own live one page
// further down (`gopi session gc -h` is where `--trim-raw-log SIZE [--apply]`
// is a definition row). A flag vocabulary assembled from the top-level page and
// the verbs' pages alone therefore cannot see `--trim-raw-log` at all, and the
// man gate then reports a TRUE sentence about a REAL flag as an invented one —
// measured 2026-10-30 on man/gopi-session.md:155. The defect was in the reader's
// reach, not in the page, and this function is how the caller extends it.
//
// The verb argument is re-checked against every match rather than assumed from
// the page's identity, so a page that quotes another verb's row contributes
// nothing.
func HelpSubcommands(help, verb string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range subcommandRow.FindAllStringSubmatch(help, -1) {
		if m[1] != verb || seen[m[2]] {
			continue
		}
		seen[m[2]] = true
		out = append(out, m[2])
	}
	sort.Strings(out)
	return out
}

// UsageRows returns the verbatim rows of a help page's usage block — the
// `Usage:` line and everything up to the first blank line, with the `Usage:`
// token itself stripped from the head. The rows are returned as printed
// (wrapped continuation lines included, indentation preserved), because they
// are copied into the man source as a literal block and compared byte for byte.
//
// THREE help forms are handled, and each difference is measured rather than
// assumed. The TOP-LEVEL page prints a bare `Usage:` header with the rows
// beneath it; most verbs put the first command shape on the header line itself
// (`Usage: gopi chat [--tools] …`); and two verbs — `merge` and `worker` —
// print NO `Usage:` header at all, opening straight onto an indented
// `gopi <verb> …` block after the page's one-line title. Reading all three the
// same way is what lets one parser serve every page a man source is derived
// from, and it is why the headerless fallback below exists: a page whose block
// this reader could not find would report "no usage block", which is not a
// property of that page but of a reader that only knew two shapes.
//
// The headerless fallback is BOUNDED by the page's first section heading, so a
// prose line in a Notes tail that happens to begin `gopi <verb> ` is never
// mistaken for an invocation — see usageBlockStart, and
// TestUsageRowsDoesNotReadATailProseRow in this package's own guards.
func UsageRows(help string) []string {
	lines := strings.Split(help, "\n")
	start := usageBlockStart(lines)
	if start < 0 {
		return nil
	}
	var out []string
	if strings.HasPrefix(strings.TrimLeft(lines[start], " \t"), UsageHeader) {
		first := strings.TrimLeft(strings.TrimLeft(lines[start], " \t")[len(UsageHeader):], " \t")
		if first != "" {
			out = append(out, first)
		}
		start++
	} else {
		// The headerless form's first row IS its first line, indentation kept.
		out = append(out, lines[start])
		start++
	}
	for _, l := range lines[start:] {
		if strings.TrimSpace(l) == "" {
			break
		}
		out = append(out, l)
	}
	return out
}

// usageBlockStart is the index of the line a page's usage block begins on:
// the `Usage:` header when the page prints one, otherwise the first indented
// `gopi <verb> …` row. -1 when the page has neither, which the caller reports
// as a missing fact source rather than as an empty block.
//
// THE SEARCH IS BOUNDED BY THE PAGE'S FIRST SECTION HEADING, and that bound is
// not a formality. Without it the headerless scan walks the whole page, so a
// page that prints its invocations first and its PROSE later has its usage
// block read from a sentence in the Notes tail: measured (2026-10-11) on the
// shape `merge`/`worker` itself establishes — an indented line in Notes that
// happens to begin `gopi <verb> ` is accepted as "the first usage row", and the
// SYNOPSIS of the man page then quotes a paragraph instead of a synopsis. A
// page's own invocation block is always above its first section heading
// (`Options:`, `Notes:`, `Environment:`), because that is the shape
// `help_capability_first_test.go` gates: title, invocations, then sections.
// So the block lives where the scan starts, and the scan must stop before the
// prose does.
func usageBlockStart(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), UsageHeader) {
			return i
		}
	}
	for i, l := range lines {
		if isSectionHeading(l) {
			// The invocation block is behind us: everything from here on is
			// prose, and prose is not a synopsis (Rule 15 — the caller turns
			// this into a loud "no usage block", never a silent empty one).
			return -1
		}
		trimmed := strings.TrimLeft(l, " \t")
		// Indented, and a `gopi <word>` invocation: the shape every usage row
		// has, wherever on the page the block happens to start. The indent test
		// is what keeps a prose line beginning "gopi runs in other people's
		// repositories" out of the block.
		if l == trimmed || !strings.HasPrefix(trimmed, gopiPrefix) {
			continue
		}
		// The token after `gopi ` must be a WORD — a verb, or a flag-only row's
		// dash — so a prose line that happens to begin "gopi runs in other
		// people's repositories" is not mistaken for an invocation. The indent
		// test above is the other half: usage rows are indented, prose is not.
		rest := trimmed[len(gopiPrefix):]
		if rest == "" || !(isWordByte(rest[0]) || rest[0] == '-') {
			continue
		}
		return i
	}
	return -1
}

// isSectionHeading reports whether a line is one of the help formatter's
// section headings — `Usage:`, `Options:`, `Notes:`, `Environment:` — the
// bands a page is divided into: unindented, and ending in a colon. It is the
// one definition of "where a band starts", so the reader's bound and the
// shape gate's bound cannot drift apart.
func isSectionHeading(line string) bool {
	if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(line, " \t"), sectionHeadingSuffix)
}

// sectionHeadingSuffix ends every section heading the help formatter prints.
const sectionHeadingSuffix = ":"

// Block returns the body a generated region holds: the body text, its trailing
// newline included, wrapped in the markers the caller names.
func Block(begin, end, body string) string {
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return begin + "\n" + body + end + "\n"
}

// ExtractBlock returns the body between begin and end in doc. Markers missing
// or out of order is an ERROR, never an empty body: a document whose generated
// region silently vanished must not read as "nothing to compare" (Rule 15).
func ExtractBlock(doc, begin, end string) (string, error) {
	start := strings.Index(doc, begin)
	if start < 0 {
		return "", fmt.Errorf("mansource: begin marker %q not found", begin)
	}
	start += len(begin)
	stop := strings.Index(doc[start:], end)
	if stop < 0 {
		return "", fmt.Errorf("mansource: end marker %q not found after the begin marker", end)
	}
	return strings.Trim(doc[start:start+stop], "\n") + "\n", nil
}

// ReplaceBlock swaps the region between begin and end in doc for body and
// returns the whole document. It is the writer half of ExtractBlock and refuses
// exactly the same malformed shape.
func ReplaceBlock(doc, begin, end, body string) (string, error) {
	fileBlock := Block(begin, end, body)
	start := strings.Index(doc, begin)
	if start < 0 {
		return "", fmt.Errorf("mansource: begin marker %q not found", begin)
	}
	stop := strings.Index(doc[start:], end)
	if stop < 0 {
		return "", fmt.Errorf("mansource: end marker %q not found after the begin marker", end)
	}
	stop += start + len(end)
	if stop < len(doc) && doc[stop] == '\n' {
		stop++
	}
	return doc[:start] + fileBlock + doc[stop:], nil
}

// CodeLines returns the lines inside the fenced code blocks of a markdown
// string, in order — the shape a generated synopsis block is written in. Text
// outside a fence is ignored, so the block's own prose cannot be mistaken for a
// synopsis row.
func CodeLines(md string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), fence) {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
		}
	}
	return out
}

// fence is the markdown code fence marker.
const fence = "```"
