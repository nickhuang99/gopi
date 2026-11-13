// Package cliindex reads the gopi CLI's TOP-LEVEL verb vocabulary out of the
// source that decides it, so a document about the CLI never carries a list
// somebody typed.
//
// WHY IT EXISTS. COMMANDS.md is the CLI's document of record, and the one
// property that keeps a reference page honest is that its facts come from the
// machine rather than from memory (the CONFIG.md discipline). The machine has
// two halves that must agree about which verbs exist:
//
//   - the verb WORDS: cmd/agent/cli_verbs.go's CliVerbXxx constants (plus
//     CliVerbAct, which lives with the act verb, cmd/agent/cli_act.go), and
//   - the ROUTING: cmd/agent/cli_contracts_test.go's dispatcherOfToken map,
//     whose keys are the first tokens the dispatch chain recognises.
//
// Neither is importable in the place that needs them — one is package main and
// the other is a _test.go file — so this package reads both as TEXT and joins
// them on the word. Its consumers are the generated block inside COMMANDS.md
// (rendered by internal/standards/testdata/commands-census.go) and the gate
// that refuses drift (internal/standards/commands_doc_gate_test.go), which
// therefore cannot disagree about what the CLI offers.
//
// It is deliberately stdlib-only, importable from anywhere, and holds no
// policy: it reports what the source says.
package cliindex

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// AgentDir is where the CLI lives: the ONE main package, cmd/agent.
	AgentDir = "cmd/agent"
	// verbsFile holds the canonical top-level verb constants.
	verbsFile = "cmd/agent/cli_verbs.go"
	// contractsFile maps each dispatched first token to its isXxx predicate.
	contractsFile = "cmd/agent/cli_contracts_test.go"
	// internalDir is where a constant imported by cmd/agent is resolved from.
	internalDir = "internal"

	// minVerbs is the read-floor: fewer verbs than this means the parse (or the
	// file's shape) broke, and a caller must fail rather than read green-blind.
	minVerbs = 30
	// minAliasRows is the read-floor for the address table when one is in force
	// (SetAliasAddresses): fewer refused words than this means the table was
	// not passed (or emptied), which would render every row "argv[0]" — a lie
	// in the shape of a green render — so the render REFUSES instead.
	minAliasRows = 10
)

// AliasAddress is a lookup from a word this binary REFUSES at argv[0] to the
// address that word is answered with. It is narrow on purpose: the table that
// answers "how is this word refused?" is the ONE authority on the ADDRESS, and
// it does not distinguish a retired door from a synonym — a fact it records
// separately (cmd/agent's verbAlias.retired) and nothing outside package main
// can read.
//
// The caller therefore decides what a non-empty address MEANS. The reference
// generator (internal/standards/testdata/commands-census.go) passes
// config.AliasAddresses and treats every hit as ROUTED ONLY through the address
// — the HONEST, nameable set: a word in that table either moved its door into
// a family (`capture` → `tool capture`) or was retired whole (the two web tool
// words) — and every word the lookup MISSES as an argv[0] spelling. A synonym
// (`buildinfo` → `version`) is indistinguishable from a retired door through
// this lens and is reported as routed-only, which is TRUE: it runs no command.
type AliasAddress func(word string) string

var aliasAddress AliasAddress

// SetAliasAddresses puts the refusal-address table in force for this process,
// so RenderTable can state where each word is answered. It is opt-in because
// the table lives in internal/config (package main's own half, read there) and
// a render WITHOUT it would silently mark every word "argv[0]": an unset table
// is therefore reported by the render itself (see RenderTable's read-floor)
// rather than being rendered as a fact.
func SetAliasAddresses(fn AliasAddress) { aliasAddress = fn }

// routedOnlyAddress returns the address a word is answered with, or "" when no
// table is in force or the word is not refused by name.
func routedOnlyAddress(word string) string {
	if aliasAddress == nil {
		return ""
	}
	return aliasAddress(word)
}

var (
	// constDecl matches a top-level constant inside a `const ( … )` block or a
	// single `const X = …` line, capturing the name and the right-hand side.
	// The RHS is either a quoted word or a qualified reference
	// (`tools.CliVerbMemoryGroup`) resolved separately.
	constDecl = regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Za-z][A-Za-z0-9_]*)\s*=\s*("([^"]*)"|[A-Za-z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*)\s*$`)
	// tokenEntry matches one dispatcherOfToken key line: `Key: "Predicate",`
	// where the key is a bare identifier, a qualified constant (toolnames.X) or
	// a quoted literal.
	tokenEntry = regexp.MustCompile(`(?m)^\s*(?:([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)?)|"([^"]*)")\s*:\s*"([A-Za-z0-9_]+)"\s*,`)
	// predicateDecl matches a dispatch predicate's definition line.
	predicateDecl = regexp.MustCompile(`(?m)^func (is[A-Za-z0-9]+Subcommand)\(args \[\]string\) bool \{`)
)

// Verb is one top-level CLI verb as the source defines it: the word a user
// types, the constant that owns the spelling, and the predicate that routes it.
// Predicate/PredFile/PredLine are empty for a token the contract map does not
// carry (a tool-wire word the dispatch chain does not route through a named
// predicate) — the caller decides whether that is a finding.
type Verb struct {
	Word      string // "archive"
	Const     string // "CliVerbArchive"
	ConstFile string // "cmd/agent/cli_verbs.go"
	ConstLine int    // 1-based line of the constant declaration
	// ConstAnchor is the SYMBOL a doc anchor for this verb's spelling must
	// name: `Const` when the spelling is a literal declared where the anchor
	// points, and the leaf package's own identifier when the spelling is
	// aliased (`CliVerbWorker = cliverbs.Worker` ⇒ `Worker`, declared one file
	// away). Without it the anchor would name a symbol its own file does not
	// declare — the false red a symbol identity must not produce (Rule 14).
	ConstAnchor string
	Predicate   string // "ArchiveSubcommand" (the isXxx predicate's name minus `is`/`Subcommand`… see PredicateFunc)
	PredFile    string // "cmd/agent/cli_archive.go"
	PredLine    int    // 1-based line of `func isArchiveSubcommand(…`
}

// PredicateFunc returns the predicate's full function name. Verb.Predicate is
// the contract map's own value, which is the function name with its `is` prefix
// stripped (`ArchiveSubcommand` ⇒ `isArchiveSubcommand`): readPredicates strips
// it to key the index, and this accessor puts it back. One spelling of that
// transformation, so the anchor renderer and the source reader cannot disagree
// about what the predicate is called.
func (v Verb) PredicateFunc() string { return "is" + v.Predicate }

// AnchorSep separates the file from the IDENTITY inside a doc anchor
// (`cmd/agent/cli_verbs.go:CliVerbArchive`). It is spelled once because the
// renderer of an anchor and every reader of one must agree on it: the gate
// builds its matcher out of this constant rather than typing the character, so
// the document and the check cannot drift apart on the separator while each one
// stays green on its own (Rule 1b/14).
const AnchorSep = ":"

// AnchorIdentityPatternSrc is the ONE spelling of an anchor's IDENTITY inside a
// document code span: a symbol name (`CliVerbArchive`, `isArchiveSubcommand`) or
// — the legacy form the hand-written prose still carries — a line number. It is
// exported so a reader of an anchor builds its matcher out of the same
// alternation the renderer writes, instead of typing a second one beside it
// (Rules 1b/13/14).
const AnchorIdentityPatternSrc = `[A-Za-z_][A-Za-z0-9_]*|\d+`

// DocRefPatternSrc is the ONE spelling of a whole anchor span —
// `path/to/file.go:<identity>` — the shape every document gate reads. The
// separator is interpolated from AnchorSep so a change of separator cannot leave
// the renderer and its readers green on their own while they disagree about what
// an anchor IS.
const DocRefPatternSrc = "`([A-Za-z0-9_./-]+\\.go)" + AnchorSep + "(" + AnchorIdentityPatternSrc + ")`"

// LegacyDocRefPatternSrc is DocRefPatternSrc RESTRICTED to the legacy line
// identity (`file.go:NNN`) — the shape that still has a line to land on, and
// therefore the only one the degenerate-line arm ("points at a lone `}`") can be
// about. It is derived from the same pieces rather than re-typed.
const LegacyDocRefPatternSrc = "`([A-Za-z0-9_./-]+\\.go)" + AnchorSep + `(\d+)` + "`"

// DocRefPattern and LegacyDocRefPattern are the compiled forms of the two
// source constants above (Rules 13/14: define once, reference everywhere).
var (
	DocRefPattern       = regexp.MustCompile(DocRefPatternSrc)
	LegacyDocRefPattern = regexp.MustCompile(LegacyDocRefPatternSrc)
)

// Ref renders the verb's constant as a doc anchor: `cmd/agent/cli_verbs.go:CliVerbArchive`.
//
// THE IDENTITY IS THE SYMBOL, NOT THE LINE (measured 2026-10-12 — the eighth
// round of this family). It used to be `cli_verbs.go:52`, and a line number is
// not an identity: it is the address of a name in ONE revision of ONE file, so
// any edit above it — a comment, a blank line, an unrelated function — moves it.
// `cmd/agent/cli_tasks.go:528`, written by the previous re-anchor (dcd25ce61),
// was invalidated four hours later by c6e93569e, a change to a DIFFERENT verb's
// help text: the block went stale and this family's gate went red while neither
// change had anything to say about the other. The symbol name is what the
// anchor MEANS ("the constant owning `gopi archive` is declared in this file"),
// and it survives every edit that does not rename or move the declaration —
// re-anchor by symbol identity, not by line (the criterion internal/agent's
// deleted doc-anchor rung applied, corpus lesson #3313; see the HISTORY note
// below). The line is still ON the struct (ConstLine) because resolution needs
// it to locate the declaration's home; it is simply no longer what the page
// publishes.
//
// HISTORY (2026-10-22/24), so a reader does not go looking for a rung that is
// gone. The "same criterion internal/agent applies" was, at the time, a live
// gate: the devdoc line-anchor gate resolved the corpus's line anchors,
// and it was deleted WHOLE with internal/docclass in 5a35f61 — nothing in
// internal/agent resolves a devdoc line anchor any more, and the corpus's own
// `loop.go:NNN` citations are unchecked prose.
//
// WHAT ENFORCES THIS DESIGN TODAY, and it is not nothing: the criterion is
// machine-checked for the GENERATED command index. `go run
// internal/standards/testdata/commands-census.go . COMMANDS.md --check` refuses
// a COMMANDS.md that disagrees with the source, and the merge lane reads the
// anchors that block publishes through this package's own exported spelling
// (`cliindex.AnchorSep` + `cliindex.AnchorIdentityPatternSrc`,
// internal/tools/merge_anchor_gate.go) — so a SYMBOL anchor that no longer
// resolves refuses the merge, which is exactly why this is symbol-shaped and why
// a line number would put it back on a ref that a neighbouring commit can move.
// Arms: internal/tools/merge_anchor_gate_refusal_test.go — NINE arms. This line
// named a file retired in 5a9af79bd until 2026-11-06; the record of that is kept
// below the list, because a reader who finds a dead name needs to know it was
// dead rather than merely misspelt.
//
// The DECISION layer, in full:
//
//	TestMergeAnchorGateRefusal_AnchorRefExtraction              the extraction contract, BOTH identity shapes
//	TestMergeAnchorGateRefusal_BlamesBranch                     the attribution, positive and negative
//	TestMergeAnchorGateRefusal_DetailContract                   the refusal sentence's assembly
//	TestMergeAnchorGateRefusal_DisabledAbstains                 the kill switch
//	TestMergeAnchorGateRefusal_PatternUsesTheOneExportedSpelling this package's own spelling is what the
//	                                                           pattern is BUILT from, not a second copy
//
// The REFUSAL PATH on real state (a real git repo and a real ledger):
//
//	TestTaskMergeRefuseAnchor_ArchivesAndNotifies               archive tag, refs/gopi/archive-reason WHY,
//	                                                           exemption, exactly one rework row, and dedup
//	                                                           on a second call
//	TestTaskMergeRefuseAnchor_NoMandateIsStatedNotSilent        the STATED fallback, never an empty element
//	TestTaskMergeRefuseAnchor_ArchiveFailureKeepsTheBranch
//	TestTaskMergeRefuseAnchor_FinishedOwnerIsNotBuried          the OWNER gate
//
// Measured 2026-11-06 on the tree this line was written in, and re-measured
// independently by a second session: `go tool cover -func` moves
// mergeAnchorGateBlamesBranch, mergeAnchorGateAnchorRef, mergeAnchorGateDetail,
// mergeAnchorGateDisabled, taskMergeRefuseAnchor and anchorGateMandateOrFallback
// from 0.0% to 100.0%, and anchorGateSubmitRework to 80.0% — its two remaining
// statements need a ledger write failure and a deliverable-stamp error, so they
// are NOT claimed.
// NOT COVERED, named so that nobody reads the paragraph above as "this edge is
// fully tested": `mergeAnchorGate` itself and the union lane
// (`mergeAnchorGateUnionTree`, `MergeTreeConflicts`, `AddedPaths`, `TreeBlobs`,
// `ChangedPaths`, `Scratch`, `TreeRun`, `SuiteRun`, `Judge`, `Runnable`,
// `FailingTests`, `Events`, `Timeout`, `AbstainReason`) still read 0.0%. They are
// reachable only through the four retired arms that declare
// heavytest.CriterionDeviceExec (`git show 5a9af79b^:internal/tools/merge_anchor_gate_test.go`,
// lines 342, 374, 387, 452 and 475), and a heavy arm is REFUSED inside a work
// context — restoring them verbatim would SKIP in every task run, which is a
// green that measures nothing. Covering that lane is an operator-terminal job
// (`make test-heavy`), not a task run's, so this line does not promise it.
//
// The record, kept short. This `Arms:` line used to name
// TestMergeAnchorGateReadsSymbolAnchors and TestMergeAnchorGateBlamesBranch — the
// spine of internal/tools/merge_anchor_gate_test.go — and that whole file was
// retired in 5a9af79bd (2026-10-28, subject verbatim: "test(merge): 退役 merge
// 路线的 95 个测试文件", whose body records "删除 95 个 merge 测试文件（28883 行）"
// and "一行生产 .go 未改"). `go test -run` on either of those old names prints
// `[no tests to run]` and exits 0, an empty green indistinguishable from a pass,
// which is why they are quoted here as DELETED SPELLINGS and never as checks.
// Separately, CONFIG.md's rows USED to carry
// the config-anchor census (`config-anchor-census.go --check`, its
// TestMergeConfigAnchorGate* arms in internal/tools) — a different table, not
// this one, and it was deleted with the documentation-citation family in
// 78bdf05a (2026-11-05). The SYMBOL anchors above are the check that still runs.
func (v Verb) Ref() string { return v.ConstFile + AnchorSep + v.anchorSymbol() }

// anchorSymbol is the name a doc anchor must carry: the declaring identifier
// (`ConstAnchor`) when the reader recorded one, and the constant's own name
// otherwise — so a Verb built by hand in a fixture still renders a usable
// anchor rather than an empty one.
func (v Verb) anchorSymbol() string {
	if v.ConstAnchor != "" {
		return v.ConstAnchor
	}
	return v.Const
}

// PredRef renders the routing predicate as a doc anchor:
// `cmd/agent/cli_archive.go:isArchiveSubcommand` — see Ref for why the identity
// is the symbol. The empty string means the contract map carries no predicate
// for this word, which is a finding its caller reports.
func (v Verb) PredRef() string {
	if v.PredFile == "" {
		return ""
	}
	return v.PredFile + AnchorSep + v.PredicateFunc()
}

// Load reads the verb vocabulary from the tree rooted at root.
func Load(root string) ([]Verb, error) {
	consts, err := readConsts(root)
	if err != nil {
		return nil, err
	}
	tokens, err := readTokenMap(root, consts)
	if err != nil {
		return nil, err
	}
	preds, err := readPredicates(root)
	if err != nil {
		return nil, err
	}

	byWord := map[string]*Verb{}
	// Every CliVerb* constant is a verb, whether or not a contract token names
	// it: a spelled word with no routing is a defect the gate reports, not a
	// word the index may silently drop.
	for _, name := range sortedKeys(consts) {
		if !strings.HasPrefix(name, "CliVerb") {
			continue
		}
		c := consts[name]
		v := &Verb{Word: c.value, Const: name, ConstFile: c.file, ConstLine: c.line, ConstAnchor: c.ident}
		if existing, ok := byWord[v.Word]; ok {
			// Two constants for one word (an alias pair) — keep the first by
			// name so the rendering is stable, and let the gate see the rest
			// through the constant list it reads itself.
			if existing.Const < v.Const {
				continue
			}
		}
		byWord[v.Word] = v
	}
	// Tokens routed by the contract map: fill in the predicate, and add any
	// word the constants do not declare (the tool-wire mirrors).
	for _, t := range tokens {
		v, ok := byWord[t.word]
		if !ok {
			v = &Verb{Word: t.word}
			byWord[t.word] = v
		}
		if v.Const == "" {
			v.Const, v.ConstFile, v.ConstLine, v.ConstAnchor =
				t.constName, t.constFile, t.constLine, t.constName
		}
		v.Predicate = t.predicate
		if p, ok := preds[t.predicate]; ok {
			v.PredFile, v.PredLine = p.file, p.line
		}
	}

	out := make([]Verb, 0, len(byWord))
	for _, v := range byWord {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Word < out[j].Word })
	if len(out) < minVerbs {
		return nil, fmt.Errorf("cliindex: parsed %d verbs (floor %d) — the verb source changed shape, refusing to report", len(out), minVerbs)
	}
	return out, nil
}

// Words returns the verb words alone, sorted.
func Words(verbs []Verb) []string {
	out := make([]string, 0, len(verbs))
	for _, v := range verbs {
		out = append(out, v.Word)
	}
	sort.Strings(out)
	return out
}

// --- rendering ---------------------------------------------------------------

// TableBegin and TableEnd delimit the GENERATED block inside a document. A
// document that carries them is rewritten by the generator between them and
// never anywhere else.
const (
	TableBegin = "<!-- BEGIN GENERATED: verb-index -->"
	TableEnd   = "<!-- END GENERATED: verb-index -->"

	// LevelColumnHeader names the column that states WHERE a word is reachable,
	// and SpellingColumnHeader the column that states which constant owns the
	// word (it was "spelling owned by" before the level column arrived; the
	// title moved to the constant so the header and the cell vocabulary cannot
	// drift).
	LevelColumnHeader    = "argv[0]?"
	SpellingColumnHeader = "spelling owned by"

	// LevelCellTopLevel and LevelCellRoutedOnly are the two values the level
	// cell can hold. They are constants (Rule 1) because the document and the
	// gate both read them by name: the gate asserts a refused word's row does
	// NOT claim the argv[0] spelling, and a hand-typed "no" would make that
	// assertion about a string nobody renders.
	LevelCellTopLevel   = "argv[0]"
	LevelCellRoutedOnly = "routed only"
)

// RenderTable renders the documented verb index as the markdown the generated
// block holds: one row per verb, WHERE the word is reachable (the argv[0]
// column), the routing predicate with its file:line, the verb constant with
// its file:line.
//
// THE argv[0] COLUMN IS THE FACT THE FIRST TWO COLUMNS COULD NOT STATE. The
// rows below are built from the verb CONSTANTS and the routing PREDICATES, and
// a word whose door moved into a family (`capture` → `tool capture`) still has
// both: a `CliVerbCapture` constant and an `isCaptureSubcommand` predicate that
// the dispatch chain consults for `gopi tool capture`. So
// `| `gopi capture` | `cmd/agent/cli_capture.go:133` | `CliVerbCapture` |` was
// byte-identical to a row for a word that runs at argv[0], and the table taught
// the refused spelling — measured 2026-10-06: `COMMANDS.md` §4 sent readers to
// thirteen commands that exit 1 (bisected to this generator's missing column).
//
// The column is rendered from the REFUSAL table (SetAliasAddresses), never
// guessed: a word the refusal answers is marked "only as `gopi <address>`", and
// every other word "argv[0]". No table in force is a REFUSAL, not a default —
// rendering the column without the authority would mark every refused word
// "argv[0]", which is the exact lie this column exists to remove.
func RenderTable(verbs []Verb) (string, error) {
	if aliasAddress == nil {
		return "", fmt.Errorf("cliindex: no refusal-address table in force — the argv[0] column would mark every word as a top-level spelling; call SetAliasAddresses first")
	}
	routedOnly := 0
	for _, v := range verbs {
		if routedOnlyAddress(v.Word) != "" {
			routedOnly++
		}
	}
	if routedOnly < minAliasRows {
		return "", fmt.Errorf("cliindex: the refusal-address table answered %d of %d verbs (floor %d) — the render would mark refused words as argv[0] spellings", routedOnly, len(verbs), minAliasRows)
	}
	var b strings.Builder
	b.WriteString("| verb | " + LevelColumnHeader + " | routed by | " + SpellingColumnHeader + " |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, v := range verbs {
		pred := "—"
		if v.PredRef() != "" {
			pred = "`" + v.PredRef() + "`"
		}
		own := "—"
		if v.ConstFile != "" {
			own = "`" + v.Const + "` (`" + v.Ref() + "`)"
		}
		b.WriteString("| `gopi " + v.Word + "` | " + LevelCell(v.Word) + " | " + pred + " | " + own + " |\n")
	}
	return b.String(), nil
}

// LevelCell states, for one verb word, WHERE the binary reaches it: the
// argv[0] spelling itself, or the family address the refusal answers with. It
// is the ONE renderer of that cell — the generator and the gate both go through
// it, so the document and the check cannot disagree about what a row says.
func LevelCell(word string) string {
	addr := routedOnlyAddress(word)
	if addr == "" {
		return LevelCellTopLevel
	}
	return LevelCellRoutedOnly + " `gopi " + addr + "`"
}

// LevelTopLevel reports whether a word is reachable as its own argv[0]
// spelling (the semantics LevelCell renders). A word the refusal table answers
// is NOT one, whatever the reason it is answered: a retired verb whose door
// moved into a family, a retired verb with no successor, and a synonym all run
// nothing at argv[0].
func LevelTopLevel(word string) bool { return routedOnlyAddress(word) == "" }

// Block returns the full generated block, markers included.
func Block(verbs []Verb) (string, error) {
	table, err := RenderTable(verbs)
	if err != nil {
		return "", err
	}
	return TableBegin + "\n" + table + TableEnd + "\n", nil
}

// ReplaceBlock swaps the generated block inside doc for body and returns the
// new document, or an error when the markers are missing or out of order.
func ReplaceBlock(doc, body string) (string, error) {
	start := strings.Index(doc, TableBegin)
	end := strings.Index(doc, TableEnd)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("cliindex: generated-block markers not found (or out of order) — wanted %q … %q", TableBegin, TableEnd)
	}
	end += len(TableEnd)
	return doc[:start] + strings.TrimRight(body, "\n") + doc[end:], nil
}

// ExtractBlock returns the text of the generated block inside doc (markers
// included), or an error when the document does not carry one.
func ExtractBlock(doc string) (string, error) {
	start := strings.Index(doc, TableBegin)
	end := strings.Index(doc, TableEnd)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("cliindex: generated-block markers not found (or out of order) — wanted %q … %q", TableBegin, TableEnd)
	}
	end += len(TableEnd)
	return doc[start:end] + "\n", nil
}

// --- source reading ----------------------------------------------------------

type constDeclSite struct {
	value string
	file  string
	line  int
	// ident is the SYMBOL a doc anchor for this constant must name: the leaf
	// package's own identifier when the right-hand side is a qualified
	// reference (`cli_verbs.go`'s `CliVerbWorker = cliverbs.Worker` ⇒ `Worker`),
	// and the constant's own name when the value is a literal. Carrying it here
	// is what keeps the anchor honest — the declaration site the anchor names
	// must be the site that DECLARES the symbol, not the alias line that merely
	// re-exported it (Rule 14's one definition).
	ident string
}

// readConsts indexes every string-valued constant declared under cmd/agent,
// keyed by name, and resolves qualified references (`tools.X`) against the
// internal/ package of that name.
func readConsts(root string) (map[string]constDeclSite, error) {
	out := map[string]constDeclSite{}
	files, err := goFilesIn(filepath.Join(root, AgentDir))
	if err != nil {
		return nil, err
	}
	// local: bare name -> site, for resolving qualified refs lazily.
	local := map[string]constDeclSite{}
	var qualified []struct {
		name string
		pkg  string
		id   string
		file string
		line int
	}
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			return nil, fmt.Errorf("cliindex: %s is not under %s: %w", f, root, err)
		}
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("cliindex: read %s: %w", rel, err)
		}
		for _, m := range constDecl.FindAllStringSubmatchIndex(string(src), -1) {
			name := string(src[m[2]:m[3]])
			line := 1 + strings.Count(string(src[:m[0]]), "\n")
			if m[6] >= 0 { // quoted literal
				site := constDeclSite{value: string(src[m[6]:m[7]]), file: rel, line: line, ident: name}
				local[name] = site
				out[name] = site
				continue
			}
			ref := string(src[m[4]:m[5]]) // pkg.Ident
			pkg, id, _ := strings.Cut(ref, ".")
			qualified = append(qualified, struct {
				name string
				pkg  string
				id   string
				file string
				line int
			}{name, pkg, id, rel, line})
		}
	}
	for _, q := range qualified {
		// Only the CLI-verb constants are worth resolving: every OTHER
		// qualified right-hand side in cmd/agent (syscall.EBADF, time.Second,
		// config.SomeKnob, …) is not a command word, and resolving each one
		// would read the whole internal/ tree per hit. The token map's own
		// lookups (toolnames.X) go through resolveQualifiedConst directly and
		// stay LOUD — an error there is returned, not skipped.
		if !strings.HasPrefix(q.name, "CliVerb") {
			continue
		}
		value, file, line, err := resolveQualifiedConst(root, q.pkg, q.id)
		if err != nil {
			return nil, err
		}
		out[q.name] = constDeclSite{value: value, file: file, line: line, ident: q.id}
	}
	return out, nil
}

// resolveQualifiedConst finds `<pkg>.<id> = "…"` under internal/<pkg>/ and
// returns the literal with the file:line that declares it — so a verb whose
// spelling lives in a leaf package is documented at its real home (Rule 14),
// never at the alias site.
func resolveQualifiedConst(root, pkg, id string) (string, string, int, error) {
	dir := filepath.Join(root, internalDir, pkg)
	files, err := goFilesIn(dir)
	if err != nil || len(files) == 0 {
		return "", "", 0, fmt.Errorf("cliindex: cannot resolve %s.%s — no Go files under %s", pkg, id, filepath.ToSlash(filepath.Join(internalDir, pkg)))
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range constDecl.FindAllStringSubmatchIndex(string(src), -1) {
			if string(src[m[2]:m[3]]) != id || m[6] < 0 {
				continue
			}
			rel, err := filepath.Rel(root, f)
			if err != nil {
				return "", "", 0, fmt.Errorf("cliindex: %s is not under %s: %w", f, root, err)
			}
			return string(src[m[6]:m[7]]), filepath.ToSlash(rel), 1 + strings.Count(string(src[:m[0]]), "\n"), nil
		}
	}
	return "", "", 0, fmt.Errorf("cliindex: %s.%s is not declared as a string constant under %s", pkg, id, filepath.ToSlash(dir))
}

type tokenSite struct {
	word      string
	predicate string
	constName string
	constFile string
	constLine int
}

// readTokenMap reads dispatcherOfToken: the contract suite's map from a
// dispatched first token to the isXxx predicate that routes it.
func readTokenMap(root string, consts map[string]constDeclSite) ([]tokenSite, error) {
	path := filepath.Join(root, filepath.FromSlash(contractsFile))
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cliindex: read %s: %w", contractsFile, err)
	}
	body := string(src)
	start := strings.Index(body, "var dispatcherOfToken = map[string]string{")
	if start < 0 {
		return nil, fmt.Errorf("cliindex: %s no longer declares dispatcherOfToken — update this reader", contractsFile)
	}
	body = body[start:]
	if end := strings.Index(body, "\n}"); end > 0 {
		body = body[:end]
	}
	var out []tokenSite
	for _, m := range tokenEntry.FindAllStringSubmatch(body, -1) {
		key, lit, pred := m[1], m[2], m[3]
		t := tokenSite{predicate: pred}
		switch {
		case lit != "":
			t.word = lit
		case key != "":
			name := key
			if pkg, ident, ok := strings.Cut(key, "."); ok {
				// A qualified key (toolnames.ToolWebSearch) is resolved by its
				// identifier's own declaration, the same lookup the constants use.
				value, file, line, err := resolveQualifiedConst(root, pkg, ident)
				if err != nil {
					return nil, err
				}
				t.word, t.constName, t.constFile, t.constLine = value, ident, file, line
				out = append(out, t)
				continue
			}
			c, ok := consts[name]
			if !ok {
				return nil, fmt.Errorf("cliindex: dispatcherOfToken key %q resolves to no constant under %s — a rename must update this reader and the doc together", name, AgentDir)
			}
			t.word, t.constName, t.constFile, t.constLine = c.value, name, c.file, c.line
		}
		out = append(out, t)
	}
	if len(out) < minVerbs {
		return nil, fmt.Errorf("cliindex: dispatcherOfToken parse yielded %d tokens (floor %d) — refusing to report", len(out), minVerbs)
	}
	return out, nil
}

type predSite struct {
	file string
	line int
}

// readPredicates indexes every isXxxSubcommand definition under cmd/agent.
func readPredicates(root string) (map[string]predSite, error) {
	files, err := goFilesIn(filepath.Join(root, AgentDir))
	if err != nil {
		return nil, err
	}
	out := map[string]predSite{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			return nil, fmt.Errorf("cliindex: %s is not under %s: %w", f, root, err)
		}
		text := string(src)
		for _, m := range predicateDecl.FindAllStringSubmatchIndex(text, -1) {
			full := text[m[2]:m[3]] // ArchiveSubcommand
			name := strings.TrimPrefix(full, "is")
			out[name] = predSite{file: filepath.ToSlash(rel), line: 1 + strings.Count(text[:m[0]], "\n")}
		}
	}
	return out, nil
}

func goFilesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cliindex: read dir %s: %w", filepath.ToSlash(dir), err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
