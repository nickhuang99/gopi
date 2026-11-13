package tools

// task_briefing_run_alternatives.go — the SIXTH measurement on the dispatch
// seam: a briefing's acceptance line carries `go test <pkg> -run '<A|B|C>'` and
// one of those alternatives reaches NO test function at all.
//
// THE MEASURED CASE (bug row 01a18f76's own acceptance line, 2026-10-31). An
// epic's acceptance read, verbatim:
//
//	go test ./internal/tools/ -run 'TestWorktreeRemovalGroundGate|TestTaskReclaimSelfDelivery|TestEveryWorktreeRemovalArmQuestionsTheGround|TestSelfDelivery'
//
// Two of those four names exist nowhere in the repo's test corpus. `go test`
// prints `testing: warning: no tests to run` for a pattern that matches nothing
// and exits 0 — so those two families were never going to run, while the
// acceptance line would read green, and the report would cite it. That is
// PROJECT.md Rule 36's own sentence ("a named check that does not exist is worse
// than no check, because its green cannot be told apart from a clean tree") one
// level up: here the check exists but the SELECTOR misses it.
//
// WHY THIS BELONGS ON THIS SEAM. Every executor face funnels through the spawn
// seam in cmd/agent/cli_tasks.go, and this is where the briefing is already
// re-measured before the child sees it (premise, base, gate assertion, quoted
// figure, pinned path set). A dead alternative is a fact about THIS GROUND's own
// test corpus — cheap, exact, and read without running anything.
//
// WHAT IT DOES, precisely: for each `-run` pattern written on a line that also
// names the `go test` instrument, resolve the package pattern the same command
// names to a directory in this ground, read that directory's own `*_test.go`
// files, collect the top-level test function names, split the pattern into its
// `|`-separated alternatives, and count how many real test functions each
// alternative reaches. Every ZERO is named in the stamped clause. Every
// non-zero is named in the record (the census), so a reader of the round log
// sees a count per alternative rather than only a verdict — a zero stands out
// against its neighbours instead of being the only thing printed.
//
// WHAT IT MAY NOT CONCLUDE. It never says the briefing's acceptance criterion
// was met: it says one alternative cannot be met by ANY test in this tree, and
// that the child must replace the name or drop the alternative. A pattern whose
// alternatives ALL reach a test stamps nothing — that is the positive control,
// and it is the ordinary case (this clause is silent on nearly every briefing).
//
// THE VACUUM FLOOR (Rule 15, and the reason this file's own check cannot be the
// defect it repairs). A scan that judged nothing must never pass as green: a
// package pattern this ground cannot resolve, a directory carrying no `_test.go`
// file, and a directory whose files yielded no `func Test…` line are each
// recorded as UNMEASURED with the reason — never counted as "zero reach", and
// never silently skipped.
//
// NO NEW MECHANISM (Rule 35): no tool, no script, no subprocess, no resident
// anything. One clause on the seam that already exists, reading files in this
// ground with the standard library. ADDITIVE by the same contract as its five
// siblings: best-effort, never errors, never blocks a run, silent on the
// ordinary briefing that names no `-run` pattern, and idempotent.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TaskBriefingRunAlternativesLabel heads the dead-alternative clause, spelled
// once and exported for the same reason as its five siblings (Rules 1/13/14):
// the producer, the spawn seam's record and the regression tests read ONE
// string.
const TaskBriefingRunAlternativesLabel = "[Briefing acceptance — a `go test -run` ALTERNATIVE in this briefing reaches NO test function in the package it names]"

// TaskBriefingRunAlternativesVerifyOnly is the instruction the clause carries.
const TaskBriefingRunAlternativesVerifyOnly = "REPLACE THE DEAD NAME WITH THE TEST THAT EXISTS, OR DROP THE ALTERNATIVE"

const (
	// runAltGoTestInstrument is the instrument a line must name before any
	// `-run` on it is read as an acceptance command (the same reading
	// internal/taskreport's filtered-claim predicate takes: a `-run` outside a
	// `go test` sentence is somebody's prose, not a selection).
	runAltGoTestInstrument = "go test"
	// runAltFlagToken is the flag this clause reads, spelled once.
	runAltFlagToken = "-run"
	// runAltFlagAssignment is how the flag is glued to its value
	// ("-run=Ping"), accepted beside the spaced spelling ("-run 'Ping'").
	runAltFlagAssignment = "="
	// runAltAlternativeSeparator splits a pattern into the alternatives Go
	// itself treats as alternatives.
	runAltAlternativeSeparator = "|"
	// runAltSubtestSeparator is Go's subtest separator inside one alternative.
	// The part BEFORE it is a top-level test function name this seam can count;
	// the part AFTER it names a subtest, which is not visible in the source
	// without running the test, so it is stated rather than guessed at.
	runAltSubtestSeparator = "/"
	// runAltRecursiveSuffix is Go's whole-subtree package spelling, which
	// expands to many directories and is deliberately not walked here.
	runAltRecursiveSuffix = "..."
	// runAltPackagePrefix marks the token that names a package pattern.
	runAltPackagePrefix = "./"
	// runAltTestFileSuffix is the only file kind this clause reads (Rule: a
	// non-test file's `func Test…` is a helper or a literal, never a selection).
	runAltTestFileSuffix = "_test.go"
	// runAltTestFuncPrefix is the ONLY spelling a top-level test function opens
	// with; a line that merely CONTAINS it (a comment, a string literal, a
	// helper invocation) is not a test function.
	runAltTestFuncPrefix = "func Test"
	// runAltMaxAlternatives caps how many alternatives this clause will judge
	// from ONE pattern: the stamp goes into a child's prompt and must stay a
	// stamp.
	runAltMaxAlternatives = 12
	// runAltMaxPatterns caps how many patterns are judged per briefing.
	runAltMaxPatterns = 4
	// runAltMaxFiles caps how many test files one package scan will read.
	// MEASURED, not guessed: `./internal/tools` carries 588 `*_test.go` files on
	// this rev, and the first draft's 400 silently truncated the census — the
	// truncation produced FALSE ZERO verdicts for names that live in the files
	// past the cap, which is the very defect this clause repairs one level up.
	// So the cap is set above the largest package and, when it IS hit, the arm
	// is STATED as unmeasured rather than returning a partial list.
	runAltMaxFiles = 4000
	// runAltMaxFileBytes caps how large a single test file this seam will read.
	runAltMaxFileBytes = 8 << 20
	// runAltMinCensusNames is the VACUUM FLOOR: a package scan that produced
	// fewer top-level test names than this cannot support a "zero reach"
	// conclusion, because the scan itself is what failed.
	runAltMinCensusNames = 1
	// runAltMarkdownTick is MARKDOWN's inline-code delimiter. It is not one of
	// Go's string quotes, and it is treated as syntax ONLY when it PAIRS — see
	// runAltTokens' own comment for the measured false stamp a naive reading
	// produced, and for why a paired tick is DROPPED rather than opened as a
	// quoting region.
	runAltMarkdownTick = '`'
	// runAltTicksPerPair is how many backticks make one markdown code span: a
	// tick is a DELIMITER only as half of a pair, which is the whole judgement
	// this file's backtick arm turns on (a lone tick stays content).
	runAltTicksPerPair = 2
)

// taskBriefingRunAlternativesReasons are the stated reasons a pattern could not
// be judged. Named, because a bare sentence at a fmt site is the shape Rule 1
// forbids and because the record is the only place a reader learns WHY.
const (
	runAltNoRepoReason       = "there is no ground to resolve the package pattern against"
	runAltOutsideReason      = "the package pattern resolves outside this ground, so this run cannot count its tests"
	runAltRecursiveReason    = "the package pattern is recursive (`./...`), which this clause does not expand"
	runAltNoPackageReason    = "the command names no package pattern this clause can resolve"
	runAltMissingDirReason   = "the package directory the command names is not present in this ground"
	runAltNoTestFilesReason  = "the package directory carries no _test.go file this clause could read (a scan that judged nothing is not a pass)"
	runAltNoTestNamesReason  = "the package's test files yielded no top-level `func Test…` line (a scan that judged nothing is not a pass)"
	runAltUnreadableReason   = "the package directory could not be listed"
	runAltTruncatedReason    = "the package carries more test files (or larger ones) than this clause will read, and a PARTIAL census produces false ZERO verdicts — so it is stated, never returned as a count"
	runAltBadRegexpReason    = "the alternative is not a valid Go regular expression"
	runAltTooManyReason      = "the pattern carries more alternatives than this clause will judge"
	runAltAlreadyStampedText = "the briefing already carries a dead-alternative stamp (an earlier pass appended it); no pattern was judged and nothing was appended"
	// TaskBriefingRunAltAlreadyStampedExcerpt is quoted in the record when this
	// clause meets its own stamp a second time.
	TaskBriefingRunAltAlreadyStampedExcerpt = "the briefing already carries a dead-alternative stamp"
)

const (
	// taskBriefingRunAltRecordFmt is the CENSUS line — one per alternative
	// judged, whatever the count. It is the honest minimum the epic asks for:
	// a zero is visible against its neighbours, in the acceptance output.
	taskBriefingRunAltRecordFmt = "briefing -run census → %q in %s: %d top-level test function(s) of %d in the package reach it (pattern %q)\n"
	// taskBriefingRunAltUnresolvedRecordFmt is the stated arm: a pattern this
	// clause could not judge, which is never a pass (Rule 15).
	taskBriefingRunAltUnresolvedRecordFmt = "briefing -run census → UNMEASURED (%s) — the pattern was NOT judged, which is not a pass: %s\n"
	// taskBriefingRunAltSubtestRecordFmt states that an alternative's subtest
	// half is outside what a source scan can count.
	taskBriefingRunAltSubtestRecordFmt = "briefing -run census → %q names a SUBTEST (%s); only its top-level test function %q is counted here\n"
	// taskBriefingRunAltStampedRecordFmt is the record for a ZERO alternative —
	// the finding, restated in the log beside its count.
	taskBriefingRunAltStampedRecordFmt = "briefing -run census → ZERO REACH %q in %s: no test function in the package matches it — STAMPED into the child's briefing (%s)\n"
)

// taskBriefingRunAltLineRepeatNote replaces a REPEATED quotation of the same
// briefing line. It is a pointer, never a truncation: the line itself is above,
// in full.
const taskBriefingRunAltLineRepeatNote = "  (the briefing line is the same one quoted above, for the same command — not repeated here)\n"

// taskBriefingRunAltHeadFmt introduces the clause. Its substitutions are the
// label and how many dead alternatives were found. It states the measurement and
// refuses the completion reading in its own words.
const taskBriefingRunAltHeadFmt = "%s\n" +
	"%d alternative(s) of a `go test -run` pattern this briefing writes as an ACCEPTANCE command reach NO test function in the package that command names. `go test` prints `no tests to run` and exits 0 for a pattern that matches nothing — so that family would never run, and the acceptance line would still read green.\n"

// taskBriefingRunAltRepairFmt is the PASTEABLE repair: the SAME command with the
// dead alternatives removed, so the reader is not left to construct a selector
// from prose. It is the half that makes this clause a check rather than a note —
// the seam does not rewrite the author's line (that is the submitter's prose), so
// it SUPPLIES the line that cannot produce `no tests to run`, explicitly labelled.
// Its substitutions are the package pattern and the surviving alternatives joined
// by Go's own separator.
const taskBriefingRunAltRepairFmt = "  REPLACEMENT (paste this — it cannot produce `no tests to run`): go test %s -run '%s'\n"

// taskBriefingRunAltNoRepairFmt is the STATED absence of a replacement: when EVERY
// alternative is dead there is no name to keep, and silence here would read like a
// repair that exists (Rule 15). Its one substitution is the count of alternatives.
const taskBriefingRunAltNoRepairFmt = "  NO REPLACEMENT EXISTS: all %d alternative(s) of this pattern are dead, so there is no selector to paste — DROP this acceptance line, or write the test it means and name it.\n"

// taskBriefingRunAltClaimFmt is the clause's per-alternative block: the pattern,
// the package, the dead alternative, what this ground counts, the instruction,
// the denial of the completion reading, and the quoted briefing line. Its
// substitutions are the alternative, the package, the pattern, the instruction
// constant, the counts, and the briefing line.
const taskBriefingRunAltClaimFmt = "- alternative %q of `go test %s -run %q` reaches NO test function in %s (this ground counts %d top-level test function(s) there, and none matches).\n" +
	"  %s: run `grep -n '^%s' %s/*_test.go` HERE to read the names that exist, then re-write the acceptance command with a name that does — or drop this alternative. An acceptance line carrying a name nothing implements is a check that cannot fail, which is not a check.\n" +
	"  This clause is NOT evidence that the work is done.\n" +
	"  briefing line: %q\n"

// TaskBriefingRunAlternativesRefresh judges, at dispatch time, every `go test
// <pkg> -run '<pattern>'` the briefing writes, and returns the clause to APPEND
// to the briefing the child receives plus the census record lines for every
// alternative examined.
//
// Returns ("", nil) when the briefing names no `-run` pattern — the ordinary
// case, and the reason this cannot degrade every briefing it touches.
func TaskBriefingRunAlternativesRefresh(repo, briefing string) (clause string, record []string) {
	if strings.Contains(briefing, TaskBriefingRunAlternativesLabel) {
		return "", []string{fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt,
			runAltAlreadyStampedText, TaskBriefingRunAltAlreadyStampedExcerpt)}
	}
	patterns := runAltPatternsIn(briefing)
	if len(patterns) == 0 {
		return "", nil
	}
	r := strings.TrimSpace(repo)
	type dead struct {
		alternative string
		pkg         string
		pattern     string
		names       int
		line        string
		// surviving are this pattern's alternatives that DO reach a test, in the
		// order the author wrote them. The REPLACEMENT line is built from them:
		// the point of naming a dead selector is to hand the reader one that
		// works, and re-deriving it from the pattern at render time would be a
		// SECOND reading of the same text (Rule 13) — one that could disagree
		// with the counts printed directly above it.
		surviving []string
		// patternSize is how many alternatives the pattern CARRIES (live and
		// dead), so the no-replacement arm can say "all N are dead" truthfully
		// instead of counting the dead ones it happens to have rendered.
		patternSize int
		// linePrinted makes the quoted briefing line appear ONCE per pattern
		// rather than once per dead alternative of it: the stamp is prepended
		// to a child's prompt and a repeated 200-character command is the
		// bloat that displaces the reader's attention from the finding.
		linePrinted bool
	}
	var stamped []dead
	printedLine := map[string]bool{}
	for i, p := range patterns {
		if i >= runAltMaxPatterns {
			record = append(record, fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt,
				runAltTooManyReason, p.line))
			continue
		}
		if r == "" {
			record = append(record, fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt,
				runAltNoRepoReason, p.line))
			continue
		}
		if p.pkg == "" {
			record = append(record, fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt,
				runAltNoPackageReason, p.line))
			continue
		}
		dir, reason := runAltPackageDir(r, p.pkg)
		if reason != "" {
			record = append(record, fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt, reason, p.line))
			continue
		}
		names, reason := runAltTestNames(dir)
		if reason != "" {
			record = append(record, fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt, reason, p.line))
			continue
		}
		alternatives := strings.Split(p.pattern, runAltAlternativeSeparator)
		if len(alternatives) > runAltMaxAlternatives {
			record = append(record, fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt,
				runAltTooManyReason, p.line))
			continue
		}
		var alive, gone []string
		liveCount := map[string]int{}
		for _, alt := range alternatives {
			a := strings.TrimSpace(alt)
			if a == "" {
				continue
			}
			top := a
			if i := strings.Index(a, runAltSubtestSeparator); i >= 0 {
				top = a[:i]
				record = append(record, fmt.Sprintf(taskBriefingRunAltSubtestRecordFmt,
					a, a[i+1:], top))
			}
			re, err := regexp.Compile(top)
			if err != nil {
				record = append(record, fmt.Sprintf(taskBriefingRunAltUnresolvedRecordFmt,
					fmt.Sprintf("%s: %v", runAltBadRegexpReason, err), p.line))
				continue
			}
			n := 0
			for _, name := range names {
				// Go's -run matches the pattern UNANCHORED against the test
				// name, so an alternative can be a fragment; this reads the
				// same way rather than inventing a stricter rule.
				if re.MatchString(name) {
					n++
				}
			}
			record = append(record, fmt.Sprintf(taskBriefingRunAltRecordFmt, a, p.pkg, n, len(names), p.pattern))
			liveCount[a] = n
			if n > 0 {
				alive = append(alive, a)
				continue
			}
			gone = append(gone, a)
		}
		// The REPLACEMENT is the pattern's SURVIVING alternatives in the author's
		// own order, computed over the whole pattern before any claim is
		// rendered — so a dead alternative standing in the MIDDLE of a pattern
		// does not hide the live one that follows it (the measured shape of the
		// epic's own line, where the live family stands last).
		for _, a := range gone {
			record = append(record, fmt.Sprintf(taskBriefingRunAltStampedRecordFmt, a, p.pkg, p.line))
			stamped = append(stamped, dead{alternative: a, pkg: p.pkg, pattern: p.pattern, names: len(names),
				line: p.line, surviving: append([]string(nil), alive...),
				patternSize: len(liveCount), linePrinted: printedLine[p.line]})
			printedLine[p.line] = true
		}
	}
	if len(stamped) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, taskBriefingRunAltHeadFmt, TaskBriefingRunAlternativesLabel, len(stamped))
	for _, d := range stamped {
		// The rendered command must be PASTEABLE (a double slash is not a
		// defect a reader should have to notice), so the package is spelled
		// once with its trailing separator removed.
		pkgDir := strings.TrimSuffix(d.pkg, "/")
		fmt.Fprintf(&b, taskBriefingRunAltClaimFmt,
			d.alternative, d.pkg, d.pattern, pkgDir, d.names,
			TaskBriefingRunAlternativesVerifyOnly, runAltTestFuncPrefix, pkgDir,
			d.line)
		if d.linePrinted {
			// The line already stands, in full, above this one (Rule: never
			// abbreviate a reference a reader may copy — this points at it).
			fmt.Fprint(&b, taskBriefingRunAltLineRepeatNote)
		}
		if len(d.surviving) == 0 {
			fmt.Fprintf(&b, taskBriefingRunAltNoRepairFmt, d.patternSize)
			continue
		}
		fmt.Fprintf(&b, taskBriefingRunAltRepairFmt, pkgDir,
			strings.Join(d.surviving, runAltAlternativeSeparator))
	}
	return strings.TrimRight(b.String(), "\n"), record
}

// runAltPattern is one `-run` pattern read off one briefing line, with the
// package pattern its own command names and the line it came from.
type runAltPattern struct {
	pattern string
	pkg     string
	line    string
}

// runAltPatternsIn extracts every `go test … -run '<pattern>'` the briefing
// writes. It is a TOKEN reader, not a prose scanner, because the shape it
// judges is a command: the package pattern and the flag value are the same
// command's other tokens.
func runAltPatternsIn(briefing string) []runAltPattern {
	var out []runAltPattern
	for _, line := range strings.Split(briefing, "\n") {
		if !strings.Contains(line, runAltGoTestInstrument) {
			continue
		}
		tokens := runAltTokens(line)
		value := ""
		for i := 0; i < len(tokens); i++ {
			t := tokens[i]
			if strings.HasPrefix(t, runAltFlagToken+runAltFlagAssignment) {
				value = strings.TrimPrefix(t, runAltFlagToken+runAltFlagAssignment)
				break
			}
			if t != runAltFlagToken {
				continue
			}
			if i+1 < len(tokens) {
				value = tokens[i+1]
			}
			break
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		pkg := ""
		for _, t := range tokens {
			if !strings.HasPrefix(t, runAltPackagePrefix) {
				continue
			}
			pkg = t
			break
		}
		out = append(out, runAltPattern{pattern: value, pkg: pkg, line: taskBriefingRunAltLineExcerpt(line)})
	}
	return out
}

// runAltTokens splits a command line into tokens, keeping QUOTED values whole
// and unquoting them: `-run 'A|B'` is one value, and its quotes are not part of
// the pattern Go receives.
//
// MARKDOWN INLINE CODE (the measured false stamp, bug row 01a1a6f6). A briefing
// wraps an acceptance command in backticks, and a backtick is NOT one of Go's
// string quotes — so a tokenizer that ignores it returns the `-run` value with
// the closing tick glued on (`TestB + "`"`), the alternative matches no test,
// and the clause stamps ZERO REACH over a test that exists and PASSES. The
// clause's own instruction would then DELETE a live acceptance check. Two
// spellings of the same command must therefore yield BYTE-IDENTICAL patterns.
//
// THE RULE IS PAIRING, AND A PAIRED TICK IS DROPPED — not opened as a quoting
// region. A code span delimits markdown, it does not quote: inside it the shell
// quoting is still there (`-run 'A|B'`), and `|` is an ALTERNATIVE rather than a
// pipe exactly when that quote is honoured. Opening the tick as a quote would
// eat the `'` and swallow the whole pattern as one value. So the condition is
// "this tick has a PARTNER still ahead of it on this line", counted left to
// right: an opener is dropped and its partner is dropped, and the tick itself is
// never content.
//
// THE COUNT IS ODD/EVEN, NOT "IS THERE ANY LATER TICK". The first draft asked
// only whether a later tick existed, which dropped the OPENER and left the
// CLOSER glued on — the stamp survived, one character shorter. Measured on this
// ground before the correction: wrapped pattern `TestProbeAlpha|TestProbeBeta` +
// "`" while the bare spelling was clean. So the tick's index within the line's
// tick sequence decides: an even index opens a span, an odd one closes it, and
// both are delimiters.
//
// UNPAIRED IS CONTENT (the other direction, deliberately preserved): a stray
// tick with no partner on its line leaves an ODD count, and the last one — whose
// index makes it an opener with nothing to open — stays a character of its
// token, so prose containing one never has bytes removed. Rule 15's shape: the
// answer is a measured pairing, never a blanket strip.
func runAltTokens(line string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	// ticksSeen counts this LINE's ticks ahead of the current position: an even
	// count means the tick at hand is an opener, odd means a closer. Recomputed
	// per call and never carried across lines (a code span cannot span one).
	pool := runAltTickPairing(line)
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == runAltMarkdownTick:
			if pool[i] {
				continue
			}
			cur.WriteRune(r)
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// runAltTickPairing marks, by BYTE index, which backticks on this line are a
// markdown code span's DELIMITERS. A tick is one iff the line's tick total is
// even and the tick is not the odd one out — i.e. pairs are formed left to
// right from the start of the line, and a leftover lone tick (an odd total)
// stays content. Pure, so the tokenizer above reads as one decision per rune
// and the pairing rule is testable on its own.
func runAltTickPairing(line string) map[int]bool {
	var at []int
	for i, r := range line {
		if r == runAltMarkdownTick {
			at = append(at, i)
		}
	}
	if len(at) < runAltTicksPerPair {
		return nil
	}
	paired := make(map[int]bool, len(at))
	for n := 0; n+runAltTicksPerPair <= len(at); n += runAltTicksPerPair {
		paired[at[n]] = true
		paired[at[n+1]] = true
	}
	return paired
}

// runAltPackageDir resolves one package pattern to a directory in this ground,
// stating the reason when it cannot — the first half of the vacuum floor.
func runAltPackageDir(repo, pkg string) (string, string) {
	if strings.Contains(pkg, runAltRecursiveSuffix) {
		return "", runAltRecursiveReason
	}
	rel := strings.TrimPrefix(pkg, runAltPackagePrefix)
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" || rel == "." {
		return "", runAltNoPackageReason
	}
	rootAbs, err := filepath.Abs(repo)
	if err != nil {
		return "", runAltOutsideReason
	}
	abs, err := filepath.Abs(filepath.Join(rootAbs, rel))
	if err != nil {
		return "", runAltOutsideReason
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		// A pattern that climbs out of the ground names another checkout's
		// population; this seam cannot count it and says so rather than
		// counting THIS tree's tests under another tree's name (Rules 8/15).
		return "", runAltOutsideReason
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Sprintf("%s: %v", runAltMissingDirReason, err)
	}
	if !info.IsDir() {
		return "", runAltMissingDirReason
	}
	return abs, ""
}

// runAltTestNames lists the top-level test function names a package directory
// declares in its own `*_test.go` files — the second half of the vacuum floor:
// zero NAMES is a failed scan, never a "zero reach" verdict.
func runAltTestNames(dir string) ([]string, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Sprintf("%s: %v", runAltUnreadableReason, err)
	}
	var names []string
	files := 0
	truncated := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), runAltTestFileSuffix) {
			continue
		}
		files++
		if files > runAltMaxFiles {
			truncated = true
			break
		}
		abs := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil || info.Size() > runAltMaxFileBytes {
			// A file this seam cannot read makes the census PARTIAL, and a
			// partial name list turns a live alternative into a false ZERO.
			// Stated, never skipped silently (Rule 15).
			truncated = true
			continue
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			truncated = true
			continue
		}
		for _, line := range strings.Split(string(body), "\n") {
			t := strings.TrimSpace(line)
			// A test function OPENS with the prefix at the start of a
			// left-trimmed line. A comment, a string literal, or a call site
			// never does, which is what keeps a non-test-file literal out of
			// the census.
			if !strings.HasPrefix(t, runAltTestFuncPrefix) {
				continue
			}
			if name, ok := runAltTestFuncName(t); ok {
				names = append(names, name)
			}
		}
	}
	if files == 0 {
		return nil, runAltNoTestFilesReason
	}
	if truncated {
		return nil, runAltTruncatedReason
	}
	if len(names) < runAltMinCensusNames {
		return nil, runAltNoTestNamesReason
	}
	return names, ""
}

// runAltTestFuncName reads the identifier out of a `func TestXxx(` line,
// rejecting the shapes that are not a top-level test: no name, a lowercase
// continuation, a method receiver (`func (x T) Test…` never reaches here
// because the prefix requires `func Test`), and `TestMain`, which testing
// treats as a hook rather than a test.
func runAltTestFuncName(line string) (string, bool) {
	rest := strings.TrimPrefix(line, runAltTestFuncPrefix)
	name := ""
	for _, r := range rest {
		if r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			name += string(r)
			continue
		}
		break
	}
	if name == "" {
		return "", false
	}
	full := runAltTestNamePrefix + name
	if full == runAltTestMainName {
		return "", false
	}
	return full, true
}

// runAltTestNamePrefix is the identifier prefix every test function's NAME
// carries, spelled once beside runAltTestFuncPrefix so the census and the
// rendered instruction cannot drift apart (Rules 1/13).
const runAltTestNamePrefix = "Test"

// runAltTestMainName is the one `Test…` identifier that is a hook rather than a
// test a `-run` pattern can select.
const runAltTestMainName = "TestMain"

// taskBriefingRunAltLineExcerpt bounds the quoted briefing line a stamp carries.
func taskBriefingRunAltLineExcerpt(s string) string {
	t := strings.TrimSpace(s)
	const maxRunes = 240
	r := []rune(t)
	if len(r) <= maxRunes {
		return t
	}
	return string(r[:maxRunes]) + "…"
}
