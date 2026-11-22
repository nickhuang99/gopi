package taskreport

// filteredclaim.go — THE FILTERED-PACKAGE CLAIM: a package-level test reading
// reported from a command whose own filter means the package was never the
// thing that ran.
//
// WHY (the measured case, run 01a12ee4, 2026-10-12). That run selected
// `./internal/standards` in its own PKGS list — its words: "the touched
// packages: ./internal/scratch ./internal/tools ./internal/config ./cmd/agent
// ./internal/testutil ./internal/standards" — then killed the run after twelve
// minutes with no output and REPLACED it with a self-authored
// `go test -run 'Holder|LiveHolders|ProfileRoot|ScratchGC|Sandbox' ./internal/scratch/ ./internal/tools/`.
// That filter matched not one gate test (TestStandardGates /
// TestStandardGateDataReport are not in the pattern), and on the same rev the
// package's whole run was RED:
//
//	go test ./internal/standards/                     → FAIL exit 1
//	go test ./internal/standards/ -run TestConfigDoc  → ok … exit 0
//
// and so was the honest counterexample this predicate must leave alone:
//
//	go test ./internal/standards/                     → ok … exit 0   (narrow nothing)
//
// So the report carried `ok github.com/nickhuang99/gopi/internal/standards` — a
// PACKAGE-LEVEL reading from a command that narrowed the package away — and the
// reviewer had no instrument for it. `scripts/test-budget.sh` models the
// neighbouring gap and closes it: exit 5 (NO TEST EXECUTED) refuses a filtered
// run that executed ZERO tests. It cannot see this one, because the run was not
// empty — it ran OTHER tests, in OTHER packages — it was simply not about the
// package whose name the reading carries.
//
// THE CONTRACT — one predicate, THREE conditions in ONE sentence:
//
//  1. the sentence NAMES a test instrument (the `go test` shape, or the two
//     declared test entry points `make test-budget` / `scripts/test-budget.sh`);
//  2. the SAME sentence NAMES a narrowing FILTER FLAG — `-run`, `-skip`,
//     `-bench`, the selecting spelling Go's own tool excludes whole tests with
//     (never `-count`/`-timeout`/`-v`/`-json`, which narrow nothing); and
//  3. the SAME sentence ASSERTS a PACKAGE-LEVEL result (`ok`, PASS, green,
//     clean, passed).
//
// A sentence satisfying all three states a reading the command it names cannot
// have produced: a filter REMOVES tests from the selection, so the
// package-level line it printed answers a different question than the one the
// report reads it as. That is not a lie about the code — it is a reading of the
// wrong population, which is why the remedy is a RE-RUN and not a retraction
// (the remedy sentence rides every record).
//
// THE BOUNDARY IS THE SPECIFICATION'S, and the counterexample is measured: a
// package-level `ok` from a command that narrows NOTHING (`go test ./pkg/`, or
// `make test-budget PKGS='./pkg'`) is the ordinary, honest shape and is
// untouched by condition 2. What condition 1+2+3 does NOT try to decide is
// ADEQUACY — whether a given `-run` pattern was a sensible one, or whether the
// pattern really matched the tests the reading is about, is a reviewer's
// judgment; a mechanical check that pretended to make it would be lying (the
// limit runcheck.go states for its own face, restated here because the
// temptation is the same). The predicate reads SENTENCES, not streams.
//
// THE ACTION IS (A) RECORD — never a rewrite, never a refusal — the discipline
// gateclaim.go, runcheck.go and delegated.go established. The justification is
// MEASURED, not aesthetic, and it is the reason this face is not a gate: see
// devdoc/report-filtered-package-claim.md for the corpus census (how many real
// reports this predicate touches, and the size of its honest overlap). A
// refusal keyed on a reading of prose would refuse runs that did report
// honestly; a RECORD costs a reviewer one line and destroys nothing (Rule 15 —
// no silent failure, no silent rewrite).
//
// The package is a LEAF (Rule 14): stdlib only, constants + pure text
// functions, so the writer (internal/tools), the reader (`task show`) and every
// future face share ONE definition of the contract.

import (
	"strings"
)

const (
	// FilteredClaimDigestPrefix introduces the recorded finding in the ledger's
	// result digest: the sentences ride the append-only task_events detail, so a
	// reviewer reaches them from the row even after the session directory is
	// pruned (the durability the report path itself gets).
	FilteredClaimDigestPrefix = "report-filtered-package-claim: "

	// FilteredClaimWarningLabel is the stderr label of the seal-time finding —
	// the `[task] `-prefixed family the gate-claim, ran-check and
	// result-delegation records belong to, so one line of a close-out names its
	// own family (Rule 1b: named here, never typed twice).
	FilteredClaimWarningLabel = "filtered-package"

	// filteredClaimMaxSentences caps how many offending sentences the record
	// carries: a report with a systemic problem must not push the row's digest
	// past what an operator reads, and the first few name the pattern (the cap
	// discipline gateclaim.go / delegated.go follow).
	filteredClaimMaxSentences = 3

	// filteredClaimSentenceMaxRunes bounds a quoted sentence: a bullet can be a
	// paragraph, and a stamp has to stay a stamp (the shape
	// delegatedClaimSentenceMaxRunes pinned).
	filteredClaimSentenceMaxRunes = 200

	// filteredClaimEllipsis marks a sentence the record truncated.
	filteredClaimEllipsis = "…"

	// filteredClaimSeparator separates recorded sentences. It never occurs in a
	// sentence — the splitter cuts on bullet boundaries, and a claim that
	// carried this token by accident would be one sentence, not two.
	filteredClaimSeparator = " | "

	// FilteredFlagSeparator separates the narrowing flags a record names, and is
	// EXPORTED so the reader (`task show`) renders the same list with the same
	// separator the digest record used — one definition, never a second
	// vocabulary for the same list (Rules 1b/14).
	FilteredFlagSeparator = " | "

	// filterFlagMarker is the dash every narrowing flag opens with, and the
	// FILTER-FLAG ARRAY below is the only place a flag is spelled (Rule 13).
	filterFlagMarker = "-"

	// filterFlagAssignment is how a flag is glued to its value ("-run=Ping"),
	// accepted beside the spaced spelling ("-run 'Ping'").
	filterFlagAssignment = "="

	// instrumentArgHint is the argument shape that marks an instrument as a
	// PACKAGE-LEVEL run: a `./`-relative pattern or the whole-module `./...`.
	// It is what separates `go test ./internal/standards/` (a package reading)
	// from a sentence that merely mentions the tool.
	instrumentArgHint = "./"
)

// testInstruments are the test instruments a report names when it reports a
// reading: the `go test` shape and the repository's two declared test entry
// points (`make test`, `make test-budget`, and the script behind them). A
// sentence naming no instrument has not reported a reading, whatever else it
// says.
var testInstruments = []string{
	"go test",
	"make test",
	"test-budget.sh",
}

// narrowingFilterFlags are the flags that REMOVE whole tests from Go's
// selection — the narrowing this predicate is about, and only those: `-count`,
// `-timeout`, `-v`, `-json` and `-benchtime` narrow no test out, and a flag that
// merely contains one of these spellings (`-run-not-this`) is not a match (the
// predicate matches a whole token, not a substring).
var narrowingFilterFlags = []string{
	"-run",
	"-skip",
	"-bench",
}

// filteredPackageResults are the PACKAGE-LEVEL readings: Go's own package line
// (`ok  github.com/nickhuang99/gopi/internal/standards  43.6s`), the whole-package
// PASS report line, and the colour words a report uses for the same statement.
var filteredPackageResults = []string{
	"ok",
	"pass",
	"green",
	"clean",
}

// filteredPopulationMarkers are the phrases with which a sentence states the
// POPULATION it measured — which tests ran, how many passed — and they SUPPRESS
// the finding, because a sentence that states its own population has already
// answered the question this check asks (it is the remedy shape, not the
// defect). They are needed because Go prints its package line for a FILTERED run
// too: `go test ./pkg/ -run TestX` → `ok github.com/…/pkg 0.3s` is spelled
// exactly like a whole-package reading, so the spelling cannot separate them and
// the run's own report of what executed is what does.
//
// The vocabulary is MEASURED from this repository's report corpus (see
// devdoc/report-filtered-package-claim.md), not invented: these are the
// spellings runs actually use when they report a filtered run honestly.
var filteredPopulationMarkers = []string{
	"tests executed",
	"executed:",
	"ran:",
	"test(s) ran",
	"passed",
	"fail, ",
	"pass, ",
	"0 fail",
	"× ",
	"name(s)",
}

// filterFlagNeighbours are the characters that may follow a flag's spelling
// where the flag carries NO value of its own — the fence or punctuation a
// report wraps it in (`-run`'s value may also be glued with `=`, or follow on
// the next token).
const filterFlagNeighbours = " \t\n`,);:，、"

// FilteredClaim is one sentence that reports a package-level test reading from a
// command whose own filter means the package was never the thing that ran.
type FilteredClaim struct {
	// Sentence is the claim, quoted verbatim (a record must carry the author's
	// own words; nothing here rewrites prose).
	Sentence string
	// Filters are the narrowing flags the sentence names, in order of
	// appearance, deduplicated.
	Filters []string
}

// FilteredClaims is the check's verdict over a report body.
type FilteredClaims struct {
	// Section is the heading whose content was adjudicated — the UNION of the
	// sections a report states results in, the same reading the
	// result-delegation check uses: a run states its gate readings under
	// `## Verification` and its observations under the evidence section, and
	// the motivating sentence sat in the first while the second was present and
	// non-empty. "" when the report carries neither (then the whole body is
	// adjudicated, so deleting a heading cannot dodge the check).
	Section string
	// Claims are the offending sentences, in order of appearance, deduplicated
	// by sentence.
	Claims []FilteredClaim
}

// FilteredClaimsIn scans a report body for package-level test readings taken
// from a narrowed command.
func FilteredClaimsIn(body string) FilteredClaims {
	v := FilteredClaims{}
	text := body
	if heading, union := delegatedResultSections(body); union != "" {
		v.Section, text = heading, union
	}
	// The packages this run ALSO ran whole (an unfiltered command naming them),
	// read from the same text: a filtered package line whose package appears
	// here is a SUPPLEMENT to a reading that exists, not a reading standing in
	// for one — see wholeRunPackages.
	whole := wholeRunPackages(text)
	seen := map[string]bool{}
	for _, s := range filteredClaimSentences(text) {
		if seen[s] {
			continue
		}
		filters := sentenceNarrowingFilters(s)
		if len(filters) == 0 || !sentenceReportsFilteredPackage(s) {
			continue
		}
		if sentenceCoversAWholeRun(s, whole) {
			continue
		}
		seen[s] = true
		v.Claims = append(v.Claims, FilteredClaim{Sentence: filteredSentenceExcerpt(s), Filters: filters})
		if len(v.Claims) >= filteredClaimMaxSentences {
			break
		}
	}
	return v
}

// filteredClaimSentences splits a section body into the units this check reads.
//
// It differs from claimSentences in TWO deliberate ways, each one a measured
// correction:
//
//  1. it does NOT cut on `|` — that character is exactly how Go's `-run`
//     alternation is written (`-run 'TestA|TestB'`), and reading the motivating
//     sentence through the shared splitter tore the command in half: claims = 0
//     on the first run of this file's own proof, because the fragment carrying
//     the filter no longer carried the package result. The contract is "ONE
//     sentence names all three", so the reader must see the whole sentence;
//
//  2. it splits a BULLET at every line that does not CONTINUE the line above —
//     a wrapped command and its result are one claim (the motivating report
//     writes the invocation on one line and its `→ ok` on the next, so a
//     per-physical-line reader would miss the defect entirely), while two
//     unrelated lines of the same bullet are two claims. A continuation is a
//     line opening with the project's own measured-transition arrow, which is
//     what the report uses to attach a reading to the command above it.
//
// Fenced blocks are removed first: what a fence holds is a TRANSCRIPT (a pasted
// `Ran:` block, a shell script, a log dump), not the report's prose about an
// outcome — the same clause, and the same reason, delegated.go's stripDelegatedFences
// carries.
func filteredClaimSentences(body string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, trimClaimBullet(s))
		}
		cur.Reset()
	}
	for _, line := range strings.Split(stripDelegatedFences(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isClaimBulletLine(line) {
			flush()
		} else if cur.Len() > 0 && !isFilteredContinuation(trimmed) {
			// A new line inside the same bullet: it continues only when it
			// attaches itself to the claim above (the `→` arrow).
			flush()
		} else if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(line)
		cur.WriteString("\n")
	}
	flush()
	return out
}

// isFilteredContinuation reports whether a line attaches itself to the claim on
// the line above: the measured-transition arrow a report writes between a
// command and its reading — the same arrow claimEvidences names, read here as a
// line prefix rather than a sentence-internal marker (Rule 1b: one spelling).
func isFilteredContinuation(trimmed string) bool {
	for _, m := range filteredContinuationMarkers {
		if strings.HasPrefix(trimmed, m) {
			return true
		}
	}
	return false
}

// filteredContinuationMarkers are the spellings of that arrow — the two
// claimEvidences carries.
var filteredContinuationMarkers = []string{"→", "->"}

// wholeRunPackages returns the package patterns the text names in a
// WHOLE-PACKAGE run — a test instrument carrying the pattern with NO narrowing
// flag in that command. It is the discrimination Go's own output cannot make for
// us: `ok github.com/…/pkg 0.3s` is spelled identically whether the package ran
// whole or was narrowed by `-run`, so the run's own statement that it ALSO ran
// the package unfiltered is what turns a filtered package line into a
// SUPPLEMENT (the thing the whole run measured, told apart afterwards) instead
// of a reading standing in for it (the defect).
//
// Measured on this repository's report corpus: this clause is what separates
// the motivating sentence — whose report ran `./internal/standards/` NOWHERE
// whole, because the whole run is exactly what the `-run` replaced — from the
// common honest shape, where a filtered branch-scoped line sits beside the
// whole-package run it supplements.
func wholeRunPackages(text string) map[string]bool {
	out := map[string]bool{}
	for _, s := range filteredClaimSentences(text) {
		if len(sentenceNarrowingFilters(s)) != 0 {
			continue
		}
		out[packagePatternOf(s)] = true
	}
	return out
}

// packagePatternOf returns the package pattern a sentence names ("" when it
// names none): the LAST `./`-relative or whole-module spelling in the sentence,
// from its opening dot to the end of that spelling, with the punctuation a writer
// wraps it in trimmed off. The spelling is looked for INSIDE a token as well as
// at its start, because a report writes the pattern glued to the flag that takes
// it (`PKGS='./internal/tools'`, `--pkgs=./cmd/agent/`) as often as it writes it
// alone.
func packagePatternOf(sentence string) string {
	fields := strings.Fields(sentence)
	for i := len(fields) - 1; i >= 0; i-- {
		tok := fields[i]
		at := strings.LastIndex(tok, instrumentArgHint)
		if at < 0 {
			continue
		}
		pattern := strings.Trim(tok[at:], packagePatternTrim)
		if pattern != "" {
			return pattern
		}
	}
	return ""
}

// sentenceCoversAWholeRun reports whether the package a filtered sentence is
// about is one the report ALSO ran whole: then the filtered line tells the
// reader what the filter selected, which is what a filter is for, and the
// whole-package reading exists beside it. Only when the filtered line stands
// ALONE — the package's whole run was replaced by the filter, which is the
// motivating defect — does it read as the package's own result.
func sentenceCoversAWholeRun(sentence string, whole map[string]bool) bool {
	pattern := packagePatternOf(sentence)
	if pattern == "" {
		// The sentence names no package pattern of its own, so there is no
		// package whose reading it could stand in for: a filtered reading
		// without a package is a test-level measurement, not this claim.
		return true
	}
	if strings.HasSuffix(pattern, packagePatternWholeModule) {
		return true // `./...` is every package at once — a filtered line beside it supplements a reading that exists
	}
	return whole[pattern]
}

// packagePatternTrim is the punctuation a package pattern is wrapped in inside a
// sentence — the fence, the quotes, the sentence's own separators.
const packagePatternTrim = "`'\"(),;:，。、"

// packagePatternWholeModule is the `.`-opening whole-module pattern (`./...`).
const packagePatternWholeModule = "..."

// sentenceReportsFilteredPackage is the ONE predicate: does this sentence
// report a package-level test reading under a filter that narrowed the
// selection? The three conditions the contract states, read from the SAME
// sentence — a report that names the instrument in one bullet and the result in
// another has not made this claim, and cutting sentences apart is what
// claimSentences deliberately refuses to do inside a wrapped bullet.
func sentenceReportsFilteredPackage(sentence string) bool {
	low := strings.ToLower(sentence)
	if !namesTestInstrument(low) {
		return false
	}
	if hasAny(low, filteredPopulationMarkers) {
		return false
	}
	return hasAny(low, filteredPackageResults)
}

// namesTestInstrument reports whether the sentence names a test instrument AND
// a package-level pattern — the argument shape that makes the instrument's
// reading a PACKAGE reading rather than a mention of the tool.
func namesTestInstrument(low string) bool {
	for _, form := range testInstruments {
		i := strings.Index(low, form)
		if i < 0 {
			continue
		}
		if strings.Contains(low[i+len(form):], instrumentArgHint) {
			return true
		}
	}
	return false
}

// sentenceNarrowingFilters returns the narrowing flags the sentence names, in
// order of appearance, deduplicated (empty = the sentence narrows nothing, and
// the honest package reading is untouched).
func sentenceNarrowingFilters(sentence string) []string {
	low := strings.ToLower(sentence)
	var out []string
	for _, f := range narrowingFilterFlags {
		if namesFilterFlag(low, f) {
			out = append(out, f)
		}
	}
	return out
}

// namesFilterFlag reports whether the lowered sentence names flag f as a FLAG:
// its own token (`-run 'X|Y'`, `-run=X|Y`, or the fenced `` `-run` `` a report
// quotes), never a substring of a longer word or a path (`-run-not-this`, a
// directory named `-benchmarks`, `run:` inside prose).
func namesFilterFlag(low, flag string) bool {
	for i := 0; ; {
		j := strings.Index(low[i:], flag)
		if j < 0 {
			return false
		}
		at := i + j
		i = at + len(flag)
		if !flagStartsAToken(low, at) {
			continue
		}
		if flagEndsTheToken(low, i) || flagIsGluedToItsValue(low, i) {
			return true
		}
	}
}

// flagStartsAToken reports whether the spelling at index at opens a token
// (preceded by the start of the sentence, whitespace, or the fence punctuation
// a report quotes with) — the same anchoring runcheck.go's token lines use,
// applied to a flag instead of a line prefix. The comparisons are on BYTES: the
// index arrives from strings.Index, and a multi-byte character before the dash
// can never be one of these single-byte boundaries.
func flagStartsAToken(low string, at int) bool {
	if at == 0 {
		return true
	}
	switch low[at-1] {
	case ' ', '\t', '\n', '`', '(', '[', '"', '\'':
		return true
	}
	return false
}

// flagEndsTheToken reports whether the flag's spelling ends where its token
// ends — its value following on another token.
func flagEndsTheToken(low string, end int) bool {
	if end >= len(low) {
		return true
	}
	return strings.ContainsRune(filterFlagNeighbours, rune(low[end]))
}

// flagIsGluedToItsValue reports whether the flag is glued to its value with
// `=` ("-run=Ping").
func flagIsGluedToItsValue(low string, end int) bool {
	return end < len(low) && string(low[end]) == filterFlagAssignment
}

// filteredSentenceExcerpt bounds a quoted sentence for a record: a bullet can be
// a paragraph, and a stamp has to stay a stamp.
func filteredSentenceExcerpt(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= filteredClaimSentenceMaxRunes {
		return string(r)
	}
	return string(r[:filteredClaimSentenceMaxRunes]) + filteredClaimEllipsis
}

// FilteredClaimRemedy is the sentence every record of this finding carries: what
// the reading cannot mean, and what to run instead. A record that says only
// "wrong" leaves the author guessing the fix — and the fix is one command.
const FilteredClaimRemedy = "a `-run`/`-skip`/`-bench` filter REMOVES tests from the selection, so the " +
	"package-level line it printed is not a reading of the package: re-run the package itself " +
	"(`make test-budget PKGS='./<pkg>'` — one package per invocation, its own budget) and report THAT " +
	"line; or keep the filtered command and report ITS reading (which tests ran, named)"

// FilteredRecord renders the digest fragment for a verdict ("" when the report
// takes no narrowed package reading) — the recorded index a reviewer reads off
// the row's own trail. The remedy rides the record, so the trail states the fix
// as well as the gap.
func FilteredRecord(v FilteredClaims) string {
	if len(v.Claims) == 0 {
		return ""
	}
	parts := make([]string, 0, len(v.Claims))
	for _, c := range v.Claims {
		parts = append(parts, c.Sentence+" ["+strings.Join(c.Filters, FilteredFlagSeparator)+"]")
	}
	return FilteredClaimDigestPrefix + strings.Join(parts, filteredClaimSeparator) + " — " + FilteredClaimRemedy
}

// FilteredWarning renders the operator line(s) for a verdict: the finding's
// label, the section it was found under, each offending sentence with the flags
// that narrow it, and the remedy. Callers print it on stderr — a finding that is
// recorded but never shown is half a record.
func FilteredWarning(taskID string, v FilteredClaims) string {
	if len(v.Claims) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(FilteredClaimWarningLabel)
	if taskID != "" {
		b.WriteString(" ")
		b.WriteString(taskID)
	}
	b.WriteString(" — under ")
	if v.Section == "" {
		b.WriteString("the report body")
	} else {
		b.WriteString(v.Section)
	}
	b.WriteString(": a package-level reading is reported from a command narrowed by a filter — the package was not the thing that ran\n")
	for _, c := range v.Claims {
		b.WriteString("  ")
		b.WriteString(c.Sentence)
		b.WriteString("\n")
		b.WriteString("    filter")
		if len(c.Filters) > 1 {
			b.WriteString("s")
		}
		b.WriteString(": ")
		b.WriteString(strings.Join(c.Filters, FilteredFlagSeparator))
		b.WriteString("\n")
	}
	b.WriteString("  ")
	b.WriteString(FilteredClaimRemedy)
	b.WriteString("\n")
	return b.String()
}
