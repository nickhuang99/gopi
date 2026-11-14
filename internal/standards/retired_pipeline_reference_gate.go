package standardgate

// retired_pipeline_reference_gate.go — the scan half of Rule 36's
// retired-surface gate (its check lives in
// retired_pipeline_reference_gate_test.go, `TestRetiredPipelineGate*`).
//
// WHY IT EXISTS. The document pipeline was removed in slices (e6bfa10 cut
// `gopi tool doccheck` + internal/docindex; 29882b3 dropped its gates; the
// classifier `tools/docclass/` went with them). The annotation sweep that made
// the corpus stop teaching those surfaces was a ONE-OFF — nothing checked that
// it had happened, and nothing keeps a later commit from teaching them again.
// Rule 36 states the promise ("classified in the SAME commit"); this file is
// the part of it that is checkable: a document that still INVOKES a retired
// capability must say, EARLIER IN THE FILE, that the mechanism is retired.
//
// WHAT IT JUDGES. Four roots, and the fourth is the one this gate was blind to
// until 2026-11-01:
//
//   - the corpus (`devdoc/`), class-scoped: a file whose declared class is
//     `normative` or `roadmap` is a STANDING claim, the kind a reader acts on,
//     and is judged; a `historical` document is one run's record, which must be
//     free to quote what was typed that day, so it is counted but not judged.
//   - the man pages (`man/`), the official promise surface, held to the
//     STRICTER man rule below.
//   - the module root's own documents, BY NAME (`retiredRefRootDocuments`:
//     `PROJECT.md`, `CONFIG.md`, `README.md`). This root's membership is data
//     because a name missing from a literal cannot be noticed by reading the
//     literal — `CONFIG.md`, the densest teaching surface the retired
//     documentation-citation family has, was absent from it while the comment
//     beside it claimed otherwise, and every invocation on that page was outside
//     the gate.
//   - the WORK ORDERS (`briefings/`), which carry NO class banner at all — a
//     briefing is what one run was told to do, so there is no banner to read and
//     nothing to filter on: every briefing is JUDGED, through the same generic
//     branch the root documents use (state the retirement ABOVE a command-position
//     invocation). This root was missing, and the cost was measured: 292 tracked
//     briefing files went unscanned while 6 of them opened a line with `gopi
//     merge` and 3 with `gopi task run` — both retired surfaces — and the gate
//     printed a green over them.
//
// THE THREE HALVES, because a "retired surface" is three different addresses and
// a reader can paste any of them:
//
//	document pipeline     `python3 tools/docclass/…`, `gopi tool doccheck` (its
//	                      own vocabulary, the original subject of this gate)
//	retired CLI verb      `gopi task <verb>` for every verb in
//	                      taskverbs.RemovedVerbs — DERIVED, never mirrored
//	retired top-level     `gopi <word>` for a command surface that was removed
//	                      outright (epic merge-removal: `gopi merge`)
//
// Each half is counted and floored SEPARATELY: a combined number is the shape in
// which a dead recognizer hides, because a healthy half keeps the total up while
// a broken one measures nothing. On a MAN PAGE the boundary differs and is not
// negotiable — a command-position invocation is refused with no escape (a SYNOPSIS
// row is an instruction to run the thing, so a note cannot license it), while a
// prose mention needs the retirement stated above it. That is information versus
// advertisement, and the page is the promise surface.
//
// WHAT IT NEVER DOES. It never writes a note and never writes a class: both are
// the author's statements, and a gate that guessed one would be the filename
// heuristic Rule 36 exists to have removed. It is a READER.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/devdoc"
	"github.com/nickhuang99/gopi/internal/taskverbs"
)

const (
	// retiredRefProvenance names who is speaking, so a failure attributed to
	// this gate is distinguishable from the other standards gates (Rule 1: the
	// spelling lives here, once).
	retiredRefProvenance = "Rule 36 retired-surface gate"

	// retiredRefNoteMarker is the ANCESTOR TEXT of the pipeline-specific shipped
	// note, kept for the failure messages and the fixtures that quote it.
	retiredRefNoteMarker = "Retired mechanism (document-pipeline teardown"

	// retiredRefNoteRecognizer is the GENERAL note recognizer, and it is what the
	// ORDER rule is measured against. Rule 36's corpus carries one note shape —
	// `Retired mechanism (…)` — used for every retirement the sweep has annotated
	// so far (the document pipeline, the memory-store sync decouple, the
	// delete-by-`4ab33ce8` slice). The recognizer is that shared phrase rather
	// than any one retirement's parenthetical, because the vocabulary below is now
	// DERIVED (it grows whenever a verb is retired) and a recognizer tied to one
	// mechanism would make every future retirement un-annotatable without editing
	// this gate.
	retiredRefNoteRecognizer = "Retired mechanism ("

	// The retired DOCUMENT-PIPELINE vocabulary (Rule 1/13/14, named once).
	// `tools/docclass` was the corpus's classifier; `doccheck` was the verb that
	// read its manifest. Both are gone from the tree and neither may come back as
	// a live command.
	retiredRefTokenClassifier = "tools/docclass"
	retiredRefTokenVerb       = "doccheck"

	// retiredRefTokenCensusGenerator is the SAME FAMILY's other address, and its
	// absence from this vocabulary was a measured HOLE rather than an oversight in
	// the sweep: the documentation-citation teardown (`78bdf05a`, `b8cad761`)
	// deleted three generators, and the pages that used to teach them
	// (`CONFIG.md` in thirteen places, `PROJECT.md` in three) still carry their
	// invocations. The recognizer knew only `tools/docclass`/`doccheck`, so a page
	// pasting `go run internal/standards/testdata/config-anchor-census.go … --check`
	// was invisible to a gate whose whole subject is "a reader must be told above
	// the command it cannot run". Measured on this rev (2026-11-05): with the token
	// absent, a BARE invocation planted at line 2 of `CONFIG.md` left
	// `TestRetiredPipelineGate` GREEN — the green was a vacuum, not a clean corpus.
	//
	// THE WHOLE FAMILY, because the teardown deleted three generators and a
	// reader can paste any of them. An earlier version of this vocabulary named
	// only the first and said the other two "were never taught by a page that
	// survives" — measurably false on the tree it shipped on:
	// `citation-census.go` stands at command position in `devdoc/citation-gate.md`
	// and `citation-selector-census.go` in `devdoc/cited-test-selector-gate.md`.
	// Those pages are classed `historical` (counted, not judged), which is the
	// state in which a missing member costs nothing today and everything the day
	// one page is re-classed or a new one teaches the address. So the members
	// live in ONE list (`retiredRefCensusGenerators`) and both patterns are
	// DERIVED from it (Rule 1/13/14): a fourth address is one row, and an arm
	// fails if the list ever drops below the family floor.
	retiredRefTokenCensusGenerator = "internal/standards/testdata/config-anchor-census.go"
	retiredRefTokenCitationCensus  = "internal/standards/testdata/citation-census.go"
	retiredRefTokenSelectorCensus  = "internal/standards/testdata/citation-selector-census.go"

	// retiredRefTokenMergeVerb is the removed TOP-LEVEL verb word (epic
	// merge-removal). It is named HERE, and not reached by derivation, because
	// its own home is gone: `cmd/agent/cli_verbs.go` no longer declares a
	// `CliVerbMerge` (its absence is documented there as the decision), and
	// `internal/cliverbs` removed its constant when the last reader left. What
	// survives in the tree are two DIFFERENT surfaces that must not be
	// conflated with this one:
	//
	//   - `cmd/agent/cli_verb_aliases.go`'s private `verbAliasMerge` — the
	//     REFUSAL row, unimportable (package main), which is what makes the word
	//     answer `gopi: `merge` is RETIRED — use `gopi task merge`` instead of
	//     falling through to the bare-prompt path;
	//   - `taskverbs.Merge` — the FAMILY spelling `gopi task merge`, which is
	//     LIVE and is the address the retirement points at.
	//
	// So this constant is the only spelling of the RETIRED top-level word a
	// gate can read, and the drift arm below (`…TopLevelVocabularyMatches…`)
	// pins it against the refusal table's own source line, so a rename there
	// cannot leave this gate judging a word nothing refuses.
	retiredRefTokenMergeVerb = "merge"

	// retiredRefAliasSourceRel and retiredRefAliasRowName are the DRIFT ANCHOR for
	// the constant above: the refusal row that keeps the retired top-level word
	// refused BY NAME, and the file that declares it. The row is `package main`,
	// so this gate cannot import it and cannot share a constant with it — the
	// dangerous rename is a one-sided one, so the arm
	// (…TopLevelVocabularyMatchesTheRefusalTable) reads that source and pins the
	// row's NAME and its VALUE to the vocabulary above.
	retiredRefAliasSourceRel = "cmd/agent/cli_verb_aliases.go"
	retiredRefAliasRowName   = "verbAliasMerge"

	// retiredRefCorpusRoot is the corpus whose files declare a class; the other
	// two roots are the promise surfaces that carry no banner and are judged
	// unconditionally.
	retiredRefCorpusRoot = "devdoc"
	retiredRefManRoot    = "man"

	// retiredRefBriefingRoot is the WORK-ORDER root (Rule 1/13/14: named once).
	// Its files are the instructions a dispatched run is handed, so they are the
	// most paste-prone prose in the tree — and they carry no doc-class banner,
	// which is why they are judged unconditionally rather than class-scoped.
	retiredRefBriefingRoot = "briefings"
)

// retiredRefTopLevelVerbs is the RETIRED TOP-LEVEL half of the vocabulary: words
// that USED to be a `gopi <verb>` command surface and no longer dispatch. It is
// a list rather than a single constant because the class is open (a future
// removal adds a row here and needs no new pattern), and it is NOT derived from
// `taskverbs.RemovedVerbs` on purpose: that list is the FAMILY surface
// (`gopi task <verb>`), a different address with a different surviving hint.
//
// WHY THE HALF EXISTS AT ALL. The CLI half judges `gopi task <retired>`; the
// top-level word is a different line a reader can paste, and on this tree it
// was judged by NOTHING. Measured before this entry was added (2026-11-01, this
// clone): a normative fixture carrying `$ gopi merge --status` scanned
// `clean 1`, `command-position invocation 0` — the gate printed a green over a
// planted invocation of a word the binary refuses.
var retiredRefTopLevelVerbs = []string{retiredRefTokenMergeVerb}

// retiredRefCLIVerbs is the retired-CLI half of the vocabulary, DERIVED from the
// single source of truth that already exists (Rule 14: one definition, in a leaf
// package every consumer can import; Rule 35: no new mechanism).
//
// WHY DERIVED. A hand-written list here is the defect this half repairs: the
// vocabulary used to name only the document pipeline's two tokens, so every CLI
// verb retired afterwards (`internal/taskverbs.RemovedVerbs` carries ten) landed
// in a blind spot — `gopi task finish-cleanup` was retired in `58b5618b` while
// normative pages went on teaching it at command position and the gate stayed
// green. Deriving means a future retirement needs NO edit in this file.
//
// It is sorted so the report and the failure text read the same on every run
// (map iteration order is not evidence of anything).
func retiredRefCLIVerbs() []string {
	verbs := append([]string(nil), taskverbs.RemovedVerbs...)
	sort.Strings(verbs)
	return verbs
}

// retiredRefCLIFamilyPattern recognizes a retired family verb at COMMAND
// POSITION: the first word of the line, after a prompt/indent/quote marker, in
// either the `gopi task <verb>` or the bare `task <verb>` spelling.
//
// COMMAND POSITION is the criterion, and it is the SAME one the pipeline half
// uses (Rule 36). It was not chosen for convenience — two wider predicates were
// MEASURED against this tree and rejected:
//
//   - `task <verb>` anywhere: `run` is an ordinary English noun here ("a task
//     run's OWN report", "the log a task run writes"), so 20+ prose lines matched
//     and told nobody to run anything.
//   - `gopi task <verb>` anywhere: 100+ normative documents matched, because a
//     page that DISCUSSES `gopi task run` in a sentence is doing the corpus a
//     service — punishing it would make the gate punish the fix.
//
// What must not stand unannotated is a line a reader could PASTE. The wide
// mention population (measured on this rev: 82 normative documents naming a
// retired verb somewhere, 119 at `task <verb>`) is deliberately NOT this gate's
// business; it is recorded in the run's report as the work it did not do.
//
// LEADING MARKERS are prompt and indentation ONLY — no `-`, no `*`, no backtick.
// A list item that names the spelling in code font (`- `+"`"+`gopi task run`+"`"+` started the run`)
// is a mention like any other, and admitting bullets was measured to turn 51
// documents red, nearly all of them prose. A command a reader can paste starts
// with whitespace or a prompt, and that is what is recognized.
//
// The verb must be FOLLOWED by a command boundary (whitespace, end of line, a
// closing delimiter or punctuation) so `task verify`, `task verify:` and
// `gopi task run\“ match while `task verified` and `task run-away` do not.
var retiredRefCLIFamilyPattern = regexp.MustCompile(
	`^[ \t]*\$?[ \t>]*(?:gopi[ \t]+)?task[ \t]+((?:` +
		strings.Join(retiredRefCLIVerbs(), `|`) +
		`))(?:[ \t]|$|[,;:)\]}.` + "`" + `])`)

// retiredRefCLIVerbsIn returns the retired family verbs a line teaches at
// command position. Empty means the line only mentions the word in prose.
func retiredRefCLIVerbsIn(line string) []string {
	var out []string
	for _, groups := range retiredRefCLIFamilyPattern.FindAllStringSubmatch(line, -1) {
		for i := 1; i < len(groups); i++ {
			if groups[i] != "" {
				out = append(out, groups[i])
			}
		}
	}
	return out
}

// retiredRefTopLevelPattern recognizes a RETIRED TOP-LEVEL VERB at command
// position: `gopi merge`, `./bin/gopi merge --status`, or a bare `  merge …`
// after a prompt. Same criterion as the other two halves (Rule 36: what a reader
// can PASTE), same leading-marker set (prompt and indentation only).
//
// THE BOUNDARY IS WHAT KEEPS IT HONEST, and it was measured before it was
// chosen: the verb must be followed by whitespace, end of line or a closing
// delimiter, so `gopi merge` matches while `gopi merger` (the RETIRED DAEMON's
// own name, discussed by normative pages for its own reasons) does not, and
// `gopi task merge` does not match at all because the word after `gopi` is
// `task`. The two surviving/different surfaces — the family spelling and the
// daemon — are therefore outside this half by construction rather than by an
// exception list.
// THE TERMINATING BOUNDARY IS MEASURED, not copied from the halves above, and
// it deliberately OMITS `:` — an omission this gate paid for once already.
// A retired verb's own OUTPUT prefix is `gopi merge: …` (the engine prints
// `gopi merge: holding the single-instance lock; …`, `gopi merge: run over — …`),
// so admitting a colon as a boundary reported six NORMATIVE pages as teaching a
// command when every one of them was quoting a past run's stdout inside a fenced
// block. Measured on this rev before the narrowing: 8 flagged files, 6 of them
// output quotes (`devdoc/merge-stop.md:118`, `merge-image-selfcheck.md:101`,
// `merge-unrelated-history-diagnosis.md:12`, `worker-merge-tick-retired.md:62`,
// `archive-retirement-absent-ledger.md:15`, `version-identity.md:193`), 2 real
// commands. Rule 36's criterion is the line a reader could PASTE, and nobody
// pastes `gopi merge: run over — 2 branch decision(s)`. Narrowing the recognizer
// to its stated criterion is the fix; annotating a hundred log quotes with a
// retirement note would be the wrong repair (and the wide mention population is
// recorded as work this gate does not do, exactly as the CLI half states).
var retiredRefTopLevelPattern = regexp.MustCompile(
	`^[ \t]*\$?[ \t>]*(?:\./bin/)?gopi[ \t]+((?:` +
		strings.Join(retiredRefTopLevelVerbs, `|`) +
		`))(?:[ \t]|$|[,;)\]}.` + "`" + `])`)

// retiredRefTopLevelVerbsIn returns the retired top-level words a line invokes
// at command position. Empty means the line only mentions the word in prose.
func retiredRefTopLevelVerbsIn(line string) []string {
	var out []string
	for _, m := range retiredRefTopLevelPattern.FindAllStringSubmatch(line, -1) {
		if len(m) > 1 && m[1] != "" {
			out = append(out, m[1])
		}
	}
	return out
}

// retiredRefInvocationPattern recognizes a retired surface at COMMAND POSITION:
// a shell prompt (or a bare command line / a fenced block body) whose first
// word is the classifier's path or the retired verb in any of its spellings.
//
// Command position, not mere mention, is the criterion. A normative page that
// says "the verb is not registered" is doing the corpus a service; requiring a
// note in front of it would punish the fix. What must not stand unannotated is
// a line a reader could PASTE — `$ gopi tool doccheck`, `python3
// tools/docclass/classify.py .`.
// retiredRefCensusGenerators is the FAMILY as a LIST, and the list is the ONE
// definition (Rule 1/13/14): the alternation below is derived from it, so a
// fourth address is one row here and BOTH patterns pick it up. Keeping the
// members in a slice rather than hand-expanding them into two regexes is what
// makes the pair impossible to drift apart — the failure this change repairs
// was exactly that shape, a vocabulary that named one address while the family
// had three.
//
// WHY THE WHOLE FAMILY AND NOT JUST THE ONE ADDRESS A PAGE TEACHES. The earlier
// version of this constant said a single member was enough because "the other
// two generators were never taught by a page that survives". That was wrong on
// the tree it shipped on: `citation-census.go` and `citation-selector-census.go`
// both stand at COMMAND POSITION in tracked pages (`devdoc/citation-gate.md:157`,
// `devdoc/cited-test-selector-gate.md:167`). Those pages are classed
// `historical`, so they are counted and not judged — which is precisely why the
// vocabulary must still contain their addresses: the day one of them is
// re-classed, or a NEW page teaches one, the arm that can see it must already
// exist. A member whose only current hits are historical is coverage that costs
// one row, not an arm that "nothing can match".
var retiredRefCensusGenerators = []string{
	retiredRefTokenCensusGenerator,
	retiredRefTokenCitationCensus,
	retiredRefTokenSelectorCensus,
}

// retiredRefCensusGeneratorAlternation is the family as a regex group, derived
// from the list above. Each member is QuoteMeta'd INDIVIDUALLY (a `.` in an
// address must not become a wildcard); longest-first ordering is NOT needed
// because no member is a prefix of another, and an arm pins that property so a
// future member cannot silently shadow a shorter one.
var retiredRefCensusGeneratorAlternation = func() string {
	quoted := make([]string, 0, len(retiredRefCensusGenerators))
	for _, generator := range retiredRefCensusGenerators {
		quoted = append(quoted, regexp.QuoteMeta(generator))
	}
	return `(?:` + strings.Join(quoted, `|`) + `)`
}()

var retiredRefInvocationPattern = regexp.MustCompile(
	`^[\s>*-]*\$?[\s>*-]*(?:\./bin/)?(?:python3?[ \t]+)?(?:` +
		regexp.QuoteMeta(retiredRefTokenClassifier) + `|gopi[ \t]+(?:tool[ \t]+)?` +
		regexp.QuoteMeta(retiredRefTokenVerb) + `|(?:go[ \t]+run[ \t]+)?` +
		retiredRefCensusGeneratorAlternation + `)`)

// retiredRefMemorySyncPhrase is the ONE retired TWO-WORD sub-verb this gate
// knows, and it is a literal rather than a derived value on purpose: its home
// constant (`cmd/agent/cli_memory_sync.go`'s verb list) was deleted WITH the
// verb, and `internal/cliverbs` removed its `MemorySyncVerb` when its last
// reader left — so there is no shared definition left to derive from, exactly
// the situation the retired top-level `merge` word is in.
//
// MEASURED WHY IT IS NEEDED (2026-11-06, task 01a1ad90-40d0). `5677cc6e`
// deleted the cross-node sync CLI family, and the pages went on teaching the
// invocation:
//
//	DELETED, and this is what it does:  $ ./bin/gopi memory sync --peer peerbox --push  → exit 1
//	gopi memory: unknown subcommand "sync" — want search|store|…|backup|…
//
// — while `gopi memory backup` (a ONE-WAY copy: snapshot + one ssh ship leg, no
// import) was the only surviving cross-host door. `devdoc/memory-sync-periodic-leg.md`
// and `devdoc/memory-sync-timeout-and-oneway.md` both stood at COMMAND POSITION
// with it, classed `normative`, and this gate printed a green over both: the
// vocabulary knew `tools/docclass`, `doccheck`, three census generators, every
// `task` family verb and the top-level `merge`, and none of them is this word.
const retiredRefMemorySyncPhrase = "gopi memory sync"

// retiredRefMemorySyncEveryFlag is the ONE part of that invocation still
// deleted: the cadence flag, whose reader `ResolveMemorySyncEvery`, its 300s
// default and the loop consuming them went in 2026-11-06 (batch 5). The verb
// itself came back the same day as a manual door, which is why the recognizer
// below requires this flag rather than the verb alone.
const retiredRefMemorySyncEveryFlag = "--every"

// retiredRefMemorySyncPattern recognizes the retired two-word sub-verb at
// COMMAND POSITION — the same criterion the other halves use (Rule 36: what a
// reader can PASTE). The leading-marker set is the pipeline half's (prompt,
// indentation, quote markers), because this spelling appears in fenced blocks
// the same way.
//
// The TERMINATING boundary admits whitespace, end of line, and the closing
// delimiters a pasted command carries — and NOT `:` — for the reason the
// top-level half documents at length: a retired verb's own OUTPUT prefix is
// `gopi memory sync: …`, so admitting a colon would report every page that
// quotes a past run's stdout.
// NARROWED 2026-11-06: the VERB is back. `gopi memory sync` was restored as a
// MANUAL door (one invocation = one session, exit), so the bare spelling at
// command position is a command that RUNS — judging it here would report a
// false red on correct prose, which is how a gate gets suppressed. What is
// STILL retired is the PERIODIC face: `--every <dur>` dispatched a loop that
// re-ran the session on a cadence (deleted with its reader and its 300s
// default), so the recognizer requires BOTH the verb and that flag on the line
// — at any position, because a reader pastes the whole invocation and `--every`
// is usually written after `--peer`.
var retiredRefMemorySyncPattern = regexp.MustCompile(
	`^[\s>*-]*\$?[\s>*-]*(?:\./bin/)?` + regexp.QuoteMeta(retiredRefMemorySyncPhrase) +
		`[^\n]*` + regexp.QuoteMeta(retiredRefMemorySyncEveryFlag) +
		`(?:[ \t]|$|[,;)\]}.` + "`" + `=])`)

// retiredRefMentionPattern is the wider predicate the MAN pages are held to:
// a man page is the official promise surface, so it names no retired capability
// AT ALL — not at command position, not in a list of internal commands. It
// covers both halves of the vocabulary (the document pipeline's two tokens and
// every retired family verb), because a page that lists `for-branch` beside the
// commands that survived is the same lie in smaller type. A page that genuinely
// needs to discuss a retirement carries the note and an exemption row with its
// reason.
var retiredRefMentionPattern = regexp.MustCompile(
	regexp.QuoteMeta(retiredRefTokenClassifier) + `|` +
		regexp.QuoteMeta(retiredRefTokenVerb) + `|` +
		retiredRefCensusGeneratorAlternation + `|` +
		`(?:^|[^a-z-])gopi[ \t]+task[ \t]+(?:` + strings.Join(retiredRefCLIVerbs(), `|`) +
		`)(?:[^a-z-]|$)|` +
		// THE TOP-LEVEL half, which this predicate did not cover until slice 1 of
		// the merge-removal sweep: a man page naming the REMOVED top-level verb is
		// the promise surface promising a command the tree does not have, and it
		// is a stronger defect than the family half — there is no refusal to read
		// on that page, only the advertisement. `[^a-z-]` on both sides keeps
		// `gopi merger` (the retired DAEMON's own word, a different surface with
		// its own page-level history) out of it.
		`(?:^|[^a-z-])gopi[ \t]+(?:` + strings.Join(retiredRefTopLevelVerbs, `|`) +
		`)(?:[^a-z-]|$)|` +
		// the two-word sub-verb's PERIODIC face: the verb came back 2026-11-06,
		// so the man page may name it — what a promise surface must not
		// advertise is the `--every` cadence flag, which no binary parses.
		regexp.QuoteMeta(retiredRefMemorySyncPhrase) + `[^\n]*` +
		regexp.QuoteMeta(retiredRefMemorySyncEveryFlag) + `(?:[^a-z-]|$)`)

// retiredRefHit is one command-position invocation, of either kind. The KIND is
// carried because the two halves must be counted and reported apart: the whole
// point of the CLI half is that a green on the pipeline half proves nothing
// about it (a recognizer that only ever matches `tools/docclass` keeps the
// combined count healthy while the CLI surface is unchecked).
type retiredRefHit struct {
	Line int    // 1-based
	Kind string // retiredRefKindPipeline, retiredRefKindCLI or retiredRefKindTopLevel
	Verb string // the token/verb named, for the message
}

const (
	retiredRefKindPipeline = "document-pipeline"
	retiredRefKindCLI      = "retired CLI verb"
	retiredRefKindTopLevel = "retired top-level verb"
	// retiredRefKindMemorySync is the TWO-WORD sub-verb half: the retired
	// `gopi memory sync`, whose verb word sits one level below a live group
	// (`gopi memory` still dispatches search|store|backup|…), so neither the
	// top-level pattern (which reads the word right after `gopi`) nor the family
	// pattern (which reads `task <verb>`) can see it. It gets its own kind so a
	// green on the other three proves nothing about it — the same reason the CLI
	// half was split out.
	retiredRefKindMemorySync = "retired memory sub-verb"
)

// retiredRefFile is one document as the scan sees it. The body is a string so a
// proof can drive the real predicate with an in-memory fixture; the real-tree
// run fills it from disk.
type retiredRefFile struct {
	Path string // repository-relative, slash-separated
	Body string
}

// retiredRefReport is what one scan measured. Walked, Judged and Clean are
// separate numbers on purpose: a corpus that only holds historical records walks
// and judges nothing, and that is precisely the shape a silent green hides.
type retiredRefReport struct {
	Walked      int            // files the walk offered
	Judged      int            // files the rule applies to (class-scoped or promise surface)
	Historical  int            // corpus files skipped as one-run records
	Briefings   int            // WORK ORDERS judged (no class banner: judged unconditionally)
	WithCommand int            // judged files carrying at least one command-position invocation
	WithCLI     int            // judged files carrying at least one RETIRED-CLI-VERB invocation
	WithPipe    int            // judged files carrying at least one document-pipeline invocation
	WithTop     int            // judged files carrying at least one RETIRED TOP-LEVEL VERB invocation
	PipelineCmd int            // command-position hits naming the document pipeline
	CLICmd      int            // command-position hits naming a retired family verb
	TopCmd      int            // command-position hits naming a retired top-level verb
	WithMemSync int            // judged files carrying at least one RETIRED MEMORY SUB-VERB invocation
	MemSyncCmd  int            // command-position hits naming the retired memory sub-verb
	Noted       int            // judged files whose note precedes their first invocation
	Exempt      int            // judged files carrying a reasoned exemption
	Exemptions  map[string]int // how many rows the table holds, by verdict
	Failures    []string       // "<path>[:<line>]: <reason>", one per offending file
}

// Summary renders the scan's own accounting, so a reader of a green run knows
// what the green is worth. The three halves are reported SEPARATELY on purpose:
// a combined number is the shape in which a dead recognizer hides (a healthy
// half keeps the total up while a broken one measures nothing).
func (r retiredRefReport) Summary() string {
	return fmt.Sprintf(
		"%s: walked %d file(s), judged %d (briefings %d), clean %d, historical-skipped %d, "+
			"command-position invocation %d file(s) (CLI half %d, top-level half %d, pipeline half %d, memory-sync half %d), "+
			"hits %d (CLI %d, top-level %d, pipeline %d, memory-sync %d), annotated %d, exempted %d",
		retiredRefProvenance, r.Walked, r.Judged, r.Briefings, r.Judged-len(r.Failures), r.Historical,
		r.WithCommand, r.WithCLI, r.WithTop, r.WithPipe, r.WithMemSync,
		r.CLICmd+r.TopCmd+r.PipelineCmd+r.MemSyncCmd, r.CLICmd, r.TopCmd, r.PipelineCmd, r.MemSyncCmd,
		r.Noted, r.Exempt)
}

// retiredRefNoteLine returns the 1-based line of the first retired-mechanism
// note, or 0 when the file carries none. It is a LINE NUMBER and not a bool
// because the rule is about ORDER: a note after the invocation explains nothing
// to the reader who already pasted the command.
func retiredRefNoteLine(body string) int {
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, retiredRefNoteRecognizer) {
			return i + 1
		}
	}
	return 0
}

// retiredRefInvocationLines returns the command-position invocations the file
// carries, ordered by line: the document pipeline first at a given line, then
// the retired family verbs, so the message names the surface a reader would
// paste.
func retiredRefInvocationLines(body string) []retiredRefHit {
	var out []retiredRefHit
	for i, line := range strings.Split(body, "\n") {
		if retiredRefInvocationPattern.MatchString(line) {
			out = append(out, retiredRefHit{
				Line: i + 1,
				Kind: retiredRefKindPipeline,
				Verb: retiredRefMentionPattern.FindString(line),
			})
			continue
		}
		matched := false
		for _, verb := range retiredRefTopLevelVerbsIn(line) {
			out = append(out, retiredRefHit{Line: i + 1, Kind: retiredRefKindTopLevel, Verb: "gopi " + verb})
			matched = true
		}
		if matched {
			continue
		}
		for _, verb := range retiredRefCLIVerbsIn(line) {
			out = append(out, retiredRefHit{Line: i + 1, Kind: retiredRefKindCLI, Verb: "gopi task " + verb})
		}
		if retiredRefMemorySyncPattern.MatchString(line) {
			out = append(out, retiredRefHit{Line: i + 1, Kind: retiredRefKindMemorySync, Verb: retiredRefMemorySyncPhrase})
		}
	}
	return out
}

// retiredRefFirstLine is the class-banner line of a corpus document (line 1 by
// Rule 36). It is read, never written.
func retiredRefFirstLine(body string) string {
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		return body[:i]
	}
	return body
}

// scanRetiredRefs applies the rule to a list of documents and reports rather
// than fails: the caller (a test) owns the verdict, which is what lets the
// proofs drive the same scan over a fixture corpus.
//
// exemptions maps a repository-relative path to the reason it is allowed to
// stand. An empty reason is a FAILURE, not a pass: an unreasoned permit is
// indistinguishable from a leak, and the table is meant to ratchet down.
func scanRetiredRefs(files []retiredRefFile, exemptions map[string]string) retiredRefReport {
	rep := retiredRefReport{Exemptions: make(map[string]int)}
	for path, reason := range exemptions {
		if strings.TrimSpace(reason) == "" {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s: exemption carries no reason", path))
			continue
		}
		rep.Exemptions[path]++
	}
	for _, f := range files {
		rep.Walked++
		inCorpus := strings.HasPrefix(f.Path, retiredRefCorpusRoot+"/")
		onManPage := strings.HasPrefix(f.Path, retiredRefManRoot+"/")
		if inCorpus && !devdoc.IsProse(f.Path) {
			// A capture, a fixture or a TSV inside the corpus is out of scope by
			// a written rule (devdoc.IsProse): it cannot make a standing claim.
			continue
		}
		if inCorpus {
			class, err := devdoc.HeaderClass(retiredRefFirstLine(f.Body))
			if err != nil {
				rep.Failures = append(rep.Failures, fmt.Sprintf("%s: %v", f.Path, err))
				continue
			}
			if class == devdoc.ClassHistorical {
				// One run's record: it must be free to quote what was typed that
				// day. Counted, never judged.
				rep.Historical++
				continue
			}
		}
		rep.Judged++
		if strings.HasPrefix(f.Path, retiredRefBriefingRoot+"/") {
			rep.Briefings++
		}
		if _, ok := exemptions[f.Path]; ok {
			rep.Exempt++
			continue
		}
		if onManPage {
			// A MAN PAGE IS THE PROMISE SURFACE, and the two halves of that
			// promise fail differently (this branch used to collapse them, and
			// its own message already promised the distinction it did not
			// implement — Rule 15):
			//
			//   - a COMMAND-POSITION invocation is refused with no escape at all.
			//     A SYNOPSIS row is an instruction to run the thing; a note above
			//     it would leave a page that both prescribes a removed command
			//     and apologises for it, and the reader who pastes it is the one
			//     who pays.
			//   - a PROSE MENTION is allowed when the page STATES the retirement
			//     before it — and only then. That is the shape a page needs to
			//     explain what was removed and where the engine lives now, which
			//     is information, not an advertisement.
			if invs := retiredRefInvocationLines(f.Body); len(invs) != 0 {
				rep.Failures = append(rep.Failures, fmt.Sprintf(
					"%s:%d: a man page prescribes %s %q at command position — the promise surface must not present a removed capability as runnable (document the surviving spelling instead; a note cannot license a SYNOPSIS row)",
					f.Path, invs[0].Line, invs[0].Kind, invs[0].Verb))
				continue
			}
			if line := retiredRefMentionLine(f.Body); line != 0 {
				noteLine := retiredRefNoteLine(f.Body)
				if noteLine == 0 {
					rep.Failures = append(rep.Failures, fmt.Sprintf(
						"%s:%d: a man page names a retired surface %q with no retirement stated — state it above the mention, or delete the line",
						f.Path, line, retiredRefMentionPattern.FindString(f.Body)))
					continue
				}
				if noteLine > line {
					rep.Failures = append(rep.Failures, fmt.Sprintf(
						"%s:%d: the retirement note is at line %d, AFTER the mention a reader meets first — a note below it is no note to that reader",
						f.Path, line, noteLine))
					continue
				}
				rep.Noted++
			}
			continue
		}
		invs := retiredRefInvocationLines(f.Body)
		if len(invs) == 0 {
			continue
		}
		rep.WithCommand++
		firstCLI, firstTop := -1, -1
		sawPipe, sawMemSync := false, false
		for _, inv := range invs {
			switch inv.Kind {
			case retiredRefKindCLI:
				rep.CLICmd++
				if firstCLI < 0 {
					firstCLI = inv.Line
				}
			case retiredRefKindTopLevel:
				rep.TopCmd++
				if firstTop < 0 {
					firstTop = inv.Line
				}
			case retiredRefKindMemorySync:
				// Its OWN arm, never the pipeline default: the whole reason the
				// kinds are counted apart is that a green on one proves nothing
				// about another, and a recognizer folding a new surface into an
				// existing bucket is exactly how a checked surface becomes an
				// unchecked one wearing a healthy count.
				rep.MemSyncCmd++
				sawMemSync = true
			default:
				rep.PipelineCmd++
				sawPipe = true
			}
		}
		if firstCLI >= 0 {
			rep.WithCLI++
		}
		if firstTop >= 0 {
			rep.WithTop++
		}
		if sawPipe {
			rep.WithPipe++
		}
		if sawMemSync {
			rep.WithMemSync++
		}
		note := retiredRefNoteLine(f.Body)
		switch {
		case note == 0:
			rep.Failures = append(rep.Failures, fmt.Sprintf(
				"%s:%d: invokes %s %q with no retired-mechanism note anywhere in the file — put the note ABOVE this line (marker: %q)",
				f.Path, invs[0].Line, invs[0].Kind, invs[0].Verb, retiredRefNoteRecognizer))
		case note > invs[0].Line:
			rep.Failures = append(rep.Failures, fmt.Sprintf(
				"%s:%d: invokes %s %q, but the retired-mechanism note is at :%d — AFTER it, where the reader who already pasted the command never meets it",
				f.Path, invs[0].Line, invs[0].Kind, invs[0].Verb, note))
		default:
			rep.Noted++
		}
	}
	return rep
}

// retiredRefMentionLine returns the 1-based line of the first mention of a
// retired surface, or 0 when the file names none.
func retiredRefMentionLine(body string) int {
	for i, line := range strings.Split(body, "\n") {
		if retiredRefMentionPattern.MatchString(line) {
			return i + 1
		}
	}
	return 0
}
